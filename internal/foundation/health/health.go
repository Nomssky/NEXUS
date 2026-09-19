// Package health implements the M0 health/readiness foundation (contributes to
// component C07; bootstrapped at L0).
//
// It exposes two distinct signals per the mission and contracts:
//
//   - Liveness:  PROCESS IS ALIVE.
//   - Readiness: SYSTEM IS READY to accept work.
//
// Invariant preserved: readiness is NOT claimed merely because the process is
// running. Readiness is derived from explicit, injected checks. M0 registers at
// most a self-check so the surface is real and testable without pretending any
// downstream dependency is healthy. Readiness does NOT grant authorization
// (health ≠ authorization).
//
// Deferred to later milestones (recorded, not implemented): distributed health
// aggregation, dependency health registries, worker liveness/lease checks,
// metrics/alerting integration.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Status is a component's health status.
type Status string

const (
	// StatusUp means a check passed.
	StatusUp Status = "up"
	// StatusDown means a check failed.
	StatusDown Status = "down"
	// StatusDegraded means a check is operational but degraded.
	StatusDegraded Status = "degraded"
)

// Check is a named readiness probe.
type Check struct {
	Name string
	// Probe reports whether the dependency is healthy. It must not block
	// indefinitely; the Server applies a timeout.
	Probe func(ctx context.Context) error
}

// Server serves liveness and readiness endpoints.
type Server struct {
	mu           sync.RWMutex
	ready        bool
	checks       []Check
	probeTimeout time.Duration
	startedAt    time.Time
}

// NewServer constructs a Server. It starts unready; call MarkReady once startup
// initialization has actually completed.
func NewServer() *Server {
	return &Server{
		probeTimeout: 2 * time.Second,
		startedAt:    time.Now().UTC(),
	}
}

// RegisterCheck adds a readiness check.
func (s *Server) RegisterCheck(c Check) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checks = append(s.checks, c)
}

// MarkReady flips the server to ready. Idempotent.
func (s *Server) MarkReady() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = true
}

// MarkNotReady flips the server to not-ready (used when shutdown begins).
func (s *Server) MarkNotReady() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = false
}

// LiveResponse is the liveness payload.
type LiveResponse struct {
	Status    Status `json:"status"`
	StartedAt string `json:"started_at"`
}

// ReadyResponse is the readiness payload.
type ReadyResponse struct {
	Status Status            `json:"status"`
	Checks map[string]Status `json:"checks"`
}

// LiveHandler reports process liveness. It is intentionally independent of
// dependency readiness.
func (s *Server) LiveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, LiveResponse{
			Status:    StatusUp,
			StartedAt: s.startedAt.Format(time.RFC3339Nano),
		})
	}
}

// ReadyHandler reports system readiness, running each registered check.
func (s *Server) ReadyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		ready := s.ready
		checks := append([]Check(nil), s.checks...)
		timeout := s.probeTimeout
		s.mu.RUnlock()

		results := make(map[string]Status, len(checks))
		overall := StatusUp

		for _, c := range checks {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			err := c.Probe(ctx)
			cancel()
			if err != nil {
				results[c.Name] = StatusDown
				overall = StatusDown
			} else {
				results[c.Name] = StatusUp
			}
		}

		code := http.StatusOK
		if !ready || overall != StatusUp {
			if overall == StatusUp {
				overall = StatusDown
			}
			code = http.StatusServiceUnavailable
		}
		writeJSON(w, code, ReadyResponse{Status: overall, Checks: results})
	}
}

// Mux returns an http.ServeMux with the health/readiness routes registered.
func (s *Server) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.LiveHandler())
	mux.HandleFunc("/readiness", s.ReadyHandler())
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
