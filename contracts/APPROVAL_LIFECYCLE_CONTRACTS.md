# NEXUS — Approval Lifecycle Contracts (v6)

**Layer:** Governance / approval lifecycle and cross-execution resume
**Status:** MAKER — guidance for all current implementations
**Depends on:**

- `AGENT_GOVERNANCE_CONTROL_CONTRACTS.md` (§5 approval binding, §4 constraints)
- `AGENT_DELEGATION_CONTRACTS.md` (v4 parent/child approval independence)
- `AGENT_COORDINATION_CONTRACTS.md` (v5 settlement and regroup semantics)
- `AGENT_EXECUTION_DEPTH_CONTRACTS.md` (v3 budgets, ledger, evidence)
- `OPERATIONAL_RELIABILITY_CONTRACTS.md` (unknown outcomes, no redispatch)
- `SCHEMA_WORK` (§6 approval record and states)

---

## 1. Purpose

This contract makes approval a complete lifecycle — request, wait, decide,
resume — without turning it into blanket authorization and without inventing
new engines, ledgers, schedulers, or storage:

```text
Execution → proposal → admission → REQUIRE_APPROVAL → waiting →
decision → resume → revalidate → re-admit → consume once → effect
```

Four things that must never be confused:

```text
approval decision   (a human says yes/no to a bound action)
governance re-admission (the engine evaluates the CURRENT proposal again)
execution resumption (a waiting run continues with its authoritative state)
effect completion   (the mediated path ran and reported honestly)
```

## 2. Action binding (immutable target)

Every approval is bound to the exact consequential action via the canonical
proposal fingerprint: correlation, execution, objective, actor, agent,
business, division, action, resource, resource type, tool, operation, and the
effective constraints. The fingerprint is computed from runtime-established
values — never from model text, and model payloads cannot carry approval IDs,
fingerprints, approver names, or approval states (the strict protocol rejects
unknown fields).

Changing any consequential field (target, operation, scope, constraints)
changes the fingerprint, so the old approval cannot authorize the new
proposal: re-admission sees no applicable approval and opens a fresh
lifecycle. Stale approvals are rejected by mismatch, never by trust.

## 3. Authorized approvers (no new model)

Approver authorization reuses identity and governance authority exactly as
before: the policy's approver list, self-approval prohibition, and
business/division visibility. No roles, owners, or superusers are added.
Error semantics are preserved: 404 for unknown/invisible approvals, 403 for
unauthorized deciders, 409 for invalid lifecycle transitions — and an
approval outside the caller's visibility never leaks existence.

## 4. Lifecycle and single use

```text
pending → approved → consumed
pending → denied (terminal, no resume)
pending → expired (terminal; silence is denial where configured)
```

A decision is immutable once accepted. An approval authorizes at most one
admission: the controller's consumed ledger (`executionID|fingerprint`,
mutex-guarded, bounded FIFO) and the engine's action-approval index enforce
single use, so concurrent resumes cannot dispatch duplicate effects — the
first admission consumes, the rest re-evaluate without an applicable
approval. Approval records are cleaned up once their resume run stores a
terminal result; denials are dropped at decision time.

## 5. Persistence and crash consistency (process-local, honestly)

Approval records, consumption ledgers, and resume snapshots are
PROCESS-LOCAL by explicit architectural decision (G4 unchanged): nothing
here survives restart, and no durable execution recovery is claimed. This is
coherent rather than partial — a restart loses approvals and waiting-state
snapshots together, so no half-state can ever authorize an effect:

- approval creation/decision/consumption are mutex-guarded in-memory
  transitions; concurrent decisions are serialized (one wins, the rest get
  not-pending);
- a crash loses everything; after restart every approval ID is unknown
  (404) and every execution ID is gone (G4);
- exactly-once external effects are never promised: consumption and effect
  dispatch are separate steps, and an `unknown` outcome stays unknown with
  no redispatch.

Durable approval would need multi-record transactions the storage contracts
do not offer; per §19/§37 that milestone is explicitly not built here.

## 6. Resume continues, never replays

Approving re-submits the SAME request (same correlation/execution id). The
resumed run restores the waiting run's authoritative snapshot — plan and
version (re-validated), step ledger, observations, budget counters, completed
effect fingerprints, and the ORIGINAL caller deadline — instead of starting
blank. Consequences:

