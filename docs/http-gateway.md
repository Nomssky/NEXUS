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

Every route below is registered in `internal/gateway/server.go`. Two
authentication groups cover all of them:

* **Identity-scoped** — `/api/v1/requests…`, `/events`, `/api/v1/approvals…`,
  `/api/v1/escalations…`, `/api/v1/identities…`, `/api/v1/businesses…`,
  `/api/v1/divisions…`, `/api/v1/agents…`, `/api/v1/executions…`,
  `/api/v1/intelligence…`. Authenticated with `X-Actor-ID` + `X-Actor-Credential`
  (or `Authorization: Basic …`) and, where a `business_id` is involved,
  membership-checked. `401 UNAUTHORIZED` without credentials, `403
  AUTHORIZATION` outside the scope. An `X-API-Key` does not authenticate these
  paths.
* **Control** — everything under `/api/v1/control/`, key-gated with `X-API-Key`
  and no identity at all: `403 CONTROL_DISABLED` when no key is configured,
  `401 UNAUTHORIZED` on a missing or wrong key.

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` | Liveness check — always 200 if server running |
| `GET` | `/ready` | Readiness check — 200 if engine running, 503 otherwise |
| `GET` | `/status` | Engine status (CREATED, RUNNING, DRAINING, STOPPED) |
| `POST` | `/api/v1/requests` | Submit a new request |
| `GET` | `/api/v1/requests/{id}` | Get request result (`202 pending` while running) |
| `POST` | `/api/v1/requests/{id}/cancel` | Cancel an in-flight request (E-005) |
| `GET` | `/events` | SSE stream of events (scoped: `business_id` required) |
| `GET` | `/api/v1/approvals` | List the scope's **pending** approvals |
| `POST` | `/api/v1/approvals/{id}/approve` | Approve and resume the held request |
| `POST` | `/api/v1/approvals/{id}/deny` | Deny — no resume |
| `GET` | `/api/v1/escalations` | List the scope's escalation records |
| `POST` | `/api/v1/escalations/{id}/ack` | Acknowledge an escalation |
| `POST` | `/api/v1/escalations/{id}/resolve` | Resolve an escalation |
| `GET` | `/api/v1/identities` | List a business's identities |
| `POST` | `/api/v1/identities` | Create an identity (+ optional credential, + membership) |
| `GET` | `/api/v1/identities/{id}` | Read one identity |
| `POST` | `/api/v1/identities/{id}/suspend` | Suspend an identity |
| `POST` | `/api/v1/identities/{id}/revoke` | Revoke an identity |
| `POST` | `/api/v1/identities/{id}/activate` | Reactivate an identity |
| `GET` | `/api/v1/businesses` | List the businesses the actor belongs to |
| `POST` | `/api/v1/businesses` | Onboard a business (bootstrap: membership-free) |
| `POST` | `/api/v1/businesses/{id}/members` | Establish the owner's initial business-wide membership (onboarding) |
| `GET` | `/api/v1/businesses/{id}` | Read one business |
| `POST` | `/api/v1/businesses/{id}/suspend` | Suspend a business |
| `POST` | `/api/v1/businesses/{id}/archive` | Archive a business |
| `POST` | `/api/v1/businesses/{id}/activate` | Reactivate a business |
| `GET` | `/api/v1/divisions` | List a business's divisions |
| `POST` | `/api/v1/divisions` | Create a division of the caller's business |
| `GET` | `/api/v1/divisions/{id}` | Read one division |
| `POST` | `/api/v1/divisions/{id}/suspend` | Suspend a division |
| `POST` | `/api/v1/divisions/{id}/archive` | Archive a division |
| `POST` | `/api/v1/divisions/{id}/activate` | Reactivate a division |
| `POST` | `/api/v1/agents` | Register an agent definition (Agent Execution Layer v1) |
| `GET` | `/api/v1/agents` | List a business's agents (optionally `division_id`) |
| `GET` | `/api/v1/agents/{id}` | Read one agent definition |
| `POST` | `/api/v1/agents/{id}/update` | Update name/description/capabilities/tools/memory |
| `POST` | `/api/v1/agents/{id}/suspend` | Suspend an agent |
| `POST` | `/api/v1/agents/{id}/archive` | Archive an agent (terminal) |
| `POST` | `/api/v1/agents/{id}/activate` | Reactivate an agent |
| `POST` | `/api/v1/executions` | Submit an agent execution |
| `GET` | `/api/v1/executions/{id}` | Get an execution result (`202` pending) |
| `POST` | `/api/v1/executions/{id}/cancel` | Cancel an in-flight execution (E-005) |
| `POST` | `/api/v1/intelligence/execute` | Submit an objective execution (Agent Intelligence Layer v1) |
| `GET` | `/api/v1/intelligence/{id}` | Get an objective execution result (`202` pending) |
| `POST` | `/api/v1/intelligence/{id}/cancel` | Cancel an objective execution (E-005) |
| `GET` | `/api/v1/control/status` | Control status, uptime, components, request count |
| `POST` | `/api/v1/control/pause` | Pause admission |
| `POST` | `/api/v1/control/resume` | Resume admission |
| `GET` | `/api/v1/control/metrics` | Executor / backpressure / circuit-breaker / recovery metrics |
| `GET` | `/api/v1/control/components` | Component states (constructed vs running) |
| `GET` | `/api/v1/control/config` | Redacted boot-configuration snapshot |
| `GET` | `/api/v1/control/config/storage` | Bounded host volume discovery |
| `POST` | `/api/v1/control/config/storage/validate` | Validate a data-directory path (read-only) |
| `GET` | `/api/v1/control/policies` | List the effective policy set |
| `GET` | `/api/v1/control/policies/{id}` | Read one policy record |
| `PUT` | `/api/v1/control/policies/{id}` | Create or replace one policy record |
| `DELETE` | `/api/v1/control/policies/{id}` | Remove one policy record |

### POST /api/v1/requests

```json
{
  "intent": "owner's intent",
  "business_id": "biz-1",
  "division_id": "div-1",
  "actor_id": "user-1",
  "priority": 5,
  "constraints": ["budget:1000"]
}
```

`division_id` is optional (CORE_INTERFACE_CONTRACTS §4.2 `CTR-AUTH-001`).
Omitting it submits at business scope; supplying it narrows the request to one
division of `business_id` and that division is carried on the admitted entry,
the stored result and the terminal result. It must exist and belong to
`business_id` (SCHEMA_IDENTITIES_ORG §8), and when enforcement is on the
caller's membership must cover it:

| Status | Category | When |
|---|---|---|
| `400` | `VALIDATION` | `division not found` |
| `400` | `VALIDATION` | `division does not belong to the requested business` |
| `403` | `AUTHORIZATION` | `access denied: actor is not a member of the requested division` |
| `403` | `AUTHORIZATION` | `access denied: business-scope requests require a business-wide membership` (divisionless submit) |
| `409` | `CONFLICT` | the business or the named division is not `active` (G2) |
| `503` | `DEPENDENCY_FAILURE` | a division was claimed but no registry is wired to verify it |

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
business and division. Visibility is the G3/G5 rule: a recorded division
narrows the read to callers whose membership covers it; divisionless records
are visible only to business-wide members; an id outside the caller's scope
answers `404`, never a revealing `403`.

The terminal result echoes the scope it was admitted under:

```json
{
  "request_id": "api-1234567890",
  "business_id": "biz-1",
  "division_id": "div-1",
  "status": "completed"
}
```

`division_id` is omitted when the request was submitted at business scope.

| Status | Category | Body |
|---|---|---|
| `200` | — | the terminal result (`completed` / `failed` / `cancelled`) |
| `202` | — | `{"request_id","correlation_id","status":"pending"}` |
| `400` | `VALIDATION` | `business_id` missing |
| `401` | `UNAUTHORIZED` | enforcement on, no credentials |
| `403` | `AUTHORIZATION` | not a member of `business_id`, or a scope this actor cannot see expressed in the query |
| `404` | `VALIDATION` | unknown request id — also the answer for any id outside the caller's scope (G5) |

`404` covers both an id that was never admitted and one that exists but is
not visible to the caller — the two are deliberately indistinguishable
(CORE_INTERFACE_CONTRACTS §11.2).

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
on), identity-bound membership in `business_id` (`403 AUTHORIZATION`), core
ownership (`404 unknown`, `404` for foreign or out-of-division scope), then the recorded
division when one exists. It is an identity-scoped API path — a control API key is **not** required, and
no governance re-evaluation happens at this boundary. The division check runs
against the recorded scope, not the pending map, so it applies to admitted
in-flight work as well as stored results.

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
| `403` | `AUTHORIZATION` | not a member of `business_id` |
| `404` | `VALIDATION` | unknown request, or an id outside the caller's scope (G5), or a recorded-division mismatch |
| `409` | `CONFLICT` | already in a terminal state (`completed`/`failed`) |

Repeat cancels are idempotent: an already-cancelled request returns `202`
again (never `404`/`409`).

### Approvals — `/api/v1/approvals`

The human surface of a `REQUIRE_APPROVAL` policy: the policy holds the request
as a terminal `failed` / `APPROVAL_REQUIRED` and puts a record here to decide.

| Method | Path | Success | Notes |
|---|---|---|---|
| `GET` | `/api/v1/approvals?business_id=` | `200 {"approvals":[…]}` | **Pending only**; each record carries `entity_id`, `requester_id`, `policy_ref` and the projected `status:"PENDING"` |
| `POST` | `/api/v1/approvals/{id}/approve?business_id=` | `202 {"approval_id","status":"approved"}` | Accepts first: the resume runs asynchronously, observed on `GET /api/v1/requests/{id}` |
| `POST` | `/api/v1/approvals/{id}/deny?business_id=` | `200 {"approval_id","status":"denied"}` | No resume — the stored result stays `failed` / `APPROVAL_REQUIRED` |

Decision body: `{"reason":"…"}` — required (SCHEMA_WORK §6.2
`decision_rationale`).

| Status | Category | When |
|---|---|---|
| `400` | `VALIDATION` | `business_id` missing, malformed body, or empty `reason` (`reason required (decision_rationale)`) |
| `403` | `AUTHORIZATION` | not a member of `business_id`, not a business-wide member, self-approval under `self_approval_prohibited`, or an approver outside `approver_ids` |
| `404` | `VALIDATION` | unknown approval id, one outside the caller's business, or — once the resume has stored its terminal result — the now-closed record |
| `409` | `CONFLICT` | `approval not pending` / `approval not resumable`, while the record is still actionable |

### Escalations — `/api/v1/escalations`

The human surface of an `ESCALATE` policy: the request fails with
`ESCALATION_REQUIRED` / category `POLICY_DENIED` and `error.details.escalation_ref`,
and the alert is queued asynchronously from the `governance.escalated` event —
so the list must be **polled**.

| Method | Path | Success | Notes |
|---|---|---|---|
| `GET` | `/api/v1/escalations?business_id=` | `200 {"escalations":[…]}` | Records answer `pending → acknowledged → resolved` (`expired` past `deadline`) |
| `POST` | `/api/v1/escalations/{id}/ack?business_id=` | `200 {"escalation_id","status":"acknowledged","accepted":true}` | only from `pending` |
| `POST` | `/api/v1/escalations/{id}/resolve?business_id=` | `200 {"escalation_id","status":"resolved","accepted":true}` | from `pending` or `acknowledged` |

Decision body: `{"reasoning":"…"}` — required.

| Status | Category | When |
|---|---|---|
| `400` | `VALIDATION` | `business_id` missing, malformed body, or empty `reasoning` |
| `403` | `AUTHORIZATION` | not a member of `business_id`, or no business-wide membership |
| `404` | `VALIDATION` | unknown escalation id, or one outside the caller's business |
| `409` | `CONFLICT` | the record is not in a decidable state (`ack` only from `pending`) |

### Objectives — `/api/v1/intelligence`

Agent Intelligence Layer v1 (`docs/agent-intelligence.md`,
`contracts/AGENT_INTELLIGENCE_CONTRACTS.md`). An objective execution is an
ordinary admitted request whose handler is the bounded control loop, so
identity, scope (G3), active-org admission (G2), governance/approval,
cancellation and visibility (G5) behave exactly as everywhere else.

`POST /api/v1/intelligence/execute` body:

```json
{
  "business_id": "biz-1",
  "division_id": "div-1",
  "actor_id": "user-1",
  "objective": {
    "description": "multiply six by seven with the calculator",
    "success_criteria": ["the product is reported"],
    "context": {"agent_id": "analyst"},
    "budget": {"max_iterations": 4}
  }
}
```

`description` is required; `budget` may only **tighten** the server caps
(iterations 8, model calls 16, tool calls 8, delegations 3, replans 2, steps 12,
depth 2, 60s). Response `202 {execution_id, correlation_id, status:"accepted"}`.

`GET /api/v1/intelligence/{id}?business_id=` returns the standard request
response; the loop's terminal state and counters ride in `outcome.summary`:

```
state=completed | objective=… | agent=analyst | iterations=2 tool_calls=1
delegations=0 model_calls=2 replans=0 observations=1 | result=…
```

Non-`completed` states are `failed` results whose `error.message` starts with
`state=<state>:` — one of `failed`, `cancelled`, `budget_exhausted`,
`deadline_exceeded`. Cancellation uses the same E-005 semantics
(`202`/`409`/`404`). After a restart an objective execution id answers `404`:
objective state is process-local (G4 Level 1); agent definitions and agent
memory are the durable parts.

| Status | Category | When |
|---|---|---|
| `400` | `VALIDATION` | missing `business_id`/`actor_id`, malformed body, empty objective |
| `401` | `UNAUTHORIZED` | enforcement on, no/invalid credentials |
| `403` | `AUTHORIZATION` | actor mismatch, out-of-scope membership (incl. G3 division rules) |
| `404` | `VALIDATION` | unknown or out-of-scope execution id |
| `409` | `CONFLICT` | business/division not `active` (G2) |
| `503` | `DEPENDENCY_FAILURE` | intelligence runtime not configured |

### Agent definitions — `/api/v1/agents`

Agent Execution Layer v1; full semantics in `docs/agent-contract.md` and
`contracts/AGENT_EXECUTION_CONTRACTS.md`. Definitions are durable organization
records; they are visible under the same rules as the rest of the org surface
(business-wide membership for mutation/listing, G3 visibility for reads, `404`
for foreign/unknown).

`POST /api/v1/agents` body:

```json
{
  "entity_id": "researcher",
  "name": "Researcher",
  "description": "reads and summarizes",
  "business_id": "biz-1",
  "division_id": "div-1",
  "capabilities": ["research", "analysis"],
  "allowed_tools": ["echo", "calculator"],
  "model": { "prefer_local": true, "tool_calling": true },
  "memory": { "mode": "business" },
  "parameters": { "tone": "concise" }
}
```

`entity_id`, `business_id` and `capabilities` are required; a referenced
business/division must exist and be `active`; unknown tool ids are rejected.
Response is `200` with the stored definition.

| Status | Category | When |
|---|---|---|
| `400` | `VALIDATION` | malformed body, missing field, unknown tool id |
| `401` | `UNAUTHORIZED` | no credentials (enforcement on) |
| `403` | `AUTHORIZATION` | not a business-wide member, or the scope query denied |
| `404` | `VALIDATION` | unknown or not visible agent |
| `409` | `CONFLICT` | duplicate `entity_id`, illegal transition (e.g. `archived → active`), or a non-active business/division |

### Executions — `/api/v1/executions`

An execution is an ordinary request instrumented with the agent runtime, so
governance, approvals, escalations, cancellation and every G1–G5 rule apply
unchanged.

`POST /api/v1/executions` body (request body fields plus):

```json
{
  "intent": "research the quarterly report",
  "business_id": "biz-1",
  "division_id": "div-1",
  "actor_id": "user-1",
  "agent_id": "researcher",
  "required_capabilities": ["research"],
  "optional_capabilities": ["summarization"],
  "tools": [{"tool_id": "echo", "input": {"text": "hi"}}],
  "delegates": [{"id": "sub", "intent": "verify numbers"}],
  "workflow": { "strategy": "sequential", "nodes": [{"id": "a", "intent": "..."}] }
}
```

Admission mirrors `POST /api/v1/requests` (actor must equal the authenticated
identity, membership must cover the declared scope, non-active business or
division → `409 CONFLICT` (G2), divisionless business-scope executions require a
business-wide membership (G3)). Response `202`:

```json
{ "execution_id": "api-…-e", "correlation_id": "api-…-e", "status": "accepted", "actor_id": "user-1" }
```

`GET /api/v1/executions/{id}?business_id=` returns the canonical request
response, with agent telemetry in `outcome.metrics` (`provider`, `model`,
`routing_reason`, `tools_executed`, `child_executions`, `retries`) and
`202`/`404` following the same rules as request results (§G5: unknown or
out-of-scope id → `404`). Cancellation follows E-005 exactly
(`202`/`409`/`404`). Execution ids are correlation ids: an execution can also be
observed through `GET /api/v1/requests/{id}`.

### Organization records — `/api/v1/{identities,businesses,divisions}`

CRUD and lifecycle transitions over the identity registry
(`contracts/SCHEMA_IDENTITIES_ORG.md` §2–§4, §9). All three families are
identity-scoped; the registry is required, so a process started without it
answers `503 DEPENDENCY_FAILURE` rather than fabricating records.

Common rules:

* **Lists fail closed on scope.** `GET /api/v1/identities` and
  `GET /api/v1/divisions` require `business_id` (`400 VALIDATION`); with
  enforcement on, a non-member gets `403`, and a member holding only
  division-scoped memberships gets `403` (business-level surface, G3).
  `GET /api/v1/businesses` takes no parameter and is filtered to the
  businesses where the actor holds a business-wide membership — otherwise
  it sees an empty list (no cross-tenant directory).
* **Creation is membership-bound.** Creating an identity or a division inside
  a business requires a business-wide membership in it. Creating a business does not: it is
  bootstrap and the business does not exist yet.
* **Foreign scope reads and transitions answer `404`**, not `403` — no
  cross-tenant existence leak. The same `404 VALIDATION` covers an unknown id.
* **`POST` returns `200` with the created record** (not `202`, not `201`).
* Transitions answer `200 {"identity_id"|"business_id"|"division_id","status"}`;
  an invalid transition or a duplicate is `409 CONFLICT`.
* System identities cannot be created here: `identity_type: "system"` →
  `400 VALIDATION` (`system identities are created by the runtime, not via the API`).

> **Authority model (G1).** The contract defines (SCHEMA_IDENTITIES_ORG §12.1)
> that lifecycle transitions are authorized by any business-wide member of the
> record's business, with no role distinction; self-mutation is allowed;
> suspended records reactivate through another member; revoked is terminal.
> The bootstrap identity has no special privilege — its credential is simply
> re-armed from `NEXUS_BOOTSTRAP_CREDENTIAL` at every boot.
>
> **Caution — self-lockout.** Status is enforced at authentication, and
> `bootstrapIdentity` only *creates* `nx:human:bootstrap` when it does not
> exist — so suspending or revoking it is **not** undone by a restart. Create a
> second active identity first (`POST /api/v1/identities` with a `credential`);
> that identity can `activate` the bootstrap one again. Without a second
> identity every identity-scoped endpoint answers `401` until the data
> directory is reset or enforcement is relaxed. The control API key and the
> health surface are unaffected.

#### POST /api/v1/businesses/{id}/members — initial membership (onboarding)

Establishes the owner's initial business-wide membership in a business. This
resolves the circular dependency of `POST /api/v1/businesses`: business
creation is membership-free (the business does not exist yet), so the owner is
not automatically enrolled. The owner claims their first membership here.

Authorization:

* **Authentication required.** The identity middleware binds the actor (A6); no
  credentials → `401 UNAUTHORIZED`.
* **Actor must be the `owner_identity_id`** recorded on the business. Any other
  authenticated identity is refused `403 AUTHORIZATION` — no caller may enroll
  arbitrary members in arbitrary businesses.
* **Business must exist** (`404 VALIDATION`) and be `active` (G2 —
  `409 CONFLICT` otherwise).
* Idempotent: if the owner already holds a business-wide membership, this is a
  no-op returning `200` (safe to retry after a transient failure or on a second
  boot).

Body: `{}` (empty). The response echoes `business_id`, `identity_id`, `role`
(`OWNER`) and `status` (`active`). The role is descriptive only
(MEMBERSHIP != AUTHORITY); it grants no governance permission.

This is the supported path to make a newly created business usable by its
owner: after this call, the owner can submit work, create divisions, and use
every business-scoped endpoint for that business.

| Status | Category | When |
|---|---|---|
| `200` | — | membership established (or already existed) |
| `400` | `VALIDATION` | `business_id` missing |
| `401` | `UNAUTHORIZED` | enforcement on, no/invalid credentials |
| `403` | `AUTHORIZATION` | authenticated actor is not the business owner |
| `404` | `VALIDATION` | unknown business |
| `409` | `CONFLICT` | business is not `active` (G2) |
| `503` | `DEPENDENCY_FAILURE` | membership store not configured |

---

`POST /api/v1/identities` body: `entity_id?`, `identity_type` (required,
canonical), `display_name`, `status` (`active`/`pending` only at creation),
`business_id?`, `division_id?`, `parent_id?`, `expires_at?`, `metadata?`,
`role?`, `credential?`, `credential_method?` (`token`/`password`/`service`/
`device`). When `credential` is present it is registered in the same step, the
identity joins its `business_id` with the given role, and the raw value is
**never echoed back**; if credential registration fails the identity record is
rolled back, so no identity exists without the credential the caller asked for.

### Control surface — `/api/v1/control/{status,pause,resume,metrics,components}`

Key-gated (no identity, no membership). See also *Policy control* below, which
shares the prefix.

| Method | Path | Success |
|---|---|---|
| `GET` | `/status` | `200 {status, uptime, components{engine,…}, request_count}` |
| `POST` | `/pause` | `200 {status:"paused"}`; repeat → `409 ALREADY_PAUSED` |
| `POST` | `/resume` | `200 {status:"resumed"}`; repeat → `409 ALREADY_RUNNING` |
| `GET` | `/metrics` | `200 {executor{executed,failed,denied,cancelled,active}, backpressure{queue_size,rejected_count}, circuit_breaker{state}, recovery{failure_count}}` |
| `GET` | `/components` | `200 {components:[{name,status,type}], count}` |

`uptime` is the elapsed wall time since the gateway was constructed, as a Go
duration string (`"1m30.5s"`, `"0s"` before the first tick), measured against
the server clock — never a raw number and never negative. `request_count` is
the executor's cumulative executed count, not the number of live requests.

`/components` reports **authoritative state where one exists** (`engine` → the
lifecycle state, `circuit_breaker` → its state, `task_executor` →
`running`/`stopped`) and `configured` for components that are constructed but
have no start/stop lifecycle — never a blanket "active" claim.

While paused: `GET /ready` → `503`, `POST /api/v1/requests` → `503
RESOURCE_UNAVAILABLE` (never `202`).

### Configuration & storage inspection — `/api/v1/control/config…`

These routes are key-gated by the same `X-API-Key` control check. They expose
the **running process's boot configuration** and a bounded view of the host
storage. They are deliberately read-only:

- There is no route to write configuration, change providers, store secrets or
  apply a data directory.
- Control responses carry `Cache-Control: no-store` so a shared browser or
  intermediary cache cannot retain configuration or filesystem metadata.
- The snapshot never contains a credential value. A model provider reports the
  name of its `api_key_env` and `credential_status`
  (`present`/`missing`/`not_required`); a Brave search provider reports
  `BRAVE_API_KEY` presence the same way. Injected adapters that declare no
  credential requirement report `unknown`, never `present` or
  `not_required`. `credential_storage` is always `environment_only`.
- Credential presence and the web-search selection are sampled **at boot**,
  from the descriptor the launcher captured from the adapter it actually
  constructed. Request handling does not re-read `NEXUS_CAPABILITY_WEB` or a
  credential variable, so changing the environment afterwards cannot retcon
  the reported posture.
- `live_verified` and `readiness.gate_zero_authorized` are always `false`. The
  gateway does not probe a model or search endpoint from these routes.
- `model_routing.per_purpose_selection` is always `false`: this build registers
  providers in configuration order and has no per-purpose selector.

`GET /api/v1/control/config` returns the redacted snapshot: environment, nexus
id, providers (adapter `openai_compatible`, endpoint, model id, timeout,
capabilities), the boot-captured web-search descriptor, storage posture,
readiness blockers and warnings. It also returns provenance fields:

- `fingerprint` — versioned `sha256:v2:<64 hex>` identifier of the effective
  non-secret settings; `fingerprint_version` is `sha256:v2`. The fingerprint is
  material to audit/drift correlation and carries no authority.
- `sources` — section-level provenance for `nexus`, `logging`, `health`,
  `lifecycle`, `security`, `storage`, `models`, `rate_limit` and `http`, with
  values `default`, `file`, `environment` or `mixed` (both file and environment
  supplied a value in that section). It is not per-field provenance.
- `provenance` — `kind: "boot_snapshot"`, documented precedence, `immutable`,
  and the capture time.

The web-search descriptor reports `provider` (canonical adapter id such as
`scripted-research`, `wikipedia` or `brave`), `kind` (`disabled`, `scripted`,
`live` or `injected`), `registered`, `live`, endpoint (userinfo/query/fragment
stripped), and credential status. The scripted provider is always labelled
test-only and is never presented as live evidence. An adapter injected by an
embedding process is reported as `injected` with unknown liveness and
credential requirements; the gateway does not guess. An unknown
`NEXUS_CAPABILITY_WEB` value still fails boot closed and is never reported as a
registered provider.

`GET /api/v1/control/config/storage` returns bounded volume candidates: the
configured `storage.data_dir`, the process user's home, `/`, mounts under
`/mnt`, `/media`, `/srv` and `/data`, plus any absolute paths in
`NEXUS_STORAGE_ROOTS` (colon-separated). There is no directory-listing API.
Capacity and filesystem metadata come from `statfs` and
`/proc/self/mountinfo`; a media type is reported only when
`/sys/block/<device>/rotational` is readable. NVMe is reported as a bus, not as
"SSD". Paths are host-local: they belong to the machine running NEXUS, not to
the browser.

`POST /api/v1/control/config/storage/validate` accepts
`{"path":"/absolute/path"}` and returns deterministic server-side checks:
absolute-path requirement, `..` traversal rejection, final-symlink rejection,
sensitive-root rejection, existence/parent checks, writability via an access
check, readability of an existing directory, and capacity thresholds (fail
below 64 MiB, warn below 1 GiB). A directory is reported writable only when the
process has both write and search (execute) permission, because creating an
entry requires both; an existing directory must also be readable and searchable
so the file store can load records. Allowed roots are the home directory,
`/mnt`, `/media`, `/srv`, `/opt`, `/var/lib`, `/data`, `/tmp`, the configured
data directory, and `NEXUS_STORAGE_ROOTS`.
Validation never creates, moves or deletes anything. The response always
reports `applying_supported:false`, `restart_required:true` and
`migration_supported:false`; a validated path must still be set by an operator
and applied with a controlled restart. One process must own one data directory.
Validation is advisory and point-in-time: it is not a guarantee that a later
writer will be allowed to create or keep data there. Symlink replacement and
other time-of-check/time-of-use changes remain possible after validation, so
the eventual storage writer must enforce its own boundary at the point of use.

### Server-Sent Events — `/events`

`GET /events?business_id=` → `200 text/event-stream`, frames
`event: <type>\ndata: <json>\n\n`. `business_id` is **required**
(`400 VALIDATION`): scoping cannot be bypassed by omitting it, and with
enforcement on the subscriber must hold a business-wide membership (`403`
  otherwise). Only events of
that business scope are delivered. Each frame is the SCHEMA §2.2 Event Record;
`chain.*` events carry `correlation_id` equal to the request id.

### Policy control — `/api/v1/control/policies`

The governance engine is seeded at boot with one built-in policy,
`default-allow`, so an unconfigured installation allows requests instead of
falling through to the contract default `DENY`. These endpoints expose that
policy set (contracts/SCHEMA_GOVERNANCE_ATTENTION.md §9).

Authentication is the **control API key**, not an identity:

```http
X-API-Key: <NEXUS_CONTROL_API_KEY>
```

No `X-Actor-ID` / `business_id` is required or honoured — this is a control-plane
path, not an identity-scoped one. No key configured → `403 CONTROL_DISABLED`;
wrong key → `401 UNAUTHORIZED`.

```bash
BASE=http://127.0.0.1:8081
KEY=dev-control-key
curl -fsS -H "X-API-Key: $KEY" "$BASE/api/v1/control/policies"
```

#### GET /api/v1/control/policies

`200 {"policies":[ Policy, ... ]}` — the complete effective set, sorted by
`policy_id`, including the built-in `default-allow`.

#### GET /api/v1/control/policies/{id}

`200` with the bare Policy record, or `404 VALIDATION` (`policy not found`).

#### PUT /api/v1/control/policies/{id}

Create-or-replace. The path `policy_id` wins: it is copied onto the record, and
any `policy_id` in the body that disagrees is rejected (`400 VALIDATION`).

```json
{
  "policy_version": "1",
  "policy_type": "access_control",
  "name": "deny billing writes",
  "description": "billing intent is blocked in business biz-a",
  "status": "active",
  "business_id": "biz-a",
  "subject": {"subject_type": "all"},
  "action": {"action_type": "custom", "action_ids": []},
  "resource": {"resource_type": "all"},
  "effect": "DENY",
  "precedence": 10,
  "effective_from": "2026-01-01T00:00:00Z"
}
```

Omitted server-derivable fields are defaulted: `schema_version`,
`entity_type`, `nexus_id`, `created_at`, `created_by` (your identity), `provenance`,
`effective_from`, `policy_version`. Everything else that the schema marks
required must be present and valid — otherwise `400 VALIDATION` with the
field-level reason.

Response: `200`
```json
{"policy_id": "deny-billing", "policy_version": "1", "status": "active"}
```

Re-`PUT`ting an existing `policy_id` replaces the record in place: `created_at`
and `created_by` are inherited from the existing record (a replace does not
rewrite who created it or when) and `updated_at` is stamped. Omitting
`policy_id` from the body is fine — the path is authoritative; a body that
names a *different* `policy_id` is rejected.

The built-in policy is read-only:

| Status | Code | When |
|---|---|---|
| `400` | `VALIDATION` | malformed body, invalid enum/required field, `policy_id` mismatch |
| `401` | `UNAUTHORIZED` | wrong or missing `X-API-Key` |
| `403` | `CONTROL_DISABLED` | no control API key configured |
| `404` | `VALIDATION` | unknown `policy_id` on GET/DELETE |
| `409` | `CONFLICT` | `PUT`/`DELETE` targeting `default-allow` |

#### DELETE /api/v1/control/policies/{id}

`200 {"policy_id":"…","deleted":true}`, `404 VALIDATION` when unknown,
`409 CONFLICT` for the built-in `default-allow`.

#### What a policy does to a submitted request

A policy only affects requests whose scope matches. The submit body carries
`business_id` only — there is no division/workflow/task field — so policies
pinned to a narrower scope never match a request submitted through this API.

| Effect | Observed on `GET /api/v1/requests/{id}` |
|---|---|
| `ALLOW` | normal terminal result (also what `default-allow` does) |
| `DENY` | `status:"failed"`, `error.category:"POLICY_DENIED"`, message names the matched policy |
| `ALLOW_WITH_CONSTRAINTS` | terminal result carries `constraints:["type:expression"]` — reported, not enforced |
| `REQUIRE_APPROVAL` | `status:"failed"`, `error.category:"APPROVAL_REQUIRED"`, `error.details.approval_id` |
| `ESCALATE` | `status:"failed"`, `error.code:"ESCALATION_REQUIRED"`, `error.details.escalation_ref` |

Policies are **process-lifetime state**: they are not written to disk, so a
restart reloads only the seeded `default-allow`.

#### What a provider failure looks like

Admission does not depend on provider health: the request is accepted (`202`)
and then ends honestly. `NEXUS_SEEDED_PROVIDER_STATUS=offline` starts the
launcher-seeded `simulated` provider offline (`contracts/PROVIDER_CONTRACTS.md`
§12; the default is healthy), and `GET /api/v1/requests/{id}` answers:

| Field | Value |
|---|---|
| `status` | `failed` — never `completed`, and `outcome.summary` stays empty |
| `error.code` | `EXECUTION_FAILED` |
| `error.category` | `INTERNAL_FAILURE` |
| `error.chain_step` | `agent` |
| `error.retryable` | `false` |
| `error.message` | contains `provider invocation failed` … `provider simulated is offline` |
| `outcome.metrics.executor_status` | `failed` |
| `audit_trace` | the `agent` step reads `status=failed …`, `verify` reads `status=failed` |

`GET /ready` and the control plane keep answering normally — the gateway did
not fail. Any other value for the variable is a `VALIDATION` boot error.

### Headers

- `X-Correlation-ID`: Custom correlation ID (optional, auto-generated if not provided)

---

## Rate limiting & browser security (Production Hardening v2)

### Rate limiting (F4)

A bounded, **process-local** token-bucket limiter (complementing queue-level
backpressure) runs in the request chain. It is **not** a distributed quota: with
multiple instances each has its own buckets, so a global limit needs an external
limiter (out of scope).

Three classes:

| Class | Applies to | Key | Default (`/min`) |
|---|---|---|---|
| `submit` | `POST /api/v1/requests`, `/api/v1/executions`, `/api/v1/intelligence/execute` | validated `IdentityID` | 60 |
| `api` | other scoped **writes** (agent registration, memory writes, approvals, escalations, cancellation, org transitions) | validated `IdentityID` | 600 |
| `ip` | every non-exempt request (all methods) | direct peer IP | 6000 |

Design points:

- The actor classes key on the **validated identity** bound by the identity
  middleware — never on the caller-supplied `X-Actor-ID` header. An unauthenticated
  or spoofed request cannot consume or choose another actor's bucket.
- `submit` and `api` are **separate buckets**, so exhausting submission can never
  block cancellation or other safety-relevant operations.
- **Read methods (`GET`/`HEAD`/`OPTIONS`) are not per-actor limited.** Polling
  execution status is high-frequency and cheap; conflating it with writes would
  let one actor's polling throttle an unrelated write (e.g. agent registration).
  Reads remain bounded by the coarse `ip` class and the existing queue
  backpressure.
- `/health`, `/ready`, and `/status` are **exempt** so deployment monitoring is
  never throttled.
- Exceeding a limit returns `429` in the standard error envelope
  (`code: RATE_LIMIT`, category `RATE_LIMIT`, `retryable: true`) with an accurate
  `Retry-After` (seconds). No key, credential or IP is disclosed.

Configuration (`rate_limit.*`, env `NEXUS_RATE_LIMIT_*`):

```json
{ "rate_limit": { "enabled": true, "submit_per_minute": 60,
                  "request_per_minute": 600, "per_ip_per_minute": 6000 } }
