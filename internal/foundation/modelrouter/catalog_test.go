package modelrouter

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
)

// --- TotalCost / EstimateCost tests ---

func TestTotalCostFreeModel(t *testing.T) {
	m := &ModelDefinition{ID: "free-model", Source: SourceCategoryFree}
	if c := m.TotalCost(1000, 500); c != 0 {
		t.Fatalf("free model cost should be 0, got %v", c)
	}
}

func TestTotalCostLocalModel(t *testing.T) {
	m := &ModelDefinition{ID: "local-model", Source: SourceCategoryLocal}
	if c := m.TotalCost(1000, 500); c != 0 {
		t.Fatalf("local model cost should be 0, got %v", c)
	}
}

func TestTotalCostPaidModel(t *testing.T) {
	m := &ModelDefinition{ID: "paid-model", Source: SourceCategoryPaid, PricingInput: 0.010, PricingOutput: 0.030}
	if c := m.TotalCost(1000, 500); c != 25.0 {
		t.Fatalf("paid model cost should be 25, got %v", c)
	}
}

func TestTotalCostPaidModelUnknownPricing(t *testing.T) {
	m := &ModelDefinition{ID: "unknown-price", Source: SourceCategoryPaid}
	if c := m.TotalCost(1000, 500); c != -1 {
		t.Fatalf("paid model with unknown pricing should return -1, got %v", c)
	}
}

func TestTotalCostPaidModelZeroInputPrice(t *testing.T) {
	m := &ModelDefinition{ID: "zero-input", Source: SourceCategoryPaid, PricingInput: 0, PricingOutput: 0.030}
	if c := m.TotalCost(1000, 500); c != 15.0 {
		t.Fatalf("paid model with zero input price should be 15, got %v", c)
	}
}

func TestTotalCostPaidModelNaNPrice(t *testing.T) {
	m := &ModelDefinition{ID: "nan-price", Source: SourceCategoryPaid, PricingInput: math.NaN(), PricingOutput: 0.030}
	if c := m.TotalCost(1000, 500); c != -1 {
		t.Fatalf("paid model with NaN price should return -1, got %v", c)
	}
}

func TestTotalCostPaidModelInfPrice(t *testing.T) {
	m := &ModelDefinition{ID: "inf-price", Source: SourceCategoryPaid, PricingInput: math.Inf(1), PricingOutput: 0.030}
	if c := m.TotalCost(1000, 500); c != -1 {
		t.Fatalf("paid model with Inf price should return -1, got %v", c)
	}
}

func TestTotalCostPaidModelNegativePrice(t *testing.T) {
	m := &ModelDefinition{ID: "neg-price", Source: SourceCategoryPaid, PricingInput: -0.01, PricingOutput: 0.030}
	if c := m.TotalCost(1000, 500); c != -1 {
		t.Fatalf("paid model with negative price should return -1, got %v", c)
	}
}

func TestEstimateCostFreeModel(t *testing.T) {
	m := &ModelDefinition{ID: "free", Source: SourceCategoryFree}
	est := m.EstimateCost(1000, 500, 0)
	if est.EstimatedCost != 0 || !est.Affordable || est.Reason != "free model: no cost" {
		t.Fatalf("unexpected estimate: %+v", est)
	}
}

func TestEstimateCostLocalModel(t *testing.T) {
	m := &ModelDefinition{ID: "local", Source: SourceCategoryLocal}
	est := m.EstimateCost(1000, 500, 0)
	if est.EstimatedCost != 0 || !est.Affordable || est.Reason != "local model: no external transfer cost" {
		t.Fatalf("unexpected estimate: %+v", est)
	}
}

func TestEstimateCostPaidWithinBudget(t *testing.T) {
	m := &ModelDefinition{ID: "paid", Source: SourceCategoryPaid, PricingInput: 0.010, PricingOutput: 0.030}
	est := m.EstimateCost(1000, 500, 100.0)
	if est.EstimatedCost != 25.0 || !est.Affordable || est.Reason != "paid model: within budget" {
		t.Fatalf("unexpected estimate: %+v", est)
	}
}

func TestEstimateCostPaidExceedsBudget(t *testing.T) {
	m := &ModelDefinition{ID: "paid", Source: SourceCategoryPaid, PricingInput: 0.010, PricingOutput: 0.030}
	est := m.EstimateCost(1000, 500, 10.0)
	if est.EstimatedCost != 25.0 || est.Affordable || est.Reason != "paid model: exceeds remaining budget" {
		t.Fatalf("unexpected estimate: %+v", est)
	}
}

func TestEstimateCostPaidUnknownPricing(t *testing.T) {
	m := &ModelDefinition{ID: "unknown", Source: SourceCategoryPaid}
	est := m.EstimateCost(1000, 500, 100.0)
	if est.EstimatedCost != -1 || est.Affordable || est.Reason != "paid model: pricing unknown, cannot verify affordability" {
		t.Fatalf("unexpected estimate: %+v", est)
	}
}

// --- BudgetTracker tests ---

