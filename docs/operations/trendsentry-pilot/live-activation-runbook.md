# NEXUS — TrendSentry Live Activation Runbook

**Status:** `NOT EXECUTED` — this runbook is a plan. No gate below has been run against a live model provider, a live web-search provider, production data, or a production host.
**Prepared:** 2026-10-26
**Validated source baseline:** `origin/master` = `098267d279569e5f7ac6a934b8d78419a777baa8`.
**Companion documents:** [Owner handoff](owner-handoff.md) · [Activation input register](activation-inputs.md) · [Existing scripted pilot runbook](../TRENDSENTRY_PILOT_RUNBOOK.md)

> **Authority.** Gate 0 is the authorization gate. Until Gate 0 is signed, do not start a production instance, contact a live provider, use production data, or execute any gate below. A scripted test pass is not a live-readiness result and never authorizes a live call.

---

## 0. How to use this runbook

### 0.1 Gate discipline

1. Gates run **in order**, `0 → 8`. Do not start a later gate while an earlier gate is `FAIL`, `BLOCKED`, or incomplete.
2. A gate passes only when **every required check** passes. Partial results are not a pass.
3. Any `FAIL` stops the activation. Preserve evidence, record the failure, and return to Gate 0 for a new decision.
4. An `N/A` result is permitted only where this runbook explicitly allows it, and requires a written rationale accepted by the accountable role.
5. Re-running a gate after a change is mandatory. Record both runs; do not overwrite the failed record.
6. Do not change the configuration, the artifact, the data directory, or the policy set during a gate. Any change restarts Gate 2 (and Gate 1 if source changed).

### 0.2 Gate result vocabulary

| Result | Meaning |
|---|---|
| `NOT EXECUTED` | The default. No evidence exists. |
| `PASS` | Every required check passed and evidence is recorded. |
| `FAIL` | At least one required check failed. Stop. |
| `BLOCKED` | A prerequisite is unavailable, so the gate cannot be evaluated. Stop. |
| `N/A` | Explicitly inapplicable, with an accountable-role rationale. |

### 0.3 Evidence standard

Every gate record must include:

- gate ID and result;
- UTC start and end timestamps;
- executing operator and accountable reviewer;
- exact command or action, with secret values redacted;
- observed output (not a paraphrase), stored as a file or immutable ticket attachment;
- SHA-256 of every captured evidence file;
- the source/config/binary identity used (commit, binary checksum, config hash);
- any deviation from this runbook and its approval.

Evidence files are retained for the pilot life plus the owner's retention period. **Never record a credential value, an `Authorization` header, a cookie, or a raw secret in evidence.** Request bodies may contain business-sensitive objective text and source snippets: apply the data-classification decision from Gate 0 before attaching them.

### 0.4 Gate status ledger

All gates are `NOT EXECUTED`. This table is the authoritative status record for the handoff.

| Gate | Purpose | Accountable role | Status | Date | Evidence reference |
|---|---|---|---|---|---|
| **0** | Authorization and scope freeze | Pilot Owner | `NOT EXECUTED` | — | — |
| **1** | Artifact and build integrity | Engineering Lead | `NOT EXECUTED` | — | — |
| **2** | Configuration, secrets, and environment readiness | Platform Operator | `NOT EXECUTED` | — | — |
| **3** | Boot, health, and fail-closed security verification | Platform Operator + Security Reviewer | `NOT EXECUTED` | — | — |
| **4** | Onboarding, identity, membership, and agent provisioning | Platform Operator | `NOT EXECUTED` | — | — |
| **5** | Governance policy application and verification | Security Reviewer | `NOT EXECUTED` | — | — |
| **6** | First live provider workflow | Platform Operator + Research Reviewer | `NOT EXECUTED` | — | — |
| **7** | Durability, recovery, backup, and restore | Platform Operator + Engineering Lead | `NOT EXECUTED` | — | — |
| **8** | Acceptance, operational handover, and final GO/NO-GO | Pilot Owner | `NOT EXECUTED` | — | — |

### 0.5 Roles

| Role | Gate accountability |
|---|---|
| **Pilot Owner** | Gate 0 authorization; Gate 8 final decision; accepts warnings and residual risk. |
| **Platform Operator** | Executes Gates 1–7; owns host, config, secrets, data directory, backup/restore, evidence capture. |
| **Security Reviewer** | Verifies Gates 2, 3, and 5; holds a veto at Gate 8. |
| **Research Reviewer** | Observes Gate 6; performs semantic source review at Gate 8. |
| **Engineering Lead** | Owns Gate 1 artifact integrity; verifies Gate 7 recovery evidence; escalates defects. |

Roles may be combined in a small pilot, but the combination must be recorded in Gate 0, and no one may approve their own action where self-approval is prohibited.

### 0.6 Shell setup

The commands below assume `bash`, `curl`, `jq`, and the binaries built in Gate 1. Never type a secret literally. Read it from the operator's secret mechanism into the environment without printing it.

```bash
# Adjust paths to the pilot host. Never echo a secret value.
# NEXUS_HOME is the installed release tree produced by Gate 1: its bin/nexus
# and bin/nexus-backup must match the Gate 1 checksums. Do not point it at a
# developer workspace.
export NEXUS_HOME="/opt/nexus"
export CONFIG="$NEXUS_HOME/config/nexus.production.json"
export BASE="http://127.0.0.1:8081"
export BIZ="<RF-04 business entity id>"
export OWNER="<RF-05 owner identity id>"
export AGENT="<RF-07 agent entity id>"
export DIVISION="<OD-01 division id, or empty>"
export DATA_DIR="<RF-08 absolute data_dir>"
export BACKUP_DIR="<RF-15 backup destination>"
export EVIDENCE_DIR="<immutable evidence directory>"

# Secret material comes from the process supervisor / secret manager.
# These files are examples; substitute the pilot's actual mechanism.
export CONTROL_KEY="$(cat /run/secrets/nexus-control-api-key)"
export BOOTSTRAP_CREDENTIAL="$(cat /run/secrets/nexus-bootstrap-credential)"
export OWNER_CREDENTIAL="$(cat /run/secrets/nexus-owner-credential)"

# The approver is a separate authorized business-wide member when
# self-approval is prohibited. If the owner may self-approve, set
# APPROVER="$OWNER" and APPROVER_CREDENTIAL="$OWNER_CREDENTIAL".
export APPROVER="<RF-12 approver identity id>"
export APPROVER_CREDENTIAL="$(cat /run/secrets/nexus-approver-credential)"
```

Every curl call that touches a scoped route uses the **owner's** actor headers. If the owner is the bootstrap identity, set `OWNER_CREDENTIAL="$BOOTSTRAP_CREDENTIAL"`.

```bash
OWNER_HEADERS=(
  -H "X-Actor-ID: $OWNER"
  -H "X-Actor-Credential: $OWNER_CREDENTIAL"
)
```

The bootstrap identity uses:

```bash
BOOTSTRAP_HEADERS=(
  -H "X-Actor-ID: nx:human:bootstrap"
  -H "X-Actor-Credential: $BOOTSTRAP_CREDENTIAL"
)
```

The approver uses:

```bash
APPROVER_HEADERS=(
  -H "X-Actor-ID: $APPROVER"
  -H "X-Actor-Credential: $APPROVER_CREDENTIAL"
)
```

Control routes use `-H "X-API-Key: $CONTROL_KEY"`. A missing or wrong control key returns `401` when a key is configured. If no key is configured, control is disabled with `403 CONTROL_DISABLED`; that is a Gate 2 failure for a live pilot.

---

## Gate 0 — Authorization and scope freeze

**Accountable:** Pilot Owner (authorizes) · Platform Operator (records)
**Prerequisites:** Completed [activation input register](activation-inputs.md); a named change window; the owner has read [owner-handoff.md](owner-handoff.md) §6.
**Status:** `NOT EXECUTED`

