# Operational Observability Contracts (v15)

Status: **active**. Authoritative for what the NEXUS runtime's operational
metrics mean, where they come from, and what they must never be used to claim.

This document is *observability only*. It changes no execution, governance,
journal or retention semantics. Where this document and
`contracts/EXECUTION_JOURNAL_RETENTION_CONTRACTS.md` overlap, the retention
contract remains authoritative for retention semantics (eligibility, atomicity,
crash recovery); this document only describes how a retention *run* is reported.

Related authoritative documents (unchanged by v15):

- `contracts/DURABLE_EXECUTION_RECOVERY_CONTRACTS.md` — journal states, recovery
  classification, `unknown`.
- `contracts/EXECUTION_JOURNAL_RETENTION_CONTRACTS.md` — retention policy,
  `DeleteBatchHard`, crash matrix.
- `contracts/APPROVAL_LIFECYCLE_CONTRACTS.md` — approval pause/resume,
  cancellation.
- `contracts/OPERATIONAL_RELIABILITY_CONTRACTS.md` — retry classes, outcome
  vocabulary (`completed`, `failed`, `cancelled`, `timed_out`, `unknown`).

---

## 1. Scope and non-goals

v15 adds a bounded, process-local metrics layer and extends the offline
retention operator's accounting.

Explicitly **not** in scope, and **not** implemented:

- No metrics/alerting platform, dashboard, exporter or hosted monitoring.
- No Prometheus text exposition, no OpenTelemetry, no tracing backend. There is
  no new dependency; `go.mod` remains standard-library-only.
- No distributed or multi-instance metrics aggregation. Every metric is
  process-local. There is no cross-instance reconciliation and no node identity.
- No new durable telemetry store, no raw trace store, no chain-of-thought
  capture.
- No change to the existing execution-trace endpoint or its redaction rules.
- No online/live retention, no scheduled retention, no startup pruning, no
  disk-pressure cleanup trigger.

---

## 2. Metric taxonomy

Every series below is classified. The classification is part of the contract:
an operator may only draw the conclusions the class permits.

| Class | Meaning |
| --- | --- |
| **Authoritative (process-local)** | Counted exactly once at a defined lifecycle hook in this process. Authoritative for *this process, since this process started*. |
| **Durable evidence (startup snapshot)** | Copied from a durable journal recovery pass that ran at startup. Authoritative for *the records that existed at that moment*, not for the present. |
| **Estimate** | Bounded but not exact. None in this milestone. |
| **Unavailable** | Cannot be measured cheaply or reliably; deliberately absent. Listed in §8. |

### 2.1 Reset behavior (normative)

All series are **process-local** and reset to their initial value on every
process start:

- Counters start at `0`.
- Gauges start at `0`, except `journal.last_recovery_unix_seconds`, which starts
  **unset (`0`)** and means "no successful recovery has completed in this
  process".
- No series is durable. No series survives a restart. Nothing here may be
  presented as durable journal evidence.

Because counters reset, a counter that reads `0` after a restart means "no such
event in this process since it started" — **not** "no such event ever".

---

## 3. Series definitions

All series live in one `metrics.Metrics` owner (`internal/foundation/metrics`)
and are exposed as fixed JSON fields (no label strings are emitted; see §5).
A fresh owner starts at the values in §3.0.

### 3.0 Initial values

Every field is `0`, including `execution.in_flight` and
`journal.last_recovery_unix_seconds`.

### 3.1 Execution lifecycle (authoritative, process-local)

"Logical execution" throughout means **one admitted run of one execution
request** — one execution that reached a stored terminal result. Requests,
retries and tool attempts are counted separately (§3.3) and are never conflated
with it.

