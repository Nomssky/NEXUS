# Agent Execution Layer — Contract (v1)

**Status:** v1, additive. It does not modify any locked contract section; where
it refines an existing contract it names the section it refines. The G1–G5
decisions (`docs/ARCHITECTURE_DECISIONS.md`, `SCHEMA_IDENTITIES_ORG` §12,
`CORE_INTERFACE_CONTRACTS` §11) remain authoritative and are *inherited*, not
restated with different rules.

## 1. Terminology

| Term | Meaning |
|---|---|
| **Agent definition** | A durable record: `id`, `business_id`, optional `division_id`, `name`, `description`, `capabilities[]`, `model` requirements, `allowed_tools[]`, `memory`, `parameters`, `status`, `created_at`, `updated_at`. It declares *what an agent is*, not what it may do. |
| **Selection** | Deterministic choice of one eligible definition for one task. Explainable, never model-driven. |
| **Execution** | One admitted request that runs the agent execution runtime. An execution is an ordinary NEXUS request in every other respect. |
| **Execution context** | The inherited `(request_id, correlation_id, business_id, division_id, actor, agent, parent_execution_id, workflow_id, depth)` tuple (§5). |
| **Delegate / child execution** | A nested execution spawned by a parent execution inside the same scope. |
| **Node** | One step of a workflow: `(id, intent, agent_id?, capabilities[], tools[], input, depends_on[])`. |

## 2. Agent definition (registry record)

1. An agent belongs to exactly one business; `division_id` is optional.
2. A division-scoped agent never escapes its division: a business-scope task can
   only be served by a business-wide agent, a division task by a business-wide
   or same-division agent (G3 narrowing, `SCHEMA_IDENTITIES_ORG` §12.3).
3. `capabilities[]` are declarative metadata used by selection. A capability
   never grants a tool, a permission, or authority (`MEMBERSHIP != AUTHORITY`,
   `CAPABILITY != PERMISSION`).
4. `model` expresses *requirements* (`prefer_local`, `tool_calling`,
   `structured_output`, `preferred_provider`, `allowed_providers`), never a
   hard binding to one vendor.
5. `allowed_tools[]` is an allowlist of registered tool ids; unknown ids are
   rejected at registration, invocation is still mediated (§4).
6. `status ∈ {active, suspended, archived}` with the org transition matrix
   (`active ↔ suspended → archived`, `archived` terminal) — the same philosophy
   as business/division (G2, `SCHEMA_IDENTITIES_ORG` §12.2). Only `active`
   agents are selectable.
7. The definition is a **durable organization record** (store type `agent`,
   write-through, fail-closed hydration) — a restart preserves definitions
   exactly like identities.
8. Registry references must exist and be active: `business_id` must exist and
   be `active`; `division_id`, when set, must exist, belong to that business,
   and be `active`.

## 3. Selection algorithm (deterministic)

Order of evaluation for every candidate in the business:

1. `status == active`, else rejected `lifecycle status <s>`.
2. Scope: division-eligible (business-scope task ↔ business-wide agent;
   division task ↔ business-wide or same-division agent), else rejected
   `division scope mismatch`.
3. `required_capabilities ⊆ agent.capabilities`, else rejected with the missing
   capability names.
4. Every requested tool id is in `allowed_tools[]` **and** registered, else
   rejected.
5. Explicit `agent_id` preference short-circuits the pool but is validated
   against 1–4; an ineligible explicit id fails the execution (never a silent
   fallback to another agent).

Among the eligible pool: highest optional-capability coverage, then
lexicographically smallest id. The selection result carries
`selected agent`, `reason`, `matched_capabilities[]`,
`optional_matched_capabilities[]`, `rejected_candidates[]`; the runtime
publishes `agent.selected` with the same facts. No eligible agent ⇒ the
execution ends `failed` with `no eligible agent: <reason>` — never a
successful execution with no agent.

## 4. Tool boundary

* All tool invocation is mediated: `Agent → ToolCallSpec → allowlist check →
  registry lookup → deterministic executor → result`.
* Unknown tool, tool outside the allowlist, executor error, and tool-failure all
  end the *execution unit* as `failed` with the tool id in the message. Tool
  failure never becomes execution success.
