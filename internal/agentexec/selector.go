package agentexec

import (
	"fmt"
	"sort"
	"strings"
)

// Selection executes the deterministic, explainable selection algorithm:
// determinism over eligibility, scope and capability matching first, explicit
// agent preference last. No LLM picks the pickers.
type Selection struct {
	Agent *Definition `json:"agent,omitempty"`
	// Reason is the human/agent-level cause.
	Reason string `json:"reason"`
	// MatchedCapabilities reports required caps the selected agent carries.
	MatchedCapabilities []string `json:"matched_capabilities,omitempty"`
	// OptionalMatchedCapabilities matched optional caps.
	OptionalMatchedCapabilities []string `json:"optional_matched_capabilities,omitempty"`
	// RejectedCandidates: every inspected eligible-skip candidate with why.
	RejectedCandidates []RejectedCandidate `json:"rejected_candidates,omitempty"`
}

// RejectedCandidate explains one candidate rejection.
type RejectedCandidate struct {
	AgentID string `json:"agent_id"`
	Reason  string `json:"reason"`
}

// SelectionRequest is the input to Select.
type SelectionRequest struct {
	BusinessID      string   `json:"business_id"`
	DivisionID      string   `json:"division_id,omitempty"` // empty = business scope
	Required        []string `json:"required_capabilities"`
	Optional        []string `json:"optional_capabilities,omitempty"`
	AgentID         string   `json:"agent_id,omitempty"` // explicit preference
	RequiredToolIDs []string `json:"required_tool_ids,omitempty"`
}

// Select implements the deterministic selection algorithm.
//
// Eligibility gates, in order: lifecycle active → scope (business; division
// confined by the narrower of the agent's division and the request's) →
// required capabilities ⊆ agent capabilities → required tools ⊆ the
// registry (checked here by callers that care; governed by allowlist at
// invocation regardless). Ordering: required-cap match count (all required
// must match, so 0 among them), then optional-cap match count, then agent
// id (stable). The numbers are surfaced in RejectedCandidates for
// explainability.
func Select(reg *Registry, req *SelectionRequest, hasTool func(string) bool) *Selection {
	if reg == nil || req == nil {
		return &Selection{Reason: "no registry or empty request"}
	}
	sel := &Selection{}
	candidates := reg.List(req.BusinessID, "")
	if req.AgentID != "" {
		for i := range candidates {
			if candidates[i].ID == req.AgentID {
				c := candidates[i]
				if selEligible(c, req, hasTool) {
					sel.Agent = &c
					sel.Reason = fmt.Sprintf("explicit agent preference %q is eligible", c.ID)
					sel.MatchedCapabilities = intersect(req.Required, c.Capabilities)
					sel.OptionalMatchedCapabilities = intersect(req.Optional, c.Capabilities)
					return sel
				}
				sel.Reason = fmt.Sprintf("explicit agent %q is ineligible", c.ID)
				return sel
			}
		}
		return &Selection{Reason: fmt.Sprintf("explicit agent %q not found in business %q", req.AgentID, req.BusinessID)}
	}
	type scored struct {
		d   Definition
		opt int
	}
	var pool []scored
	for _, c := range candidates {
		switch {
		case c.Status != AgentActive:
			sel.RejectedCandidates = append(sel.RejectedCandidates, RejectedCandidate{AgentID: c.ID, Reason: "lifecycle status " + string(c.Status)})
		case !divisionEligible(c, req):
			sel.RejectedCandidates = append(sel.RejectedCandidates, RejectedCandidate{AgentID: c.ID, Reason: "division scope mismatch"})
		case !covers(req.Required, c.Capabilities):
			sel.RejectedCandidates = append(sel.RejectedCandidates, RejectedCandidate{AgentID: c.ID, Reason: "missing required capabilities: " + strings.Join(missing(req.Required, c.Capabilities), ",")})
		case !coversAllTools(req.RequiredToolIDs, c.AllowedTools):
			sel.RejectedCandidates = append(sel.RejectedCandidates, RejectedCandidate{AgentID: c.ID, Reason: "missing required tool allowlist entries"})
		case hasTool != nil && !toolsResolvable(req.RequiredToolIDs, hasTool):
			sel.RejectedCandidates = append(sel.RejectedCandidates, RejectedCandidate{AgentID: c.ID, Reason: "a required tool is not registered"})
		default:
			pool = append(pool, scored{c, len(intersect(req.Optional, c.Capabilities))})
		}
	}
	if len(pool) == 0 {
		sel.Reason = "no eligible agent matched capabilities/scope"
		return sel
	}
	sort.Slice(pool, func(i, j int) bool {
		if pool[i].opt != pool[j].opt {
			return pool[i].opt > pool[j].opt
		}
		return pool[i].d.ID < pool[j].d.ID
	})
	sel.Agent = &pool[0].d
	sel.Reason = fmt.Sprintf("selected on optional-capability coverage (%d) and stable id order", pool[0].opt)
	sel.MatchedCapabilities = intersect(req.Required, pool[0].d.Capabilities)
	sel.OptionalMatchedCapabilities = intersect(req.Optional, pool[0].d.Capabilities)
	return sel
}

func selEligible(c Definition, req *SelectionRequest, hasTool func(string) bool) bool {
	return c.Status == AgentActive && divisionEligible(c, req) &&
		covers(req.Required, c.Capabilities) && coversAllTools(req.RequiredToolIDs, c.AllowedTools) &&
		(hasTool == nil || toolsResolvable(req.RequiredToolIDs, hasTool))
}

// divisionEligible mirrors G3 narrow membership: a division-scoped request
// admits only business-wide agents and agents of that exact division; a
// business-scoped request (DivisionID empty) admits only business-wide
// agents. A division agent never silently escapes its division.
func divisionEligible(c Definition, req *SelectionRequest) bool {
	if req.DivisionID == "" {
		return c.DivisionID == ""
	}
	return c.DivisionID == req.DivisionID || c.DivisionID == ""
}

func covers(required, actual []string) bool {
	set := map[string]struct{}{}
	for _, a := range actual {
		set[a] = struct{}{}
	}
	for _, r := range required {
		if _, ok := set[r]; !ok {
			return false
		}
	}
	return true
}

func coversAllTools(required, allowed []string) bool {
	return covers(required, allowed)
}

func toolsResolvable(required []string, has func(string) bool) bool {
	for _, id := range required {
		if !has(id) {
			return false
		}
	}
	return true
}

func intersect(req, actual []string) []string {
	set := map[string]struct{}{}
	for _, a := range actual {
		set[a] = struct{}{}
	}
	out := []string{}
	for _, r := range req {
		if _, ok := set[r]; ok {
			out = append(out, r)
		}
	}
	return out
}

func missing(req, actual []string) []string {
	set := map[string]struct{}{}
	for _, a := range actual {
		set[a] = struct{}{}
	}
	out := []string{}
	for _, r := range req {
		if _, ok := set[r]; !ok {
			out = append(out, r)
		}
	}
	return out
}
