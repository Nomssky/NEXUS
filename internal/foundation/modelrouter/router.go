package modelrouter

import (
	"context"
	"errors"
	"fmt"
	"math"
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
	// EstimatedInputTokens is the caller's best estimate of input tokens for
	// this invocation. Used for cost-aware selection when provided.
	EstimatedInputTokens int `json:"estimated_input_tokens,omitempty"`
	// EstimatedOutputTokens is the caller's best estimate of output tokens for
	// this invocation. Used for cost-aware selection when provided.
	EstimatedOutputTokens int `json:"estimated_output_tokens,omitempty"`
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
	registry      *ModelRegistry
	providers     map[string]Provider
	health        *HealthRegistry
	accounting    *InvocationAccounting
	budgetTracker *BudgetTracker
	budgetPolicy  BudgetPolicy
	policy        RoutingPolicy
	mu            sync.RWMutex
	now           func() time.Time
}

// NewModelRouter creates a new model router.
func NewModelRouter(registry *ModelRegistry, policy RoutingPolicy) *ModelRouter {
	return &ModelRouter{
		registry:     registry,
		providers:    make(map[string]Provider),
		health:       NewHealthRegistry(),
		accounting:   NewInvocationAccounting(),
		policy:       policy,
		budgetPolicy: BudgetPolicyOptional,
		now:          time.Now,
	}
}

// NewModelRouterWithClock creates a new model router with an injectable clock.
func NewModelRouterWithClock(registry *ModelRegistry, policy RoutingPolicy, now func() time.Time) *ModelRouter {
	return &ModelRouter{
		registry:     registry,
		providers:    make(map[string]Provider),
		health:       NewHealthRegistry(),
		accounting:   NewInvocationAccounting(),
		policy:       policy,
		budgetPolicy: BudgetPolicyOptional,
		now:          now,
	}
}

// RegisterProvider adds a provider to the router.
func (mr *ModelRouter) RegisterProvider(provider Provider) {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	mr.providers[provider.Identify()] = provider
}

// WithBudgetTracker sets the budget tracker for cost enforcement. When set,
// the router checks affordability before each invocation and deducts the
// actual cost after a successful call. A nil tracker means no budget is
// enforced (all invocations allowed) under BudgetPolicyOptional; under
// BudgetPolicyMandatory a nil tracker fails closed.
func (mr *ModelRouter) WithBudgetTracker(bt *BudgetTracker) {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	mr.budgetTracker = bt
}

// WithBudgetPolicy sets the budget enforcement policy. BudgetPolicyOptional
// (default) allows invocations when no tracker is configured.
// BudgetPolicyMandatory fails closed when no tracker is configured.
func (mr *ModelRouter) WithBudgetPolicy(p BudgetPolicy) {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	mr.budgetPolicy = p
}

// Registry exposes the underlying model registry so callers can build
// capability-filtered candidate sets (never a parallel router).
func (mr *ModelRouter) Registry() *ModelRegistry { return mr.registry }

// ModelDiscoverer is implemented by providers that can enumerate locally
// installed models (the kind=ollama local runtime). It is optional: remote
// providers need not implement it.
type ModelDiscoverer interface {
	Discover(ctx context.Context) ([]InstalledModel, error)
}

