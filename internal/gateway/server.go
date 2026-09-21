// Package gateway implements the NEXUS HTTP Gateway — the external-facing
// API layer that makes the Core Runtime reachable via HTTP/WebSocket.
//
// The gateway provides:
//   - REST API for submitting requests and querying results
//   - Health/readiness endpoints
//   - Server-Sent Events (SSE) for real-time event streaming
//   - Status endpoints
//
// Key invariants:
//   - All requests require business_id
//   - Correlation ID propagated from HTTP headers
//   - Governance checked at API boundary
//   - No internal details leaked in error responses
package gateway

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
)

// Server is the HTTP Gateway server.
type Server struct {
	engine        *core.Engine
	server        *http.Server
	mux           *http.ServeMux
	now           func() time.Time
	controlAPIKey string
	corrSeq       atomic.Int64 // monotonic sequence for correlation IDs

	// SSE concurrency limiter — buffered channel acts as a semaphore.
	// Max 10 concurrent SSE clients to prevent unbounded goroutine creation.
	sseClients chan struct{}
}

// ServerOption configures the gateway server.
type ServerOption func(*Server)

// WithClock injects a clock for testing.
func WithClock(now func() time.Time) ServerOption {
	return func(s *Server) { s.now = now }
}

// WithControlAPIKey sets the API key required for control endpoints.
// If empty, control endpoints are unprotected (backward compatible).
func WithControlAPIKey(key string) ServerOption {
	return func(s *Server) { s.controlAPIKey = key }
}

// NewServer creates a new HTTP Gateway server.
func NewServer(engine *core.Engine, addr string, opts ...ServerOption) *Server {
	s := &Server{
		engine:     engine,
		mux:        http.NewServeMux(),
		now:        time.Now,
		sseClients: make(chan struct{}, 10),
	}

	for _, opt := range opts {
		opt(s)
	}

	// Register routes
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /ready", s.handleReady)
	s.mux.HandleFunc("GET /status", s.handleStatus)
	s.mux.HandleFunc("POST /api/v1/requests", s.handleSubmitRequest)
	s.mux.HandleFunc("GET /api/v1/requests/{id}", s.handleGetResult)
	s.mux.HandleFunc("GET /events", s.handleSSE)

	// Control surface endpoints
	s.mux.HandleFunc("GET /api/v1/control/status", s.handleControlStatus)
	s.mux.HandleFunc("POST /api/v1/control/pause", s.handleControlPause)
	s.mux.HandleFunc("POST /api/v1/control/resume", s.handleControlResume)
	s.mux.HandleFunc("GET /api/v1/control/metrics", s.handleControlMetrics)
	s.mux.HandleFunc("GET /api/v1/control/components", s.handleControlComponents)

	s.server = &http.Server{
		Addr:              addr,
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return s
}

// Start starts the HTTP server.
func (s *Server) Start(ctx context.Context) error {
	// Start event dispatch goroutine (H3 fix: SSE never receives events)
	go s.dispatchLoop(ctx)

	// Wrap handler with auth middleware for control endpoints (H8 fix)
	var handler http.Handler = s.mux
	if s.controlAPIKey != "" {
		handler = s.authMiddleware(s.mux)
	}

	s.server.Handler = handler

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.server.Shutdown(shutdownCtx)
	}()

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("gateway server error: %w", err)
	}
	return nil
}

// dispatchLoop periodically dispatches events from the MemBus so that
// SSE subscribers receive them. Without this goroutine, events are published
// but never delivered because Dispatch() is never called.
func (s *Server) dispatchLoop(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.engine.Status() == lifecycle.StateRunning {
				s.engine.EventBus().Dispatch()
			}
		}
	}
}

// authMiddleware checks X-API-Key on control endpoints.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/control/") {
			got := r.Header.Get("X-API-Key")
			if subtle.ConstantTimeCompare([]byte(got), []byte(s.controlAPIKey)) != 1 {
				s.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid or missing API key")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Stop gracefully shuts down the HTTP server.
func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// Mux returns the raw HTTP handler for testing purposes only.
// WARNING: This handler does NOT include auth middleware. Use only in tests.
// Production code must use the handler returned by Start() which includes auth.
func (s *Server) Mux() http.Handler {
	return s.mux
}

// handleHealth returns liveness status.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"time":   s.now().UTC().Format(time.RFC3339),
	})
}

// handleReady returns readiness status.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ready := s.engine.Status() == lifecycle.StateRunning
	w.Header().Set("Content-Type", "application/json")

	status := "ready"
	httpCode := http.StatusOK
	if !ready {
		status = "not ready"
		httpCode = http.StatusServiceUnavailable
	}

	w.WriteHeader(httpCode)
	json.NewEncoder(w).Encode(map[string]string{
		"status": status,
	})
}

// handleStatus returns engine status.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": string(s.engine.Status()),
	})
}

