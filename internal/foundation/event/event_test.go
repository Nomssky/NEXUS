package event

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TEST-M3-015: Event bus interface compliance
func TestMemBusInterface(t *testing.T) {
	var _ Bus = (*MemBus)(nil)
}

// TEST-M3-016: Publish and dispatch
func TestMemBusPublishDispatch(t *testing.T) {
	bus := NewMemBus()
	received := make([]*Event, 0)

	bus.Subscribe(ConsumerFunc(func(e *Event) error {
		received = append(received, e)
		return nil
	}))

	event := &Event{
		ID:        "evt-1",
		Type:      EventTypeTaskCreated,
		Source:    "test",
		Timestamp: time.Now(),
		Priority:  PriorityNormal,
	}

	bus.Publish(event)
	bus.Dispatch()

	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0].ID != "evt-1" {
		t.Errorf("expected event ID evt-1, got %v", received[0].ID)
	}
}

// TEST-M3-017: Priority ordering
func TestMemBusPriorityOrdering(t *testing.T) {
	bus := NewMemBus()
	received := make([]Priority, 0)

	bus.Subscribe(ConsumerFunc(func(e *Event) error {
		received = append(received, e.Priority)
		return nil
	}))

	// Publish in reverse priority order
	bus.Publish(&Event{ID: "low", Priority: PriorityLow, Timestamp: time.Now()})
	bus.Publish(&Event{ID: "critical", Priority: PriorityCritical, Timestamp: time.Now()})
	bus.Publish(&Event{ID: "normal", Priority: PriorityNormal, Timestamp: time.Now()})

	bus.Dispatch()

	if len(received) != 3 {
		t.Fatalf("expected 3 events, got %d", len(received))
	}
	// Critical should come first
	if received[0] != PriorityCritical {
		t.Errorf("expected critical first, got %v", received[0])
	}
}

// TEST-M3-018: Dedup by idempotency key
func TestMemBusDedup(t *testing.T) {
	bus := NewMemBus()
	var count int32

	bus.Subscribe(ConsumerFunc(func(e *Event) error {
		atomic.AddInt32(&count, 1)
		return nil
	}))

	event := &Event{
		ID:             "evt-1",
		Type:           EventTypeTaskCreated,
		IdempotencyKey: "key-1",
		Timestamp:      time.Now(),
	}

	// Publish same event twice
	bus.Publish(event)
	bus.Publish(event) // should be deduped

	bus.Dispatch()

	if atomic.LoadInt32(&count) != 1 {
		t.Errorf("expected 1 dispatch (dedup), got %d", count)
	}
}

// TEST-M3-019: Wildcard subscription
func TestMemBusWildcardSubscription(t *testing.T) {
	bus := NewMemBus()
	received := make([]EventType, 0)

	bus.Subscribe(ConsumerFunc(func(e *Event) error {
		received = append(received, e.Type)
		return nil
	})) // no event types = wildcard

	bus.Publish(&Event{ID: "e1", Type: EventTypeTaskCreated, Timestamp: time.Now()})
	bus.Publish(&Event{ID: "e2", Type: EventTypeAgentSpawned, Timestamp: time.Now()})

	bus.Dispatch()

	if len(received) != 2 {
		t.Errorf("expected 2 events, got %d", len(received))
	}
}

// TEST-M3-020: Type-specific subscription
func TestMemBusTypeSubscription(t *testing.T) {
	bus := NewMemBus()
	taskCount := int32(0)
	agentCount := int32(0)

	bus.Subscribe(ConsumerFunc(func(e *Event) error {
		atomic.AddInt32(&taskCount, 1)
		return nil
	}), EventTypeTaskCreated)

	bus.Subscribe(ConsumerFunc(func(e *Event) error {
		atomic.AddInt32(&agentCount, 1)
		return nil
	}), EventTypeAgentSpawned)

	bus.Publish(&Event{ID: "e1", Type: EventTypeTaskCreated, Timestamp: time.Now()})
	bus.Publish(&Event{ID: "e2", Type: EventTypeAgentSpawned, Timestamp: time.Now()})

	bus.Dispatch()

	if atomic.LoadInt32(&taskCount) != 1 {
		t.Errorf("expected 1 task event, got %d", taskCount)
	}
	if atomic.LoadInt32(&agentCount) != 1 {
		t.Errorf("expected 1 agent event, got %d", agentCount)
	}
}

// TEST-M3-021: Handler error propagation
func TestMemBusHandlerError(t *testing.T) {
	bus := NewMemBus()

	bus.Subscribe(ConsumerFunc(func(e *Event) error {
		return errors.New("handler failed")
	}))

	bus.Publish(&Event{ID: "e1", Type: EventTypeTaskCreated, Timestamp: time.Now()})

	_, err := bus.Dispatch()
	if err == nil {
		t.Error("expected error from dispatch")
	}
	var deliveryErr *DeliveryError
	if !errors.As(err, &deliveryErr) {
		t.Errorf("expected DeliveryError, got %T", err)
	}
}

