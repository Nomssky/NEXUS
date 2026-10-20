# NEXUS — Execution Journal Retention & Physical Reclamation (v14)

Authoritative for execution-journal retention and physical disk reclamation.
Cross-references (never redefines): `DURABLE_EXECUTION_RECOVERY_CONTRACTS.md`
(the journal + recovery, v10), `DURABLE_EXECUTION_READ_AND_RECONCILIATION_CONTRACTS.md`
(the read API, v11), `OPERATIONAL_RELIABILITY_CONTRACTS.md`, and the store
atomicity contract (`internal/foundation/store`). Design rationale:
`docs/EXECUTION_JOURNAL_RETENTION_DESIGN.md` (v13).

This document separates three things explicitly:

- **Established** — behavior that already exists and v14 relies on.
- **New in v14** — behavior this milestone adds.
- **Deferred** — analyzed but deliberately not built.

---

## 0. Scope and the one rule

Retention physically removes a *verified subset* of durable execution-journal
records to reclaim disk. Its single safety rule:

> **Never remove recovery evidence that could still be needed.** A record is
> removed only when it is provably terminal, provably old, and provably without
> any ambiguous consequence.

Retention is **off by default**, is **never** a startup/backup/restore side
effect, and is **never** triggered by disk pressure. Deleting evidence to make
room is forbidden.

## 1. What is established (v10–v13, unchanged)

- Journal records are `execution/<id>.json` and `invocation/<id>.json`, one
  independently-atomic FileStore record write each. The journal stores only
  identifiers, scope, coarse classes and timestamps.
- Terminal lifecycle states are final (`Journal.Terminal` is first-wins);
  recovery never rewrites a terminal record. Recovery classifies an interrupted
  (nonterminal) execution, and a `dispatched` invocation of an interrupted
  execution, as `unknown`. `unknown` is assigned only by recovery.
- A corrupt, unsupported or contradictory record aborts startup (fail closed)
  and is rejected on read; the reader never reinterprets an unknown version.
- `Store.DeleteBatch` is an atomic **soft** delete (journaled, rolled back on an
  interrupted batch); it reclaims no disk.
- Backup captures every record file plus the root batch journal; restore
  reopens the store through its real recovery path before serving.
- Process-local approvals, resume snapshots and traces stay process-local; the
  execution-result API stays `404`-after-restart.

## 2. Eligibility (new in v14)

A durable **execution** record is *eligible* for removal only if **all** hold:

1. **Terminal:** `state ∈ {completed, failed, cancelled, timed_out}`. A
   nonterminal state (`accepted`, `running`, `pending_approval`) is never
   eligible. `unknown` is never eligible (it is not terminal).

   > **v27 (ratified) — the approval pause is deliberately terminal.** An
   > execution awaiting approval is journaled `failed` with
   > `error_class = APPROVAL_REQUIRED`, and is treated here as a **provisional
   > terminal**. That is what makes an abandoned approval reclaimable without a
   > new expiry knob: it ages on the ordinary `UpdatedAt` basis and becomes
   > eligible after `MinAge` like any other terminal record.
   >
   > The alternative — a nonterminal `pending_approval` — was rejected because
   > rule 1 would make every abandoned approval permanently ineligible, and
   > carving out an exception for `pending_approval` alone would be
   > inconsistent (an equally-abandoned `accepted` record stays ineligible) and
   > would change eligibility for a state nothing currently writes.
   > `pending_approval` therefore remains **reserved and unreachable**; see the
   > read contract §4.
   >
   > Provisional does not mean weak: the pause is a full terminal for
   > eligibility, for crash recovery and for the §4 disjointness argument. It is
   > supersedable exactly once, in-process only — see the recovery contract §2.
2. **Old enough:** `now - UpdatedAt >= MinAge`, where `UpdatedAt` is the
   record's last durable transition time and `now` is the operation's clock.
   The boundary is inclusive: a record exactly `MinAge` old is eligible.
   Age alone never makes a record eligible — criteria 1 and 3 also apply.
3. **No ambiguous consequence:** every invocation record whose
   `execution_id == execution.id` has a terminal-known status
   (`completed`/`failed`). If any invocation is `dispatched` or `unknown`, the
   execution is retained (and so are all its invocations).

An **invocation** record is eligible only as part of an eligible execution:
it is removed together with its execution, and only when the execution is
eligible. An invocation whose execution is missing from the scan is never
removed (fail safe).

**Fail closed:** if *any* candidate record is malformed, has an unsupported
schema version, or is otherwise inconsistent, the whole operation refuses and
removes nothing. No record is skipped-and-continued. A missing invocation
record cannot make an execution eligible.

## 3. Atomicity and crash recovery (new in v14)

Removal uses the store's **physical batch delete**, `Store.DeleteBatchHard`,
which reuses the existing `.batch-journal.json` transaction (no second
mechanism). Its observable states:

| Phase | On-disk state | Reader sees |
|---|---|---|
| Before journal publication | all target files present, no journal | all records present |
| Journal published, before any unlink | all files present, journal present | all records present |
| Midway through unlinks | some files removed, journal present | batch not visible as committed (store lock serializes; recovery rolls back on open) |
| All files removed, before journal cleanup | no files, journal present | recovery rolls back (files re-created) on open |
| Journal removed (committed) | no files, no journal | records absent, permanently |
| Corrupt/foreign-version journal | — | **open aborts** (fail closed) |

- The pre-batch records are written to the journal and made durable *before*
  any file is removed, so recovery evidence can never be lost before the
  transaction is durably committed. Commit is the durable removal of the
  journal, which happens only after every unlink and directory fsync.
- On open, the store's existing `recoverInterruptedBatch` runs before any
  record is loaded. Its rollback rewrites each journaled record, which
  **re-creates** a physically-deleted file. The same journal therefore serves
  soft and physical batches; there is no nesting or competing journal.
