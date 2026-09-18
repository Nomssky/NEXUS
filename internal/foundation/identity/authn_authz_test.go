package identity

import (
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
)

func newAuthFixture(t *testing.T) (*LocalAuthenticator, Identity) {
	t.Helper()
	id, err := New(testNexus, TypeAgent, "Agent", BusinessScope("biz-a"))
	if err != nil {
		t.Fatal(err)
	}
	a := NewLocalAuthenticator()
	hash := security.HashCredential([]byte("correct-credential"))
	if err := a.Register(id.ID, hash, AuthMethodToken); err != nil {
		t.Fatal(err)
	}
	return a, id
}

// TEST-M1-004: authentication success.
func TestAuthenticationSuccess(t *testing.T) {
	a, id := newAuthFixture(t)
	res, err := a.Authenticate(id.ID, []byte("correct-credential"))
	if err != nil {
		t.Fatalf("expected authentication success: %v", err)
	}
	if !res.Authenticated {
		t.Fatal("expected Authenticated=true")
	}
	if res.Method != AuthMethodToken {
		t.Fatalf("unexpected method %q", res.Method)
	}
	if !res.Valid(time.Now().UTC()) {
		t.Fatal("result should be valid")
	}
}

// TEST-M1-005: authentication failure.
func TestAuthenticationFailure(t *testing.T) {
	a, id := newAuthFixture(t)
	res, err := a.Authenticate(id.ID, []byte("wrong-credential"))
	if err == nil {
		t.Fatal("expected authentication error")
	}
	if res.Authenticated {
		t.Fatal("failed authentication must not be Authenticated")
	}
	if nerrors.CategoryOf(err) != nerrors.CategoryAuth {
		t.Fatalf("expected AUTH category, got %s", nerrors.CategoryOf(err))
	}
	// Unknown identity also fails closed with AUTH.
	if _, err := a.Authenticate("nx:agent:unknown", []byte("x")); nerrors.CategoryOf(err) != nerrors.CategoryAuth {
		t.Fatalf("unknown identity must fail with AUTH, got %s", nerrors.CategoryOf(err))
	}
}

// Authentication expiry is enforced.
func TestAuthenticationExpiry(t *testing.T) {
	a, id := newAuthFixture(t)
	base := time.Now().UTC()
	a.SetClock(func() time.Time { return base })
	a.SetTTL(time.Minute)

	res, err := a.Authenticate(id.ID, []byte("correct-credential"))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid(base) {
		t.Fatal("result should be valid at issue time")
	}
	if res.Valid(base.Add(2 * time.Minute)) {
		t.Fatal("result should be expired after TTL")
	}
	if err := RequireAuthenticated(res, base.Add(2*time.Minute)); err == nil {
		t.Fatal("expired authentication must not satisfy RequireAuthenticated")
	}
}

// TEST-M1-006: authentication != authorization.
func TestAuthenticationIsNotAuthorization(t *testing.T) {
	a, id := newAuthFixture(t)
	res, err := a.Authenticate(id.ID, []byte("correct-credential"))
	if err != nil {
		t.Fatal(err)
	}
	// Authenticated, but with no permission grants, authorization is denied.
	perms, _ := NewPermissionSet()
	eval := NewPermissionEvaluator()
	dec, err := eval.Evaluate(Request{
		ActorID:   id.ID,
		ActorType: id.Type,
		Action:    "execute_tool:publish",
		Resource:  "tool:instagram",
		Scope:     BusinessScope("biz-a"),
	}, perms)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Result != NotAuthorized {
		t.Fatal("authenticated identity without permission must be NOT_AUTHORIZED")
	}
	// Authentication result carries no permission/authority fields at all.
	_ = res
}

// TEST-M1-007: authorization success (with an explicit covering grant).
func TestAuthorizationSuccess(t *testing.T) {
	id, _ := New(testNexus, TypeAgent, "Agent", BusinessScope("biz-a"))
	perm := Permission{ID: "perm-1", Action: "execute_tool:publish", Resource: "tool:instagram", Scope: BusinessScope("biz-a")}
	perms, err := NewPermissionSet(perm)
	if err != nil {
		t.Fatal(err)
	}
	eval := NewPermissionEvaluator()
	dec, err := eval.Evaluate(Request{
		ActorID: id.ID, ActorType: id.Type,
		Action: "execute_tool:publish", Resource: "tool:instagram",
		Scope: BusinessScope("biz-a"),
	}, perms)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Result != Authorized {
		t.Fatalf("expected AUTHORIZED, got %s", dec.Result)
	}
	if err := Enforce(dec); err != nil {
		t.Fatalf("authorized decision should enforce to nil: %v", err)
	}
}

