# NEXUS — Operations Runbook (backup, restore, rollback, secrets)

This runbook documents the **actual** behavior of the current implementation. It
covers durable-store backup and restore, environment separation, rollback, and
secret rotation. It does not describe a deployment platform, and it does not
claim that process-local runtime state can be recovered.

For the release build, artifact verification and the isolated smoke test, see
`docs/release.md`.

Contract references: `contracts/CORE_INTERFACE_CONTRACTS.md`,
`docs/m0-development.md` (configuration), `docs/http-gateway.md` (HTTP posture).

---

## 1. Durable state: what is persisted and what is not

The durable store is a `FileStore` rooted at `storage.data_dir`:

```
<data_dir>/
  identity/<id>.json         durable organization records
  business/<id>.json
  division/<id>.json
  credential/<id>.json       verification HASHES only — never raw secrets
  membership/<id>.json
  agent/<id>.json            durable agent definitions
  memory/<id>.json           durable memory records (when enabled)
  <other type>/<id>.json     any other store record type
  .batch-journal.json        transient crash-recovery journal (present only mid-batch / after a crash)
  workspaces/<business>/…    filesystem sandbox (NOT part of the store backup)
```

Records are written **atomically** (temp file → fsync → rename → directory
fsync). A delete batch is atomic across a crash via `.batch-journal.json`, which
the store replays and removes on the next open; a corrupt or ambiguous journal
aborts the open rather than guessing.

**Durable and recoverable from a backup:** identities, businesses, divisions,
credentials (hashes), memberships, agent definitions, and durable memory — the
JSON record files above.

**Process-local and NOT recoverable from a backup** (lost on restart, never
restored by this tool):

- running executions and their in-memory results;
- approval and resume snapshots;
- cancellation state;
- escalation state (process-local);
- runtime event buffers and execution traces.

A store backup is not a snapshot of the running system. See §6.

---

## 2. Environment separation and required production configuration

- **Environment** is `nexus.environment` (env `NEXUS_ENVIRONMENT`):
  `development` | `staging` | `production`. Default `development`.
- **Production gates** (fail-closed at startup):
  - at least one real model provider under `models.providers`
    (`config.provider_missing`); the simulated/scripted provider is
    development-only;
  - a non-empty durable `storage.data_dir` (`config.storage_missing`);
  - `security.{audit_enabled,require_authentication,enforce_business_scope,sandbox_enabled}`
    all `true`, and `security.dev_allow_unsafe_overrides` `false`
    (`config.security_unsafe`).
- **Secrets are environment-only, never in the config file.** A provider entry
  names the environment variable that carries its credential:

  ```json
  { "models": { "providers": [
      { "id": "openai", "endpoint": "https://…/v1", "model_id": "…",
        "api_key_env": "OPENAI_API_KEY" } ] } }
  ```

  The value of `OPENAI_API_KEY` is read from the environment at startup and is
  never written to a log, response, trace or the store. Two operating secrets
  are always environment-only: `NEXUS_CONTROL_API_KEY` (control-plane
  endpoints; empty disables them, `403`) and `NEXUS_BOOTSTRAP_CREDENTIAL`
  (first-install credential; empty leaves scoped endpoints `401`).

- **Safe to commit:** the configuration file with non-secret operational values
  (`nexus`, `logging`, `health`, `lifecycle`, `security`, `storage`, `models`
  provider *references*, `rate_limit`, `http`). **Never commit:** the values of
  `api_key_env`-named variables, `NEXUS_CONTROL_API_KEY`,
  `NEXUS_BOOTSTRAP_CREDENTIAL`, or any `.env` file.
- **Verifying effective configuration without exposing secrets:** the process
  emits structured JSON startup logs (secrets are never logged); provider
  registration logs `provider_id`/`model_id`/`endpoint` only; the config
  fingerprint and redacted snapshot are produced internally
  (`config.Snapshot.Redacted()`). There is no secret-dumping CLI; inspect the
  config file plus non-secret environment variables and the startup logs.

---

## 3. Backup and restore

The tool is `nexus-backup` (source `cmd/nexus-backup`). Build it with:

```bash
go build -o bin/nexus-backup ./cmd/nexus-backup     # or: make build-backup
```

### 3.1 Quiescence requirement (consistency model)

