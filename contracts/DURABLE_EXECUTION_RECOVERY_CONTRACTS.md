# NEXUS — Durable Execution Journal & Crash Recovery (v10)

Authoritative for the durable execution journal and the restart-classification
pass. Cross-references (never redefines): `CORE_INTERFACE_CONTRACTS.md`,
`AGENT_EXECUTION_CONTRACTS.md`, `AGENT_GOVERNANCE_CONTROL_CONTRACTS.md`,
`APPROVAL_LIFECYCLE_CONTRACTS.md`, `OPERATIONAL_RELIABILITY_CONTRACTS.md`,
`CAPABILITY_TOOL_CONTRACTS.md`, `EXECUTION_TRACE_CONTRACTS.md`,
`CANONICAL_TOOL_DISPATCH_CONTRACTS.md`, and the store's atomicity contract.

## 0. Scope and the one rule that governs everything

This milestone adds a **durable execution journal** and a **safe startup
recovery pass**. Its single safety rule:

> **Never blindly replay an operation whose outcome may be unknown.**
> The achievable contract is durable lifecycle evidence plus the refusal to
> replay ambiguous effects — **not exactly-once external execution.**

The journal lives in the existing durable `FileStore` (the same store the org
registry uses). No new database, broker, lock service, or persistence system is
introduced. Each journal write is a single, independently atomic `FileStore`
record write; no multi-record transaction is required or faked.

## 1. Durable execution identity (contract §A)

The durable record is `execution/<id>.json`, a `store.Record` whose `Data` is a
versioned `ExecutionRecord` JSON:

| Field | Meaning |
|---|---|
| `version` | durable schema version (currently `1`); unsupported → fail closed |
| `id` | execution id — equal to the runtime correlation id (`Request.ID`) |
| `business_id` | business scope (from runtime state, never model output) |
| `division_id` | division scope, when carried |
| `actor_id` | validated initiating principal reference (identity contract) |
| `state` | lifecycle state (§2) |
| `created_at` / `updated_at` | timestamps |
| `recovery` | recovery metadata when a state was set by the recovery pass |

Authorization is unchanged: `MembershipSet.AllowsScope` and the governance
engine remain authoritative. The journal is **not** a second authorization model
and grants nothing.

Invocation records are `invocation/<id>.json`:

| Field | Meaning |
|---|---|
| `version` | schema version |
| `execution_id` | the owning execution |
| `call_id` | logical-call id (the platform uses the correlation id) |
| `tool_id` / `operation` | the canonical dispatch target |
| `side_effect_class` | manifest-derived class (never model output) |
| `status` | `dispatched` / `completed` / `failed` / `unknown` |
| `error_class` | coarse class only; never a raw payload or secret |
| `created_at` / `updated_at` | timestamps |

Records store **only** the minimum recovery evidence: identifiers, scope,
state, coarse classes and timestamps. They never store raw prompts, model
output, tool inputs, credentials, provider secrets, or chain-of-thought.

## 2. Durable lifecycle (contract §B)

States (subset aligned with the existing engine vocabulary, which writes only
`completed`, `failed`, `cancelled`):

```
accepted ──▶ running ──▶ {completed, failed, cancelled, timed_out}
                │
                └──(recovery)──▶ unknown
```

- **accepted** — persisted before `SubmitRequest` returns success (durable
  acceptance).
- **running** — reserved; the current engine records `accepted` then terminal.
- **completed / failed / cancelled / timed_out** — terminal.
- **unknown** — assigned **only** by recovery to a nonterminal record whose
  in-flight effect may have occurred; it is an honest "we cannot know".

Terminal transitions are final. A terminal record is never moved back to a
runnable state by recovery. There is no automatic continuation in this
milestone.

