package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/foundation/attention"
	"github.com/Nomssky/NEXUS/internal/foundation/cognition"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/hardening"
	"github.com/Nomssky/NEXUS/internal/foundation/memory"
	"github.com/Nomssky/NEXUS/internal/foundation/scheduler"
	"github.com/Nomssky/NEXUS/internal/foundation/workflow"
)

// ChainStep represents a step in the canonical execution chain.
type ChainStep string

const (
	// Admission pipeline stages (RUNTIME §3.1): validate (2), identity (3),
	// authorization (4) run as a block before governance (5); resource_check
	// (7) runs before schedule (8).
	StepValidate      ChainStep = "validate"
	StepIdentity      ChainStep = "identity"
	StepAuthorization ChainStep = "authorization"
	StepGovernance    ChainStep = "governance"
	StepHardening     ChainStep = "hardening"
	StepMemoryRead    ChainStep = "memory_read"
	StepAttention     ChainStep = "attention_score"
	StepObjective     ChainStep = "objective"
	StepDecision      ChainStep = "decision"
	StepPlan          ChainStep = "plan"
	StepWorkflow      ChainStep = "workflow"
	StepResourceCheck ChainStep = "resource_check"
	StepSchedule      ChainStep = "schedule"
	StepAgent         ChainStep = "agent"
	StepModel         ChainStep = "model"
	StepTool          ChainStep = "tool"
	StepVerify        ChainStep = "verify"
	StepMemoryWrite   ChainStep = "memory_write"
)

