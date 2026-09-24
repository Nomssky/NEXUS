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
)

// G-009: when identity enforcement is off, client-asserted actor_id must not
// be trusted as the request identity (identity spoofing). A fixed marker is
// bound instead. When enforcement is on, the A6-verified identity is bound.

// g009Fixture builds a gateway with identity enforcement off (no identity
// options), matching the non-production opt-out configuration.
func g009Fixture(t *testing.T) *Server {
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
	return NewServer(engine, ":0", WithClock(func() time.Time { return now }))
}

// g009Submit posts a submit body via the production Handler and returns the
// decoded response map plus the HTTP status.
func g009Submit(t *testing.T, srv *Server, body string) (map[string]string, int) {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/requests", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	var resp map[string]string
	if w.Body.Len() > 0 {
		json.NewDecoder(w.Body).Decode(&resp)
	}
	return resp, w.Code
}

// G-009-01: enforcement off + spoofed client actor_id → 202 (compat) but the
// bound identity must be the unauthenticated marker, never the claimed value.
func TestG009EnforcementOffDiscardsClientActorID(t *testing.T) {
	srv := g009Fixture(t)

	resp, code := g009Submit(t, srv,
		`{"intent": "spoof", "business_id": "biz-1", "actor_id": "spoof-admin"}`)

	if code != http.StatusAccepted {
		t.Fatalf("expected 202 (legacy submit still accepted), got %d body=%v", code, resp)
	}
	if resp["actor_id"] != unauthenticatedActorID {
		t.Errorf("bound actor_id: want %q, got %q — client actor_id must not be trusted without authentication",
			unauthenticatedActorID, resp["actor_id"])
	}
	if resp["actor_id"] == "spoof-admin" {
		t.Errorf("identity spoofing: client-asserted actor_id leaked into bound identity %q", resp["actor_id"])
	}
}

// G-009-02: enforcement on + authenticated matching actor → bound identity is
// the A6-verified identity (unchanged from A6).
func TestG009EnforcedSubmitBindsAuthenticatedIdentity(t *testing.T) {
	srv := a6Fixture(t, "nx:human:grace", "grace-secret", "biz-A")

	body := `{"intent": "g009", "business_id": "biz-A", "actor_id": "nx:human:grace"}`
	req := httptest.NewRequest("POST", "/api/v1/requests", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	a6AuthHeaders(req, "nx:human:grace", "grace-secret")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for authenticated member, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["actor_id"] != "nx:human:grace" {
		t.Errorf("enforced bound actor_id: want %q, got %q", "nx:human:grace", resp["actor_id"])
	}
}

// G-009-03: empty actor_id is still rejected (validation unchanged — the
// field remains required for API compatibility even though its value is
// untrusted when enforcement is off).
func TestG009EmptyActorIDStillRejected(t *testing.T) {
	srv := g009Fixture(t)

	resp, code := g009Submit(t, srv,
		`{"intent": "x", "business_id": "biz-1", "actor_id": ""}`)

	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty actor_id, got %d body=%v", code, resp)
	}
}

// G-009-04: marker constant is a fixed, non-empty value (chainValidate
// rejects empty ActorID; value must be stable for governance keying).
func TestG009UnauthenticatedMarkerNonEmpty(t *testing.T) {
	if unauthenticatedActorID == "" {
		t.Fatal("unauthenticatedActorID must be non-empty")
	}
}

// G-009-05: unit — submitActorID discards the claim when enforcement is off
// and preserves the A6-verified claim when enforcement is on.
func TestG009SubmitActorIDResolution(t *testing.T) {
	off := &Server{}
	if got := off.submitActorID("spoof-admin"); got != unauthenticatedActorID {
		t.Errorf("enforcement off: want %q, got %q", unauthenticatedActorID, got)
	}

	on := &Server{requireAuth: true}
	if got := on.submitActorID("nx:human:alice"); got != "nx:human:alice" {
		t.Errorf("enforcement on: want verified claim preserved, got %q", got)
	}

	onScope := &Server{enforceBusinessScope: true}
	if got := onScope.submitActorID("nx:human:bob"); got != "nx:human:bob" {
		t.Errorf("enforce_business_scope only: want verified claim preserved, got %q", got)
	}
}
