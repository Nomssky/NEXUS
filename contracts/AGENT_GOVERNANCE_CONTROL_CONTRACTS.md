# Agent Governance & Control Integration v1 — Contract

**Status:** v1, additive. It closes the control boundary between Agent
Intelligence and the existing Governance Engine. **No locked contract section is
modified, and the following remain authoritative unchanged:**

* G1–G5 (`docs/ARCHITECTURE_DECISIONS.md` §12, §11)
* `governance.Engine` and its five canonical outcomes
  (`SCHEMA_GOVERNANCE_ATTENTION.md` §2)
* `governance.ApprovalEngine` (SCHEMA_WORK §6)
* `internal/executor` task gate and `internal/core` chain gate
* Agent Execution v1, Agent Intelligence v1, Capability & Tool Platform v1,
  Operational Reliability v1, Agent Memory & Context Platform v1
* `identity.MembershipSet.AllowsScope`, the event bus, the error taxonomy

The milestone invariant (one sentence):

> **A model may propose an action; only governance may admit it — and admission
> is re-evaluated, never remembered as permission.**

---

## 1. Audit: what already exists (and is reused, not duplicated)

| concern | existing implementation | disposition |
|---|---|---|
| policy evaluation | `governance.Engine.Evaluate` — 5 outcomes, default DENY, more-restrictive-wins, precedence, conditions | **authoritative, reused unchanged** |
| task-level gate | `executor.checkGovernance` (`execute_task`/`workflow`) once per task | unchanged |
| request-level gate | `core.chainGovernance` (`execute_request`/`core`) once per request | unchanged |
| approval records | `governance.ApprovalEngine` (pending/approver list/self-approval/timeout) | **reused**, incl. for action-level requests |
| approval resume | `core.ApproveRequest` → re-submit the original request; `approvalStateFor` feeds `Request.ApprovalState` so governance re-evaluates | **extended** with an action-level binding |
| escalation | `core.emitEscalation` → `governance.escalated` → engine intake → escalation queue + attention item | **reused unchanged** |
| allowlist/scope | `capability.Platform.CanInvoke`/`Invoke`, `MembershipSet.AllowsScope` | unchanged, strictly downstream of admission |
| constraints | `governance.Constraint{type, expression, severity}` — previously rendered as text and otherwise ignored | **given enforceable semantics** (bounded closed set, fail closed) |
| events | `governance.decided` (declared, never published), `approval.*`, `governance.escalated` | reused; only `governance.action_proposed` and `governance.constraint_applied` are new |

### The gap this milestone closes

An intelligence execution is an ordinary admitted `core.Request`, so it passes the
chain gate and the task gate. But the actions *inside* the loop were governed
only by the task-level decision:

```text
model proposes tool_call → ValidateAction (allowlist, budget) → InvokeToolScoped
                         → Platform.Invoke → adapter          ← no governance here
```

A per-tool policy, a per-operation `REQUIRE_APPROVAL`, or a per-action `DENY`
could never be reached, because governance only ever saw `execute_task`. V1 adds
exactly one admission point for agent actions, immediately before the capability
platform, and no second evaluator.

## 2. The ActionProposal boundary

```text
ActionProposal {
    proposal_id, correlation_id, execution_id, objective_id, step_id,
    actor_id, agent_id, business_id, division_id,
    action, resource, resource_type,
    tool_id?, operation?,
    constraints []Constraint,
    risk_level, context map[string]string,
    created_at
}
```

**The model may propose only `action`, `resource`, `resource_type`, `tool_id`,
`operation` and request input.** Every other field is established by the runtime
from the admitted request, the resolved agent and the executing loop:

| field | established from | never from |
|---|---|---|
| `actor_id` | authenticated identity of the admitted request | the action payload |
| `business_id` / `division_id` | request context (G3 membership already checked) | the action payload |
| `agent_id` | the agent the runtime selected | the action payload |
| `objective_id` / `execution_id` / `step_id` | the running objective | the action payload |
| `correlation_id` | request correlation | the action payload |
| `proposal_id` | runtime-generated | the action payload |

`agentintel.Action` has no field for any of those, so the separation is
structural, not merely documented. A proposal whose established fields are
missing or contradictory is rejected before governance is consulted.

### Action and resource vocabulary

`action` is derived from the closed action vocabulary: `tool_call`,
`delegate`, `memory_write`, `memory_delete`, `model_call`. `resource_type` is
`capability` for tool calls (resource = the tool id) and `memory` for memory
writes (resource = `memory:<scope>`), `agent` for delegation (resource = the child
agent id).

## 3. Admission semantics

One admission call, five canonical outcomes, no sixth:

| decision | what the runtime does |
|---|---|
| `ALLOW` | the action proceeds to the capability platform |
| `ALLOW_WITH_CONSTRAINTS` | the returned constraints become **runtime restrictions** on the effective request, then it proceeds |
| `DENY` | the capability platform is **never** invoked; the loop records a structured `denied` observation; the objective may only replan within its remaining authority |
| `REQUIRE_APPROVAL` | an approval request is created through the existing `ApprovalEngine`; the execution stops in a non-running `pending_approval` state before any side effect |
| `ESCALATE` | the action stays blocked; the existing escalation → attention path is used; the objective terminates `escalated` |

A capability invocation that governance did not allow cannot happen: the
admission point is *before* `Platform.Invoke`, and the capability platform is not
consulted for a blocked proposal. No adapter knows about governance.

### Terminal states the loop must distinguish

