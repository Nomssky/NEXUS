# NEXUS — Release & Rollback Guide

Companion to `docs/operations-runbook.md` (backup/restore, rollback, secrets).
This guide covers the local release build, CI enforcement, artifact verification,
and the isolated smoke test. It does **not** cover publishing, deployment, or
signing — none of which exist in this repository.

## 0. Scope and non-claims

A green CI run and a verified artifact set prove that the source **compiles, the
unit/race suites pass, the store backup/restore round-trips, and the artifact
checksums match**. They do **not** prove:

- real model-provider integration (CI uses deterministic providers only);
- multi-instance correctness, distributed rate limiting, or load capacity;
- disaster recovery beyond the durable store (see §5);
- production deployment readiness, TLS termination, or reverse-proxy setup.

## 1. Required local tools

- **Go 1.27.1** (see `go.mod`; CI uses `go-version-file: go.mod`). The project
  has **zero third-party Go dependencies** (standard library only).
- `bash`, `coreutils` (`sha256sum`, `find`, `mktemp`), `curl`, and `python3`
  (only to allocate a free port in the smoke test).
- For the E2E suite: Node/npm and Playwright (see §3).

## 2. Building

Development build (version `0.0.0-dev`, commit from git or `unknown`):

```bash
make build          # → bin/nexus
make build-backup   # → bin/nexus-backup
```

Both use `-trimpath` so no local absolute paths are embedded. Build metadata is
injected via `-ldflags` into
`internal/foundation/version.{Version,Commit}`; `unknown`/`0.0.0-dev` are
honest defaults when Git metadata is unavailable. Report it with:

```bash
bin/nexus -version          # nexus <version> (<commit>)
bin/nexus-backup -version   # nexus-backup <version> (<commit>)
```

## 3. CI checks (`.github/workflows/ci.yml`)

Least privilege (`contents: read`); no publishing, no deployment. Jobs:

| Job | Checks |
|---|---|
| `quality` | `gofmt`, `go vet ./...`, `git diff --check`, `go build ./...`, `go test -count=1 ./...`, `go test -race -count=1 ./...`, backup/restore tests, production provider & storage gates, rate-limit/CORS/security-header tests |
| `release` | `make release VERSION=0.0.0-ci` then `make release-verify VERSION=0.0.0-ci` |
| `smoke` | `make smoke` (isolated backup/restore round-trip) |
| `e2e` | Playwright black-box suite against the compiled binary (deterministic providers only) |

Notes and caveats:

- Actions are referenced by **major tag** (`actions/checkout@v4`,
  `actions/setup-go@v5`, `actions/setup-node@v4`), matching the repository's
  existing convention. Pinning to immutable commit SHAs is a recommended
  follow-up that requires the maintainers' chosen SHAs.
- **Hosted execution of this workflow has not been run** in this environment.
  The commands were validated locally; a green local run is not a hosted-CI
  result.
- The current working tree contains **uncommitted** work; CI only ever
  validates what is committed. CI changes take effect for future commits.
- The E2E job assumes the committed tree is self-consistent (it does not rely on
  real provider credentials, GitHub, or external web services — the suite uses
  scripted/deterministic providers and loopback fixtures).

## 4. Release artifacts

```bash
make release VERSION=1.2.3      # → dist/nexus-1.2.3-<os>-<arch>/ + dist/SHA256SUMS
make release-verify [VERSION=…] # checksums + manifest + inventory (no execution)
```

Output (default `dist/`, overridable with `DIST=`):

```
dist/
  nexus-<version>-<os>-<arch>/
    nexus                      the application
    nexus-backup               the backup/restore operator tool
    operations-runbook.md      recovery/rollback guidance
    release-manifest.txt       manifest (see below)
  SHA256SUMS                   SHA-256 of every file above (relative paths)
```

