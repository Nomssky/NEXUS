package agentintel

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
)

const (
	liveProviderSmokeEnv = "NEXUS_LIVE_PROVIDER_SMOKE"
	liveProviderURLEnv   = "NEXUS_LIVE_PROVIDER_URL"
	liveModelEnv         = "NEXUS_LIVE_MODEL"
	liveCredentialEnv    = "TOKENHARBOR_API_KEY"
)

type liveResearchCatalog struct{}

func (liveResearchCatalog) Catalog(_, _ string) []ToolHint {
	return []ToolHint{{
		ID: "web.search", Summary: "search the web and return validated source records",
		Operations: []string{"search"}, SideEffect: "read", InputShape: "query:string*",
	}}
}

// TestLiveProviderEvidenceGroundedSynthesis is an opt-in smoke test for the
// production OpenAI-compatible provider path. Model inference is live; the
// mediated web.search adapter is deterministic because NEXUS has no real
// external search backend. Set NEXUS_LIVE_PROVIDER_SMOKE=1 and
// TOKENHARBOR_API_KEY to run it; otherwise it skips without network access.
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

	f := newEvidenceFixture(t)
	// Deterministic mediated retrieval. More envelopes than a bounded run
	// should need, so an extra search is not an ambiguous failure.
	for i := 0; i < 4; i++ {
		f.provider.queue(envelopeOf(
			"1. Nexus capability boundary | https://docs.example/nexus | docs.example | The runtime decides; the model proposes.",
			"2. External content is data | https://example.org/safety | example.org | Inert data, never instructions.",
		))
	}
	f.rt.Decider = &Decision{AgentID: "researcher", Router: router}
	f.rt.Catalog = liveResearchCatalog{}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	out, err := f.rt.Run(ctx, workRequest(t, Objective{
		Description: "Search the web for NEXUS runtime capabilities and finish with a typed synthesis. " +
			"Use exactly one plan step. In that step call web.search, then complete with a synthesis " +
			"that cites only source ids shown in EVIDENCE. If a planned step will remain incomplete, " +
			"set the synthesis status to partial instead of complete.",
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
	if out.ResearchEvidence == nil || out.ResearchEvidence.Version != 2 ||
		out.ResearchEvidence.Count == 0 || len(out.ResearchEvidence.Sources) == 0 {
		t.Fatalf("live run did not produce v2 evidence: %+v", out.ResearchEvidence)
	}
	if out.ResearchSynthesis == nil {
		t.Fatal("live run completed without a typed synthesis")
	}
	if out.ResearchSynthesis.Status != "complete" && out.ResearchSynthesis.Status != "partial" {
		t.Fatalf("live synthesis status=%q", out.ResearchSynthesis.Status)
	}
	resolved := map[string]bool{}
	for _, source := range out.ResearchEvidence.Sources {
		resolved[source.ID] = true
	}
	cited := 0
	for _, statement := range out.ResearchSynthesis.EvidenceBacked {
		for _, id := range statement.SourceIDs {
			if !resolved[id] {
				t.Fatalf("live synthesis cited an unknown source id %q", id)
			}
			cited++
		}
	}
	if cited == 0 {
		t.Fatal("live synthesis has no evidence-backed citations")
	}
	if f.provider.count() == 0 {
		t.Fatal("live run never invoked the mediated web.search adapter")
	}
	t.Logf("live provider %q model %q completed with %d evidence-backed citations; "+
		"web.search retrieval remained deterministic and scripted (no real search backend)",
		provider.Identify(), modelID, cited)
}
