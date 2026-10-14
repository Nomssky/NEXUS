# Execution Trace & Operational Observability Contracts (v7)

Status: Active
Baseline: Agent Runtime v6 — Approval Lifecycle (`54120ca`)
Scope: bounded execution-trace capture and read-only retrieval for agent
executions, built entirely on the existing event substrate, engine stores,
identity/scope model and governance semantics.

This contract defines what an execution trace is, which component is
authoritative for each fact, what the trace may contain, how it is queried,
and — with equal weight — what it cannot and does not claim. It does not
weaken or redefine any existing contract; where an existing contract already
governs a fact (admission, approval, reliability, memory, visibility,
durability), that contract remains authoritative and this one only states how
the fact becomes observable.

## 1. Relationship to existing contracts

The trace is a bounded, redacted, process-local *view* over facts that other
components already record and publish. The authoritative contracts remain:

- `AGENT_INTELLIGENCE_CONTRACTS.md` — objective loop, states, plan/step
  semantics, model/runtime boundary (`agent.thinking` is telemetry only).
- `RUNTIME_EXECUTION_CONTRACTS.md` / `AGENT_EXECUTION_CONTRACTS.md` — chain
  and executor lifecycle.
- `AGENT_GOVERNANCE_CONTROL_CONTRACTS.md` — admission decisions and their
  vocabulary (ALLOW / DENY / REQUIRE_APPROVAL / ESCALATE).
- `APPROVAL_LIFECYCLE_CONTRACTS.md` (v6) — action-bound approvals, cancel
  wins, single-use consumption, resume continuation. v7 changes none of it;
  the trace only observes what v6 already publishes.
- `CAPABILITY_TOOL_CONTRACTS.md` — mediated invocation, what an invocation
  event proves.
- `OPERATIONAL_RELIABILITY_CONTRACTS.md` — attempts, retries, not-sent /
  unknown / timeout outcome vocabulary, reconciliation seams.
- `AGENT_DELEGATION_CONTRACTS.md` / `AGENT_COORDINATION_CONTRACTS.md` —
  parent/child and group/member identity.
- `AGENT_MEMORY_CONTEXT_CONTRACTS.md` §16 — memory event metadata.
- `SCHEMA_EVENTS_TRIGGERS.md` — event envelope and domains.
- `SCHEMA_IDENTITIES_ORG §9` — durable organization audit records, which the
  trace does not replace (§12).
- `CORE_INTERFACE_CONTRACTS.md` — response/error envelopes, G-010 size
  bounds, 404/403/409 semantics.
- G4 (process-local runtime state): executions, approvals, resume snapshots —
  and now traces — are process-local. See §12.

## 2. Trace identity

- A **trace** is the ordered set of runtime events that carry one execution's
  runtime-established **correlation identity** (`event.Event.CorrelationID`).
  Trace identity is therefore not a new identifier: it IS the correlation
  identity the runtime already stamps on every execution-scoped event.
- On the agent surfaces (POST `/api/v1/intelligence/execute` and POST
  `/api/v1/executions`), the submit path sets `Request.ID ==
  Context.CorrelationID == execution_id`; the accepted response returns both
  as the same value. For these surfaces, **trace id == execution id ==
  correlation id**. The trace read API (§10) is exposed on the intelligence
  surface, where this identity alignment is guaranteed by the submit path.
- Correlation identities are runtime-established. No model-supplied or
  caller-supplied value can choose or forge a trace identity: the gateway
  derives it, the engine stamps it, children inherit it.
- Events without a correlation identity (registration, organization
  lifecycle, platform-internal facts) are never part of any trace.

## 3. Capture: publish-time tap on the single in-process bus

- Production creates exactly one bus (`event.NewMemBus`, engine wiring). Every
  execution-relevant component — chain, executor, agentexec, capability
  platform, agentintel, memory BusSink, the governance controller's publisher
  seam — publishes to that bus.
- Trace capture installs an optional **synchronous tap** at bus construction
  (`event.NewMemBusWithTap`). The tap runs on the publisher's goroutine,
  inside `Publish`, after idempotency dedup and before enqueue. It is O(1),
  allocation-bounded, takes only the trace registry lock, and MUST NOT block,
  publish, subscribe, or call back into the bus.
