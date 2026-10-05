# NEXUS — Workflow Execution (v1)

Implementation: `internal/agentexec/runtime.go` (`runWorkflow`, `topoSort`).

## Shape

```json
"workflow": {
  "strategy": "sequential" | "parallel",
  "nodes": [
    { "id": "a", "intent": "first", "capabilities": [], "tools": [], "depends_on": [] }
  ]
}
```

A workflow is an **execution primitive**, not a UI concept: it is a
declaration carried by one execution request, executed inside one runtime run,
inside one governance authorization, inside one scope.

## Sequential

```
A → B → C
```

Nodes run in declaration order. First failure wins: the workflow ends `failed`
and later nodes never run.

## Parallel

```
   ┌→ B ─┐
A ─┤     ├→ D
   └→ C ─┘
```

Nodes are grouped by dependency depth (`depends_on`); each group runs
concurrently under a bounded pool (`maxFan`, default 4). All nodes in flight
run to completion (no partial cancellation semantics inside one workflow);
the workflow succeeds only if *every* node succeeded, and reports the failing
nodes.

Validation: node ids required and unique, `depends_on` must reference known
nodes (a cycle degrades to "run what you can" and surfaces as node failures
rather than an infinite loop).

## Failure and cancellation

* node failure ⇒ workflow `failed` (sequential: early stop; parallel: full run)
  — never a partially successful workflow reported as success;
* request cancellation propagates through the execution context: an executing
  workflow node sees ctx cancellation between bounded units and the request
  ends `cancelled` (E-005), never `completed`;
* workflow start/complete/fail emit `workflow.started`, `workflow.completed`,
  `workflow.failed` with the request's correlation id.

## Relationship to the legacy workflow engine

The Core Runtime's workflow/scheduler stages still run for every request
(they create the workflow record that the executor gate and `core` chain use).
The Agent Execution Layer's workflow is a *node composition* on top of that
request-level execution, executed by the agent runtime. They are different
layers and do not shadow each other: the request pipeline owns admission,
governance and cancellation; this layer owns how the admitted work is composed
across agents, tools and models.