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
//   - All requests require business_id (enforced fail-closed on submit, result
//     retrieval, and SSE streaming)
//   - Correlation ID propagated from HTTP headers
//   - Governance checked at API boundary
//   - No internal details leaked in error responses
//   - Unverified client actor_id is never trusted as request identity (G-009)
//   - One-shot responses and each SSE event frame are size-bounded (G-010);
//     SSE streams remain long-lived (no duration/cumulative byte cap)
//   - SSE write/flush failures terminate the stream cleanly (G-011); they are
//     never silently ignored as successful delivery
package gateway

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/identity"
	"github.com/Nomssky/NEXUS/internal/foundation/lifecycle"
)

// G-010 response bounds. Defaults match the gateway's existing 1 MB
// request-body (MaxBytesReader) and MaxHeaderBytes limits so one size
// convention applies across the edge. SSE connection duration and cumulative
// stream bytes are intentionally unbounded — only each individual event frame
// is capped, because the architecture intends long-lived streams.
const (
	defaultMaxResponseBytes = 1 << 20
	defaultMaxSSEEventBytes = 1 << 20
)

// Server is the HTTP Gateway server.
type Server struct {
	engine        *core.Engine
	server        *http.Server
	mux           *http.ServeMux
	now           func() time.Time
	controlAPIKey string
	corrSeq       atomic.Int64 // monotonic sequence for correlation IDs

	// L-002: deterministic startup barrier. readyCh is closed once the HTTP
	// listener is successfully bound (net.Listen succeeded), before Serve
	// begins accepting. Callers must not treat startup as successful until
	// Ready() fires or Start returns a definitive error.
	readyCh   chan struct{}
	readyOnce sync.Once
	// listenFunc allows tests to inject deterministic listen behavior
	// (delayed failure). nil means net.Listen.
	listenFunc func(network, addr string) (net.Listener, error)

	// Identity-bound authorization (A6). Enforcement is active when either
	// requireAuth or enforceBusinessScope is set; both fail closed if the
	// corresponding component is missing at request time.
	authenticator        identity.Authenticator
	memberships          *identity.MembershipSet
	requireAuth          bool
	enforceBusinessScope bool

	// G-010: maxResponseBytes caps one-shot JSON response bodies (GET result).
	// maxSSEEventBytes caps each individual SSE event frame; it does not bound
	// stream duration or cumulative stream size.
	maxResponseBytes int
	maxSSEEventBytes int

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
// If empty, control endpoints are disabled (403 fail-closed), not open.
func WithControlAPIKey(key string) ServerOption {
	return func(s *Server) { s.controlAPIKey = key }
}

// WithMaxResponseBytes overrides the one-shot JSON response size cap (G-010).
// n <= 0 is ignored and keeps the default.
func WithMaxResponseBytes(n int) ServerOption {
	return func(s *Server) {
		if n > 0 {
			s.maxResponseBytes = n
		}
	}
}

// WithMaxSSEEventBytes overrides the per-event SSE frame cap (G-010).
// It does not bound stream duration or cumulative stream bytes.
// n <= 0 is ignored and keeps the default.
func WithMaxSSEEventBytes(n int) ServerOption {
	return func(s *Server) {
		if n > 0 {
			s.maxSSEEventBytes = n
		}
	}
}

// WithListenFunc injects a custom listener factory (L-002 test seam).
// Production code leaves this nil and uses net.Listen.
func WithListenFunc(fn func(network, addr string) (net.Listener, error)) ServerOption {
	return func(s *Server) { s.listenFunc = fn }
}

// NewServer creates a new HTTP Gateway server.
func NewServer(engine *core.Engine, addr string, opts ...ServerOption) *Server {
	s := &Server{
		engine:           engine,
		mux:              http.NewServeMux(),
		now:              time.Now,
		maxResponseBytes: defaultMaxResponseBytes,
		maxSSEEventBytes: defaultMaxSSEEventBytes,
		sseClients:       make(chan struct{}, 10),
		readyCh:          make(chan struct{}),
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
		MaxHeaderBytes:    1 << 20, // 1 MB limit prevents header-based DoS
	}

	return s
}

// Start starts the HTTP server. It blocks until the server stops.
//
// L-002 deterministic startup barrier: Start binds the listener via net.Listen
// (or an injected listenFunc) before serving. On successful bind it closes
// Ready(); on bind failure it returns the error without closing Ready().
// Callers must wait for Ready() or a non-nil error — never a timing window.
func (s *Server) Start(ctx context.Context) error {
	// Start event dispatch goroutine (H3 fix: SSE never receives events)
	go s.dispatchLoop(ctx)

	// Install auth middleware unconditionally (fail-closed): control endpoints
	// are protected even when no API key is configured — see authMiddleware.
	s.server.Handler = s.Handler()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.server.Shutdown(shutdownCtx)
	}()

	listen := s.listenFunc
	if listen == nil {
		listen = net.Listen
	}
	ln, err := listen("tcp", s.server.Addr)
	if err != nil {
		return fmt.Errorf("gateway listen: %w", err)
	}
	// Listener established — signal ready before Serve (app.go pattern).
	s.readyOnce.Do(func() { close(s.readyCh) })

	if err := s.server.Serve(ln); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("gateway server error: %w", err)
	}
	return nil
}

