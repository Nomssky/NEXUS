package modelrouter

import (
	"context"
	"fmt"
	"sort"
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

// RoutingStrategy mirrors SCHEMA_EXECUTION §3.3 RoutingDecision.strategy —
// why this model was selected.
type RoutingStrategy string

const (
	StrategyCapabilityMatch RoutingStrategy = "capability_match"
	StrategyCostOptimize    RoutingStrategy = "cost_optimize"
	StrategyLatencyOptimize RoutingStrategy = "latency_optimize"
	StrategyPrivacyFirst    RoutingStrategy = "privacy_first"
	StrategyLocalFirst      RoutingStrategy = "local_first"
	StrategyManualOverride  RoutingStrategy = "manual_override"
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
	// PrivacyFirst excludes models that send data to external providers
	// (m6: privacy-aware routing — respect external transfer policies).
	PrivacyFirst    bool `json:"privacy_first,omitempty"`
	FallbackEnabled bool `json:"fallback_enabled"`
}

// RoutingDecision is the result of routing. Fields mirror SCHEMA_EXECUTION
// §3.3 RoutingDecision: strategy, candidates, selected_reason (field name
// Reason, contract JSON name selected_reason), constraints_applied
// (routing_policy/fallback_chain are additive).
type RoutingDecision struct {
	RequestID          string          `json:"request_id"`
	ModelID            string          `json:"model_id"`
	ProviderID         string          `json:"provider_id"`
	Strategy           RoutingStrategy `json:"strategy"`
	Candidates         []string        `json:"candidates"`
	Reason             string          `json:"selected_reason"`
	ConstraintsApplied []string        `json:"constraints_applied,omitempty"`
	RoutingPolicy      RoutingPolicy   `json:"routing_policy"`
	FallbackChain      []string        `json:"fallback_chain,omitempty"`
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

	// Capability match: the always-on base of selection (SCHEMA §3.3
	// strategy capability_match when nothing narrower applies).
	capMatched := mr.registry.FindByCapabilities(req.RequiredCaps)
	if len(capMatched) == 0 {
		return nil, fmt.Errorf("no models found matching capabilities %v", req.RequiredCaps)
	}

	// Candidates = every capability-matched model considered (§3.3).
	candidates := make([]string, 0, len(capMatched))
	for _, m := range capMatched {
		candidates = append(candidates, m.ID)
	}

	// Constraint filtering (cost/latency/privacy/allow-lists) with an
	// audit trail of what was applied.
	constraints := []string{fmt.Sprintf("capabilities=%v", req.RequiredCaps)}
	filtered := mr.filterCandidates(capMatched, req, &constraints)
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no models match provider/model constraints")
	}

	// Health demotion (not removal): unhealthy providers sort last within
	// their preference class and remain available to failover
	// (PROVIDER_CONTRACTS §7.3 fallback = yes).
	sorted := mr.sortByPolicy(mr.demoteUnhealthy(filtered), req)

	// Build fallback chain
	var fallbackChain []string
	for _, m := range sorted {
		fallbackChain = append(fallbackChain, m.ID)
	}

	best := sorted[0]
	strategy := mr.deriveStrategy(req)
	decision := &RoutingDecision{
		RequestID:          req.RequestID,
		ModelID:            best.ID,
		ProviderID:         best.ProviderID,
		Strategy:           strategy,
		Candidates:         candidates,
		Reason:             fmt.Sprintf("selected %s (provider=%s, runtime=%s, strategy=%s)", best.ID, best.ProviderID, best.Runtime, strategy),
		ConstraintsApplied: constraints,
		RoutingPolicy:      mr.policy,
		FallbackChain:      fallbackChain,
	}

	return decision, nil
}

