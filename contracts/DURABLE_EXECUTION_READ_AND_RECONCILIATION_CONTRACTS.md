# NEXUS — Durable Execution Read API & Recovery Reconciliation (v11)

Authoritative for the durable execution **read** surface and the reconciliation
decision. Cross-references (never redefines):
`DURABLE_EXECUTION_RECOVERY_CONTRACTS.md` (the journal + recovery),
`CORE_INTERFACE_CONTRACTS.md` (envelope/status), `SCHEMA_IDENTITIES_ORG.md`
(scope/visibility), `AGENT_GOVERNANCE_CONTROL_CONTRACTS.md`,
`APPROVAL_LIFECYCLE_CONTRACTS.md`, `EXECUTION_TRACE_CONTRACTS.md`.

## 0. Summary

v10 made execution lifecycle evidence durable. v11 adds a **read-only,
authorization-safe way to inspect that evidence after restart**, on a **new**
route so the frozen process-local execution-result semantics (including the
`404`-after-restart E2E contract) are untouched.

**Reconciliation is deliberately deferred** (§7). Read-only inspection is the
complete v11 deliverable.

## 1. Durable journal record vs in-memory execution result

- **In-memory execution result** (`GET /api/v1/requests|intelligence|executions/{id}`):
  the process-local result, including output/summary. **Lost on restart**, and
  `404` after restart (frozen). v11 does **not** change these routes.
- **Durable journal record** (`GET /api/v1/recovery/executions/{id}`): the
  minimum lifecycle evidence persisted by v10. It survives restart. It is
  **not** a result: it carries no output, model text, tool args/results, or
  proof that a side effect occurred. Responses are marked
  `"source": "durable_journal"` so a client never mistakes it for a live result.

## 2. Durable record schema and version compatibility

The read path decodes the v10 `ExecutionRecord` (schema **version 1**). A record
whose `version != 1`, whose `id` is empty, or whose `state` is outside the
closed vocabulary is **rejected** — the read fails closed and never reinterprets
an unknown version. No new fields are persisted in v11 (no second source of
truth, no duplicated state).

## 3. Exposed fields (allow-list) and internal fields

**Exposed** (only these):

| Field | Meaning |
|---|---|
| `execution_id` | execution id (= runtime correlation id) |
| `business_id` / `division_id` | scope (the caller is authorized for these) |
| `state` | durable lifecycle state (§4) |
| `error_class` | coarse code (e.g. `APPROVAL_REQUIRED`), never a raw message |
| `created_at` / `updated_at` | RFC3339 UTC timestamps |
| `recovery.recovered` | whether the recovery pass classified this record |
| `recovery.prior_state` | state before recovery (when recovered) |
| `recovery.recovered_at` | when recovery classified it |
| `approvals` | durable approval history for this execution (v34) |

**Internal / never exposed:** `actor_id` (identity reference),
`recovery.reason` (free text), the store key, and the raw durable bytes.
Invocations (`invocation/*`) are **not** exposed in v11 — dispatch evidence
stays internal; exposing it is deferred. Prompts, model output, tool
args/results, credentials and chain-of-thought are never persisted or exposed.

## 4. Lifecycle state mapping and `unknown`

`state` is one of `accepted`, `running`, `pending_approval`, `completed`,
`failed`, `cancelled`, `timed_out`, `unknown`.

- A **terminal** state (`completed`/`failed`/`cancelled`/`timed_out`) is final;
  recovery never rewrites it.
- **`unknown`** means the outcome could **not** be determined — the in-flight
  effect *may* have occurred. It is **never** a success and never a plain
  failure. It is set only by recovery (or, in future, by a validated
  reconciliation action, §7). A read must present `unknown` distinctly (it is
  not collapsed into `failed`).
- `error_class` distinguishes a known failure reason where available; an absent
  `error_class` is not a claim of success.

> **v27 (ratified) — reserved states, and how an approval pause is
> presented.** `running` and `pending_approval` are part of the durable
> vocabulary but are **reserved: no current code path writes them.** This was
> previously ambiguous — the retention contract §2 listed `pending_approval` as
> a nonterminal retention class while the recovery contract §2/§3 never
> produced it. Both stay reserved rather than introducing one.
>
> An execution awaiting approval is presented as `state = failed` with
> `error_class = APPROVAL_REQUIRED`. A reader must treat that pair as "waiting
> for approval", not as an ordinary failure — and must recognise that a later
> grant, if it happens in the same process, changes `state` to the real terminal
> outcome (recovery contract §2).
>
> No field is added, removed or renamed. The `state` and `error_class`
> projections are unchanged; only which values can occur is clarified.
>
> **Approval history (v34) IS now exposed, additively.** The journal record
> carries an `approvals` history (requested / granted / denied with the approval
> id and time) that survives the final outcome. It is projected onto this
> response as an ordered array and is **omitted entirely** when the execution
> never passed an approval gate, so every pre-existing response is unchanged.
>
> The history is a field **on the very `ExecutionRecord` this endpoint already
> gates**, so exposing it reuses the existing boundary unchanged — business
> membership, exact business match, and division scope — and cannot widen who
> may see an execution. There is no new endpoint and no new authority source.
>
> Each entry is only what was recorded: append order is preserved and nothing is
> synthesized. A superseded pause still appears as its recorded `requested`
> entry, and the execution's own `state` carries the outcome — so a historical
> decision is never presented as a currently effective one.
>
> **The decider is NOT exposed, and that is an open decision.** `decided_by` is
> omitted because this contract excludes identity references, and because the
> live approval surface (`GET /api/v1/approvals`) gates approver identity behind
> **business-wide** membership — a strictly higher bar than this
> division-scoped read. Projecting the decider here would expose decided-approval
> identities at a weaker scope than the surface that already exposes them.
> Whether to add it (and at what scope) requires an explicit decision.

