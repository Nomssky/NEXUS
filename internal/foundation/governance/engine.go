package governance

import (
	"fmt"
	"sort"
	"time"
)

// Engine is the core governance policy engine.
// It evaluates policies against requests and produces exactly one of
// the 5 canonical outcomes: ALLOW, DENY, REQUIRE_APPROVAL,
// ALLOW_WITH_CONSTRAINTS, ESCALATE.
//
// Invariants:
//   - Outcome is always exactly one of the 5 canonical outcomes.
//   - Default when no policy matches is DENY (fail-safe).
//   - More-restrictive-wins when policies conflict.
//   - Precedence: SYSTEM_SAFETY > GLOBAL > BUSINESS > DIVISION > AGENT > WORKFLOW > TASK.
//   - No self-approval: requesters cannot approve their own actions.
//   - Fail-safe: when governance is unavailable, default to DENY.
type Engine struct {
	policies []*Policy
	now      func() time.Time // injectable clock for testing
}

// NewEngine creates a new governance engine with the given policies.
func NewEngine(policies []*Policy) *Engine {
	return &Engine{
		policies: policies,
		now:      time.Now,
	}
}

// NewEngineWithClock creates a new governance engine with an injectable clock.
func NewEngineWithClock(policies []*Policy, now func() time.Time) *Engine {
	return &Engine{
		policies: policies,
		now:      now,
	}
}

// SetPolicies replaces the engine's policy set.
func (e *Engine) SetPolicies(policies []*Policy) {
	e.policies = policies
}

// Policies returns a copy of the current policy set.
func (e *Engine) Policies() []*Policy {
	out := make([]*Policy, len(e.policies))
	copy(out, e.policies)
	return out
}

// Evaluate evaluates a governance request against the loaded policies.
// It always returns a valid Decision with exactly one of the 5 canonical outcomes.
//
// Evaluation flow:
//  1. Identify actor
//  2. Resolve scope
//  3. Load matching policies (active, within scope, not expired)
//  4. Evaluate conditions
//  5. Check authority
//  6. Apply precedence (more-restrictive-wins)
//  7. Produce decision
//
// If no policy matches, the outcome is DENY (fail-safe / default deny).
func (e *Engine) Evaluate(req Request) Decision {
	now := e.now()

	// Step 1-3: Find all matching active policies
	matching := e.findMatchingPolicies(req, now)

	// If no policies match, default deny (fail-safe)
	if len(matching) == 0 {
		return Decision{
			Outcome:    DENY,
			Reason:     "no matching policy found (default deny)",
			ScopeLevel: ScopeLevelGlobal,
			Timestamp:  now,
		}
	}

	// Step 4-5: Evaluate conditions on each matching policy
	evaluated := e.evaluateConditions(matching, req)

	// Step 6: Apply precedence (more-restrictive-wins)
	return e.applyPrecedence(evaluated, req, now)
}

// findMatchingPolicies returns all policies that match the request scope
// and are currently active.
func (e *Engine) findMatchingPolicies(req Request, now time.Time) []*Policy {
	var matching []*Policy

	for _, p := range e.policies {
		// Must be active and within effective period
		if !p.IsActive(now) {
			continue
		}

		// Check scope match
		if !e.matchesScope(p, req) {
			continue
		}

		// Check subject match
		if !e.matchesSubject(p, req) {
			continue
		}

		// Check action match
		if !e.matchesAction(p, req) {
			continue
		}

		// Check resource match
		if !e.matchesResource(p, req) {
			continue
		}

		matching = append(matching, p)
	}

	return matching
}

// matchesScope checks if a policy's scope matches the request.
func (e *Engine) matchesScope(p *Policy, req Request) bool {
	// Global policy matches everything
	if p.BusinessID == "" {
		return true
	}

	// Business policy must match business
	if p.BusinessID != req.BusinessID {
		return false
	}

	// Division policy must match division
	if p.DivisionID != "" && p.DivisionID != req.DivisionID {
		return false
	}

	return true
}

// matchesSubject checks if a policy's subject matches the request actor.
func (e *Engine) matchesSubject(p *Policy, req Request) bool {
	switch p.Subject.SubjectType {
	case "all":
		return true
	case "identity":
		return e.matchesIDList(p.Subject.SubjectIDs, req.Actor)
	case "role":
		// Role matching would require role resolution; for now, exact match
		return e.matchesIDList(p.Subject.SubjectIDs, req.Actor)
	case "agent_type":
		return e.matchesIDList(p.Subject.SubjectIDs, req.Actor)
	default:
		return false
	}
}

