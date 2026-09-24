package event

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// M-043: Once Unsubscribe successfully returns for a consumer, that consumer
// must not receive any subsequent Dispatch invocation. An invocation that
// already began before Unsubscribe completed may finish.

// TestM043UnsubscribeBeforeDispatch — consumer removed before Dispatch is
// never invoked.
func TestM043UnsubscribeBeforeDispatch(t *testing.T) {
	bus := NewMemBus()
	var calls int32

	id, err := bus.Subscribe(ConsumerFunc(func(e *Event) error {
		atomic.AddInt32(&calls, 1)
		return nil
	}))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if err := bus.Unsubscribe(id); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}

	bus.Publish(&Event{ID: "e1", Type: EventTypeCustom, Timestamp: time.Now()})
	if _, err := bus.Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Errorf("invocations after unsubscribe-before-dispatch: want 0, got %d", got)
	}
}

// TestM043UnsubscribeDuringDispatchSkipsStaleSnapshot — deterministic TOCTOU:
// A's Handle fully Unsubscribes B before Dispatch iterates to B from the
// pre-Unsubscribe snapshot. After Unsubscribe(B) returns, B must not begin.
func TestM043UnsubscribeDuringDispatchSkipsStaleSnapshot(t *testing.T) {
	bus := NewMemBus()
	var aCalls, bCalls int32
	var bSubID string

	// A is subscribed first so it appears before B in the wildcard list.
	a := ConsumerFunc(func(e *Event) error {
		atomic.AddInt32(&aCalls, 1)
		// Complete Unsubscribe(B) while Dispatch still holds B in its snapshot.
		if err := bus.Unsubscribe(bSubID); err != nil {
			t.Errorf("unsubscribe B: %v", err)
		}
		return nil
	})
	b := ConsumerFunc(func(e *Event) error {
		atomic.AddInt32(&bCalls, 1)
		return nil
	})

	if _, err := bus.Subscribe(a); err != nil {
		t.Fatalf("subscribe A: %v", err)
	}
	idB, err := bus.Subscribe(b)
	if err != nil {
		t.Fatalf("subscribe B: %v", err)
	}
	bSubID = idB

	bus.Publish(&Event{ID: "e1", Type: EventTypeCustom, Timestamp: time.Now()})
	if _, err := bus.Dispatch(); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if got := atomic.LoadInt32(&aCalls); got != 1 {
		t.Errorf("A invocations: want 1, got %d", got)
	}
	// M-043: Unsubscribe(B) returned inside A.Handle before Dispatch reached B.
	if got := atomic.LoadInt32(&bCalls); got != 0 {
		t.Errorf("M-043: B invoked after Unsubscribe returned: want 0, got %d", got)
	}
}

// TestM043InFlightCallbackMayFinishAfterUnsubscribe — an invocation that
// already began may complete; Unsubscribe must not deadlock waiting for it,
// and no further invocation may start afterward.
func TestM043InFlightCallbackMayFinishAfterUnsubscribe(t *testing.T) {
	bus := NewMemBus()

	started := make(chan struct{})
	release := make(chan struct{})
	var calls int32

	id, err := bus.Subscribe(ConsumerFunc(func(e *Event) error {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			close(started)
			<-release // in-flight: began before Unsubscribe; may finish after
		}
		return nil
	}))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	bus.Publish(&Event{ID: "e1", Type: EventTypeCustom, Timestamp: time.Now()})

	dispatchDone := make(chan error, 1)
	go func() {
		_, err := bus.Dispatch()
		dispatchDone <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("callback did not start")
	}

	// Unsubscribe while Handle is blocked in-flight — must return promptly
	// (no wait-for-callback under the bus lock / at all for in-flight finish).
	unsubDone := make(chan error, 1)
	go func() { unsubDone <- bus.Unsubscribe(id) }()

	select {
	case err := <-unsubDone:
		if err != nil {
			t.Fatalf("unsubscribe: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Unsubscribe blocked on in-flight callback (deadlock risk)")
	}

	// In-flight may finish after Unsubscribe returned.
	close(release)
	select {
	case err := <-dispatchDone:
		if err != nil {
			t.Fatalf("dispatch: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch did not finish")
	}

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("in-flight invocations: want 1, got %d", got)
	}

	// No post-unsubscribe invocation.
	bus.Publish(&Event{ID: "e2", Type: EventTypeCustom, Timestamp: time.Now()})
	if _, err := bus.Dispatch(); err != nil {
		t.Fatalf("dispatch after unsub: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("post-unsubscribe invocations: want 1 total, got %d", got)
	}
}

// TestM043NoPostUnsubscribeInvocationConcurrent — repeated interleaving of
// Dispatch and Unsubscribe; after each Unsubscribe returns, that consumer
// must not be entered again. Race detector validates memory visibility.
func TestM043NoPostUnsubscribeInvocationConcurrent(t *testing.T) {
	const rounds = 50

	for round := 0; round < rounds; round++ {
		bus := NewMemBus()
		var calls int32
		gate := make(chan struct{})

		id, err := bus.Subscribe(ConsumerFunc(func(e *Event) error {
			atomic.AddInt32(&calls, 1)
			<-gate // hold in-flight briefly so Unsubscribe overlaps Dispatch
			return nil
		}))
		if err != nil {
			t.Fatalf("round %d subscribe: %v", round, err)
		}

		bus.Publish(&Event{ID: "e1", Type: EventTypeCustom, Timestamp: time.Now()})

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = bus.Dispatch()
		}()
		go func() {
			defer wg.Done()
			_ = bus.Unsubscribe(id)
		}()

		// Allow both to progress, then release the callback.
		time.Sleep(2 * time.Millisecond)
		close(gate)
		wg.Wait()

		// After both completed, further dispatches must not invoke.
		before := atomic.LoadInt32(&calls)
		bus.Publish(&Event{ID: "e2", Type: EventTypeCustom, Timestamp: time.Now()})
		if _, err := bus.Dispatch(); err != nil {
			t.Fatalf("round %d dispatch: %v", round, err)
		}
		if after := atomic.LoadInt32(&calls); after != before {
			t.Fatalf("round %d: invocation after Unsubscribe returned: before=%d after=%d",
				round, before, after)
		}
	}
}