> **v27 (ratified) — provisional approval pause, its one-shot supersede, and
> durable approval history.** Read together with §3 row 7 and the retention
> contract §2.
>
> Today the engine records an approval pause as terminal `failed` with
> `error_class = APPROVAL_REQUIRED`, and the durable record never changes
> afterwards — even when the approval is granted and the resumed execution
> completes successfully in-process. The in-process result and the durable
> record then permanently disagree.
>
> That pause is a **provisional terminal**: terminal for
> retention and for crash semantics, but supersedable exactly once, only by the
> same live process that holds the approval, and only to a real terminal state.
>
> - **The pause is provisional only while `error_class = APPROVAL_REQUIRED` and
>   no recovery metadata is present.** Any other terminal is final as today.
> - **Supersede is one-shot and in-process.** A distinct journal entry point
>   (not `Terminal`) performs it, gated on exactly `state = failed` **and**
>   `error_class = APPROVAL_REQUIRED`. Every other starting state is refused.
>   It never moves a record to a nonterminal or `unknown` state.
> - **Crash behaviour is unchanged.** A restart during a pause leaves the
>   provisional terminal untouched, because approvals are process-local and the
>   approval can no longer be granted — so "failed, awaiting approval" remains
>   the truthful durable verdict. This is already the documented §3 row 7
>   behavior and needs no recovery change.
> - **Recovery is unaffected.** Recovery never rewrites a terminal record, and a
>   provisional pause is terminal, so the supersede can never be resurrected by
>   a later startup.
>
> - **Approval history is durable and separate from the outcome.** Each approval
>   fact — requested, granted, denied — is appended to the execution record's
>   `approvals` history with its approval id, decider and time. History is
>   append-only, is never rewritten by recovery, and survives the execution's
>   final terminal state. It is independent of `state` and of `error_class`, so
>   neither is overloaded to carry it.
>
> "Terminal is final" is therefore narrowed from an absolute to a scoped rule,
> in exchange for a durable record that states the true outcome. This is the
> ratified trade-off.

### State-transition table (v27, ratified)

| From | Event | To | Writer | History appended | Notes |
|---|---|---|---|---|---|
| *(absent)* | `SubmitRequest` accepted | `accepted` | `Accepted` | — | durable acceptance precedes the 202 |
| `accepted` | run terminates | `completed` / `failed` / `cancelled` | `Terminal` | — | first terminal wins, unchanged |
| `accepted` | approval gate reached | `failed` + `APPROVAL_REQUIRED` | `Terminal` | `requested` | **provisional** pause |
| `failed` + `APPROVAL_REQUIRED`, no recovery meta | approval granted | *unchanged* | `RecordApproval` | `granted` | recorded at grant time, before the resumed run ends |
| `failed` + `APPROVAL_REQUIRED`, no recovery meta | resumed run terminates | `completed` / `failed` / `cancelled` | `SupersedeApprovedPause` | — | once only; the truthful outcome replaces the pause |
| `failed` + `APPROVAL_REQUIRED` | approval **denied** | `failed` + `POLICY_DENIED` | `RecordApproval` + `SupersedeApprovedPause` | `denied` | no resume; the denial is durable and distinguishable from a pending pause |
| `failed` + `APPROVAL_REQUIRED` | process exits; approval abandoned | *unchanged* | — | `requested` only | still terminal, ages on `UpdatedAt`, eligible after `MinAge`, **not** resumable |
| any nonterminal | startup recovery | `unknown` | `Recover` | — | unchanged; never re-driven |
| any terminal | startup recovery | *unchanged* | — | — | unchanged; history is never rewritten |
| any terminal / `unknown` | re-submitting the same execution id | *unchanged*, refused | `Accepted` | — | already shipped in v25 |

Every transition not in this table is refused. In particular `accepted` is
never written back over a terminal, `unknown` is never rewritten, a supersede
never produces a nonterminal state, and history is never fabricated for an
execution that was not durably accepted.

### Duplicate-submission behaviour (v27, ratified)

Submitting an execution id that already carries a terminal or `unknown` durable
record is refused at durable acceptance; the record is left untouched and the
execution does not run. This is already implemented (v25) and is unchanged here.
It is restated because the supersede relies on the same rule: a pause record is
terminal, so its id cannot be re-admitted as fresh work.

### Acceptance criteria (v27, ratified)

1. An approval pause is journaled `failed` + `APPROVAL_REQUIRED` and is
   immediately readable as such.
2. After a grant, a completed execution reads `completed` durably — the
   in-process result and the durable record agree.
3. `Terminal()` keeps first-wins semantics unchanged; a second ordinary terminal
   write is still a no-op.
