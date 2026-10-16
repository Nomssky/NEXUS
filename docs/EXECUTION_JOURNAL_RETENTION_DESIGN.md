# NEXUS — Execution Journal Retention & Schema-Migration Design (v13)

Status: **design only**. Nothing in this note is implemented. It describes what
exists today, then what *could* be built. No retention, expiry, compaction, or
schema migration exists in the current tree, and none is added by v13.

Cross-references (never redefines): `DURABLE_EXECUTION_RECOVERY_CONTRACTS.md`
(v10, the journal + recovery), `DURABLE_EXECUTION_READ_AND_RECONCILIATION_CONTRACTS.md`
(v11, the read API), `OPERATIONAL_RELIABILITY_CONTRACTS.md`, and the store's
atomicity contract (`internal/foundation/store`). Operations guidance lives in
`docs/operations-runbook.md` §3, §7.

---

## 0. What is established today (not proposed)

- Journal records live in the existing durable `FileStore`, one file per record:
  `execution/<id>.json` and `invocation/<id>.json` (`internal/execjournal`).
  Each write is a single, independently atomic `FileStore` record write
  (temp file → fsync → rename → directory sync).
- `SchemaVersion` is `1`. A record whose `version != 1`, whose `id` is empty, or
  whose `state` is outside the closed vocabulary is **rejected** on read and
  **aborts startup** during recovery (fail closed). The reader never reinterprets
  an unknown version.
- Terminal lifecycle states (`completed`, `failed`, `cancelled`, `timed_out`)
  are **final**: `Journal.Terminal` is first-wins (`existing.State.Terminal()` →
  no-op), recovery never rewrites a terminal record, and a failed terminal write
  leaves the record nonterminal (so recovery classifies it `unknown`). `unknown`
  is assigned **only** by recovery, and an interrupted invocation stays
  `dispatched`/`unknown` and is never replayed.
- The store's only atomic multi-record primitive is `Store.DeleteBatch(ids)`: it
  removes a **set** as one step, with a durable `.batch-journal.json` written
  before any file is published, synchronous rollback on error, and startup
  rollback of an interrupted batch. **It is a logical (soft) delete**: it writes
  `status=deleted` back to the same `<type>/<id>.json` file. `List` skips
  soft-deleted records unless the filter asks for them. **There is no hard-delete
  primitive, so `DeleteBatch` does not reclaim disk.**
- `store.Record` has `ExpiresAt` and `IsExpired`, but **nothing sweeps durable
  records by expiry**. (Expiry is consumed by the in-memory memory platform, not
  the journal.)
- The v11 read API is bounded: `limit ∈ [1,200]`, default 50, newest-first, and
  the underlying scan is capped at `MaxListScan = 5000` records per business.
  This bounds *read* cost, not *storage*.
- Backup (`internal/backup`) tars every `<type>/*.json` record file plus the
  root `.batch-journal.json` if present, with SHA-256 checksums and a manifest;
  restore writes them into an empty destination. Trivial for the operator to run,
  but it captures the **entire** journal every time.
- **Net effect: journal disk usage grows without bound** (one file per execution
  plus one per consequential invocation) until an operator intervenes. There is
  no metric, limit, alert, or retention today.

---

## 1. Authoritative recovery evidence vs disposable operational metadata

The journal is deliberately minimal (identifiers, scope, coarse classes,
timestamps). It is therefore **almost entirely** recovery evidence, and the
default posture must be **retain everything**.

- **Authoritative (never expire without a human disposition):**
  - every nonterminal execution (`accepted`, `running`, `pending_approval`);
  - every `unknown` execution and every invocation that is `dispatched` or
    `unknown` — an ambiguous effect;
  - a terminal execution that owns any such invocation.
- **Candidates for bounded retention (proposed):** terminal executions
  (`completed`/`failed`/`cancelled`/`timed_out`) whose `updated_at` is older than
  an operator-configured horizon **and** whose invocation records are all
  terminal-known (`completed`/`failed`), together with those terminal invocation
  records.
- **Disposable:** nothing is disposable by default. Retention is opt-in and
  default-off (retain forever), consistent with the store being the source of
  truth.

## 2. Whether terminal records can safely be expired

Only under all of these criteria, evaluated as one predicate:

1. the execution is **terminal** (not nonterminal, not `unknown`);
2. `updated_at` is older than the configured horizon;
3. **every** invocation record for the execution is terminal-known;
4. the policy is enabled and the horizon is configured (default off).

A record that fails any criterion is retained. In particular, `unknown` and any
execution with a `dispatched` invocation are **never** expired, because that is
exactly the evidence a human needs to reconcile an ambiguous effect.

## 3. Preserving nonterminal/unknown evidence until a safe disposition exists

Retention must treat nonterminal and `unknown` records as a *held set*: the
sweeper skips them and counts them separately. There is no automatic disposition
— resolving an `unknown` requires an evidence source and an authority model that
v11 explicitly deferred (`DURABLE_EXECUTION_READ_AND_RECONCILIATION_CONTRACTS.md`
§7). Until such a disposition exists, the only safe action is to keep the
evidence and surface its age/count for operators (§8).

## 4. Interaction with backup/restore

- Backups capture the whole journal; a retention policy changes what **future**
  backups contain, not past ones.