// submitRequest is the JSON body for POST /api/v1/requests.
type submitRequest struct {
	Intent      string   `json:"intent"`
	BusinessID  string   `json:"business_id"`
	ActorID     string   `json:"actor_id"`
	Priority    int      `json:"priority,omitempty"`
	Constraints []string `json:"constraints,omitempty"`
}

// handleSubmitRequest processes a new request.
func (s *Server) handleSubmitRequest(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit

	var req submitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "VALIDATION", "invalid request body")
		return
	}

	if req.Intent == "" {
		s.writeError(w, http.StatusBadRequest, "VALIDATION", "intent required")
		return
	}
	if req.BusinessID == "" {
		s.writeError(w, http.StatusBadRequest, "VALIDATION", "business_id required")
		return
	}
	if req.ActorID == "" {
		s.writeError(w, http.StatusBadRequest, "VALIDATION", "actor_id required")
		return
	}

	// Extract correlation ID from header or generate one
	corrID := r.Header.Get("X-Correlation-ID")
	if corrID == "" {
		corrID = fmt.Sprintf("api-%d-%d", s.now().UnixNano(), s.corrSeq.Add(1))
	}

	// Create core request
	coreReq := &core.Request{
		ID:          corrID,
		Context:     core.NewRequestContext(corrID, req.BusinessID, req.ActorID),
		Intent:      req.Intent,
		Priority:    req.Priority,
		Constraints: req.Constraints,
	}

	if err := s.engine.SubmitRequest(coreReq); err != nil {
		s.writeError(w, http.StatusServiceUnavailable, "RESOURCE_UNAVAILABLE", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-ID", corrID)
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"request_id":     corrID,
		"correlation_id": corrID,
		"status":         "accepted",
	})
}

// handleGetResult returns the result of a processed request.
// When business_id query parameter is provided, authorization is enforced —
// callers can only retrieve results belonging to their own business scope.
// When omitted, the check is skipped for backward compatibility.
func (s *Server) handleGetResult(w http.ResponseWriter, r *http.Request) {
	// Extract request ID from URL path: /api/v1/requests/{id}
	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "VALIDATION", "request ID required")
		return
	}

	result, ok := s.engine.GetResult(id)
	if !ok {
		s.writeError(w, http.StatusNotFound, "VALIDATION", "request not found")
		return
	}

	// Authorization: when business_id is provided, enforce scope match
	if businessID := r.URL.Query().Get("business_id"); businessID != "" {
		if result.BusinessID != businessID {
			s.writeError(w, http.StatusForbidden, "AUTHORIZATION", "access denied: business scope mismatch")
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleSSE streams events via Server-Sent Events.
// Supports optional business_id query parameter for scope filtering — when set,
// only events matching the business scope are delivered to the client.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	// Acquire SSE client slot (semaphore). Reject with 503 if at capacity.
	select {
	case s.sseClients <- struct{}{}:
		defer func() { <-s.sseClients }()
	default:
		s.writeError(w, http.StatusServiceUnavailable, "RESOURCE_LIMIT", "maximum SSE clients reached")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, http.StatusInternalServerError, "INTERNAL_FAILURE", "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ctx := r.Context()

	// Optional business scope filter
	businessFilter := r.URL.Query().Get("business_id")

	// Subscribe to events
	bus := s.engine.EventBus()
	consumer := event.ConsumerFunc(func(e *event.Event) error {
		// Filter by business scope if requested
		if businessFilter != "" && e.BusinessID != businessFilter {
			return nil // skip events from other businesses
		}
		data, _ := json.Marshal(e)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, data)
		flusher.Flush()
		return nil
	})

	subID, err := bus.Subscribe(consumer)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "INTERNAL_FAILURE", "subscribe failed")
		return
	}
	defer bus.Unsubscribe(subID)

	// Keep connection open until client disconnects
	<-ctx.Done()
}

// ControlStatusResponse is the detailed engine status.
type ControlStatusResponse struct {
	Status       string            `json:"status"`
	Uptime       string            `json:"uptime"`
	Components   map[string]string `json:"components"`
	RequestCount int               `json:"request_count"`
}

// handleControlStatus returns detailed engine status with component health.
func (s *Server) handleControlStatus(w http.ResponseWriter, r *http.Request) {
	components := map[string]string{
		"engine":          string(s.engine.Status()),
		"circuit_breaker": s.engine.CircuitBreaker().State(),
		"backpressure":    fmt.Sprintf("queue=%d rejected=%d", s.engine.Backpressure().QueueSize(), s.engine.Backpressure().RejectedCount()),
		"recovery":        fmt.Sprintf("failures=%d", s.engine.RecoveryManager().RecordCount()),
	}

	executed, _, _ := s.engine.TaskExecutor().Metrics()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ControlStatusResponse{
		Status:       string(s.engine.Status()),
		Components:   components,
		RequestCount: int(executed),
	})
}