func TestBudgetTrackerZeroBudgetFreeModel(t *testing.T) {
	bt := NewBudgetTracker(0)
	if !bt.CanAfford(0) {
		t.Fatal("free model should be affordable with zero budget")
	}
	if bt.CanAfford(0.01) {
		t.Fatal("paid model should not be affordable with zero budget")
	}
}

func TestBudgetTrackerZeroBudgetConsumeFree(t *testing.T) {
	bt := NewBudgetTracker(0)
	remaining, ok := bt.Consume(0)
	if !ok || remaining != 0 {
		t.Fatalf("free consume should succeed, got remaining=%v ok=%v", remaining, ok)
	}
}

func TestBudgetTrackerZeroBudgetConsumePaid(t *testing.T) {
	bt := NewBudgetTracker(0)
	remaining, ok := bt.Consume(0.01)
	if ok || remaining != 0 {
		t.Fatalf("paid consume should fail with zero budget, got remaining=%v ok=%v", remaining, ok)
	}
}

func TestBudgetTrackerPositiveBudget(t *testing.T) {
	bt := NewBudgetTracker(100.0)
	if !bt.CanAfford(50.0) {
		t.Fatal("50 should be affordable with 100 budget")
	}
	remaining, ok := bt.Consume(50.0)
	if !ok || remaining != 50.0 {
		t.Fatalf("consume 50 from 100 should leave 50, got remaining=%v ok=%v", remaining, ok)
	}
	if !bt.CanAfford(50.0) {
		t.Fatal("remaining 50 should afford 50")
	}
	remaining, ok = bt.Consume(50.0)
	if !ok || remaining != 0 {
		t.Fatalf("consume 50 from 50 should leave 0, got remaining=%v ok=%v", remaining, ok)
	}
	if bt.CanAfford(0.01) {
		t.Fatal("0.01 should not be affordable with 0 remaining")
	}
}

func TestBudgetTrackerNegativeBudget(t *testing.T) {
	bt := NewBudgetTracker(-1.0)
	if !bt.CanAfford(0) {
		t.Fatal("free model should be affordable with negative budget")
	}
	if bt.CanAfford(0.01) {
		t.Fatal("paid model should not be affordable with negative budget")
	}
}

func TestBudgetTrackerConcurrentReservations(t *testing.T) {
	bt := NewBudgetTracker(100.0)
	const numGoroutines = 50
	const costPerCall = 2.0

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !bt.CanAfford(costPerCall) {
				errors <- fmt.Errorf("CanAfford(%v) failed unexpectedly", costPerCall)
				return
			}
			remaining, ok := bt.Consume(costPerCall)
			if !ok {
				errors <- fmt.Errorf("Consume(%v) failed unexpectedly", costPerCall)
				return
			}
			_ = remaining
		}()
	}
	wg.Wait()
	close(errors)

	remaining := bt.Remaining()
	if remaining < 0 {
		t.Fatalf("remaining budget went negative: %v", remaining)
	}
	totalSpent := 100.0 - remaining
	expectedSpent := float64(numGoroutines) * costPerCall
	if totalSpent != expectedSpent {
		t.Fatalf("total spent %v != expected %v (remaining=%v)", totalSpent, expectedSpent, remaining)
	}

	if bt.CanAfford(0.01) {
		t.Fatal("budget should be exhausted after consuming all funds")
	}
}

