package agentintel

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// Step is one unit of a plan (§3). A step is a *declaration*; what actually
// runs is an action validated by the loop.
type Step struct {
	StepID         string   `json:"step_id"`
	Intent         string   `json:"intent"`
	RequiredCaps   []string `json:"required_capabilities,omitempty"`
	Dependencies   []string `json:"dependencies,omitempty"`
	PreferredAgent string   `json:"preferred_agent,omitempty"`
	AllowedTools   []string `json:"allowed_tools,omitempty"`
	// AllowedOperations narrows the step to explicit manifest operations. Each
	// pair must name a registered tool that declares the operation; a plan can
	// never widen what the tool manifest allows.
	AllowedOperations []ToolOperation `json:"allowed_operations,omitempty"`
	SuccessCriteria   []string        `json:"success_criteria,omitempty"`
	Optional          bool            `json:"optional,omitempty"`
}

// ToolOperation is one tool id plus a manifest-declared operation name.
type ToolOperation struct {
	Tool      string `json:"tool"`
	Operation string `json:"operation"`
}

const (
	maxPlanIdentifierLen = 128
	maxPlanIntentLen     = 2000
	maxPlanTools         = 16
	maxPlanOperations    = 32
	maxPlanCaps          = 16
	maxPlanDependencies  = 32
	maxPlanCriteria      = 12
	maxPlanCriterionLen  = 1000
)

