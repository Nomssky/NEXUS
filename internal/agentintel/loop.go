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
	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/foundation/agent"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
	"github.com/Nomssky/NEXUS/internal/memory"
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
		MaxTokens:      512,
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
func (r *Runtime) Run(ctx context.Context, req *executor.WorkRequest, ag *agent.Agent) (*executor.Outcome, error) {
	start := r.now()
	obj, budget, err := parseObjective(req.Input["agent_objective"], r.Caps)
	if err != nil {
		return failedOutcome(req, start, r.now(), StateFailed, "invalid objective: "+err.Error()), nil
	}
	execID := req.CorrelationID
	if execID == "" {
		execID = req.TaskID
	}
	rn := &run{rt: r, req: req, execID: execID, businessID: req.BusinessID, divisionID: req.DivisionID, ag: ag}

	deadline := start.Add(budget.MaxExecutionTime)
	loopCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	rn.transition(StateCreated, map[string]string{"objective": obj.Description})
	rn.emit(event.EventTypeObjectiveStarted, map[string]string{"objective_id": obj.ObjectiveID, "deadline_ms": fmt.Sprint(budget.MaxExecutionTime.Milliseconds())})

	// ---- planning (advisory) --------------------------------------------
	rn.transition(StatePlanning, nil)
	if ctx.Err() != nil {
		rn.transition(StateCancelled, nil)
		return cancelledOutcome(req, start, r.now()), nil
	}
	steps, planErr := r.planner().Plan(loopCtx, PlanPrompt{Objective: obj.Description, Context: obj.Context})
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
	plan := &Plan{PlanID: execID, Version: 1, Steps: steps, ObjectiveID: obj.ObjectiveID,
		CorrelationID: execID, BusinessID: req.BusinessID, DivisionID: req.DivisionID, CreatedAt: r.now()}
	rn.emit(event.EventTypePlanCreated, map[string]string{"plan_id": execID, "version": "1", "steps": fmt.Sprint(len(plan.Steps))})

	// ---- validation (fail-closed before any action) ----------------------
	res := ValidatePlan(plan, r.scope(), r.Caps)
	rn.emit(event.EventTypePlanValidated, map[string]string{"valid": fmt.Sprint(res.Valid), "rejections": fmt.Sprint(len(res.Rejections))})
	if !res.Valid {
		rn.transition(StateFailed, map[string]string{"reason": "plan rejected"})
		return failedOutcome(req, start, r.now(), StateFailed, "plan rejected: "+rejectionSummary(res)), nil
	}
	// The step ledger is runtime-owned scratch for this plan version: the
	// model proposed the steps, but readiness, execution order and terminal
	// facts are decided here (contract §3, §4).
	ledger := newPlanLedger(plan)

	agentID := r.resolveAgent(obj, req)
	usage := BudgetUsage{}
	// Working memory is process-local scratch state for THIS objective: bounded,
	// never persisted, and discarded when the execution ends (contract §2, §36).
	working := NewWorkingMemory()
	working.Bind(obj.Description)
	working.SetDeadline(deadline)
	memIdentity := r.memoryIdentity(rn, agentID)
	observations := []Observation{}
	contextAnnounced := false
	contextTruncatedAnnounced := false

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
			Tools:   r.toolHints(req, agentID),
			Memory:  assembledMemoryRecords(assembled),
			Context: assembled.Text,
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
		// A completed mutating effect is never silently repeated: the
		// fingerprint of every consequential mutation this execution performed
		// is recorded, and an identical proposal is rejected before anything
		// can run again (contract §10). Reads may repeat — the reliability
		// layer itself retries reads — and resume is a new execution with
		// fresh scratch state, so legitimate re-execution is unaffected.
		if fp := repeatGuardFingerprint(r.scope(), action); fp != "" && working.HasEffect(fp) {
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
		obs, next, termState, termMsg := r.perform(rn, loopCtx, obj, action, agentID, working, usage, budget)
		usage = next
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
					return pendingApprovalOutcome(req, start, r.now(), termMsg, approvalID), nil
				default:
					return escalatedOutcome(req, start, r.now(), termMsg, escalationRef), nil
				}
			}
			rn.transition(termState, map[string]string{"reason": termMsg})
			return failedOutcome(req, start, r.now(), termState, termMsg), nil
		}

		if action.Type == ActionContinue {
			usage.NoProgress++
			rn.emit(event.EventTypeBudgetExhausted, map[string]string{"budget": "no_progress", "used": fmt.Sprint(usage.NoProgress)})
			if usage.NoProgress >= r.Caps.MaxConsecutiveNoProgress {
				rn.transition(StateFailed, map[string]string{"reason": "no actionable output"})
				return failedOutcome(req, start, r.now(), StateFailed,
					fmt.Sprintf("model produced no actionable output %d times in a row", usage.NoProgress)), nil
			}
			continue
		}
		usage.NoProgress = 0

		switch action.Type {
		case ActionReplan:
			usage.Replans++
			if usage.Replans > budget.MaxReplans {
				rn.emit(event.EventTypeBudgetExhausted, map[string]string{"budget": "replans", "used": fmt.Sprint(usage.Replans)})
				rn.transition(StateBudgetExhausted, nil)
				return failedOutcome(req, start, r.now(), StateBudgetExhausted,
					fmt.Sprintf("replan budget exhausted (%d)", budget.MaxReplans)), nil
			}
			if ctx.Err() != nil {
				rn.transition(StateCancelled, nil)
				return cancelledOutcome(req, start, r.now()), nil
			}
			rn.transition(StateReplanning, map[string]string{"reason": action.Reason, "version": fmt.Sprint(plan.Version + 1)})
			newSteps, perr := r.planner().Plan(loopCtx, PlanPrompt{Objective: obj.Description, Context: obj.Context})
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
				Reason: action.Reason, Steps: newSteps, ObjectiveID: obj.ObjectiveID,
				CorrelationID: execID, BusinessID: req.BusinessID, DivisionID: req.DivisionID, CreatedAt: r.now()}
			nres := ValidatePlan(newPlan, r.scope(), r.Caps)
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
			ledger = ledger.carryCompleted(newPlan)
		case ActionToolCall, ActionDelegate, ActionModelCall, ActionMemoryRead, ActionMemoryWrite, ActionMemoryDelete:
		}
	}
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
func (r *Runtime) perform(rn *run, ctx context.Context, obj Objective, a Action, agentID string, working *WorkingMemory, usage BudgetUsage, budget Budget) (*Observation, BudgetUsage, State, string) {
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
		obs := &Observation{ObservationID: newID("obs"), Source: "tool:" + a.Tool, ActionType: ActionToolCall,
			Status: "ok", Timestamp: r.now()}
		out, err := r.Exec.InvokeToolScoped(ctx, agentexec.ToolScope{
			ActorID: rn.req.ActorID, BusinessID: rn.businessID, DivisionID: rn.divisionID,
			AgentTools: r.agentTools(agentID), CorrelationID: rn.req.CorrelationID,
		}, agentID, agentexec.ToolCallSpec{ToolID: a.Tool, Operation: a.Operation, Input: a.Input})
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
	if len(a.DelegateTools) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(a.AgentID) == "" {
		return nil, fmt.Errorf("delegation failed: delegate_tools requires an explicit agent_id")
	}
	childDef, ok := r.Registry.Get(a.AgentID)
	if !ok {
		return nil, fmt.Errorf("delegation failed: child agent %q is not registered", a.AgentID)
	}
	_ = childDef
	parentTools := r.agentTools(agentID)
	if strings.TrimSpace(agentID) == "" {
		return nil, fmt.Errorf("delegation failed: cannot bound child tools without a resolved parent agent")
	}
	out := make([]agentexec.ToolCallSpec, 0, len(a.DelegateTools))
	for i, t := range a.DelegateTools {
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
			return nil, fmt.Errorf("delegation failed: %s tool %q is not in child agent %q's allowlist", where, t.Tool, a.AgentID)
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
func (r *Runtime) resolveAgent(obj Objective, req *executor.WorkRequest) string {
	if obj.Context != nil {
		if id := obj.Context["agent_id"]; id != "" {
			if d, ok := r.Registry.Get(id); ok && d.BusinessID == req.BusinessID {
				return id
			}
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
