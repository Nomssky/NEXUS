package agentintel

// Integration tests that run the real external-search adapter through the SAME
// mediated capability platform and intelligence loop as production. The
// provider's HTTP endpoint is a local fixture, so these tests are deterministic
// and never claim live external retrieval.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/agentexec"
	"github.com/Nomssky/NEXUS/internal/capability"
	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
	"github.com/Nomssky/NEXUS/internal/memory"
)

const externalResearchPlan = `{"steps":[` +
	`{"step_id":"s1","intent":"search the first topic","required_capabilities":[]},` +
	`{"step_id":"s2","intent":"search a dependent topic","required_capabilities":[],"dependencies":["s1"]}]}`

func externalResearchRuntime(t *testing.T, endpoint string) *Runtime {
	t.Helper()
	provider, err := capability.NewWikipediaSearchProvider(capability.WikipediaProviderConfig{
		Endpoint: endpoint, Language: "en", AllowInsecureHTTP: true,
	})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	return externalResearchRuntimeWithProvider(t, provider)
}

func externalResearchRuntimeWithProvider(t *testing.T, provider capability.ResearchProvider) *Runtime {
	t.Helper()
	web := &capability.WebSearchTool{Provider: provider, MaxSnippet: 512, MaxResults: 10}
	return evidenceRuntimeWithAdapter(t, capability.WebSearchManifest(), web)
}