| Field | Type | Increment site | Meaning |
| --- | --- | --- | --- |
| `execution.accepted` | counter | `Engine.SubmitRequest`, after the queue send succeeds | Executions admitted by a *new* submission. An execution whose durable acceptance failed is **not** counted. |
| `execution.in_flight` | gauge | `+1` on an admitted submit, `+1` on an admitted resume, `-1` when a run stores its terminal result | Runs admitted and not yet terminal. An approval-paused execution is **not** in flight. |
| `execution.completed` | counter | terminal store, status `completed` | Runs that stored a `completed` result. |
| `execution.failed` | counter | terminal store, status `failed` | Runs that stored a `failed` result. |
| `execution.cancelled` | counter | terminal store of a `cancelled` result | Runs (or cancelled waits) that stored a `cancelled` result. |
| `execution.approval_pauses` | counter | terminal store of an approval wait — a `failed` result whose error code is `APPROVAL_REQUIRED` | Genuine approval waits (task- or action-level). |
| `execution.approval_resumes` | counter | `Engine.ApproveRequest`, after the queue send succeeds | Continuations admitted. A refused or racing resume is **not** counted. |
| `execution.cancellations_requested` | counter | `Engine.CancelRequest` entry | Calls to cancel, whatever the outcome. |
| `execution.cancellations_won` | counter | the point a cancellation actually takes effect: a waiting execution's cancelled result is stored, or an executing task's cancellation is confirmed | Cancellations that changed the outcome. |
| `execution.cancellations_refused` | counter | `CancelRequest` returns not-found, scope, division-scope or already-completed | Cancellations refused. |

**Invariant (normative).** A cancellation request that loses to an existing
terminal result is never counted as a successful cancellation: it increments
`cancellations_requested` and, when refused because the request was already
terminal or out of scope, `cancellations_refused` — never `cancellations_won`.

`execution.completed + execution.failed + execution.cancelled` equals the number
of terminal results stored by this process. It does **not** equal
`execution.accepted` in general: a run can end without a stored terminal result
(process shutdown), and a run can pause for approval (a terminal
`failed`/`APPROVAL_REQUIRED` result) and later be resumed, producing a second
terminal result for the same execution.

There are exactly two sites that commit a terminal result, and each is counted
once:

- the chain's terminal store in `processRequests` (completed, failed, cancelled),
  which also closes that run's in-flight interval;
- the waiting-cancellation commit (`storeCancelledResult` reaching its
  successful compare-and-set), which records `cancelled` and does **not** touch
  the in-flight gauge — no run is in flight while an execution waits for
  approval.

### 3.2 Terminal failure classification (authoritative, bounded)

Counted only for runs whose stored terminal status is `failed`, keyed by the
result's coarse error code (the same code the durable journal records).

| Field | Counted when the code is |
| --- | --- |
| `execution.terminal_approval_required` | `APPROVAL_REQUIRED` |
| `execution.terminal_policy_denied` | `POLICY_DENIED` |
| `execution.terminal_escalation_required` | `ESCALATION_REQUIRED` |
| `execution.terminal_deadline_exceeded` | `DEADLINE_EXCEEDED` |
| `execution.terminal_other_failure` | any other code |

The vocabulary is closed by construction: an unrecognized code lands in
`terminal_other_failure`, so a new code can never create a new series. These
fields are a coarse view of *governance and reliability decisions that ended a
run*; they are **not** a count of governance evaluations, and a governance
check that did not end a run is not represented (see §8).

### 3.3 Tool invocation and reliability (authoritative, process-local)

Counted at the capability platform, which is the single authoritative logical
invocation boundary.

| Field | Type | Meaning |
| --- | --- | --- |
| `tools.calls` | counter | **Logical** tool invocations: one dispatch of one logical call. |
| `tools.attempts` | counter | **Physical** attempts made across all logical calls. |
| `tools.retries` | counter | Attempts beyond the first, summed per logical call. |
| `tools.completed` | counter | Logical calls whose outcome was `completed`. |
| `tools.failed` | counter | Outcome `failed`. |
| `tools.unknown` | counter | Outcome `unknown` (uncertain — see §6). |
| `tools.timed_out` | counter | Outcome `timed_out`. |
| `tools.cancelled` | counter | Outcome `cancelled`. |

