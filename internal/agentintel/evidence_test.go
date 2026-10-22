package agentintel

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/agentexec"
	"github.com/Nomssky/NEXUS/internal/capability"
	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
	"github.com/Nomssky/NEXUS/internal/memory"
)

// The evidence fixture runs the loop over the REAL capability platform with a
// scripted web.search adapter, so capture, truncation rejection and terminal
// attachment are exercised through the same boundaries as production.
type webSearchFixture struct {
	rt       *Runtime
	provider *webSearchProvider
}

// webSearchProvider returns the provider envelopes a test script asks for.
type webSearchProvider struct {
	mu     sync.Mutex
	next   []string // queued envelopes, consumed in order
	output []map[string]string
	calls  int
}

func (p *webSearchProvider) Operations() []string { return []string{"search"} }

func (p *webSearchProvider) Invoke(_ context.Context, _ tool.Invocation) (tool.RawResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if len(p.output) == 0 {
		return tool.RawResult{}, fmt.Errorf("no scripted search output")
	}
	out := p.output[0]
	p.output = p.output[1:]
	return tool.RawResult{Result: out}, nil
}

func (p *webSearchProvider) queue(out map[string]string) {
	p.output = append(p.output, out)
}

func (p *webSearchProvider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func webSearchManifest(maxOutput int) tool.ToolManifest {
	limits := tool.ResourceLimits{MaxDuration: 5 * time.Second, MaxItems: 10}
	if maxOutput > 0 {
		limits.MaxOutputByte = maxOutput
	}
	return tool.ToolManifest{
		ID: "web.search", Version: "1.0.0", Name: "web.search", Description: "scripted search",
		Category:        tool.ToolCategoryRead,
		InputSchema:     tool.Schema{Fields: []tool.SchemaField{{Name: "query", Type: tool.FieldString, MaxLength: 512}}},
		OutputSchema:    tool.Schema{Fields: []tool.SchemaField{{Name: "sources", Type: tool.FieldString, MaxLength: 32768}, {Name: "count", Type: tool.FieldString, MaxLength: 8}}},
		SideEffectClass: tool.SideEffectRead, NetworkRequirement: tool.NetworkNone,
		ScopeRequirement: tool.ScopeBusiness, SecurityClass: tool.SecuritySandboxed,
		Operations: []string{"search"}, ResourceLimits: limits,
	}
}

// newEvidenceFixture wires the platform and one researcher agent allowed to use
// web.search. It mirrors the governed fixture's wiring.
func newEvidenceFixture(t *testing.T) *webSearchFixture {
	t.Helper()
	provider := &webSearchProvider{}
	return mustEvidenceFixtureWithManifest(t, provider, webSearchManifest(0))
}

func mustEvidenceFixtureWithManifest(t *testing.T, provider *webSearchProvider, manifest tool.ToolManifest) *webSearchFixture {
	t.Helper()
	orgs := testOrg(t)
	tools := tool.NewToolRegistry()
	memberships := testMemberships(t)
	platform := capability.New(tools, memberships,
		capability.NewScopedCredentialResolver(security.NewDevResolver()), event.NewMemBus())
	if err := tools.Register(manifest, provider); err != nil {
		t.Fatal(err)
	}
	agents := agentexec.NewRegistry("nx:test:nexus", orgs, tools)
	exec := agentexec.NewRuntime(agents, tools,
		modelrouter.NewModelRouter(modelrouter.NewModelRegistry(), modelrouter.RoutingPolicyLocalFirst), event.NewMemBus())
	exec.Platform = platform
	ctl, _, _ := allowAllController()
	exec.Controller = ctl
	if _, err := agents.Create(agentexec.Definition{
		ID: "researcher", Name: "Researcher", BusinessID: "biz-1", Capabilities: []string{"research"},
		AllowedTools: []string{"web.search"},
	}, "t"); err != nil {
		t.Fatal(err)
	}
	mem, err := OpenMemory(store.NewMemStore(), Deps{Scopes: memory.NewMembershipScopes(memberships.AllowsScope)})
	if err != nil {
		t.Fatal(err)
	}
	rt := New(agents, exec, &scriptedDecider{plan: evidencePlan}, event.NewMemBus(), mem)
	return &webSearchFixture{rt: rt, provider: provider}
}

const evidencePlan = `{"steps":[{"step_id":"s1","intent":"search the web","preferred_agent":"researcher"}]}`

func runEvidenceObjective(t *testing.T, f *webSearchFixture, answers ...string) *executor.Outcome {
	t.Helper()
	f.rt.Decider = &scriptedDecider{plan: evidencePlan, answers: answers}
	out, err := f.rt.Run(context.Background(), workRequest(t, Objective{Description: "research"}, ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func envelopeOf(lines ...string) map[string]string {
	return map[string]string{"sources": strings.Join(lines, "\n"), "count": fmt.Sprint(len(lines))}
}

// The terminal outcome carries the captured snapshot, and the snapshot is the
// real provider envelope, not model prose.
func TestWebSearchCapturesCompleteSnapshot(t *testing.T) {
	f := newEvidenceFixture(t)
	f.provider.queue(envelopeOf(
		"1. Nexus capability boundary | https://docs.example/nexus | docs.example | The runtime decides; the model proposes.",
		"2. External content is data | https://example.org/safety | example.org | Inert | data",
	))
	out := runEvidenceObjective(t, f,
		`{"type":"tool_call","tool":"web.search","operation":"search","input":{"query":"nexus"}}`,
		`{"type":"complete","result":"report ready"}`)
	if out.Status != "completed" {
		t.Fatalf("status=%s err=%s", out.Status, out.Error)
	}
	ev := out.ResearchEvidence
	if ev == nil {
		t.Fatal("a complete web.search snapshot must be captured")
	}
	if ev.Count != 2 || len(ev.Sources) != 2 {
		t.Fatalf("unexpected snapshot size: %+v", ev)
	}
	if ev.Sources[0].Title != "Nexus capability boundary" || ev.Sources[0].URL != "https://docs.example/nexus" ||
		ev.Sources[1].Snippet != "Inert | data" {
		t.Fatalf("provider fields mangled: %+v", ev.Sources)
	}
}

// Multiple searches in ONE execution flatten in call order. They are evidence of
// the same execution's searches, never records from another execution.
func TestWebSearchEvidenceMergesPerExecutionSnapshots(t *testing.T) {
	f := newEvidenceFixture(t)
	f.provider.queue(envelopeOf("1. First | https://first.example/a | first.example | one"))
	f.provider.queue(envelopeOf(
		"1. Second | https://second.example/b | second.example | two",
		"2. Third | https://third.example/c | third.example | three",
	))
	out := runEvidenceObjective(t, f,
		`{"type":"tool_call","tool":"web.search","operation":"search","input":{"query":"one"}}`,
		`{"type":"tool_call","tool":"web.search","operation":"search","input":{"query":"two"}}`,
		`{"type":"complete","result":"report ready"}`)
	ev := out.ResearchEvidence
	if ev == nil || ev.Count != 3 || len(ev.Sources) != 3 {
		t.Fatalf("merged snapshot expected 3 sources, got %+v", ev)
	}
	want := []string{"First", "Second", "Third"}
	for i, w := range want {
		if ev.Sources[i].Title != w {
			t.Fatalf("source %d = %q, want %q", i, ev.Sources[i].Title, w)
		}
	}
}

// Repeated delivery of the same mediated operation id never duplicates source
// records or associations. Distinct operations in different steps stay
// distinct and keep their stable source ids.
func TestCaptureResearchEvidenceIsIdempotentPerOperation(t *testing.T) {
	rn := &run{}
	first := &execjournal.ResearchEvidence{
		Version: execjournal.ResearchEvidenceVersion, Capability: execjournal.ResearchEvidenceCapability,
		Operation: execjournal.ResearchEvidenceOperation, Count: 1,
		Sources: []execjournal.ResearchSource{{ID: "local-1", Title: "One", URL: "https://one.example"}},
	}
	rn.captureResearchEvidence(first, "s1", "obs-1")
	rn.captureResearchEvidence(first, "s1", "obs-1")
	if rn.researchEvidence == nil || rn.researchEvidence.Count != 1 ||
		len(rn.researchEvidence.Sources) != 1 || len(rn.researchEvidence.Operations) != 1 {
		t.Fatalf("repeated delivery duplicated evidence: %+v", rn.researchEvidence)
	}
	if rn.researchEvidence.Sources[0].ID != "src-1" ||
		!slices.Equal(rn.researchEvidence.Operations[0].SourceIDs, []string{"src-1"}) {
		t.Fatalf("execution-scoped source id/association wrong: %+v", rn.researchEvidence)
	}

	second := &execjournal.ResearchEvidence{
		Version: execjournal.ResearchEvidenceVersion, Capability: execjournal.ResearchEvidenceCapability,
		Operation: execjournal.ResearchEvidenceOperation, Count: 1,
		Sources: []execjournal.ResearchSource{{ID: "local-1", Title: "Two", URL: "https://two.example"}},
	}
	rn.captureResearchEvidence(second, "s2", "obs-2")
	if rn.researchEvidence.Count != 2 || len(rn.researchEvidence.Operations) != 2 {
		t.Fatalf("second operation not captured: %+v", rn.researchEvidence)
	}
	if rn.researchEvidence.Sources[1].ID != "src-2" {
		t.Fatalf("source ids must stay execution-scoped: %+v", rn.researchEvidence.Sources)
	}
	if rn.researchEvidence.Operations[1].StepID != "s2" ||
		!slices.Equal(rn.researchEvidence.Operations[1].SourceIDs, []string{"src-2"}) {
		t.Fatalf("second step association wrong: %+v", rn.researchEvidence.Operations[1])
	}
}

// A resumed run can carry a v52/v53 evidence snapshot. Legacy rows have no
// ids, so merging v2 rows or operations would invent identity and produce an
// invalid record. The persisted legacy snapshot stays readable unchanged.
func TestCaptureResearchEvidenceDoesNotUpgradeLegacySnapshot(t *testing.T) {
	rn := &run{researchEvidence: &execjournal.ResearchEvidence{
		Version:    execjournal.ResearchEvidenceLegacyVersion,
		Capability: execjournal.ResearchEvidenceCapability, Operation: execjournal.ResearchEvidenceOperation,
		Count: 1, Sources: []execjournal.ResearchSource{
			{Title: "Legacy", URL: "https://legacy.example"},
		},
	}}
	incoming := &execjournal.ResearchEvidence{
		Version: execjournal.ResearchEvidenceVersion, Capability: execjournal.ResearchEvidenceCapability,
		Operation: execjournal.ResearchEvidenceOperation, Count: 1,
		Sources: []execjournal.ResearchSource{{ID: "src-1", Title: "New", URL: "https://new.example"}},
	}
	rn.captureResearchEvidence(incoming, "s1", "obs-1")
	ev := rn.researchEvidence
	if ev.Version != execjournal.ResearchEvidenceLegacyVersion || ev.Count != 1 ||
		len(ev.Sources) != 1 || ev.Sources[0].ID != "" || ev.Sources[0].Title != "Legacy" ||
		len(ev.Operations) != 0 || ev.Plan != nil || len(ev.Steps) != 0 {
		t.Fatalf("legacy evidence must not be upgraded or mixed: %+v", ev)
	}
	if err := execjournal.ValidateResearchEvidenceForExecution("exec-1", ev); err != nil {
		t.Fatalf("legacy evidence must remain readable: %v", err)
	}
}

// The terminal snapshot attaches only steps that exist in the final plan. A
// source whose step was dropped by a replan stays present but unattributed.
func TestFinalizeResearchEvidenceDropsOnlyUnprovableStepReferences(t *testing.T) {
	rn := &run{
		plan: &Plan{PlanID: "exec-1", Version: 2, Steps: []Step{
			{StepID: "s1", Intent: "gather", Dependencies: nil},
			{StepID: "s2", Intent: "summarize", Dependencies: []string{"s1"}},
		}},
		ledger: &planLedger{
			planID: "exec-1", version: 2,
			order: []string{"s1", "s2"},
			steps: map[string]*Step{
				"s1": {StepID: "s1", Intent: "gather"},
				"s2": {StepID: "s2", Intent: "summarize", Dependencies: []string{"s1"}},
			},
			records: map[string]*StepRecord{
				"s1": {StepID: "s1", Status: StepCompleted, PlanVersion: 1, ObservationID: "obs-1"},
				"s2": {StepID: "s2", Status: StepReady, PlanVersion: 2},
			},
		},
		researchEvidence: &execjournal.ResearchEvidence{
			Version: execjournal.ResearchEvidenceVersion, Capability: execjournal.ResearchEvidenceCapability,
			Operation: execjournal.ResearchEvidenceOperation, Count: 2,
			Sources: []execjournal.ResearchSource{
				{ID: "src-1", Title: "One", URL: "https://one.example"},
				{ID: "src-2", Title: "Dropped", URL: "https://dropped.example"},
			},
			Operations: []execjournal.ResearchOperationEvidence{
				{OperationID: "obs-1", StepID: "s1", ToolID: execjournal.ResearchEvidenceCapability,
					Operation: execjournal.ResearchEvidenceOperation, SourceIDs: []string{"src-1"}},
				{OperationID: "obs-9", StepID: "s9", ToolID: execjournal.ResearchEvidenceCapability,
					Operation: execjournal.ResearchEvidenceOperation, SourceIDs: []string{"src-2"}},
			},
		},
	}
	rn.finalizeResearchEvidence(rn.plan, rn.ledger)
	ev := rn.researchEvidence
	if ev.Plan == nil || ev.Plan.PlanID != "exec-1" || ev.Plan.Version != 2 ||
		len(ev.Steps) != 2 || len(ev.Operations) != 1 {
		t.Fatalf("unexpected finalized provenance: %+v", ev)
	}
	if ev.Operations[0].StepID != "s1" || !slices.Equal(ev.Operations[0].SourceIDs, []string{"src-1"}) {
		t.Fatalf("dropped-step operation must not be reassigned: %+v", ev.Operations[0])
	}
	if err := execjournal.ValidateResearchEvidence(ev); err != nil {
		t.Fatalf("finalized provenance must validate: %v", err)
	}
}

// A plan step without a ledger record has no provable lifecycle fact. It is
// omitted with its operation instead of being assigned an invented status;
// the source stays present and unattributed.
func TestFinalizeResearchEvidenceOmitsUnprovenStep(t *testing.T) {
	rn := &run{
		plan: &Plan{PlanID: "exec-1", Version: 1, Steps: []Step{
			{StepID: "s1", Intent: "gather"},
			{StepID: "s2", Intent: "summarize", Dependencies: []string{"s1"}},
		}},
		ledger: &planLedger{
			planID: "exec-1", version: 1,
			order: []string{"s1", "s2"},
			steps: map[string]*Step{
				"s1": {StepID: "s1", Intent: "gather"},
				"s2": {StepID: "s2", Intent: "summarize", Dependencies: []string{"s1"}},
			},
			records: map[string]*StepRecord{
				"s1": {StepID: "s1", Status: StepCompleted, PlanVersion: 1, ObservationID: "obs-1"},
			},
		},
		researchEvidence: &execjournal.ResearchEvidence{
			Version: execjournal.ResearchEvidenceVersion, Capability: execjournal.ResearchEvidenceCapability,
			Operation: execjournal.ResearchEvidenceOperation, Count: 2,
			Sources: []execjournal.ResearchSource{
				{ID: "src-1", Title: "One", URL: "https://one.example"},
				{ID: "src-2", Title: "Unproven", URL: "https://unproven.example"},
			},
			Operations: []execjournal.ResearchOperationEvidence{
				{OperationID: "obs-1", StepID: "s1", ToolID: execjournal.ResearchEvidenceCapability,
					Operation: execjournal.ResearchEvidenceOperation, SourceIDs: []string{"src-1"}},
				{OperationID: "obs-2", StepID: "s2", ToolID: execjournal.ResearchEvidenceCapability,
					Operation: execjournal.ResearchEvidenceOperation, SourceIDs: []string{"src-2"}},
			},
		},
	}
	rn.finalizeResearchEvidence(rn.plan, rn.ledger)
	ev := rn.researchEvidence
	if ev.Plan == nil || ev.Plan.PlanID != "exec-1" || len(ev.Steps) != 1 || len(ev.Operations) != 1 {
		t.Fatalf("unproven step must be omitted: %+v", ev)
	}
	if ev.Steps[0].StepID != "s1" || ev.Operations[0].StepID != "s1" {
		t.Fatalf("proven step must survive: %+v", ev)
	}
	if err := execjournal.ValidateResearchEvidenceForExecution("exec-1", ev); err != nil {
		t.Fatalf("finalized provenance must validate: %v", err)
	}
}

// If the settled plan cannot satisfy the evidence contract (for example a
// model-authored step id beyond the durable bound), provenance is dropped
// rather than turning the terminal write into a failure. Sources stay present
// but unattributed.
func TestFinalizeResearchEvidenceDropsInvalidProvenance(t *testing.T) {
	longStepID := strings.Repeat("s", execjournal.MaxResearchStepID+1)
	rn := &run{
		plan: &Plan{PlanID: "exec-1", Version: 1, Steps: []Step{
			{StepID: longStepID, Intent: "gather"},
		}},
		ledger: &planLedger{
			planID: "exec-1", version: 1,
			order: []string{longStepID},
			steps: map[string]*Step{
				longStepID: {StepID: longStepID, Intent: "gather"},
			},
			records: map[string]*StepRecord{
				longStepID: {StepID: longStepID, Status: StepCompleted, PlanVersion: 1, ObservationID: "obs-1"},
			},
		},
		researchEvidence: &execjournal.ResearchEvidence{
			Version: execjournal.ResearchEvidenceVersion, Capability: execjournal.ResearchEvidenceCapability,
			Operation: execjournal.ResearchEvidenceOperation, Count: 1,
			Sources: []execjournal.ResearchSource{{ID: "src-1", Title: "One", URL: "https://one.example"}},
			Operations: []execjournal.ResearchOperationEvidence{
				{OperationID: "obs-1", StepID: longStepID, ToolID: execjournal.ResearchEvidenceCapability,
					Operation: execjournal.ResearchEvidenceOperation, SourceIDs: []string{"src-1"}},
			},
		},
	}
	rn.finalizeResearchEvidence(rn.plan, rn.ledger)
	ev := rn.researchEvidence
	if ev.Plan != nil || len(ev.Steps) != 0 || len(ev.Operations) != 0 {
		t.Fatalf("invalid provenance must be dropped: %+v", ev)
	}
	if ev.Count != 1 || len(ev.Sources) != 1 || ev.Sources[0].ID != "src-1" {
		t.Fatalf("source records must remain: %+v", ev)
	}
	if err := execjournal.ValidateResearchEvidenceForExecution("exec-1", ev); err != nil {
		t.Fatalf("dropped provenance must still validate: %v", err)
	}
}

// An empty complete result is evidence that the search returned nothing, not
// evidence of nothing.
func TestWebSearchEmptyResultIsStillEvidence(t *testing.T) {
	f := newEvidenceFixture(t)
	f.provider.queue(map[string]string{"sources": "", "count": "0"})
	out := runEvidenceObjective(t, f,
		`{"type":"tool_call","tool":"web.search","operation":"search","input":{"query":"nothing"}}`,
		`{"type":"complete","result":"nothing found"}`)
	if out.Status != "completed" {
		t.Fatalf("status=%s err=%s", out.Status, out.Error)
	}
	if out.ResearchEvidence == nil {
		t.Fatal("an empty but complete result must be captured")
	}
	if out.ResearchEvidence.Count != 0 || len(out.ResearchEvidence.Sources) != 0 {
		t.Fatalf("empty snapshot must stay empty: %+v", out.ResearchEvidence)
	}
	if out.ResearchEvidence.Sources == nil {
		t.Fatal("empty sources must stay a JSON array, not null")
	}
	raw, err := json.Marshal(out.ResearchEvidence)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"sources":[]`) {
		t.Fatalf("present empty evidence must serialize sources as []: %s", raw)
	}
	if len(out.ResearchEvidence.Operations) != 1 ||
		out.ResearchEvidence.Operations[0].SourceIDs == nil ||
		!strings.Contains(string(raw), `"source_ids":[]`) {
		t.Fatalf("empty operation must serialize source_ids as []: %s", raw)
	}
}

// A truncated platform result may succeed as a tool result but can never become
// evidence: evidence is only for a complete, unredacted envelope.
func TestWebSearchTruncatedResultIsNotEvidence(t *testing.T) {
	provider := &webSearchProvider{}
	f := mustEvidenceFixtureWithManifest(t, provider, webSearchManifest(64))
	provider.queue(map[string]string{
		"sources": "1. Nexus capability boundary | https://docs.example/nexus | docs.example | The runtime decides; the model proposes.",
		"count":   "1",
	})
	out := runEvidenceObjective(t, f,
		`{"type":"tool_call","tool":"web.search","operation":"search","input":{"query":"nexus"}}`,
		`{"type":"complete","result":"report ready"}`)
	if out.Status != "completed" {
		t.Fatalf("status=%s err=%s", out.Status, out.Error)
	}
	if out.ResearchEvidence != nil {
		t.Fatalf("a truncated snapshot must not become evidence: %+v", out.ResearchEvidence)
	}
}

// A malformed or count-mismatched envelope is never evidence. The tool result is
// still observable, so the execution itself is unchanged.
func TestWebSearchMalformedEnvelopeIsNotEvidence(t *testing.T) {
	cases := map[string]map[string]string{
		"count mismatch":  {"sources": "1. A | https://a.example | a | s", "count": "2"},
		"no index":        {"sources": "A | https://a.example | a | s", "count": "1"},
		"missing sources": {"count": "1"},
	}
	for name, out := range cases {
		t.Run(name, func(t *testing.T) {
			f := newEvidenceFixture(t)
			f.provider.queue(out)
			res := runEvidenceObjective(t, f,
				`{"type":"tool_call","tool":"web.search","operation":"search","input":{"query":"x"}}`,
				`{"type":"complete","result":"done"}`)
			if res.Status != "completed" {
				t.Fatalf("status=%s err=%s", res.Status, res.Error)
			}
			if res.ResearchEvidence != nil {
				t.Fatalf("malformed envelope must not become evidence: %+v", res.ResearchEvidence)
			}
		})
	}
}

// Flattened snapshots never exceed the durable bound. If they would, the whole
// accumulated snapshot is dropped and nothing partial survives.
func TestWebSearchEvidenceOverflowDropsTheWholeSnapshot(t *testing.T) {
	f := newEvidenceFixture(t)
	// 6 searches x 9 sources = 54 > the 50-source durable bound.
	lines := make([]string, 9)
	for i := range lines {
		lines[i] = fmt.Sprintf("%d. S | https://example.test/%d | example.test | snippet", i+1, i)
	}
	sources := strings.Join(lines, "\n")
	for i := 0; i < 6; i++ {
		f.provider.queue(map[string]string{"sources": sources, "count": "9"})
	}
	answers := make([]string, 0, 7)
	for i := 0; i < 6; i++ {
		answers = append(answers, `{"type":"tool_call","tool":"web.search","operation":"search","input":{"query":"q"}}`)
	}
	answers = append(answers, `{"type":"complete","result":"done"}`)
	out := runEvidenceObjective(t, f, answers...)
	if out.Status != "completed" {
		t.Fatalf("status=%s err=%s", out.Status, out.Error)
	}
	if out.ResearchEvidence != nil {
		t.Fatalf("overflow must not expose a partial snapshot: %+v", out.ResearchEvidence)
	}
}

// Evidence is an internal executor hand-off: it is NOT serializable on the
// process-local authoring surface. Only the durable journal read may expose it.
func TestOutcomeEvidenceIsNotSerialized(t *testing.T) {
	f := newEvidenceFixture(t)
	f.provider.queue(envelopeOf("1. A | https://a.example | a | s"))
	out := runEvidenceObjective(t, f,
		`{"type":"tool_call","tool":"web.search","operation":"search","input":{"query":"a"}}`,
		`{"type":"complete","result":"done"}`)
	if out.ResearchEvidence == nil {
		t.Fatal("setup: evidence was not captured")
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "research_evidence") {
		t.Fatalf("process-local evidence must not be serialized: %s", encoded)
	}
}

// Each execution owns its accumulator. A second run in the same process must
// start from zero, never inherit the previous execution's records.
func TestEvidenceAccumulatorIsExecutionScoped(t *testing.T) {
	f := newEvidenceFixture(t)
	f.provider.queue(envelopeOf("1. A | https://a.example | a | s"))
	if out := runEvidenceObjective(t, f,
		`{"type":"tool_call","tool":"web.search","operation":"search","input":{"query":"a"}}`,
		`{"type":"complete","result":"done"}`); out.ResearchEvidence == nil {
		t.Fatal("first run must capture evidence")
	}
	f.provider.queue(envelopeOf("1. B | https://b.example | b | s"))
	out := runEvidenceObjective(t, f,
		`{"type":"tool_call","tool":"web.search","operation":"search","input":{"query":"b"}}`,
		`{"type":"complete","result":"done"}`)
	if out.ResearchEvidence == nil || out.ResearchEvidence.Count != 1 ||
		out.ResearchEvidence.Sources[0].Title != "B" {
		t.Fatalf("the second run must not inherit the first's records: %+v", out.ResearchEvidence)
	}
	if f.provider.count() != 2 {
		t.Fatalf("each run must perform its own search, got %d", f.provider.count())
	}
}
