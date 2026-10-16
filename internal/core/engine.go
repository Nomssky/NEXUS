package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/foundation/agent"
	"github.com/Nomssky/NEXUS/internal/foundation/attention"
	"github.com/Nomssky/NEXUS/internal/foundation/cognition"
	"github.com/Nomssky/NEXUS/internal/foundation/config"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/hardening"
	"github.com/Nomssky/NEXUS/internal/foundation/health"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
	"github.com/Nomssky/NEXUS/internal/foundation/memory"
	"github.com/Nomssky/NEXUS/internal/foundation/metrics"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
	"github.com/Nomssky/NEXUS/internal/foundation/scheduler"
	"github.com/Nomssky/NEXUS/internal/foundation/schema"
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
	// traces is the bounded, redacted, process-local execution-trace
	// registry fed synchronously by the bus tap (EXECUTION_TRACE_CONTRACTS
	// §3, §11). A view only — never a source of authority.
	traces *traceRegistry
	// store is the externally exposed persistent store (C-019).
	// It is intentionally NOT consumed by engine runtime: request results
	// use the bounded in-memory results map, and knowledge/memory uses
	// memoryStore. External consumers access it via Store(); configure
	// durable FileStore via WithPersistence, default MemStore otherwise.
	// Ownership: external API surface only — never engine-internal state.
	store        store.Store
	healthServer *health.Server

	// journal is the durable execution journal (v10). Nil = no durable
	// execution evidence (in-memory/dev installs). It records acceptance
	// before the caller is told the execution is durable, and the terminal
	// state after the result is stored.
	journal *execjournal.Journal

	// metrics is the optional, bounded, process-local operational metrics owner
	// (contracts/OPERATIONAL_OBSERVABILITY_CONTRACTS.md). Nil disables metric
	// instrumentation (a safe no-op); metrics never block execution.
	metrics *metrics.Metrics

	// Governance
	govEngine *governance.Engine

	// Admission pipeline identity stages (A: RUNTIME §3.1 stages 3-4).
	// Enforcement mirrors the A6 gateway posture: both flags off = stages
	// record a not-enforced pass (process-boundary trust); either flag on =
	// fail closed on a missing store or unknown actor (RT-02 default deny).
	// The chain never verifies credentials (that is the gateway's A6
	// boundary) — it re-checks the already-bound actor against
	// foundation/identity as defense in depth.
	identityMemberships  *identity.MembershipSet
	identityRequireAuth  bool
	identityEnforceScope bool

	// Approval wiring (P1): validator + core-side resume index.
	// approvalMu is a leaf lock — it may call into approvalEngine (which
	// has its own lock) but is never held while acquiring e.mu/resultsMu/
	// inflightMu.
	approvalEngine *governance.ApprovalEngine
	approvalMu     sync.Mutex
	approvals      map[string]*approvalEntry // approvalID → entry
	approvalByReq  map[string]string         // requestID → approvalID
	// actionApprovals indexes action-level approvals by
	// "requestID|fingerprint" -> approvalID. It is separate from approvalByReq
	// because one objective may need several distinct action approvals
	// (AGENT_GOVERNANCE_CONTROL_CONTRACTS section 5).
	actionApprovals *actionApprovalIndex
	// cancelledReqs records terminally cancelled request IDs so a late
	// approval can never revive them (APPROVAL_LIFECYCLE_CONTRACTS §8).
	// Bounded FIFO like the consumed ledgers; guarded by approvalMu.
	cancelledReqs  map[string]struct{}
	cancelledOrder []string
	// requestRefs is a plain reference index of admitted/in-flight requests, so
	// an action-level approval raised inside one has something to resume. No
	// lifecycle, no authorization, no durability (G4).
	requestRefs   map[string]*Request
	requestRefsMu sync.RWMutex

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
	// execConfig feeds executor construction; MaxConcurrent backs the
	// RESOURCE CHECK admission stage (A: RUNTIME §3.1 stage 7).
	execConfig executor.Config

	// Intelligence
	modelRegistry *modelrouter.ModelRegistry
	modelRouter   *modelrouter.ModelRouter

	// Memory & Knowledge
	memoryStore *memory.MemoryStore

	// Attention
	attentionEng *attention.FullAttentionEngine
	// escalations is the CTR-GOV-002 escalation queue (governance → attention).
	escalations *escalationQueue

	// Hardening
	circuitBreaker *hardening.CircuitBreaker
	backpressure   *hardening.Backpressure
	recoveryMgr    *hardening.RecoveryManager

	// Processing
	requests    chan *Request
	results     map[string]*Response
	resultOrder []string // FIFO order for eviction
	resultsMu   sync.RWMutex

	// In-flight registry (E-005): requestID -> admission/cancellation state.
	// Leaf lock — see cancel.go for the locking rules.
	inflightMu sync.RWMutex
	inflight   map[string]*inflightRequest

	// admitMu makes an admission's registration (inflight + requestRefs) and a
	// finished run's teardown of the same state mutually exclusive, so a run
	// that finishes can never remove the registration a resume re-established
	// for it. Ordered outer of inflightMu/requestRefsMu; never held across a
	// blocking operation (the resume slot/queue send happen outside it).
	admitMu sync.Mutex

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

