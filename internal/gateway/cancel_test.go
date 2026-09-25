package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/modelrouter"
)

// ---------------------------------------------------------------------------
// E-005: external cancellation of in-flight requests (gateway scope).
// ---------------------------------------------------------------------------

// waitUntil polls cond until it holds, failing the test if the bounded
// deadline elapses first (E-015 pattern: observable state, never sleep).
func waitUntil(t *testing.T, desc string, cond func() bool) {
	t.Helper()

	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(10 * time.Second)

	for {
		if cond() {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %s", desc)
		case <-tick.C:
		}
	}
}

// waitSignal waits for one value on ch with a bounded deadline.
func waitSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// blockingProvider is a cooperative model provider that blocks until its
// context is cancelled (or the test releases it), keeping a submitted request
// deterministically in flight for cancellation tests. Context cancellation
// reaching it is the end-to-end proof of provider ctx threading (decision 9).
type blockingProvider struct {
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	sawCtx  bool
}

func newBlockingProvider() *blockingProvider {
	return &blockingProvider{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
}

func (p *blockingProvider) Identify() string              { return "cancel-blocking-provider" }
func (p *blockingProvider) HealthCheck() error            { return nil }
func (p *blockingProvider) ListModels() ([]string, error) { return nil, nil }

func (p *blockingProvider) Invoke(ctx context.Context, req *modelrouter.GenerateRequest) (*modelrouter.GenerateResponse, error) {
	select {
	case p.started <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
		p.mu.Lock()
		p.sawCtx = true
		p.mu.Unlock()
		return nil, ctx.Err()
	case <-p.release:
		return &modelrouter.GenerateResponse{
			RequestID:    req.RequestID,
			ModelID:      req.ModelID,
			Content:      "released",
			FinishReason: "stop",
		}, nil
	}
}

// cancelled reports whether the provider observed context cancellation.
func (p *blockingProvider) cancelled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sawCtx
}

// cancelEngine builds a started engine wired with the blocking provider so a
// submitted request stays in flight until cancelled. Tests must defer
// close(engine release) themselves (after engine.Stop is registered by
// t.Cleanup — in-function defers run first, so a blocked provider is released
// before Stop waits on it).
func cancelEngine(t *testing.T) (*core.Engine, *blockingProvider) {
	t.Helper()
	now := time.Now()
	engine, err := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	p := newBlockingProvider()
	if err := engine.ModelRegistry().RegisterModel(&modelrouter.ModelDefinition{
		ID:         "cancel-model",
		ProviderID: p.Identify(),
		Capabilities: []modelrouter.ModelCapability{
			modelrouter.CapabilityReasoning,
			modelrouter.CapabilityToolCalling,
		},
		Runtime: modelrouter.RuntimeLocal,
		Status:  modelrouter.ModelStatusActive,
	}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	engine.ModelRouter().RegisterProvider(p)
	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })
	return engine, p
}

// fastEngine builds a started engine without a blocking provider: submitted
// requests complete quickly (used for terminal-state tests).
func fastEngine(t *testing.T) *core.Engine {
	t.Helper()
	now := time.Now()
	engine, err := core.NewEngine(nil, core.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })
	return engine
}

// submitAndWait submits a request through the real HTTP path and returns its
// request_id, waiting until the terminal result is recorded when waitResult
// is true.
func submitAndWait(t *testing.T, srv *Server, engine *core.Engine, intent, businessID, actorID string, waitResult bool) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"intent":      intent,
		"business_id": businessID,
		"actor_id":    actorID,
	})
	req := httptest.NewRequest("POST", "/api/v1/requests", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("submit: expected 202, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode submit response: %v", err)
	}
	if waitResult {
		waitUntil(t, "request result", func() bool {
			_, ok := engine.GetResult(resp["request_id"])
			return ok
		})
	}
	return resp["request_id"]
}

// postCancel issues the cancel request and returns the recorded response.
func postCancel(srv *Server, id, businessID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/v1/requests/"+id+"/cancel?business_id="+businessID, nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	return w
}

func decodeErrorBody(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var body map[string]map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v (raw: %s)", err, w.Body.String())
	}
	return body["error"]
}

