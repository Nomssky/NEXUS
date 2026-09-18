package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TEST-M0-006: liveness responds and is independent of readiness.
func TestLiveness(t *testing.T) {
	s := NewServer() // not ready
	rec := httptest.NewRecorder()
	s.LiveHandler()(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("liveness should be 200 while alive, got %d", rec.Code)
	}
}

// TEST-M0-007: readiness is 503 before MarkReady and 200 after.
func TestReadiness(t *testing.T) {
	s := NewServer()

	rec := httptest.NewRecorder()
	s.ReadyHandler()(rec, httptest.NewRequest(http.MethodGet, "/readiness", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 before ready, got %d", rec.Code)
	}

	s.MarkReady()
	rec2 := httptest.NewRecorder()
	s.ReadyHandler()(rec2, httptest.NewRequest(http.MethodGet, "/readiness", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 after ready, got %d", rec2.Code)
	}

	var body ReadyResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != StatusUp {
		t.Fatalf("expected up status, got %s", body.Status)
	}
}

// Readiness reflects failing checks; a running process is not automatically
// "ready" when a dependency is down.
func TestReadinessFailingCheck(t *testing.T) {
	s := NewServer()
	s.RegisterCheck(Check{Name: "db", Probe: func(context.Context) error {
		return errors.New("unreachable")
	}})
	s.MarkReady()

	rec := httptest.NewRecorder()
	s.ReadyHandler()(rec, httptest.NewRequest(http.MethodGet, "/readiness", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when a dependency check fails, got %d", rec.Code)
	}
}

// Readiness declines once the server is marked not-ready (shutdown).
func TestReadinessAfterMarkNotReady(t *testing.T) {
	s := NewServer()
	s.MarkReady()
	s.MarkNotReady()
	rec := httptest.NewRecorder()
	s.ReadyHandler()(rec, httptest.NewRequest(http.MethodGet, "/readiness", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 after MarkNotReady, got %d", rec.Code)
	}
}
