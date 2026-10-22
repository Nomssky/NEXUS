package agentintel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/agentexec"
	"github.com/Nomssky/NEXUS/internal/capability"
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
