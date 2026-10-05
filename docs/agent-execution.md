# NEXUS — Agent Execution Layer v1

**Status:** implemented (Agent Execution Layer v1 milestone, baseline `82c5420`).
Normative contract: `contracts/AGENT_EXECUTION_CONTRACTS.md`.
G1–G5 (`docs/ARCHITECTURE_DECISIONS.md`) remain authoritative and are
*inherited* by this layer: executions are ordinary NEXUS requests on the
identity-scoped gateway surface.

Companion documents: [agent-contract.md](agent-contract.md),
[model-routing.md](model-routing.md), [tool-execution.md](tool-execution.md),
[workflows.md](workflows.md), [agent-delegation.md](agent-delegation.md).

---

## 1. Discovery note (written before implementation)

Where the layer plugs in, and why:

| Existing piece | Where | What v1 uses from it |
|---|---|---|
| request admission, identity, authorization, governance, approval, escalation, events, cancellation | `internal/core` (chain, E-005 cancel registry) | the *entire* admission + governance pipeline — executions are core requests |
| task executor | `internal/executor` | governance gate, capacity, cancellation, events, `WorkRequest.Handler` seam |
| agent runtime (C12) | `internal/foundation/agent` | spawn/depth semantics, lifecycle vocabulary |
| model router (C05) | `internal/foundation/modelrouter` | `Provider`, `RoutingRequest`, deterministic `Route`, failover inside `Invoke` |
| tool runtime (C13) | `internal/foundation/tool` | `ToolDefinition`, validation boundary; v1 adds deterministic example executors behind it |
| workflow (C11) | `internal/foundation/workflow` | node/state vocabulary (v1 composes workflow steps inside one runtime execution) |
| event bus | `internal/foundation/event` | single event envelope; v1 adds execution event types |
| org record persistence | `internal/foundation/identity` + `foundation/store` | same durable-record pattern (write-through, fail-closed hydration) |

The ten discovery answers (where agents live, where execution lives, how
requests invoke it, scope propagation, model selection, tool invocation,
workflows, delegation, events, failure propagation) are all *the request
pipeline + a handler seam*: the runtime never bypasses the pipeline, so
governance, approvals, scope and durability semantics are the ones the whole
platform already enforces.

## 2. Architecture

```
HTTP  POST /api/v1/executions ─┐
                               │  admission: actor_id == identity, membership covers
                               │  scope (G3), business/division active (G2)
                               ▼
                    core.Engine.SubmitRequest(Request{Handler: agentexec runtime})
                               │
        identity → authorization → governance → (approval | escalation)
                               ▼
                    executor (capacity, cancel, events)
                               ▼
        ┌────────────────── agentexec.Runtime ───────────────────┐
        │  select agent (deterministic, explainable)             │
        │  bounded tools (allowlist ∩ registry)                   │
        │  modelrouter.Invoke (deterministic, failover)           │
        │  delegates (children inherit scope + authorization)     │
        │  workflow (sequential | parallel, bounded pool)         │
        └────────────────────── events ───────────────────────────┘
```

## 3. Semantics (the decisions in one table)

| Question | Answer |
|---|---|
| Who can register/list/mutate agents? | Business-wide members of the agent's business (G3). No roles, no new authority. |
| Who can see an agent? | G3 visibility: business-wide agents → business-wide members; division agents → business-wide members *and* that division's members. Foreign/unknown → 404 (G5). |
| Which agent runs? | Deterministic selection (status → scope → capabilities → tools → optional coverage → id). Explicit `agent_id` is honored only if eligible. |
| What does a provider failure do? | Terminal `failed` execution; never `completed`. Failover (if any) is reported via `routing_reason`/`fallback`. |
| What does a tool failure do? | Terminal `failed` unit naming the tool; never success. |
| What does governance denial do? | Fails *before* the runtime: no model call, no tool call, no child. |
| What does `REQUIRE_APPROVAL` do? | Existing approval record + `failed`/`APPROVAL_REQUIRED`; the approval decision re-admits the *same request* (handler included) so an approved execution really runs. |
| What does cancellation do? | Cooperative: pre-execution cancels are terminal `cancelled`; in-flight cancels propagate through ctx; children and workflow nodes observe the same context. |
| What is retried? | One extra model attempt, transient-only (deadline/timeout, never offline). Everything else is an outcome, not noise. |
| What survives a restart? | Agent definitions (durable records). Execution state: nothing — execution ids `404` exactly like request ids (G4 Level 1). |

