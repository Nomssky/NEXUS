package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/foundation/agent"
	"github.com/Nomssky/NEXUS/internal/foundation/attention"
	"github.com/Nomssky/NEXUS/internal/foundation/cognition"
	"github.com/Nomssky/NEXUS/internal/foundation/config"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/hardening"
	"github.com/Nomssky/NEXUS/internal/foundation/health"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/memory"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
	"github.com/Nomssky/NEXUS/internal/foundation/scheduler"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
	"github.com/Nomssky/NEXUS/internal/foundation/workflow"
)

const (
	// maxResults bounds the in-memory results map to prevent unbounded growth.
	maxResults = 1000
)

// Engine is the NEXUS Core Runtime — the central orchestrator that wires
// all foundation packages into a cohesive, running system.
type Engine struct {
	config *config.Config
	status lifecycle.State
	mu     sync.RWMutex
	now    func() time.Time

	// Foundation components
	eventBus *event.MemBus
	// store is the externally exposed persistent store (C-019).
	// It is intentionally NOT consumed by engine runtime: request results
	// use the bounded in-memory results map, and knowledge/memory uses
	// memoryStore. External consumers access it via Store(); configure
	// durable FileStore via WithPersistence, default MemStore otherwise.
	// Ownership: external API surface only — never engine-internal state.
	store        store.Store
	healthServer *health.Server

	// Governance
	govEngine *governance.Engine

	// Cognition
	objectiveEng *cognition.ObjectiveEngine
	decisionEng  *cognition.DecisionEngine
	planner      *cognition.Planner

	// Execution
	workflowEng  *workflow.WorkflowEngine
	scheduler    *scheduler.Scheduler
	agentRuntime *agent.AgentRuntime
	toolRegistry *tool.ToolRegistry
	taskExec     *executor.Executor

	// Intelligence
	modelRegistry *modelrouter.ModelRegistry
	modelRouter   *modelrouter.ModelRouter

	// Memory & Knowledge
	memoryStore *memory.MemoryStore

	// Attention
	attentionEng *attention.FullAttentionEngine

	// Hardening
	circuitBreaker *hardening.CircuitBreaker
	backpressure   *hardening.Backpressure
	recoveryMgr    *hardening.RecoveryManager

	// Processing
	requests    chan *Request
	results     map[string]*Response
	resultOrder []string // FIFO order for eviction
	resultsMu   sync.RWMutex

	// Persistence
	persistErr error

	// Shutdown
	shutdownCh   chan struct{}
	shutdownOnce sync.Once
	loopDone     chan struct{} // closed when processRequests exits
}

// EngineOption configures the engine.
type EngineOption func(*Engine)

// WithClock injects a clock for testing.
func WithClock(now func() time.Time) EngineOption {
	return func(e *Engine) { e.now = now }
}

// WithPersistence configures the engine to use a FileStore rooted at dir.
// Records survive engine restarts. Returns an error if the directory
// cannot be created or loaded. This configures the external Store()
// surface only — engine runtime does not auto-persist to it (C-019).
func WithPersistence(dir string) EngineOption {
	return func(e *Engine) {
		fs, err := store.NewFileStoreWithClock(dir, e.now)
		if err != nil {
			e.persistErr = err
			return
		}
		e.store = fs
	}
}

