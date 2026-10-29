# NEXUS — TrendSentry Pilot Deployment Readiness & Operator Runbook

**Status:** Operational readiness verification (read-only verification milestone).  
**Baseline:** `origin/master` = `098267d279569e5f7ac6a934b8d78419a777baa8` (includes merged PR #67: auth-hexagonal refactor + multi-business onboarding).  
**Validated against:** real compiled `cmd/nexus` binary over HTTP, deterministic scripted provider (`NEXUS_SEEDED_PROVIDER_MODE=scripted`, `NEXUS_CAPABILITY_WEB=scripted`), isolated temporary data directories, black-box Playwright E2E (`e2e/tests/31-durable-recovery-read.spec.ts`: 4/4 passed), full Playwright suite (267 passed), `go test -count=1 ./...` (all packages), `go test -race` (key packages), `make smoke` (PASS).

This runbook documents the **actual** behavior of the current implementation for operating a TrendSentry pilot. It is **not** a feature specification — nothing here describes behavior that does not exist in code. Where a behavior is not verified, it is labeled "not verified" rather than assumed.

---

## 1. Configuration

### 1.1 `storage.data_dir` is the sole durability setting

The `storage.data_dir` configuration field (`internal/foundation/config/config.go:101-103`) is the **only** switch that enables durable persistence:

| `storage.data_dir` | Store | Registry / memberships | Execution journal | Survives restart? |
|---|---|---|---|---|
| `""` (unset / default) | `nil` (in-memory) | `NewRegistry` — in-memory | `nil` (no-op journal) | **No** — everything lost on restart |
| non-empty path | `FileStore` (disk) | `OpenRegistry` — hydrates on boot, fail-closed | `execjournal.New` — durable records | **Yes** |

**Production requires `storage.data_dir`.** `config.go:887-889` fails startup in production (`config.storage_missing`) when `storage.data_dir` is empty or blank (`config.go:800-801`). Development installs intentionally run in-memory (no durability).

### 1.2 How the path is resolved

`internal/foundation/app/app.go:129-146` (`openStore`):
- If `DataDir == ""`: returns `nil` store + in-memory `NewRegistry`. No durability.
- If `DataDir != ""`: calls `store.NewFileStore(cfg.Storage.DataDir)`, then `identity.OpenRegistry(cfg.Nexus.ID, st)` which hydrates identities, businesses, divisions, credentials and memberships from disk. Any store open failure or corrupt/unsupported record **aborts startup (fail closed)** — NEXUS never serves partial durable state.

The same store is shared by the registry, authenticator, and membership set (`SCHEMA_IDENTITIES_ORG §10`).

### 1.3 Operator requirements

- **Persistent, writable directory.** The directory is created if it does not exist (`os.MkdirAll(dir, 0o755)` in `filestore.go:43-45`). It must be writable by the NEXUS service account and protected from other users (it contains verification hashes and business records).
- **Relative paths:** Not documented as supported. Treat `storage.data_dir` as an absolute path; do not rely on relative resolution.
- **Production gates (fail-closed at startup):** In addition to `storage.data_dir`, production requires at least one real model provider under `models.providers` (the simulated/scripted provider is development-only), `security.audit_enabled`, `security.require_authentication`, `security.enforce_business_scope`, `security.sandbox_enabled` all `true`, and `security.dev_allow_unsafe_overrides` `false` (`config.go:875-901`).
- **Secrets are environment-only.** `NEXUS_CONTROL_API_KEY` and `NEXUS_BOOTSTRAP_CREDENTIAL` are read from the environment at startup and never written to logs, responses, traces, or the store. Provider API keys are named via `api_key_env` and read from the environment at provider construction. **Never commit secret values to the config file.**
- **Do not modify `configs/nexus.dev.json`.** It is protected local state. Create a separate pilot configuration file elsewhere if needed.

---

## 2. Startup and readiness

### 2.1 Safe startup sequence

1. **Provision `storage.data_dir`** on a persistent volume, owned by the NEXUS service account, mode `0755` or more restrictive.
2. **Set environment variables:** `NEXUS_DATA_DIR=<path>`, `NEXUS_BOOTSTRAP_CREDENTIAL=<secret>`, `NEXUS_CONTROL_API_KEY=<secret>`, any provider `api_key_env` variables, `NEXUS_ENVIRONMENT=production` (if production), `NEXUS_HEALTH_HOST/PORT`.
3. **Start NEXUS.** The launcher:
   1. Opens the `FileStore` rooted at `data_dir` and hydrates the registry, authenticator credentials, and membership set. Any corrupt or unsupported record **aborts startup (fail closed)**.
   2. Runs `execjournal.Recover()`: loads and validates every execution record; corrupt/unsupported records abort startup; nonterminal records are classified `unknown` (never retried); terminal records are left terminal.
   3. Seeds the model layer (simulated provider unless `models.providers` is configured).
   4. Starts the core engine and HTTP gateway. The gateway does **not** accept new work until startup recovery completes and the listener is bound (`L-002` startup barrier).
4. **Verify readiness:** `GET /ready` returns `200 {"status":"ready"}` only while the engine is `RUNNING`. `GET /health` returns `200 {"status":"ok"}` whenever the server is running.

> **Health/readiness ≠ durable recovery.** A `200` from `/ready` means the engine is running and the listener is bound. It does **not** prove that durable execution recovery succeeded. Check startup logs for "durable execution recovery classified interrupted work" and the recovery metrics (`GET /api/v1/control/metrics` reports `recovery.failures`, `recovery_executions_scanned/terminal/unknown_invocations`, etc.) to confirm recovery ran cleanly.

### 2.2 Single-process / single data-directory constraint

**Multiple NEXUS instances must not share one data directory.** The store and journal have no cross-process lock; retention is explicitly operator-run and offline against a quiesced store (`EXECUTION_JOURNAL_RETENTION_CONTRACTS §4`). Running two instances against one data directory is unsupported and unsafe.

### 2.3 Fail-closed behavior

- Corrupt or unsupported durable record at startup → **abort startup** (fail closed; never serve partial state).
- Corrupt record on a read request → `503 RESOURCE_UNAVAILABLE` (never reinterpreted).
- Nil journal (no `storage.data_dir`) → recovery read API returns `503 RESOURCE_UNAVAILABLE`.
- Unknown / invalid model provider → startup fails closed in production.

---

## 3. Verify durable execution (operator procedure)

Run this procedure against an **isolated temporary data directory** (not your development or production data dir). All calls are against the real compiled `cmd/nexus` binary over HTTP.

### 3.1 Prerequisites

- Compiled `cmd/nexus` binary (`go build -o bin/nexus ./cmd/nexus` or it already exists).
- A unique, empty temporary data directory.
- `NEXUS_DATA_DIR=<tmp dir>`, `NEXUS_SEEDED_PROVIDER_MODE=scripted`, `NEXUS_CAPABILITY_WEB=scripted`, `NEXUS_BOOTSTRAP_CREDENTIAL=<test secret>`, `NEXUS_CONTROL_API_KEY=<test key>`, `NEXUS_ENVIRONMENT=development`.
- `curl` or an equivalent HTTP client; `jq` for JSON parsing (optional).

### 3.2 Procedure

#### Step 1: Start NEXUS with durable storage

```bash
export NEXUS_DATA_DIR=/tmp/nexus-pilot-data-$(date -u +%Y%m%dT%H%M%SZ)
export NEXUS_SEEDED_PROVIDER_MODE=scripted
export NEXUS_CAPABILITY_WEB=scripted
export NEXUS_BOOTSTRAP_CREDENTIAL=<pilot-bootstrap-secret>
export NEXUS_CONTROL_API_KEY=<pilot-control-key>
export NEXUS_ENVIRONMENT=development

./bin/nexus &   # or systemd / your process manager
NEXUS_PID=$!

# Wait for readiness
for i in $(seq 1 150); do
  if curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:8081/ready | grep -q 200; then break; fi
  sleep 0.2
done
```

> Default gateway port is `health_port + 1`; with health on `8080`, gateway is `8081`. Override with `--http-addr` if needed.

#### Step 2: Authenticate and create a pilot business

```bash
BASE=http://127.0.0.1:8081
BIZ=biz-pilot-$(date +%s)
CRED=<bootstrap-credential>
AUTH="-H 'X-Actor-ID: nx:human:bootstrap' -H 'X-Actor-Credential: $CRED'"

# Create business (membership-free; owner not enrolled yet)
curl -s -X POST "$BASE/api/v1/businesses" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"entity_id\":\"$BIZ\",\"name\":\"TrendSentry Pilot\",\"owner_identity_id\":\"nx:human:bootstrap\"}"

# Establish owner membership (public onboarding endpoint)
curl -s -X POST "$BASE/api/v1/businesses/$BIZ/members" \
  -H "$AUTH" -H 'Content-Type: application/json' -d '{}'
# → 200 {business_id, identity_id, role: OWNER, status: active}

# Non-owner cannot claim membership (use a different authenticated identity)
# → 403 AUTHORIZATION
```

#### Step 3: Create a division and register an agent

```bash
DIV=div-pilot-$(date +%s)
AGENT=agent-pilot-$(date +%s)

curl -s -X POST "$BASE/api/v1/divisions" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"entity_id\":\"$DIV\",\"business_id\":\"$BIZ\",\"name\":\"TrendSentry Division\",\"owner_identity_id\":\"nx:human:bootstrap\",\"parent_business_id\":\"$BIZ\"}"

curl -s -X POST "$BASE/api/v1/agents" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"entity_id\":\"$AGENT\",\"name\":\"Pilot Agent\",\"business_id\":\"$BIZ\",\"division_id\":\"$DIV\",\"capabilities\":[\"analysis\"],\"allowed_tools\":[\"web.search\"]}"
```

#### Step 4: Submit a scripted research objective

```bash
curl -s -X POST "$BASE/api/v1/intelligence/execute" \
  -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"business_id\":\"$BIZ\",\"actor_id\":\"nx:human:bootstrap\",\"division_id\":\"$DIV\",\"objective\":{\"description\":\"research thoroughly: search the web for nexus capabilities\",\"context\":{\"agent_id\":\"$AGENT\"}}}"

# → 202 {execution_id, correlation_id, status:"accepted", actor_id}
EXEC_ID=<from response>
```

#### Step 5: Poll to terminal via the live request-result API

```bash
for i in $(seq 1 600); do
  CODE=$(curl -s -o /tmp/resp.json -w "%{http_code}" \
    "$BASE/api/v1/requests/$EXEC_ID?business_id=$BIZ" -H "$AUTH")
  if [ "$CODE" = "200" ]; then
    STATUS=$(jq -r '.status' /tmp/resp.json)
    if [ "$STATUS" != "pending" ]; then break; fi
  fi
  sleep 0.1
done
cat /tmp/resp.json
# → {request_id, business_id, division_id, status:"completed", outcome:{...}, ...}
# Terminal state is process-local; it will NOT survive restart.
```

#### Step 6: Read the durable record via the recovery API

```bash
curl -s "$BASE/api/v1/recovery/executions/$EXEC_ID?business_id=$BIZ" -H "$AUTH"
# → 200 {source:"durable_journal", execution_id, business_id, division_id,
#     state:"completed", error_class, created_at, updated_at,
#     research_evidence:{...}, research_synthesis:{...complete...},
#     research_evaluation:{...}, recovery:null, approvals:[...]}
```

Verify:
- `source` == `"durable_journal"`
- `state` == `"completed"`
- `research_evidence` present; `research_synthesis.status` == `"complete"`; `evidence_backed` non-empty with `source_ids`
- `actor_id` **absent** (identity references excluded per contract v11 §3)
- No `sk-`, `credential`, or `secret` substrings in the body

#### Step 7: Restart NEXUS (same data directory)

```bash
kill $NEXUS_PID
wait $NEXUS_PID
./bin/nexus &   # same NEXUS_DATA_DIR
# Wait for /ready again
```

#### Step 8: Read the same durable record again

```bash
curl -s "$BASE/api/v1/recovery/executions/$EXEC_ID?business_id=$BIZ" -H "$AUTH"
# → 200 {source:"durable_journal", state:"completed", ... evidence+synthesis intact}
# recovery.recovery is undefined — terminal records are never rewritten by recovery
```

#### Step 9: Confirm the live result API is process-local

```bash
curl -s -o /dev/null -w "%{http_code}" \
  "$BASE/api/v1/requests/$EXEC_ID?business_id=$BIZ" -H "$AUTH"
# → 404 (execution results are process-local; lost on restart)
```

#### Step 10: Verify authorization boundaries

- **Unauthenticated** `GET /api/v1/recovery/executions/$EXEC_ID?business_id=$BIZ` → `401 UNAUTHORIZED`
- **Foreign business identity** (authenticated but not a member of `$BIZ`) → `403 AUTHORIZATION` (membership checked before record lookup)
- **Division-scoped identity** reading another division's record → `404` (G5: indistinguishable from missing)

---

## 4. Recovery and execution semantics

### 4.1 What survives restart

| Category | Survives restart? | Durable? | Notes |
|---|---|---|---|
| Identities / businesses / divisions | Yes (if `storage.data_dir`) | Yes | FileStore-hydrated on boot |
| Credentials (verification hash only) | Yes | Yes | Raw secrets never stored |
| Memberships | Yes | Yes | `MembershipSet` write-through, fail-closed hydration |
| Execution lifecycle records (`accepted`/`terminal`) | Yes | Yes | `execution/<id>.json`, one atomic write per transition |
| Terminal execution outcomes | Yes | Yes | `Terminal`/`TerminalWithResearch`; first-wins, immutable |
| Invocation records (dispatch intent/outcome) | Yes | Yes | `invocation/<id>.json`; `dispatched` → `unknown` on recovery |
| Research evidence / synthesis / evaluation | Yes | Yes | Atomic with terminal write; bound to evidence fingerprint |
| Approval history (requested/granted/denied) | Yes | Yes | Append-only on the execution record |
| Live execution results (`GET /api/v1/requests/{id}`) | **No** | Process-local | `404` after restart |
| Approval / resume snapshots | **No** | Process-local | Lost on restart; fresh governance admission required |
| Event bus / traces | **No** | Process-local | In-memory MemBus |
| Circuit breaker / backpressure / recovery manager state | **No** | Process-local | Hardening metrics reset on restart |

### 4.1.1 What `unknown` means

Recovery classifies any nonterminal (interrupted) execution record as `state: unknown` with recovery metadata (`recovered: true`, `prior_state`, `recovered_at`, `reason`). `unknown` is **never** blindly retried or auto-resumed. It is an honest "we cannot know whether the in-flight effect occurred." Operators must treat it as uncertain and make a fresh governance decision before re-submitting.

### 4.1.2 Approval pauses across restart

An execution parked on an approval gate is journaled as terminal `failed` with `error_class: APPROVAL_REQUIRED`. After restart:
- The durable record remains `failed`/`APPROVAL_REQUIRED` (terminal).
- The **approval snapshot is process-local and is gone** after restart.
- The execution is **not resumable** via the paused path; a fresh governance admission is required to submit new work.

---

## 5. Backup, restore, and retention

### 5.1 Backup (`nexus-backup`)

Build: `go build -o bin/nexus-backup ./cmd/nexus-backup` (or `make build-backup`).

**Quiesce first.** The application must be stopped before backup. `nexus-backup backup` never opens or mutates the live source store; it requires the application to be stopped and never opens a live store.

```bash
# 1. Stop NEXUS
kill $NEXUS_PID; wait $NEXUS_PID

# 2. Backup
nexus-backup backup -data-dir /var/lib/nexus -out /backups/nexus-$(date -u +%Y%m%dT%H%M%SZ).tar
# → backup: N file(s) → … (format v1, created …)
```

The artifact is a single tar whose first entry is the manifest (`nexus-backup-manifest.json`) followed by store files at store-relative paths. The manifest records format/version, creation time, store layout, scope, exclusions, and each file's path, size, and SHA-256. The tool refuses to overwrite an existing artifact, refuses a symlinked store root, and never includes `workspaces/`, temp files, `.env` files, or secrets.

### 5.2 Verify a backup (read-only)

```bash
nexus-backup verify -in /backups/nexus-….tar
# → verify: OK — N file(s), format v1, created …
```

### 5.3 Restore

Restore extracts the tar into an isolated data directory and reopens the store through its real recovery path. Restored state includes identities, businesses, divisions, credentials (hashes), memberships, agent definitions, durable memory, and the execution journal. **Restored state does not reconstruct:** running executions, in-memory results, approval/resume snapshots, cancellation state, escalation state, event buffers, or execution traces. Restarting from a restored store begins with empty in-flight state.

### 5.4 Retention

Retention is **operator-run and offline** (`EXECUTION_JOURNAL_RETENTION_CONTRACTS §6`):
- Disabled by default; no background scheduler, no startup pruning, no automatic pruning in backup or restore.
- Runs only via explicit operator invocation with `Enabled` and `MinAge` set.
- Removes only terminal executions old enough (`UpdatedAt >= MinAge`) whose every invocation has a terminal-known status (`completed`/`failed`). `unknown` and nonterminal records are always retained.
- Uses `DeleteBatchHard` (physical removal) atomically via the same batch journal; interrupted batches roll back on the next open.
- **Never run against a live store.** Requires the store to be quiesced (no other process using the data directory). Running retention against a live store is unsupported.

### 5.5 What must be backed up

The entire `storage.data_dir` tree **while the process is stopped**:
```
<data_dir>/
  identity/<id>.json
  business/<id>.json
  division/<id>.json
  credential/<id>.json
  membership/<id>.json
  agent/<id>.json
  memory/<id>.json
  execution/<id>.json
  invocation/<id>.json
  .batch-journal.json   (present only mid-batch / after a crash)
```

Do not back up `workspaces/` (filesystem sandbox — not part of the store), temp files, `.env` files, or secrets.

---

## 6. Pilot go/no-go checklist

| # | Check | Verified? |
|---|-------|-----------|
| 1 | `storage.data_dir` set to a persistent, writable, service-owned directory | ☐ |
| 2 | Filesystem permissions: service account can write; other users cannot read | ☐ |
| 3 | Available disk space sufficient for expected journal volume | ☐ |
| 4 | Backup owner identified; restore procedure tested on an isolated copy | ☐ |
| 5 | Correct business and division created; owner membership established | ☐ |
| 6 | Provisioned identities and membership scope correct | ☐ |
| 7 | Provider configuration and web-search capability configured (or scripted for pilot validation) | ☐ |
| 8 | Governance policies and approval ownership defined | ☐ |
| 9 | Restart/read acceptance test passed (business + membership + durable record survive restart) | ☐ |
| 10 | Operator knows response for `unknown` executions: fresh governance admission, never blind retry | ☐ |
| 11 | Acknowledgement: multi-instance coordination and approval-resume-across-restart are **unsupported** | ☐ |

### Scripted vs live provider

The scripted provider (`NEXUS_SEEDED_PROVIDER_MODE=scripted`, `NEXUS_CAPABILITY_WEB=scripted`) is **deterministic simulation for validation only**. A scripted-provider pass does **not** prove live provider connectivity or live research quality. For live TrendSentry operation, configure real `models.providers` with `api_key_env` pointing to environment-held credentials, and a real web-search provider (`wikipedia` or `brave` with `NEXUS_CAPABILITY_WEB` and the corresponding key env var). Production refuses to start without at least one real model provider (`config.storage_missing` / `config.provider_missing` gates).

---

## 7. Remaining pilot prerequisites

| Prerequisite | Status | Owner input needed? |
|---|---|---|
| `storage.data_dir` configured | Not set in `configs/nexus.dev.json` (empty = in-memory) | **Yes** — deployment decision |
| TrendSentry division names / business owner identity | Not documented anywhere in repo | **Yes** |
| Model provider credentials (`api_key_env`) | Not configured | **Yes** |
| Web-search credentials | Not configured | **Yes** |
| Approval/resume-across-restart | Not supported (process-local by design) | **Yes** — product decision if required |
| Multi-instance coordination | Not supported | **Yes** — if HA is required |

**Go/no-go:** The pilot is **not** yet ready for live operation until `storage.data_dir` is configured, TrendSentry division/owner inputs are provided, and (for live research) model provider and web-search credentials are configured. The durable read API and restart persistence are verified working; the remaining gaps are configuration and business-taxonomy inputs, not engineering defects.

---

## 8. Documentation inaccuracies and unresolved questions

| Item | Status |
|---|---|
| `storage.data_dir` default empty = in-memory | Verified in `app.go:130-131` |
| Production requires `storage.data_dir` | Verified in `config.go:887-889` |
| Recovery classifies nonterminal → `unknown` | Verified in `journal.go:650-714` |
| Terminal records immutable | Verified in `journal.go:307` |
| Approval/resume not durable across restart | Verified in `APPROVAL_LIFECYCLE_CONTRACTS §5` and `approval.go:18-19` |
| Live results 404 after restart | Verified in E2E `08-restart.spec.ts` and `31-durable-recovery-read.spec.ts` |
| Multi-instance coordination | Not supported; documented in `EXECUTION_JOURNAL_RETENTION_CONTRACTS §4` |
| Resume-across-restart support | **Not provided** — product-owner decision required |
| Live provider credentials | Not configured; scripted provider used for validation only |

---

## 9. Summary

NEXUS has a **durable execution journal** (v10) and a **durable read API** (v11) that survive restart when `storage.data_dir` is configured. Terminal execution outcomes, evidence, synthesis, citations, and approval history are durable; live execution results, approval/resume snapshots, and traces are process-local and lost on restart. Recovery classifies interrupted executions as `unknown` and never replays ambiguous effects. Approval/resume continuity across restart is intentionally not provided.

The pilot is **operationally ready only after** `storage.data_dir` is configured, TrendSentry business/division/owner inputs are supplied, and (for live research) model provider and web-search credentials are provisioned. Until then, the system operates in development (in-memory) mode and is not suitable for live TrendSentry workloads.

---
*This runbook documents verified behavior only. No implementation changes were made in this milestone. All statements are traceable to source code and contracts under `/home/pc/NEXUS`.*
