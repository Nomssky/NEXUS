package identity

import (
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/nerrors"
)

const testNexus = "nx:nexus:test"

// TEST-M1-001: identity creation/validation.
func TestIdentityCreateValidate(t *testing.T) {
	sc := BusinessScope("biz-a")
	id, err := New(testNexus, TypeAgent, "Research Agent", sc)
	if err != nil {
		t.Fatalf("expected valid identity, got %v", err)
	}
	if err := id.Validate(); err != nil {
		t.Fatalf("expected identity to validate, got %v", err)
	}
	if id.Status != StatusActive {
		t.Fatalf("new identity should be active, got %q", id.Status)
	}
	if id.ID == "" || id.NexusID != testNexus {
		t.Fatalf("identity id/nexus id not set: %+v", id)
	}
}

// TEST-M1-002: identity uniqueness — generated IDs differ.
func TestIdentityUniqueness(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 100; i++ {
		id, err := New(testNexus, TypeService, "svc", GlobalScope())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, dup := seen[id.ID]; dup {
			t.Fatalf("duplicate identity id generated: %s", id.ID)
		}
		seen[id.ID] = struct{}{}
	}
}

// TEST-M1-003: identity type separation — types are distinct and validated.
func TestIdentityTypeSeparation(t *testing.T) {
	if TypeHuman == TypeAgent || TypeAgent == TypeService || TypeService == TypeSystem {
		t.Fatal("identity types must be distinct values")
	}
	if !TypeHuman.IsValid() || !TypeAgent.IsValid() {
		t.Fatal("known types must validate")
	}
	if Type("wizard").IsValid() {
		t.Fatal("unknown type must not validate")
	}
	// A global system identity must not be business-scoped.
	_, err := New(testNexus, TypeSystem, "nexus core", BusinessScope("biz-a"))
	if err == nil {
		t.Fatal("system identity must not be business-scoped")
	}
	if nerrors.CategoryOf(err) != nerrors.CategoryValidation {
		t.Fatalf("expected VALIDATION, got %s", nerrors.CategoryOf(err))
	}
}

// TEST-M1-025: malformed identity rejected.
func TestMalformedIdentityRejected(t *testing.T) {
	bad := Identity{ID: "", NexusID: testNexus, Type: TypeAgent, Status: StatusActive, DisplayName: "x", Scope: BusinessScope("b")}
	if err := bad.Validate(); err == nil {
		t.Fatal("missing id must be rejected")
	}
	bad2 := Identity{ID: "nx:agent:x", NexusID: testNexus, Type: TypeAgent, Status: "nonsense", DisplayName: "x", Scope: BusinessScope("b")}
	if err := bad2.Validate(); err == nil {
		t.Fatal("invalid status must be rejected")
	}
	// Secret-bearing metadata must be rejected so secrets cannot hide in identity metadata.
	bad3 := Identity{ID: "nx:agent:x", NexusID: testNexus, Type: TypeAgent, Status: StatusActive, DisplayName: "x", Scope: BusinessScope("b"), Metadata: map[string]string{"api_key": "abc"}}
	if err := bad3.Validate(); err == nil {
		t.Fatal("secret-bearing metadata key must be rejected")
	}
}

// TEST-M1-020: identity != authority. Identity has no authority field, and a
// membership/role is descriptive only.
func TestIdentityIsNotAuthority(t *testing.T) {
	id, err := New(testNexus, TypeHuman, "Owner", BusinessScope("biz-a"))
	if err != nil {
		t.Fatal(err)
	}
	// A role label exists on membership only as a descriptive string; it does not
	// produce a permission or authorization.
	m := Membership{IdentityID: id.ID, BusinessID: "biz-a", Role: RoleOwner, Status: StatusActive}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	// Being OWNER does not yield any permission grant by itself.
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
	if dec.Result == Authorized {
		t.Fatal("identity/role must not grant authorization without an explicit permission")
	}
}

// ---------- Scope tests ----------

// TEST-M1-010: business scope isolation.
func TestBusinessScopeIsolation(t *testing.T) {
	a := BusinessScope("biz-a")
	b := BusinessScope("biz-b")
	if a.Covers(b) {
		t.Fatal("business A scope must not cover business B scope")
	}
	if a.Covers(a) != true {
		t.Fatal("business A scope must cover itself")
	}
	if !GlobalScope().Covers(b) {
		t.Fatal("global scope covers any business")
	}
}

