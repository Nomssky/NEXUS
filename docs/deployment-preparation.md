# NEXUS — Deployment Preparation Guide

This document is a **deployment preparation artifact** for operators who need to
run NEXUS on a production host. It consolidates the operational surface across
`docs/m0-development.md`, `docs/operations-runbook.md`, `docs/release.md`,
`docs/http-gateway.md`, `docs/storage-ownership.md`, and `docs/model-routing.md`
into a single reference.

It is **not** a deployment itself: it does not provision infrastructure, change
DNS, open firewall ports, or touch production data. All secret values must be
supplied by the operator through environment variables or a secret store; none
appear in this document or in any committed config file.

---

## 1. Architecture distinction

NEXUS distinguishes two deployment postures:

| Posture | Description | Supported by this milestone |
|---------|-------------|------------------------------|
| **Local development** | Starts on the owner's PC, serves authenticated requests, uses simulated/scripted providers by default | Yes — `make run` or `go run ./cmd/nexus` |
| **Production-host** | Stable service on a dedicated host/VPS with controlled storage, secrets, network exposure, restart/recovery, monitoring, and backup | Partially — binary and tooling exist; no deployment automation, no TLS, no container image |

No deployment pipeline, container image, systemd unit, or Terraform module exists
in this repository. The operator is responsible for provisioning the host and
running the compiled binary.

---

## 2. Binary and artifact production

Build the release artifacts from a clean tree:

```bash
# Build all packages and tools
make build            # ./bin/nexus
make build-backup     # ./bin/nexus-backup
make build-retention  # ./bin/nexus-retention

# Build versioned release artifacts with checksums and manifest
make release VERSION=1.0.0          # ./dist/nexus-1.0.0-<os>-<arch>/
make release-verify VERSION=1.0.0   # verify checksums, manifest, inventory
```

Release artifacts contain only:
- `nexus` — the application binary
- `nexus-backup` — the backup/restore operator tool
- `nexus-retention` — the execution-journal retention tool
- `operations-runbook.md` — recovery/rollback guidance
- `release-manifest.txt` — format, name, version, commit, tree state, Go version, platform, build time
- `SHA256SUMS` — SHA-256 of every artifact file

No `.env` files, credentials, `workspaces/`, persistent data, or frontend build output are ever included.

**Reproducibility:** With the same `version`, `commit`, and Go toolchain, binaries
are bit-for-bit reproducible (`-trimpath`). The manifest and `SHA256SUMS` are not
byte-identical across builds because `built_at` differs.

**Tree state:** A build from a dirty tree prints a warning and is not an official
release. Commit all intended changes before building for production.

---

## 3. Environment variable inventory (least privilege)

All configuration is non-secret unless explicitly marked. Secrets are **never**
stored in config files; they are read from environment variables at startup.

### Non-secret operational configuration

| Variable | Meaning | Default | Production requirement |
|----------|---------|---------|----------------------|
| `NEXUS_ID` | Installation ID | `nx:nexus:local` | Any non-empty value |
| `NEXUS_ENVIRONMENT` | `development` \| `staging` \| `production` | `development` | `production` |
| `NEXUS_LOG_LEVEL` | `debug` \| `info` \| `warn` \| `error` \| `fatal` | `info` | `info` or `warn` |
| `NEXUS_LOG_FORMAT` | `json` \| `text` | `json` | `json` |
| `NEXUS_HEALTH_ENABLED` | Enable health/readiness surface | `true` | `true` |
| `NEXUS_HEALTH_HOST` | Health bind host (loopback default) | `127.0.0.1` | `127.0.0.1` |
| `NEXUS_HEALTH_PORT` | Health bind port | `8080` | Any free port |
| `NEXUS_HTTP_ADDR` | Gateway bind address (`<host>:<port>`) | `127.0.0.1:8081` | Any free port |
| `NEXUS_SHUTDOWN_TIMEOUT_SECONDS` | Graceful drain budget | `30` | `30` or higher |
| `NEXUS_DATA_DIR` | Durable record root (`storage.data_dir`); empty = in-memory | `` | **Required** — non-empty, local durable filesystem |
| `NEXUS_SEEDED_PROVIDER_STATUS` | Seeded provider health: `""`/`healthy`/`offline` | `` | Not used in production |
| `NEXUS_SECURITY_AUDIT_ENABLED` | Security audit trail | `true` | `true` |
| `NEXUS_SECURITY_REQUIRE_AUTHENTICATION` | Require authenticated actor on scoped paths | `true` | `true` |
| `NEXUS_SECURITY_ENFORCE_BUSINESS_SCOPE` | Require membership of requested `business_id` | `true` | `true` |
| `NEXUS_SECURITY_SANDBOX_ENABLED` | Filesystem isolation sandbox | `true` | `true` |
| `NEXUS_SECURITY_EGRESS_ALLOW_LIST` | Outbound egress allow-list (empty = deny-by-default) | `` | Set to needed hosts |
| `NEXUS_SECURITY_DEV_ALLOW_UNSAFE_OVERRIDES` | Dev-only escape hatch | `false` | `false` |

