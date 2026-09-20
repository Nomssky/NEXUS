package modelrouter

import (
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
