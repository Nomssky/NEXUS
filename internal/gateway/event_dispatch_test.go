package gateway

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/core"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
)

// postControl issues an authenticated control-plane request against a live
// gateway address.
func postControl(t *testing.T, base, path, key string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+path, strings.NewReader(""))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("X-API-Key", key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// TEST-N4: event delivery is driven by the gateway's dispatchLoop, which only
// drains while the engine is Running — a paused engine deliberately delivers
// nothing. This pins the two halves of that contract: the pause really does
// stop delivery, and the backlog it accumulates is NOT lost — Resume resumes
// delivery and the queued event reaches its subscriber. Losing the backlog
// would make every event published during a maintenance pause invisible.
func TestPauseStopsDispatchAndResumeDeliversBacklog(t *testing.T) {
	engine, err := core.NewEngine(nil)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = engine.Stop(ctx) })

	const key = "n4-dispatch-key"
	addr := listenAndServe(t, engine, func(s *Server) { s.controlAPIKey = key })
	base := "http://" + addr

	var mu sync.Mutex
	seen := make(map[string]bool)
	_, err = engine.EventBus().Subscribe(event.ConsumerFunc(func(ev *event.Event) error {
		mu.Lock()
		seen[ev.ID] = true
		mu.Unlock()
		return nil
	}))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	observed := func(id string) bool {
		mu.Lock()
		defer mu.Unlock()
		return seen[id]
	}

	if code := postControl(t, base, "/api/v1/control/pause", key); code != http.StatusOK {
		t.Fatalf("pause: got %d", code)
	}
	if got := engine.Status(); got != "STOPPED" {
		t.Fatalf("expected STOPPED after pause, got %s", got)
	}

	// Published while paused: it must sit in the queue, not be dropped and
	// not be delivered until the engine runs again.
	const marker = "n4-marker-while-paused"
	if err := engine.EventBus().Publish(&event.Event{
		ID:        marker,
		Type:      "n4.dispatch.probe",
		Source:    "gateway:test",
		Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	time.Sleep(400 * time.Millisecond) // several 100ms dispatch ticks
	if observed(marker) {
		t.Fatal("event delivered while the engine is paused — the dispatch gate must hold")
	}

	if code := postControl(t, base, "/api/v1/control/resume", key); code != http.StatusOK {
		t.Fatalf("resume: got %d", code)
	}

	deadline := time.Now().Add(5 * time.Second)
	for !observed(marker) {
		if time.Now().After(deadline) {
			t.Fatal("queued event lost across pause/resume — dispatch never delivered it after Resume")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
