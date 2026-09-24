package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
)

// G-007: Mux() is a test-only raw handler; production path (Handler()/Start())
// always wraps auth middleware. Invariant: control endpoints are unreachable
// via the production handler without a valid API key; Mux() intentionally
// lacks auth for unit tests and must never be used in production code.

// g007Fixture builds a server with a control API key configured.
func g007Fixture(t *testing.T) *Server {
	t.Helper()
	now := time.Now()
	engine, err := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = engine.Stop(ctx) })

	return NewServer(engine, ":0",
		WithClock(func() time.Time { return now }),
		WithControlAPIKey("g007-secret"),
	)
}

// Production handler (Handler(), as installed by Start) enforces control auth.
func TestG007HandlerEnforcesControlAuth(t *testing.T) {
	srv := g007Fixture(t)

	req := httptest.NewRequest("GET", "/api/v1/control/status", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Handler() control without key: want 401, got %d", w.Code)
	}
}

// Mux() intentionally does NOT enforce auth — documents the raw test-only
// surface that G-007 deprecates for production use.
func TestG007MuxIsRawTestOnlyBypass(t *testing.T) {
	srv := g007Fixture(t)

	req := httptest.NewRequest("GET", "/api/v1/control/status", nil)
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	// Raw mux reaches the handler (no auth layer) — this is the residual
	// misuse risk G-007 flags; production must use Handler() instead.
	if w.Code != http.StatusOK {
		t.Errorf("Mux() control without key: want 200 (raw bypass), got %d", w.Code)
	}
}

// Handler() and Mux() must differ for control paths — proves production path
// is not the raw mux (Start installs Handler(), not Mux()).
func TestG007HandlerDiffersFromMuxForControl(t *testing.T) {
	srv := g007Fixture(t)

	muxW := httptest.NewRecorder()
	srv.Mux().ServeHTTP(muxW, httptest.NewRequest("GET", "/api/v1/control/status", nil))

	handlerW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(handlerW, httptest.NewRequest("GET", "/api/v1/control/status", nil))

	if muxW.Code == handlerW.Code {
		t.Errorf("Handler() and Mux() returned same status %d for control without key — production path must differ", muxW.Code)
	}
	if handlerW.Code != http.StatusUnauthorized {
		t.Errorf("Handler() control without key: want 401, got %d", handlerW.Code)
	}
}
