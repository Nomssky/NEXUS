package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/foundation/health"

	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/hardening"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/memory"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
	"github.com/Nomssky/NEXUS/internal/foundation/workflow"
)

// waitForResult polls the engine until it records a result for requestID and
// returns it, failing the test if the bounded deadline elapses first (C-032).
// Tests wait for the observable state they assert on instead of sleeping a
// fixed duration and hoping asynchronous processing finished. The poll
// interval is short so successful waits return promptly; the deadline only
// bounds genuinely stuck behavior.
func waitForResult(t *testing.T, e *Engine, requestID string) *Response {
	t.Helper()

	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(10 * time.Second)

	for {
		if result, ok := e.GetResult(requestID); ok {
			return result
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for result %q", requestID)
		case <-tick.C:
		}
	}
}

// TEST-CORE-001: RequestContext creation
func TestRequestContextCreation(t *testing.T) {
	ctx := NewRequestContext("corr-1", "biz-1", "user-1")
	if ctx.CorrelationID != "corr-1" {
		t.Errorf("expected corr-1, got %s", ctx.CorrelationID)
	}
	if ctx.BusinessID != "biz-1" {
		t.Errorf("expected biz-1, got %s", ctx.BusinessID)
	}
	if ctx.ActorID != "user-1" {
		t.Errorf("expected user-1, got %s", ctx.ActorID)
	}
}

// TEST-CORE-002: RequestContext with objective
func TestRequestContextObjective(t *testing.T) {
	ctx := NewRequestContext("corr-1", "biz-1", "user-1").
		WithObjective("obj-1", "fulfill owner intent")
	if ctx.ObjectiveID != "obj-1" {
		t.Errorf("expected obj-1, got %s", ctx.ObjectiveID)
	}
	if len(ctx.WhyChain) != 1 || ctx.WhyChain[0] != "fulfill owner intent" {
		t.Errorf("expected why chain [fulfill owner intent], got %v", ctx.WhyChain)
	}
}

// TEST-CORE-003: RequestContext clone
func TestRequestContextClone(t *testing.T) {
	ctx := NewRequestContext("corr-1", "biz-1", "user-1").
		WithObjective("obj-1", "why")
	clone := ctx.Clone()

	if clone.CorrelationID != ctx.CorrelationID {
		t.Error("clone should have same correlation ID")
	}
	if clone.WhyChain[0] != ctx.WhyChain[0] {
		t.Error("clone should have same why chain")
	}

	// Modify original — clone should not change
	ctx.WhyChain = append(ctx.WhyChain, "new reason")
	if len(clone.WhyChain) != 1 {
		t.Error("clone should be independent of original")
	}
}

// TEST-CORE-004: Engine creation
func TestEngineCreation(t *testing.T) {
	e, _ := NewEngine(nil)
	if e == nil {
		t.Fatal("expected non-nil engine")
	}
	if e.Status() != lifecycle.StateCreated {
		t.Errorf("expected CREATED, got %s", e.Status())
	}
}

// TEST-CORE-005: Engine start and stop
func TestEngineStartStop(t *testing.T) {
	e, _ := NewEngine(nil)
	ctx := context.Background()

	if err := e.Start(ctx); err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	if e.Status() != lifecycle.StateRunning {
		t.Errorf("expected RUNNING, got %s", e.Status())
	}

	if err := e.Stop(ctx); err != nil {
		t.Fatalf("failed to stop: %v", err)
	}
	if e.Status() != lifecycle.StateStopped {
		t.Errorf("expected STOPPED, got %s", e.Status())
	}
}

// TEST-CORE-006: Engine double start prevented
func TestEngineDoubleStart(t *testing.T) {
	e, _ := NewEngine(nil)
	ctx := context.Background()

	e.Start(ctx)
	defer e.Stop(ctx)

	if err := e.Start(ctx); err == nil {
		t.Error("expected error on double start")
	}
}

// TEST-CORE-007: Submit request to running engine
func TestSubmitRequest(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	registerSimulatedProvider(t, e)
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := &Request{
		ID:       "req-1",
		Context:  NewRequestContext("corr-1", "biz-1", "user-1"),
		Intent:   "test intent",
		Priority: 5,
	}

	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("failed to submit: %v", err)
	}

	// Wait until the engine records the result (deterministic — C-032).
	result := waitForResult(t, e, "req-1")
	if result.Status != "completed" {
		t.Errorf("expected completed, got %s (error: %v)", result.Status, result.Error)
	}
}

// TEST-CORE-008: Chain validation — missing request ID
func TestChainValidationMissingID(t *testing.T) {
	e, _ := NewEngine(nil)
	req := &Request{
		Context: NewRequestContext("corr-1", "biz-1", "user-1"),
		Intent:  "test",
	}
	err := e.chainValidate(context.Background(), req)
	if err == nil {
		t.Error("expected error for missing request ID")
	}
}

// TEST-CORE-009: Chain validation — missing business ID
func TestChainValidationMissingBusiness(t *testing.T) {
	e, _ := NewEngine(nil)
	req := &Request{
		ID:      "req-1",
		Context: NewRequestContext("corr-1", "", "user-1"),
		Intent:  "test",
	}
	err := e.chainValidate(context.Background(), req)
	if err == nil {
		t.Error("expected error for missing business ID")
	}
}

// TEST-CORE-010: Chain validation — missing intent
func TestChainValidationMissingIntent(t *testing.T) {
	e, _ := NewEngine(nil)
	req := &Request{
		ID:      "req-1",
		Context: NewRequestContext("corr-1", "biz-1", "user-1"),
	}
	err := e.chainValidate(context.Background(), req)
	if err == nil {
		t.Error("expected error for missing intent")
	}
}

// TEST-CORE-011: Full chain execution
func TestFullChainExecution(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	registerSimulatedProvider(t, e)
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := &Request{
		ID:       "req-full",
		Context:  NewRequestContext("corr-full", "biz-1", "user-1").WithObjective("obj-1", "why"),
		Intent:   "fulfill owner request",
		Priority: 5,
	}

	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("failed to submit: %v", err)
	}

	result := waitForResult(t, e, "req-full")
	if result.Status != "completed" {
		t.Errorf("expected completed, got %s (error: %v)", result.Status, result.Error)
	}
	if len(result.AuditTrail) < 5 {
		t.Errorf("expected at least 5 audit entries, got %d", len(result.AuditTrail))
	}
}

// TEST-CORE-012: Audit trail captures chain steps
func TestAuditTrailChainSteps(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-audit",
		Context: NewRequestContext("corr-audit", "biz-1", "user-1"),
		Intent:  "test audit",
	}

	e.SubmitRequest(req)
	result := waitForResult(t, e, "req-audit")

	// Check that key steps are in the audit trail
	steps := make(map[string]bool)
	for _, entry := range result.AuditTrail {
		steps[entry.Step] = true
	}

	if !steps[string(StepValidate)] {
		t.Error("expected validate step in audit trail")
	}
	if !steps[string(StepGovernance)] {
		t.Error("expected governance step in audit trail")
	}
	if !steps[string(StepObjective)] {
		t.Error("expected objective step in audit trail")
	}
}

// TEST-CORE-013: Chain error format
func TestChainErrorFormat(t *testing.T) {
	err := &ChainError{
		Code:      "VALIDATION",
		Category:  "VALIDATION",
		Message:   "missing field",
		ChainStep: "validate",
	}
	if err.Error() != "[VALIDATION] validate: missing field" {
		t.Errorf("unexpected error format: %s", err.Error())
	}
}

// TEST-CORE-014: Request with division
func TestRequestWithDivision(t *testing.T) {
	ctx := NewRequestContext("corr-1", "biz-1", "user-1").
		WithDivision("div-1")
	if ctx.DivisionID != "div-1" {
		t.Errorf("expected div-1, got %s", ctx.DivisionID)
	}
}

// TEST-CORE-015: Request with timeout
func TestRequestWithTimeout(t *testing.T) {
	ctx := NewRequestContext("corr-1", "biz-1", "user-1").
		WithTimeout(5 * time.Second)
	if ctx.Timeout != 5*time.Second {
		t.Errorf("expected 5s, got %v", ctx.Timeout)
	}
}

// TEST-CORE-016: Submit to stopped engine fails
func TestSubmitToStoppedEngine(t *testing.T) {
	e, _ := NewEngine(nil)
	req := &Request{
		ID:      "req-1",
		Context: NewRequestContext("corr-1", "biz-1", "user-1"),
		Intent:  "test",
	}
	if err := e.SubmitRequest(req); err == nil {
		t.Error("expected error submitting to stopped engine")
	}
}

// TEST-CORE-017: Health check
func TestEngineHealth(t *testing.T) {
	e, _ := NewEngine(nil)
	ctx := context.Background()

	// Before start — down
	h := e.HealthStatus()
	if h != health.StatusDown {
		t.Errorf("expected down before start, got %s", h)
	}

	// After start — up
	e.Start(ctx)
	defer e.Stop(ctx)
	h = e.HealthStatus()
	if h != health.StatusUp {
		t.Errorf("expected up after start, got %s", h)
	}
}

// TEST-CORE-018: EventBus accessible
func TestEngineEventBus(t *testing.T) {
	e, _ := NewEngine(nil)
	if e.EventBus() == nil {
		t.Error("expected non-nil event bus")
	}
}

// TEST-CORE-019: Store accessible
func TestEngineStore(t *testing.T) {
	e, _ := NewEngine(nil)
	if e.Store() == nil {
		t.Error("expected non-nil store")
	}
}

// TEST-CORE-020: WHY chain preserved through objective creation
func TestWHYChainPreserved(t *testing.T) {
	ctx := NewRequestContext("corr-1", "biz-1", "user-1").
		WithObjective("obj-1", "mission level why").
		WithObjective("obj-2", "objective level why")

	if len(ctx.WhyChain) != 2 {
		t.Fatalf("expected 2 why entries, got %d", len(ctx.WhyChain))
	}
	if ctx.WhyChain[0] != "mission level why" {
		t.Errorf("expected first why, got %s", ctx.WhyChain[0])
	}
	if ctx.WhyChain[1] != "objective level why" {
		t.Errorf("expected second why, got %s", ctx.WhyChain[1])
	}
}

// TEST-CORE-021: Memory read returns context before objective creation
func TestMemoryReadInChain(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	// Pre-populate memory
	_ = e.memoryStore.Admit(&memory.MemoryEntry{
		Type:       memory.MemoryTypeEpisodic,
		BusinessID: "biz-1",
		Content:    "previous invoice processing result",
		Summary:    "invoice processed",
		Provenance: memory.Provenance{Source: "test", Confidence: 1.0},
		Tags:       []string{"invoice"},
	})

	req := &Request{
		ID:       "req-mem",
		Context:  NewRequestContext("corr-mem", "biz-1", "user-1"),
		Intent:   "process invoice",
		Priority: 5,
	}

	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("failed to submit: %v", err)
	}
	result := waitForResult(t, e, "req-mem")

	// Check that memory_read step is in audit trail
	found := false
	for _, entry := range result.AuditTrail {
		if entry.Step == "memory_read" {
			found = true
			if entry.Outcome == "" {
				t.Error("expected non-empty outcome for memory_read")
			}
		}
	}
	if !found {
		t.Error("expected memory_read step in audit trail")
	}
}

// TEST-CORE-022: Attention scoring in chain
func TestAttentionScoreInChain(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := &Request{
		ID:       "req-att",
		Context:  NewRequestContext("corr-att", "biz-1", "user-1"),
		Intent:   "urgent task",
		Priority: 8,
	}

	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("failed to submit: %v", err)
	}
	result := waitForResult(t, e, "req-att")

	// Check that attention_score step is in audit trail
	found := false
	for _, entry := range result.AuditTrail {
		if entry.Step == "attention_score" {
			found = true
			if entry.Outcome == "" {
				t.Error("expected non-empty outcome for attention_score")
			}
		}
	}
	if !found {
		t.Error("expected attention_score step in audit trail")
	}
}

// TEST-CORE-023: Memory write after execution
func TestMemoryWriteInChain(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := &Request{
		ID:       "req-memwrite",
		Context:  NewRequestContext("corr-memwrite", "biz-1", "user-1"),
		Intent:   "test memory write",
		Priority: 5,
	}

	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("failed to submit: %v", err)
	}
	result := waitForResult(t, e, "req-memwrite")

	// Check that memory_write step is in audit trail
	found := false
	for _, entry := range result.AuditTrail {
		if entry.Step == "memory_write" {
			found = true
		}
	}
	if !found {
		t.Error("expected memory_write step in audit trail")
	}

	// Verify memory was actually written
	query := &memory.MemoryQuery{
		BusinessID: "biz-1",
		MaxResults: 10,
	}
	entries := e.memoryStore.Retrieve(query)
	if len(entries) == 0 {
		t.Error("expected at least 1 memory entry after chain execution")
	}
}

// TEST-CORE-024: Full chain has memory and attention steps
func TestFullChainHasMemoryAndAttentionSteps(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := &Request{
		ID:       "req-full-steps",
		Context:  NewRequestContext("corr-full", "biz-1", "user-1"),
		Intent:   "full chain test",
		Priority: 5,
	}

	e.SubmitRequest(req)
	result := waitForResult(t, e, "req-full-steps")

	// Collect all steps
	steps := make(map[string]bool)
	for _, entry := range result.AuditTrail {
		steps[entry.Step] = true
	}

	// Verify key steps exist (uses ChainStep constants — C-038)
	requiredSteps := []ChainStep{
		StepValidate, StepGovernance, StepMemoryRead, StepAttention,
		StepObjective, StepDecision, StepPlan, StepWorkflow, StepSchedule,
		StepAgent, StepMemoryWrite,
	}
	for _, step := range requiredSteps {
		if !steps[string(step)] {
			t.Errorf("expected step '%s' in audit trail", step)
		}
	}
}