`completed`, `failed`, `cancelled`, `timed_out`, `unknown`, `denied`,
`pending_approval`, `escalated`. A governance denial is never collapsed into a
tool failure; approval-pending is never a failure; escalation is never a failure.
The execution *outcome* (`unknown` from Operational Reliability) stays a
different axis from the *governance* state: the two are never merged.

## 4. Constraints are enforceable, not informational

`ALLOW_WITH_CONSTRAINTS` yields `governance.Constraint`s. V1 enforces a **closed
set** of types with bounded, existing-shaped semantics:

| constraint_type | expression | effect on the effective request |
|---|---|---|
| `tool_allowlist` | comma-separated tool ids | a tool outside the list fails closed |
| `operation_allowlist` | comma-separated operations | an operation outside the list fails closed |
| `max_duration_ms` | integer milliseconds | tightens the capability duration limit (never widens it) |
| `resource_restriction` | comma-separated resource ids | a resource outside the list fails closed |

Rules:

* `severity: mandatory` (the default) must be enforceable, else **fail closed**
  with a structured governance failure — never silently ignored;
* `severity: advisory` is recorded in the observation and in
  `governance.constraint_applied`, and is by definition not a restriction;
* an unknown `constraint_type` fails closed;
* a model cannot remove, weaken or "ignore" a constraint: constraints travel from
  the decision into the invocation, and the capability platform still applies its
  own allowlist, scope and manifest gates afterwards.

## 5. Approval is not permission forever

```text
proposal → governance → REQUIRE_APPROVAL → approval record → approver
        → APPROVED → RE-EVALUATE GOVERNANCE → ALLOW → capability
```

The approval record is bound to a **fingerprint** of the proposal:

```text
fingerprint = sha256(correlation_id | execution_id | objective_id | actor_id |
                      business_id | division_id | agent_id | action |
                      resource | resource_type | tool_id | operation |
                      sorted(constraint ids+expressions))
```

Rules:

* the resume path re-runs the original request; the loop re-proposes the action;
  admission finds an approved record **only** for the identical fingerprint and
  passes `ApprovalState=approved` to the same governance engine, which then
  re-evaluates and converts only the approval gate to `ALLOW`;
* a changed action, resource, tool, operation, scope or constraint set produces a
  different fingerprint and therefore **requires fresh approval**;
* an approved record authorizes exactly **one** admission after the resume: a
  repeated identical proposal needs a fresh approval;
* an approval never overrides an explicit `DENY`, a revoked identity, a suspended
  business/division, a disabled capability, a changed scope or a security
  rejection — governance re-evaluation is authoritative and the capability platform
  still applies its own gates;
* a missing, stale, expired, denied or mismatched approval leaves the action
  blocked. Freshness uses the policy's existing `timeout_seconds` /
  `auto_deny_on_timeout` semantics via `ApprovalEngine.CheckTimeouts`.

## 6. Escalation

`ESCALATE` blocks the action, publishes the existing `governance.escalated`
event with the proposal's correlation/objective context, and lets the existing
intake queue an escalation plus an attention item. Attention prioritizes and
interrupts; it never authorizes. An attention resolution that would result in
execution must re-enter governance like any other proposal.

## 7. Observability

| event | status |
|---|---|
| `governance.action_proposed` | **new** — the proposal exists (fact) |
| `governance.decided` | existing vocabulary, now published per admission with proposal metadata |
| `governance.constraint_applied` | **new** — constraints became runtime restrictions |
| `approval.requested` / `approved` / `denied` / `expired` | existing |
| `governance.escalated` | existing |

Payloads are metadata only: ids, scope, action, resource, decision, matched
policy id/version, constraint ids, counts. Never prompt contents, never
chain-of-thought, never secrets or credential material. Events are facts: no
event grants authority, and no consumer of an event may execute an action
without admission.

## 8. Model limitations (structural, not documented-only)

The model cannot: select an actor; change business/division; grant itself a tool;
change an agent allowlist; bypass admission; change an outcome; create an
approval that authorizes itself; approve its own request; mark an action
approved; modify a policy; change capability lifecycle; disable security; widen
memory scope; or treat memory, tool output or attention alerts as authority.

Enforcement points: `agentintel.Action` has no authority fields;
`ValidateAction` still checks the agent allowlist; `agentexec.InvokeToolScoped`
admits before touching the platform; `core.ApproveRequest` resolves the approver
through the existing `ApprovalEngine` rules.

## 9. Scope, identity and durability

* **G3**: membership/scope uses `MembershipSet.AllowsScope`; admission never
  establishes scope, it only asks governance about an action inside a scope the
  request pipeline already authorized.
* **G5**: a foreign business/division proposal is never admitted and its
  existence is not leaked by the control surface.
* **G2**: suspended/archived business or division still admits no new work
  (unchanged, gateway admission).
* **G1**: no owner/superuser/RBAC behaviour is invented.
* **G4**: approval and escalation records are **process-local**, exactly like the
  existing P1 decision. After a restart an old approval or execution is
  unavailable (`404`) and must be re-requested. This milestone does not add
  durable approval or durable execution recovery, and does not claim otherwise.

## 10. Non-goals

No second policy engine, no RBAC, no model-driven policy, no autonomous
privilege escalation, no persistent approval workflow, no multi-instance
governance, no durable execution recovery, no distributed coordination, no new
event bus, no new memory store, no shell/browser/code execution, no vector DB,
embeddings or RAG, no UI.