// evidenceRuntimeWithAdapter wires the real platform, the real adapter and one
// researcher agent. It mirrors mustEvidenceFixtureWithManifest, which is typed
// to the scripted envelope provider.
func evidenceRuntimeWithAdapter(t *testing.T, manifest tool.ToolManifest, adapter tool.Adapter) *Runtime {
	t.Helper()
	orgs := testOrg(t)
	tools := tool.NewToolRegistry()
	memberships := testMemberships(t)
	platform := capability.New(tools, memberships,
		capability.NewScopedCredentialResolver(security.NewDevResolver()), event.NewMemBus())
	if err := tools.Register(manifest, adapter); err != nil {
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
	return New(agents, exec, &scriptedDecider{plan: externalResearchPlan}, event.NewMemBus(), mem)
}

func wikiFixture(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestRealExternalAdapterThroughMediatedPlatform(t *testing.T) {
	srv, calls := wikiFixture(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("srsearch")
		title := "Alpha source"
		page := 11
		if q == "beta" {
			title, page = "Beta source", 22
		}
		fmt.Fprintf(w, `{"batchcomplete":true,"query":{"search":[`+
			`{"title":%q,"pageid":%d,"snippet":"Excerpt for <span class=\"searchmatch\">%s</span>"}]}}`,
			title, page, q)
	})

	rt := externalResearchRuntime(t, srv.URL+"/w/api.php")
	rt.Decider = &scriptedDecider{
		plan: externalResearchPlan,
		answers: []string{
			`{"type":"tool_call","tool":"web.search","operation":"search","input":{"query":"alpha"}}`,
			`{"type":"tool_call","tool":"web.search","operation":"search","input":{"query":"beta"}}`,
			groundedCompletion("src-1", "src-2"),
		},
	}
	out, err := rt.Run(context.Background(), workRequest(t, Objective{
		ObjectiveID: "exec-external", RequestID: "exec-external",
		Description: "research the external topic",
	}, ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "completed" {
		t.Fatalf("status=%s error=%s", out.Status, out.Error)
	}
	if got := atomic.LoadInt32(calls); got != 2 {
		t.Fatalf("expected 2 real provider requests, got %d", got)
	}
	ev := out.ResearchEvidence
	if ev == nil || ev.Version != execjournal.ResearchEvidenceVersion || ev.Count != 2 {
		t.Fatalf("unexpected evidence: %+v", ev)
	}
	if ev.Sources[0].Title != "Alpha source" || ev.Sources[0].URL != "https://en.wikipedia.org/wiki/Alpha_source" {
		t.Fatalf("first provider record not preserved: %+v", ev.Sources[0])
	}
	if ev.Sources[1].Title != "Beta source" || ev.Sources[1].URL != "https://en.wikipedia.org/wiki/Beta_source" {
		t.Fatalf("second provider record not preserved: %+v", ev.Sources[1])
	}
	if ev.Plan == nil || ev.Plan.PlanID != "exec-1" || len(ev.Steps) != 2 || len(ev.Operations) != 2 {
		t.Fatalf("provenance missing: %+v", ev)
	}
	want := map[string][]string{"s1": {"src-1"}, "s2": {"src-2"}}
	for _, op := range ev.Operations {
		if op.ToolID != execjournal.ResearchEvidenceCapability || op.Operation != execjournal.ResearchEvidenceOperation {
			t.Fatalf("operation is not a mediated search: %+v", op)
		}
		if !slices.Equal(op.SourceIDs, want[op.StepID]) {
			t.Fatalf("step %s association = %v, want %v", op.StepID, op.SourceIDs, want[op.StepID])
		}
		delete(want, op.StepID)
	}
	if len(want) != 0 {
		t.Fatalf("missing step associations: %v", want)
	}
	if err := execjournal.ValidateResearchEvidenceForExecution("exec-1", ev); err != nil {
		t.Fatalf("evidence invalid: %v", err)
	}
	if out.ResearchSynthesis == nil || len(out.ResearchSynthesis.EvidenceBacked) != 1 ||
		!slices.Equal(out.ResearchSynthesis.EvidenceBacked[0].SourceIDs, []string{"src-1", "src-2"}) {
		t.Fatalf("synthesis citations did not resolve: %+v", out.ResearchSynthesis)
	}
}

func TestLiveProviderFailureNeverFallsBackToScriptedEvidence(t *testing.T) {
	srv, _ := wikiFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"message":"unavailable"}`)
	})
	rt := externalResearchRuntime(t, srv.URL+"/w/api.php")
	rt.Decider = &scriptedDecider{
		plan: externalResearchPlan,
		answers: []string{
			`{"type":"tool_call","tool":"web.search","operation":"search","input":{"query":"alpha"}}`,
		},
	}
	out, err := rt.Run(context.Background(), workRequest(t, Objective{Description: "research the external topic"}, ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "failed" || !strings.Contains(out.Error, "external error") {
		t.Fatalf("provider failure must fail closed: status=%s error=%q", out.Status, out.Error)
	}
	if out.ResearchEvidence != nil {
		t.Fatalf("failed retrieval must not yield evidence: %+v", out.ResearchEvidence)
	}
	if strings.Contains(out.Output, "docs.example") || strings.Contains(out.Error, "docs.example") {
		t.Fatalf("scripted provider must not substitute for the failed live provider: %q %q", out.Output, out.Error)
	}
}

func braveResearchRuntime(t *testing.T, endpoint, key string) (*Runtime, *int32) {
	t.Helper()
	provider, err := capability.NewBraveSearchProvider(capability.BraveProviderConfig{
		APIKey: key, Endpoint: endpoint, AllowInsecureHTTP: true, Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("brave provider: %v", err)
	}
	var calls int32
	counting := &countingResearchProvider{inner: provider}
	rt := externalResearchRuntimeWithProvider(t, &callCounter{provider: counting, calls: &calls})
	return rt, &calls
}

// callCounter counts provider invocations without changing behavior. The live
// test already has a similar helper; this one is local to the deterministic
// integration test.
type callCounter struct {
	provider capability.ResearchProvider
	calls    *int32
}

func (p *callCounter) Identify() string { return p.provider.Identify() }

func (p *callCounter) Search(ctx context.Context, query string, limit int) ([]capability.Source, error) {
	atomic.AddInt32(p.calls, 1)
	return p.provider.Search(ctx, query, limit)
}

func TestBraveProviderThroughMediatedPlatform(t *testing.T) {
	const key = "brave-integration-test-key-0123456789"
	var gotKey string
	srv, calls := wikiFixture(t, func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Subscription-Token")
		q := r.URL.Query().Get("q")
		title, resultURL := "Alpha general result", "https://alpha.example/article"
		if q == "beta" {
			title, resultURL = "Beta general result", "https://beta.example/report"
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"type":"search","query":{"original":%q},"web":{"results":[`+
			`{"title":%q,"url":%q,"description":"Grounded excerpt for %s","page_age":"3 days ago"}]}}`,
			q, title, resultURL, q)
	})

	rt, providerCalls := braveResearchRuntime(t, srv.URL+"/res/v1/web/search", key)
	rt.Decider = &scriptedDecider{
		plan: externalResearchPlan,
		answers: []string{
			`{"type":"tool_call","tool":"web.search","operation":"search","input":{"query":"alpha"}}`,
			`{"type":"tool_call","tool":"web.search","operation":"search","input":{"query":"beta"}}`,
			groundedCompletion("src-1", "src-2"),
		},
	}
	out, err := rt.Run(context.Background(), workRequest(t, Objective{
		Description: "research the general web",
	}, ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "completed" {
		t.Fatalf("status=%s error=%s", out.Status, out.Error)
	}
	if gotKey != key {
		t.Fatal("Brave subscription header was not sent through the mediated path")
	}
	if atomic.LoadInt32(calls) != 2 || atomic.LoadInt32(providerCalls) != 2 {
		t.Fatalf("expected 2 provider requests, fixture=%d adapter=%d", *calls, *providerCalls)
	}
	ev := out.ResearchEvidence
	if ev == nil || ev.Version != execjournal.ResearchEvidenceVersion || ev.Count != 2 {
		t.Fatalf("unexpected evidence: %+v", ev)
	}
	if ev.Sources[0].Title != "Alpha general result" || ev.Sources[0].URL != "https://alpha.example/article" ||
		ev.Sources[0].Source != "alpha.example" || ev.Sources[0].Snippet != "Grounded excerpt for alpha" {
		t.Fatalf("first Brave record not preserved: %+v", ev.Sources[0])
	}
	if ev.Sources[1].Title != "Beta general result" || ev.Sources[1].URL != "https://beta.example/report" ||
		ev.Sources[1].Source != "beta.example" {
		t.Fatalf("second Brave record not preserved: %+v", ev.Sources[1])
	}
	if ev.Plan == nil || len(ev.Steps) != 2 || len(ev.Operations) != 2 {
		t.Fatalf("provenance missing: %+v", ev)
	}
	want := map[string][]string{"s1": {"src-1"}, "s2": {"src-2"}}
	for _, op := range ev.Operations {
		if op.ToolID != execjournal.ResearchEvidenceCapability || op.Operation != execjournal.ResearchEvidenceOperation {
			t.Fatalf("operation is not a mediated search: %+v", op)
		}
		if !slices.Equal(op.SourceIDs, want[op.StepID]) {
			t.Fatalf("step %s association = %v, want %v", op.StepID, op.SourceIDs, want[op.StepID])
		}
		delete(want, op.StepID)
	}
	if len(want) != 0 {
		t.Fatalf("missing step associations: %v", want)
	}
	if err := execjournal.ValidateResearchEvidenceForExecution("exec-1", ev); err != nil {
		t.Fatalf("evidence invalid: %v", err)
	}
	if out.ResearchSynthesis == nil || len(out.ResearchSynthesis.EvidenceBacked) != 1 ||
		!slices.Equal(out.ResearchSynthesis.EvidenceBacked[0].SourceIDs, []string{"src-1", "src-2"}) {
		t.Fatalf("synthesis citations did not resolve: %+v", out.ResearchSynthesis)
	}
	if strings.Contains(ev.Sources[0].Snippet, "3 days ago") {
		t.Fatal("provider freshness metadata must not be treated as page content")
	}
}

func TestBraveCredentialFailureNeverFallsBackToScriptedEvidence(t *testing.T) {
	const key = "brave-integration-test-key-0123456789"
	var calls int32
	srv, _ := wikiFixture(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, `{"detail":%q}`, key)
	})
	rt, providerCalls := braveResearchRuntime(t, srv.URL+"/res/v1/web/search", key)
	rt.Decider = &scriptedDecider{
		plan: externalResearchPlan,
		answers: []string{
			`{"type":"tool_call","tool":"web.search","operation":"search","input":{"query":"alpha"}}`,
		},
	}
	out, err := rt.Run(context.Background(), workRequest(t, Objective{Description: "research the general web"}, ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "failed" || !strings.Contains(out.Error, "credential") {
		t.Fatalf("credential failure must fail closed: status=%s error=%q", out.Status, out.Error)
	}
	if out.ResearchEvidence != nil {
		t.Fatalf("failed retrieval must not yield evidence: %+v", out.ResearchEvidence)
	}
	if atomic.LoadInt32(&calls) != 1 || atomic.LoadInt32(providerCalls) != 1 {
		t.Fatalf("expected exactly one provider attempt, fixture=%d adapter=%d", calls, *providerCalls)
	}
	if strings.Contains(out.Output, "docs.example") || strings.Contains(out.Error, "docs.example") {
		t.Fatalf("scripted provider must not substitute for the failed Brave provider: %q %q", out.Output, out.Error)
	}
	if strings.Contains(out.Error, key) {
		t.Fatalf("credential leaked into execution error: %q", out.Error)
	}
}

// A real normalized Brave record enters evidence through the same capture path
// as every other provider. A synthesis whose citation does not resolve in this
// execution fails closed, while the captured evidence remains a fact.
func TestBraveNormalizedEvidenceRejectsInvalidCitation(t *testing.T) {
	const key = "brave-integration-test-key-0123456789"
	srv, _ := wikiFixture(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"type":"search","web":{"results":[`+
			`{"title":"Only general result","url":"https://general.example/only","description":"Grounded excerpt"}]}}`)
	})
	rt, _ := braveResearchRuntime(t, srv.URL+"/res/v1/web/search", key)
	rt.Decider = &scriptedDecider{
		plan: evidencePlan,
		answers: []string{
			`{"type":"tool_call","tool":"web.search","operation":"search","input":{"query":"alpha"}}`,
			groundedCompletion("src-999"),
		},
	}
	out, err := rt.Run(context.Background(), workRequest(t, Objective{Description: "research the general web"}, ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "failed" || !strings.Contains(out.Error, "unknown source") {
		t.Fatalf("invalid Brave citation must fail closed: status=%s error=%q", out.Status, out.Error)
	}
	if out.ResearchSynthesis != nil {
		t.Fatalf("invalid synthesis must not be exposed: %+v", out.ResearchSynthesis)
	}
	if out.ResearchEvidence == nil || out.ResearchEvidence.Count != 1 ||
		out.ResearchEvidence.Sources[0].Title != "Only general result" ||
		out.ResearchEvidence.Sources[0].URL != "https://general.example/only" {
		t.Fatalf("captured Brave evidence must remain a fact: %+v", out.ResearchEvidence)
	}
}