### G0 checks

| ID | Exact check | PASS criterion | Evidence |
|---|---|---|---|
| `G0-01` | Written authorization to execute this runbook against the named host, environment, and data classification. | Authorization is signed and references this runbook, the baseline commit, and the pilot host. | Signed authorization or ticket reference. |
| `G0-02` | Roles and deputies are named. | Pilot Owner, Platform Operator, Security Reviewer, Research Reviewer, and Engineering Lead each have a named person and a deputy. | Roster with contacts. |
| `G0-03` | Change window is fixed. | Start and end are recorded with timezone and blackout constraints. | Change record. |
| `G0-04` | First live objective and human acceptance criteria are approved. | Objective is a real, bounded question of ≤ 4,000 characters; Research Reviewer accepts the criteria. | `RF-14`, `RF-17` approved. |
| `G0-05` | Data classification is fixed. | The first run uses only data approved for the selected provider and web search; no unapproved production data. | Classification record. |
| `G0-06` | Known boundaries are explicitly accepted. | Owner accepts: process-local result routes, no approval resume across restart, policies do not survive restart, no monetary cap, egress allow list not wired, single-process only. | Signed acceptance of [owner-handoff.md](owner-handoff.md) §6. |
| `G0-07` | Provider spend control exists. | Hard spend limit and alert threshold are configured in the provider account; spend owner is named. | Provider-console evidence reference. |
| `G0-08` | Approval design is decided. | `RF-12` records whether `web.search` needs approval, who approves, timeout, auto-deny, self-approval, and delegation. | `RF-12` decision. |
| `G0-09` | Stop and rollback authority is named. | At least two people can stop the pilot; the rollback procedure and preservation rules are accepted. | `RF-18` decision. |
| `G0-10` | No secret value has been recorded in the register, this runbook, chat, tickets, or commits. | Secret scan of the handoff package is clean. | Scan output. |

### Stop conditions

- Any required input is still `UNRESOLVED`.
- The owner has not accepted the operating boundaries in writing.
- The first objective or its acceptance criteria are not approved.
- No hard provider spend limit exists.
- No named stop authority exists.

**Gate 0 result:** `NOT EXECUTED`. A Gate 0 `GO` authorizes Gates 1–8 only; it does not authorize production cutover. Gate 8 is the production decision.

---

## Gate 1 — Artifact and build integrity

**Accountable:** Engineering Lead · Platform Operator witnesses
**Prerequisites:** Gate 0 `PASS`; access to the repository object store; Go toolchain matching the validated build; a build host that is not the production data host if the owner requires separation.
**Status:** `NOT EXECUTED`

### Why an archive build

Building from the working tree would compile untracked files and could silently differ from the validated commit. Build from an exported tree of the validated commit. `git archive` does not create a worktree and does not alter the repository.

### G1 checks

| ID | Exact check | PASS criterion | Evidence |
|---|---|---|---|
| `G1-01` | Resolve the build commit. | `BASELINE=098267d279569e5f7ac6a934b8d78419a777baa8`; `git cat-file -e "$BASELINE^{commit}"` exits 0. | Commit ID and `git show -s --format='%H %cI %s' "$BASELINE"`. |
| `G1-02` | Confirm the chosen build commit contains the validated baseline. If a later commit is used, a new validation record is required. | `git merge-base --is-ancestor "$BASELINE" "$BUILD_COMMIT"` exits 0. For this handoff, `BUILD_COMMIT="$BASELINE"`. | Command output, exit code. |
| `G1-03` | Export a clean source tree. | `git archive --format=tar "$BUILD_COMMIT" \| tar -x -C "$RELEASE_DIR"` exits 0; `$RELEASE_DIR` was empty before extraction. | Extraction log and file count. |
| `G1-04` | Confirm the exported tree is exactly the commit. | No untracked or modified source files exist in `$RELEASE_DIR`; record `find "$RELEASE_DIR" -type f \| wc -l`. | File inventory hash. |
| `G1-05` | Record the toolchain. | `go version`, `go env GOOS GOARCH CGO_ENABLED` recorded; the toolchain is the one used for the validation run or a documented equivalent. | Toolchain output. |
| `G1-06` | Compile all packages. | `go build ./...` exits 0. | Command output, exit code. |
| `G1-07` | Static analysis. | `go vet ./...` exits 0. | Command output, exit code. |
| `G1-08` | Unit/integration tests. | `go test -count=1 ./...` exits 0. | Full output or per-package summary. |
| `G1-09` | Build the release binaries. | `make build VERSION="$VERSION" COMMIT="$BUILD_COMMIT"` and `make build-backup` exit 0; `bin/nexus` and `bin/nexus-backup` exist. | Build log. |
| `G1-10` | Record binary identity. | `./bin/nexus --version` and `./bin/nexus-backup -version` report the intended version and commit; SHA-256 of both binaries is recorded. Install the verified tree at `$NEXUS_HOME` (or set `NEXUS_HOME="$RELEASE_DIR"`); the binaries used in Gates 2–8 must match these checksums. | Version output and checksums. |
| `G1-11` | Confirm the release artifact does not rely on workspace-only files. | The build uses only files from the exported commit. Any required test or fixture that exists only in the working tree is recorded as an evidence gap. | Gap record. |
| `G1-12` | Record the evidence-reproducibility gap for the two workflow specs. | At the validated baseline, `e2e/tests/30-trendsentry-research-workflow.spec.ts` and `e2e/tests/31-durable-recovery-read.spec.ts` are **not tracked** in `origin/master`. The scripted validation evidence is from the workspace, not from the release commit. Record whether the owner accepts this or requires a separately authorized change to commit the specs before Gate 6. | `git ls-tree` output and Gate 8 risk acceptance if accepted. |

### Scripted tests are not live readiness

`go test` and the Playwright suite prove platform mechanics under deterministic providers. They do **not** prove provider connectivity, model structured-output compliance, live search quality, quota, latency, or production host security. Gate 1 cannot be used to skip Gates 2–7.

### Stop conditions

- Commit mismatch, non-zero build/test/vet exit, or missing binary.
- Untracked source enters the build.
- The validation evidence cannot be tied to the built commit and the owner has not accepted the gap.

---

## Gate 2 — Configuration, secrets, and environment readiness

**Accountable:** Platform Operator · Security Reviewer verifies secret handling
**Prerequisites:** Gate 1 `PASS`; a production config created from [activation-inputs.md](activation-inputs.md) §14; the data directory and service account exist; the process supervisor can inject secrets; `jq` and `curl` are available.
**Status:** `NOT EXECUTED`

### Configuration rules

- Create a **new** production config. Do not use `docs/operations/trendsentry-pilot/config.example.json`: it is a scripted-development template, not a live-activation config.
- The config schema rejects unknown fields. A typo is a boot failure, not a silent default.
- Secret values are **environment-only**. The config carries `api_key_env` (a name), never the key.
- `NEXUS_ENVIRONMENT` and `NEXUS_DATA_DIR` override the file. Set them consistently or leave the overrides unset.
- `NEXUS_CAPABILITY_WEB` is a process variable, not a config-file field.

### G2 checks

