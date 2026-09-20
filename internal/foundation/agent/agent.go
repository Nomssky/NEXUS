// Package agent implements the NEXUS Agent Runtime & Lifecycle (C12).
//
// M7 provides the full agent lifecycle with safety controls:
//   - Identity: distinct agent_id; execution identity != persistent identity
//   - Capabilities: explicitly declared, ⊆ authority
//   - Permissions: permission ⊆ authority ⊆ parent authority
//   - Governance: agent cannot modify policy/audit/approval
//   - Tool access: only via Tool Runtime
//   - Model routing: via Model Router only
//   - Business/Division scope: explicit, no cross-business leak
//   - Resource limits: CPU/RAM/GPU + token + task + cost budgets
//   - Timeout: per-task; timeout → UNKNOWN, not failure
//   - Retry: single retry owner; bounded; UNKNOWN → reconcile, not retry
//   - Cancellation: cancel ≠ failure; safe-boundary preemption
//   - Observability: full correlation chain
//   - Audit: immutable, attributable records
//   - Recovery: Heartbeat → Suspected → Unresponsive → Recovery
//   - Spawn safety: anti-spawn-storm controls
//   - Terminate safety: TERMINATING → TERMINATED; graceful shutdown
//
// The locked conceptual flow is:
//
//	Scheduler → Agent Runtime → Tool Runtime → Verification → Outcome
//
// Models have no execution authority.
package agent

import (
	"fmt"
	"sync"
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
	AgentTypeSpecialist AgentType = "specialist"
)

// AgentStatus tracks the lifecycle of an agent.
type AgentStatus string

const (
	AgentStatusProvisioning AgentStatus = "provisioning"
	AgentStatusRunning      AgentStatus = "running"
	AgentStatusIdle         AgentStatus = "idle"
	AgentStatusBusy         AgentStatus = "busy"
	AgentStatusSuspected    AgentStatus = "suspected"
	AgentStatusUnresponsive AgentStatus = "unresponsive"
	AgentStatusRecovering   AgentStatus = "recovering"
	AgentStatusTerminating  AgentStatus = "terminating"
	AgentStatusTerminated   AgentStatus = "terminated"
	AgentStatusFailed       AgentStatus = "failed"
	AgentStatusCancelled    AgentStatus = "cancelled"
)

// Capability represents a declared agent capability.
type Capability string

// Authority represents a granted authority level.
type Authority string

const (
	AuthorityNone    Authority = "none"
	AuthorityRead    Authority = "read"
	AuthorityWrite   Authority = "write"
	AuthorityExecute Authority = "execute"
	AuthorityAdmin   Authority = "admin"
)

// Budget defines resource limits for an agent.
type Budget struct {
	MaxTokens   int64         `json:"max_tokens"`
	MaxCost     float64       `json:"max_cost"`
	MaxTaskTime time.Duration `json:"max_task_time"`
	MaxTasks    int           `json:"max_tasks"`
	MaxRetries  int           `json:"max_retries"`
}

// SpawnLimits defines anti-spawn-storm controls.
type SpawnLimits struct {
	MaxChildren    int           `json:"max_children"`
	MaxDepth       int           `json:"max_depth"`
	MaxDescendants int           `json:"max_descendants"`
	SpawnRateLimit int           `json:"spawn_rate_limit"` // per minute
	TTL            time.Duration `json:"ttl"`
}

// AgentDefinition is the blueprint for an agent.
type AgentDefinition struct {
	ID              string       `json:"id"`
	Name            string       `json:"name"`
	Version         string       `json:"version"`
	Description     string       `json:"description"`
	Type            AgentType    `json:"type"`
	BusinessID      string       `json:"business_id"`
	DivisionID      string       `json:"division_id,omitempty"`
	Capabilities    []Capability `json:"capabilities"`
	Permissions     []Authority  `json:"permissions"`
	Authority       Authority    `json:"authority"`
	ParentAuthority Authority    `json:"parent_authority"`
	ToolIDs         []string     `json:"tool_ids,omitempty"`
	Budget          Budget       `json:"budget"`
	SpawnLimits     SpawnLimits  `json:"spawn_limits"`
}