// executeChain runs the canonical execution chain for a request.
//
//	OWNER REQUEST
//	  → VALIDATE → GOVERNANCE
//	  → EXECUTIVE → OBJECTIVE → DECISION → PLANNER
//	  → WORKFLOW → SCHEDULE → AGENT → MODEL/TOOL
//	  → VERIFY → OUTCOME
func (e *Engine) executeChain(ctx context.Context, req *Request) *Response {
	start := e.now()
	audit := make([]AuditEntry, 0, 14)
	e.chainEmit(req, "chain.started", "core", req.Intent)

	// Step 1: Validate
	if err := e.chainValidate(ctx, req); err != nil {
		return e.chainError(req, err, StepValidate, audit, start)
	}
	audit = append(audit, AuditEntry{
		Step:      string(StepValidate),
		Action:    "validate request",
		Actor:     "core",
		Timestamp: e.now(),
		Duration:  e.now().Sub(start),
		Outcome:   "passed",
	})
	e.chainEmit(req, "chain.validate.passed", "core", "passed")

	// Admission stages 3-4 (RUNTIME §3.1): IDENTITY then AUTHORIZATION,
	// immediately after VALIDATE and before every later stage — no stage
	// may be skipped (RT-01), so both always append an audit entry even
	// when enforcement is off (outcome records the not-enforced posture).
	idOutcome, err := e.chainIdentity(ctx, req)
	if err != nil {
		return e.chainError(req, err, StepIdentity, audit, start)
	}
	audit = append(audit, AuditEntry{
		Step:      string(StepIdentity),
		Action:    "verify identity",
		Actor:     "identity",
		Timestamp: e.now(),
		Duration:  e.now().Sub(start),
		Outcome:   idOutcome,
	})
	e.chainEmit(req, "chain.identity.checked", "identity", idOutcome)

	authzOutcome, err := e.chainAuthorization(ctx, req)
	if err != nil {
		return e.chainError(req, err, StepAuthorization, audit, start)
	}
	audit = append(audit, AuditEntry{
		Step:      string(StepAuthorization),
		Action:    "authorize business scope",
		Actor:     "governance",
		Timestamp: e.now(),
		Duration:  e.now().Sub(start),
		Outcome:   authzOutcome,
	})
	e.chainEmit(req, "chain.authorization.checked", "governance", authzOutcome)

	// E-005: cancelled while queued — the request never executes beyond this
	// point (objective/workflow creation and executor submission are skipped).
	if e.isCancelRequested(req.ID) {
		return e.chainCancelled(ctx, req, audit, start, "request cancelled before execution")
	}

	// Hardening: circuit breaker gate
	if !e.circuitBreaker.Allow() {
		err := fmt.Errorf("circuit breaker open: too many recent failures")
		return e.chainError(req, err, StepHardening, audit, start)
	}
	e.chainEmit(req, "chain.hardening.circuit_breaker.ok", "hardening", e.circuitBreaker.State())

	// Step 2: Governance check. The decision is kept so an
	// ALLOW_WITH_CONSTRAINTS outcome can surface its constraints on the
	// Response (N2) — proceeding on IsAllowing alone discarded them.
	govDecision, govErr := e.chainGovernance(ctx, req)
	if govErr != nil {
		return e.chainError(req, govErr, StepGovernance, audit, start)
	}
	audit = append(audit, AuditEntry{
		Step:      string(StepGovernance),
		Action:    "governance check",
		Actor:     "governance",
		Timestamp: e.now(),
		Duration:  e.now().Sub(start),
		Outcome:   "allowed",
	})
	e.chainEmit(req, "chain.governance.passed", "governance", "allowed")

	// Step 2b: Retrieve relevant context from memory
	memContext := e.chainMemoryRead(ctx, req)
	audit = append(audit, AuditEntry{
		Step:      string(StepMemoryRead),
		Action:    "retrieve context",
		Actor:     "memory",
		Timestamp: e.now(),
		Duration:  e.now().Sub(start),
		Outcome:   fmt.Sprintf("found=%d", len(memContext)),
	})
	e.chainEmit(req, "chain.memory.read", "memory", fmt.Sprintf("found=%d", len(memContext)))

	// Step 2c: Attention scoring
	attItem, attErr := e.chainAttentionScore(ctx, req)
	suppressed := false
	if attItem != nil {
		suppressed = e.attentionEng.ShouldSuppress(attItem)
	}
	attOutcome := fmt.Sprintf("score=%.2f suppressed=%v", attItemScore(attItem), suppressed)
	if attErr != nil {
		// C-011 fix: attention failures are recorded in the audit trail,
		// not silently absorbed. The chain continues (attention is advisory).
		attOutcome = fmt.Sprintf("score=0.00 suppressed=false attention_error=%v", attErr)
	}
	audit = append(audit, AuditEntry{
		Step:      string(StepAttention),
		Action:    "score attention",
		Actor:     "attention",
		Timestamp: e.now(),
		Duration:  e.now().Sub(start),
		Outcome:   attOutcome,
	})
	e.chainEmit(req, "chain.attention.scored", "attention", fmt.Sprintf("score=%.2f", attItemScore(attItem)))

	// Step 3: Create objective from intent
	objective, err := e.chainObjective(ctx, req)
	if err != nil {
		return e.chainError(req, err, StepObjective, audit, start)
	}
	audit = append(audit, AuditEntry{
		Step:      string(StepObjective),
		Action:    "create objective",
		Actor:     "objective-engine",
		Timestamp: e.now(),
		Duration:  e.now().Sub(start),
		Outcome:   fmt.Sprintf("objective=%s", objective.ID),
	})
	e.chainEmit(req, "chain.objective.created", "objective-engine", objective.ID)

	// Step 4: Decision
	decision, err := e.chainDecision(ctx, req, objective)
	if err != nil {
		return e.chainError(req, err, StepDecision, audit, start)
	}
	audit = append(audit, AuditEntry{
		Step:      string(StepDecision),
		Action:    "make decision",
		Actor:     "decision-engine",
		Timestamp: e.now(),
		Duration:  e.now().Sub(start),
		Outcome:   fmt.Sprintf("decision=%s", decision.ID),
	})
	e.chainEmit(req, "chain.decision.made", "decision-engine", decision.ID)

	// Step 5: Plan
	plan, err := e.chainPlan(ctx, req, objective, decision)
	if err != nil {
		return e.chainError(req, err, StepPlan, audit, start)
	}
	audit = append(audit, AuditEntry{
		Step:      string(StepPlan),
		Action:    "create plan",
		Actor:     "planner",
		Timestamp: e.now(),
		Duration:  e.now().Sub(start),
		Outcome:   fmt.Sprintf("plan=%s", plan.ID),
	})
	e.chainEmit(req, "chain.plan.created", "planner", plan.ID)

	// Step 6: Create workflow from plan
	wf, err := e.chainWorkflow(ctx, req, objective, plan)
	if err != nil {
		return e.chainError(req, err, StepWorkflow, audit, start)
	}
	audit = append(audit, AuditEntry{
		Step:      string(StepWorkflow),
		Action:    "create workflow",
		Actor:     "workflow-engine",
		Timestamp: e.now(),
		Duration:  e.now().Sub(start),
		Outcome:   fmt.Sprintf("workflow=%s", wf.ID),
	})
	e.chainEmit(req, "chain.workflow.created", "workflow-engine", wf.ID)

	// Admission stage 7 (RUNTIME §3.1): RESOURCE CHECK before SCHEDULE.
	// Pre-check only — admission never guarantees capacity at execution
	// time (RUNTIME §3.2); a capacity race still surfaces at Submit.
	resOutcome, err := e.chainResourceCheck(ctx, req)
	if err != nil {
		return e.chainError(req, err, StepResourceCheck, audit, start)
	}
	audit = append(audit, AuditEntry{
		Step:      string(StepResourceCheck),
		Action:    "check resource availability",
		Actor:     "scheduler",
		Timestamp: e.now(),
		Duration:  e.now().Sub(start),
		Outcome:   resOutcome,
	})
	e.chainEmit(req, "chain.resource.checked", "scheduler", resOutcome)

	// Step 7: Schedule
	job, err := e.chainSchedule(ctx, req, wf)
	if err != nil {
		return e.chainError(req, err, StepSchedule, audit, start)
	}
	audit = append(audit, AuditEntry{
		Step:      string(StepSchedule),
		Action:    "schedule job",
		Actor:     "scheduler",
		Timestamp: e.now(),
		Duration:  e.now().Sub(start),
		Outcome:   fmt.Sprintf("job=%s", job.ID),
	})
	e.chainEmit(req, "chain.schedule.scheduled", "scheduler", job.ID)

	// Step 8: Execute via task executor
	execOutcome, execErr := e.chainExecute(ctx, req, wf)
	// E-005: a cancellation that landed after dequeue but before the executor
	// submit — cancelled, not failed. No failure audit entry, no circuit-breaker
	// failure, no recovery record; workflow bookkeeping is cancelled (the task
	// never ran, so no executor-side context cancellation happened).
	if errors.Is(execErr, errCancelledBeforeSubmit) {
		_ = e.workflowEng.Cancel(wf.ID)
		return e.chainCancelled(ctx, req, audit, start, "request cancelled before execution")
	}
	// C-5: a cancellation that raced an executor error must be reported as
	// cancelled, never as a failure (E-005: cancellation is not a failure).
	// The executor's wait-expiry path already applies "first cause wins" —
	// an explicit user cancellation is never downgraded — so the chain
	// re-checks the cancel flag BEFORE recording a circuit-breaker failure
	// and a recovery record, which user cancellations must never cause.
	if execErr != nil && e.isCancelRequested(req.ID) {
		_ = e.workflowEng.Cancel(wf.ID)
		return e.chainCancelled(ctx, req, audit, start, "request cancelled before completion")
	}
	if execErr != nil {
		audit = append(audit, AuditEntry{
			Step:      string(StepAgent),
			Action:    "execute task",
			Actor:     "executor",
			Timestamp: e.now(),
			Duration:  e.now().Sub(start),
			Outcome:   fmt.Sprintf("error=%v", execErr),
		})

		// Hardening: record failure for circuit breaker
		e.circuitBreaker.RecordFailure()
		// Recovery: detect and track the failure
		failRec := e.recoveryMgr.Detect(
			hardening.FailureTaskUnknown,
			"executor",
			req.Context.BusinessID,
			fmt.Sprintf("execution failed: %v", execErr),
		)
		e.chainEmit(req, "chain.hardening.failure_detected", "hardening", failRec.ID)
	} else {
		audit = append(audit, AuditEntry{
			Step:      string(StepAgent),
			Action:    "execute task",
			Actor:     "executor",
			Timestamp: e.now(),
			Duration:  e.now().Sub(start),
			Outcome:   fmt.Sprintf("status=%s agent=%s", execOutcome.Status, execOutcome.AgentID),
		})
		e.chainEmit(req, "chain.executor.completed", "executor", execOutcome.Status)
		// C-005 fix: model/tool routing happens inside the executor and is
		// not observable at the chain layer — no phantom audit entries are
		// fabricated here. The verify entry reflects the actual outcome.
		audit = append(audit, AuditEntry{
			Step:      string(StepVerify),
			Action:    "verify outcome",
			Actor:     "executor",
			Timestamp: e.now(),
			Duration:  e.now().Sub(start),
			Outcome:   fmt.Sprintf("status=%s", execOutcome.Status),
		})
	}

	// Step 12: Outcome
	status := "completed"
	var outcomeResult *Outcome
	var respErr *ChainError
	if execErr != nil {
		status = "failed"
		// Preserve execution failure details in the response (was silently lost).
		chainErr, ok := execErr.(*ChainError)
		if !ok {
			chainErr = &ChainError{
				Code:      "EXECUTION_FAILED",
				Category:  "INTERNAL_FAILURE",
				Message:   execErr.Error(),
				ChainStep: string(StepAgent),
			}
		}
		if req.Context != nil {
			chainErr.CorrelationID = req.Context.CorrelationID
		}
		if chainErr.Timestamp.IsZero() {
			chainErr.Timestamp = e.now()
		}
		respErr = chainErr
	} else if execOutcome != nil && execOutcome.Status == "cancelled" {
		// E-005: explicit cancellation is terminal but not a failure. The
		// executor already emitted task.cancelled; the chain records the
		// contract ChainError (CANCELLED/CANCELLATION, non-retryable) and
		// cancels the workflow bookkeeping status.
		status = "cancelled"
		_ = e.workflowEng.Cancel(wf.ID)
		outcomeResult = &Outcome{
			Summary: "cancelled before completion",
			Metrics: map[string]interface{}{
				"duration_ms":     e.now().Sub(start).Milliseconds(),
				"executor_status": execOutcome.Status,
			},
		}
		if execOutcome.AgentID != "" {
			outcomeResult.Metrics["agent_id"] = execOutcome.AgentID
		}
		respErr = &ChainError{
			Code:      "CANCELLED",
			Category:  "CANCELLATION",
			Message:   "request cancelled before completion",
			ChainStep: string(StepAgent),
			Retryable: false,
		}
		if req.Context != nil {
			respErr.CorrelationID = req.Context.CorrelationID
		}
		respErr.Timestamp = e.now()
	} else if execOutcome != nil && execOutcome.Status == "denied" {
		// D1: executor-level governance DENY must surface exactly like the
		// chain-level gate — failed with POLICY_DENIED (CORE_INTERFACE_CONTRACTS
		// §3). The executor records DENY as an outcome with execErr == nil,
		// which would otherwise fall through to the default "completed" status.
		// REQUIRE_APPROVAL (pending_approval → D2) and ESCALATE (escalated
		// → D3) have their own Step-12 branches below.
		status = "failed"
		outcomeResult = &Outcome{
			Metrics: map[string]interface{}{
				"duration_ms":     e.now().Sub(start).Milliseconds(),
				"executor_status": execOutcome.Status,
			},
		}
		if execOutcome.AgentID != "" {
			outcomeResult.Metrics["agent_id"] = execOutcome.AgentID
		}
		// Preserve the executor's governance denial reason ("governance
		// denied: <reason>") in the contract message field.
		message := execOutcome.Error
		if message == "" {
			message = "governance denied"
		}
		respErr = &ChainError{
			Code:      "POLICY_DENIED",
			Category:  "POLICY_DENIED",
			Message:   message,
			ChainStep: string(StepAgent),
			Retryable: false,
		}
		if req.Context != nil {
			respErr.CorrelationID = req.Context.CorrelationID
		}
		respErr.Timestamp = e.now()
	} else if execOutcome != nil && execOutcome.Status == "pending_approval" {
		// D2: executor-level governance REQUIRE_APPROVAL surfaces through the
		// same error envelope as the chain-level gate — failed with
		// APPROVAL_REQUIRED (CORE_INTERFACE_CONTRACTS §3). No contract defines
		// pending_approval as a Response.Status; first-class approval states
		// wait for the ApprovalEngine/admission-pipeline milestone. D4:
		// Retryable is false — "wait for approval", not "retry now".
		status = "failed"
		outcomeResult = &Outcome{
			Metrics: map[string]interface{}{
				"duration_ms":     e.now().Sub(start).Milliseconds(),
				"executor_status": execOutcome.Status,
			},
		}
		if execOutcome.AgentID != "" {
			outcomeResult.Metrics["agent_id"] = execOutcome.AgentID
		}
		// Preserve the executor's governance reason ("governance requires
		// approval: <reason>") in the contract message field.
		message := execOutcome.Error
		if message == "" {
			message = "governance requires approval"
		}
		respErr = &ChainError{
			Code:      "APPROVAL_REQUIRED",
			Category:  "APPROVAL_REQUIRED",
			Message:   message,
			ChainStep: string(StepAgent),
			Retryable: false,
		}
		if req.Context != nil {
			respErr.CorrelationID = req.Context.CorrelationID
			// P1 wiring: the executor gate produced REQUIRE_APPROVAL — open
			// the approval loop from core. The executor's Decision payload
			// is not carried on the Outcome, so re-evaluate with the
			// executor's exact governance inputs (single source of truth:
			// executor.ActionExecuteTask/executor.ResourceWorkflow). Falls
			// back to the outcome reason when governance no longer yields
			// REQUIRE_APPROVAL (policy changed mid-run) — the record must
			// exist either way for the approval flow to be reachable.
			govReq := governance.Request{
				Actor:      req.Context.ActorID,
				Action:     executor.ActionExecuteTask,
				Resource:   executor.ResourceWorkflow,
				BusinessID: req.Context.BusinessID,
				// N1: mirror the executor gate's scope ids so this re-eval
				// reproduces the same decision (incl. ApprovalConfig) the
				// gate made — TaskID from the outcome, division from the
				// request context.
				TaskID:     execOutcome.TaskID,
				DivisionID: req.Context.DivisionID,
				// chainExecute sets WorkflowID = TaskID (the workflow record
				// id), so mirror it for an exact gate decision.
				WorkflowID: execOutcome.TaskID,
			}
			decision := e.govEngine.Evaluate(govReq)
			if decision.Outcome != governance.REQUIRE_APPROVAL {
				decision = governance.Decision{
					Outcome:   governance.REQUIRE_APPROVAL,
					Reason:    message,
					Timestamp: e.now(),
				}
			}
			// A handler that already opened an approval for a governance-gated
			// ACTION (agent intelligence, AGENT_GOVERNANCE_CONTROL_CONTRACTS §5)
			// owns the record: reuse its id instead of opening a second one for
			// the same work.
			if execOutcome.ApprovalID != "" {
				respErr.Details = map[string]string{"approval_id": execOutcome.ApprovalID}
			} else if ar, err := e.createApproval(req, decision, govReq); err == nil {
				respErr.Details = map[string]string{"approval_id": ar.DecisionID}
			}
		}
		respErr.Timestamp = e.now()
	} else if execOutcome != nil && execOutcome.Status == "escalated" {
		// D3: executor-level governance ESCALATE must not fall through to
		// "completed" — the handler never ran (pre-dispatch block). Same
		// envelope as the chain gate: failed / ESALATION_REQUIRED /
		// POLICY_DENIED (CORE §3 has no escalation category), Retryable
		// false, error.details.escalation_ref, governance.escalated event.
		status = "failed"
		outcomeResult = &Outcome{
			Metrics: map[string]interface{}{
				"duration_ms":     e.now().Sub(start).Milliseconds(),
				"executor_status": execOutcome.Status,
			},
		}
		if execOutcome.AgentID != "" {
			outcomeResult.Metrics["agent_id"] = execOutcome.AgentID
		}
		message := execOutcome.Error
		if message == "" {
			message = "governance escalation required"
		}
		escRef := execOutcome.EscalationRef
		if escRef == "" {
			escRef = fmt.Sprintf("esc-%s-%d", req.Context.ActorID, e.now().UnixNano())
		}
		respErr = &ChainError{
			Code:      "ESCALATION_REQUIRED",
			Category:  "POLICY_DENIED",
			Message:   message,
			Details:   map[string]string{"escalation_ref": escRef},
			ChainStep: string(StepAgent),
			Retryable: false,
		}
		if req.Context != nil {
			respErr.CorrelationID = req.Context.CorrelationID
		}
		respErr.Timestamp = e.now()
		e.emitEscalation(req, escRef, execOutcome.Error, "executor")
	} else if execOutcome != nil && execOutcome.Status == "completed" {
		outcomeResult = &Outcome{
			Summary: execOutcome.Output,
			// C-025: populate execution metrics (was always nil).
			Metrics: map[string]interface{}{
				"duration_ms":     e.now().Sub(start).Milliseconds(),
				"executor_status": execOutcome.Status,
			},
		}
		if execOutcome.AgentID != "" {
			outcomeResult.Metrics["agent_id"] = execOutcome.AgentID
		}
		if execOutcome.Provider != "" {
			outcomeResult.Metrics["provider"] = execOutcome.Provider
		}
		if execOutcome.Model != "" {
			outcomeResult.Metrics["model"] = execOutcome.Model
		}
		if execOutcome.RoutingReason != "" {
			outcomeResult.Metrics["routing_reason"] = execOutcome.RoutingReason
		}
		if execOutcome.Fallback {
			outcomeResult.Metrics["fallback"] = true
		}
		if execOutcome.ToolsExecuted > 0 {
			outcomeResult.Metrics["tools_executed"] = execOutcome.ToolsExecuted
		}
		if execOutcome.ChildExecutions > 0 {
			outcomeResult.Metrics["child_executions"] = execOutcome.ChildExecutions
		}
		if execOutcome.Retries > 0 {
			outcomeResult.Metrics["retries"] = execOutcome.Retries
		}
	} else if execOutcome != nil {
		// Fail closed: any other executor outcome (failed, unknown — e.g. an
		// E-008 provider failure or "no agent available" — or a handler-
		// passthrough status) is NOT a success. WaitOutcome returns terminal
		// outcomes with a nil error, so without this branch those outcomes
		// fell through to "completed" with a nil error (D1's no-false-success
		// rule applied to every status, not just the governance ones).
		// Envelope mirrors the generic execution-failure wrap above.
		status = "failed"
		outcomeResult = &Outcome{
			Metrics: map[string]interface{}{
				"duration_ms":     e.now().Sub(start).Milliseconds(),
				"executor_status": execOutcome.Status,
			},
		}
		if execOutcome.AgentID != "" {
			outcomeResult.Metrics["agent_id"] = execOutcome.AgentID
		}
		message := execOutcome.Error
		if message == "" {
			message = fmt.Sprintf("executor did not complete the task (status=%s)", execOutcome.Status)
		}
		respErr = &ChainError{
			Code:      "EXECUTION_FAILED",
			Category:  "INTERNAL_FAILURE",
			Message:   message,
			ChainStep: string(StepAgent),
			Retryable: false,
		}
		if req.Context != nil {
			respErr.CorrelationID = req.Context.CorrelationID
		}
		respErr.Timestamp = e.now()
	}

	// Step 13: Store outcome in memory
	e.chainMemoryWrite(ctx, req, status, outcomeResult)
	audit = append(audit, AuditEntry{
		Step:      string(StepMemoryWrite),
		Action:    "store outcome",
		Actor:     "memory",
		Timestamp: e.now(),
		Duration:  e.now().Sub(start),
		Outcome:   fmt.Sprintf("status=%s", status),
	})
	e.chainEmit(req, "chain.memory.written", "memory", status)

	// C-010 fix: terminal event reflects actual status — failures emit
	// chain.failed, cancellations emit chain.cancelled (E-005), only successes
	// emit chain.completed.
	switch status {
	case "completed":
		e.chainEmit(req, "chain.completed", "core", status)
	case "cancelled":
		e.chainEmit(req, "chain.cancelled", "core", fmt.Sprintf("step=%s status=%s", StepAgent, status))
	default:
		e.chainEmit(req, "chain.failed", "core", fmt.Sprintf("step=%s status=%s", StepAgent, status))
	}

	return &Response{
		RequestID:   req.ID,
		BusinessID:  req.Context.BusinessID,
		DivisionID:  req.Context.DivisionID,
		Status:      status,
		Constraints: governanceConstraints(govDecision),
		Outcome:     outcomeResult,
		Error:       respErr,
		AuditTrail:  audit,
		Duration:    e.now().Sub(start),
	}
}

