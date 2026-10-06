# Tool Reliability (v1)

`contracts/OPERATIONAL_RELIABILITY_CONTRACTS.md` is authoritative. This page is
the concrete semantics: what the runtime guarantees for every mediated
capability invocation, and what it deliberately does not do.

```text
one logical call (call_id)  →  bounded physical attempts (attempt 1..N)
                            →  exactly one terminal outcome
```

## One call, bounded attempts

A **logical call** is one `Platform.Invoke`: it starts at dispatch and ends at a
terminal outcome. A **physical attempt** is one `adapter.Invoke` inside it.

| field | meaning |
|---|---|
| `Invocation.CallID` | the logical call identity; identical on every attempt |
| `Invocation.AttemptIndex` | 0-based index of this physical attempt |
| `Invocation.IdempotencyKey` | stable per logical call; never a secret |
| `Result.Attempts` | attempts actually made, always ≥ 1 for a dispatched call |

The attempt bound is **2** for every side-effect class: exactly one retry. What
that retry is *allowed* to be depends on the class and the error
([tool-retries.md](tool-retries.md)).

## Terminal outcomes

`Result.Outcome` is always exactly one of:

| outcome | means | automatic retry |
|---|---|---|
| `completed` | the adapter finished and the platform knows it | n/a |
| `failed` | the platform knows the operation did not take effect | class-dependent |
| `cancelled` | cancellation reached the adapter, nothing observable happened | never |
| `timed_out` | the call deadline expired; cancellation reached the adapter | class-dependent |
| `unknown` | the outcome **cannot** be distinguished | **never** |

```json
{
  "tool_id": "http.request",
  "operation": "post",
  "status": "error",
  "outcome": "unknown",
  "attempts": 1,
  "capability_state": "enabled",
  "retry_recommended": false,
  "reconciliation_available": false,
  "error": "unknown outcome: mutation outcome unobserved on POST (Post \"https://api.example/v1/entries\": EOF)"
}
```

`unknown` is a safety signal, not an error line: it is never downgraded to
`failed`, never retried automatically, and it always sets
`retry_recommended=false`. The intelligence layer surfaces it as an observation
with `reconciliation_required=true`; nothing polls, re-sends or guesses.

## Capability lifecycle

```text
registered → enabled → disabled | deprecated
                  deprecated → disabled | enabled
                  disabled   → enabled
```

* a **disabled** capability is refused *before* the adapter runs
  (`capability_disabled`, `tool.invocation.rejected`, `tool.capability.disabled`);
* a **deprecated** capability still executes, and every invocation emits
  `tool.capability.deprecated`;
* an untouched capability is **enabled**;
* transitions outside the graph are refused (`409`), never silently applied.

Operator control surface (X-API-Key, `internal/gateway/capability.go`):

```bash
curl -XPOST "$BASE/api/v1/control/capabilities/http.request/state" \
  -H "X-API-Key: $KEY" -d '{"state":"disabled"}'
# {"tool_id":"http.request","capability_state":"disabled", ...}
```

A disable only removes invocations. It never widens scope, never unregisters a
capability, and never interrupts a dispatch that was already authorized — the
gate is read once, immediately before the attempt loop.

## Deadline hierarchy

```text
objective budget (MaxExecutionTime)     agentintel control loop
        ↓
call deadline = min(ctx deadline, now + caps/manifest MaxDuration)
        ↓  shared by every attempt of that call
attempt N gets a slice of the remaining call deadline (never a fresh one)
```

Because every attempt context is a child of the call context, a retry can never
outlive the call: with `http.request` (10 s) a call that hangs forever produces
`attempts=2`, `outcome=timed_out` in ~10 s — not 20 s.

## Cancellation

Cancellation propagates from the request context into the adapter's context.
A cancelled attempt reports `cancelled`, never `failed`, and is never retried. A
mutation whose response was unreadable when cancellation hit degrades to
`unknown`, because "we stopped waiting" says nothing about whether the remote
side committed.

## Budget accounting

`Result.Attempts` counts physical attempts of one logical call. Counts never
reset across a retry, and a replan never resurrects a consumed budget: a replan
proposal is a *new* logical call with a *new* `call_id`, while the objective
budget keeps counting (`replans=1` in the terminal summary).

## What this is not

No distributed execution, no durable execution recovery (G4 stays Level 1), no
second execution path, no background reconciliation workers, no model-driven
retries, no automatic mutation replay.