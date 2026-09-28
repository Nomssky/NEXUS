// Condition evaluation for policy applicability (B): the five contract
// types of SCHEMA_GOVERNANCE_ATTENTION §2.6 — time, scope, attribute,
// count, composite.
//
// The contract defines the fields (condition_id, condition_type,
// expression, negate) but leaves the expression grammar to the
// implementation. This package defines a small, uniform key=value grammar
// (comma-separated pairs) with per-type keys:
//
//	time       start=HH:MM,end=HH:MM[,days=mon,tue,...]
//	           In-window iff now ∈ [start,end); a start>end window wraps
//	           past midnight; start==end covers the full day. days is
//	           optional (absent = every day), otherwise an unordered list
//	           of 3-letter English day names.
//	scope      business=<id> and/or division=<id> and/or agent=<id> and/or
//	           workflow=<id> and/or task=<id> (at least one required). Each
//	           set key must equal the request's field exactly; an unset
//	           request field never matches a set condition key.
//	attribute  key=value pairs over the fixed request attributes: actor,
//	           action, resource, resource_type, business, division, agent,
//	           workflow, task, objective, risk (risk is case-insensitive).
//	           Unknown keys are an evaluation error.
//	count      action=<action>[,actor=<actor>],max=<n>[,window_seconds=<n>]
//	           Records one evaluation per Evaluate() call for this condition
//	           (the current evaluation included) and is true once the number
//	           of recorded evaluations within the window reaches max
//	           (window_seconds=0 keeps history since process start).
//	composite  all:<id>[,<id>...] or any:<id>[,<id>...] — references sibling
//	           conditions of the same policy by condition_id and combines
//	           their results (AND / OR). Unknown ids and cycles are
//	           evaluation errors. Conditions referenced by a top-level
//	           composite are not additionally required by the policy-level
//	           AND (otherwise any/composite could never take effect).
//
// Semantics:
//   - A condition that evaluates false excludes its policy — the policy
//     simply does not apply (the decision then falls to other policies or
//     the default-deny rule).
//   - negate inverts that condition's raw result.
//   - A condition that cannot be evaluated (malformed expression, unknown
//     key, unknown reference, cycle) is an evaluation ERROR: the whole
//     evaluation fails safe to DENY (consistent with the engine's
//     fail-safe invariant), never silently to "condition passes" (the
//     pre-B no-op behavior).
package governance

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// conditionOutcome is the result of evaluating one condition before negate.
type conditionOutcome bool

// evaluateConditions returns the subset of matching policies whose
// conditions all hold for req at now. A non-nil error means at least one
// condition was not evaluable — the caller must deny fail-safe.
func (e *Engine) evaluateConditions(policies []*Policy, req Request, now time.Time) ([]*Policy, error) {
	evaluated := make([]*Policy, 0, len(policies))
	for _, p := range policies {
		ok, err := e.policyConditionsHold(p, req, now)
		if err != nil {
			return nil, err
		}
		if ok {
			evaluated = append(evaluated, p)
		}
	}
	return evaluated, nil
}

