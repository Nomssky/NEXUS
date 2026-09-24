package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
)

// G-011: SSE write/flush failures must not be silently treated as successful
// delivery. The handler must terminate on an unwritable stream, release the
// subscription, and stop writing further frames. Normal context cancellation
// remains a clean exit (not an internal error). The consumer must not return
// an error to the event bus for a doomed write (MemBus would re-queue the
// event and abort the rest of the dispatch batch).

// sseFailWriter is a minimal http.ResponseWriter + http.Flusher test double.
// httptest.ResponseRecorder never fails Write or flush, so G-011 needs an
// explicit double that can inject both failure modes. FlushError is what
// http.ResponseController.Flush() prefers over classic Flush() (Go 1.20+).
type sseFailWriter struct {
	header http.Header

	mu       sync.Mutex
	buf      bytes.Buffer
	writes   int // Write call count (including failed attempts)
	flushes  int // FlushError call count
	failFrom int // fail Write when write index >= failFrom; -1 disables
	flushErr error
}

func newSSEFailWriter() *sseFailWriter {
	return &sseFailWriter{
		header:   make(http.Header),
		failFrom: -1,
	}
}

func (w *sseFailWriter) Header() http.Header { return w.header }

func (w *sseFailWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	idx := w.writes
	w.writes++
	if w.failFrom >= 0 && idx >= w.failFrom {
		return 0, errors.New("sseFailWriter: write failed")
	}
	return w.buf.Write(b)
}

func (w *sseFailWriter) WriteHeader(int) {}

// Flush implements http.Flusher (no error — classic interface).
func (w *sseFailWriter) Flush() {}

// FlushError is preferred by http.ResponseController.Flush.
func (w *sseFailWriter) FlushError() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushes++
	return w.flushErr
}

func (w *sseFailWriter) writeCalls() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writes
}

func (w *sseFailWriter) body() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// g011SSE runs handleSSE against a custom writer with a cancellable context.
type g011SSE struct {
	w      *sseFailWriter
	cancel context.CancelFunc
	done   chan struct{}
}

// startSSEWithWriter begins GET /events?business_id=biz-1 using the given
// ResponseWriter and waits briefly for subscription.
func startSSEWithWriter(t *testing.T, srv *Server, w *sseFailWriter) *g011SSE {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req := httptest.NewRequest("GET", "/events?business_id=biz-1", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.Handler().ServeHTTP(w, req)
	}()
	time.Sleep(50 * time.Millisecond)
	return &g011SSE{w: w, cancel: cancel, done: done}
}

func (s *g011SSE) stillOpen() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

// waitExit waits for the handler to return without cancelling the request
// context — used when the handler must stop due to write/flush failure alone.
func (s *g011SSE) waitExit(t *testing.T) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE handler did not stop after stream failure (still waiting on ctx only)")
	}
}

func (s *g011SSE) finishCancel(t *testing.T) {
	t.Helper()
	s.cancel()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE handler did not return after context cancel")
	}
}

// g011Publish dispatches one biz-1 event on the engine bus.
func g011Publish(t *testing.T, engine *core.Engine, id string) {
	t.Helper()
	e := &event.Event{
		ID:         id,
		Type:       event.EventType("custom"),
		Source:     "g011-test",
		Timestamp:  time.Now(),
		BusinessID: "biz-1",
		Data:       []byte(`{"probe":true}`),
	}
	if err := engine.EventBus().Publish(e); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := engine.EventBus().Dispatch(); err != nil {
		t.Logf("dispatch: %v", err)
	}
}

// A. Fprintf/Write failure causes the SSE handler to stop (no ctx cancel).
func TestG011WriteFailureStopsHandler(t *testing.T) {
	engine := g010Engine(t)
	srv := NewServer(engine, ":0")
	w := newSSEFailWriter()
	w.failFrom = 0 // every Write fails

	sse := startSSEWithWriter(t, srv, w)
	if !sse.stillOpen() {
		t.Fatal("stream should be open before any event")
	}

	g011Publish(t, engine, "g011-write-fail")
	sse.waitExit(t) // must stop without test cancelling ctx

	if w.writeCalls() == 0 {
		t.Fatal("expected at least one Write attempt before failure")
	}
}