- Restoring an older backup **reintroduces** records a retention pass may have
  removed. Retention must therefore be **idempotent and re-runnable**: it makes a
  decision from the records' own state and timestamps, never from a "already
  pruned" marker the restore would not carry.
- Because the current delete is a **soft** delete, a soft-expired record is still
  present in `<type>/<id>.json` (with `status=deleted`) and is therefore still in
  the backup; a restore of it is re-swept. This is safe but does **not** bound
  backup size. Bounding backup size requires physical removal, which requires
  the prerequisite in §5.
- Restore remains "into an empty destination"; retention never runs against the
  live store during a restore.

## 5. Crash consistency and atomicity requirements for future compaction

- **Delete-only compaction** (remove a set of whole records, no summary record)
  can use `DeleteBatch` directly: it is atomic, journaled, and rolled back on
  crash. This is the **only** compaction shape the current interface supports
  atomically.
- **Summarising compaction** (write one rollup record and remove the originals)
  **cannot** be atomic today: `Put` writes a single record and `DeleteBatch` only
  deletes; there is **no** atomic "write N + delete M" transaction. A sequence of
  independent `Put`/`DeleteBatch` calls is **not** transactional and must not be
  described as such.
  - Smallest prerequisite: a store primitive that publishes a new record and
    removes a set atomically (a single journaled batch covering both), **or** a
    two-phase design with its own durable intent record and an idempotent startup
    replay (write rollup → durably record the intent → `DeleteBatch` the
    originals → clear the intent), failing closed if the intent is ambiguous on
    open.
- **Hard deletion (disk reclamation)** is also a prerequisite: the current
  `Delete`/`DeleteBatch` soft-delete only. Reclaiming space needs a physical
  remove that is crash-safe (unlink/rename + directory sync) and journaled like
  `DeleteBatch`.
- Any future compaction must write and fsync its intent **before** mutating, and
  must be a no-op if re-run after it already completed.

## 6. Coexisting / migrating schema versions

- Today `SchemaVersion = 1` and a non-`1` record is **rejected** (read) and
  **aborts startup** (recovery). So a `version: 2` record cannot simply appear:
  the code must first be taught to accept it.
- Recommended path: keep old and new records both readable by an explicit
  **version set** (`{1, 2}`) during a migration window, with a default of
  "write the newest version, read every supported version". Never guess at an
  unsupported version.
- Any rewrite of existing records (1 → 2) is itself a durable write: it needs a
  resumable, idempotent migration pass with a durable progress marker, run
  **before** the gateway accepts new work (same slot as the existing recovery
  pass), and it must be safe to interrupt and re-run.
- The v11 read API exposes `state`/`error_class`/timestamps, not the raw version,
  so reader compatibility is a matter of continuing to project the same fields
  across versions; it must not silently reinterpret a field.

## 7. Fail-closed behavior on malformed records / interrupted migrations

- Malformed or unsupported records keep today's behavior: **abort startup**
  (recovery) and **reject** (read). Retention/migration never "skip and
  continue" past a corrupt record.
- An interrupted migration or compaction must be resolved deterministically on
  the next open (roll back, or complete idempotently) or **abort startup** —
  never expose a half-migrated/half-compacted view.
- A retention pass that cannot read a record must fail that record (retain it)
  and record the failure, never delete on uncertainty.

## 8. Disk-growth visibility and operational alerts/limits

Proposed (none exist today):

- A periodic gauge of store size: total record count (`CountAll` including
  soft-deleted), per-type counts (`Count` by `execution`/`invocation`), and
  on-disk bytes for the data directory.
- Counts of `unknown` and nonterminal records (the held set) and their oldest
  age — the numbers that must **not** be pruned.
- Alert thresholds (warn/act) per class, and a configurable hard cap that
  **refuses new acceptances** (fail closed, exiting the documented submit-error
  path) rather than silently dropping or overwriting evidence.
- Explicit non-goal: the cap must never delete records to make room.

## 9. Tests required before any destructive retention ships

Every listed test is a prerequisite, not a proposal:

1. **Reason predicate**: table tests proving the retention predicate keeps
   nonterminal, `unknown`, and any execution with a `dispatched` invocation.
2. **Atomicity**: a `DeleteBatch`-based compaction that is interrupted mid-batch
   leaves the store reading as pre-batch after reopen (reuse the store's batch
   crash tests).
3. **Idempotence/re-run**: running the pass twice removes nothing the second
   time and never errors.
4. **Backup/restore interaction**: prune → back up → restore reintroduces the
   records and the pass re-prunes them deterministically.
5. **Fail-closed**: a corrupt record aborts/skips-as-retained, never deletes.
6. **Migration**: version-1 and version-2 records coexist; a `version:3` record
   fails closed; an interrupted migration resumes/rolls back.
7. **No effect on live state**: retention never touches in-memory results,
   approvals, the v10 recovery path, or the v11 read API's statuses/authorization.
8. **Bounds**: the read API's `MaxListScan`/limit behavior is unchanged by any
   retention bookkeeping.

## 10. Explicit non-goals

- No distributed locking, no multi-instance coordination.
- No exactly-once external-effect guarantees; retention never claims to prove an
  effect did or did not occur.
- No implicit reconciliation: retention never converts `unknown` to any other
  state, and no automatic replay of anything.
- No change to the process-local execution-result semantics or the frozen
  post-restart `404`; no change to the v11 read-route authorization or
  allow-listed fields.