// Package agentintel implements the Agent Intelligence Layer v1: a
// deterministic, bounded, observable control loop that pursues an objective
// through the Agent Execution Layer.
//
// It runs *inside* an already-admitted execution: identity, scope (G3),
// governance, approval, cancellation, tool boundaries, events and the G4
// durability posture are all inherited unchanged. The model proposes actions;
// this runtime validates and performs them. Nothing here admits work, grants
// authority, widens scope, or bypasses the request pipeline.
package agentintel

import (
	"fmt"
	"strings"
	"time"
)

// State is the control-loop state (AGENT_INTELLIGENCE_CONTRACTS §4).
type State string

const (
	StateCreated          State = "created"
	StatePlanning         State = "planning"
	StateExecuting        State = "executing"
	StateObserving        State = "observing"
	StateReplanning       State = "replanning"
	StateCompleted        State = "completed"
	StateFailed           State = "failed"
	StateCancelled        State = "cancelled"
	StateBudgetExhausted  State = "budget_exhausted"
	StateDeadlineExceeded State = "deadline_exceeded"
	// Governance terminal states (AGENT_GOVERNANCE_CONTROL_CONTRACTS §3).
	// They are NOT collapsed into StateFailed: a governance decision is a
	// different axis from an execution outcome.
	StateDenied          State = "denied"
	StatePendingApproval State = "pending_approval"
	StateEscalated       State = "escalated"
)

// Terminal reports whether s is a terminal loop state.
func (s State) Terminal() bool {
	switch s {
	case StateCompleted, StateFailed, StateCancelled, StateBudgetExhausted, StateDeadlineExceeded:
		return true
	}
	return false
}

// Budget is the runtime-enforced limit set (§9). Every field is checked by the
// runtime code path, never by the model, and caller-supplied values are
// clamped to Caps.
type Budget struct {
	MaxIterations    int           `json:"max_iterations,omitempty"`
	MaxModelCalls    int           `json:"max_model_calls,omitempty"`
	MaxToolCalls     int           `json:"max_tool_calls,omitempty"`
	MaxDelegations   int           `json:"max_delegations,omitempty"`
	MaxReplans       int           `json:"max_replans,omitempty"`
	MaxSteps         int           `json:"max_steps,omitempty"`
	MaxDepth         int           `json:"max_depth,omitempty"`
	MaxExecutionTime time.Duration `json:"max_execution_time,omitempty"`
	MaxParallelNodes int           `json:"max_parallel_nodes,omitempty"`
}

// Caps are the server-side hard limits; a request can lower any budget but
// never raise one (§9).
type Caps struct {
	MaxIterations    int
	MaxModelCalls    int
	MaxToolCalls     int
	MaxDelegations   int
	MaxReplans       int
	MaxSteps         int
	MaxDepth         int
	MaxExecutionTime time.Duration
	MaxParallelNodes int
	// MaxConsecutiveNoProgress terminates a loop that keeps answering
	// "continue" without producing an observation (§4).
	MaxConsecutiveNoProgress int
}

// DefaultCaps returns the shipped bounded defaults of §9.
func DefaultCaps() Caps {
	return Caps{
		MaxIterations: 8, MaxModelCalls: 16, MaxToolCalls: 8, MaxDelegations: 3,
		MaxReplans: 2, MaxSteps: 12, MaxDepth: 2,
		MaxExecutionTime: 60 * time.Second, MaxParallelNodes: 4,
		MaxConsecutiveNoProgress: 2,
	}
}

// Max returns the full budget implied by the caps — the widest autonomy the
// runtime can ever grant (used when no caller budget is supplied).
func (c Caps) Max() Budget {
	return Budget{
		MaxIterations: c.MaxIterations, MaxModelCalls: c.MaxModelCalls,
		MaxToolCalls: c.MaxToolCalls, MaxDelegations: c.MaxDelegations,
		MaxReplans: c.MaxReplans, MaxSteps: c.MaxSteps, MaxDepth: c.MaxDepth,
		MaxExecutionTime: c.MaxExecutionTime, MaxParallelNodes: c.MaxParallelNodes,
	}
}

// Clamp returns b bounded by caps, filling unset fields from caps. A caller
// can only ever tighten autonomy.
func (b Budget) Clamp(c Caps) Budget {
	out := Budget{
		MaxIterations:    pick(b.MaxIterations, c.MaxIterations),
		MaxModelCalls:    pick(b.MaxModelCalls, c.MaxModelCalls),
		MaxToolCalls:     pick(b.MaxToolCalls, c.MaxToolCalls),
		MaxDelegations:   pick(b.MaxDelegations, c.MaxDelegations),
		MaxReplans:       pick(b.MaxReplans, c.MaxReplans),
		MaxSteps:         pick(b.MaxSteps, c.MaxSteps),
		MaxDepth:         pick(b.MaxDepth, c.MaxDepth),
		MaxParallelNodes: pick(b.MaxParallelNodes, c.MaxParallelNodes),
		MaxExecutionTime: b.MaxExecutionTime,
	}
	if out.MaxExecutionTime <= 0 || out.MaxExecutionTime > c.MaxExecutionTime {
		out.MaxExecutionTime = c.MaxExecutionTime
	}
	return out
}

func pick(v, def int) int {
	if v <= 0 {
		return def
	}
	if v > def {
		return def // callers may only tighten autonomy (§9)
	}
	return v
}

// Objective is the desired outcome of an intelligent execution (§2). It carries
// no authority: business/division/actor come from the parent execution and are
// stamped by the runtime, never by the caller.
type Objective struct {
	ObjectiveID     string            `json:"objective_id,omitempty"`
	RequestID       string            `json:"request_id,omitempty"`
	Description     string            `json:"description"`
	SuccessCriteria []string          `json:"success_criteria,omitempty"`
	Constraints     []string          `json:"constraints,omitempty"`
	Budget          Budget            `json:"budget,omitempty"`
	Context         map[string]string `json:"context,omitempty"`
}

// Validate checks the objective payload only. Scope-bearing fields are ignored
// if a caller sends them: they are not part of Objective and cannot widen scope.
func (o *Objective) Validate() error {
	if strings.TrimSpace(o.Description) == "" {
		return fmt.Errorf("objective description is required")
	}
	if len(o.Description) > 4000 {
		return fmt.Errorf("objective description exceeds 4000 characters")
	}
	return nil
}