// B. Flush failure causes the SSE handler to stop.
func TestG011FlushFailureStopsHandler(t *testing.T) {
	engine := g010Engine(t)
	srv := NewServer(engine, ":0")
	w := newSSEFailWriter()
	w.flushErr = errors.New("sseFailWriter: flush failed")

	sse := startSSEWithWriter(t, srv, w)

	g011Publish(t, engine, "g011-flush-fail")
	sse.waitExit(t)

	if w.flushes == 0 {
		t.Fatal("expected at least one FlushError attempt")
	}
}

// C. Successful SSE write+flush still delivers and stays open until cancel.
func TestG011SuccessfulWriteStillStreams(t *testing.T) {
	engine := g010Engine(t)
	srv := NewServer(engine, ":0")
	w := newSSEFailWriter() // no failures injected

	sse := startSSEWithWriter(t, srv, w)
	g011Publish(t, engine, "g011-ok")
	time.Sleep(50 * time.Millisecond)

	if !sse.stillOpen() {
		t.Fatal("healthy stream must stay open until client disconnect")
	}
	if !strings.Contains(w.body(), `"id":"g011-ok"`) {
		t.Errorf("event not written; body=%q", w.body())
	}
	if w.flushes == 0 {
		t.Error("expected FlushError on successful delivery")
	}

	sse.finishCancel(t)
	if sse.stillOpen() {
		t.Fatal("handler should exit after context cancel")
	}
}

// D. Client context cancellation remains a clean normal exit path.
func TestG011ContextCancelCleanExit(t *testing.T) {
	engine := g010Engine(t)
	srv := NewServer(engine, ":0")
	w := newSSEFailWriter()

	sse := startSSEWithWriter(t, srv, w)
	if !sse.stillOpen() {
		t.Fatal("stream should start open")
	}
	sse.finishCancel(t)
	if sse.stillOpen() {
		t.Fatal("handler still running after cancel")
	}
}

// E+F. After a write failure: handler exits, subscription is cleaned up, and
// no further frames are written even if more events are published.
func TestG011WriteFailureCleansUpAndStopsWrites(t *testing.T) {
	engine := g010Engine(t)
	srv := NewServer(engine, ":0")
	w := newSSEFailWriter()
	// Allow the first Write (event 1) to succeed, fail subsequent Writes.
	w.failFrom = 1

	sse := startSSEWithWriter(t, srv, w)

	// Event 1: Write succeeds (index 0 < failFrom). Event 2: Write fails.
	g011Publish(t, engine, "g011-first")
	time.Sleep(30 * time.Millisecond)
	if !sse.stillOpen() {
		t.Fatal("stream should remain open after successful first event")
	}
	g011Publish(t, engine, "g011-second")
	sse.waitExit(t)

	writesAtExit := w.writeCalls()
	bodyAtExit := w.body()

	// Publish more events: a leaked subscription would attempt more Writes.
	for i := 0; i < 5; i++ {
		g011Publish(t, engine, fmt.Sprintf("g011-after-%d", i))
	}
	time.Sleep(100 * time.Millisecond)

	if got := w.writeCalls(); got != writesAtExit {
		t.Errorf("writes after handler exit: want %d, got %d (subscription not cleaned up or stream still writing)",
			writesAtExit, got)
	}
	if w.body() != bodyAtExit {
		t.Error("body changed after handler exit — frames written after unwritable stream")
	}
}

// G-010 regression guard: oversized events are still dropped without killing
// the stream when writes are otherwise healthy.
func TestG011PreservesG010OversizedDrop(t *testing.T) {
	engine := g010Engine(t)
	srv := NewServer(engine, ":0", WithMaxSSEEventBytes(256))
	w := newSSEFailWriter()
	sse := startSSEWithWriter(t, srv, w)

	big := make([]byte, 4096)
	if err := engine.EventBus().Publish(&event.Event{
		ID:         "g011-big",
		Type:       event.EventType("custom"),
		Source:     "g011-test",
		Timestamp:  time.Now(),
		BusinessID: "biz-1",
		Data:       big,
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := engine.EventBus().Dispatch(); err != nil {
		t.Logf("dispatch: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	if !sse.stillOpen() {
		t.Fatal("oversized drop must not terminate the stream (G-010)")
	}
	if strings.Contains(w.body(), `"id":"g011-big"`) {
		t.Error("oversized frame should be dropped, not written")
	}
	sse.finishCancel(t)
}