// WithExecutionJournal wires the durable execution journal (v10). With a
// journal set, SubmitRequest persists durable acceptance before returning and
// the processing loop persists the terminal state. A nil journal (the default)
// leaves execution state process-local.
func WithExecutionJournal(j *execjournal.Journal) EngineOption {
	return func(e *Engine) { e.journal = j }
}

// WithMetrics wires the optional operational metrics owner (v15). A nil metrics
// (the default) leaves instrumentation a no-op.
func WithMetrics(m *metrics.Metrics) EngineOption {
	return func(e *Engine) { e.metrics = m }
}

// WithIdentity wires the membership store and enforcement posture for the
// chain's IDENTITY and AUTHORIZATION admission stages (RUNTIME §3.1 stages
// 3-4). requireAuth verifies the bound actor is a known identity;
// enforceScope requires an active membership in the request's business.
// Both false = not-enforced pass (A6 enforcement-off posture); either true =
// fail closed (AUTH / AUTHORIZATION) on a missing store or unknown actor
// (RT-02 default deny). Credentials are never verified here — that is the
// gateway's A6 boundary; the chain re-checks the already-bound actor.
func WithIdentity(members *identity.MembershipSet, requireAuth, enforceScope bool) EngineOption {
	return func(e *Engine) {
		e.identityMemberships = members
		e.identityRequireAuth = requireAuth
		e.identityEnforceScope = enforceScope
	}
}

// WithExecutorConfig overrides the task executor configuration (defaults to
// executor.DefaultConfig()). Its MaxConcurrent feeds the RESOURCE CHECK
// admission stage (RUNTIME §3.1 stage 7).
func WithExecutorConfig(cfg executor.Config) EngineOption {
	return func(e *Engine) { e.execConfig = cfg }
}

