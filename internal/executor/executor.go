// Package executor implements the NEXUS Task Executor — the execution runtime
// that makes the canonical chain actually do work.
//
// The executor accepts work submissions from the chain, dispatches them to
// agents, records outcomes, and emits events at every step. It is the missing
// heartbeat that turns "create state objects" into "execute work."
//
// Key invariants:
//   - Governance checked before every execution
//   - Business isolation enforced at every boundary
//   - Unknown outcome semantics honored (timeout = UNKNOWN, not failure)
//   - Single retry owner per operation
//   - Full correlation chain preserved
package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/agent"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// Cancellation errors returned by CancelTask (E-005). Callers map them to
// HTTP semantics: ErrTaskNotFound → 404, a terminal task → 409.
var (
	// ErrTaskNotFound reports that no control and no outcome exist for taskID.
	ErrTaskNotFound = errors.New("executor: task not found")
	// ErrTaskCompleted reports that the task already reached a terminal state.
	// Use errors.Is to match; *TaskTerminalError carries the terminal status.
	ErrTaskCompleted = errors.New("executor: task already terminal")
)

// TaskTerminalError reports a terminal task status alongside ErrTaskCompleted.
type TaskTerminalError struct {
	Status string
}

func (e *TaskTerminalError) Error() string {
	return fmt.Sprintf("executor: task already terminal (status=%s)", e.Status)
}

// Is matches the ErrTaskCompleted sentinel so errors.Is works.
func (e *TaskTerminalError) Is(target error) bool { return target == ErrTaskCompleted }

// taskCancelCause distinguishes why a task's context was cancelled (E-005/OQ8).
// First cause wins: once set it is never upgraded or downgraded, so a wait-context
// expiry can never be relabelled as a user cancellation (or vice versa).
type taskCancelCause int

const (
	taskCancelCauseNone        taskCancelCause = iota
	taskCancelCauseUser                        // explicit external cancellation (E-005)
	taskCancelCauseWaitExpired                 // SubmitSync/WaitOutcome context expired (OQ8)
)

// taskControl is the per-task cancellation control registered atomically with
// the slot reservation in Submit and removed exactly once in executeWork's defer.
// All fields are guarded by the executor's activeMu.
type taskControl struct {
	// ctx is the cancelable base context; the handler's exec ctx derives from it.
	ctx    context.Context
	cancel context.CancelFunc
	cause  taskCancelCause
	reason string
	actor  string
}

// cancellationEventPayload is the task.cancelled event payload
// (RUNTIME_EXECUTION_CONTRACTS §16.2: task_id + cancellation_reason;
// actor/business_id are additive attribution fields).
type cancellationEventPayload struct {
	TaskID             string `json:"task_id"`
	CancellationReason string `json:"cancellation_reason"`
	Actor              string `json:"actor,omitempty"`
	BusinessID         string `json:"business_id,omitempty"`
}

// Outcome represents the result of executing a task.
type Outcome struct {
	// TaskID links to the originating task.
	TaskID string `json:"task_id"`
	// AgentID is the agent that executed the work.
	AgentID string `json:"agent_id"`
	// Status is the final status: completed, failed, denied, unknown,
	// pending_approval, escalated, cancelled.
	Status string `json:"status"`
	// Output is the human-readable result.
	Output string `json:"output,omitempty"`
	// Evidence is the evidence for verification.
	Evidence []string `json:"evidence,omitempty"`
	// Error is the error message if failed.
	Error string `json:"error,omitempty"`
	// Duration is total execution time.
	Duration time.Duration `json:"duration"`
	// CorrelationID for tracing.
	CorrelationID string `json:"correlation_id"`
	// BusinessID for scope isolation.
	BusinessID string `json:"business_id"`
	// CreatedAt records when execution started.
	CreatedAt time.Time `json:"created_at"`
	// CompletedAt records when execution finished.
	CompletedAt time.Time `json:"completed_at"`
}