// chainValidate validates the incoming request.
func (e *Engine) chainValidate(_ context.Context, req *Request) error {
	if req.ID == "" {
		return &ChainError{Code: "VALIDATION", Category: "VALIDATION", Message: "request ID required", ChainStep: string(StepValidate)}
	}
	if req.Context == nil {
		return &ChainError{Code: "VALIDATION", Category: "VALIDATION", Message: "request context required", ChainStep: string(StepValidate)}
	}
	if req.Context.BusinessID == "" {
		return &ChainError{Code: "VALIDATION", Category: "VALIDATION", Message: "business_id required", ChainStep: string(StepValidate)}
	}
	if req.Context.ActorID == "" {
		return &ChainError{Code: "VALIDATION", Category: "VALIDATION", Message: "actor_id required", ChainStep: string(StepValidate)}
	}
	if req.Intent == "" {
		return &ChainError{Code: "VALIDATION", Category: "VALIDATION", Message: "intent required", ChainStep: string(StepValidate)}
	}
	return nil
}

// identityEnforced reports whether identity-bound admission is active
// (A6 posture: either security flag enables it).
func (e *Engine) identityEnforced() bool {
	return e.identityRequireAuth || e.identityEnforceScope
}

// chainIdentity implements admission stage 3 (RUNTIME §3.1): actor identity
// verification. Failure is category AUTH ("actor not found" / "identity
// store unavailable"), Retryable false.
//
// Credentials are never verified here — no token reaches core; credential
// verification is the gateway's A6 boundary. The chain re-checks the
// already-bound actor against foundation/identity as defense in depth:
// "known identity" means the actor holds a membership record in the store
// (the authoritative record A6 binds).
//
// When enforcement is off the stage passes with outcome not_enforced
// (process-boundary trust — the A6 enforcement-off posture). When either
// flag is on, a missing store or unknown actor fails closed (RT-02).
func (e *Engine) chainIdentity(_ context.Context, req *Request) (string, error) {
	if !e.identityEnforced() {
		return "not_enforced", nil
	}
	if e.identityMemberships == nil {
		return "", &ChainError{
			Code:      "AUTH",
			Category:  "AUTH",
			Message:   "identity store unavailable (fail closed)",
			ChainStep: string(StepIdentity),
			Retryable: false,
		}
	}
	actor := req.Context.ActorID
	if len(e.identityMemberships.For(actor)) == 0 {
		return "", &ChainError{
			Code:      "AUTH",
			Category:  "AUTH",
			Message:   fmt.Sprintf("actor not found: %s", actor),
			ChainStep: string(StepIdentity),
			Retryable: false,
		}
	}
	return "verified actor=" + actor, nil
}