## 4. Routes

| Method | Path | Semantics |
|---|---|---|
| `POST` | `/api/v1/agents` | register a definition (`business_id` required, business-wide membership) |
| `GET` | `/api/v1/agents?business_id=&division_id=` | list (business-wide membership) |
| `GET` | `/api/v1/agents/{id}` | read under G3/G5 visibility |
| `POST` | `/api/v1/agents/{id}/update` | name/description/capabilities/tools/memory |
| `POST` | `/api/v1/agents/{id}/suspend|archive|activate` | lifecycle matrix; illegal edge → `409` |
| `POST` | `/api/v1/executions` | submit execution → `202 {execution_id, correlation_id, status}` |
| `GET` | `/api/v1/executions/{id}?business_id=` | `200` terminal / `202` pending / `404` unknown-or-out-of-scope |
| `POST` | `/api/v1/executions/{id}/cancel?business_id=` | E-005 semantics |

All routes are identity-scoped (`X-Actor-ID` + credential), carry the standard
error envelope and `X-Correlation-ID`, and use the existing membership helpers:
no new authorization layer exists in this milestone.

## 5. Execution result shape

The terminal response is the ordinary request response; the agent execution
telemetry is carried in `outcome.metrics` and the summary:

```json
{
  "request_id": "api-…-e",
  "business_id": "biz-1",
  "status": "completed",
  "outcome": {
    "summary": "intent=… | echo.echoed=hi",
    "metrics": {
      "executor_status": "completed", "agent_id": "agent-…",
      "provider": "simulated", "model": "simulated:default",
      "routing_reason": "selected simulated:default (provider=simulated, …)",
      "tools_executed": 1, "child_executions": 0
    }
  }
}
```

## 6. Security boundaries

* **Identity**: unchanged — authentication is authoritative on every scoped call.
* **Authority**: unchanged — business-wide membership only; no RBAC, no owners.
* **Division**: unchanged — strict sub-scope (G3), enforced in selection too.
* **Lifecycle (G2)**: non-active business/division blocks admission (`409`).
* **Visibility (G5)**: unknown-or-invisible → `404`; scope-entry/action → `403`.
* **Durability (G4)**: definitions durable, execution state process-local.
* **Tools**: allowlist + registry + deterministic executors only.
* **Delegation**: children inherit; no authority escalation is expressible.
* **No RBAC / no sandbox shortcuts**: shell, filesystem, browser, arbitrary
  code, cross-business delegation, multi-instance execution and durable
  recovery are documented future milestones, each requiring its own contract.

## 7. Known limitations (v1, explicit)

* Selection is capability-based and deterministic; no semantic/LLM-based
  selection.
* Tools are fixed deterministic executors; a model does not autonomously decide
  tool calls in a loop (the tool set of a node/execution is declared).
* Delegates run synchronously in declaration order; parallel fan-out is
  expressed with parallel workflow nodes.
* Governance re-evaluation per child: children inherit the parent's decision
  rather than re-entering the full chain (the full chain is per request).
* Multi-provider failover exists inside the router's bounded chain; runtime
  retry is transient-only and bounded to one extra attempt.
* Agent definitions are managed over HTTP only; no bulk import/CLI.

## 8. Testing

* Go unit: `internal/agentexec` (registry CRUD/lifecycle/durability, selection
  determinism + explainability, offline-no-retry, transient-retry,
  provider-failure-never-success) and `internal/gateway` (execution admission,
  visibility, telemetry, failure integrity).
* E2E: `e2e/tests/15-agent-execution.spec.ts` — register/discover/lifecycle,
  simple execution, tools + allowlist denial, sequential + parallel workflows,
  delegation, cancellation, offline provider failure, governance denial, G2
  admission, restart semantics, event correlation.