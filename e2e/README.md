# NEXUS black-box E2E suite

A Playwright suite that exercises the **real compiled `cmd/nexus` binary** over
HTTP only. Nothing here imports Go packages, mocks a dependency, or reaches into
the gateway's internals: every assertion is made against what a client can
actually observe on the wire.

## Prerequisites

| Requirement | Why |
| --- | --- |
| Go toolchain (as used to build this repo) | `npm test` compiles `./cmd/nexus` on first run |
| Node.js ≥ 18 (tested on 26.x) | Playwright + `fetch` used by the harness |
| `npm install` inside `e2e/` | installs `@playwright/test` |

No browser download is needed — the suite is API-only, so
`npx playwright install` may be skipped entirely.

## Running

```bash
cd e2e
npm install          # first time only
npx playwright test  # or: npm test
npx playwright show-report   # HTML report from the last run
```

Useful variables:

| Variable | Effect |
| --- | --- |
| `E2E_KEEP=1` | keep the per-run `NEXUS_DATA_DIR` under `e2e/.tmp/` for inspection |
| `DEBUG=1` | Playwright's own debug output for the harness |

## How the harness works (`fixtures/nexus.ts`)

1. **Build once per worker.** `go build -o e2e/.bin/nexus ./cmd/nexus` is run
   lazily the first time a worker needs it; the resulting binary is the only
   thing the tests ever talk to.
2. **Reserve a real port pair.** `defaultAddr` binds the gateway to
   `127.0.0.1:<health port + 1>`, so the harness reserves two consecutive free
   ports rather than relying on `:0` (the launcher logs the *requested* address,
   not the bound one, so ephemeral ports cannot be discovered afterwards).
3. **Start with an explicit environment.** Each instance gets:

   | Variable | Value |
   | --- | --- |
   | `NEXUS_DATA_DIR` | a fresh directory under `e2e/.tmp/run-*` |
   | `NEXUS_HEALTH_HOST` / `NEXUS_HEALTH_PORT` | `127.0.0.1` / reserved health port |
   | `NEXUS_BOOTSTRAP_CREDENTIAL` | a random per-instance secret |
   | `NEXUS_BOOTSTRAP_BUSINESS` | `default` |
   | `NEXUS_CONTROL_API_KEY` | a random per-instance control key |
   | `NEXUS_LOG_FORMAT` / `NEXUS_LOG_LEVEL` | `json` / `info` |
   | `NEXUS_ENVIRONMENT` | `development` |

   The bootstrap identity (`nx:human:bootstrap`), its credential and its
   membership of `default` are therefore provisioned by the process itself at
   boot — no test pre-seeds data behind its back.
4. **Wait for real readiness.** `GET /ready` is polled until it returns
   `200 {"status":"ready"}`, which the gateway only serves while the engine is
   `RUNNING`. Startup failures dump the captured stdout/stderr.
5. **Teardown.** `SIGTERM`, then `SIGKILL` after 15s; the temp data directory
   is removed unless `E2E_KEEP=1`.

## Discovered HTTP contract

Endpoints are registered in `internal/gateway/server.go`. The chain is
`identityMiddleware(authMiddleware(envelopeRoutes()))`.

