# NEXUS — Tool Execution Boundary (v1)

Implementation: `internal/foundation/tool` (registry + validation),
`internal/agentexec/tools.go` (v1 deterministic executors),
`internal/agentexec/runtime.go` (`runAgentUnit` gather phase).

## The boundary

```
AgentRuntime
  └─ ToolCallSpec{tool_id, input}
       ├─ 1. agent allowlist check      (agent.AllowedTools ⊇ tool_id)   → else failed
       ├─ 2. registry existence check   (tool.Registry.GetTool)           → else failed
       ├─ 3. scope validation           (tool.Registry.ValidateRequest: business isolation) → else failed
       └─ 4. deterministic executor     (BuiltinExecutor)                 → else failed
```

An agent can never call a host function, a shell, or a path: it can only name
a tool id that (a) it is allowed to use and (b) exists in the registry. Both
checks happen *before* any executor runs, and the executor set is fixed,
in-process, side-effect-free code.

## v1 tool catalog (deterministic, side-effect free)

| id | input | output | notes |
|---|---|---|---|
| `echo` | `{"text"}` | `{"echoed"}` | identity |
| `calculator` | `{"a","b","op": add|sub|mul|div}` | `{"result"}` | div-by-zero is an error |
| `transform` | `{"text","op": upper|lower|reverse}` | `{"transformed"}` | string ops |

## Failure semantics

Any tool error (unknown id, not allowlisted, missing executor, executor error,
bad input) ends the execution *unit* as `failed` with the tool id in the
message. Tool failure never becomes execution success. In a sequential workflow
the failing node fails the workflow; in a parallel workflow every node runs and
the workflow fails if any node failed.

## Out of scope for v1 (documented, not silently missing)

Shell/command execution, arbitrary filesystem access, network fetch, browser
automation, arbitrary code, WASM, and any privileged host access are *not*
implemented and have no configuration switch in v1. They are future milestones
that must arrive with an explicit sandbox/isolation contract; the v1 boundary
is deliberately structured so those tools can be added behind the same
registry/allowlist checks without weakening them.

## Observability

`tool.requested`, `tool.started`, `tool.completed` events carry
`tool_id`, `agent_id`, and (on completion) the tool name; the terminal result
reports `tools_executed`. Inputs/outputs of tools are not dumped into events
by default (they stay in the executor's returned outcome summary).