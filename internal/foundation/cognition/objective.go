// Package cognition implements the NEXUS core cognitive control (C08).
//
// C08 owns executive control, objective lifecycle (+WHY, success criteria,
// scope), decision framing, and planning (plan production + validation handoff).
//
// C08 depends on C01–C07. C08 is forbidden from executing actions, owning
// model/provider selection, or owning workflow durability.
//
// The locked conceptual flow is:
//
//	Executive → Objective Engine → Decision Engine → Planner → Workflow
//
// Models have no execution authority. C08 neither calls external tools nor
// selects models.
package cognition

import (
	"fmt"
	"sync"
	"time"
)

// ObjectiveType classifies the level of an objective in the hierarchy.
type ObjectiveType string

const (
	ObjectiveTypeOwner    ObjectiveType = "owner"
	ObjectiveTypeBusiness ObjectiveType = "business"
	ObjectiveTypeDivision ObjectiveType = "division"
	ObjectiveTypeMission  ObjectiveType = "mission"
	ObjectiveTypeTask     ObjectiveType = "task"
)

// ObjectiveStatus tracks the lifecycle of an objective.
type ObjectiveStatus string

const (
	ObjectiveStatusDraft     ObjectiveStatus = "draft"
	ObjectiveStatusActive    ObjectiveStatus = "active"
	ObjectiveStatusBlocked   ObjectiveStatus = "blocked"
	ObjectiveStatusAtRisk    ObjectiveStatus = "at_risk"
	ObjectiveStatusCompleted ObjectiveStatus = "completed"
	ObjectiveStatusFailed    ObjectiveStatus = "failed"
	ObjectiveStatusCancelled ObjectiveStatus = "cancelled"
)