| Area | Contract |
| --- | --- |
| Auth | `X-Actor-ID` + `X-Actor-Credential`, or `Authorization: Basic base64(id:cred)`. Because SCHEMA §7 ids are `{nx}:{entity_type}:{unique}` and credentials are opaque, the Basic payload is split at **every** colon and each candidate is verified (see *Findings*). Missing → `401 UNAUTHORIZED` / category `AUTH` / `authentication required`; bad → `401 UNAUTHORIZED` / `AUTH` / `invalid credentials` (unknown id and wrong credential are byte-identical). Non-member or mismatched `actor_id` → `403 AUTHORIZATION` / category `AUTHORIZATION`. |
| Control auth | `X-API-Key`. Missing/wrong → `401 UNAUTHORIZED` / `AUTH`. |
| Submit | `POST /api/v1/requests` `{intent, business_id, actor_id, priority?, constraints?}` → `202 {request_id, correlation_id, status:"accepted", actor_id}`; `request_id == correlation_id` unless `X-Correlation-ID` is supplied (and then it is honoured). Missing fields → `400 VALIDATION`. While the engine is stopped → `503 RESOURCE_UNAVAILABLE` (never `202`). |
| Result | `GET /api/v1/requests/{id}?business_id=` → `202 {request_id, correlation_id, status:"pending"}` while running, `200 {request_id, business_id, status, outcome?, error?, audit_trace, duration}` when terminal, `404` code `VALIDATION` for an unknown id, `403` for a foreign scope. Missing `business_id` → `400 VALIDATION`. |
| Errors | `{"error":{code, category, message, details?, retryable, correlation_id, timestamp}}` plus an `X-Correlation-ID` response header matching the body. Categories are the closed set of 14 in CORE §3 — there is no `NOT_FOUND`; a 404 carries code `VALIDATION`. |
| Route errors | Unrouted path → `404` code `VALIDATION`, message `no such endpoint: <METHOD> <path>`. Wrong method → `405` code `METHOD_NOT_ALLOWED`, message `method <METHOD> not allowed for <path>` (the mux's own text), keeping the mux's `Allow` header. |
| Control | `GET /api/v1/control/status` → `200 {status, uptime, components{engine,…}, request_count}`. `POST …/pause` → `200 {status:"paused"}`; repeat → `409 ALREADY_PAUSED`. `POST …/resume` → `200 {status:"resumed"}`; repeat → `409 ALREADY_RUNNING`. While paused: `GET /ready` → `503`, submit → `503 RESOURCE_UNAVAILABLE`. |
| Cancel | `POST /api/v1/requests/{id}/cancel?business_id=` → `202 {request_id, correlation_id, status:"cancelling"}`; repeat on a `cancelled` result → `202` (idempotent); on a `completed` result → `409 CONFLICT`; unknown id → `404 VALIDATION`; foreign scope on a known id → `403`; missing `business_id` → `400`. |
| SSE | `GET /events?business_id=` → `200 text/event-stream`, frames `event: <type>\ndata: <json>\n\n`. Missing `business_id` → `400`; non-member → `403`. Frames are the SCHEMA §2.2 Event Record (`schema_version`, `entity_type:"event"`, `event_id`, `event_type`, `nexus_id`, `business_id`, `occurred_at`, `emitted_at`, `producer{…}`, `correlation_id`, `payload`). `chain.*` events carry `correlation_id == request id` and `event_id == "<request id>-<event type>"`. |
| Identity listing | `GET /api/v1/identities?business_id=` returns identities whose **record** is scoped to that business. The bootstrap record is global-scope (it is created before any business exists) so it is absent from its own business's listing while still being a member of it; it is individually readable at `GET /api/v1/identities/nx:human:bootstrap`. |
| Policy control | `GET/PUT/DELETE /api/v1/control/policies[/{id}]`, gated by `X-API-Key` (no identity, no membership). The body *is* the §2 Policy Record: `effect` is the enum name (`ALLOW`, `DENY`, `REQUIRE_APPROVAL`, `ALLOW_WITH_CONSTRAINTS`, `ESCALATE`), and server-derivable §2.2 fields (`schema_version`, `entity_type`, `nexus_id`, `created_at`, `created_by`, `effective_from`, `policy_version`, `provenance`) are stamped by the gateway. Missing/invalid required field → `400 VALIDATION`. Unknown id → `404 VALIDATION`. The seeded `default-allow` is read-only → `409 CONFLICT`. Policies are process-lifetime state: a restart reloads only the built-in. |
| Governance decisions | A `DENY` reaches the client as a terminal `failed` whose `error` carries code/category `POLICY_DENIED`, `chain_step: "governance"`, `retryable: false` and a message naming the matched policy (`matched policy <id> (v<version>, precedence <n>)` — that is how the winning rule is observed). `ALLOW_WITH_CONSTRAINTS` is the only allowing outcome that leaves a mark: the terminal result carries `constraints: ["type:expression"]`, reported and not enforced. A policy pinned to another `business_id` never matches, and `status: "disabled"` is never active. |
| Persistence | File store writes `<NEXUS_DATA_DIR>/identity/<id>.json` and `<NEXUS_DATA_DIR>/business/<id>.json`; the bootstrap identity, credential and membership are re-armed on every boot from the environment. |

## Coverage

| Area | Spec |
| --- | --- |
| AUTH | `tests/01-auth.spec.ts` |
| HAPPY PATH | `tests/02-lifecycle.spec.ts` |
| RESULT POLLING | `tests/02-lifecycle.spec.ts` (202 pending → 200 completed) |
| SCOPE ISOLATION | `tests/03-scope.spec.ts` |
| UNKNOWN RESOURCE | `tests/04-errors.spec.ts` |
| METHOD/ROUTE ERROR | `tests/04-errors.spec.ts` |
| PAUSE/RESUME | `tests/05-control.spec.ts` |
| SSE | `tests/06-sse.spec.ts` |
| CANCEL | `tests/07-cancel.spec.ts` |
| RESTART/PERSISTENCE | `tests/08-restart.spec.ts` |
| GOVERNANCE | `tests/09-governance.spec.ts` |

The core executes admitted requests serially, so the pending-state, cancel and
"SSE outlives the 30s write timeout" assertions are deterministic rather than
timing luck.

## Findings

* **Basic authentication was broken for colon-delimited identities (fixed).**
  `Authorization: Basic base64(actor_id:credential)` split the decoded value on
  the *first* colon, so `nx:human:bootstrap:<credential>` authenticated as
  identity `nx` — the default, only pre-provisioned identity could never use
  the documented Basic form (`docs/http-gateway.md`), and it answered `401
  invalid credentials` while the identical `X-Actor-ID`/`X-Actor-Credential`
  pair succeeded. Ids are `{nx}:{entity_type}:{unique}` (SCHEMA §7) and carry
  colons by design, while credentials are opaque and may carry them too, so no
  single position is a safe separator. `extractActorCredentials` now returns
  every colon split as a candidate (bounded by `maxBasicAuthSplits`), and
  `identityMiddleware` verifies them until one succeeds; the failure envelope
  is unchanged, so the F11 unknown-id / wrong-credential indistinguishability
  still holds. Regression tests:
  `TestBasicAuthColonDelimitedIdentity`, `TestBasicAuthWrongCredentialStill401`.

## Known limitations (recorded, not "fixed" by this suite)

* **Client constraints are still not observable.** `constraints` in the submit
  body reaches the executor, but `Response.constraints` is only populated from
  a governance decision of `ALLOW_WITH_CONSTRAINTS`. Installing a policy is now
  possible (`/api/v1/control/policies`, covered by `09-governance.spec.ts`),
  but the *client-supplied* list has no corresponding decision output, and
  constraint *enforcement* remains out of scope — the decision's values are
  reported, never applied.
* **No `failed` terminal status is reachable through execution with the default
  configuration.** Every field `chainValidate` checks is validated by the
  gateway before admission, `findAgent` provisions an agent on demand, and the
  launcher seeds `simulated:default` when the model list is empty, so a
  well-formed request always completes — unless governance denies it, which
  `09-governance.spec.ts` covers. Admission-level rejection is covered instead
  (`503` while paused, `409` on a terminal request). The no-false-success
  invariant for a *provider* failure is still covered by Go tests only; there
  is no configuration surface that makes a provider fail deterministically.
* **Credentials registered through the API do not survive a restart.** The
  local authenticator is in-memory only; only the bootstrap identity is
  re-armed at boot. Their *records* do persist (asserted), their credentials
  do not.
* **Memberships are also re-armed only for bootstrap**, for the same reason.
* **Approvals and escalations are not covered.** Their endpoints exist and are
  contract-defined; this suite does not yet drive them.
* **Divisions are not covered** — the submit body carries no division field, so
  a division-pinned policy can never match a request raised through the public
  API and there is nothing observable to assert.
* **No coverage of:** TLS/mTLS, multi-instance operation, rate limiting,
  the `failed`/oversized-response (`413 RESOURCE_LIMIT`) path, SSE client
  capacity (`503 RESOURCE_LIMIT`), and `GET /api/v1/control/{metrics,components}`.

## Conventions

* Tests run serially (`fullyParallel: false`, one worker) against one gateway
  instance per worker; specs are ordered `01`…`09`.
* Timeouts are bounded everywhere; on failure the harness prints the request
  id, the last response body and the captured NEXUS stdout/stderr so a red run
  is diagnosable without reproducing it by hand.
* Assertions are on contract shape, never on implementation details: no
  internal Go symbols, no fixtures injected behind the API.
