# NEXUS — Development (M3 Persistence, Events & Observability)

This document covers **only** M3: the persistence, event substrate, and
observability layers. It does not duplicate architecture or contract docs.

> M3 extends M0–M2. It adds **durable storage, event bus with priority
> queues and dedup, and observability (tracing, metrics, audit)**. It does
> **not** implement cognition, agents, tools, or autonomous behavior (M4+).

---

## Scope (C05 + C06 + C07)

| Component | What M3 adds |
|---|---|
| **C05 Persistence** | Store interface, Record envelope, MemStore (in-memory reference impl), optimistic concurrency, business-scoped filtering |
| **C06 Event Substrate** | Event bus, priority queue, consumer subscriptions, dedup/idempotency, delivery modes, event types |
| **C07 Observability** | Distributed tracing (spans, traces), metrics collection, audit logging, correlation IDs, redaction |

### Invariants preserved by M3

- **Events are facts, never commands:** the event substrate records what happened, not what should happen.
- **Observability cannot grant authority:** audit records are informational only.
- **Secrets redacted before storage:** audit details use "REDACTED" for sensitive values.
- **Optimistic concurrency:** store updates require matching version numbers.
- **Business isolation:** store queries respect business scope boundaries.
- **Default deny:** store returns nil for non-existent records.

---

## Layout

```
internal/foundation/
  store/
    store.go       Record envelope, Store interface, Filter, errors
    memstore.go    In-memory Store implementation (testing/development)
  event/
    event.go       Event type, Bus/Queue/Consumer interfaces, DeliveryMode
    membus.go      In-memory Bus with priority queue and dedup
  observability/
    observability.go   Tracer, MetricsCollector, AuditLog interfaces, Span, Metric, AuditRecord
    memory.go          In-memory Recorder implementation
```

---

## Usage

### Store (C05)

```go
s := store.NewMemStore()
s.Put(&store.Record{ID: "r1", Type: store.RecordTypeTask, Data: data})
record, _ := s.Get("r1")
records, _ := s.List(store.Filter{Type: store.RecordTypeTask, BusinessID: "biz-1"})
```

### Event Bus (C06)

```go
bus := event.NewMemBus()
subID, _ := bus.Subscribe(event.ConsumerFunc(func(e *event.Event) error {
    log.Printf("received: %s", e.Type)
    return nil
}), event.EventTypeTaskCreated)

bus.Publish(&event.Event{ID: "e1", Type: event.EventTypeTaskCreated, Priority: event.PriorityHigh})
bus.Dispatch()
bus.Unsubscribe(subID)
```

### Observability (C07)

```go
rec := observability.NewMemRecorder()

// Tracing
span := rec.StartTrace("my-operation")
child := rec.StartSpan(span, "child-operation")
rec.FinishSpan(child, observability.TraceStatusOK)
rec.FinishSpan(span, observability.TraceStatusOK)

// Metrics
rec.RecordMetric(observability.Metric{Name: "latency", Value: 0.5})

// Audit
rec.RecordAudit(&observability.AuditRecord{
    Actor: "agent-1", Action: "tool_invoke", Resource: "tool-1", Outcome: "success",
})
```

---

## Testing

- 44 tests covering TEST-M3-001..044
- M0–M2 tests unchanged and passing
- Race detector clean
- Locked-layer guard passes (no `contracts/` or `Core/` changes)

---

## Invariants Preserved

- Events are immutable facts (structural)
- Observability cannot grant authority (no authority fields in AuditRecord)
- Secrets redacted before storage (tested with REDACTED values)
- Optimistic concurrency (version conflict on stale updates)
- Business isolation (scoped queries)
