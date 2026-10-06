package agentintel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Nomssky/NEXUS/internal/agentexec"
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

// Decide asks the model for the next structured action (§6).
func (d *Decision) Decide(ctx context.Context, p DecisionPrompt) (string, error) {
	if d == nil || d.Router == nil {
		return "", fmt.Errorf("no model router wired")
	}
	return d.invoke(ctx, p.StepID, buildDecisionMessages(p))
}

// Plan asks the model for an advisory plan (§3).
func (d *Decision) Plan(ctx context.Context, p PlanPrompt) (string, error) {
	if d == nil || d.Router == nil {
		return "", fmt.Errorf("no model router wired")
	}
	return d.invoke(ctx, "plan", buildPlanMessages(p))
}

func (d *Decision) invoke(ctx context.Context, id string, lines []string) (string, error) {
	routeReq := &modelrouter.RoutingRequest{
		RequestID:       id,
		AgentID:         d.AgentID,
		RequiredCaps:    []modelrouter.ModelCapability{modelrouter.CapabilityStructuredOutput},
		FallbackEnabled: true,
	}
	gen := &modelrouter.GenerateRequest{
		RequestID:      id,
		Messages:       toMessages(lines),
		ResponseFormat: "json",
		MaxTokens:      512,
	}
	resp, _, err := d.Router.Invoke(ctx, routeReq, gen)
	if err != nil {
		return "", fmt.Errorf("provider invocation failed: %w", err)
	}
	return resp.Content, nil
}

// toMessages maps prompts onto provider messages. Every prompt is sent as
// untrusted user content: the runtime composes the instruction envelope, so
// no provider-visible string carries runtime authority (§8).
func toMessages(lines []string) []modelrouter.Message {
	msgs := make([]modelrouter.Message, 0, len(lines))
	for _, l := range lines {
		msgs = append(msgs, modelrouter.Message{Role: "user", Content: l})
	}
	return msgs
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
	steps, planErr := r.planner().Plan(loopCtx, PlanPrompt{Objective: obj.Description, Context: obj.Context})
	if planErr != nil {
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

	agentID := r.resolveAgent(obj, req)
	usage := BudgetUsage{}
	// Working memory is process-local scratch state for THIS objective: bounded,
	// never persisted, and discarded when the execution ends (contract §2, §36).
	working := NewWorkingMemory()
	working.Bind(obj.Description)
	working.SetDeadline(deadline)
	memIdentity := r.memoryIdentity(rn, agentID)
	observations := []Observation{}
	stepIdx := 0
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

		step := Step{StepID: "finalize", Intent: "produce the final result"}
		if stepIdx < len(plan.Steps) {
			step = plan.Steps[stepIdx]
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
		})
		if derr != nil {
			if ctx.Err() != nil {
				rn.transition(StateCancelled, nil)
				return cancelledOutcome(req, start, r.now()), nil
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
			Used: usage, Budget: budget, Depth: 1,
			StepHasWork: stepIdx < len(plan.Steps) || len(observations) > 0,
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
		rn.emit(event.EventTypeActionProposed, map[string]string{"action": string(action.Type), "step": step.StepID})

		// ---- act --------------------------------------------------------
		working.SetPending(string(action.Type) + " " + firstNonEmpty(action.Tool, action.Key, action.AgentID))
		obs, next, termState, termMsg := r.perform(rn, loopCtx, obj, action, agentID, working, usage, budget)
		usage = next
		if obs != nil {
			observations = append(observations, *obs)
			working.AddObservation(*obs)
			_ = working.Put(obs.ObservationID, obs.Source+": "+obs.Status)
			rn.transition(StateObserving, map[string]string{"observation": obs.ObservationID, "status": obs.Status})
			rn.emit(event.EventTypeObservationCreated, map[string]string{"observation_id": obs.ObservationID,
				"source": obs.Source, "action_type": string(obs.ActionType), "status": obs.Status})
			rn.emit(event.EventTypeStepCompleted, map[string]string{"step": step.StepID, "observation": obs.ObservationID})
		} else {
			rn.emit(event.EventTypeStepCompleted, map[string]string{"step": step.StepID, "observation": ""})
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
			rn.transition(StateReplanning, map[string]string{"reason": action.Reason, "version": fmt.Sprint(plan.Version + 1)})
			newSteps, perr := r.planner().Plan(loopCtx, PlanPrompt{Objective: obj.Description, Context: obj.Context})
			if perr != nil {
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
			plan = newPlan
			stepIdx = 0
		case ActionToolCall, ActionDelegate, ActionModelCall, ActionMemoryRead, ActionMemoryWrite, ActionMemoryDelete:
			if stepIdx < len(plan.Steps) {
				stepIdx++
			}
		}
		if stepIdx >= len(plan.Steps) && action.Type != ActionComplete {
			stepIdx = len(plan.Steps) // next iteration is the finalize decision
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
		rn.emit(event.EventTypeAgentDelegated, map[string]string{"agent_id": a.AgentID, "depth": "1"})
		out, err := r.Exec.DelegateChild(ctx, rn.req, rn.ag, agentexec.DelegateSpec{
			ID: newID("child"), Intent: a.Objective, AgentID: a.AgentID,
		}, 0)
		if err != nil || out == nil || out.Status != "completed" {
			msg := "delegation failed"
			if err != nil {
				msg = err.Error()
			} else if out != nil {
				msg = fmt.Sprintf("child execution ended %s: %s", out.Status, out.Error)
			}
			obs.Status, obs.Text = "failed", msg
			return obs, usage, StateFailed, msg
		}
		obs.Text = out.Output
		return obs, usage, "", ""
	case ActionMemoryWrite:
		obs := &Observation{ObservationID: newID("obs"), Source: "memory", ActionType: ActionMemoryWrite,
			Status: "ok", Timestamp: r.now()}
		rec, err := r.memoryWrite(rn, agentID, a, observationsFor(working))
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
		n, err := r.memoryDelete(rn, agentID, a.Key)
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