| ID | Exact check | PASS criterion | Evidence |
|---|---|---|---|
| `G2-01` | Strict JSON. | `jq empty "$CONFIG"` exits 0. | Command output. |
| `G2-02` | No placeholders remain. | `grep -nE '<[A-Z][A-Z0-9_]*>' "$CONFIG"` produces no output. | Grep output (empty). |
| `G2-03` | Installation identity. | `jq -r '.nexus.id' "$CONFIG"` is non-empty, stable, and not another installation's ID. | Value and owner reference. |
| `G2-04` | Production environment. | `jq -r '.nexus.environment' "$CONFIG"` is `production` **and** `printenv NEXUS_ENVIRONMENT` is `production`. The capability layer reads the process variable directly; leaving it unset is not acceptable for live activation. | Both values. |
| `G2-05` | Durable storage path. | `jq -r '.storage.data_dir' "$CONFIG"` is an absolute, persistent, service-owned path; it is not the repository, `workspaces/`, a container layer, or a shared directory. | Path, mount, ownership, mode. |
| `G2-06` | Data-directory readiness. | Directory exists (or its parent is writable by the service account); `stat -c '%U:%G %a %n' "$DATA_DIR"` recorded; no second process uses it. | `stat`, `pgrep -af 'bin/nexus'`. |
| `G2-07` | Model provider present. | Exactly the approved provider entry exists; `id`, `endpoint`, `model_id`, and `api_key_env` are non-empty. | `jq '.models.providers'`. |
| `G2-08` | Endpoint security. | `endpoint` is `https://` or an explicitly Security-approved loopback address with `allow_insecure_http: true`. No plain HTTP to a remote host. | Endpoint value and approval reference. |
| `G2-09` | Timeout shape. | If `timeout_ms` is present, it is an integer (milliseconds), not a string such as `"30s"`; omit it to use the 30-second adapter default. | Config fragment. |
| `G2-10` | Capability declaration. | `capabilities` is omitted (defaults to reasoning, tool_calling, structured_output) or explicitly includes those three. | Config fragment. |
| `G2-11` | Production security flags. | `audit_enabled=true`, `require_authentication=true`, `enforce_business_scope=true`, `sandbox_enabled=true`, `dev_allow_unsafe_overrides=false`. | `jq '.security'`. |
| `G2-12` | No raw secret fields. | `test "$(jq '[.. \| objects \| has("api_key") or has("password") or has("token") or has("secret")] \| any' "$CONFIG")" = "false"` exits 0. `api_key_env` is the only credential reference. | Command output. |
| `G2-13` | Control key present. | `test -n "${CONTROL_KEY:-}"` exits 0; `printenv NEXUS_CONTROL_API_KEY >/dev/null` exits 0. Never print the value. | Exit codes. |
| `G2-14` | Bootstrap credential present. | `test -n "${BOOTSTRAP_CREDENTIAL:-}"` exits 0; `printenv NEXUS_BOOTSTRAP_CREDENTIAL >/dev/null` exits 0. | Exit codes. |
| `G2-15` | Model key present under the configured name. | `KEY_ENV=$(jq -r '.models.providers[0].api_key_env' "$CONFIG")`; `test -n "${!KEY_ENV:-}"` exits 0. | Exit code, variable name only. |
| `G2-16` | Search credential present. | If `NEXUS_CAPABILITY_WEB=brave`: `test -n "${BRAVE_API_KEY:-}"` exits 0. `wikipedia` needs no key. | Exit code. |
| `G2-17` | Live search selected. | `printenv NEXUS_CAPABILITY_WEB` is `wikipedia` or `brave`; it is **not** `scripted`, empty, or unknown. | Value. |
| `G2-18` | No scripted model seed. | `NEXUS_SEEDED_PROVIDER_MODE` is unset or not `scripted`. | Value. |
| `G2-19` | Data-directory override is consistent. | `NEXUS_DATA_DIR` is unset or identical to `storage.data_dir`. | Values. |
| `G2-20` | File permissions. | Config file mode is `0600` or stricter; data directory mode is `0750` or stricter; secrets are not world-readable. | `stat`. |
| `G2-21` | Fail-closed configuration negative tests (on copies, never the production file). Run with `NEXUS_ENVIRONMENT=production` and with `NEXUS_DATA_DIR` unset so the file value is authoritative. | Blank `storage.data_dir` → boot exits non-zero with `production requires storage.data_dir`; remove providers → exits non-zero with `production requires at least one real model provider`; flip a security flag → exits non-zero with the corresponding `must be ... in production` message. | Three exit codes and stderr excerpts. |
| `G2-22` | Preflight boot. | Start with `--http-addr 127.0.0.1:<loopback port>` and loopback health; `GET /ready` returns `200`; the log contains `production model provider registered`; no `no model registered: seeded simulated default model` line; stop the process. No objective is submitted. | Log excerpt, readiness output. |
| `G2-23` | Evidence-grade logging. | `logging.level` is `info` (or a level that emits provider registration) and `logging.format` is `json`, so Gate 3 can search the registration line and Gate 6 can correlate requests. If a different level is chosen, record an equivalent provider-registration evidence path. | Config fragment and rationale. |

### Exact preflight sequence

```bash
# 1. Validate JSON and placeholders.
jq empty "$CONFIG"
! grep -qE '<[A-Z][A-Z0-9_]*>' "$CONFIG"

# 2. Show only the non-secret shape.
jq '{nexus, storage, security, providers: [.models.providers[] | {id, endpoint, model_id, api_key_env, timeout_ms}]}' "$CONFIG"

# 3. Prove secret presence without printing values.
test -n "${CONTROL_KEY:-}"
test -n "${BOOTSTRAP_CREDENTIAL:-}"
KEY_ENV=$(jq -r '.models.providers[0].api_key_env' "$CONFIG")
test -n "${!KEY_ENV:-}"

# 4. Prove the live environment selection.
test "$(printenv NEXUS_ENVIRONMENT)" = "production"
test "${NEXUS_CAPABILITY_WEB:-}" != "scripted"
test -n "${NEXUS_CAPABILITY_WEB:-}"
test "${NEXUS_SEEDED_PROVIDER_MODE:-}" != "scripted"

# 5. Preflight boot (loopback only), then readiness and stop.
NEXUS_HEALTH_PORT=18080 "$NEXUS_HOME/bin/nexus" --config "$CONFIG" \
  --http-addr 127.0.0.1:18081 >"$EVIDENCE_DIR/gate2-preflight.log" 2>&1 &
PREFLIGHT_PID=$!
for i in $(seq 1 100); do
  curl -fsS "http://127.0.0.1:18081/ready" && break
  sleep 0.1
done
curl -fsS "http://127.0.0.1:18081/health"
kill -TERM "$PREFLIGHT_PID"
wait "$PREFLIGHT_PID"
```

The preflight **constructs** the provider and validates credentials, but submits no objective. It must not be treated as a live provider call.

### Stop conditions

- Any placeholder, unknown field, or raw secret in the config.
- `NEXUS_CAPABILITY_WEB=scripted` or an empty/unknown value.
- Missing model/search/control/bootstrap secret.
- A negative configuration test starts and binds a listener instead of failing closed.
- The preflight logs a seeded simulated model.
- The data directory is shared, temporary, or world-writable.

---

## Gate 3 — Boot, health, and fail-closed security verification

**Accountable:** Platform Operator · Security Reviewer witnesses
**Prerequisites:** Gate 2 `PASS`; process supervisor configured; log destination and retention ready; operator has the actor and control credentials.
**Status:** `NOT EXECUTED`

### G3 checks