// WorkRequest is a request submitted to the executor for processing.
type WorkRequest struct {
	// TaskID is the unique task identifier.
	TaskID string `json:"task_id"`
	// CorrelationID for tracing.
	CorrelationID string `json:"correlation_id"`
	// BusinessID for scope isolation.
	BusinessID string `json:"business_id"`
	// ActorID who initiated the work.
	ActorID string `json:"actor_id"`
	// Intent describes what to do.
	Intent string `json:"intent"`
	// Input provides task-specific input data.
	Input map[string]string `json:"input,omitempty"`
	// Constraints on execution.
	Constraints []string `json:"constraints,omitempty"`
	// Priority (0-10, 10 highest).
	Priority int `json:"priority"`
	// Handler performs the actual work. If nil, a default handler is used.
	Handler TaskHandler
}

// TaskHandler is a function that executes a task's actual work.
type TaskHandler func(ctx context.Context, req *WorkRequest, ag *agent.Agent) (*Outcome, error)

// Config configures the task executor.
type Config struct {
	// MaxConcurrent is the maximum number of concurrent task executions.
	MaxConcurrent int
	// TaskTimeout is the maximum time for a single task execution.
	TaskTimeout time.Duration
	// MaxRetries is the maximum number of retries for failed tasks.
	MaxRetries int
	// DefaultModelID is the model requested when no specific model is set.
	// Empty falls back to the router's default routing behavior.
	DefaultModelID string
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxConcurrent:  10,
		TaskTimeout:    5 * time.Minute,
		MaxRetries:     3,
		DefaultModelID: "default",
	}
}

// Executor is the NEXUS Task Executor.
type Executor struct {
	config      Config
	agents      *agent.AgentRuntime
	tools       *tool.ToolRegistry
	governance  *governance.Engine
	events      *event.MemBus
	modelRouter *modelrouter.ModelRouter

	// State
	running      bool
	active       map[string]*Outcome // taskID -> outcome in progress
	controls     map[string]*taskControl
	activeMu     sync.RWMutex
	outcomes     map[string]*Outcome // taskID -> completed outcome
	outcomesMu   sync.RWMutex
	agentMu      sync.Mutex // serializes agent provisioning (AgentRuntime not thread-safe)
	shutdownCh   chan struct{}
	shutdownOnce sync.Once

	// Clock
	now func() time.Time

	// Metrics
	totalExecuted  int64
	totalFailed    int64
	totalDenied    int64
	totalCancelled int64
	metricsMu      sync.RWMutex

	// Event sequence for unique IDs
	evtSeq atomic.Int64
}

// Option configures the executor.
type Option func(*Executor)

// WithClock injects a clock for testing.
func WithClock(now func() time.Time) Option {
	return func(e *Executor) { e.now = now }
}

