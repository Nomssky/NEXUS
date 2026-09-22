package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/security"
)

// a6Fixture builds a gateway with identity enforcement on and a single
// authenticated actor who is an active member of memberBusiness.
func a6Fixture(t *testing.T, actorID, credential string, memberBusiness string, extraMemberships ...identity.Membership) *Server {
	t.Helper()

	now := time.Now()
	engine, err := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { engine.Stop(ctx) })

	auth := identity.NewLocalAuthenticator()
	if actorID != "" && credential != "" {
		if err := auth.Register(actorID, security.HashCredential([]byte(credential)), identity.AuthMethodToken); err != nil {
			t.Fatalf("register credential: %v", err)
		}
	}

	members := identity.NewMembershipSet()
	if actorID != "" && memberBusiness != "" {
		if err := members.Add(identity.Membership{
			IdentityID: actorID,
			BusinessID: memberBusiness,
			Role:       identity.RoleMember,
			Status:     identity.StatusActive,
		}); err != nil {
			t.Fatalf("add membership: %v", err)
		}
	}
	for _, m := range extraMemberships {
		if err := members.Add(m); err != nil {
			t.Fatalf("add extra membership: %v", err)
		}
	}

	return NewServer(engine, ":0",
		WithClock(func() time.Time { return now }),
		WithIdentity(auth, members),
		WithRequireAuthentication(true),
		WithEnforceBusinessScope(true),
	)
}

func a6AuthHeaders(req *http.Request, actorID, credential string) {
	req.Header.Set("X-Actor-ID", actorID)
	req.Header.Set("X-Actor-Credential", credential)
}

func a6Submit(t *testing.T, srv *Server, actorID, credential, businessID string) string {
	t.Helper()
	body := `{"intent": "a6", "business_id": "` + businessID + `", "actor_id": "` + actorID + `"}`
	req := httptest.NewRequest("POST", "/api/v1/requests", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	a6AuthHeaders(req, actorID, credential)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("submit: expected 202, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	return resp["request_id"]
}

// A6-01: authenticated actor not a member of target business_id → 403 on result GET.
func TestA6ResultNonMemberDenied(t *testing.T) {
	srv := a6Fixture(t, "nx:human:alice", "alice-secret", "biz-A")

	reqID := a6Submit(t, srv, "nx:human:alice", "alice-secret", "biz-A")
	time.Sleep(100 * time.Millisecond)

	// Same authenticated actor claims foreign business_id they are not in.
	req := httptest.NewRequest("GET", "/api/v1/requests/"+reqID+"?business_id=biz-B", nil)
	a6AuthHeaders(req, "nx:human:alice", "alice-secret")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member, got %d", w.Code)
	}
	var errResp map[string]map[string]interface{}
	json.NewDecoder(w.Body).Decode(&errResp)
	if errResp["error"]["category"] != "AUTHORIZATION" {
		t.Errorf("expected AUTHORIZATION, got %v", errResp["error"]["category"])
	}
}

// A6-02: member of A requests business B result → 403; SSE for B → 403 at subscribe.
func TestA6CrossBusinessResultAndSSE(t *testing.T) {
	srv := a6Fixture(t, "nx:human:bob", "bob-secret", "biz-A")

	reqID := a6Submit(t, srv, "nx:human:bob", "bob-secret", "biz-A")
	time.Sleep(100 * time.Millisecond)

	// Result for foreign business → 403
	req := httptest.NewRequest("GET", "/api/v1/requests/"+reqID+"?business_id=biz-B", nil)
	a6AuthHeaders(req, "nx:human:bob", "bob-secret")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("result foreign biz: expected 403, got %d", w.Code)
	}

	// SSE for foreign business → denied at subscribe (403)
	sse := httptest.NewRequest("GET", "/events?business_id=biz-B", nil)
	a6AuthHeaders(sse, "nx:human:bob", "bob-secret")
	sw := httptest.NewRecorder()
	srv.Handler().ServeHTTP(sw, sse)
	if sw.Code != http.StatusForbidden {
		t.Fatalf("sse foreign biz: expected 403, got %d", sw.Code)
	}
}

// A6-03: missing or invalid credential → 401 on scoped endpoints.
func TestA6MissingOrInvalidCredential(t *testing.T) {
	srv := a6Fixture(t, "nx:human:carol", "carol-secret", "biz-A")

	// Missing credentials
	req := httptest.NewRequest("GET", "/api/v1/requests/any?business_id=biz-A", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing cred: expected 401, got %d", w.Code)
	}

	// Invalid credential
	req2 := httptest.NewRequest("GET", "/api/v1/requests/any?business_id=biz-A", nil)
	a6AuthHeaders(req2, "nx:human:carol", "wrong-secret")
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("invalid cred: expected 401, got %d", w2.Code)
	}

	// Unknown actor with any credential
	req3 := httptest.NewRequest("GET", "/api/v1/requests/any?business_id=biz-A", nil)
	a6AuthHeaders(req3, "nx:human:unknown", "whatever")
	w3 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w3, req3)
	if w3.Code != http.StatusUnauthorized {
		t.Fatalf("unknown actor: expected 401, got %d", w3.Code)
	}
}