- Retry after interruption is idempotent: a rolled-back batch leaves the
  records present and re-eligible; a committed batch leaves them absent and no
  longer eligible.
- No stronger durability is claimed than the filesystem and implementation
  provide: durability rests on temp-file fsync, rename, and directory fsync, as
  the store already documents.

## 4. Concurrency (new in v14)

- **Single process.** `DeleteBatchHard` holds the store lock for the whole
  batch and updates the in-memory index only after the durable commit, so a
  concurrent `Put`, `Get`, `List` or `DeleteBatch` observes the whole batch or
  none of it and can never observe a half-applied deletion. The batch is
  bounded, so the lock is never held across unbounded work.
- **Journal disjointness.** Recovery mutates only nonterminal executions and
  `dispatched` invocations; retention removes only terminal executions and
  terminal-known invocations. The two sets are disjoint, so retention and
  recovery never touch the same record.
  > **v27 (ratified).** The in-process approval supersede is a third writer.
  > It writes only to a record that is *already terminal*
  > (`failed` + `APPROVAL_REQUIRED`), so it is disjoint from recovery in the
  > same way, and it is disjoint from retention by the offline precondition
  > below: a supersede requires a live process holding a live approval, and
  > retention requires a quiesced store. The two preconditions are mutually
  > exclusive, so no supersede can be in flight during a retention run.
- **New records are never lost.** Eligibility is decided from durable state;
  a record written after (or concurrently with) the decision has a new id and
  is not in the removal set. A concurrent write to an eligible record cannot
  occur under the current runtime (terminal is final for executions; invocations
  are written once per dispatch), and in any case a writer blocks on the store
  lock for the batch's duration.
- **Multi-instance:** not supported and not claimed. Retention is an
  operator-run, offline operation (like backup): it requires the store to be
  quiesced (no other process using the data directory). Running it against a
  live store is unsupported; there is no cross-process lock, and none is added.

## 5. Compatibility and recovery semantics (unchanged)

- `unknown` recovery semantics are preserved: retention never converts a state
  and never redispacts an uncertain effect.
- Terminal-is-final is preserved apart from the one ratified narrowing: the
  in-process supersede of an approval pause (recovery contract §2), scoped to a
  record that is already terminal, never moving it to a nonterminal or
  `unknown` state, and refused once recovery has classified it.
- Approval history lives on the execution record itself, so it is retained and
  reclaimed with that record: a retained execution keeps its approval history,
  and removing the execution removes both together. No separate record class,
  scan or eligibility rule is introduced.
- Existing durable records need no migration. A record already written as
  `failed` + `APPROVAL_REQUIRED` is interpreted identically before and after
  this change: it is eligible for removal after `MinAge` either way. A supersede
  can only be triggered by a live approval held by the live process, which a
  pre-upgrade record cannot have, so an existing record is never rewritten by an
  unrelated run. `SchemaVersion` stays `1`.
- The v10 recovery pass and the v11 read API keep their response contracts; the
  read API's statuses, authorization, bounds and allow-listed fields are
  unchanged.
- Approvals, resume snapshots and traces remain process-local.
- No exactly-once claim, no reconciliation, no distributed coordination.

## 6. Operational safety (new in v14)

- **Disabled by default.** Retention runs only through an explicit operator
  invocation with retention enabled and `MinAge` set. No background scheduler,
  no startup pruning, no automatic pruning in backup or restore.
- **Dry-run is read-only.** A plan/dry-run reports examined, eligible, retained
  and deleted counts and the reason classes, and mutates nothing (no record
  bytes, filenames, journal metadata or disk usage change). The destructive
  operation is separate and explicit.
- **Inspectable policy.** The age basis, boundary, batch bound and eligibility
  rules are stated here and reported by the dry-run before any deletion.
- **Failure handling.** If the operation fails (I/O error, corrupt journal),
  it removes nothing that the transaction did not commit; the batch journal
  guarantees all-or-nothing. If the transaction metadata is corrupt or
  unsupported, the store open aborts (fail closed) and retention cannot run.
- **No disk cap.** There is no hard disk cap that deletes to make room.

## 7. Deferred (analyzed, not built in v14)

- **Summary/roll-up compaction** (write one aggregate record and delete many)
  is **not** implemented: the store has no atomic "write one + delete many"
  primitive, and a sequence of independent writes is not a transaction. If a
  later milestone needs it, the smallest prerequisite is a journaled write+delete
  batch (extending the batch journal) or a two-phase intent with replay.
- **Hard delete of individual records** as a public store operation (retention
  uses the batch form only).
- **Online/live retention**, cross-process locking, multi-instance coordination.
- **Schema migration** of existing records: none is added. `SchemaVersion`
  stays `1`; a record of another version fails closed (read and open) as today.
- **Metrics/alerts and dashboards** for disk growth.
- **Retention of any record class other than the execution journal.**
- **Reconciliation** of `unknown` — unchanged from v11.

## 8. Required tests before this is trusted

Deterministic, no sleeps: retention-disabled no-op; dry-run mutates nothing;
nonterminal retained; `unknown` retained; dispatched/unknown invocations
retained; eligible terminal records removed only when enabled; boundary age;
corrupt/unsupported fail closed; idempotent repeats; a concurrent write is not
lost; the crash matrix of §3 at every boundary (previous-file, journal-no-unlink,
midway, all-removed-pre-cleanup, committed, corrupt-journal); existing
batch-journal recovery, store atomicity, journal terminal monotonicity and
execution recovery continue to pass; and backup/restore recovers a pending
transaction safely. Physical reclamation is demonstrated by inspecting on-disk
files, not the store's logical listing.