package governance

import (
	"fmt"
	"time"
)

// PolicyType classifies what a policy governs.
type PolicyType string

const (
	PolicyTypeAccessControl  PolicyType = "access_control"
	PolicyTypeDataGovernance PolicyType = "data_governance"
	PolicyTypeModelUsage     PolicyType = "model_usage"
	PolicyTypeToolUsage      PolicyType = "tool_usage"
	PolicyTypeApprovalPolicy PolicyType = "approval_workflow"
	PolicyTypeRetention      PolicyType = "retention"
	PolicyTypeSecurity       PolicyType = "security"
	PolicyTypeCompliance     PolicyType = "compliance"
	PolicyTypeCustom         PolicyType = "custom"
)

// PolicyStatus indicates whether a policy is active.
type PolicyStatus string

const (
	PolicyStatusActive   PolicyStatus = "active"
	PolicyStatusDisabled PolicyStatus = "disabled"
	PolicyStatusDraft    PolicyStatus = "draft"
	PolicyStatusArchived PolicyStatus = "archived"
)

// ScopeLevel represents the hierarchical level of a policy scope.
// Precedence: SYSTEM_SAFETY > GLOBAL > BUSINESS > DIVISION > AGENT > WORKFLOW > TASK.
type ScopeLevel int

const (
	ScopeLevelGlobal   ScopeLevel = iota // applies to all businesses
	ScopeLevelBusiness                   // applies to one business
	ScopeLevelDivision                   // applies to one division within a business
	ScopeLevelAgent                      // applies to a specific agent
	ScopeLevelWorkflow                   // applies to a specific workflow
	ScopeLevelTask                       // applies to a specific task
)

// String returns the canonical name of the scope level.
func (s ScopeLevel) String() string {
	switch s {
	case ScopeLevelGlobal:
		return "GLOBAL"
	case ScopeLevelBusiness:
		return "BUSINESS"
	case ScopeLevelDivision:
		return "DIVISION"
	case ScopeLevelAgent:
		return "AGENT"
	case ScopeLevelWorkflow:
		return "WORKFLOW"
	case ScopeLevelTask:
		return "TASK"
	default:
		return fmt.Sprintf("ScopeLevel(%d)", int(s))
	}
}

// Subject identifies who or what a policy applies to.
type Subject struct {
	// SubjectType: "identity", "agent_type", "role", "all"
	SubjectType string `json:"subject_type"`
	// SubjectIDs: specific IDs (empty = all of type)
	SubjectIDs []string `json:"subject_ids,omitempty"`
	// SubjectScope: optional scope restriction
	SubjectScope string `json:"subject_scope,omitempty"`
}

// Action identifies what action a policy governs.
type Action struct {
	// ActionType: "execute_tool", "invoke_model", "access_data",
	// "create_workflow", "approve_action", "escalate", "custom"
	ActionType string `json:"action_type"`
	// ActionIDs: specific action IDs (empty = all of type)
	ActionIDs []string `json:"action_ids,omitempty"`
}

// Resource identifies what resource a policy protects.
type Resource struct {
	// ResourceType: "data", "tool", "model", "workflow", "agent",
	// "memory", "configuration", "all"
	ResourceType string `json:"resource_type"`
	// ResourceIDs: specific resource IDs (empty = all of type)
	ResourceIDs []string `json:"resource_ids,omitempty"`
	// ResourceScope: optional scope restriction
	ResourceScope string `json:"resource_scope,omitempty"`
}

// Condition represents a condition for policy evaluation.
type Condition struct {
	ConditionID string `json:"condition_id"`
	// ConditionType: "time", "scope", "attribute", "count", "composite"
	ConditionType string `json:"condition_type"`
	Expression    string `json:"expression"`
	Negate        bool   `json:"negate,omitempty"`
}

// Constraint represents a constraint when effect is ALLOW_WITH_CONSTRAINTS.
type Constraint struct {
	ConstraintID   string `json:"constraint_id"`
	ConstraintType string `json:"constraint_type"` // e.g. "time_limit", "budget", "scope_restriction"
	Expression     string `json:"expression"`
	Severity       string `json:"severity"` // "advisory", "mandatory"
}

// ApprovalConfig specifies approval requirements when effect is REQUIRE_APPROVAL.
type ApprovalConfig struct {
	// ApproverType: "human", "governance", "delegated"
	ApproverType string `json:"approver_type"`
	// ApproverIDs: specific approvers (empty = any authorized)
	ApproverIDs []string `json:"approver_ids,omitempty"`
	// TimeoutSeconds: approval timeout
	TimeoutSeconds int `json:"timeout_seconds"`
	// AutoDenyOnTimeout: whether timeout results in denial
	AutoDenyOnTimeout bool `json:"auto_deny_on_timeout"`
	// SelfApprovalProhibited: whether the requester can approve their own action
	SelfApprovalProhibited bool `json:"self_approval_prohibited"`
	// DelegationAllowed: whether approvers can delegate
	DelegationAllowed bool `json:"delegation_allowed"`
}

// Policy is a complete governance policy record.
type Policy struct {
	SchemaVersion  string          `json:"schema_version"`
	EntityType     string          `json:"entity_type"` // always "policy"
	PolicyID       string          `json:"policy_id"`
	PolicyVersion  string          `json:"policy_version"`
	NexusID        string          `json:"nexus_id"`
	BusinessID     string          `json:"business_id,omitempty"`
	DivisionID     string          `json:"division_id,omitempty"`
	PolicyType     PolicyType      `json:"policy_type"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Status         PolicyStatus    `json:"status"`
	Subject        Subject         `json:"subject"`
	Action         Action          `json:"action"`
	Resource       Resource        `json:"resource"`
	Conditions     []Condition     `json:"conditions,omitempty"`
	Effect         Outcome         `json:"effect"`
	Constraints    []Constraint    `json:"constraints,omitempty"`
	ApprovalConfig *ApprovalConfig `json:"approval_config,omitempty"`
	// Precedence: higher number = higher precedence
	Precedence        int        `json:"precedence"`
	OverridePolicyIDs []string   `json:"override_policy_ids,omitempty"`
	EffectiveFrom     time.Time  `json:"effective_from"`
	EffectiveUntil    *time.Time `json:"effective_until,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         *time.Time `json:"updated_at,omitempty"`
	CreatedBy         string     `json:"created_by"`
	ApprovedBy        string     `json:"approved_by,omitempty"`
}

// IsExpired returns true if the policy has passed its effective_until time.
func (p *Policy) IsExpired(now time.Time) bool {
	if p.EffectiveUntil == nil {
		return false
	}
	return now.After(*p.EffectiveUntil)
}

// IsActive returns true if the policy is active and within its effective period.
func (p *Policy) IsActive(now time.Time) bool {
	return p.Status == PolicyStatusActive &&
		!p.IsExpired(now) &&
		now.After(p.EffectiveFrom)
}

// ScopeLevel returns the scope level of this policy based on which IDs are set.
func (p *Policy) ScopeLevelFor() ScopeLevel {
	if p.BusinessID == "" {
		return ScopeLevelGlobal
	}
	if p.DivisionID == "" {
		return ScopeLevelBusiness
	}
	return ScopeLevelDivision
}