The store writes atomically, so a **read-only copy taken while no writer is
active is a consistent committed state**. `nexus-backup backup` therefore
**requires the application to be stopped** and never opens or mutates the
source store. A recursive copy of a *live* data directory is NOT supported and
is not what this tool does.

### 3.2 Create a backup

```bash
# 1. Stop the NEXUS application (quiesce writers).
# 2. Back up the durable store:
nexus-backup backup -data-dir /var/lib/nexus -out /backups/nexus-$(date -u +%Y%m%dT%H%M%SZ).tar
# backup: N file(s) → … (format v1, created …)
```

- The artifact is a single tar whose **first entry is the manifest**
  (`nexus-backup-manifest.json`) followed by the store files at their
  store-relative paths.
- The manifest records format/version, creation time, store layout, scope,
  exclusions, and each file's path, size and **SHA-256**.
- The tool **refuses to overwrite an existing artifact**, refuses a symlinked
  store root entry, and never includes the `workspaces/` sandbox, temp files,
  `.env` files or secrets.
- On any failure no artifact is published (it is written to a temp file and
  atomically renamed only on success).

### 3.3 Verify a backup (read-only)

```bash
nexus-backup verify -in /backups/nexus-….tar
# verify: OK — N file(s), format v1, created …
```

Verification checks the manifest, the inventory (no missing or extra entries),
every SHA-256 and size, rejects traversal/absolute/symlink entries and
unsupported format versions, and **mutates nothing**.

### 3.4 Restore into a new destination

```bash
# The destination must be empty or not exist. Never restore into the live store.
nexus-backup restore -in /backups/nexus-….tar -dest /var/lib/nexus-restored
# restore: OK — N file(s) restored and reopened into … (format v1)
```

- The store is built in a hidden staging directory beside the destination and
  **published with an atomic rename only after the whole artifact verifies and
  the store reopens cleanly** through the real `FileStore` initialization/
  recovery path.
- A non-empty destination is refused; a failure removes the staging directory
  and leaves the source artifact and any existing data untouched — a partial
  restore is never published as a valid store.
- Restoring the same artifact into two empty destinations yields equivalent
  logical state.

### 3.5 Post-restore validation and cutover

1. `nexus-backup verify` the artifact again if it moved between hosts.
2. Start NEXUS against the restored directory (`NEXUS_DATA_DIR=/var/lib/nexus-restored`)
   on a **non-production port** first and confirm `/health` and `/ready`, and
   that the org records are present (`GET /api/v1/identities?business_id=…` with
   a valid credential).
3. To cut over: stop the app, point `storage.data_dir` at the restored
   directory (or move it into place), and start the app. Do **not** run the app
   against the destination during restore.

### 3.6 Recovery from an unsuccessful restore

Because restore is staged and atomic, a failed restore changes nothing: fix the
artifact (or use a good one) and retry into the same empty/missing destination.
The source store and any existing data are never touched by a failed restore.

### 3.7 Retention and access control (operational guidance)

Artifacts contain the durable organization records — treat them as sensitive.
Restrict filesystem access (e.g. `0600`/directory `0700`), store them off-host
or in encrypted storage, and keep a bounded number of generations (e.g. a
daily/weekly rotation). These are recommendations, not features this code
enforces.

---

## 4. Rollback

Two independent things can be rolled back; keep them separate:

- **Application binary rollback** — redeploy the previous `nexus` binary. Safe
  as long as the previous binary can read the current `storage.data_dir`
  format. See the format note below.
- **Data rollback** — restore a pre-change backup into a **new** directory and
  cut over (§3.4–§3.5).

**Schema/data migrations:** the store layout is a single versioned JSON format
(`filestore-json/v1`) and the current code implements **no automatic
migrations**. There is therefore nothing to "reverse"; the backup manifest
records the store layout so an incompatible future change is detectable.

**Before any future incompatible store change, take and verify a backup.**
Restoring an older data backup **after newer writes have occurred discards
those newer writes** — it is not a merge and not harmless. Plan the cutover
window accordingly.

---

## 5. Secret rotation

- **Model provider secret:** update the value of the variable named by
  `api_key_env` (e.g. `OPENAI_API_KEY`) in the environment/secret store and
  **restart the process** — the credential is read at startup. No store change
  is needed and the value is never persisted.
- **Control API key (`NEXUS_CONTROL_API_KEY`):** update the environment value
  and **restart**. There is no live reload; clients using the old key receive
  `401` on `/api/v1/control/*` until they adopt the new one.
