// Package agent implements the NEXUS minimal Agent Runtime (C12).
//
// C12 provides agent definition, lifecycle, and bounded task execution.
// Agents are units of autonomous work with identity, capabilities,
// authority, and scope. Models have no execution authority.
//
// M5 implements the minimal agent runtime: definition, provisioning,
// task execution, and heartbeat. Full agent lifecycle (anti-spawn-storm,
// lease fencing, recovery) is M7 scope.
//
// The locked conceptual flow is:
//
//	Scheduler → Agent Runtime → Tool Runtime → Verification → Outcome
//
// Agent does NOT own model/provider selection (M6 scope).
// Agent does NOT own tool selection (Tool Runtime scope).
package agent

import (
	"fmt"
	"time"
)

// AgentType classifies the role of an agent.
type AgentType string

const (
	AgentTypeWorker     AgentType = "worker"
	AgentTypeResearcher AgentType = "researcher"
	AgentTypeReviewer   AgentType = "reviewer"
	AgentTypeMonitor    AgentType = "monitor"
	AgentTypeTemporary  AgentType = "temporary"
)

// AgentStatus tracks the lifecycle of an agent.
type AgentStatus string

const (
	AgentStatusProvisioning AgentStatus = "provisioning"
	AgentStatusRunning      AgentStatus = "running"
	AgentStatusIdle         AgentStatus = "idle"
	AgentStatusBusy         AgentStatus = "busy"
	AgentStatusSuspected    AgentStatus = "suspected"
	AgentStatusTerminating  AgentStatus = "terminating"
	AgentStatusTerminated   AgentStatus = "terminated"
	AgentStatusFailed       AgentStatus = "failed"
)

// AgentDefinition is the blueprint for an agent.
// It declares capabilities, permissions, and constraints.
type AgentDefinition struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Version      string        `json:"version"`
	Description  string        `json:"description"`
	Type         AgentType     `json:"type"`
	BusinessID   string        `json:"business_id"`
	Capabilities []string      `json:"capabilities"`
	Permissions  []string      `json:"permissions"`
	ToolIDs      []string      `json:"tool_ids,omitempty"`
	MaxTaskTime  time.Duration `json:"max_task_time"`
	MaxRetries   int           `json:"max_retries"`
}