// New creates a new Task Executor.
func New(
	agentRuntime *agent.AgentRuntime,
	toolReg *tool.ToolRegistry,
	govEngine *governance.Engine,
	eventBus *event.MemBus,
	mdlRouter *modelrouter.ModelRouter,
	cfg Config,
	opts ...Option,
) *Executor {
	e := &Executor{
		config:      cfg,
		agents:      agentRuntime,
		tools:       toolReg,
		governance:  govEngine,
		events:      eventBus,
		modelRouter: mdlRouter,
		active:      make(map[string]*Outcome),
		controls:    make(map[string]*taskControl),
		outcomes:    make(map[string]*Outcome),
		shutdownCh:  make(chan struct{}),
		now:         time.Now,
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Start begins the executor, making it ready to accept work.
func (e *Executor) Start(_ context.Context) error {
	e.activeMu.Lock()
	if e.running {
		e.activeMu.Unlock()
		return fmt.Errorf("executor already running")
	}
	e.running = true
	e.activeMu.Unlock()

	e.emitEvent("executor.started", "", "", nil) // system-level event: intentionally unscoped (no BusinessID)
	return nil
}

// Stop gracefully shuts down the executor, waiting for active tasks.
func (e *Executor) Stop(ctx context.Context) error {
	e.shutdownOnce.Do(func() {
		close(e.shutdownCh)
	})

	// Wait for active tasks to complete or context cancellation
	deadline := time.After(30 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			e.activeMu.Lock()
			e.running = false
			e.activeMu.Unlock()
			return fmt.Errorf("shutdown forced by context")
		case <-deadline:
			e.activeMu.Lock()
			count := len(e.active)
			e.running = false
			e.activeMu.Unlock()
			if count > 0 {
				return fmt.Errorf("shutdown timed out with %d active tasks", count)
			}
			return nil
		case <-ticker.C:
			e.activeMu.RLock()
			count := len(e.active)
			e.activeMu.RUnlock()
			if count == 0 {
				e.activeMu.Lock()
				e.running = false
				e.activeMu.Unlock()
				return nil
			}
		}
	}
}

// Submit submits a work request for asynchronous execution.
// Returns immediately after queuing; results are available via GetOutcome.
// The cancellation control is registered atomically with the slot reservation
// (E-005), so a cancel that arrives any time after Submit is honored.
func (e *Executor) Submit(req *WorkRequest) error {
	baseCtx, baseCancel := context.WithCancel(context.Background())

	e.activeMu.Lock()
	if !e.running {
		e.activeMu.Unlock()
		baseCancel()
		return fmt.Errorf("executor not running")
	}
	if len(e.active) >= e.config.MaxConcurrent {
		count := len(e.active)
		e.activeMu.Unlock()
		baseCancel()
		return fmt.Errorf("executor at capacity (%d/%d)", count, e.config.MaxConcurrent)
	}
	// Reserve a slot atomically
	e.active[req.TaskID] = &Outcome{TaskID: req.TaskID}
	e.controls[req.TaskID] = &taskControl{ctx: baseCtx, cancel: baseCancel}
	e.activeMu.Unlock()

	// Execute asynchronously
	go e.executeWork(req)
	return nil
}

// SubmitSync submits a work request and waits for the result.
func (e *Executor) SubmitSync(ctx context.Context, req *WorkRequest) (*Outcome, error) {
	if err := e.Submit(req); err != nil {
		return nil, err
	}
	return e.WaitOutcome(ctx, req.TaskID)
}

// WaitOutcome waits for an already-submitted task's outcome.
//
// If ctx expires before an outcome is recorded, the task's handler context is
// cancelled (cause=waitExpired, OQ8) so cooperative handlers stop, and ctx.Err()
// is returned — DeadlineExceeded keeps its existing deadline semantics and is
// never relabelled as a user cancellation. An outcome that raced in during
// expiry is returned instead of the error.
func (e *Executor) WaitOutcome(ctx context.Context, taskID string) (*Outcome, error) {
	// Poll for outcome
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Prefer an outcome that raced in during ctx expiry.
			if o, ok := e.GetOutcome(taskID); ok {
				return o, nil
			}
			e.expireWait(taskID)
			return nil, ctx.Err()
		case <-ticker.C:
			if o, ok := e.GetOutcome(taskID); ok {
				return o, nil
			}
		}
	}
}

// expireWait records a wait-context expiry on the task control (OQ8) and
// cancels the handler context so cooperative handlers stop. First cause wins:
// an explicit user cancellation already recorded is never downgraded.
func (e *Executor) expireWait(taskID string) {
	e.activeMu.Lock()
	defer e.activeMu.Unlock()
	ctrl, ok := e.controls[taskID]
	if !ok {
		return
	}
	if ctrl.cause == taskCancelCauseNone {
		ctrl.cause = taskCancelCauseWaitExpired
		ctrl.reason = "wait context expired"
	}
	ctrl.cancel()
}

