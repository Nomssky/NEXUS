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

`Evaluate` is called in exactly three places in the whole runtime — the chain
gate, the task gate, and admission — and all three go through the same
`governance.Engine`. There is no second policy evaluator and no per-adapter
check.

### The gap this milestone closes

An intelligence execution is an ordinary admitted `core.Request`, so it passes the
chain gate and the task gate. But the actions *inside* the loop were governed
only by the task-level decision:

```text
model proposes tool_call → ValidateAction (allowlist, budget) → InvokeToolScoped
                         → Platform.Invoke → adapter          ← no governance here
```

A per-tool policy, a per-operation `REQUIRE_APPROVAL`, or a per-action `DENY`
could never be reached, because governance only ever saw `execute_task`. The same
was true of delegation and durable memory writes, which had no governance
decision of their own at all. V1 adds exactly one admission point, immediately
before each consequential effect, and no second evaluator.

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

`risk_level` is derived from the tool manifest the runtime already validated
(`external_mutation`/`credentialed_external_mutation` → high, `write` → medium,
otherwise low), never from the payload. A tool with no available manifest is
reported as an external mutation, so a risk-conditioned policy cannot be evaded
by naming a tool the runtime does not know. It is evaluation context, not a
privilege: it can make a policy match, never make one stop matching.

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
`delegate`, `memory_write`, `memory_delete`, `model_call`. `resource_type` reuses
the EXISTING policy vocabulary (`SCHEMA_GOVERNANCE_ATTENTION` §2.4): `tool` for a
capability (resource = the tool id), `memory` for memory writes (resource =
`memory:<resolved-record-id>`) and `agent` for delegation (resource =
`agent:<child-agent-id>`). An operator therefore pins a capability with the same
words the policy schema already accepts.

For `memory_write` the record id is the one the memory platform itself derived
by running the same authorization and scope clamping `Write` performs
(`Platform.ResolveWriteTarget`) — not the scope the model requested. For
`memory_delete` it is the set of records the delete would actually remove,
rendered as one deterministic resource id over that set
(`memory-delete:<sorted ids>`); a `resource_restriction` on a delete names the
record ids themselves and must cover **every** member of that set. This is what
makes constraints meaningful: a `resource_restriction` is compared against the
effect that will actually happen (contract §6), so a model cannot route around
it by asking for a different scope or a different key.

**Effect binding (binding rule).** A memory mutation must execute only against
the runtime-established target that was presented to governance. Re-resolution
that can broaden, narrow, or otherwise change the effect after admission is
forbidden:

```text
resolve target → governance admission → constraint enforcement → execute EXACTLY that target
```

There is no independently re-resolved target between admission and mutation. A
delete deletes exactly the admitted record ids and does so as ONE atomic batch:
every id is verified first, the storage mutation is a single `store.DeleteBatch`
(never a per-record loop), and any failure leaves the store untouched and
publishes no deletion event — a governed delete is never partially applied and
never reported as applied when it was not (`Platform.DeleteExact`, contract
memory §10). A write is executed with `Platform.WriteBound`: the platform
re-derives the target inside the same critical section that guards the mutation,
compares it to the admitted target, and persists the record built from that
compared target, so a mismatch — or an authorization that no longer holds —
refuses the write before anything is stored. Model-supplied scope, key or record
selection carries no authority after admission.

### Which actions are admitted

Every **consequential** action is admitted, not only tool calls:

| action | admitted | why |
|---|---|---|
| `tool_call` | yes | it reaches an external system through the capability platform |
| `delegate` | yes | it starts another execution |
| `memory_write` | yes | it changes persistent state |
| `memory_delete` | yes | it removes persistent state |
| `model_call` | no | it has no side effect beyond the request itself, and the request was already admitted |
| `memory_read` | no | reading is not a side effect; it is already bounded, scoped and authorized by the memory platform |
| `complete`, `fail`, `continue`, `replan` | no | they control the loop, not the world |

`memory_write`/`memory_delete` are admitted **on their own merits**, which is the
point: a record claiming authority is admitted or refused by governance like any
other action, never trusted because it is in memory. The runtime establishes the
scope (contract §2), so a model cannot widen it and cannot escape governance by
choosing a different write.