// InstalledModels queries every registered discovery-capable provider and
// returns the set of locally installed model IDs.
//
// The boolean reports whether ANY source responded: false means installation
// state is UNKNOWN (e.g. remote-only configuration) and callers must treat it
// as uncertainty — never as "nothing is installed".
func (mr *ModelRouter) InstalledModels(ctx context.Context) (map[string]bool, bool, error) {
	mr.mu.RLock()
	providers := make([]Provider, 0, len(mr.providers))
	for _, p := range mr.providers {
		providers = append(providers, p)
	}
	mr.mu.RUnlock()

	installed := map[string]bool{}
	known := false
	for _, p := range providers {
		d, ok := p.(ModelDiscoverer)
		if !ok {
			continue
		}
		models, err := d.Discover(ctx)
		if err != nil {
			return nil, false, fmt.Errorf("installed-model discovery via %s: %w", p.Identify(), err)
		}
		known = true
		for _, m := range models {
			installed[m.Name] = true
		}
	}
	return installed, known, nil
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

	// A structured-output request must only reach a model that explicitly
	// advertises CapabilityStructuredOutput — never silently downgraded to
	// free text. The primary candidate was already capability-filtered, but
	// the check is repeated here so a caller that sets Structured without the
	// matching RequiredCaps still fails honestly instead of prose-parsing.
	if genReq.Structured != nil && !mr.modelAdvertises(decision.ModelID, CapabilityStructuredOutput) {
		reselected := ""
		for _, modelID := range decision.FallbackChain {
			if mr.modelAdvertises(modelID, CapabilityStructuredOutput) {
				reselected = modelID
				break
			}
		}
		if reselected == "" {
			return nil, decision, fmt.Errorf("no routed model advertises %q for structured output %q",
				CapabilityStructuredOutput, genReq.Structured.Name)
		}
		mdl, ok := mr.registry.GetModel(reselected)
		if !ok {
			return nil, decision, fmt.Errorf("no routed model advertises %q for structured output %q",
				CapabilityStructuredOutput, genReq.Structured.Name)
		}
		decision.ModelID = mdl.ID
		decision.ProviderID = mdl.ProviderID
		decision.Reason = fmt.Sprintf("reselected %s (provider=%s) for structured output %q",
			mdl.ID, mdl.ProviderID, genReq.Structured.Name)
	}

	// A request that advertises tool definitions must only reach a model that
	// explicitly advertises CapabilityToolCalling — never silently sent to a
	// model that cannot honor them. As with structured output, the check is
	// repeated here so a caller that sets Tools without the matching
	// RequiredCaps fails honestly instead of shipping tools into the void.
	if len(genReq.Tools) > 0 && !mr.modelAdvertises(decision.ModelID, CapabilityToolCalling) {
		reselected := ""
		for _, modelID := range decision.FallbackChain {
			if !mr.modelAdvertises(modelID, CapabilityToolCalling) {
				continue
			}
			// Never trade away a structured-output capability the request
			// depends on just to reach a tool-calling model.
			if genReq.Structured != nil && !mr.modelAdvertises(modelID, CapabilityStructuredOutput) {
				continue
			}
			reselected = modelID
			break
		}
		if reselected == "" {
			return nil, decision, fmt.Errorf("no routed model advertises %q for a tool-calling request", CapabilityToolCalling)
		}
		mdl, ok := mr.registry.GetModel(reselected)
		if !ok {
			return nil, decision, fmt.Errorf("no routed model advertises %q for a tool-calling request", CapabilityToolCalling)
		}
		decision.ModelID = mdl.ID
		decision.ProviderID = mdl.ProviderID
		decision.Reason = fmt.Sprintf("reselected %s (provider=%s) for tool calling", mdl.ID, mdl.ProviderID)
	}

	// D2: the primary attempt must invoke the model routing actually chose
	// — callers pass an empty/placeholder ModelID and the router owns
	// selection (routing ≠ authorization; selection here, governance at
	// the gate).
	genReq.ModelID = decision.ModelID

	// Look up the model definition for budget estimation.
	mdl, ok := mr.registry.GetModel(decision.ModelID)
	if !ok {
		return nil, decision, fmt.Errorf("model %q not found in registry", decision.ModelID)
	}

	// Try the selected provider first with budget enforcement.
	resp, invokeErr := mr.invokeWithBudget(ctx, mdl, decision.ProviderID, genReq)
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
			// A structured request never falls back to a model that cannot
			// honor it — skipping is honest, downgrading would not be.
			if genReq.Structured != nil && !hasModelCapability(mdl, CapabilityStructuredOutput) {
				continue
			}
			// Likewise a tool-calling request never falls back to a model that
			// does not advertise tool calling.
			if len(genReq.Tools) > 0 && !hasModelCapability(mdl, CapabilityToolCalling) {
				continue
			}
			genReq.ModelID = modelID
			resp, err := mr.invokeWithBudget(ctx, mdl, mdl.ProviderID, genReq)
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

// invokeWithBudget reserves budget before invoking a provider, then settles
// the actual cost after a successful call or handles the reservation on
// failure. Returns a budgetExceeded error if the model cannot be afforded.
// The reservation is atomic: concurrent calls cannot both reserve the same
// funds.
// Reservation lifecycle:
//   - Reserve estimated cost atomically (unique reservation ID returned)
//   - On provider success: ConsumeReserved against actual cost. If actual
//     cost exceeds the reservation, the excess is tracked as unresolved
//     exposure (Disputed state) and the reservation is not fully settled.
//   - On provider error with definite-no-charge semantics
//     (invalid_request, policy_rejection, model_missing): ReleaseReserved.
//   - On provider error with ambiguous semantics (unavailable, timeout,
//     cancelled, malformed_output): MarkDisputed — funds remain held as
//     unresolved exposure because a charge may have occurred.
//   - Unknown reservation IDs and duplicate transitions are rejected
//     (exactly-once enforcement).
func (mr *ModelRouter) invokeWithBudget(
	ctx context.Context,
	mdl *ModelDefinition,
	providerID string,
	genReq *GenerateRequest,
) (*GenerateResponse, error) {
	// Mandatory budget policy: fail closed if no tracker is configured.
	if mr.budgetTracker == nil {
		if mr.budgetPolicy == BudgetPolicyMandatory {
			return nil, &ProviderError{
				Kind:     "budget_missing_tracker",
				Provider: providerID,
				Cause:    fmt.Errorf("model %q requires budget enforcement but no tracker is configured", mdl.ID),
			}
		}
		// Optional policy: nil tracker means all invocations allowed.
		return mr.invokeProvider(ctx, providerID, genReq)
	}

	// Unresolved exposure from a prior ambiguous error blocks new paid calls.
	// Free/local calls (cost 0) are still allowed.
	if mr.budgetTracker.UnresolvedExposure() > 0 {
		estimate := mdl.EstimateCost(
			genReq.EstimatedInputTokens,
			genReq.EstimatedOutputTokens,
			mr.budgetTracker.Remaining(),
		)
		if estimate.EstimatedCost > 0 {
			return nil, &ProviderError{
				Kind:     "budget_unresolved_exposure",
				Provider: providerID,
				Cause:    fmt.Errorf("model %q cannot be invoked: unresolved budget exposure of %v blocks new paid calls", mdl.ID, mr.budgetTracker.UnresolvedExposure()),
			}
		}
	}

	// Estimate cost before invocation.
	estimate := mdl.EstimateCost(
		genReq.EstimatedInputTokens,
		genReq.EstimatedOutputTokens,
		mr.budgetTracker.Remaining(),
	)

	// Unknown pricing: cannot verify affordability, fail closed.
	if estimate.EstimatedCost < 0 {
		return nil, &ProviderError{
			Kind:     "budget_unknown_pricing",
			Provider: providerID,
			Cause:    fmt.Errorf("model %q pricing unknown: cannot verify affordability", mdl.ID),
		}
	}

	// Invalid estimate (NaN, Inf, negative): never affordable.
	if !isValidEstimate(estimate.EstimatedCost) {
		return nil, &ProviderError{
			Kind:     "budget_invalid_estimate",
			Provider: providerID,
			Cause:    fmt.Errorf("model %q has invalid estimated cost %v", mdl.ID, estimate.EstimatedCost),
		}
	}

	// Free or local calls (zero estimated cost) never consume budget.
	if estimate.EstimatedCost == 0 {
		return mr.invokeProvider(ctx, providerID, genReq)
	}

	// Reserve the estimated cost atomically. This prevents concurrent calls
	// from both reserving the same funds.
	resID, _, ok := mr.budgetTracker.Reserve(estimate.EstimatedCost)
	if !ok {
		return nil, &ProviderError{
			Kind:     "budget_exceeded",
			Provider: providerID,
			Cause:    fmt.Errorf("model %q estimated cost %v exceeds remaining budget %v", mdl.ID, estimate.EstimatedCost, mr.budgetTracker.Remaining()),
		}
	}

	// Invoke the provider while holding the reservation.
	resp, err := mr.invokeProvider(ctx, providerID, genReq)
	if err != nil {
		// Classify the error to decide whether to release or dispute the
		// reservation. A provider error is not automatically proof that no
		// charge occurred.
		if isDefiniteNoChargeError(err) {
			// Definite: the provider definitely did not process the request.
			// Safe to release the reservation.
			mr.budgetTracker.ReleaseReserved(resID)
		} else {
			// Ambiguous: a charge may or may not have occurred. Hold the
			// funds as unresolved exposure to prevent silent overspend.
			mr.budgetTracker.MarkDisputed(resID)
		}
		return nil, err
	}

	// Provider call succeeded: settle the reservation against actual cost.
	actualCost := mdl.TotalCost(resp.InputTokens, resp.OutputTokens)
	_, ok = mr.budgetTracker.ConsumeReserved(resID, actualCost)
	if !ok {
		// Reservation was already consumed or released — should not happen
		// since we hold the reservation from above. Fail closed.
		return nil, &ProviderError{
			Kind:     "budget_exceeded",
			Provider: providerID,
			Cause:    fmt.Errorf("budget reservation already settled for model %q", mdl.ID),
		}
	}

	return resp, nil
}

// isDefiniteNoChargeError classifies provider errors where we can be
// reasonably certain no charge occurred (the request was rejected before
// processing). Ambiguous errors (unavailable, timeout, cancelled,
// malformed_output) are treated as potentially-charged and the reservation
// is held as disputed exposure.
func isDefiniteNoChargeError(err error) bool {
	if err == nil {
		return true
	}
	var pe *ProviderError
	if errors.As(err, &pe) {
		switch pe.Kind {
		case "invalid_request", "policy_rejection", "model_missing":
			return true
		}
	}
	return false
}

// isValidEstimate reports whether an estimated cost is a valid, finite,
// non-negative monetary value.
func isValidEstimate(cost float64) bool {
	return !math.IsNaN(cost) && !math.IsInf(cost, 0) && cost >= 0
}

func (mr *ModelRouter) invokeProvider(ctx context.Context, providerID string, req *GenerateRequest) (*GenerateResponse, error) {
	// R-3: the providers map is written by RegisterProvider under the
	// router lock; read it under RLock too. Only the lookup is guarded —
	// provider.Invoke does I/O and must not hold the lock.
	mr.mu.RLock()
	provider, ok := mr.providers[providerID]
	mr.mu.RUnlock()
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
// price ascending (stable). nearest/random keep the deterministic candidate
// order from FindByCapabilities (ascending model ID). Tie-break contract:
// ascending lexicographic model ID resolves equal-rank ties. This is
// intentional for failover semantics — selection is deterministic, not
// load-balanced, and `random` is not genuine randomness (it preserves the
// deterministic enumeration order).
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

// modelAdvertises reports whether the named model explicitly carries the
// capability. Unknown models never count as capable — fail closed.
func (mr *ModelRouter) modelAdvertises(modelID string, cap ModelCapability) bool {
	mdl, ok := mr.registry.GetModel(modelID)
	if !ok {
		return false
	}
	return hasModelCapability(mdl, cap)
}

func hasModelCapability(mdl *ModelDefinition, cap ModelCapability) bool {
	if mdl == nil {
		return false
	}
	for _, c := range mdl.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}
