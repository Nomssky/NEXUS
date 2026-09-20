// Package workflow implements the NEXUS Workflow & Orchestration Engine (C11).
//
// C11 turns objectives, decisions, and plans into durable autonomous work.
// A workflow is a persistent execution process that can survive restarts,
// coordinate agents/tools, wait for events, recover from failures, and
// replan when reality changes.
//
// C11 depends on C01–C08. C11 is forbidden from bypassing governance,
// owning model/provider selection, or granting authority.
//
// The locked conceptual flow is:
//
//	Plan → Workflow → Task Graph → Scheduler → Agent → Tool → Verification → Outcome
//
// Models have no execution authority. C11 neither selects models nor
// owns agent identity.
package workflow

import (
	"fmt"
	"time"
)

// WorkflowStatus tracks the lifecycle of a workflow.
type WorkflowStatus string

const (
	WorkflowStatusDraft      WorkflowStatus = "draft"
	WorkflowStatusReady      WorkflowStatus = "ready"
	WorkflowStatusQueued     WorkflowStatus = "queued"
	WorkflowStatusRunning    WorkflowStatus = "running"
	WorkflowStatusWaiting    WorkflowStatus = "waiting"
	WorkflowStatusPaused     WorkflowStatus = "paused"
	WorkflowStatusBlocked    WorkflowStatus = "blocked"
	WorkflowStatusReplanning WorkflowStatus = "replanning"
	WorkflowStatusCompleted  WorkflowStatus = "completed"
	WorkflowStatusFailed     WorkflowStatus = "failed"
	WorkflowStatusCancelled  WorkflowStatus = "cancelled"
)

// TaskStatus tracks the lifecycle of a task within a workflow.
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusReady     TaskStatus = "ready"
	TaskStatusQueued    TaskStatus = "queued"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusWaiting   TaskStatus = "waiting"
	TaskStatusBlocked   TaskStatus = "blocked"
	TaskStatusRetrying  TaskStatus = "retrying"
	TaskStatusVerifying TaskStatus = "verifying"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
	TaskStatusSkipped   TaskStatus = "skipped"
)

// Task represents one executable unit of work within a workflow.
type Task struct {
	ID          string     `json:"id"`
	WorkflowID  string     `json:"workflow_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      TaskStatus `json:"status"`
	Assignee    string     `json:"assignee,omitempty"`
	// Dependencies are task IDs that must complete before this task.
	Dependencies []string `json:"dependencies,omitempty"`
	// AgentID is the agent assigned to execute this task.
	AgentID string `json:"agent_id,omitempty"`
	// ToolIDs are the tools this task may use.
	ToolIDs []string `json:"tool_ids,omitempty"`
	// VerificationCriteria define how to verify completion.
	VerificationCriteria []string `json:"verification_criteria,omitempty"`
	// Result stores the task execution result.
	Result *TaskResult `json:"result,omitempty"`
	// RetryCount tracks how many times this task has been retried.
	RetryCount int `json:"retry_count"`
	// MaxRetries is the maximum number of retries allowed.
	MaxRetries int `json:"max_retries"`
	// CreatedAt is when the task was created.
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is when the task was last updated.
	UpdatedAt time.Time `json:"updated_at"`
	// CompletedAt is when the task completed (nil if not completed).
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// TaskResult stores the outcome of a task execution.
type TaskResult struct {
	// Status is the final status of the task.
	Status TaskStatus `json:"status"`
	// Output is the task output.
	Output string `json:"output,omitempty"`
	// Evidence is the evidence for verification.
	Evidence []string `json:"evidence,omitempty"`
	// Error is the error message if the task failed.
	Error string `json:"error,omitempty"`
	// CompletedAt is when the result was recorded.
	CompletedAt time.Time `json:"completed_at"`
}

// Workflow represents a durable execution process.
// Workflows are objective-linked, governed, and recoverable.
type Workflow struct {
	ID          string         `json:"id"`
	Status      WorkflowStatus `json:"status"`
	PlanID      string         `json:"plan_id,omitempty"`
	ObjectiveID string         `json:"objective_id,omitempty"`
	DecisionIDs []string       `json:"decision_ids,omitempty"`
	BusinessID  string         `json:"business_id,omitempty"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Tasks       []*Task        `json:"tasks"`
	// WHY is preserved from the parent objective.
	WHY string `json:"why"`
	// Version is the optimistic concurrency version.
	Version int `json:"version"`
	// CreatedBy is who created this workflow.
	CreatedBy string `json:"created_by"`
	// CreatedAt is when the workflow was created.
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is when the workflow was last updated.
	UpdatedAt time.Time `json:"updated_at"`
	// CompletedAt is when the workflow completed (nil if not completed).
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	// Deadline is when the workflow must complete (nil if no deadline).
	Deadline *time.Time `json:"deadline,omitempty"`
}