### Secrets (environment only, never in config files)

| Variable | Meaning | Empty behavior |
|----------|---------|---------------|
| `NEXUS_CONTROL_API_KEY` | API key for `/api/v1/control/*` | Control endpoints answer `403` (fail closed) |
| `NEXUS_BOOTSTRAP_CREDENTIAL` | Provisions the first identity + credential + membership on fresh install | With enforcement on, scoped endpoints stay `401` |
| `NEXUS_BOOTSTRAP_BUSINESS` | Business the bootstrap identity joins | `default` |
| `<api_key_env>` per provider | Model provider API key (named in `models.providers[].api_key_env`) | Provider fails to initialize |

Generate secrets with a cryptographically random source, e.g.:

```bash
NEXUS_CONTROL_API_KEY=$(openssl rand -hex 32)
NEXUS_BOOTSTRAP_CREDENTIAL=$(openssl rand -hex 32)
```

---

## 4. Configuration file template

A production configuration file is JSON. Unknown fields are rejected. Provider
entries reference credentials by environment variable name only — never by value.

```json
{
  "nexus": { "id": "nx:nexus:production", "environment": "production" },
  "storage": { "data_dir": "/var/lib/nexus" },
  "logging": { "level": "info", "format": "json" },
  "health": { "enabled": true, "host": "127.0.0.1", "port": 8080 },
  "lifecycle": { "shutdown_timeout_seconds": 30 },
  "security": {
    "audit_enabled": true,
    "require_authentication": true,
    "enforce_business_scope": true,
    "sandbox_enabled": true,
    "dev_allow_unsafe_overrides": false,
    "egress_allow_list": []
  },
  "models": {
    "providers": [
      {
        "id": "openai-prod",
        "name": "Production OpenAI-compatible provider",
        "endpoint": "https://api.example.com/v1",
        "model_id": "gpt-4o-mini",
        "api_key_env": "OPENAI_API_KEY",
        "timeout_ms": 30000,
        "allow_insecure_http": false
      }
    ]
  }
}
```

**Rules:**
- `endpoint` must be `https`, or `http` only with `allow_insecure_http: true` and a loopback host
- No URL userinfo, query string, or fragment in endpoints
- `api_key_env` names the environment variable; the value is read at startup and never serialized
- With `models.providers` set, the seeded simulator is NOT registered
- Production refuses to start without at least one real provider (`config.provider_missing`)
- Production refuses to start without `storage.data_dir` (`config.storage_missing`)
- Production refuses to start with any security setting relaxed (`config.security_unsafe`)

---

## 5. Persistent data directory requirements

The durable store root (`storage.data_dir`) is single-writer: exactly one live
NEXUS process may own it. An OS advisory `flock` is acquired on
`<data_dir>/.nexus-store.lock` before any record is read.

### Filesystem requirements

- **Local, durable, single-writer filesystem.** Do **not** place `storage.data_dir`
  on NFS/SMB or any network filesystem that does not propagate `flock` semantics.
- **Container platforms:** guarantee only one replica mounts the data directory
  read-write (e.g. a ReadWriteOnce volume with one replica).
- **Directory permissions:** newly created store roots and type directories are
  `0700`; new record files, batch journals, lock markers, restored files, and
  backup artifacts are `0600`; existing directory group/other bits are tightened.
- **The lock marker is never deleted.** A leftover `.nexus-store.lock` after a
  crash is normal and harmless — only the kernel lock state is authoritative.

### Directory layout

```
<data_dir>/
  identity/<id>.json
  business/<id>.json
  division/<id>.json
  credential/<id>.json     verification hashes only — never raw secrets
  membership/<id>.json
  agent/<id>.json
  memory/<id>.json
  execution/<id>.json      durable execution journal records
  invocation/<id>.json     durable invocation records
  .batch-journal.json      transient crash-recovery journal
  .nexus-store.lock        OS advisory single-writer marker (never deleted)
  workspaces/<business>/   filesystem sandbox — NOT part of store backup
```