// matchesAction checks if a policy's action matches the request action.
func (e *Engine) matchesAction(p *Policy, req Request) bool {
	if p.Action.ActionType == "" || p.Action.ActionType == "custom" {
		return len(p.Action.ActionIDs) == 0 || e.matchesIDList(p.Action.ActionIDs, req.Action)
	}
	// Exact action type match, or action is in the list
	return p.Action.ActionType == req.Action || e.matchesIDList(p.Action.ActionIDs, req.Action)
}

// matchesResource checks if a policy's resource matches the request resource.
func (e *Engine) matchesResource(p *Policy, req Request) bool {
	if p.Resource.ResourceType == "" || p.Resource.ResourceType == "all" {
		return true
	}
	if p.Resource.ResourceType != req.ResourceType {
		return false
	}
	if len(p.Resource.ResourceIDs) > 0 && !e.matchesIDList(p.Resource.ResourceIDs, req.Resource) {
		return false
	}
	return true
}

// matchesIDList checks if a target ID is in the list (or list is empty = match all).
func (e *Engine) matchesIDList(ids []string, target string) bool {
	if len(ids) == 0 {
		return true // empty list = all match
	}
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

// evaluateConditions evaluates conditions on each matching policy.
// For now, all conditions are considered satisfied (condition evaluation
// will be extended in M3+ when the full condition engine is built).
func (e *Engine) evaluateConditions(policies []*Policy, req Request) []*Policy {
	// All matching policies pass condition evaluation for now.
	// Future: evaluate time, scope, attribute, count, composite conditions.
	return policies
}

// applyPrecedence applies precedence rules to produce the final decision.
// Rule: more-restrictive-wins. When policies conflict, the most restrictive
// outcome takes precedence. Within the same restrictiveness level, higher
// precedence number wins.
//
// Restrictiveness order (least to most):
//
//	ALLOW < ALLOW_WITH_CONSTRAINTS < REQUIRE_APPROVAL < ESCALATE < DENY
func (e *Engine) applyPrecedence(policies []*Policy, req Request, now time.Time) Decision {
	if len(policies) == 0 {
		return Decision{
			Outcome:    DENY,
			Reason:     "no active policies (default deny)",
			ScopeLevel: ScopeLevelGlobal,
			Timestamp:  now,
		}
	}

	// Sort by restrictiveness (most restrictive first), then by precedence
	sort.Slice(policies, func(i, j int) bool {
		ri := restrictiveness(policies[i].Effect)
		rj := restrictiveness(policies[j].Effect)
		if ri != rj {
			return ri > rj // more restrictive first
		}
		return policies[i].Precedence > policies[j].Precedence // higher precedence first
	})

	// The most restrictive policy wins
	winner := policies[0]

	// Check for override relationships
	for _, p := range policies[1:] {
		for _, overrideID := range p.OverridePolicyIDs {
			if overrideID == winner.PolicyID {
				// p overrides winner
				winner = p
				break
			}
		}
	}

	// Build the decision
	decision := Decision{
		Outcome:              winner.Effect,
		Reason:               fmt.Sprintf("matched policy %s (v%s, precedence %d)", winner.PolicyID, winner.PolicyVersion, winner.Precedence),
		MatchedPolicyID:      winner.PolicyID,
		MatchedPolicyVersion: winner.PolicyVersion,
		ScopeLevel:           winner.ScopeLevelFor(),
		Timestamp:            now,
	}

	// Attach constraints for ALLOW_WITH_CONSTRAINTS
	if winner.Effect == ALLOW_WITH_CONSTRAINTS {
		decision.Constraints = winner.Constraints
	}

	// Attach approval config for REQUIRE_APPROVAL
	if winner.Effect == REQUIRE_APPROVAL {
		decision.ApprovalRequired = winner.ApprovalConfig
	}

	return decision
}

// restrictiveness returns a numeric restrictiveness score for an outcome.
// Higher score = more restrictive.
func restrictiveness(o Outcome) int {
	switch o {
	case ALLOW:
		return 0
	case ALLOW_WITH_CONSTRAINTS:
		return 1
	case REQUIRE_APPROVAL:
		return 2
	case ESCALATE:
		return 3
	case DENY:
		return 4
	default:
		return -1 // invalid outcome
	}
}
