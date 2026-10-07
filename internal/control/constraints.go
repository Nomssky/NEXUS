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
			// Tighten only. A zero caller limit means the call is not duration
			// budgeted at all (the pre-platform builtin path), and a constraint
			// still gives it a bound — it never removes one.
			if c.MaxDuration > 0 && (cr.MaxDuration <= 0 || c.MaxDuration < cr.MaxDuration) {
				cr.MaxDuration = c.MaxDuration
			}
		}
		cr.Constraints = append(cr.Constraints, c)
	}
	return cr, nil
}

// ConstrainEffect is the single reusable enforcement point for a consequential
// effect that is NOT a capability invocation: delegation, a durable memory write
// and a durable memory delete all pass through it (contract §4).
//
// It exists so a constraint can never be merely recorded. The caller supplies the
// RUNTIME-ESTABLISHED target — the thing the effect will actually touch, never a
// value the model chose — and this function decides, before the effect, whether
// every returned constraint holds for it:
//
//   - a mandatory constraint the runtime cannot check for this action type fails
//     closed (ErrConstraintUnenforceable), which is the honest answer when the
//     runtime cannot prove compliance;
//   - an allowlist constraint admits exactly the values it names and rejects
//     everything else, including an empty value that cannot be identified;
//   - advisory constraints are recorded, never enforced, by construction: they
//     are not present in Admission.Effective.
//
// targetKind selects which comparison applies: a delegation is constrained by the
// delegate target id, a memory mutation by the resolved record id.
func ConstrainEffect(targetKind string, target string, adm Admission) (EffectConstraint, error) {
	ec := EffectConstraint{Kind: targetKind, Target: target}
	for _, c := range adm.Effective {
		if target == "" {
			// A restriction cannot be checked against an unidentified target, and
			// failing open here would let a constraint be skipped by omitting the
			// target.
			return ec, fmt.Errorf("%w: %s constraint %q cannot be checked against an unidentified target",
				ErrConstraintUnenforceable, targetKind, c.ID)
		}
		switch c.Kind {
		case ConstraintToolAllowlist, ConstraintOperationAllowlist, ConstraintResourceRestrict:
			if !c.Allows(target) {
				return ec, fmt.Errorf("%w: %s %q is outside the allowed set of %s",
					ErrConstraintUnenforceable, targetKind, target, c.ID)
			}
			ec.Applied = append(ec.Applied, c)
		case ConstraintMaxDuration:
			// A duration bound is meaningful only for an effect that has a
			// deadline. A non-tool effect has none, so enforcing it is impossible
			// rather than satisfied — fail closed instead of ignoring it.
			return ec, fmt.Errorf("%w: %s constraint %q has no duration to bound",
				ErrConstraintUnenforceable, targetKind, c.ID)
		default:
			return ec, fmt.Errorf("%w: %s constraint type %q is not enforceable for %s",
				ErrConstraintUnenforceable, targetKind, c.Kind, targetKind)
		}
	}
	return ec, nil
}

// EffectConstraint is the proven-compliant view of a non-tool effect: the target
// the runtime established, and the constraints that were checked against it.
type EffectConstraint struct {
	Kind    string
	Target  string
	Applied []EffectiveConstraint
}

// Limits is the minimal shape of the capability limits a constraint tightens.
type Limits struct {
	MaxDuration time.Duration
}
