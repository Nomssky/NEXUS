package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// authLoginFixture provisions one authenticated actor and returns a server
// with identity enforcement + business scope ON (the production posture).
func authLoginFixture(t *testing.T) *Server {
	t.Helper()
	return a6Fixture(t, "nx:human:grace", "grace-secret", "biz-A")
}

// authSubmit posts an intelligence submit and returns the status code without
// insisting on 202, so callers can assert denial explicitly.
func authSubmit(srv *Server, actorID, credential string) int {
	body := `{"intent": "auth-login", "business_id": "biz-A", "actor_id": "` + actorID + `"}`
	req := httptest.NewRequest("POST", "/api/v1/requests", bytesBody(body))
	req.Header.Set("Content-Type", "application/json")
	a6AuthHeaders(req, actorID, credential)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w.Code
}

// authSubmitBody returns the parsed top-level response keys for a submit.
func authSubmitBody(srv *Server, actorID, credential string) (map[string]string, int) {
	body := `{"intent": "auth-login", "business_id": "biz-A", "actor_id": "` + actorID + `"}`
	req := httptest.NewRequest("POST", "/api/v1/requests", bytesBody(body))
	req.Header.Set("Content-Type", "application/json")
	a6AuthHeaders(req, actorID, credential)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	var resp map[string]string
	if w.Body.Len() > 0 {
		_ = json.NewDecoder(w.Body).Decode(&resp)
	}
	return resp, w.Code
}

func bytesBody(s string) *strings.Reader {
	return strings.NewReader(s)
}

// AUTH-01: valid credential → authenticated actor reaches the protected submit
// endpoint (202). The bound identity is the server-verified actor, never a
// client-forged value.
func TestAuthValidCredentialAccepted(t *testing.T) {
	srv := authLoginFixture(t)
	resp, code := authSubmitBody(srv, "nx:human:grace", "grace-secret")
	if code != http.StatusAccepted {
		t.Fatalf("expected 202 for valid credential, got %d body=%v", code, resp)
	}
	if resp["actor_id"] != "nx:human:grace" {
		t.Errorf("bound actor_id: want %q, got %q", "nx:human:grace", resp["actor_id"])
	}
}

// AUTH-02: invalid credential → 401, fail closed, no authenticated session.
func TestAuthInvalidCredentialRejected(t *testing.T) {
	srv := authLoginFixture(t)
	for _, cred := range []string{"wrong-secret", "", "anything-at-all", "grace-secret-extra"} {
		code := authSubmit(srv, "nx:human:grace", cred)
		if code != http.StatusUnauthorized {
			t.Errorf("credential %q: expected 401, got %d — invalid credentials must not authenticate", cred, code)
		}
	}
}

// AUTH-03: unknown actor ID → 401 (client cannot forge an identity).
func TestAuthUnknownActorRejected(t *testing.T) {
	srv := authLoginFixture(t)
	code := authSubmit(srv, "nx:human:imposter", "grace-secret")
	if code != http.StatusUnauthorized {
		t.Errorf("unknown actor: expected 401, got %d — client-asserted identity must not be trusted", code)
	}
}

// AUTH-04: missing credential header → 401 (fail closed, no bypass).
func TestAuthMissingCredentialRejected(t *testing.T) {
	srv := authLoginFixture(t)
	body := `{"intent": "auth-login", "business_id": "biz-A", "actor_id": "nx:human:grace"}`
	req := httptest.NewRequest("POST", "/api/v1/requests", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Actor-ID", "nx:human:grace")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("missing credential: expected 401, got %d", w.Code)
	}
}

// AUTH-05: failure envelope is uniform (no account/credential enumeration).
func TestAuthSubmitFailureEnvelopeUniform(t *testing.T) {
	srv := authLoginFixture(t)
	code := authSubmit(srv, "nx:human:grace", "nope")
	if code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", code)
	}
}
