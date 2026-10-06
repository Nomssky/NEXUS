# Tool Retries (v1)

Contract: `contracts/OPERATIONAL_RELIABILITY_CONTRACTS.md` §1–§3, §5.
Implementation: `internal/capability/semantics.go` (`DecideRetry`,
`RetryRecommended`, `RetryAdmitted`, `EffectiveClass`) and `Platform.runAttempts`.

There is exactly **one** retry verdict in the codebase. Adapters never retry
themselves and never sleep-and-retry; they return one error and the platform
decides.

## Effective side-effect class

Retryability is a **class** decision, never a name guess. The manifest's
tool-level class is the conservative default. A capability may narrow it for one
operation by implementing `OperationSideEffects` — and the only legal narrowing
is to `read`:

```go
// internal/capability/http.go
func (t *HTTPTool) OperationSideEffect(op string) (tool.SideEffectClass, bool) {
    if isReadMethod(op) { return tool.SideEffectRead, true } // GET, HEAD
    return tool.SideEffectWrite, true                        // POST/PUT/PATCH/DELETE
}
```

So `http.request` (declared `write`) takes the **read** retry policy for `get`
and the **mutation** policy for `post`. An adapter declaring a *wider* class than
its manifest is ignored.

## The matrix

| effective class | error class | retry? |
|---|---|---|
| `read` | `external error`, `timeout`, `network blocked`, deadline exceeded | **yes** (once) |
| `read` | `validation`, `permission denied`, `scope denied`, `credential unavailable`, `resource limit`, `internal adapter failure` | no |
| `read` | `cancelled` | no — an explicit cancel is not a retry condition |
| `read` | `unknown` | **no** |
| mutation | `request never sent` (`ErrNotSent`) **and** the capability supports idempotency | **yes** (once) |
| mutation | everything else, including `unknown` | no |

`RetryAdmitted` is the full rule:

```go
func RetryAdmitted(err error, class tool.SideEffectClass, adapter tool.Adapter, attemptsMade int) bool {
    if !DecideRetry(err, class, attemptsMade) { return false } // budget: 2 attempts per call
    if class == tool.SideEffectRead { return true }
    return errors.Is(err, ErrNotSent) && adapterSupportsIdempotency(adapter)
}
```

## Why mutations need "never sent"

`ErrNotSent` is raised by the HTTP adapter only for failures that provably
happened before the request left the process — a DNS failure or a connect-phase
error (`net.OpError{Op:"dial"}`):

* dial refused → nothing was sent → the re-issue cannot duplicate anything;
* write started, response lost → **cannot tell** → `ErrUnknownOutcome` → never
  retried, regardless of idempotency support.

That is the whole difference between a safe retry and a duplicate, and it is
decided by transport evidence, not by optimism.

## The deadline is shared, never restarted

The call gets one deadline. The attempt budget is a share of it:

```go
slice := limits.MaxDuration
if bound > 1 { slice = time.Until(deadline) / time.Duration(bound) } // 2 attempts
attemptCtx, cancel := context.WithTimeout(callCtx, slice)
```

Consequences, all observable:

* a hang costs the *call's* deadline, not twice the deadline per attempt;
* a retry that starts with almost no time left is cut short by its parent
  context — it can never extend the call;
* when no time remains at all, the loop stops before dispatching anything.

## Terminal frames

```text
tool.attempt.started    {"tool_id","operation","call_id","attempt","max_attempts"}
tool.attempt.completed  {"tool_id","operation","call_id","attempt","outcome","duration_ms","error_class"}
tool.retry.scheduled    {"tool_id","operation","call_id","attempt","error_class"}
```

`error_class` is the taxonomy of §16 (`validation`, `permission denied`,
`scope denied`, `credential unavailable`, `capability_disabled`, `not sent`,
`network blocked`, `timeout`, `cancelled`, `resource limit`, `unknown outcome`,
`external error`, `internal adapter failure`) — never the raw error text.

## Worked examples

| situation | attempts | outcome | retried |
|---|---|---|---|
| GET, response lost mid-flight, second GET OK | 2 | `completed` | yes |
| GET, host unreachable (dial refused) | 2 | `failed` (`not sent`) | yes |
| POST, response lost after dispatch | 1 | `unknown` | **no** |
| POST, dial refused (idempotency-capable) | 2 | second attempt's outcome | yes |
| POST, dial refused (no idempotency support) | 1 | `failed` | no |
| GET, host not on the allowlist | 1 | `failed` (`network blocked`) | no |
| any, caller cancels mid-call | 1 | `cancelled` | no |
| GET, endpoint never answers (10 s call) | 2 | `timed_out` in ~10 s | yes (cut short) |
| any, input rejected by the schema | 1 | `failed` (`validation`) | no |