// CancelTask cancels an in-flight task (E-005). It is a mechanical runtime
// control: authorization (identity, membership, ownership) is enforced by the
// caller at the API boundary.
//
// Semantics:
//   - task running or queued: records cause=user, cancels the handler context,
//     returns nil (repeat calls are idempotent while the control exists)
//   - task already terminal with status cancelled: nil (idempotent repeat)
//   - task already terminal otherwise: *TaskTerminalError (ErrTaskCompleted)
//   - unknown task: ErrTaskNotFound
//
// The handler observes cancellation through its ctx; cooperative handlers stop.
// The resulting outcome is Status=cancelled (never failed) with a
// task.cancelled event.
func (e *Executor) CancelTask(taskID, reason, actor string) error {
	e.activeMu.Lock()
	ctrl, ok := e.controls[taskID]
	if !ok {
		e.activeMu.Unlock()
		// Terminal arbitration: the control is removed only after the outcome
		// is recorded, so a missing control with an outcome means terminal.
		if o, has := e.GetOutcome(taskID); has {
			if o.Status == "cancelled" {
				return nil
			}
			return &TaskTerminalError{Status: o.Status}
		}
		return ErrTaskNotFound
	}
	if ctrl.cause == taskCancelCauseNone {
		ctrl.cause = taskCancelCauseUser
		ctrl.reason = reason
		ctrl.actor = actor
	}
	ctrl.cancel()
	e.activeMu.Unlock()
	return nil
}

// taskControlState snapshots the task control's base ctx and cancellation
// cause under a single read lock. Returns a background ctx when the control
// no longer exists (terminal task).
func (e *Executor) taskControlState(taskID string) (context.Context, taskCancelCause, string, string) {
	e.activeMu.RLock()
	defer e.activeMu.RUnlock()
	ctrl, ok := e.controls[taskID]
	if !ok {
		return context.Background(), taskCancelCauseNone, "", ""
	}
	return ctrl.ctx, ctrl.cause, ctrl.reason, ctrl.actor
}

// emitCancellation publishes the contract task.cancelled event (§16.2).
func (e *Executor) emitCancellation(req *WorkRequest, reason, actor string) {
	e.emitEvent(string(event.EventTypeTaskCancelled), req.TaskID, req.CorrelationID, &cancellationEventPayload{
		TaskID:             req.TaskID,
		CancellationReason: reason,
		Actor:              actor,
		BusinessID:         req.BusinessID,
	})
}

