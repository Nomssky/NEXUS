# NEXUS — Development (M7 First Safe Autonomous Agent)

This document covers **only** M7: the first safe autonomous agent.
It does not duplicate architecture or contract docs.

> M7 extends M0–M6. It adds **full agent lifecycle with safety controls**:
> authority chain, anti-spawn-storm, heartbeat/recovery, lease fencing,
> budget enforcement, and basic attention/review routing. It does **not**
> implement memory, attention dedup, or full attention (M8–M9).

---

## Scope (C12 enhanced + basic C10)

| Component | What M7 adds |
|---|---|
| **Agent Lifecycle (C12)** | full lifecycle: identity, capabilities ⊆ authority, permissions, budget, spawn, heartbeat, recovery, termination |
| **Anti-Spawn-Storm** | max children, depth, descendants, spawn rate, TTL |
| **Heartbeat/Recovery** | heartbeat → suspected → unresponsive → recovery |
| **Lease Fencing** | lease expiry detection, zombie stop |
| **Budget Enforcement** | token/cost/task limits, timeout → UNKNOWN |
| **Attention (C10)** | attention items, priority classification, escalation, review routing |

### Invariants preserved by M7

- **capabilities ⊆ authority ⊆ parent authority** — validated at provisioning
- **capability ≠ permission** — distinct concepts
- **Agent cannot modify policy/audit/approval** — governance highest layer
- **Cancel ≠ failure** — safe-boundary preemption
- **Timeout → UNKNOWN, not failure** — not a task failure
- **Spawn storm prevented** — hard limits on children, depth, rate
- **Business isolation** — no cross-business leak
- **Full audit** — immutable, attributable records

---

## Layout

```
internal/foundation/
  agent/
    agent.go          Full lifecycle: identity, authority, budget, spawn, heartbeat, recovery
    agent_test.go     14 tests (TEST-M7-001..014)
  attention/
    attention.go      Attention items, priority, escalation, review routing
    attention_test.go 10 tests (TEST-M7-015..024)
```

---

## Usage

```go
import (
    "github.com/Nomssky/NEXUS/internal/foundation/agent"
    "github.com/Nomssky/NEXUS/internal/foundation/attention"
)

// 1. Create agent with authority chain
ar := agent.NewAgentRuntime()
def := &agent.AgentDefinition{
    ID:         "worker-1",
    Name:       "Worker",
    Type:       agent.AgentTypeWorker,
    BusinessID: "biz-1",
    Authority:  agent.AuthorityRead,
    ParentAuthority: agent.AuthorityWrite,
    Budget:     agent.Budget{MaxTokens: 10000, MaxTaskTime: 5 * time.Minute},
    SpawnLimits: agent.SpawnLimits{MaxChildren: 3, MaxDepth: 2},
}
a, _ := ar.ProvisionAgent(def)
ar.StartAgent(a.ID)

// 2. Assign and execute task
ar.AssignTask(a.ID, &agent.TaskExecution{TaskID: "task-1", Budget: def.Budget})
ar.CompleteTask(a.ID, &agent.TaskOutcome{TaskID: "task-1", Status: "completed"})

// 3. Heartbeat and recovery
ar.Heartbeat(a.ID)
ar.CheckHeartbeat(a.ID, 30*time.Second)

// 4. Spawn child (with anti-spawn controls)
child, _ := ar.SpawnChild(&agent.SpawnRequest{
    ParentAgentID: a.ID,
    Definition:    childDef,
})

// 5. Attention routing
ae := attention.NewAttentionEngine()
item, _ := ae.SubmitItem("High priority", "Needs review", "biz-1", "agent", 9)
ae.Escalate(item.ID)
```

---

## Testing

- 24 new tests covering TEST-M7-001..024
- M0–M6 tests unchanged and passing
- Race detector clean
- Locked-layer guard passes (no `contracts/` or `Core/` changes)