// TEST-CORE-025: Chain emits events at each step via event bus
func TestChainEmitsEvents(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	registerSimulatedProvider(t, e)
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	// Collect events via subscription
	var receivedEvents []*event.Event
	var eventsMu sync.Mutex
	_, _ = e.EventBus().Subscribe(event.ConsumerFunc(func(ev *event.Event) error {
		eventsMu.Lock()
		defer eventsMu.Unlock()
		receivedEvents = append(receivedEvents, ev)
		return nil
	}))

	req := &Request{
		ID:       "req-events",
		Context:  NewRequestContext("corr-events", "biz-1", "user-1"),
		Intent:   "event emission test",
		Priority: 5,
	}

	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("failed to submit: %v", err)
	}

	// Every chain event is published synchronously while the chain executes,
	// and the result is recorded only after executeChain returns — so once the
	// result is visible the queue already holds all of them. One Dispatch
	// delivers the full batch. (C-032/C-034: replaced sleeps and arbitrary
	// repeated Dispatch calls with this deterministic ordering.)
	waitForResult(t, e, "req-events")
	if _, err := e.EventBus().Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	eventsMu.Lock()
	defer eventsMu.Unlock()

	// Verify key events were emitted
	expectedTypes := []string{
		"chain.started",
		"chain.governance.passed",
		"chain.memory.read",
		"chain.attention.scored",
		"chain.objective.created",
		"chain.decision.made",
		"chain.plan.created",
		"chain.workflow.created",
		"chain.schedule.scheduled",
		"chain.executor.completed",
		"chain.memory.written",
		"chain.completed",
	}

	found := make(map[string]bool)
	for _, ev := range receivedEvents {
		found[string(ev.Type)] = true
	}

	for _, et := range expectedTypes {
		if !found[et] {
			t.Errorf("expected event type '%s' to be emitted", et)
		}
	}
}

// TEST-CORE-026: Backpressure rejects when queue is full
func TestBackpressureRejectsWhenFull(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	// Fill the backpressure queue to capacity
	for i := 0; i < 100; i++ {
		if !e.Backpressure().Accept() {
			t.Fatalf("expected Accept at %d", i)
		}
	}

	// Next submission should be rejected
	req := &Request{
		ID:       "req-bp-full",
		Context:  NewRequestContext("corr-bp", "biz-1", "user-1"),
		Intent:   "should be rejected",
		Priority: 5,
	}
	err := e.SubmitRequest(req)
	if err == nil {
		t.Fatal("expected backpressure rejection error")
	}
	if !strings.Contains(err.Error(), "backpressure") {
		t.Errorf("expected backpressure error, got: %v", err)
	}
}

// TEST-CORE-027: Circuit breaker trips after consecutive failures
func TestCircuitBreakerTrips(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	// Record 5 failures (threshold)
	for i := 0; i < 5; i++ {
		e.CircuitBreaker().RecordFailure()
	}

	if e.CircuitBreaker().State() != "open" {
		t.Errorf("expected breaker open, got %s", e.CircuitBreaker().State())
	}

	// Submit should be rejected by circuit breaker
	req := &Request{
		ID:       "req-cb-open",
		Context:  NewRequestContext("corr-cb", "biz-1", "user-1"),
		Intent:   "should be rejected by breaker",
		Priority: 5,
	}
	e.SubmitRequest(req)
	result := waitForResult(t, e, "req-cb-open")

	if result.Status != "failed" {
		t.Errorf("expected failed status, got %s", result.Status)
	}
	if result.Error == nil {
		t.Fatal("expected error in response")
	}
	if !strings.Contains(result.Error.Message, "circuit breaker") {
		t.Errorf("expected circuit breaker error, got: %s", result.Error.Message)
	}
}

// TEST-CORE-028: RecoveryManager records failures on execution errors.
// Direct unit coverage of Detect() bookkeeping. Chain-integration coverage —
// recovery triggered through SubmitRequest → executeChain → chainExecute —
// lives in TestRecoveryRecordsFailureThroughChain (C-035).
func TestRecoveryManagerRecordsFailure(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	rec := e.RecoveryManager().Detect(
		hardening.FailureTaskUnknown,
		"executor",
		"biz-1",
		"test failure detection",
	)

	if rec == nil {
		t.Fatal("expected failure record")
	}
	if rec.Mode != hardening.FailureTaskUnknown {
		t.Errorf("expected task_unknown mode, got %s", rec.Mode)
	}
	if rec.Component != "executor" {
		t.Errorf("expected executor component, got %s", rec.Component)
	}
	if e.RecoveryManager().RecordCount() != 1 {
		t.Errorf("expected 1 record, got %d", e.RecoveryManager().RecordCount())
	}
}

// TEST-CORE-045: RecoveryManager records failures through the real chain
// (C-035). Unlike TestRecoveryManagerRecordsFailure, this test never calls
// Detect() itself: an expired deadline drives Step 8 (chainExecute) to fail
// through SubmitRequest → executeChain, which is the production call site of
// RecoveryManager.Detect (chain.go) — the record and the
// chain.hardening.failure_detected event must both appear as a result.
func TestRecoveryRecordsFailureThroughChain(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	// Capture the chain's failure-detection event; its outcome carries the
	// FailureRecord ID created by Detect (chain.go chainEmit payload).
	var eventsMu sync.Mutex
	var failureIDs []string
	_, _ = e.EventBus().Subscribe(event.ConsumerFunc(func(ev *event.Event) error {
		if string(ev.Type) != "chain.hardening.failure_detected" {
			return nil
		}
		var payload struct {
			Outcome string `json:"outcome"`
		}
		if err := json.Unmarshal(ev.Data, &payload); err != nil || payload.Outcome == "" {
			return nil
		}
		eventsMu.Lock()
		failureIDs = append(failureIDs, payload.Outcome)
		eventsMu.Unlock()
		return nil
	}))

	// An expired deadline makes chainExecute fail through the production path.
	past := now.Add(-time.Minute)
	req := &Request{
		ID:       "req-recovery-chain",
		Context:  NewRequestContext("corr-recovery", "biz-1", "user-1"),
		Intent:   "trigger execution failure",
		Deadline: &past,
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}

	result := waitForResult(t, e, "req-recovery-chain")
	if result.Status != "failed" {
		t.Fatalf("expected failed status, got %s", result.Status)
	}
	if result.Error == nil {
		t.Fatal("expected error in result")
	}
	if !strings.Contains(result.Error.Message, "deadline") {
		t.Errorf("expected deadline execution error, got: %s", result.Error.Message)
	}

	// The chain must have recorded exactly one failure — this test never
	// called Detect() directly, so the record proves the chain path ran.
	if got := e.RecoveryManager().RecordCount(); got != 1 {
		t.Fatalf("expected 1 recovery record from chain execution, got %d", got)
	}

	if _, err := e.EventBus().Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	eventsMu.Lock()
	ids := append([]string(nil), failureIDs...)
	eventsMu.Unlock()
	if len(ids) != 1 {
		t.Fatalf("expected 1 chain.hardening.failure_detected event, got %d", len(ids))
	}

	rec, ok := e.RecoveryManager().GetRecord(ids[0])
	if !ok {
		t.Fatalf("expected failure record %q to be retrievable", ids[0])
	}
	if rec.Mode != hardening.FailureTaskUnknown {
		t.Errorf("expected task_unknown mode, got %s", rec.Mode)
	}
	if rec.Component != "executor" {
		t.Errorf("expected executor component, got %s", rec.Component)
	}
	if rec.BusinessID != "biz-1" {
		t.Errorf("expected business id biz-1, got %s", rec.BusinessID)
	}
	if !strings.Contains(rec.Description, "execution failed") {
		t.Errorf("expected execution failure description, got %q", rec.Description)
	}
}

// =====================================================================
// P2 TEST COVERAGE GAPS
// =====================================================================

// TEST-CORE-030: WithPersistence error path — invalid directory
func TestPersistenceErrorPath(t *testing.T) {
	// Create engine with persistence pointing to an invalid path (file, not dir)
	f, err := os.CreateTemp("", "not-a-dir-*")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	defer os.Remove(f.Name())

	_, err = NewEngine(nil, WithPersistence(f.Name()))
	if err == nil {
		t.Fatal("expected error for invalid persistence directory")
	}
	// Error should indicate the directory issue
	if !strings.Contains(err.Error(), "not a directory") && !strings.Contains(err.Error(), "filestore") {
		t.Errorf("expected filestore/dir error, got: %v", err)
	}
}