// executeWork runs a single work request through the execution pipeline.
func (e *Executor) executeWork(req *WorkRequest) {
	start := e.now()

	// Create outcome tracker (slot already reserved by Submit)
	outcome := &Outcome{
		TaskID:        req.TaskID,
		CorrelationID: req.CorrelationID,
		BusinessID:    req.BusinessID,
		CreatedAt:     start,
	}

	// Update the reserved slot with full outcome
	e.activeMu.Lock()
	e.active[req.TaskID] = outcome
	e.activeMu.Unlock()

	defer func() {
		// Move from active to outcomes
		e.activeMu.Lock()
		delete(e.active, req.TaskID)
		e.activeMu.Unlock()

		outcome.CompletedAt = e.now()
		outcome.Duration = e.now().Sub(start)

		e.outcomesMu.Lock()
		e.outcomes[req.TaskID] = outcome
		e.outcomesMu.Unlock()

		// Update metrics
		e.metricsMu.Lock()
		e.totalExecuted++
		switch outcome.Status {
		case "failed":
			e.totalFailed++
		case "denied":
			e.totalDenied++
		case "cancelled":
			e.totalCancelled++
		}
		e.metricsMu.Unlock()

		// Remove the cancellation control exactly once — after the outcome is
		// visible, so CancelTask's terminal arbitration (control gone + outcome
		// present) can never race a not-yet-recorded outcome (E-005).
		e.activeMu.Lock()
		if ctrl := e.controls[req.TaskID]; ctrl != nil {
			ctrl.cancel() // release ctx resources even if the task was never cancelled
			delete(e.controls, req.TaskID)
		}
		e.activeMu.Unlock()
	}()

	// E-005: cancellation recorded before execution starts — the handler must
	// never run. Status cancelled, never a failure.
	if _, cause, reason, actor := e.taskControlState(req.TaskID); cause == taskCancelCauseUser {
		outcome.Status = "cancelled"
		e.emitCancellation(req, reason, actor)
		return
	}

	// Step 1: Governance check
	decision := e.checkGovernance(req)
	switch decision.Outcome {
	case governance.DENY:
		outcome.Status = "denied"
		outcome.Error = fmt.Sprintf("governance denied: %s", decision.Reason)
		e.emitEvent("executor.denied", req.TaskID, req.CorrelationID, outcome)
		return
	case governance.REQUIRE_APPROVAL:
		outcome.Status = "pending_approval"
		outcome.Error = fmt.Sprintf("governance requires approval: %s", decision.Reason)
		e.emitEvent("executor.pending_approval", req.TaskID, req.CorrelationID, outcome)
		return
	case governance.ESCALATE:
		outcome.Status = "escalated"
		outcome.Error = fmt.Sprintf("governance escalated: %s", decision.Reason)
		e.emitEvent("executor.escalated", req.TaskID, req.CorrelationID, outcome)
		return
	case governance.ALLOW, governance.ALLOW_WITH_CONSTRAINTS:
		// proceed
	default:
		outcome.Status = "denied"
		outcome.Error = fmt.Sprintf("governance unknown outcome: %s", decision.Outcome)
		e.emitEvent("executor.denied", req.TaskID, req.CorrelationID, outcome)
		return
	}

	e.emitEvent("executor.received", req.TaskID, req.CorrelationID, outcome)

	// Step 2: Find or provision an agent
	ag := e.findAgent(req)
	if ag == nil {
		outcome.Status = "unknown"
		outcome.Error = "no agent available"
		e.emitEvent("executor.no_agent", req.TaskID, req.CorrelationID, outcome)
		return
	}

	outcome.AgentID = ag.ID
	e.emitEvent("executor.assigned", req.TaskID, req.CorrelationID, outcome)

	// Step 3: Execute the task. The handler ctx derives from the task control's
	// cancelable base ctx so CancelTask (E-005) and wait-expiry (OQ8) reach the
	// handler; the pre-handler check closes the window where a cancel landed
	// during governance/agent provisioning.
	baseCtx, cause, reason, actor := e.taskControlState(req.TaskID)
	if cause == taskCancelCauseUser {
		outcome.Status = "cancelled"
		e.emitCancellation(req, reason, actor)
		return
	}
	execCtx, cancel := context.WithTimeout(baseCtx, e.config.TaskTimeout)
	defer cancel()

	var result *Outcome
	var err error

	if req.Handler != nil {
		result, err = req.Handler(execCtx, req, ag)
	} else {
		result, err = e.defaultHandler(execCtx, req, ag)
	}

	if err != nil {
		// E-005/OQ8 classification: the first recorded cause decides how an
		// interrupted handler error is reported. An explicit cancellation is
		// status=cancelled (never failed); a wait-context expiry keeps the
		// existing failed/timeout semantics; no recorded cause means the
		// handler failed on its own.
		_, cause, reason, actor := e.taskControlState(req.TaskID)
		switch {
		case cause == taskCancelCauseUser:
			outcome.Status = "cancelled"
			e.emitCancellation(req, reason, actor)
			return
		case cause == taskCancelCauseWaitExpired:
			outcome.Status = "failed"
			outcome.Error = fmt.Sprintf("wait context expired: %v", err)
			e.emitEvent("executor.failed", req.TaskID, req.CorrelationID, outcome)
			return
		}
		outcome.Status = "failed"
		outcome.Error = err.Error()
		e.emitEvent("executor.failed", req.TaskID, req.CorrelationID, outcome)
		return
	}

	// err == nil: the handler ran to completion. Completion wins over any
	// recorded cause — a non-cooperative handler that ignores its ctx finishes
	// normally (documented limitation: cancellation is cooperative).
	// Merge handler result into outcome
	if result != nil {
		outcome.Status = result.Status
		outcome.Output = result.Output
		outcome.Evidence = result.Evidence
	}

	if outcome.Status == "" {
		outcome.Status = "completed"
	}

	e.emitEvent("executor.completed", req.TaskID, req.CorrelationID, outcome)
}

