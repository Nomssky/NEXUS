package modelrouter

import (
	"fmt"
	"math"
	"sync"
)

// SourceCategory identifies where a model runs and who bears the cost.
type SourceCategory string

const (
	// SourceCategoryFree is a free model, including eligible free tiers exposed
	// by LLM gateways and providers. No per-invocation cost is expected.
	SourceCategoryFree SourceCategory = "free"
	// SourceCategoryPaid is a paid model with explicit pricing and budget
	// enforcement required before invocation.
	SourceCategoryPaid SourceCategory = "paid"
	// SourceCategoryLocal is a model running on the user's own infrastructure
	// (e.g. Ollama). No external data transfer occurs.
	SourceCategoryLocal SourceCategory = "local"
)

// FreeTierQuota describes free-tier limits for a free model. All fields are
// optional; zero values mean "unlimited" or "unknown".
type FreeTierQuota struct {
	// RequestsPerPeriod is the maximum number of requests allowed per period.
	RequestsPerPeriod int64 `json:"requests_per_period,omitempty"`
	// PeriodSeconds is the length of the quota period in seconds.
	PeriodSeconds int64 `json:"period_seconds,omitempty"`
	// RemainingRequests is the best-effort count of remaining requests in the
	// current period. Zero means unknown.
	RemainingRequests int64 `json:"remaining_requests,omitempty"`
	// ResetAt is when the quota period resets (RFC3339).
	ResetAt string `json:"reset_at,omitempty"`
}

// ModelSourceInfo describes the provenance and privacy classification of a
// model's source. It is populated at registration time from provider
// configuration and discovery; never inferred from the model name.
type ModelSourceInfo struct {
	// Category classifies the model as free, paid, or local.
	Category SourceCategory `json:"category"`
	// EvidenceSource identifies where the metadata came from (e.g.
	// "ollama_api", "provider_api", "configured"). Empty means unknown.
	EvidenceSource string `json:"evidence_source,omitempty"`
	// EvidenceFreshness is when the metadata was last verified (RFC3339).
	// Empty means unknown.
	EvidenceFreshness string `json:"evidence_freshness,omitempty"`
}

// TotalCost estimates the cost of an invocation from input and output token
// counts using the model's per-token pricing. Returns 0 for free and local
// models (no pricing), and -1 when pricing is unknown or invalid.
//
// Pricing is invalid (and treated as unknown) when either price is negative,
// NaN, or infinite. Missing pricing (both prices zero) is also unknown.
func (m *ModelDefinition) TotalCost(inputTokens, outputTokens int) float64 {
	switch m.Source {
	case SourceCategoryFree, SourceCategoryLocal:
		return 0
	case SourceCategoryPaid:
		if m.PricingInput <= 0 && m.PricingOutput <= 0 {
			return -1 // unknown pricing
		}
		if !isValidPrice(m.PricingInput) || !isValidPrice(m.PricingOutput) {
			return -1 // invalid pricing: cannot verify affordability
		}
		return float64(inputTokens)*m.PricingInput + float64(outputTokens)*m.PricingOutput
	default:
		return -1
	}
}

// isValidPrice reports whether a per-token price is a valid, finite,
// non-negative monetary value. NaN, positive/negative infinity, and negative
// prices are all invalid.
func isValidPrice(price float64) bool {
	return !math.IsNaN(price) && !math.IsInf(price, 0) && price >= 0
}

// IsFree reports whether the model is free (no per-invocation cost).
func (m *ModelDefinition) IsFree() bool {
	return m.Source == SourceCategoryFree
}

// IsPaid reports whether the model is paid (requires budget enforcement).
func (m *ModelDefinition) IsPaid() bool {
	return m.Source == SourceCategoryPaid
}

// IsLocal reports whether the model runs on local infrastructure.
func (m *ModelDefinition) IsLocal() bool {
	return m.Source == SourceCategoryLocal
}

// CanAfford checks whether the model can be invoked within the given remaining
// budget. For free and local models, always returns true. For paid models with
// unknown pricing, returns false (cannot verify affordability). For paid models
// with known pricing, compares the estimated total cost against remaining.
func (m *ModelDefinition) CanAfford(inputTokens, outputTokens int, remainingBudget float64) bool {
	cost := m.TotalCost(inputTokens, outputTokens)
	if cost < 0 {
		return false // unknown pricing: cannot verify affordability
	}
	return cost <= remainingBudget
}