## 5. Read API

### 5.1 Detail

```
GET /api/v1/recovery/executions/{id}?business_id=<b>
```

Authorization mirrors the execution-result read sequence:

1. `business_id` missing → **400** VALIDATION.
2. no/invalid credentials → **401**; not a member of `business_id` → **403**
   (`requireActorMembership`).
3. record absent, or `record.business_id != business_id`, or
   `!AllowsScope(actor, business, record.division_id)` → **404** (unknown,
   foreign and unreadable are indistinguishable, preserving G5).
4. otherwise **200** with the projection (§3) and `"source": "durable_journal"`.

Durable inspection disabled (no durable store / nil journal) → **503**
`RESOURCE_UNAVAILABLE`.

### 5.2 List

```
GET /api/v1/recovery/executions?business_id=<b>[&division_id=<d>][&limit=N][&offset=M]
```

- `business_id` required (**400** otherwise); membership required (**403**).
- If `division_id` is given, the caller must hold that scope
  (`authorizeMembershipScope`) → **403** otherwise; results are limited to that
  division.
- If `division_id` is omitted, each record is filtered by
  `AllowsScope(actor, business, record.division_id)`: a business-wide member
  sees all divisions; a division-scoped member sees only their division.
- `limit` ∈ [1, 200], default 50 (**400** outside range / non-integer);
  `offset` ≥ 0 (**400** otherwise).
- **Ordering:** newest first by `created_at`, tie-broken by `execution_id`.
- **Bounds:** the underlying scan is capped at `maxListScan = 5000` newest
  records for the business; the response is capped at `limit`. `has_more`
  reports whether more rows matched than were returned. There is no unbounded
  scan and no fragile in-memory-only index — the store's index is rebuilt from
  disk on open (durable).

### 5.3 Read-only

Inspection never mutates execution state, renews approvals, changes deadlines,
creates an execution, or dispatches a tool.

## 6. Authorization and visibility

- Authority is the existing `MembershipSet` + gateway identity middleware
  (`AllowsScope`, `HasAnyMembership`). **No RBAC, owner roles, superusers, or
  ad hoc checks are added.**
- Business-wide membership covers the business; a division membership covers
  only its own division; divisionless records require business-wide authority.
- Knowing an execution id is never sufficient: the record's business/division
  must be within the caller's scope, else **404**.
- Suspended/archived lifecycle admission is unchanged; the 404/403/409
  distinction is preserved (400 for bad input, 401 unauthenticated, 403 not a
  member / division not held, 404 unknown-or-unauthorized resource).

## 7. Reconciliation — deferred

Reconciliation (changing a durable `unknown` to a resolved classification) is
**not implemented in v11**. Reasons:

- There is **no reliable evidence source**: `unknown` is inherently
  indeterminate, and no tool or provider exposes a read-back/reconciliation
  contract NEXUS could trust. Inferring a result from a missing error, a
  timeout, or a restart is forbidden.
- There is **no authority model** for it that does not invent a role. Adding a
  "mark completed" action based on caller input would fabricate outcomes and
  violate the no-new-roles rule; the control-plane API key is an operator
  credential but still lacks a verifiable evidence source.

The read path therefore exposes `unknown` honestly and leaves reconciliation to
a later milestone that defines a real evidence source and a reviewed authority.
Negative space is explicit: **no mutation endpoint exists**, so no reconciliation
label can falsely claim external success.

## 8. Durable-write failure and restart behavior

- A failed durable write at accept time fails the submission (no 202 without a
  record). A failed terminal write leaves the record nonterminal → recovery
  classifies it `unknown` (never a fabricated completion).
- After restart: terminal records remain terminal and immutable; interrupted
  records are `unknown`; a subsequent restart preserves `unknown`
  (idempotent). Corrupt/unsupported records abort startup (v10) and are rejected
  on read (v11) — fail closed, never silently reinterpreted.