// chainAuthorization implements admission stage 4 (RUNTIME §3.1): authority
// resolution at business scope. Failure is category AUTHORIZATION, Retryable
// false. The check is scope authority via foundation/identity membership
// (INV-01 / A6 semantics: active membership in the request's business,
// division-aware); full action authority remains with Governance stage 5 —
// MEMBERSHIP != AUTHORITY (membership.go).
//
// Runs only when enforce_scope is on (scope enforcement is what this stage
// resolves); otherwise outcome not_enforced. A missing store while enforced
// fails closed (RT-02).
func (e *Engine) chainAuthorization(_ context.Context, req *Request) (string, error) {
	if !e.identityEnforceScope {
		return "not_enforced", nil
	}
	if e.identityMemberships == nil {
		return "", &ChainError{
			Code:      "AUTHORIZATION",
			Category:  "AUTHORIZATION",
			Message:   "membership store unavailable (fail closed)",
			ChainStep: string(StepAuthorization),
			Retryable: false,
		}
	}
	actor, business := req.Context.ActorID, req.Context.BusinessID
	if !e.identityMemberships.AllowsScope(actor, business, req.Context.DivisionID) {
		return "", &ChainError{
			Code:      "AUTHORIZATION",
			Category:  "AUTHORIZATION",
			Message:   fmt.Sprintf("actor %s is not authorized for scope business=%s division=%s", actor, business, req.Context.DivisionID),
			ChainStep: string(StepAuthorization),
			Retryable: false,
		}
	}
	return "authorized business=" + business, nil
}