| ID | Exact check | PASS criterion | Evidence |
|---|---|---|---|
| `G3-01` | Start the service with the production config and durable storage. | Process starts under the service account; PID and start time recorded. | Supervisor status, PID. |
| `G3-02` | Health and readiness. | `GET /health` returns `200` with `status: ok`; `GET /ready` returns `200` with `status: ready`; `GET /status` returns the running state. | Bodies and timestamps. |
| `G3-03` | Listener inventory. | `ss -ltnp \| grep "$PID"` shows only the intended health and gateway bindings. Before TLS/front-door cutover, both are loopback. No unintended wildcard bind. | `ss` output. |
| `G3-04` | Real provider registration. | Startup log contains `production model provider registered` with the approved provider ID, model ID, and endpoint. | Log excerpt. |
| `G3-05` | No simulated provider. | Startup log does **not** contain `no model registered: seeded simulated default model`. | Log search (empty). |
| `G3-06` | Control-plane authentication. | `GET /api/v1/control/status` without `X-API-Key` → `401`; with a wrong key → `401`; with the correct key → `200`. A `403 CONTROL_DISABLED` means no control key is configured: FAIL. | Three status codes. |
| `G3-07` | Scoped-route authentication. | `GET /api/v1/businesses` with no actor headers → `401`; with a wrong credential → `401`; with the bootstrap credential → `200`. | Three status codes. |
| `G3-08` | Unauthenticated business scope. | `GET /api/v1/intelligence/<random-id>?business_id=<BIZ>` without credentials → `401` (not `404`, not `200`). | Status code. |
| `G3-09` | No wildcard CORS. | With `Origin: https://example.invalid`, the response does not contain `Access-Control-Allow-Origin: *`. | Headers. |
| `G3-10` | Secret redaction in logs. | The startup and request logs contain no field named `credential`, `authorization`, `api_key`, or `secret` with a non-empty value. | Structured grep output. |
| `G3-11` | Clean shutdown. | `SIGTERM` stops the process without panic within the configured shutdown timeout; no listener remains; exit status recorded. | Exit status, `ss` output (empty). |
| `G3-12` | Restart hygiene. | A second start with the same config/data directory succeeds only after the first process is gone; a concurrent start is not used as a test. | PID and readiness records. |

### Exact commands

```bash
curl -sS -o /dev/null -w '%{http_code}\n' "$BASE/health"
curl -sS -o /dev/null -w '%{http_code}\n' "$BASE/ready"
curl -sS "$BASE/status"

# Control auth matrix.
curl -sS -o /dev/null -w '%{http_code}\n' "$BASE/api/v1/control/status"
curl -sS -o /dev/null -w '%{http_code}\n' -H "X-API-Key: wrong" "$BASE/api/v1/control/status"
curl -sS -o /dev/null -w '%{http_code}\n' -H "X-API-Key: $CONTROL_KEY" "$BASE/api/v1/control/status"

# Actor auth matrix.
curl -sS -o /dev/null -w '%{http_code}\n' "$BASE/api/v1/businesses"
curl -sS -o /dev/null -w '%{http_code}\n' \
  -H "X-Actor-ID: $OWNER" -H "X-Actor-Credential: wrong" "$BASE/api/v1/businesses"
curl -sS -o /dev/null -w '%{http_code}\n' "${BOOTSTRAP_HEADERS[@]}" "$BASE/api/v1/businesses"
```

### Stop conditions

- Any readiness failure.
- Any unintended listener or wildcard bind.
- Any control or scoped route that returns `200` without valid authentication.
- Any simulated-model log line.
- A secret value visible in logs or error output.
- A shutdown that hangs or leaves a listener.

On failure, stop the process, preserve the logs, and do not proceed to onboarding.

---

## Gate 4 — Onboarding, identity, membership, and agent provisioning

**Accountable:** Platform Operator · Pilot Owner confirms the business/agent decisions
**Prerequisites:** Gate 3 `PASS`; `RF-03`–`RF-07` and `OD-01`/`OD-02` decided; a second human approver identity if `RF-12` prohibits self-approval.
**Status:** `NOT EXECUTED`

### Identity options

Use the owner decision from `RF-05`. The bootstrap identity is `nx:human:bootstrap`, created at boot when enforcement is on and `NEXUS_BOOTSTRAP_CREDENTIAL` is set; its credential is re-armed on every boot. A dedicated human identity is created through `POST /api/v1/identities`; creating an identity **inside** a business requires business-wide membership, so a dedicated owner identity for a not-yet-created business must be created without `business_id` (or after the business and the bootstrap membership exist). Do not invent an identity ID, business ID, division ID, or agent ID. Record every returned value.

### G4 checks

| ID | Exact check | PASS criterion | Evidence |
|---|---|---|---|
| `G4-00` | Owner identity is active and authenticates before onboarding. | `GET /api/v1/identities/{id}` returns `200`; a scoped request with the owner credential does not return `401`. If the identity is dedicated, its creation record is attached. | Identity body and auth check. |
| `G4-01` | Create the business without automatic membership. | `POST /api/v1/businesses` returns `200`; `business_id` equals `RF-04` (or the recorded generated ID); `owner_identity_id` equals `RF-05`. | Request/response, redacted. |
| `G4-02` | Prove the owner has no membership yet. | `POST /api/v1/intelligence/execute` for the new business returns `403 AUTHORIZATION`. | Status and error envelope. |
| `G4-03` | Claim the initial business-wide membership. | `POST /api/v1/businesses/{id}/members` with `{}` returns `200` with the owner ID, role `OWNER`, status `active`. | Response. |
| `G4-04` | The business is visible to its member. | `GET /api/v1/businesses` includes the business; `GET /api/v1/businesses/{id}` returns `200`. | Bodies. |
| `G4-05` | Division decision applied. | If `OD-01` uses a division: `POST /api/v1/divisions` returns `200` with the approved business, owner, name, and active status; `GET /api/v1/divisions?business_id=...` includes it. If no division: `OD-01` records `N/A` with rationale. | Response and register. |
| `G4-06` | Register the research agent. | `POST /api/v1/agents` returns `200`; `business_id` matches; `allowed_tools` contains `web.search`; status is active; division matches `OD-01`. | Response. |
| `G4-07` | Agent read-back. | `GET /api/v1/agents/{id}` returns `200` and the approved capabilities/tools. | Body. |
| `G4-08` | Tool catalogue contains `web.search`. | `GET /api/v1/tools?business_id=...` returns `200` and `tools[]` contains an entry with `id == "web.search"`. | `jq` output. |
| `G4-09` | Foreign-scope isolation. | A second active identity that is not a member of `BIZ`: `GET /api/v1/businesses/{id}` → `404`; `GET /api/v1/intelligence/{id}?business_id=BIZ` → `403`; `GET /api/v1/recovery/executions/{id}?business_id=BIZ` → `403`; `GET /api/v1/businesses` does not list `BIZ`. Record the exact status for each route. | Status codes and bodies. |
| `G4-10` | Owner-only membership claim. | A non-owner authenticated identity cannot claim membership in `BIZ` (`403`). | Status code. |
| `G4-11` | Second approver exists (if required). | If `RF-12.self_approval_prohibited=true`, a second active human with business-wide membership exists and can authenticate. | Identity ID and auth check. |
| `G4-12` | No live provider call occurred. | The only Gate 4 execution attempt is the pre-membership negative check, which is rejected before admission and cannot call a provider. No execution is admitted in Gate 4. | Execution count before/after; 403 body. |

### Exact commands