- The tap introduces **no new goroutine, no asynchronous buffering, no
  second bus, no queue of its own**. Shutdown, cancellation and backpressure
  questions do not arise: capture is complete when `Publish` returns.
- Events removed by the bus's idempotency dedup never reach the tap, so a
  deduplicated event is never presented as an additional observation.
- Capture redacts (§9) *before* any data enters a trace buffer. Buffers never
  hold unredacted payloads, so no later view or serialization path can leak
  what redaction removed.

## 4. Event identity, ordering, correlation

- **Event identity** is preserved verbatim: each entry carries the source
  event's `ID`, `Type`, `Source` and `Timestamp`.
- **No source sequence number exists.** The event envelope
  (`SCHEMA_EVENTS_TRIGGERS` §2.2 `sequence_number`) is not implemented in the
  runtime; this contract does not invent one. Each entry instead carries a
  **capture sequence** (`seq`): the per-execution position at which the event
  was appended by the tap.
- **Ordering guarantee**: entries are totally ordered by capture sequence,
  and capture order equals **publish (enqueue) order at the single bus
  funnel**. For a sequentially executing agent this equals source order.
  Concurrent producers (delegation children, coordination members) interleave
  exactly as they genuinely raced at the funnel; the trace applies **no
  causal reordering** and makes no causal claim about concurrent entries.
- **Correlation identifiers** in entry data (task ids, proposal ids, approval
  ids, call ids, group ids, child task ids) originate from runtime state only
  (§2).
- **Duplicates**: the same logical fact observed twice (two real attempts, a
  repeated decision) appears as two distinct events with distinct event ids
  and is never collapsed into one — and conversely one event is never
  presented as multiple effects. Consumers count physical attempts via
  `tool.attempt.*` events and `attempt`/`max_attempts` fields, not via
  narrative inference (§7).
- **Missing evidence stays missing**: fields dropped by redaction are listed
  by name per entry (`redacted_keys`); entries evicted at capacity are
  counted (`capture.dropped`); unparseable source payloads keep the entry
  envelope with `data_unparseable: true` and empty data. Nothing is silently
  filled, guessed or renumbered.

## 5. Authoritative source per event category

| Trace category | Authoritative publisher (event types) |
|---|---|
| Admission & chain gate | core chain events (`chain.*`), controller `governance.decided` |
| Agent lifecycle | agentintel state events (`objective.*`, `plan.*`, `step.*`, `agent.*`) |
| Model/runtime boundary | agentintel `agent.thinking`, `action.proposed`, `action.rejected`; agentexec `model.started`/`model.completed` |
| Governance & approval | controller `governance.decided`; engine `approval.requested/approved/denied/expired`; `governance.escalated` |
| Tool dispatch & reliability | capability platform `tool.invocation.*`, `tool.attempt.*`, `tool.retry.scheduled`, `tool.permission.denied`, `tool.credential.denied`, `tool.capability.*`, `tool.reconciliation.*`; agentintel `tool.requested`, `tool.completed` |
| Delegation & coordination | agentintel `agent.delegated`, `coordination.started/completed`; agentexec child events (`agent.*`, tool events under the child task id) |
| Memory effects | agentintel `memory.read`/`memory.written`; memory platform events via the shared-bus BusSink (`memory.*` metadata) |
| Budget & terminal | agentintel `budget.exhausted`, `objective.completed` (state-bearing); core `chain.completed/cancelled/failed` |

If a fact has no authoritative publisher today, the trace does not contain
it, and this contract says so rather than approximating (see §14).

## 6. Evidence classes (what each entry proves)

The trace keeps these five classes strictly distinct; it never narrates one
into another:

1. **Model proposal** — `agent.thinking` (`reason_category: model_proposal`)
   and `action.proposed` record what the model suggested. A proposal proves
   nothing was dispatched.
2. **Runtime decision** — `action.rejected` (runtime validation refused the
   proposal), `governance.decided`, `approval.*`, state transitions.
   A rejection or denial proves the runtime did *not* proceed.
3. **Logical tool call** — `tool.requested`/`tool.completed` from the loop
   and the invocation identity (`call_id`) bind attempts to one logical call.