### Boot-time path validation

- Must be absolute, clean, free of `..` traversal and NUL bytes
- Must not be the filesystem root or a sensitive system path
  (`/etc`, `/proc`, `/sys`, `/dev`, `/boot`, `/root`, `/run`, `/usr`, `/bin`,
  `/sbin`, `/lib*`, `/var/run`, `/var/log`, `/var/spool`, `~/.ssh`, `~/.gnupg`)
- Must be an existing directory, or a final component whose immediate parent
  already exists as a real directory (only the final component is created)
- Configured symlinks are resolved once; the canonical target is used thereafter

---

## 6. Startup, shutdown, and health

### Startup

```bash
NEXUS_DATA_DIR=/var/lib/nexus \
NEXUS_CONTROL_API_KEY=<control-key> \
NEXUS_BOOTSTRAP_CREDENTIAL=<bootstrap-credential> \
NEXUS_ENVIRONMENT=production \
./bin/nexus
```

Startup sequence: `START → INITIALIZE → READY → RUNNING`. Invalid or missing
configuration causes a **safe failure**: the process does not start, prints a
secret-free diagnostic to stderr, and exits non-zero.

### Health and readiness

| Endpoint | Port | Meaning |
|----------|------|---------|
| `GET /health` | Health port (default 8080) | Liveness — process is alive |
| `GET /readiness` | Health port | Readiness — initialized, store recovered, accepting work |
| `GET /ready` | Gateway port (default 8081) | Engine accepting work (`503` until running) |

Readiness is **not** claimed merely because the process is running: it is `503`
until initialization completes.

### Shutdown

On `SIGINT`/`SIGTERM` or context cancellation:
`SHUTDOWN_REQUESTED → DRAINING → STOPPED`

The process stops accepting new work, marks itself not-ready, runs close hooks in
reverse order (core runtime → record store → health server), and exits `0` on a
clean stop (`1` if a close hook fails). Storage ownership is released on every
exit, including crash (`SIGKILL` is safe — the kernel releases the flock).

---

## 7. Backup, restore, and retention

All three tools are **offline/quiescent**: stop NEXUS before running them. The
single-writer lock prevents a live process from sharing the data directory.

### Backup

```bash
# 1. Stop NEXUS
# 2. Back up
./bin/nexus-backup backup -data-dir /var/lib/nexus -out /backups/nexus-$(date -u +%Y%m%dT%H%M%SZ).tar
# 3. Verify
./bin/nexus-backup verify -in /backups/nexus-....tar
```

- Artifact is owner-only (`0600`) — contains credential verification hashes
- First tar entry is the manifest; store files follow at store-relative paths
- Refuses to overwrite an existing artifact; never includes `workspaces/`,
  temp files, `.env` files, secrets, or the lock marker
- Written to a temp file and atomically renamed only on success

### Restore

```bash
# Destination must be empty or not exist
./bin/nexus-backup restore -in /backups/nexus-....tar -dest /var/lib/nexus-restored
```

- Built in a hidden staging directory, validated by reopening through the real
  store path, published by atomic rename
- Validation store is closed (lock released) before publishing
- Restored directories are `0700`, restored files are `0600`
- Restoring the same artifact into two empty destinations yields equivalent
  logical state

### Retention (journal disk reclamation)

```bash
# Dry-run (plan mutates nothing durable)
./bin/nexus-retention plan -data-dir /var/lib/nexus -min-age 168h -batch 256

# Apply (removes eligible terminal records in one atomic batch)
./bin/nexus-retention apply -data-dir /var/lib/nexus -min-age 168h -batch 256
```

- Acquires the same single-writer lock as the application — fails closed if
  NEXUS is running
- Only terminal executions with all-known terminal outcomes and age ≥ `-min-age`
  are removed; nonterminal, unknown, too-young, and ambiguous records retained
- `plan` reports counts without deleting; `apply` requires explicit `-min-age`

---

## 8. Rollback

Two independent rollbacks — keep them separate:

- **Binary rollback:** redeploy the previous `nexus` binary. Safe as long as
  the previous binary can read the current `storage.data_dir` format
  (`filestore-json/v1`).