// Ready returns a channel closed once the HTTP listener is successfully
// bound. It is the deterministic startup signal for L-002; callers must not
// treat startup as successful until Ready fires or Start returns an error.
func (s *Server) Ready() <-chan struct{} {
	return s.readyCh
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

// Handler returns the production HTTP handler: every route wrapped with
// identity binding (when enforced) and auth middleware. Start() installs this
// on the server; security regression tests must exercise this (not raw Mux())
// so the real request path is covered.
func (s *Server) Handler() http.Handler {
	return s.identityMiddleware(s.authMiddleware(s.mux))
}

// authMiddleware checks X-API-Key on control endpoints.
// Fail-closed: if no control API key is configured, control endpoints are
// disabled entirely (403) rather than left unprotected.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/control/") {
			if s.controlAPIKey == "" {
				s.writeError(w, http.StatusForbidden, "CONTROL_DISABLED",
					"control API key not configured — control endpoints disabled")
				return
			}
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

	// Identity-bound submit path (A6): actor_id must match the authenticated
	// identity, and that identity must be an active member of business_id.
	if s.identityEnforced() {
		res, ok := actorFromContext(r.Context())
		if !ok {
			s.writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
			return
		}
		if req.ActorID != res.IdentityID {
			s.writeError(w, http.StatusForbidden, "AUTHORIZATION",
				"access denied: actor_id does not match authenticated identity")
			return
		}
		if err := s.authorizeMembership(res.IdentityID, req.BusinessID); err != nil {
			s.writeError(w, http.StatusForbidden, "AUTHORIZATION",
				"access denied: actor is not a member of the requested business")
			return
		}
	}

	// Extract correlation ID from header or generate one
	corrID := r.Header.Get("X-Correlation-ID")
	if corrID == "" {
		corrID = fmt.Sprintf("api-%d-%d", s.now().UnixNano(), s.corrSeq.Add(1))
	}

	// G-009: bind only a trusted actor identity. When enforcement is off the
	// client-asserted actor_id above is unauthenticated and is discarded.
	actorID := s.submitActorID(req.ActorID)

	// Create core request
	coreReq := &core.Request{
		ID:          corrID,
		Context:     core.NewRequestContext(corrID, req.BusinessID, actorID),
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
		"actor_id":       actorID,
	})
}

