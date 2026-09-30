# NEXUS — HTTP Gateway

This document covers the HTTP Gateway implementation.

---

## What It Is

The HTTP Gateway (`internal/gateway/`) is the external-facing API layer that makes the Core Runtime reachable via HTTP. It provides REST endpoints for submitting requests, querying results, health checks, and Server-Sent Events for real-time streaming.

### Where it listens

The gateway binds `<health-host>:<health-port+1>` by default — with the stock config that is `127.0.0.1:8081`, because the health server owns `8080` first (`cmd/nexus/main.go` `defaultAddr`). Override it with `-http-addr`. The health endpoints (`/health`, `/readiness`) are served by the health server on the health port, not by the gateway; the gateway serves its own `/health` and `/ready`.

```bash
BASE=http://127.0.0.1:8081
curl -fsS "$BASE/ready"
```

### Authentication and business scope

Identity enforcement is **on by default** (`security.require_authentication`,
`security.enforce_business_scope`) — an unauthenticated call to any scoped
endpoint gets `401`, and a caller outside the requested `business_id` gets
`403`. Identify yourself on every call with either:

```http
X-Actor-ID: nx:human:bootstrap
X-Actor-Credential: <secret>
```

or `Authorization: Basic base64(actor_id:credential)`.

On a fresh install the only credential that exists is the one you supply at
boot (nothing in the API can create the *first* membership):

```bash
export NEXUS_BOOTSTRAP_CREDENTIAL=$(openssl rand -hex 32)
go run ./cmd/nexus          # provisions nx:human:bootstrap in business "default"
```

`business_id` is a **required** query/body parameter on every scoped endpoint:
missing → `400 VALIDATION`, not a member → `403 AUTHORIZATION`.


---

## Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` | Liveness check — always 200 if server running |
| `GET` | `/ready` | Readiness check — 200 if engine running, 503 otherwise |
| `GET` | `/status` | Engine status (CREATED, RUNNING, DRAINING, STOPPED) |
| `POST` | `/api/v1/requests` | Submit a new request |
| `GET` | `/api/v1/requests/{id}` | Get request result (`202 pending` while running) |
| `POST` | `/api/v1/requests/{id}/cancel` | Cancel an in-flight request (E-005) |
| `GET` | `/events` | SSE stream of events |

### POST /api/v1/requests

```json
{
  "intent": "owner's intent",
  "business_id": "biz-1",
  "actor_id": "user-1",
  "priority": 5,
  "constraints": ["budget:1000"]
}
```

Response: `202 Accepted`
```json
{
  "request_id": "api-1234567890",
  "correlation_id": "api-1234567890",
  "status": "accepted"
}
```

### GET /api/v1/requests/{id}

Query parameters:

- `business_id` (required) — authorization scope. Missing → `400 VALIDATION` (fail closed).

Authorization mirrors the cancel path: identity middleware, then identity-bound
membership in `business_id`, then a scope match against the request's recorded
business (`403 AUTHORIZATION`).

| Status | Category | Body |
|---|---|---|
| `200` | — | the terminal result (`completed` / `failed` / `cancelled`) |
| `202` | — | `{"request_id","correlation_id","status":"pending"}` |
| `404` | `VALIDATION` | unknown request id |

`202 pending` is returned while the request is admitted but has not reached a
terminal state. A result is only stored when the chain finishes, so without
this the whole run window answered `404` — indistinguishable from a typo'd id
on the endpoint clients are told to poll. `404` therefore means the id was
never admitted (or its admission was rejected). Poll until `200`.

When the chain-gate governance decision was `ALLOW_WITH_CONSTRAINTS`, the
terminal result carries `constraints` — `["type:expression", ...]`, e.g.
`["budget:1000"]` — matching CORE_INTERFACE_CONTRACTS §4.2's decision output
and the SCHEMA_GOVERNANCE decision record. The values are **reported, not
enforced**: constraint enforcement is a governance-execution milestone. Every
other outcome omits the field.

### POST /api/v1/requests/{id}/cancel

External cancellation of an in-flight request. No body.

Query parameters:

- `business_id` (required) — authorization scope. Missing → `400 VALIDATION` (fail closed).

Authorization mirrors `GET /api/v1/requests/{id}`: identity middleware (path is
scope-protected → `401 UNAUTHORIZED` without credentials when enforcement is
on), identity-bound membership in `business_id` (`403 AUTHORIZATION`), then
core ownership (`403` foreign scope, `404` unknown request). It is an
identity-scoped API path — a control API key is **not** required, and no
governance re-evaluation happens at this boundary.

Response: `202 Accepted`
```json
{
  "request_id": "req-123",
  "correlation_id": "req-123",
  "status": "cancelling"
}
```

`202` means cancellation was **accepted**, not completed. The final state is
observed via `GET /api/v1/requests/{id}` (usually `cancelled`, but a request
that reaches a terminal state first returns its real state).

| Status | Category | When |
|---|---|---|
| `400` | `VALIDATION` | `business_id` missing |
| `401` | `UNAUTHORIZED` | enforcement on, no credentials |
| `403` | `AUTHORIZATION` | not a member of `business_id`, or foreign scope |
| `404` | `VALIDATION` | unknown request |
| `409` | `CONFLICT` | already in a terminal state (`completed`/`failed`) |

Repeat cancels are idempotent: an already-cancelled request returns `202`
again (never `404`/`409`).

### Headers

- `X-Correlation-ID`: Custom correlation ID (optional, auto-generated if not provided)

---

## Layout

```
internal/gateway/
  server.go        HTTP server, routes, handlers
  server_test.go   15 tests (TEST-GW-001..015)
```

---

## Testing

- 15 tests covering health, ready, status, submit, get result, validation, error handling
- 11 cancellation tests (`TestCancel*`): 400/401/403/404/409 paths, 202
  acceptance + final state via GET, idempotent repeat, no control API key,
  additive `executor.cancelled` metrics, unauthenticated attribution
- Core Runtime tests unchanged and passing
- Foundation M0–M11 tests unchanged and passing
- Race detector clean
