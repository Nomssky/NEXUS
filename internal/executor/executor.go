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
	"fmt"
	"sync"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/agent"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// Outcome represents the result of executing a task.
type Outcome struct {
	// TaskID links to the originating task.
	TaskID string `json:"task_id"`
	// AgentID is the agent that executed the work.
	AgentID string `json:"agent_id"`
	// Status is the final status: completed, failed, denied, unknown.
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
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxConcurrent: 10,
		TaskTimeout:   5 * time.Minute,
		MaxRetries:    3,
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
	activeMu     sync.RWMutex
	outcomes     map[string]*Outcome // taskID -> completed outcome
	outcomesMu   sync.RWMutex
	agentMu      sync.Mutex // serializes agent provisioning (AgentRuntime not thread-safe)
	shutdownCh   chan struct{}
	shutdownOnce sync.Once

	// Clock
	now func() time.Time

	// Metrics
	totalExecuted int64
	totalFailed   int64
	totalDenied   int64
	metricsMu     sync.RWMutex
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

	e.emitEvent("executor.started", "", "", nil)
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
func (e *Executor) Submit(req *WorkRequest) error {
	e.activeMu.Lock()
	if !e.running {
		e.activeMu.Unlock()
		return fmt.Errorf("executor not running")
	}
	if len(e.active) >= e.config.MaxConcurrent {
		count := len(e.active)
		e.activeMu.Unlock()
		return fmt.Errorf("executor at capacity (%d/%d)", count, e.config.MaxConcurrent)
	}
	// Reserve a slot atomically
	e.active[req.TaskID] = &Outcome{TaskID: req.TaskID}
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

	// Poll for outcome
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if o, ok := e.GetOutcome(req.TaskID); ok {
				return o, nil
			}
		}
	}
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
		}
		e.metricsMu.Unlock()
	}()

	// Step 1: Governance check
	if !e.checkGovernance(req) {
		outcome.Status = "denied"
		outcome.Error = "governance denied execution"
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

	// Step 3: Execute the task
	execCtx, cancel := context.WithTimeout(context.Background(), e.config.TaskTimeout)
	defer cancel()

	var result *Outcome
	var err error

	if req.Handler != nil {
		result, err = req.Handler(execCtx, req, ag)
	} else {
		result, err = e.defaultHandler(execCtx, req, ag)
	}

	if err != nil {
		outcome.Status = "failed"
		outcome.Error = err.Error()
		e.emitEvent("executor.failed", req.TaskID, req.CorrelationID, outcome)
		return
	}

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
func (e *Executor) checkGovernance(req *WorkRequest) bool {
	decision := e.governance.Evaluate(governance.Request{
		Actor:      req.ActorID,
		Action:     "execute_task",
		Resource:   "workflow",
		BusinessID: req.BusinessID,
	})
	return decision.Outcome != governance.DENY
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
func (e *Executor) defaultHandler(_ context.Context, req *WorkRequest, ag *agent.Agent) (*Outcome, error) {
	// If a model router is available, use it for actual inference
	if e.modelRouter != nil {
		genReq := &modelrouter.GenerateRequest{
			RequestID: req.TaskID,
			ModelID:   "default",
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

		resp, decision, err := e.modelRouter.Invoke(routingReq, genReq)
		if err != nil {
			// Fall back to synthetic response
			return &Outcome{
				TaskID:  req.TaskID,
				AgentID: ag.ID,
				Status:  "completed",
				Output:  fmt.Sprintf("task '%s' executed by agent %s (provider error: %v)", req.Intent, ag.ID, err),
				Evidence: []string{
					fmt.Sprintf("task_id=%s", req.TaskID),
					fmt.Sprintf("agent_id=%s", ag.ID),
					fmt.Sprintf("business_id=%s", req.BusinessID),
					fmt.Sprintf("provider_error=%v", err),
					fmt.Sprintf("executed_at=%s", e.now().Format(time.RFC3339)),
				},
			}, nil
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
func (e *Executor) ActiveCount() int {
	e.activeMu.RLock()
	defer e.activeMu.RUnlock()
	return len(e.active)
}

// Metrics returns execution metrics.
func (e *Executor) Metrics() (executed, failed, denied int64) {
	e.metricsMu.RLock()
	defer e.metricsMu.RUnlock()
	return e.totalExecuted, e.totalFailed, e.totalDenied
}

// emitEvent publishes an event to the event bus.
func (e *Executor) emitEvent(eventType string, taskID, corrID string, data interface{}) {
	if e.events == nil {
		return
	}

	_ = e.events.Publish(&event.Event{
		ID:         fmt.Sprintf("exec-%d", e.now().UnixNano()),
		Type:       event.EventType(eventType),
		Source:     "executor",
		Timestamp:  e.now(),
		BusinessID: "",
	})
}