- completed actions are never re-executed (fingerprints survive);
- budgets never reset (counters continue; a resume that would exceed them
  fails honestly);
- the deadline never extends (the original deadline governs; an expired one
  yields `deadline_exceeded` with no effects);
- the next decision is a fresh model proposal against full context; only the
  fingerprint-bound action can consume the approval — anything else needs
  fresh admission.

Snapshot restore is verified (execution id plus objective, business, actor,
division must match) and consumed on load; mismatches start fresh; stale
entries are bounded and evicted. If no snapshot exists (restart, non-agent
request), resume falls back to the historical full re-run.

## 7. Re-admission stays authoritative

After approval, the engine may still deny, escalate, require a FRESH
approval, or allow with constraints — policy, scope, lifecycle, or target
may have changed. Constraints enforce against the actual runtime target;
unenforceable constraints fail closed. Approval never substitutes for
current governance and never widens authority.

A resume is admitted atomically with respect to a run that is still finishing
its teardown. Publishing a run's terminal result and releasing the state that
run was admitted with (its in-flight registration, its request reference, and
the approvals it consumed) are not one step, so an approval can be granted in
between. Therefore:

- a finishing run releases ONLY the approvals it was authorized by when it
  began (captured before it ran) — an approval granted while it finishes
  belongs to the resume that approval triggers and is never consumed by the
  earlier run;
- a finishing run deregisters/untracks the request only while it still owns
  the registration; once a resume re-admits the request the earlier run leaves
  that state alone, so a reader never sees the request disappear from both the
  in-flight registry and the results map (which would answer 404 mid-resume);
- re-admission is serialized with teardown, so neither can clobber the other.

A resume is refused (not silently retried, not fabricated) when it cannot be
admitted, and an unresumable approval never leaves a decided record without a
run.

## 8. Cancellation and deadlines win

- Cancelling a waiting (`pending_approval`) execution is allowed: it stores
  a cancelled result and invalidates every approval bound to that request,
  so a later approve finds nothing (404) and nothing revives.
- A cancelled request ID is recorded in a bounded set; any later approve or
  resume for it is refused — an approval arriving after cancellation never
  revives the execution.
- A resume admitted **concurrently** with a cancellation of the same waiting
  execution linearizes to `cancelled`, never to a failure: the chain consults
  the recorded cancellation set (not only the in-flight flag) at dequeue, at
  executor submit, and in the executor-denial arbitration, so the canceled
  terminal is never overwritten by a `failed`/`POLICY_DENIED` outcome that the
  cancelled re-admission produced. A cancellation that landed before the
  effect is dispatched prevents the effect.
- The waiting period never extends the caller deadline (§6).
- In-flight effect semantics are unchanged: an already-dispatched effect
  reports honestly; cancellation only prevents NEW effects.

## 9. Parent, child, and group approval independence

- A parent delegation approval never approves child effects; a child action
  approval never approves parent effects. Every approval binds (action,
  fingerprint, execution).
- A child waiting on approval surfaces its authoritative status; the parent
  receives it as evidence (pending_approval with its reference), never as
  success, never auto-resumed.
- A coordination group with a pending member settles honestly around it;
  after the member is approved, the parent re-proposes the unfinished
  remainder (completed siblings prune via fingerprints — never re-run);
  multiple pending members stay independently bound and consumed.
- Rejection, expiry, and invalidation propagate as terminal honesty: the
  action does not execute, no silent retry, no replacement approval without
  a new admission.

## 10. Budgets, idempotency, memory, capabilities

- Waiting and resumption consume no budget; resumed counters continue from
  the snapshot; repeated resumes cannot multiply decisions, attempts,
  fan-out, or depth.
- Approval consumption is not effect proof: an admitted effect may still
  fail or return `unknown` — outcomes stay separate, no blind redispatch,
  existing idempotency keys still reconcile single calls.
- Resumed effects use the same canonical paths (`Platform.Invoke`, bound
  memory write/delete, admission per effect) with the same scope,
  credentials-at-execution-time, and redaction. Resume expands nothing:
  business, division, capabilities, memory, credentials, child authority,
  depth, budget, deadline.

## 11. Observability

The existing vocabulary suffices (`approval.requested/approved/denied/
expired`, objective/plan/step/coordination events, governance decisions):
no new event types are introduced. Payloads stay metadata-only. Correctness
never depends on event order.
