package governance

import (
	"fmt"
	"strings"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/schema"
)

// DefaultAllowPolicyID is the built-in policy the runtime seeds at boot.
// It exists so an unconfigured installation allows requests instead of
// failing every one on the engine's default-DENY fall-through (§2.8 step 6).
// The HTTP control surface treats it as read-only (see contracts §9.4).
const DefaultAllowPolicyID = "default-allow"

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

// IsValid reports whether the policy type is one of the §2.2 enum values.
func (t PolicyType) IsValid() bool {
	switch t {
	case PolicyTypeAccessControl, PolicyTypeDataGovernance, PolicyTypeModelUsage,
		PolicyTypeToolUsage, PolicyTypeApprovalPolicy, PolicyTypeRetention,
		PolicyTypeSecurity, PolicyTypeCompliance, PolicyTypeCustom:
		return true
	}
	return false
}

// IsValid reports whether the status is one of the §2.2 enum values.
func (s PolicyStatus) IsValid() bool {
	switch s {
	case PolicyStatusActive, PolicyStatusDisabled, PolicyStatusDraft, PolicyStatusArchived:
		return true
	}
	return false
}

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
	SchemaVersion string `json:"schema_version"`
	EntityType    string `json:"entity_type"` // always "policy"
	PolicyID      string `json:"policy_id"`
	PolicyVersion string `json:"policy_version"`
	NexusID       string `json:"nexus_id"`
	BusinessID    string `json:"business_id,omitempty"`
	DivisionID    string `json:"division_id,omitempty"`
	// D1: narrower scope levels (additive, optional). A policy pinned to an
	// agent/workflow/task applies only when the request carries that id.
	AgentID        string          `json:"agent_id,omitempty"`
	WorkflowID     string          `json:"workflow_id,omitempty"`
	TaskID         string          `json:"task_id,omitempty"`
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
	// Provenance is the §2.2 Universal Required origin record
	// (SCHEMA_COMMON §4). It is evidence of where the policy came from and
	// is never consulted during evaluation.
	Provenance schema.ProvenanceRef `json:"provenance"`
	// Metadata is the §2.2 optional extension map, round-tripped verbatim.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// subjectTypes, actionTypes and resourceTypes are the §2.3 / §2.4 / §2.5
// enums. Anything outside them is rejected at the boundary rather than
// silently never matching (an unknown subject type falls through to a
// non-match in matchesSubject, which would read as a policy that exists,
// is active, and does nothing).
var (
	subjectTypes = map[string]bool{
		"identity": true, "agent_type": true, "role": true, "all": true,
	}
	actionTypes = map[string]bool{
		"execute_tool": true, "invoke_model": true, "access_data": true,
		"create_workflow": true, "approve_action": true, "escalate": true,
		"custom": true,
	}
	resourceTypes = map[string]bool{
		"data": true, "tool": true, "model": true, "workflow": true,
		"agent": true, "memory": true, "configuration": true, "all": true,
	}
	conditionTypes = map[string]bool{
		"time": true, "scope": true, "attribute": true, "count": true,
		"composite": true,
	}
)

// Validate checks the §2.2 required fields and the §2.3–§2.7 enums, plus the
// one conditional requirement §2.2 states: approval_config when the effect is
// REQUIRE_APPROVAL. Server-derivable §2.2 fields (schema_version, nexus_id,
// created_at, created_by, provenance, effective_from) are defaulted by the
// caller before this runs, so they are asserted rather than optional here.
// Evaluation semantics are untouched — this is a boundary check only.
func (p *Policy) Validate() error {
	if p == nil {
		return fmt.Errorf("policy is required")
	}
	if strings.TrimSpace(p.PolicyID) == "" {
		return fmt.Errorf("policy_id is required")
	}
	if p.EntityType != "policy" {
		return fmt.Errorf("entity_type must be %q", "policy")
	}
	if strings.TrimSpace(p.SchemaVersion) == "" {
		return fmt.Errorf("schema_version is required")
	}
	if strings.TrimSpace(p.NexusID) == "" {
		return fmt.Errorf("nexus_id is required")
	}
	if !p.PolicyType.IsValid() {
		return fmt.Errorf("policy_type must be one of access_control, data_governance, model_usage, tool_usage, approval_workflow, retention, security, compliance, custom")
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(p.Description) == "" {
		return fmt.Errorf("description is required")
	}
	if !p.Status.IsValid() {
		return fmt.Errorf("status must be one of active, disabled, draft, archived")
	}
	if !subjectTypes[p.Subject.SubjectType] {
		return fmt.Errorf("subject.subject_type must be one of identity, agent_type, role, all")
	}
	if !actionTypes[p.Action.ActionType] {
		return fmt.Errorf("action.action_type must be one of execute_tool, invoke_model, access_data, create_workflow, approve_action, escalate, custom")
	}
	if !resourceTypes[p.Resource.ResourceType] {
		return fmt.Errorf("resource.resource_type must be one of data, tool, model, workflow, agent, memory, configuration, all")
	}
	if err := ValidateOutcome(p.Effect); err != nil {
		return err
	}
	if p.Effect == REQUIRE_APPROVAL && p.ApprovalConfig == nil {
		return fmt.Errorf("approval_config is required when effect is REQUIRE_APPROVAL")
	}
	for i, c := range p.Conditions {
		if strings.TrimSpace(c.ConditionID) == "" {
			return fmt.Errorf("conditions[%d].condition_id is required", i)
		}
		if !conditionTypes[c.ConditionType] {
			return fmt.Errorf("conditions[%d].condition_type must be one of time, scope, attribute, count, composite", i)
		}
		if strings.TrimSpace(c.Expression) == "" {
			return fmt.Errorf("conditions[%d].expression is required", i)
		}
	}
	if p.EffectiveFrom.IsZero() {
		return fmt.Errorf("effective_from is required")
	}
	if p.CreatedAt.IsZero() {
		return fmt.Errorf("created_at is required")
	}
	if strings.TrimSpace(p.CreatedBy) == "" {
		return fmt.Errorf("created_by is required")
	}
	if !p.Provenance.Valid() {
		return fmt.Errorf("provenance requires origin, producer and produced_at")
	}
	return nil
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

// ScopeLevelFor returns the scope level of this policy: the most specific
// identifier set wins (TASK > WORKFLOW > AGENT > DIVISION > BUSINESS >
// GLOBAL, per the ScopeLevel precedence documented above).
func (p *Policy) ScopeLevelFor() ScopeLevel {
	if p.TaskID != "" {
		return ScopeLevelTask
	}
	if p.WorkflowID != "" {
		return ScopeLevelWorkflow
	}
	if p.AgentID != "" {
		return ScopeLevelAgent
	}
	if p.BusinessID == "" {
		return ScopeLevelGlobal
	}
	if p.DivisionID == "" {
		return ScopeLevelBusiness
	}
	return ScopeLevelDivision
}