// NewEngine creates a new Core Runtime engine with all foundation components wired.
func NewEngine(cfg *config.Config, opts ...EngineOption) (*Engine, error) {
	e := &Engine{
		config:          cfg,
		status:          lifecycle.StateCreated,
		now:             time.Now,
		requests:        make(chan *Request, 100),
		results:         make(map[string]*Response),
		resultOrder:     make([]string, 0, maxResults),
		inflight:        make(map[string]*inflightRequest),
		approvals:       make(map[string]*approvalEntry),
		approvalByReq:   make(map[string]string),
		actionApprovals: newActionApprovalIndex(),
		cancelledReqs:   make(map[string]struct{}),
		requestRefs:     make(map[string]*Request),
		traces:          newTraceRegistry(),
		shutdownCh:      make(chan struct{}),
		loopDone:        make(chan struct{}),
		execConfig:      executor.DefaultConfig(),
	}

	for _, opt := range opts {
		opt(e)
	}

	// Check for errors recorded during option application (e.g. persistence setup).
	if e.persistErr != nil {
		return nil, e.persistErr
	}

	// Wire foundation components. The bus carries the synchronous
	// publish-time tap that feeds the trace registry (contract §3), so
	// capture is complete before any Publish call returns.
	e.eventBus = event.NewMemBusWithTap(e.traces.record)
	if e.store == nil {
		e.store = store.NewMemStore()
	}
	e.healthServer = health.NewServer()

	// Governance — permissive default policy so the runtime can operate.
	// It is a full §2.2 record because the control API reads it back verbatim;
	// policy_id is the exported DefaultAllowPolicyID so the gateway can
	// protect it (contracts §9.4) without a magic string. Tests construct the
	// engine with a nil config, so the installation id is read defensively.
	seededAt := e.now()
	seededNexusID := ""
	if cfg != nil {
		seededNexusID = cfg.Nexus.ID
	}
	e.govEngine = governance.NewEngine([]*governance.Policy{
		{
			SchemaVersion: schema.Version,
			EntityType:    "policy",
			PolicyID:      governance.DefaultAllowPolicyID,
			PolicyVersion: "1",
			NexusID:       seededNexusID,
			PolicyType:    governance.PolicyTypeAccessControl,
			Name:          "Default Allow",
			Description:   "Seeded built-in policy: allows every request so an unconfigured installation does not fall through to the engine default-DENY (SCHEMA_GOVERNANCE §2.8).",
			Status:        governance.PolicyStatusActive,
			Subject:       governance.Subject{SubjectType: "all"},
			Action:        governance.Action{ActionType: "custom"},
			Resource:      governance.Resource{ResourceType: "all"},
			Effect:        governance.ALLOW,
			Precedence:    0,
			EffectiveFrom: seededAt,
			CreatedAt:     seededAt,
			CreatedBy:     "system",
			Provenance: schema.ProvenanceRef{
				Origin:     "system",
				Producer:   "core.engine",
				ProducedAt: seededAt,
			},
		},
	})
	// Approval validator for REQUIRE_APPROVAL outcomes — shares the engine
	// clock so timeout sweeps are deterministic under WithClock tests.
	e.approvalEngine = governance.NewApprovalEngineWithClock(e.now)

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
		e.execConfig,
	)

	// Memory & Knowledge
	e.memoryStore = memory.NewMemoryStore()

	// Attention
	e.attentionEng = attention.NewFullAttentionEngineWithClock(
		attention.AttentionBudget{},
		attention.QuietHours{},
		attention.SuppressionGuard{},
		e.now,
	)

	// Escalation queue + CTR-GOV-002 intake: the bus consumer acks
	// governance.escalated by queueing (D3 handoff, contract output
	// { escalation_id }). The consumer lives as long as the bus, which the
	// engine owns — both are released together.
	e.escalations = newEscalationQueue(e.now)
	if _, err := e.eventBus.Subscribe(
		event.ConsumerFunc(e.handleGovernanceEscalated),
		event.EventTypeGovernanceEscalated,
	); err != nil {
		return nil, fmt.Errorf("core: subscribe escalation intake: %w", err)
	}

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
// It creates a fresh shutdown channel, restarts the task executor (Stop
// stopped it — without this every post-resume request died with
// "executor not running") and restarts the request processing loop.
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
	e.mu.Unlock()

	// C-3: restart the task executor BEFORE flipping to RUNNING, so a failed
	// restart leaves the engine cleanly STOPPED instead of accepting work it
	// cannot execute.
	if err := e.taskExec.Start(ctx); err != nil {
		return fmt.Errorf("executor restart: %w", err)
	}

	e.mu.Lock()
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

	// E-005: register before the channel send so a cancellation can always
	// find the request (queued or executing) from the moment it is admitted.
	// admission registration is serialized with a finishing run's teardown
	// (admitMu) so neither can clobber the other.
	e.admitMu.Lock()
	e.registerInflight(req)
	// Index the request so an action-level approval raised inside it can bind
	// to it and resume it (AGENT_GOVERNANCE_CONTROL_CONTRACTS section 5).
	e.trackRequestRef(req)
	e.admitMu.Unlock()

	// Durable acceptance (v10): persist the accepted execution BEFORE the
	// caller is told the execution is durable. If this fails, the request is
	// not enqueued and the caller gets an error — no 202 without a durable
	// record.
	if e.journal != nil {
		var biz, div, actor string
		if req.Context != nil {
			biz, div, actor = req.Context.BusinessID, req.Context.DivisionID, req.Context.ActorID
		}
		if jerr := e.journal.Accepted(req.ID, biz, div, actor); jerr != nil {
			e.metrics.JournalWriteFailure()
			e.backpressure.Release()
			e.deregisterInflight(req.ID)
			e.untrackRequestRef(req)
			return fmt.Errorf("durable acceptance failed: %w", jerr)
		}
	}

	select {
	case e.requests <- req:
		// Admitted and durably accepted: one logical execution is now in flight.
		e.metrics.ExecutionAccepted()
		return nil
	case <-shutdownCh:
		e.backpressure.Release()
		// Send never happened — drop the registration; exactly one Release
		// follows the Accept above, none is skipped or duplicated. The durable
		// record remains "accepted"; recovery classifies it as unknown (safe,
		// never replayed).
		e.deregisterInflight(req.ID)
		e.untrackRequestRef(req)
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

			// Capture the approvals this run is authorized by BEFORE it runs
			// (approval.go: approvedApprovalsFor / cleanupConsumedApprovals):
			// an approval granted in the window between this run's terminal
			// store and its cleanup belongs to the resume it triggers, never
			// to this run. Capturing here rather than keying cleanup off the
			// current status is what keeps a concurrent approve from being
			// deleted out from under its own resume.
			consumedApprovals := e.approvedApprovalsFor(req.ID)
			// Ownership token for the in-flight registration/deregistration
			// pairing (see the guarded teardown below).
			infAtDequeue := e.inflightRef(req.ID)

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

			// Durable terminal state (v10): record the terminal outcome after
			// the result is stored. A write failure leaves the record
			// nonterminal so recovery classifies it as unknown — never a
			// fabricated completion.
			if e.journal != nil {
				if terr := e.journal.Terminal(req.ID, journalState(result), journalErrorClass(result)); terr != nil {
					e.metrics.JournalWriteFailure()
				}
			}

			// Operational metrics (v15, process-local, bounded): close the run's
			// in-flight interval, record the exact terminal status, classify a
			// failure by its coarse code, and count a stored approval wait. These
			// never change execution semantics and never block.
			e.metrics.ExecutionFinished()
			e.metrics.TerminalOutcome(result.Status)
			if result.Status == "failed" {
				e.metrics.TerminalErrorClass(journalErrorClass(result))
			}
			if isApprovalWaiting(result) {
				e.metrics.ApprovalPaused()
			}

			// E-005 teardown, serialized with resume re-admission (admitMu): a run
			// releases the in-flight registration and the request reference ONLY
			// while it still owns them. A resume admitted in the window between
			// the terminal store and here re-registered the request; removing
			// that registration would blank the request out of the registry
			// (GetResult and Pending both miss → a concurrent reader sees 404)
			// and drop the reference the resume's action admission needs
			// (APPROVAL_LIFECYCLE §8 / AGENT_GOVERNANCE_CONTROL §5).
			e.admitMu.Lock()
			if e.inflightRef(req.ID) == infAtDequeue {
				e.deregisterInflight(req.ID)
				e.untrackRequestRef(req)
			}
			e.admitMu.Unlock()

			// Approval resume: release exactly the approvals this run was
			// authorized by (captured at dequeue). An approval granted
			// concurrently with this run's completion is left for the resume it
			// triggers; deleting by id never touches it.
			e.cleanupConsumedApprovals(req.ID, consumedApprovals)

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

// ExecutionJournal returns the durable execution journal, or nil when no
// durable store is configured. The v11 read API uses it to expose durable
// lifecycle evidence (contracts/DURABLE_EXECUTION_READ_AND_RECONCILIATION_CONTRACTS.md).
func (e *Engine) ExecutionJournal() *execjournal.Journal {
	return e.journal
}

// MetricsSnapshot returns a bounded, process-local operational metrics view
// (contracts/OPERATIONAL_OBSERVABILITY_CONTRACTS.md). It is nil-safe (an empty
// snapshot when no metrics owner is wired) and performs no I/O — it never
// touches the store or the journal, so it cannot recursively depend on a
// failing subsystem.
func (e *Engine) MetricsSnapshot() metrics.Snapshot {
	return e.metrics.Snapshot()
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

// Governance returns the governance engine for external policy management
// (loading/refreshing policies at runtime — same export rationale as C-020's
// ModelRegistry/ModelRouter accessors).
func (e *Engine) Governance() *governance.Engine {
	return e.govEngine
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