// TEST-M1-008: authorization denial.
func TestAuthorizationDenial(t *testing.T) {
	id, _ := New(testNexus, TypeAgent, "Agent", BusinessScope("biz-a"))
	perm := Permission{ID: "perm-1", Action: "execute_tool:publish", Resource: "tool:instagram", Scope: BusinessScope("biz-a")}
	perms, _ := NewPermissionSet(perm)
	eval := NewPermissionEvaluator()
	dec, err := eval.Evaluate(Request{
		ActorID: id.ID, ActorType: id.Type,
		Action: "execute_tool:delete", Resource: "tool:database",
		Scope: BusinessScope("biz-a"),
	}, perms)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Result != NotAuthorized {
		t.Fatalf("expected NOT_AUTHORIZED, got %s", dec.Result)
	}
	if err := Enforce(dec); nerrors.CategoryOf(err) != nerrors.CategoryAuthorization {
		t.Fatalf("expected AUTHORIZATION error, got %v", err)
	}
}

// Cross-business permission leakage: a grant scoped to biz-a must not authorize
// biz-b.
func TestPermissionBusinessIsolation(t *testing.T) {
	perm := Permission{ID: "p", Action: "execute_tool:publish", Resource: "tool:x", Scope: BusinessScope("biz-a")}
	perms, _ := NewPermissionSet(perm)
	eval := NewPermissionEvaluator()
	dec, _ := eval.Evaluate(Request{
		ActorID: "nx:agent:a", ActorType: TypeAgent,
		Action: "execute_tool:publish", Resource: "tool:x",
		Scope: BusinessScope("biz-b"),
	}, perms)
	if dec.Result != NotAuthorized {
		t.Fatal("a biz-a grant must not authorize a biz-b request")
	}
}

// TEST-M1-009: capability != permission.
func TestCapabilityIsNotPermission(t *testing.T) {
	// The actor is technically capable of using the tool...
	caps, err := NewCapabilitySet(CapabilityRef{ID: "tool:instagram", Type: CapabilityTool})
	if err != nil {
		t.Fatal(err)
	}
	if !caps.Has("tool:instagram") {
		t.Fatal("capability should be present")
	}
	// ...but has no permission grant. Authorization must still deny.
	perms, _ := NewPermissionSet()
	eval := NewPermissionEvaluator()
	dec, _ := eval.Evaluate(Request{
		ActorID: "nx:agent:a", ActorType: TypeAgent,
		Action: "execute_tool:publish", Resource: "tool:instagram",
		Scope: BusinessScope("biz-a"),
	}, perms)
	if dec.Result != NotAuthorized {
		t.Fatal("a capability must not imply permission")
	}
}

// TEST-M1-024: authorization context propagation — the request scope/business is
// carried through the decision.
func TestAuthorizationContextPropagation(t *testing.T) {
	perm := Permission{ID: "p", Action: "a", Resource: "r", Scope: BusinessScope("biz-a")}
	perms, _ := NewPermissionSet(perm)
	eval := NewPermissionEvaluator()
	req := Request{ActorID: "nx:agent:a", ActorType: TypeAgent, Action: "a", Resource: "r", Scope: BusinessScope("biz-a"), CorrelationID: "corr-9"}
	dec, _ := eval.Evaluate(req, perms)
	if dec.Scope.BusinessID != "biz-a" {
		t.Fatalf("decision must carry the request scope, got %+v", dec.Scope)
	}
	if dec.CorrelationID != "corr-9" {
		t.Fatalf("decision must echo correlation id, got %q", dec.CorrelationID)
	}
}

// TEST-M1-019: governance outcomes remain distinct from authorization results.
func TestGovernanceOutcomesDistinct(t *testing.T) {
	for _, outcome := range []string{"ALLOW", "DENY", "REQUIRE_APPROVAL", "ALLOW_WITH_CONSTRAINTS", "ESCALATE"} {
		if !IsGovernanceOutcome(outcome) {
			t.Fatalf("%s must be recognized as a governance outcome", outcome)
		}
	}
	// Our internal result vocabulary must never collide with governance outcomes.
	if IsGovernanceOutcome(string(Authorized)) || IsGovernanceOutcome(string(NotAuthorized)) {
		t.Fatal("authorization results must not collide with governance outcomes")
	}
}
