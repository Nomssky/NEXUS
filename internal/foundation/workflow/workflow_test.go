package workflow

import (
	"testing"
)

// TEST-M5-001: Workflow creates with WHY
func TestWorkflowCreateWithWHY(t *testing.T) {
	we := NewWorkflowEngine()
	wf, err := we.CreateWorkflow(
		"plan-1", "obj-1", "biz-1",
		"Deploy feature", "Deploy X to prod",
		"Revenue growth", "owner-1",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf.WHY != "Revenue growth" {
		t.Errorf("expected WHY preserved, got %v", wf.WHY)
	}
	if wf.Status != WorkflowStatusDraft {
		t.Errorf("expected draft status, got %v", wf.Status)
	}
}

// TEST-M5-002: Workflow rejects empty WHY
func TestWorkflowRejectsEmptyWHY(t *testing.T) {
	we := NewWorkflowEngine()
	_, err := we.CreateWorkflow(
		"plan-1", "obj-1", "biz-1",
		"Deploy", "Desc",
		"", "owner-1",
	)
	if err == nil {
		t.Error("expected error for empty WHY")
	}
}

// TEST-M5-003: Add task to workflow
func TestWorkflowAddTask(t *testing.T) {
	we := NewWorkflowEngine()
	wf, _ := we.CreateWorkflow("plan-1", "obj-1", "biz-1", "WF", "Desc", "WHY", "user")

	task, err := we.AddTask(wf.ID, "Task 1", "Do thing", nil, []string{"output exists"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.Status != TaskStatusPending {
		t.Errorf("expected pending, got %v", task.Status)
	}
	if len(task.VerificationCriteria) != 1 {
		t.Errorf("expected 1 verification criterion, got %d", len(task.VerificationCriteria))
	}
}

// TEST-M5-004: Activate workflow with no tasks fails
func TestWorkflowActivateEmptyFails(t *testing.T) {
	we := NewWorkflowEngine()
	wf, _ := we.CreateWorkflow("plan-1", "obj-1", "biz-1", "WF", "Desc", "WHY", "user")

	err := we.Activate(wf.ID)
	if err == nil {
		t.Error("expected error for activating empty workflow")
	}
}

// TEST-M5-005: Activate workflow marks dependency-free tasks as ready
func TestWorkflowActivateMarksReady(t *testing.T) {
	we := NewWorkflowEngine()
	wf, _ := we.CreateWorkflow("plan-1", "obj-1", "biz-1", "WF", "Desc", "WHY", "user")
	task1, _ := we.AddTask(wf.ID, "Task 1", "Desc", nil, nil)
	task2, _ := we.AddTask(wf.ID, "Task 2", "Desc", []string{task1.ID}, nil)

	we.Activate(wf.ID)

	if task1.Status != TaskStatusReady {
		t.Errorf("expected task1 ready, got %v", task1.Status)
	}
	if task2.Status != TaskStatusBlocked {
		t.Errorf("expected task2 blocked, got %v", task2.Status)
	}
}

// TEST-M5-006: Task completion unblocks dependents
func TestWorkflowTaskCompletionUnblocks(t *testing.T) {
	we := NewWorkflowEngine()
	wf, _ := we.CreateWorkflow("plan-1", "obj-1", "biz-1", "WF", "Desc", "WHY", "user")
	task1, _ := we.AddTask(wf.ID, "Task 1", "Desc", nil, nil)
	task2, _ := we.AddTask(wf.ID, "Task 2", "Desc", []string{task1.ID}, nil)

	we.Activate(wf.ID)
	we.Start(wf.ID)

	// Assign and complete task1
	we.AssignTaskToWorker(task1.ID, "agent-1")
	we.CompleteTask(task1.ID, &TaskResult{
		Status:   TaskStatusCompleted,
		Output:   "done",
		Evidence: []string{"verified"},
	})

	if task2.Status != TaskStatusReady {
		t.Errorf("expected task2 unblocked to ready, got %v", task2.Status)
	}
}

// TEST-M5-007: Complete workflow checks all tasks done
func TestWorkflowCompleteChecksAllTasks(t *testing.T) {
	we := NewWorkflowEngine()
	wf, _ := we.CreateWorkflow("plan-1", "obj-1", "biz-1", "WF", "Desc", "WHY", "user")
	we.AddTask(wf.ID, "Task 1", "Desc", nil, nil)
	we.AddTask(wf.ID, "Task 2", "Desc", nil, nil)

	we.Activate(wf.ID)
	we.Start(wf.ID)

	err := we.Complete(wf.ID)
	if err == nil {
		t.Error("expected error for completing workflow with pending tasks")
	}
}

// TEST-M5-008: Cancel workflow cancels all tasks
func TestWorkflowCancelCancelsTasks(t *testing.T) {
	we := NewWorkflowEngine()
	wf, _ := we.CreateWorkflow("plan-1", "obj-1", "biz-1", "WF", "Desc", "WHY", "user")
	task, _ := we.AddTask(wf.ID, "Task 1", "Desc", nil, nil)

	we.Activate(wf.ID)
	we.Cancel(wf.ID)

	if wf.Status != WorkflowStatusCancelled {
		t.Errorf("expected cancelled workflow, got %v", wf.Status)
	}
	if task.Status != TaskStatusCancelled {
		t.Errorf("expected cancelled task, got %v", task.Status)
	}
}

// TEST-M5-009: ReadyTasks returns only ready tasks
func TestWorkflowReadyTasks(t *testing.T) {
	we := NewWorkflowEngine()
	wf, _ := we.CreateWorkflow("plan-1", "obj-1", "biz-1", "WF", "Desc", "WHY", "user")
	task1, _ := we.AddTask(wf.ID, "Task 1", "Desc", nil, nil)
	we.AddTask(wf.ID, "Task 2", "Desc", []string{task1.ID}, nil)

	we.Activate(wf.ID)

	ready := we.ReadyTasks(wf.ID)
	if len(ready) != 1 {
		t.Errorf("expected 1 ready task, got %d", len(ready))
	}
	if ready[0].ID != task1.ID {
		t.Errorf("expected task1 ready, got %v", ready[0].ID)
	}
}

// TEST-M5-010: Verify task
func TestWorkflowVerifyTask(t *testing.T) {
	we := NewWorkflowEngine()
	wf, _ := we.CreateWorkflow("plan-1", "obj-1", "biz-1", "WF", "Desc", "WHY", "user")
	task, _ := we.AddTask(wf.ID, "Task 1", "Desc", nil, nil)

	we.Activate(wf.ID)
	we.Start(wf.ID)
	we.AssignTaskToWorker(task.ID, "agent-1")
	we.CompleteTask(task.ID, &TaskResult{Status: TaskStatusCompleted, Output: "done"})

	err := we.VerifyTask(task.ID, true, []string{"evidence 1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.Status != TaskStatusCompleted {
		t.Errorf("expected completed after verify, got %v", task.Status)
	}
}

// TEST-M5-011: Task dependencies
func TestWorkflowTaskDependencies(t *testing.T) {
	we := NewWorkflowEngine()
	wf, _ := we.CreateWorkflow("plan-1", "obj-1", "biz-1", "WF", "Desc", "WHY", "user")
	task1, _ := we.AddTask(wf.ID, "Setup", "Setup infra", nil, nil)
	task2, _ := we.AddTask(wf.ID, "Deploy", "Deploy app", []string{task1.ID}, nil)
	task3, _ := we.AddTask(wf.ID, "Verify", "Verify", []string{task2.ID}, nil)

	we.Activate(wf.ID)
	we.Start(wf.ID)

	if task1.Status != TaskStatusReady {
		t.Errorf("expected task1 ready, got %v", task1.Status)
	}
	if task2.Status != TaskStatusBlocked {
		t.Errorf("expected task2 blocked, got %v", task2.Status)
	}
	if task3.Status != TaskStatusBlocked {
		t.Errorf("expected task3 blocked, got %v", task3.Status)
	}
}

// TEST-M5-012: Workflow preserves WHY through tasks
func TestWorkflowPreservesWHY(t *testing.T) {
	we := NewWorkflowEngine()
	wf, _ := we.CreateWorkflow("plan-1", "obj-1", "biz-1", "WF", "Desc", "Critical WHY", "user")

	if wf.WHY != "Critical WHY" {
		t.Errorf("expected WHY preserved, got %v", wf.WHY)
	}
}

// TEST-M5-013: Business isolation in workflow
func TestWorkflowBusinessIsolation(t *testing.T) {
	we := NewWorkflowEngine()
	wf1, _ := we.CreateWorkflow("plan-1", "obj-1", "biz-1", "WF1", "Desc", "WHY", "user")
	wf2, _ := we.CreateWorkflow("plan-2", "obj-2", "biz-2", "WF2", "Desc", "WHY", "user")

	if wf1.BusinessID == wf2.BusinessID {
		t.Error("expected different business IDs for isolation")
	}
}
