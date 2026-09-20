# NEXUS — HTTP Gateway

This document covers the HTTP Gateway implementation.

---

## What It Is

The HTTP Gateway (`internal/gateway/`) is the external-facing API layer that makes the Core Runtime reachable via HTTP. It provides REST endpoints for submitting requests, querying results, health checks, and Server-Sent Events for real-time streaming.

---

## Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` | Liveness check — always 200 if server running |
| `GET` | `/ready` | Readiness check — 200 if engine running, 503 otherwise |
| `GET` | `/status` | Engine status (CREATED, RUNNING, DRAINING, STOPPED) |
| `POST` | `/api/v1/requests` | Submit a new request |
| `GET` | `/api/v1/requests/{id}` | Get request result |
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
- Core Runtime tests unchanged and passing
- Foundation M0–M11 tests unchanged and passing
- Race detector clean