**Invariants (normative).**

1. A retry never increments `tools.calls`: one logical call with three physical
   attempts yields `calls += 1`, `attempts += 3`, `retries += 2`.
2. An outcome counter increments **once per logical call, after the dispatch
   completes** — never when a dispatch merely starts. A started dispatch that
   never produces an outcome increments neither.
3. `tools.unknown` is never added to `tools.completed`. An uncertain outcome is
   not a success.
4. Refusals decided **before** dispatch (unknown tool, allowlist, scope,
   permission, credential, disabled capability) are **not** logical calls and are
   not counted here. They surface through the audit/event stream and the
   existing trace endpoint, not through these counters.

### 3.4 Durable journal (authoritative, process-local)

| Field | Type | Meaning |
| --- | --- | --- |
| `journal.write_failures` | counter | Durable journal writes that returned an error. |

`write_failures` covers the runtime write path: execution acceptance, terminal
state, and the agent runtime's dispatch-intent / outcome writes. It counts
*failures*, not records.

**Interpretation (normative).** A failed terminal write leaves the journal
record non-terminal, which a later recovery classifies `unknown`. A failed
dispatch-intent write **refuses the consequential effect** (fail closed). This
counter makes those paths visible; it never changes them, and it is *not* a count
of lost or corrupted records.

### 3.5 Startup recovery (durable evidence, startup snapshot)

Set once per successful recovery pass, in `internal/launcher`, immediately
before the gateway begins serving.

| Field | Source | Meaning |
| --- | --- | --- |
| `journal.recovery_executions_scanned` | `Report.Scanned` | Execution records examined by that pass. |
| `journal.recovery_terminal` | `Report.Terminal` | Of those, already terminal (left untouched). |
| `journal.recovery_classified_unknown` | `Report.Recovered` | Records that pass newly classified `unknown`. |
| `journal.recovery_unknown_invocations` | `Report.UnknownInvocations` | Invocation records that pass newly classified `unknown`. |
| `journal.recovery_failures` | counter | Recovery passes that failed closed (startup aborts). |
| `journal.last_recovery_unix_seconds` | — | When the last successful pass completed (UTC seconds). `0` = none completed. |

**Invariants (normative).**

- The counts are exactly what the pass classified. A record already `unknown`
  from a *prior* pass is counted in `recovery_executions_scanned` but not in
  `recovery_classified_unknown` — re-running recovery is not double-counted.
- These fields are a **snapshot at startup**. They do not grow afterwards and
  are **not** a count of currently running executions, of records added since,
  or of the current journal size.
- `recovery_failures` is incremented and startup aborts; the service then does
  not serve.

### 3.6 Pre-existing control-plane metrics (unchanged)

`executor`, `backpressure`, `circuit_breaker` and `recovery` in the same
response are the pre-existing v-series fields and keep their existing meaning
and reset behavior. v15 does not redefine them. Note in particular that
`recovery.failure_count` is the **executor recovery manager's** failure count
and is unrelated to `journal.recovery_failures` (journal recovery passes).

---

## 4. Exposure and authorization

### 4.1 Endpoint

`GET /api/v1/control/metrics` — **extended in place**, not a new route.

The endpoint already existed and already carried
`executor`/`backpressure`/`circuit_breaker`/`recovery`. v15 **adds**
`execution`, `tools` and `journal` objects. This is a purely additive JSON
change: every pre-existing field keeps its name, type and meaning, so existing
consumers keep working.

Response format is JSON (`application/json`), matching the existing endpoint.
No new format or dependency is introduced.

### 4.2 Authorization (normative)

- The route is under `/api/v1/control/`, so the existing `authMiddleware`
  applies: `X-API-Key` must match the configured control API key, compared in
  constant time.
