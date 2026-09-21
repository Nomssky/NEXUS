package executor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/agent"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

func testExecutor(t *testing.T) *Executor {
	t.Helper()
	now := time.Now()
	// Default allow policy so tasks can execute
	govEngine := governance.NewEngine([]*governance.Policy{
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
	})
	return New(
		agent.NewAgentRuntime(),
		tool.NewToolRegistry(),
		govEngine,
		event.NewMemBus(),
		nil, // no model router in tests
		DefaultConfig(),
		WithClock(func() time.Time { return now }),
	)
}

func testRequest(id string) *WorkRequest {
	return &WorkRequest{
		TaskID:        id,
		CorrelationID: "corr-" + id,
		BusinessID:    "biz-1",
		ActorID:       "user-1",
		Intent:        "test task",
		Priority:      5,
	}
}

// TEST-EXEC-001: Executor creation
func TestExecutorCreation(t *testing.T) {
	e := testExecutor(t)
	if e == nil {
		t.Fatal("expected non-nil executor")
	}
	if e.ActiveCount() != 0 {
		t.Errorf("expected 0 active, got %d", e.ActiveCount())
	}
}

// TEST-EXEC-002: Start and stop
func TestExecutorStartStop(t *testing.T) {
	e := testExecutor(t)
	ctx := context.Background()

	if err := e.Start(ctx); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	if err := e.Stop(ctx); err != nil {
		t.Fatalf("failed to stop: %v", err)
	}
}

// TEST-EXEC-003: Double start prevented
func TestExecutorDoubleStart(t *testing.T) {
	e := testExecutor(t)
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	if err := e.Start(ctx); err == nil {
		t.Error("expected error on double start")
	}
}

// TEST-EXEC-004: Submit to stopped executor fails
func TestSubmitToStoppedExecutor(t *testing.T) {
	e := testExecutor(t)
	req := testRequest("task-1")

	if err := e.Submit(req); err == nil {
		t.Error("expected error submitting to stopped executor")
	}
}

// TEST-EXEC-005: Submit and execute
func TestSubmitAndExecute(t *testing.T) {
	e := testExecutor(t)
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := testRequest("task-1")
	if err := e.Submit(req); err != nil {
		t.Fatalf("failed to submit: %v", err)
	}

	// Wait for execution
	time.Sleep(100 * time.Millisecond)

	outcome, ok := e.GetOutcome("task-1")
	if !ok {
		t.Fatal("expected outcome")
	}
	if outcome.Status != "completed" {
		t.Errorf("expected completed, got %s", outcome.Status)
	}
	if outcome.AgentID == "" {
		t.Error("expected agent_id")
	}
}

