package agentintel

import (
	"fmt"
	"strings"
	"time"
)

// Step is one unit of a plan (§3). A step is a *declaration*; what actually
// runs is an action validated by the loop.
type Step struct {
	StepID          string   `json:"step_id"`
	Intent          string   `json:"intent"`
	RequiredCaps    []string `json:"required_capabilities,omitempty"`
	Dependencies    []string `json:"dependencies,omitempty"`
	PreferredAgent  string   `json:"preferred_agent,omitempty"`
	AllowedTools    []string `json:"allowed_tools,omitempty"`
	SuccessCriteria []string `json:"success_criteria,omitempty"`
	Optional        bool     `json:"optional,omitempty"`
}

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
	if len(p.Steps) > caps.MaxSteps {
		reject("", fmt.Sprintf("plan exceeds max_steps (%d)", caps.MaxSteps))
		return res
	}
	ids := map[string]struct{}{}
	for _, s := range p.Steps {
		if strings.TrimSpace(s.StepID) == "" {
			reject("", "step has no step_id")
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
		if s.PreferredAgent != "" && r != nil && !r.AgentVisible(s.PreferredAgent, p.BusinessID, p.DivisionID) {
			reject(s.StepID, fmt.Sprintf("preferred_agent %q is not visible in this scope", s.PreferredAgent))
		}
		for _, t := range s.AllowedTools {
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
	}
	for _, s := range p.Steps {
		for _, d := range s.Dependencies {
			if _, ok := ids[d]; !ok {
				reject(s.StepID, fmt.Sprintf("depends on unknown step %q", d))
			}
			if d == s.StepID {
				reject(s.StepID, "step depends on itself")
			}
		}
	}
	if hasCycle(p) {
		reject("", "plan dependency graph contains a cycle")
	}
	return res
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