// TEST-M1-011: division scope isolation.
func TestDivisionScopeIsolation(t *testing.T) {
	media := DivisionScope("biz-a", "media")
	research := DivisionScope("biz-a", "research")
	bizA := BusinessScope("biz-a")
	if media.Covers(research) {
		t.Fatal("sibling divisions must not see each other")
	}
	if !bizA.Covers(media) {
		t.Fatal("business scope covers its divisions")
	}
	if media.Covers(bizA) {
		t.Fatal("division scope must not cover the whole business")
	}
	if divisionScopeWithoutBusiness() {
		t.Fatal("division without business must be invalid")
	}
}

func divisionScopeWithoutBusiness() bool {
	s := Scope{Kind: ScopeDivision, DivisionID: "media"}
	return s.Validate() == nil
}

// TEST-M1-012: security context validation.
func TestSecurityContextValidation(t *testing.T) {
	id, _ := New(testNexus, TypeHuman, "User", BusinessScope("biz-a"))
	auth := AuthResult{Authenticated: true, IdentityID: id.ID, Method: AuthMethodToken, AuthenticatedAt: time.Now().UTC()}
	sc, err := NewSecurityContext(id, auth, "biz-a", "", "corr-1", ClassificationInternal)
	if err != nil {
		t.Fatalf("valid context rejected: %v", err)
	}
	if sc.ActiveScope().BusinessID != "biz-a" {
		t.Fatal("active scope mismatch")
	}
}

// TEST-M1-026: malformed security context rejected.
func TestMalformedSecurityContextRejected(t *testing.T) {
	id, _ := New(testNexus, TypeHuman, "User", BusinessScope("biz-a"))
	auth := AuthResult{Authenticated: true, IdentityID: id.ID, Method: AuthMethodToken}

	// Division without business is malformed.
	if _, err := NewSecurityContext(id, auth, "", "media", "c", ClassificationInternal); err == nil {
		t.Fatal("division without business must be rejected")
	}
	// Auth/actor mismatch.
	other := AuthResult{Authenticated: true, IdentityID: "nx:human:other"}
	if _, err := NewSecurityContext(id, other, "biz-a", "", "c", ClassificationInternal); err == nil {
		t.Fatal("auth identity mismatch must be rejected")
	}
	// Invalid classification.
	if _, err := NewSecurityContext(id, auth, "biz-a", "", "c", Classification("NOPE")); err == nil {
		t.Fatal("invalid classification must be rejected")
	}
	// Actor scoped to biz-a cannot carry a biz-b context.
	if _, err := NewSecurityContext(id, auth, "biz-b", "", "c", ClassificationInternal); err == nil {
		t.Fatal("cross-business context must be rejected")
	}
}

// ---------- Membership tests ----------

// TEST-M1-022: no cross-business authorization leakage.
func TestCrossBusinessMembershipIsolation(t *testing.T) {
	id, _ := New(testNexus, TypeHuman, "User", BusinessScope("biz-a"))
	ms := NewMembershipSet()
	if err := ms.Add(Membership{IdentityID: id.ID, BusinessID: "biz-a", Role: RoleOperator, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.CurrentBusiness(id.ID, "biz-a", ""); err != nil {
		t.Fatalf("expected membership in biz-a: %v", err)
	}
	// Not a member of biz-b -> fail closed.
	_, err := ms.CurrentBusiness(id.ID, "biz-b", "")
	if err == nil {
		t.Fatal("membership in biz-a must not grant biz-b context")
	}
	if nerrors.CategoryOf(err) != nerrors.CategoryAuthorization {
		t.Fatalf("expected AUTHORIZATION, got %s", nerrors.CategoryOf(err))
	}
}

// Business switching is explicit and verified; a context cannot silently repoint
// to a business the actor does not belong to.
func TestSecurityContextBusinessSwitch(t *testing.T) {
	id, _ := New(testNexus, TypeService, "svc", GlobalScope())
	auth := AuthResult{Authenticated: true, IdentityID: id.ID, Method: AuthMethodService}
	sc, err := NewSecurityContext(id, auth, "", "", "c", ClassificationInternal)
	if err != nil {
		t.Fatal(err)
	}
	ms := NewMembershipSet()
	_ = ms.Add(Membership{IdentityID: id.ID, BusinessID: "biz-a", Role: RoleMember, Status: StatusActive})

	if _, err := sc.TrySwitchBusiness(ms, "biz-a", ""); err != nil {
		t.Fatalf("switch to member business should succeed: %v", err)
	}
	if _, err := sc.TrySwitchBusiness(ms, "biz-b", ""); err == nil {
		t.Fatal("switch to non-member business must fail closed")
	}
}