// TEST-GW-E005-01: cancel without business_id → 400 VALIDATION (fail closed).
func TestCancelMissingBusinessID(t *testing.T) {
	engine := fastEngine(t)
	srv := NewServer(engine, ":0")

	// Build the request explicitly so the query parameter is genuinely absent.
	req := httptest.NewRequest("POST", "/api/v1/requests/req-any/cancel", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	errBody := decodeErrorBody(t, w)
	if errBody["category"] != "VALIDATION" {
		t.Errorf("expected VALIDATION, got %v", errBody["category"])
	}
}

// TEST-GW-E005-02: unknown request → 404 VALIDATION "request not found".
func TestCancelUnknownRequest(t *testing.T) {
	engine := fastEngine(t)
	srv := NewServer(engine, ":0")

	w := postCancel(srv, "req-does-not-exist", "biz-1")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
	errBody := decodeErrorBody(t, w)
	if errBody["category"] != "VALIDATION" {
		t.Errorf("expected VALIDATION, got %v", errBody["category"])
	}
}

// TEST-GW-E005-03: foreign business scope → 403 AUTHORIZATION (ownership
// enforced by core against the recorded business, in any state).
func TestCancelScopeMismatch(t *testing.T) {
	engine := fastEngine(t)
	srv := NewServer(engine, ":0")

	id := submitAndWait(t, srv, engine, "scope check", "biz-1", "user-1", false)

	w := postCancel(srv, id, "biz-2")
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
	errBody := decodeErrorBody(t, w)
	if errBody["category"] != "AUTHORIZATION" {
		t.Errorf("expected AUTHORIZATION, got %v", errBody["category"])
	}
	if msg, _ := errBody["message"].(string); !bytes.Contains([]byte(msg), []byte("business scope mismatch")) {
		t.Errorf("expected scope mismatch message, got %v", errBody["message"])
	}
}

// TEST-GW-E005-04: cancelling an in-flight request → 202 Accepted with
// {request_id, correlation_id, status:cancelling}; the final state is observed
// via GET (cancelled).
func TestCancelAccepted202(t *testing.T) {
	engine, p := cancelEngine(t)
	defer close(p.release)
	srv := NewServer(engine, ":0")

	id := submitAndWait(t, srv, engine, "long running work", "biz-1", "user-1", false)
	waitUntil(t, "executor task in flight", func() bool { return engine.TaskExecutor().ActiveCount() >= 1 })

	w := postCancel(srv, id, "biz-1")
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode 202 body: %v", err)
	}
	if body["request_id"] != id || body["correlation_id"] != id || body["status"] != "cancelling" {
		t.Errorf("unexpected 202 body: %v", body)
	}
	if got := w.Header().Get("X-Correlation-ID"); got != id {
		t.Errorf("expected X-Correlation-ID %q, got %q", id, got)
	}

	// Final state observed via GET.
	waitUntil(t, "cancelled result", func() bool {
		result, ok := engine.GetResult(id)
		return ok && result.Status == "cancelled"
	})
	getReq := httptest.NewRequest("GET", "/api/v1/requests/"+id+"?business_id=biz-1", nil)
	getW := httptest.NewRecorder()
	srv.Handler().ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("GET result: expected 200, got %d", getW.Code)
	}
	var result core.Response
	if err := json.NewDecoder(getW.Body).Decode(&result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Status != "cancelled" {
		t.Errorf("expected final status cancelled, got %s", result.Status)
	}
	if result.Error == nil || result.Error.Code != "CANCELLED" || result.Error.Category != "CANCELLATION" {
		t.Errorf("expected contract CANCELLED/CANCELLATION error, got %+v", result.Error)
	}
	if !p.cancelled() {
		t.Error("provider must observe context cancellation (ctx threading)")
	}
}

// TEST-GW-E005-05: terminal request → 409 CONFLICT with the terminal status.
func TestCancelCompletedConflict(t *testing.T) {
	engine := fastEngine(t)
	srv := NewServer(engine, ":0")

	id := submitAndWait(t, srv, engine, "quick work", "biz-1", "user-1", true)

	w := postCancel(srv, id, "biz-1")
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", w.Code, w.Body.String())
	}
	errBody := decodeErrorBody(t, w)
	if errBody["category"] != "CONFLICT" {
		t.Errorf("expected CONFLICT, got %v", errBody["category"])
	}
	if msg, _ := errBody["message"].(string); !bytes.Contains([]byte(msg), []byte("terminal state: completed")) {
		t.Errorf("expected terminal status in message, got %v", errBody["message"])
	}
}

// TEST-GW-E005-06: repeat cancel against an already-cancelled request is
// idempotent — 202 again (never 404/409).
func TestCancelIdempotentRepeatAccepted(t *testing.T) {
	engine, p := cancelEngine(t)
	defer close(p.release)
	srv := NewServer(engine, ":0")

	id := submitAndWait(t, srv, engine, "cancel me twice", "biz-1", "user-1", false)
	waitUntil(t, "executor task in flight", func() bool { return engine.TaskExecutor().ActiveCount() >= 1 })

	if w := postCancel(srv, id, "biz-1"); w.Code != http.StatusAccepted {
		t.Fatalf("first cancel: expected 202, got %d body=%s", w.Code, w.Body.String())
	}
	waitUntil(t, "cancelled result", func() bool {
		result, ok := engine.GetResult(id)
		return ok && result.Status == "cancelled"
	})

	if w := postCancel(srv, id, "biz-1"); w.Code != http.StatusAccepted {
		t.Fatalf("repeat cancel: expected 202, got %d body=%s", w.Code, w.Body.String())
	}
}