// String returns a human-readable description of the source category.
func (s SourceCategory) String() string {
	switch s {
	case SourceCategoryFree:
		return "free"
	case SourceCategoryPaid:
		return "paid"
	case SourceCategoryLocal:
		return "local"
	default:
		return "unknown"
	}
}

// Validate checks that the source category is one of the known values.
func (s SourceCategory) Validate() error {
	switch s {
	case SourceCategoryFree, SourceCategoryPaid, SourceCategoryLocal:
		return nil
	case "":
		return fmt.Errorf("source category is required")
	default:
		return fmt.Errorf("unknown source category %q", s)
	}
}

// CostEstimate is the result of a cost estimation for a proposed invocation.
type CostEstimate struct {
	// ModelID is the model being estimated.
	ModelID string `json:"model_id"`
	// ProviderID is the provider serving the model.
	ProviderID string `json:"provider_id"`
	// Source is the model's source category.
	Source SourceCategory `json:"source"`
	// InputTokens is the estimated input token count.
	InputTokens int `json:"input_tokens"`
	// OutputTokens is the estimated output token count.
	OutputTokens int `json:"output_tokens"`
	// EstimatedCost is the estimated cost in the configured currency unit.
	// Zero for free and local models. Negative (-1) when pricing is unknown.
	EstimatedCost float64 `json:"estimated_cost"`
	// Affordable reports whether the model can be invoked within the remaining
	// budget. False when pricing is unknown.
	Affordable bool `json:"affordable"`
	// Reason explains the estimate (e.g. "free model", "paid model within
	// budget", "pricing unknown").
	Reason string `json:"reason"`
}

// EstimateCost produces a CostEstimate for invoking the model with the given
// token counts and remaining budget.
func (m *ModelDefinition) EstimateCost(inputTokens, outputTokens int, remainingBudget float64) CostEstimate {
	cost := m.TotalCost(inputTokens, outputTokens)
	affordable := m.CanAfford(inputTokens, outputTokens, remainingBudget)

	var reason string
	switch {
	case m.IsFree():
		reason = "free model: no cost"
	case m.IsLocal():
		reason = "local model: no external transfer cost"
	case m.IsPaid():
		if cost < 0 {
			reason = "paid model: pricing unknown, cannot verify affordability"
		} else if affordable {
			reason = "paid model: within budget"
		} else {
			reason = "paid model: exceeds remaining budget"
		}
	default:
		reason = "unknown source category: cannot verify affordability"
	}

	return CostEstimate{
		ModelID:       m.ID,
		ProviderID:    m.ProviderID,
		Source:        m.Source,
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		EstimatedCost: cost,
		Affordable:    affordable,
		Reason:        reason,
	}
}

// BudgetPolicy determines how the router behaves when no budget tracker is
// configured.
type BudgetPolicy int

const (
	// BudgetPolicyOptional means a nil tracker disables enforcement
	// (backward-compatible: all invocations allowed).
	BudgetPolicyOptional BudgetPolicy = iota
	// BudgetPolicyMandatory means a nil tracker fails closed: no paid
	// invocation may proceed without a valid tracker.
	BudgetPolicyMandatory
)

// ReservationState tracks the terminal lifecycle of a reservation.
// Exactly-once: a reservation transitions from Reserved to exactly one of
// Settled, Released, or Disputed. No further transitions are allowed.
type ReservationState int32

const (
	// ReservationStateReserved funds are held but not yet settled.
	ReservationStateReserved ReservationState = iota + 1
	// ReservationStateSettled funds have been charged (actual cost deducted).
	ReservationStateSettled
	// ReservationStateReleased funds have been returned (no charge).
	ReservationStateReleased
	// ReservationStateDisputed funds are held as unresolved exposure due to
	// an ambiguous provider error where a charge may or may not have occurred.
	ReservationStateDisputed
)

// Reservation represents a single budget hold with a unique identity.
type Reservation struct {
	ID     string
	Amount float64
	State  ReservationState
}