// NewEngine creates a new Core Runtime engine with all foundation components wired.
func NewEngine(cfg *config.Config, opts ...EngineOption) (*Engine, error) {
	e := &Engine{
		config:      cfg,
		status:      lifecycle.StateCreated,
		now:         time.Now,
		requests:    make(chan *Request, 100),
		results:     make(map[string]*Response),
		resultOrder: make([]string, 0, maxResults),
		shutdownCh:  make(chan struct{}),
		loopDone:    make(chan struct{}),
	}

	for _, opt := range opts {
		opt(e)
	}

	// Check for errors recorded during option application (e.g. persistence setup).
	if e.persistErr != nil {
		return nil, e.persistErr
	}

	// Wire foundation components
	e.eventBus = event.NewMemBus()
	if e.store == nil {
		e.store = store.NewMemStore()
	}
	e.healthServer = health.NewServer()

	// Governance — permissive default policy so the runtime can operate
	e.govEngine = governance.NewEngine([]*governance.Policy{
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

	// Cognition
	e.objectiveEng = cognition.NewObjectiveEngine()
	e.decisionEng = cognition.NewDecisionEngine()
	e.planner = cognition.NewPlanner()

	// Execution
	e.workflowEng = workflow.NewWorkflowEngine()
	e.scheduler = scheduler.NewScheduler()
	e.agentRuntime = agent.NewAgentRuntime()
	e.toolRegistry = tool.NewToolRegistry()

	// Intelligence (create before executor so it can use the router)
	e.modelRegistry = modelrouter.NewModelRegistry()
	e.modelRouter = modelrouter.NewModelRouter(e.modelRegistry, modelrouter.RoutingPolicyLocalFirst)

	e.taskExec = executor.New(
		e.agentRuntime,
		e.toolRegistry,
		e.govEngine,
		e.eventBus,
		e.modelRouter,
		executor.DefaultConfig(),
	)

	// Memory & Knowledge
	e.memoryStore = memory.NewMemoryStore()

	// Attention
	e.attentionEng = attention.NewFullAttentionEngine(
		attention.AttentionBudget{},
		attention.QuietHours{},
		attention.SuppressionGuard{},
	)

	// Hardening
	e.circuitBreaker = hardening.NewCircuitBreakerWithClock(5, 30*time.Second, e.now)
	e.backpressure = hardening.NewBackpressureWithClock(100, e.now)
	e.recoveryMgr = hardening.NewRecoveryManagerWithClock(e.now)

	return e, nil
}

// Status returns the current engine status.
func (e *Engine) Status() lifecycle.State {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.status
}

// Start begins the engine's processing loops.
func (e *Engine) Start(ctx context.Context) error {
	e.mu.Lock()
	if e.status != lifecycle.StateCreated {
		e.mu.Unlock()
		return fmt.Errorf("engine already started (status: %s)", e.status)
	}
	e.status = lifecycle.StateRunning
	e.mu.Unlock()

	// Register health check
	e.healthServer.RegisterCheck(health.Check{
		Name: "core",
		Probe: func(_ context.Context) error {
			return nil
		},
	})
	e.healthServer.MarkReady()

	// Start the task executor
	if err := e.taskExec.Start(ctx); err != nil {
		return fmt.Errorf("executor start: %w", err)
	}

	// Start request processing loop
	go e.processRequests(ctx)

	return nil
}

// Stop gracefully shuts down the engine.
func (e *Engine) Stop(_ context.Context) error {
	e.mu.Lock()
	if e.status != lifecycle.StateRunning {
		e.mu.Unlock()
		return nil
	}
	e.status = lifecycle.StateDraining
	e.mu.Unlock()

	e.shutdownOnce.Do(func() {
		close(e.shutdownCh)
	})

	// Stop the task executor
	e.taskExec.Stop(context.Background())

	// C-021 fix: wait for the processing loop to exit instead of
	// time.Sleep(50ms). Bounded wait — never hang forever.
	select {
	case <-e.loopDone:
	case <-time.After(5 * time.Second):
		// Loop did not exit in time; proceed with shutdown anyway.
	}

	e.mu.Lock()
	e.status = lifecycle.StateStopped
	e.mu.Unlock()

	return nil
}

// Resume resumes the engine from STOPPED state back to RUNNING.
// It creates a fresh shutdown channel and restarts the request processing loop.
func (e *Engine) Resume(ctx context.Context) error {
	e.mu.Lock()
	if e.status != lifecycle.StateStopped {
		e.mu.Unlock()
		return fmt.Errorf("cannot resume: engine not stopped (status: %s)", e.status)
	}
	// Create a fresh shutdown channel (the old one was closed during Stop)
	e.shutdownCh = make(chan struct{})
	e.loopDone = make(chan struct{})
	// Reset Once so the next Stop() closes the fresh shutdownCh —
	// previously the fired Once made Stop after Resume a no-op (latent bug).
	e.shutdownOnce = sync.Once{}
	e.status = lifecycle.StateRunning
	e.mu.Unlock()

	// Restart request processing loop
	go e.processRequests(ctx)

	return nil
}

// SubmitRequest submits a new request to the engine for processing.
func (e *Engine) SubmitRequest(req *Request) error {
	// Capture lifecycle state under the lock (C-018): e.status is written
	// by Stop()/Resume() under e.mu, and e.shutdownCh is rewritten by
	// Resume() under e.mu — reading either after RUnlock races with those
	// writers (processRequests uses the same capture pattern for
	// shutdownCh). The admission sequence status-check → Accept → send
	// remains a documented non-atomic window (see below).
	e.mu.RLock()
	status := e.status
	shutdownCh := e.shutdownCh
	e.mu.RUnlock()
	if status != lifecycle.StateRunning {
		return fmt.Errorf("engine not running (status: %s)", status)
	}

	// Backpressure gate: reject if queue is full.
	// Semantics (C-018): Accept() counts accepted-but-not-yet-dequeued requests.
	// The count briefly includes requests between Accept and channel enqueue —
	// intentional: they represent real incoming load. Exactly one Release()
	// follows per Accept (on dequeue or shutdown-reject), so the count cannot
	// leak. Over-counting under concurrency errs on the safe (rejecting) side.
	if !e.backpressure.Accept() {
		return fmt.Errorf("backpressure: queue full (rejected=%d)", e.backpressure.RejectedCount())
	}

	select {
	case e.requests <- req:
		return nil
	case <-shutdownCh:
		e.backpressure.Release()
		return fmt.Errorf("engine shutting down")
	}
}

// GetResult returns the result of a processed request.
func (e *Engine) GetResult(requestID string) (*Response, bool) {
	e.resultsMu.RLock()
	defer e.resultsMu.RUnlock()
	result, ok := e.results[requestID]
	return result, ok
}

// processRequests is the main processing loop.
func (e *Engine) processRequests(ctx context.Context) {
	// Signal Stop() when this loop exits (C-021: deterministic shutdown).
	e.mu.RLock()
	loopDone := e.loopDone
	e.mu.RUnlock()
	defer close(loopDone)

	for {
		// Read shutdown channel under lock to avoid racing with Resume().
		e.mu.RLock()
		shutdownCh := e.shutdownCh
		e.mu.RUnlock()

		select {
		case <-shutdownCh:
			return
		case <-ctx.Done():
			return
		case req := <-e.requests:
			// Release the backpressure slot immediately after dequeuing.
			// Accept() was called in SubmitRequest; exactly one Release()
			// must follow per Accept — never skip, never duplicate.
			e.backpressure.Release()

			result := e.executeChain(ctx, req)
			e.resultsMu.Lock()
			e.results[req.ID] = result
			e.resultOrder = append(e.resultOrder, req.ID)
			// Evict oldest entry if over capacity. The FIFO slice makes
			// each eviction O(1) — only one entry is removed per insert,
			// and the 1000-entry cap keeps total memory bounded.
			for len(e.resultOrder) > maxResults {
				delete(e.results, e.resultOrder[0])
				e.resultOrder = e.resultOrder[1:]
			}
			e.resultsMu.Unlock()

			// Emit completion event
			_ = e.eventBus.Publish(&event.Event{
				ID:         req.ID,
				Type:       event.EventType("request.completed"),
				Source:     "core",
				Timestamp:  e.now(),
				BusinessID: req.Context.BusinessID,
			})
		}
	}
}

// HealthStatus returns the engine's health status string.
func (e *Engine) HealthStatus() health.Status {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.status == lifecycle.StateRunning {
		return health.StatusUp
	}
	return health.StatusDown
}

// EventBus returns the engine's event bus for external integration.
func (e *Engine) EventBus() *event.MemBus {
	return e.eventBus
}

// Store returns the engine's persistent store for external consumers.
// Engine runtime does not read or write this store — it is an
// external-facing API (C-019), configured by WithPersistence or
// defaulting to MemStore. Never nil after NewEngine succeeds.
func (e *Engine) Store() store.Store {
	return e.store
}

// ToolRegistry returns the engine's tool registry.
func (e *Engine) ToolRegistry() *tool.ToolRegistry {
	return e.toolRegistry
}

// ModelRegistry returns the engine's model registry (C-020: was unexported).
func (e *Engine) ModelRegistry() *modelrouter.ModelRegistry {
	return e.modelRegistry
}

// ModelRouter returns the engine's model router (C-020: was unexported).
func (e *Engine) ModelRouter() *modelrouter.ModelRouter {
	return e.modelRouter
}

// AgentRuntime returns the engine's agent runtime.
func (e *Engine) AgentRuntime() *agent.AgentRuntime {
	return e.agentRuntime
}

// TaskExecutor returns the engine's task executor.
func (e *Engine) TaskExecutor() *executor.Executor {
	return e.taskExec
}

// CircuitBreaker returns the engine's circuit breaker for external inspection.
func (e *Engine) CircuitBreaker() *hardening.CircuitBreaker {
	return e.circuitBreaker
}

// Backpressure returns the engine's backpressure controller for external inspection.
func (e *Engine) Backpressure() *hardening.Backpressure {
	return e.backpressure
}

// RecoveryManager returns the engine's recovery manager for external inspection.
func (e *Engine) RecoveryManager() *hardening.RecoveryManager {
	return e.recoveryMgr
}