// WorkflowEngine manages workflow lifecycle and task execution.
// It turns plans into durable workflows with task graphs.
type WorkflowEngine struct {
	workflows map[string]*Workflow
	tasks     map[string]*Task // task index by task ID
	now       func() time.Time
}

// NewWorkflowEngine creates a new workflow engine.
func NewWorkflowEngine() *WorkflowEngine {
	return &WorkflowEngine{
		workflows: make(map[string]*Workflow),
		tasks:     make(map[string]*Task),
		now:       time.Now,
	}
}

// NewWorkflowEngineWithClock creates a new workflow engine with an injectable clock.
func NewWorkflowEngineWithClock(now func() time.Time) *WorkflowEngine {
	return &WorkflowEngine{
		workflows: make(map[string]*Workflow),
		tasks:     make(map[string]*Task),
		now:       now,
	}
}

// CreateWorkflow creates a new workflow from a plan.
func (we *WorkflowEngine) CreateWorkflow(
	planID, objectiveID string,
	businessID, title, description, why, createdBy string,
) (*Workflow, error) {
	if why == "" {
		return nil, fmt.Errorf("WHY is required for all workflows")
	}

	now := we.now()
	wf := &Workflow{
		ID:          fmt.Sprintf("wf-%d", now.UnixNano()),
		Status:      WorkflowStatusDraft,
		PlanID:      planID,
		ObjectiveID: objectiveID,
		BusinessID:  businessID,
		Title:       title,
		Description: description,
		WHY:         why,
		Version:     1,
		CreatedBy:   createdBy,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	we.workflows[wf.ID] = wf
	return wf, nil
}

// AddTask adds a task to a workflow.
func (we *WorkflowEngine) AddTask(
	workflowID, title, description string,
	dependencies []string,
	verificationCriteria []string,
) (*Task, error) {
	wf, ok := we.workflows[workflowID]
	if !ok {
		return nil, fmt.Errorf("workflow %s not found", workflowID)
	}

	now := we.now()
	task := &Task{
		ID:                   fmt.Sprintf("task-%d", now.UnixNano()),
		WorkflowID:           workflowID,
		Title:                title,
		Description:          description,
		Status:               TaskStatusPending,
		Dependencies:         dependencies,
		VerificationCriteria: verificationCriteria,
		MaxRetries:           3,
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	wf.Tasks = append(wf.Tasks, task)
	we.tasks[task.ID] = task
	wf.UpdatedAt = now
	return task, nil
}

// Activate transitions a workflow from draft to ready.
func (we *WorkflowEngine) Activate(workflowID string) error {
	wf, ok := we.workflows[workflowID]
	if !ok {
		return fmt.Errorf("workflow %s not found", workflowID)
	}

	if wf.Status != WorkflowStatusDraft {
		return fmt.Errorf("can only activate draft workflows (current: %s)", wf.Status)
	}

	if len(wf.Tasks) == 0 {
		return fmt.Errorf("cannot activate workflow with no tasks")
	}

	wf.Status = WorkflowStatusReady
	wf.UpdatedAt = we.now()

	// Mark tasks with no dependencies as ready, others as blocked
	for _, task := range wf.Tasks {
		if len(task.Dependencies) == 0 {
			task.Status = TaskStatusReady
		} else {
			task.Status = TaskStatusBlocked
		}
		task.UpdatedAt = we.now()
	}

	return nil
}

// Start transitions a workflow from ready to running.
func (we *WorkflowEngine) Start(workflowID string) error {
	wf, ok := we.workflows[workflowID]
	if !ok {
		return fmt.Errorf("workflow %s not found", workflowID)
	}

	if wf.Status != WorkflowStatusReady && wf.Status != WorkflowStatusQueued {
		return fmt.Errorf("can only start ready or queued workflows (current: %s)", wf.Status)
	}

	wf.Status = WorkflowStatusRunning
	wf.UpdatedAt = we.now()
	return nil
}

// CompleteTask marks a task as completed with a result.
func (we *WorkflowEngine) CompleteTask(taskID string, result *TaskResult) error {
	task, ok := we.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}

	if task.Status != TaskStatusRunning && task.Status != TaskStatusVerifying {
		return fmt.Errorf("can only complete running or verifying tasks (current: %s)", task.Status)
	}

	now := we.now()
	task.Status = TaskStatusCompleted
	task.Result = result
	task.CompletedAt = &now
	task.UpdatedAt = now

	// Unblock dependent tasks
	wf := we.workflows[task.WorkflowID]
	for _, t := range wf.Tasks {
		if t.Status == TaskStatusBlocked {
			allDone := true
			for _, depID := range t.Dependencies {
				dep, ok := we.tasks[depID]
				if !ok || dep.Status != TaskStatusCompleted {
					allDone = false
					break
				}
			}
			if allDone {
				t.Status = TaskStatusReady
				t.UpdatedAt = now
			}
		}
	}

	return nil
}

// VerifyTask sets the verification status of a task from evidence.
func (we *WorkflowEngine) VerifyTask(taskID string, passed bool, evidence []string) error {
	task, ok := we.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}

	if task.Status != TaskStatusCompleted {
		return fmt.Errorf("can only verify completed tasks (current: %s)", task.Status)
	}

	now := we.now()
	if passed {
		task.Status = TaskStatusCompleted // already completed, verification passed
	} else {
		task.Status = TaskStatusFailed
		if task.Result == nil {
			task.Result = &TaskResult{}
		}
		task.Result.Evidence = evidence
		task.Result.Error = "verification failed"
	}
	task.UpdatedAt = now

	return nil
}