// deriveStrategy maps the routing request and router policy onto the
// SCHEMA_EXECUTION §3.3 strategy enum. Most specific signal wins: a manual
// allow-list is an explicit override, then privacy, request cost/latency
// bounds, the router's own cost/locality policy, and finally plain
// capability matching.
func (mr *ModelRouter) deriveStrategy(req *RoutingRequest) RoutingStrategy {
	switch {
	case len(req.AllowedModels) > 0:
		return StrategyManualOverride
	case req.PrivacyFirst:
		return StrategyPrivacyFirst
	case req.MaxCostPerToken > 0:
		return StrategyCostOptimize
	case req.MaxLatencyMs > 0:
		return StrategyLatencyOptimize
	case mr.policy == RoutingPolicyCheapest:
		return StrategyCostOptimize
	case req.PreferLocal || mr.policy == RoutingPolicyLocalFirst:
		return StrategyLocalFirst
	default:
		return StrategyCapabilityMatch
	}
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

	// D2: the primary attempt must invoke the model routing actually chose
	// — callers pass an empty/placeholder ModelID and the router owns
	// selection (routing ≠ authorization; selection here, governance at
	// the gate).
	genReq.ModelID = decision.ModelID

	// Try the selected provider first
	resp, invokeErr := mr.invokeProvider(ctx, decision.ProviderID, genReq)
	if invokeErr == nil {
		mr.health.RecordSuccess(decision.ProviderID, resp.LatencyMs)
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

	// The primary provider failed — feed the health registry so later
	// routes demote it (it stays in the chain as a last-resort fallback).
	mr.health.RecordFailure(decision.ProviderID)

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
				mr.health.RecordSuccess(mdl.ProviderID, resp.LatencyMs)
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
			mr.health.RecordFailure(mdl.ProviderID)
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

// filterCandidates applies allow-lists and the routing constraints
// (cost, latency, privacy), appending each applied constraint to *applied
// for the decision's constraints_applied audit trail (§3.3).
func (mr *ModelRouter) filterCandidates(candidates []*ModelDefinition, req *RoutingRequest, applied *[]string) []*ModelDefinition {
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
		// Cost bound: PricingInput is per token (registry field contract).
		if req.MaxCostPerToken > 0 && m.PricingInput > req.MaxCostPerToken {
			continue
		}
		// Latency bound: use the provider's last measured latency when the
		// health registry knows it; unknown latency is not filtered.
		if req.MaxLatencyMs > 0 {
			if hs, ok := mr.health.GetStatus(m.ProviderID); ok && hs.LatencyMs > req.MaxLatencyMs {
				continue
			}
		}
		// Privacy: m6 privacy-aware routing — no external transfer of data.
		if req.PrivacyFirst && m.ExternalTransfer {
			continue
		}
		filtered = append(filtered, m)
	}
	if len(req.AllowedProviders) > 0 {
		*applied = append(*applied, "allowed_providers")
	}
	if len(req.AllowedModels) > 0 {
		*applied = append(*applied, "allowed_models")
	}
	if req.MaxCostPerToken > 0 {
		*applied = append(*applied, fmt.Sprintf("max_cost_per_token=%g", req.MaxCostPerToken))
	}
	if req.MaxLatencyMs > 0 {
		*applied = append(*applied, fmt.Sprintf("max_latency_ms=%d", req.MaxLatencyMs))
	}
	if req.PrivacyFirst {
		*applied = append(*applied, "privacy_first")
	}
	return filtered
}

// demoteUnhealthy stably pushes models of unhealthy providers behind the
// healthy ones. They are not removed: failover may still need them
// (PROVIDER_CONTRACTS §7.3). Unknown providers count as healthy.
func (mr *ModelRouter) demoteUnhealthy(models []*ModelDefinition) []*ModelDefinition {
	var healthy, other []*ModelDefinition
	for _, m := range models {
		if mr.health.IsHealthy(m.ProviderID) {
			healthy = append(healthy, m)
		} else {
			other = append(other, m)
		}
	}
	return append(healthy, other...)
}

// sortByPolicy orders candidates by the router policy. local_first splits
// local before remote (stable within each class); cheapest sorts by input
// price ascending (stable). nearest/random have no distance/load signal in
// this milestone and keep registry order — deterministic routing is an
// accepted risk (audit §6), not load balancing.
func (mr *ModelRouter) sortByPolicy(candidates []*ModelDefinition, req *RoutingRequest) []*ModelDefinition {
	if mr.policy == RoutingPolicyCheapest {
		sorted := append([]*ModelDefinition(nil), candidates...)
		sort.SliceStable(sorted, func(i, j int) bool {
			return sorted[i].PricingInput < sorted[j].PricingInput
		})
		return sorted
	}
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