// chainResourceCheck implements admission stage 7 (RUNTIME §3.1): resource
// availability before SCHEDULE. The tracked dispatch resource is executor
// worker capacity (MaxConcurrent). Failure is category RESOURCE_UNAVAILABLE
// with Retryable true (RUNTIME admission rejection table: ADM_RESOURCES,
// retry after queueing). This is a pre-check only — admission never
// guarantees capacity at execution time (RUNTIME §3.2); a race still
// surfaces at executor Submit.
func (e *Engine) chainResourceCheck(_ context.Context, _ *Request) (string, error) {
	active, max := e.taskExec.Capacity()
	if max > 0 && active >= max {
		return "", &ChainError{
			Code:      "RESOURCE_UNAVAILABLE",
			Category:  "RESOURCE_UNAVAILABLE",
			Message:   fmt.Sprintf("executor at capacity (%d/%d)", active, max),
			ChainStep: string(StepResourceCheck),
			Retryable: true,
		}
	}
	return fmt.Sprintf("available active=%d/%d", active, max), nil
}

// chainGovernance checks governance policies. It returns the evaluated
// decision alongside the error so the caller can surface an
// ALLOW_WITH_CONSTRAINTS decision's constraints (N2).
func (e *Engine) chainGovernance(_ context.Context, req *Request) (governance.Decision, error) {
	govReq := governance.Request{
		Actor:      req.Context.ActorID,
		Action:     "execute_request",
		Resource:   "core",
		BusinessID: req.Context.BusinessID,
		// N1: scope ids known at this gate — division and objective live on
		// the request context. Narrower ids (agent/workflow/task) do not
		// exist yet (this step runs before objective/workflow creation);
		// they are wired at the executor gate instead.
		DivisionID:  req.Context.DivisionID,
		ObjectiveID: req.Context.ObjectiveID,
		// Approval resume: an approved record for this request satisfies
		// a REQUIRE_APPROVAL policy (nil otherwise — no behavior change).
		ApprovalState: e.approvalStateFor(req.ID),
	}
	decision := e.govEngine.Evaluate(govReq)
	if decision.IsAllowing() {
		return decision, nil
	}
	// CORE_INTERFACE_CONTRACTS §3: POLICY_DENIED covers governance outcome
	// DENY only; a governance REQUIRE_APPROVAL surfaces as category
	// APPROVAL_REQUIRED ("Wait for approval") — a distinct machine-readable
	// code/category so callers can tell approval-gated work from a denial.
	// D4: Retryable is false for both — the contract category tables say
	// No ("wait for approval"); retrying now cannot succeed for DENY and
	// cannot succeed before approval for REQUIRE_APPROVAL.
	code, category := "POLICY_DENIED", "POLICY_DENIED"
	message := fmt.Sprintf("governance denied (outcome=%s): %s", decision.Outcome, decision.Reason)
	var details map[string]string
	if decision.Outcome == governance.REQUIRE_APPROVAL {
		code, category = "APPROVAL_REQUIRED", "APPROVAL_REQUIRED"
		message = fmt.Sprintf("governance requires approval (outcome=%s): %s", decision.Outcome, decision.Reason)
		// P1 wiring: open the approval loop — record + approval.requested
		// event; the client learns approval_id via error.details.
		if ar, err := e.createApproval(req, decision, govReq); err == nil {
			details = map[string]string{"approval_id": ar.DecisionID}
		}
	} else if decision.Outcome == governance.ESCALATE {
		// D3: code ESALATION_REQUIRED distinguishes an escalation from a
		// pure DENY (D1 uses code POLICY_DENIED); category stays
		// POLICY_DENIED because CORE §3 has no escalation category and
		// the action was blocked by governance (documented stretch of the
		// §3 "(outcome DENY)" parenthetical — the free-form code is the
		// machine-readable distinction). Retryable=false: an escalation
		// needs a higher authority, not a retry.
		code, category = "ESCALATION_REQUIRED", "POLICY_DENIED"
		message = fmt.Sprintf("governance escalation required (outcome=%s): %s", decision.Outcome, decision.Reason)
		escRef := fmt.Sprintf("esc-%s-%d", req.Context.ActorID, e.now().UnixNano())
		details = map[string]string{"escalation_ref": escRef}
		e.emitEscalation(req, escRef, decision.Reason, "chain")
	}
	return decision, &ChainError{
		Code:      code,
		Category:  category,
		Message:   message,
		Details:   details,
		ChainStep: string(StepGovernance),
		Retryable: false,
	}
}

