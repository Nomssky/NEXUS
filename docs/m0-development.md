# NEXUS — Development (M0 Foundation)

This document covers **only** the M0 foundation: how to build, run, test, and
lint the repository. It does not duplicate architecture or contract docs.

> M0 is foundation only. There is **no** autonomous intelligence yet: no
> Objective Engine, Decision Engine, Planner, Workflow, Agent Runtime, Model
> Router, Tool Runtime, providers, or autonomous loops.

---

## Prerequisites

- **Go 1.27+** (`go version`)
- `make` (optional; every target is a thin wrapper over `go`)

No third-party dependencies are required. The module is standard-library only.

---

## Repository layout

```
cmd/nexus/                     process entrypoint
internal/foundation/
  app/                         bootstrap wiring (config -> logging -> health -> lifecycle)
  config/                      C01 configuration foundation
  logging/                     structured logging foundation (+ secret redaction)
  nerrors/                     canonical error envelope foundation
  lifecycle/                   process lifecycle foundation
  health/                      liveness/readiness foundation
  version/                     build identity
  guard/                       locked-layer guard tests
configs/nexus.example.json     example (public) configuration
```

This maps to the blueprint's **C01 Foundation & Config** (layer L0).

---

## Install / build

```sh
make build          # -> ./bin/nexus
# or
go build ./...
```

## Run

```sh
make run
# or
go run ./cmd/nexus
# with a config file:
go run ./cmd/nexus -config configs/nexus.example.json
```

Print version:

```sh
go run ./cmd/nexus -version
```

## Test (canonical command)

```sh
make test           # go test ./...
```

Other useful targets:

```sh
make test-race      # race detector
make cover          # coverage summary
```

## Lint / format / static checks

```sh
make fmt            # gofmt -w .
make fmt-check      # fail if not gofmt-clean
make vet            # go vet ./...
make check          # fmt-check + vet + build + test
```

---

## Configuration basics

Configuration precedence is:

```
defaults  <  file (JSON)  <  environment
```

- Config file: JSON, loaded with `-config <path>`. Unknown fields are rejected.
- Environment variables (non-secret operational settings only):

| Variable | Meaning | Default |
|---|---|---|
| `NEXUS_ID` | installation id | `nx:nexus:local` |
| `NEXUS_ENVIRONMENT` | `development`/`staging`/`production` | `development` |
| `NEXUS_LOG_LEVEL` | `debug`/`info`/`warn`/`error`/`fatal` | `info` |
| `NEXUS_LOG_FORMAT` | `json`/`text` | `json` |
| `NEXUS_HEALTH_ENABLED` | enable health/readiness surface | `true` |
| `NEXUS_HEALTH_HOST` | bind host (safe default: loopback) | `127.0.0.1` |
| `NEXUS_HEALTH_PORT` | bind port | `8080` |
| `NEXUS_SHUTDOWN_TIMEOUT_SECONDS` | graceful drain budget | `30` |

### Public vs secret configuration

- **Public configuration** (the table above) may be set in files or environment.
- **Secret configuration** (API keys, passwords, tokens) is **never** stored as a
  plain configuration value. The foundation defines a `SecretRef` (a pointer such
  as `vault:nexus/provider/openrouter`) whose resolution is deferred to M1.
  Secrets are never logged; secret-looking log fields are redacted.

### Startup validation

Invalid or missing required configuration causes a **safe failure**: the process
does not start, prints a secret-free diagnostic to stderr, and exits non-zero.
No partial application state is created.

---

## Startup and shutdown

Startup sequence:

```
START -> INITIALIZE (config, logging) -> READY -> RUNNING
```

Shutdown (on `SIGINT`/`SIGTERM`, or context cancellation):

```
SHUTDOWN_REQUESTED -> DRAINING -> STOPPED
```

During shutdown the process stops accepting new work, marks itself not-ready,
runs close hooks in reverse order, and exits `0` on a clean stop (`1` if a close
hook fails).

### Health and readiness

| Endpoint | Meaning |
|---|---|
| `GET /health` | liveness — the process is alive |
| `GET /readiness` | readiness — the system is ready to accept work |

Readiness is **not** claimed merely because the process is running: it is `503`
until initialization completes and whenever a registered dependency check fails.

---

## Deferred to later milestones

Recorded here, intentionally **not** implemented at M0:

- `DEFERRED → M1` : secret store / `SecretRef` resolution (C02/C04)
- `DEFERRED → M1` : full configuration control plane (scoping, versioning,
  approval, rollback, drift, hot reload, feature flags)
- `DEFERRED → M3` : persistence, event substrate, observability/audit stores
- `DEFERRED → M4+` : all cognitive and execution components