// TEST-CORE-031: WithPersistence success path
func TestPersistenceSuccessPath(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	e, err := NewEngine(nil,
		WithPersistence(dir),
		WithClock(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e == nil {
		t.Fatal("expected engine")
	}
}

// TEST-CORE-032: Resume lifecycle — start, stop, resume, submit. C-3
// regression: the resumed request must genuinely COMPLETE — Resume restarts
// the task executor, so before the fix every post-resume request died with
// "executor not running" (this test then passed on a failed result because
// it only asserted BusinessID).
func TestResumeLifecycle(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	registerSimulatedProvider(t, e)
	ctx := context.Background()

	// Start
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if e.Status() != lifecycle.StateRunning {
		t.Fatalf("expected RUNNING, got %s", e.Status())
	}

	// Stop
	if err := e.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if e.Status() != lifecycle.StateStopped {
		t.Fatalf("expected STOPPED, got %s", e.Status())
	}

	// Resume
	if err := e.Resume(ctx); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if e.Status() != lifecycle.StateRunning {
		t.Fatalf("expected RUNNING after resume, got %s", e.Status())
	}

	// Submit after resume — should work
	req := &Request{
		ID:      "req-resume-1",
		Context: NewRequestContext("corr-1", "biz-1", "user-1"),
		Intent:  "test resume",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit after resume: %v", err)
	}

	// Wait for the post-resume request to be processed (deterministic).
	result := waitForResult(t, e, "req-resume-1")
	if result.BusinessID != "biz-1" {
		t.Errorf("expected business_id biz-1, got %s", result.BusinessID)
	}
	if result.Status != "completed" {
		t.Errorf("C-3: post-resume request must complete, got %q (err=%+v)", result.Status, result.Error)
	}
}

// TEST-CORE-033: Resume fails on non-stopped engine
func TestResumeFailsOnRunning(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	if err := e.Resume(ctx); err == nil {
		t.Fatal("expected error resuming running engine")
	}
}

// TEST-CORE-034: Concurrent request submissions
func TestConcurrentSubmissions(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	const n = 20
	var wg sync.WaitGroup
	errors := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := &Request{
				ID:      fmt.Sprintf("req-concurrent-%d", idx),
				Context: NewRequestContext(fmt.Sprintf("corr-%d", idx), "biz-1", "user-1"),
				Intent:  fmt.Sprintf("concurrent test %d", idx),
			}
			if err := e.SubmitRequest(req); err != nil {
				errors <- fmt.Errorf("submit %d: %w", idx, err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}

	// Wait for all requests to be processed (deterministic — C-032).
	for i := 0; i < n; i++ {
		waitForResult(t, e, fmt.Sprintf("req-concurrent-%d", i))
	}
}

// TEST-CORE-035: ChainError includes CorrelationID and Timestamp
func TestChainErrorFields(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	// Submit with empty intent to trigger validation error
	req := &Request{
		ID:      "req-error-fields",
		Context: NewRequestContext("corr-err", "biz-err", "user-err"),
		Intent:  "", // empty intent triggers validation failure
	}
	_ = e.SubmitRequest(req)

	result := waitForResult(t, e, "req-error-fields")

	if result.Error == nil {
		t.Fatal("expected error in result")
	}
	if result.Error.CorrelationID == "" {
		t.Error("expected CorrelationID in error")
	}
	if result.Error.Timestamp.IsZero() {
		t.Error("expected Timestamp in error")
	}
	if result.BusinessID != "biz-err" {
		t.Errorf("expected business_id 'biz-err', got %q", result.BusinessID)
	}
}

// TEST-CORE-036: BusinessID propagated through chain
func TestBusinessIDInResponse(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-biz-id",
		Context: NewRequestContext("corr-biz", "my-business", "user-1"),
		Intent:  "test business id",
	}
	_ = e.SubmitRequest(req)

	result := waitForResult(t, e, "req-biz-id")
	if result.BusinessID != "my-business" {
		t.Errorf("expected business_id 'my-business', got %q", result.BusinessID)
	}
}

// TEST-CORE-037: REQUIRE_APPROVAL → Retryable=false (D4: contract tables
// CORE_INTERFACE_CONTRACTS.md §3 / SCHEMA_GOVERNANCE_ATTENTION.md §5.3 /
// RUNTIME_EXECUTION_CONTRACTS.md §3.3 all say Retryable No — "wait for
// approval"; "retry after approval" is a future state transition, not an
// instruction to retry now). DENY → Retryable=false as well.
func TestChainGovernanceRetryable(t *testing.T) {
	now := time.Now()
	e, err := NewEngine(nil, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}

	req := &Request{
		ID:      "req-gov-retry",
		Context: NewRequestContext("corr-gov", "biz-1", "user-1"),
		Intent:  "test",
	}

	// Case 1: REQUIRE_APPROVAL → Retryable = false (D4: contract No — wait
	// for approval; retrying now cannot succeed)
	e.govEngine = governance.NewEngine([]*governance.Policy{
		{
			PolicyID:   "require-approval",
			Name:       "Require Approval",
			Status:     governance.PolicyStatusActive,
			Effect:     governance.REQUIRE_APPROVAL,
			Subject:    governance.Subject{SubjectType: "all"},
			Action:     governance.Action{ActionType: "custom"},
			Resource:   governance.Resource{ResourceType: "all"},
			Precedence: 0,
		},
	})
	_, err = e.chainGovernance(context.Background(), req)
	if err == nil {
		t.Fatal("expected governance error")
	}
	ce, ok := err.(*ChainError)
	if !ok {
		t.Fatalf("expected *ChainError, got %T", err)
	}
	if ce.Retryable {
		t.Error("expected Retryable=false for REQUIRE_APPROVAL (D4: contract = No, wait for approval)")
	}

	// Case 2: DENY → Retryable = false (retry won't help)
	e.govEngine = governance.NewEngine([]*governance.Policy{
		{
			PolicyID:   "deny",
			Name:       "Deny",
			Status:     governance.PolicyStatusActive,
			Effect:     governance.DENY,
			Subject:    governance.Subject{SubjectType: "all"},
			Action:     governance.Action{ActionType: "custom"},
			Resource:   governance.Resource{ResourceType: "all"},
			Precedence: 0,
		},
	})
	_, err = e.chainGovernance(context.Background(), req)
	if err == nil {
		t.Fatal("expected governance error")
	}
	ce, ok = err.(*ChainError)
	if !ok {
		t.Fatalf("expected *ChainError, got %T", err)
	}
	if ce.Retryable {
		t.Error("expected Retryable=false for DENY")
	}
}

// TEST-CORE-046: REQUIRE_APPROVAL surfaces as category APPROVAL_REQUIRED.
// CORE_INTERFACE_CONTRACTS §3: POLICY_DENIED covers governance outcome DENY
// only; "a governance REQUIRE_APPROVAL surfaces as category APPROVAL_REQUIRED"
// (same in SCHEMA_GOVERNANCE_ATTENTION.md). Retryable is false for
// REQUIRE_APPROVAL per D4 — the authoritative contract tables say No
// ("wait for approval"), so retrying now is not a valid caller action —
// this test pins both at unit and end-to-end (stored result) level.
func TestChainGovernanceApprovalRequiredCategory(t *testing.T) {
	requireApproval := []*governance.Policy{
		{
			PolicyID:   "require-approval",
			Name:       "Require Approval",
			Status:     governance.PolicyStatusActive,
			Effect:     governance.REQUIRE_APPROVAL,
			Subject:    governance.Subject{SubjectType: "all"},
			Action:     governance.Action{ActionType: "custom"},
			Resource:   governance.Resource{ResourceType: "all"},
			Precedence: 0,
		},
	}
	denyAll := []*governance.Policy{
		{
			PolicyID:   "deny",
			Name:       "Deny",
			Status:     governance.PolicyStatusActive,
			Effect:     governance.DENY,
			Subject:    governance.Subject{SubjectType: "all"},
			Action:     governance.Action{ActionType: "custom"},
			Resource:   governance.Resource{ResourceType: "all"},
			Precedence: 0,
		},
	}

	now := time.Now()
	e, err := NewEngine(nil, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	req := &Request{
		ID:      "req-appr-cat",
		Context: NewRequestContext("corr-appr-cat", "biz-1", "user-1"),
		Intent:  "test",
	}

	// Unit: REQUIRE_APPROVAL → APPROVAL_REQUIRED category/code.
	e.govEngine = governance.NewEngine(requireApproval)
	_, err = e.chainGovernance(context.Background(), req)
	if err == nil {
		t.Fatal("expected governance error")
	}
	ce, ok := err.(*ChainError)
	if !ok {
		t.Fatalf("expected *ChainError, got %T", err)
	}
	if ce.Category != "APPROVAL_REQUIRED" {
		t.Errorf("expected category APPROVAL_REQUIRED for REQUIRE_APPROVAL, got %q", ce.Category)
	}
	if ce.Code != "APPROVAL_REQUIRED" {
		t.Errorf("expected code APPROVAL_REQUIRED for REQUIRE_APPROVAL, got %q", ce.Code)
	}
	if ce.Retryable {
		t.Error("expected Retryable=false for REQUIRE_APPROVAL (D4: contract = No, wait for approval)")
	}

	// Unit: DENY keeps POLICY_DENIED category (contract: DENY outcome only).
	e.govEngine = governance.NewEngine(denyAll)
	_, err = e.chainGovernance(context.Background(), req)
	if err == nil {
		t.Fatal("expected governance error")
	}
	ce, ok = err.(*ChainError)
	if !ok {
		t.Fatalf("expected *ChainError, got %T", err)
	}
	if ce.Category != "POLICY_DENIED" {
		t.Errorf("expected category POLICY_DENIED for DENY, got %q", ce.Category)
	}
	if ce.Code != "POLICY_DENIED" {
		t.Errorf("expected code POLICY_DENIED for DENY, got %q", ce.Code)
	}
	if ce.Retryable {
		t.Error("expected Retryable=false for DENY")
	}

	// End-to-end: the stored result carries the approval-required category.
	e.govEngine = governance.NewEngine(requireApproval)
	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer e.Stop(ctx)
	e2e := &Request{
		ID:      "req-appr-e2e",
		Context: NewRequestContext("corr-appr-e2e", "biz-1", "user-1"),
		Intent:  "approval gated work",
	}
	if err := e.SubmitRequest(e2e); err != nil {
		t.Fatalf("submit: %v", err)
	}
	result := waitForResult(t, e, "req-appr-e2e")
	if result.Status != "failed" {
		t.Errorf("expected status failed at the approval gate, got %q", result.Status)
	}
	if result.Error == nil {
		t.Fatal("expected error on approval-gated result")
	}
	if result.Error.Category != "APPROVAL_REQUIRED" || result.Error.Code != "APPROVAL_REQUIRED" {
		t.Errorf("expected APPROVAL_REQUIRED category/code, got code=%q category=%q",
			result.Error.Code, result.Error.Category)
	}
	if result.Error.Retryable {
		t.Error("expected Retryable=false on approval-gated result (D4: contract = No, wait for approval)")
	}
}

// TEST-CORE-047: executor-level governance DENY surfaces as a failed response
// with POLICY_DENIED (D1). CORE_INTERFACE_CONTRACTS §3 requires the same
// denied outcome the chain-level gate produces: Status failed, Error
// category/code POLICY_DENIED, terminal chain.failed event. The deny policy
// is scoped to action execute_task so the chain gate (execute_request)
// passes and the executor's Step-1 gate is the one that denies — before the
// fix the "denied" outcome (execErr == nil) fell through Step-12's default
// branch and stored a "completed" response with a nil error.
func TestExecutorDeniedSurfacesPolicyDenied(t *testing.T) {
	now := time.Now()
	e, err := NewEngine(nil, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	// SetPolicies mutates the shared engine in place: the executor holds the
	// same *governance.Engine pointer captured at construction (engine.go:186),
	// so the executor gate sees this policy set too. Default-allow keeps the
	// chain gate (execute_request) passing; the DENY is scoped to the
	// executor's action (execute_task) so only the executor gate denies.
	e.govEngine.SetPolicies([]*governance.Policy{
		{
			PolicyID:   "default-allow",
			Name:       "Default Allow",
			Status:     governance.PolicyStatusActive,
			Effect:     governance.ALLOW,
			Subject:    governance.Subject{SubjectType: "all"},
			Action:     governance.Action{ActionType: "custom"},
			Resource:   governance.Resource{ResourceType: "all"},
			Precedence: 0,
		},
		{
			PolicyID: "deny-execute-task",
			Name:     "Deny Execute Task",
			Status:   governance.PolicyStatusActive,
			Effect:   governance.DENY,
			Subject:  governance.Subject{SubjectType: "all"},
			// matchesAction treats an empty ActionIDs list as "all actions",
			// so the ID list is what actually scopes the deny to the
			// executor's action (execute_task) and spares the chain gate
			// (execute_request).
			Action:     governance.Action{ActionType: "execute_task", ActionIDs: []string{"execute_task"}},
			Resource:   governance.Resource{ResourceType: "all"},
			Precedence: 0,
		},
	})

	ctx := context.Background()
	var received []string
	_, _ = e.EventBus().Subscribe(event.ConsumerFunc(func(ev *event.Event) error {
		received = append(received, string(ev.Type))
		return nil
	}))

	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-exec-denied",
		Context: NewRequestContext("corr-exec-denied", "biz-1", "user-1"),
		Intent:  "denied work",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	result := waitForResult(t, e, req.ID)

	if result.Status != "failed" {
		t.Errorf("executor DENY must store status failed, got %q", result.Status)
	}
	if result.Error == nil {
		t.Fatal("executor DENY must store a contract ChainError, got nil")
	}
	if result.Error.Category != "POLICY_DENIED" {
		t.Errorf("expected category POLICY_DENIED, got %q", result.Error.Category)
	}
	if result.Error.Code != "POLICY_DENIED" {
		t.Errorf("expected code POLICY_DENIED, got %q", result.Error.Code)
	}
	if result.Error.Retryable {
		t.Error("expected Retryable=false for POLICY_DENIED (parity with chain gate)")
	}
	// The governance denial reason must not be lost — the contract stores a
	// human-readable message on the error.
	if !strings.Contains(result.Error.Message, "governance denied") {
		t.Errorf("expected executor governance reason preserved in message, got %q", result.Error.Message)
	}
	if result.Error.ChainStep != string(StepAgent) {
		t.Errorf("expected chain step %q (executor gate, not the chain gate), got %q",
			string(StepAgent), result.Error.ChainStep)
	}
	if result.Error.CorrelationID != "corr-exec-denied" {
		t.Errorf("expected correlation id propagated, got %q", result.Error.CorrelationID)
	}

	// Proof the chain gate PASSED and the executor gate is the one that
	// denied: the governance audit entry records "allowed" and the executor
	// outcome carries status=denied.
	govAllowed := false
	for _, entry := range result.AuditTrail {
		if entry.Step == string(StepGovernance) && entry.Outcome == "allowed" {
			govAllowed = true
		}
	}
	if !govAllowed {
		t.Error("expected chain governance audit entry allowed (chain gate must pass)")
	}
	if result.Outcome == nil || result.Outcome.Metrics["executor_status"] != "denied" {
		t.Errorf("expected executor_status denied in metrics, got %+v", result.Outcome)
	}

	if _, err := e.EventBus().Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	got := make(map[string]bool, len(received))
	for _, typ := range received {
		got[typ] = true
	}
	for _, typ := range []string{"chain.governance.passed", "executor.denied", "chain.failed"} {
		if !got[typ] {
			t.Errorf("expected event %s, got %v", typ, received)
		}
	}
	// The handler must never run past the gate: no executor.received /
	// executor.assigned, and no chain.completed terminal event.
	for _, typ := range []string{"executor.received", "executor.assigned", "chain.completed"} {
		if got[typ] {
			t.Errorf("event %s must not fire when the executor gate denies, got %v", typ, received)
		}
	}
}

// TEST-CORE-048: executor-level governance REQUIRE_APPROVAL surfaces through
// the same error envelope as the chain-level gate (D2): Status failed, Error
// code/category APPROVAL_REQUIRED, Retryable false (D4: contract table says
// No — "wait for approval", not "retry now"), ChainStep agent, governance
// reason preserved, terminal chain.failed. No contract defines
// pending_approval as a Response.Status — the executor Outcome keeps its
// pending_approval value, the stored Response must not. The deny/require
// policy is scoped to action execute_task so the chain gate (execute_request)
// passes and the executor's Step-1 gate is the one that requires approval.
func TestExecutorRequiresApprovalSurfacesApprovalRequired(t *testing.T) {
	now := time.Now()
	e, err := NewEngine(nil, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	// SetPolicies mutates the shared engine in place: the executor holds the
	// same *governance.Engine pointer captured at construction (engine.go:186).
	// Default-allow keeps the chain gate passing; the REQUIRE_APPROVAL is
	// scoped to the executor's action (execute_task) via ActionIDs — an empty
	// ActionIDs list would match every action (matchesIDList = all).
	e.govEngine.SetPolicies([]*governance.Policy{
		{
			PolicyID:   "default-allow",
			Name:       "Default Allow",
			Status:     governance.PolicyStatusActive,
			Effect:     governance.ALLOW,
			Subject:    governance.Subject{SubjectType: "all"},
			Action:     governance.Action{ActionType: "custom"},
			Resource:   governance.Resource{ResourceType: "all"},
			Precedence: 0,
		},
		{
			PolicyID:   "require-approval-execute-task",
			Name:       "Require Approval For Execute Task",
			Status:     governance.PolicyStatusActive,
			Effect:     governance.REQUIRE_APPROVAL,
			Subject:    governance.Subject{SubjectType: "all"},
			Action:     governance.Action{ActionType: "execute_task", ActionIDs: []string{"execute_task"}},
			Resource:   governance.Resource{ResourceType: "all"},
			Precedence: 0,
		},
	})

	ctx := context.Background()
	var received []string
	_, _ = e.EventBus().Subscribe(event.ConsumerFunc(func(ev *event.Event) error {
		received = append(received, string(ev.Type))
		return nil
	}))

	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-exec-appr",
		Context: NewRequestContext("corr-exec-appr", "biz-1", "user-1"),
		Intent:  "approval gated work",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	result := waitForResult(t, e, req.ID)

	if result.Status != "failed" {
		t.Errorf("executor REQUIRE_APPROVAL must store status failed, got %q", result.Status)
	}
	if result.Status == "pending_approval" {
		t.Error("pending_approval must not become a Response.Status (D2)")
	}
	if result.Error == nil {
		t.Fatal("executor REQUIRE_APPROVAL must store a contract ChainError, got nil")
	}
	if result.Error.Code != "APPROVAL_REQUIRED" {
		t.Errorf("expected code APPROVAL_REQUIRED, got %q", result.Error.Code)
	}
	if result.Error.Category != "APPROVAL_REQUIRED" {
		t.Errorf("expected category APPROVAL_REQUIRED, got %q", result.Error.Category)
	}
	if result.Error.Retryable {
		t.Error("expected Retryable=false for APPROVAL_REQUIRED (D4: contract table = No, wait for approval)")
	}
	if result.Error.ChainStep != string(StepAgent) {
		t.Errorf("expected chain step %q (executor gate, not the chain gate), got %q",
			string(StepAgent), result.Error.ChainStep)
	}
	// The governance reason must survive the mapping (executor formats
	// "governance requires approval: <reason>").
	if !strings.Contains(result.Error.Message, "governance requires approval") {
		t.Errorf("expected executor governance reason preserved in message, got %q", result.Error.Message)
	}
	if result.Error.CorrelationID != "corr-exec-appr" {
		t.Errorf("expected correlation id propagated, got %q", result.Error.CorrelationID)
	}

	// Proof the chain gate PASSED and the executor gate is the one that
	// required approval: the governance audit entry records "allowed" and the
	// executor outcome carries its own pending_approval value.
	govAllowed := false
	for _, entry := range result.AuditTrail {
		if entry.Step == string(StepGovernance) && entry.Outcome == "allowed" {
			govAllowed = true
		}
	}
	if !govAllowed {
		t.Error("expected chain governance audit entry allowed (chain gate must pass)")
	}
	if result.Outcome == nil || result.Outcome.Metrics["executor_status"] != "pending_approval" {
		t.Errorf("expected executor_status pending_approval in metrics, got %+v", result.Outcome)
	}

	if _, err := e.EventBus().Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	got := make(map[string]bool, len(received))
	for _, typ := range received {
		got[typ] = true
	}
	for _, typ := range []string{"chain.governance.passed", "executor.pending_approval", "chain.failed"} {
		if !got[typ] {
			t.Errorf("expected event %s, got %v", typ, received)
		}
	}
	// The handler must never run past the gate: no executor.received /
	// executor.assigned, and no chain.completed terminal event.
	for _, typ := range []string{"executor.received", "executor.assigned", "chain.completed"} {
		if got[typ] {
			t.Errorf("event %s must not fire when the executor gate requires approval, got %v", typ, received)
		}
	}
}

// TEST-CORE-038: No phantom model/tool audit entries (C-005 fix)
func TestNoPhantomAuditEntries(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-phantom",
		Context: NewRequestContext("corr-phantom", "biz-1", "user-1"),
		Intent:  "test phantom entries",
	}
	_ = e.SubmitRequest(req)
	result := waitForResult(t, e, "req-phantom")

	for _, entry := range result.AuditTrail {
		if entry.Step == "model" || entry.Step == "tool" {
			t.Errorf("phantom audit entry '%s' with fabricated outcome: %s", entry.Step, entry.Outcome)
		}
		if strings.Contains(entry.Outcome, "model=executor") || strings.Contains(entry.Outcome, "tool=executor") {
			t.Errorf("fabricated outcome in audit entry %s: %s", entry.Step, entry.Outcome)
		}
	}

	// Verify entry must reflect actual execution status
	for _, entry := range result.AuditTrail {
		if entry.Step == "verify" && !strings.Contains(entry.Outcome, "status=") {
			t.Errorf("verify entry lacks actual status: %s", entry.Outcome)
		}
	}
}

// TEST-CORE-039: Request.Deadline honored (C-026 fix)
func TestRequestDeadlineEnforced(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	// Deadline already in the past → fail fast
	past := now.Add(-1 * time.Minute)
	req := &Request{
		ID:       "req-deadline-past",
		Context:  NewRequestContext("corr-dl", "biz-1", "user-1"),
		Intent:   "test deadline",
		Deadline: &past,
	}
	_ = e.SubmitRequest(req)
	result := waitForResult(t, e, "req-deadline-past")

	if result.Error == nil {
		t.Fatal("expected deadline error for expired deadline")
	}
	if !strings.Contains(result.Error.Message, "deadline") {
		t.Errorf("expected deadline error message, got: %s", result.Error.Message)
	}
}

// TEST-CORE-040: ModelRegistry/ModelRouter public accessors (C-020 fix)
func TestModelAccessors(t *testing.T) {
	e, err := NewEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	if e.ModelRegistry() == nil {
		t.Error("expected non-nil ModelRegistry accessor")
	}
	if e.ModelRouter() == nil {
		t.Error("expected non-nil ModelRouter accessor")
	}
}

// TEST-CORE-041: Outcome.Metrics populated on success (C-025 fix)
func TestOutcomeMetricsPopulated(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	registerSimulatedProvider(t, e)
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-metrics",
		Context: NewRequestContext("corr-metrics", "biz-1", "user-1"),
		Intent:  "test metrics",
	}
	_ = e.SubmitRequest(req)
	result := waitForResult(t, e, "req-metrics")
	if result.Status != "completed" {
		t.Fatalf("expected completed, got %s", result.Status)
	}
	if result.Outcome == nil {
		t.Fatal("expected outcome")
	}
	if result.Outcome.Metrics == nil {
		t.Fatal("expected Metrics map populated (C-025)")
	}
	if _, hasDuration := result.Outcome.Metrics["duration_ms"]; !hasDuration {
		t.Error("expected duration_ms in metrics")
	}
	if _, hasStatus := result.Outcome.Metrics["executor_status"]; !hasStatus {
		t.Error("expected executor_status in metrics")
	}
}

// TEST-CORE-042: Failure emits chain.failed event on event bus (C-009/C-010)
func TestFailureEmitsChainFailedEvent(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	// Subscribe before triggering failure
	var received []string
	consumer := event.ConsumerFunc(func(ev *event.Event) error {
		received = append(received, string(ev.Type))
		return nil
	})
	if _, err := e.EventBus().Subscribe(consumer); err != nil {
		t.Fatal(err)
	}

	// Trigger validation failure (empty intent)
	req := &Request{
		ID:      "req-fail-event",
		Context: NewRequestContext("corr-fail", "biz-1", "user-1"),
		Intent:  "",
	}
	_ = e.SubmitRequest(req)

	// chain.failed is published by chainError before the Response is
	// returned, so it is queued by the time the result is recorded —
	// wait for that observable state, then deliver (C-032: no timing).
	waitForResult(t, e, "req-fail-event")
	if _, err := e.EventBus().Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	found := false
	for _, typ := range received {
		if typ == "chain.failed" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected chain.failed event, got: %v", received)
	}
}

// ---------------------------------------------------------------------------
// E-005: external cancellation of in-flight requests (core scope).
// ---------------------------------------------------------------------------

// waitUntil polls cond until it holds, failing the test if the bounded
// deadline elapses first (E-015 pattern: wait for observable state, never sleep).
func waitUntil(t *testing.T, desc string, cond func() bool) {
	t.Helper()

	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(10 * time.Second)

	for {
		if cond() {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %s", desc)
		case <-tick.C:
		}
	}
}

// waitSignal waits for one value on ch with a bounded deadline.
func waitSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// blockingProvider is a cooperative model provider that blocks until its
// context is cancelled (or the test releases it), giving cancellation tests a
// deterministic in-flight window. It records whether cancellation reached it —
// end-to-end proof of provider context threading (E-005 decision 9).
type blockingProvider struct {
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	sawCtx  bool
}

func newBlockingProvider() *blockingProvider {
	return &blockingProvider{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
}

func (p *blockingProvider) Identify() string              { return "blocking-provider" }
func (p *blockingProvider) HealthCheck() error            { return nil }
func (p *blockingProvider) ListModels() ([]string, error) { return nil, nil }

func (p *blockingProvider) Invoke(ctx context.Context, req *modelrouter.GenerateRequest) (*modelrouter.GenerateResponse, error) {
	select {
	case p.started <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
		p.mu.Lock()
		p.sawCtx = true
		p.mu.Unlock()
		return nil, ctx.Err()
	case <-p.release:
		return &modelrouter.GenerateResponse{
			RequestID:    req.RequestID,
			ModelID:      req.ModelID,
			Content:      "released",
			FinishReason: "stop",
		}, nil
	}
}

// cancelled reports whether the provider observed context cancellation.
func (p *blockingProvider) cancelled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sawCtx
}

// registerBlockingProvider wires the cooperative blocking provider into the
// engine so a submitted request deterministically parks inside the executor's
// default handler (model invocation).
func registerBlockingProvider(t *testing.T, e *Engine) *blockingProvider {
	t.Helper()
	p := newBlockingProvider()
	if err := e.ModelRegistry().RegisterModel(&modelrouter.ModelDefinition{
		ID:         "cancel-model",
		ProviderID: p.Identify(),
		Capabilities: []modelrouter.ModelCapability{
			modelrouter.CapabilityReasoning,
			modelrouter.CapabilityToolCalling,
		},
		Runtime: modelrouter.RuntimeLocal,
		Status:  modelrouter.ModelStatusActive,
	}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	e.ModelRouter().RegisterProvider(p)
	return p
}

// registerSimulatedProvider wires the repository's simulated LocalProvider and
// one model into the engine so the executor's default handler can route and
// complete a request. The default engine ships an empty model registry — with
// no model the provider invocation fails (E-008) and honest chain semantics
// report "failed", not "completed" — tests that assert a genuinely completed
// lifecycle must register a model first (mirrors the production wiring in
// launcher.New).
func registerSimulatedProvider(t *testing.T, e *Engine) {
	t.Helper()
	p := modelrouter.NewLocalProvider(modelrouter.ProviderConfig{ID: "simulated"})
	if err := e.ModelRegistry().RegisterModel(&modelrouter.ModelDefinition{
		ID:         "simulated:default",
		ProviderID: p.Identify(),
		Capabilities: []modelrouter.ModelCapability{
			modelrouter.CapabilityReasoning,
			modelrouter.CapabilityToolCalling,
		},
		Runtime: modelrouter.RuntimeLocal,
		Status:  modelrouter.ModelStatusActive,
	}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	e.ModelRouter().RegisterProvider(p)
}

// TEST-E005-CORE-01: unknown request → ErrRequestNotFound (gateway 404).
func TestCancelRequestUnknown(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	err := e.CancelRequest("req-nope", "biz-1", "user-1")
	if !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("expected ErrRequestNotFound, got %v", err)
	}
}

// TEST-E005-CORE-02: empty business scope fails closed → ErrScopeMismatch
// (gateway 403) — an empty scope can never match a recorded owner.
func TestCancelRequestEmptyScopeFailClosed(t *testing.T) {
	e, _ := NewEngine(nil)

	err := e.CancelRequest("req-any", "", "user-1")
	if !errors.Is(err, ErrScopeMismatch) {
		t.Fatalf("expected ErrScopeMismatch, got %v", err)
	}
}

// TEST-E005-CORE-03: another business's request → ErrScopeMismatch (403) even
// when the request is already terminal (403 precedes 409, mirroring GET result).
func TestCancelRequestScopeMismatch(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-scope",
		Context: NewRequestContext("corr-scope", "biz-1", "user-1"),
		Intent:  "scope check",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	waitForResult(t, e, req.ID)

	err := e.CancelRequest(req.ID, "biz-2", "user-1")
	if !errors.Is(err, ErrScopeMismatch) {
		t.Fatalf("expected ErrScopeMismatch, got %v", err)
	}
}

// TEST-E005-CORE-04: queued cancel — the request never executes. Terminal
// cancelled response with the contract ChainError; no objective/workflow/
// executor audit steps; chain.cancelled emitted, never chain.failed or
// chain.completed; no workflow created; inflight deregistered after the result.
func TestCancelQueuedRequestNeverExecutes(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()

	// Mark the engine running WITHOUT starting the processing loop: the
	// submitted request stays queued until the test starts the loop — a fully
	// deterministic queued-cancel window (no timing race with a live chain).
	e.mu.Lock()
	e.status = lifecycle.StateRunning
	e.mu.Unlock()

	var received []string
	_, _ = e.EventBus().Subscribe(event.ConsumerFunc(func(ev *event.Event) error {
		received = append(received, string(ev.Type))
		return nil
	}))

	req := &Request{
		ID:      "req-queued-cancel",
		Context: NewRequestContext("corr-q", "biz-1", "user-1"),
		Intent:  "queued work",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := e.CancelRequest(req.ID, "biz-1", "user-1"); err != nil {
		t.Fatalf("cancel queued: %v", err)
	}

	// Start the loop — the cancelled request must terminate without executing.
	go e.processRequests(ctx)
	defer e.Stop(ctx)

	result := waitForResult(t, e, req.ID)
	if result.Status != "cancelled" {
		t.Fatalf("expected cancelled, got %s (err=%v)", result.Status, result.Error)
	}
	if result.Error == nil {
		t.Fatal("expected contract ChainError on cancelled response")
	}
	if result.Error.Code != "CANCELLED" || result.Error.Category != "CANCELLATION" || result.Error.Retryable {
		t.Errorf("expected CANCELLED/CANCELLATION non-retryable, got %+v", result.Error)
	}
	if result.Error.CorrelationID != "corr-q" {
		t.Errorf("expected correlation id propagated, got %q", result.Error.CorrelationID)
	}

	for _, entry := range result.AuditTrail {
		switch entry.Step {
		case string(StepObjective), string(StepDecision), string(StepPlan),
			string(StepWorkflow), string(StepSchedule):
			t.Errorf("cancelled queued request must not reach step %q", entry.Step)
		}
	}
	if e.workflowEng.WorkflowCount() != 0 {
		t.Errorf("expected no workflow created, got %d", e.workflowEng.WorkflowCount())
	}

	// Result stored → inflight entry deregistered (no registry leak).
	waitUntil(t, "inflight deregistration", func() bool {
		e.inflightMu.RLock()
		defer e.inflightMu.RUnlock()
		_, ok := e.inflight[req.ID]
		return !ok
	})

	if _, err := e.EventBus().Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	wantPresent := []string{"chain.started", "chain.validate.passed", "chain.cancelled"}
	wantAbsent := []string{"chain.failed", "chain.completed", "chain.objective.created", "chain.executor.completed"}
	got := make(map[string]bool, len(received))
	for _, typ := range received {
		got[typ] = true
	}
	for _, typ := range wantPresent {
		if !got[typ] {
			t.Errorf("expected event %s, got %v", typ, received)
		}
	}
	for _, typ := range wantAbsent {
		if got[typ] {
			t.Errorf("event %s must not fire for a cancelled queued request", typ)
		}
	}
}

// TEST-E005-CORE-05: executing cancel — end to end: executor task cancelled,
// response carries the contract ChainError, task.cancelled + chain.cancelled
// events fire, the provider observed context cancellation (decision 9), and
// no failure is recorded (circuit breaker/recovery untouched).
func TestCancelExecutingRequestCancelsTask(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	p := registerBlockingProvider(t, e)
	// LIFO: release a still-blocked provider before Stop waits on it.
	defer e.Stop(ctx)
	defer close(p.release)

	var received []string
	_, _ = e.EventBus().Subscribe(event.ConsumerFunc(func(ev *event.Event) error {
		received = append(received, string(ev.Type))
		return nil
	}))

	req := &Request{
		ID:      "req-exec-cancel",
		Context: NewRequestContext("corr-e", "biz-1", "user-1"),
		Intent:  "long running work",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	waitSignal(t, p.started, "provider invocation")

	if err := e.CancelRequest(req.ID, "biz-1", "user-1"); err != nil {
		t.Fatalf("cancel executing: %v", err)
	}

	result := waitForResult(t, e, req.ID)
	if result.Status != "cancelled" {
		t.Fatalf("expected cancelled, got %s (err=%v)", result.Status, result.Error)
	}
	if result.Error == nil || result.Error.Code != "CANCELLED" ||
		result.Error.Category != "CANCELLATION" || result.Error.Retryable {
		t.Fatalf("expected CANCELLED/CANCELLATION non-retryable, got %+v", result.Error)
	}
	if result.Outcome == nil || result.Outcome.Metrics["executor_status"] != "cancelled" {
		t.Errorf("expected executor_status cancelled in metrics, got %+v", result.Outcome)
	}

	if !p.cancelled() {
		t.Error("provider must observe context cancellation (ctx threading)")
	}
	if got := e.TaskExecutor().CancelledCount(); got != 1 {
		t.Errorf("expected CancelledCount 1, got %d", got)
	}
	if state := e.CircuitBreaker().State(); state != "closed" {
		t.Errorf("cancellation must not record a circuit-breaker failure, state=%s", state)
	}
	if got := e.RecoveryManager().RecordCount(); got != 0 {
		t.Errorf("cancellation must not record a recovery failure, got %d", got)
	}

	waitUntil(t, "inflight deregistration", func() bool {
		e.inflightMu.RLock()
		defer e.inflightMu.RUnlock()
		_, ok := e.inflight[req.ID]
		return !ok
	})

	if _, err := e.EventBus().Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	got := make(map[string]bool, len(received))
	for _, typ := range received {
		got[typ] = true
	}
	for _, typ := range []string{"task.cancelled", "chain.cancelled"} {
		if !got[typ] {
			t.Errorf("expected event %s, got %v", typ, received)
		}
	}
	for _, typ := range []string{"chain.failed", "executor.failed", "executor.completed"} {
		if got[typ] {
			t.Errorf("event %s must not fire for a cancelled execution", typ)
		}
	}
}

// uncooperativeProvider blocks until the test releases it and IGNORES context
// cancellation — the shape of a provider/tool that does not honour ctx promptly.
// It gives cancellation tests a deterministic window where the chain's wait
// context expires while an explicit user cancellation is already on record.
type uncooperativeProvider struct {
	started chan struct{}
	release chan struct{}
}

func registerUncooperativeProvider(t *testing.T, e *Engine) *uncooperativeProvider {
	t.Helper()
	p := &uncooperativeProvider{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	if err := e.ModelRegistry().RegisterModel(&modelrouter.ModelDefinition{
		ID:         "uncoop-model",
		ProviderID: "uncooperative-provider",
		Capabilities: []modelrouter.ModelCapability{
			modelrouter.CapabilityReasoning,
			modelrouter.CapabilityToolCalling,
		},
		Runtime: modelrouter.RuntimeLocal,
		Status:  modelrouter.ModelStatusActive,
	}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	e.ModelRouter().RegisterProvider(uncooperativeRouterShim{p})
	return p
}

// uncooperativeRouterShim adapts the blocking test double to the provider
// interface without pulling the modelrouter provider types into the test.
type uncooperativeRouterShim struct{ p *uncooperativeProvider }

func (s uncooperativeRouterShim) Identify() string              { return "uncooperative-provider" }
func (s uncooperativeRouterShim) HealthCheck() error            { return nil }
func (s uncooperativeRouterShim) ListModels() ([]string, error) { return nil, nil }
func (s uncooperativeRouterShim) Invoke(ctx context.Context, req *modelrouter.GenerateRequest) (*modelrouter.GenerateResponse, error) {
	select {
	case s.p.started <- struct{}{}:
	default:
	}
	<-s.p.release // deliberately ignores ctx — see uncooperativeProvider docs
	return &modelrouter.GenerateResponse{
		RequestID:    req.RequestID,
		ModelID:      req.ModelID,
		Content:      "released",
		FinishReason: "stop",
	}, nil
}

// TEST-C5-01 (C-5): a user cancellation that races an executor error must
// still be reported as cancelled, never as a failure. The executor's own
// wait-expiry path already applies "first cause wins" (an explicit user
// cancellation is never downgraded); the chain must do the same before it
// records a circuit-breaker failure and a recovery record for the request.
//
// Deterministic repro: the provider ignores ctx, so cancelling cannot end the
// handler; the request's Deadline expires the chain's wait context and
// WaitOutcome returns an error while cancelRequested is already true.
func TestCancelRacingExecutorFailureReportsCancelled(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	p := registerUncooperativeProvider(t, e)
	defer e.Stop(ctx)
	defer close(p.release)

	deadline := time.Now().Add(400 * time.Millisecond)
	req := &Request{
		ID:       "req-cancel-race",
		Context:  NewRequestContext("corr-c5", "biz-1", "user-1"),
		Intent:   "work that outlives its deadline",
		Deadline: &deadline,
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	waitSignal(t, p.started, "uncooperative provider invocation")

	if err := e.CancelRequest(req.ID, "biz-1", "user-1"); err != nil {
		t.Fatalf("cancel executing: %v", err)
	}

	result := waitForResult(t, e, req.ID)
	if result.Status != "cancelled" {
		t.Fatalf("expected cancelled (user cancel wins), got %s (err=%v)", result.Status, result.Error)
	}
	if result.Error == nil || result.Error.Code != "CANCELLED" ||
		result.Error.Category != "CANCELLATION" || result.Error.Retryable {
		t.Fatalf("expected CANCELLED/CANCELLATION non-retryable, got %+v", result.Error)
	}
	if state := e.CircuitBreaker().State(); state != "closed" {
		t.Errorf("cancellation must not record a circuit-breaker failure, state=%s", state)
	}
	if got := e.RecoveryManager().RecordCount(); got != 0 {
		t.Errorf("cancellation must not record a recovery failure, got %d", got)
	}
}

// TEST-E005-CORE-06: completed request → ErrAlreadyCompleted with the terminal
// status (gateway 409 CONFLICT).
func TestCancelRequestCompletedConflict(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	registerSimulatedProvider(t, e)
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-done",
		Context: NewRequestContext("corr-done", "biz-1", "user-1"),
		Intent:  "already done",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	waitForResult(t, e, req.ID)

	err := e.CancelRequest(req.ID, "biz-1", "user-1")
	if !errors.Is(err, ErrAlreadyCompleted) {
		t.Fatalf("expected ErrAlreadyCompleted, got %v", err)
	}
	var terminal *TerminalStateError
	if !errors.As(err, &terminal) || terminal.Status != "completed" {
		t.Errorf("expected terminal status completed, got %#v", terminal)
	}
}

// TEST-E005-CORE-07: repeat cancellation is idempotent — flagged while queued
// (twice) and again against the terminal cancelled result: all return nil.
func TestCancelRequestCancelledIdempotent(t *testing.T) {
	e, _ := NewEngine(nil)

	// (a) Terminal cancelled result → nil.
	e.resultsMu.Lock()
	e.results["req-already-cancelled"] = &Response{
		RequestID:  "req-already-cancelled",
		BusinessID: "biz-1",
		Status:     "cancelled",
	}
	e.resultsMu.Unlock()
	if err := e.CancelRequest("req-already-cancelled", "biz-1", "user-1"); err != nil {
		t.Fatalf("cancel against terminal cancelled result: %v", err)
	}

	// (b) Queued request cancelled twice before it ever executes → nil both times.
	e.mu.Lock()
	e.status = lifecycle.StateRunning
	e.mu.Unlock()
	req := &Request{
		ID:      "req-queued-twice",
		Context: NewRequestContext("corr-qt", "biz-1", "user-1"),
		Intent:  "queued twice",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := e.CancelRequest(req.ID, "biz-1", "user-1"); err != nil {
		t.Fatalf("first queued cancel: %v", err)
	}
	if err := e.CancelRequest(req.ID, "biz-1", "user-1"); err != nil {
		t.Fatalf("second queued cancel: %v", err)
	}
	e.inflightMu.RLock()
	inf := e.inflight[req.ID]
	e.inflightMu.RUnlock()
	if inf == nil || !inf.cancelRequested || inf.cancelActor != "user-1" {
		t.Errorf("expected a single flagged inflight entry attributed to user-1, got %+v", inf)
	}
}

// TEST-E005-CORE-08: pending_approval is terminal → 409-class conflict.
// Category C 5d must revisit approval-state cancellation when pending_approval
// becomes a first-class response status.
func TestCancelPendingApprovalResultConflict(t *testing.T) {
	e, _ := NewEngine(nil)

	e.resultsMu.Lock()
	e.results["req-approval"] = &Response{
		RequestID:  "req-approval",
		BusinessID: "biz-1",
		Status:     "pending_approval",
	}
	e.resultsMu.Unlock()

	err := e.CancelRequest("req-approval", "biz-1", "user-1")
	if !errors.Is(err, ErrAlreadyCompleted) {
		t.Fatalf("expected ErrAlreadyCompleted for pending_approval, got %v", err)
	}
	var terminal *TerminalStateError
	if !errors.As(err, &terminal) || terminal.Status != "pending_approval" {
		t.Errorf("expected terminal status pending_approval, got %#v", terminal)
	}
}

// TEST-E005-CORE-09: admission send failure (shutdown during enqueue) drops
// the inflight registration — no leak, and the request is subsequently 404.
func TestCancelRequestSendFailureDeregisters(t *testing.T) {
	e, _ := NewEngine(nil)

	e.mu.Lock()
	e.status = lifecycle.StateRunning
	e.mu.Unlock()

	// Fill the processing channel directly (no admission bookkeeping), so the
	// channel — not the backpressure gate — is what blocks the next send.
	// Capacity is 100; no loop is running, so nothing dequeues.
	for i := 0; i < 100; i++ {
		e.requests <- &Request{ID: fmt.Sprintf("fill-%d", i)}
	}

	// Close the shutdown channel exactly as Stop would.
	e.shutdownOnce.Do(func() { close(e.shutdownCh) })

	req := &Request{
		ID:      "req-send-fail",
		Context: NewRequestContext("corr-f", "biz-1", "user-1"),
		Intent:  "never enqueued",
	}
	err := e.SubmitRequest(req)
	if err == nil || !strings.Contains(err.Error(), "shutting down") {
		t.Fatalf("expected shutdown error, got %v", err)
	}

	e.inflightMu.RLock()
	_, ok := e.inflight[req.ID]
	e.inflightMu.RUnlock()
	if ok {
		t.Fatal("inflight entry must be deregistered when the channel send fails")
	}
	if cancelErr := e.CancelRequest(req.ID, "biz-1", "user-1"); !errors.Is(cancelErr, ErrRequestNotFound) {
		t.Fatalf("expected ErrRequestNotFound after send failure, got %v", cancelErr)
	}
}

// TEST-E005-CORE-10: inflight entries are deregistered after a normal terminal
// result — the registry does not leak on the success path.
func TestInflightDeregisteredAfterNormalResult(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-normal-dereg",
		Context: NewRequestContext("corr-nd", "biz-1", "user-1"),
		Intent:  "normal completion",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	waitForResult(t, e, req.ID)

	waitUntil(t, "inflight deregistration after normal result", func() bool {
		e.inflightMu.RLock()
		defer e.inflightMu.RUnlock()
		_, ok := e.inflight[req.ID]
		return !ok
	})
}

// TEST-E005-CORE-11: cancelling an executing request also cancels the workflow
// bookkeeping status (decision H — executor context remains authoritative).
func TestCancelExecutingCancelsWorkflowBookkeeping(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	p := registerBlockingProvider(t, e)
	defer e.Stop(ctx)
	defer close(p.release)

	req := &Request{
		ID:      "req-wf-cancel",
		Context: NewRequestContext("corr-wf", "biz-1", "user-1"),
		Intent:  "workflow bookkeeping",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	waitSignal(t, p.started, "provider invocation")

	// Wait until chainExecute registered the executor task (wf.ID).
	waitUntil(t, "task registration", func() bool {
		e.inflightMu.RLock()
		defer e.inflightMu.RUnlock()
		inf := e.inflight[req.ID]
		return inf != nil && inf.taskID != ""
	})
	e.inflightMu.RLock()
	taskID := e.inflight[req.ID].taskID
	e.inflightMu.RUnlock()

	if err := e.CancelRequest(req.ID, "biz-1", "user-1"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	result := waitForResult(t, e, req.ID)
	if result.Status != "cancelled" {
		t.Fatalf("expected cancelled, got %s", result.Status)
	}

	wf, ok := e.workflowEng.GetWorkflow(taskID)
	if !ok {
		t.Fatalf("workflow %s not found", taskID)
	}
	if wf.Status != workflow.WorkflowStatusCancelled {
		t.Errorf("expected workflow status cancelled, got %s", wf.Status)
	}
}

// TEST-E005-CORE-12: cancellation racing completion keeps its invariants —
// CancelRequest returns nil or ErrAlreadyCompleted (never not-found, never an
// unexpected error) and the request always terminates completed or cancelled.
func TestCancelRaceWithCompletionInvariants(t *testing.T) {
	now := time.Now()
	e, _ := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("req-race-%d", i)
		req := &Request{
			ID:      id,
			Context: NewRequestContext(fmt.Sprintf("corr-race-%d", i), "biz-1", "user-1"),
			Intent:  "race cancellation",
		}
		if err := e.SubmitRequest(req); err != nil {
			t.Fatalf("%s: submit: %v", id, err)
		}

		err := e.CancelRequest(id, "biz-1", "user-1")
		switch {
		case err == nil:
			// Accepted (queued flag or executor cancellation).
		case errors.Is(err, ErrAlreadyCompleted):
			var terminal *TerminalStateError
			if !errors.As(err, &terminal) {
				t.Fatalf("%s: expected TerminalStateError, got %v", id, err)
			}
		default:
			t.Fatalf("%s: unexpected cancel error: %v", id, err)
		}

		result := waitForResult(t, e, id)
		if result.Status != "completed" && result.Status != "cancelled" {
			t.Fatalf("%s: terminal status must be completed or cancelled, got %s (err=%v)",
				id, result.Status, result.Error)
		}
	}
}

// TEST-CORE-049: P1 approval wiring — chain-gate REQUIRE_APPROVAL opens the
// approval loop end to end: approval record + approval.requested event +
// error.details.approval_id (CORE_INTERFACE_CONTRACTS §3 envelope), the
// contract-shaped pending record (SCHEMA_WORK §6.2), self-approval refused
// (SCHEMA_WORK §6.1), then approve → approval.approved + resume
// re-execution that satisfies both governance gates via the approved record
// → completed (Response.Status stays failed/APPROVAL_REQUIRED while gated —
// D2 pin unchanged).
func TestApprovalWiringChainGateResume(t *testing.T) {
	now := time.Now()
	e, err := NewEngine(nil, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	registerSimulatedProvider(t, e)
	// All-action REQUIRE_APPROVAL: the chain gate gates the first run; on
	// the resume run BOTH the chain gate and the executor gate must be
	// satisfied by the approved record (ApprovalState pass-through).
	e.govEngine.SetPolicies([]*governance.Policy{
		{
			PolicyID:   "require-approval",
			Name:       "Require Approval",
			Status:     governance.PolicyStatusActive,
			Effect:     governance.REQUIRE_APPROVAL,
			Subject:    governance.Subject{SubjectType: "all"},
			Action:     governance.Action{ActionType: "custom"},
			Resource:   governance.Resource{ResourceType: "all"},
			Precedence: 0,
			ApprovalConfig: &governance.ApprovalConfig{
				TimeoutSeconds:         3600,
				AutoDenyOnTimeout:      true,
				SelfApprovalProhibited: true,
			},
		},
	})

	ctx := context.Background()
	var received []string
	_, _ = e.EventBus().Subscribe(event.ConsumerFunc(func(ev *event.Event) error {
		received = append(received, string(ev.Type))
		return nil
	}))
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-apr-resume",
		Context: NewRequestContext("corr-apr-resume", "biz-1", "user-1"),
		Intent:  "gated work",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}

	// Gated result: D2 envelope unchanged, approval_id surfaced in details.
	result := waitForResult(t, e, req.ID)
	if result.Status != "failed" {
		t.Fatalf("expected failed while gated, got %q", result.Status)
	}
	if result.Error == nil || result.Error.Category != "APPROVAL_REQUIRED" || result.Error.Retryable {
		t.Fatalf("expected APPROVAL_REQUIRED/Retryable=false, got %+v", result.Error)
	}
	approvalID := result.Error.Details["approval_id"]
	if approvalID == "" {
		t.Fatal("expected error.details.approval_id on APPROVAL_REQUIRED")
	}

	// Contract-shaped pending record (SCHEMA_WORK §6.2/§6.3).
	records := e.ListApprovals("biz-1")
	if len(records) != 1 {
		t.Fatalf("expected 1 pending approval, got %d", len(records))
	}
	rec := records[0]
	if rec.EntityID != approvalID || rec.Status != "PENDING" {
		t.Errorf("unexpected record identity/status: %+v", rec)
	}
	if rec.RequesterID != "user-1" || rec.BusinessID != "biz-1" {
		t.Errorf("unexpected requester/business: %+v", rec)
	}
	if rec.EntityType != "approval" || rec.SchemaVersion != "1.0.0" {
		t.Errorf("expected contract entity_type/schema_version, got %+v", rec)
	}
	if rec.AuditRef != req.ID || rec.CorrelationID != "corr-apr-resume" {
		t.Errorf("expected audit_ref/correlation_id links, got %+v", rec)
	}
	if rec.ExpiresAt == nil {
		t.Error("expected expires_at from the policy approval timeout (INV-16)")
	}
	if rec.PolicyRef != "require-approval" || rec.Scope != "core" {
		t.Errorf("expected policy_ref/scope wired, got %+v", rec)
	}

	// SCHEMA_WORK §6.1: the requester must not decide their own request.
	if err := e.ApproveRequest(approvalID, "biz-1", "user-1", "self"); !errors.Is(err, ErrSelfApprovalProhibited) {
		t.Fatalf("expected ErrSelfApprovalProhibited, got %v", err)
	}
	// Still pending, stored result untouched by the refused decision.
	if got := e.ListApprovals("biz-1"); len(got) != 1 {
		t.Fatalf("self-approval refusal must leave the record pending, got %d", len(got))
	}
	if r, _ := e.GetResult(req.ID); r == nil || r.Status != "failed" {
		t.Fatal("refused decision must not touch the stored result")
	}

	// Approve by another identity → resume re-execution.
	if err := e.ApproveRequest(approvalID, "biz-1", "user-2", "approved for test"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	// ApproveRequest drops the stale result before re-admission, so
	// waitForResult here observes the resume's own terminal result.
	resumed := waitForResult(t, e, req.ID)
	if resumed.Status != "completed" {
		t.Fatalf("resume must re-execute to completed, got %q (err=%v)", resumed.Status, resumed.Error)
	}
	if resumed.Error != nil {
		t.Fatalf("resumed result must carry no error, got %+v", resumed.Error)
	}
	// Approved record is consumed: not listed, index cleaned after the run.
	if got := e.ListApprovals("biz-1"); len(got) != 0 {
		t.Errorf("expected no pending approvals after resume, got %d", len(got))
	}

	if _, err := e.EventBus().Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	got := make(map[string]bool, len(received))
	for _, typ := range received {
		got[typ] = true
	}
	for _, typ := range []string{
		"approval.requested", "approval.approved",
		"chain.failed", "chain.completed",
		// Resume proof at the executor gate: the first run never reached
		// the executor (chain gate), so received/assigned firing now means
		// the approved ApprovalState satisfied the executor's own gate.
		"executor.received", "executor.assigned",
	} {
		if !got[typ] {
			t.Errorf("expected event %s, got %v", typ, received)
		}
	}
	for _, typ := range []string{"approval.denied", "approval.expired"} {
		if got[typ] {
			t.Errorf("event %s must not fire on the approve path, got %v", typ, received)
		}
	}
}

// TEST-CORE-050: deny closes the approval loop without resume — the stored
// response stays failed/APPROVAL_REQUIRED (D2 pin), approval.denied is
// emitted, the record is removed, and any later decision is 404-class.
func TestApprovalDenyKeepsApprovalRequiredResult(t *testing.T) {
	now := time.Now()
	e, err := NewEngine(nil, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	e.govEngine.SetPolicies([]*governance.Policy{
		{
			PolicyID:   "require-approval",
			Name:       "Require Approval",
			Status:     governance.PolicyStatusActive,
			Effect:     governance.REQUIRE_APPROVAL,
			Subject:    governance.Subject{SubjectType: "all"},
			Action:     governance.Action{ActionType: "custom"},
			Resource:   governance.Resource{ResourceType: "all"},
			Precedence: 0,
			ApprovalConfig: &governance.ApprovalConfig{
				TimeoutSeconds:    3600,
				AutoDenyOnTimeout: true,
			},
		},
	})

	ctx := context.Background()
	var received []string
	_, _ = e.EventBus().Subscribe(event.ConsumerFunc(func(ev *event.Event) error {
		received = append(received, string(ev.Type))
		return nil
	}))
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-apr-deny",
		Context: NewRequestContext("corr-apr-deny", "biz-1", "user-1"),
		Intent:  "gated work",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	result := waitForResult(t, e, req.ID)
	if result.Error == nil || result.Error.Details["approval_id"] == "" {
		t.Fatalf("expected approval_id in details, got %+v", result.Error)
	}
	approvalID := result.Error.Details["approval_id"]

	if err := e.DenyRequest(approvalID, "biz-1", "user-2", "not safe"); err != nil {
		t.Fatalf("deny: %v", err)
	}

	// No resume: stored result remains the D2 gate failure.
	stored, ok := e.GetResult(req.ID)
	if !ok || stored.Status != "failed" {
		t.Fatalf("deny must not change the stored result, got ok=%v %+v", ok, stored)
	}
	if stored.Error == nil || stored.Error.Category != "APPROVAL_REQUIRED" {
		t.Fatalf("expected APPROVAL_REQUIRED to remain, got %+v", stored.Error)
	}
	// Record resolved and removed from the index.
	if got := e.ListApprovals("biz-1"); len(got) != 0 {
		t.Errorf("expected no pending approvals after deny, got %d", len(got))
	}
	// Second decision → not found (already resolved).
	if err := e.DenyRequest(approvalID, "biz-1", "user-2", "again"); !errors.Is(err, ErrApprovalNotFound) {
		t.Errorf("expected ErrApprovalNotFound on repeat deny, got %v", err)
	}
	if err := e.ApproveRequest(approvalID, "biz-1", "user-2", "reverse"); !errors.Is(err, ErrApprovalNotFound) {
		t.Errorf("expected ErrApprovalNotFound on approve-after-deny, got %v", err)
	}

	if _, err := e.EventBus().Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	got := make(map[string]bool)
	for _, typ := range received {
		got[typ] = true
	}
	if !got["approval.denied"] {
		t.Errorf("expected approval.denied, got %v", received)
	}
	if got["approval.approved"] || got["chain.completed"] {
		t.Errorf("deny must not approve/resume: %v", received)
	}
}

// TEST-CORE-051: executor-gate REQUIRE_APPROVAL (D2 chain-step agent) opens
// the approval loop from Step-12 — the record is created with the executor's
// exact governance inputs (Scope workflow / Action execute_task, single
// source executor.ActionExecuteTask/ResourceWorkflow), and approve resumes
// through the executor-gate ApprovalState pass-through to a real execution.
func TestApprovalWiringExecutorGateResume(t *testing.T) {
	now := time.Now()
	e, err := NewEngine(nil, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	// Working (non-blocking) provider so the resumed handler actually
	// completes: model-less runs record executor_status=failed at the
	// executor layer — pre-existing routing behavior, presence-only pinned
	// by TEST-CORE-041. Release is closed up front → Invoke returns at once.
	p := registerBlockingProvider(t, e)
	close(p.release)
	// Default-allow keeps the chain gate passing; REQUIRE_APPROVAL is
	// scoped to the executor's action (TEST-CORE-047 pattern).
	e.govEngine.SetPolicies([]*governance.Policy{
		{
			PolicyID:   "default-allow",
			Name:       "Default Allow",
			Status:     governance.PolicyStatusActive,
			Effect:     governance.ALLOW,
			Subject:    governance.Subject{SubjectType: "all"},
			Action:     governance.Action{ActionType: "custom"},
			Resource:   governance.Resource{ResourceType: "all"},
			Precedence: 0,
		},
		{
			PolicyID:   "require-approval-exec",
			Name:       "Require Approval On Execute Task",
			Status:     governance.PolicyStatusActive,
			Effect:     governance.REQUIRE_APPROVAL,
			Subject:    governance.Subject{SubjectType: "all"},
			Action:     governance.Action{ActionType: "execute_task", ActionIDs: []string{"execute_task"}},
			Resource:   governance.Resource{ResourceType: "all"},
			Precedence: 0,
			ApprovalConfig: &governance.ApprovalConfig{
				TimeoutSeconds:         3600,
				AutoDenyOnTimeout:      true,
				SelfApprovalProhibited: true,
			},
		},
	})

	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-apr-exec",
		Context: NewRequestContext("corr-apr-exec", "biz-1", "user-1"),
		Intent:  "executor gated work",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}

	result := waitForResult(t, e, req.ID)
	if result.Status != "failed" || result.Error == nil ||
		result.Error.Category != "APPROVAL_REQUIRED" {
		t.Fatalf("expected failed/APPROVAL_REQUIRED, got %+v", result)
	}
	if result.Error.ChainStep != string(StepAgent) {
		t.Errorf("expected executor gate (agent), got chain step %q", result.Error.ChainStep)
	}
	approvalID := result.Error.Details["approval_id"]
	if approvalID == "" {
		t.Fatal("expected Step-12 to surface approval_id")
	}

	// Record created from the executor's governance inputs.
	records := e.ListApprovals("biz-1")
	if len(records) != 1 {
		t.Fatalf("expected 1 pending approval, got %d", len(records))
	}
	rec := records[0]
	if rec.Scope != "workflow" || rec.RequestedAction != "execute_task" {
		t.Errorf("record must carry executor governance inputs, got scope=%q action=%q",
			rec.Scope, rec.RequestedAction)
	}
	if rec.PolicyRef != "require-approval-exec" {
		t.Errorf("expected policy_ref from the executor-gate decision, got %q", rec.PolicyRef)
	}
	// N3: the executor discards its Decision, so core re-evaluates to build
	// the record. The re-eval must reproduce the matched policy's
	// ApprovalConfig — losing it would silently create a zero-config
	// approval (no timeout, no self-approval prohibition) and weaken the
	// policy that produced REQUIRE_APPROVAL.
	if rec.ExpiresAt == nil {
		t.Error("expected expires_at from the policy approval timeout (INV-16)")
	}
	if !rec.SelfApprovalProhibited {
		t.Error("expected self_approval_prohibited from the policy approval config")
	}

	// The reproduced config is enforced, not merely present: the requester
	// cannot approve their own action.
	if err := e.ApproveRequest(approvalID, "biz-1", "user-1", "self"); !errors.Is(err, ErrSelfApprovalProhibited) {
		t.Fatalf("self-approval must be refused by the reproduced config, got %v", err)
	}

	// Approve → resume must pass the executor gate and actually execute.
	if err := e.ApproveRequest(approvalID, "biz-1", "user-2", "approved"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	resumed := waitForResult(t, e, req.ID)
	if resumed.Status != "completed" {
		t.Fatalf("resume must complete, got %q (err=%v)", resumed.Status, resumed.Error)
	}
	if resumed.Outcome == nil || resumed.Outcome.Metrics["executor_status"] != "completed" {
		t.Errorf("expected executor to run the handler on resume, got %+v", resumed.Outcome)
	}
	if got := e.ListApprovals("biz-1"); len(got) != 0 {
		t.Errorf("expected approval index cleaned after resume, got %d", len(got))
	}
}

// TEST-CORE-052: INV-16 — silence ≠ approval: a timed-out approval auto-denies
// (policy config), emits approval.expired, disappears from the list, and can
// no longer be decided. Clock is advanced only after the engine stops, so no
// core-path reader races the mutation.
func TestApprovalTimeoutExpires(t *testing.T) {
	now := time.Now()
	e, err := NewEngine(nil, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	e.govEngine.SetPolicies([]*governance.Policy{
		{
			PolicyID:   "require-approval",
			Name:       "Require Approval",
			Status:     governance.PolicyStatusActive,
			Effect:     governance.REQUIRE_APPROVAL,
			Subject:    governance.Subject{SubjectType: "all"},
			Action:     governance.Action{ActionType: "custom"},
			Resource:   governance.Resource{ResourceType: "all"},
			Precedence: 0,
			ApprovalConfig: &governance.ApprovalConfig{
				TimeoutSeconds:    60,
				AutoDenyOnTimeout: true,
			},
		},
	})

	ctx := context.Background()
	var received []string
	_, _ = e.EventBus().Subscribe(event.ConsumerFunc(func(ev *event.Event) error {
		received = append(received, string(ev.Type))
		return nil
	}))
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-apr-timeout",
		Context: NewRequestContext("corr-apr-timeout", "biz-1", "user-1"),
		Intent:  "gated work",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	result := waitForResult(t, e, req.ID)
	approvalID := result.Error.Details["approval_id"]
	if approvalID == "" {
		t.Fatal("expected approval_id")
	}

	// Stop the engine so no core path can read the clock while we advance
	// it (Stop waits for the processing loop — happens-before edge).
	if err := e.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	now = now.Add(61 * time.Second)

	// Lazy sweep on the next approval touch (ListApprovals).
	if got := e.ListApprovals("biz-1"); len(got) != 0 {
		t.Fatalf("timed-out approval must be swept from the list, got %d", len(got))
	}
	if err := e.ApproveRequest(approvalID, "biz-1", "user-2", "late"); !errors.Is(err, ErrApprovalNotFound) {
		t.Errorf("expired approval must not be approvable, got %v", err)
	}

	if _, err := e.EventBus().Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	got := make(map[string]bool)
	for _, typ := range received {
		got[typ] = true
	}
	if !got["approval.expired"] {
		t.Errorf("expected approval.expired, got %v", received)
	}
	if got["approval.approved"] {
		t.Errorf("expired approval must not be approved: %v", received)
	}
}

// TEST-CORE-053: approval decisions require a live processing loop — when
// the engine is stopped, approve is refused BEFORE mutating the approval
// state (no decided record without a resume run); the record stays pending
// and the stored result is untouched.
func TestApprovalApproveRequiresRunningEngine(t *testing.T) {
	now := time.Now()
	e, err := NewEngine(nil, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	e.govEngine.SetPolicies([]*governance.Policy{
		{
			PolicyID:   "require-approval",
			Name:       "Require Approval",
			Status:     governance.PolicyStatusActive,
			Effect:     governance.REQUIRE_APPROVAL,
			Subject:    governance.Subject{SubjectType: "all"},
			Action:     governance.Action{ActionType: "custom"},
			Resource:   governance.Resource{ResourceType: "all"},
			Precedence: 0,
		},
	})

	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-apr-stopped",
		Context: NewRequestContext("corr-apr-stopped", "biz-1", "user-1"),
		Intent:  "gated work",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	result := waitForResult(t, e, req.ID)
	approvalID := result.Error.Details["approval_id"]
	if approvalID == "" {
		t.Fatal("expected approval_id")
	}

	if err := e.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}

	if err := e.ApproveRequest(approvalID, "biz-1", "user-2", "while stopped"); !errors.Is(err, ErrApprovalNotResumable) {
		t.Fatalf("expected ErrApprovalNotResumable, got %v", err)
	}
	// Refused before mutation: still pending, result untouched.
	if got := e.ListApprovals("biz-1"); len(got) != 1 {
		t.Errorf("record must stay pending after refused approve, got %d", len(got))
	}
	if r, _ := e.GetResult(req.ID); r == nil || r.Status != "failed" {
		t.Error("refused approve must not touch the stored result")
	}
}

// TEST-CORE-054 (D3): chain-gate ESCALATE → failed / ESALATION_REQUIRED /
// POLICY_DENIED with error.details.escalation_ref and the
// governance.escalated handoff event (CTR-GOV-002 minimal artifact).
func TestEscalationChainGateEnvelope(t *testing.T) {
	now := time.Now()
	e, err := NewEngine(nil, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	e.govEngine.SetPolicies([]*governance.Policy{
		{
			PolicyID:   "escalate-all",
			Name:       "Escalate All",
			Status:     governance.PolicyStatusActive,
			Effect:     governance.ESCALATE,
			Subject:    governance.Subject{SubjectType: "all"},
			Action:     governance.Action{ActionType: "custom"},
			Resource:   governance.Resource{ResourceType: "all"},
			Precedence: 0,
		},
	})

	ctx := context.Background()
	var received []string
	_, _ = e.EventBus().Subscribe(event.ConsumerFunc(func(ev *event.Event) error {
		received = append(received, string(ev.Type))
		return nil
	}))
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-esc-chain",
		Context: NewRequestContext("corr-esc-chain", "biz-1", "user-1"),
		Intent:  "work that must escalate",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	result := waitForResult(t, e, req.ID)

	if result.Status != "failed" {
		t.Errorf("expected failed, got %s", result.Status)
	}
	if result.Error == nil {
		t.Fatal("expected error envelope")
	}
	if result.Error.Code != "ESCALATION_REQUIRED" {
		t.Errorf("expected code ESALATION_REQUIRED, got %s", result.Error.Code)
	}
	if result.Error.Category != "POLICY_DENIED" {
		t.Errorf("expected category POLICY_DENIED (CORE §3), got %s", result.Error.Category)
	}
	if result.Error.Retryable {
		t.Error("expected Retryable=false — escalation needs authority, not retry")
	}
	if ref := result.Error.Details["escalation_ref"]; ref == "" {
		t.Errorf("expected error.details.escalation_ref, got %v", result.Error.Details)
	}
	if !strings.Contains(result.Error.Message, "outcome=ESCALATE") {
		t.Errorf("message should carry the outcome, got %q", result.Error.Message)
	}
	// D1 distinction: a pure DENY uses code POLICY_DENIED — the code must
	// never be POLICY_DENIED on this escalation path.
	if result.Error.Code == "POLICY_DENIED" {
		t.Error("ESCALATE must not share D1's code")
	}

	if _, err := e.EventBus().Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	var sawEscalated bool
	for _, typ := range received {
		if typ == "governance.escalated" {
			sawEscalated = true
		}
		if typ == "approval.requested" {
			t.Error("escalation must not open the approval loop")
		}
	}
	if !sawEscalated {
		t.Errorf("expected governance.escalated event, got %v", received)
	}
}

// TEST-CORE-055 (D3): executor-gate ESCALATE surfaced as failed /
// ESCALATION_REQUIRED — pins the fix of the pre-D3 fallthrough where the
// pre-dispatch block was reported as status "completed".
func TestEscalationExecutorGateEnvelope(t *testing.T) {
	now := time.Now()
	e, err := NewEngine(nil, WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	// Default-allow keeps the chain gate passing; ESCALATE is scoped to the
	// executor's action (TEST-CORE-051 pattern).
	e.govEngine.SetPolicies([]*governance.Policy{
		{
			PolicyID:   "default-allow",
			Name:       "Default Allow",
			Status:     governance.PolicyStatusActive,
			Effect:     governance.ALLOW,
			Subject:    governance.Subject{SubjectType: "all"},
			Action:     governance.Action{ActionType: "custom"},
			Resource:   governance.Resource{ResourceType: "all"},
			Precedence: 0,
		},
		{
			PolicyID:   "escalate-exec",
			Name:       "Escalate On Execute Task",
			Status:     governance.PolicyStatusActive,
			Effect:     governance.ESCALATE,
			Subject:    governance.Subject{SubjectType: "all"},
			Action:     governance.Action{ActionType: "execute_task", ActionIDs: []string{"execute_task"}},
			Resource:   governance.Resource{ResourceType: "all"},
			Precedence: 0,
		},
	})

	ctx := context.Background()
	var received []string
	_, _ = e.EventBus().Subscribe(event.ConsumerFunc(func(ev *event.Event) error {
		received = append(received, string(ev.Type))
		return nil
	}))
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-esc-exec",
		Context: NewRequestContext("corr-esc-exec", "biz-1", "user-1"),
		Intent:  "executor-gated escalation",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	result := waitForResult(t, e, req.ID)

	if result.Status != "failed" {
		t.Errorf("expected failed (pre-dispatch block must not report completed), got %s", result.Status)
	}
	if result.Error == nil {
		t.Fatal("expected error envelope")
	}
	if result.Error.Code != "ESCALATION_REQUIRED" || result.Error.Category != "POLICY_DENIED" {
		t.Errorf("expected ESALATION_REQUIRED/POLICY_DENIED, got %s/%s",
			result.Error.Code, result.Error.Category)
	}
	if result.Error.Retryable {
		t.Error("expected Retryable=false")
	}
	if result.Error.Details["escalation_ref"] == "" {
		t.Errorf("expected error.details.escalation_ref, got %v", result.Error.Details)
	}
	if result.Outcome == nil || result.Outcome.Metrics["executor_status"] != "escalated" {
		t.Errorf("expected executor_status escalated, got %+v", result.Outcome)
	}

	if _, err := e.EventBus().Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	var sawEscalated bool
	for _, typ := range received {
		if typ == "governance.escalated" {
			sawEscalated = true
		}
	}
	if !sawEscalated {
		t.Errorf("expected governance.escalated event, got %v", received)
	}
}

// activeMembership is a shared fixture helper for the admission-pipeline
// tests (A): an active member of biz-1.
func activeMembership(t *testing.T, identityID, businessID string) *identity.MembershipSet {
	t.Helper()
	members := identity.NewMembershipSet()
	if err := members.Add(identity.Membership{
		IdentityID: identityID,
		BusinessID: businessID,
		Role:       identity.RoleMember,
		Status:     identity.StatusActive,
	}); err != nil {
		t.Fatalf("add membership: %v", err)
	}
	return members
}

// TEST-CORE-056 (A): admission IDENTITY stage (RUNTIME §3.1 stage 3) —
// enforcement on, actor unknown to the membership store → failed / AUTH,
// ChainStep identity, Retryable false (RT-01 admission rejection with
// reason code).
func TestAdmissionIdentityUnknownActor(t *testing.T) {
	now := time.Now()
	members := activeMembership(t, "alice", "biz-1")
	e, err := NewEngine(nil,
		WithClock(func() time.Time { return now }),
		WithIdentity(members, true, false),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-idn-unknown",
		Context: NewRequestContext("corr-idn-unknown", "biz-1", "ghost"),
		Intent:  "work by an unknown actor",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	result := waitForResult(t, e, req.ID)

	if result.Status != "failed" {
		t.Fatalf("expected failed, got %s", result.Status)
	}
	if result.Error == nil {
		t.Fatal("expected error envelope")
	}
	if result.Error.Code != "AUTH" || result.Error.Category != "AUTH" {
		t.Errorf("expected AUTH/AUTH, got %s/%s", result.Error.Code, result.Error.Category)
	}
	if result.Error.ChainStep != string(StepIdentity) {
		t.Errorf("expected chain_step identity, got %q", result.Error.ChainStep)
	}
	if result.Error.Retryable {
		t.Error("expected Retryable=false (§3 AUTH = No)")
	}
	if !strings.Contains(result.Error.Message, "actor not found: ghost") {
		t.Errorf("message should name the actor, got %q", result.Error.Message)
	}
}

// TEST-CORE-057 (A): admission AUTHORIZATION stage (RUNTIME §3.1 stage 4) —
// known actor without active membership in the request's business → failed /
// AUTHORIZATION, ChainStep authorization.
func TestAdmissionAuthorizationForeignBusiness(t *testing.T) {
	now := time.Now()
	members := activeMembership(t, "alice", "biz-1")
	e, err := NewEngine(nil,
		WithClock(func() time.Time { return now }),
		WithIdentity(members, true, true),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-authz-foreign",
		Context: NewRequestContext("corr-authz-foreign", "biz-2", "alice"),
		Intent:  "work for a foreign business",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	result := waitForResult(t, e, req.ID)

	if result.Status != "failed" {
		t.Fatalf("expected failed, got %s", result.Status)
	}
	if result.Error == nil {
		t.Fatal("expected error envelope")
	}
	if result.Error.Code != "AUTHORIZATION" || result.Error.Category != "AUTHORIZATION" {
		t.Errorf("expected AUTHORIZATION/AUTHORIZATION, got %s/%s",
			result.Error.Code, result.Error.Category)
	}
	if result.Error.ChainStep != string(StepAuthorization) {
		t.Errorf("expected chain_step authorization, got %q", result.Error.ChainStep)
	}
	if result.Error.Retryable {
		t.Error("expected Retryable=false (§3 AUTHORIZATION = No)")
	}
	if !strings.Contains(result.Error.Message, "not an active member of business biz-2") {
		t.Errorf("message should name the business, got %q", result.Error.Message)
	}
}

// TEST-CORE-058 (A): enforcement on + missing store → fail closed
// (RT-02 default deny) at the IDENTITY stage with AUTH.
func TestAdmissionIdentityStoreMissing(t *testing.T) {
	now := time.Now()
	e, err := NewEngine(nil,
		WithClock(func() time.Time { return now }),
		WithIdentity(nil, true, true),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-idn-nostore",
		Context: NewRequestContext("corr-idn-nostore", "biz-1", "alice"),
		Intent:  "work with no identity store",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	result := waitForResult(t, e, req.ID)

	if result.Status != "failed" || result.Error == nil {
		t.Fatalf("expected failed with error, got %+v", result)
	}
	if result.Error.Code != "AUTH" || result.Error.ChainStep != string(StepIdentity) {
		t.Errorf("expected AUTH at identity, got %s@%s", result.Error.Code, result.Error.ChainStep)
	}
	if !strings.Contains(result.Error.Message, "identity store unavailable") {
		t.Errorf("expected fail-closed message, got %q", result.Error.Message)
	}
}

// TEST-CORE-059 (A): full admission pass-through — enforcement on, valid
// member → completed, and every stage leaves an auditable record with its
// decision (RT-01: no stage skipped, every admission decision auditable).
func TestAdmissionPipelinePassThrough(t *testing.T) {
	now := time.Now()
	members := activeMembership(t, "alice", "biz-1")
	e, err := NewEngine(nil,
		WithClock(func() time.Time { return now }),
		WithIdentity(members, true, true),
	)
	if err != nil {
		t.Fatal(err)
	}
	registerSimulatedProvider(t, e)
	ctx := context.Background()
	var received []string
	_, _ = e.EventBus().Subscribe(event.ConsumerFunc(func(ev *event.Event) error {
		received = append(received, string(ev.Type))
		return nil
	}))
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-admission-pass",
		Context: NewRequestContext("corr-admission-pass", "biz-1", "alice"),
		Intent:  "gated-by-identity work",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	result := waitForResult(t, e, req.ID)

	if result.Status != "completed" {
		t.Fatalf("expected completed for a valid member, got %s (err=%+v)", result.Status, result.Error)
	}
	steps := make(map[string]string, len(result.AuditTrail))
	for _, entry := range result.AuditTrail {
		steps[entry.Step] = entry.Outcome
	}
	if steps[string(StepIdentity)] != "verified actor=alice" {
		t.Errorf("expected identity audit record, got %q", steps[string(StepIdentity)])
	}
	if steps[string(StepAuthorization)] != "authorized business=biz-1" {
		t.Errorf("expected authorization audit record, got %q", steps[string(StepAuthorization)])
	}
	if !strings.HasPrefix(steps[string(StepResourceCheck)], "available active=") {
		t.Errorf("expected resource_check audit record, got %q", steps[string(StepResourceCheck)])
	}

	if _, err := e.EventBus().Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	seen := make(map[string]bool, len(received))
	for _, typ := range received {
		seen[typ] = true
	}
	for _, typ := range []string{"chain.identity.checked", "chain.authorization.checked", "chain.resource.checked"} {
		if !seen[typ] {
			t.Errorf("expected event %s, got %v", typ, received)
		}
	}
}

// TEST-CORE-060 (A): admission RESOURCE CHECK stage (RUNTIME §3.1 stage 7)
// — executor at MaxConcurrent → failed / RESOURCE_UNAVAILABLE with
// Retryable=true (RUNTIME rejection table ADM_RESOURCES: yes, queue),
// ChainStep resource_check. The slot is occupied by a direct executor
// submission so the chain's single processing loop is never blocked.
func TestAdmissionResourceCheckCapacity(t *testing.T) {
	now := time.Now()
	cfg := executor.DefaultConfig()
	cfg.MaxConcurrent = 1
	e, err := NewEngine(nil,
		WithClock(func() time.Time { return now }),
		WithExecutorConfig(cfg),
	)
	if err != nil {
		t.Fatal(err)
	}
	// Occupier parks inside the handler (release stays open until cleanup).
	p := registerBlockingProvider(t, e)

	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer e.Stop(ctx)
	// LIFO: release before Stop so teardown never waits on the parked
	// occupier (Stop would otherwise drain against an open handler).
	defer close(p.release)

	if err := e.TaskExecutor().Submit(&executor.WorkRequest{
		TaskID:        "occupier-1",
		CorrelationID: "corr-occupier-1",
		BusinessID:    "biz-1",
		ActorID:       "system",
		Intent:        "hold the only slot",
	}); err != nil {
		t.Fatalf("submit occupier: %v", err)
	}
	waitUntil(t, "occupier active", func() bool {
		active, _ := e.TaskExecutor().Capacity()
		return active >= 1
	})

	req := &Request{
		ID:      "req-res-full",
		Context: NewRequestContext("corr-res-full", "biz-1", "user-1"),
		Intent:  "work with no dispatch capacity",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	result := waitForResult(t, e, req.ID)

	if result.Status != "failed" || result.Error == nil {
		t.Fatalf("expected failed with error, got %+v", result)
	}
	if result.Error.Code != "RESOURCE_UNAVAILABLE" || result.Error.Category != "RESOURCE_UNAVAILABLE" {
		t.Errorf("expected RESOURCE_UNAVAILABLE, got %s/%s", result.Error.Code, result.Error.Category)
	}
	if !result.Error.Retryable {
		t.Error("expected Retryable=true (§3 RESOURCE_UNAVAILABLE = Yes)")
	}
	if result.Error.ChainStep != string(StepResourceCheck) {
		t.Errorf("expected chain_step resource_check, got %q", result.Error.ChainStep)
	}
}

// TEST-CORE-066 (P3 C-012): chainMemoryRead forwards the caller's objective
// scope into the memory query — retrieval excludes entries tagged with a
// different objective while untagged entries (no objective relation) still
// match (SCHEMA_MEMORY §1: retrieval is computed according to scope,
// relevance, objective, time, confidence, privacy, and policy).
func TestChainMemoryReadForwardsObjectiveScope(t *testing.T) {
	e, err := NewEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	entries := []*memory.MemoryEntry{
		{Type: memory.MemoryTypeEpisodic, BusinessID: "biz-1", ObjectiveID: "obj-keep", Content: "keep me", Provenance: memory.Provenance{Confidence: 1.0}},
		{Type: memory.MemoryTypeEpisodic, BusinessID: "biz-1", ObjectiveID: "obj-other", Content: "other objective memory", Provenance: memory.Provenance{Confidence: 1.0}},
		{Type: memory.MemoryTypeEpisodic, BusinessID: "biz-1", Content: "untagged memory", Provenance: memory.Provenance{Confidence: 1.0}},
	}
	for _, entry := range entries {
		if err := e.memoryStore.Admit(entry); err != nil {
			t.Fatalf("admit: %v", err)
		}
	}

	// Empty intent disables the keyword filter so the objective scope is the
	// only discriminating dimension under test.
	ctx := NewRequestContext("corr-066", "biz-1", "user-1").WithObjective("obj-keep", "test objective scope")
	req := &Request{ID: "req-066", Context: ctx, Intent: ""}
	got := e.chainMemoryRead(context.Background(), req)

	contents := make(map[string]bool, len(got))
	for _, entry := range got {
		contents[entry.Content] = true
	}
	if !contents["keep me"] || !contents["untagged memory"] {
		t.Errorf("expected untagged + obj-keep entries, got %v", contents)
	}
	if contents["other objective memory"] {
		t.Errorf("objective-scoped read must exclude the other-objective entry, got %v", contents)
	}
}

// TEST-CORE-067 (P3 C-024): a capacity rejection that surfaces at executor
// Submit — the race shape RUNTIME §3.2 says admission cannot rule out —
// records ChainError.Retryable=true with category RESOURCE_UNAVAILABLE,
// matching the stage-7 pre-check envelope (CORE §3 table: Yes).
// MaxConcurrent=0 makes the admission→submit divergence deterministic: the
// pre-check's `max > 0` guard passes while Submit's `0 >= 0` rejects, a
// controlled version of capacity filling between check and submit.
func TestSubmitCapacityFailureIsRetryable(t *testing.T) {
	now := time.Now()
	cfg := executor.DefaultConfig()
	cfg.MaxConcurrent = 0
	e, err := NewEngine(nil,
		WithClock(func() time.Time { return now }),
		WithExecutorConfig(cfg),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-cap-submit",
		Context: NewRequestContext("corr-cap-submit", "biz-1", "user-1"),
		Intent:  "work submitted into full capacity",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	result := waitForResult(t, e, req.ID)

	if result.Status != "failed" || result.Error == nil {
		t.Fatalf("expected failed with error, got %+v", result)
	}
	if result.Error.Code != "RESOURCE_UNAVAILABLE" || result.Error.Category != "RESOURCE_UNAVAILABLE" {
		t.Errorf("expected RESOURCE_UNAVAILABLE, got %s/%s", result.Error.Code, result.Error.Category)
	}
	if !result.Error.Retryable {
		t.Error("expected Retryable=true (§3 RESOURCE_UNAVAILABLE = Yes) — C-024")
	}
	if result.Error.ChainStep != string(StepAgent) {
		t.Errorf("expected chain_step %s, got %q", StepAgent, result.Error.ChainStep)
	}
	if result.Error.Message != "executor at capacity (0/0)" {
		t.Errorf("expected wrapped capacity message, got %q", result.Error.Message)
	}
}

// TEST-N2: an ALLOW_WITH_CONSTRAINTS decision used to be indistinguishable
// from a plain ALLOW — the chain gate treated both as "proceed" and
// decision.Constraints was never read again, so neither the caller nor the
// policy author could tell that constraints applied. CORE_INTERFACE_CONTRACTS
// §4.2 defines the governance decision output as `constraints: list[string]`
// when the outcome is ALLOW_WITH_CONSTRAINTS, and SCHEMA_GOVERNANCE records
// the same; the Response is where an HTTP caller reads it back.
func TestGovernanceConstraintsSurfacedOnResponse(t *testing.T) {
	e, err := NewEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	registerSimulatedProvider(t, e)
	e.govEngine.SetPolicies([]*governance.Policy{
		{
			PolicyID: "constrained-allow",
			Name:     "Allow With Budget",
			Status:   governance.PolicyStatusActive,
			Effect:   governance.ALLOW_WITH_CONSTRAINTS,
			Subject:  governance.Subject{SubjectType: "all"},
			Action:   governance.Action{ActionType: "custom"},
			Resource: governance.Resource{ResourceType: "all"},
			Constraints: []governance.Constraint{
				{ConstraintID: "c-budget", ConstraintType: "budget", Expression: "1000", Severity: "mandatory"},
			},
		},
	})

	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-n2-constraints",
		Context: NewRequestContext("corr-n2", "biz-1", "user-1"),
		Intent:  "constrained work",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	result := waitForResult(t, e, req.ID)
	if result.Status != "completed" {
		t.Fatalf("ALLOW_WITH_CONSTRAINTS must still complete, got %q (err=%v)", result.Status, result.Error)
	}
	if len(result.Constraints) != 1 || result.Constraints[0] != "budget:1000" {
		t.Errorf("constraints: want [budget:1000], got %v — the ALLOW_WITH_CONSTRAINTS decision was dropped", result.Constraints)
	}
}
