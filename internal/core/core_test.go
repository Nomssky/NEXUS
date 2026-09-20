package core

import (
	"context"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/health"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/memory"
)

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
	e := NewEngine(nil)
	if e == nil {
		t.Fatal("expected non-nil engine")
	}
	if e.Status() != lifecycle.StateCreated {
		t.Errorf("expected CREATED, got %s", e.Status())
	}
}

// TEST-CORE-005: Engine start and stop
func TestEngineStartStop(t *testing.T) {
	e := NewEngine(nil)
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
	e := NewEngine(nil)
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
	e := NewEngine(nil, WithClock(func() time.Time { return now }))
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

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	result, ok := e.GetResult("req-1")
	if !ok {
		t.Fatal("expected result")
	}
	if result.Status != "completed" {
		t.Errorf("expected completed, got %s (error: %v)", result.Status, result.Error)
	}
}

// TEST-CORE-008: Chain validation — missing request ID
func TestChainValidationMissingID(t *testing.T) {
	e := NewEngine(nil)
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
	e := NewEngine(nil)
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
	e := NewEngine(nil)
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
	e := NewEngine(nil, WithClock(func() time.Time { return now }))
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

	time.Sleep(100 * time.Millisecond)

	result, ok := e.GetResult("req-full")
	if !ok {
		t.Fatal("expected result")
	}
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
	e := NewEngine(nil, WithClock(func() time.Time { return now }))
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := &Request{
		ID:      "req-audit",
		Context: NewRequestContext("corr-audit", "biz-1", "user-1"),
		Intent:  "test audit",
	}

	e.SubmitRequest(req)
	time.Sleep(100 * time.Millisecond)

	result, _ := e.GetResult("req-audit")
	if result == nil {
		t.Fatal("expected result")
	}

	// Check that key steps are in the audit trail
	steps := make(map[string]bool)
	for _, entry := range result.AuditTrail {
		steps[entry.Step] = true
	}

	if !steps["validate"] {
		t.Error("expected validate step in audit trail")
	}
	if !steps["governance"] {
		t.Error("expected governance step in audit trail")
	}
	if !steps["objective"] {
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
	e := NewEngine(nil)
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
	e := NewEngine(nil)
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
	e := NewEngine(nil)
	if e.EventBus() == nil {
		t.Error("expected non-nil event bus")
	}
}

// TEST-CORE-019: Store accessible
func TestEngineStore(t *testing.T) {
	e := NewEngine(nil)
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
	e := NewEngine(nil, WithClock(func() time.Time { return now }))
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
	time.Sleep(200 * time.Millisecond)

	result, ok := e.GetResult("req-mem")
	if !ok {
		t.Fatal("expected result")
	}

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
	e := NewEngine(nil, WithClock(func() time.Time { return now }))
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
	time.Sleep(200 * time.Millisecond)

	result, ok := e.GetResult("req-att")
	if !ok {
		t.Fatal("expected result")
	}

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
	e := NewEngine(nil, WithClock(func() time.Time { return now }))
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
	time.Sleep(200 * time.Millisecond)

	result, ok := e.GetResult("req-memwrite")
	if !ok {
		t.Fatal("expected result")
	}

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
	e := NewEngine(nil, WithClock(func() time.Time { return now }))
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
	time.Sleep(200 * time.Millisecond)

	result, _ := e.GetResult("req-full-steps")
	if result == nil {
		t.Fatal("expected result")
	}

	// Collect all steps
	steps := make(map[string]bool)
	for _, entry := range result.AuditTrail {
		steps[entry.Step] = true
	}

	// Verify key steps exist
	requiredSteps := []string{
		"validate", "governance", "memory_read", "attention_score",
		"objective", "decision", "plan", "workflow", "schedule",
		"agent", "memory_write",
	}
	for _, step := range requiredSteps {
		if !steps[step] {
			t.Errorf("expected step '%s' in audit trail", step)
		}
	}
}
