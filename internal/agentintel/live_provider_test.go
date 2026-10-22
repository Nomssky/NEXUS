package agentintel

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/capability"
	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
)

const (
	liveProviderSmokeEnv = "NEXUS_LIVE_PROVIDER_SMOKE"
	liveProviderURLEnv   = "NEXUS_LIVE_PROVIDER_URL"
	liveWebEndpointEnv   = "NEXUS_LIVE_WEB_ENDPOINT"
	liveWebProviderEnv   = "NEXUS_LIVE_WEB_PROVIDER"
	liveModelEnv         = "NEXUS_LIVE_MODEL"
	liveCredentialEnv    = "TOKENHARBOR_API_KEY"
	liveBraveCredential  = "BRAVE_API_KEY"
)

type liveResearchCatalog struct{}

func (liveResearchCatalog) Catalog(_, _ string) []ToolHint {
	return []ToolHint{{
		ID: "web.search", Summary: "search the web and return validated source records",
		Operations: []string{"search"}, SideEffect: "read", InputShape: "query:string*",
	}}
}

type countingResearchProvider struct {
	inner capability.ResearchProvider
	calls int32
}

func (p *countingResearchProvider) Identify() string { return p.inner.Identify() }

func (p *countingResearchProvider) Search(ctx context.Context, query string, limit int) ([]capability.Source, error) {
	atomic.AddInt32(&p.calls, 1)
	return p.inner.Search(ctx, query, limit)
}

