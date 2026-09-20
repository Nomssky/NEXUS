# NEXUS — Development (M5 First Bootable NEXUS)

This document covers **only** M5: the first bootable runtime layer.
It does not duplicate architecture or contract docs.

> M5 extends M0–M4. It adds the **Workflow Engine, Scheduler,
> Agent Runtime, and Tool Runtime** — the execution layer that turns
> plans into durable autonomous work. It does **not** implement model
> routing (M6), full agent lifecycle (M7), or memory/attention (M8–M9).

---

## Scope (C11 + minimal C12 + minimal C13)

| Component | What M5 adds |
|---|---|
| **Workflow Engine (C11)** | workflow lifecycle, task graph, state machine, verification, cancellation |
| **Scheduler (C11)** | job queue, priority ordering, task leasing, deadline expiry, retry |
| **Agent Runtime (C12)** | agent definition, provisioning, task assignment, heartbeat, termination |
| **Tool Runtime (C13)** | READ-ONLY tool registry, validation, authorization, business isolation |

### Invariants preserved by M5

- **WHY mandatory for workflows** — every workflow requires purpose
- **Business isolation** — workflows, jobs, agents, tools respect scope
- **Governance never bypassed** — tool execution goes through validation
- **Read-only tools only** — M5 only supports non-side-effecting tools
- **Task dependencies enforced** — blocked tasks wait for predecessors
- **Verification from evidence** — task completion requires evidence

---

## Layout

```
internal/foundation/
  workflow/
    workflow.go        Workflow lifecycle, task graph, state machine
    workflow_test.go   13 tests (TEST-M5-001..013)
  scheduler/
    scheduler.go       Job queue, priority, leasing, deadline
    scheduler_test.go  10 tests (TEST-M5-014..023)
  agent/
    agent.go           Agent definition, lifecycle, task execution
    agent_test.go      8 tests (TEST-M5-024..031)
  tool/
    tool.go            READ-ONLY tool registry, validation
    tool_test.go       7 tests (TEST-M5-032..038)
```

---

## Usage

```go
import (
    "github.com/Nomssky/NEXUS/internal/foundation/workflow"
    "github.com/Nomssky/NEXUS/internal/foundation/scheduler"
    "github.com/Nomssky/NEXUS/internal/foundation/agent"
    "github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// 1. Create workflow from plan
we := workflow.NewWorkflowEngine()
wf, _ := we.CreateWorkflow("plan-1", "obj-1", "biz-1", "Deploy", "Desc", "WHY", "user")
task1, _ := we.AddTask(wf.ID, "Setup", "Setup infra", nil, []string{"ready"})
task2, _ := we.AddTask(wf.ID, "Deploy", "Deploy app", []string{task1.ID}, nil)
we.Activate(wf.ID)
we.Start(wf.ID)

// 2. Schedule tasks
s := scheduler.NewScheduler()
s.SubmitJob(task1.ID, wf.ID, "biz-1", scheduler.PriorityNormal, nil)
job := s.LeaseJob("agent-1", "biz-1")

// 3. Execute via agent
ar := agent.NewAgentRuntime()
agentDef := &agent.AgentDefinition{ID: "def-1", Name: "Worker", Type: agent.AgentTypeWorker, BusinessID: "biz-1"}
a, _ := ar.ProvisionAgent(agentDef)
ar.StartAgent(a.ID)
ar.AssignTask(a.ID, task1.ID, wf.ID)

// 4. Use read-only tool
tr := tool.NewToolRegistry()
tr.RegisterTool(&tool.ToolDefinition{ID: "tool-1", Name: "Read", Category: tool.ToolCategoryRead, ReadOnly: true})
resp, _ := tr.ExecuteTool(&tool.ToolRequest{ID: "req-1", ToolID: "tool-1", AgentID: a.ID, BusinessID: "biz-1"})

// 5. Complete and verify
we.CompleteTask(task1.ID, &workflow.TaskResult{Status: workflow.TaskStatusCompleted, Output: "done"})
we.VerifyTask(task1.ID, true, []string{"evidence"})
```

---

## Testing

- 38 new tests covering TEST-M5-001..038
- M0–M4 tests unchanged and passing
- Race detector clean
- Locked-layer guard passes (no `contracts/` or `Core/` changes)

---

## 17-Step Boot Sequence Coverage

| Step | M5 Coverage |
|---|---|
| 1–6 | M0–M4 (config, identity, persistence, governance, observability) |
| 7 | Executive classifies owner intent ✅ |
| 8 | Objective persisted with WHY ✅ |
| 9 | Workflow accepts validated plan ✅ |
| 10 | Scheduler leases task to agent ✅ |
| 11 | Agent runtime provisions, runs, heartbeats ✅ |
| 12 | Model Router (M6 scope — deferred) |
| 13 | READ-ONLY tool execution ✅ |
| 14 | Task verification from evidence ✅ |
| 15 | Outcome persisted with WHY ✅ |
| 16 | Safe shutdown (cancel workflow) ✅ |
| 17 | Recovery (state reconstruction from store) ✅ |