- **Fail closed.** With no control API key configured, the endpoint returns
  `403 CONTROL_DISABLED`. It is never left open.
- A wrong or missing key returns `401 UNAUTHORIZED` in the standard error
  envelope.
- No identity/role/permission model is added. Access is exactly the existing
  control-plane key. There is no RBAC, no owner concept, no per-tenant metrics
  scoping, and no claims about multi-tenant authorization of telemetry.

### 4.3 Middleware (normative)

The endpoint is served by the production handler and is subject to the full
chain, in the shipped composition order (outermost first):

1. security headers,
2. CORS (preflight short-circuits before auth),
3. per-IP rate limit,
4. identity binding (when enforced),
5. per-actor rate limit,
6. **control-plane API key**,
7. routes.

Specifically:

- It is **not** rate-limit exempt. Only `/health`, `/ready` and `/status` are.
  A control-plane metrics scrape therefore consumes the coarse per-IP budget,
  exactly like other control traffic.
- It is a `GET`, so it consumes no per-actor write bucket.
- Security headers and the configured CORS allow-list apply unchanged.

### 4.4 Information disclosure (normative)

The response contains **only** the fixed numeric fields of §3. It contains no
execution ids, request ids, agent ids, business or division ids, business
records, tool arguments, tool ids, URLs, file paths, store paths, prompts,
memory, tool output, credentials, secrets or raw error strings. There is no
store or journal path in any field, and no unbounded string is ever serialized.

Because the metric set is fixed, **cardinality is bounded by construction**:
the response has a constant number of fields regardless of load. See §5.

### 4.5 Failure behavior

- Collection cannot fail: `MetricsSnapshot` is a bounded read of atomics. It
  performs **no I/O**, touches no store and no journal, and therefore cannot
  recursively depend on the subsystem it is diagnosing. If the store is broken,
  metrics still answer.
- If no metrics owner is wired (any engine built without one), the snapshot is
  all zeros rather than an error.

### 4.6 Deliberately not added

No unauthenticated `/metrics` endpoint, no new port or listener, no
`/api/v1/metrics` outside the control prefix, and no change to the health
server's routes. A metrics surface reachable without the control API key would
broaden public access, which this milestone does not do.

---

## 5. Cardinality and collection bounds (normative)

- Every series is a **fixed struct field**. There are no label maps, no
  dynamically created series, and no metric keyed by any string.
- **No user-controlled value is ever used as a metric label** — not execution
  ids, request ids, correlation ids, agent ids, business ids, division ids,
  tool ids, operations, arguments, URLs, paths or error messages. This is
  enforced structurally: no API accepts a label parameter.
- The error-class and tool-outcome vocabularies are closed sets mapped onto
  fixed fields, with an `other` bucket, so a new code cannot grow the series
  count.
- Series count is constant: **30 numeric fields** in the v15 snapshot
  (15 `execution` + 8 `tools` + 7 `journal`), plus the pre-existing sections.
- All updates and reads are atomic; all structures are fixed-size. No
  in-memory collection grows with load.

High-cardinality debugging remains the job of the existing execution-trace
endpoint, whose redaction semantics are unchanged. Metrics were not given
high-cardinality labels to compete with it.

---

## 6. Interpretation rules (normative)

1. **`unknown` is not success.** A tool call or journal record classified
   `unknown` had an outcome that could not be established. It is never counted
   as completed, and no rate computed from it may be presented as a success
   rate without naming the uncertainty.
2. **Process-local ≠ durable.** Every series in §3 resets on restart (§2.1).
   Only §3.5 is derived from durable evidence, and only as a startup snapshot.
   Journal counts are **never** presented as counts of running executions.
3. **Counters are not rates.** The endpoint exposes no rate, percentile or
   latency statistic. Deriving one requires two samples and an external clock;
   nothing here provides that.
4. **No exactly-once claim.** Nothing in this document or its implementation
   establishes that an execution or tool effect happened exactly once. The
   runtime makes no such guarantee and these metrics do not imply one.