- **Bootstrap credential (`NEXUS_BOOTSTRAP_CREDENTIAL`):** it provisions/re-arms
  the first identity's credential on each boot while enforcement is on. To
  rotate it, set the new value and restart; existing stored credential hashes
  are overwritten by the boot-time re-arm. Rotate any downstream users' own
  credentials through the identity endpoints.
- Never place secret values in config files, example files, or shell history;
  use a secret manager or environment injection.

---

## 6. Recovery limitations (explicit)

A durable-store backup restores **only** the JSON record files in §1. It does
**not** restore, and this milestone does **not** implement recovery for:

- running executions or their in-memory results;
- approval/resume snapshots;
- cancellation state;
- escalation state where process-local;
- runtime event buffers and execution traces.

These are process-local by design and are lost on restart. Do not describe them
as recoverable, and do not represent a store backup as a full-system snapshot.

**Not implemented:** distributed coordination, container/Kubernetes deployment,
and metrics **exporters** (v15 adds bounded in-process counters readable over the
control-plane endpoint — see §8 — but no Prometheus/OpenTelemetry export,
scraping endpoint, alerting or dashboard integration).

---

## 7. Durable execution journal (v10)

Contract: `contracts/DURABLE_EXECUTION_RECOVERY_CONTRACTS.md`.

With a durable `storage.data_dir`, execution lifecycle evidence and
consequential dispatch evidence are now persisted as `execution/<id>.json` and
`invocation/<id>.json` records in the same FileStore (and therefore captured by
the backup in §3).

**What is durable:** execution id, business/division scope, initiating actor,
lifecycle state (`accepted` → terminal, or `unknown` after recovery), coarse
error class, and, for a consequential tool call, the dispatch intent and its
outcome status. **What is not:** prompts, model output, tool inputs/args,
credentials, raw error messages, or traces — the journal stores identifiers and
coarse classes only.

**Startup recovery** runs after the store is opened and before the gateway
accepts work:

- durably-terminal executions are left terminal (never re-scheduled);
- an interrupted (nonterminal) execution is classified **`unknown`** and never
  replays its ambiguous effect; the runtime never auto-continues it;
- an invocation that was dispatched without a recorded outcome is classified
  `unknown`;
- a corrupt/unsupported/contradictory record **aborts startup** (fail closed).

**Operator meaning of the states:** `completed` succeeded; `failed` has a coarse
error class; `cancelled` was cancelled; `unknown` means the outcome cannot be
determined (the effect *may* have occurred). An `unknown` outcome is **not**
retried automatically and must be reconciled manually against the external
system using whatever evidence it exposes.

**The HTTP result API is unchanged.** Execution results remain process-local:
`GET /api/v1/executions/{id}` still answers `404` after a restart (the documented
G4 behavior). The journal is recovery evidence, not a result cache; exposing
durable results through the API is deferred. A store backup captures the journal,
but restoring it cannot reconstruct process-local runtime state.

### 7.1 Inspecting durable evidence after restart (v11)

Contract: `contracts/DURABLE_EXECUTION_READ_AND_RECONCILIATION_CONTRACTS.md`.

A read-only, scope-authorized view of the journal is available on a **separate**
route (the process-local result routes are untouched):

```
GET /api/v1/recovery/executions/{id}?business_id=<b>          # one execution
GET /api/v1/recovery/executions?business_id=<b>[&division_id=<d>][&limit=N][&offset=M]
```

- Authorization mirrors the execution-result read: membership of `business_id`,
  business match, and division visibility; unknown/foreign/unauthorized are all
  `404`. No durable journal → `503`; bad `limit`/`offset` → `400`.
- Responses are marked `"source": "durable_journal"` and expose only
  `execution_id`, `business_id`, `division_id`, `state`, `error_class`,
  timestamps, and recovery classification. They never include `actor_id`,
  prompts, model output, tool args/results, credentials, or traces.
- `state: "unknown"` means the outcome could not be determined; it is never a
  success. The list is bounded (≤ 200 per page; ≤ 5000 scanned) and newest-first.
- **No reconciliation endpoint exists**: there is no reliable evidence source or
  authority model to resolve an `unknown`, so a mutation would fabricate an
  outcome. Reconciliation is deferred (contract §7).

### 7.2 Reclaiming journal disk (v14, opt-in)

Contract: `contracts/EXECUTION_JOURNAL_RETENTION_CONTRACTS.md`.