## 9. Explicit non-guarantees

- No exactly-once external effects.
- No distributed coordination, distributed locking, or multi-instance
  correctness.
- No automatic replay or resumption of `unknown`/interrupted executions.
- No approval/resume continuity across restart.
- Process-local traces and in-memory results remain non-durable and `404` after
  restart (unchanged).
- The durable record is **lifecycle evidence**, not a result, not an event
  trace, and not proof of an external side effect.

## 10. Compatibility

- The existing execution-result routes and their `404`-after-restart behavior
  are **unchanged** (frozen E2E `15-agent-execution` keeps passing).
- The new route is additive under `/api/v1/recovery/`. Existing clients are
  unaffected.
- The new contract does not contradict the v10 recovery contract; it reads the
  same records and adds no writes.

## 11. Optional research evidence (v52, extended by v54 and v60)

The detail read may carry an additive `research_evidence` object for executions
that captured a complete, validated `web.search` result. Its absence means no
valid capture was persisted; it never means "the search returned nothing".
An empty capture is present evidence with `count = 0`. The list read never
carries this object.

### 11.1 Versioned contract

- **v1 (legacy):** `{version, capability, operation, count, sources}`. Source
  rows have no ids, and there are no plan/step/operation fields. It is readable
  exactly as written; no identity, attribution, plan or timestamp is invented.
- **v2 (legacy provenance):** the same fields, each source with an
  execution-scoped `id` (`src-1`, `src-2`, ...), plus optional:
  - `plan: {plan_id, version}` — the active plan version at the terminal
    transition. `plan_id` is execution-scoped and must equal the durable
    execution id.
  - ordered `steps: [{step_id, status, dependencies?, observation_id?}]` in
    plan order. Status is the runtime ledger state, not a success claim.
  - `operations: [{operation_id, step_id, tool_id, operation, source_ids}]` —
    one record per mediated successful `web.search` invocation, written when
    the runtime knew the executing step and observation id. `source_ids` refer
    to source ids in the same object.
  v2 predates intent, objective reference, completion relationship and explicit
  provenance markers. Those fields are absent and are reported as unavailable;
  they are never inferred on read.
- **v3 (current):** the v2 shape plus:
  - `plan: {plan_id, version, objective_id?, completion_step_id?,
    provenance: "authoritative"}`. `objective_id` is the runtime-owned
    objective reference. `completion_step_id` names the step whose completion
    action settled a completed run.
  - `steps: [{step_id, intent, plan_version, status, dependencies?,
    observation_id?, provenance: "authoritative"}]`. `intent` is the
    runtime-owned step declaration; `plan_version` is the revision in which the
    step was declared.
  - `operations: [{..., provenance: "authoritative"}]`.
  - `attribution: "derived"` when at least one operation record exists, else
    `"unavailable"`.

Attribution is derived only from these runtime records. It is never inferred
from source order, titles, URLs, timestamps, or the order of the provider's
result rows. Sources without an operation remain explicitly unattributed.
Operations whose step is absent from the final plan are dropped; their sources
remain present but unattributed rather than being reassigned. Step attribution
proves which runtime step captured a source; it is not an entailment or truth
claim about that source.

### 11.2 Bounds and failure behavior

The snapshot is atomic with the terminal/supersede write. It is validated
wholesale on write and read: unknown versions, missing v2/v3 source ids, count
mismatch, missing/duplicate/cyclic step references, unknown operation
step/source ids, a source referenced by more than one operation, invalid status
values, missing or incorrect v3 authoritative markers, missing step intent or
plan version, a completion step absent from the snapshot, control characters,
and the documented bounds fail closed. A plan whose `plan_id` does not equal
the durable execution id is rejected at write and decode. Bounds are 50
sources, 64 plan steps, 32 dependencies per step, 64 operations, 50 source
references per operation, 2000-byte step intent, 128-byte
plan/step/objective/observation/operation ids, and 64-byte source ids.

A repeated delivery of the same mediated operation id does not duplicate source
or operation records. Legacy v1 evidence cannot carry v2 ids or provenance;
legacy v2 evidence cannot carry v3-only fields. Neither is upgraded on read.

### 11.3 What recovery may render

Recovery may render the historical durable plan identity, revision, objective
reference, completion relationship, step order, intent, dependencies and
lifecycle statuses, operation-to-step source associations, source records, the
durable validated synthesis and evaluation (v59/v60), and the execution
record's real lifecycle timestamps. The plan block is explicitly labeled as the
historical snapshot persisted at the terminal transition, not the current live
plan; a later replan does not rewrite it. Legacy v2 evidence is labeled as
lacking v3 intent/objective/completion/provenance, and legacy v1 as lacking
plan provenance entirely; missing fields are shown as unavailable. Recovery
may not fabricate step timestamps, source attribution, plan facts or a plan for
legacy evidence, and may not imply that an old plan equals the current plan.