5. **No reconciliation claim.** A counter reaching a value does not establish
   that an external system agrees. No external reconciliation exists.
6. **No durability claim from metrics.** `journal.write_failures == 0` means no
   write call returned an error in this process; it is not a proof that the
   store is durable, intact or consistent.
7. **Missing instrumentation is not a zero.** A metric that is *unavailable*
   (§8) is absent, not zero, and its absence must never be read as "none
   occurred".

---

## 7. Retention operator accounting

`cmd/nexus-retention` remains the supported offline retention path
(`plan` = dry-run, `apply` = destructive). v15 adds accounting only.

Each run reports, to stdout:

- the run timestamp (UTC, RFC 3339) and the operation duration,
- the **selected parameters** the decision actually used: `min-age`, `batch`,
- records **examined**: executions and invocations scanned,
- records **eligible**: executions and invocations,
- records **rejected by reason**: nonterminal, unknown, too-young,
  ambiguous-invocation,
- records **physically deleted** — printed only when the storage operation
  returned success,
- the **outcome**, and whether the output is a `dry-run` or an `applied`
  deletion.

Normative rules:

1. **Dry-run stays strictly non-mutating.** `plan` reports the same decision
   `apply` would take and changes nothing.
2. **`apply` preserves v14 semantics.** Eligibility, the bounded batch and the
   single atomic `DeleteBatchHard` are unchanged; only reporting was added.
3. **A failure never prints a deletion count.** A failed batch is reported as
   failed, with the statement that no deletion is durably applied — an
   interrupted batch is rolled back (inline, or on the next store open) per the
   retention contract's crash matrix. The output never implies a deletion
   succeeded when the result is uncertain.
4. **No post-hoc audit record.** Nothing is written after a destructive
   operation and claimed to be atomic with it. The only atomic artifact is the
   store's existing `.batch-journal.json`, and no second transaction journal is
   introduced.
5. **Byte reclamation is deliberately omitted.** The store exposes no
   per-record size, and a directory-size delta would conflate the records with
   the index file. Any byte figure would therefore be an estimate, so the tool
   reports record counts and states explicitly that bytes are not reported.
   This is a reporting omission, not a claim that zero bytes were reclaimed.
6. **Disclosure.** The report body carries counts and status only — never
   record contents, ids, scope or credentials, and the line count is bounded
   regardless of how many records exist. A **failure** additionally echoes the
   underlying store diagnostic, which may name a record id or a file path. That
   is deliberate: this is an offline tool whose operator owns the data
   directory, it is not a network surface, and it never reads or prints
   secrets. The HTTP metrics endpoint is governed separately by §4.4, which
   permits no such values.
7. **No scheduling, no startup pruning, no online retention, no disk-pressure
   trigger** is added.

---

## 8. Health and readiness (unchanged)

v15 changes **no** health or readiness behavior. No response contract, status
code or field was added, removed or altered on `/health`, `/ready`, `/status`,
or the health server's `/health` and `/readiness`.

The existing model already distinguishes the required signals:

| Signal | Existing source |
| --- | --- |
| Process alive | gateway `GET /health`; health server `/health` |
| Required durable store opened and recovered; production provider configuration validated; accepting new work | health server `/readiness` (the launcher marks ready only after `lifecycle.Start`, which includes store open, journal recovery and config validation) |
| Engine accepting work | gateway `GET /ready` (503 until the engine is running) |

Because the distinction is not currently machine-readable *within a single
response*, an operator correlates the health server's `/readiness` with the
gateway's `/ready` and the `journal.last_recovery_unix_seconds` metric.

**Deferred, documented (not implemented).** A single response that enumerates
the five readiness conditions separately would require adding fields to a
public endpoint. That is a contract change for existing consumers and is out of
scope here. It is recorded as future work rather than shipped.

Also unchanged: liveness never depends on a remote provider call or a journal
scan, and readiness is never reported before required recovery completes. No
multi-instance or aggregated health model was invented.

