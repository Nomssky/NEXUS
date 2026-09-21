package cognition

import (
	"fmt"
	"sync"
	"time"
)

// ExecutiveStatus tracks the lifecycle of an executive request.
type ExecutiveStatus string

const (
	ExecutiveStatusReceived    ExecutiveStatus = "received"
	ExecutiveStatusClassifying ExecutiveStatus = "classifying"
	ExecutiveStatusObjective   ExecutiveStatus = "objective_created"
	ExecutiveStatusDecision    ExecutiveStatus = "decision_framed"
	ExecutiveStatusPlanning    ExecutiveStatus = "planning"
	ExecutiveStatusReady       ExecutiveStatus = "ready_for_workflow"
	ExecutiveStatusFailed      ExecutiveStatus = "failed"
)

// Request represents an owner intent received by the Executive.
// The Executive classifies intent, creates objectives, frames decisions,
// and hands off to the Planner. It does NOT execute actions.
type Request struct {
	ID          string            `json:"id"`
	Status      ExecutiveStatus   `json:"status"`
	Owner       string            `json:"owner"`
	Intent      string            `json:"intent"`
	BusinessID  string            `json:"business_id,omitempty"`
	Context     map[string]string `json:"context,omitempty"`
	ObjectiveID string            `json:"objective_id,omitempty"`
	DecisionID  string            `json:"decision_id,omitempty"`
	PlanID      string            `json:"plan_id,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// Executive coordinates the cognitive flow:
//
//	Owner Intent → Objective → Decision → Plan → Workflow Handoff
//
// Executive is forbidden from executing actions, owning model/provider
// selection, or owning workflow durability. Models have no execution authority.
type Executive struct {
	mu              sync.RWMutex
	objectiveEngine *ObjectiveEngine
	decisionEngine  *DecisionEngine
	planner         *Planner
	requests        map[string]*Request
	now             func() time.Time
}

// NewExecutive creates a new executive coordinator.
func NewExecutive(oe *ObjectiveEngine, de *DecisionEngine, pl *Planner) *Executive {
	return &Executive{
		objectiveEngine: oe,
		decisionEngine:  de,
		planner:         pl,
		requests:        make(map[string]*Request),
		now:             time.Now,
	}
}

// NewExecutiveWithClock creates a new executive with an injectable clock.
func NewExecutiveWithClock(oe *ObjectiveEngine, de *DecisionEngine, pl *Planner, now func() time.Time) *Executive {
	return &Executive{
		objectiveEngine: oe,
		decisionEngine:  de,
		planner:         pl,
		requests:        make(map[string]*Request),
		now:             now,
	}
}

// ReceiveIntent processes an owner intent and creates the cognitive chain.
// Flow: Intent → Objective → Decision → Plan → Ready for Workflow.
//
// This is the main entry point for the cognitive pipeline.
// It does NOT execute anything — it only prepares for workflow execution.
func (ex *Executive) ReceiveIntent(
	owner, intent, title, purpose, desiredOutcome, businessID string,
) (*Request, error) {
	if owner == "" {
		return nil, fmt.Errorf("owner is required")
	}
	if purpose == "" {
		return nil, fmt.Errorf("purpose (WHY) is required")
	}

	ex.mu.Lock()
	defer ex.mu.Unlock()

	now := ex.now()
	req := &Request{
		ID:         fmt.Sprintf("req-%d", now.UnixNano()),
		Status:     ExecutiveStatusReceived,
		Owner:      owner,
		Intent:     intent,
		BusinessID: businessID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	ex.requests[req.ID] = req

	// Step 1: Create objective from intent
	obj, err := ex.objectiveEngine.CreateObjective(
		ObjectiveTypeMission,
		title,
		purpose,
		desiredOutcome,
		owner,
		businessID,
	)
	if err != nil {
		req.Status = ExecutiveStatusFailed
		return req, fmt.Errorf("failed to create objective: %w", err)
	}

	if err := ex.objectiveEngine.Activate(obj.ID); err != nil {
		req.Status = ExecutiveStatusFailed
		return req, fmt.Errorf("failed to activate objective: %w", err)
	}

	req.ObjectiveID = obj.ID
	req.Status = ExecutiveStatusObjective
	req.UpdatedAt = ex.now()

	// Step 2: Frame decision from objective
	dec, err := ex.decisionEngine.FrameDecision(
		fmt.Sprintf("How to achieve: %s", title),
		[]string{obj.ID},
		owner,
		businessID,
	)
	if err != nil {
		req.Status = ExecutiveStatusFailed
		return req, fmt.Errorf("failed to frame decision: %w", err)
	}

	req.DecisionID = dec.ID
	req.Status = ExecutiveStatusDecision
	req.UpdatedAt = ex.now()

	// Step 3: Create plan from objective and decision
	plan, err := ex.planner.CreatePlan(
		[]string{obj.ID},
		[]string{dec.ID},
		businessID,
		owner,
	)
	if err != nil {
		req.Status = ExecutiveStatusFailed
		return req, fmt.Errorf("failed to create plan: %w", err)
	}

	// Add mission with WHY preserved
	_, err = ex.planner.AddMission(
		plan.ID,
		obj.ID,
		title,
		desiredOutcome,
		purpose, // WHY preserved
	)
	if err != nil {
		req.Status = ExecutiveStatusFailed
		return req, fmt.Errorf("failed to add mission: %w", err)
	}

	req.PlanID = plan.ID
	req.Status = ExecutiveStatusReady
	req.UpdatedAt = ex.now()

	return req, nil
}

// GetRequest returns a request by ID.
func (ex *Executive) GetRequest(requestID string) (*Request, bool) {
	ex.mu.RLock()
	defer ex.mu.RUnlock()
	req, ok := ex.requests[requestID]
	return req, ok
}

// RequestCount returns the total number of requests.
func (ex *Executive) RequestCount() int {
	ex.mu.RLock()
	defer ex.mu.RUnlock()
	return len(ex.requests)
}
