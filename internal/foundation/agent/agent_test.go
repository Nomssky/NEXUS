package agent

import (
	"testing"
	"time"
)

// TEST-M5-024: Provision agent
func TestAgentProvision(t *testing.T) {
	ar := NewAgentRuntime()
	def := &AgentDefinition{
		ID:           "def-1",
		Name:         "Worker",
		Type:         AgentTypeWorker,
		BusinessID:   "biz-1",
		Capabilities: []string{"read", "write"},
	}

	agent, err := ar.ProvisionAgent(def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agent.Status != AgentStatusProvisioning {
		t.Errorf("expected provisioning, got %v", agent.Status)
	}
	if agent.Definition.BusinessID != "biz-1" {
		t.Errorf("expected biz-1, got %v", agent.Definition.BusinessID)
	}
}

// TEST-M5-025: Agent rejects empty business ID
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

// TEST-M5-026: Start agent
func TestAgentStart(t *testing.T) {
	ar := NewAgentRuntime()
	def := &AgentDefinition{ID: "def-1", Name: "W", Type: AgentTypeWorker, BusinessID: "biz-1"}
	agent, _ := ar.ProvisionAgent(def)

	err := ar.StartAgent(agent.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agent.Status != AgentStatusRunning {
		t.Errorf("expected running, got %v", agent.Status)
	}
}

// TEST-M5-027: Assign task to agent
func TestAgentAssignTask(t *testing.T) {
	ar := NewAgentRuntime()
	def := &AgentDefinition{ID: "def-1", Name: "W", Type: AgentTypeWorker, BusinessID: "biz-1"}
	agent, _ := ar.ProvisionAgent(def)
	ar.StartAgent(agent.ID)

	err := ar.AssignTask(agent.ID, "task-1", "wf-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agent.Status != AgentStatusBusy {
		t.Errorf("expected busy, got %v", agent.Status)
	}
	if agent.CurrentTask != "task-1" {
		t.Errorf("expected task-1, got %v", agent.CurrentTask)
	}
}

// TEST-M5-028: Complete task
func TestAgentCompleteTask(t *testing.T) {
	ar := NewAgentRuntime()
	def := &AgentDefinition{ID: "def-1", Name: "W", Type: AgentTypeWorker, BusinessID: "biz-1"}
	agent, _ := ar.ProvisionAgent(def)
	ar.StartAgent(agent.ID)
	ar.AssignTask(agent.ID, "task-1", "wf-1")

	outcome := &TaskOutcome{
		TaskID:  "task-1",
		AgentID: agent.ID,
		Status:  "completed",
		Output:  "done",
	}

	err := ar.CompleteTask(agent.ID, outcome)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agent.Status != AgentStatusIdle {
		t.Errorf("expected idle, got %v", agent.Status)
	}
	if agent.CurrentTask != "" {
		t.Errorf("expected no current task, got %v", agent.CurrentTask)
	}
}

// TEST-M5-029: Heartbeat
func TestAgentHeartbeat(t *testing.T) {
	now := time.Now()
	ar := NewAgentRuntimeWithClock(func() time.Time { return now })
	def := &AgentDefinition{ID: "def-1", Name: "W", Type: AgentTypeWorker, BusinessID: "biz-1"}
	agent, _ := ar.ProvisionAgent(def)
	ar.StartAgent(agent.ID)

	err := ar.Heartbeat(agent.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agent.LastHeartbeat == nil {
		t.Error("expected heartbeat recorded")
	}
}

// TEST-M5-030: Terminate agent
func TestAgentTerminate(t *testing.T) {
	ar := NewAgentRuntime()
	def := &AgentDefinition{ID: "def-1", Name: "W", Type: AgentTypeWorker, BusinessID: "biz-1"}
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

// TEST-M5-031: Agent types
func TestAgentTypes(t *testing.T) {
	types := []AgentType{AgentTypeWorker, AgentTypeResearcher, AgentTypeReviewer, AgentTypeMonitor, AgentTypeTemporary}
	if len(types) != 5 {
		t.Errorf("expected 5 agent types, got %d", len(types))
	}
}