// handleControlPause pauses the engine (stops accepting new requests).
func (s *Server) handleControlPause(w http.ResponseWriter, r *http.Request) {
	status := s.engine.Status()
	if status == lifecycle.StateStopped {
		s.writeError(w, http.StatusConflict, "ALREADY_PAUSED", "engine is already stopped")
		return
	}
	if status != lifecycle.StateRunning {
		s.writeError(w, http.StatusBadRequest, "INVALID_STATE", fmt.Sprintf("engine in %s state, cannot pause", status))
		return
	}

	if err := s.engine.Stop(r.Context()); err != nil {
		s.writeError(w, http.StatusInternalServerError, "CONTROL_FAILURE", fmt.Sprintf("pause failed: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "paused",
		"message": "engine stopped accepting new requests",
	})
}

// handleControlResume resumes the engine.
func (s *Server) handleControlResume(w http.ResponseWriter, r *http.Request) {
	status := s.engine.Status()
	if status == lifecycle.StateRunning {
		s.writeError(w, http.StatusConflict, "ALREADY_RUNNING", "engine is already running")
		return
	}
	if status != lifecycle.StateStopped {
		s.writeError(w, http.StatusBadRequest, "INVALID_STATE", fmt.Sprintf("engine in %s state, cannot resume", status))
		return
	}

	if err := s.engine.Resume(r.Context()); err != nil {
		s.writeError(w, http.StatusInternalServerError, "CONTROL_FAILURE", fmt.Sprintf("resume failed: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "resumed",
		"message": "engine resumed accepting new requests",
	})
}

// MetricsResponse is the runtime metrics snapshot.
type MetricsResponse struct {
	Executor       ExecutorMetrics       `json:"executor"`
	Backpressure   BackpressureMetrics   `json:"backpressure"`
	CircuitBreaker CircuitBreakerMetrics `json:"circuit_breaker"`
	Recovery       RecoveryMetrics       `json:"recovery"`
}

// ExecutorMetrics holds executor statistics.
type ExecutorMetrics struct {
	Executed int64 `json:"executed"`
	Failed   int64 `json:"failed"`
	Denied   int64 `json:"denied"`
	Active   int   `json:"active"`
}

// BackpressureMetrics holds backpressure statistics.
type BackpressureMetrics struct {
	QueueSize     int `json:"queue_size"`
	RejectedCount int `json:"rejected_count"`
}

// CircuitBreakerMetrics holds circuit breaker state.
type CircuitBreakerMetrics struct {
	State string `json:"state"`
}

// RecoveryMetrics holds recovery manager stats.
type RecoveryMetrics struct {
	FailureCount int `json:"failure_count"`
}

// handleControlMetrics returns runtime metrics.
func (s *Server) handleControlMetrics(w http.ResponseWriter, r *http.Request) {
	executed, failed, denied := s.engine.TaskExecutor().Metrics()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MetricsResponse{
		Executor: ExecutorMetrics{
			Executed: executed,
			Failed:   failed,
			Denied:   denied,
			Active:   s.engine.TaskExecutor().ActiveCount(),
		},
		Backpressure: BackpressureMetrics{
			QueueSize:     s.engine.Backpressure().QueueSize(),
			RejectedCount: s.engine.Backpressure().RejectedCount(),
		},
		CircuitBreaker: CircuitBreakerMetrics{
			State: s.engine.CircuitBreaker().State(),
		},
		Recovery: RecoveryMetrics{
			FailureCount: s.engine.RecoveryManager().RecordCount(),
		},
	})
}

// ComponentInfo describes a single component.
type ComponentInfo struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Type   string `json:"type"`
}

// handleControlComponents lists all engine components and their states.
func (s *Server) handleControlComponents(w http.ResponseWriter, r *http.Request) {
	components := []ComponentInfo{
		{Name: "engine", Status: string(s.engine.Status()), Type: "core"},
		{Name: "circuit_breaker", Status: s.engine.CircuitBreaker().State(), Type: "hardening"},
		{Name: "backpressure", Status: "active", Type: "hardening"},
		{Name: "recovery_manager", Status: "active", Type: "hardening"},
		{Name: "event_bus", Status: "active", Type: "foundation"},
		{Name: "memory_store", Status: "active", Type: "foundation"},
		{Name: "attention_engine", Status: "active", Type: "foundation"},
		{Name: "governance", Status: "active", Type: "foundation"},
		{Name: "task_executor", Status: "active", Type: "execution"},
		{Name: "model_router", Status: "active", Type: "intelligence"},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"components": components,
		"count":      len(components),
	})
}

// writeError writes a JSON error response.
func (s *Server) writeError(w http.ResponseWriter, code int, category, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"code":      fmt.Sprintf("%d", code),
			"category":  category,
			"message":   message,
			"retryable": code >= 500,
		},
	})
}
