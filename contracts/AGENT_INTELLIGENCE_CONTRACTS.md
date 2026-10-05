# Agent Intelligence Layer — Contract (v1)

**Status:** v1, additive. It builds **on** `contracts/AGENT_EXECUTION_CONTRACTS.md`
and does not modify any locked or previously added contract section. G1–G5
(`docs/ARCHITECTURE_DECISIONS.md`, `SCHEMA_IDENTITIES_ORG` §12,
`CORE_INTERFACE_CONTRACTS` §11) and the Agent Execution Layer remain
authoritative. Where this document refines an existing rule it names the rule
it refines.

The intelligence layer is a **deterministic control loop** inside one already
admitted execution. It never admits work, never grants authority, and never
bypasses the request pipeline:

```
request → identity → scope → governance → approval/escalation → EXECUTION
        → OBJECTIVE → PLAN → VALIDATE → DECIDE → ACTION → OBSERVE → …
```

---

## 1. Terminology

| Term | Meaning |
|---|---|
| **Objective** | The desired outcome of an intelligent execution: id, request id, description, success criteria, constraints, budget, context. Carries no authority. |
| **Plan** | A validated, versioned set of steps derived from the objective. Advisory until the validator accepts it. |
| **Step** | `{step_id, intent, required_capabilities[], dependencies[], preferred_agent, allowed_tools[], success_criteria}`. |
| **Action** | A structured, validated instruction proposed by the model: `model_call`, `tool_call`, `delegate`, `memory_read`, `memory_write`, `memory_delete`, `replan`, `complete`, `fail`, `continue`. |
| **Observation** | The runtime's record of what an action produced: `{observation_id, source, action_type, status, result, timestamp}`. Observations are **data**, never instructions. |
| **Reasoning record** | Execution telemetry about *how* the runtime decided (phase, step, action type, reason category, attempt). Private chain-of-thought is never captured, emitted or persisted. |
| **Control loop** | The bounded state machine of §4 that turns objectives into one deterministic terminal outcome. |
| **Budget** | Runtime-enforced counters/limits (§9). Not descriptions in a prompt. |

## 2. Objective

```
objective_id   stable id for this objective (execution correlation id)
request_id     the parent request/execution id
description    what the caller wants (free text; data)
success_criteria  list of strings (informational in v1, see §7)
constraints    caller-supplied constraints (echoed into decisions as data)
budget         per-objective budget overrides (clamped to server caps, §9)
context        caller-supplied key/value data (data, not authority)
```

Rules:

1. An objective **cannot** change business, division, actor, tools, permissions
   or policy. It is a *request* to the loop, evaluated under the parent
   execution's scope and governance decision.
2. `description` and `context` are untrusted data. They are never parsed for
   authority; they are passed to the model as content.
3. An objective never starts work before the parent request's governance gate
   and approval are satisfied — those run before the runtime at all.

## 3. Planning

`Planner.Plan(ctx, objective, ctxSnapshot) (*Plan, error)`; implementations:
`ModelPlanner` (asks the routed model for a JSON plan — **advisory**) and
`ScriptedPlanner` (deterministic, used by contract tests and the seeded
simulation provider). The runtime treats every plan as untrusted input and
passes it through the validator before any step executes.

Plans carry `plan_id`, `version` (starting at 1), `parent_version`, `reason`
(empty for the initial plan), `steps[]`, `created_at`.

## 4. Control-loop state machine

```
created → planning → executing ⇄ observing → replanning → executing …
        → completed | failed | cancelled | budget_exhausted | deadline_exceeded
```

* Every transition is deterministic and emits exactly one event.
* Terminal states are exactly: `completed`, `failed`, `cancelled`,
  `budget_exhausted`, `deadline_exceeded`. There are no ambiguous states.
* The loop must reach a terminal state within the budgets (§9). It cannot
  continue indefinitely because a model keeps answering `continue`: repeated
  non-action iterations are counted and terminate the loop
  (`max_consecutive_no_progress`, default 2) as `failed`.
* Cancellation is checked at every iteration boundary and propagated through the
  execution context to model calls, tools and children.

## 5. Plan validation (before execution)

A plan is rejected as a whole (strict, fail-closed: one rejected step fails the
objective) when any of the following holds:

| Rejection | Rule |
|---|---|
| unknown agent | `preferred_agent` is not a visible agent of the execution's business/division scope |
| unknown tool | a step/tool names a tool id that is not registered, or not in the agent's `allowed_tools[]` |
| invalid dependency | `dependencies[]` references an unknown step id |
| cycle | the dependency graph is not acyclic |
| cross-business delegation | a step/delegation names a business different from the objective's |
| scope escalation | a step/delegation names a division outside the execution's division scope |
| excessive depth | delegation depth beyond the server cap |
| excessive steps | more steps than the plan cap (§9) |
| invalid schema | missing/blank `step_id` or `intent`, duplicate ids, unknown action verbs |

Rejections are recorded per step (`action.rejected` / `plan.validated` with
`valid:false`) and never reach the executor. A future "drop rejected steps"
policy would be a contract change, not a code toggle.

## 6. Action model and validation

The model proposes exactly one structured action per decision:

```json
{ "type": "tool_call",  "tool": "calculator", "input": {"a":"2","b":"3","op":"mul"} }
{ "type": "delegate",   "agent_id": "researcher", "objective": "find sources" }
{ "type": "memory_write", "key": "notes", "value": "…" }
{ "type": "model_call", "prompt": "…" }
{ "type": "complete",   "result": "…" }
{ "type": "fail",       "reason": "…" }
{ "type": "replan",     "reason": "step 2 blocked" }
{ "type": "continue" }
```

Validation order (all runtime-side): schema → action type → scope →
permissions/tool allowlist → budgets → iteration limit → governance state →
cancellation. Any failure rejects the action (`action.rejected`) and — per the
strict policy — fails the objective. Model output is **never** interpreted as
host operations: the runtime only ever performs the five mediated effects
(tool registry, delegated child execution, memory store, model call through the
provider abstraction, completion).

Unknown action types, extra verbs and non-JSON responses are protocol errors
(`failed`, reason `protocol`), not guesses.

## 7. Completion and success

* `complete` is accepted only when the loop has produced at least one
  observation **or** the plan has no steps requiring work; the terminal state
  records `completed_by: model_request`.
* Semantic verification of `success_criteria` strings is **out of scope for
  v1** — v1 verifies structural success (bounded loop, terminal state, no
  rejected action, result produced). Machine-checkable criteria are a future
  contract decision.
* The runtime decides termination, not the model: a model that never completes
  terminates through budgets (§9) or the no-progress rule (§4).

## 8. Observations and prompt-injection boundary

An observation is `{observation_id, source, action_type, status, result,
timestamp}`. Authority precedence is explicit and enforced:

```
RUNTIME AUTHORITY > MODEL PROPOSAL > TOOL/MEMORY/CHILD DATA > EXTERNAL DATA
```

Consequences:

* tool results, memory values and child results are appended to the model's
  context **as data**, inside a delimited observation block, never as
  instructions;
* no observation text is ever parsed for actions — only the model's structured
  action field is parsed, and only after full runtime validation;
* nothing an observation says can change scope, tools, policy, identity or
  configuration, because those are decided before and outside the loop.

## 9. Budgets (runtime-enforced, §20)

| Budget | Default | Server cap |
|---|---|---|
| `max_iterations` | 8 | 32 |
| `max_model_calls` | 16 | 64 |
| `max_tool_calls` | 8 | 32 |
| `max_delegations` | 3 | 8 |
| `max_replans` | 2 | 4 |
| `max_steps` | 12 | 32 |
| `max_depth` | 2 | 3 (matches Agent Execution v1) |
| `max_execution_time` | 60s | 300s |
| `max_parallel_nodes` | 4 | 8 |

Caller-supplied budgets are clamped to the server caps; exceeding any budget
terminates the loop as `budget_exhausted` (or `deadline_exceeded` for the time
budget) with a `budget.exhausted` event. Budgets are enforced by the runtime
code path, never delegated to the model.

## 10. Delegation

Delegation reuses the Agent Execution Layer delegation primitive
(`agentexec.Spec.Delegates`): a `delegate` action runs a child execution inside
the parent execution context — same business, division, actor, governance
authorization, correlation id, cancellation, and depth accounting. Children
run through the same deterministic Agent Selector: the model may *propose* an
`agent_id`, the runtime resolves and validates it (§5, §6). A child failure is
an observation and, under the strict action policy, fails the objective unless
the plan explicitly marked the delegate as `optional`.

