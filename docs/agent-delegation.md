# NEXUS — Agent-to-Agent Delegation (v1)

Implementation: `internal/agentexec/runtime.go` (`execute` delegate loop).

## Shape

```json
"delegates": [
  { "id": "subclass", "intent": "run a subtask", "capabilities": ["research"], "agent_id": null }
]
```

Each delegate is a **child execution**: its own selection, its own bounded
tools, its own model call, its own events — executed inside the parent's
execution context.

## Inherited authority (the invariant)

```
User ─▶ Research Agent ─▶ Coding Agent
        └ scope: business, division, actor, correlation, deadline-context ─┘ (unchanged)
```

A child inherits, verbatim:

* `business_id`, `division_id`, actor identity,
* `correlation_id`/`root_request_id`, so the whole tree is one trace,
* the parent's remaining deadline (context-carried; the child cannot extend it).

What is NOT inherited (v4, `contracts/AGENT_DELEGATION_CONTRACTS.md`):

* **governance decisions and approvals** — the same engine admits every
  effect, but each admission is its own: the parent delegation is admitted on
  `agent:<child>`, and every child effect is admitted on its own target with
  its own fingerprint. Approving the delegation never approves a child
  effect; a pending/escalated/denied child is reported as what it is, never
  adopted, never auto-resumed;
* **capabilities** — the child's effective tools are the intersection of the
  model proposal with the tool registry, the child agent's allowlist, the
  parent agent's allowlist, governance, and platform gates. The delegator
  cannot give what it lacks; the child cannot gain what its definition
  forbids;
* **memory** — children have no durable memory path (no platform, no
  memory-reaching tool), so isolation holds by construction. What the parent
  learns arrives only as child output text inside the parent observation;
* **budget** — delegation count is bounded by the parent's delegation budget,
  nesting depth by `maxDepth`, spec fan-out by `maxFan`. No layer multiplies
  budgets.

A child cannot: switch business, switch division, widen a division scope,
bypass governance/approval, or see a foreign resource. There is no API to
delegate across businesses, and the selector's division rule (§G3) applies
identically to children.

## Observability and bookkeeping

* `agent.delegated` event per child (delegate id, target agent id) on the same
  event bus with the root correlation id;
* every child request carries a runtime-stamped `Delegation` link (parent
  task/agent/correlation, admission proposal id, depth), surfaced on the
  child's `agent.started` event as metadata;
* child outcomes are summarized in the parent's output;
* the parent's terminal result reports `child_executions`;
* a child failure fails the parent (with the child's failure named) — a failed
  child is never silently ignored into a "successful" parent;
* an unknown child outcome stays unknown on the parent observation (with
  reconciliation markers) and is never re-dispatched.

## Limits (v1)

* depth ≤ 3 (`maxDepth`), exceeded depth fails the execution;
* children are synchronous with respect to their parent (they run inline, in
  order of declaration) — parallel fan-out across children is expressed with
  workflow nodes (`strategy: parallel`), not with delegates;
* children are process-local (G4): a restart loses the whole tree; ids of
  children are never exposed as independently addressable executions;
* no cross-business delegation, no "spawn a privileged child" path — the
  foundation spawn-safety counters (`foundation/agent`) remain the authority
  for agent-level spawning; the v1 delegate path cannot create authority the
  parent does not hold.
## Bounded parallel coordination (v5)

`contracts/AGENT_COORDINATION_CONTRACTS.md` is authoritative. In short:

* one typed `coordinate` action names an explicit member list — one concurrent
  wave of mutually independent children. Parallelism is never inferred;
  ordering across waves stays at the plan level (v3 dependencies);
* members run concurrently under one semaphore (`maxConcurrent`, separate
  from the total bound `maxFan`); every member passes its own admission
  before any member starts, so a group is never a blanket authorization;
* members count fully against the parent delegation budget, share the parent
  deadline, and abort with it; cancellation stops new members and settles the
  wave as cancelled/deadline-exceeded;
* settlement is one deterministic parent observation in member declaration
  order (never completion race order): per-member identity, status, outcome
  and truncated output, plus totals per status class. Partial settlement is
  non-terminal — the parent may replan — while unknown stays unknown,
  approvals/escalations stay reported (never adopted, never converted), and
  completed members are fingerprinted so regroups prune them instead of
  re-running them;
* children keep the full v4 boundary (own identity, scope, governance,
  capability intersection, reliability, memory isolation); G4 is unchanged.