// BudgetTracker tracks remaining budget and outstanding reservations across
// invocations within a task. Each reservation has a unique identity and
// exactly-once terminal transitions.
// A zero budget means no funds are available: only free models (cost 0) can
// proceed; paid models always fail. A negative budget behaves the same way:
// no paid calls allowed. There is no "unlimited" mode here — callers that
// want no budget enforcement should pass nil instead of NewBudgetTracker.
type BudgetTracker struct {
	remaining    float64                 // funds not yet reserved or spent
	reserved     float64                 // funds in Reserved state (held but not settled)
	disputed     float64                 // funds in Disputed state (unresolved exposure)
	reservations map[string]*Reservation // reservation ID → reservation
	mu           sync.RWMutex
	idCounter    int64
}

// NewBudgetTracker creates a budget tracker with the given remaining budget.
// A zero budget means no funds are available: only free models (cost 0) can
// proceed; paid models always fail. A negative budget behaves the same way:
// no paid calls allowed. There is no "unlimited" mode here — callers that
// want no budget enforcement should pass nil instead of NewBudgetTracker.
func NewBudgetTracker(budget float64) *BudgetTracker {
	return &BudgetTracker{
		remaining:    budget,
		reservations: make(map[string]*Reservation),
	}
}

// Remaining returns the remaining budget (unreserved and unspent).
func (bt *BudgetTracker) Remaining() float64 {
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	return bt.remaining
}

// Reserved returns the total amount currently reserved (held but not yet
// settled via ConsumeReserved or released via ReleaseReserved).
func (bt *BudgetTracker) Reserved() float64 {
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	return bt.reserved
}

// UnresolvedExposure returns the total amount held in Disputed state due to
// ambiguous provider errors where a charge may or may not have occurred.
// These funds reduce available budget and prevent new paid calls until
// resolved.
func (bt *BudgetTracker) UnresolvedExposure() float64 {
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	return bt.disputed
}

// isValidCost reports whether a monetary value is valid for budget operations.
// Negative, NaN, and infinite values are rejected.
func isValidCost(cost float64) bool {
	return !math.IsNaN(cost) && !math.IsInf(cost, 0) && cost >= 0
}

// CanAfford checks whether a model invocation with the given estimated cost
// would fit within the remaining budget. Disputed (unresolved exposure) funds
// reduce available budget. A zero or negative remaining budget means no funds
// are available: only free models (cost 0) can proceed.
// Invalid costs (negative, NaN, Inf) are never affordable.
func (bt *BudgetTracker) CanAfford(estimatedCost float64) bool {
	if !isValidCost(estimatedCost) {
		return false
	}
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	available := bt.remaining
	if estimatedCost <= 0 {
		return true // free calls never consume budget
	}
	if available <= 0 {
		return false // no budget for paid calls
	}
	return estimatedCost <= available
}

// Consume deducts the given cost from the remaining budget. Returns the new
// remaining balance. Returns false if the cost would exceed the budget (no
// deduction is made). A zero remaining budget permits only free calls
// (cost <= 0); paid calls are rejected.
// Invalid costs (negative, NaN, Inf) are rejected.
// Consume does not reserve anything — it is a settle-only operation. Use
// Reserve/ConsumeReserved for a reserve-then-settle lifecycle.
func (bt *BudgetTracker) Consume(cost float64) (float64, bool) {
	if !isValidCost(cost) {
		return 0, false
	}
	bt.mu.Lock()
	defer bt.mu.Unlock()
	if bt.remaining <= 0 {
		if cost > 0 {
			return bt.remaining, false // no budget for paid calls
		}
		return bt.remaining, true // free call, no deduction needed
	}
	if cost > bt.remaining {
		return bt.remaining, false // would exceed budget
	}
	bt.remaining -= cost
	return bt.remaining, true
}

// nextID generates a monotonically increasing reservation ID.
func (bt *BudgetTracker) nextID() string {
	bt.idCounter++
	return fmt.Sprintf("res-%d", bt.idCounter)
}

// Reserve holds `cost` from the remaining budget for a pending invocation.
// Returns a unique reservation ID, the remaining balance after reservation,
// and true on success. Returns an empty ID and false if the cost would exceed
// the remaining budget; no funds are held on failure.
// Invalid costs (negative, NaN, Inf) are rejected.
// The reserved amount must later be settled via ConsumeReserved (on success)
// or released via ReleaseReserved (on failure or abort).
func (bt *BudgetTracker) Reserve(cost float64) (reservationID string, remaining float64, ok bool) {
	if !isValidCost(cost) {
		return "", 0, false
	}
	bt.mu.Lock()
	defer bt.mu.Unlock()
	if cost == 0 {
		// Free reservation: nothing held, return a terminal "free" ID.
		return bt.nextID(), bt.remaining, true
	}
	if bt.remaining <= 0 {
		return "", bt.remaining, false // no budget for paid calls
	}
	if cost > bt.remaining {
		return "", bt.remaining, false // would exceed budget
	}
	id := bt.nextID()
	bt.remaining -= cost
	bt.reserved += cost
	bt.reservations[id] = &Reservation{ID: id, Amount: cost, State: ReservationStateReserved}
	return id, bt.remaining, true
}

