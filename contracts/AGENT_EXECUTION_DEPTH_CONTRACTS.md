# NEXUS — Agent Execution Depth Contracts (v3)

**Layer:** Agent Intelligence / bounded multi-step execution
**Status:** MAKER — guidance for all current implementations
**Depends on:**

- `AGENT_INTELLIGENCE_CONTRACTS.md`
- `AGENT_MODEL_PROTOCOL_CONTRACTS.md` (v2)
- `AGENT_GOVERNANCE_CONTROL_CONTRACTS.md`
- `AGENT_MEMORY_CONTEXT_CONTRACTS.md`
- `CAPABILITY_TOOL_CONTRACTS.md`
- `OPERATIONAL_RELIABILITY_CONTRACTS.md`

---

## 1. Purpose

This contract turns the typed model-decision runtime (v2) into a robust
bounded multi-step execution runtime. The target loop is:

```text
Objective → Plan → Action → Governance → Effect → Observation →
State Update → Replan → Action → … →
Completed / Failed / Cancelled / Budget Exhausted / Deadline Exceeded
```

It establishes four inequalities as architecture, not documentation:

```text
PLAN ≠ AUTHORITY          (a plan is validated structure, never permission)
ACTION ≠ EFFECT           (a proposal becomes an effect only through the
                           mediated path)
OBSERVATION ≠ INSTRUCTION (only runtime/tool evidence creates facts)
MODEL OUTPUT ≠ RUNTIME STATE (the runtime owns every state transition)
```

## 2. Canonical plan model

One typed plan representation. A plan has an identity (runtime-assigned),
an objective association, an immutable version, and a bounded ordered step
list. A step carries an id, an intent, optional dependencies, and advisory
hints (capabilities, tools, agent preference). Steps carry NO actions, NO
authority, NO state — state lives in the runtime ledger (§3), never in the
model payload.

## 3. Step state machine (runtime-owned)

Each step of the active plan version has exactly one runtime-owned status:

```text
pending → ready → executing → observing → completed
                        ↓          ↓
                      failed    blocked / cancelled
```

- **pending**: unresolved dependencies.
- **ready**: every dependency completed successfully; eligible for execution.
- **executing**: the runtime began the attributed action.
- **observing**: the effect returned; the runtime is normalizing the result.
- **completed**: the runtime holds authoritative evidence of success
  (an observation produced by performing the step's action).
- **failed**: the runtime holds authoritative evidence of failure.
- **blocked**: a dependency failed (or cannot succeed); the step cannot run
  under this plan version.
- **cancelled**: cancellation prevented completion.

Transitions are performed by the runtime only, in this order, and are
emitted as metadata-only step events (`step.ready`, `step.started`,
`step.observing`, `step.completed`, `step.failed`, `step.blocked`). The model
cannot assign, advance, or rewrite a step status: plans and decisions have no
status fields, and unknown fields fail closed at the protocol boundary.

## 4. Dependencies

The model may propose `dependencies: ["step-a"]`. The runtime validates,
before anything executes:

- every referenced step exists in the same plan;
- no self-dependency, no duplicate dependency;
- no cycle (deterministic detection);
- the graph is bounded (step count within budget).

An invalid graph fails the plan before any effect. At runtime the loop
executes the first ready step in plan order — never array order when array
order violates dependencies. A step whose dependency failed or is blocked
becomes blocked itself (transitively), never executed.

## 5. Plan validation (structural, not governance)

One authoritative validator checks structure only: non-empty steps, unique
step ids, valid bounded dependency graph, acyclicity, known tools/operations,
scope-visible agents, closed vocabularies, and absence of authority-shaped
fields (identity, scope, approval, governance, budget, deadline). It grants
nothing: every consequential action still passes per-effect governance
admission (§8).

## 6. Budgets

The existing execution budget is the only budget. Replanning consumes
`MaxReplans`; decisions consume `MaxModelCalls`; effects consume their own
counters; wall-clock consumes `MaxExecutionTime`. Exhaustion terminates with
the existing `budget_exhausted` outcome. No new terminal outcome, no second
budget subsystem, no model-raised limits.

## 7. Replanning

A replan is an explicit bounded event: the model proposes `replan` with a
reason, the runtime asks for a fresh plan, validates it, and activates it as
a NEW immutable version (never a mutation of the old one). Completed steps
are carried forward by step id with their original observation and version —
immutable historical fact. Failed steps may be retried only under an
explicitly validated new plan; completed effects are never re-executed
automatically. Replanning changes neither deadline nor budget nor authority.

## 8. Governance per effect

```text
model proposal → structural validation → runtime target establishment →
governance admission → capability / memory / delegation → effect
```

There is no plan-level authorization: approving step A never authorizes step
B. Approval stays bound to (request, action, fingerprint); resume revalidates,
re-establishes the target, and re-admits. Escalation blocks the action and
enters the existing attention path. `ALLOW_WITH_CONSTRAINTS` is enforced
against the actual runtime target per effect; unenforceable constraints fail
closed.

## 9. Observations and completion

Every effect result normalizes to an observation preserving outcome semantics:
`completed`, `failed`, `timed_out`, `cancelled`, and `unknown` — unknown is
never collapsed into failed and never triggers automatic redispatch. Only
runtime/tool evidence creates an observation; the model cannot manufacture
one. A `complete` decision is accepted only with runtime evidence of progress
(at least one observation from this execution); a completion claim with no
evidence fails the execution instead of falsely completing it.

## 10. Duplicate effects

The runtime records a fingerprint of every completed consequential effect in
this execution (action type + target + payload hash, bounded). A proposal
identical to an already-completed effect is rejected — never silently
re-executed. Cross-execution duplication is out of scope: each execution owns
its own scratch state, and approval resume is a new execution.

## 11. Cancellation and deadline

Cancellation is checked before planning, before every step, before
governance, before every effect, during model inference, during tool
execution, and before replanning. After cancellation no new consequential
effect begins. The caller deadline is fixed at execution start and never
extended by replanning; expiry yields the existing `deadline_exceeded`
outcome. The model cannot request more time, steps, or replans.

## 12. Context

After every effect the next decision receives authoritative runtime state —
active plan version, per-step statuses, completed/failed/blocked sets,
observations, objective, and remaining budget — rendered inside the v2
typed context (never inside the system instructions, never from raw
conversation memory). External content stays delimited data.

## 13. Observability and limits

Step events carry ids, versions, statuses and references only — no prompts,
responses, credentials, or reasoning. Execution state stays process-local
(G4): no durable execution recovery, no scheduler, no background workers, no
durable scheduler state. Every loop remains bounded by budget, deadline and
replan limits.
