package modelrouter

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RoutingPolicy determines how the router selects providers.
type RoutingPolicy string

const (
	RoutingPolicyLocalFirst RoutingPolicy = "local_first"
	RoutingPolicyNearest    RoutingPolicy = "nearest"
	RoutingPolicyCheapest   RoutingPolicy = "cheapest"
	RoutingPolicyRandom     RoutingPolicy = "random"
)

// RoutingRequest is a request to the model router.
type RoutingRequest struct {
	RequestID        string            `json:"request_id"`
	AgentID          string            `json:"agent_id"`
	BusinessID       string            `json:"business_id"`
	RequiredCaps     []ModelCapability `json:"required_caps"`
	PreferLocal      bool              `json:"prefer_local"`
	AllowedProviders []string          `json:"allowed_providers,omitempty"`
	AllowedModels    []string          `json:"allowed_models,omitempty"`
	MaxLatencyMs     int64             `json:"max_latency_ms,omitempty"`
	MaxCostPerToken  float64           `json:"max_cost_per_token,omitempty"`
	FallbackEnabled  bool              `json:"fallback_enabled"`
}

// RoutingDecision is the result of routing.
type RoutingDecision struct {
	RequestID     string        `json:"request_id"`
	ModelID       string        `json:"model_id"`
	ProviderID    string        `json:"provider_id"`
	RoutingPolicy RoutingPolicy `json:"routing_policy"`
	FallbackChain []string      `json:"fallback_chain,omitempty"`
	Reason        string        `json:"reason"`
}