// governanceConstraints renders an ALLOW_WITH_CONSTRAINTS decision's
// constraints as the contract's list[string] (CORE_INTERFACE_CONTRACTS §4.2,
// SCHEMA_GOVERNANCE decision record). Rendered `type:expression` — the shape
// request constraints already use ("budget:1000", docs/http-gateway.md).
// Every other outcome contributes nothing.
func governanceConstraints(decision governance.Decision) []string {
	if decision.Outcome != governance.ALLOW_WITH_CONSTRAINTS || len(decision.Constraints) == 0 {
		return nil
	}
	rendered := make([]string, 0, len(decision.Constraints))
	for _, c := range decision.Constraints {
		switch {
		case c.ConstraintType != "" && c.Expression != "":
			rendered = append(rendered, c.ConstraintType+":"+c.Expression)
		case c.Expression != "":
			rendered = append(rendered, c.Expression)
		case c.ConstraintType != "":
			rendered = append(rendered, c.ConstraintType)
		default:
			rendered = append(rendered, c.ConstraintID)
		}
	}
	return rendered
}

// chainObjective creates an objective from the request intent.
func (e *Engine) chainObjective(_ context.Context, req *Request) (*cognition.Objective, error) {
	obj, err := e.objectiveEng.CreateObjective(
		cognition.ObjectiveTypeOwner,
		req.Intent,
		// Description explains WHY this objective exists (context for humans).
		fmt.Sprintf("Owner %s requested: %s", req.Context.ActorID, req.Intent),
		// Success criteria states the verifiable completion condition —
		// intentionally distinct from the description (C-002).
		fmt.Sprintf("Request %s processed with status=completed and audit trail recorded", req.ID),
		req.Context.ActorID,
		req.Context.BusinessID,
	)
	if err != nil {
		return nil, err
	}
	return obj, nil
}