```

`enabled` defaults to `true`; each per-minute value must be `>= 1` when enabled
(a zero/negative value fails validation rather than silently disabling
protection). `NEXUS_RATE_LIMIT_ENABLED` must be `true`/`false`.

**Proxy trust:** the `ip` class uses the direct TCP peer address and deliberately
ignores `X-Forwarded-For`/`Forwarded`. With no trusted-proxy contract those
headers are caller-controlled; behind a reverse proxy all clients would therefore
share the proxy's IP for the `ip` class, and the per-actor classes remain the
effective control. A trusted-proxy mode is future work.

### CORS (F5)

Cross-origin policy is explicit and secure by default. An **empty** allow-list is
the same-origin default: the gateway emits **no** CORS headers at all
(same-origin deployments need no configuration). For a cross-origin browser
deployment, list exact origins (`http.cors_allowed_origins`, env
`NEXUS_HTTP_CORS_ALLOWED_ORIGINS` as a comma-separated list):

```json
{ "http": { "cors_allowed_origins": ["https://app.example"] } }
```

- Origins must be a real origin (`scheme://host[:port]`); wildcards, paths,
  queries, fragments and non-http(s) schemes fail validation at startup.
- An allowed origin is echoed **exactly** (never a wildcard, never an arbitrary
  origin); responses carry `Vary: Origin`. Disallowed origins receive no
  permission headers.