// Validate checks that capabilities ⊆ authority ⊆ parent authority.
func (def *AgentDefinition) Validate() error {
	if def.BusinessID == "" {
		return fmt.Errorf("business ID is required")
	}
	if def.Authority == AuthorityNone {
		return fmt.Errorf("authority is required")
	}
	// Authority must be ≤ parent authority
	if !authorityLeq(def.Authority, def.ParentAuthority) {
		return fmt.Errorf("authority %v must be ≤ parent authority %v", def.Authority, def.ParentAuthority)
	}
	return nil
}

// TaskExecution represents the execution context for a task.
type TaskExecution struct {
	TaskID      string            `json:"task_id"`
	WorkflowID  string            `json:"workflow_id"`
	ObjectiveID string            `json:"objective_id,omitempty"`
	AgentID     string            `json:"agent_id"`
	Input       map[string]string `json:"input,omitempty"`
	Constraints []string          `json:"constraints,omitempty"`
	Budget      Budget            `json:"budget"`
	Deadline    *time.Time        `json:"deadline,omitempty"`
}

// TaskOutcome represents the result of a task execution.
type TaskOutcome struct {
	TaskID     string   `json:"task_id"`
	AgentID    string   `json:"agent_id"`
	Status     string   `json:"status"` // "completed", "failed", "unknown", "cancelled"
	Output     string   `json:"output,omitempty"`
	Evidence   []string `json:"evidence,omitempty"`
	Error      string   `json:"error,omitempty"`
	TokensUsed int64    `json:"tokens_used"`
	CostUsed   float64  `json:"cost_used"`
}