// ConsumeReserved settles a previously reserved reservation against actual
// cost. Takes the reservation ID returned by Reserve. Returns the actual cost
// charged and true on success.
// The reserved amount was already deducted from bt.remaining at Reserve
// time. This method returns any unused portion (reserved - actualCost) to the
// budget and charges actualCost (capped at reserved) against the budget.
// If actualCost exceeds the reservation, the excess is tracked as unresolved
// exposure: the reservation transitions to Disputed and the excess remains
// held against the budget. The caller is responsible for reconciling the
// discrepancy with the provider's actual bill.
// Returns ok=false if the reservation ID is unknown or already terminal
// (Settled, Released, or Disputed); no change is made to the budget.
// Invalid actualCost (negative, NaN, Inf) is treated as 0.
func (bt *BudgetTracker) ConsumeReserved(reservationID string, actualCost float64) (charged float64, ok bool) {
	if !isValidCost(actualCost) {
		actualCost = 0
	}
	bt.mu.Lock()
	defer bt.mu.Unlock()
	res, exists := bt.reservations[reservationID]
	if !exists {
		return 0, false // unknown reservation ID
	}
	switch res.State {
	case ReservationStateSettled, ReservationStateReleased, ReservationStateDisputed:
		return 0, false // already terminal: exactly-once enforcement
	}
	// Undo the reservation hold: return reserved funds to remaining.
	bt.reserved -= res.Amount
	bt.remaining += res.Amount

	charge := actualCost
	excess := float64(0)
	if charge > res.Amount {
		// Actual cost exceeds reservation: charge only the reserved amount
		// and track the excess as unresolved exposure.
		charge = res.Amount
		excess = actualCost - res.Amount
	}
	bt.remaining -= charge
	res.State = ReservationStateSettled
	if excess > 0 {
		// Hold the excess as disputed exposure. The reservation remains
		// Disputed so the excess continues to count against the budget.
		bt.disputed += excess
		res.State = ReservationStateDisputed
	}
	return charge, true
}

// ReleaseReserved releases a previously reserved reservation back to the
// budget without charging anything. Takes the reservation ID returned by
// Reserve. Returns true if the reservation existed and was released; false if
// the reservation ID is unknown or already terminal (exactly-once enforcement).
// Invalid or unknown reservation IDs are rejected without changing accounting.
func (bt *BudgetTracker) ReleaseReserved(reservationID string) bool {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	res, exists := bt.reservations[reservationID]
	if !exists {
		return false // unknown reservation ID
	}
	switch res.State {
	case ReservationStateSettled, ReservationStateReleased, ReservationStateDisputed:
		return false // already terminal: exactly-once enforcement
	case ReservationStateReserved:
		bt.reserved -= res.Amount
		bt.remaining += res.Amount
		res.State = ReservationStateReleased
		return true
	}
	return false
}

// MarkDisputed transitions a Reserved reservation to Disputed state, holding
// its funds as unresolved exposure. Used by the router when a provider returns
// an ambiguous error where a charge may or may not have occurred. Returns true
// if the transition succeeded; false if the reservation ID is unknown or
// already terminal.
func (bt *BudgetTracker) MarkDisputed(reservationID string) bool {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	res, exists := bt.reservations[reservationID]
	if !exists {
		return false // unknown reservation ID
	}
	if res.State != ReservationStateReserved {
		return false // already terminal: exactly-once enforcement
	}
	// Funds move from Reserved to Disputed: decrement reserved, increment disputed.
	bt.reserved -= res.Amount
	bt.disputed += res.Amount
	res.State = ReservationStateDisputed
	return true
}

// ReservationCount returns the number of reservations currently in Reserved
// state (useful for testing and diagnostics).
func (bt *BudgetTracker) ReservationCount() int {
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	count := 0
	for _, r := range bt.reservations {
		if r.State == ReservationStateReserved {
			count++
		}
	}
	return count
}
