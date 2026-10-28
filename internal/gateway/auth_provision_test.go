package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
)

// ===========================================================================
// Auth provisioning contract tests (mission: first-run + auth hardening)
//
// These tests exercise the server-side contracts the first-run frontend
// depends on: verify via authenticated business-scoped endpoint, create
// business, establish membership. No client-side session is ever established
// without a successful server response.
// ===========================================================================

// provisionFixture builds a gateway with identity enforcement, a Registry,
// authenticator bound to the Registry, and memberships — the production
// posture for first-run provisioning.
func provisionFixture(t *testing.T, actorID, credential, memberBiz string) (*identity.Registry, *identity.LocalAuthenticator, *identity.MembershipSet, *Server) {
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
	if err := auth.Register(actorID, security.HashCredential([]byte(credential)), identity.AuthMethodPassword); err != nil {
		t.Fatalf("register: %v", err)
	}
	// Add the identity to the Registry so directory lookup (active status)
	// succeeds — production posture where credential alone is not enough.
	if _, err := reg.CreateIdentity(identity.Identity{
		ID:          actorID,
		Type:        identity.TypeHuman,
		DisplayName: actorID,
		Status:      identity.StatusActive,
		Scope:       identity.GlobalScope(),
		CreatedAt:   time.Now().UTC(),
	}, "runtime:test"); err != nil {
		t.Fatalf("create identity: %v", err)
	}

	members := identity.NewMembershipSet()
	if memberBiz != "" {
		if err := members.Add(identity.Membership{
			IdentityID: actorID,
			BusinessID: memberBiz,
			Role:       identity.RoleMember,
			Status:     identity.StatusActive,
		}); err != nil {
			t.Fatalf("membership: %v", err)
		}
	}

	return reg, auth, members, NewServer(engine, ":0",
		WithRegistry(reg),
		WithIdentity(auth, members),
		WithRequireAuthentication(true),
		WithEnforceBusinessScope(true),
	)
}

