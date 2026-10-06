# Operational Reliability & Tool Semantics — Contract (v1)

**Status:** v1, additive. It hardens the existing Capability & Tool Platform
(`CAPABILITY_TOOL_CONTRACTS`) and the agent intelligence control loop
(`AGENT_INTELLIGENCE_CONTRACTS`). **No locked contract section is modified and
the following remain authoritative unchanged:**

* G1–G5 (`docs/ARCHITECTURE_DECISIONS.md`, §12, §11)
* Agent Execution v1: selection, tool allowlists, delegation, workflows, cancellation
* Agent Intelligence v1: closed action vocabulary, budgets, observation boundary
* Capability & Tool Platform v1: one registry, one invocation path,
  bounded adapters, redaction, G5 response shape

The milestone invariant (one sentence):

> **Autonomous capability execution must derive an explicit, bounded,
> deterministic outcome — never a guessed one.**

---

## 1. Logical invocation ≠ physical attempt

A **logical tool invocation** has one identity (`call_id`): it starts when
`Platform.Invoke` is called and ends when it produces a terminal outcome.

A **physical attempt** is one dispatch against the adapter (`attempt_id`):
attempt count is bounded and is never reset on retry or replan.

```text
call_id = one agent decision to use a tool
attempt_id = one network/runtime call issued for that call
```

Platform invariants per call:

- `attempt_count` starts at 1 and never exceeds `MaxAttempts(manifest, class)`
- each attempt carries its own `time.Duration` budget
- a failed attempt that is retryable emits `tool.retry.scheduled`
- a failed attempt that is terminal emits `tool.invocation.failed` or the
  per-outcome terminal event (see §10)

## 2. Retryable classes

Retryability is a side-effect-class decision, never a name guess:

| side-effect class | May retry automatically |
|---|---|
| `read` | when the error class is in the retryable set |
| `write` | only when the error class says the *platform knows* it did not commit |
| `external_mutation` | only when the error is a confirmed-not-sent failure |
| `credentialed_external_mutation` | same as `external_mutation`; no extra retries |

The class that governs one invocation is its **effective class**: the manifest's
tool-level class, unless the adapter itself declares a narrower class for that
one operation. Narrowing is allowed only to `read` (e.g. `http.request`: GET and
HEAD read, POST/PUT/PATCH/DELETE mutate); a capability may never widen the
manifest's class, and the model may never influence it.

`MaxAttempts(call)` is 2 for every class — exactly one retry. Whether that retry
is *admitted* is the matrix above; a mutation additionally needs the capability
to support idempotency keys, so a replay is de-duplicated rather than repeated.

Persistent non-retryable classes:

| error class | retryable |
|---|---|
| `validation`, `permission denied`, `scope denied`, `credential unavailable` | never |
| `resource limit`, `internal adapter failure` | never |
| `cancelled` | never (an explicit cancel is not a retry condition) |
| `timeout` | read: yes; mutating external: only if classified as not-sent |
| `external error` / network-denied | read: yes; mutations: only if definitively not sent |
| `not sent` (DNS or connect failure: provably nothing left the process) | read: yes; mutation: only with idempotency support |
| `unknown` (remote-side response unknown) | **never** |

Mutation retries are gated by idempotency:

* an HTTP mutation must send a stable `Idempotency-Key` header derived from
  the call id when re-issuing a single request;
* a GitHub mutation must send the idempotency key in the payload mapping;
* all other mutation retries are suppressed by design (`retry_recommended = false`).

## 3. Outcome model

Every terminal outcome is one of:

| outcome | means | retry |
|---|---|---|
| `completed` | the adapter finished and the platform knows it finished | n/a |
| `failed` | the platform knows the operation did not take effect | classified driver |
| `cancelled` | cancellation propagated and no effect is observable | never |
| `timed_out` | the tool invocation deadline expired; cancellation reached the adapter | per class |
| `unknown` | the outcome cannot be distinguished (e.g. a request reached the remote system but the response was lost) | **never** |

`unknown` is a **first-class safety signal**, not an error line:

