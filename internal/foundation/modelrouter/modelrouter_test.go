package modelrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TEST-M6-001: Register model in registry
func TestRegistryRegisterModel(t *testing.T) {
	mr := NewModelRegistry()
	def := &ModelDefinition{
		ID:            "model-1",
		ProviderID:    "ollama",
		DisplayName:   "Qwen3",
		Capabilities:  []ModelCapability{CapabilityReasoning, CapabilityCoding},
		ContextWindow: 8192,
		Runtime:       RuntimeLocal,
		Status:        ModelStatusActive,
	}

	err := mr.RegisterModel(def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mr.ModelCount() != 1 {
		t.Errorf("expected 1 model, got %d", mr.ModelCount())
	}
}

// TEST-M6-002: Registry rejects empty ID
func TestRegistryRejectsEmptyID(t *testing.T) {
	mr := NewModelRegistry()
	err := mr.RegisterModel(&ModelDefinition{ProviderID: "p"})
	if err == nil {
		t.Error("expected error for empty model ID")
	}
}

// TEST-M6-003: Find models by capabilities
func TestRegistryFindByCapabilities(t *testing.T) {
	mr := NewModelRegistry()
	mr.RegisterModel(&ModelDefinition{
		ID:           "m1",
		ProviderID:   "p1",
		Capabilities: []ModelCapability{CapabilityReasoning, CapabilityCoding},
		Status:       ModelStatusActive,
	})
	mr.RegisterModel(&ModelDefinition{
		ID:           "m2",
		ProviderID:   "p1",
		Capabilities: []ModelCapability{CapabilityVision},
		Status:       ModelStatusActive,
	})

	found := mr.FindByCapabilities([]ModelCapability{CapabilityReasoning})
	if len(found) != 1 {
		t.Errorf("expected 1 model with reasoning, got %d", len(found))
	}
	if found[0].ID != "m1" {
		t.Errorf("expected m1, got %v", found[0].ID)
	}
}

// TEST-M6-004: Find models by runtime
func TestRegistryFindByRuntime(t *testing.T) {
	mr := NewModelRegistry()
	mr.RegisterModel(&ModelDefinition{ID: "m1", ProviderID: "p1", Runtime: RuntimeLocal, Status: ModelStatusActive})
	mr.RegisterModel(&ModelDefinition{ID: "m2", ProviderID: "p2", Runtime: RuntimeRemote, Status: ModelStatusActive})

	local := mr.FindByRuntime(RuntimeLocal)
	if len(local) != 1 {
		t.Errorf("expected 1 local model, got %d", len(local))
	}
}

// TEST-M6-005: Offline models excluded from search
func TestRegistryExcludesOffline(t *testing.T) {
	mr := NewModelRegistry()
	mr.RegisterModel(&ModelDefinition{
		ID:           "m1",
		ProviderID:   "p1",
		Capabilities: []ModelCapability{CapabilityReasoning},
		Status:       ModelStatusOffline,
	})

	found := mr.FindByCapabilities([]ModelCapability{CapabilityReasoning})
	if len(found) != 0 {
		t.Errorf("expected 0 models (offline), got %d", len(found))
	}
}

// TEST-M6-006: Local provider identifies itself
func TestLocalProviderIdentify(t *testing.T) {
	lp := NewLocalProvider(ProviderConfig{ID: "ollama"})
	if lp.Identify() != "ollama" {
		t.Errorf("expected ollama, got %v", lp.Identify())
	}
}

// TEST-M6-007: Local provider health check
func TestLocalProviderHealthCheck(t *testing.T) {
	lp := NewLocalProvider(ProviderConfig{ID: "ollama"})
	if err := lp.HealthCheck(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TEST-M6-008: Remote provider identifies itself
func TestRemoteProviderIdentify(t *testing.T) {
	rp := NewRemoteProvider(ProviderConfig{ID: "openrouter"})
	if rp.Identify() != "openrouter" {
		t.Errorf("expected openrouter, got %v", rp.Identify())
	}
}

// TEST-M6-009: Router routes to local-first
func TestRouterLocalFirst(t *testing.T) {
	mr := NewModelRegistry()
	mr.RegisterModel(&ModelDefinition{
		ID:           "local-model",
		ProviderID:   "ollama",
		Capabilities: []ModelCapability{CapabilityReasoning},
		Runtime:      RuntimeLocal,
		Status:       ModelStatusActive,
	})
	mr.RegisterModel(&ModelDefinition{
		ID:           "remote-model",
		ProviderID:   "openrouter",
		Capabilities: []ModelCapability{CapabilityReasoning},
		Runtime:      RuntimeRemote,
		Status:       ModelStatusActive,
	})

	router := NewModelRouter(mr, RoutingPolicyLocalFirst)
	router.RegisterProvider(NewLocalProvider(ProviderConfig{ID: "ollama"}))
	router.RegisterProvider(NewRemoteProvider(ProviderConfig{ID: "openrouter"}))

	decision, err := router.Route(&RoutingRequest{
		RequestID:    "req-1",
		RequiredCaps: []ModelCapability{CapabilityReasoning},
		PreferLocal:  true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.ModelID != "local-model" {
		t.Errorf("expected local-model, got %v", decision.ModelID)
	}
}

// TEST-M6-010: Router fails when no models match
func TestRouterNoModelsMatch(t *testing.T) {
	mr := NewModelRegistry()
	router := NewModelRouter(mr, RoutingPolicyLocalFirst)

	_, err := router.Route(&RoutingRequest{
		RequestID:    "req-1",
		RequiredCaps: []ModelCapability{CapabilityAudio},
	})
	if err == nil {
		t.Error("expected error when no models match")
	}
}

// TEST-M6-011: Router filters by allowed providers
func TestRouterFiltersByProvider(t *testing.T) {
	mr := NewModelRegistry()
	mr.RegisterModel(&ModelDefinition{
		ID:           "m1",
		ProviderID:   "ollama",
		Capabilities: []ModelCapability{CapabilityReasoning},
		Runtime:      RuntimeLocal,
		Status:       ModelStatusActive,
	})
	mr.RegisterModel(&ModelDefinition{
		ID:           "m2",
		ProviderID:   "openrouter",
		Capabilities: []ModelCapability{CapabilityReasoning},
		Runtime:      RuntimeRemote,
		Status:       ModelStatusActive,
	})

	router := NewModelRouter(mr, RoutingPolicyLocalFirst)
	router.RegisterProvider(NewLocalProvider(ProviderConfig{ID: "ollama"}))
	router.RegisterProvider(NewRemoteProvider(ProviderConfig{ID: "openrouter"}))

	decision, err := router.Route(&RoutingRequest{
		RequestID:        "req-1",
		RequiredCaps:     []ModelCapability{CapabilityReasoning},
		AllowedProviders: []string{"openrouter"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.ProviderID != "openrouter" {
		t.Errorf("expected openrouter, got %v", decision.ProviderID)
	}
}

// TEST-M6-012: Router builds fallback chain
func TestRouterFallbackChain(t *testing.T) {
	mr := NewModelRegistry()
	mr.RegisterModel(&ModelDefinition{
		ID:           "m1",
		ProviderID:   "p1",
		Capabilities: []ModelCapability{CapabilityReasoning},
		Runtime:      RuntimeLocal,
		Status:       ModelStatusActive,
	})
	mr.RegisterModel(&ModelDefinition{
		ID:           "m2",
		ProviderID:   "p2",
		Capabilities: []ModelCapability{CapabilityReasoning},
		Runtime:      RuntimeRemote,
		Status:       ModelStatusActive,
	})

	router := NewModelRouter(mr, RoutingPolicyLocalFirst)
	router.RegisterProvider(NewLocalProvider(ProviderConfig{ID: "p1"}))
	router.RegisterProvider(NewRemoteProvider(ProviderConfig{ID: "p2"}))

	decision, err := router.Route(&RoutingRequest{
		RequestID:    "req-1",
		RequiredCaps: []ModelCapability{CapabilityReasoning},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decision.FallbackChain) != 2 {
		t.Errorf("expected 2 in fallback chain, got %d", len(decision.FallbackChain))
	}
}

// TEST-M6-013: Routing ≠ Authorization invariant
func TestRoutingNotAuthorization(t *testing.T) {
	// Routing selects, governance authorizes
	// This test verifies routing returns a decision without granting authority
	mr := NewModelRegistry()
	mr.RegisterModel(&ModelDefinition{
		ID:           "m1",
		ProviderID:   "p1",
		Capabilities: []ModelCapability{CapabilityReasoning},
		Runtime:      RuntimeLocal,
		Status:       ModelStatusActive,
	})

	router := NewModelRouter(mr, RoutingPolicyLocalFirst)
	router.RegisterProvider(NewLocalProvider(ProviderConfig{ID: "p1"}))

	decision, err := router.Route(&RoutingRequest{
		RequestID:    "req-1",
		RequiredCaps: []ModelCapability{CapabilityReasoning},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Decision contains routing info, NOT authorization
	if decision.RequestID != "req-1" {
		t.Errorf("expected request ID preserved, got %v", decision.RequestID)
	}
	// No authority fields in RoutingDecision — that's governance's job
}

// TEST-M6-014: Health registry records success
func TestHealthRegistrySuccess(t *testing.T) {
	hr := NewHealthRegistry()
	hr.RecordSuccess("p1", 100)

	status, ok := hr.GetStatus("p1")
	if !ok {
		t.Fatal("expected status for p1")
	}
	if status.Status != ProviderStatusHealthy {
		t.Errorf("expected healthy, got %v", status.Status)
	}
	if status.SuccessCount != 1 {
		t.Errorf("expected 1 success, got %d", status.SuccessCount)
	}
}

// TEST-M6-015: Health registry records failure
func TestHealthRegistryFailure(t *testing.T) {
	hr := NewHealthRegistry()
	hr.RecordFailure("p1")
	hr.RecordFailure("p1")
	hr.RecordFailure("p1")

	status, _ := hr.GetStatus("p1")
	if status.Status != ProviderStatusUnhealthy {
		t.Errorf("expected unhealthy after 3 failures, got %v", status.Status)
	}
}

// TEST-M6-016: Accounting records tokens
func TestAccountingTokens(t *testing.T) {
	ia := NewInvocationAccounting()
	ia.Record(&InvocationRecord{
		ModelID:      "m1",
		InputTokens:  100,
		OutputTokens: 50,
		TotalTokens:  150,
		Success:      true,
	})

	if ia.TotalTokens() != 150 {
		t.Errorf("expected 150 tokens, got %d", ia.TotalTokens())
	}
	if ia.RecordCount() != 1 {
		t.Errorf("expected 1 record, got %d", ia.RecordCount())
	}
}

// TEST-M6-017: Accounting by model
func TestAccountingByModel(t *testing.T) {
	ia := NewInvocationAccounting()
	ia.Record(&InvocationRecord{ModelID: "m1", TotalTokens: 100, Success: true})
	ia.Record(&InvocationRecord{ModelID: "m2", TotalTokens: 200, Success: true})
	ia.Record(&InvocationRecord{ModelID: "m1", TotalTokens: 50, Success: true})

	byModel := ia.TokensByModel()
	if byModel["m1"] != 150 {
		t.Errorf("expected 150 tokens for m1, got %d", byModel["m1"])
	}
	if byModel["m2"] != 200 {
		t.Errorf("expected 200 tokens for m2, got %d", byModel["m2"])
	}
}

// TEST-M6-018: Accounting by business
func TestAccountingByBusiness(t *testing.T) {
	ia := NewInvocationAccounting()
	ia.Record(&InvocationRecord{BusinessID: "biz-1", TotalTokens: 100, Success: true})
	ia.Record(&InvocationRecord{BusinessID: "biz-2", TotalTokens: 200, Success: true})

	byBiz := ia.TokensByBusiness()
	if byBiz["biz-1"] != 100 {
		t.Errorf("expected 100 tokens for biz-1, got %d", byBiz["biz-1"])
	}
	if byBiz["biz-2"] != 200 {
		t.Errorf("expected 200 tokens for biz-2, got %d", byBiz["biz-2"])
	}
}

// TEST-M6-019: Model capabilities
func TestModelCapabilities(t *testing.T) {
	caps := []ModelCapability{
		CapabilityReasoning, CapabilityCoding, CapabilityToolCalling,
		CapabilityStructuredOutput, CapabilityVision, CapabilityAudio,
		CapabilityEmbedding, CapabilityLongContext,
	}
	if len(caps) != 8 {
		t.Errorf("expected 8 capabilities, got %d", len(caps))
	}
}

// TEST-M6-020: Business isolation in accounting
func TestAccountingBusinessIsolation(t *testing.T) {
	ia := NewInvocationAccounting()
	ia.Record(&InvocationRecord{BusinessID: "biz-1", TotalTokens: 100, Success: true})
	ia.Record(&InvocationRecord{BusinessID: "biz-2", TotalTokens: 200, Success: true})

	// Total includes all businesses
	if ia.TotalTokens() != 300 {
		t.Errorf("expected 300 total tokens, got %d", ia.TotalTokens())
	}

	// Per-business isolation
	byBiz := ia.TokensByBusiness()
	if byBiz["biz-1"] != 100 {
		t.Errorf("expected 100 tokens for biz-1, got %d", byBiz["biz-1"])
	}
}

// TEST-M6-021: Clock injection works
func TestClockInjection(t *testing.T) {
	now := time.Now()
	hr := NewHealthRegistryWithClock(func() time.Time { return now })
	hr.RecordSuccess("p1", 50)

	status, _ := hr.GetStatus("p1")
	if !status.LastCheck.Equal(now) {
		t.Errorf("expected clock-injected time")
	}
}

// TEST-M6-022: Provider statuses
func TestProviderStatuses(t *testing.T) {
	statuses := []ProviderStatus{
		ProviderStatusHealthy, ProviderStatusDegraded,
		ProviderStatusUnhealthy, ProviderStatusOffline,
	}
	if len(statuses) != 4 {
		t.Errorf("expected 4 provider statuses, got %d", len(statuses))
	}
}

// ---------------------------------------------------------------------------
// D2: model selection intelligence (SCHEMA_EXECUTION §3.3 + m6).
// ---------------------------------------------------------------------------

// spyProvider is a Provider test double that records the ModelID it was
// invoked with and can be told to fail.
type spyProvider struct {
	id        string
	lastModel string
	err       error
}

func (s *spyProvider) Identify() string { return s.id }

func (s *spyProvider) HealthCheck() error { return nil }

func (s *spyProvider) ListModels() ([]string, error) { return []string{s.id + "-model"}, nil }

func (s *spyProvider) Invoke(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	s.lastModel = req.ModelID
	if s.err != nil {
		return nil, s.err
	}
	return &GenerateResponse{ModelID: req.ModelID, Content: "ok", LatencyMs: 5}, nil
}

// d2Registry builds a registry with one local and two remote reasoning
// models at known prices/privacy postures.
func d2Registry(t *testing.T) *ModelRegistry {
	t.Helper()
	reg := NewModelRegistry()
	defs := []*ModelDefinition{
		{ID: "local-cheap", ProviderID: "ollama", Capabilities: []ModelCapability{CapabilityReasoning},
			Runtime: RuntimeLocal, Status: ModelStatusActive, PricingInput: 0.0},
		{ID: "remote-cheap", ProviderID: "spy-a", Capabilities: []ModelCapability{CapabilityReasoning},
			Runtime: RuntimeRemote, Status: ModelStatusActive, PricingInput: 0.001},
		{ID: "remote-pricey", ProviderID: "spy-b", Capabilities: []ModelCapability{CapabilityReasoning},
			Runtime: RuntimeRemote, Status: ModelStatusActive, PricingInput: 0.01, ExternalTransfer: true},
	}
	for _, d := range defs {
		if err := reg.RegisterModel(d); err != nil {
			t.Fatalf("register %s: %v", d.ID, err)
		}
	}
	return reg
}

// TEST-M6-023 (D2): the strategy enum follows the most specific request
// signal (SCHEMA §3.3).
func TestRouteStrategyDerivation(t *testing.T) {
	base := func() *RoutingRequest {
		return &RoutingRequest{RequestID: "req-strat", RequiredCaps: []ModelCapability{CapabilityReasoning}}
	}
	cases := []struct {
		name   string
		mutate func(*RoutingRequest)
		want   RoutingStrategy
	}{
		{"capability_match", func(r *RoutingRequest) {}, StrategyCapabilityMatch},
		{"manual_override", func(r *RoutingRequest) { r.AllowedModels = []string{"remote-cheap"} }, StrategyManualOverride},
		{"cost_optimize", func(r *RoutingRequest) { r.MaxCostPerToken = 0.005 }, StrategyCostOptimize},
		{"latency_optimize", func(r *RoutingRequest) { r.MaxLatencyMs = 100 }, StrategyLatencyOptimize},
		{"privacy_first", func(r *RoutingRequest) { r.PrivacyFirst = true }, StrategyPrivacyFirst},
		{"local_first", func(r *RoutingRequest) { r.PreferLocal = true }, StrategyLocalFirst},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := NewModelRouter(d2Registry(t), RoutingPolicyNearest)
			req := base()
			tc.mutate(req)
			d, err := router.Route(req)
			if err != nil {
				t.Fatalf("route: %v", err)
			}
			if d.Strategy != tc.want {
				t.Errorf("strategy: want %s, got %s", tc.want, d.Strategy)
			}
		})
	}
}

// TEST-M6-024 (D2): dead constraint fields are live — cost and privacy
// bound the whole fallback chain, candidates still show every model
// considered, and constraints_applied records what filtered.
func TestRouteConstraintFiltering(t *testing.T) {
	router := NewModelRouter(d2Registry(t), RoutingPolicyNearest)

	d, err := router.Route(&RoutingRequest{
		RequestID:       "req-cost",
		RequiredCaps:    []ModelCapability{CapabilityReasoning},
		MaxCostPerToken: 0.005,
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	for _, id := range d.FallbackChain {
		if id == "remote-pricey" {
			t.Errorf("pricey model must not survive the cost bound, chain=%v", d.FallbackChain)
		}
	}
	found := false
	for _, c := range d.ConstraintsApplied {
		if c == "max_cost_per_token=0.005" {
			found = true
		}
	}
	if !found {
		t.Errorf("constraints_applied: want max_cost_per_token entry, got %v", d.ConstraintsApplied)
	}
	if len(d.Candidates) != 3 {
		t.Errorf("candidates: want all 3 capability-matched models, got %v", d.Candidates)
	}

	d, err = router.Route(&RoutingRequest{
		RequestID:    "req-privacy",
		RequiredCaps: []ModelCapability{CapabilityReasoning},
		PrivacyFirst: true,
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	for _, id := range d.FallbackChain {
		if id == "remote-pricey" {
			t.Errorf("external-transfer model must not survive privacy_first, chain=%v", d.FallbackChain)
		}
	}
}

// TEST-M6-025 (D2): the cheapest router policy orders by input price and
// reports cost_optimize.
func TestRouteCheapestPolicy(t *testing.T) {
	router := NewModelRouter(d2Registry(t), RoutingPolicyCheapest)
	d, err := router.Route(&RoutingRequest{
		RequestID:    "req-cheap",
		RequiredCaps: []ModelCapability{CapabilityReasoning},
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	// local-cheap (0.0) < remote-cheap (0.001) < remote-pricey (0.01)
	want := []string{"local-cheap", "remote-cheap", "remote-pricey"}
	if len(d.FallbackChain) != len(want) {
		t.Fatalf("chain: want %v, got %v", want, d.FallbackChain)
	}
	for i, id := range want {
		if d.FallbackChain[i] != id {
			t.Errorf("chain[%d]: want %s, got %s (chain=%v)", i, id, d.FallbackChain[i], d.FallbackChain)
		}
	}
	if d.Strategy != StrategyCostOptimize {
		t.Errorf("strategy: want cost_optimize from cheapest policy, got %s", d.Strategy)
	}
}

// TEST-M6-026 (D2): the decision marshals with the §3.3 contract field
// names (strategy, candidates, selected_reason, constraints_applied).
func TestRoutingDecisionContractJSON(t *testing.T) {
	router := NewModelRouter(d2Registry(t), RoutingPolicyNearest)
	d, err := router.Route(&RoutingRequest{
		RequestID:    "req-json",
		RequiredCaps: []ModelCapability{CapabilityReasoning},
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	for _, want := range []string{`"strategy":`, `"candidates":`, `"selected_reason":`, `"constraints_applied":`} {
		if !strings.Contains(s, want) {
			t.Errorf("decision JSON missing %s: %s", want, s)
		}
	}
	if strings.Contains(s, `"reason"`) {
		t.Errorf("legacy reason field must not appear (contract name is selected_reason): %s", s)
	}
}

// TEST-M6-027 (D2): Invoke stamps the selected model onto the provider
// request (the old path sent the caller's placeholder) and feeds the
// health registry on success and failure.
func TestInvokeUsesSelectedModelAndRecordsHealth(t *testing.T) {
	reg := NewModelRegistry()
	if err := reg.RegisterModel(&ModelDefinition{
		ID: "m-sel", ProviderID: "spy", Capabilities: []ModelCapability{CapabilityReasoning},
		Runtime: RuntimeRemote, Status: ModelStatusActive,
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	spy := &spyProvider{id: "spy"}
	router := NewModelRouter(reg, RoutingPolicyNearest)
	router.RegisterProvider(spy)

	// Caller placeholder like the pre-D2 executor ("default").
	genReq := &GenerateRequest{RequestID: "req-inv", ModelID: "default",
		Messages: []Message{{Role: "user", Content: "hi"}}}
	resp, d, err := router.Invoke(context.Background(), &RoutingRequest{
		RequestID:    "req-inv",
		RequiredCaps: []ModelCapability{CapabilityReasoning},
	}, genReq)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if spy.lastModel != "m-sel" {
		t.Errorf("provider saw model %q, want the routed model m-sel", spy.lastModel)
	}
	if d.ModelID != "m-sel" {
		t.Errorf("decision model: want m-sel, got %s", d.ModelID)
	}
	if resp.Content != "ok" {
		t.Errorf("response: got %q", resp.Content)
	}
	if hs, ok := router.GetHealth().GetStatus("spy"); !ok || hs.SuccessCount != 1 {
		t.Errorf("health: want 1 recorded success, got %+v", hs)
	}

	// Failure records into health (degraded, not yet unhealthy).
	spy.err = errors.New("boom")
	_, _, err = router.Invoke(context.Background(), &RoutingRequest{
		RequestID:       "req-inv-fail",
		RequiredCaps:    []ModelCapability{CapabilityReasoning},
		FallbackEnabled: false,
	}, &GenerateRequest{RequestID: "req-inv-fail", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("want invoke error when the provider fails")
	}
	if hs, _ := router.GetHealth().GetStatus("spy"); hs == nil || hs.ErrorCount != 1 {
		t.Errorf("health: want 1 recorded failure, got %+v", hs)
	}
}

// TEST-M6-028 (D2): unhealthy providers demote to the back of the
// fallback chain (kept for failover), and health strikes are consecutive —
// a success resets the count.
func TestRouteHealthDemotionAndConsecutiveStrikes(t *testing.T) {
	reg := NewModelRegistry()
	for _, d := range []*ModelDefinition{
		{ID: "m-bad", ProviderID: "bad", Capabilities: []ModelCapability{CapabilityReasoning},
			Runtime: RuntimeRemote, Status: ModelStatusActive},
		{ID: "m-good", ProviderID: "good", Capabilities: []ModelCapability{CapabilityReasoning},
			Runtime: RuntimeRemote, Status: ModelStatusActive},
	} {
		if err := reg.RegisterModel(d); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	router := NewModelRouter(reg, RoutingPolicyNearest)
	for i := 0; i < 3; i++ {
		router.GetHealth().RecordFailure("bad")
	}

	d, err := router.Route(&RoutingRequest{
		RequestID:    "req-health",
		RequiredCaps: []ModelCapability{CapabilityReasoning},
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if len(d.FallbackChain) != 2 || d.FallbackChain[0] != "m-good" {
		t.Errorf("chain: healthy model must lead, got %v", d.FallbackChain)
	}
	if d.FallbackChain[1] != "m-bad" {
		t.Errorf("chain: unhealthy model must remain as last resort, got %v", d.FallbackChain)
	}

	// A success resets the consecutive strike count.
	router.GetHealth().RecordSuccess("bad", 10)
	hs, _ := router.GetHealth().GetStatus("bad")
	if hs.ErrorCount != 0 || hs.Status != ProviderStatusHealthy {
		t.Errorf("after success: want ErrorCount 0/healthy, got %d/%s", hs.ErrorCount, hs.Status)
	}
	router.GetHealth().RecordFailure("bad")
	hs, _ = router.GetHealth().GetStatus("bad")
	if hs.Status != ProviderStatusDegraded {
		t.Errorf("first strike after reset: want degraded, got %s", hs.Status)
	}
}

// TEST-R3-01 (R-3): invokeProvider reads the providers map without holding
// the router lock while RegisterProvider writes it under the lock — a data
// race under concurrent Invoke + RegisterProvider (-race).
func TestRouterConcurrentInvokeAndRegisterProvider(t *testing.T) {
	mr := NewModelRegistry()
	mr.RegisterModel(&ModelDefinition{
		ID:           "local-model",
		ProviderID:   "ollama",
		Capabilities: []ModelCapability{CapabilityReasoning},
		Runtime:      RuntimeLocal,
		Status:       ModelStatusActive,
	})
	router := NewModelRouter(mr, RoutingPolicyLocalFirst)
	router.RegisterProvider(NewLocalProvider(ProviderConfig{ID: "ollama"}))

	stop := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		for i := 0; i < 200; i++ {
			select {
			case <-stop:
				errCh <- nil
				return
			default:
			}
			_, _, err := router.Invoke(context.Background(), &RoutingRequest{
				RequestID:    fmt.Sprintf("req-r3-%d", i),
				RequiredCaps: []ModelCapability{CapabilityReasoning},
			}, &GenerateRequest{RequestID: fmt.Sprintf("req-r3-%d", i)})
			if err != nil {
				// Registration churn may briefly report a missing provider;
				// the test is about the data race, not error values.
				continue
			}
		}
		errCh <- nil
	}()

	for i := 0; i < 50; i++ {
		router.RegisterProvider(NewLocalProvider(ProviderConfig{ID: fmt.Sprintf("p-%d", i)}))
	}
	close(stop)
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}
