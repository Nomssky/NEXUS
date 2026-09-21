package cognition

import (
	"fmt"
	"sync"
	"time"
)

// PlanStatus tracks the lifecycle of a plan.
type PlanStatus string

const (
	PlanStatusDraft      PlanStatus = "draft"
	PlanStatusValidating PlanStatus = "validating"
	PlanStatusValidated  PlanStatus = "validated"
	PlanStatusExecuting  PlanStatus = "executing"
	PlanStatusCompleted  PlanStatus = "completed"
	PlanStatusFailed     PlanStatus = "failed"
	PlanStatusReplanning PlanStatus = "replanning"
)

// MissionStatus tracks the lifecycle of a mission within a plan.
type MissionStatus string

const (
	MissionStatusPending   MissionStatus = "pending"
	MissionStatusActive    MissionStatus = "active"
	MissionStatusCompleted MissionStatus = "completed"
	MissionStatusFailed    MissionStatus = "failed"
)

// TaskStatus tracks the lifecycle of a task within a mission.
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusAssigned  TaskStatus = "assigned"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
)

// Task represents a unit of work within a mission.
type Task struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      TaskStatus `json:"status"`
	Assignee    string     `json:"assignee,omitempty"`
	// Dependencies are task IDs that must complete before this task.
	Dependencies []string `json:"dependencies,omitempty"`
	// ResourceRequirements describes what resources are needed.
	ResourceRequirements string `json:"resource_requirements,omitempty"`
	// VerificationCriteria define how to verify completion.
	VerificationCriteria []string `json:"verification_criteria,omitempty"`
	// RetryPolicy defines retry behavior.
	RetryPolicy *RetryPolicy `json:"retry_policy,omitempty"`
}

// RetryPolicy defines how retries are handled.
type RetryPolicy struct {
	MaxRetries  int           `json:"max_retries"`
	RetryDelay  time.Duration `json:"retry_delay"`
	BackoffRate float64       `json:"backoff_rate"`
}

// Mission represents a coordinated set of tasks to achieve a sub-objective.
type Mission struct {
	ID          string        `json:"id"`
	ObjectiveID string        `json:"objective_id"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Status      MissionStatus `json:"status"`
	Tasks       []Task        `json:"tasks"`
	// WHY is preserved from the parent objective.
	WHY string `json:"why"`
}

// Plan represents a validated plan produced by the Planner.
// Plans are produced from authorized decisions and lead to workflow execution.
type Plan struct {
	ID                 string     `json:"id"`
	Status             PlanStatus `json:"status"`
	ObjectiveIDs       []string   `json:"objective_ids"`
	DecisionIDs        []string   `json:"decision_ids"`
	Missions           []Mission  `json:"missions"`
	Scope              string     `json:"scope"`
	Version            int        `json:"version"`
	CreatedBy          string     `json:"created_by"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	Deadline           *time.Time `json:"deadline,omitempty"`
	Constraints        []string   `json:"constraints,omitempty"`
	ResourceBudget     string     `json:"resource_budget,omitempty"`
	VerificationPolicy string     `json:"verification_policy,omitempty"`
}

// Planner converts objective-aligned, authorized decisions into executable plans.
// It decomposes objectives into missions and tasks, preserving WHY.
//
// Planner plans work. It does not execute work.
// Planner is forbidden from redefining objectives, overriding decisions,
// executing actions directly, or creating unbounded loops.
type Planner struct {
	mu    sync.RWMutex
	plans map[string]*Plan
	now   func() time.Time
}

// NewPlanner creates a new planner.
func NewPlanner() *Planner {
	return &Planner{
		plans: make(map[string]*Plan),
		now:   time.Now,
	}
}

// NewPlannerWithClock creates a new planner with an injectable clock.
func NewPlannerWithClock(now func() time.Time) *Planner {
	return &Planner{
		plans: make(map[string]*Plan),
		now:   now,
	}
}

// CreatePlan creates a new plan from objectives and decisions.
func (p *Planner) CreatePlan(
	objectiveIDs, decisionIDs []string,
	scope, createdBy string,
) (*Plan, error) {
	if len(objectiveIDs) == 0 {
		return nil, fmt.Errorf("at least one objective ID is required")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	now := p.now()
	plan := &Plan{
		ID:           fmt.Sprintf("plan-%d", now.UnixNano()),
		Status:       PlanStatusDraft,
		ObjectiveIDs: objectiveIDs,
		DecisionIDs:  decisionIDs,
		Scope:        scope,
		Version:      1,
		CreatedBy:    createdBy,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	p.plans[plan.ID] = plan
	return plan, nil
}

// AddMission adds a mission to a plan.
// The WHY is preserved from the objective.
func (p *Planner) AddMission(
	planID, objectiveID, title, description, why string,
) (*Mission, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	plan, ok := p.plans[planID]
	if !ok {
		return nil, fmt.Errorf("plan %s not found", planID)
	}

	mission := Mission{
		ID:          fmt.Sprintf("mis-%d", p.now().UnixNano()),
		ObjectiveID: objectiveID,
		Title:       title,
		Description: description,
		Status:      MissionStatusPending,
		WHY:         why, // WHY preserved from objective
	}

	plan.Missions = append(plan.Missions, mission)
	plan.UpdatedAt = p.now()
	return &mission, nil
}

// AddTask adds a task to a mission.
func (p *Planner) AddTask(
	planID, missionID, title, description string,
	dependencies []string,
) (*Task, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	plan, ok := p.plans[planID]
	if !ok {
		return nil, fmt.Errorf("plan %s not found", planID)
	}

	for i := range plan.Missions {
		if plan.Missions[i].ID == missionID {
			task := Task{
				ID:           fmt.Sprintf("task-%d", p.now().UnixNano()),
				Title:        title,
				Description:  description,
				Status:       TaskStatusPending,
				Dependencies: dependencies,
			}
			plan.Missions[i].Tasks = append(plan.Missions[i].Tasks, task)
			plan.UpdatedAt = p.now()
			return &task, nil
		}
	}

	return nil, fmt.Errorf("mission %s not found in plan %s", missionID, planID)
}

// Validate transitions a plan to validated status.
// A validated plan is ready for workflow execution.
func (p *Planner) Validate(planID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	plan, ok := p.plans[planID]
	if !ok {
		return fmt.Errorf("plan %s not found", planID)
	}

	if len(plan.Missions) == 0 {
		return fmt.Errorf("plan has no missions")
	}

	for _, mis := range plan.Missions {
		if len(mis.WHY) == 0 {
			return fmt.Errorf("mission %s has no WHY (objective intent lost)", mis.ID)
		}
	}

	plan.Status = PlanStatusValidated
	plan.UpdatedAt = p.now()
	return nil
}

// Get returns a plan by ID.
func (p *Planner) Get(planID string) (*Plan, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	plan, ok := p.plans[planID]
	return plan, ok
}

// PlanCount returns the total number of plans.
func (p *Planner) PlanCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.plans)
}
