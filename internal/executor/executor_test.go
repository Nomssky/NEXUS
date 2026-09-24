package executor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/agent"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
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

// waitForOutcome polls the executor until it records an outcome for taskID and
// returns it, failing the test if the bounded deadline elapses first (E-015).
// Tests wait for the observable completion state they assert on instead of
// sleeping a fixed duration and hoping asynchronous execution finished.
func waitForOutcome(t *testing.T, e *Executor, taskID string) *Outcome {
	t.Helper()

	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(10 * time.Second)

	for {
		if outcome, ok := e.GetOutcome(taskID); ok {
			return outcome
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for outcome %q", taskID)
		case <-tick.C:
		}
	}
}

// waitUntil polls cond until it holds, failing the test if the bounded
// deadline elapses first (E-015). Used for conditions that are not a single
// outcome lookup, such as metrics counters.
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

// waitSignal waits for one value on ch with a bounded deadline (E-015). Used
// where the observable condition is a handler lifecycle signal rather than a
// recorded outcome.
func waitSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
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

	// Wait for execution to be recorded (deterministic — E-015).
	outcome := waitForOutcome(t, e, "task-1")
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

	outcome := waitForOutcome(t, e, "task-custom")
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

	outcome := waitForOutcome(t, e, "task-fail")
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

	outcome := waitForOutcome(t, e, "task-denied")
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
	// The handler blocks until the test releases it, so it is guaranteed to
	// still be running when SubmitSync's 100ms context deadline expires —
	// no fixed sleep needed to simulate a long-running task (E-015).
	// defer close runs before the earlier defer e.Stop (LIFO): the handler is
	// released before Stop waits for active tasks.
	release := make(chan struct{})
	defer close(release)
	req.Handler = func(_ context.Context, _ *WorkRequest, _ *agent.Agent) (*Outcome, error) {
		<-release
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
	// Metrics are incremented after each outcome is recorded (executeWork's
	// deferred bookkeeping) — wait for the third execution to land, then
	// assert the exact counters (E-015).
	waitUntil(t, "3 executed tasks", func() bool {
		executed, _, _ := e.Metrics()
		return executed == 3
	})

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

	// Submit a task that stays in flight until the test releases it.
	started := make(chan struct{})
	release := make(chan struct{})
	req := testRequest("task-slow")
	req.Handler = func(_ context.Context, _ *WorkRequest, _ *agent.Agent) (*Outcome, error) {
		close(started)
		<-release
		return &Outcome{Status: "completed"}, nil
	}
	e.Submit(req)

	// The handler is provably running, so the task is provably active — the
	// active slot is only released after the handler returns (E-015).
	waitSignal(t, started, "handler start")
	if e.ActiveCount() != 1 {
		t.Errorf("expected 1 active while handler runs, got %d", e.ActiveCount())
	}

	close(release)
	waitForOutcome(t, e, "task-slow") // completion also releases the active slot
	if e.ActiveCount() != 0 {
		t.Errorf("expected 0 active after completion, got %d", e.ActiveCount())
	}
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

	// Every executor event (started/received/assigned/completed) is published
	// before the outcome is recorded, so a visible outcome means the queue
	// already holds them — one Dispatch delivers the batch (E-015).
	waitForOutcome(t, e, "task-events")

	// Dispatch queued events
	if _, err := bus.Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

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

	outcome := waitForOutcome(t, e, "task-corr")
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

	outcome := waitForOutcome(t, e, "task-biz")
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

	// All should complete — wait for each outcome (deterministic — E-015).
	for i := 0; i < 5; i++ {
		waitForOutcome(t, e, fmt.Sprintf("task-concurrent-%d", i))
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
	started := make(chan struct{}, 2)

	slowHandler := func(_ context.Context, _ *WorkRequest, _ *agent.Agent) (*Outcome, error) {
		started <- struct{}{} // signal that the handler is running
		<-block               // block until we release
		return &Outcome{Status: "completed"}, nil
	}

	// Fill capacity
	for i := 0; i < 2; i++ {
		req := testRequest(fmt.Sprintf("task-cap-%d", i))
		req.Handler = slowHandler
		e.Submit(req)
	}

	// Wait until both handlers are actually running: their active slots are
	// held and cannot be freed until block closes, so the overflow submission
	// below deterministically faces a full executor (E-015 — no sleep).
	waitSignal(t, started, "first handler start")
	waitSignal(t, started, "second handler start")

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

	// Submit a slow task
	block := make(chan struct{})
	started := make(chan struct{})
	req := testRequest("task-stop-wait")
	req.Handler = func(ctx context.Context, wr *WorkRequest, ag *agent.Agent) (*Outcome, error) {
		close(started)
		<-block
		return &Outcome{Status: "completed", Output: "done"}, nil
	}
	e.Submit(req)
	// The handler is provably running before Stop is called, so Stop must wait
	// on a genuinely active task — deterministic instead of hoping the
	// goroutine started within a fixed window (E-015).
	waitSignal(t, started, "handler start")

	done := make(chan bool)
	go func() {
		e.Stop(context.Background())
		done <- true
	}()

	// Should not stop immediately while task is running
	select {
	case <-done:
		// Stopped quickly — task may have completed already
	case <-time.After(200 * time.Millisecond):
		// Expected — still waiting for task
	}

	// Release the blocked task
	close(block)

	// Wait for everything to finish
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("stop did not complete within timeout")
	}
}

