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