// Agent represents a running agent instance.
// An agent executes bounded tasks within its authority and scope.
type Agent struct {
	ID            string           `json:"id"`
	Definition    *AgentDefinition `json:"definition"`
	Status        AgentStatus      `json:"status"`
	CurrentTask   string           `json:"current_task,omitempty"`
	WorkflowID    string           `json:"workflow_id,omitempty"`
	LastHeartbeat *time.Time       `json:"last_heartbeat,omitempty"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
	TerminatedAt  *time.Time       `json:"terminated_at,omitempty"`
}

// TaskExecution represents the execution context for a task.
type TaskExecution struct {
	TaskID      string            `json:"task_id"`
	WorkflowID  string            `json:"workflow_id"`
	ObjectiveID string            `json:"objective_id,omitempty"`
	AgentID     string            `json:"agent_id"`
	Input       map[string]string `json:"input,omitempty"`
	Constraints []string          `json:"constraints,omitempty"`
}

// TaskOutcome represents the result of a task execution.
type TaskOutcome struct {
	TaskID   string   `json:"task_id"`
	AgentID  string   `json:"agent_id"`
	Status   string   `json:"status"` // "completed", "failed"
	Output   string   `json:"output,omitempty"`
	Evidence []string `json:"evidence,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// AgentRuntime manages agent lifecycle and task execution.
type AgentRuntime struct {
	agents map[string]*Agent
	now    func() time.Time
}

// NewAgentRuntime creates a new agent runtime.
func NewAgentRuntime() *AgentRuntime {
	return &AgentRuntime{
		agents: make(map[string]*Agent),
		now:    time.Now,
	}
}

// NewAgentRuntimeWithClock creates a new agent runtime with an injectable clock.
func NewAgentRuntimeWithClock(now func() time.Time) *AgentRuntime {
	return &AgentRuntime{
		agents: make(map[string]*Agent),
		now:    now,
	}
}

// ProvisionAgent creates a new agent from a definition.
func (ar *AgentRuntime) ProvisionAgent(def *AgentDefinition) (*Agent, error) {
	if def.ID == "" {
		return nil, fmt.Errorf("agent definition ID is required")
	}
	if def.BusinessID == "" {
		return nil, fmt.Errorf("business ID is required for agent isolation")
	}

	now := ar.now()
	agent := &Agent{
		ID:         fmt.Sprintf("agent-%d", now.UnixNano()),
		Definition: def,
		Status:     AgentStatusProvisioning,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	ar.agents[agent.ID] = agent
	return agent, nil
}

// StartAgent transitions an agent from provisioning to running.
func (ar *AgentRuntime) StartAgent(agentID string) error {
	agent, ok := ar.agents[agentID]
	if !ok {
		return fmt.Errorf("agent %s not found", agentID)
	}

	if agent.Status != AgentStatusProvisioning {
		return fmt.Errorf("can only start provisioning agents (current: %s)", agent.Status)
	}

	agent.Status = AgentStatusRunning
	agent.UpdatedAt = ar.now()
	return nil
}

// AssignTask assigns a task to an agent.
func (ar *AgentRuntime) AssignTask(agentID, taskID, workflowID string) error {
	agent, ok := ar.agents[agentID]
	if !ok {
		return fmt.Errorf("agent %s not found", agentID)
	}

	if agent.Status != AgentStatusRunning && agent.Status != AgentStatusIdle {
		return fmt.Errorf("can only assign tasks to running or idle agents (current: %s)", agent.Status)
	}

	agent.CurrentTask = taskID
	agent.WorkflowID = workflowID
	agent.Status = AgentStatusBusy
	agent.UpdatedAt = ar.now()
	return nil
}

// CompleteTask marks the agent's current task as completed.
func (ar *AgentRuntime) CompleteTask(agentID string, outcome *TaskOutcome) error {
	agent, ok := ar.agents[agentID]
	if !ok {
		return fmt.Errorf("agent %s not found", agentID)
	}

	if agent.Status != AgentStatusBusy {
		return fmt.Errorf("can only complete tasks on busy agents (current: %s)", agent.Status)
	}

	agent.CurrentTask = ""
	agent.Status = AgentStatusIdle
	agent.UpdatedAt = ar.now()
	return nil
}

// Heartbeat records a heartbeat from an agent.
func (ar *AgentRuntime) Heartbeat(agentID string) error {
	agent, ok := ar.agents[agentID]
	if !ok {
		return fmt.Errorf("agent %s not found", agentID)
	}

	now := ar.now()
	agent.LastHeartbeat = &now
	agent.UpdatedAt = now

	if agent.Status == AgentStatusSuspected {
		agent.Status = AgentStatusRunning
	}

	return nil
}

// TerminateAgent gracefully terminates an agent.
func (ar *AgentRuntime) TerminateAgent(agentID string) error {
	agent, ok := ar.agents[agentID]
	if !ok {
		return fmt.Errorf("agent %s not found", agentID)
	}

	now := ar.now()
	agent.Status = AgentStatusTerminating
	agent.UpdatedAt = now

	// In a real implementation, this would wait for task completion
	agent.Status = AgentStatusTerminated
	agent.TerminatedAt = &now
	return nil
}

// GetAgent returns an agent by ID.
func (ar *AgentRuntime) GetAgent(agentID string) (*Agent, bool) {
	agent, ok := ar.agents[agentID]
	return agent, ok
}

// AgentCount returns the total number of agents.
func (ar *AgentRuntime) AgentCount() int {
	return len(ar.agents)
}