// Complete marks a workflow as completed.
func (we *WorkflowEngine) Complete(workflowID string) error {
	wf, ok := we.workflows[workflowID]
	if !ok {
		return fmt.Errorf("workflow %s not found", workflowID)
	}

	// Check all tasks are completed
	for _, task := range wf.Tasks {
		if task.Status != TaskStatusCompleted && task.Status != TaskStatusSkipped {
			return fmt.Errorf("cannot complete workflow: task %s is %s", task.ID, task.Status)
		}
	}

	now := we.now()
	wf.Status = WorkflowStatusCompleted
	wf.CompletedAt = &now
	wf.UpdatedAt = now
	return nil
}

// Cancel cancels a workflow.
func (we *WorkflowEngine) Cancel(workflowID string) error {
	wf, ok := we.workflows[workflowID]
	if !ok {
		return fmt.Errorf("workflow %s not found", workflowID)
	}

	wf.Status = WorkflowStatusCancelled
	wf.UpdatedAt = we.now()

	// Cancel all non-completed tasks
	for _, task := range wf.Tasks {
		if task.Status != TaskStatusCompleted && task.Status != TaskStatusSkipped {
			task.Status = TaskStatusCancelled
			task.UpdatedAt = we.now()
		}
	}

	return nil
}

// AssignTaskToWorker assigns a task to a worker agent.
func (we *WorkflowEngine) AssignTaskToWorker(taskID, agentID string) error {
	task, ok := we.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}

	if task.Status != TaskStatusReady {
		return fmt.Errorf("can only assign ready tasks (current: %s)", task.Status)
	}

	task.AgentID = agentID
	task.Status = TaskStatusRunning
	task.UpdatedAt = we.now()
	return nil
}

// GetWorkflow returns a workflow by ID.
func (we *WorkflowEngine) GetWorkflow(workflowID string) (*Workflow, bool) {
	wf, ok := we.workflows[workflowID]
	return wf, ok
}

// GetTask returns a task by ID.
func (we *WorkflowEngine) GetTask(taskID string) (*Task, bool) {
	task, ok := we.tasks[taskID]
	return task, ok
}

// ReadyTasks returns all tasks that are ready to be scheduled.
func (we *WorkflowEngine) ReadyTasks(workflowID string) []*Task {
	wf, ok := we.workflows[workflowID]
	if !ok {
		return nil
	}

	var ready []*Task
	for _, task := range wf.Tasks {
		if task.Status == TaskStatusReady {
			ready = append(ready, task)
		}
	}
	return ready
}

// WorkflowCount returns the total number of workflows.
func (we *WorkflowEngine) WorkflowCount() int {
	return len(we.workflows)
}

// TaskCount returns the total number of tasks.
func (we *WorkflowEngine) TaskCount() int {
	return len(we.tasks)
}