```text
unknown ⇒ no automatic retry enqueue
unknown ⇒ observation must state retry_recommended=false
unknown ⇒ reconciliation requirement is surfaced explicitly
unknown ⇒ every downstream interpretation (audit, ops, docs) keeps the
          distinction instead of downgrading it to failed/cancelled.
```

## 4. Capability lifecycle

Manifest state is bounded and runtime-enforced. Allowed states:

```text
registered → enabled → disabled | deprecated
            deprecated   → disabled | enabled
            disabled     → enabled
```

Rules:

* adapters may only execute when their registered manifest is `enabled`;
* `deprecated` manifests may still execute while they are the registered tool,
  but they emit `tool.capability.deprecated` and surface in events/audit;
* `disabled` short-circuits invocation before any adapter runs
  (`400 VALIDATION`-style error class `capability_disabled`);
* the lifecycle transition checks are guarded by a single mutex so an
  operator-side `disable` never races a dispatch we already authorized.

## 5. Deadline hierarchy

One canonical clock:

```text
objective budget (MaxExecutionTime)
        ↓ timebox, enforced by the agentintel control loop
agent execution ctx
        ↓ same ctx, plus Platform.Caps.MaxDuration per logical call
call deadline = min(ctx deadline, now + Caps.MaxDuration (+ manifest))
        ↓ call carries a shared ChildInstanteTime; retries share the same child context
attempt N uses the remaining time, never a fresh deadline
```

A retry may only be scheduled if it fits inside the remaining call deadline. That
is structural, not advisory: every attempt context is a child of the call
context and its budget is a share of the call deadline, so a retry can never
outlive the call even when less than a full share remains. An attempt is never
started at all once the call deadline has passed.

## 6. Cancellation

Cancellation propagates to the adapter's context. A cancelled *physical attempt*
produces outcome `cancelled` (not `failed`), and no automatic retry is
scheduled. If a mutation's response was unreadable when cancellation hit, the
outcome degrades to `unknown` — never to `cancelled` blindly.

## 7. Budget accounting

Each logical call reports:

```text
attempt_count     // total attempts made, >= 1 per logical call
invocation_time_ms
result_bytes
attempts_budget_left  // derived from remaining deadline
```

Attempt counts from the logical call never reset across a retry of the same
call and never reset because the intelligence loop re-plans. A pure
"replan" re-proposal is a *new logical call* and gets a new `call_id`;
it cannot resurrect the old call's consumed budget.

## 8. Observation normalization

The intelligence layer wraps every capability result in exactly one structured,
bounded shape:

```text
observation {
  observation_id, source, action_type, status,
  outcome, attempt_count, retry_recommended, reconciliation_required,
  text, result, timestamp
}
```

`text` remains bounded and redacted; `result` carries the bounded adapter
payload verbatim (it is already normalized/redacted by the platform).
This shape never carries credentials, root paths, provider topology, or raw
error internals — only `outcome`, counts, and the redacted result surface.

## 9. Duplicate suppression

A mutation that enters `unknown` **must never** be silently retried. The
attempt-loop guard is: if the adapter's final error is classified
`unknown`, skip the retry path entirely and emit
`tool.invocation.unknown` (plus a follow-up `tool.reconciliation.available`
when the capability advertises one).

## 10. Event vocabulary additions

Beyond §15 of the Capability v1 contract, the bus gains:

```text
tool.attempt.started
tool.attempt.completed
tool.retry.scheduled
tool.invocation.cancelled
tool.invocation.timed_out
tool.invocation.unknown
tool.capability.disabled
tool.capability.deprecated
tool.reconciliation.available
tool.reconciliation.completed
```

All are metadata-only (tool id, operation, agent, business, division, outcome,
attempt id/index, error class): no secret material, no raw adapter payload,
no internal topology.

## 11. Non-goals / boundary

No distributed execution, no durable execution recovery (G4 Level 1), no
second execution framework, no model-driven retries, no automatic mutation
retry, no raw provider payload in observations, no model-controlled
credentials or permission widening.
