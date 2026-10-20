package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func permissiveRL() RateLimitPolicy {
	return RateLimitPolicy{Enabled: true, SubmitPerMinute: 1000, RequestPerMinute: 1000, PerIPPerMinute: 1000}
}

// Security headers must be present on both normal and error responses.
func TestSecurityHeadersOnNormalAndErrorResponses(t *testing.T) {
	f := newRLFixture(t, permissiveRL(), nil)

	check := func(w *httptest.ResponseRecorder, what string) {
		t.Helper()
		if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options = %q", what, got)
		}
		if got := w.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("%s: X-Frame-Options = %q", what, got)
		}
		if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("%s: Referrer-Policy = %q", what, got)
		}
	}

	okW := doReq(f.srv, http.MethodGet, "/health", "")
	check(okW, "normal")

	errW := doReq(f.srv, http.MethodGet, "/api/v1/nope", "alice")
	check(errW, "error")
}

// HSTS must not be emitted by the plain-HTTP gateway.
func TestNoHSTSOnPlainHTTP(t *testing.T) {
	f := newRLFixture(t, permissiveRL(), nil)
	w := doReq(f.srv, http.MethodGet, "/health", "")
	if got := w.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("gateway must not emit HSTS, got %q", got)
	}
}

// With no allow-list configured, the same-origin default emits no CORS headers.
func TestCORSDefaultSameOriginNoHeaders(t *testing.T) {
	f := newRLFixture(t, permissiveRL(), nil)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://app.example")
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("same-origin default must not emit ACAO, got %q", got)
	}
}

// An allowed origin receives exactly that origin back, plus Vary: Origin.
func TestCORSAllowedOriginReceivesHeaders(t *testing.T) {
	f := newRLFixture(t, permissiveRL(), []string{"https://app.example"})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://app.example")
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Fatalf("ACAO = %q, want the allow-listed origin", got)
	}
	if !strings.Contains(w.Header().Get("Vary"), "Origin") {
		t.Fatalf("Vary must include Origin, got %q", w.Header().Get("Vary"))
	}
}

// A disallowed origin is never reflected and gets no permission headers.
func TestCORSDisallowedOriginNotReflected(t *testing.T) {
	f := newRLFixture(t, permissiveRL(), []string{"https://app.example"})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("a disallowed origin must never be reflected, got %q", got)
	}
}

// A near-miss (path, suffix, subdomain) must not match the allow-list.
func TestCORSNearMissOriginsRejected(t *testing.T) {
	f := newRLFixture(t, permissiveRL(), []string{"https://app.example"})
	for _, origin := range []string{
		"https://app.example.evil.com",
		"https://sub.app.example",
		"http://app.example",       // scheme differs
		"https://app.example:8443", // port differs
	} {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(w, req)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("origin %q must not be reflected, got %q", origin, got)
		}
	}
}

// Preflight for an allowed origin is answered with the method/header contract.
func TestCORSPreflightAllowed(t *testing.T) {
	f := newRLFixture(t, permissiveRL(), []string{"https://app.example"})
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/intelligence/execute", nil)
	req.Header.Set("Origin", "https://app.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Fatalf("preflight ACAO = %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "POST") {
		t.Fatalf("preflight methods = %q, want POST", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(strings.ToLower(got), "x-actor-id") {
		t.Fatalf("preflight headers = %q, want X-Actor-ID", got)
	}
}

// Preflight for a disallowed origin carries no permission headers.
func TestCORSPreflightDisallowed(t *testing.T) {
	f := newRLFixture(t, permissiveRL(), []string{"https://app.example"})
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/intelligence/execute", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("disallowed preflight must not set ACAO, got %q", got)
	}
}

// A non-browser client (no Origin) is unaffected by the CORS policy.
func TestCORSNonBrowserClientUnaffected(t *testing.T) {
	f := newRLFixture(t, permissiveRL(), []string{"https://app.example"})
	w := doReq(f.srv, http.MethodGet, "/health", "")
	if w.Code != http.StatusOK {
		t.Fatalf("no-Origin request must proceed normally, got %d", w.Code)
	}
}

// CORS is not authorization: a cross-origin request to a protected path without
// credentials is still 401.
func TestCORSDoesNotBypassAuth(t *testing.T) {
	f := newRLFixture(t, permissiveRL(), []string{"https://app.example"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/execute", strings.NewReader("{}"))
	req.Header.Set("Origin", "https://app.example")
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("cross-origin request without credentials must be 401, got %d", w.Code)
	}
}
