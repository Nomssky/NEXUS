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
        └ authority: business, division, actor, governance decision ─┘ (unchanged)
```

A child inherits, verbatim:

* `business_id`, `division_id`, actor identity,
* the parent's **governance authorization** (no second gate is invented, and no
  second approval mechanism),
* `correlation_id`/`root_request_id`, so the whole tree is one trace.

A child cannot: switch business, switch division, widen a division scope,
bypass governance/approval, or see a foreign resource. There is no API to
delegate across businesses, and the selector's division rule (§G3) applies
identically to children.

## Observability and bookkeeping

* `agent.delegated` event per child (delegate id, target agent id) on the same
  event bus with the root correlation id;
* child outcomes are summarized in the parent's output;
* the parent's terminal result reports `child_executions`;
* a child failure fails the parent (with the child's failure named) — a failed
  child is never silently ignored into a "successful" parent.

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