// chainDecision makes a decision on how to fulfill the objective.
func (e *Engine) chainDecision(_ context.Context, req *Request, obj *cognition.Objective) (*cognition.Decision, error) {
	decision, err := e.decisionEng.FrameDecision(
		req.Intent,
		[]string{obj.ID},
		req.Context.ActorID,
		req.Context.BusinessID,
	)
	if err != nil {
		return nil, err
	}
	return decision, nil
}

// chainPlan creates a plan from the decision.
func (e *Engine) chainPlan(_ context.Context, req *Request, obj *cognition.Objective, dec *cognition.Decision) (*cognition.Plan, error) {
	plan, err := e.planner.CreatePlan(
		[]string{obj.ID},
		[]string{dec.ID},
		req.Intent,
		req.Context.ActorID,
	)
	if err != nil {
		return nil, err
	}
	return plan, nil
}

// chainWorkflow creates a workflow from the plan.
func (e *Engine) chainWorkflow(_ context.Context, req *Request, objective *cognition.Objective, plan *cognition.Plan) (*workflow.Workflow, error) {
	wf, err := e.workflowEng.CreateWorkflow(
		plan.ID,
		objective.ID,
		req.Context.BusinessID,
		req.Intent,
		"auto-generated from plan",
		req.Intent,
		req.Context.ActorID,
	)
	if err != nil {
		return nil, err
	}
	return wf, nil
}

// chainSchedule schedules the workflow for execution.
func (e *Engine) chainSchedule(_ context.Context, req *Request, wf *workflow.Workflow) (*scheduler.Job, error) {
	job, err := e.scheduler.SubmitJob(
		wf.ID,
		wf.ID,
		req.Context.BusinessID,
		scheduler.PriorityNormal,
		nil,
	)
	if err != nil {
		return nil, err
	}
	return job, nil
}