```bash
# Create the business (membership-free). A dedicated owner does not need a
# membership to be named as owner_identity_id; the authenticated actor only
# needs to be a valid identity.
curl -sS "${BOOTSTRAP_HEADERS[@]}" -H 'Content-Type: application/json' \
  -X POST "$BASE/api/v1/businesses" \
  -d "{\"entity_id\":\"$BIZ\",\"name\":\"<RF-03 business name>\",\"owner_identity_id\":\"$OWNER\"}" \
  | tee "$EVIDENCE_DIR/gate4-business.json"

# Negative: the OWNER is not yet a member.
curl -sS "${OWNER_HEADERS[@]}" -H 'Content-Type: application/json' \
  -X POST "$BASE/api/v1/intelligence/execute" \
  -d "{\"business_id\":\"$BIZ\",\"actor_id\":\"$OWNER\",\"objective\":{\"description\":\"membership pre-check\"}}" \
  | tee "$EVIDENCE_DIR/gate4-premembership.json"

# Claim the initial business-wide membership. This must be the owner identity.
curl -sS "${OWNER_HEADERS[@]}" -H 'Content-Type: application/json' \
  -X POST "$BASE/api/v1/businesses/$BIZ/members" -d '{}' \
  | tee "$EVIDENCE_DIR/gate4-membership.json"

# Optional division, per OD-01.
curl -sS "${OWNER_HEADERS[@]}" -H 'Content-Type: application/json' \
  -X POST "$BASE/api/v1/divisions" \
  -d "{\"entity_id\":\"$DIVISION\",\"business_id\":\"$BIZ\",\"name\":\"<OD-01 name>\",\"owner_identity_id\":\"$OWNER\",\"parent_business_id\":\"$BIZ\"}" \
  | tee "$EVIDENCE_DIR/gate4-division.json"

# Register the research agent.
curl -sS "${OWNER_HEADERS[@]}" -H 'Content-Type: application/json' \
  -X POST "$BASE/api/v1/agents" \
  -d "{\"entity_id\":\"$AGENT\",\"name\":\"<RF-07 agent name>\",\"business_id\":\"$BIZ\",\"division_id\":\"$DIVISION\",\"capabilities\":[\"analysis\"],\"allowed_tools\":[\"web.search\"]}" \
  | tee "$EVIDENCE_DIR/gate4-agent.json"

# Tool catalogue.
curl -sS "${OWNER_HEADERS[@]}" \
  "$BASE/api/v1/tools?business_id=$BIZ" \
  | jq -e '.tools[] | select(.id == "web.search")' \
  | tee "$EVIDENCE_DIR/gate4-tools.json"
```

The business creation uses the bootstrap identity (or any authenticated identity). If the owner is a dedicated identity, it must already exist (`G4-00`); the membership, division, agent, and tool-catalogue commands use `OWNER_HEADERS`. If the owner is the bootstrap identity, set `OWNER_CREDENTIAL="$BOOTSTRAP_CREDENTIAL"` so both header sets are equivalent. The bootstrap identity may still exist; record why it exists and whether it remains usable.

### Stop conditions

- Business creation returns an unexpected owner or ID.
- The pre-membership negative check returns anything other than `403`.
- Membership claim fails or returns a non-`OWNER` role.
- The agent lacks `web.search` or is division-incompatible with the planned objective.
- A foreign identity can read the business or its records.
- Any live provider call occurs during Gate 4.

---

## Gate 5 — Governance policy application and verification

**Accountable:** Security Reviewer (approves) · Platform Operator (applies)
**Prerequisites:** Gate 4 `PASS`; `RF-12` decided; control key available; the policy body approved by the Security Reviewer.
**Status:** `NOT EXECUTED`

### Policy rules

- Policies are applied through `PUT /api/v1/control/policies/{id}` with `X-API-Key`. They are **not** in the config file.
- Policies are process-lifetime state. They do **not** survive a restart. Gate 7 includes reapplication.
- The built-in `default-allow` policy is read-only; `PUT`/`DELETE` return `409`.
- Most-restrictive-wins. A `REQUIRE_APPROVAL` policy wins over `ALLOW` regardless of precedence.
- Do not broaden a policy to make a test pass. Fix the decision and re-apply.
- **If `RF-12` chooses no approval:** `G5-02`–`G5-04` are `N/A`; record the approved alternative controls and the rationale. `G5-05`, `G5-06`, and the Gate 6 `N/A` record still apply.

### G5 checks

| ID | Exact check | PASS criterion | Evidence |
|---|---|---|---|
| `G5-01` | Policy surface is reachable. | `GET /api/v1/control/policies` with the control key returns `200`; `default-allow` is present. | Body and status. |
| `G5-02` | Apply the approved policy. | `PUT /api/v1/control/policies/<RF-12 id>` returns `200` with the expected `policy_id`, version, and active status. | Request body (no secrets), response. |
| `G5-03` | Read-back matches the decision. | `GET /api/v1/control/policies/<id>` returns `effect: REQUIRE_APPROVAL` (or the approved effect), `action.action_type: custom`, `action.action_ids: [tool_call]`, `resource.resource_type: tool`, `resource.resource_ids: [web.search]`, and the approved `approval_config`. | Full policy JSON. |
| `G5-04` | Scope is intentional. | The policy applies to the intended business/division/agent, or the owner has explicitly accepted a global scope. | Policy scope fields and `RF-12`. |
| `G5-05` | Built-in policy is immutable. | `PUT` and `DELETE` against `default-allow` return `409`. | Two status codes. |
| `G5-06` | No conflicting restrictive policy is hidden. | The effective set contains no unexpected `DENY` or `ESCALATE` that would block the approved objective. Any such policy is recorded and resolved before Gate 6. | Policy list. |
| `G5-07` | Approval enforcement is assigned to Gate 6. | If approval is required, record that Gate 6 must observe `APPROVAL_REQUIRED` and the approval decision. If approval is not required, record `N/A` and the alternative control accepted in `RF-12`. | Gate 6 plan and `RF-12`. |
| `G5-08` | Restart limitation is acknowledged. | The operator records that the policy must be re-applied after every restart; Gate 7 verifies reapplication. | Runbook acknowledgement. |

### Exact command

Use the body approved in `RF-12`. The shape below is the verified matching shape; replace the approval configuration with the owner's decision. The **policy ID, effect, action, and resource names are not examples to vary** without Security Reviewer approval.

```bash
curl -sS -X PUT "$BASE/api/v1/control/policies/<RF-12 policy id>" \
  -H "X-API-Key: $CONTROL_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "policy_type": "tool_usage",
    "name": "TrendSentry web.search approval",
    "description": "Require a human decision before any live web.search tool call.",
    "status": "active",
    "subject": { "subject_type": "all" },
    "action": { "action_type": "custom", "action_ids": ["tool_call"] },
    "resource": { "resource_type": "tool", "resource_ids": ["web.search"] },
    "effect": "REQUIRE_APPROVAL",
    "approval_config": {
      "approver_type": "human",
      "timeout_seconds": 3600,
      "auto_deny_on_timeout": true,
      "self_approval_prohibited": true,
      "delegation_allowed": false
    },
    "precedence": 1000
  }' | tee "$EVIDENCE_DIR/gate5-policy.json"
```

The `timeout_seconds`, `auto_deny_on_timeout`, `self_approval_prohibited`, `delegation_allowed`, and optional `approver_ids` values are owner decisions, not defaults to copy. `approver_ids` empty means any authorized business-wide member may decide.

### Stop conditions

- The policy cannot be read back with the intended effect.
- A broader `ALLOW` overrides the approval requirement.
- `default-allow` can be modified.
- The policy scope is broader than the owner approved.
- The approval timeout or self-approval rule is unset or contradicts `RF-12`.

---

## Gate 6 — First live provider workflow

**Accountable:** Platform Operator (executes) · Research Reviewer (observes) · Pilot Owner (authorizes the objective)
**Prerequisites:** Gates 0–5 `PASS`; the provider spend cap is active; the first objective is approved; the approver is available for the whole window; monitoring and log capture are live.
**Status:** `NOT EXECUTED`

### What this gate proves

This is the **first live call** in the program. It proves that:

- NEXUS can reach the configured OpenAI-compatible model endpoint with the injected credential;
- the model honors the strict JSON-schema structured-output request and the bounded agent loop;
- `web.search` reaches the selected live provider;
- the approval requirement, if configured, pauses and resumes the execution;
- the execution reaches a terminal result with evidence, synthesis, and evaluation.

It does **not** prove semantic source support, production scale, or cost beyond the recorded calls.

### Required pre-checks