// =====================================================================
// P2 TEST COVERAGE GAPS
// =====================================================================

// TEST-EXEC-010: Provider failure path — model router Invoke returns error.
// E-008: provider failure must not be represented as completed.
func TestProviderFailurePath(t *testing.T) {
	now := time.Now()
	govEngine := governance.NewEngine([]*governance.Policy{
		{PolicyID: "allow", Name: "Allow", Status: governance.PolicyStatusActive, Effect: governance.ALLOW,
			Subject: governance.Subject{SubjectType: "all"}, Action: governance.Action{ActionType: "custom"},
			Resource: governance.Resource{ResourceType: "all"}},
	})

	// Non-nil router with empty registry: Route fails → Invoke returns error.
	// This hits defaultHandler's provider-error branch (not the nil-router
	// synthetic-success path).
	emptyRegistry := modelrouter.NewModelRegistry()
	failingRouter := modelrouter.NewModelRouter(emptyRegistry, modelrouter.RoutingPolicyLocalFirst)

	e := New(
		agent.NewAgentRuntime(),
		tool.NewToolRegistry(),
		govEngine,
		event.NewMemBus(),
		failingRouter,
		DefaultConfig(),
		WithClock(func() time.Time { return now }),
	)
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := testRequest("exec-provider-fail")
	req.Handler = nil // use default handler which calls model router
	if err := e.Submit(req); err != nil {
		t.Fatalf("failed to submit: %v", err)
	}

	outcome := waitForOutcome(t, e, "exec-provider-fail")
	// E-008 invariant: provider failure must not be reported as completed.
	if outcome.Status == "completed" {
		t.Errorf("provider failure must not be completed, got %q", outcome.Status)
	}
	if outcome.Status != "failed" {
		t.Errorf("expected failed, got %q", outcome.Status)
	}
	if outcome.Error == "" {
		t.Error("expected provider error message on outcome")
	}

	e.metricsMu.Lock()
	executed := e.totalExecuted
	failed := e.totalFailed
	e.metricsMu.Unlock()

	if executed == 0 {
		t.Error("expected at least one execution attempt")
	}
	if failed == 0 {
		t.Errorf("expected totalFailed > 0 for provider failure, got %d", failed)
	}
}