- **Data rollback:** restore a pre-change backup into a **new** directory and
  cut over (§7). Restoring an older backup after newer writes have occurred
  discards those newer writes — it is not a merge.

There is **no migration framework** and no automatic schema migration.

---

## 9. Operational metrics and observability

Operational metrics are served on the existing control-plane endpoint:

```bash
curl -s -H "X-API-Key: $NEXUS_CONTROL_API_KEY" \
     http://127.0.0.1:8080/api/v1/control/metrics
```

- Authentication: fail-closed constant-time comparison; `403` with no key,
  `401` with wrong/missing key
- Response contains only fixed numeric fields — no IDs, prompts, output,
  credentials, secrets, or store paths
- Counters are **process-local** and reset to `0` on every restart; they do
  **not** establish exactly-once execution, durable persistence, or cluster state

Health endpoints:
- `GET /health` — liveness (`200` if server running)
- `GET /readiness` — readiness (`200` only after startup barrier)
- `GET /status` — engine lifecycle state (`CREATED`/`RUNNING`/`DRAINING`/`STOPPED`)

No Prometheus/OpenTelemetry export, no scraping endpoint, no alerting, no
dashboard integration. No distributed metrics aggregation (single process only).

---

## 10. Network exposure and reverse-proxy assumptions

The gateway binds `<health-host>:<health-port+1>` by default
(`127.0.0.1:8081` with health on `8080`). Override with `-http-addr`.

**No TLS termination is implemented.** If the service is exposed beyond
loopback, the operator must provide a reverse proxy (e.g. nginx, Envoy, Caddy)
that terminates TLS and forwards to the loopback gateway port. The reverse
proxy must:
- Forward `X-Actor-ID`, `X-Actor-Credential`, `X-API-Key`, and `Business-ID`
  headers unchanged
- Not rewrite `/api/v1/control/` paths (control plane is key-gated)
- Enforce its own rate limits in addition to NEXUS's per-IP rate limits

No SSRF protection bypass: provider base URLs are fixed at launch and model
output cannot change them.

---

## 11. What is NOT implemented (do not plan around these)

- Online/live retention (retention is offline and quiescence-required)
- Scheduled retention, startup pruning, disk-pressure cleanup triggers
- Multi-instance metrics aggregation or coordination
- Hosted monitoring, alerts, dashboards, Prometheus/OpenTelemetry export
- Container images, Kubernetes manifests, systemd units, or deployment
  automation
- Real model-provider integration in CI (deterministic providers only)
- Disaster recovery beyond the durable store (running executions, approval
  snapshots, cancellation state, escalation state, event buffers, and traces
  are process-local and lost on restart)
- Signing of release artifacts (checksums detect corruption, not tampering)

---

## 12. Operator checklist

Before starting NEXUS in production:

- [ ] `storage.data_dir` is on a local durable single-writer filesystem (not NFS/SMB)
- [ ] `storage.data_dir` permissions are `0700` (or will be created as such)
- [ ] `NEXUS_ENVIRONMENT=production`
- [ ] `NEXUS_SECURITY_REQUIRE_AUTHENTICATION=true`
- [ ] `NEXUS_SECURITY_ENFORCE_BUSINESS_SCOPE=true`
- [ ] `NEXUS_SECURITY_AUDIT_ENABLED=true`
- [ ] `NEXUS_SECURITY_SANDBOX_ENABLED=true`
- [ ] `NEXUS_SECURITY_DEV_ALLOW_UNSAFE_OVERRIDES=false`
- [ ] `models.providers` contains at least one real provider (not simulated)
- [ ] Each provider entry has `id`, `endpoint` (https), `model_id`, `api_key_env`
- [ ] `NEXUS_CONTROL_API_KEY` is set to a long random value
- [ ] `NEXUS_BOOTSTRAP_CREDENTIAL` is set to a long random value (first install only)
- [ ] Reverse proxy configured for TLS termination (if exposed beyond loopback)
- [ ] Backup and restore procedures tested against a disposable data directory
- [ ] Rollback procedure documented (previous binary retained)

---

## 13. What a backup does and does not recover

**Recovered:** identities, businesses, divisions, credentials (hashes only),
memberships, agent definitions, durable memory, execution journal records.

**Not recovered** (process-local, lost on restart): running executions and
in-memory results, approval/resume snapshots, cancellation state, escalation
state, runtime event buffers, execution traces.

Do not describe a store backup as a full-system snapshot.
