// Package identity — authorization primitives.
//
// Authorization evaluation answers "is this action permitted for this actor in
// this scope?", producing an INTERNAL result of AUTHORIZED / NOT_AUTHORIZED.
//
// This internal result is deliberately DISTINCT from the canonical Governance
// outcomes (ALLOW, DENY, REQUIRE_APPROVAL, ALLOW_WITH_CONSTRAINTS, ESCALATE).
// It exists only to express a local evaluation and MUST NOT replace or emulate
// the Governance vocabulary. Any place that needs a governance decision must go
// through Governance (C03, a later milestone). See contracts/SCHEMA_GOVERNANCE_ATTENTION.md §2.1.
//
// Conceptually:
//
//	IDENTITY -> AUTHENTICATION -> AUTHORIZATION -> (GOVERNANCE DECISION)
//
// M1 provides the evaluation boundary only. It does NOT implement the complete
// Governance Policy Engine (that is M2).
package identity

import (
	"fmt"
	"strings"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
)

// Result is the internal authorization evaluation result.
//
// It is intentionally a two-valued vocabulary, NOT the five governance
// outcomes. Authorization != Governance.
type Result string

const (
	// Authorized means the local policy evaluation permits the action. It is NOT
	// a governance ALLOW and does not override governance.
	Authorized Result = "AUTHORIZED"
	// NotAuthorized means the local policy evaluation does not permit the action.
	NotAuthorized Result = "NOT_AUTHORIZED"
)

// IsGovernanceOutcome reports whether a string collides with the canonical
// Governance outcome vocabulary. Authorization results must never use these
// values; this guard makes the separation explicit and testable.
func IsGovernanceOutcome(v string) bool {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "ALLOW", "DENY", "REQUIRE_APPROVAL", "ALLOW_WITH_CONSTRAINTS", "ESCALATE":
		return true
	default:
		return false
	}
}

// Request is an access request. It equates to the conceptual Access Request in
// Core/NEXUS-IDENTITY-ACCESS-TRUST-SYSTEM.md §32, reduced to what M1 evaluates.
//
// It carries identity, scope, and the requested action/resource. It carries no
// capability or permission claims that could be trusted blindly.
type Request struct {
	// ActorID is the identity requesting the action.
	ActorID string `json:"actor_id"`
	// ActorType is the actor's identity type (who/what).
	ActorType Type `json:"actor_type"`
	// Action is the action being requested (e.g. "execute_tool:publish").
	Action string `json:"action"`
	// Resource is the resource the action targets (e.g. "tool:instagram").
	Resource string `json:"resource"`
	// Scope is the explicit scope of the request (business/division/...).
	Scope Scope `json:"scope"`
	// CorrelationID is for tracing only and grants no authority (INV-19).
	CorrelationID string `json:"correlation_id,omitempty"`
}

// Validate checks the request is well-formed and fails closed on malformed input.
func (r Request) Validate() error {
	if strings.TrimSpace(r.ActorID) == "" {
		return nerrors.Validation("authz.actor_required", "authorization request requires an actor_id")
	}
	if !r.ActorType.IsValid() {
		return nerrors.Validation("authz.actor_type_invalid",
			fmt.Sprintf("authorization request actor_type %q is not valid", r.ActorType))
	}
	if strings.TrimSpace(r.Action) == "" {
		return nerrors.Validation("authz.action_required", "authorization request requires an action")
	}
	if strings.TrimSpace(r.Resource) == "" {
		return nerrors.Validation("authz.resource_required", "authorization request requires a resource")
	}
	return r.Scope.Validate()
}

// Decision is the internal authorization decision, carrying the internal result
// plus the inputs that produced it (for auditability). It is NOT a governance
// decision and must not be presented as one.
type Decision struct {
	Result Result `json:"result"`
	// ActorID/Action/Resource echo the evaluated request.
	ActorID  string `json:"actor_id"`
	Action   string `json:"action"`
	Resource string `json:"resource"`
	// Scope is the scope evaluated.
	Scope Scope `json:"scope"`
	// CorrelationID echoes the request's correlation id for audit tracing only.
	// It grants no authority (INV-19).
	CorrelationID string `json:"correlation_id,omitempty"`
	// Reason is a short, secret-free explanation.
	Reason string `json:"reason"`
	// EvaluatedAt is when the evaluation happened.
	EvaluatedAt time.Time `json:"evaluated_at"`
}

// Evaluator performs a minimal local authorization evaluation.
//
// It must NOT be a governance engine. Evaluators are intentionally small and
// composable so the real policy engine can replace them at M2.
type Evaluator interface {
	// Evaluate returns an internal authorization decision. It fails closed: any
	// error yields NotAuthorized.
	Evaluate(req Request, perm *PermissionSet) (Decision, error)
}

// PermissionEvaluator authorizes an action iff an explicit permission grant
// covers the action/resource/scope.
//
// This is the default M1 evaluator. It implements least privilege and business
// isolation: a grant whose scope does not cover the request is not granted.
// Crucially, it evaluates PERMISSIONS only — a capability is never consulted, so
// CAPABILITY != PERMISSION holds structurally.
type PermissionEvaluator struct {
	now func() time.Time
}

// NewPermissionEvaluator constructs a permission evaluator.
func NewPermissionEvaluator() *PermissionEvaluator {
	return &PermissionEvaluator{now: func() time.Time { return time.Now().UTC() }}
}

// SetClock injects a clock for deterministic tests.
func (e *PermissionEvaluator) SetClock(now func() time.Time) { e.now = now }

// Evaluate implements Evaluator. It fails closed on malformed requests or errors.
func (e *PermissionEvaluator) Evaluate(req Request, perm *PermissionSet) (Decision, error) {
	now := e.now()
	dec := Decision{
		ActorID:       req.ActorID,
		Action:        req.Action,
		Resource:      req.Resource,
		Scope:         req.Scope,
		CorrelationID: req.CorrelationID,
		EvaluatedAt:   now,
	}
	if err := req.Validate(); err != nil {
		dec.Result = NotAuthorized
		dec.Reason = "malformed request"
		return dec, err
	}
	if perm.Has(req.Action, req.Resource, req.Scope) {
		dec.Result = Authorized
		dec.Reason = "explicit permission grant covers the requested action/resource/scope"
		return dec, nil
	}
	dec.Result = NotAuthorized
	dec.Reason = "no permission grant covers the requested action/resource/scope"
	return dec, nil
}

// Enforce converts a NOT_AUTHORIZED decision into a canonical AUTHORIZATION
// error. An AUTHORIZED decision returns nil. This is the boundary helper later
// components use; it produces an AUTHORIZATION-category error, never a policy
// outcome.
func Enforce(dec Decision) error {
	if dec.Result == Authorized {
		return nil
	}
	return nerrors.New("authz.not_authorized", nerrors.CategoryAuthorization,
		"action is not authorized for this actor in this scope")
}
