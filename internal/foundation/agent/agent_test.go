package agent

import (
	"testing"
	"time"
)

// TEST-M7-001: Agent rejects empty business ID
func TestAgentRejectsEmptyBusinessID(t *testing.T) {
	ar := NewAgentRuntime()
	def := &AgentDefinition{
		ID:   "def-1",
		Name: "Worker",
		Type: AgentTypeWorker,
	}

	_, err := ar.ProvisionAgent(def)
	if err == nil {
		t.Error("expected error for empty business ID")
	}
}

// TEST-M7-002: Agent rejects authority > parent authority
func TestAgentRejectsExcessiveAuthority(t *testing.T) {
	ar := NewAgentRuntime()
	def := &AgentDefinition{
		ID:              "def-1",
		Name:            "Worker",
		Type:            AgentTypeWorker,
		BusinessID:      "biz-1",
		Authority:       AuthorityAdmin,
		ParentAuthority: AuthorityRead,
	}

	_, err := ar.ProvisionAgent(def)
	if err == nil {
		t.Error("expected error for authority > parent authority")
	}
}

// TEST-M7-003: Agent accepts valid authority chain
func TestAgentAcceptsValidAuthority(t *testing.T) {
	ar := NewAgentRuntime()
	def := &AgentDefinition{
		ID:              "def-1",
		Name:            "Worker",
		Type:            AgentTypeWorker,
		BusinessID:      "biz-1",
		Authority:       AuthorityRead,
		ParentAuthority: AuthorityWrite,
	}

	agent, err := ar.ProvisionAgent(def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agent.Definition.Authority != AuthorityRead {
		t.Errorf("expected read authority, got %v", agent.Definition.Authority)
	}
}

// TEST-M7-004: Agent lifecycle: provision → start → assign → complete
func TestAgentLifecycle(t *testing.T) {
	ar := NewAgentRuntime()
	def := &AgentDefinition{
		ID:              "def-1",
		Name:            "Worker",
		Type:            AgentTypeWorker,
		BusinessID:      "biz-1",
		Authority:       AuthorityRead,
		ParentAuthority: AuthorityRead,
	}
	agent, _ := ar.ProvisionAgent(def)
	ar.StartAgent(agent.ID)

	exec := &TaskExecution{
		TaskID:     "task-1",
		WorkflowID: "wf-1",
	}
	ar.AssignTask(agent.ID, exec)

	if agent.Status != AgentStatusBusy {
		t.Errorf("expected busy, got %v", agent.Status)
	}

	outcome := &TaskOutcome{
		TaskID: "task-1",
		Status: "completed",
		Output: "done",
	}
	ar.CompleteTask(agent.ID, outcome)

	if agent.Status != AgentStatusIdle {
		t.Errorf("expected idle, got %v", agent.Status)
	}
	if agent.TasksCompleted != 1 {
		t.Errorf("expected 1 task completed, got %d", agent.TasksCompleted)
	}
}

// TEST-M7-005: Heartbeat recovery from suspected
func TestHeartbeatRecovery(t *testing.T) {
	ar := NewAgentRuntime()
	def := &AgentDefinition{
		ID:              "def-1",
		Name:            "Worker",
		Type:            AgentTypeWorker,
		BusinessID:      "biz-1",
		Authority:       AuthorityRead,
		ParentAuthority: AuthorityRead,
	}
	agent, _ := ar.ProvisionAgent(def)
	ar.StartAgent(agent.ID)
	ar.Heartbeat(agent.ID)

	// Simulate suspected state
	agent.Status = AgentStatusSuspected

	// Heartbeat recovers
	ar.Heartbeat(agent.ID)
	if agent.Status != AgentStatusRunning {
		t.Errorf("expected running after heartbeat recovery, got %v", agent.Status)
	}
}

// TEST-M7-006: CheckHeartbeat detects unresponsive
func TestCheckHeartbeatTimeout(t *testing.T) {
	now := time.Now()
	ar := NewAgentRuntimeWithClock(func() time.Time { return now })
	def := &AgentDefinition{
		ID:              "def-1",
		Name:            "Worker",
		Type:            AgentTypeWorker,
		BusinessID:      "biz-1",
		Authority:       AuthorityRead,
		ParentAuthority: AuthorityRead,
	}
	agent, _ := ar.ProvisionAgent(def)
	ar.StartAgent(agent.ID)

	// Set heartbeat to 10 seconds ago
	past := now.Add(-10 * time.Second)
	agent.LastHeartbeat = &past

	status := ar.CheckHeartbeat(agent.ID, 5*time.Second)
	if status != AgentStatusSuspected {
		t.Errorf("expected suspected, got %v", status)
	}
}

// TEST-M7-007: Lease expiry detection
func TestLeaseExpiry(t *testing.T) {
	now := time.Now()
	ar := NewAgentRuntimeWithClock(func() time.Time { return now })
	def := &AgentDefinition{
		ID:              "def-1",
		Name:            "Worker",
		Type:            AgentTypeWorker,
		BusinessID:      "biz-1",
		Authority:       AuthorityRead,
		ParentAuthority: AuthorityRead,
		Budget:          Budget{MaxTaskTime: 5 * time.Minute},
	}
	agent, _ := ar.ProvisionAgent(def)
	ar.StartAgent(agent.ID)

	exec := &TaskExecution{
		TaskID: "task-1",
		Budget: Budget{MaxTaskTime: 5 * time.Minute},
	}
	ar.AssignTask(agent.ID, exec)

	// Lease should not be expired yet
	if ar.CheckLease(agent.ID) {
		t.Error("expected lease not expired")
	}

	// Advance time past lease
	expiry := now.Add(5 * time.Minute)
	agent.LeaseExpiry = &expiry

	// Now it should be expired (clock is 6 minutes ahead, expiry is 5 minutes ahead)
	ar2 := NewAgentRuntimeWithClock(func() time.Time { return now.Add(6 * time.Minute) })
	ar2.agents = ar.agents
	ar2.spawnSafety = ar.spawnSafety

	if !ar2.CheckLease(agent.ID) {
		t.Error("expected lease expired after time advance")
	}
}

// TEST-M7-008: Spawn child with limits
func TestSpawnChildLimits(t *testing.T) {
	ar := NewAgentRuntime()
	parentDef := &AgentDefinition{
		ID:              "parent-1",
		Name:            "Parent",
		Type:            AgentTypeWorker,
		BusinessID:      "biz-1",
		Authority:       AuthorityWrite,
		ParentAuthority: AuthorityWrite,
		SpawnLimits:     SpawnLimits{MaxChildren: 2},
	}
	parent, _ := ar.ProvisionAgent(parentDef)
	ar.StartAgent(parent.ID)

	childDef := &AgentDefinition{
		ID:              "child-1",
		Name:            "Child",
		Type:            AgentTypeWorker,
		BusinessID:      "biz-1",
		Authority:       AuthorityRead,
		ParentAuthority: AuthorityWrite,
	}

	// Spawn 2 children (should succeed)
	_, err := ar.SpawnChild(&SpawnRequest{ParentAgentID: parent.ID, Definition: childDef})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = ar.SpawnChild(&SpawnRequest{ParentAgentID: parent.ID, Definition: childDef})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Third child should fail
	_, err = ar.SpawnChild(&SpawnRequest{ParentAgentID: parent.ID, Definition: childDef})
	if err == nil {
		t.Error("expected error for exceeding max children")
	}
}

// TEST-M7-009: Spawn child depth limit
func TestSpawnChildDepthLimit(t *testing.T) {
	ar := NewAgentRuntime()
	def := &AgentDefinition{
		ID:              "agent-1",
		Name:            "Agent",
		Type:            AgentTypeWorker,
		BusinessID:      "biz-1",
		Authority:       AuthorityWrite,
		ParentAuthority: AuthorityWrite,
		SpawnLimits:     SpawnLimits{MaxDepth: 1},
	}
	agent, _ := ar.ProvisionAgent(def)
	ar.StartAgent(agent.ID)

	// First child (should succeed)
	childDef := &AgentDefinition{
		ID:              "child-1",
		Name:            "Child",
		Type:            AgentTypeWorker,
		BusinessID:      "biz-1",
		Authority:       AuthorityRead,
		ParentAuthority: AuthorityWrite,
		SpawnLimits:     SpawnLimits{MaxDepth: 1},
	}

	child, _ := ar.SpawnChild(&SpawnRequest{ParentAgentID: agent.ID, Definition: childDef})
	ar.StartAgent(child.ID)

	// Grandchild should fail (depth 1 = max)
	grandchildDef := &AgentDefinition{
		ID:              "grandchild-1",
		Name:            "Grandchild",
		Type:            AgentTypeWorker,
		BusinessID:      "biz-1",
		Authority:       AuthorityRead,
		ParentAuthority: AuthorityRead,
	}
	_, err := ar.SpawnChild(&SpawnRequest{ParentAgentID: child.ID, Definition: grandchildDef})
	if err == nil {
		t.Error("expected error for exceeding max depth")
	}
}

// TEST-M7-010: Cancel task (cancel ≠ failure)
func TestCancelTaskNotFailure(t *testing.T) {
	ar := NewAgentRuntime()
	def := &AgentDefinition{
		ID:              "def-1",
		Name:            "Worker",
		Type:            AgentTypeWorker,
		BusinessID:      "biz-1",
		Authority:       AuthorityRead,
		ParentAuthority: AuthorityRead,
	}
	agent, _ := ar.ProvisionAgent(def)
	ar.StartAgent(agent.ID)
	ar.AssignTask(agent.ID, &TaskExecution{TaskID: "task-1"})

	err := ar.CancelTask(agent.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agent.Status != AgentStatusIdle {
		t.Errorf("expected idle after cancel, got %v", agent.Status)
	}
	// Cancel is not failure - agent is still usable
	if agent.CurrentTask != "" {
		t.Error("expected no current task after cancel")
	}
}

// TEST-M7-011: Terminate agent
func TestTerminateAgent(t *testing.T) {
	ar := NewAgentRuntime()
	def := &AgentDefinition{
		ID:              "def-1",
		Name:            "Worker",
		Type:            AgentTypeWorker,
		BusinessID:      "biz-1",
		Authority:       AuthorityRead,
		ParentAuthority: AuthorityRead,
	}
	agent, _ := ar.ProvisionAgent(def)
	ar.StartAgent(agent.ID)

	err := ar.TerminateAgent(agent.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agent.Status != AgentStatusTerminated {
		t.Errorf("expected terminated, got %v", agent.Status)
	}
	if agent.TerminatedAt == nil {
		t.Error("expected terminated at timestamp")
	}
}

// TEST-M7-012: Authority levels
func TestAuthorityLevels(t *testing.T) {
	levels := []Authority{AuthorityNone, AuthorityRead, AuthorityWrite, AuthorityExecute, AuthorityAdmin}
	if len(levels) != 5 {
		t.Errorf("expected 5 authority levels, got %d", len(levels))
	}
}

// TEST-M7-013: Agent types
func TestAgentTypes(t *testing.T) {
	types := []AgentType{AgentTypeWorker, AgentTypeResearcher, AgentTypeReviewer, AgentTypeMonitor, AgentTypeTemporary, AgentTypeSpecialist}
	if len(types) != 6 {
		t.Errorf("expected 6 agent types, got %d", len(types))
	}
}

// TEST-M7-014: Budget enforcement on complete
func TestBudgetEnforcement(t *testing.T) {
	ar := NewAgentRuntime()
	def := &AgentDefinition{
		ID:              "def-1",
		Name:            "Worker",
		Type:            AgentTypeWorker,
		BusinessID:      "biz-1",
		Authority:       AuthorityRead,
		ParentAuthority: AuthorityRead,
		Budget:          Budget{MaxTokens: 1000},
	}
	agent, _ := ar.ProvisionAgent(def)
	ar.StartAgent(agent.ID)
	ar.AssignTask(agent.ID, &TaskExecution{TaskID: "task-1"})

	// Complete with over budget
	err := ar.CompleteTask(agent.ID, &TaskOutcome{
		TaskID:     "task-1",
		Status:     "completed",
		TokensUsed: 1500,
	})
	if err == nil {
		t.Error("expected error for exceeding token budget")
	}
}