// chainExecute submits the work to the task executor and waits for completion.
func (e *Engine) chainExecute(ctx context.Context, req *Request, wf *workflow.Workflow) (*executor.Outcome, error) {
	workReq := &executor.WorkRequest{
		Handler:       req.Handler,
		TaskID:        wf.ID,
		CorrelationID: req.Context.CorrelationID,
		BusinessID:    req.Context.BusinessID,
		// N1: wire the division sub-scope and the workflow id so
		// division/workflow-pinned governance policies match at the
		// executor gate (an unset id never matches a pinned policy id).
		DivisionID:  req.Context.DivisionID,
		WorkflowID:  wf.ID,
		ActorID:     req.Context.ActorID,
		Intent:      req.Intent,
		Priority:    req.Priority,
		Constraints: req.Constraints,
		Input:       req.Input,
		// Approval resume: carries an approved approval into the executor
		// gate so its REQUIRE_APPROVAL policy is satisfied on the re-run.
		ApprovalState: e.approvalStateFor(req.ID),
	}

	// C-026 fix: honor caller-owned Deadline when set, else default 60s.
	timeout := 60 * time.Second
	if req.Deadline != nil && !req.Deadline.IsZero() {
		if d := time.Until(*req.Deadline); d > 0 {
			timeout = d
		} else {
			// Deadline already passed — fail fast without executing.
			return nil, fmt.Errorf("deadline exceeded: %s", req.Deadline.Format(time.RFC3339))
		}
	}

	// E-005 submit-time check: cancellation landed after dequeue but before
	// the executor submit — never submit.
	e.inflightMu.RLock()
	inf := e.inflight[req.ID]
	cancelledEarly := inf != nil && inf.cancelRequested
	e.inflightMu.RUnlock()
	if cancelledEarly {
		return nil, errCancelledBeforeSubmit
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Submit outside inflightMu (leaf lock — no nesting in either direction).
	if err := e.taskExec.Submit(workReq); err != nil {
		// RUNTIME §3.2: admission never guarantees capacity at execution
		// time — a capacity race that surfaces at Submit is the same
		// RESOURCE_UNAVAILABLE/retryable envelope as the stage-7 pre-check
		// (CORE §3 table), not a generic internal failure (C-024).
		if errors.Is(err, executor.ErrAtCapacity) {
			return nil, &ChainError{
				Code:      "RESOURCE_UNAVAILABLE",
				Category:  "RESOURCE_UNAVAILABLE",
				Message:   err.Error(),
				ChainStep: string(StepAgent),
				Retryable: true,
			}
		}
		return nil, err
	}

	// Register the task and re-check the cancel flag under one critical
	// section: a cancel that arrived while Submit was in flight either already
	// flagged the request (caught here) or saw state=executing with this taskID
	// and called the executor itself — both paths are idempotent.
	e.inflightMu.Lock()
	inf = e.inflight[req.ID]
	applyCancel := false
	var reason, actor string
	if inf != nil {
		inf.taskID = wf.ID
		inf.state = inflightExecuting
		applyCancel = inf.cancelRequested
		reason, actor = inf.cancelReason, inf.cancelActor
	}
	e.inflightMu.Unlock()
	if applyCancel {
		// Best effort: WaitOutcome below remains authoritative for the outcome.
		_ = e.taskExec.CancelTask(wf.ID, reason, actor)
	}

	return e.taskExec.WaitOutcome(execCtx, workReq.TaskID)
}

// chainCancelled builds the terminal cancelled response for a request that was
// cancelled before (or instead of) executing (E-005). Cancellation is not a
// failure: no circuit-breaker failure, no recovery record, and the terminal
// event is chain.cancelled.
func (e *Engine) chainCancelled(ctx context.Context, req *Request, audit []AuditEntry, start time.Time, message string) *Response {
	audit = append(audit, AuditEntry{
		Step:      string(StepAgent),
		Action:    "cancel request",
		Actor:     "core",
		Timestamp: e.now(),
		Duration:  e.now().Sub(start),
		Outcome:   "cancelled",
	})

	// Terminal outcome recorded like any other terminal status.
	e.chainMemoryWrite(ctx, req, "cancelled", nil)
	audit = append(audit, AuditEntry{
		Step:      string(StepMemoryWrite),
		Action:    "store outcome",
		Actor:     "memory",
		Timestamp: e.now(),
		Duration:  e.now().Sub(start),
		Outcome:   "status=cancelled",
	})
	e.chainEmit(req, "chain.memory.written", "memory", "cancelled")
	e.chainEmit(req, "chain.cancelled", "core", fmt.Sprintf("step=%s status=cancelled", StepAgent))

	chainErr := &ChainError{
		Code:      "CANCELLED",
		Category:  "CANCELLATION",
		Message:   message,
		ChainStep: string(StepAgent),
		Retryable: false,
	}
	if req.Context != nil {
		chainErr.CorrelationID = req.Context.CorrelationID
	}
	chainErr.Timestamp = e.now()

	return &Response{
		RequestID:  req.ID,
		BusinessID: req.Context.BusinessID,
		DivisionID: req.Context.DivisionID,
		Status:     "cancelled",
		Error:      chainErr,
		AuditTrail: audit,
		Duration:   e.now().Sub(start),
	}
}

// chainError creates an error response with audit trail.
func (e *Engine) chainError(req *Request, err error, step ChainStep, audit []AuditEntry, start time.Time) *Response {
	chainErr, ok := err.(*ChainError)
	if !ok {
		chainErr = &ChainError{
			Code:      "INTERNAL_FAILURE",
			Category:  "INTERNAL_FAILURE",
			Message:   err.Error(),
			ChainStep: string(step),
		}
	}
	// Ensure correlation_id and timestamp are always set on the error.
	if req.Context != nil {
		chainErr.CorrelationID = req.Context.CorrelationID
	}
	if chainErr.Timestamp.IsZero() {
		chainErr.Timestamp = e.now()
	}

	audit = append(audit, AuditEntry{
		Step:      string(step),
		Action:    "error",
		Actor:     "core",
		Timestamp: e.now(),
		Duration:  e.now().Sub(start),
		Outcome:   fmt.Sprintf("error=%s", err.Error()),
	})

	// C-009 fix: chain failures are observable on the event bus (was silent).
	e.chainEmit(req, "chain.failed", "core", fmt.Sprintf("step=%s error=%s", step, chainErr.Code))

	return &Response{
		RequestID:  req.ID,
		BusinessID: req.Context.BusinessID,
		DivisionID: req.Context.DivisionID,
		Status:     "failed",
		Error:      chainErr,
		AuditTrail: audit,
		Duration:   e.now().Sub(start),
	}
}

// chainMemoryRead retrieves relevant context from memory before objective creation.
func (e *Engine) chainMemoryRead(_ context.Context, req *Request) []*memory.MemoryEntry {
	query := &memory.MemoryQuery{
		BusinessID:    req.Context.BusinessID,
		ObjectiveID:   req.Context.ObjectiveID, // C-012: forward the caller's objective scope; empty = cross-objective retrieval (SCHEMA_MEMORY §1: retrieval considers objective)
		Keywords:      req.Intent,
		MaxResults:    5,
		MinConfidence: 0.3,
	}
	return e.memoryStore.Retrieve(query)
}

// chainMemoryWrite stores the outcome as a memory entry after execution.
func (e *Engine) chainMemoryWrite(_ context.Context, req *Request, status string, outcome *Outcome) {
	content := fmt.Sprintf("Request '%s' completed with status: %s", req.Intent, status)
	if outcome != nil && outcome.Summary != "" {
		content = outcome.Summary
	}

	if memErr := e.memoryStore.Admit(&memory.MemoryEntry{
		Type:        memory.MemoryTypeEpisodic,
		BusinessID:  req.Context.BusinessID,
		ObjectiveID: req.Context.ObjectiveID,
		Content:     content,
		Summary:     fmt.Sprintf("outcome: %s", status),
		Provenance: memory.Provenance{
			Source:     "chain",
			SourceID:   req.ID,
			Confidence: 1.0,
		},
		Tags: []string{"outcome", status},
	}); memErr != nil {
		e.chainEmit(req, "chain.error", "memory", fmt.Sprintf("admit failed: %v", memErr))
	}
}

// chainAttentionScore submits the request to the attention engine for scoring.
// Returns the scored item (nil on failure) and any error for audit recording.
func (e *Engine) chainAttentionScore(_ context.Context, req *Request) (*attention.AttentionItem, error) {
	urgency := req.Priority
	if urgency > 10 {
		urgency = 10
	}

	item, attErr := e.attentionEng.SubmitItem(
		req.Intent,
		fmt.Sprintf("Owner request from %s", req.Context.ActorID),
		req.Context.BusinessID,
		"owner",
		urgency,
		5, // importance
		2, // risk
		0.8,
	)
	if attErr != nil {
		e.chainEmit(req, "chain.error", "attention", fmt.Sprintf("submit failed: %v", attErr))
		return nil, attErr
	}
	return item, nil
}

// attItemScore safely extracts the score from an attention item.
func attItemScore(item *attention.AttentionItem) float64 {
	if item == nil {
		return 0
	}
	return item.Score
}

// chainEmit publishes an event to the bus for chain step observation.
// Failures are silently ignored - event emission must not block the chain.
func (e *Engine) chainEmit(req *Request, eventType, actor, outcome string) {
	payload := fmt.Sprintf(`{"step":"%s","actor":"%s","outcome":"%s"}`, eventType, actor, outcome)
	_ = e.eventBus.Publish(&event.Event{
		ID:            fmt.Sprintf("%s-%s", req.ID, eventType),
		Type:          event.EventType(eventType),
		Source:        "chain",
		Timestamp:     e.now(),
		BusinessID:    req.Context.BusinessID,
		CorrelationID: req.ID,
		Priority:      event.PriorityNormal,
		Data:          []byte(payload),
	})
}