* v1 ships only deterministic, side-effect-free example tools: `echo`,
  `calculator` (add/sub/mul/div), `transform` (upper/lower/reverse). No shell,
  filesystem, network, browser, or arbitrary-code tool exists (§9).

## 5. Execution context and inheritance

An execution runs as a NEXUS request, so the context is the request context:
`correlation_id/request_id`, `business_id`, `division_id`, actor identity,
plus runtime fields `agent_id`, `parent_execution_id`, `root_execution_id`,
`workflow_id`, `depth`. **Inheritance rule:** a child execution (delegate or
workflow node) inherits business, division, actor and the parent's governance
authorization verbatim; nothing a child does can widen them. Depth is bounded
(default 3); exceeding it fails the execution (`delegation depth exceeded`).

## 6. Execution semantics

```
request → identity → scope (G3) → governance → approval/escalation if required
        → agent selection → bounded tools → model (router) → delegates
        → deterministic terminal result
```

* Governance denial ⇒ `failed` with `POLICY_DENIED`, no model call, no tool
  call, no child work (the denial happens in the chain gate before the runtime
  runs).
* `REQUIRE_APPROVAL` ⇒ the existing approval record is created and the request
  ends `failed`/`APPROVAL_REQUIRED`; an approval decision re-admits the *same*
  request — including the same execution handler — so an approved execution
  really runs agent work. No second approval mechanism exists.
* `ESCALATE` ⇒ existing escalation semantics (`ESCALATION_REQUIRED` +
  `governance.escalated`).
* Provider failure ⇒ `failed` (never `completed`), with provider error text
  preserved in the terminal message.
* Cancellation ⇒ cooperative. A cancel that lands before execution makes the
  request terminal `cancelled`; a cancel during a model call propagates
  through the execution context and the executor's first-cause-wins rule.

## 7. Model routing

The runtime expresses the agent's requirements as a
`modelrouter.RoutingRequest` and calls `ModelRouter.Invoke`, which owns
selection and the bounded failover chain. The terminal result reports
`provider`, `model`, `routing_reason`, and `fallback` (true when the router's
reason is a failover). Routing is deterministic (documented strategy, stable
candidate order). Retry policy of the runtime itself: at most one extra model
attempt, only for transient-looking provider errors (deadline/timeout, never
`offline`); `retries` is reported in the result.

## 8. Durability (G4)

Agent definitions are durable organization records. Execution state is
**process-local**: pending, running and terminal executions live in the same
in-memory stores as requests. After a restart an execution id answers `404`,
exactly like a request id (CORE §11.1) — no execution is ever "recovered", and
the API never claims otherwise.

## 9. Out of scope for v1 (documented, not silently absent)

Unrestricted shell, arbitrary filesystem access, browser automation, arbitrary
code/WASM execution, privileged host access, cross-business delegation,
multi-instance or distributed execution, durable execution recovery, and
autonomous privilege escalation are **not** implemented. Each is a future
milestone that must arrive with its own sandbox/isolation contract; nothing in
v1 can be "opened up" to reach them without that contract.

## 10. HTTP surface (G1–G5 inherited)

| Method | Path | Notes |
|---|---|---|
| `POST` | `/api/v1/agents` | register; `business_id` required; business-wide membership required (G3) |
| `GET` | `/api/v1/agents?business_id=[&division_id=]` | business-wide membership required |
| `GET` | `/api/v1/agents/{id}` | visible per G3 rule; otherwise `404` (G5) |
| `POST` | `/api/v1/agents/{id}/update` | mutable projection fields only |
| `POST` | `/api/v1/agents/{id}/suspend|archive|activate` | transition matrix; `409` on illegal edge |
| `POST` | `/api/v1/executions` | submit execution → `202 {execution_id, correlation_id, status:"accepted"}` |
| `GET` | `/api/v1/executions/{id}?business_id=` | `200` terminal, `202` pending, `404` unknown/out-of-scope |
| `POST` | `/api/v1/executions/{id}/cancel?business_id=` | E-005 semantics (same as requests) |

Submission admission is identical to `POST /api/v1/requests`: actor_id must
match the authenticated identity, membership must cover the declared scope
(division-aware), and a non-active business/division is `409 CONFLICT` (G2).
Execution ids are correlation ids, so a single request id can be observed
through either surface.