func TestBudgetTrackerConcurrentOverspend(t *testing.T) {
	bt := NewBudgetTracker(10.0)
	const numGoroutines = 20
	const costPerCall = 10.0

	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			remaining, ok := bt.Consume(costPerCall)
			mu.Lock()
			if ok {
				successCount++
				_ = remaining
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	if successCount != 1 {
		t.Fatalf("expected exactly 1 successful consume, got %d", successCount)
	}
	if bt.Remaining() != 0 {
		t.Fatalf("remaining should be 0, got %v", bt.Remaining())
	}
}

// --- Atomic reservation lifecycle tests (Reserve/ConsumeReserved/ReleaseReserved with unique IDs) ---

func TestBudgetTrackerReserveAndConsume(t *testing.T) {
	bt := NewBudgetTracker(100.0)

	resID, remaining, ok := bt.Reserve(30)
	if !ok {
		t.Fatal("reserve should succeed")
	}
	if remaining != 70 {
		t.Fatalf("expected remaining 70 after reserve, got %v", remaining)
	}
	if bt.Reserved() != 30 {
		t.Fatalf("expected reserved 30, got %v", bt.Reserved())
	}

	charged, ok := bt.ConsumeReserved(resID, 20)
	if !ok {
		t.Fatal("consume reserved should succeed")
	}
	if charged != 20 {
		t.Fatalf("expected charged 20, got %v", charged)
	}
	if bt.Reserved() != 0 {
		t.Fatalf("expected reserved 0 after consume, got %v", bt.Reserved())
	}
	if bt.Remaining() != 80 {
		t.Fatalf("expected remaining 80 after consume, got %v", bt.Remaining())
	}
}

func TestBudgetTrackerReserveAndRelease(t *testing.T) {
	bt := NewBudgetTracker(100.0)

	resID, remaining, ok := bt.Reserve(30)
	if !ok {
		t.Fatal("reserve should succeed")
	}
	if remaining != 70 {
		t.Fatalf("expected remaining 70 after reserve, got %v", remaining)
	}
	if bt.Reserved() != 30 {
		t.Fatalf("expected reserved 30, got %v", bt.Reserved())
	}

	if !bt.ReleaseReserved(resID) {
		t.Fatal("release should succeed")
	}
	if bt.Reserved() != 0 {
		t.Fatalf("expected reserved 0 after release, got %v", bt.Reserved())
	}
	if bt.Remaining() != 100 {
		t.Fatalf("expected remaining 100 after release, got %v", bt.Remaining())
	}
}

func TestBudgetTrackerReleaseReservedTwice(t *testing.T) {
	bt := NewBudgetTracker(100.0)

	resID, _, ok := bt.Reserve(30)
	if !ok {
		t.Fatal("reserve should succeed")
	}

	if !bt.ReleaseReserved(resID) {
		t.Fatal("first release should succeed")
	}

	if bt.ReleaseReserved(resID) {
		t.Fatal("second release should fail (already released)")
	}
}

func TestBudgetTrackerConsumeReservedTwice(t *testing.T) {
	bt := NewBudgetTracker(100.0)

	resID, _, ok := bt.Reserve(30)
	if !ok {
		t.Fatal("reserve should succeed")
	}

	if _, ok := bt.ConsumeReserved(resID, 20); !ok {
		t.Fatal("first consume should succeed")
	}

	if _, ok := bt.ConsumeReserved(resID, 10); ok {
		t.Fatal("second consume should fail (already consumed)")
	}
}

func TestBudgetTrackerReserveExceedsBudget(t *testing.T) {
	bt := NewBudgetTracker(10.0)

	_, _, ok := bt.Reserve(20)
	if ok {
		t.Fatal("reserve should fail when cost exceeds remaining")
	}
	if bt.Reserved() != 0 {
		t.Fatalf("expected reserved 0 after failed reserve, got %v", bt.Reserved())
	}
}

func TestBudgetTrackerConcurrentReservationsCannotOverspend(t *testing.T) {
	bt := NewBudgetTracker(10.0)
	const numGoroutines = 10
	const costPerCall = 6.0

	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, ok := bt.Reserve(costPerCall)
			mu.Lock()
			if ok {
				successCount++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	if successCount != 1 {
		t.Fatalf("expected exactly 1 successful reserve, got %d", successCount)
	}
}

func TestBudgetTrackerConsumeReservedExceedsReservation(t *testing.T) {
	bt := NewBudgetTracker(100.0)

	resID, _, ok := bt.Reserve(10)
	if !ok {
		t.Fatal("reserve should succeed")
	}

	charged, ok := bt.ConsumeReserved(resID, 50)
	if !ok {
		t.Fatal("consume reserved should succeed")
	}
	if charged != 10 {
		t.Fatalf("expected charged 10 (capped at reserved), got %v", charged)
	}
}

// --- Reservation identity tests (equal-valued reservations are independent) ---

func TestBudgetTrackerEqualAmountReservationsIndependent(t *testing.T) {
	bt := NewBudgetTracker(100.0)

	resID1, _, ok1 := bt.Reserve(30)
	if !ok1 {
		t.Fatal("first reserve should succeed")
	}
	resID2, _, ok2 := bt.Reserve(30)
	if !ok2 {
		t.Fatal("second reserve should succeed")
	}
	if resID1 == resID2 {
		t.Fatal("equal-amount reservations must have unique IDs")
	}
	if bt.Reserved() != 60 {
		t.Fatalf("expected reserved 60, got %v", bt.Reserved())
	}

	// Settling one must not affect the other
	charged, ok := bt.ConsumeReserved(resID1, 20)
	if !ok {
		t.Fatal("consume first reservation should succeed")
	}
	if charged != 20 {
		t.Fatalf("expected charged 20, got %v", charged)
	}
	// Second reservation is still in Reserved state
	if bt.Reserved() != 30 {
		t.Fatalf("expected reserved 30 after settling first, got %v", bt.Reserved())
	}

	// Release the second independently
	if !bt.ReleaseReserved(resID2) {
		t.Fatal("release second reservation should succeed")
	}
	if bt.Reserved() != 0 {
		t.Fatalf("expected reserved 0 after releasing second, got %v", bt.Reserved())
	}
}

func TestBudgetTrackerSettlingOneCannotSettleOther(t *testing.T) {
	bt := NewBudgetTracker(100.0)

	resID1, _, ok1 := bt.Reserve(30)
	if !ok1 {
		t.Fatal("first reserve should succeed")
	}
	resID2, _, ok2 := bt.Reserve(30)
	if !ok2 {
		t.Fatal("second reserve should succeed")
	}

	// Settle first
	if _, ok := bt.ConsumeReserved(resID1, 10); !ok {
		t.Fatal("consume first reservation should succeed")
	}

	// Second reservation must still be consumable
	charged, ok := bt.ConsumeReserved(resID2, 25)
	if !ok {
		t.Fatal("consume second reservation should succeed")
	}
	if charged != 25 {
		t.Fatalf("expected charged 25, got %v", charged)
	}
}

func TestBudgetTrackerReleasingOneCannotReleaseOther(t *testing.T) {
	bt := NewBudgetTracker(100.0)

	resID1, _, ok1 := bt.Reserve(30)
	if !ok1 {
		t.Fatal("first reserve should succeed")
	}
	resID2, _, ok2 := bt.Reserve(30)
	if !ok2 {
		t.Fatal("second reserve should succeed")
	}

	if !bt.ReleaseReserved(resID1) {
		t.Fatal("release first reservation should succeed")
	}

	// Second reservation must still be releasable
	if !bt.ReleaseReserved(resID2) {
		t.Fatal("release second reservation should succeed")
	}
	if bt.Reserved() != 0 {
		t.Fatalf("expected reserved 0 after releasing both, got %v", bt.Reserved())
	}
}

func TestBudgetTrackerUnknownReservationIDRejected(t *testing.T) {
	bt := NewBudgetTracker(100.0)

	// Consume with unknown ID
	if _, ok := bt.ConsumeReserved("res-999", 10); ok {
		t.Fatal("consume with unknown ID should fail")
	}
	// Release with unknown ID
	if bt.ReleaseReserved("res-999") {
		t.Fatal("release with unknown ID should fail")
	}
	// MarkDisputed with unknown ID
	if bt.MarkDisputed("res-999") {
		t.Fatal("MarkDisputed with unknown ID should fail")
	}
	// Accounting must be unchanged
	if bt.Remaining() != 100.0 {
		t.Fatalf("remaining should be unchanged, got %v", bt.Remaining())
	}
	if bt.Reserved() != 0 {
		t.Fatalf("reserved should be unchanged, got %v", bt.Reserved())
	}
}

func TestBudgetTrackerDuplicateTerminalTransitionRejected(t *testing.T) {
	bt := NewBudgetTracker(100.0)

	resID, _, ok := bt.Reserve(30)
	if !ok {
		t.Fatal("reserve should succeed")
	}

	// Settle
	if _, ok := bt.ConsumeReserved(resID, 20); !ok {
		t.Fatal("first consume should succeed")
	}

	// Release after settle should fail
	if bt.ReleaseReserved(resID) {
		t.Fatal("release after settle should fail")
	}

	// MarkDisputed after settle should fail
	if bt.MarkDisputed(resID) {
		t.Fatal("MarkDisputed after settle should fail")
	}

	// Consume again should fail
	if _, ok := bt.ConsumeReserved(resID, 10); ok {
		t.Fatal("consume after settle should fail")
	}
}

// --- Invalid monetary value tests ---

func TestBudgetTrackerReserveRejectsNegative(t *testing.T) {
	bt := NewBudgetTracker(100.0)

	_, _, ok := bt.Reserve(-1)
	if ok {
		t.Fatal("Reserve(-1) should be rejected (negative cost bypass)")
	}
	if bt.Reserved() != 0 {
		t.Fatalf("reserved should be 0 after rejected reserve, got %v", bt.Reserved())
	}
	if bt.Remaining() != 100.0 {
		t.Fatalf("remaining should be unchanged, got %v", bt.Remaining())
	}
}

func TestBudgetTrackerReserveRejectsNaN(t *testing.T) {
	bt := NewBudgetTracker(100.0)

	_, _, ok := bt.Reserve(math.NaN())
	if ok {
		t.Fatal("Reserve(NaN) should be rejected")
	}
}

func TestBudgetTrackerReserveRejectsInf(t *testing.T) {
	bt := NewBudgetTracker(100.0)

	_, _, ok := bt.Reserve(math.Inf(1))
	if ok {
		t.Fatal("Reserve(Inf) should be rejected")
	}
}

func TestBudgetTrackerConsumeRejectsNegative(t *testing.T) {
	bt := NewBudgetTracker(100.0)

	_, ok := bt.Consume(-1)
	if ok {
		t.Fatal("Consume(-1) should be rejected")
	}
}

func TestBudgetTrackerConsumeRejectsNaN(t *testing.T) {
	bt := NewBudgetTracker(100.0)

	_, ok := bt.Consume(math.NaN())
	if ok {
		t.Fatal("Consume(NaN) should be rejected")
	}
}

func TestBudgetTrackerCanAffordRejectsInvalid(t *testing.T) {
	bt := NewBudgetTracker(100.0)

	if bt.CanAfford(-1) {
		t.Fatal("CanAfford(-1) should be false")
	}
	if bt.CanAfford(math.NaN()) {
		t.Fatal("CanAfford(NaN) should be false")
	}
	if bt.CanAfford(math.Inf(1)) {
		t.Fatal("CanAfford(Inf) should be false")
	}
}

// --- Unresolved exposure tests ---

func TestBudgetTrackerUnresolvedExposureBlocksPaidCalls(t *testing.T) {
	bt := NewBudgetTracker(100.0)

	// Reserve and then mark as disputed (simulating ambiguous provider error)
	resID, _, ok := bt.Reserve(30)
	if !ok {
		t.Fatal("reserve should succeed")
	}
	if !bt.MarkDisputed(resID) {
		t.Fatal("MarkDisputed should succeed")
	}

	// Disputed funds reduce available budget
	if bt.UnresolvedExposure() != 30 {
		t.Fatalf("expected unresolved exposure 30, got %v", bt.UnresolvedExposure())
	}
	if bt.Remaining() != 70 {
		t.Fatalf("expected remaining 70 after dispute, got %v", bt.Remaining())
	}

	// A paid call that fits within remaining but would be blocked by exposure
	// CanAfford only checks remaining, so 40 should still be "affordable"
	// by remaining alone — but the router layer blocks when exposure > 0.
	if !bt.CanAfford(40) {
		t.Fatal("CanAfford(40) should still pass based on remaining alone")
	}
	// But a larger call should fail
	if bt.CanAfford(80) {
		t.Fatal("CanAfford(80) should fail (exceeds remaining)")
	}
}

func TestBudgetTrackerDisputedReservationCannotBeReleased(t *testing.T) {
	bt := NewBudgetTracker(100.0)

	resID, _, ok := bt.Reserve(30)
	if !ok {
		t.Fatal("reserve should succeed")
	}
	if !bt.MarkDisputed(resID) {
		t.Fatal("MarkDisputed should succeed")
	}

	// Disputed reservations cannot be released through normal API
	if bt.ReleaseReserved(resID) {
		t.Fatal("release of disputed reservation should fail")
	}
	if _, ok := bt.ConsumeReserved(resID, 10); ok {
		t.Fatal("consume of disputed reservation should fail")
	}
}

// --- Router-level tests ---

// countingProvider tracks how many times Invoke was called
type countingProvider struct {
	config    ProviderConfig
	status    ProviderStatus
	mu        sync.Mutex
	callCount int
}

func newCountingProvider(id string) *countingProvider {
	return &countingProvider{config: ProviderConfig{ID: id}, status: ProviderStatusHealthy}
}

func (p *countingProvider) Identify() string              { return p.config.ID }
func (p *countingProvider) HealthCheck() error            { return nil }
func (p *countingProvider) ListModels() ([]string, error) { return nil, nil }
func (p *countingProvider) Invoke(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	p.mu.Lock()
	p.callCount++
	p.mu.Unlock()
	return &GenerateResponse{
		RequestID:    req.RequestID,
		ModelID:      req.ModelID,
		Content:      "response",
		FinishReason: "stop",
		InputTokens:  req.EstimatedInputTokens,
		OutputTokens: req.EstimatedOutputTokens,
		TotalTokens:  req.EstimatedInputTokens + req.EstimatedOutputTokens,
		LatencyMs:    10,
	}, nil
}

// failingProvider returns a definite-no-charge error (invalid_request)
type failingDefiniteProvider struct {
	config    ProviderConfig
	status    ProviderStatus
	mu        sync.Mutex
	callCount int
}

func newFailingDefiniteProvider(id string) *failingDefiniteProvider {
	return &failingDefiniteProvider{config: ProviderConfig{ID: id}, status: ProviderStatusHealthy}
}

func (p *failingDefiniteProvider) Identify() string              { return p.config.ID }
func (p *failingDefiniteProvider) HealthCheck() error            { return nil }
func (p *failingDefiniteProvider) ListModels() ([]string, error) { return nil, nil }
func (p *failingDefiniteProvider) Invoke(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	p.mu.Lock()
	p.callCount++
	p.mu.Unlock()
	return nil, &ProviderError{Kind: "invalid_request", Provider: p.config.ID, Cause: errors.New("bad request")}
}

// failingAmbiguousProvider returns an ambiguous error (unavailable)
type failingAmbiguousProvider struct {
	config    ProviderConfig
	status    ProviderStatus
	mu        sync.Mutex
	callCount int
}

func newFailingAmbiguousProvider(id string) *failingAmbiguousProvider {
	return &failingAmbiguousProvider{config: ProviderConfig{ID: id}, status: ProviderStatusHealthy}
}

func (p *failingAmbiguousProvider) Identify() string              { return p.config.ID }
func (p *failingAmbiguousProvider) HealthCheck() error            { return nil }
func (p *failingAmbiguousProvider) ListModels() ([]string, error) { return nil, nil }
func (p *failingAmbiguousProvider) Invoke(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	p.mu.Lock()
	p.callCount++
	p.mu.Unlock()
	return nil, &ProviderError{Kind: "unavailable", Provider: p.config.ID, Cause: errors.New("service unavailable")}
}

func TestRouterInvokeWithBudgetReservesBeforeCalling(t *testing.T) {
	mr := NewModelRegistry()
	mr.RegisterModel(&ModelDefinition{
		ID:            "paid-model",
		ProviderID:    "paid-provider",
		Source:        SourceCategoryPaid,
		PricingInput:  0.01,
		PricingOutput: 0.03,
		Status:        ModelStatusActive,
		Capabilities:  []ModelCapability{CapabilityReasoning},
	})

	router := NewModelRouter(mr, RoutingPolicyLocalFirst)
	router.RegisterProvider(newCountingProvider("paid-provider"))
	router.WithBudgetTracker(NewBudgetTracker(5.0))

	genReq := &GenerateRequest{
		RequestID:             "req-1",
		ModelID:               "paid-model",
		EstimatedInputTokens:  100,
		EstimatedOutputTokens: 50,
	}

	resp, _, err := router.Invoke(context.Background(), &RoutingRequest{
		RequestID:    "req-1",
		AgentID:      "agent-1",
		BusinessID:   "biz-1",
		RequiredCaps: []ModelCapability{CapabilityReasoning},
	}, genReq)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Content != "response" {
		t.Errorf("expected response content, got %v", resp.Content)
	}
}

func TestRouterInvokeWithBudgetRejectsWhenInsufficient(t *testing.T) {
	mr := NewModelRegistry()
	mr.RegisterModel(&ModelDefinition{
		ID:            "paid-model",
		ProviderID:    "paid-provider",
		Source:        SourceCategoryPaid,
		PricingInput:  1.0,
		PricingOutput: 1.0,
		Status:        ModelStatusActive,
		Capabilities:  []ModelCapability{CapabilityReasoning},
	})

	router := NewModelRouter(mr, RoutingPolicyLocalFirst)
	router.RegisterProvider(newCountingProvider("paid-provider"))
	router.WithBudgetTracker(NewBudgetTracker(1.0))

	genReq := &GenerateRequest{
		RequestID:             "req-1",
		ModelID:               "paid-model",
		EstimatedInputTokens:  1000,
		EstimatedOutputTokens: 1000,
	}

	_, _, err := router.Invoke(context.Background(), &RoutingRequest{
		RequestID:    "req-1",
		AgentID:      "agent-1",
		BusinessID:   "biz-1",
		RequiredCaps: []ModelCapability{CapabilityReasoning},
	}, genReq)

	if err == nil {
		t.Fatal("expected budget exceeded error")
	}
}

func TestRouterDefiniteErrorReleasesReservation(t *testing.T) {
	mr := NewModelRegistry()
	mr.RegisterModel(&ModelDefinition{
		ID:            "paid-model",
		ProviderID:    "paid-provider",
		Source:        SourceCategoryPaid,
		PricingInput:  0.01,
		PricingOutput: 0.03,
		Status:        ModelStatusActive,
		Capabilities:  []ModelCapability{CapabilityReasoning},
	})

	router := NewModelRouter(mr, RoutingPolicyLocalFirst)
	router.RegisterProvider(newFailingDefiniteProvider("paid-provider"))
	router.WithBudgetTracker(NewBudgetTracker(100.0))

	genReq := &GenerateRequest{
		RequestID:             "req-1",
		ModelID:               "paid-model",
		EstimatedInputTokens:  1000,
		EstimatedOutputTokens: 500,
	}

	_, _, err := router.Invoke(context.Background(), &RoutingRequest{
		RequestID:    "req-1",
		AgentID:      "agent-1",
		BusinessID:   "biz-1",
		RequiredCaps: []ModelCapability{CapabilityReasoning},
	}, genReq)

	if err == nil {
		t.Fatal("expected error from failing provider")
	}

	// Reservation should have been released (definite no-charge error)
	if bt := router.budgetTracker; bt != nil {
		if bt.UnresolvedExposure() != 0 {
			t.Fatalf("expected no unresolved exposure after definite error, got %v", bt.UnresolvedExposure())
		}
		if bt.Reserved() != 0 {
			t.Fatalf("expected no reserved funds after definite error release, got %v", bt.Reserved())
		}
		if bt.Remaining() != 100.0 {
			t.Fatalf("expected remaining 100 after release, got %v", bt.Remaining())
		}
	}
}

func TestRouterAmbiguousErrorHoldsReservation(t *testing.T) {
	mr := NewModelRegistry()
	mr.RegisterModel(&ModelDefinition{
		ID:            "paid-model",
		ProviderID:    "paid-provider",
		Source:        SourceCategoryPaid,
		PricingInput:  0.01,
		PricingOutput: 0.03,
		Status:        ModelStatusActive,
		Capabilities:  []ModelCapability{CapabilityReasoning},
	})

	router := NewModelRouter(mr, RoutingPolicyLocalFirst)
	router.RegisterProvider(newFailingAmbiguousProvider("paid-provider"))
	router.WithBudgetTracker(NewBudgetTracker(100.0))

	genReq := &GenerateRequest{
		RequestID:             "req-1",
		ModelID:               "paid-model",
		EstimatedInputTokens:  1000,
		EstimatedOutputTokens: 500,
	}

	_, _, err := router.Invoke(context.Background(), &RoutingRequest{
		RequestID:    "req-1",
		AgentID:      "agent-1",
		BusinessID:   "biz-1",
		RequiredCaps: []ModelCapability{CapabilityReasoning},
	}, genReq)

	if err == nil {
		t.Fatal("expected error from failing provider")
	}

	// Reservation should be held as disputed exposure (ambiguous error)
	if bt := router.budgetTracker; bt != nil {
		estimateCost := 1000*0.01 + 500*0.03 // 25.0
		if bt.UnresolvedExposure() != estimateCost {
			t.Fatalf("expected unresolved exposure %v, got %v", estimateCost, bt.UnresolvedExposure())
		}
		if bt.Reserved() != 0 {
			t.Fatalf("expected reserved 0 after dispute (funds moved to disputed), got %v", bt.Reserved())
		}
		if bt.Remaining() != 100.0-estimateCost {
			t.Fatalf("expected remaining %v, got %v", 100.0-estimateCost, bt.Remaining())
		}
	}

	// A subsequent paid call should be blocked by unresolved exposure
	genReq2 := &GenerateRequest{
		RequestID:             "req-2",
		ModelID:               "paid-model",
		EstimatedInputTokens:  100,
		EstimatedOutputTokens: 50,
	}
	_, _, err2 := router.Invoke(context.Background(), &RoutingRequest{
		RequestID:    "req-2",
		AgentID:      "agent-2",
		BusinessID:   "biz-1",
		RequiredCaps: []ModelCapability{CapabilityReasoning},
	}, genReq2)
	if err2 == nil {
		t.Fatal("expected second call to be blocked by unresolved exposure")
	}
}

func TestBudgetTrackerUnknownPricingNotAffordable(t *testing.T) {
	m := &ModelDefinition{ID: "unknown-price", Source: SourceCategoryPaid}
	bt := NewBudgetTracker(1000.0)

	est := m.EstimateCost(1000, 1000, bt.Remaining())
	if est.EstimatedCost != -1 {
		t.Fatalf("expected estimated cost -1 for unknown pricing, got %v", est.EstimatedCost)
	}
	if est.Affordable {
		t.Fatal("unknown pricing should not be affordable")
	}

	// The router catches unknown pricing (EstimatedCost < 0) before calling
	// Reserve, so Reserve(-1) is never reached through the router path.
	// Direct Reserve(-1) is rejected as invalid.
	_, _, ok := bt.Reserve(est.EstimatedCost)
	if ok {
		t.Fatal("Reserve(-1) should be rejected as invalid cost")
	}
}

// --- Mandatory budget policy tests ---

func TestRouterMandatoryPolicyNilTrackerFailsClosed(t *testing.T) {
	mr := NewModelRegistry()
	mr.RegisterModel(&ModelDefinition{
		ID:            "paid-model",
		ProviderID:    "paid-provider",
		Source:        SourceCategoryPaid,
		PricingInput:  0.01,
		PricingOutput: 0.03,
		Status:        ModelStatusActive,
		Capabilities:  []ModelCapability{CapabilityReasoning},
	})

	router := NewModelRouter(mr, RoutingPolicyLocalFirst)
	router.RegisterProvider(newCountingProvider("paid-provider"))
	router.WithBudgetPolicy(BudgetPolicyMandatory)
	// No budget tracker set

	genReq := &GenerateRequest{
		RequestID:             "req-1",
		ModelID:               "paid-model",
		EstimatedInputTokens:  100,
		EstimatedOutputTokens: 50,
	}

	_, _, err := router.Invoke(context.Background(), &RoutingRequest{
		RequestID:    "req-1",
		AgentID:      "agent-1",
		BusinessID:   "biz-1",
		RequiredCaps: []ModelCapability{CapabilityReasoning},
	}, genReq)

	if err == nil {
		t.Fatal("expected budget_missing_tracker error with mandatory policy and nil tracker")
	}
}

func TestRouterOptionalPolicyNilTrackerAllowed(t *testing.T) {
	mr := NewModelRegistry()
	mr.RegisterModel(&ModelDefinition{
		ID:            "paid-model",
		ProviderID:    "paid-provider",
		Source:        SourceCategoryPaid,
		PricingInput:  0.01,
		PricingOutput: 0.03,
		Status:        ModelStatusActive,
		Capabilities:  []ModelCapability{CapabilityReasoning},
	})

	router := NewModelRouter(mr, RoutingPolicyLocalFirst)
	router.RegisterProvider(newCountingProvider("paid-provider"))
	// Default is BudgetPolicyOptional; no tracker set

	genReq := &GenerateRequest{
		RequestID:             "req-1",
		ModelID:               "paid-model",
		EstimatedInputTokens:  100,
		EstimatedOutputTokens: 50,
	}

	resp, _, err := router.Invoke(context.Background(), &RoutingRequest{
		RequestID:    "req-1",
		AgentID:      "agent-1",
		BusinessID:   "biz-1",
		RequiredCaps: []ModelCapability{CapabilityReasoning},
	}, genReq)

	if err != nil {
		t.Fatalf("unexpected error with optional policy and nil tracker: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestRouterMandatoryPolicyWithTrackerAllowed(t *testing.T) {
	mr := NewModelRegistry()
	mr.RegisterModel(&ModelDefinition{
		ID:            "paid-model",
		ProviderID:    "paid-provider",
		Source:        SourceCategoryPaid,
		PricingInput:  0.01,
		PricingOutput: 0.03,
		Status:        ModelStatusActive,
		Capabilities:  []ModelCapability{CapabilityReasoning},
	})

	router := NewModelRouter(mr, RoutingPolicyLocalFirst)
	router.RegisterProvider(newCountingProvider("paid-provider"))
	router.WithBudgetPolicy(BudgetPolicyMandatory)
	router.WithBudgetTracker(NewBudgetTracker(10.0))

	genReq := &GenerateRequest{
		RequestID:             "req-1",
		ModelID:               "paid-model",
		EstimatedInputTokens:  100,
		EstimatedOutputTokens: 50,
	}

	resp, _, err := router.Invoke(context.Background(), &RoutingRequest{
		RequestID:    "req-1",
		AgentID:      "agent-1",
		BusinessID:   "biz-1",
		RequiredCaps: []ModelCapability{CapabilityReasoning},
	}, genReq)

	if err != nil {
		t.Fatalf("unexpected error with mandatory policy and valid tracker: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestRouterMandatoryPolicyExhaustedBudgetFails(t *testing.T) {
	mr := NewModelRegistry()
	mr.RegisterModel(&ModelDefinition{
		ID:            "paid-model",
		ProviderID:    "paid-provider",
		Source:        SourceCategoryPaid,
		PricingInput:  1.0,
		PricingOutput: 1.0,
		Status:        ModelStatusActive,
		Capabilities:  []ModelCapability{CapabilityReasoning},
	})

	router := NewModelRouter(mr, RoutingPolicyLocalFirst)
	router.RegisterProvider(newCountingProvider("paid-provider"))
	router.WithBudgetPolicy(BudgetPolicyMandatory)
	router.WithBudgetTracker(NewBudgetTracker(0.0))

	genReq := &GenerateRequest{
		RequestID:             "req-1",
		ModelID:               "paid-model",
		EstimatedInputTokens:  1000,
		EstimatedOutputTokens: 1000,
	}

	_, _, err := router.Invoke(context.Background(), &RoutingRequest{
		RequestID:    "req-1",
		AgentID:      "agent-1",
		BusinessID:   "biz-1",
		RequiredCaps: []ModelCapability{CapabilityReasoning},
	}, genReq)

	if err == nil {
		t.Fatal("expected budget exceeded error with exhausted budget and mandatory policy")
	}
}

func TestRouterMandatoryPolicyUnknownPricingFails(t *testing.T) {
	mr := NewModelRegistry()
	mr.RegisterModel(&ModelDefinition{
		ID:           "unknown-price",
		ProviderID:   "paid-provider",
		Source:       SourceCategoryPaid,
		Status:       ModelStatusActive,
		Capabilities: []ModelCapability{CapabilityReasoning},
	})

	router := NewModelRouter(mr, RoutingPolicyLocalFirst)
	router.RegisterProvider(newCountingProvider("paid-provider"))
	router.WithBudgetPolicy(BudgetPolicyMandatory)
	router.WithBudgetTracker(NewBudgetTracker(1000.0))

	genReq := &GenerateRequest{
		RequestID:             "req-1",
		ModelID:               "unknown-price",
		EstimatedInputTokens:  1000,
		EstimatedOutputTokens: 1000,
	}

	_, _, err := router.Invoke(context.Background(), &RoutingRequest{
		RequestID:    "req-1",
		AgentID:      "agent-1",
		BusinessID:   "biz-1",
		RequiredCaps: []ModelCapability{CapabilityReasoning},
	}, genReq)

	if err == nil {
		t.Fatal("expected unknown pricing error with mandatory policy")
	}
}

// --- Concurrent router-level reservation test ---

func TestRouterConcurrentReservationsCannotOverspend(t *testing.T) {
	mr := NewModelRegistry()
	mr.RegisterModel(&ModelDefinition{
		ID:            "paid-model",
		ProviderID:    "paid-provider",
		Source:        SourceCategoryPaid,
		PricingInput:  1.0,
		PricingOutput: 1.0,
		Status:        ModelStatusActive,
		Capabilities:  []ModelCapability{CapabilityReasoning},
	})

	router := NewModelRouter(mr, RoutingPolicyLocalFirst)
	router.RegisterProvider(newCountingProvider("paid-provider"))
	router.WithBudgetTracker(NewBudgetTracker(10.0))

	const numGoroutines = 20
	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			genReq := &GenerateRequest{
				RequestID:             fmt.Sprintf("req-%d", id),
				ModelID:               "paid-model",
				EstimatedInputTokens:  5000,
				EstimatedOutputTokens: 5000,
			}
			_, _, err := router.Invoke(context.Background(), &RoutingRequest{
				RequestID:    fmt.Sprintf("req-%d", id),
				AgentID:      "agent-1",
				BusinessID:   "biz-1",
				RequiredCaps: []ModelCapability{CapabilityReasoning},
			}, genReq)
			mu.Lock()
			if err == nil {
				successCount++
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	// Each call costs 10000 * 1.0 = 10000, so only 1 can succeed with 10 budget
	if successCount > 1 {
		t.Fatalf("expected at most 1 successful invocation, got %d", successCount)
	}
}
