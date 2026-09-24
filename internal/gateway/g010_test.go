package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
)

// G-010: one-shot GET responses and each SSE event frame must be size-bounded.
// SSE streams remain long-lived: no connection-duration cap and no cumulative
// stream-byte cap — only per-event and per-response bounds.

// g010Engine starts an engine with deterministic clock for G-010 tests.
func g010Engine(t *testing.T) *core.Engine {
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
	return engine
}

// g010SubmitAndWait submits a request and returns its request_id after the
// result exists (HTTP 200, or 413 when a tiny response cap already rejects
// the encoded body — both prove the result is stored).
func g010SubmitAndWait(t *testing.T, srv *Server) string {
	t.Helper()
	body := `{"intent": "g010", "business_id": "biz-1", "actor_id": "user-1"}`
	req := httptest.NewRequest("POST", "/api/v1/requests", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("submit: expected 202, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)

	deadline := time.Now().Add(2 * time.Second)
	for {
		greq := httptest.NewRequest("GET", "/api/v1/requests/"+resp["request_id"]+"?business_id=biz-1", nil)
		gw := httptest.NewRecorder()
		srv.Mux().ServeHTTP(gw, greq)
		if gw.Code == http.StatusOK || gw.Code == http.StatusRequestEntityTooLarge {
			return resp["request_id"]
		}
		if time.Now().After(deadline) {
			t.Fatalf("result not ready: last status %d", gw.Code)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// g010SSE runs handleSSE in a goroutine with a cancellable request context.
type g010SSE struct {
	w      *httptest.ResponseRecorder
	cancel context.CancelFunc
	done   chan struct{}
}

// startSSE begins an SSE stream for business_id=biz-1 and waits until the
// handler has subscribed to the event bus.
func startSSE(t *testing.T, srv *Server) *g010SSE {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req := httptest.NewRequest("GET", "/events?business_id=biz-1", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.Handler().ServeHTTP(w, req)
	}()
	// Allow identity middleware + Subscribe to complete before publishing.
	time.Sleep(50 * time.Millisecond)
	return &g010SSE{w: w, cancel: cancel, done: done}
}

// publishDispatch publishes an event for biz-1 and dispatches the bus.
func publishDispatch(t *testing.T, engine *core.Engine, e *event.Event) {
	t.Helper()
	e.BusinessID = "biz-1"
	if e.ID == "" {
		e.ID = "g010-" + time.Now().Format("150405.000000000")
	}
	if e.Type == "" {
		e.Type = event.EventType("custom")
	}
	if e.Source == "" {
		e.Source = "g010-test"
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	if err := engine.EventBus().Publish(e); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := engine.EventBus().Dispatch(); err != nil {
		// Delivery errors from other consumers must not fail setup.
		t.Logf("dispatch: %v", err)
	}
}

// finish cancels the stream, waits for the handler to return, and returns
// the full response body. Reading the body only after the handler exits
// avoids concurrent access to httptest.ResponseRecorder.
func (s *g010SSE) finish(t *testing.T) string {
	t.Helper()
	s.cancel()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE handler did not return after context cancel")
	}
	return s.w.Body.String()
}

// stillOpen reports whether the SSE handler has not yet returned.
func (s *g010SSE) stillOpen() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

// A. Oversized one-shot GET result → bounded rejection (413), not full body.
func TestG010OversizedResultRejected(t *testing.T) {
	engine := g010Engine(t)
	// Tiny cap: any real result (~2 KB) exceeds it without multi-MB fixtures.
	srv := NewServer(engine, ":0", WithMaxResponseBytes(64))
	id := g010SubmitAndWait(t, srv)

	req := httptest.NewRequest("GET", "/api/v1/requests/"+id+"?business_id=biz-1", nil)
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized result: expected 413, got %d body_len=%d", w.Code, w.Body.Len())
	}
	// The bound applies to the result payload, not the fixed error envelope.
	// Reject with a small JSON error — never the full result (~2 KB here).
	if w.Body.Len() >= 2000 {
		t.Errorf("oversized result body not bounded: wrote %d bytes", w.Body.Len())
	}
	var errResp map[string]map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("expected JSON error body, got %q", w.Body.String())
	}
	if errResp["error"]["category"] != "RESOURCE_LIMIT" {
		t.Errorf("expected RESOURCE_LIMIT, got %v", errResp["error"]["category"])
	}
	// Must not leak the full result payload.
	if strings.Contains(w.Body.String(), `"audit_trace"`) {
		t.Error("oversized result leaked audit_trace payload")
	}
}

// C. Normal one-shot GET result with default limit remains unchanged (200 + JSON).
func TestG010NormalResultUnchanged(t *testing.T) {
	engine := g010Engine(t)
	srv := NewServer(engine, ":0", WithClock(time.Now))
	if srv.maxResponseBytes != defaultMaxResponseBytes {
		t.Fatalf("default maxResponseBytes: want %d, got %d", defaultMaxResponseBytes, srv.maxResponseBytes)
	}
	id := g010SubmitAndWait(t, srv)

	req := httptest.NewRequest("GET", "/api/v1/requests/"+id+"?business_id=biz-1", nil)
	w := httptest.NewRecorder()
	srv.Mux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("normal result: expected 200, got %d", w.Code)
	}
	if w.Body.Len() == 0 || w.Body.Len() > defaultMaxResponseBytes {
		t.Fatalf("normal result size unexpected: %d", w.Body.Len())
	}
	var result core.Response
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("normal result not valid JSON: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("expected completed, got %s", result.Status)
	}
}

// B. Oversized SSE event is not written; stream stays open (not torn down).
func TestG010OversizedSSEEventDropped(t *testing.T) {
	engine := g010Engine(t)
	// Per-event cap far below a large Data payload.
	srv := NewServer(engine, ":0", WithMaxSSEEventBytes(256))
	sse := startSSE(t, srv)

	// Large payload: Event.Data is JSON-marshaled as base64 (~4/3 expansion),
	// so the frame far exceeds the 256-byte cap. Use a distinctive pattern.
	big := make([]byte, 4096)
	for i := range big {
		big[i] = 'A'
	}
	bigB64 := base64.StdEncoding.EncodeToString(big)
	publishDispatch(t, engine, &event.Event{
		ID:   "g010-big",
		Type: event.EventType("custom"),
		Data: big,
	})

	// Give the consumer a moment to run; stream must remain open.
	time.Sleep(50 * time.Millisecond)
	if !sse.stillOpen() {
		t.Fatal("SSE stream terminated after oversized event (must stay open)")
	}

	// Deliver a normal-sized event after the drop to prove the stream still works.
	publishDispatch(t, engine, &event.Event{
		ID:   "g010-after",
		Type: event.EventType("custom"),
		Data: []byte(`{"ok":true}`),
	})
	time.Sleep(50 * time.Millisecond)
	if !sse.stillOpen() {
		t.Fatal("SSE stream terminated after follow-up event")
	}

	body := sse.finish(t)
	// []byte payloads serialize as base64 — match the encoded form, not raw bytes.
	if strings.Contains(body, bigB64[:64]) {
		t.Error("oversized SSE event payload was written to the stream")
	}
	if strings.Contains(body, `"id":"g010-big"`) {
		t.Error("oversized SSE event frame was written to the stream")
	}
	if !strings.Contains(body, `"id":"g010-after"`) {
		t.Errorf("follow-up normal event not delivered; body_len=%d", len(body))
	}
}

// D. Normal SSE event remains deliverable under the default per-event cap.
func TestG010NormalSSEEventDelivered(t *testing.T) {
	engine := g010Engine(t)
	srv := NewServer(engine, ":0")
	if srv.maxSSEEventBytes != defaultMaxSSEEventBytes {
		t.Fatalf("default maxSSEEventBytes: want %d, got %d", defaultMaxSSEEventBytes, srv.maxSSEEventBytes)
	}
	sse := startSSE(t, srv)

	publishDispatch(t, engine, &event.Event{
		ID:   "g010-normal",
		Type: event.EventType("custom"),
		Data: []byte(`{"hello":"world"}`),
	})
	time.Sleep(50 * time.Millisecond)

	body := sse.finish(t)
	if !strings.Contains(body, "text/event-stream") && !strings.Contains(sse.w.Header().Get("Content-Type"), "text/event-stream") {
		// Content-Type checked via header after handler returns.
	}
	if ct := sse.w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %q", ct)
	}
	if !strings.Contains(body, `"id":"g010-normal"`) {
		t.Errorf("normal SSE event not delivered; body=%q", body)
	}
	if !strings.Contains(body, "event: custom") {
		t.Errorf("SSE frame format broken; body=%q", body)
	}
}

// E. Bound is per-event, not cumulative: several frames each under the cap
// (sum > cap) are all delivered; the stream is not torn down.
func TestG010SSEPerEventNotCumulative(t *testing.T) {
	engine := g010Engine(t)
	// Cap sized so each small event fits but their sum exceeds the cap.
	srv := NewServer(engine, ":0", WithMaxSSEEventBytes(400))
	sse := startSSE(t, srv)

	// Measure a typical frame: if a single normal event exceeds 400 the test
	// would false-fail; use compact Data and assert delivery of many events.
	const n = 8
	for i := 0; i < n; i++ {
		publishDispatch(t, engine, &event.Event{
			ID:   "g010-cum-" + string(rune('a'+i)),
			Type: event.EventType("custom"),
			Data: []byte(`{"n":1}`),
		})
	}
	time.Sleep(100 * time.Millisecond)
	if !sse.stillOpen() {
		t.Fatal("SSE stream terminated — bound must be per-event, not cumulative")
	}

	body := sse.finish(t)
	delivered := strings.Count(body, "event: custom")
	if delivered < n {
		t.Errorf("expected %d per-event deliveries (sum may exceed cap), got %d; body_len=%d",
			n, delivered, len(body))
	}
	if len(body) <= 400 {
		t.Errorf("cumulative body %d should exceed per-event cap 400 to prove non-cumulative bound", len(body))
	}
}

// E2. Long-lived semantics: the stream stays open across multiple dispatch
// cycles and is only closed by client disconnect (context cancel).
func TestG010SSELongLivedUntilDisconnect(t *testing.T) {
	engine := g010Engine(t)
	srv := NewServer(engine, ":0")
	sse := startSSE(t, srv)

	for round := 0; round < 3; round++ {
		publishDispatch(t, engine, &event.Event{
			ID:   "g010-live-" + string(rune('0'+round)),
			Type: event.EventType("custom"),
			Data: []byte(`{"round":1}`),
		})
		time.Sleep(30 * time.Millisecond)
		if !sse.stillOpen() {
			t.Fatalf("stream closed during round %d without client disconnect", round)
		}
	}

	body := sse.finish(t)
	if !strings.Contains(body, "g010-live-") {
		t.Errorf("expected live events in stream; body=%q", body)
	}
}

// F. Client disconnect / context cancellation still terminates the handler.
func TestG010SSEClientDisconnectCancels(t *testing.T) {
	engine := g010Engine(t)
	srv := NewServer(engine, ":0")
	sse := startSSE(t, srv)

	if !sse.stillOpen() {
		t.Fatal("stream should be open before disconnect")
	}

	// finish() cancels the request context (client disconnect) and waits.
	start := time.Now()
	_ = sse.finish(t)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("handler took too long to exit after disconnect: %v", elapsed)
	}
	if sse.stillOpen() {
		t.Fatal("handler still running after disconnect")
	}
}

// Defaults are positive and match the documented 1 MB edge convention.
func TestG010DefaultLimitsConfigured(t *testing.T) {
	engine := g010Engine(t)
	srv := NewServer(engine, ":0")
	if srv.maxResponseBytes != 1<<20 {
		t.Errorf("maxResponseBytes default: want 1<<20, got %d", srv.maxResponseBytes)
	}
	if srv.maxSSEEventBytes != 1<<20 {
		t.Errorf("maxSSEEventBytes default: want 1<<20, got %d", srv.maxSSEEventBytes)
	}
}