// Plan is a validated-once-accepted set of steps (§3).
type Plan struct {
	PlanID        string    `json:"plan_id"`
	Version       int       `json:"version"`
	ParentVersion int       `json:"parent_version,omitempty"`
	Reason        string    `json:"reason,omitempty"`
	Steps         []Step    `json:"steps"`
	ObjectiveID   string    `json:"objective_id,omitempty"`
	CorrelationID string    `json:"correlation_id,omitempty"`
	BusinessID    string    `json:"business_id,omitempty"`
	DivisionID    string    `json:"division_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// Rejection records why a plan step was rejected (§5). Rejections are
// fail-closed: a plan with any rejection never executes.
type Rejection struct {
	StepID string `json:"step_id,omitempty"`
	Reason string `json:"reason"`
}

// ValidationResult is the validator verdict for a plan.
type ValidationResult struct {
	Valid      bool        `json:"valid"`
	Rejections []Rejection `json:"rejections,omitempty"`
}

// ScopeResolver exposes the visibility rules the validator needs without
// letting the validator own authorization: agents, tools and scope all come
// from the Agent Execution Layer.
type ScopeResolver interface {
	// AgentVisible reports whether an agent id exists and is visible in the
	// given scope (business/division), per Agent Execution G3 rules.
	AgentVisible(agentID, businessID, divisionID string) bool
	// ToolRegistered reports whether a tool id exists in the tool registry.
	ToolRegistered(toolID string) bool
	// AgentAllowsTool reports whether the agent's allowlist covers toolID.
	AgentAllowsTool(agentID, toolID string) bool
}

// ValidatePlan applies the §5 table. It is pure: no side effects, no model
// input. It is called before any step executes, on every plan version,
// including replans.
func ValidatePlan(p *Plan, r ScopeResolver, caps Caps) ValidationResult {
	res := ValidationResult{Valid: true}
	reject := func(stepID, reason string) {
		res.Valid = false
		res.Rejections = append(res.Rejections, Rejection{StepID: stepID, Reason: reason})
	}
	if p == nil || len(p.Steps) == 0 {
		reject("", "plan has no steps")
		return res
	}
	if caps.MaxSteps <= 0 || len(p.Steps) > caps.MaxSteps {
		reject("", fmt.Sprintf("plan exceeds max_steps (%d)", caps.MaxSteps))
		return res
	}
	ids := map[string]struct{}{}
	for _, s := range p.Steps {
		if !validPlanIdentifier(s.StepID) {
			reject("", "step has an invalid step_id")
			continue
		}
		if _, dup := ids[s.StepID]; dup {
			reject(s.StepID, "duplicate step_id")
			continue
		}
		ids[s.StepID] = struct{}{}
		if strings.TrimSpace(s.Intent) == "" {
			reject(s.StepID, "step has no intent")
		}
		if len(s.Intent) > maxPlanIntentLen {
			reject(s.StepID, fmt.Sprintf("intent exceeds %d characters", maxPlanIntentLen))
		}
		if len(s.RequiredCaps) > maxPlanCaps {
			reject(s.StepID, fmt.Sprintf("too many required_capabilities (max %d)", maxPlanCaps))
		}
		seenCaps := map[string]struct{}{}
		for _, capability := range s.RequiredCaps {
			if !validPlanIdentifier(capability) {
				reject(s.StepID, "required capability is malformed")
				continue
			}
			if _, dup := seenCaps[capability]; dup {
				reject(s.StepID, fmt.Sprintf("duplicate required capability %q", capability))
			}
			seenCaps[capability] = struct{}{}
		}
		if s.PreferredAgent != "" && !validPlanIdentifier(s.PreferredAgent) {
			reject(s.StepID, "preferred_agent is malformed")
		} else if s.PreferredAgent != "" && r != nil &&
			!r.AgentVisible(s.PreferredAgent, p.BusinessID, p.DivisionID) {
			reject(s.StepID, fmt.Sprintf("preferred_agent %q is not visible in this scope", s.PreferredAgent))
		}
		if len(s.AllowedTools) > maxPlanTools {
			reject(s.StepID, fmt.Sprintf("too many allowed_tools (max %d)", maxPlanTools))
		}
		allowedTools := make(map[string]struct{}, len(s.AllowedTools))
		for _, t := range s.AllowedTools {
			if !validPlanIdentifier(t) {
				reject(s.StepID, "allowed_tools contains a malformed tool id")
				continue
			}
			if _, dup := allowedTools[t]; dup {
				reject(s.StepID, fmt.Sprintf("duplicate allowed tool %q", t))
				continue
			}
			allowedTools[t] = struct{}{}
			if r == nil {
				continue
			}
			if !r.ToolRegistered(t) {
				reject(s.StepID, fmt.Sprintf("tool %q is not registered", t))
			}
			if s.PreferredAgent != "" && !r.AgentAllowsTool(s.PreferredAgent, t) {
				reject(s.StepID, fmt.Sprintf("tool %q is not in agent %q's allowlist", t, s.PreferredAgent))
			}
		}
		if len(s.AllowedOperations) > maxPlanOperations {
			reject(s.StepID, fmt.Sprintf("too many allowed_operations (max %d)", maxPlanOperations))
		}
		seenOps := make(map[string]struct{}, len(s.AllowedOperations))
		for _, op := range s.AllowedOperations {
			if !validPlanIdentifier(op.Tool) || !validPlanIdentifier(op.Operation) {
				reject(s.StepID, "allowed_operations contains a malformed tool or operation")
				continue
			}
			key := op.Tool + "\x00" + op.Operation
			if _, dup := seenOps[key]; dup {
				reject(s.StepID, fmt.Sprintf("duplicate allowed operation %q/%q", op.Tool, op.Operation))
				continue
			}
			seenOps[key] = struct{}{}
			if len(allowedTools) > 0 {
				if _, ok := allowedTools[op.Tool]; !ok {
					reject(s.StepID, fmt.Sprintf("allowed operation tool %q is not in allowed_tools", op.Tool))
				}
			}
			if r == nil {
				reject(s.StepID, "operation validation is unavailable")
				continue
			}
			if !r.ToolRegistered(op.Tool) {
				reject(s.StepID, fmt.Sprintf("tool %q is not registered", op.Tool))
			}
			if s.PreferredAgent != "" && !r.AgentAllowsTool(s.PreferredAgent, op.Tool) {
				reject(s.StepID, fmt.Sprintf("tool %q is not in agent %q's allowlist", op.Tool, s.PreferredAgent))
			}
			ops, ok := r.(OperationResolver)
			if !ok || !ops.ToolSupportsOperation(op.Tool, op.Operation) {
				reject(s.StepID, fmt.Sprintf("tool %q does not support operation %q", op.Tool, op.Operation))
			}
		}
		if len(s.SuccessCriteria) > maxPlanCriteria {
			reject(s.StepID, fmt.Sprintf("too many success_criteria (max %d)", maxPlanCriteria))
		}
		seenCriteria := map[string]struct{}{}
		for _, criterion := range s.SuccessCriteria {
			if strings.TrimSpace(criterion) == "" {
				reject(s.StepID, "success criterion is empty")
				continue
			}
			if len(criterion) > maxPlanCriterionLen {
				reject(s.StepID, fmt.Sprintf("success criterion exceeds %d characters", maxPlanCriterionLen))
				continue
			}
			if _, dup := seenCriteria[criterion]; dup {
				reject(s.StepID, "duplicate success criterion")
			}
			seenCriteria[criterion] = struct{}{}
		}
	}
	for _, s := range p.Steps {
		if len(s.Dependencies) > maxPlanDependencies {
			reject(s.StepID, fmt.Sprintf("too many dependencies (max %d)", maxPlanDependencies))
		}
		seen := map[string]struct{}{}
		for _, d := range s.Dependencies {
			if !validPlanIdentifier(d) {
				reject(s.StepID, "dependency has an invalid step id")
				continue
			}
			if _, ok := ids[d]; !ok {
				reject(s.StepID, fmt.Sprintf("depends on unknown step %q", d))
			}
			if d == s.StepID {
				reject(s.StepID, "step depends on itself")
			}
			if _, dup := seen[d]; dup {
				reject(s.StepID, fmt.Sprintf("duplicate dependency %q", d))
			}
			seen[d] = struct{}{}
		}
	}
	if hasCycle(p) {
		reject("", "plan dependency graph contains a cycle")
	}
	return res
}

// validPlanIdentifier bounds model-authored identifiers before they reach
// ledger keys, durable evidence step ids or event metadata.
func validPlanIdentifier(value string) bool {
	if value == "" || len(value) > maxPlanIdentifierLen {
		return false
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// hasCycle reports whether the dependency graph has a cycle.
func hasCycle(p *Plan) bool {
	deps := map[string][]string{}
	for _, s := range p.Steps {
		deps[s.StepID] = s.Dependencies
	}
	const (
		white = 0
		grey  = 1
		black = 2
	)
	color := map[string]int{}
	var visit func(string) bool
	visit = func(id string) bool {
		switch color[id] {
		case grey:
			return true
		case black:
			return false
		}
		color[id] = grey
		for _, d := range deps[id] {
			if _, ok := deps[d]; !ok {
				continue
			}
			if visit(d) {
				return true
			}
		}
		color[id] = black
		return false
	}
	for _, s := range p.Steps {
		if visit(s.StepID) {
			return true
		}
	}
	return false
}