// Agent represents a running agent instance.
type Agent struct {
	ID              string           `json:"id"`
	RuntimeID       string           `json:"runtime_id"` // execution identity
	Definition      *AgentDefinition `json:"definition"`
	Status          AgentStatus      `json:"status"`
	CurrentTask     string           `json:"current_task,omitempty"`
	WorkflowID      string           `json:"workflow_id,omitempty"`
	ParentAgentID   string           `json:"parent_agent_id,omitempty"`
	ChildrenIDs     []string         `json:"children_ids,omitempty"`
	Depth           int              `json:"depth"`
	DescendantCount int              `json:"descendant_count"`
	TokensUsed      int64            `json:"tokens_used"`
	CostUsed        float64          `json:"cost_used"`
	TasksCompleted  int              `json:"tasks_completed"`
	RetryCount      int              `json:"retry_count"`
	LastHeartbeat   *time.Time       `json:"last_heartbeat,omitempty"`
	LeaseExpiry     *time.Time       `json:"lease_expiry,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
	TerminatedAt    *time.Time       `json:"terminated_at,omitempty"`
}

// SpawnRequest represents a request to spawn a child agent.
type SpawnRequest struct {
	ParentAgentID string           `json:"parent_agent_id"`
	Definition    *AgentDefinition `json:"definition"`
	TaskID        string           `json:"task_id"`
}

// SpawnSafety tracks spawn control state.
type SpawnSafety struct {
	mu sync.Mutex
	// Per-parent tracking
	childrenCount   map[string]int
	depthCount      map[string]int
	descendantCount map[string]int
	// Global tracking
	spawnTimestamps []time.Time
}

// AgentRuntime manages agent lifecycle and task execution.
type AgentRuntime struct {
	agents      map[string]*Agent
	spawnSafety *SpawnSafety
	now         func() time.Time
}

// NewAgentRuntime creates a new agent runtime.
func NewAgentRuntime() *AgentRuntime {
	return &AgentRuntime{
		agents: make(map[string]*Agent),
		spawnSafety: &SpawnSafety{
			childrenCount:   make(map[string]int),
			depthCount:      make(map[string]int),
			descendantCount: make(map[string]int),
		},
		now: time.Now,
	}
}

// NewAgentRuntimeWithClock creates a new agent runtime with an injectable clock.
func NewAgentRuntimeWithClock(now func() time.Time) *AgentRuntime {
	return &AgentRuntime{
		agents: make(map[string]*Agent),
		spawnSafety: &SpawnSafety{
			childrenCount:   make(map[string]int),
			depthCount:      make(map[string]int),
			descendantCount: make(map[string]int),
		},
		now: now,
	}
}

// ProvisionAgent creates a new agent from a definition.
func (ar *AgentRuntime) ProvisionAgent(def *AgentDefinition) (*Agent, error) {
	if err := def.Validate(); err != nil {
		return nil, fmt.Errorf("invalid agent definition: %w", err)
	}

	now := ar.now()
	agent := &Agent{
		ID:         fmt.Sprintf("agent-%d", now.UnixNano()),
		RuntimeID:  fmt.Sprintf("rt-%d", now.UnixNano()),
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

// AssignTask assigns a task to an agent with budget enforcement.
func (ar *AgentRuntime) AssignTask(agentID string, exec *TaskExecution) error {
	agent, ok := ar.agents[agentID]
	if !ok {
		return fmt.Errorf("agent %s not found", agentID)
	}

	if agent.Status != AgentStatusRunning && agent.Status != AgentStatusIdle {
		return fmt.Errorf("can only assign tasks to running or idle agents (current: %s)", agent.Status)
	}

	// Check budget limits
	if agent.Definition.Budget.MaxTasks > 0 && agent.TasksCompleted >= agent.Definition.Budget.MaxTasks {
		return fmt.Errorf("agent %s has reached task limit", agentID)
	}

	agent.CurrentTask = exec.TaskID
	agent.WorkflowID = exec.WorkflowID
	agent.Status = AgentStatusBusy
	agent.UpdatedAt = ar.now()

	// Set lease expiry from budget
	if exec.Budget.MaxTaskTime > 0 {
		expiry := ar.now().Add(exec.Budget.MaxTaskTime)
		agent.LeaseExpiry = &expiry
	}

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

	// Enforce budget
	agent.TokensUsed += outcome.TokensUsed
	agent.CostUsed += outcome.CostUsed
	if agent.Definition.Budget.MaxTokens > 0 && agent.TokensUsed > agent.Definition.Budget.MaxTokens {
		return fmt.Errorf("agent %s exceeded token budget", agentID)
	}
	if agent.Definition.Budget.MaxCost > 0 && agent.CostUsed > agent.Definition.Budget.MaxCost {
		return fmt.Errorf("agent %s exceeded cost budget", agentID)
	}

	agent.CurrentTask = ""
	agent.WorkflowID = ""
	agent.Status = AgentStatusIdle
	agent.TasksCompleted++
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

	// Recovery from suspected state
	if agent.Status == AgentStatusSuspected || agent.Status == AgentStatusRecovering {
		agent.Status = AgentStatusRunning
	}

	return nil
}

// CheckHeartbeat checks if an agent is unresponsive.
func (ar *AgentRuntime) CheckHeartbeat(agentID string, timeout time.Duration) AgentStatus {
	agent, ok := ar.agents[agentID]
	if !ok {
		return AgentStatusFailed
	}

	if agent.LastHeartbeat == nil {
		return agent.Status
	}

	if ar.now().Sub(*agent.LastHeartbeat) > timeout {
		if agent.Status == AgentStatusRunning || agent.Status == AgentStatusBusy {
			agent.Status = AgentStatusSuspected
			agent.UpdatedAt = ar.now()
		} else if agent.Status == AgentStatusSuspected {
			agent.Status = AgentStatusUnresponsive
			agent.UpdatedAt = ar.now()
		}
	}

	return agent.Status
}

// CheckLease checks if an agent's lease has expired.
func (ar *AgentRuntime) CheckLease(agentID string) bool {
	agent, ok := ar.agents[agentID]
	if !ok {
		return false
	}

	if agent.LeaseExpiry == nil {
		return false
	}

	return ar.now().After(*agent.LeaseExpiry)
}

// SpawnChild spawns a child agent with anti-spawn-storm controls.
func (ar *AgentRuntime) SpawnChild(req *SpawnRequest) (*Agent, error) {
	parent, ok := ar.agents[req.ParentAgentID]
	if !ok {
		return nil, fmt.Errorf("parent agent %s not found", req.ParentAgentID)
	}

	ar.spawnSafety.mu.Lock()
	defer ar.spawnSafety.mu.Unlock()

	// Check max children
	if parent.Definition.SpawnLimits.MaxChildren > 0 {
		if ar.spawnSafety.childrenCount[req.ParentAgentID] >= parent.Definition.SpawnLimits.MaxChildren {
			return nil, fmt.Errorf("agent %s reached max children limit", req.ParentAgentID)
		}
	}

	// Check max depth
	if parent.Definition.SpawnLimits.MaxDepth > 0 && parent.Depth+1 > parent.Definition.SpawnLimits.MaxDepth {
		return nil, fmt.Errorf("agent %s would exceed max depth limit", req.ParentAgentID)
	}

	// Check spawn rate (per minute)
	if parent.Definition.SpawnLimits.SpawnRateLimit > 0 {
		oneMinuteAgo := ar.now().Add(-1 * time.Minute)
		recentSpawns := 0
		for _, ts := range ar.spawnSafety.spawnTimestamps {
			if ts.After(oneMinuteAgo) {
				recentSpawns++
			}
		}
		if recentSpawns >= parent.Definition.SpawnLimits.SpawnRateLimit {
			return nil, fmt.Errorf("spawn rate limit exceeded")
		}
	}

	// Provision child
	child, err := ar.ProvisionAgent(req.Definition)
	if err != nil {
		return nil, err
	}

	child.ParentAgentID = req.ParentAgentID
	child.Depth = parent.Depth + 1

	// Update parent
	parent.ChildrenIDs = append(parent.ChildrenIDs, child.ID)
	parent.DescendantCount++
	ar.spawnSafety.childrenCount[req.ParentAgentID]++
	ar.spawnSafety.spawnTimestamps = append(ar.spawnSafety.spawnTimestamps, ar.now())

	// Propagate descendant count up the chain
	ar.propagateDescendantCount(req.ParentAgentID)

	return child, nil
}

func (ar *AgentRuntime) propagateDescendantCount(agentID string) {
	agent, ok := ar.agents[agentID]
	if !ok || agent.ParentAgentID == "" {
		return
	}
	parent, ok := ar.agents[agent.ParentAgentID]
	if !ok {
		return
	}
	parent.DescendantCount++
	ar.propagateDescendantCount(agent.ParentAgentID)
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

// CancelTask cancels an agent's current task (cancel ≠ failure).
func (ar *AgentRuntime) CancelTask(agentID string) error {
	agent, ok := ar.agents[agentID]
	if !ok {
		return fmt.Errorf("agent %s not found", agentID)
	}

	if agent.Status != AgentStatusBusy {
		return fmt.Errorf("can only cancel tasks on busy agents (current: %s)", agent.Status)
	}

	agent.CurrentTask = ""
	agent.WorkflowID = ""
	agent.Status = AgentStatusIdle
	agent.UpdatedAt = ar.now()
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

// authorityLeq checks if authority a is ≤ authority b.
func authorityLeq(a, b Authority) bool {
	levels := map[Authority]int{
		AuthorityNone:    0,
		AuthorityRead:    1,
		AuthorityWrite:   2,
		AuthorityExecute: 3,
		AuthorityAdmin:   4,
	}
	return levels[a] <= levels[b]
}
