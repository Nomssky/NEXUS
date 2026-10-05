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

`./manual-probes.sh` is a separate, Playwright-free record: it boots the same
binary on a throwaway data directory and drives it with `curl`/`jq` only. It
exists for the surfaces this suite does not automate (control
`metrics`/`components`, division record read and transitions) and for the
Phase 14 wire spot-check; results are recorded in
`docs/PLATFORM_INTEGRITY_AUDIT.md` §9.

Useful variables:

| Variable | Effect |
| --- | --- |
| `E2E_KEEP=1` | keep the per-run `NEXUS_DATA_DIR` under `e2e/.tmp/` for inspection |
| `DEBUG=1` | Playwright's own debug output for the harness |
| `PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1` | skip the browser download: the suite is API-only and never opens a page |

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
   boot — no test pre-seeds data behind its back. `startNexus({extraEnv})`
   merges additional variables over this table for configuration that only
   exists at boot (`11-provider-failure.spec.ts`); the harness still owns the
   credentials.
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
| Submit | `POST /api/v1/requests` `{intent, business_id, actor_id, division_id?, priority?, constraints?}` → `202 {request_id, correlation_id, status:"accepted", actor_id}`; `request_id == correlation_id` unless `X-Correlation-ID` is supplied (and then it is honoured). Missing fields → `400 VALIDATION`. While the engine is stopped → `503 RESOURCE_UNAVAILABLE` (never `202`). `division_id` is optional (CTR-AUTH-001): unknown → `400` `division not found`, not a division of `business_id` → `400` `division does not belong to the requested business`, membership not covering it → `403 AUTHORIZATION` `access denied: actor is not a member of the requested division`, registry claimed but not wired → `503 DEPENDENCY_FAILURE`. Omitting it submits at business scope. |
| Result | `GET /api/v1/requests/{id}?business_id=` → `202 {request_id, correlation_id, status:"pending"}` while running, `200 {request_id, business_id, division_id?, status, outcome?, error?, audit_trace, duration}` when terminal (`division_id` echoed only when the request recorded one), `404` code `VALIDATION` for an unknown id, `403` for a foreign scope and, when the caller's membership does not cover the recorded division, `403` `access denied: division scope mismatch`. Missing `business_id` → `400 VALIDATION`. |
| Errors | `{"error":{code, category, message, details?, retryable, correlation_id, timestamp}}` plus an `X-Correlation-ID` response header matching the body. Categories are the closed set of 14 in CORE §3 — there is no `NOT_FOUND`; a 404 carries code `VALIDATION`. |
| Route errors | Unrouted path → `404` code `VALIDATION`, message `no such endpoint: <METHOD> <path>`. Wrong method → `405` code `METHOD_NOT_ALLOWED`, message `method <METHOD> not allowed for <path>` (the mux's own text), keeping the mux's `Allow` header. |
| Control | `GET /api/v1/control/status` → `200 {status, uptime, components{engine,…}, request_count}`; `uptime` is a Go duration string (`1m30.5s`) measured from gateway construction on the server clock and clamped at `0`. `POST …/pause` → `200 {status:"paused"}`; repeat → `409 ALREADY_PAUSED`. `POST …/resume` → `200 {status:"resumed"}`; repeat → `409 ALREADY_RUNNING`. While paused: `GET /ready` → `503`, submit → `503 RESOURCE_UNAVAILABLE`. |
| Cancel | `POST /api/v1/requests/{id}/cancel?business_id=` → `202 {request_id, correlation_id, status:"cancelling"}`; repeat on a `cancelled` result → `202` (idempotent); on a `completed` result → `409 CONFLICT`; unknown id → `404 VALIDATION`; foreign scope on a known id → `403`; recorded division the caller's membership does not cover → `403` `access denied: division scope mismatch` (checked against the recorded scope, so in-flight work is covered too); missing `business_id` → `400`. |
| SSE | `GET /events?business_id=` → `200 text/event-stream`, frames `event: <type>\ndata: <json>\n\n`. Missing `business_id` → `400`; non-member → `403`. Frames are the SCHEMA §2.2 Event Record (`schema_version`, `entity_type:"event"`, `event_id`, `event_type`, `nexus_id`, `business_id`, `occurred_at`, `emitted_at`, `producer{…}`, `correlation_id`, `payload`). `chain.*` events carry `correlation_id == request id` and `event_id == "<request id>-<event type>"`. |
| Identity listing | `GET /api/v1/identities?business_id=` returns identities whose **record** is scoped to that business. The bootstrap record is global-scope (it is created before any business exists) so it is absent from its own business's listing while still being a member of it; it is individually readable at `GET /api/v1/identities/nx:human:bootstrap`. |
| Policy control | `GET/PUT/DELETE /api/v1/control/policies[/{id}]`, gated by `X-API-Key` (no identity, no membership). The body *is* the §2 Policy Record: `effect` is the enum name (`ALLOW`, `DENY`, `REQUIRE_APPROVAL`, `ALLOW_WITH_CONSTRAINTS`, `ESCALATE`), and server-derivable §2.2 fields (`schema_version`, `entity_type`, `nexus_id`, `created_at`, `created_by`, `effective_from`, `policy_version`, `provenance`) are stamped by the gateway. Missing/invalid required field → `400 VALIDATION`. Unknown id → `404 VALIDATION`. `PUT` is an upsert on `policy_id` (a second `PUT` replaces the effect and keeps the record readable back); `DELETE` → `200 {policy_id, deleted:true}`, a repeat or a deleted id → `404 VALIDATION`. The seeded `default-allow` is read-only → `409 CONFLICT`. Policies are process-lifetime state: a restart reloads only the built-in. |
| Governance decisions | A `DENY` reaches the client as a terminal `failed` whose `error` carries code/category `POLICY_DENIED`, `chain_step: "governance"`, `retryable: false` and a message naming the matched policy (`matched policy <id> (v<version>, precedence <n>)` — that is how the winning rule is observed). `ALLOW_WITH_CONSTRAINTS` is the only allowing outcome that leaves a mark: the terminal result carries `constraints: ["type:expression"]`, reported and not enforced. A policy pinned to another `business_id` never matches, and `status: "disabled"` is never active. |
| Approvals | A `REQUIRE_APPROVAL` policy holds the request: terminal `failed` with code/category `APPROVAL_REQUIRED`, `retryable: false` and `error.details.approval_id`. `GET /api/v1/approvals?business_id=` lists only the **pending** records as `{approvals:[{entity_id, requester_id, policy_ref, status:"PENDING", …}]}` (missing `business_id` → `400`, non-member → `403`). `POST /api/v1/approvals/{id}/{approve,deny}?business_id=` requires a non-empty `reason` (`400 VALIDATION`, the §6.2 `decision_rationale`); `approve` → `202 {approval_id, status:"approved"}` and the original request resumes to `completed`, `deny` → `200 {approval_id, status:"denied"}` with **no** resume (the stored result stays `failed`/`APPROVAL_REQUIRED`). The approver must be a member of `business_id`; with `self_approval_prohibited` the requester is refused `403 AUTHORIZATION` (`core: self-approval prohibited`), and with `approval_config.approver_ids` set a member outside that list is refused `403 AUTHORIZATION` (`core: approver not authorized`). Membership is the read boundary, the policy list is the decision boundary: neither refusal resolves the approval, which stays actionable until the named approver decides. Unknown id → `404 VALIDATION`. |
| Escalations | An `ESCALATE` policy fails the request with code `ESCALATION_REQUIRED`, category `POLICY_DENIED` (CORE §3 has no escalation category), `retryable: false` and `error.details.escalation_ref`; the alert is queued asynchronously from the `governance.escalated` event, so `GET /api/v1/escalations?business_id=` must be polled. The record answers `pending → acknowledged → resolved` (`expired` past `deadline`). `POST /api/v1/escalations/{id}/{ack,resolve}?business_id=` requires non-empty `reasoning` (`400 VALIDATION`) and returns `{escalation_id, status, accepted: true}`; an `ack` that is not `pending` or a `resolve` that is not `pending`/`acknowledged` → `409 CONFLICT`. Unknown id → `404`, foreign scope → `403`. |
| Provider failure | The launcher seeds `simulated:default` + a `simulated` provider when no model is registered. `NEXUS_SEEDED_PROVIDER_STATUS=offline` starts that provider offline (PROVIDER_CONTRACTS §12); admission is unaffected (`202`) but the request ends `200` with `status:"failed"`, `error.code:"EXECUTION_FAILED"`, `error.category:"INTERNAL_FAILURE"`, `error.chain_step:"agent"`, `error.retryable:false`, a message containing `provider invocation failed` … `provider simulated is offline`, `outcome.metrics.executor_status:"failed"` and an audit trace whose `agent` step reads `status=failed`. It is never `completed` and never carries a summary (no false success). `/ready` and the control plane stay up — the gateway did not fail. Any other value for the variable is a `VALIDATION` boot error. |
| Persistence | File store writes `<NEXUS_DATA_DIR>/identity/<id>.json`, `<NEXUS_DATA_DIR>/business/<id>.json`, `<NEXUS_DATA_DIR>/credential/credential:<identity id>.json` and `<NEXUS_DATA_DIR>/membership/membership:<identity id>.json`. The registry, the credential verifiers and the memberships all hydrate on boot and all fail closed on a corrupt record. Record ids are unique store-wide, hence the `<type>:` namespace on the last two. The bootstrap identity's credential is re-armed (overwritten) from `NEXUS_BOOTSTRAP_CREDENTIAL` on every boot; rotating the variable takes effect immediately, unsetting it stops refreshing rather than removing the stored hash. |

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
| RESTART/PERSISTENCE | `tests/08-restart.spec.ts` (records, credential **and** membership across a restart; G4 Level 1: a completed request's id 404s after restart) |
| GOVERNANCE | `tests/09-governance.spec.ts` |
| APPROVAL/ESCALATION | `tests/10-approval.spec.ts` |
| PROVIDER FAILURE | `tests/11-provider-failure.spec.ts` (its own offline gateway; the shared one stays healthy) |
| DIVISION TOPOLOGY | `tests/12-topology.spec.ts` (`division_id` validation/recording, division-scoped membership on submit, read and cancel, division-pinned policy) |
| IDENTITY LIFECYCLE | `tests/13-identity-lifecycle.spec.ts` (suspend/revoke/activate, pending identities, malformed input rollback, duplicate `entity_id`, bootstrap self-lockout recovery, self-mutation) |
| ADMISSION LIFECYCLE | `tests/14-admission.spec.ts` (G2: suspended/archived business/division rejects new submits `409`, reads/cancels continue, `active` reopens) |
| POLICY LIFECYCLE | `tests/09-governance.spec.ts` (PUT upsert, DELETE, default-allow restored after removal) |
| APPROVER AUTHORITY | `tests/10-approval.spec.ts` (`approval_config.approver_ids` refusal vs. self-approval vs. acceptance) |

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
* **With the default configuration a well-formed request still always
  completes.** Every field `chainValidate` checks is validated by the gateway
  before admission, `findAgent` provisions an agent on demand, and the launcher
  seeds `simulated:default` when the model list is empty — so `failed` is
  reachable only by *asking* for it: a governance decision
  (`09-governance.spec.ts`, `10-approval.spec.ts`) or
  `NEXUS_SEEDED_PROVIDER_STATUS=offline` (`11-provider-failure.spec.ts`).
  Admission-level rejection is covered instead (`503` while paused, `409` on a
  terminal request). Nothing here invents a failure: the provider key only
  selects an existing health state, and the process keeps serving.
* **Unsetting `NEXUS_BOOTSTRAP_CREDENTIAL` no longer removes an already-armed
  bootstrap credential.** The variable is still the only *source* for it, and
  every boot with it set overwrites the stored hash; but once a hash is on
  disk a later boot without the variable simply stops refreshing it.
  Revocation is the identity record's status transition, which authentication
  enforces on every call (SCHEMA_IDENTITIES_ORG §10.2). Recorded rather than
  "fixed": it is the direct consequence of making credentials durable.
* **Approval/escalation state is in-memory only.** `10-approval.spec.ts` drives
  both lifecycles over HTTP, but neither the pending approval index nor the
  escalation queue is written to the file store, so a restart loses them (the
  request's stored `failed`/`APPROVAL_REQUIRED` result survives). Deciding an
  already-decided approval also depends on that index: once the resume has
  stored its terminal result the entry is cleaned up and a repeat decision
  answers `404`, not `409`.
* **Identity lifecycle transitions are covered; the authority model behind them
  is not defined by any contract.** `13-identity-lifecycle.spec.ts` drives
  `suspend`, `revoke`, `activate`, the `pending → active` admission path,
  malformed-input rollback and the bootstrap self-lockout recovery (a second
  identity created first, then `activate` restores the original). The
  remaining open question is a *contract* gap, not a test gap: SCHEMA
  §9/§10.2 assigns no authority model to a transition, so the implementation's
  rule — membership in the record's own business, no role check — stands
  unchallenged. Recorded as audit §G1 rather than invented here.
* **Division records are covered at the edge that matters.**
  `12-topology.spec.ts` creates divisions over `POST /api/v1/divisions`, pins
  identities and policies to them, and asserts the narrowing on submit, result
  retrieval and cancel. Single-record `GET`, the duplicate-create `409`, the
  `suspend`/`activate`/`archive` transitions (and `archived` being terminal),
  plus `GET /api/v1/control/{metrics,components}`, are probed by
  `manual-probes.sh` rather than by this suite.
* **Cross-business and foreign-record branches are Go-tested, not
  HTTP-tested.** A caller can only be a member of one business over HTTP
  (`docs/http-gateway.md`: nothing in the API creates the first membership for
  a second business), so §8's cross-business `division_id` rejection and the
  `404`-for-a-foreign-org-record asymmetry (audit §G5) are asserted in
  `TestDivisionScopeOnSubmitValidation/division_of_another_business` and
  `internal/gateway/org_test.go` instead of through the gateway.
* **No coverage of:** TLS/mTLS, multi-instance operation, rate limiting,
  the oversized-response (`413 RESOURCE_LIMIT`) path, and SSE client capacity
  (`503 RESOURCE_LIMIT`).

## Conventions

* Tests run serially (`fullyParallel: false`, one worker) against one gateway
  instance per worker; specs are ordered `01`…`13`. `11-provider-failure`
  starts its own gateway because its configuration exists only at boot.
* Timeouts are bounded everywhere; on failure the harness prints the request
  id, the last response body and the captured NEXUS stdout/stderr so a red run
  is diagnosable without reproducing it by hand.
* Assertions are on contract shape, never on implementation details: no
  internal Go symbols, no fixtures injected behind the API.