// TestLiveProviderEvidenceGroundedSynthesis is an opt-in smoke test for the
// production OpenAI-compatible provider path AND the real external web.search
// adapter. Model inference, planning and external retrieval are all live.
// Set NEXUS_LIVE_PROVIDER_SMOKE=1, TOKENHARBOR_API_KEY and optionally
// NEXUS_LIVE_MODEL / NEXUS_LIVE_WEB_ENDPOINT (Wikipedia) or
// NEXUS_LIVE_WEB_PROVIDER=brave + BRAVE_API_KEY to run it; otherwise it skips
// without network access.
func TestLiveProviderEvidenceGroundedSynthesis(t *testing.T) {
	if strings.TrimSpace(os.Getenv(liveProviderSmokeEnv)) != "1" {
		t.Skip("live provider smoke is opt-in: set NEXUS_LIVE_PROVIDER_SMOKE=1")
	}
	if strings.TrimSpace(os.Getenv(liveCredentialEnv)) == "" {
		t.Skip("live provider smoke requires TOKENHARBOR_API_KEY")
	}

	endpoint := strings.TrimSpace(os.Getenv(liveProviderURLEnv))
	if endpoint == "" {
		endpoint = "https://tokenharbor.ai/v1"
	}
	modelID := strings.TrimSpace(os.Getenv(liveModelEnv))
	if modelID == "" {
		modelID = "deepseek-v4.1-flash:free"
	}
	webEndpoint := strings.TrimSpace(os.Getenv(liveWebEndpointEnv))
	if webEndpoint == "" {
		webEndpoint = capability.DefaultWikipediaEndpoint
	}

	provider, err := modelrouter.NewOpenAIProvider(modelrouter.ProviderConfig{
		ID: "live-openai-compatible", Name: "Live OpenAI-compatible provider",
		Endpoint: endpoint, APIKeyEnv: liveCredentialEnv, Timeout: 90 * time.Second,
	})
	if err != nil {
		t.Fatalf("live provider configuration: %v", err)
	}
	registry := modelrouter.NewModelRegistry()
	router := modelrouter.NewModelRouter(registry, modelrouter.RoutingPolicyLocalFirst)
	router.RegisterProvider(provider)
	if err := registry.RegisterModel(&modelrouter.ModelDefinition{
		ID: modelID, ProviderID: provider.Identify(),
		Capabilities: []modelrouter.ModelCapability{
			modelrouter.CapabilityReasoning,
			modelrouter.CapabilityToolCalling,
			modelrouter.CapabilityStructuredOutput,
		},
		Runtime: modelrouter.RuntimeRemote, Status: modelrouter.ModelStatusActive,
		ExternalTransfer: true, DataRetention: "standard",
	}); err != nil {
		t.Fatalf("register live model: %v", err)
	}

	external, providerLabel, err := liveWebResearchProvider(webEndpoint)
	if err != nil {
		t.Fatalf("live web search configuration: %v", err)
	}
	counting := &countingResearchProvider{inner: external}
	rt := externalResearchRuntimeWithProvider(t, counting)
	rt.Decider = &Decision{AgentID: "researcher", Router: router}
	rt.Catalog = liveResearchCatalog{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	out, err := rt.Run(ctx, workRequest(t, Objective{
		Description: "Research two related topics using the mediated web.search tool on " + providerLabel + ". " +
			"The plan must contain exactly these two steps and nothing else: " +
			"{\"steps\":[{\"step_id\":\"s1\",\"intent\":\"search for Go programming language\"}," +
			"{\"step_id\":\"s2\",\"intent\":\"search for large language model\",\"dependencies\":[\"s1\"]}]}. " +
			"Step s1 must call web.search once with the query \"Go programming language\". " +
			"Step s2 must call web.search once with the query \"large language model\". " +
			"Do not call web.search twice in the same step. Only after both searches have run, " +
			"complete with a typed synthesis that cites at least one source id from each step. " +
			"Cite only ids shown in EVIDENCE, and use status partial only if a planned step remains incomplete.",
		Context: map[string]string{"agent_id": "researcher"},
	}, ""), nil)
	if err != nil {
		t.Fatalf("live research run failed: %v", err)
	}
	if out.Status != "completed" {
		// Never echo the prompt or raw model output here; the runtime error
		// carries the bounded validation reason.
		t.Fatalf("live research run status=%s error=%s", out.Status, out.Error)
	}
	ev := out.ResearchEvidence
	if ev == nil || ev.Version != execjournal.ResearchEvidenceVersion ||
		ev.Count < 2 || len(ev.Sources) < 2 {
		t.Fatalf("live run did not produce multi-source v2 evidence: %+v", ev)
	}
	for _, source := range ev.Sources {
		if external.Identify() == "wikipedia" {
			if !strings.HasSuffix(source.Source, ".wikipedia.org") ||
				!strings.HasPrefix(source.URL, "https://en.wikipedia.org/wiki/") {
				t.Fatalf("source is not a live Wikipedia record: %+v", source)
			}
			continue
		}
		parsed, err := url.Parse(source.URL)
		if err != nil || parsed.Host == "" ||
			(parsed.Scheme != "https" && parsed.Scheme != "http") ||
			source.Source != strings.ToLower(parsed.Hostname()) {
			t.Fatalf("source is not a normalized live provider record: %+v", source)
		}
	}
	if ev.Plan == nil || len(ev.Steps) != 2 {
		t.Fatalf("live plan did not produce two steps: %+v", ev.Plan)
	}
	if !slices.Contains(ev.Steps[1].Dependencies, ev.Steps[0].StepID) {
		t.Fatalf("second live step is not dependent on the first: %+v", ev.Steps)
	}
	if len(ev.Operations) < 2 {
		t.Fatalf("expected two mediated search operations, got %+v", ev.Operations)
	}
	stepSearches := map[string]bool{}
	for _, op := range ev.Operations {
		if op.ToolID != execjournal.ResearchEvidenceCapability ||
			op.Operation != execjournal.ResearchEvidenceOperation || len(op.SourceIDs) == 0 {
			t.Fatalf("operation is not a captured mediated search: %+v", op)
		}
		if stepSearches[op.StepID] {
			t.Fatalf("step %s searched more than once: %+v", op.StepID, ev.Operations)
		}
		stepSearches[op.StepID] = true
	}
	if len(stepSearches) != 2 {
		t.Fatalf("searches did not cover both steps: %+v", ev.Operations)
	}
	if got := atomic.LoadInt32(&counting.calls); got < 2 {
		t.Fatalf("expected at least two real provider requests, got %d", got)
	}
	if out.ResearchSynthesis == nil {
		t.Fatal("live run completed without a typed synthesis")
	}
	if out.ResearchSynthesis.Status != "complete" && out.ResearchSynthesis.Status != "partial" {
		t.Fatalf("live synthesis status=%q", out.ResearchSynthesis.Status)
	}
	resolved := map[string]bool{}
	for _, source := range ev.Sources {
		resolved[source.ID] = true
	}
	cited := map[string]bool{}
	for _, statement := range out.ResearchSynthesis.EvidenceBacked {
		for _, id := range statement.SourceIDs {
			if !resolved[id] {
				t.Fatalf("live synthesis cited an unknown source id %q", id)
			}
			cited[id] = true
		}
	}
	if len(cited) == 0 {
		t.Fatal("live synthesis has no evidence-backed citations")
	}
	// Require a citation that resolves from each captured search, proving the
	// synthesis used both steps rather than one lucky retrieval.
	for _, op := range ev.Operations {
		found := false
		for _, id := range op.SourceIDs {
			if cited[id] {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("synthesis cites no source from step %s: cited=%v op=%+v", op.StepID, cited, op)
		}
	}
	t.Logf("live provider %q model %q completed %d external provider requests, "+
		"%d evidence sources, %d search operations and %d evidence-backed citations",
		provider.Identify(), modelID, counting.calls, ev.Count, len(ev.Operations), len(cited))
}

// liveWebResearchProvider resolves the explicitly selected live web-search
// backend. There is no fallback: an unknown selection fails the opt-in test,
// and Brave requires its own credential.
func liveWebResearchProvider(wikipediaEndpoint string) (capability.ResearchProvider, string, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(liveWebProviderEnv))) {
	case "", "wikipedia":
		provider, err := capability.NewWikipediaSearchProvider(capability.WikipediaProviderConfig{
			Endpoint: wikipediaEndpoint, Language: "en", AllowInsecureHTTP: true,
		})
		return provider, "the English Wikipedia", err
	case "brave":
		provider, err := capability.NewBraveSearchProvider(capability.BraveProviderConfig{
			APIKey: os.Getenv(liveBraveCredential), Timeout: 15 * time.Second,
		})
		return provider, "the general web", err
	default:
		return nil, "", fmt.Errorf("NEXUS_LIVE_WEB_PROVIDER must be empty, \"wikipedia\", or \"brave\"")
	}
}