// TEST-GW-E005-07: identity enforcement on + missing credential → 401
// (identity middleware covers the cancel path like every scoped path).
func TestCancelMissingCredential401(t *testing.T) {
	srv := a6Fixture(t, "nx:human:alice", "alice-secret", "biz-A")

	req := httptest.NewRequest("POST", "/api/v1/requests/req-1/cancel?business_id=biz-A", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
	errBody := decodeErrorBody(t, w)
	if errBody["category"] != "UNAUTHORIZED" {
		t.Errorf("expected UNAUTHORIZED, got %v", errBody["category"])
	}
}

// TEST-GW-E005-08: authenticated actor outside the target business → 403
// membership denial (before any core lookup).
func TestCancelNonMember403(t *testing.T) {
	srv := a6Fixture(t, "nx:human:alice", "alice-secret", "biz-A")

	req := httptest.NewRequest("POST", "/api/v1/requests/req-1/cancel?business_id=biz-B", nil)
	a6AuthHeaders(req, "nx:human:alice", "alice-secret")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
	errBody := decodeErrorBody(t, w)
	if errBody["category"] != "AUTHORIZATION" {
		t.Errorf("expected AUTHORIZATION, got %v", errBody["category"])
	}
	if msg, _ := errBody["message"].(string); !bytes.Contains([]byte(msg), []byte("not a member")) {
		t.Errorf("expected membership denial message, got %v", errBody["message"])
	}
}

// TEST-GW-E005-09: the cancel path is an identity-scoped API path, not a
// control path — a configured control API key is NOT required to cancel.
func TestCancelDoesNotRequireControlAPIKey(t *testing.T) {
	engine := fastEngine(t)
	srv := NewServer(engine, ":0", WithControlAPIKey("control-secret"))

	// No X-API-Key header: authMiddleware must not intercept. The 404 proves
	// the handler ran (unknown request), not an auth rejection.
	w := postCancel(srv, "req-no-key", "biz-1")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 from handler, got %d body=%s", w.Code, w.Body.String())
	}
}

// TEST-GW-E005-10: control metrics expose the additive executor.cancelled
// counter after a real cancellation.
func TestControlMetricsIncludeCancelled(t *testing.T) {
	engine, p := cancelEngine(t)
	defer close(p.release)
	srv := NewServer(engine, ":0", WithControlAPIKey("control-secret"))

	id := submitAndWait(t, srv, engine, "cancel for metrics", "biz-1", "user-1", false)
	waitUntil(t, "executor task in flight", func() bool { return engine.TaskExecutor().ActiveCount() >= 1 })

	if w := postCancel(srv, id, "biz-1"); w.Code != http.StatusAccepted {
		t.Fatalf("cancel: expected 202, got %d", w.Code)
	}
	waitUntil(t, "cancelled result", func() bool {
		result, ok := engine.GetResult(id)
		return ok && result.Status == "cancelled"
	})

	req := httptest.NewRequest("GET", "/api/v1/control/metrics", nil)
	req.Header.Set("X-API-Key", "control-secret")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("metrics: expected 200, got %d", w.Code)
	}
	var metrics MetricsResponse
	if err := json.NewDecoder(w.Body).Decode(&metrics); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}
	if metrics.Executor.Cancelled != 1 {
		t.Errorf("expected executor cancelled=1, got %d", metrics.Executor.Cancelled)
	}
	if metrics.Executor.Failed != 0 {
		t.Errorf("cancellation must not count as failure, got failed=%d", metrics.Executor.Failed)
	}
}

// TEST-GW-E005-11: without identity enforcement the fixed unauthenticated
// marker is attributed to the cancellation (G-009), visible on the
// task.cancelled event payload.
func TestCancelAttributionUnauthenticated(t *testing.T) {
	engine, p := cancelEngine(t)
	defer close(p.release)
	srv := NewServer(engine, ":0")

	var received []*event.Event
	_, _ = engine.EventBus().Subscribe(event.ConsumerFunc(func(ev *event.Event) error {
		received = append(received, ev)
		return nil
	}))

	id := submitAndWait(t, srv, engine, "cancel attribution", "biz-1", "user-1", false)
	waitUntil(t, "executor task in flight", func() bool { return engine.TaskExecutor().ActiveCount() >= 1 })

	if w := postCancel(srv, id, "biz-1"); w.Code != http.StatusAccepted {
		t.Fatalf("cancel: expected 202, got %d", w.Code)
	}
	waitUntil(t, "cancelled result", func() bool {
		result, ok := engine.GetResult(id)
		return ok && result.Status == "cancelled"
	})
	if _, err := engine.EventBus().Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	var cancelEvent *event.Event
	for _, ev := range received {
		if ev.Type == event.EventTypeTaskCancelled {
			cancelEvent = ev
			break
		}
	}
	if cancelEvent == nil {
		t.Fatalf("expected task.cancelled event, got %d events", len(received))
	}
	var payload struct {
		TaskID             string `json:"task_id"`
		CancellationReason string `json:"cancellation_reason"`
		Actor              string `json:"actor"`
	}
	if err := json.Unmarshal(cancelEvent.Data, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Actor != "unauthenticated" {
		t.Errorf("expected actor unauthenticated (enforcement off), got %q", payload.Actor)
	}
	if payload.TaskID == "" || payload.CancellationReason == "" {
		t.Errorf("expected contract fields on payload, got %+v", payload)
	}
	if cancelEvent.BusinessID != "biz-1" {
		t.Errorf("expected event business_id biz-1, got %q", cancelEvent.BusinessID)
	}
}