## 11. Memory

Two stores, deliberately separate:

* **Working memory** — process-local, per objective execution, keyed by
  `key`; dies with the execution (G4: no execution recovery).
* **Agent memory** — durable organization-scoped records
  (`store` type `memory`): `{key, value, scope, agent_id, business_id,
  created_at, updated_at, metadata}`. Access rules: the acting agent and the
  agents it legitimately selected in the same business; foreign business or
  unknown agent → denied; division-scoped executions never see records outside
  their division's agents.

Writes happen **only** through an explicit `memory_write` action; there is no
automatic persistence of model output or conversation context. `memory_delete`
removes a key the caller could write. Memory persistence is not execution
recovery: after a restart an active control loop does not resume, while agent
memory records remain readable.

## 12. Failure semantics

| Condition | Terminal state |
|---|---|
| planner error / no agent selectable | `failed` |
| invalid plan (validator rejection) | `failed` |
| invalid model action (protocol/validation) | `failed` |
| tool error | observation `failed`; strict policy ⇒ `failed` |
| child/delegation failure | `failed` |
| budget/limit exhaustion | `budget_exhausted` |
| wall-clock deadline | `deadline_exceeded` |
| cancellation | `cancelled` |
| model `fail` action | `failed` (reason recorded) |
| everything else terminating normally | `completed` |

Failure never becomes success: a model asserting "everything is fine" cannot
overturn any of the above. In the HTTP surface these states surface as
`outcome.metrics.intelligence_state` with `status: failed` (or `cancelled` for
cancellation) — the Agent Execution response contract is unchanged.

Retry: only transient provider errors are retried, exactly as in Agent
Execution v1 (one extra attempt). Authorization, policy, scope, plan and action
failures are never retried.

## 13. Events

Added to the existing event envelope (`foundation/event`), same bus, same
correlation: `objective.started`, `objective.completed`, `plan.created`,
`plan.validated`, `plan.replanned`, `step.started`, `step.completed`,
`action.proposed`, `action.rejected`, `observation.created`, `memory.read`,
`memory.written`, `agent.thinking`, `budget.exhausted`.

`agent.thinking` carries **telemetry only** — `{phase, step_id, action_type,
reason_category, attempt, iteration}` — never raw reasoning text, never model
prompts. Observations are not persisted anywhere durable.

## 14. Durability

Objectives, plans, steps, observations, working memory and loop state are
**process-local**. Agent memory and agent definitions are durable organization
records. After a restart an intelligence execution id answers `404`, exactly
like a request/execution id (CORE §11.1). Nothing in this layer claims
recovery.

## 15. Security boundaries

The intelligence layer is an untrusted-model interface. Model output may only
cause the five mediated effects of §6. It can never: change identity, business,
division, actor or scope; grant itself or another agent a tool; bypass
governance or approval; touch foreign records; create unrestricted agents;
alter configuration; execute host commands. There is no RBAC, no owner, no
superuser concept introduced.

## 16. Out of scope for v1

Multi-agent debate, self-modifying prompts, learned/planned capability
acquisition, vector/embedding memory, semantic retrieval, per-step governance
re-evaluation (the open governance question from Agent Execution v1 is left
open by instruction), cross-business delegation, distributed execution, durable
execution recovery, arbitrary code/sandbox tools.

## 17. HTTP surface

| Method | Path | Semantics |
|---|---|---|
| `POST` | `/api/v1/intelligence/execute` | admit an objective execution → `202 {execution_id, correlation_id, status:"accepted"}` |
| `GET` | `/api/v1/intelligence/{execution_id}?business_id=` | `200` terminal (with `intelligence_state` telemetry), `202` pending, `404` unknown/out-of-scope |
| `POST` | `/api/v1/intelligence/{execution_id}/cancel?business_id=` | E-005 cancellation, identical to requests/executions |

Admission is identical to `POST /api/v1/requests` and
`POST /api/v1/executions`: actor_id must equal the authenticated identity,
membership must cover the declared scope (G3), non-active business/division →
`409 CONFLICT` (G2), visibility `404`/`403` per G5. The distinction between the
two execution surfaces: `/api/v1/executions` runs one declared composition
(one plan of tools/delegates/workflows, one model call); `/api/v1/intelligence`
runs an **objective through the control loop** (plan → validate → decide →
act → observe → replan, bounded by budgets).