Journal disk grows with the number of executions and consequential tool calls.
**Nothing is pruned automatically** — not at startup, backup or restore. An
operator reclaims space deliberately with the offline `nexus-retention` tool
(the store must be quiesced, exactly like a backup):

```
bin/nexus-retention plan  -data-dir <dir> [-min-age 168h] [-batch 256]   # dry-run report
bin/nexus-retention apply -data-dir <dir>  -min-age 168h  [-batch 256]   # remove eligible records
```

- Only a **terminal** execution (`completed`/`failed`/`cancelled`/`timed_out`)
  whose invocations are **all** known terminal outcomes and that is at least
  `-min-age` old is removed. Nonterminal, `unknown`, too-young and
  ambiguous-invocation records are always retained; `plan` reports the counts.
  Age is measured from the record's last durable transition (`updated_at`).
- `plan` mutates nothing. `apply` requires an explicit `-min-age` and removes
  eligible records in one atomic batch; an interrupted batch is rolled back on
  the next open, so no recovery evidence is lost.
- A backup taken after retention simply contains fewer records; a restore never
  resurrects a committed removal. There is no disk cap that deletes to make room.
- Each run reports its own accounting: the run timestamp, the `min-age`/`batch`
  actually used, records examined, eligible, rejected by reason, physically
  deleted, the outcome, the duration, and whether the output is a dry-run or an
  applied deletion. A **failed** run never prints a deletion count and states
  that the removal is not durably applied (an interrupted batch is rolled back on
  the next open). **Byte reclamation is deliberately not reported** — the store
  exposes no per-record size, so any byte figure would be an estimate; record
  counts are reported instead.
- A failure may echo the store's own diagnostic, which can name a file path or
  record id. That is expected for an offline tool whose operator owns the data
  directory.

---

## 8. Operational metrics and health (v15)

Contract: `contracts/OPERATIONAL_OBSERVABILITY_CONTRACTS.md`.

### 8.1 Reading the metrics

Operational metrics are served on the **existing control-plane** endpoint, which
was extended rather than duplicated:

```
curl -s -H "X-API-Key: $NEXUS_CONTROL_API_KEY" \
     http://127.0.0.1:8080/api/v1/control/metrics
```

- **Authentication is the existing control API key**, compared in constant
  time. It **fails closed**: with no key configured the endpoint returns `403`
  and is never open; a wrong or missing key returns `401`. There is no
  unauthenticated `/metrics`, no new port, and no new identity/RBAC model.
- The route is subject to the full middleware chain (security headers, CORS,
  per-IP rate limit, identity binding, per-actor limit, API key). Only `/health`,
  `/ready` and `/status` are rate-limit exempt — metrics is **not**, so scraping
  consumes the normal per-IP budget.
- The pre-existing `executor`, `backpressure`, `circuit_breaker` and `recovery`
  sections are unchanged; `execution`, `tools` and `journal` were added. The
  change is additive, so existing consumers of this endpoint keep working.
- The response contains **only fixed numeric fields**. It never contains
  execution/request/agent/business ids, tool ids or arguments, prompts, memory,
  tool output, credentials, secrets, error strings, or store/journal paths.
  Cardinality is fixed by construction — there is no per-id or per-label series.

### 8.2 Process-local counters vs durable journal evidence

This distinction is the most important thing to get right when reading the
output.

| Source | Scope | Reset |
| --- | --- | --- |
| `execution.*`, `tools.*`, `journal.write_failures` | **this process only** | reset to `0` on every restart |
| `journal.recovery_*`, `journal.last_recovery_unix_seconds` | **durable**, a snapshot taken by the startup recovery pass | set once per successful pass |

- A counter reading `0` after a restart means "no such event in this process
  since it started" — **not** "no such event ever".
- `journal.recovery_*` counts describe the records that existed **at startup**.
  They do not grow afterwards and they are **not** a count of currently running
  executions, of records added since, or of the current journal size.
- Journal **record counts and disk bytes are not on this endpoint.** An exact
  figure needs a scan, and scanning on every metrics request is rejected by
  design. Use §7.2 (`nexus-retention plan`) for retention candidates, and the
  operator's own filesystem tooling for physical disk usage.

### 8.3 Reading the counters correctly

- `execution.accepted` counts **admitted executions** (new submissions); a
  resume is counted in `approval_resumes`, never as a new submission.