4. **Physical attempt** — `tool.invocation.started` proves dispatch into the
   mediated platform began; `tool.attempt.started/completed` with `attempt`
   numbers prove physical attempts; `tool.retry.scheduled` proves a retry
   decision. Absence of `tool.invocation.started` for a refused call is the
   positive evidence that dispatch did not occur.
5. **Confirmed outcome** — `tool.invocation.completed` proves the *adapter*
   completed within this runtime, classified by `side_effect_class`; it is
   **not** a claim about the external system beyond what
   `CAPABILITY_TOOL_CONTRACTS` and `OPERATIONAL_RELIABILITY_CONTRACTS` already
   guarantee. `tool.invocation.unknown` keeps an outcome unknown;
   `tool.invocation.failed` with not-sent semantics (no attempt events) is
   distinguishable from unknown; `tool.invocation.cancelled`/`timed_out` are
   their own outcomes. `tool.reconciliation.*` appears only when the existing
   adapter seam actually produced it — the trace never implies
   reconciliation exists.

A trace entry never upgrades an outcome: HTTP success is not an effect, a
status label is not an effect, an admission is not an execution, and the
absence of a success event is not evidence of "no effect".

## 7. Delegation, coordination, approval pause/resume

- Children (delegation and coordination members) **inherit the parent's
  correlation identity**, so their events join the parent's trace. A child's
  events carry runtime-stamped linkage fields (`parent_task_id`,
  `parent_agent_id`, `delegation_depth`, `delegation_action_id` on every
  agentexec child event; `agent_id`/`depth`/`parent_agent_id` on the loop's
  `agent.delegated`), and coordination members are bound to their group by
  `group_id` plus declaration counts on `coordination.started/completed`.
  The event envelope's optional `task_id` is carried verbatim when a
  publisher sets it; today most publishers do not, and the trace does not
  invent it. Declaration identity (member index, child agent id) is carried
  in the events themselves; the trace preserves it verbatim and never
  re-associates a result by completion order.
- A trace query for a parent grants no new access to anything (§10): it
  returns only events the querying identity was already entitled to observe
  under its business/division scope. There is no independent child-trace
  query surface.
- Approval pause: the waiting episode ends with `objective.completed`
  (`state: pending_approval`) plus `approval.requested`; the approval
  decision produces `approval.approved`/`denied` (and `expired`), all under
  the same correlation identity, so pause and resume correlate to the same
  authorized action by construction.
- Resume re-submits the same execution id (v6), so it continues **the same
  trace** — capture is keyed by correlation identity, not by run episode.
  Completed steps and effects are never replayed into the trace by resume;
  what the loop re-proposes after a governance refusal appears as a new
  proposal + decision pair, honestly reflecting v6 semantics.
- **Approval consumption has no dedicated event.** Where the trace shows
  `approval.approved` followed by an `ALLOW` `governance.decided` for the
  same proposal identity, consumption is *inferred* from that sequence; the
  consumed ledger itself is not evented. The contract states this as
  inference, not evidence.
- Cancellation: `objective.completed` (`state: cancelled`),
  `chain.cancelled`, and `tool.invocation.cancelled` (when a dispatch was
  interrupted) are the authoritative facts. Approval invalidation by
  cancellation is observable as the absence of any later approving decision
  plus the v6 stored terminal state; a cancelled execution cannot be revived,
  and no trace entry can suggest otherwise (§11).

## 8. Terminal semantics

- The **authoritative execution status** (`completed`, `failed`,
  `cancelled`; `pending` while in flight) always comes from the engine's
  stored execution record — the same record GET the execution detail returns.
  The trace never re-derives status by counting entries and never overrides
  it.
- A **terminal marker** is the first captured entry that ends an execution
  episode: `objective.completed` whose state is a terminal state (completed,
  failed, cancelled, budget_exhausted, deadline_exceeded, denied, escalated —
  **not** `pending_approval`, which is a wait state), or
  `chain.completed`/`chain.cancelled`/`chain.failed`. The trace records
  `capture.terminal_seq` (the first marker) and `capture.post_terminal`
  (entries captured after it).