| ID | Exact check | PASS criterion | Evidence |
|---|---|---|---|
| `G6-01` | Confirm the live search provider. | `printenv NEXUS_CAPABILITY_WEB` is `wikipedia` or `brave`; record the value. | Value. |
| `G6-02` | Confirm the provider registration log from this boot. | The log contains `production model provider registered` with the approved provider/model. | Log excerpt. |
| `G6-03` | Confirm the control policy from this boot. | `GET /api/v1/control/policies/<id>` returns the approved active policy, or the recorded `N/A` decision. | Policy JSON. |
| `G6-04` | Confirm the objective and agent. | Objective text matches `RF-14`; `context.agent_id` equals `RF-07` (or the deterministic selector is explicitly accepted); division compatibility holds. | Submitted body. |
| `G6-05` | Confirm spend headroom. | Provider console shows remaining budget above the owner's threshold; record the reading. | Console evidence. |

### G6 checks

| ID | Exact check | PASS criterion | Evidence |
|---|---|---|---|
| `G6-06` | Submit the objective. | `POST /api/v1/intelligence/execute` returns `202` with `execution_id`, `correlation_id`, and `status: accepted`. | Response body. |
| `G6-07` | Observe the approval pause (if required). | Polling `GET /api/v1/intelligence/{id}?business_id=...` returns `200` with `status: failed`, `error.code: APPROVAL_REQUIRED`, and `error.details.approval_id`. The approval appears in `GET /api/v1/approvals?business_id=...` as `approvals[].entity_id`. If approval is not required, record `N/A` with the `RF-12` rationale. | Both bodies. |
| `G6-08` | Human decision. | The named approver reviews the proposed action and calls `POST /api/v1/approvals/{id}/approve?business_id=...` with a non-empty `reason`; the endpoint returns `202`. If the approver denies, the run is not a pass; record the denial and stop. | Decision request/response, approver ID, time. |
| `G6-09` | Reach a terminal result. | Polling returns `200` with `status: completed`; `outcome.summary` contains `state=completed`, within the configured execution budget. | Final result body. |
| `G6-10` | Synthesis is present and typed. | `outcome.research_synthesis.status` is `complete`; `evidence_backed` is a non-empty array; each statement has at least one `source_ids` entry that resolves to a captured source. | Synthesis JSON. |
| `G6-11` | Evidence sources are live. | `research_evidence.sources[]` contains real, resolvable URLs; no scripted fixture URL or placeholder domain appears; the domains match the selected provider's coverage. | Source list and reviewer spot-check. |
| `G6-12` | Deterministic evaluation. | `outcome.research_evaluation.status` is `passed` or an explicitly accepted `warnings`; method is `deterministic_v1`; `failed`, `incomplete`, or unreviewed warnings are a FAIL. | Evaluation JSON. |
| `G6-13` | Approval history. | The durable record's `approvals[]` (read through `/api/v1/recovery/executions/{id}`) shows the approval ID, outcome, and decision time. The durable projection deliberately excludes the approver identity; verify the actor from the live approval decision response and logs. | Approval history and live decision record. |
| `G6-14` | Tool confinement. | The trace/evidence shows only the approved `web.search` capability for the research call; no `http.request`, filesystem, GitHub, memory write, or delegation occurred unless separately approved. | Trace/evidence. |
| `G6-15` | No secret exposure. | Result, trace, logs, and error envelopes contain no credential, `Authorization` header, or API key. | Redaction scan. |
| `G6-16` | Provider usage recorded. | Record provider-side call count, tokens, and spend if available. If the provider exposes no usage log, record the owner's explicit acceptance from `RF-10` and use the NEXUS trace plus durable evidence as the available proof. | Console export or acceptance. |
| `G6-17` | Execution identity recorded. | Record `execution_id`, `correlation_id`, business, division, agent, actor, and UTC start/end. | Evidence index. |

### Exact submission

```bash
BODY=$(jq -n \
  --arg biz "$BIZ" \
  --arg owner "$OWNER" \
  --arg div "$DIVISION" \
  --arg agent "$AGENT" \
  --arg desc "$(cat "$NEXUS_HOME/objective.txt")" \
  '{business_id:$biz, actor_id:$owner, division_id:$div,
    objective:{description:$desc, context:{agent_id:$agent}}}')

curl -sS "${OWNER_HEADERS[@]}" -H 'Content-Type: application/json' \
  -X POST "$BASE/api/v1/intelligence/execute" -d "$BODY" \
  | tee "$EVIDENCE_DIR/gate6-submit.json"
```

The commands use `OWNER_HEADERS` for submission and polling. The approval decision must use the **approver's** headers: `APPROVER_HEADERS`. If self-approval is prohibited, the owner cannot approve. If self-approval is allowed, set `APPROVER` to the owner.

Use the owner's actual actor headers, not bootstrap, if the objective is submitted by the owner identity. The body field is `objective.description` (≤ 4,000 characters). `objective.context.agent_id` is optional but should be explicit for the first run. A budget, if used, can only tighten the runtime caps; `max_execution_time` is a Go duration integer in nanoseconds, not a string.

### Polling and approval

```bash
EXEC=$(jq -r '.execution_id' "$EVIDENCE_DIR/gate6-submit.json")

# Poll to the approval pause or terminal state.
for i in $(seq 1 120); do
  curl -sS "${OWNER_HEADERS[@]}" \
    "$BASE/api/v1/intelligence/$EXEC?business_id=$BIZ" \
    >"$EVIDENCE_DIR/gate6-poll-$i.json"
  STATUS=$(jq -r '.status // empty' "$EVIDENCE_DIR/gate6-poll-$i.json")
  CODE=$(jq -r '.error.code // empty' "$EVIDENCE_DIR/gate6-poll-$i.json")
  [ "$STATUS" = "completed" ] && break
  [ "$CODE" = "APPROVAL_REQUIRED" ] && break
  sleep 1
done

# If paused: list the pending approval, decide it once, and resume polling.
curl -sS "${OWNER_HEADERS[@]}" \
  "$BASE/api/v1/approvals?business_id=$BIZ" \
  | tee "$EVIDENCE_DIR/gate6-approvals.json"

APPROVAL=$(jq -r '.approvals[0].entity_id' "$EVIDENCE_DIR/gate6-approvals.json")
curl -sS "${APPROVER_HEADERS[@]}" -H 'Content-Type: application/json' \
  -X POST "$BASE/api/v1/approvals/$APPROVAL/approve?business_id=$BIZ" \
  -d '{"reason":"Gate 6 approved first live web.search"}' \
  | tee "$EVIDENCE_DIR/gate6-approve.json"
```

The approver must be an authorized business-wide member. If self-approval is prohibited, the submitting identity cannot be the approver. A decision without a non-empty `reason` is invalid.

### Stop conditions

Stop immediately and preserve evidence if any of the following occurs:

- the provider returns an authentication, quota, schema, or timeout error that is not resolved by an approved configuration change;
- the approval requirement is bypassed, or a tool runs before the approval decision;
- the synthesis is absent, `inconclusive`, or has no evidence-backed statement;
- the evaluation is `failed` or `incomplete`;
- the sources are scripted fixtures, placeholders, or unreachable;
- an unapproved tool, delegation, or scope is used;
- spend reaches the owner's threshold;
- any secret appears in output or logs;
- the execution does not reach a terminal state within its budget plus the approval wait.

A failed first live workflow is not retried automatically. Preserve the record, diagnose, obtain a new owner decision, and re-enter at the appropriate gate.

---

## Gate 7 — Durability, recovery, backup, and restore

**Accountable:** Platform Operator · Engineering Lead verifies · Security Reviewer witnesses
**Prerequisites:** Gate 6 `PASS`; the execution ID and evidence snapshot; the process is under the pilot supervisor; a maintenance window for the restart and backup; `bin/nexus-backup` built in Gate 1.
**Status:** `NOT EXECUTED`

### G7 checks

