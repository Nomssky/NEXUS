package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
)

// orgGateway builds a started engine plus registry/authenticator/memberships
// and a gateway with the org surface wired. The authenticator is registry-
// bound (production posture: credential alone is not enough).
func orgGateway(t *testing.T, opts ...ServerOption) (*identity.Registry, *identity.LocalAuthenticator, *identity.MembershipSet, *Server) {
	t.Helper()
	engine, err := core.NewEngine(nil)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = engine.Stop(ctx) })

	reg := identity.NewRegistry("nx:nexus:test")
	auth := identity.NewLocalAuthenticator()
	auth.SetRegistry(reg)
	members := identity.NewMembershipSet()

	base := []ServerOption{WithRegistry(reg), WithIdentity(auth, members)}
	srv := NewServer(engine, ":0", append(base, opts...)...)
	return reg, auth, members, srv
}

// postJSON issues a JSON POST and returns the recorder.
func postJSON(srv *Server, path string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

// getOrg issues a GET and returns the recorder.
func getOrg(srv *Server, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

// bootstrapOrg creates a global human owner, a business owned by them, and
// returns the business record.
func bootstrapOrg(t *testing.T, srv *Server) (ownerID string, b identity.Business) {
	t.Helper()
	w := postJSON(srv, "/api/v1/identities", map[string]any{
		"identity_type": "human",
		"display_name":  "Owner",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("owner create: %d body=%s", w.Code, w.Body.String())
	}
	var owner identity.Identity
	if err := json.Unmarshal(w.Body.Bytes(), &owner); err != nil {
		t.Fatal(err)
	}
	w = postJSON(srv, "/api/v1/businesses", map[string]any{
		"name":              "Acme",
		"owner_identity_id": owner.ID,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("business create: %d body=%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	return owner.ID, b
}

// TEST-GW-IDN-01: identity create stamps the contract envelope (§2.2), the
// optional credential works in the same step, and system/API-invalid types
// are rejected.
func TestGatewayCreateIdentity(t *testing.T) {
	_, auth, _, srv := orgGateway(t)

	w := postJSON(srv, "/api/v1/identities", map[string]any{
		"identity_type": "human",
		"display_name":  "Alice",
		"credential":    "alice-secret",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d body=%s", w.Code, w.Body.String())
	}
	var ident identity.Identity
	if err := json.Unmarshal(w.Body.Bytes(), &ident); err != nil {
		t.Fatal(err)
	}
	if ident.SchemaVersion == "" || ident.EntityType != "identity" || ident.CreatedAt.IsZero() {
		t.Errorf("envelope missing: %+v", ident)
	}
	if !ident.Provenance.Valid() {
		t.Errorf("provenance missing: %+v", ident.Provenance)
	}

	// One-step onboarding: the credential authenticates against the record.
	if _, err := auth.Authenticate(ident.ID, []byte("alice-secret")); err != nil {
		t.Errorf("credential must authenticate: %v", err)
	}
	// Wrong credential still denied.
	if _, err := auth.Authenticate(ident.ID, []byte("nope")); err == nil {
		t.Error("wrong credential must be denied")
	}

	// System identities are runtime-created, not API-created.
	if w := postJSON(srv, "/api/v1/identities", map[string]any{
		"identity_type": "system", "display_name": "Core",
	}); w.Code != http.StatusBadRequest {
		t.Errorf("system type: expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	// Unknown type rejected.
	if w := postJSON(srv, "/api/v1/identities", map[string]any{
		"identity_type": "wizard", "display_name": "X",
	}); w.Code != http.StatusBadRequest {
		t.Errorf("bad type: expected 400, got %d", w.Code)
	}
	// Invalid creation status rejected (only active/pending).
	if w := postJSON(srv, "/api/v1/identities", map[string]any{
		"identity_type": "human", "display_name": "X", "status": "revoked",
	}); w.Code != http.StatusBadRequest {
		t.Errorf("bad status: expected 400, got %d", w.Code)
	}
}

// TEST-GW-IDN-02: business bootstrap + scoped identity creation, membership
// handoff, fail-closed identity listing, duplicate conflict.
func TestGatewayBusinessAndScopedIdentity(t *testing.T) {
	reg, _, members, srv := orgGateway(t)

	_, b := bootstrapOrg(t, srv)
	if b.BusinessID != b.EntityID || b.Status != identity.BusinessActive {
		t.Errorf("business self-ref/status: %+v", b)
	}

	// Scoped identity inside the business: auto-membership, list scoped.
	w := postJSON(srv, "/api/v1/identities", map[string]any{
		"identity_type": "agent",
		"display_name":  "Worker",
		"business_id":   b.BusinessID,
		"role":          "WORKER",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("scoped create: %d body=%s", w.Code, w.Body.String())
	}
	var agent identity.Identity
	if err := json.Unmarshal(w.Body.Bytes(), &agent); err != nil {
		t.Fatal(err)
	}
	if agent.BusinessID != b.BusinessID {
		t.Errorf("business_id not stamped: %+v", agent)
	}
	if !members.IsMember(agent.ID, b.BusinessID, "") {
		t.Error("created identity must hold an active membership")
	}

	// List: fail-closed without business_id; scoped with it.
	if w := getOrg(srv, "/api/v1/identities"); w.Code != http.StatusBadRequest {
		t.Errorf("list without business_id: expected 400, got %d", w.Code)
	}
	w = getOrg(srv, "/api/v1/identities?business_id="+b.BusinessID)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d body=%s", w.Code, w.Body.String())
	}
	var list struct {
		Identities []identity.Identity `json:"identities"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Identities) != 1 || list.Identities[0].ID != agent.ID {
		t.Errorf("business list: got %d", len(list.Identities))
	}

	// Scoped identity in an unknown business → 400 (registry §8).
	if w := postJSON(srv, "/api/v1/identities", map[string]any{
		"identity_type": "agent", "display_name": "Orphan",
		"business_id": "biz-missing",
	}); w.Code != http.StatusBadRequest {
		t.Errorf("unknown business: expected 400, got %d body=%s", w.Code, w.Body.String())
	}

	// Duplicate entity_id → 409.
	if w := postJSON(srv, "/api/v1/identities", map[string]any{
		"entity_id": agent.ID, "identity_type": "agent",
		"display_name": "Again", "business_id": b.BusinessID,
	}); w.Code != http.StatusConflict {
		t.Errorf("duplicate: expected 409, got %d body=%s", w.Code, w.Body.String())
	}
	if _, ok := reg.GetIdentity(agent.ID); !ok {
		t.Error("identity lost")
	}
}

// TEST-GW-IDN-03: lifecycle transitions — suspend kills authentication,
// activate restores it, revoke is terminal (409), unknown id 404.
func TestGatewayIdentityTransitions(t *testing.T) {
	_, auth, _, srv := orgGateway(t)

	w := postJSON(srv, "/api/v1/identities", map[string]any{
		"identity_type": "human",
		"display_name":  "Temp",
		"credential":    "pw",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d", w.Code)
	}
	var ident identity.Identity
	if err := json.Unmarshal(w.Body.Bytes(), &ident); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Authenticate(ident.ID, []byte("pw")); err != nil {
		t.Fatalf("precondition auth: %v", err)
	}

	// suspend → auth denied.
	if w := postJSON(srv, "/api/v1/identities/"+ident.ID+"/suspend", nil); w.Code != http.StatusOK {
		t.Fatalf("suspend: %d body=%s", w.Code, w.Body.String())
	}
	if _, err := auth.Authenticate(ident.ID, []byte("pw")); err == nil {
		t.Error("suspended identity must not authenticate")
	}

	// activate → auth restored.
	if w := postJSON(srv, "/api/v1/identities/"+ident.ID+"/activate", nil); w.Code != http.StatusOK {
		t.Fatalf("activate: %d body=%s", w.Code, w.Body.String())
	}
	if _, err := auth.Authenticate(ident.ID, []byte("pw")); err != nil {
		t.Errorf("reactivated identity must authenticate: %v", err)
	}

	// revoke → terminal: activate afterwards is 409.
	if w := postJSON(srv, "/api/v1/identities/"+ident.ID+"/revoke", nil); w.Code != http.StatusOK {
		t.Fatalf("revoke: %d body=%s", w.Code, w.Body.String())
	}
	if _, err := auth.Authenticate(ident.ID, []byte("pw")); err == nil {
		t.Error("revoked identity must not authenticate")
	}
	if w := postJSON(srv, "/api/v1/identities/"+ident.ID+"/activate", nil); w.Code != http.StatusConflict {
		t.Errorf("revoked terminal: expected 409, got %d body=%s", w.Code, w.Body.String())
	}

	// Unknown id → 404.
	if w := postJSON(srv, "/api/v1/identities/nx:human:missing/revoke", nil); w.Code != http.StatusNotFound {
		t.Errorf("unknown: expected 404, got %d", w.Code)
	}
}

// TEST-GW-IDN-04: division create enforces §8 references through the API and
// archived businesses refuse new divisions; archived is terminal.
func TestGatewayDivisions(t *testing.T) {
	_, _, _, srv := orgGateway(t)

	ownerID, b := bootstrapOrg(t, srv)

	// Division list fails closed without business_id.
	if w := getOrg(srv, "/api/v1/divisions"); w.Code != http.StatusBadRequest {
		t.Errorf("list without business_id: expected 400, got %d", w.Code)
	}

	// Happy division: parent == business, business.Divisions updated.
	w := postJSON(srv, "/api/v1/divisions", map[string]any{
		"business_id": b.BusinessID, "name": "Engineering",
		"owner_identity_id": ownerID,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("division create: %d body=%s", w.Code, w.Body.String())
	}
	var div identity.Division
	if err := json.Unmarshal(w.Body.Bytes(), &div); err != nil {
		t.Fatal(err)
	}
	if div.ParentBusinessID != b.BusinessID || div.BusinessID != b.BusinessID {
		t.Errorf("division refs: %+v", div)
	}
	w = getOrg(srv, "/api/v1/businesses/"+b.EntityID)
	var bRef identity.Business
	if err := json.Unmarshal(w.Body.Bytes(), &bRef); err != nil {
		t.Fatal(err)
	}
	if len(bRef.Divisions) != 1 || bRef.Divisions[0] != div.EntityID {
		t.Errorf("business.divisions not updated: %+v", bRef.Divisions)
	}

	// Division in unknown business → 400.
	if w := postJSON(srv, "/api/v1/divisions", map[string]any{
		"business_id": "biz-missing", "name": "Ghost",
		"owner_identity_id": ownerID,
	}); w.Code != http.StatusBadRequest {
		t.Errorf("unknown business: expected 400, got %d", w.Code)
	}
	// Cross-scope owner (scoped to another business) → 400.
	_, otherBiz := bootstrapOrg(t, srv)
	w = postJSON(srv, "/api/v1/identities", map[string]any{
		"identity_type": "human", "display_name": "Foreign Owner",
		"business_id": otherBiz.BusinessID,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("foreign owner create: %d body=%s", w.Code, w.Body.String())
	}
	var foreignOwner identity.Identity
	if err := json.Unmarshal(w.Body.Bytes(), &foreignOwner); err != nil {
		t.Fatal(err)
	}
	if w := postJSON(srv, "/api/v1/divisions", map[string]any{
		"business_id": b.BusinessID, "name": "Sneaky",
		"owner_identity_id": foreignOwner.ID,
	}); w.Code != http.StatusBadRequest {
		t.Errorf("cross-business owner: expected 400, got %d body=%s", w.Code, w.Body.String())
	}

	// Archive the business: terminal, and new divisions are refused.
	if w := postJSON(srv, "/api/v1/businesses/"+b.EntityID+"/archive", nil); w.Code != http.StatusOK {
		t.Fatalf("archive: %d body=%s", w.Code, w.Body.String())
	}
	if w := postJSON(srv, "/api/v1/businesses/"+b.EntityID+"/activate", nil); w.Code != http.StatusConflict {
		t.Errorf("archived terminal: expected 409, got %d", w.Code)
	}
	if w := postJSON(srv, "/api/v1/divisions", map[string]any{
		"business_id": b.BusinessID, "name": "Late",
		"owner_identity_id": ownerID,
	}); w.Code != http.StatusBadRequest {
		t.Errorf("division in archived business: expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// TEST-GW-IDN-05: without a registry the org surface fails closed (503) —
// records are never fabricated.
func TestGatewayOrgWithoutRegistry(t *testing.T) {
	engine, err := core.NewEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Stop(ctx) })
	srv := NewServer(engine, ":0")

	w := getOrg(srv, "/api/v1/identities?business_id=biz-1")
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
	if errBody := decodeErrorBody(t, w); errBody["category"] != "DEPENDENCY_FAILURE" {
		t.Errorf("expected DEPENDENCY_FAILURE, got %v", errBody["category"])
	}
}

// TEST-GW-IDN-06: identity enforcement on the org surface — missing
// credentials 401, non-member 403, foreign records hidden behind 404, and
// the business directory is membership-filtered.
func TestGatewayOrgIdentityEnforcement(t *testing.T) {
	auth := identity.NewLocalAuthenticator()
	if err := auth.Register("alice", security.HashCredential([]byte("alice-secret")), identity.AuthMethodToken); err != nil {
		t.Fatal(err)
	}
	if err := auth.Register("mallory", security.HashCredential([]byte("mallory-secret")), identity.AuthMethodToken); err != nil {
		t.Fatal(err)
	}
	members := identity.NewMembershipSet()
	if err := members.Add(identity.Membership{
		IdentityID: "alice", BusinessID: "biz-1",
		Role: identity.RoleMember, Status: identity.StatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	if err := members.Add(identity.Membership{
		IdentityID: "mallory", BusinessID: "biz-9",
		Role: identity.RoleMember, Status: identity.StatusActive,
	}); err != nil {
		t.Fatal(err)
	}

	engine, err := core.NewEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Stop(ctx) })

	reg := identity.NewRegistry("nx:nexus:test")
	auth.SetRegistry(reg)
	// Production posture: authentication requires an identity record, so seed
	// the actors through the registry (the API path does this in real flows).
	for _, actor := range []string{"alice", "mallory"} {
		if _, err := reg.CreateIdentity(identity.Identity{
			ID: actor, Type: identity.TypeHuman, DisplayName: actor,
		}, "test"); err != nil {
			t.Fatalf("seed actor %s: %v", actor, err)
		}
	}
	srv := NewServer(engine, ":0",
		WithRegistry(reg),
		WithIdentity(auth, members),
		WithRequireAuthentication(true),
		WithEnforceBusinessScope(true),
	)

	// Seed: owner record via the registry (tests the registry API directly),
	// then alice bootstraps her business over HTTP (membership-free bootstrap).
	regOwner, err := reg.CreateIdentity(identity.Identity{
		Type: identity.TypeHuman, DisplayName: "Owner", ID: "nx:human:owner-x",
	}, "test")
	if err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	w := postJSON(srv, "/api/v1/businesses", map[string]any{
		"name": "Acme", "owner_identity_id": regOwner.ID,
	})
	// postJSON sends no credentials — enforcement must 401 first.
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauth business create: expected 401, got %d body=%s", w.Code, w.Body.String())
	}
	// With alice's credentials (entity_id matches her seeded membership):
	raw, _ := json.Marshal(map[string]any{
		"entity_id": "biz-1", "name": "Acme", "owner_identity_id": regOwner.ID,
	})
	req := httptest.NewRequest("POST", "/api/v1/businesses", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	a6AuthHeaders(req, "alice", "alice-secret")
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("business create by alice: %d body=%s", w.Code, w.Body.String())
	}
	var biz identity.Business
	if err := json.Unmarshal(w.Body.Bytes(), &biz); err != nil {
		t.Fatal(err)
	}

	// Helper for identity paths.
	call := func(method, path, actor, cred string, body any) *httptest.ResponseRecorder {
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if actor != "" {
			a6AuthHeaders(req, actor, cred)
		}
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		return w
	}

	// Missing credentials → 401 (fail closed).
	if w := call("GET", "/api/v1/identities?business_id=biz-1", "", "", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("unauth list: expected 401, got %d", w.Code)
	}
	// Authenticated non-member → 403.
	if w := call("GET", "/api/v1/identities?business_id=biz-1", "mallory", "mallory-secret", nil); w.Code != http.StatusForbidden {
		t.Errorf("non-member list: expected 403, got %d", w.Code)
	}
	// Member → 200.
	if w := call("GET", "/api/v1/identities?business_id=biz-1", "alice", "alice-secret", nil); w.Code != http.StatusOK {
		t.Errorf("member list: expected 200, got %d", w.Code)
	}

	// Creating in a foreign business → 403.
	if w := call("POST", "/api/v1/identities", "mallory", "mallory-secret", map[string]any{
		"identity_type": "agent", "display_name": "Spy", "business_id": biz.BusinessID,
	}); w.Code != http.StatusForbidden {
		t.Errorf("cross-tenant create: expected 403, got %d", w.Code)
	}
	// Member create → 200.
	w = call("POST", "/api/v1/identities", "alice", "alice-secret", map[string]any{
		"identity_type": "agent", "display_name": "Worker", "business_id": biz.BusinessID,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("member create: %d body=%s", w.Code, w.Body.String())
	}
	var created identity.Identity
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	// Foreign record hidden behind 404 (no existence leak).
	if w := call("GET", "/api/v1/identities/"+created.ID, "mallory", "mallory-secret", nil); w.Code != http.StatusNotFound {
		t.Errorf("foreign get: expected 404, got %d", w.Code)
	}
	// Foreign transition also hidden + refused.
	if w := call("POST", "/api/v1/identities/"+created.ID+"/revoke", "mallory", "mallory-secret", nil); w.Code != http.StatusNotFound {
		t.Errorf("foreign revoke: expected 404, got %d", w.Code)
	}

	// Business directory: alice sees her business, mallory sees none.
	w = call("GET", "/api/v1/businesses", "alice", "alice-secret", nil)
	var bizList struct {
		Businesses []identity.Business `json:"businesses"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &bizList); err != nil {
		t.Fatal(err)
	}
	if len(bizList.Businesses) != 1 || bizList.Businesses[0].EntityID != biz.EntityID {
		t.Errorf("alice business dir: got %d", len(bizList.Businesses))
	}
	w = call("GET", "/api/v1/businesses", "mallory", "mallory-secret", nil)
	if err := json.Unmarshal(w.Body.Bytes(), &bizList); err != nil {
		t.Fatal(err)
	}
	if len(bizList.Businesses) != 0 {
		t.Errorf("mallory business dir must be empty, got %d", len(bizList.Businesses))
	}
}
