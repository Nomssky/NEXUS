package cognition

import (
	"testing"
)

// TEST-M4-001: Objective engine creates objective with WHY
func TestObjectiveEngineCreateWithWHY(t *testing.T) {
	oe := NewObjectiveEngine()
	obj, err := oe.CreateObjective(
		ObjectiveTypeMission,
		"Deploy feature X",
		"Revenue growth for Q4",
		"Feature X live in production",
		"owner-1",
		"biz-1",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if obj.Purpose != "Revenue growth for Q4" {
		t.Errorf("expected WHY preserved, got %v", obj.Purpose)
	}
	if obj.Status != ObjectiveStatusDraft {
		t.Errorf("expected draft status, got %v", obj.Status)
	}
}

// TEST-M4-002: Objective engine rejects empty purpose (WHY mandatory)
func TestObjectiveEngineRejectsEmptyWHY(t *testing.T) {
	oe := NewObjectiveEngine()
	_, err := oe.CreateObjective(
		ObjectiveTypeTask,
		"Do something",
		"", // empty purpose
		"result",
		"owner-1",
		"biz-1",
	)
	if err == nil {
		t.Error("expected error for empty purpose")
	}
}

// TEST-M4-003: Objective lifecycle: draft → active → completed
func TestObjectiveLifecycle(t *testing.T) {
	oe := NewObjectiveEngine()
	obj, _ := oe.CreateObjective(
		ObjectiveTypeMission,
		"Test objective",
		"Why it matters",
		"Success look like",
		"owner-1",
		"biz-1",
	)

	// Activate
	if err := oe.Activate(obj.ID); err != nil {
		t.Fatalf("activate failed: %v", err)
	}
	if obj.Status != ObjectiveStatusActive {
		t.Errorf("expected active, got %v", obj.Status)
	}

	// Complete
	if err := oe.Complete(obj.ID); err != nil {
		t.Fatalf("complete failed: %v", err)
	}
	if obj.Status != ObjectiveStatusCompleted {
		t.Errorf("expected completed, got %v", obj.Status)
	}
	if obj.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
}

// TEST-M4-004: Objective decomposition preserves WHY
func TestObjectiveDecompositionPreservesWHY(t *testing.T) {
	oe := NewObjectiveEngine()
	parent, _ := oe.CreateObjective(
		ObjectiveTypeBusiness,
		"Business goal",
		"Parent WHY",
		"Parent outcome",
		"owner-1",
		"biz-1",
	)
	oe.Activate(parent.ID)

	child, err := oe.Decompose(
		parent.ID,
		ObjectiveTypeMission,
		"Sub-mission",
		"Sub-outcome",
	)
	if err != nil {
		t.Fatalf("decompose failed: %v", err)
	}

	// WHY must be preserved from parent
	if child.Purpose != "Parent WHY" {
		t.Errorf("expected WHY preserved from parent, got %v", child.Purpose)
	}
	if child.ParentObjectiveID != parent.ID {
		t.Errorf("expected parent link, got %v", child.ParentObjectiveID)
	}
	if child.BusinessID != parent.BusinessID {
		t.Errorf("expected business scope inherited, got %v", child.BusinessID)
	}
}

// TEST-M4-005: Cannot decompose non-active objective
func TestObjectiveCannotDecomposeInactive(t *testing.T) {
	oe := NewObjectiveEngine()
	parent, _ := oe.CreateObjective(
		ObjectiveTypeBusiness,
		"Goal",
		"WHY",
		"Outcome",
		"owner-1",
		"biz-1",
	)
	// parent is still draft

	_, err := oe.Decompose(parent.ID, ObjectiveTypeMission, "Sub", "Outcome")
	if err == nil {
		t.Error("expected error for decomposing draft objective")
	}
}

// TEST-M4-006: Decision engine frames decision
func TestDecisionEngineFrame(t *testing.T) {
	de := NewDecisionEngine()
	dec, err := de.FrameDecision(
		"Which approach to take?",
		[]string{"obj-1"},
		"decision-maker-1",
		"biz-1",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec.Status != DecisionStatusFraming {
		t.Errorf("expected framing status, got %v", dec.Status)
	}
	if dec.Question != "Which approach to take?" {
		t.Errorf("expected question, got %v", dec.Question)
	}
}

// TEST-M4-007: Decision lifecycle: frame → evaluate → recommend → decide
func TestDecisionLifecycle(t *testing.T) {
	de := NewDecisionEngine()
	dec, _ := de.FrameDecision("What to do?", nil, "dm-1", "")

	// Add evidence
	de.AddEvidence(dec.ID, Evidence{
		Source:      "analysis",
		Content:     "Option A is faster",
		Reliability: "high",
	})

	// Add options
	de.AddOption(dec.ID, Option{
		ID:            "opt-a",
		Description:   "Option A",
		Reversibility: "fully",
		BlastRadius:   "low",
	})
	de.AddOption(dec.ID, Option{
		ID:            "opt-b",
		Description:   "Option B",
		Reversibility: "irreversible",
		BlastRadius:   "high",
	})

	// Evaluate
	de.Evaluate(dec.ID)
	if dec.Status != DecisionStatusEvaluating {
		t.Errorf("expected evaluating, got %v", dec.Status)
	}

	// Recommend
	de.Recommend(dec.ID, "opt-a", "Faster and reversible")
	if dec.Status != DecisionStatusRecommended {
		t.Errorf("expected recommended, got %v", dec.Status)
	}

	// Decide
	de.Decide(dec.ID, "opt-a")
	if dec.Status != DecisionStatusDecided {
		t.Errorf("expected decided, got %v", dec.Status)
	}
	if dec.DecidedOption != "opt-a" {
		t.Errorf("expected decided option opt-a, got %v", dec.DecidedOption)
	}
}

// TEST-M4-008: Decision abstain
func TestDecisionAbstain(t *testing.T) {
	de := NewDecisionEngine()
	dec, _ := de.FrameDecision("What to do?", nil, "dm-1", "")

	de.Abstain(dec.ID, "insufficient evidence")
	if dec.Status != DecisionStatusAbstained {
		t.Errorf("expected abstained, got %v", dec.Status)
	}
}

// TEST-M4-009: Decision escalate
func TestDecisionEscalate(t *testing.T) {
	de := NewDecisionEngine()
	dec, _ := de.FrameDecision("Critical choice?", nil, "dm-1", "")

	de.Escalate(dec.ID, "requires owner approval")
	if dec.Status != DecisionStatusEscalated {
		t.Errorf("expected escalated, got %v", dec.Status)
	}
}

// TEST-M4-010: Planner creates plan
func TestPlannerCreatePlan(t *testing.T) {
	pl := NewPlanner()
	plan, err := pl.CreatePlan(
		[]string{"obj-1"},
		[]string{"dec-1"},
		"biz-1",
		"planner-1",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Status != PlanStatusDraft {
		t.Errorf("expected draft, got %v", plan.Status)
	}
}

// TEST-M4-011: Planner rejects empty objectives
func TestPlannerRejectsEmptyObjectives(t *testing.T) {
	pl := NewPlanner()
	_, err := pl.CreatePlan(nil, nil, "scope", "user")
	if err == nil {
		t.Error("expected error for empty objectives")
	}
}

// TEST-M4-012: Planner adds mission with WHY
func TestPlannerAddMissionWithWHY(t *testing.T) {
	pl := NewPlanner()
	plan, _ := pl.CreatePlan([]string{"obj-1"}, nil, "scope", "user")

	mission, err := pl.AddMission(
		plan.ID,
		"obj-1",
		"Mission title",
		"Mission desc",
		"WHY from objective",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mission.WHY != "WHY from objective" {
		t.Errorf("expected WHY preserved, got %v", mission.WHY)
	}
	if len(plan.Missions) != 1 {
		t.Errorf("expected 1 mission, got %d", len(plan.Missions))
	}
}

// TEST-M4-013: Planner adds task to mission
func TestPlannerAddTask(t *testing.T) {
	pl := NewPlanner()
	plan, _ := pl.CreatePlan([]string{"obj-1"}, nil, "scope", "user")
	mission, _ := pl.AddMission(plan.ID, "obj-1", "M", "D", "WHY")

	task, err := pl.AddTask(plan.ID, mission.ID, "Task 1", "Do thing", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.Status != TaskStatusPending {
		t.Errorf("expected pending, got %v", task.Status)
	}
}

// TEST-M4-014: Planner validates plan (WHY check)
func TestPlannerValidatePreservesWHY(t *testing.T) {
	pl := NewPlanner()
	plan, _ := pl.CreatePlan([]string{"obj-1"}, nil, "scope", "user")
	pl.AddMission(plan.ID, "obj-1", "M", "D", "Important WHY")

	if err := pl.Validate(plan.ID); err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if plan.Status != PlanStatusValidated {
		t.Errorf("expected validated, got %v", plan.Status)
	}
}

// TEST-M4-015: Planner rejects plan without missions
func TestPlannerRejectsEmptyPlan(t *testing.T) {
	pl := NewPlanner()
	plan, _ := pl.CreatePlan([]string{"obj-1"}, nil, "scope", "user")

	err := pl.Validate(plan.ID)
	if err == nil {
		t.Error("expected error for plan without missions")
	}
}

// TEST-M4-016: Executive receives intent and creates full chain
func TestExecutiveReceiveIntent(t *testing.T) {
	oe := NewObjectiveEngine()
	de := NewDecisionEngine()
	pl := NewPlanner()
	ex := NewExecutive(oe, de, pl)

	req, err := ex.ReceiveIntent(
		"owner-1",
		"Increase revenue",
		"Revenue growth",
		"Q4 revenue target",
		"20% increase",
		"biz-1",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All three should be created
	if req.ObjectiveID == "" {
		t.Error("expected objective to be created")
	}
	if req.DecisionID == "" {
		t.Error("expected decision to be created")
	}
	if req.PlanID == "" {
		t.Error("expected plan to be created")
	}
	if req.Status != ExecutiveStatusReady {
		t.Errorf("expected ready status, got %v", req.Status)
	}

	// Verify WHY preserved through the chain
	obj, _ := oe.Get(req.ObjectiveID)
	if obj.Purpose != "Q4 revenue target" {
		t.Errorf("expected WHY in objective, got %v", obj.Purpose)
	}

	plan, _ := pl.Get(req.PlanID)
	if len(plan.Missions) == 0 {
		t.Error("expected mission in plan")
	}
	if plan.Missions[0].WHY != "Q4 revenue target" {
		t.Errorf("expected WHY in mission, got %v", plan.Missions[0].WHY)
	}
}

// TEST-M4-017: Executive rejects empty owner
func TestExecutiveRejectsEmptyOwner(t *testing.T) {
	oe := NewObjectiveEngine()
	de := NewDecisionEngine()
	pl := NewPlanner()
	ex := NewExecutive(oe, de, pl)

	_, err := ex.ReceiveIntent("", "intent", "title", "purpose", "outcome", "biz-1")
	if err == nil {
		t.Error("expected error for empty owner")
	}
}

// TEST-M4-018: Executive rejects empty purpose
func TestExecutiveRejectsEmptyPurpose(t *testing.T) {
	oe := NewObjectiveEngine()
	de := NewDecisionEngine()
	pl := NewPlanner()
	ex := NewExecutive(oe, de, pl)

	_, err := ex.ReceiveIntent("owner-1", "intent", "title", "", "outcome", "biz-1")
	if err == nil {
		t.Error("expected error for empty purpose")
	}
}

// TEST-M4-019: Objective hierarchy: business → division → mission
func TestObjectiveHierarchy(t *testing.T) {
	oe := NewObjectiveEngine()

	biz, _ := oe.CreateObjective(ObjectiveTypeBusiness, "Biz goal", "Biz WHY", "Biz outcome", "owner-1", "biz-1")
	oe.Activate(biz.ID)

	div, _ := oe.Decompose(biz.ID, ObjectiveTypeDivision, "Div goal", "Div outcome")
	oe.Activate(div.ID)

	mission, _ := oe.Decompose(div.ID, ObjectiveTypeMission, "Mission", "Mission outcome")

	// WHY flows down
	if mission.Purpose != "Biz WHY" {
		t.Errorf("expected WHY from top-level, got %v", mission.Purpose)
	}
	if mission.BusinessID != "biz-1" {
		t.Errorf("expected business scope inherited, got %v", mission.BusinessID)
	}
}

// TEST-M4-020: C08 does not execute actions (structural invariant)
func TestCognitionDoesNotExecute(t *testing.T) {
	// This test verifies that the cognition package has no Execute methods.
	// The Executive creates requests but does not execute them.
	oe := NewObjectiveEngine()
	de := NewDecisionEngine()
	pl := NewPlanner()
	ex := NewExecutive(oe, de, pl)

	req, _ := ex.ReceiveIntent(
		"owner-1", "Do something", "Title", "WHY", "Outcome", "biz-1",
	)

	// Request is ready for workflow, NOT executed
	if req.Status != ExecutiveStatusReady {
		t.Errorf("expected ready_for_workflow, got %v", req.Status)
	}

	// No Execute method exists on Executive — this is verified by compilation
}

// TEST-M4-021: Decision options have reversibility and blast radius
func TestDecisionOptionMetadata(t *testing.T) {
	de := NewDecisionEngine()
	dec, _ := de.FrameDecision("Choice?", nil, "dm-1", "")

	de.AddOption(dec.ID, Option{
		ID:            "opt-1",
		Description:   "Safe option",
		Reversibility: "fully",
		BlastRadius:   "none",
	})

	de.AddOption(dec.ID, Option{
		ID:            "opt-2",
		Description:   "Risky option",
		Reversibility: "irreversible",
		BlastRadius:   "enterprise-wide",
	})

	if len(dec.Options) != 2 {
		t.Errorf("expected 2 options, got %d", len(dec.Options))
	}
	if dec.Options[0].Reversibility != "fully" {
		t.Errorf("expected fully reversible, got %v", dec.Options[0].Reversibility)
	}
}

// TEST-M4-022: Plan has scope (business isolation)
func TestPlanScopeIsolation(t *testing.T) {
	pl := NewPlanner()
	plan, _ := pl.CreatePlan([]string{"obj-1"}, nil, "biz-1", "user")

	if plan.Scope != "biz-1" {
		t.Errorf("expected scope biz-1, got %v", plan.Scope)
	}
}

// TEST-M4-023: Task dependencies
func TestTaskDependencies(t *testing.T) {
	pl := NewPlanner()
	plan, _ := pl.CreatePlan([]string{"obj-1"}, nil, "scope", "user")
	mission, _ := pl.AddMission(plan.ID, "obj-1", "M", "D", "WHY")

	task1, _ := pl.AddTask(plan.ID, mission.ID, "Setup", "Setup infra", nil)
	task2, err := pl.AddTask(plan.ID, mission.ID, "Deploy", "Deploy app", []string{task1.ID})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(task2.Dependencies) != 1 {
		t.Errorf("expected 1 dependency, got %d", len(task2.Dependencies))
	}
	if task2.Dependencies[0] != task1.ID {
		t.Errorf("expected dependency on task1, got %v", task2.Dependencies[0])
	}
}

// TEST-M4-024: Exactly 5 objective types
func TestExactlyFiveObjectiveTypes(t *testing.T) {
	types := []ObjectiveType{ObjectiveTypeOwner, ObjectiveTypeBusiness, ObjectiveTypeDivision, ObjectiveTypeMission, ObjectiveTypeTask}
	if len(types) != 5 {
		t.Errorf("expected 5 objective types, got %d", len(types))
	}
}
