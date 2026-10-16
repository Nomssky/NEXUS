package gateway

// Operational metrics exposure (contracts/OPERATIONAL_OBSERVABILITY_CONTRACTS.md
// §4, §5, §11).
//
// These tests exercise the REAL production handler chain (Handler(), not the
// deprecated Mux()) so the authentication, rate-limit and security-header
// behavior asserted here is the behavior actually served. They also pin the
// information-disclosure rule: the response carries bounded numeric fields and
// nothing else.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/metrics"
)

// newMetricsServer boots a running engine with a metrics owner and a server
// whose control plane requires the given key.
func newMetricsServer(t *testing.T, controlKey string) (*Server, *metrics.Metrics) {
	t.Helper()
	now := time.Now()
	m := metrics.New()
	engine, err := core.NewEngine(nil,
		core.WithClock(func() time.Time { return now }),
		core.WithMetrics(m))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Stop(ctx) })

	opts := []ServerOption{WithClock(func() time.Time { return now })}
	if controlKey != "" {
		opts = append(opts, WithControlAPIKey(controlKey))
	}
	return NewServer(engine, ":0", opts...), m
}

// Obligation 15: the endpoint requires the control API key.
func TestControlMetricsRequiresTheControlAPIKey(t *testing.T) {
	srv, _ := newMetricsServer(t, "metrics-secret")

	cases := []struct {
		name string
		key  string
		want int
	}{
		{"no key", "", http.StatusUnauthorized},
		{"wrong key", "not-the-key", http.StatusUnauthorized},
		{"correct key", "metrics-secret", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/v1/control/metrics", nil)
			if tc.key != "" {
				req.Header.Set("X-API-Key", tc.key)
			}
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req) // the real production chain
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

// Fail-closed: with no control key configured the endpoint is disabled, never open.
func TestControlMetricsFailsClosedWithoutConfiguredKey(t *testing.T) {
	srv, _ := newMetricsServer(t, "")

	req := httptest.NewRequest("GET", "/api/v1/control/metrics", nil)
	req.Header.Set("X-API-Key", "anything")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (control plane must fail closed)", w.Code)
	}
	if strings.Contains(w.Body.String(), "execution") {
		t.Fatalf("a refused request must not disclose metrics: %s", w.Body.String())
	}
}

// The metrics endpoint is NOT rate-limit exempt and still carries security
// headers: it must not bypass the global limits (§4.3).
func TestControlMetricsIsNotRateLimitExemptAndKeepsSecurityHeaders(t *testing.T) {
	srv, _ := newMetricsServer(t, "metrics-secret")
	if isRateLimitExempt("/api/v1/control/metrics") {
		t.Fatal("the metrics endpoint must not be rate-limit exempt")
	}

	req := httptest.NewRequest("GET", "/api/v1/control/metrics", nil)
	req.Header.Set("X-API-Key", "metrics-secret")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if w.Header().Get("X-Content-Type-Options") == "" {
		t.Fatal("security headers must apply to the metrics response")
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content type = %q, want JSON", ct)
	}
}

// The additive sections exist and the pre-existing sections are unchanged, so
// existing consumers of this endpoint keep working (§4.1).
func TestControlMetricsExposesAdditiveSections(t *testing.T) {
	srv, m := newMetricsServer(t, "metrics-secret")
	// Produce a real lifecycle event so the counters are not all zero.
	m.ExecutionAccepted()
	m.TerminalOutcome("completed")
	m.ExecutionFinished()
	m.ToolCall()
	m.ToolAttempts(2)
	m.ToolOutcome("unknown")
	m.JournalWriteFailure()
	m.RecoveryCompleted(10, 9, 1, 2, time.Unix(1_700_000_000, 0))

	req := httptest.NewRequest("GET", "/api/v1/control/metrics", nil)
	req.Header.Set("X-API-Key", "metrics-secret")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response must be JSON: %v (%s)", err, w.Body.String())
	}
	// Pre-existing sections must survive.
	for _, k := range []string{"executor", "backpressure", "circuit_breaker", "recovery"} {
		if _, ok := resp[k]; !ok {
			t.Fatalf("pre-existing section %q disappeared (breaking change)", k)
		}
	}
	// New additive sections.
	for _, k := range []string{"execution", "tools", "journal"} {
		if _, ok := resp[k]; !ok {
			t.Fatalf("section %q is missing", k)
		}
	}

	tools := resp["tools"].(map[string]any)
	if tools["calls"].(float64) != 1 || tools["attempts"].(float64) != 2 || tools["retries"].(float64) != 1 {
		t.Fatalf("tool metrics are wrong: %+v", tools)
	}
	if tools["unknown"].(float64) != 1 || tools["completed"].(float64) != 0 {
		t.Fatalf("an unknown outcome must surface as unknown, not completed: %+v", tools)
	}

	journal := resp["journal"].(map[string]any)
	if journal["write_failures"].(float64) != 1 {
		t.Fatalf("write failures = %v, want 1", journal["write_failures"])
	}
	if journal["recovery_executions_scanned"].(float64) != 10 ||
		journal["recovery_classified_unknown"].(float64) != 1 ||
		journal["recovery_unknown_invocations"].(float64) != 2 {
		t.Fatalf("recovery metrics are wrong: %+v", journal)
	}
	if journal["last_recovery_unix_seconds"].(float64) != 1_700_000_000 {
		t.Fatalf("last recovery timestamp is wrong: %v", journal["last_recovery_unix_seconds"])
	}
}

