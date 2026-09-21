package event

import (
	"container/heap"
	"fmt"
	"sync"
	"time"
)

// subscriber wraps a Consumer with a unique ID for comparison.
type subscriber struct {
	id       int
	consumer Consumer
}

// MemBus is an in-memory event bus with priority queue and dedup.
// It is suitable for testing and development.
type MemBus struct {
	mu        sync.RWMutex
	queue     *priorityQueue
	consumers map[EventType][]*subscriber
	allSubs   []*subscriber
	dedup     map[string]bool // idempotency key -> processed
	dedupKeys []string        // ordered keys for FIFO eviction
	dedupSize int
	nextID    int
	now       func() time.Time
}

// maxDedupSize limits the dedup map to prevent unbounded memory growth.
const maxDedupSize = 10000

// NewMemBus creates a new in-memory event bus.
func NewMemBus() *MemBus {
	pq := &priorityQueue{}
	heap.Init(pq)

	return &MemBus{
		queue:     pq,
		consumers: make(map[EventType][]*subscriber),
		dedup:     make(map[string]bool),
		now:       time.Now,
	}
}

// Publish publishes an event to the bus.
// If the event has an idempotency key that was already processed, it is skipped.
func (b *MemBus) Publish(event *Event) error {
	if event == nil {
		return &DeliveryError{Code: "INVALID_EVENT", Message: "event is nil"}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Dedup check
	if event.IdempotencyKey != "" {
		if b.dedup[event.IdempotencyKey] {
			return nil // already processed, skip
		}
		b.dedup[event.IdempotencyKey] = true
		b.dedupKeys = append(b.dedupKeys, event.IdempotencyKey)
		b.dedupSize++

		// Evict oldest entries when map exceeds capacity
		for len(b.dedupKeys) > maxDedupSize {
			old := b.dedupKeys[0]
			b.dedupKeys = b.dedupKeys[1:]
			delete(b.dedup, old)
			b.dedupSize--
		}
	}

	// Enqueue
	heap.Push(b.queue, event)

	return nil
}

// Subscribe registers a consumer for events matching the given types.
// If no types are specified, the consumer receives all events.
// Returns a subscription ID for later unsubscription.
func (b *MemBus) Subscribe(consumer Consumer, eventTypes ...EventType) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++
	sub := &subscriber{id: b.nextID, consumer: consumer}
	b.allSubs = append(b.allSubs, sub)

	if len(eventTypes) == 0 {
		// Subscribe to all events
		b.consumers["*"] = append(b.consumers["*"], sub)
	} else {
		for _, et := range eventTypes {
			b.consumers[et] = append(b.consumers[et], sub)
		}
	}

	return fmt.Sprintf("sub-%d", b.nextID), nil
}

// Unsubscribe removes a consumer by subscription ID.
func (b *MemBus) Unsubscribe(subscriptionID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Find the subscriber by ID
	var subID int
	for _, s := range b.allSubs {
		if fmt.Sprintf("sub-%d", s.id) == subscriptionID {
			subID = s.id
			break
		}
	}

	if subID == 0 {
		return nil // not found
	}

	// Remove from all type subscriptions
	for et, subs := range b.consumers {
		for i, s := range subs {
			if s.id == subID {
				b.consumers[et] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
	}

	// Remove from allSubs
	for i, s := range b.allSubs {
		if s.id == subID {
			b.allSubs = append(b.allSubs[:i], b.allSubs[i+1:]...)
			break
		}
	}

	return nil
}

// Dispatch dequeues all events and delivers them to matching consumers.
// Returns the number of events dispatched.
func (b *MemBus) Dispatch() (int, error) {
	b.mu.Lock()
	events := make([]*Event, 0)
	for b.queue.Len() > 0 {
		event := heap.Pop(b.queue).(*Event)
		events = append(events, event)
	}
	b.mu.Unlock()

	dispatched := 0
	for _, event := range events {
		b.mu.RLock()
		consumers := b.getMatchingConsumers(event.Type)
		b.mu.RUnlock()

		for _, sub := range consumers {
			if err := sub.consumer.Handle(event); err != nil {
				return dispatched, &DeliveryError{
					EventID: event.ID,
					Code:    "HANDLER_ERROR",
					Message: err.Error(),
				}
			}
			dispatched++
		}
	}

	return dispatched, nil
}

// QueueSize returns the number of events in the queue.
func (b *MemBus) QueueSize() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.queue.Len()
}

// DedupSize returns the number of idempotency keys tracked.
func (b *MemBus) DedupSize() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.dedupSize
}

// getMatchingConsumers returns subscribers that should receive events of the given type.
// Must be called with mu held (at least RLock).
// Deduplicates: a subscriber registered for both wildcard and type-specific
// subscriptions receives the event only once.
func (b *MemBus) getMatchingConsumers(eventType EventType) []*subscriber {
	seen := make(map[int]bool)
	var result []*subscriber

	// Wildcard subscribers
	if subs, ok := b.consumers["*"]; ok {
		for _, sub := range subs {
			if !seen[sub.id] {
				seen[sub.id] = true
				result = append(result, sub)
			}
		}
	}

	// Type-specific subscribers
	if subs, ok := b.consumers[eventType]; ok {
		for _, sub := range subs {
			if !seen[sub.id] {
				seen[sub.id] = true
				result = append(result, sub)
			}
		}
	}

	return result
}

// priorityQueue implements heap.Interface for events.
type priorityQueue []*Event

func (pq priorityQueue) Len() int { return len(pq) }

func (pq priorityQueue) Less(i, j int) bool {
	// Higher priority first
	return pq[i].Priority > pq[j].Priority
}

func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *priorityQueue) Push(x interface{}) {
	*pq = append(*pq, x.(*Event))
}

func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	*pq = old[:n-1]
	return item
}
