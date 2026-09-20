package governance

import (
	"time"
)

// Request represents a governance evaluation request.
// It captures everything the policy engine needs to produce a decision.
type Request struct {
	// Actor identifies who is requesting the action.
	Actor string `json:"actor"`
	// Action identifies what action is being requested.
	Action string `json:"action"`
	// Resource identifies what resource is being accessed.
	Resource string `json:"resource"`
	// ResourceType classifies the resource.
	ResourceType string `json:"resource_type"`
	// BusinessID scopes the request to a business (empty = global).
	BusinessID string `json:"business_id,omitempty"`
	// DivisionID scopes the request to a division (empty = business-level).
	DivisionID string `json:"division_id,omitempty"`
	// ObjectiveID links the request to an objective (optional).
	ObjectiveID string `json:"objective_id,omitempty"`
	// RiskLevel indicates the assessed risk of the action.
	RiskLevel RiskLevel `json:"risk_level"`
	// Context carries additional evaluation context.
	Context map[string]string `json:"context,omitempty"`
	// Timestamp is when the request was made.
	Timestamp time.Time `json:"timestamp"`
	// ApprovalState captures any existing approval for this request.
	ApprovalState *ApprovalState `json:"approval_state,omitempty"`
}

// RiskLevel classifies the risk of an action.
type RiskLevel string

const (
	RiskLevelLow      RiskLevel = "LOW"
	RiskLevelMedium   RiskLevel = "MEDIUM"
	RiskLevelHigh     RiskLevel = "HIGH"
	RiskLevelCritical RiskLevel = "CRITICAL"
)

// ApprovalState represents the current state of an approval for a request.
type ApprovalState string

const (
	ApprovalStateNone     ApprovalState = "none"
	ApprovalStatePending  ApprovalState = "pending"
	ApprovalStateApproved ApprovalState = "approved"
	ApprovalStateDenied   ApprovalState = "denied"
)

// Decision is the structured output of a policy evaluation.
type Decision struct {
	// Outcome is one of the 5 canonical governance outcomes.
	Outcome Outcome `json:"outcome"`
	// Reason explains why this outcome was produced.
	Reason string `json:"reason"`
	// MatchedPolicy is the policy that produced this outcome.
	MatchedPolicyID string `json:"matched_policy_id,omitempty"`
	// MatchedPolicyVersion is the version of the matched policy.
	MatchedPolicyVersion string `json:"matched_policy_version,omitempty"`
	// Constraints are the constraints when outcome is ALLOW_WITH_CONSTRAINTS.
	Constraints []Constraint `json:"constraints,omitempty"`
	// ApprovalRequired is set when outcome is REQUIRE_APPROVAL.
	ApprovalRequired *ApprovalConfig `json:"approval_required,omitempty"`
	// ScopeLevel indicates the scope level of the matched policy.
	ScopeLevel ScopeLevel `json:"scope_level"`
	// Timestamp is when the decision was made.
	Timestamp time.Time `json:"timestamp"`
}

// IsAllowing returns true if the decision permits the action to proceed.
func (d *Decision) IsAllowing() bool {
	return d.Outcome.IsAllowing()
}

// RequiresApproval returns true if the decision requires approval.
func (d *Decision) RequiresApproval() bool {
	return d.Outcome == REQUIRE_APPROVAL
}

// IsDeny returns true if the decision denies the action.
func (d *Decision) IsDeny() bool {
	return d.Outcome == DENY
}

// IsEscalation returns true if the decision escalates.
func (d *Decision) IsEscalation() bool {
	return d.Outcome == ESCALATE
}