## 3. Admission semantics

One admission call, five canonical outcomes, no sixth:

| decision | what the runtime does |
|---|---|
| `ALLOW` | the action proceeds to the capability platform |
| `ALLOW_WITH_CONSTRAINTS` | the returned constraints become **runtime restrictions** on the effective request, then it proceeds |
| `DENY` | the capability platform is **never** invoked; the loop records a structured `denied` observation; the objective may only replan within its remaining authority |
| `REQUIRE_APPROVAL` | an approval request is created through the existing `ApprovalEngine`; the execution stops in a non-running `pending_approval` state before any side effect |
| `ESCALATE` | the action stays blocked; the existing escalation → attention path is used; the objective terminates `escalated` |

An effect that governance did not allow cannot happen: the admission point is
*before* the effect, so `Platform.Invoke` is not reached for a blocked tool call,
no child execution starts for a blocked delegation, and no record is written or
deleted for a blocked memory action. No adapter knows about governance.

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
| `max_duration_ms` | integer milliseconds | tightens the call's duration limit, or gives a duration-free path one; never widens a bound |
| `resource_restriction` | comma-separated resource ids | a resource outside the list fails closed |

Rules:

* `severity: mandatory` (the default) must be enforceable, else **fail closed**
  with a structured governance failure — never silently ignored;
* `severity: advisory` is recorded in the observation and in
  `governance.constraint_applied`, and is by definition not a restriction;
* an unknown `constraint_type` fails closed;
* a model cannot remove, weaken or "ignore" a constraint: constraints travel from
  the decision into the invocation, and the capability platform still applies its
  own allowlist, scope and manifest gates afterwards;
* enforcement is NOT optional and is NOT per-action-type. For a tool call the
  constraint restricts the capability invocation (`control.Constrain`). For a
  delegation the constraint restricts the delegated agent id, and for a durable
  memory write/delete it restricts the runtime-established record id
  (`control.ConstrainEffect`); for a delete, which targets a set, compliance
  requires the restriction to cover **every** member id. A constraint the
  runtime cannot check against that target fails closed rather than being treated
  as satisfied. A `max_duration_ms` constraint against a non-tool effect fails
  closed, because such an effect has no deadline to bound: enforcing it would
  require inventing one.

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
  repeated identical proposal needs a fresh approval. This is enforced by the
  controller's process-local ledger of spent `execution_id|fingerprint` keys, not
  by the approval record itself: a record stays APPROVED after it is spent, so
  the ledger is what makes the authorization single-use. The ledger is bounded
  (4096 entries, FIFO); evicting an entry can only cause one *extra* admission to
  re-ask, never a silent allow, because a spent key's absence simply restores the
  approval-gate evaluation.
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

`ESCALATE` blocks the action absolutely until a human decides: an ack moves the
record to `acknowledged`, a resolve or expiry closes it, and none of those
grants admission. Continuation requires the human to re-submit the work, which
produces a NEW proposal that governance evaluates from scratch — it never
inherits the escalated proposal's state.

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
admits before touching the platform **and before the in-process builtin
fallback**, which is an execution path too, not an exception;
`agentintel` admits before delegating and before every durable memory write or
delete; `core.ApproveRequest` resolves the approver through the existing
`ApprovalEngine` rules.

The model cannot install or change a policy either: policies are written only
through `/api/v1/control/policies`, which requires the control-plane API key
(`X-API-Key`), a separate credential from the actor identity. That is the existing
control-plane boundary, not a new authorization layer.

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

## 10. Deferred / not implemented

Recorded rather than implied: scheduler; event-triggered automation; browser
automation; unrestricted shell; arbitrary code/WASM execution; distributed
execution; durable execution recovery; persistent (durable) approval workflow;
RBAC; multi-instance governance; autonomous privilege escalation; a
general-purpose policy DSL or expression evaluator; tool self-installation or
modification; a second policy engine; a second authorization layer; a new event
bus; a new memory store; vector DB, embeddings or RAG; UI; virtual office.

Also not implemented, deliberately: a governance decision at the request or task
gate still only *reports* its `ALLOW_WITH_CONSTRAINTS` values. Enforcement
(contract §4) applies to agent actions, which are the ones that reach a
consequential effect.