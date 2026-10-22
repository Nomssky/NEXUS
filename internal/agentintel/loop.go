package agentintel

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Nomssky/NEXUS/internal/agentexec"
	"github.com/Nomssky/NEXUS/internal/control"
	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/foundation/agent"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
	"github.com/Nomssky/NEXUS/internal/memory"
	"github.com/Nomssky/NEXUS/internal/researchquality"
)

// Decision is the runtime's bounded adapter over the model provider: it is
// the only place intelligence code talks to a model, and it always asks for a
// structured answer (JSON) through the provider abstraction.
type Decision struct {
	Router  *modelrouter.ModelRouter
	AgentID string
	Now     func() time.Time
}

func (d *Decision) now() time.Time {
	if d != nil && d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// Decide asks the model for the next structured action (§6, protocol v2).
func (d *Decision) Decide(ctx context.Context, p DecisionPrompt) (string, error) {
	if d == nil || d.Router == nil {
		return "", fmt.Errorf("no model router wired")
	}
	return d.invoke(ctx, p.StepID, buildDecisionEnvelope(p), DecisionStructuredRequest())
}

// Plan asks the model for an advisory plan (§3, protocol v2).
func (d *Decision) Plan(ctx context.Context, p PlanPrompt) (string, error) {
	if d == nil || d.Router == nil {
		return "", fmt.Errorf("no model router wired")
	}
	return d.invoke(ctx, "plan", buildPlanEnvelope(p), PlanStructuredRequest())
}

func (d *Decision) invoke(ctx context.Context, id string, msgs []ModelMessage, structured *modelrouter.StructuredOutputSchema) (string, error) {
	routeReq := &modelrouter.RoutingRequest{
		RequestID:       id,
		AgentID:         d.AgentID,
		RequiredCaps:    []modelrouter.ModelCapability{modelrouter.CapabilityStructuredOutput},
		FallbackEnabled: true,
	}
	gen := &modelrouter.GenerateRequest{
		RequestID:      id,
		Messages:       toProviderMessages(msgs),
		ResponseFormat: "json",
		Structured:     structured,
		// Bounded, but large enough for a bounded plan with explicit operations
		// and a synthesized answer with several citations. MaxDecisionBytes
		// remains the hard wire-shape bound on what the runtime accepts.
		MaxTokens: 2048,
	}
	resp, _, err := d.Router.Invoke(ctx, routeReq, gen)
	if err != nil {
		return "", fmt.Errorf("provider invocation failed: %w", err)
	}
	return resp.Content, nil
}

// Runtime is the bounded control loop (§4). It runs as an executor handler
// inside an admitted request: identity/scope/governance/approval/cancel are
// inherited; the loop only decides what an already authorized agent does.
type Runtime struct {
	Registry *agentexec.Registry
	Exec     *agentexec.Runtime // tool + delegation primitives (reused, not reimplemented)
	Decider  DecisionRequester
	Planner  Planner
	Bus      *event.MemBus
	Caps     Caps
	Now      func() time.Time
	// Memory is the single durable agent-memory platform
	// (AGENT_MEMORY_CONTEXT_CONTRACTS). Optional: without it the loop performs no
	// memory operation and assembles context from execution state only.
	Memory *MemoryPlatform
	// Assembler is the single context-assembly boundary. Optional: the shipped
	// default is used when nil.
	Assembler *memory.Assembler
	// Catalog provides the bounded capability catalog for the prompt
	// (CAPABILITY_TOOL_CONTRACTS §4). Optional: without it the prompt carries
	// no catalog and tool calls are still validated by the platform.
	Catalog CatalogProvider

	mu     sync.Mutex
	states map[string]State
	// resumes holds waiting-execution snapshots for approval resume,
	// keyed by execution id (APPROVAL_LIFECYCLE_CONTRACTS §6). Bounded
	// FIFO, consumed on load, verified before use.
	resumes     map[string]*runSnapshot
	resumeOrder []string
}

// New wires a runtime with the shipped bounded defaults.
func New(reg *agentexec.Registry, exec *agentexec.Runtime, decider DecisionRequester, bus *event.MemBus, mem *MemoryPlatform) *Runtime {
	return &Runtime{
		Registry: reg, Exec: exec, Decider: decider, Bus: bus, Caps: DefaultCaps(),
		Now: time.Now, Memory: mem, Assembler: memory.NewAssembler(memory.DefaultContextBudget()),
		states: map[string]State{},
	}
}

// assembler returns the effective context assembler.
func (r *Runtime) assembler() *memory.Assembler {
	if r.Assembler != nil {
		return r.Assembler
	}
	return memory.NewAssembler(memory.DefaultContextBudget())
}

func (r *Runtime) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// State returns the last recorded loop state for an execution id (observable
// for tests and diagnostics; the events carry the same information).
func (r *Runtime) State(executionID string) State {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.states[executionID]
}

func (r *Runtime) setState(id string, s State) {
	r.mu.Lock()
	r.states[id] = s
	r.mu.Unlock()
}

// run carries the per-execution tracing identity. Every event keeps
// request/correlation, business and division ids (§26 of the contract).
type run struct {
	rt         *Runtime
	req        *executor.WorkRequest
	execID     string
	businessID string
	divisionID string
	ag         *agent.Agent
	// depth is the intelligence delegation depth of this execution: gateway
	// roots run at 0, and every delegation validates depth+1 against the
	// budget before creating a child (delegation contract §8).
	depth int
	// planVersion is the active plan version, maintained by the loop across
	// replans. Event metadata reads it; the model never sets it.
	planVersion int
	// plan and ledger are the active runtime-owned plan snapshot. They are
	// attached to the durable evidence only at terminal/pause time; evidence
	// capture itself carries the step/operation attribution known when the
	// mediated search ran.
	plan   *Plan
	ledger *planLedger
	// researchEvidence accumulates only complete, validated web.search
	// snapshots from THIS execution. evidenceOverflow latches when the bound is
	// exceeded; the snapshot is then dropped rather than exposing a partial
	// list as complete.
	researchEvidence *execjournal.ResearchEvidence
	evidenceOverflow bool
	// question is the parsed objective description for this execution. It is
	// used only for deterministic lexical relevance checks; it is never added
	// to durable evidence or the evaluation record.
	question string
	// researchAttempted is true when this execution invoked web.search, even
	// if every envelope was malformed or no complete snapshot was captured.
	// It lets evaluation distinguish "not research" from "research input
	// unavailable" without inventing a source list.
	researchAttempted bool
	// currentStepID is the step that was executing when the run settled. A
	// completed run settles that step through the completion action, so its
	// ledger status can still read "executing"; the evaluation excludes it
	// from plan completeness exactly like the runtime's own completion gate.
	currentStepID string
	// synthesis is the validated typed completion answer for this execution.
	// It is process-local (never journaled) and only attached to a completed
	// outcome by the terminal defer.
	synthesis *executor.ResearchSynthesis
}

// captureResearchEvidence merges one complete web.search snapshot into this
// execution's evidence. It never mixes records across executions: the run
// object — and therefore the accumulator — belongs to exactly one execution.
// If the combined list would exceed the durable bound, the whole snapshot is
// dropped and no later capture can resurrect a partial list.
//
// stepID and observationID are the stable execution-scoped identities known at
// the moment the mediated tool call ran. A source is associated with a step
// only through this operation record; nothing is inferred later from source
// order, titles, URLs or timestamps.
func (rn *run) captureResearchEvidence(ev *execjournal.ResearchEvidence, stepID, observationID string) {
	if rn == nil || ev == nil || rn.evidenceOverflow ||
		ev.Version != execjournal.ResearchEvidenceVersion {
		return
	}
	if rn.researchEvidence != nil &&
		rn.researchEvidence.Version != execjournal.ResearchEvidenceVersion {
		// A resumed v1 snapshot cannot be merged: legacy source rows have no
		// ids, and assigning them now would invent identity. Keep the persisted
		// legacy evidence readable and do not claim provenance for new searches.
		return
	}
	if stepID != "" && observationID != "" && rn.hasResearchOperation(observationID) {
		return // repeated delivery of the same mediated operation
	}
	if rn.researchEvidence == nil {
		copy := *ev
		// Keep the JSON representation an array even for a complete search
		// that returned zero sources; null would be indistinguishable from
		// evidence absent on the read side.
		copy.Sources = []execjournal.ResearchSource{}
		copy.Plan, copy.Steps, copy.Operations = nil, nil, nil
		rn.researchEvidence = &copy
	}
	combined := len(rn.researchEvidence.Sources) + len(ev.Sources)
	if combined > execjournal.MaxResearchEvidenceSources {
		rn.researchEvidence = nil
		rn.evidenceOverflow = true
		return
	}
	if stepID != "" && observationID != "" &&
		len(rn.researchEvidence.Operations)+1 > execjournal.MaxResearchOperations {
		rn.researchEvidence = nil
		rn.evidenceOverflow = true
		return
	}
	offset := len(rn.researchEvidence.Sources)
	sourceIDs := make([]string, 0, len(ev.Sources))
	for i, source := range ev.Sources {
		source.ID = fmt.Sprintf("src-%d", offset+i+1)
		rn.researchEvidence.Sources = append(rn.researchEvidence.Sources, source)
		sourceIDs = append(sourceIDs, source.ID)
	}
	rn.researchEvidence.Count = len(rn.researchEvidence.Sources)
	if stepID != "" && observationID != "" {
		rn.researchEvidence.Operations = append(rn.researchEvidence.Operations,
			execjournal.ResearchOperationEvidence{
				OperationID: observationID,
				StepID:      stepID,
				ToolID:      execjournal.ResearchEvidenceCapability,
				Operation:   execjournal.ResearchEvidenceOperation,
				SourceIDs:   sourceIDs,
			})
	}
}

func (rn *run) hasResearchOperation(operationID string) bool {
	if rn == nil || rn.researchEvidence == nil {
		return false
	}
	for _, op := range rn.researchEvidence.Operations {
		if op.OperationID == operationID {
			return true
		}
	}
	return false
}

// finalizeResearchEvidence attaches the active plan/step snapshot to the
// evidence. It runs immediately before the outcome hand-off and again before
// an approval snapshot, because a paused run must resume with the same
// authoritative step state it durably recorded. The snapshot is final plan
// order; operations whose step no longer exists are dropped (their sources
// remain explicitly unattributed rather than guessed onto another step).
func (rn *run) finalizeResearchEvidence(plan *Plan, ledger *planLedger) {
	if rn == nil || rn.researchEvidence == nil ||
		rn.researchEvidence.Version != execjournal.ResearchEvidenceVersion {
		return
	}
	ev := rn.researchEvidence
	operations := ev.Operations
	ev.Plan, ev.Steps, ev.Operations = nil, nil, nil
	if plan == nil || ledger == nil {
		return
	}
	ev.Plan = &execjournal.ResearchPlanEvidence{
		PlanID: plan.PlanID, Version: plan.Version,
	}
	allowed := make(map[string]struct{}, len(plan.Steps))
	for _, step := range plan.Steps {
		rec := ledger.get(step.StepID)
		if rec == nil {
			// A plan step with no ledger record has no provable lifecycle
			// state. Do not invent one; an operation for it is dropped below
			// and its sources stay unattributed.
			continue
		}
		ev.Steps = append(ev.Steps, execjournal.ResearchStepEvidence{
			StepID:        step.StepID,
			Dependencies:  append([]string(nil), step.Dependencies...),
			Status:        string(rec.Status),
			ObservationID: rec.ObservationID,
		})
		allowed[step.StepID] = struct{}{}
	}
	if len(ev.Steps) == 0 {
		// A plan with no provable steps cannot satisfy the evidence contract.
		// Keep the source records, but expose no invented plan identity.
		ev.Plan = nil
		return
	}
	for _, op := range operations {
		if _, ok := allowed[op.StepID]; !ok {
			continue
		}
		ev.Operations = append(ev.Operations, op)
	}
	// Model-authored plan identifiers are validated as bounded, non-empty and
	// schema-shaped by the plan validator. If the settled provenance still
	// cannot satisfy the durable evidence contract, drop the provenance rather
	// than poisoning the terminal write; the source records remain honest but
	// unattributed.
	if err := execjournal.ValidateResearchEvidenceForExecution(ev.Plan.PlanID, ev); err != nil {
		ev.Plan, ev.Steps, ev.Operations = nil, nil, nil
		if err := execjournal.ValidateResearchEvidence(ev); err != nil {
			// The source rows themselves cannot be represented honestly.
			// Drop the whole snapshot rather than fail the terminal write.
			rn.researchEvidence = nil
		}
	}
}

// evaluateResearch builds the deterministic quality verdict from this run's
// final plan, evidence and synthesis. The evaluator itself is pure; this method
// only maps runtime-owned facts into its neutral input.
func (rn *run) evaluateResearch(status string) *execjournal.ResearchEvaluation {
	if rn == nil {
		return nil
	}
	if !rn.researchAttempted && rn.researchEvidence == nil && rn.synthesis == nil {
		return nil
	}
	steps := make([]researchquality.Step, 0)
	if rn.plan != nil {
		steps = make([]researchquality.Step, 0, len(rn.plan.Steps))
		for _, step := range rn.plan.Steps {
			stepStatus := ""
			if rn.ledger != nil {
				if rec := rn.ledger.get(step.StepID); rec != nil {
					stepStatus = string(rec.Status)
				}
			}
			steps = append(steps, researchquality.Step{
				StepID:   step.StepID,
				Intent:   step.Intent,
				Status:   stepStatus,
				Optional: step.Optional,
			})
		}
	}
	currentStepID := ""
	if status == "completed" {
		// A completed run settles the current step through the completion
		// action, so its ledger status may still read "executing".
		currentStepID = rn.currentStepID
	}
	return researchquality.Evaluate(researchquality.Input{
		ExecutionID:   rn.execID,
		Question:      rn.question,
		Evidence:      rn.researchEvidence,
		Synthesis:     rn.synthesis,
		Plan:          steps,
		CurrentStepID: currentStepID,
		Attempted:     rn.researchAttempted || rn.researchEvidence != nil || rn.synthesis != nil,
	})
}

// validateCompletionSynthesis enforces the completion gate for research
// evidence. v2 evidence requires a typed synthesis; v1 evidence has no stable
// source ids and must not be upgraded into one. The model's synthesis is only
// a proposal until executor.ValidateResearchSynthesis resolves every citation
// against this run's current evidence and checks status against the plan facts.
func (rn *run) validateCompletionSynthesis(s *executor.ResearchSynthesis, plan *Plan, ledger *planLedger, currentStepID string) error {
	ev := rn.researchEvidence
	if s == nil {
		if ev != nil && ev.Version == execjournal.ResearchEvidenceVersion {
			return fmt.Errorf("evidence was captured but the complete action has no synthesis")
		}
		return nil
	}
	if ev == nil {
		return fmt.Errorf("synthesis requires evidence captured by this execution")
	}
	if ev.Version != execjournal.ResearchEvidenceVersion {
		return fmt.Errorf("synthesis requires v2 evidence with stable source ids")
	}
	return executor.ValidateResearchSynthesis(s, ev, completionIsPartial(plan, ledger, currentStepID))
}

// completionIsPartial is the runtime's verdict on the active plan at the
// moment a complete action is validated. The currently executing step is
// excluded: its completion is the action being validated. Any other step that
// has not completed (including blocked/failed/dropped optional steps) makes a
// `complete` synthesis status dishonest.
func completionIsPartial(plan *Plan, ledger *planLedger, currentStepID string) bool {
	if plan == nil || ledger == nil {
		return false
	}
	for _, step := range plan.Steps {
		if step.StepID == currentStepID {
			continue
		}
		rec := ledger.get(step.StepID)
		if rec == nil || rec.Status != StepCompleted {
			return true
		}
	}
	return false
}

func (rn *run) emit(ev event.EventType, extra map[string]string) {
	if rn.rt.Bus == nil {
		return
	}
	data := map[string]string{"event": string(ev)}
	for k, v := range extra {
		data[k] = v
	}
	payload, _ := jsonString(data)
	_ = rn.rt.Bus.Publish(&event.Event{
		ID:            fmt.Sprintf("%s-%s-%d", rn.execID, ev, rn.rt.now().UnixNano()),
		Type:          ev,
		Source:        "agentintel",
		Timestamp:     rn.rt.now(),
		BusinessID:    rn.businessID,
		CorrelationID: rn.execID,
		Priority:      event.PriorityNormal,
		Data:          payload,
	})
}

func (rn *run) transition(s State, extra map[string]string) {
	rn.rt.setState(rn.execID, s)
	e := map[string]string{"state": string(s)}
	for k, v := range extra {
		e[k] = v
	}
	rn.emit(stateEvent(s), e)
}

// Handler returns the executor.TaskHandler for objective executions.
func (r *Runtime) Handler() executor.TaskHandler {
	return func(ctx context.Context, req *executor.WorkRequest, ag *agent.Agent) (*executor.Outcome, error) {
		return r.Run(ctx, req, ag)
	}
}

// Run executes the control loop for one objective request and returns the
// canonical executor outcome. Failure never becomes success: every terminal
// state other than completed/cancelled is a failed outcome whose message
// names the terminal state, and the state itself is echoed in the summary.
func (r *Runtime) Run(ctx context.Context, req *executor.WorkRequest, ag *agent.Agent) (out *executor.Outcome, err error) {
	start := r.now()
	obj, budget, err := parseObjective(req.Input["agent_objective"], r.Caps)
	if err != nil {
		return failedOutcome(req, start, r.now(), StateFailed, "invalid objective: "+err.Error()), nil
	}
	// Success criteria are parsed once, here, and fail closed: a malformed
	// criterion is a defect in the objective, not something to skip silently
	// and not something to guess at. The execution ends as `failed` before any
	// plan is built and before any effect is possible (contract §7).
	crits, cerr := parseCriteria(obj.SuccessCriteria)
	if cerr != nil {
		return failedOutcome(req, start, r.now(), StateFailed, "invalid success criteria: "+cerr.Error()), nil
	}
	criteria := newCriteriaTracker(crits)
	// Resume identity is built from the already-normalized forms: the parsed
	// description, the parsed criteria and the clamped budget. A re-submission
	// that changes any of them is a different objective and must start fresh
	// rather than inherit this execution's plan, effects and counters
	// (APPROVAL_LIFECYCLE_CONTRACTS §6).
	identity := newObjectiveIdentity(obj, budget, crits)
	execID := req.CorrelationID
	if execID == "" {
		execID = req.TaskID
	}
	rn := &run{rt: r, req: req, execID: execID, businessID: req.BusinessID, divisionID: req.DivisionID, ag: ag, planVersion: 1, question: obj.Description}
	// Evidence is attached to every terminal outcome, including failure,
	// cancellation, timeout and approval pause. It is a fact about a search
	// that actually ran, not a success claim. The journal remains the only
	// durable exposure; this hand-off is internal.
	defer func() {
		rn.finalizeResearchEvidence(rn.plan, rn.ledger)
		if out != nil && rn.researchEvidence != nil {
			out.ResearchEvidence = rn.researchEvidence
		}
		// Synthesis is never attached to a non-completed outcome or journaled.
		if out != nil && rn.synthesis != nil && out.Status == "completed" {
			out.ResearchSynthesis = rn.synthesis
		}
		// Evaluation is deterministic and safe on every terminal status. It is
		// attached only when this execution actually attempted research.
		if out != nil {
			out.ResearchEvaluation = rn.evaluateResearch(out.Status)
		}
	}()

	// Approval resume: a waiting run left its authoritative snapshot behind
	// under this execution id. The re-submitted request continues it —
	// same plan, ledger, observations, budgets and deadline — instead of
	// restarting blank (APPROVAL_LIFECYCLE_CONTRACTS §6). Anything else
	// starts fresh.
	snap, resumed := r.takeResume(execID, identity, req)

	deadline := start.Add(budget.MaxExecutionTime)
	if resumed {
		// Waiting never extends the caller deadline: the original one
		// governs, even if it already passed (then the first loop check
		// ends the run honestly with no effects).
		deadline = snap.deadline
	}
	loopCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	rn.transition(StateCreated, map[string]string{"objective": obj.Description})
	rn.emit(event.EventTypeObjectiveStarted, map[string]string{"objective_id": obj.ObjectiveID, "deadline_ms": fmt.Sprint(budget.MaxExecutionTime.Milliseconds())})

	var (
		plan         *Plan
		ledger       *planLedger
		usage        BudgetUsage
		working      *WorkingMemory
		observations []Observation
		memIdentity  memory.Identity
		agentID      string
	)
	// The acting agent is resolved before planning so the planner receives the
	// same bounded tool catalog a decision receives.
	agentID = r.resolveAgent(obj, req)
	planCaps := r.Caps
	if budget.MaxSteps > 0 {
		planCaps.MaxSteps = budget.MaxSteps
	}
	if resumed {
		// Continue the waiting execution: re-validate the restored plan
		// (the registry may have changed while waiting) and pick up
		// exactly where the pause happened. No re-planning, no budget
		// reset, no deadline extension.
		plan = snap.plan
		res := ValidatePlan(plan, r.scope(), planCaps)
		rn.emit(event.EventTypePlanValidated, map[string]string{"valid": fmt.Sprint(res.Valid), "rejections": fmt.Sprint(len(res.Rejections))})
		if !res.Valid {
			rn.transition(StateFailed, map[string]string{"reason": "plan rejected"})
			return failedOutcome(req, start, r.now(), StateFailed, "plan rejected: "+rejectionSummary(res)), nil
		}
		ledger = snap.ledger
		usage = snap.usage
		working = restoreWorking(snap)
		observations = snap.observations
		rn.depth = snap.depth
		rn.planVersion = snap.planVersion
		rn.researchEvidence = snap.researchEvidence
		rn.evidenceOverflow = snap.evidenceOverflow
		rn.researchAttempted = snap.researchAttempted
	} else {
		// ---- planning (advisory) --------------------------------------------
		rn.transition(StatePlanning, nil)
		if ctx.Err() != nil {
			rn.transition(StateCancelled, nil)
			return cancelledOutcome(req, start, r.now()), nil
		}
		steps, planErr := r.planner().Plan(loopCtx, PlanPrompt{
			Objective: obj.Description,
			Context:   obj.Context,
			Tools:     r.toolHints(req, agentID),
			MaxSteps:  planCaps.MaxSteps,
		})
		if planErr != nil {
			if ctx.Err() != nil {
				rn.transition(StateCancelled, nil)
				return cancelledOutcome(req, start, r.now()), nil
			}
			if loopCtx.Err() != nil {
				rn.transition(StateDeadlineExceeded, nil)
				return failedOutcome(req, start, r.now(), StateDeadlineExceeded, "deadline exceeded"), nil
			}
			rn.transition(StateFailed, map[string]string{"reason": "planner failed"})
			return failedOutcome(req, start, r.now(), StateFailed, "planner failed: "+planErr.Error()), nil
		}
		plan = &Plan{PlanID: execID, Version: 1, Steps: steps, ObjectiveID: obj.ObjectiveID,
			CorrelationID: execID, BusinessID: req.BusinessID, DivisionID: req.DivisionID, CreatedAt: r.now()}
		rn.emit(event.EventTypePlanCreated, map[string]string{"plan_id": execID, "version": "1", "steps": fmt.Sprint(len(plan.Steps))})

		// ---- validation (fail-closed before any action) ----------------------
		res := ValidatePlan(plan, r.scope(), planCaps)
		rn.emit(event.EventTypePlanValidated, map[string]string{"valid": fmt.Sprint(res.Valid), "rejections": fmt.Sprint(len(res.Rejections))})
		if !res.Valid {
			rn.transition(StateFailed, map[string]string{"reason": "plan rejected"})
			return failedOutcome(req, start, r.now(), StateFailed, "plan rejected: "+rejectionSummary(res)), nil
		}
		// The step ledger is runtime-owned scratch for this plan version: the
		// model proposed the steps, but readiness, execution order and terminal
		// facts are decided here (contract §3, §4).
		ledger = newPlanLedger(plan)
	}
	rn.plan, rn.ledger = plan, ledger

	if !resumed {
		usage = BudgetUsage{}
		// Working memory is process-local scratch state for THIS objective: bounded,
		// never persisted, and discarded when the execution ends (contract §2, §36).
		working = NewWorkingMemory()
		working.Bind(obj.Description)
		working.SetDeadline(deadline)
		observations = []Observation{}
	}
	// Criterion satisfaction is seeded with the evidence THIS execution already
	// owns — an empty window for a fresh run, the restored window after an
	// approval resume — so the first iteration's evidence counts as genuinely new
	// progress instead of as a re-report of what already arrived (contract §7).
	// No effect has run at this point, so seeding cannot grant a free reset.
	criteria.advance(observationsFor(working), working.EffectCount())

	memIdentity = r.memoryIdentity(rn, agentID)
	contextAnnounced := false
	contextTruncatedAnnounced := false
	// stalls counts progress-triggered replans in this execution, so the
	// terminal message can say the loop was stalled rather than merely
	// out of replans.
	stalls := 0

	// ---- execute ⇄ observe ---------------------------------------------
	for {
		if ctx.Err() != nil {
			rn.transition(StateCancelled, nil)
			return cancelledOutcome(req, start, r.now()), nil
		}
		if loopCtx.Err() != nil {
			rn.transition(StateDeadlineExceeded, nil)
			return failedOutcome(req, start, r.now(), StateDeadlineExceeded, "deadline exceeded"), nil
		}
		if usage.Iterations >= budget.MaxIterations {
			rn.emit(event.EventTypeBudgetExhausted, map[string]string{"budget": "iterations", "used": fmt.Sprint(usage.Iterations)})
			rn.transition(StateBudgetExhausted, nil)
			return failedOutcome(req, start, r.now(), StateBudgetExhausted,
				fmt.Sprintf("iteration budget exhausted (%d)", budget.MaxIterations)), nil
		}
		usage.Iterations++

		// ---- step readiness (runtime decides) -------------------------
		// Dependencies resolve here, never in the model: the current step is
		// the first ready step in plan order, so a step can never execute
		// before the steps it depends on, whatever array position the model
		// gave it. Newly ready or newly blocked steps emit one event each.
		for _, id := range ledger.refresh() {
			switch ledger.get(id).Status {
			case StepReady:
				rn.emit(event.EventTypeStepReady, map[string]string{
					"step": id, "plan_version": fmt.Sprint(ledger.version)})
			case StepBlocked:
				rn.emit(event.EventTypeStepBlocked, map[string]string{
					"step": id, "plan_version": fmt.Sprint(ledger.version)})
			}
		}
		step := Step{StepID: "finalize", Intent: "produce the final result"}
		current := ledger.nextReady()
		if current != nil {
			step = *current
			if err := ledger.begin(step.StepID); err != nil {
				rn.transition(StateFailed, map[string]string{"reason": "ledger refused the step"})
				return failedOutcome(req, start, r.now(), StateFailed, "step refused: "+err.Error()), nil
			}
		}
		currentStepID := ""
		if current != nil {
			currentStepID = step.StepID
		}
		rn.currentStepID = currentStepID
		rn.transition(StateExecuting, map[string]string{"step": step.StepID, "iteration": fmt.Sprint(usage.Iterations)})
		rn.emit(event.EventTypeStepStarted, map[string]string{"step": step.StepID, "intent": step.Intent})
		working.SetStep(step.StepID, step.Intent)

		// ---- decide (model proposes) ------------------------------------
		rn.emit(event.EventTypeAgentThinking, map[string]string{"phase": "decide", "step": step.StepID,
			"reason_category": "awaiting_model_proposal", "iteration": fmt.Sprint(usage.Iterations)})
		// Context assembly is the single bounded boundary: the objective and the
		// current step are reserved first, memory can never crowd them out, and
		// the result is a pure function of (objective, memory, observations,
		// budget) so a deterministic provider sees identical context every run
		// (contract §12, §38).
		assembled := r.assembler().Assemble(
			r.contextInputs(rn, memIdentity, obj, step, observations, r.toolHints(req, agentID)))
		if !contextAnnounced {
			contextAnnounced = true
			rn.emit(event.EventTypeContextAssembled, map[string]string{
				"memory_records": fmt.Sprint(len(assembled.MemoryBlocks)),
				"observations":   fmt.Sprint(len(assembled.ObsBlocks)),
				"memory_chars":   fmt.Sprint(assembled.MemoryChars),
				"obs_chars":      fmt.Sprint(assembled.ObsChars),
			})
		}
		if assembled.Truncated() && !contextTruncatedAnnounced {
			contextTruncatedAnnounced = true
			for _, drop := range assembled.Dropped {
				rn.emit(event.EventTypeContextTruncated, map[string]string{
					"kind": drop.Kind, "dropped": fmt.Sprint(drop.Count), "reason": drop.Reason,
				})
			}
		}
		raw, derr := r.Decider.Decide(loopCtx, DecisionPrompt{
			Objective: obj.Description, StepID: step.StepID, StepIntent: step.Intent,
			Observations: observations, BudgetUsage: usage, Budget: budget,
			Tools:    r.toolHints(req, agentID),
			Evidence: rn.researchEvidence,
			Memory:   assembledMemoryRecords(assembled),
			Context:  assembled.Text,
			// Authoritative runtime state travels with every decision: the
			// active plan version and the ledger's step facts (contract §12).
			PlanVersion: plan.Version,
			StepStates:  ledger.describe(),
		})
		if derr != nil {
			if ctx.Err() != nil {
				rn.transition(StateCancelled, nil)
				return cancelledOutcome(req, start, r.now()), nil
			}
			// The caller's deadline expired mid-decision: that is the
			// existing deadline_exceeded outcome, not a provider failure.
			if loopCtx.Err() != nil {
				rn.transition(StateDeadlineExceeded, nil)
				return failedOutcome(req, start, r.now(), StateDeadlineExceeded, "deadline exceeded"), nil
			}
			rn.transition(StateFailed, map[string]string{"reason": "decision failed"})
			return failedOutcome(req, start, r.now(), StateFailed, "decision failed: "+derr.Error()), nil
		}
		usage.ModelCalls++
		action, aerr := ParseAction(raw)
		if aerr != nil {
			rn.emit(event.EventTypeActionRejected, map[string]string{"action": "unknown", "reason": aerr.Error()})
			rn.transition(StateFailed, map[string]string{"reason": "protocol"})
			return failedOutcome(req, start, r.now(), StateFailed, aerr.Error()), nil
		}
		rn.emit(event.EventTypeAgentThinking, map[string]string{"phase": "decided", "step": step.StepID,
			"action_type": string(action.Type), "reason_category": "model_proposal", "attempt": "1"})

		// ---- validate (runtime decides) ---------------------------------
		actx := &ActionContext{
			Objective: obj, BusinessID: req.BusinessID, DivisionID: req.DivisionID, ActorID: req.ActorID,
			AgentID: agentID, AgentTools: r.agentTools(agentID), Iterations: usage.Iterations,
			Used: usage, Budget: budget, Depth: rn.depth + 1,
			StepHasWork: current != nil || len(observations) > 0,
		}
		if verr := ValidateAction(action, actx, r.scope(), r.Caps); verr != nil {
			rn.emit(event.EventTypeActionRejected, map[string]string{"action": string(action.Type), "reason": verr.Error()})
			if strings.HasPrefix(verr.Error(), "cancelled") {
				rn.transition(StateCancelled, nil)
				return cancelledOutcome(req, start, r.now()), nil
			}
			rn.transition(StateFailed, map[string]string{"reason": "action rejected"})
			return failedOutcome(req, start, r.now(), StateFailed, "action rejected: "+verr.Error()), nil
		}
		// Completion over captured v2 evidence is only meaningful as a typed,
		// citation-resolved synthesis. Validate before any effect so an invalid
		// citation can never be mistaken for a successful research result.
		var validatedSynthesis *executor.ResearchSynthesis
		if action.Synthesis != nil && action.Type != ActionComplete {
			msg := "synthesis is only valid on a complete action"
			rn.emit(event.EventTypeActionRejected, map[string]string{"action": string(action.Type), "reason": msg})
			rn.transition(StateFailed, map[string]string{"reason": "invalid synthesis"})
			return failedOutcome(req, start, r.now(), StateFailed, msg), nil
		}
		if action.Type == ActionComplete {
			if serr := rn.validateCompletionSynthesis(action.Synthesis, plan, ledger, currentStepID); serr != nil {
				msg := "invalid synthesis: " + serr.Error()
				rn.emit(event.EventTypeActionRejected, map[string]string{"action": string(action.Type), "reason": msg})
				rn.transition(StateFailed, map[string]string{"reason": "invalid synthesis"})
				return failedOutcome(req, start, r.now(), StateFailed, msg), nil
			}
			validatedSynthesis = action.Synthesis
		}
		// A completed mutating effect is never silently repeated: the
		// fingerprint of every consequential mutation this execution performed
		// is recorded, and an identical proposal is rejected before anything
		// can run again (contract §10). Reads may repeat — the reliability
		// layer itself retries reads — and resume is a new execution with
		// fresh scratch state, so legitimate re-execution is unaffected.
		// A coordination group whose every member already completed is the
		// same duplicate: nothing new would run.
		if action.Type == ActionCoordinate {
			if fps := coordinateMemberFingerprints(action); coordinateFullyExecuted(fps, working.HasEffect) {
				msg := "action rejected: duplicate of an already-completed effect (not re-executed)"
				rn.emit(event.EventTypeActionRejected, map[string]string{"action": string(action.Type), "reason": msg})
				rn.transition(StateFailed, map[string]string{"reason": "duplicate effect"})
				return failedOutcome(req, start, r.now(), StateFailed, msg), nil
			}
		} else if fp := repeatGuardFingerprint(r.scope(), action); fp != "" && working.HasEffect(fp) {
			msg := "action rejected: duplicate of an already-completed effect (not re-executed)"
			rn.emit(event.EventTypeActionRejected, map[string]string{"action": string(action.Type), "reason": msg})
			rn.transition(StateFailed, map[string]string{"reason": "duplicate effect"})
			return failedOutcome(req, start, r.now(), StateFailed, msg), nil
		}
		rn.emit(event.EventTypeActionProposed, map[string]string{"action": string(action.Type), "step": step.StepID})

		// ---- act --------------------------------------------------------
		// Cancellation and deadline are re-checked after the model call and
		// before any effect: a decision that arrived too late never runs
		// (contract §11).
		if ctx.Err() != nil {
			rn.transition(StateCancelled, nil)
			return cancelledOutcome(req, start, r.now()), nil
		}
		if loopCtx.Err() != nil {
			rn.transition(StateDeadlineExceeded, nil)
			return failedOutcome(req, start, r.now(), StateDeadlineExceeded, "deadline exceeded"), nil
		}
		working.SetPending(string(action.Type) + " " + firstNonEmpty(action.Tool, action.Key, action.AgentID))
		obs, next, termState, termMsg := r.perform(rn, loopCtx, obj, action, currentStepID, agentID, working, usage, budget)
		usage = next
		// Completion gate (contract §7). With criteria on the objective, a
		// completion claim is only a REQUEST: it is accepted when every
		// criterion is satisfied by evidence from THIS execution. Rejection
		// is terminal under the strict policy (§6) — it ends the execution as
		// `failed` and is never recovered by a later iteration, so it is
		// applied here, before the ledger settles the step or the loop counts
		// progress, rather than inside perform's execution path.
		if termState == StateCompleted && len(crits) > 0 {
			if st := evaluateCriteria(crits, observationsFor(working), working.EffectCount()); !st.all {
				reason := "success criteria unmet: " + strings.Join(st.unmet, ", ")
				rn.emit(event.EventTypeActionRejected, map[string]string{
					"action": string(ActionComplete), "reason": reason})
				termState, termMsg = StateFailed, "complete rejected: "+reason
			}
		}
		if obs != nil {
			observations = append(observations, *obs)
			working.AddObservation(*obs)
			_ = working.Put(obs.ObservationID, obs.Source+": "+obs.Status)
			if current != nil {
				if oerr := ledger.observe(step.StepID); oerr == nil {
					rn.emit(event.EventTypeStepObserving, map[string]string{
						"step": step.StepID, "observation": obs.ObservationID,
						"plan_version": fmt.Sprint(ledger.version)})
				}
			}
			rn.transition(StateObserving, map[string]string{"observation": obs.ObservationID, "status": obs.Status})
			rn.emit(event.EventTypeObservationCreated, map[string]string{"observation_id": obs.ObservationID,
				"source": obs.Source, "action_type": string(obs.ActionType), "status": obs.Status})
			rn.emit(event.EventTypeStepCompleted, map[string]string{"step": step.StepID, "observation": obs.ObservationID})
		} else {
			rn.emit(event.EventTypeStepCompleted, map[string]string{"step": step.StepID, "observation": ""})
		}
		// The ledger settles the current step from the authoritative result:
		// success completes it on the observation's evidence, failure fails
		// it and blocks everything downstream. Governance terminals leave
		// the step unmarked — a denial, a pending approval and an escalation
		// are not execution outcomes (contract §8).
		if current != nil && termState != "" {
			switch termState {
			case StateCompleted, StateFailed:
				failed := termState == StateFailed
				obsID := ""
				if obs != nil {
					obsID = obs.ObservationID
				}
				if ferr := ledger.finish(step.StepID, obsID,
					firstNonEmpty(termMsg, string(termState)),
					obsOutcome(obs), obsAttempts(obs), failed); ferr == nil {
					if failed {
						rn.emit(event.EventTypeStepFailed, map[string]string{
							"step": step.StepID, "plan_version": fmt.Sprint(ledger.version)})
						for _, id := range ledger.refresh() {
							if ledger.get(id).Status == StepBlocked {
								rn.emit(event.EventTypeStepBlocked, map[string]string{
									"step": id, "plan_version": fmt.Sprint(ledger.version)})
							}
						}
					}
				}
			}
		}
		// A consequential mutation that ran to success is fingerprinted so the
		// model cannot make the runtime repeat it blindly (contract §10).
		if termState == "" && obs != nil {
			if fp := repeatGuardFingerprint(r.scope(), action); fp != "" {
				if _, ferr := working.NoteEffect(fp); ferr != nil {
					rn.transition(StateFailed, map[string]string{"reason": "effect ledger refused"})
					return failedOutcome(req, start, r.now(), StateFailed, "effect ledger refused: "+ferr.Error()), nil
				}
			}
			if current != nil {
				if ferr := ledger.finish(step.StepID, obs.ObservationID,
					firstNonEmpty(obs.Text, string(obs.Status)),
					obsOutcome(obs), obsAttempts(obs), false); ferr == nil {
					rn.emit(event.EventTypeStepCompleted, map[string]string{
						"step": step.StepID, "observation": obs.ObservationID,
						"plan_version": fmt.Sprint(ledger.version)})
				}
			}
		}
		if termState != "" {
			if termState == StateCompleted {
				if validatedSynthesis != nil {
					rn.synthesis = validatedSynthesis
				}
				rn.transition(StateCompleted, map[string]string{"iterations": fmt.Sprint(usage.Iterations)})
				return completedOutcome(req, start, r.now(), obj, observations, usage, agentID, termMsg), nil
			}
			if termState == StateCancelled {
				rn.transition(StateCancelled, nil)
				return cancelledOutcome(req, start, r.now()), nil
			}
			// Governance terminal states keep their own status: a denial, a
			// pending approval and an escalation are not failures
			// (AGENT_GOVERNANCE_CONTROL_CONTRACTS §3, §8).
			if termState == StateDenied || termState == StatePendingApproval || termState == StateEscalated {
				approvalID, escalationRef := "", ""
				if obs != nil {
					approvalID, escalationRef = obs.Result["approval_id"], obs.Result["escalation_id"]
				}
				rn.transition(termState, map[string]string{"reason": termMsg})
				switch termState {
				case StateDenied:
					return deniedOutcome(req, start, r.now(), termMsg), nil
				case StatePendingApproval:
					// The run pauses here: snapshot its authoritative state so
					// the approval-driven re-submission continues instead of
					// restarting blank (APPROVAL_LIFECYCLE_CONTRACTS §6).
					r.snapshotRun(rn, identity, plan, ledger, observations, usage, working, deadline)
					return pendingApprovalOutcome(req, start, r.now(), termMsg, approvalID), nil
				default:
					return escalatedOutcome(req, start, r.now(), termMsg, escalationRef), nil
				}
			}
			rn.transition(termState, map[string]string{"reason": termMsg})
			return failedOutcome(req, start, r.now(), termState, termMsg), nil
		}

		// ---- progress and bounded replanning (contract §7) -----------------
		// An iteration is progress only when it produced evidence that
		// satisfies a criterion that was still unmet before it ran. Anything
		// else — a bare `continue`, an observation that satisfies nothing new,
		// a repeated evidence record — counts as no progress. Objectives
		// without criteria keep the v1 rule, where only a `continue` is no
		// progress and any other action clears the counter.
		progressed := action.Type != ActionContinue
		if len(crits) > 0 {
			progressed = criteria.advance(observationsFor(working), working.EffectCount())
		}
		if progressed {
			usage.NoProgress = 0
		} else {
			usage.NoProgress++
			rn.emit(event.EventTypeBudgetExhausted, map[string]string{"budget": "no_progress", "used": fmt.Sprint(usage.NoProgress)})
		}
		stalled := usage.NoProgress >= stallLimit(r.Caps.MaxConsecutiveNoProgress)
		if stalled {
			stalls++
		}

		// A plain `continue` with progress still left just loops again.
		if !stalled && action.Type == ActionContinue {
			continue
		}

		// Stalled progress and a model-initiated replan are the same bounded
		// resource: they share `usage.Replans` and `budget.MaxReplans`, so
		// neither can be used to escape the other's allowance. A stalled loop
		// therefore spends a replan instead of grinding, and the exhaustion of
		// the shared budget terminates the execution honestly rather than
		// letting it spin.
		if stalled || action.Type == ActionReplan {
			reason := action.Reason
			if stalled {
				reason = "progress stalled: no new evidence for " + fmt.Sprint(stallLimit(r.Caps.MaxConsecutiveNoProgress)) + " iterations"
				rn.emit(event.EventTypeAgentThinking, map[string]string{"phase": "stalled", "step": step.StepID,
					"reason_category": "no_progress", "attempt": fmt.Sprint(usage.NoProgress)})
			}
			usage.Replans++
			if usage.Replans > budget.MaxReplans {
				rn.emit(event.EventTypeBudgetExhausted, map[string]string{"budget": "replans", "used": fmt.Sprint(usage.Replans)})
				rn.transition(StateBudgetExhausted, nil)
				// A stall-driven exhaustion names the stall: "replan budget
				// exhausted" alone would hide why the loop was replanning.
				msg := fmt.Sprintf("replan budget exhausted (%d)", budget.MaxReplans)
				if stalls > 0 {
					msg = fmt.Sprintf("progress stalled %d times with no new evidence; %s", stalls, msg)
				}
				return failedOutcome(req, start, r.now(), StateBudgetExhausted, msg), nil
			}
			// Cancellation still wins over a replan, and waiting never buys
			// more time: the replan shares this run's original deadline.
			if ctx.Err() != nil {
				rn.transition(StateCancelled, nil)
				return cancelledOutcome(req, start, r.now()), nil
			}
			if loopCtx.Err() != nil {
				rn.transition(StateDeadlineExceeded, nil)
				return failedOutcome(req, start, r.now(), StateDeadlineExceeded, "deadline exceeded"), nil
			}
			rn.transition(StateReplanning, map[string]string{"reason": reason, "version": fmt.Sprint(plan.Version + 1)})
			newSteps, perr := r.planner().Plan(loopCtx, PlanPrompt{
				Objective:   obj.Description,
				Context:     obj.Context,
				Tools:       r.toolHints(req, agentID),
				MaxSteps:    planCaps.MaxSteps,
				PriorSteps:  plan.Steps,
				PlanVersion: plan.Version,
				StepStates:  ledger.describe(),
				Reason:      reason,
			})
			if perr != nil {
				if ctx.Err() != nil {
					rn.transition(StateCancelled, nil)
					return cancelledOutcome(req, start, r.now()), nil
				}
				if loopCtx.Err() != nil {
					rn.transition(StateDeadlineExceeded, nil)
					return failedOutcome(req, start, r.now(), StateDeadlineExceeded, "deadline exceeded"), nil
				}
				rn.transition(StateFailed, map[string]string{"reason": "replan failed"})
				return failedOutcome(req, start, r.now(), StateFailed, "replan failed: "+perr.Error()), nil
			}
			newPlan := &Plan{PlanID: execID, Version: plan.Version + 1, ParentVersion: plan.Version,
				Reason: reason, Steps: newSteps, ObjectiveID: obj.ObjectiveID,
				CorrelationID: execID, BusinessID: req.BusinessID, DivisionID: req.DivisionID, CreatedAt: r.now()}
			nres := ValidatePlan(newPlan, r.scope(), planCaps)
			if !nres.Valid {
				rn.transition(StateFailed, map[string]string{"reason": "replan rejected"})
				return failedOutcome(req, start, r.now(), StateFailed, "replan rejected: "+rejectionSummary(nres)), nil
			}
			rn.emit(event.EventTypePlanReplanned, map[string]string{"version": fmt.Sprint(newPlan.Version),
				"parent_version": fmt.Sprint(newPlan.ParentVersion), "reason": newPlan.Reason})
			// The new version is a fresh ledger that carries completed steps
			// forward as immutable fact: what already ran is never re-run,
			// and a later model response cannot rewrite it (contract §7, §9).
			plan = newPlan
			rn.planVersion = newPlan.Version
			ledger = ledger.carryCompleted(newPlan)
			rn.plan, rn.ledger = plan, ledger
			usage.NoProgress = 0
			continue
		}
	}
}

// stallLimit normalises the no-progress threshold. A non-positive configured
// limit still means "stall after one barren iteration", as it did before
// stalled progress began spending replans; it must never mean "every iteration
// is a stall", which would turn a degenerate cap into a replan storm.
func stallLimit(configured int) int {
	if configured < 1 {
		return 1
	}
	return configured
}

// CatalogProvider yields the bounded capability catalog for one scope.
type CatalogProvider interface {
	Catalog(businessID, divisionID string) []ToolHint
}

// toolHints returns the catalog narrowed to the acting agent's allowlist.
func (r *Runtime) toolHints(req *executor.WorkRequest, agentID string) []ToolHint {
	if r.Catalog == nil {
		return nil
	}
	allowed := map[string]struct{}{}
	for _, t := range r.agentTools(agentID) {
		allowed[t] = struct{}{}
	}
	all := r.Catalog.Catalog(req.BusinessID, req.DivisionID)
	out := make([]ToolHint, 0, len(all))
	for _, h := range all {
		if _, ok := allowed[h.ID]; !ok {
			continue
		}
		out = append(out, h)
	}
	return out
}

// planner returns the configured planner (advisory model by default).
func (r *Runtime) planner() Planner {
	if r.Planner != nil {
		return r.Planner
	}
	return &ModelPlanner{Decider: r.Decider}
}

// perform executes exactly one validated action and returns the observation it
// produced plus the updated counters. It can only cause the five mediated
// effects of §6; the returned terminal state is "" when the loop continues.
func (r *Runtime) perform(rn *run, ctx context.Context, obj Objective, a Action, stepID, agentID string, working *WorkingMemory, usage BudgetUsage, budget Budget) (*Observation, BudgetUsage, State, string) {
	switch a.Type {
	case ActionComplete:
		// Completion needs runtime evidence, not model assertion: at least
		// one observation from this execution must exist (contract §9). A
		// completion claim with no evidence — a fabricated "done", a success
		// story for an effect that never ran — fails the execution instead
		// of falsely completing it.
		if len(observationsFor(working)) == 0 {
			return nil, usage, StateFailed, "complete rejected: no runtime evidence of progress"
		}
		if a.Synthesis != nil {
			// Typed synthesis is rendered from the structured outcome, not from
			// free-form result prose. The runtime owns this summary fragment so
			// two model-authored versions cannot disagree.
			return nil, usage, StateCompleted, "source-grounded synthesis"
		}
		return nil, usage, StateCompleted, a.Result
	case ActionFail:
		return nil, usage, StateFailed, "model requested failure: " + a.Reason
	case ActionContinue, ActionReplan:
		return nil, usage, "", ""
	case ActionModelCall:
		usage.ModelCalls++
		raw, err := r.Decider.Decide(ctx, DecisionPrompt{Objective: a.Prompt, StepID: "model_call", BudgetUsage: usage, Budget: budget})
		obs := &Observation{ObservationID: newID("obs"), Source: "model", ActionType: ActionModelCall,
			Status: "ok", Timestamp: r.now()}
		if err != nil {
			obs.Status, obs.Text = "failed", err.Error()
			return obs, usage, StateFailed, "model_call failed: " + err.Error()
		}
		obs.Text = raw
		return obs, usage, "", ""
	case ActionToolCall:
		usage.ToolCalls++
		if a.Tool == execjournal.ResearchEvidenceCapability {
			// An attempt is a runtime fact even when governance denies it or
			// the result envelope is malformed. Evaluation reports incomplete
			// input instead of pretending research never happened.
			rn.researchAttempted = true
		}
		obs := &Observation{ObservationID: newID("obs"), Source: "tool:" + a.Tool, ActionType: ActionToolCall,
			Status: "ok", Timestamp: r.now()}
		// The correlation carried into the tool scope is this execution's resolved
		// identity, not the raw request field: the capability layer derives the
		// logical call id — and therefore the mutation idempotency key — from it
		// (OPERATIONAL_RELIABILITY_CONTRACTS §7). A request without a CorrelationID
		// falls back to TaskID up here; passing the empty field instead would make
		// the capability layer mint a timestamp, so every dispatch of the SAME
		// execution would present a different dedupe token and a remote system
		// could not recognise a replay. This is the same identity events, memory
		// writes and delegation already carry.
		res, err := r.Exec.InvokeToolScopedResult(ctx, agentexec.ToolScope{
			ActorID: rn.req.ActorID, BusinessID: rn.businessID, DivisionID: rn.divisionID,
			AgentTools: r.agentTools(agentID), CorrelationID: rn.execID,
		}, agentID, agentexec.ToolCallSpec{ToolID: a.Tool, Operation: a.Operation, Input: a.Input})
		out := res.Result
		rn.emit(event.EventTypeToolRequested, map[string]string{"tool_id": a.Tool, "agent_id": agentID})
		if err != nil {
			// A governance refusal is NOT a tool failure. It reports its own
			// observation status and terminal state, so approval-pending is never
			// collapsed into failure, a denial is never a side effect, and
			// escalation is never a failure (AGENT_GOVERNANCE_CONTROL_CONTRACTS
			// §3, §8).
			var ae *agentexec.AdmissionError
			if errors.As(err, &ae) {
				obs.Text = ae.Error()
				obs.Result = map[string]string{
					"governance_outcome": ae.Outcome.String(),
					"action":             ae.Action,
					"tool_id":            ae.ToolID,
				}
				if ae.Operation != "" {
					obs.Result["operation"] = ae.Operation
				}
				if ae.ApprovalID != "" {
					obs.Result["approval_id"] = ae.ApprovalID
				}
				if ae.EscalationID != "" {
					obs.Result["escalation_id"] = ae.EscalationID
				}
				if ae.PolicyID != "" {
					obs.Result["policy_id"] = ae.PolicyID
				}
				switch ae.Outcome {
				case governance.REQUIRE_APPROVAL:
					obs.Status = "pending_approval"
					return obs, usage, StatePendingApproval, ae.Error()
				case governance.ESCALATE:
					obs.Status = "escalated"
					return obs, usage, StateEscalated, ae.Error()
				default:
					obs.Status = "denied"
					return obs, usage, StateDenied, ae.Error()
				}
			}
			obs.Status, obs.Text = "failed", err.Error()
			var oe *agentexec.OutcomeError
			if errors.As(err, &oe) {
				obs.Outcome, obs.Attempts = oe.Outcome, oe.Attempts
				obs.RetryRecommended = oe.RetryRecommended
				// An unknown outcome is never re-driven: it only carries a
				// reconciliation requirement (OPERATIONAL_RELIABILITY §3, §9).
				obs.ReconciliationRequired = oe.Outcome == "unknown"
			}
			return obs, usage, StateFailed, "tool failed: " + err.Error()
		}
		// Only a successful, untruncated web.search result can become durable
		// evidence. ParseWebSearchEvidence rejects the whole snapshot on a
		// count/format/size mismatch, so a partial source list is never stored.
		if ev, ok := execjournal.ParseWebSearchEvidence(a.Tool, res.Operation, out, res.Truncated); ok {
			rn.captureResearchEvidence(ev, stepID, obs.ObservationID)
		}
		obs.Result = out
		obs.Text = kvString(out)
		obs.Outcome, obs.Attempts = "completed", 1
		rn.emit(event.EventTypeToolCompleted, map[string]string{"tool_id": a.Tool, "agent_id": agentID, "status": "ok"})
		return obs, usage, "", ""
	case ActionDelegate:
		usage.Delegations++
		obs := &Observation{ObservationID: newID("obs"), Source: "delegate", ActionType: ActionDelegate,
			Status: "ok", Timestamp: r.now()}
		// Delegation is a consequential action: it starts another execution. It
		// passes through the same admission point as a tool call, with the child
		// agent as the resource (contract §2). The child runs one level deeper;
		// validation already enforced the depth bound.
		childDepth := rn.depth + 1
		// Proposed child tools are intersected here, at effect time, against
		// the registry and both allowlists — validation checked the same
		// shape, but authority is re-established now, not trusted from the
		// proposal (contract §4).
		childTools, terr := r.resolveDelegateTools(rn, agentID, a)
		if terr != nil {
			obs.Status, obs.Text = "failed", terr.Error()
			return obs, usage, StateFailed, "delegation failed: " + terr.Error()
		}
		admObs, st, msg, done, proposalID := r.admitNonTool(rn, agentID, control.ActionDelegate,
			control.ResourceTypeAgent, control.DelegateTarget(a.AgentID))
		if done {
			return admObs, usage, st, msg
		}
		rn.emit(event.EventTypeAgentDelegated, map[string]string{
			"agent_id": a.AgentID, "depth": fmt.Sprint(childDepth),
			"parent_agent_id": agentID, "child_tools": fmt.Sprint(len(childTools))})
		out, err := r.Exec.DelegateChild(ctx, rn.req, rn.ag, agentexec.DelegateSpec{
			ID: newID("child"), Intent: a.Objective, AgentID: a.AgentID, Tools: childTools,
			ParentAgentID: agentID, DelegationActionID: proposalID,
		}, rn.depth)
		if err != nil || out == nil || out.Status != "completed" {
			return r.delegateChildOutcome(obs, usage, out, err)
		}
		// Child output is DATA for the parent: it becomes observation text,
		// never an instruction and never a state rewrite (contract §7).
		obs.Text = out.Output
		obs.Result = map[string]string{
			"child_status": out.Status, "child_agent_id": out.AgentID,
			"child_task_id": out.TaskID,
		}
		return obs, usage, "", ""
	case ActionCoordinate:
		return r.performCoordinate(rn, ctx, a, agentID, working, usage, budget, rn.planVersion)
	case ActionMemoryWrite:
		obs := &Observation{ObservationID: newID("obs"), Source: "memory", ActionType: ActionMemoryWrite,
			Status: "ok", Timestamp: r.now()}
		// A durable write is a consequential action, and memory is DATA — never
		// authority. The target is resolved by the memory platform FIRST, using
		// the same authorization and clamping Write performs, and that resolved
		// record id is what governance decides about (contract §6). A model
		// asking for a broader scope than it may write gets the clamped target
		// evaluated, not the request.
		target, terr := r.memoryWriteTarget(rn, agentID, a, observationsFor(working))
		if terr != nil {
			// The memory platform refused the candidate outright, so governance
			// never decided anything. That is an action failure (nothing ran), not
			// a governance denial — the two axes stay separate.
			obs.Status, obs.Text = "failed", terr.Error()
			return obs, usage, StateFailed, "memory_write failed: " + terr.Error()
		}
		if obs, st, msg, done, _ := r.admitNonTool(rn, agentID, control.ActionMemoryWrite,
			control.ResourceTypeMemory, control.MemoryResource(target.RecordID)); done {
			return obs, usage, st, msg
		}
		rec, err := r.memoryWrite(rn, agentID, a, observationsFor(working), target)
		if err != nil {
			obs.Status, obs.Text = "failed", err.Error()
			return obs, usage, StateFailed, "memory_write failed: " + err.Error()
		}
		rn.emit(event.EventTypeMemoryWritten, map[string]string{
			"key": rec.Key, "agent_id": agentID, "scope": string(rec.Scope),
			"trust": string(rec.Trust), "version": fmt.Sprint(rec.Version),
		})
		obs.Text = "wrote " + rec.Key + " (v" + fmt.Sprint(rec.Version) + ", trust=" + string(rec.Trust) + ")"
		obs.Result = map[string]string{"memory_id": rec.ID, "scope": string(rec.Scope),
			"trust": string(rec.Trust), "version": fmt.Sprint(rec.Version)}
		return obs, usage, "", ""
	case ActionMemoryRead:
		obs := &Observation{ObservationID: newID("obs"), Source: "memory", ActionType: ActionMemoryRead,
			Status: "ok", Timestamp: r.now()}
		rec, found := r.memoryRead(rn, agentID, a.Key)
		rn.emit(event.EventTypeMemoryRead, map[string]string{
			"key": a.Key, "agent_id": agentID, "found": fmt.Sprint(found)})
		if !found {
			obs.Status, obs.Text = "notfound", "no memory for key "+a.Key
			return obs, usage, "", "" // a missing key is data, not a failure
		}
		obs.Text = rec.Value
		obs.Outcome = rec.Outcome
		obs.Attempts = rec.Attempts
		obs.RetryRecommended = rec.RetryRecommended
		obs.ReconciliationRequired = rec.ReconciliationRequired
		obs.Result = map[string]string{"memory_id": rec.ID, "scope": string(rec.Scope),
			"trust": string(rec.Trust), "source": string(rec.Source), "type": string(rec.Type)}
		return obs, usage, "", ""
	case ActionMemoryDelete:
		obs := &Observation{ObservationID: newID("obs"), Source: "memory", ActionType: ActionMemoryDelete,
			Status: "ok", Timestamp: r.now()}
		// A delete removes EVERY record the caller can see under that key, across
		// scopes. The runtime therefore resolves the actual affected set first
		// (through the authorized query path, which is the same query the delete
		// performs) and governance decides about that set — not about a
		// model-supplied scope (contract §6).
		targets, terr := r.memoryDeleteTargets(rn, agentID, a.Key)
		if terr != nil {
			obs.Status, obs.Text = "failed", terr.Error()
			return obs, usage, StateFailed, "memory_delete failed: " + terr.Error()
		}
		if len(targets) == 0 {
			// Nothing is visible under that key: the delete removes nothing, so
			// there is no effect to admit. Report it honestly instead of asking
			// governance about an effect that cannot happen.
			obs.Status, obs.Text = "notfound", "no memory for key "+a.Key
			return obs, usage, "", ""
		}
		adm, aerr := r.admitMemoryDelete(rn, agentID, targets)
		if aerr != nil {
			// No controller is a governance availability failure, which is a
			// denial by design (fail closed), not a generic failure.
			obs.Status, obs.Text = "denied", aerr.Error()
			obs.Result = map[string]string{
				"governance_outcome": governance.DENY.String(),
				"action":             control.ActionMemoryDelete,
			}
			return obs, usage, StateDenied, "memory_delete denied: " + aerr.Error()
		}
		if obs2, st, msg, done := finishMemoryDeleteAdmission(obs, adm); done {
			return obs2, usage, st, msg
		}
		n, err := r.memoryDeleteExactly(rn, agentID, targets)
		if err != nil {
			obs.Status, obs.Text = "failed", err.Error()
			return obs, usage, StateFailed, "memory_delete failed: " + err.Error()
		}
		obs.Text = fmt.Sprintf("deleted %d record(s) for key %s", n, a.Key)
		return obs, usage, "", ""
	default:
		return nil, usage, StateFailed, "unsupported action " + string(a.Type)
	}
}

// obsOutcome normalizes a possibly-nil observation to its reliability outcome.
func obsOutcome(obs *Observation) string {
	if obs == nil {
		return ""
	}
	return obs.Outcome
}

// obsAttempts normalizes a possibly-nil observation to its attempt count.
func obsAttempts(obs *Observation) int {
	if obs == nil {
		return 0
	}
	return obs.Attempts
}

// resolveDelegateTools intersects model-proposed child tools with the tool
// registry, the child agent's allowlist and the parent agent's allowlist,
// and converts the survivors to execution specs. Validation already checked
// this shape; this re-establishes it at effect time because the registry may
// have changed between proposal and effect. Empty proposal means a model-only
// child (no child tools) and needs no agent resolution.
func (r *Runtime) resolveDelegateTools(rn *run, agentID string, a Action) ([]agentexec.ToolCallSpec, error) {
	return r.resolveDelegateToolsFor(rn, agentID, a.AgentID, a.DelegateTools)
}

// resolveDelegateToolsFor is the shared intersection core behind single
// delegations and coordination members.
func (r *Runtime) resolveDelegateToolsFor(rn *run, agentID, childID string, proposed []DelegateToolCall) ([]agentexec.ToolCallSpec, error) {
	if len(proposed) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(childID) == "" {
		return nil, fmt.Errorf("delegation failed: delegate_tools requires an explicit agent_id")
	}
	childDef, ok := r.Registry.Get(childID)
	if !ok {
		return nil, fmt.Errorf("delegation failed: child agent %q is not registered", childID)
	}
	if strings.TrimSpace(agentID) == "" {
		return nil, fmt.Errorf("delegation failed: cannot bound child tools without a resolved parent agent")
	}
	parentTools := r.agentTools(agentID)
	out := make([]agentexec.ToolCallSpec, 0, len(proposed))
	for i, t := range proposed {
		where := fmt.Sprintf("delegate_tools[%d]", i)
		if strings.TrimSpace(t.Tool) == "" {
			return nil, fmt.Errorf("delegation failed: %s needs a tool id", where)
		}
		if !r.scope().ToolRegistered(t.Tool) {
			return nil, fmt.Errorf("delegation failed: %s tool %q is not registered", where, t.Tool)
		}
		if t.Operation != "" {
			ops, ok := r.scope().(OperationResolver)
			if !ok || !ops.ToolSupportsOperation(t.Tool, t.Operation) {
				return nil, fmt.Errorf("delegation failed: %s tool %q does not support operation %q", where, t.Tool, t.Operation)
			}
		}
		if !contains(childDef.AllowedTools, t.Tool) {
			return nil, fmt.Errorf("delegation failed: %s tool %q is not in child agent %q's allowlist", where, t.Tool, childID)
		}
		if !contains(parentTools, t.Tool) {
			return nil, fmt.Errorf("delegation failed: %s tool %q is not in parent agent %q's allowlist", where, t.Tool, agentID)
		}
		out = append(out, agentexec.ToolCallSpec{ToolID: t.Tool, Operation: t.Operation, Input: t.Input})
	}
	return out, nil
}

// delegateChildOutcome normalizes a non-completed child execution into the
// parent's observation without collapsing distinct outcomes (contract §7): an
// unknown child stays unknown (with reconciliation markers, never
// re-dispatched here), a denied/pending/escalated child is reported as what
// it is with its references, and anything else fails honestly. The parent run
// always ends failed — a child that did not complete is never mistaken for
// success, and a foreign approval is never adopted as the parent's own.
func (r *Runtime) delegateChildOutcome(obs *Observation, usage BudgetUsage, out *executor.Outcome, err error) (*Observation, BudgetUsage, State, string) {
	msg := "delegation failed"
	if err != nil {
		msg = err.Error()
	} else if out != nil {
		msg = fmt.Sprintf("child execution ended %s: %s", out.Status, out.Error)
	}
	obs.Status, obs.Text = "failed", msg
	if out != nil {
		obs.Result = map[string]string{"child_status": out.Status, "child_agent_id": out.AgentID, "child_task_id": out.TaskID}
		switch out.Status {
		case "unknown":
			obs.Outcome = "unknown"
			obs.ReconciliationRequired = true
		case "timed_out":
			obs.Outcome = "timed_out"
		case "cancelled":
			obs.Outcome = "cancelled"
		case "pending_approval":
			obs.Result["approval_id"] = out.ApprovalID
		case "escalated":
			obs.Result["escalation_id"] = out.EscalationRef
		case "denied":
			obs.Result["governance_outcome"] = "DENY"
		}
	}
	return obs, usage, StateFailed, msg
}

// scope exposes the Agent Execution visibility rules to the validators.
func (r *Runtime) scope() ScopeResolver {
	return &execScope{registry: r.Registry, tools: r.Exec}
}

type execScope struct {
	registry *agentexec.Registry
	tools    *agentexec.Runtime
}

func (s *execScope) AgentVisible(agentID, businessID, divisionID string) bool {
	d, ok := s.registry.Get(agentID)
	if !ok || d.BusinessID != businessID {
		return false
	}
	if divisionID == "" {
		return d.DivisionID == ""
	}
	return d.DivisionID == "" || d.DivisionID == divisionID
}

func (s *execScope) ToolRegistered(toolID string) bool {
	_, ok := s.tools.Tools.GetTool(toolID)
	return ok
}

// ToolSupportsOperation reports whether the tool's manifest declares the
// operation. The manifest is the only authority: a model cannot invent one.
func (s *execScope) ToolSupportsOperation(toolID, operation string) bool {
	m, ok := s.tools.Tools.Manifest(toolID)
	if !ok {
		return false
	}
	return m.Supports(operation)
}

// ToolSideEffect reports the manifest's side-effect class for a tool.
func (s *execScope) ToolSideEffect(toolID string) (string, bool) {
	m, ok := s.tools.Tools.Manifest(toolID)
	if !ok {
		return "", false
	}
	return string(m.SideEffectClass), true
}

func (s *execScope) AgentAllowsTool(agentID, toolID string) bool {
	d, ok := s.registry.Get(agentID)
	if !ok {
		return false
	}
	return contains(d.AllowedTools, toolID)
}

// resolveAgent picks the acting agent: an explicit, scope-visible agent when
// the objective names one, otherwise the deterministic selector's winner.
//
// An explicit agent_id must satisfy the same eligibility rules as the selector:
// active status, business ownership, and division visibility. If the explicit
// selection fails these checks, we do NOT fall back to a different agent —
// that would silently invoke an unauthorized agent and obscure the failure.
func (r *Runtime) resolveAgent(obj Objective, req *executor.WorkRequest) string {
	if obj.Context != nil {
		if id := obj.Context["agent_id"]; id != "" {
			if d, ok := r.Registry.Get(id); ok && d.BusinessID == req.BusinessID {
				if d.Status == agentexec.AgentActive &&
					(d.DivisionID == "" || d.DivisionID == req.DivisionID) {
					return id
				}
			}
			// Explicit selection failed — do not fall back to a different agent.
			return ""
		}
	}
	sel := agentexec.Select(r.Registry, &agentexec.SelectionRequest{
		BusinessID: req.BusinessID, DivisionID: req.DivisionID,
	}, func(id string) bool { _, ok := r.Exec.Tools.GetTool(id); return ok })
	if sel.Agent != nil {
		return sel.Agent.ID
	}
	return ""
}

func (r *Runtime) agentTools(agentID string) []string {
	d, ok := r.Registry.Get(agentID)
	if !ok {
		return nil
	}
	return d.AllowedTools
}

// ---- outcome helpers ------------------------------------------------------

func completedOutcome(req *executor.WorkRequest, start, now time.Time, obj Objective, obs []Observation, usage BudgetUsage, agentID, result string) *executor.Outcome {
	parts := []string{
		fmt.Sprintf("state=%s", StateCompleted),
		"objective=" + obj.Description,
		fmt.Sprintf("agent=%s", agentID),
		fmt.Sprintf("iterations=%d tool_calls=%d delegations=%d model_calls=%d replans=%d observations=%d",
			usage.Iterations, usage.ToolCalls, usage.Delegations, usage.ModelCalls, usage.Replans, len(obs)),
	}
	if result != "" {
		parts = append(parts, "result="+result)
	}
	return &executor.Outcome{
		TaskID: req.TaskID, AgentID: agentID, Status: "completed",
		Output: strings.Join(parts, " | "), Duration: now.Sub(start), CorrelationID: req.CorrelationID,
		BusinessID: req.BusinessID, CreatedAt: start, CompletedAt: now,
	}
}

func failedOutcome(req *executor.WorkRequest, start, now time.Time, s State, msg string) *executor.Outcome {
	return &executor.Outcome{TaskID: req.TaskID, Status: "failed", Error: fmt.Sprintf("state=%s: %s", s, msg),
		Output: fmt.Sprintf("state=%s", s), Duration: now.Sub(start), CorrelationID: req.CorrelationID,
		BusinessID: req.BusinessID, CreatedAt: start, CompletedAt: now}
}

func cancelledOutcome(req *executor.WorkRequest, start, now time.Time) *executor.Outcome {
	return &executor.Outcome{TaskID: req.TaskID, Status: "cancelled", Error: fmt.Sprintf("state=%s: cancelled before completion", StateCancelled),
		Output: fmt.Sprintf("state=%s", StateCancelled), Duration: now.Sub(start), CorrelationID: req.CorrelationID,
		BusinessID: req.BusinessID, CreatedAt: start, CompletedAt: now}
}

// stateEvent maps a loop state to its canonical event type (§13).
func stateEvent(s State) event.EventType {
	switch s {
	case StateCreated:
		return event.EventTypeObjectiveStarted
	case StatePlanning:
		return event.EventTypePlanCreated
	case StateExecuting:
		return event.EventTypeStepStarted
	case StateObserving:
		return event.EventTypeObservationCreated
	case StateReplanning:
		return event.EventTypePlanReplanned
	case StateCompleted, StateFailed, StateCancelled, StateBudgetExhausted, StateDeadlineExceeded,
		StateDenied, StatePendingApproval, StateEscalated:
		return event.EventTypeObjectiveCompleted
	default:
		return event.EventTypeObjectiveStarted
	}
}

func rejectionSummary(res ValidationResult) string {
	parts := make([]string, 0, len(res.Rejections))
	for _, rj := range res.Rejections {
		if rj.StepID != "" {
			parts = append(parts, rj.StepID+": "+rj.Reason)
		} else {
			parts = append(parts, rj.Reason)
		}
	}
	return strings.Join(parts, "; ")
}

// admitNonTool runs governance admission AND constraint enforcement for a
// consequential action that is not a mediated tool call: delegation (it starts
// another execution) and durable memory writes/deletes (they change persistent
// state).
//
// target is the RUNTIME-ESTABLISHED thing the effect will actually touch — the
// child agent id, or the record id the memory platform derived — never a value
// the model chose. That is what makes the decision meaningful and what makes
// constraint enforcement possible: an allowlist is compared against the real
// effect, not the request.
//
// An ALLOW_WITH_CONSTRAINTS is not enough to proceed. Every returned constraint
// is checked against target through the single shared enforcement point, and a
// constraint the runtime cannot enforce fails closed (contract §4).
//
// It returns done=false when the action may proceed. The caller keeps its own
// observation so the status, source and action type stay correct; a refused
// action returns the observation with the governance state instead.
func (r *Runtime) admitNonTool(rn *run, agentID, action, resourceType, resource string) (
	*Observation, State, string, bool, string) {
	obs := &Observation{ObservationID: newID("obs"), Timestamp: r.now(),
		Source: "governance", ActionType: ActionType(action)}
	proposalID := ""

	if r.Exec == nil || r.Exec.Controller == nil {
		// Fail closed: governance unavailable is a denial, never a silent allow.
		obs.Status = "denied"
		obs.Text = "no governance controller configured (fail closed)"
		obs.Result = map[string]string{"governance_outcome": governance.DENY.String(), "action": action}
		return obs, StateDenied, obs.Text, true, proposalID
	}
	proposalID = newID("prop")
	adm, err := r.Exec.Controller.Admit(control.Proposal{
		ProposalID: proposalID,
		// execID is the identity the loop itself runs under (correlation id,
		// falling back to the task id) — the same value the objective, the plan
		// and the events use, so an approval binds to THIS execution and to
		// nothing else.
		CorrelationID: rn.execID,
		ExecutionID:   rn.execID,
		ObjectiveID:   rn.execID,
		StepID:        rn.req.TaskID,
		ActorID:       rn.req.ActorID,
		AgentID:       agentID,
		BusinessID:    rn.businessID,
		DivisionID:    rn.divisionID,
		Action:        action,
		Resource:      resource,
		ResourceType:  resourceType,
	})
	if err != nil {
		// Cancellation is reported, never mislabeled as a policy denial:
		// the action was not refused, its execution is gone.
		if errors.Is(err, control.ErrRequestCancelled) {
			obs.Status = "cancelled"
			obs.Text = err.Error()
			obs.Result = map[string]string{"governance_outcome": "cancelled", "action": action}
			return obs, StateCancelled, obs.Text, true, proposalID
		}
		obs.Status = "denied"
		obs.Text = err.Error()
		obs.Result = map[string]string{"governance_outcome": governance.DENY.String(), "action": action}
		return obs, StateDenied, obs.Text, true, proposalID
	}
	if adm.Allowed() {
		// ALLOW or ALLOW_WITH_CONSTRAINTS. The constraints are enforced here, not
		// merely recorded: an unchecked ALLOW_WITH_CONSTRAINTS must never reach
		// the effect.
		effect, cerr := control.ConstrainEffect(action, resource, adm)
		if cerr != nil {
			obs.Status = "denied"
			obs.Text = cerr.Error()
			obs.Result = map[string]string{
				"governance_outcome": governance.DENY.String(),
				"action":             action,
				"constraint_failure": "unenforceable",
			}
			rn.emit(event.EventTypeActionRejected, map[string]string{
				"action": action, "reason": "constraint cannot be enforced (fail closed)"})
			return obs, StateDenied, cerr.Error(), true, proposalID
		}
		// Constraints were satisfied by this very effect; that is an admission
		// fact, so it rides the existing proposal fact rather than a new event.
		for _, applied := range effect.Applied {
			rn.emit(event.EventTypeActionProposed, map[string]string{
				"action": action, "status": "constrained",
				"constraint_id": applied.ID, "constraint_type": applied.Kind,
			})
		}
		return nil, "", "", false, proposalID
	}
	obs.Text = admissionMessage(adm)
	obs.Status = "denied"
	obs.Result = map[string]string{"governance_outcome": adm.Decision.Outcome.String(), "action": action}
	if adm.ApprovalID != "" {
		obs.Result["approval_id"] = adm.ApprovalID
	}
	if adm.EscalationID != "" {
		obs.Result["escalation_id"] = adm.EscalationID
	}
	if adm.Decision.MatchedPolicyID != "" {
		obs.Result["policy_id"] = adm.Decision.MatchedPolicyID
	}
	switch adm.Decision.Outcome {
	case governance.REQUIRE_APPROVAL:
		obs.Status = "pending_approval"
		return obs, StatePendingApproval, obs.Text, true, proposalID
	case governance.ESCALATE:
		obs.Status = "escalated"
		return obs, StateEscalated, obs.Text, true, proposalID
	default:
		obs.Status = "denied"
		return obs, StateDenied, obs.Text, true, proposalID
	}
}

// admitMemoryDelete admits a delete whose effect spans a SET of records. The
// proposal names the resolved set (each record the caller can actually see under
// the key), so a policy that pins specific records is checked against the real
// target set instead of a model-supplied scope.
func (r *Runtime) admitMemoryDelete(rn *run, agentID string, recordIDs []string) (control.Admission, error) {
	if r.Exec == nil || r.Exec.Controller == nil {
		return control.Admission{}, fmt.Errorf("no governance controller configured (fail closed)")
	}
	return r.Exec.Controller.Admit(control.Proposal{
		ProposalID:    newID("prop"),
		CorrelationID: rn.execID, ExecutionID: rn.execID, ObjectiveID: rn.execID,
		StepID:     rn.req.TaskID,
		ActorID:    rn.req.ActorID,
		AgentID:    agentID,
		BusinessID: rn.businessID, DivisionID: rn.divisionID,
		Action:       control.ActionMemoryDelete,
		Resource:     memoryDeleteResource(recordIDs),
		ResourceType: control.ResourceTypeMemory,
		// The resolved records travel as bounded evaluation context so a policy
		// can be written against them, never as authority.
		Context: map[string]string{"record_count": fmt.Sprint(len(recordIDs))},
	})
}

// memoryDeleteResource renders the resolved target set as ONE stable resource id,
// so a policy's resource_ids can name an exact multi-record delete.
func memoryDeleteResource(recordIDs []string) string {
	sorted := append([]string(nil), recordIDs...)
	sort.Strings(sorted)
	return "memory-delete:" + strings.Join(sorted, ",")
}

// finishMemoryDeleteAdmission applies the shared non-tool enforcement rules to a
// delete admission: constraints are enforced against every record in the resolved
// set, and a blocked decision stops the delete.
func finishMemoryDeleteAdmission(obs *Observation, adm control.Admission) (*Observation, State, string, bool) {
	if adm.Allowed() {
		effect, cerr := control.ConstrainEffect(control.ActionMemoryDelete, adm.Proposal.Resource, adm)
		if cerr != nil {
			obs.Status, obs.Text = "denied", cerr.Error()
			return obs, StateDenied, cerr.Error(), true
		}
		for _, applied := range effect.Applied {
			obs.Result = appendConstraintEvidence(obs.Result, applied)
		}
		return obs, "", "", false
	}
	obs.Text = admissionMessage(adm)
	obs.Status = "denied"
	obs.Result = map[string]string{"governance_outcome": adm.Decision.Outcome.String(),
		"action": control.ActionMemoryDelete}
	if adm.ApprovalID != "" {
		obs.Result["approval_id"] = adm.ApprovalID
	}
	if adm.EscalationID != "" {
		obs.Result["escalation_id"] = adm.EscalationID
	}
	switch adm.Decision.Outcome {
	case governance.REQUIRE_APPROVAL:
		obs.Status = "pending_approval"
		return obs, StatePendingApproval, obs.Text, true
	case governance.ESCALATE:
		obs.Status = "escalated"
		return obs, StateEscalated, obs.Text, true
	default:
		return obs, StateDenied, obs.Text, true
	}
}

func appendConstraintEvidence(dst map[string]string, c control.EffectiveConstraint) map[string]string {
	if dst == nil {
		dst = map[string]string{}
	}
	dst["constraint_id"] = c.ID
	dst["constraint_type"] = c.Kind
	return dst
}

func admissionMessage(adm control.Admission) string {
	switch adm.Decision.Outcome {
	case governance.REQUIRE_APPROVAL:
		return fmt.Sprintf("governance requires approval for %s on %s (approval_id=%s)",
			adm.Proposal.Action, adm.Proposal.Resource, adm.ApprovalID)
	case governance.ESCALATE:
		return fmt.Sprintf("governance escalated %s on %s (escalation_id=%s): %s",
			adm.Proposal.Action, adm.Proposal.Resource, adm.EscalationID, adm.Decision.Reason)
	default:
		return fmt.Sprintf("governance denied %s on %s: %s",
			adm.Proposal.Action, adm.Proposal.Resource, adm.Decision.Reason)
	}
}

// ---- governance terminal outcomes -------------------------------------------

// deniedOutcome is a governance refusal of one proposed action. It is its own
// state, never StateFailed: the action was blocked before any side effect, and
// the objective may still replan within its remaining authority.
func deniedOutcome(req *executor.WorkRequest, start, now time.Time, message string) *executor.Outcome {
	return &executor.Outcome{
		TaskID: req.TaskID, Status: "denied",
		Error:    fmt.Sprintf("state=%s: %s", StateDenied, message),
		Output:   fmt.Sprintf("state=%s", StateDenied),
		Duration: now.Sub(start), CorrelationID: req.CorrelationID, BusinessID: req.BusinessID,
		CreatedAt: start, CompletedAt: now,
	}
}

// pendingApprovalOutcome stops the execution BEFORE the consequential side
// effect and exposes the approval id so the existing approval surface can decide
// it. Approval is not execution: nothing ran.
func pendingApprovalOutcome(req *executor.WorkRequest, start, now time.Time, message, approvalID string) *executor.Outcome {
	return &executor.Outcome{
		TaskID: req.TaskID, Status: "pending_approval",
		Error:      fmt.Sprintf("state=%s: %s", StatePendingApproval, message),
		Output:     fmt.Sprintf("state=%s", StatePendingApproval),
		ApprovalID: approvalID,
		Duration:   now.Sub(start), CorrelationID: req.CorrelationID, BusinessID: req.BusinessID,
		CreatedAt: start, CompletedAt: now,
	}
}

// escalatedOutcome hands the action to the existing escalation → attention
// path. The action stays blocked; attention never authorizes it.
func escalatedOutcome(req *executor.WorkRequest, start, now time.Time, message, escalationID string) *executor.Outcome {
	return &executor.Outcome{
		TaskID: req.TaskID, Status: "escalated",
		Error:         fmt.Sprintf("state=%s: %s", StateEscalated, message),
		Output:        fmt.Sprintf("state=%s", StateEscalated),
		EscalationRef: escalationID,
		Duration:      now.Sub(start), CorrelationID: req.CorrelationID, BusinessID: req.BusinessID,
		CreatedAt: start, CompletedAt: now,
	}
}