// InvocationRecord tracks a model invocation for accounting.
type InvocationRecord struct {
	RequestID    string    `json:"request_id"`
	ModelID      string    `json:"model_id"`
	ProviderID   string    `json:"provider_id"`
	AgentID      string    `json:"agent_id"`
	BusinessID   string    `json:"business_id"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	TotalTokens  int       `json:"total_tokens"`
	Cost         float64   `json:"cost"`
	LatencyMs    int64     `json:"latency_ms"`
	Success      bool      `json:"success"`
	Error        string    `json:"error,omitempty"`
	InvokedAt    time.Time `json:"invoked_at"`
}

// ModelRouter selects the best provider/model based on needs, policy, and conditions.
// It implements local-first routing, capability matching, and failover.
type ModelRouter struct {
	registry   *ModelRegistry
	providers  map[string]Provider
	health     *HealthRegistry
	accounting *InvocationAccounting
	policy     RoutingPolicy
	mu         sync.RWMutex
	now        func() time.Time
}

// NewModelRouter creates a new model router.
func NewModelRouter(registry *ModelRegistry, policy RoutingPolicy) *ModelRouter {
	return &ModelRouter{
		registry:   registry,
		providers:  make(map[string]Provider),
		health:     NewHealthRegistry(),
		accounting: NewInvocationAccounting(),
		policy:     policy,
		now:        time.Now,
	}
}

// NewModelRouterWithClock creates a new model router with an injectable clock.
func NewModelRouterWithClock(registry *ModelRegistry, policy RoutingPolicy, now func() time.Time) *ModelRouter {
	return &ModelRouter{
		registry:   registry,
		providers:  make(map[string]Provider),
		health:     NewHealthRegistry(),
		accounting: NewInvocationAccounting(),
		policy:     policy,
		now:        now,
	}
}

// RegisterProvider adds a provider to the router.
func (mr *ModelRouter) RegisterProvider(provider Provider) {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	mr.providers[provider.Identify()] = provider
}

// Route selects the best model/provider for a request.
// This implements routing ≠ authorization: routing selects, governance authorizes.
// NOTE: Provider selection is deterministic (not load-balanced) — this is
// intentional for failover semantics. The primary use case is selecting the
// best provider and falling back to alternatives on failure, not distributing
// load across providers.
func (mr *ModelRouter) Route(req *RoutingRequest) (*RoutingDecision, error) {
	mr.mu.RLock()
	defer mr.mu.RUnlock()

	// Find models matching capabilities
	candidates := mr.registry.FindByCapabilities(req.RequiredCaps)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no models found matching capabilities %v", req.RequiredCaps)
	}

	// Filter by allowed providers/models
	candidates = mr.filterCandidates(candidates, req)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no models match provider/model constraints")
	}

	// Sort by policy
	sorted := mr.sortByPolicy(candidates, req)

	// Build fallback chain
	var fallbackChain []string
	for _, m := range sorted {
		fallbackChain = append(fallbackChain, m.ID)
	}

	best := sorted[0]
	decision := &RoutingDecision{
		RequestID:     req.RequestID,
		ModelID:       best.ID,
		ProviderID:    best.ProviderID,
		RoutingPolicy: mr.policy,
		FallbackChain: fallbackChain,
		Reason:        fmt.Sprintf("selected %s (provider=%s, runtime=%s)", best.ID, best.ProviderID, best.Runtime),
	}

	return decision, nil
}

// Invoke routes and invokes a model. It implements failover without unsafe duplicates.
// ctx propagates the caller's cancellation/deadline (E-005/OQ8): an already-cancelled
// context fails fast, and cancellation during invocation reaches the provider call.
func (mr *ModelRouter) Invoke(ctx context.Context, req *RoutingRequest, genReq *GenerateRequest) (*GenerateResponse, *RoutingDecision, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	decision, err := mr.Route(req)
	if err != nil {
		return nil, nil, err
	}

	// Try the selected provider first
	resp, invokeErr := mr.invokeProvider(ctx, decision.ProviderID, genReq)
	if invokeErr == nil {
		// Record successful invocation
		mr.accounting.Record(&InvocationRecord{
			RequestID:    genReq.RequestID,
			ModelID:      decision.ModelID,
			ProviderID:   decision.ProviderID,
			AgentID:      req.AgentID,
			BusinessID:   req.BusinessID,
			InputTokens:  resp.InputTokens,
			OutputTokens: resp.OutputTokens,
			TotalTokens:  resp.TotalTokens,
			LatencyMs:    resp.LatencyMs,
			Success:      true,
			InvokedAt:    mr.now(),
		})
		return resp, decision, nil
	}

	// Failover: try fallback chain
	if req.FallbackEnabled {
		for _, modelID := range decision.FallbackChain[1:] {
			mdl, ok := mr.registry.GetModel(modelID)
			if !ok {
				continue
			}
			genReq.ModelID = modelID
			resp, err := mr.invokeProvider(ctx, mdl.ProviderID, genReq)
			if err == nil {
				mr.accounting.Record(&InvocationRecord{
					RequestID:    genReq.RequestID,
					ModelID:      modelID,
					ProviderID:   mdl.ProviderID,
					AgentID:      req.AgentID,
					BusinessID:   req.BusinessID,
					InputTokens:  resp.InputTokens,
					OutputTokens: resp.OutputTokens,
					TotalTokens:  resp.TotalTokens,
					LatencyMs:    resp.LatencyMs,
					Success:      true,
					InvokedAt:    mr.now(),
				})
				decision.ModelID = modelID
				decision.ProviderID = mdl.ProviderID
				decision.Reason = fmt.Sprintf("failover to %s (provider=%s)", modelID, mdl.ProviderID)
				return resp, decision, nil
			}
		}
	}

	// All providers failed
	mr.accounting.Record(&InvocationRecord{
		RequestID:  genReq.RequestID,
		ModelID:    decision.ModelID,
		ProviderID: decision.ProviderID,
		AgentID:    req.AgentID,
		BusinessID: req.BusinessID,
		Success:    false,
		Error:      invokeErr.Error(),
		InvokedAt:  mr.now(),
	})

	return nil, decision, fmt.Errorf("all providers failed: %w", invokeErr)
}

func (mr *ModelRouter) invokeProvider(ctx context.Context, providerID string, req *GenerateRequest) (*GenerateResponse, error) {
	provider, ok := mr.providers[providerID]
	if !ok {
		return nil, fmt.Errorf("provider %s not found", providerID)
	}

	if err := provider.HealthCheck(); err != nil {
		return nil, fmt.Errorf("provider %s unhealthy: %w", providerID, err)
	}

	return provider.Invoke(ctx, req)
}

func (mr *ModelRouter) filterCandidates(candidates []*ModelDefinition, req *RoutingRequest) []*ModelDefinition {
	var filtered []*ModelDefinition
	for _, m := range candidates {
		// Check allowed providers
		if len(req.AllowedProviders) > 0 {
			allowed := false
			for _, p := range req.AllowedProviders {
				if p == m.ProviderID {
					allowed = true
					break
				}
			}
			if !allowed {
				continue
			}
		}
		// Check allowed models
		if len(req.AllowedModels) > 0 {
			allowed := false
			for _, id := range req.AllowedModels {
				if id == m.ID {
					allowed = true
					break
				}
			}
			if !allowed {
				continue
			}
		}
		filtered = append(filtered, m)
	}
	return filtered
}

func (mr *ModelRouter) sortByPolicy(candidates []*ModelDefinition, req *RoutingRequest) []*ModelDefinition {
	// For now, simple sort: local first if prefer_local, then by runtime
	if req.PreferLocal || mr.policy == RoutingPolicyLocalFirst {
		var local, remote []*ModelDefinition
		for _, m := range candidates {
			if m.Runtime == RuntimeLocal {
				local = append(local, m)
			} else {
				remote = append(remote, m)
			}
		}
		return append(local, remote...)
	}
	return candidates
}

// GetAccounting returns the invocation accounting.
func (mr *ModelRouter) GetAccounting() *InvocationAccounting {
	return mr.accounting
}

// GetHealth returns the health registry.
func (mr *ModelRouter) GetHealth() *HealthRegistry {
	return mr.health
}