4. A supersede is refused unless the record is exactly `failed` +
   `APPROVAL_REQUIRED` with no recovery metadata; every other starting state is
   refused without mutating the record.
5. A supersede never yields a nonterminal or `unknown` state, and never moves a
   record that recovery has already classified.
6. After a restart with a pending approval, the record is unchanged, the
   approval is gone, and the execution is not resumable.
7. An abandoned approval becomes retention-eligible on the ordinary `MinAge`
   basis; no new policy knob is introduced.
8. Every pre-existing test keeps passing **without changing its expectation** —
   in particular `TestTerminalIsMonotonic`, `TestTerminalRejectsNonTerminalState`
   and the retention suite.

### Regression scenarios (v27, ratified)

Pause then grant then complete → durable `completed`; pause then deny → durable
`failed` with the denial class, distinguishable from a still-pending pause;
pause then cancel → durable `cancelled`;
supersede twice → second refused, record unchanged; supersede from `completed`,
`cancelled`, `timed_out`, `unknown` or `accepted` → refused; supersede after
recovery has classified the record → refused; restart during a pause → record
unchanged and not resumable; abandoned approval past `MinAge` → retention
eligible and removed; abandoned approval before `MinAge` → retained; a completed
id re-submitted → refused at acceptance and the record unchanged; the full
existing journal, recovery, retention, gateway and E2E suites unchanged.

## 3. Crash windows (contract §C)

| # | Crash point | Recovered state | May the effect be dispatched again? |
|---|---|---|---|
| 1 | Before a tool invocation is admitted | execution nonterminal → `unknown` | no (nothing was admitted) |
| 2 | After governance admission, before dispatch | nonterminal → `unknown`; no intent record | no |
| 3 | After dispatch begins, before a result is recorded | invocation `dispatched` → `unknown`; execution `unknown` | **no** — outcome may be unknown |
| 4 | After the external effect succeeds, before completion is recorded | same as 3 → `unknown` | **no** — the effect may have happened |
| 5 | After a step is durably completed, before the parent advances | nonterminal → `unknown` | no |
| 6 | During cancellation / timeout handling | nonterminal → `unknown` (cancellation is **not** assumed) | no |
| 7 | During approval pause or resume | recorded terminal `failed` (`APPROVAL_REQUIRED`) by the engine; not resumed | no — fresh admission required |
| 8 | During parallel child execution / aggregation | nonterminal → `unknown` | no |
| 9 | During a journal update or store journal recovery | the `FileStore` batch-journal recovery runs first; a partially written record is atomic (old-or-new) | no |

In every window the recovery pass **classifies**, it does not continue. A
missing invocation outcome is never treated as proof that the operation did not
run.

## 4. Explicit non-guarantees (contract §D)

- Durable execution records do **not** make external effects exactly-once. A
  crash may leave an external operation's outcome unknowable.
- Unknown operations are **never** blindly retried.
- Multi-instance execution coordination is **not** provided.
- Approval/resume continuity across restart is **not** provided; approval
  snapshots remain process-local, and any later continuation requires fresh
  governance admission with binding, scope, single-use and expiry validation.
- Process-local traces remain non-durable.
- The durable journal does **not** restore the HTTP execution-result surface:
  the existing API keeps its documented process-local `404`-after-restart
  behavior (`CORE_INTERFACE_CONTRACTS`; E2E `15-agent-execution`). The journal is
  recovery evidence, not a result cache. Exposing durable results through the
  API is deferred.
- Backup/restore semantics are unchanged; a durable-store backup now also
  captures the journal records, but restoring it cannot reconstruct
  process-local runtime state.

## 5. Startup recovery order

1. Open and recover the `FileStore` (existing journal replay; fail closed on
   corruption).
2. Run `Journal.Recover()`:
   - load and validate every `execution` record (version + required fields);
   - corrupt/contradictory/unsupported → **abort startup** (fail closed);
   - durably-terminal records are left terminal;
   - nonterminal records → set `state=unknown` with `recovery` metadata;
   - `dispatched` invocation records for recovered executions → `unknown`;
   - return a bounded, deterministic report (counts) for the startup log.
3. Only then does the gateway accept new work.

Recovery is bounded by the number of records and is deterministic; it never
reads the in-memory event bus and never fabricates a result.