// A6-04: SSE subscribe with foreign business_id is denied (403) — no stream.
func TestA6SSEForeignBusinessDenied(t *testing.T) {
	srv := a6Fixture(t, "nx:human:dave", "dave-secret", "biz-A")

	req := httptest.NewRequest("GET", "/events?business_id=biz-foreign", nil)
	a6AuthHeaders(req, "nx:human:dave", "dave-secret")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for foreign SSE scope, got %d", w.Code)
	}
}

// A6-05: submit with actor_id ≠ authenticated identity → 403.
func TestA6SubmitActorMismatchDenied(t *testing.T) {
	srv := a6Fixture(t, "nx:human:eve", "eve-secret", "biz-A")

	// Authenticated as eve, but claims actor_id mallory.
	body := `{"intent": "spoof", "business_id": "biz-A", "actor_id": "nx:human:mallory"}`
	req := httptest.NewRequest("POST", "/api/v1/requests", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	a6AuthHeaders(req, "nx:human:eve", "eve-secret")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for actor_id mismatch, got %d", w.Code)
	}
}

// A6-06: empty membership store → fail-closed deny (403) even with valid credentials.
func TestA6EmptyMembershipStoreDenied(t *testing.T) {
	srv := a6Fixture(t, "nx:human:frank", "frank-secret", "" /* no memberships */)

	// Valid credentials, but membership store has no entries for this actor.
	req := httptest.NewRequest("GET", "/api/v1/requests/any?business_id=biz-A", nil)
	a6AuthHeaders(req, "nx:human:frank", "frank-secret")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 with empty membership store, got %d", w.Code)
	}

	// Submit also denied
	body := `{"intent": "x", "business_id": "biz-A", "actor_id": "nx:human:frank"}`
	sreq := httptest.NewRequest("POST", "/api/v1/requests", bytes.NewBufferString(body))
	sreq.Header.Set("Content-Type", "application/json")
	a6AuthHeaders(sreq, "nx:human:frank", "frank-secret")
	sw := httptest.NewRecorder()
	srv.Handler().ServeHTTP(sw, sreq)
	if sw.Code != http.StatusForbidden {
		t.Fatalf("submit with empty memberships: expected 403, got %d", sw.Code)
	}
}

// A6-07: happy path via production Handler() — member can submit and read own result.
func TestA6MemberAllowedViaHandler(t *testing.T) {
	srv := a6Fixture(t, "nx:human:grace", "grace-secret", "biz-A")

	reqID := a6Submit(t, srv, "nx:human:grace", "grace-secret", "biz-A")

	// Poll until the result is available (race builds are slower than 50ms).
	deadline := time.Now().Add(2 * time.Second)
	var w *httptest.ResponseRecorder
	for {
		req := httptest.NewRequest("GET", "/api/v1/requests/"+reqID+"?business_id=biz-A", nil)
		a6AuthHeaders(req, "nx:human:grace", "grace-secret")
		w = httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code == http.StatusOK {
			break
		}
		if w.Code != http.StatusNotFound {
			t.Fatalf("member result: expected 200 or transient 404, got %d body=%s", w.Code, w.Body.String())
		}
		if time.Now().After(deadline) {
			t.Fatalf("member result: timed out waiting for result, last=%d body=%s", w.Code, w.Body.String())
		}
		time.Sleep(25 * time.Millisecond)
	}

	var result core.Response
	json.NewDecoder(w.Body).Decode(&result)
	if result.BusinessID != "biz-A" {
		t.Errorf("expected biz-A, got %s", result.BusinessID)
	}
}

// A6: enforcement off — identity middleware is inert (backward compatibility).
func TestA6EnforcementOffSkipsIdentity(t *testing.T) {
	now := time.Now()
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop(ctx)

	// No identity options: requireAuth/enforceBusinessScope default false.
	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	body := `{"intent": "legacy", "business_id": "biz-1", "actor_id": "user-1"}`
	req := httptest.NewRequest("POST", "/api/v1/requests", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 with identity off, got %d", w.Code)
	}
}

// A6: enforcement on but no authenticator configured → fail-closed 401.
func TestA6EnforcedWithoutAuthenticatorDenied(t *testing.T) {
	now := time.Now()
	engine, _ := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	engine.Start(ctx)
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0",
		WithClock(func() time.Time { return now }),
		WithRequireAuthentication(true),
		// No WithIdentity — authenticator nil
	)

	req := httptest.NewRequest("GET", "/api/v1/requests/x?business_id=biz-1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when enforced without authenticator, got %d", w.Code)
	}
}