- `execution.in_flight` is admitted-and-not-yet-terminal. An execution **waiting
  for approval is not in flight**.
- `execution.completed + failed + cancelled` equals the number of terminal
  results this process stored. It need **not** equal `accepted`: a run can be
  interrupted, and a paused-then-resumed execution produces two terminal results.
- `cancellations_requested` counts attempts; `cancellations_won` counts only
  cancellations that actually changed the outcome. A cancellation that loses to
  an existing terminal result increments `cancellations_refused`, never `won`.
- `tools.calls` counts **logical** invocations; `tools.attempts` counts
  **physical** attempts; `tools.retries` is the difference. A retry never adds a
  logical call, and an outcome is counted once per logical call, after the
  dispatch completes.
- `tools.*` counts a refusal decided **before** dispatch (unknown tool,
  allowlist, scope, permission, credential, disabled capability) as **nothing**.
  Those are visible in the audit/event stream, not here.

### 8.4 Interpreting `unknown`

`unknown` is the honest "outcome could not be determined" state, and it is never
a success:

- a **tool** call classified `unknown` appears in `tools.unknown` and **never**
  in `tools.completed`;
- an **execution** interrupted before a durable terminal state is classified
  `unknown` by recovery, appears in `journal.recovery_classified_unknown`, and is
  never auto-retried or auto-continued.

A nonzero `unknown` count is a signal to reconcile against the external system by
hand (§7). Do not compute a "success rate" from these numbers without naming the
uncertainty.

### 8.5 What the metrics do not establish

None of these counters proves any of the following, and none may be reported as
if it did:

- **exactly-once execution** — the runtime makes no such guarantee;
- **successful external reconciliation** — no reconciliation exists;
- **durable persistence** — `journal.write_failures == 0` means no write call
  returned an error in this process, not that the store is intact;
- **cluster or cross-instance state** — every counter is process-local; there is
  no aggregation and no node identity.

A metric that is *not implemented* is **absent, not zero**. Never read a missing
series as "none occurred".

### 8.6 Health and readiness

v15 changed no health or readiness behavior. Use the existing surfaces:

- **process alive** → `GET /health` (gateway) or the health server's `/health`;
- **startup validation done, durable store opened and recovered, provider
  configuration validated, accepting new work** → the health server's
  `/readiness` (the launcher marks ready only after the whole startup barrier,
  which includes store recovery);
- **engine accepting work** → `GET /ready` on the gateway (`503` until running);
- **recovery actually happened** → `journal.last_recovery_unix_seconds`
  (nonzero means a recovery pass completed; `0` means none has in this process).

These are separate responses rather than one enumerated readiness document;
adding fields to a public endpoint was deliberately deferred (contract §8).
Liveness never depends on a remote provider call or a journal scan.

### 8.7 When recovery or retention fails closed

**Recovery failed (startup aborted).** The process logs
`durable execution recovery failed` and exits without serving. The metrics show
`journal.recovery_failures > 0` and `last_recovery_unix_seconds = 0` — but
because the process does not start, read them from the failing startup's logs,
not from a live endpoint. Procedure:

1. do **not** "fix" the record by deleting the data directory;
2. inspect the corrupt record file directly (backup §3 first — the store is
   corrupt, so make a copy before touching anything);
3. restore from a known-good backup (§3.4) into a **new** directory and
   cut over (§3.5);
4. if the record is intentionally retained, expect the same fail-closed result;
   the runtime will not serve partial durable state.

**Retention failed.** A failed `nexus-retention` run deletes nothing: it rolls
the batch back inline, or leaves the batch journal for the next store open,
which replays it as a rollback. Nothing is half-deleted. Re-run `plan` to see
the current state; a corrupt record will keep failing closed until restored.

### 8.8 Still unsupported

Do not plan around these — they are not implemented:

- online/live retention (retention is offline and quiescence-required);
- scheduled retention, startup pruning, disk-pressure cleanup triggers;
- multi-instance metrics aggregation or coordination (single process only);
- delegation/coordination counters, governance-evaluation counts, durable
  journal record counts or byte totals, latency percentiles, per-actor/per-tool
  rates;
- hosted monitoring, alerts, dashboards, Prometheus/OpenTelemetry export and CI
  integration of any of it.

An exact per-record byte figure is omitted by design; if you need disk numbers,
use `du` on the data directory while the store is quiesced.