// Obligation 10 + §4.4: no ids, prompts, arguments, secrets or paths may appear.
func TestControlMetricsResponseDisclosesNoSensitiveValues(t *testing.T) {
	srv, m := newMetricsServer(t, "metrics-secret")
	// Inject values an implementation might carelessly echo back.
	m.ExecutionAccepted()
	m.TerminalErrorClass("POLICY_DENIED")
	m.ToolCall()
	m.ToolOutcome("failed")
	m.RecoveryCompleted(5, 5, 0, 0, time.Unix(0, 0))

	req := httptest.NewRequest("GET", "/api/v1/control/metrics", nil)
	req.Header.Set("X-API-Key", "metrics-secret")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	// The data dir used by the harness and the store internals must never leak.
	forbidden := []string{
		"execution:", "invocation:", "req-", "prompt", "api_key", "apiKey",
		"secret", "password", "token", "credential", ".json", "e2e/.bin",
		"/tmp/", "Bearer", "argument",
	}
	lowered := strings.ToLower(body)
	for _, f := range forbidden {
		if strings.Contains(lowered, strings.ToLower(f)) {
			t.Fatalf("metrics response leaked %q: %s", f, body)
		}
	}

	// Structurally: the v15 sections contain numbers only — no strings at all.
	var decoded map[string]any
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"execution", "tools", "journal"} {
		fields, ok := decoded[section].(map[string]any)
		if !ok {
			t.Fatalf("section %q is not an object", section)
		}
		for k, v := range fields {
			if _, ok := v.(float64); !ok {
				t.Fatalf("%s.%s must be a number, got %T (%v)", section, k, v, v)
			}
		}
	}
}

// Obligation 12/14: reading metrics performs no store I/O and cannot fail. With
// the engine's store unavailable the endpoint still answers.
func TestControlMetricsDoesNotDependOnTheFailingSubsystem(t *testing.T) {
	// An engine with no durable store at all: there is nothing to scan, and the
	// endpoint must still answer from process-local state alone.
	now := time.Now()
	m := metrics.New()
	engine, err := core.NewEngine(nil,
		core.WithClock(func() time.Time { return now }), core.WithMetrics(m))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer engine.Stop(ctx)

	srv := NewServer(engine, ":0", WithControlAPIKey("k"))
	req := httptest.NewRequest("GET", "/api/v1/control/metrics", nil)
	req.Header.Set("X-API-Key", "k")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("metrics must answer without a durable store, got %d", w.Code)
	}
}

// Obligation 1: a freshly started process reports the documented initial values.
func TestControlMetricsStartsAtDocumentedInitialValues(t *testing.T) {
	srv, _ := newMetricsServer(t, "metrics-secret")

	req := httptest.NewRequest("GET", "/api/v1/control/metrics", nil)
	req.Header.Set("X-API-Key", "metrics-secret")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	var resp struct {
		Execution struct {
			Accepted int64 `json:"accepted"`
			InFlight int64 `json:"in_flight"`
		} `json:"execution"`
		Journal struct {
			LastRecoveryUnixSeconds int64 `json:"last_recovery_unix_seconds"`
		} `json:"journal"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Execution.Accepted != 0 || resp.Execution.InFlight != 0 {
		t.Fatalf("a fresh process must report zero, got %+v", resp.Execution)
	}
	if resp.Journal.LastRecoveryUnixSeconds != 0 {
		t.Fatalf("an unrecovered process must report an unset recovery timestamp, got %d",
			resp.Journal.LastRecoveryUnixSeconds)
	}
}

// Obligation 13: two independent servers never share metric state.
func TestIndependentServersDoNotShareMetrics(t *testing.T) {
	srvA, mA := newMetricsServer(t, "k")
	_ = srvA
	srvB, _ := newMetricsServer(t, "k")
	_ = srvB

	mA.ExecutionAccepted()

	fetch := func(srv *Server) map[string]any {
		req := httptest.NewRequest("GET", "/api/v1/control/metrics", nil)
		req.Header.Set("X-API-Key", "k")
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out["execution"].(map[string]any)
	}
	if got := fetch(srvA)["accepted"].(float64); got != 1 {
		t.Fatalf("server A accepted = %v, want 1", got)
	}
	if got := fetch(srvB)["accepted"].(float64); got != 0 {
		t.Fatalf("server B must not see server A's metrics, got %v", got)
	}
}

// Obligation 16: readiness behavior is unchanged — /ready stays 503 until the
// engine is running. v15 does not alter liveness/readiness semantics (§8).
func TestReadinessSemanticsUnchangedByMetrics(t *testing.T) {
	now := time.Now()
	// Not started: the engine is not running.
	engine, err := core.NewEngine(nil,
		core.WithClock(func() time.Time { return now }), core.WithMetrics(metrics.New()))
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(engine, ":0", WithClock(func() time.Time { return now }))

	req := httptest.NewRequest("GET", "/ready", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("/ready before start = %d, want 503", w.Code)
	}

	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer engine.Stop(ctx)

	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/ready", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/ready after start = %d, want 200", w.Code)
	}

	// Liveness is independent of readiness.
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/health = %d, want 200", w.Code)
	}
}

// The endpoint must remain a GET: no other method may expose the metrics.
func TestControlMetricsRejectsNonGET(t *testing.T) {
	srv, _ := newMetricsServer(t, "k")
	req := httptest.NewRequest("POST", "/api/v1/control/metrics", nil)
	req.Header.Set("X-API-Key", "k")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Fatalf("a non-GET request must not serve metrics, got %d", w.Code)
	}
}