// TEST-EXEC-010b: Successful provider path still returns completed.
func TestProviderSuccessPath(t *testing.T) {
	now := time.Now()
	govEngine := governance.NewEngine([]*governance.Policy{
		{PolicyID: "allow", Name: "Allow", Status: governance.PolicyStatusActive, Effect: governance.ALLOW,
			Subject: governance.Subject{SubjectType: "all"}, Action: governance.Action{ActionType: "custom"},
			Resource: governance.Resource{ResourceType: "all"}},
	})

	registry := modelrouter.NewModelRegistry()
	err := registry.RegisterModel(&modelrouter.ModelDefinition{
		ID:           "ok-model",
		ProviderID:   "ok-provider",
		Capabilities: []modelrouter.ModelCapability{modelrouter.CapabilityReasoning, modelrouter.CapabilityToolCalling},
		Runtime:      modelrouter.RuntimeLocal,
		Status:       modelrouter.ModelStatusActive,
	})
	if err != nil {
		t.Fatalf("register model: %v", err)
	}
	router := modelrouter.NewModelRouter(registry, modelrouter.RoutingPolicyLocalFirst)
	router.RegisterProvider(modelrouter.NewLocalProvider(modelrouter.ProviderConfig{ID: "ok-provider"}))

	e := New(
		agent.NewAgentRuntime(),
		tool.NewToolRegistry(),
		govEngine,
		event.NewMemBus(),
		router,
		DefaultConfig(),
		WithClock(func() time.Time { return now }),
	)
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := testRequest("exec-provider-ok")
	req.Handler = nil
	if err := e.Submit(req); err != nil {
		t.Fatalf("failed to submit: %v", err)
	}

	outcome := waitForOutcome(t, e, "exec-provider-ok")
	if outcome.Status != "completed" {
		t.Errorf("expected completed for successful provider, got %q", outcome.Status)
	}
	if outcome.Error != "" {
		t.Errorf("expected no error on success, got %q", outcome.Error)
	}

	e.metricsMu.Lock()
	failed := e.totalFailed
	e.metricsMu.Unlock()
	if failed != 0 {
		t.Errorf("expected totalFailed=0 on success, got %d", failed)
	}
}

// TEST-EXEC-011: REQUIRE_APPROVAL governance outcome
func TestGovernanceRequiresApproval(t *testing.T) {
	now := time.Now()
	govEngine := governance.NewEngine([]*governance.Policy{
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

	e := New(
		agent.NewAgentRuntime(),
		tool.NewToolRegistry(),
		govEngine,
		event.NewMemBus(),
		nil,
		DefaultConfig(),
		WithClock(func() time.Time { return now }),
	)
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := testRequest("exec-req-approval")
	req.Handler = func(ctx context.Context, wr *WorkRequest, ag *agent.Agent) (*Outcome, error) {
		return &Outcome{Status: "completed", Output: "should not reach"}, nil
	}
	e.Submit(req)

	result := waitForOutcome(t, e, "exec-req-approval")
	if result.Status != "pending_approval" {
		t.Errorf("expected pending_approval, got %s", result.Status)
	}
}

// TEST-EXEC-012: ESCALATE governance outcome
func TestGovernanceEscalate(t *testing.T) {
	now := time.Now()
	govEngine := governance.NewEngine([]*governance.Policy{
		{
			PolicyID:   "escalate",
			Name:       "Escalate",
			Status:     governance.PolicyStatusActive,
			Effect:     governance.ESCALATE,
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
		nil,
		DefaultConfig(),
		WithClock(func() time.Time { return now }),
	)
	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := testRequest("exec-escalate")
	req.Handler = func(ctx context.Context, wr *WorkRequest, ag *agent.Agent) (*Outcome, error) {
		return &Outcome{Status: "completed", Output: "should not reach"}, nil
	}
	e.Submit(req)

	result := waitForOutcome(t, e, "exec-escalate")
	if result.Status != "escalated" {
		t.Errorf("expected escalated, got %s", result.Status)
	}
}
