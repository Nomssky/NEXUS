# NEXUS — Task Executor

This document covers the Task Executor implementation.

---

## What It Is

The Task Executor (`internal/executor/`) is the execution runtime that makes the canonical chain actually do work. It is the missing heartbeat that turns "create state objects" into "execute work."

Before the executor, the chain created workflow objects and scheduled jobs but nothing ever ran them. Steps 8-12 (agent, model, tool, verify, outcome) were fake audit entries. Now they're real.

## How It Works

```
Chain → Submit(WorkRequest) → Executor
  → Governance check (ALLOW/DENY)
  → Find/provision agent
  → Execute via TaskHandler
  → Record outcome
  → Emit events at every step
```

### WorkRequest

```go
type WorkRequest struct {
    TaskID        string
    CorrelationID string
    BusinessID    string
    ActorID       string
    Intent        string
    Input         map[string]string
    Constraints   []string
    Priority      int
    Handler       TaskHandler  // custom execution logic
}
```

### TaskHandler

```go
type TaskHandler func(ctx context.Context, req *WorkRequest, ag *agent.Agent) (*Outcome, error)
```

Custom handlers can be provided per-request. If nil, a default handler simulates execution.

### Concurrency

- Configurable max concurrent tasks (default: 10)
- Capacity check is atomic (no TOCTOU races)
- Agent provisioning serialized (AgentRuntime not thread-safe)

### Events

Emitted at every step:
- `executor.started` — executor started
- `executor.received` — work request received
- `executor.assigned` — agent assigned
- `executor.completed` — task completed
- `executor.failed` — task failed
- `executor.denied` — governance denied

---

## Integration with Core Engine

The executor is wired into `core.Engine`:

```go
engine.TaskExecutor()  // access the executor
```

The chain now calls `chainExecute()` which submits work to the executor and waits for completion via `SubmitSync()`.

---

## Layout

```
internal/executor/
  executor.go      Task Executor (submit, execute, outcomes, events)
  executor_test.go 19 tests (TEST-EXEC-001..019)
```

---

## Testing

- 19 tests covering creation, start/stop, submit, custom handlers, errors, governance denied, sync execution, timeout, metrics, events, correlation ID, business ID, concurrency, capacity
- Core tests updated to use executor (chain now executes real work)
- Foundation M0–M11 tests unchanged
- Race detector clean