// authCreateBusiness posts a create-business request; returns recorder.
func authCreateBusiness(srv *Server, actorID, credential string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/v1/businesses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	a6AuthHeaders(req, actorID, credential)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

// authCreateBusinessMember posts a membership request; returns recorder.
func authCreateBusinessMember(srv *Server, actorID, credential, businessID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/v1/businesses/"+businessID+"/members", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	a6AuthHeaders(req, actorID, credential)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

// AuthProvisionValidCredentials: valid bootstrap credentials can create a
// business and establish membership (server-verified first-run flow).
func TestAuthProvisionValidCredentials(t *testing.T) {
	_, _, _, srv := provisionFixture(t, "nx:human:grace", "grace-secret", "biz-A")

	w := authCreateBusiness(srv, "nx:human:grace", "grace-secret",
		`{"entity_id":"biz-new","name":"New Business","owner_identity_id":"nx:human:grace"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("create business: expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var created struct {
		BusinessID      string `json:"business_id"`
		OwnerIdentityID string `json:"owner_identity_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.BusinessID != "biz-new" {
		t.Errorf("expected biz-new, got %s", created.BusinessID)
	}
	if created.OwnerIdentityID != "nx:human:grace" {
		t.Errorf("expected owner nx:human:grace, got %s", created.OwnerIdentityID)
	}

	// Membership can then be claimed by the owner.
	mw := authCreateBusinessMember(srv, "nx:human:grace", "grace-secret", created.BusinessID)
	if mw.Code != http.StatusOK && mw.Code != http.StatusConflict {
		t.Fatalf("create member: expected 200 or 409, got %d body=%s", mw.Code, mw.Body.String())
	}
}

// AuthProvisionInvalidCredentials: invalid credentials are rejected 401 with
// no business/session created.
func TestAuthProvisionInvalidCredentials(t *testing.T) {
	_, _, _, srv := provisionFixture(t, "nx:human:grace", "grace-secret", "biz-A")

	for _, cred := range []string{"wrong-secret", "", "anything-at-all"} {
		w := authCreateBusiness(srv, "nx:human:grace", cred,
			`{"entity_id":"biz-bad","name":"Bad","owner_identity_id":"nx:human:grace"}`)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("credential %q: expected 401, got %d body=%s", cred, w.Code, w.Body.String())
		}
	}
}

// AuthProvisionUnknownActor: unknown actor cannot create a business (401).
func TestAuthProvisionUnknownActor(t *testing.T) {
	_, _, _, srv := provisionFixture(t, "nx:human:grace", "grace-secret", "biz-A")

	w := authCreateBusiness(srv, "nx:human:imposter", "grace-secret",
		`{"entity_id":"biz-imposter","name":"Imposter","owner_identity_id":"nx:human:imposter"}`)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unknown actor: expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}

// AuthProvisionMissingCredentials: missing credential header → 401, no business.
func TestAuthProvisionMissingCredentials(t *testing.T) {
	_, _, _, srv := provisionFixture(t, "nx:human:grace", "grace-secret", "biz-A")

	req := httptest.NewRequest("POST", "/api/v1/businesses", strings.NewReader(
		`{"entity_id":"biz-nocred","name":"NoCred","owner_identity_id":"nx:human:grace"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Actor-ID", "nx:human:grace")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("missing credential: expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}

// AuthProvisionDuplicateBusinessIdempotent: creating the same business twice
// does not create duplicates — second create returns 409 conflict.
func TestAuthProvisionDuplicateBusinessIdempotent(t *testing.T) {
	_, _, _, srv := provisionFixture(t, "nx:human:grace", "grace-secret", "biz-A")

	body := `{"entity_id":"biz-dup","name":"Duplicate","owner_identity_id":"nx:human:grace"}`

	w1 := authCreateBusiness(srv, "nx:human:grace", "grace-secret", body)
	if w1.Code != http.StatusOK {
		t.Fatalf("first create: expected 200, got %d body=%s", w1.Code, w1.Body.String())
	}

	w2 := authCreateBusiness(srv, "nx:human:grace", "grace-secret", body)
	if w2.Code != http.StatusConflict {
		t.Errorf("second create (same id): expected 409 conflict, got %d body=%s", w2.Code, w2.Body.String())
	}
}

// AuthProvisionAuthFailNoFalseSession: a failed provisioning (bad credential)
// leaves no authenticated session; good credentials still work afterward.
func TestAuthProvisionAuthFailNoFalseSession(t *testing.T) {
	_, _, _, srv := provisionFixture(t, "nx:human:grace", "grace-secret", "biz-A")

	// First attempt with bad credential → 401.
	w1 := authCreateBusiness(srv, "nx:human:grace", "bad",
		`{"entity_id":"biz-session","name":"Session","owner_identity_id":"nx:human:grace"}`)
	if w1.Code != http.StatusUnauthorized {
		t.Fatalf("first bad attempt: expected 401, got %d body=%s", w1.Code, w1.Body.String())
	}

	// Second attempt with same bad credential → still 401.
	w2 := authCreateBusiness(srv, "nx:human:grace", "bad",
		`{"entity_id":"biz-session","name":"Session","owner_identity_id":"nx:human:grace"}`)
	if w2.Code != http.StatusUnauthorized {
		t.Errorf("second bad attempt: expected 401, got %d body=%s", w2.Code, w2.Body.String())
	}

	// Good credential still works after failures (server state not poisoned).
	w3 := authCreateBusiness(srv, "nx:human:grace", "grace-secret",
		`{"entity_id":"biz-session-ok","name":"OK","owner_identity_id":"nx:human:grace"}`)
	if w3.Code != http.StatusOK {
		t.Errorf("good credential after failures: expected 200, got %d body=%s", w3.Code, w3.Body.String())
	}
}

// AuthValidatedBusinessScope: authenticated caller cannot access a business
// outside their membership scope via protected endpoints.
func TestAuthValidatedBusinessScope(t *testing.T) {
	_, _, _, srv := provisionFixture(t, "nx:human:grace", "grace-secret", "biz-A")

	foreign := httptest.NewRequest("GET", "/api/v1/businesses/biz-foreign", nil)
	a6AuthHeaders(foreign, "nx:human:grace", "grace-secret")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, foreign)

	if w.Code == http.StatusOK {
		t.Errorf("foreign business access: expected 404/403, got 200 body=%s", w.Body.String())
	}
}

// AuthValidCredentialsServerVerified: /api/v1/approvals (authenticated,
// business-scoped) verifies valid bootstrap credentials; 200 means the
// server accepted them.
func TestAuthValidCredentialsServerVerified(t *testing.T) {
	_, _, _, srv := provisionFixture(t, "nx:human:grace", "grace-secret", "biz-A")

	req := httptest.NewRequest("GET", "/api/v1/approvals?business_id=biz-A", nil)
	a6AuthHeaders(req, "nx:human:grace", "grace-secret")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("valid creds on approved endpoint: expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

// AuthFailureEnvelopeUniform: failed auth returns a uniform error envelope
// that never leaks whether actor ID or credential was at fault.
func TestAuthFailureEnvelopeUniform(t *testing.T) {
	_, _, _, srv := provisionFixture(t, "nx:human:grace", "grace-secret", "biz-A")

	for _, tc := range []struct {
		name string
		act  string
		cred string
	}{
		{"wrong credential", "nx:human:grace", "wrong"},
		{"unknown actor", "nx:human:unknown", "anything"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/v1/approvals?business_id=biz-A", nil)
			a6AuthHeaders(req, tc.act, tc.cred)
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("%s: expected 401, got %d", tc.name, w.Code)
			}
			var env struct {
				Error struct {
					Code     string `json:"code"`
					Category string `json:"category"`
					Message  string `json:"message"`
				} `json:"error"`
			}
			if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
				t.Fatalf("%s: decode envelope: %v", tc.name, err)
			}
			if env.Error.Code != "UNAUTHORIZED" || env.Error.Category != "AUTH" {
				t.Errorf("%s: expected UNAUTHORIZED/AUTH envelope, got %+v", tc.name, env.Error)
			}
			if strings.Contains(env.Error.Message, tc.cred) || strings.Contains(env.Error.Message, tc.act) {
				t.Errorf("%s: error message leaks input: %q", tc.name, env.Error.Message)
			}
		})
	}
}

// AuthProvisionPartialFailureNoMembership: a business can be created but the
// owner may fail to establish membership (e.g. membership endpoint fails).
// Without a business-wide membership, scoped endpoints fail closed — the
// business exists but the owner cannot act within it via membership-scoped
// operations. Retry of the membership step recovers.
func TestAuthProvisionPartialFailureNoMembership(t *testing.T) {
	_, _, _, srv := provisionFixture(t, "nx:human:grace", "grace-secret", "biz-A")

	// Create a business without establishing membership.
	w := authCreateBusiness(srv, "nx:human:grace", "grace-secret",
		`{"entity_id":"biz-nomem","name":"No Membership","owner_identity_id":"nx:human:grace"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("create business: expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	// Without business-wide membership, the owner cannot access scoped
	// endpoints for biz-nomem (no business-wide membership exists).
	req := httptest.NewRequest("GET", "/api/v1/approvals?business_id=biz-nomem", nil)
	a6AuthHeaders(req, "nx:human:grace", "grace-secret")
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, req)

	// 403 (membership not established) — never 200, never an existence leak.
	if w2.Code == http.StatusOK {
		t.Errorf("business without membership: expected not-OK, got 200 body=%s", w2.Body.String())
	}

	// Now establish membership — recovery succeeds.
	mw := authCreateBusinessMember(srv, "nx:human:grace", "grace-secret", "biz-nomem")
	if mw.Code != http.StatusOK && mw.Code != http.StatusConflict {
		t.Fatalf("establish membership after partial failure: expected 200/409, got %d body=%s", mw.Code, mw.Body.String())
	}

	// After membership is established, scoped access works.
	req3 := httptest.NewRequest("GET", "/api/v1/approvals?business_id=biz-nomem", nil)
	a6AuthHeaders(req3, "nx:human:grace", "grace-secret")
	w3 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Errorf("after membership established: expected 200, got %d body=%s", w3.Code, w3.Body.String())
	}
}

// AuthProvisionRetryAfterLostResponse: if the create-business response is
// lost, retrying with the same entity_id returns 409 Conflict (idempotent
// duplicate detection) and the subsequent membership step succeeds.
func TestAuthProvisionRetryAfterLostResponse(t *testing.T) {
	_, _, _, srv := provisionFixture(t, "nx:human:grace", "grace-secret", "biz-A")

	body := `{"entity_id":"biz-retry","name":"Retry","owner_identity_id":"nx:human:grace"}`

	// First request succeeds.
	w1 := authCreateBusiness(srv, "nx:human:grace", "grace-secret", body)
	if w1.Code != http.StatusOK {
		t.Fatalf("first create: expected 200, got %d body=%s", w1.Code, w1.Body.String())
	}

	// Retry (simulating lost response) → 409 Conflict, recoverable.
	w2 := authCreateBusiness(srv, "nx:human:grace", "grace-secret", body)
	if w2.Code != http.StatusConflict {
		t.Errorf("retry create (same id): expected 409 conflict, got %d body=%s", w2.Code, w2.Body.String())
	}

	// Membership step still succeeds (idempotent).
	mw := authCreateBusinessMember(srv, "nx:human:grace", "grace-secret", "biz-retry")
	if mw.Code != http.StatusOK && mw.Code != http.StatusConflict {
		t.Fatalf("create member after retry: expected 200/409, got %d body=%s", mw.Code, mw.Body.String())
	}
}

// AuthProvisionNonOwnerCannotClaimMembership: a different authenticated actor
// cannot claim membership in a business they do not own (403 Forbidden).
func TestAuthProvisionNonOwnerCannotClaimMembership(t *testing.T) {
	reg, auth, members, srv := provisionFixture(t, "nx:human:grace", "grace-secret", "biz-A")

	// Register mallory on the same registry so mallory's credential is valid.
	if err := auth.Register("nx:human:mallory", security.HashCredential([]byte("mallory-secret")), identity.AuthMethodPassword); err != nil {
		t.Fatalf("register mallory: %v", err)
	}
	if _, err := reg.CreateIdentity(identity.Identity{
		ID:          "nx:human:mallory",
		Type:        identity.TypeHuman,
		DisplayName: "nx:human:mallory",
		Status:      identity.StatusActive,
		Scope:       identity.GlobalScope(),
		CreatedAt:   time.Now().UTC(),
	}, "runtime:test"); err != nil {
		t.Fatalf("create mallory identity: %v", err)
	}
	_ = members

	// grace creates biz-owner as owner.
	w := authCreateBusiness(srv, "nx:human:grace", "grace-secret",
		`{"entity_id":"biz-owner","name":"Owner Biz","owner_identity_id":"nx:human:grace"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("create biz-owner: expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	// mallory is authenticated (valid credential) but NOT the owner → 403.
	malloryReq := httptest.NewRequest("POST", "/api/v1/businesses/biz-owner/members", strings.NewReader("{}"))
	malloryReq.Header.Set("Content-Type", "application/json")
	a6AuthHeaders(malloryReq, "nx:human:mallory", "mallory-secret")
	mw := httptest.NewRecorder()
	srv.Handler().ServeHTTP(mw, malloryReq)

	if mw.Code != http.StatusForbidden {
		t.Errorf("non-owner claiming membership: expected 403, got %d body=%s", mw.Code, mw.Body.String())
	}
}

// AuthProvisionNoAutomaticMembership: creating a business does not
// automatically enroll the owner — the membership endpoint is mandatory.
// This is by design: POST /businesses is membership-free (the business does
// not exist yet), so the owner is not automatically enrolled.
func TestAuthProvisionNoAutomaticMembership(t *testing.T) {
	_, _, _, srv := provisionFixture(t, "nx:human:grace", "grace-secret", "biz-A")

	w := authCreateBusiness(srv, "nx:human:grace", "grace-secret",
		`{"entity_id":"biz-noauto","name":"No Auto","owner_identity_id":"nx:human:grace"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("create business: expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	// Without the membership step, scoped access is not available.
	req := httptest.NewRequest("GET", "/api/v1/approvals?business_id=biz-noauto", nil)
	a6AuthHeaders(req, "nx:human:grace", "grace-secret")
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, req)

	if w2.Code == http.StatusOK {
		t.Errorf("no automatic membership: expected not-200, got 200 body=%s", w2.Body.String())
	}
}
