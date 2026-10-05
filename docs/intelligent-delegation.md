# NEXUS — Intelligent Delegation (v1)

Contract: `contracts/AGENT_INTELLIGENCE_CONTRACTS.md` §10;
`contracts/AGENT_EXECUTION_CONTRACTS.md` §10.
Implementation: `internal/agentintel/loop.go` (`perform` →
`agentexec.DelegateChild`).

## One delegation mechanism, two callers

The intelligence layer does **not** implement delegation. It proposes:

```json
{ "type": "delegate", "agent_id": "researcher", "objective": "find three sources" }
```

and the Agent Execution Layer performs the child execution through its existing
primitive (`DelegateChild` → `Spec.Delegates`). One mechanism, one set of
semantics, one place to audit.

## The model proposes, the selector resolves

* A model-supplied `agent_id` is validated against the registry and the
  execution's scope before anything runs (`action rejected: agent "…" is not
  visible in this scope`).
* With no `agent_id`, the deterministic Agent Selector picks the child exactly
  as it picks any agent — capabilities, scope, lifecycle and tool availability.
  The model can never invent an agent that bypasses the registry.

## Inherited authority

The child execution inherits the parent's request context verbatim: business,
division, actor, correlation id, governance authorization and cancellation. It
is executed by the same executor, so the same admission, capacity and
cancellation rules apply. Depth is bounded (`max_depth`, default 2, cap 3);
exceeding it is an action rejection, not a silent cut.

## Failure and cost

* Child failure is terminal for the objective under the strict action policy,
  with the child's status in the message. A failed child is never folded into a
  "successful" parent.
* Each delegation consumes the delegation budget (3 default, 8 cap).
* Children are process-local: after a restart the whole tree is gone, exactly
  like the parent (G4).

## Sequential by design

Delegates run in declaration order, inline in the parent's loop, so their
observations arrive in a deterministic order. Parallel fan-out across
independent sub-objectives stays in the execution layer's workflow nodes
(`strategy: parallel`, bounded pool) — one place owns concurrency.