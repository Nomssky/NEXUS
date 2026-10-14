package event

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// Tap semantics (EXECUTION_TRACE_CONTRACTS §3): the tap observes every
// accepted event synchronously at publish time — before any Dispatch and in
// publish order — exactly once per accepted publish.
func TestMemBusTapObservesAtPublish(t *testing.T) {
	var seen []string
	bus := NewMemBusWithTap(func(e *Event) { seen = append(seen, e.ID) })

	for _, id := range []string{"evt-1", "evt-2", "evt-3"} {
		if err := bus.Publish(&Event{ID: id, Type: EventTypeTaskCreated, Source: "test", Timestamp: time.Now(), Priority: PriorityNormal}); err != nil {
			t.Fatalf("publish %s: %v", id, err)
		}
	}
	// Captured before any dispatch: the tap is publish-time, not delivery-time.
	if len(seen) != 3 || seen[0] != "evt-1" || seen[1] != "evt-2" || seen[2] != "evt-3" {
		t.Fatalf("expected publish-order capture [evt-1 evt-2 evt-3], got %v", seen)
	}
	bus.Dispatch()
	if len(seen) != 3 {
		t.Fatalf("dispatch must not re-tap, got %d observations", len(seen))
	}
}

// A deduplicated event (idempotency key already processed) never reaches the
// tap: a suppressed duplicate is not an additional observation.
func TestMemBusTapSkipsDedupedEvents(t *testing.T) {
	var seen []string
	bus := NewMemBusWithTap(func(e *Event) { seen = append(seen, e.ID) })

	first := &Event{ID: "evt-1", Type: EventTypeTaskCreated, Source: "test", Timestamp: time.Now(), Priority: PriorityNormal, IdempotencyKey: "k-1"}
	dup := &Event{ID: "evt-1-dup", Type: EventTypeTaskCreated, Source: "test", Timestamp: time.Now(), Priority: PriorityNormal, IdempotencyKey: "k-1"}
	if err := bus.Publish(first); err != nil {
		t.Fatalf("publish first: %v", err)
	}
	if err := bus.Publish(dup); err != nil {
		t.Fatalf("publish dup: %v", err)
	}
	if len(seen) != 1 || seen[0] != "evt-1" {
		t.Fatalf("expected exactly one tap observation (evt-1), got %v", seen)
	}
}

// Concurrent publishers race at the single funnel; the tap must be race-free
// and observe every event exactly once.
func TestMemBusTapConcurrentPublishers(t *testing.T) {
	var mu sync.Mutex
	counts := map[string]int{}
	bus := NewMemBusWithTap(func(e *Event) {
		mu.Lock()
		counts[e.ID]++
		mu.Unlock()
	})

	const publishers, perPublisher = 8, 50
	var wg sync.WaitGroup
	for p := 0; p < publishers; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			for i := 0; i < perPublisher; i++ {
				id := fmt.Sprintf("evt-%d-%d", p, i)
				_ = bus.Publish(&Event{ID: id, Type: EventTypeTaskCreated, Source: "test", Timestamp: time.Now(), Priority: PriorityNormal})
			}
		}(p)
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(counts) != publishers*perPublisher {
		t.Fatalf("expected %d distinct tapped events, got %d", publishers*perPublisher, len(counts))
	}
	for id, n := range counts {
		if n != 1 {
			t.Fatalf("event %s tapped %d times, want exactly once", id, n)
		}
	}
}

// A nil tap (the ordinary constructor) keeps the bus behavior unchanged.
func TestMemBusNilTapUnchanged(t *testing.T) {
	bus := NewMemBus()
	var received int
	if _, err := bus.Subscribe(ConsumerFunc(func(e *Event) error { received++; return nil })); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := bus.Publish(&Event{ID: "evt-1", Type: EventTypeTaskCreated, Source: "test", Timestamp: time.Now(), Priority: PriorityNormal}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := bus.Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if received != 1 {
		t.Fatalf("expected 1 delivered event, got %d", received)
	}
}
