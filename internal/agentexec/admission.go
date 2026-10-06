package agentexec

// Action admission for mediated tool invocations
// (contracts/AGENT_GOVERNANCE_CONTROL_CONTRACTS.md).
//
// This is the ONE enforcement point between a proposed agent action and the
// capability platform. It lives on the single mediated path
// (InvokeToolScoped), so every caller — the intelligence loop, delegated
// children, workflows — is admitted, and no adapter or the platform itself ever
// learns about governance.
//
// Invariant: Platform.Invoke does not happen for a proposal admission refused.

import (
	"errors"
	"fmt"
	"time"

	"github.com/Nomssky/NEXUS/internal/capability"
	"github.com/Nomssky/NEXUS/internal/control"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// AdmissionError is returned when an action is refused BEFORE the capability
// platform. It is distinguishable from every tool failure so callers classify
// governance outcomes without string matching.
type AdmissionError struct {
	// Outcome is the canonical governance outcome that refused the action.
	Outcome governance.Outcome
	// Reason is the governance reason (metadata only).
	Reason string
	// Action, ToolID and Operation identify the refused proposal.
	Action    string
	ToolID    string
	Operation string
	// ApprovalID is set when the action is waiting for approval.
	ApprovalID string
	// EscalationID is set when the action was escalated.
	EscalationID string
	// PolicyID is the matched policy, when governance exposes one.
	PolicyID string
	// Fingerprint binds an approval to this exact proposal.
	Fingerprint string
	// Constraints are the runtime restrictions applied (allow-with-constraints).
	Constraints []control.EffectiveConstraint
	// MaxDuration is the effective (possibly tightened) call duration.
	MaxDuration time.Duration
	// Err is the underlying cause, if any.
	Err error
}

func (e *AdmissionError) Error() string {
	switch e.Outcome {
	case governance.DENY:
		return fmt.Sprintf("governance denied %s on %s: %s", e.Action, e.resourceLabel(), e.Reason)
	case governance.REQUIRE_APPROVAL:
		return fmt.Sprintf("governance requires approval for %s on %s (approval_id=%s)",
			e.Action, e.resourceLabel(), e.ApprovalID)
	case governance.ESCALATE:
		return fmt.Sprintf("governance escalated %s on %s (escalation_id=%s): %s",
			e.Action, e.resourceLabel(), e.EscalationID, e.Reason)
	default:
		return fmt.Sprintf("governance refused %s on %s: %v", e.Action, e.resourceLabel(), e.Err)
	}
}

func (e *AdmissionError) Unwrap() error { return e.Err }

func (e *AdmissionError) resourceLabel() string {
	if e.ToolID != "" {
		if e.Operation != "" {
			return e.ToolID + "." + e.Operation
		}
		return e.ToolID
	}
	return e.Operation
}

// Is reports whether this error carries the given governance outcome, so a
// caller can branch on governance state without parsing text.
func (e *AdmissionError) Is(target error) bool {
	var ae *AdmissionError
	if !errors.As(target, &ae) {
		return false
	}
	return ae.Outcome == e.Outcome
}

// Admit runs the governance admission for one proposed tool call and returns the
// effective, constrained request. A non-nil error means the action must not run.
//
// failClosed reports what the caller must do when admission cannot even be
// evaluated (no controller, no engine): refuse. Governance unavailable is DENY.
func (r *Runtime) Admit(call ToolCallSpec, scope ToolScope, correlationID, agentID string,
	limits Limits) (control.ConstrainedRequest, error) {
	if r.Controller == nil {
		return control.ConstrainedRequest{}, &AdmissionError{
			Outcome: governance.DENY,
			Reason:  "no governance controller configured (fail closed)",
			Action:  control.ActionToolCall, ToolID: call.ToolID, Operation: call.Operation,
			Err: control.ErrGovernanceUnavailable,
		}
	}
	proposal := control.Proposal{
		ProposalID:    r.newProposalID(),
		CorrelationID: correlationID,
		ExecutionID:   correlationID,
		ActorID:       scope.ActorID,
		AgentID:       agentID,
		BusinessID:    scope.BusinessID,
		DivisionID:    scope.DivisionID,
		Action:        control.ActionToolCall,
		Resource:      call.ToolID,
		ResourceType:  control.ResourceTypeCapability,
		ToolID:        call.ToolID,
		Operation:     call.Operation,
		// Risk is derived from the manifest the runtime already validated, never
		// from anything the model said. An unknown tool reports the highest
		// risk, so a missing manifest cannot make an action look harmless.
		RiskLevel: r.riskFor(call.ToolID),
		CreatedAt: r.now(),
	}
	adm, err := r.Controller.Admit(proposal)
	if err != nil {
		// A proposal that cannot be admitted (invalid, unenforceable mandatory
		// constraint, unavailable governance) fails closed.
		return control.ConstrainedRequest{}, &AdmissionError{
			Outcome: governance.DENY, Reason: err.Error(),
			Action: control.ActionToolCall, ToolID: call.ToolID, Operation: call.Operation,
			Err: err,
		}
	}
	if !adm.Allowed() {
		return control.ConstrainedRequest{}, &AdmissionError{
			Outcome: adm.Decision.Outcome, Reason: adm.Decision.Reason,
			Action: control.ActionToolCall, ToolID: call.ToolID, Operation: call.Operation,
			ApprovalID: adm.ApprovalID, EscalationID: adm.EscalationID,
			PolicyID: adm.Decision.MatchedPolicyID, Fingerprint: adm.Fingerprint,
		}
	}
	effective, err := control.Constrain(call.ToolID, call.Operation,
		control.Limits{MaxDuration: limits.MaxDuration}, adm)
	if err != nil {
		return control.ConstrainedRequest{}, &AdmissionError{
			Outcome: governance.DENY, Reason: err.Error(),
			Action: control.ActionToolCall, ToolID: call.ToolID, Operation: call.Operation,
			PolicyID: adm.Decision.MatchedPolicyID, Constraints: adm.Effective, Err: err,
		}
	}
	return effective, nil
}

// riskFor derives the risk level governance evaluates with, from the manifest
// the runtime already loaded. Fail-closed: a tool whose manifest is not
// available is reported as an external mutation, the highest class, so a policy
// that requires approval for consequential work still applies.
func (r *Runtime) riskFor(toolID string) governance.RiskLevel {
	if r.Tools == nil {
		return control.RiskFor(string(tool.SideEffectExternalMutation))
	}
	manifest, ok := r.Tools.Manifest(toolID)
	if !ok {
		return control.RiskFor(string(tool.SideEffectExternalMutation))
	}
	return control.RiskFor(string(manifest.SideEffectClass))
}

// newProposalID mints a runtime proposal identity. It is never model supplied.
func (r *Runtime) newProposalID() string {
	r.mu.Lock()
	r.proposalSeq++
	seq := r.proposalSeq
	r.mu.Unlock()
	return fmt.Sprintf("prop-%d-%d", r.now().UnixNano(), seq)
}

// Limits is the bounded resource shape admission may tighten.
type Limits struct {
	MaxDuration time.Duration
}

// capabilityMaxDuration is the ceiling a constrained call may be given. It is
// the platform's own shipped cap: a constraint can only tighten, never extend.
var capabilityMaxDuration = capability.DefaultCaps().MaxDuration