| ID | Exact check | PASS criterion | Evidence |
|---|---|---|---|
| `G7-01` | Capture the live pre-restart view. | `GET /api/v1/intelligence/{id}?business_id=...` returns `200` with the terminal result; state, synthesis status, evaluation status, and source count recorded. | Body. |
| `G7-02` | Capture the durable pre-restart view. | `GET /api/v1/recovery/executions/{id}?business_id=...` returns `200`; `state`, `research_evidence.sources[]`, `research_synthesis`, `research_evaluation`, and `approvals[]` (approval ID, outcome, decision time) match Gate 6. The durable projection does not carry the approver identity. | Body. |
| `G7-03` | Citation audit before restart. | `GET /api/v1/recovery/executions/{id}/audit?business_id=...` returns `200`; `audit.status` is `passed` or an accepted `warnings`; `audit.method` is `deterministic_v1`. | Body. |
| `G7-04` | Durable-read authorization matrix. | Missing `business_id` → `400`; no credentials → `401`; authenticated non-member of the business → `403`; unknown execution → `404`; a business member outside the record's division → `404`. | Status codes. |
| `G7-05` | Quiescent stop. | Stop the service gracefully; process exits; no listener remains. Record stop time and exit status. | Supervisor log, `ss` (empty). |
| `G7-06` | Restart with the same data directory. | `/ready` returns `200`; the service starts with the approved config. | Readiness, log. |
| `G7-07` | Live result is process-local. | `GET /api/v1/intelligence/{id}?business_id=...` returns `404` after restart. This is expected, not a failure. | Status code. |
| `G7-08` | Durable read survives. | `GET /api/v1/recovery/executions/{id}?business_id=...` returns `200` with the same state, evidence, synthesis, evaluation, and approvals as `G7-02`. | Body comparison. |
| `G7-09` | Audit survives. | The audit endpoint returns `200` with the same status and method as `G7-03`. | Body comparison. |
| `G7-10` | Approval resume is not promised. | Record that the paused snapshot is gone and the execution is not resumable through the approval path after restart; a fresh governance admission is required. If the execution was already completed before restart, this is a documentation check, not a resume test. | Record. |
| `G7-11` | Policy reapplication. | After restart, `GET /api/v1/control/policies/<id>` returns `404` (or the policy is absent). Re-apply the Gate 5 policy with `PUT`; verify it is active. No new work is admitted before this check passes. | Policy list before/after, `PUT` response. |
| `G7-12` | Backup artifact. | Stop the service again after the restart checks. With the service stopped, `nexus-backup backup -data-dir "$DATA_DIR" -out "$ARTIFACT"` exits 0; the artifact exists and is not overwritten. | Exit code, `stat`, SHA-256. |
| `G7-13` | Backup verification. | `nexus-backup verify -in "$ARTIFACT"` exits 0; the first tar entry is `nexus-backup-manifest.json`; the manifest lists the store files; no `workspaces/` entry appears. | Verify output, `tar -tf \| head -1`. |
| `G7-14` | Artifact scope. | Record that the artifact excludes the filesystem sandbox and process-local runtime state: in-memory results, approval/resume snapshots, cancellation state, escalation state, event buffers, and traces. The durable execution journal and the approval history recorded on it are included. It contains credential **verification hashes**, not raw credentials. Protect it as sensitive. | Manifest exclusions and owner acknowledgement. |
| `G7-15` | Restore into an isolated directory. | `nexus-backup restore -in "$ARTIFACT" -dest "$RESTORE_DIR"` exits 0; `$RESTORE_DIR` was empty or missing. | Exit code, directory listing. |
| `G7-16` | Restored store is usable. | Start a second, isolated instance with `NEXUS_DATA_DIR="$RESTORE_DIR"`, `NEXUS_HEALTH_PORT=<alt>`, and `--http-addr 127.0.0.1:<alt+1>`; `/ready` returns `200`; the durable execution read returns `200` with the recorded state. Stop it. | Readiness and read body. |
| `G7-17` | Restore refuses to overwrite. | Restore into a non-empty directory exits non-zero and leaves existing files unchanged. | Exit code, checksum before/after. |
| `G7-18` | Retention is offline. | If `OD-07` enables retention, run it only on a stopped store or an isolated copy; never against the live store. If disabled, record `N/A`. | Command record. |
| `G7-19` | Preserve the live store. | The production data directory, logs, and evidence are retained; nothing is deleted during recovery testing. | Directory listing. |
| `G7-20` | Restart the production service. | After recovery testing, restart the approved service with the production data directory; `/ready` returns `200`; the durable execution read still returns `200`; the Gate 5 policy is re-applied and verified before any new work. | Readiness, durable read, policy list. |

### Exact backup and restore sequence

```bash
# Gate 7 backup phase begins with the service stopped. Stop it again after
# the restart checks; the application must be quiescent for a backup.
ARTIFACT="$BACKUP_DIR/nexus-$(date -u +%Y%m%dT%H%M%SZ).tar"
"$NEXUS_HOME/bin/nexus-backup" backup -data-dir "$DATA_DIR" -out "$ARTIFACT"
"$NEXUS_HOME/bin/nexus-backup" verify -in "$ARTIFACT"
tar -tf "$ARTIFACT" | head -1          # must be nexus-backup-manifest.json
! tar -tf "$ARTIFACT" | grep -q '^workspaces/'
sha256sum "$ARTIFACT" | tee "$EVIDENCE_DIR/gate7-artifact.sha256"

# Restore to a new, isolated directory.
RESTORE_DIR="$NEXUS_HOME/restore-check-$(date -u +%Y%m%dT%H%M%SZ)"
test ! -e "$RESTORE_DIR" || test -z "$(ls -A "$RESTORE_DIR")"
"$NEXUS_HOME/bin/nexus-backup" restore -in "$ARTIFACT" -dest "$RESTORE_DIR"

# Boot the restored copy on alternate ports, with the restored data directory.
NEXUS_DATA_DIR="$RESTORE_DIR" NEXUS_HEALTH_PORT=18090 \
  "$NEXUS_HOME/bin/nexus" --config "$CONFIG" --http-addr 127.0.0.1:18091 \
  >"$EVIDENCE_DIR/gate7-restore.log" 2>&1 &
RESTORE_PID=$!
for i in $(seq 1 100); do
  curl -fsS "http://127.0.0.1:18091/ready" && break
  sleep 0.1
done
curl -sS "${OWNER_HEADERS[@]}" \
  "http://127.0.0.1:18091/api/v1/recovery/executions/$EXEC?business_id=$BIZ"
kill -TERM "$RESTORE_PID"
wait "$RESTORE_PID"
```

The restored instance uses the same production config and credential environment; it must not receive new work. Its only purpose is to prove the durable store is readable after restore.

### Stop conditions

- The durable read differs before and after restart.
- The audit status changes or becomes unavailable.
- The live result does **not** return `404` after restart (this would indicate a process-local expectation is wrong; investigate before proceeding).
- The custom policy is missing after restart and is not re-applied and verified before new work.
- The backup verify fails, the artifact is incomplete, or it contains `workspaces/`.
- Restore overwrites a non-empty destination or the restored store cannot be read.
- Any evidence is deleted.

---

## Gate 8 — Acceptance, operational handover, and final GO/NO-GO

**Accountable:** Pilot Owner (decision) · Security Reviewer (veto) · Research Reviewer (semantic acceptance) · Platform Operator (handover)
**Prerequisites:** Gates 0–7 `PASS`; complete evidence bundle; the first live output; named on-call and rollback contacts.
**Status:** `NOT EXECUTED`

### G8 checks

