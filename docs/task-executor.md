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
- `task.cancelled` — cancellation accepted for an in-flight task (contract event, payload: `task_id`, `cancellation_reason`, `actor`)

### Cancellation

`CancelTask(taskID, reason, actor)` cancels an in-flight task (E-005):

- Cancels the task's base context; the handler's `ctx` sees cancellation and
  should stop cooperatively. The handler's context is derived from the
  executor's `baseCtx`, whose values still flow through (`execCtx`).
- Governance is checked at execution start — a cancel that lands before the
  handler runs means the handler **never runs** (no side effects).
- Terminal arbitration is first-cause-wins: a cancel that arrives after the
  handler finished loses (`ErrTaskCompleted`); a handler that ignores
  cancellation and returns `nil` **completes** (non-cooperative limitation —
  the executor never relabels a completed task as cancelled).
- An already-completed task conflicts (`ErrTaskCompleted`); an unknown task
  reports `ErrTaskNotFound`; repeats are idempotent.
- Releasing the concurrency slot happens immediately, not when the handler
  observes cancellation.
- Emits the contract `task.cancelled` event and increments `CancelledCount()`
  (exposed additively as `executor.cancelled` in control metrics — the
  `Metrics()` three-value signature is unchanged).

**Timeouts vs. cancellation:** wait-context *expiry* (deadline/`WaitOutcome`
timeout) keeps the existing timeout semantics — the task ends `failed` with
`wait context expired: ...`, never relabeled as user cancellation. Only an
explicit cancel produces `cancelled`.

### Provider context threading

The default handler passes its `ctx` through `ModelRouter.Invoke(ctx, ...)` →
`Provider.Invoke(ctx, ...)`. Both the model-router entry points and the
built-in providers check `ctx.Err()` first, so a cancellation stops the
default handler at the provider boundary as well.

---

## Integration with Core Engine

The executor is wired into `core.Engine`:

```go
engine.TaskExecutor()  // access the executor
```

The chain now calls `chainExecute()` which submits work to the executor and waits for completion via `SubmitSync()`.

When a request is cancelled externally, the chain maps the executor's
`cancelled` outcome to `Response.Status = "cancelled"` with error code
`CANCELLED` / category `CANCELLATION` (contract §110), marks the workflow
`cancelled` via `workflowEng.Cancel` (bookkeeping only), and emits
`chain.cancelled`. A cancellation that lands before submit aborts the chain
before any task is created — the chain never becomes `failed` because of a
user cancellation.

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
- 11 cancellation tests (`TestCancel*`): cancel running/before-handler, terminal conflicts, idempotent repeats, waiter delivery, slot release, payload, timeout-vs-cancel distinction (handler-stop proof)
- Core tests updated to use executor (chain now executes real work)
- Foundation M0–M11 tests unchanged
- Race detector clean
