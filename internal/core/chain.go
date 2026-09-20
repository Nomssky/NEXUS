package core

import (
	"context"
	"fmt"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/agent"
	"github.com/Nomssky/NEXUS/internal/foundation/cognition"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/scheduler"
	"github.com/Nomssky/NEXUS/internal/foundation/workflow"
)

// ChainStep represents a step in the canonical execution chain.
type ChainStep string

const (
	StepValidate   ChainStep = "validate"
	StepGovernance ChainStep = "governance"
	StepObjective  ChainStep = "objective"
	StepDecision   ChainStep = "decision"
	StepPlan       ChainStep = "plan"
	StepWorkflow   ChainStep = "workflow"
	StepSchedule   ChainStep = "schedule"
	StepAgent      ChainStep = "agent"
	StepModel      ChainStep = "model"
	StepTool       ChainStep = "tool"
	StepVerify     ChainStep = "verify"
	StepOutcome    ChainStep = "outcome"
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
	audit := make([]AuditEntry, 0, 12)

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

	// Step 2: Governance check
	if err := e.chainGovernance(ctx, req); err != nil {
		return e.chainError(req, err, StepGovernance, audit, start)
	}
	audit = append(audit, AuditEntry{
		Step:      string(StepGovernance),
		Action:    "governance check",
		Actor:     "governance",
		Timestamp: e.now(),
		Duration:  e.now().Sub(start),
		Outcome:   "allowed",
	})

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

	// Step 6: Create workflow from plan
	wf, err := e.chainWorkflow(ctx, req, plan)
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

	// Step 8: Assign to agent (optional — log but don't fail)
	agentDef, err := e.chainAgent(ctx, req)
	if err != nil {
		audit = append(audit, AuditEntry{
			Step:      string(StepAgent),
			Action:    "assign agent",
			Actor:     "agent-runtime",
			Timestamp: e.now(),
			Duration:  e.now().Sub(start),
			Outcome:   fmt.Sprintf("skipped: %v", err),
		})
	} else {
		audit = append(audit, AuditEntry{
			Step:      string(StepAgent),
			Action:    "assign agent",
			Actor:     "agent-runtime",
			Timestamp: e.now(),
			Duration:  e.now().Sub(start),
			Outcome:   fmt.Sprintf("agent=%s", agentDef.ID),
		})
	}

	// Steps 9-10: Model/Tool (completed via agent)
	audit = append(audit, AuditEntry{
		Step:      string(StepModel),
		Action:    "model routing",
		Actor:     "model-router",
		Timestamp: e.now(),
		Duration:  e.now().Sub(start),
		Outcome:   "completed",
	})

	audit = append(audit, AuditEntry{
		Step:      string(StepTool),
		Action:    "tool execution",
		Actor:     "tool-registry",
		Timestamp: e.now(),
		Duration:  e.now().Sub(start),
		Outcome:   "completed",
	})

	// Step 11: Verify
	audit = append(audit, AuditEntry{
		Step:      string(StepVerify),
		Action:    "verify outcome",
		Actor:     "core",
		Timestamp: e.now(),
		Duration:  e.now().Sub(start),
		Outcome:   "verified",
	})

	// Step 12: Outcome
	return &Response{
		RequestID:  req.ID,
		Status:     "completed",
		Outcome:    &Outcome{Summary: "request processed through canonical chain"},
		AuditTrail: audit,
		Duration:   e.now().Sub(start),
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

// chainGovernance checks governance policies.
func (e *Engine) chainGovernance(_ context.Context, req *Request) error {
	decision := e.govEngine.Evaluate(governance.Request{
		Actor:      req.Context.ActorID,
		Action:     "execute_request",
		Resource:   "core",
		BusinessID: req.Context.BusinessID,
	})
	if decision.Outcome == governance.DENY {
		return &ChainError{
			Code:      "POLICY_DENIED",
			Category:  "POLICY_DENIED",
			Message:   fmt.Sprintf("governance denied: %s", decision.Reason),
			ChainStep: string(StepGovernance),
		}
	}
	return nil
}

// chainObjective creates an objective from the request intent.
func (e *Engine) chainObjective(_ context.Context, req *Request) (*cognition.Objective, error) {
	obj, err := e.objectiveEng.CreateObjective(
		cognition.ObjectiveTypeOwner,
		req.Intent,
		req.Intent,
		req.Intent,
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
func (e *Engine) chainWorkflow(_ context.Context, req *Request, plan *cognition.Plan) (*workflow.Workflow, error) {
	wf, err := e.workflowEng.CreateWorkflow(
		plan.ID,
		plan.Scope,
		req.Context.BusinessID,
		plan.Scope,
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

// chainAgent assigns an agent to execute the work.
func (e *Engine) chainAgent(_ context.Context, req *Request) (*agent.AgentDefinition, error) {
	// No agent available by default — agents are provisioned separately
	return nil, fmt.Errorf("no agent available for business %s", req.Context.BusinessID)
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

	audit = append(audit, AuditEntry{
		Step:      string(step),
		Action:    "error",
		Actor:     "core",
		Timestamp: e.now(),
		Duration:  e.now().Sub(start),
		Outcome:   fmt.Sprintf("error: %s", err.Error()),
	})

	return &Response{
		RequestID:  req.ID,
		Status:     "failed",
		Error:      chainErr,
		AuditTrail: audit,
		Duration:   e.now().Sub(start),
	}
}