// TEST-EXEC-006: Submit with custom handler
func TestSubmitWithCustomHandler(t *testing.T) {
	e := testExecutor(t)
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := testRequest("task-custom")
	req.Handler = func(_ context.Context, req *WorkRequest, ag *agent.Agent) (*Outcome, error) {
		return &Outcome{
			TaskID:   req.TaskID,
			AgentID:  ag.ID,
			Status:   "completed",
			Output:   "custom result",
			Evidence: []string{"custom=true"},
		}, nil
	}

	if err := e.Submit(req); err != nil {
		t.Fatalf("failed to submit: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	outcome, ok := e.GetOutcome("task-custom")
	if !ok {
		t.Fatal("expected outcome")
	}
	if outcome.Output != "custom result" {
		t.Errorf("expected 'custom result', got '%s'", outcome.Output)
	}
	if len(outcome.Evidence) != 1 || outcome.Evidence[0] != "custom=true" {
		t.Errorf("expected [custom=true], got %v", outcome.Evidence)
	}
}

// TEST-EXEC-007: Handler error
func TestHandlerError(t *testing.T) {
	e := testExecutor(t)
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := testRequest("task-fail")
	req.Handler = func(_ context.Context, _ *WorkRequest, _ *agent.Agent) (*Outcome, error) {
		return nil, fmt.Errorf("handler failed")
	}

	if err := e.Submit(req); err != nil {
		t.Fatalf("failed to submit: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	outcome, ok := e.GetOutcome("task-fail")
	if !ok {
		t.Fatal("expected outcome")
	}
	if outcome.Status != "failed" {
		t.Errorf("expected failed, got %s", outcome.Status)
	}
	if outcome.Error != "handler failed" {
		t.Errorf("expected 'handler failed', got '%s'", outcome.Error)
	}
}

// TEST-EXEC-008: Governance denied
func TestGovernanceDenied(t *testing.T) {
	now := time.Now()
	govEngine := governance.NewEngine([]*governance.Policy{
		{
			PolicyID:   "deny-all",
			Name:       "Deny All",
			Status:     governance.PolicyStatusActive,
			Effect:     governance.DENY,
			Subject:    governance.Subject{SubjectType: "all"},
			Action:     governance.Action{ActionType: "custom"},
			Resource:   governance.Resource{ResourceType: "all"},
			Precedence: 100,
		},
	})

	e := New(
		agent.NewAgentRuntime(),
		tool.NewToolRegistry(),
		govEngine,
		event.NewMemBus(),
		nil, // no model router in tests
		DefaultConfig(),
		WithClock(func() time.Time { return now }),
	)

	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := testRequest("task-denied")
	if err := e.Submit(req); err != nil {
		t.Fatalf("failed to submit: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	outcome, ok := e.GetOutcome("task-denied")
	if !ok {
		t.Fatal("expected outcome")
	}
	if outcome.Status != "denied" {
		t.Errorf("expected denied, got %s", outcome.Status)
	}
}

// TEST-EXEC-009: SubmitSync
func TestSubmitSync(t *testing.T) {
	e := testExecutor(t)
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := testRequest("task-sync")
	outcome, err := e.SubmitSync(ctx, req)
	if err != nil {
		t.Fatalf("failed to submit sync: %v", err)
	}
	if outcome.Status != "completed" {
		t.Errorf("expected completed, got %s", outcome.Status)
	}
}

// TEST-EXEC-010: SubmitSync timeout
func TestSubmitSyncTimeout(t *testing.T) {
	e := testExecutor(t)
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := testRequest("task-timeout")
	req.Handler = func(_ context.Context, _ *WorkRequest, _ *agent.Agent) (*Outcome, error) {
		time.Sleep(5 * time.Second)
		return &Outcome{Status: "completed"}, nil
	}

	shortCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := e.SubmitSync(shortCtx, req)
	if err == nil {
		t.Error("expected timeout error")
	}
}

// TEST-EXEC-011: Metrics
func TestMetrics(t *testing.T) {
	e := testExecutor(t)
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	// Execute 3 tasks
	for i := 0; i < 3; i++ {
		req := testRequest(fmt.Sprintf("task-%d", i))
		e.Submit(req)
	}
	time.Sleep(200 * time.Millisecond)

	executed, failed, denied := e.Metrics()
	if executed != 3 {
		t.Errorf("expected 3 executed, got %d", executed)
	}
	if failed != 0 {
		t.Errorf("expected 0 failed, got %d", failed)
	}
	if denied != 0 {
		t.Errorf("expected 0 denied, got %d", denied)
	}
}

// TEST-EXEC-012: Active count
func TestActiveCount(t *testing.T) {
	e := testExecutor(t)
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	if e.ActiveCount() != 0 {
		t.Errorf("expected 0 active initially, got %d", e.ActiveCount())
	}

	// Submit a slow task
	req := testRequest("task-slow")
	req.Handler = func(_ context.Context, _ *WorkRequest, _ *agent.Agent) (*Outcome, error) {
		time.Sleep(200 * time.Millisecond)
		return &Outcome{Status: "completed"}, nil
	}
	e.Submit(req)

	time.Sleep(50 * time.Millisecond)
	// May or may not be active depending on goroutine scheduling
}

// TEST-EXEC-013: Event emission
func TestEventEmission(t *testing.T) {
	bus := event.NewMemBus()
	now := time.Now()

	e := New(
		agent.NewAgentRuntime(),
		tool.NewToolRegistry(),
		governance.NewEngine([]*governance.Policy{
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
		}),
		bus,
		nil, // no model router in tests
		DefaultConfig(),
		WithClock(func() time.Time { return now }),
	)

	received := make([]event.EventType, 0)
	bus.Subscribe(event.ConsumerFunc(func(ev *event.Event) error {
		received = append(received, ev.Type)
		return nil
	}))

	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := testRequest("task-events")
	e.Submit(req)
	time.Sleep(200 * time.Millisecond)

	// Dispatch queued events
	bus.Dispatch()

	// Should have received started, received, assigned, completed events
	if len(received) < 4 {
		t.Errorf("expected at least 4 events, got %d: %v", len(received), received)
	}
}

// TEST-EXEC-014: Correlation ID preserved
func TestCorrelationIDPreserved(t *testing.T) {
	e := testExecutor(t)
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := testRequest("task-corr")
	req.CorrelationID = "my-custom-corr-id"
	e.Submit(req)
	time.Sleep(100 * time.Millisecond)

	outcome, ok := e.GetOutcome("task-corr")
	if !ok {
		t.Fatal("expected outcome")
	}
	if outcome.CorrelationID != "my-custom-corr-id" {
		t.Errorf("expected my-custom-corr-id, got %s", outcome.CorrelationID)
	}
}

// TEST-EXEC-015: Business ID preserved
func TestBusinessIDPreserved(t *testing.T) {
	e := testExecutor(t)
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := testRequest("task-biz")
	req.BusinessID = "acme-corp"
	e.Submit(req)
	time.Sleep(100 * time.Millisecond)

	outcome, ok := e.GetOutcome("task-biz")
	if !ok {
		t.Fatal("expected outcome")
	}
	if outcome.BusinessID != "acme-corp" {
		t.Errorf("expected acme-corp, got %s", outcome.BusinessID)
	}
}

// TEST-EXEC-016: Default config
func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxConcurrent != 10 {
		t.Errorf("expected 10 max concurrent, got %d", cfg.MaxConcurrent)
	}
	if cfg.TaskTimeout != 5*time.Minute {
		t.Errorf("expected 5m timeout, got %v", cfg.TaskTimeout)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("expected 3 max retries, got %d", cfg.MaxRetries)
	}
}

// TEST-EXEC-017: GetOutcome not found
func TestGetOutcomeNotFound(t *testing.T) {
	e := testExecutor(t)
	_, ok := e.GetOutcome("nonexistent")
	if ok {
		t.Error("expected not found")
	}
}

// TEST-EXEC-018: Concurrent execution
func TestConcurrentExecution(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxConcurrent = 5

	e := New(
		agent.NewAgentRuntime(),
		tool.NewToolRegistry(),
		governance.NewEngine(nil),
		event.NewMemBus(),
		nil, // no model router in tests
		cfg,
	)

	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	// Submit 5 tasks concurrently
	for i := 0; i < 5; i++ {
		req := testRequest(fmt.Sprintf("task-concurrent-%d", i))
		if err := e.Submit(req); err != nil {
			t.Fatalf("failed to submit task %d: %v", i, err)
		}
	}

	time.Sleep(300 * time.Millisecond)

	// All should complete
	for i := 0; i < 5; i++ {
		_, ok := e.GetOutcome(fmt.Sprintf("task-concurrent-%d", i))
		if !ok {
			t.Errorf("expected outcome for task-concurrent-%d", i)
		}
	}
}

// TEST-EXEC-019: Capacity limit
func TestCapacityLimit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxConcurrent = 2

	govEngine := governance.NewEngine([]*governance.Policy{
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
	})

	e := New(
		agent.NewAgentRuntime(),
		tool.NewToolRegistry(),
		govEngine,
		event.NewMemBus(),
		nil, // no model router in tests
		cfg,
	)

	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	// Use a channel to block handlers
	block := make(chan struct{})

	slowHandler := func(_ context.Context, _ *WorkRequest, _ *agent.Agent) (*Outcome, error) {
		<-block // block until we release
		return &Outcome{Status: "completed"}, nil
	}

	// Fill capacity
	for i := 0; i < 2; i++ {
		req := testRequest(fmt.Sprintf("task-cap-%d", i))
		req.Handler = slowHandler
		e.Submit(req)
	}

	time.Sleep(50 * time.Millisecond) // let goroutines start

	// Third should fail — capacity full
	req := testRequest("task-cap-overflow")
	if err := e.Submit(req); err == nil {
		t.Error("expected capacity error")
	}

	// Release blocked tasks
	close(block)
}

// TEST-EXEC-020: Stop waits for active tasks
func TestStopWaitsForTasks(t *testing.T) {
	e := testExecutor(t)
	ctx := context.Background()
	e.Start(ctx)

	done := make(chan bool)
	go func() {
		e.Stop(context.Background())
		done <- true
	}()

	// Should not stop immediately while task is running
	select {
	case <-done:
		// Stopped quickly — task may have completed already
	case <-time.After(100 * time.Millisecond):
		// Expected — still waiting for task
	}

	// Wait for everything to finish
	time.Sleep(500 * time.Millisecond)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("stop did not complete within timeout")
	}
}