// TEST-M3-024: Failing handler retries are bounded — event dropped after
// maxDeliveryAttempts so the dispatch loop cannot spin forever (E-046 fix).
func TestMemBusBoundedRetry(t *testing.T) {
	bus := NewMemBus()

	handleCount := 0
	bus.Subscribe(ConsumerFunc(func(e *Event) error {
		handleCount++
		return errors.New("handler failed")
	}))

	bus.Publish(&Event{ID: "e-fail", Type: EventTypeTaskCreated, Timestamp: time.Now()})

	// Dispatch until the retry budget is exhausted.
	for i := 0; i < maxDeliveryAttempts; i++ {
		_, err := bus.Dispatch()
		if err == nil {
			t.Fatalf("attempt %d: expected error", i+1)
		}
	}

	if handleCount != maxDeliveryAttempts {
		t.Errorf("expected %d handle attempts, got %d", maxDeliveryAttempts, handleCount)
	}

	// Event must be dropped now — queue empty, further dispatch is a no-op.
	if bus.QueueSize() != 0 {
		t.Errorf("expected queue empty after retry budget exhausted, got %d", bus.QueueSize())
	}
	if _, err := bus.Dispatch(); err != nil {
		t.Errorf("expected no error on empty dispatch, got %v", err)
	}
}

// TEST-M3-025: Handler error does not drop remaining events (E-046 fix)
func TestMemBusHandlerErrorKeepsRemaining(t *testing.T) {
	bus := NewMemBus()

	// First consumer fails on the first event only; second event must survive.
	failFirst := true
	bus.Subscribe(ConsumerFunc(func(e *Event) error {
		if e.ID == "e-bad" && failFirst {
			failFirst = false
			return errors.New("transient failure")
		}
		return nil
	}))

	bus.Publish(&Event{ID: "e-bad", Type: EventTypeTaskCreated, Timestamp: time.Now()})
	bus.Publish(&Event{ID: "e-good", Type: EventTypeTaskCreated, Timestamp: time.Now()})

	// First dispatch fails on e-bad, re-queues it and e-good.
	if _, err := bus.Dispatch(); err == nil {
		t.Fatal("expected error on first dispatch")
	}

	// Both events must still be queued (nothing silently dropped).
	if got := bus.QueueSize(); got != 2 {
		t.Errorf("expected 2 events re-queued, got %d", got)
	}

	// Second dispatch: transient failure cleared, both deliver.
	n, err := bus.Dispatch()
	if err != nil {
		t.Fatalf("expected success on retry, got %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 dispatched, got %d", n)
	}
}

// TEST-M3-022: Queue size
func TestMemBusQueueSize(t *testing.T) {
	bus := NewMemBus()

	if bus.QueueSize() != 0 {
		t.Errorf("expected empty queue, got %d", bus.QueueSize())
	}

	bus.Publish(&Event{ID: "e1", Timestamp: time.Now()})
	bus.Publish(&Event{ID: "e2", Timestamp: time.Now()})

	if bus.QueueSize() != 2 {
		t.Errorf("expected queue size 2, got %d", bus.QueueSize())
	}
}

// TEST-M3-023: Unsubscribe
func TestMemBusUnsubscribe(t *testing.T) {
	bus := NewMemBus()
	var count int32

	consumer := ConsumerFunc(func(e *Event) error {
		atomic.AddInt32(&count, 1)
		return nil
	})

	subID, _ := bus.Subscribe(consumer)
	bus.Publish(&Event{ID: "e1", Timestamp: time.Now()})
	bus.Dispatch()

	if atomic.LoadInt32(&count) != 1 {
		t.Fatalf("expected 1 dispatch before unsubscribe, got %d", count)
	}

	bus.Unsubscribe(subID)
	bus.Publish(&Event{ID: "e2", Timestamp: time.Now()})
	bus.Dispatch()

	// Should still be 1 (unsubscribe worked)
	if atomic.LoadInt32(&count) != 1 {
		t.Errorf("expected 1 dispatch after unsubscribe, got %d", count)
	}
}

// TEST-M3-024: Dedup size tracking
func TestMemBusDedupSize(t *testing.T) {
	bus := NewMemBus()

	bus.Publish(&Event{ID: "e1", IdempotencyKey: "k1", Timestamp: time.Now()})
	bus.Publish(&Event{ID: "e2", IdempotencyKey: "k2", Timestamp: time.Now()})
	bus.Publish(&Event{ID: "e3", IdempotencyKey: "k1", Timestamp: time.Now()}) // duplicate

	if bus.DedupSize() != 2 {
		t.Errorf("expected dedup size 2, got %d", bus.DedupSize())
	}
}