| ID | Exact check | PASS criterion | Evidence |
|---|---|---|---|
| `G8-01` | Human source review. | The Research Reviewer opens every cited source and records, per evidence-backed statement, whether the source supports it. Acceptance is explicit; a structural audit pass is not semantic acceptance. | Signed review sheet with URLs and verdicts. |
| `G8-02` | Owner acceptance of the output. | The Pilot Owner accepts the first live output against `RF-14`/`RF-17`, including any warnings and limitations. | Signed acceptance. |
| `G8-03` | Security review. | No secret exposure; auth boundaries verified; egress controlled outside NEXUS; provider spend within cap; logs retained; policy reapplication procedure verified; no scripted provider used. | Security checklist. |
| `G8-04` | Artifact and evidence bundle. | Commit, binary checksums, config hash, execution IDs, approval IDs, audit result, provider usage, backup/restore evidence, and gate records are present and hashed. | Bundle index and hashes. |
| `G8-05` | Operational handover. | On-call owner, monitoring/alert route, backup schedule, restore-test cadence, unknown-outcome procedure, stop/rollback authority, and credential rotation are recorded. | Runbook acknowledgement. |
| `G8-06` | Known boundaries are re-confirmed. | The owner re-confirms the [owner-handoff.md](owner-handoff.md) §6 boundaries; no one has promised approval resume across restart, multi-instance coordination, automatic replay of `unknown`, a monetary cap, or enforced egress allow-list behavior. | Signed acknowledgement. |
| `G8-07` | Evidence-reproducibility gap decision. | If `G1-12` recorded that the two workflow specs are untracked, the owner either accepts the recorded validation evidence as sufficient or requires a separately authorized change to commit the specs. | Decision record. |
| `G8-08` | Final decision. | A written `GO` or `NO-GO` is recorded. `GO` names the scope, activation window, monitoring period, review date, and rollback triggers. `NO-GO` names the reasons and required corrections. The default is `NO-GO`. | Signed decision. |
| `G8-09` | Post-activation review scheduled. | A dated review covers continue/expand/stop, incident review, spend, source quality, and policy reapplication after every restart. | Calendar/record. |
| `G8-10` | Rollback walkthrough. | The operator walks through the `RF-18` rollback procedure: stop, preserve evidence, restore the last verified backup into an isolated directory, rotate exposed credentials, reapply governance, and notify the owner/security reviewer. A tabletop plus the Gate 7 restore evidence is acceptable; do not perform a destructive rollback on production data. | Walkthrough record with owners and commands. |

### Final decision record

| Field | Value |
|---|---|
| Decision | `GO` / `NO-GO` (default `NO-GO`) |
| Scope authorized | |
| Activation window (UTC) | |
| First production objective | |
| Monitoring period | |
| Review date | |
| Rollback triggers | |
| Pilot Owner signature | |
| Security Reviewer signature (veto acknowledged) | |
| Research Reviewer signature | |
| Platform Operator handover signature | |

**Current decision: `NO-GO`.** No live call, deployment, production data use, or production cutover is authorized until Gate 0 is signed and Gates 1–8 pass in order.

---

## Appendix A — Gate evidence record template

```text
Gate:                    <0–8>
Result:                  <PASS | FAIL | BLOCKED | N/A>
UTC start / end:         <YYYY-MM-DDTHH:MM:SSZ> / <...>
Executed by:             <name, role>
Accountable reviewer:    <name, role>
Build commit:            <098267d... or approved successor>
Binary SHA-256:          <nexus> / <nexus-backup>
Config SHA-256:          <hash of the secret-free config>
Environment:             <host, OS, Go version, NEXUS_ENVIRONMENT>
Commands / actions:      <exact, secrets redacted>
Observed output:         <file paths + SHA-256>
Deviations:              <none, or description + approval>
Stop conditions hit:     <none, or list>
Decision:                <continue | stop>
```

## Appendix B — Stop matrix

| Condition | Immediate action | Authority |
|---|---|---|
| Secret exposure in logs, chat, or evidence | Stop; rotate the exposed credential; incident process; preserve logs. | Security Reviewer |
| `unknown` execution | Stop new work; preserve the record; do not auto-retry; human decision required. | Pilot Owner + Platform Operator |
| Approval policy missing after restart | Stop admitting work; re-apply and verify the policy. | Platform Operator |
| Unauthorized access or foreign-scope success | Stop; security review; preserve evidence. | Security Reviewer |
| Citation audit `failed` / `incomplete` / `unavailable` / `incompatible` | Stop; do not accept the output. | Research Reviewer |
| Provider spend threshold reached | Stop; notify the owner. | Pilot Owner |
| Data-directory corruption or unreadable record | Stop; preserve the store; restore only into an isolated directory. | Platform Operator + Engineering Lead |
| Two processes on one data directory | Stop all but one; investigate. | Platform Operator |
| Owner or Security Reviewer calls stop | Stop immediately. | Pilot Owner / Security Reviewer |

## Appendix C — Known non-guarantees

These are properties of the current build, not activation defects:

- live result routes (`/api/v1/requests/{id}`, `/api/v1/intelligence/{id}`, `/api/v1/executions/{id}`) are process-local and return `404` after restart;
- durable inspection is read-only; there is no resume, replay, reconciliation write, or repair endpoint;
- `unknown` is terminal and never auto-retried;
- approval pauses do not survive restart; a fresh governance admission is required;
- policies are process-lifetime state and must be re-applied after every restart;
- multi-instance coordination against one data directory is unsupported;
- NEXUS does not enforce a monetary cost cap; the provider account must;
- `security.egress_allow_list` is not wired to the model or web-search adapters; enforce egress at the host/network layer;
- the citation audit is structural and deterministic; it is not a semantic truth judgment;
- SSE `tool.registered` is not reliable provider proof; use the process environment, live source domains, and provider-side evidence.

## Appendix D — Verified source references

| Source | What this runbook relies on |
|---|---|
| `internal/foundation/config/config.go` | Config schema, unknown-field rejection, env precedence, production gates, `timeout_ms` derivation. |
| `internal/foundation/app/app.go` | FileStore creation/hydration and fail-closed startup. |
| `internal/gateway/server.go` | Routes, control-key gate, health/readiness/status, approval endpoints. |
| `internal/gateway/org.go` | Business/identity/division/membership request bodies and owner-membership rules. |
| `internal/gateway/agents.go` | Agent registration and tool allowlist. |
| `internal/gateway/intelligence.go` | Objective submission, polling, trace, process-local result behavior. |
| `internal/gateway/recovery.go` | Durable read/list/audit auth and response shapes. |
| `internal/gateway/policies.go` | Policy upsert, server-derived fields, built-in read-only policy. |
| `internal/foundation/governance/policy.go` | Policy schema, `approval_config`, `REQUIRE_APPROVAL` validation. |
| `internal/foundation/governance/engine.go` | Default-deny, most-restrictive-wins precedence. |
| `internal/launcher/capability.go` | `NEXUS_CAPABILITY_WEB` provider selection; no production scripted guard. |
| `internal/launcher/launcher.go` | Real-provider registration and simulated-provider guard. |
| `internal/foundation/modelrouter/openai.go` | OpenAI-compatible `POST {endpoint}/chat/completions`, `api_key_env`, 30-second default, strict structured output. |
| `internal/capability/websearch_brave.go` | `BRAVE_API_KEY` requirement and endpoint handling. |
| `internal/backup/backup.go` | Backup artifact, manifest, exclusions, verify/restore semantics. |
| `cmd/nexus/main.go` | CLI flags, bootstrap identity provisioning, gateway-address default. |
| `cmd/nexus-backup/main.go` | Backup CLI modes and flags. |
| `e2e/tests/30-trendsentry-research-workflow.spec.ts` | Scripted workflow evidence (workspace artifact, not tracked at baseline). |
| `e2e/tests/31-durable-recovery-read.spec.ts` | Scripted durable-read evidence (workspace artifact, not tracked at baseline). |
| `contracts/DURABLE_EXECUTION_READ_AND_RECONCILIATION_CONTRACTS.md` | Durable read guarantees and non-guarantees. |