// handleGetResult returns the result of a processed request.
// Fail-closed authorization: the business_id query parameter is REQUIRED.
// Without it the request is rejected (400) — callers cannot bypass the scope
// check by omitting the parameter. With it, callers can only retrieve results
// belonging to their own business scope (403 on mismatch).
// When identity enforcement is on, the authenticated actor must also be an
// active member of business_id (A6) — client-asserted scope alone is not enough.
func (s *Server) handleGetResult(w http.ResponseWriter, r *http.Request) {
	// Authorization scope is mandatory — no unscoped result access.
	businessID := r.URL.Query().Get("business_id")
	if businessID == "" {
		s.writeError(w, http.StatusBadRequest, "VALIDATION", "business_id required")
		return
	}

	// Identity-bound membership check (A6): authenticated actor ∈ business_id.
	if _, stopped := s.requireActorMembership(w, r, businessID); stopped {
		return
	}

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

	// Enforce scope match
	if result.BusinessID != businessID {
		s.writeError(w, http.StatusForbidden, "AUTHORIZATION", "access denied: business scope mismatch")
		return
	}

	// G-010: bound one-shot response size. Encode to a buffer first so an
	// oversized result is rejected before any body bytes are written (no
	// truncated JSON). Default cap matches the 1 MB request-body limit.
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(result); err != nil {
		s.writeError(w, http.StatusInternalServerError, "INTERNAL_FAILURE", "failed to encode result")
		return
	}
	if buf.Len() > s.maxResponseBytes {
		s.writeError(w, http.StatusRequestEntityTooLarge, "RESOURCE_LIMIT",
			fmt.Sprintf("response exceeds size limit of %d bytes", s.maxResponseBytes))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(buf.Bytes())
}

// handleSSE streams events via Server-Sent Events.
// Fail-closed scoping: the business_id query parameter is REQUIRED. Only events
// matching the business scope are delivered; unscoped events (empty BusinessID)
// are never streamed to a scoped client. Requests without business_id are
// rejected (400) — scoping cannot be bypassed by omitting the parameter.
// When identity enforcement is on, the subscriber must be an active member of
// business_id before the stream opens (A6); foreign scopes are denied at subscribe.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	// Authorization scope is mandatory — no unscoped event stream.
	businessFilter := r.URL.Query().Get("business_id")
	if businessFilter == "" {
		s.writeError(w, http.StatusBadRequest, "VALIDATION", "business_id required")
		return
	}

	// Identity-bound membership check (A6) before opening the stream.
	if _, stopped := s.requireActorMembership(w, r, businessFilter); stopped {
		return
	}

	// Acquire SSE client slot (semaphore). Reject with 503 if at capacity.
	select {
	case s.sseClients <- struct{}{}:
		defer func() { <-s.sseClients }()
	default:
		s.writeError(w, http.StatusServiceUnavailable, "RESOURCE_LIMIT", "maximum SSE clients reached")
		return
	}

	if _, ok := w.(http.Flusher); !ok {
		s.writeError(w, http.StatusInternalServerError, "INTERNAL_FAILURE", "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ctx := r.Context()
	// streamCtx ends when the request context ends (normal client disconnect)
	// OR when a write/flush fails (stream no longer writable). Both are clean
	// termination paths for the handler — neither is an internal HTTP error
	// after SSE headers are sent (G-011).
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	rc := http.NewResponseController(w)

	// Subscribe to events
	bus := s.engine.EventBus()
	consumer := event.ConsumerFunc(func(e *event.Event) error {
		// Stream already unwritable or cancelled: do not write further frames.
		if streamCtx.Err() != nil {
			return nil
		}
		// Filter by business scope (always active — businessFilter is required)
		if e.BusinessID != businessFilter {
			return nil // skip events from other businesses and unscoped events
		}
		data, _ := json.Marshal(e)
		// G-010: bound each SSE event frame before writing. Oversized events
		// are dropped without tearing down the stream — connection lifetime
		// and cumulative stream bytes remain unbounded (long-lived by design).
		// Frame layout: "event: " + type + "\ndata: " + json + "\n\n".
		const frameOverhead = len("event: \ndata: \n\n")
		if frameOverhead+len(e.Type)+len(data) > s.maxSSEEventBytes {
			return nil
		}
		// G-011: surface write/flush failures. Return nil (not error) so the
		// event bus does not re-queue a doomed write or abort the dispatch
		// batch; cancelStream wakes the handler to unsubscribe and exit.
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, data); err != nil {
			cancelStream()
			return nil
		}
		if err := rc.Flush(); err != nil {
			cancelStream()
			return nil
		}
		return nil
	})

	subID, err := bus.Subscribe(consumer)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "INTERNAL_FAILURE", "subscribe failed")
		return
	}
	defer bus.Unsubscribe(subID)

	// Keep connection open until client disconnect or stream write failure.
	<-streamCtx.Done()
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

// componentStatusConfigured is the truthful status for components that are
// intentionally always constructed with the engine and have no start/stop
// lifecycle. It asserts presence/configuration only — never "active"/running
// (G-012: constructed ≠ ready ≠ running ≠ active).
const componentStatusConfigured = "configured"

// executorComponentStatus returns the executor's authoritative runtime state.
func executorComponentStatus(ex *executor.Executor) string {
	if ex.IsRunning() {
		return "running"
	}
	return "stopped"
}

// handleControlComponents lists all engine components and their states.
//
// G-012: status is derived from authoritative runtime state where one exists;
// components without a lifecycle API report "configured" (present, wired)
// rather than a false "active"/running claim.
func (s *Server) handleControlComponents(w http.ResponseWriter, r *http.Request) {
	components := []ComponentInfo{
		{Name: "engine", Status: string(s.engine.Status()), Type: "core"},
		{Name: "circuit_breaker", Status: s.engine.CircuitBreaker().State(), Type: "hardening"},
		{Name: "backpressure", Status: componentStatusConfigured, Type: "hardening"},
		{Name: "recovery_manager", Status: componentStatusConfigured, Type: "hardening"},
		{Name: "event_bus", Status: componentStatusConfigured, Type: "foundation"},
		{Name: "memory_store", Status: componentStatusConfigured, Type: "foundation"},
		{Name: "attention_engine", Status: componentStatusConfigured, Type: "foundation"},
		{Name: "governance", Status: componentStatusConfigured, Type: "foundation"},
		{Name: "task_executor", Status: executorComponentStatus(s.engine.TaskExecutor()), Type: "execution"},
		{Name: "model_router", Status: componentStatusConfigured, Type: "intelligence"},
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