---

## 9. Availability and collection cost

Durable store bytes and record counts are **not** exposed on the metrics
endpoint. An exact figure requires scanning the journal, and a scan on every
metrics request is explicitly rejected.

| Need | Where to get it |
| --- | --- |
| Process-local counters/gauges | `GET /api/v1/control/metrics` (O(1), atomic reads) |
| Recovery outcome counts and last recovery time | the same endpoint (§3.5) |
| Retention candidates / eligible / rejected / deleted | `nexus-retention plan` / `apply` (offline, quiesced) |
| Physical disk usage | the operator's own filesystem tooling on the data directory |

---

## 10. Unavailable metrics (documented gaps)

These are **not** instrumented, because no accurate, bounded-cost, authoritative
hook exists. They are absent, not zero (§6.7). Approximating them with
misleading counters is explicitly rejected.

| Gap | Why |
| --- | --- |
| Durable journal record count / current size | Requires a store scan; no bounded-cost exact source. The store's total record count is not journal-specific. |
| Durable store bytes reclaimed | No per-record size is exposed; see §7.5. |
| Delegation and coordination counts | The agent runtime has no single authoritative admission point for delegations and coordination members; counting at a loop would double-count and cannot be made exact without a contract change. |
| Governance *evaluations* | The governance engine evaluates at several gates (chain, executor, action admission). Counting there would either double-count or silently under-count. §3.2 counts only decisions that ended a run, which is exact. |
| Tool calls refused before dispatch | Counted at no dispatch; see §3.3.4. Available in the audit/event stream, not here. |
| Per-actor, per-tool or per-error rates | Would require high-cardinality labels (§5). |
| Cross-instance / cluster metrics | No multi-instance coordination exists (§1). |
| Latency distributions / percentiles | Would require a histogram with unbounded label and time-bucket cost; not implemented. |

---

## 11. Test obligations

Deterministic tests must cover, adapted to the implementation above:

1. a fresh metrics owner reports the §3.0 initial values;
2. logical executions and physical tool attempts are counted separately;
3. a retry increments `tools.retries` but never `tools.calls`;
4. terminal counters reflect the actual stored terminal result;
5. a cancellation that loses to an existing terminal result increments
   `cancellations_requested` and `cancellations_refused` and **not**
   `cancellations_won`;
6. approval pause/resume counters follow actual transitions;
7. an `unknown` outcome is never counted as completed;
8. recovery counters reflect exactly what recovery classified;
9. collecting metrics does not mutate journal records;
10. the metrics response exposes no ids, prompts, arguments, secrets or paths;
11. high-cardinality inputs cannot create high-cardinality series (structurally
    impossible — asserted by the absence of any label-taking API);
12. concurrent updates and reads are race-safe;
13. two independent engine/gateway instances do not share metric state;
14. a nil metrics owner neither blocks nor fails execution, approval,
    cancellation, dispatch or journal writes;
15. the endpoint requires the control API key, fails closed when unconfigured,
    and is not rate-limit exempt;
16. readiness remains false until the engine is running (unchanged behavior,
    regression-locked);
17. `plan` reports correct counts and leaves on-disk state unchanged;
18. `apply` reports deletions only when the storage operation succeeded;
19. the pre-existing retention crash-recovery and physical-deletion tests still
    pass unchanged;
20. the pre-existing approval/resume, cancellation, governance and
    execution-state consistency tests still pass unchanged.

No test may be weakened, skipped, quarantined, or retried until green.

---

## 12. Change history

- **v15** — added this document, the `metrics` owner and its injection points,
  the additive `execution`/`tools`/`journal` sections on
  `GET /api/v1/control/metrics`, and the retention operator's accounting
  (timestamp, parameters, examined/eligible/rejected/deleted, duration, outcome,
  dry-run marker, explicit omission of byte claims). Health and readiness
  unchanged.