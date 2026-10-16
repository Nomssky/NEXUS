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