// TestM043MultipleConsumersStillReceive — existing multi-consumer semantics
// preserved when nobody unsubscribes mid-dispatch.
func TestM043MultipleConsumersStillReceive(t *testing.T) {
	bus := NewMemBus()
	var c1, c2, c3 int32

	bus.Subscribe(ConsumerFunc(func(e *Event) error {
		atomic.AddInt32(&c1, 1)
		return nil
	}))
	bus.Subscribe(ConsumerFunc(func(e *Event) error {
		atomic.AddInt32(&c2, 1)
		return nil
	}))
	bus.Subscribe(ConsumerFunc(func(e *Event) error {
		atomic.AddInt32(&c3, 1)
		return nil
	}))

	bus.Publish(&Event{ID: "e1", Type: EventTypeCustom, Timestamp: time.Now()})
	n, err := bus.Dispatch()
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if n != 3 {
		t.Errorf("dispatched count: want 3, got %d", n)
	}
	if atomic.LoadInt32(&c1) != 1 || atomic.LoadInt32(&c2) != 1 || atomic.LoadInt32(&c3) != 1 {
		t.Errorf("consumers: c1=%d c2=%d c3=%d, want all 1", c1, c2, c3)
	}
}

// TestM043DispatchErrorBehaviorUnchanged — handler error still yields
// DeliveryError and bounded retry semantics (regression guard).
func TestM043DispatchErrorBehaviorUnchanged(t *testing.T) {
	bus := NewMemBus()
	attempts := 0
	bus.Subscribe(ConsumerFunc(func(e *Event) error {
		attempts++
		return errors.New("boom")
	}))

	bus.Publish(&Event{ID: "e-fail", Type: EventTypeCustom, Timestamp: time.Now()})

	for i := 0; i < maxDeliveryAttempts; i++ {
		_, err := bus.Dispatch()
		var de *DeliveryError
		if !errors.As(err, &de) {
			t.Fatalf("attempt %d: want DeliveryError, got %v", i+1, err)
		}
		if de.Code != "HANDLER_ERROR" {
			t.Errorf("attempt %d: code=%q want HANDLER_ERROR", i+1, de.Code)
		}
	}
	if attempts != maxDeliveryAttempts {
		t.Errorf("handle attempts: want %d, got %d", maxDeliveryAttempts, attempts)
	}
	if bus.QueueSize() != 0 {
		t.Errorf("queue after budget: want 0, got %d", bus.QueueSize())
	}
}

// TestM043UnsubscribeUnknownIDStillNil — public API behavior preserved.
func TestM043UnsubscribeUnknownIDStillNil(t *testing.T) {
	bus := NewMemBus()
	if err := bus.Unsubscribe("sub-999"); err != nil {
		t.Errorf("unknown id: want nil, got %v", err)
	}
}