Manifest fields (relative names only; **no absolute paths, no secrets**):
`format`, `name`, `version`, `commit`, `tree_state` (`clean`/`dirty`),
`go_version`, `goos`, `goarch`, `built_at`, and the artifact list.

- Only the host `GOOS/GOARCH` is built and validated. No other platform is
  claimed.
- The build **fails on any compile error** — a stale binary is never packaged.
- `dist/` is git-ignored and treated as tool-owned; `release.sh` recreates it
  only when it is empty or carries a prior `SHA256SUMS`. It never runs
  `git clean` and never touches the live data directory.
- The release directory contains only the four files above — never `.env`
  files, credentials, `workspaces/`, persistent data, or the frontend's local
  build output.
- `nexus-backup` is compiled from the current source with the same
  version/commit metadata, so its backup/verify/restore commands are always
  current and its safety contracts (staged restore, empty-destination
  requirement, no partial publish) are unchanged.

### Source revision and dirty-tree status

`release-manifest.txt` records the full `commit` and `tree_state`. A build from
a **dirty** tree prints a warning and is **not** an official release. Determine
the revision directly with `git rev-parse HEAD` / `git status --porcelain`.

### Reproducibility

With the same `version`, `commit`, and Go toolchain, the **binaries are
bit-for-bit reproducible** (verified: two builds compared byte-for-byte with
`-trimpath`). The manifest embeds `built_at`, so the manifest and `SHA256SUMS`
are **not** byte-identical across builds; only the binaries are.

### Verification

`make release-verify` recomputes and checks `SHA256SUMS`, validates the manifest
fields, and confirms the expected files exist. It does **not** execute the
binaries. Checksums detect accidental corruption; they are **not** a signature
and do not authenticate a publisher. **No signing is implemented.**

## 5. Isolated smoke test

```bash
make smoke
```

Uses `mktemp -d` and a dynamically allocated free port; it never points at a
real/developer data directory and never uses a hard-coded port. Steps: build
both binaries (current source) → print version metadata → boot NEXUS on isolated
config/data → confirm `/health` → stop the process → `nexus-backup backup` →
`verify` → `restore` into a fresh directory → reopen the restored store → verify
`dist/SHA256SUMS` if present → remove only its own temp dir and process. A
non-zero exit means a step failed; the trap cleans up.

## 6. Rollback

Two independent rollbacks; keep them separate:

- **Binary rollback:** retain the previous `nexus` (and `nexus-backup`) binary
  before deploying a new one, and redeploy the old binary. Safe as long as the
  old binary can read the current `storage.data_dir` format (`filestore-json/v1`).
- **Data rollback:** restore a pre-change backup into a **new** directory and
  cut over (see `docs/operations-runbook.md` §3).

There is **no migration framework** and no automatic schema migration; the store
format is a single versioned JSON layout. Restoring an older data backup **after
newer writes have occurred discards those newer writes** — it is not a merge.

## 7. What a durable-store backup does and does not recover

Recovered: durable store records (identities, businesses, divisions,
credentials as hashes, memberships, agent definitions, durable memory).

**Not recovered** (process-local, lost on restart): running executions and their
in-memory results, approval/resume snapshots, cancellation state, escalation
state, runtime event buffers and execution traces. Do not describe a store
backup as a full-system snapshot.

## 8. Manual vs automated

| Step | Automated? |
|---|---|
| Format, vet, unit/race tests, backup & gate tests | **Yes** — CI `quality` |
| Release artifact build + checksum/manifest verification | **Yes** — CI `release`, `make release`/`release-verify` |
| Isolated backup/restore round-trip | **Yes** — CI `smoke`, `make smoke` |
| Black-box E2E (deterministic providers) | **Yes** — CI `e2e` (hosted run unverified here) |
| Choosing `VERSION`, committing, tagging, publishing, deploying | **Manual** — out of scope; nothing is published or deployed by this tooling |
| Real-provider configuration, TLS/proxy, secret provisioning | **Manual** — see `docs/operations-runbook.md` |