- Legitimate later entries exist — a wait episode followed by an approved
  resume produces a second `objective.completed` with a terminal state, and
  late governance facts may follow. They appear in order with their own
  sequences; they are visible, never silently folded, and they **cannot
  contradict** the terminal status: the stored record is immutable once
  terminal, entries are append-only, and nothing rewrites history.

## 9. Redaction and data minimization

Redaction is applied by the tap **at capture time**, before any buffer
holds the data (§3):

- **Allowlist**: an entry's payload keeps only keys from an explicit,
  documented allowlist (identifiers, runtime enums, counts, durations,
  error classes, bounded reasons). Every other key is dropped and its name
  listed in `redacted_keys` (names only — never values).
- **Denylist** (free-text, dropped even if otherwise allowed):
  `decision_rationale` (approver-supplied text), `objective` (request
  objective text), `intent` (step/delegate intent text), and `outcome` on
  `chain.started` (it carries the request intent). Model-authored step
  intents, approver prose and caller prose never enter a trace.
- **Never captured** anywhere: raw chain-of-thought, prompts, model raw
  outputs, tool arguments, tool results, credentials, API keys,
  authorization headers, provider secrets. The runtime's existing event
  payloads do not carry them (AGENT_INTELLIGENCE §13, CAPABILITY §15,
  MEMORY §16); the allowlist guarantees a future payload cannot slip them
  in through a new key name.
- **Value bounds**: string values are truncated to 200 characters; non-string
  JSON values are rendered compactly and truncated to the same bound; an
  entry holds at most 32 keys (excess keys are dropped and listed).
- **Unparseable payloads** (e.g. legacy Sprintf-built chain JSON broken by
  caller text) keep the envelope with `data_unparseable: true` and empty
  data — the trace reports the gap instead of guessing.
- Error strings that do pass the allowlist (bounded `reason`, `error_class`)
  originate from runtime-generated taxonomy code (capability error classes,
  validation messages), which the existing reliability contracts already
  require to be credential-free.

## 10. Read API and authorization

Route (additive; the existing detail endpoint is unchanged):

    GET /api/v1/intelligence/{id}/trace?business_id=...&limit=...&after_seq=...

- **Why a sub-resource route**: the existing detail endpoint returns the
  stored response (G-010 size-bounded, consumed as-is). Embedding a second
  evidence source there would inflate every existing consumer and conflate
  two records with different lifetimes. One new read-only route reusing the
  identical authorization sequence is the smallest compatible change.
- **Authorization** is exactly the execution-detail sequence: identity
  middleware (scoped-path matching covers `/api/v1/intelligence/`), actor
  membership for `business_id` (401/403 as elsewhere), result/pending
  resolution (404 unknown), business match (404), division read check (404).
  400 for missing/invalid parameters. No roles, owners, superusers, or
  cross-business access are introduced; a division-scoped member sees only
  its own division's traces, exactly as it sees division execution details.
- **Read-only**: retrieval performs no mutation of execution, approval,
  governance, memory or tool state; it only reads the stored record for
  authorization/status and the trace buffers for entries.
- **Result size and pagination**: `limit` (default and maximum
  `core.MaxTraceEntries` = 256, minimum 1) and `after_seq` (default 0,
  minimum 0) select a stable, capture-ordered window; continuation is
  `after_seq = last returned seq`. `capture.has_more` reports whether more
  entries exist beyond the window. Filters are exact identifiers only; there
  is no search, no wildcard, no indexing service.
- **Unavailable / incomplete data is represented, not hidden**: a known
  execution whose trace was evicted at capacity (§11) or never captured
  returns 200 with `capture.available: false`; a pending execution returns
  200 with `status: "pending"` and the partial entries captured so far;
  `capture.dropped`/`truncated`/`redacted_keys`/`data_unparseable` expose
  every loss inside a returned window.
- Responses carry no credentials; approver identity appears only as
  `approver_id` on approval events — the same identifier the existing
  approval events and records already expose to the same business audience.
- The trace endpoint is under `/api/v1/intelligence/`, which the existing
  API-surface inventory and error-envelope contracts cover; no new auth
  mode, header or key semantics are introduced.

Example response shape:

```json
{
  "execution_id": "api-1-int",
  "correlation_id": "api-1-int",
  "business_id": "biz-1",
  "division_id": "div-1",
  "status": "completed",
  "durability": "process_local",
  "capture": {
    "available": true, "entries": 12, "dropped": 0, "limit": 256,
    "truncated": false, "first_seq": 1, "last_seq": 12,
    "terminal_seq": 12, "post_terminal": 0
  },
  "entries": [
    {
      "seq": 1, "event_id": "api-1-int-chain.started", "type": "chain.started",
      "source": "chain", "timestamp": "2026-10-14T00:00:00Z",
      "data": {"step": "chain.started", "actor": "core"},
      "redacted_keys": ["outcome"]
    }
  ]
}
```

## 11. Limits and capacity behavior

All bounds are explicit and deterministic at capacity:

- **Per execution**: `core.MaxTraceEntries` = 256 entries. At capacity the
  **oldest** entry is dropped (drop-oldest FIFO), `capture.dropped` is
  incremented, `capture.truncated` becomes true, and sequences of remaining
  entries keep their original values (a gap in `seq` is the visible signal).
- **Global**: at most `core.MaxTracedExecutions` = 512 traced executions,
  evicted oldest-first; an evicted execution's trace is gone from memory and
  reads report `capture.available: false`. Eviction of a trace never touches
  the execution record, approvals or memory.
- No unbounded in-memory collection exists on the trace path; buffers are
  grown to their caps and no further.

## 12. Durability boundary (G4)

- Traces are **process-local**, exactly like execution results, approvals and
  resume snapshots under G4. A trace is lost on process restart and may be
  lost earlier through bounded eviction (§11). `durability: "process_local"`
  is returned in every trace response so API consumers cannot mistake the
  view for a durable journal.
- Trace loss changes nothing about executions, approvals or governance: the
  trace is a view, never a source of authority.
- The trace is **not** a durable audit record. Durable organization audit
  facts remain whatever `SCHEMA_IDENTITIES_ORG §9` already provides elsewhere;
  the stored execution record (also process-local) remains authoritative for
  outcome. No cross-restart trace continuity is claimed, offered or implied;
  after restart, executions have no trace until new events are captured.
- v6 crash-safety posture is unchanged: no durable resume, no durable
  approval store, no exactly-once claims.

## 13. Query-time behavior

- Retrieval is safe under concurrency: reads take the registry lock, copy
  the bounded window, and release; concurrent capture never corrupts a view
  (race-detector verified). Concurrent reads observe consistent snapshots of
  the buffer at some instant; they do not observe a torn or partially
  written entry.
- A read can observe a trace mid-execution (`status: pending`); entries
  captured after the read are simply not in that response. Re-reading
  continues from `after_seq` without duplication or gaps caused by the read
  itself.

## 14. Explicitly unsupported guarantees

The trace does **not** provide, and no consumer may infer:

- exactly-once capture, exactly-once effects, or any replay guarantee;
- durability, crash safety, or continuity across process restart;
- a global or cross-process total order, or a source-level sequence number;
- causal ordering among concurrent producers — only interleaving as raced;
- completeness under capacity pressure — only explicit drop counters;
- confirmation of external effects beyond the adapter-level evidence the
  reliability contracts already define; reconciliation is never implied;
- approval consumption as a directly observed fact (it is inferred, §7);
- access to any child, sibling, parent, business or division the querying
  identity is not already authorized for (§10);
- chain-of-thought, prompts, model outputs, tool arguments/results, or any
  credential material (§9).

Deferred work (not gaps in the delivered scope): a source-level sequence
number in the event envelope (requires the SCHEMA_EVENTS_TRIGGERS
`sequence_number` field to be implemented end-to-end); durable trace storage
(separately justified milestone, incompatible with G4 as-is); trace read
surfaces for the plain requests/executions chains; per-field provenance
linking every entry to the code site that produced it.

## 15. Non-goals

This milestone adds: one bus tap, one bounded in-process registry, one
read-only route, one contract. It adds **no** second event bus, audit
engine, execution store, governance engine, policy evaluator, scheduler,
background worker, production goroutine, event type, database, index or
search service, and it changes **no** admission, approval, resume,
cancellation, deadline, budget, memory, delegation or coordination
semantics.
