// Package control implements the Agent Governance & Control Integration v1
// admission boundary (contracts/AGENT_GOVERNANCE_CONTROL_CONTRACTS.md).
//
// It is a CLIENT of the existing governance engine, never a second evaluator:
// every decision comes from governance.Engine.Evaluate, every approval record
// from governance.ApprovalEngine, and every escalation from the existing
// core escalation → attention path. This package only
//
//   - builds the immutable, runtime-established ActionProposal,
//   - asks the engine for a decision,
//   - turns an allowing decision into enforceable runtime constraints,
//   - and reports what the caller must do next (execute / wait / escalate).
//
// The model may propose what it wants to do. Only governance admits it.
package control

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/governance"
)

// Proposal is one agent action proposed for admission. Every field is
// runtime-established: the model's action payload can only supply Action,
// Resource, ResourceType, ToolID and Operation (contract §2).
type Proposal struct {
	ProposalID    string `json:"proposal_id"`
	CorrelationID string `json:"correlation_id"`
	ExecutionID   string `json:"execution_id,omitempty"`
	ObjectiveID   string `json:"objective_id,omitempty"`
	StepID        string `json:"step_id,omitempty"`

	ActorID    string `json:"actor_id"`
	AgentID    string `json:"agent_id,omitempty"`
	BusinessID string `json:"business_id"`
	DivisionID string `json:"division_id,omitempty"`

	Action       string `json:"action"`
	Resource     string `json:"resource"`
	ResourceType string `json:"resource_type"`

	ToolID    string `json:"tool_id,omitempty"`
	Operation string `json:"operation,omitempty"`

	// Constraints are runtime restrictions already attached to the work
	// (executor.WorkRequest.Constraints), merged with what governance returns.
	Constraints []governance.Constraint `json:"constraints,omitempty"`

	RiskLevel governance.RiskLevel `json:"risk_level,omitempty"`
	// Context is bounded evaluation context (ids and small facts). It never
	// carries prompts, tool output or credentials (contract §10).
	Context   map[string]string `json:"context,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// Validate enforces the runtime-established invariants before governance is
// consulted. A proposal that cannot name its authority is not admissible at all.
func (p Proposal) Validate() error {
	switch {
	case strings.TrimSpace(p.ActorID) == "":
		return fmt.Errorf("%w: proposal has no actor", ErrInvalidProposal)
	case strings.TrimSpace(p.BusinessID) == "":
		return fmt.Errorf("%w: proposal has no business", ErrInvalidProposal)
	case strings.TrimSpace(p.Action) == "":
		return fmt.Errorf("%w: proposal has no action", ErrInvalidProposal)
	case strings.TrimSpace(p.ResourceType) == "":
		return fmt.Errorf("%w: proposal has no resource type", ErrInvalidProposal)
	}
	return nil
}

// GovernanceRequest renders the proposal as the existing engine's request. The
// engine's own vocabulary is reused as-is: nothing here invents a new policy
// language, and the approval state is the single mechanism the engine already
// understands.
func (p Proposal) GovernanceRequest(approval *governance.ApprovalState) governance.Request {
	ctx := map[string]string{"proposal_id": p.ProposalID}
	for k, v := range p.Context {
		if k == "proposal_id" {
			continue // the runtime's own id always wins
		}
		ctx[k] = v
	}
	risk := p.RiskLevel
	if risk == "" {
		risk = governance.RiskLevelLow
	}
	return governance.Request{
		Actor:         p.ActorID,
		Action:        p.Action,
		Resource:      p.Resource,
		ResourceType:  p.ResourceType,
		BusinessID:    p.BusinessID,
		DivisionID:    p.DivisionID,
		AgentID:       p.AgentID,
		ObjectiveID:   p.ObjectiveID,
		TaskID:        p.ExecutionID,
		RiskLevel:     risk,
		Context:       ctx,
		Timestamp:     p.CreatedAt,
		ApprovalState: approval,
	}
}

// Fingerprint binds an approval to exactly this proposal. Every authority-bearing
// field participates, so a changed action, resource, scope, agent or constraint
// set produces a different fingerprint and needs fresh approval (contract §5).
func (p Proposal) Fingerprint() string {
	parts := []string{
		p.CorrelationID, p.ExecutionID, p.ObjectiveID, p.ActorID, p.AgentID,
		p.BusinessID, p.DivisionID, p.Action, p.Resource, p.ResourceType,
		p.ToolID, p.Operation,
	}
	parts = append(parts, constraintKey(p.Constraints)...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func constraintKey(cs []governance.Constraint) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.ConstraintType+"="+c.ConstraintID+":"+c.Expression)
	}
	sort.Strings(out)
	return out
}

// RiskFor maps a capability side-effect class to the risk level the engine
// evaluates with. It is derived from the manifest the runtime already loaded —
// never from a model-supplied value.
func RiskFor(sideEffectClass string) governance.RiskLevel {
	switch sideEffectClass {
	case "external_mutation", "credentialed_external_mutation":
		return governance.RiskLevelHigh
	case "write":
		return governance.RiskLevelMedium
	default:
		return governance.RiskLevelLow
	}
}

// Action/resource vocabulary derived from the closed agent action set
// (contract §2). These are constants, never request fields.
const (
	ActionToolCall     = "tool_call"
	ActionDelegate     = "delegate"
	ActionMemoryWrite  = "memory_write"
	ActionMemoryDelete = "memory_delete"
	ActionModelCall    = "model_call"

	// Resource types reuse the EXISTING policy vocabulary
	// (SCHEMA_GOVERNANCE_ATTENTION §2.4 resource_type), so an operator pins a
	// capability with the same words the policy schema already accepts.
	ResourceTypeCapability = "tool"
	ResourceTypeMemory     = "memory"
	ResourceTypeAgent      = "agent"
)

// MemoryResource renders the resource id of a memory action.
func MemoryResource(scope, key string) string {
	return "memory:" + scope + "/" + key
}