// checkGovernance evaluates governance for the work request.
// Returns the governance decision for the caller to handle.
func (e *Executor) checkGovernance(req *WorkRequest) governance.Decision {
	return e.governance.Evaluate(governance.Request{
		Actor:      req.ActorID,
		Action:     "execute_task",
		Resource:   "workflow",
		BusinessID: req.BusinessID,
	})
}

// findAgent finds or provisions an agent for the work request.
func (e *Executor) findAgent(req *WorkRequest) *agent.Agent {
	def := &agent.AgentDefinition{
		Name:        fmt.Sprintf("worker-%s", req.TaskID),
		Version:     "1.0.0",
		Description: fmt.Sprintf("Worker for task %s", req.TaskID),
		Type:        agent.AgentTypeWorker,
		BusinessID:  req.BusinessID,
		Capabilities: []agent.Capability{
			"execute_task",
		},
		Permissions: []agent.Authority{
			agent.AuthorityExecute,
		},
		Authority:       agent.AuthorityExecute,
		ParentAuthority: agent.AuthorityAdmin,
		Budget: agent.Budget{
			MaxTokens: 10000,
			MaxCost:   1.0,
			MaxTasks:  1,
		},
		SpawnLimits: agent.SpawnLimits{
			MaxChildren:    0,
			MaxDepth:       0,
			MaxDescendants: 0,
		},
	}

	e.agentMu.Lock()
	defer e.agentMu.Unlock()

	ag, err := e.agents.ProvisionAgent(def)
	if err != nil {
		return nil
	}

	if err := e.agents.StartAgent(ag.ID); err != nil {
		return nil
	}

	return ag
}

// defaultHandler is the default task handler that simulates execution.
func (e *Executor) defaultHandler(ctx context.Context, req *WorkRequest, ag *agent.Agent) (*Outcome, error) {
	// If a model router is available, use it for actual inference
	if e.modelRouter != nil {
		genReq := &modelrouter.GenerateRequest{
			RequestID: req.TaskID,
			ModelID:   e.config.DefaultModelID,
			Messages: []modelrouter.Message{
				{Role: "user", Content: req.Intent},
			},
			MaxTokens:   256,
			Temperature: 0.7,
		}

		routingReq := &modelrouter.RoutingRequest{
			RequestID:       req.TaskID,
			AgentID:         ag.ID,
			BusinessID:      req.BusinessID,
			RequiredCaps:    []modelrouter.ModelCapability{modelrouter.CapabilityReasoning, modelrouter.CapabilityToolCalling},
			PreferLocal:     true,
			FallbackEnabled: true,
		}

		resp, decision, err := e.modelRouter.Invoke(ctx, routingReq, genReq)
		if err != nil {
			// E-008: provider failure must not be reported as completed.
			// Propagate as a handler error so executeWork records status=failed,
			// increments totalFailed, and emits executor.failed.
			return nil, fmt.Errorf("provider invocation failed: %w", err)
		}

		output := resp.Content
		if output == "" {
			output = fmt.Sprintf("task '%s' executed by agent %s via %s/%s", req.Intent, ag.ID, decision.ProviderID, resp.ModelID)
		}

		return &Outcome{
			TaskID:  req.TaskID,
			AgentID: ag.ID,
			Status:  "completed",
			Output:  output,
			Evidence: []string{
				fmt.Sprintf("task_id=%s", req.TaskID),
				fmt.Sprintf("agent_id=%s", ag.ID),
				fmt.Sprintf("business_id=%s", req.BusinessID),
				fmt.Sprintf("provider=%s", decision.ProviderID),
				fmt.Sprintf("model=%s", resp.ModelID),
				fmt.Sprintf("input_tokens=%d", resp.InputTokens),
				fmt.Sprintf("output_tokens=%d", resp.OutputTokens),
				fmt.Sprintf("latency_ms=%d", resp.LatencyMs),
				fmt.Sprintf("executed_at=%s", e.now().Format(time.RFC3339)),
			},
		}, nil
	}

	// No model router available — synthetic response
	return &Outcome{
		TaskID:  req.TaskID,
		AgentID: ag.ID,
		Status:  "completed",
		Output:  fmt.Sprintf("task '%s' executed by agent %s", req.Intent, ag.ID),
		Evidence: []string{
			fmt.Sprintf("task_id=%s", req.TaskID),
			fmt.Sprintf("agent_id=%s", ag.ID),
			fmt.Sprintf("business_id=%s", req.BusinessID),
			fmt.Sprintf("executed_at=%s", e.now().Format(time.RFC3339)),
		},
	}, nil
}