// Objective represents a durable entity capturing what NEXUS is trying to
// achieve, why it exists, and how to measure success.
//
// Objectives preserve owner intent and WHY through every decomposition layer.
// A completed task is NOT equivalent to an achieved objective.
type Objective struct {
	// ID is the unique objective identifier.
	ID string `json:"id"`
	// Type classifies the level in the hierarchy.
	Type ObjectiveType `json:"type"`
	// Status tracks lifecycle state.
	Status ObjectiveStatus `json:"status"`
	// BusinessID scopes the objective to a business.
	BusinessID string `json:"business_id,omitempty"`
	// DivisionID scopes the objective to a division.
	DivisionID string `json:"division_id,omitempty"`
	// ParentObjectiveID links to the parent objective (lineage).
	ParentObjectiveID string `json:"parent_objective_id,omitempty"`
	// Title is a human-readable description.
	Title string `json:"title"`
	// Purpose is the WHY — why this objective exists.
	Purpose string `json:"purpose"`
	// DesiredOutcome describes what success looks like.
	DesiredOutcome string `json:"desired_outcome"`
	// Constraints are boundaries the objective must respect.
	Constraints []string `json:"constraints,omitempty"`
	// SuccessCriteria are measurable conditions for completion.
	SuccessCriteria []string `json:"success_criteria,omitempty"`
	// Metrics are quantitative measures of progress.
	Metrics map[string]float64 `json:"metrics,omitempty"`
	// Priority determines execution order (higher = more important).
	Priority int `json:"priority"`
	// Owner is who requested this objective.
	Owner string `json:"owner"`
	// Version is the optimistic concurrency version.
	Version int `json:"version"`
	// CreatedAt is when the objective was created.
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is when the objective was last updated.
	UpdatedAt time.Time `json:"updated_at"`
	// CompletedAt is when the objective was completed (nil if not completed).
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// ObjectiveEngine manages objective lifecycle and hierarchy.
// It preserves WHY through every decomposition layer.
type ObjectiveEngine struct {
	mu         sync.RWMutex
	objectives map[string]*Objective
	now        func() time.Time
}

// NewObjectiveEngine creates a new objective engine.
func NewObjectiveEngine() *ObjectiveEngine {
	return &ObjectiveEngine{
		objectives: make(map[string]*Objective),
		now:        time.Now,
	}
}

// NewObjectiveEngineWithClock creates a new objective engine with an injectable clock.
func NewObjectiveEngineWithClock(now func() time.Time) *ObjectiveEngine {
	return &ObjectiveEngine{
		objectives: make(map[string]*Objective),
		now:        now,
	}
}

// CreateObjective creates a new objective with the given properties.
// The WHY (purpose) is mandatory and must be preserved through decomposition.
func (oe *ObjectiveEngine) CreateObjective(
	objType ObjectiveType,
	title, purpose, desiredOutcome, owner string,
	businessID string,
) (*Objective, error) {
	if purpose == "" {
		return nil, fmt.Errorf("purpose (WHY) is mandatory for all objectives")
	}

	oe.mu.Lock()
	defer oe.mu.Unlock()

	return oe.createObjectiveLocked(objType, title, purpose, desiredOutcome, owner, businessID)
}

// createObjectiveLocked is the internal implementation that assumes the caller holds mu.
func (oe *ObjectiveEngine) createObjectiveLocked(
	objType ObjectiveType,
	title, purpose, desiredOutcome, owner string,
	businessID string,
) (*Objective, error) {
	if purpose == "" {
		return nil, fmt.Errorf("purpose (WHY) is mandatory for all objectives")
	}

	now := oe.now()
	obj := &Objective{
		ID:             fmt.Sprintf("obj-%d", now.UnixNano()),
		Type:           objType,
		Status:         ObjectiveStatusDraft,
		BusinessID:     businessID,
		Title:          title,
		Purpose:        purpose,
		DesiredOutcome: desiredOutcome,
		Owner:          owner,
		Version:        1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	oe.objectives[obj.ID] = obj
	return obj, nil
}

// Activate transitions an objective from draft to active.
func (oe *ObjectiveEngine) Activate(objectiveID string) error {
	oe.mu.Lock()
	defer oe.mu.Unlock()

	obj, ok := oe.objectives[objectiveID]
	if !ok {
		return fmt.Errorf("objective %s not found", objectiveID)
	}

	if obj.Status != ObjectiveStatusDraft {
		return fmt.Errorf("can only activate draft objectives (current: %s)", obj.Status)
	}

	obj.Status = ObjectiveStatusActive
	obj.UpdatedAt = oe.now()
	return nil
}

// Complete marks an objective as completed.
func (oe *ObjectiveEngine) Complete(objectiveID string) error {
	oe.mu.Lock()
	defer oe.mu.Unlock()

	obj, ok := oe.objectives[objectiveID]
	if !ok {
		return fmt.Errorf("objective %s not found", objectiveID)
	}

	if obj.Status != ObjectiveStatusActive {
		return fmt.Errorf("can only complete active objectives (current: %s)", obj.Status)
	}

	now := oe.now()
	obj.Status = ObjectiveStatusCompleted
	obj.CompletedAt = &now
	obj.UpdatedAt = now
	return nil
}

// Block marks an objective as blocked.
func (oe *ObjectiveEngine) Block(objectiveID string) error {
	oe.mu.Lock()
	defer oe.mu.Unlock()

	obj, ok := oe.objectives[objectiveID]
	if !ok {
		return fmt.Errorf("objective %s not found", objectiveID)
	}

	obj.Status = ObjectiveStatusBlocked
	obj.UpdatedAt = oe.now()
	return nil
}

// Get returns an objective by ID.
func (oe *ObjectiveEngine) Get(objectiveID string) (*Objective, bool) {
	oe.mu.RLock()
	defer oe.mu.RUnlock()
	obj, ok := oe.objectives[objectiveID]
	return obj, ok
}

// Children returns all objectives that are children of the given objective.
func (oe *ObjectiveEngine) Children(objectiveID string) []*Objective {
	oe.mu.RLock()
	defer oe.mu.RUnlock()
	var children []*Objective
	for _, obj := range oe.objectives {
		if obj.ParentObjectiveID == objectiveID {
			children = append(children, obj)
		}
	}
	return children
}

// Decompose creates a child objective under a parent, preserving WHY.
// The child's purpose is derived from the parent's purpose.
func (oe *ObjectiveEngine) Decompose(
	parentID string,
	childType ObjectiveType,
	title, desiredOutcome string,
) (*Objective, error) {
	oe.mu.Lock()
	defer oe.mu.Unlock()

	parent, ok := oe.objectives[parentID]
	if !ok {
		return nil, fmt.Errorf("parent objective %s not found", parentID)
	}

	if parent.Status != ObjectiveStatusActive {
		return nil, fmt.Errorf("can only decompose active objectives (parent status: %s)", parent.Status)
	}

	child, err := oe.createObjectiveLocked(
		childType,
		title,
		parent.Purpose, // WHY preserved from parent
		desiredOutcome,
		parent.Owner,
		parent.BusinessID,
	)
	if err != nil {
		return nil, err
	}

	child.ParentObjectiveID = parentID
	child.DivisionID = parent.DivisionID
	child.UpdatedAt = oe.now()
	return child, nil
}

// ObjectiveCount returns the total number of objectives.
func (oe *ObjectiveEngine) ObjectiveCount() int {
	oe.mu.RLock()
	defer oe.mu.RUnlock()
	return len(oe.objectives)
}