- Preflight (`OPTIONS`) for an allowed origin answers `204` with
  `Access-Control-Allow-Methods` / `-Headers` / `Max-Age`. `Access-Control-
  Allow-Credentials` is never set: the API authenticates with headers, not
  cookies.
- CORS is a browser policy only — it is **not** authentication or authorization
  and never bypasses them.

### Security headers

The API gateway emits, on every response (including errors):
`X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
`Referrer-Policy: no-referrer`.

It deliberately does **not** emit `Strict-Transport-Security`,
`Content-Security-Policy` or `Permissions-Policy`: those belong to the
frontend/reverse-proxy layer that terminates TLS and serves browser content.
Emitting HSTS here would be wrong on plain-HTTP local development and would not
reflect this layer's TLS contract. **Ownership: browser-document headers
(HSTS/CSP/Permissions-Policy) are the reverse proxy's responsibility; API
response-hardening headers are the gateway's.**

---

## Layout

```
internal/gateway/
  server.go        HTTP server, routes, handlers, control surface, approvals,
                   escalations, SSE
  identity.go      identity middleware, credential extraction, scoped-path set
  org.go           identities / businesses / divisions (SCHEMA_IDENTITIES_ORG
                   §2–§4, §9 audit)
  policies.go      policy control surface (SCHEMA_GOVERNANCE_ATTENTION §9)
  *_test.go        136 tests
```

---

## Testing

- 136 tests in the package: health / ready / status / submit / result /
  validation / error envelopes, cancellation (400/401/403/404/409, idempotent
  repeat, attribution), identity and scope, approvals, escalations, policy
  control, organization records, agent definitions, executions and objective
  executions, control pause/resume, SSE wire and lifetime
- Core Runtime tests unchanged and passing
- Foundation M0–M11 tests unchanged and passing
- Race detector clean
- The black-box HTTP suite lives in `e2e/` (85 Playwright tests driving the
  compiled `cmd/nexus` binary)