// GetOutcome returns the outcome of a completed task.
func (e *Executor) GetOutcome(taskID string) (*Outcome, bool) {
	e.outcomesMu.RLock()
	defer e.outcomesMu.RUnlock()
	o, ok := e.outcomes[taskID]
	return o, ok
}

// ActiveCount returns the number of currently active tasks.
// The read lock is held for the duration of the call (via defer), ensuring
// the count is consistent with the live active-tasks map.
func (e *Executor) ActiveCount() int {
	e.activeMu.RLock()
	defer e.activeMu.RUnlock()
	return len(e.active)
}

// IsRunning reports whether the executor is accepting work (Start has been
// called and Stop has not completed). This is the authoritative runtime state
// used by the control-components endpoint (G-012) — never infer "active" from
// construction alone.
func (e *Executor) IsRunning() bool {
	e.activeMu.RLock()
	defer e.activeMu.RUnlock()
	return e.running
}

// Metrics returns execution metrics.
func (e *Executor) Metrics() (executed, failed, denied int64) {
	e.metricsMu.RLock()
	defer e.metricsMu.RUnlock()
	return e.totalExecuted, e.totalFailed, e.totalDenied
}

// CancelledCount returns the number of tasks that reached status=cancelled
// (E-005). Metrics() keeps its existing three-value signature; this accessor
// is additive so existing callers are unaffected.
func (e *Executor) CancelledCount() int64 {
	e.metricsMu.RLock()
	defer e.metricsMu.RUnlock()
	return e.totalCancelled
}

// emitEvent publishes an event to the event bus.
func (e *Executor) emitEvent(eventType string, taskID, corrID string, data interface{}) {
	if e.events == nil {
		return
	}

	evt := &event.Event{
		ID:            fmt.Sprintf("exec-%d-%d", e.now().UnixNano(), e.evtSeq.Add(1)),
		Type:          event.EventType(eventType),
		Source:        "executor",
		Timestamp:     e.now(),
		CorrelationID: corrID,
	}

	// Serialize data payload
	if data != nil {
		jsonData, err := json.Marshal(data)
		if err == nil {
			evt.Data = jsonData
		}
	}

	// Extract BusinessID from Outcome/cancellation payload if available
	switch d := data.(type) {
	case *Outcome:
		if d.BusinessID != "" {
			evt.BusinessID = d.BusinessID
		}
	case *cancellationEventPayload:
		if d.BusinessID != "" {
			evt.BusinessID = d.BusinessID
		}
	}

	_ = e.events.Publish(evt)
}
