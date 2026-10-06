package control

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/governance"
)

// Enforceable constraint types (contract §4). The set is closed on purpose: an
// unknown type cannot be enforced, so it fails closed rather than being ignored.
const (
	ConstraintToolAllowlist      = "tool_allowlist"
	ConstraintOperationAllowlist = "operation_allowlist"
	ConstraintMaxDuration        = "max_duration_ms"
	ConstraintResourceRestrict   = "resource_restriction"
)

// EffectiveConstraint is one resolved, enforceable restriction. It travels with
// the invocation: the capability platform never has to know about governance.
type EffectiveConstraint struct {
	Kind       string
	ID         string
	Expression string
	// Allowed is the parsed allowlist for allowlist/resource restrictions.
	Allowed []string
	// MaxDuration is the parsed bound for max_duration_ms.
	MaxDuration time.Duration
	// Severity is advisory or mandatory.
	Severity string
}

// Allows reports whether a value satisfies this constraint. An empty constraint
// allows everything (it constrains nothing).
func (c EffectiveConstraint) Allows(value string) bool {
	if len(c.Allowed) == 0 {
		return true
	}
	for _, a := range c.Allowed {
		if a == value {
			return true
		}
	}
	return false
}

// resolveConstraints turns a decision's constraints plus the request's own
// constraints into enforceable restrictions. A mandatory constraint that cannot
// be enforced returns ErrConstraintUnenforceable and the caller fails closed.
func (c *Controller) resolveConstraints(p Proposal, d governance.Decision) ([]EffectiveConstraint, error) {
	raw := append(append([]governance.Constraint{}, p.Constraints...), d.Constraints...)
	out := make([]EffectiveConstraint, 0, len(raw))
	for _, rc := range raw {
		ec := EffectiveConstraint{Kind: rc.ConstraintType, ID: rc.ConstraintID,
			Expression: rc.Expression, Severity: severity(rc.Severity)}
		switch rc.ConstraintType {
		case ConstraintToolAllowlist, ConstraintOperationAllowlist, ConstraintResourceRestrict:
			ec.Allowed = splitList(rc.Expression)
			if len(ec.Allowed) == 0 {
				return nil, unenforceable(rc, "expression lists no values")
			}
		case ConstraintMaxDuration:
			ms, err := strconv.Atoi(strings.TrimSpace(rc.Expression))
			if err != nil || ms <= 0 {
				return nil, unenforceable(rc, "max_duration_ms must be a positive integer")
			}
			if time.Duration(ms)*time.Millisecond > c.maxDur {
				// A constraint may only tighten the runtime bound.
				return nil, unenforceable(rc, "max_duration_ms exceeds the runtime bound")
			}
			ec.MaxDuration = time.Duration(ms) * time.Millisecond
		default:
			if ec.Severity == "advisory" {
				// Advisory is not a restriction by definition; it is recorded
				// but never silently treated as one.
				continue
			}
			return nil, unenforceable(rc, "constraint type is not enforceable by this runtime")
		}
		out = append(out, ec)
	}
	return out, nil
}

func severity(s string) string {
	if strings.EqualFold(strings.TrimSpace(s), "advisory") {
		return "advisory"
	}
	return "mandatory"
}

func unenforceable(c governance.Constraint, why string) error {
	return fmt.Errorf("%w: %s (%s/%s): %s", ErrConstraintUnenforceable,
		c.ConstraintID, c.ConstraintType, c.Severity, why)
}

func splitList(s string) []string {
	out := []string{}
	for _, part := range strings.Split(s, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// ConstrainedRequest is the effective request a caller must execute under: the
// original tool/operation plus the restrictions governance returned. It is a
// plain value the caller applies — governance never reaches into the capability
// platform.
type ConstrainedRequest struct {
	ToolID      string
	Operation   string
	MaxDuration time.Duration
	Constraints []EffectiveConstraint
}

// Constrain applies the resolved restrictions to one tool invocation. It is the
// only place a constraint becomes an effect, and it fails closed.
func Constrain(toolID, operation string, limits Limits, adm Admission) (ConstrainedRequest, error) {
	cr := ConstrainedRequest{ToolID: toolID, Operation: operation, MaxDuration: limits.MaxDuration}
	for _, c := range adm.Effective {
		switch c.Kind {
		case ConstraintToolAllowlist:
			if !c.Allows(toolID) {
				return cr, fmt.Errorf("%w: tool %q is outside the allowed set", ErrConstraintUnenforceable, toolID)
			}
		case ConstraintOperationAllowlist:
			if !c.Allows(operation) {
				return cr, fmt.Errorf("%w: operation %q is outside the allowed set",
					ErrConstraintUnenforceable, operation)
			}
		case ConstraintResourceRestrict:
			if !c.Allows(toolID) && !c.Allows(operation) {
				return cr, fmt.Errorf("%w: resource %q is outside the allowed set",
					ErrConstraintUnenforceable, toolID)
			}
		case ConstraintMaxDuration:
			// Tighten only.
			if c.MaxDuration > 0 && (cr.MaxDuration <= 0 || c.MaxDuration < cr.MaxDuration) {
				cr.MaxDuration = c.MaxDuration
			}
		}
		cr.Constraints = append(cr.Constraints, c)
	}
	if cr.MaxDuration <= 0 {
		return cr, fmt.Errorf("%w: constraints left the call without a duration bound",
			ErrConstraintUnenforceable)
	}
	return cr, nil
}

// Limits is the minimal shape of the capability limits a constraint tightens.
type Limits struct {
	MaxDuration time.Duration
}