// policyConditionsHold reports whether every top-level condition of p holds.
func (e *Engine) policyConditionsHold(p *Policy, req Request, now time.Time) (bool, error) {
	if len(p.Conditions) == 0 {
		return true, nil
	}

	// Static walk first: unknown references and composite cycles are errors
	// even when the cycle would keep every composite out of the top-level
	// set (mutually-referencing composites would otherwise both be excluded
	// as operands and never be evaluated at all).
	if err := checkCompositeGraph(p); err != nil {
		return false, err
	}

	// Conditions referenced by a top-level composite are operands of that
	// composite — they are evaluated recursively and must not additionally
	// gate the policy (an any(a,b) with a=false would otherwise always fail
	// the policy-level AND).
	referenced := make(map[string]bool)
	for _, c := range p.Conditions {
		if c.ConditionType == "composite" {
			ids, err := parseCompositeRefs(c.Expression)
			if err != nil {
				return false, fmt.Errorf("condition %s: %w", c.ConditionID, err)
			}
			for _, id := range ids {
				referenced[id] = true
			}
		}
	}

	for _, c := range p.Conditions {
		if referenced[c.ConditionID] {
			continue
		}
		ok, err := e.evalCondition(p, c, req, now, make(map[string]bool))
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// evalCondition evaluates a single condition (recursing for composite) and
// applies negate. path guards against composite cycles.
func (e *Engine) evalCondition(p *Policy, c Condition, req Request, now time.Time, path map[string]bool) (bool, error) {
	if c.ConditionID != "" {
		if path[c.ConditionID] {
			return false, fmt.Errorf("condition %s: composite cycle via %s", c.ConditionID, c.ConditionID)
		}
		path[c.ConditionID] = true
		defer delete(path, c.ConditionID)
	}

	var (
		result conditionOutcome
		err    error
	)
	switch c.ConditionType {
	case "time":
		result, err = evalTime(c, now)
	case "scope":
		result, err = evalScope(c, req)
	case "attribute":
		result, err = evalAttribute(c, req)
	case "count":
		result, err = e.evalCount(c, now)
	case "composite":
		result, err = e.evalComposite(p, c, req, now, path)
	default:
		err = fmt.Errorf("condition %s: unknown condition_type %q", c.ConditionID, c.ConditionType)
	}
	if err != nil {
		return false, err
	}
	if c.Negate {
		return !bool(result), nil
	}
	return bool(result), nil
}

// parseCondExpr parses the shared grammar: comma-separated key=value pairs.
func parseCondExpr(expr string) (map[string]string, error) {
	out := make(map[string]string)
	for _, part := range strings.Split(expr, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if !ok || k == "" || v == "" {
			return nil, fmt.Errorf("malformed expression %q (want key=value)", expr)
		}
		if _, dup := out[k]; dup {
			return nil, fmt.Errorf("duplicate key %q in %q", k, expr)
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty expression")
	}
	return out, nil
}

// evalTime implements the time window condition.
func evalTime(c Condition, now time.Time) (conditionOutcome, error) {
	kv, err := parseCondExpr(c.Expression)
	if err != nil {
		return false, fmt.Errorf("condition %s: %w", c.ConditionID, err)
	}
	startRaw, ok := kv["start"]
	if !ok {
		return false, fmt.Errorf("condition %s: time requires start=HH:MM", c.ConditionID)
	}
	endRaw, ok := kv["end"]
	if !ok {
		return false, fmt.Errorf("condition %s: time requires end=HH:MM", c.ConditionID)
	}
	start, err := time.Parse("15:04", startRaw)
	if err != nil {
		return false, fmt.Errorf("condition %s: bad start %q: %w", c.ConditionID, startRaw, err)
	}
	end, err := time.Parse("15:04", endRaw)
	if err != nil {
		return false, fmt.Errorf("condition %s: bad end %q: %w", c.ConditionID, endRaw, err)
	}
	if daysRaw, ok := kv["days"]; ok {
		ok, err := dayMatches(daysRaw, now)
		if err != nil {
			return false, fmt.Errorf("condition %s: %w", c.ConditionID, err)
		}
		if !ok {
			return false, nil
		}
	}

	cur := time.Date(2000, 1, 1, now.Hour(), now.Minute(), 0, 0, time.UTC)
	s := time.Date(2000, 1, 1, start.Hour(), start.Minute(), 0, 0, time.UTC)
	en := time.Date(2000, 1, 1, end.Hour(), end.Minute(), 0, 0, time.UTC)
	switch {
	case s.Equal(en):
		return true, nil // full-day window
	case s.Before(en):
		return conditionOutcome(!cur.Before(s) && cur.Before(en)), nil
	default:
		// Wraps past midnight: [start,24:00) ∪ [00:00,end).
		return conditionOutcome(!cur.Before(s) || cur.Before(en)), nil
	}
}

// dayMatches checks now's weekday against a comma-separated list of
// 3-letter English day names (mon, tue, ...).
func dayMatches(expr string, now time.Time) (bool, error) {
	want := now.Weekday().String()[:3]
	want = strings.ToLower(want)
	for _, d := range strings.Split(expr, ",") {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		if !isKnownDay(d) {
			return false, fmt.Errorf("unknown day %q (want 3-letter english day)", d)
		}
		if d == want {
			return true, nil
		}
	}
	return false, nil
}

func isKnownDay(d string) bool {
	switch d {
	case "mon", "tue", "wed", "thu", "fri", "sat", "sun":
		return true
	}
	return false
}

// evalScope implements the business/division/agent/workflow/task condition.
func evalScope(c Condition, req Request) (conditionOutcome, error) {
	kv, err := parseCondExpr(c.Expression)
	if err != nil {
		return false, fmt.Errorf("condition %s: %w", c.ConditionID, err)
	}
	if _, ok := kv["business"]; !ok {
		if _, ok := kv["division"]; !ok {
			if _, ok := kv["agent"]; !ok {
				if _, ok := kv["workflow"]; !ok {
					if _, ok := kv["task"]; !ok {
						return false, fmt.Errorf("condition %s: scope requires at least one of business=/division=/agent=/workflow=/task=", c.ConditionID)
					}
				}
			}
		}
	}
	if b, ok := kv["business"]; ok && b != req.BusinessID {
		return false, nil
	}
	if d, ok := kv["division"]; ok && d != req.DivisionID {
		return false, nil
	}
	if a, ok := kv["agent"]; ok && a != req.AgentID {
		return false, nil
	}
	if w, ok := kv["workflow"]; ok && w != req.WorkflowID {
		return false, nil
	}
	if tk, ok := kv["task"]; ok && tk != req.TaskID {
		return false, nil
	}
	return true, nil
}

// evalAttribute implements fixed request-attribute equality.
func evalAttribute(c Condition, req Request) (conditionOutcome, error) {
	kv, err := parseCondExpr(c.Expression)
	if err != nil {
		return false, fmt.Errorf("condition %s: %w", c.ConditionID, err)
	}
	for k, v := range kv {
		switch k {
		case "actor":
			if v != req.Actor {
				return false, nil
			}
		case "action":
			if v != req.Action {
				return false, nil
			}
		case "resource":
			if v != req.Resource {
				return false, nil
			}
		case "resource_type":
			if v != req.ResourceType {
				return false, nil
			}
		case "business":
			if v != req.BusinessID {
				return false, nil
			}
		case "division":
			if v != req.DivisionID {
				return false, nil
			}
		case "agent":
			if v != req.AgentID {
				return false, nil
			}
		case "workflow":
			if v != req.WorkflowID {
				return false, nil
			}
		case "task":
			if v != req.TaskID {
				return false, nil
			}
		case "objective":
			if v != req.ObjectiveID {
				return false, nil
			}
		case "risk":
			if !strings.EqualFold(v, string(req.RiskLevel)) {
				return false, nil
			}
		default:
			return false, fmt.Errorf("condition %s: unknown attribute %q", c.ConditionID, k)
		}
	}
	return true, nil
}

// evalCount records this evaluation for the condition's selector and reports
// whether the windowed count has reached max. Counters are keyed per
// condition id + selector and guarded by condMu (Evaluate holds e.mu.RLock,
// so the counters must live behind their own leaf lock).
func (e *Engine) evalCount(c Condition, now time.Time) (conditionOutcome, error) {
	kv, err := parseCondExpr(c.Expression)
	if err != nil {
		return false, fmt.Errorf("condition %s: %w", c.ConditionID, err)
	}
	action, ok := kv["action"]
	if !ok {
		return false, fmt.Errorf("condition %s: count requires action=", c.ConditionID)
	}
	maxRaw, ok := kv["max"]
	if !ok {
		return false, fmt.Errorf("condition %s: count requires max=", c.ConditionID)
	}
	max, err := strconv.Atoi(maxRaw)
	if err != nil || max < 1 {
		return false, fmt.Errorf("condition %s: bad max %q (want integer >= 1)", c.ConditionID, maxRaw)
	}
	window := 0
	if wRaw, ok := kv["window_seconds"]; ok {
		w, err := strconv.Atoi(wRaw)
		if err != nil || w < 0 {
			return false, fmt.Errorf("condition %s: bad window_seconds %q", c.ConditionID, wRaw)
		}
		window = w
	}
	actor := kv["actor"] // optional selector

	key := fmt.Sprintf("%s|%s|%s", c.ConditionID, action, actor)
	e.condMu.Lock()
	defer e.condMu.Unlock()
	hist := e.condCounts[key]
	if window > 0 {
		cutoff := now.Add(-time.Duration(window) * time.Second)
		kept := hist[:0]
		for _, t := range hist {
			if t.After(cutoff) {
				kept = append(kept, t)
			}
		}
		hist = kept
	}
	hist = append(hist, now)
	e.condCounts[key] = hist
	return conditionOutcome(len(hist) >= max), nil
}

// parseCompositeRefs parses all:/any: reference lists.
func parseCompositeRefs(expr string) ([]string, error) {
	kind, rest, ok := strings.Cut(expr, ":")
	if !ok || (kind != "all" && kind != "any") {
		return nil, fmt.Errorf("composite expression %q must be all:<ids> or any:<ids>", expr)
	}
	var ids []string
	for _, id := range strings.Split(rest, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, fmt.Errorf("composite expression %q has an empty id", expr)
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("composite expression %q references no conditions", expr)
	}
	return ids, nil
}

// checkCompositeGraph statically validates every composite reference in the
// policy: referenced ids must exist and the reference graph must be acyclic.
// Evaluation-time recursion cannot catch these — a cycle keeps its members
// in the operand set and they would never be evaluated at all.
func checkCompositeGraph(p *Policy) error {
	byID := make(map[string]Condition, len(p.Conditions))
	for _, c := range p.Conditions {
		if c.ConditionID == "" {
			continue
		}
		if _, dup := byID[c.ConditionID]; dup {
			return fmt.Errorf("policy %s: duplicate condition_id %q", p.PolicyID, c.ConditionID)
		}
		byID[c.ConditionID] = c
	}
	const (
		stateOpen = 1
		stateDone = 2
	)
	state := make(map[string]int)
	var visit func(id string) error
	visit = func(id string) error {
		switch state[id] {
		case stateOpen:
			return fmt.Errorf("policy %s: composite cycle via condition %s", p.PolicyID, id)
		case stateDone:
			return nil
		}
		c, ok := byID[id]
		if !ok {
			return fmt.Errorf("policy %s: unknown condition reference %q", p.PolicyID, id)
		}
		if c.ConditionType != "composite" {
			state[id] = stateDone
			return nil
		}
		state[id] = stateOpen
		ids, err := parseCompositeRefs(c.Expression)
		if err != nil {
			return fmt.Errorf("condition %s: %w", id, err)
		}
		for _, ref := range ids {
			if err := visit(ref); err != nil {
				return err
			}
		}
		state[id] = stateDone
		return nil
	}
	for _, c := range p.Conditions {
		if c.ConditionType == "composite" && c.ConditionID != "" {
			if err := visit(c.ConditionID); err != nil {
				return err
			}
		}
	}
	return nil
}

// evalComposite evaluates referenced sibling conditions and combines them.
func (e *Engine) evalComposite(p *Policy, c Condition, req Request, now time.Time, path map[string]bool) (conditionOutcome, error) {
	kind, _, _ := strings.Cut(c.Expression, ":")
	ids, err := parseCompositeRefs(c.Expression)
	if err != nil {
		return false, fmt.Errorf("condition %s: %w", c.ConditionID, err)
	}
	byID := make(map[string]Condition, len(p.Conditions))
	for _, sib := range p.Conditions {
		if sib.ConditionID == "" {
			continue
		}
		if _, dup := byID[sib.ConditionID]; dup {
			return false, fmt.Errorf("condition %s: duplicate condition_id %q in policy %s", c.ConditionID, sib.ConditionID, p.PolicyID)
		}
		byID[sib.ConditionID] = sib
	}
	for _, id := range ids {
		sib, ok := byID[id]
		if !ok {
			return false, fmt.Errorf("condition %s: unknown reference %q in policy %s", c.ConditionID, id, p.PolicyID)
		}
		ok, err := e.evalCondition(p, sib, req, now, path)
		if err != nil {
			return false, err
		}
		if kind == "all" {
			if !ok {
				return false, nil
			}
		} else if ok {
			return true, nil
		}
	}
	if kind == "all" {
		return true, nil
	}
	return false, nil
}
