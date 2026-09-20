# NEXUS — Core Runtime

This document covers the Core Runtime implementation.

---

## What It Is

The Core Runtime (`internal/core/`) is the central orchestrator that wires all 24 foundation packages into a cohesive, running system. It implements the canonical execution chain:

```
OWNER REQUEST
  → VALIDATE
  → GOVERNANCE
  → EXECUTIVE → OBJECTIVE → DECISION → PLANNER
  → WORKFLOW → SCHEDULE → AGENT → MODEL/TOOL
  → VERIFY → OUTCOME
```

---

## Components

| Component | Purpose |
|---|---|
| **Engine** | Central orchestrator, wires all foundation packages |
| **RequestContext** | Carries correlation_id, business_id, objective_id, why_chain |
| **Chain** | Canonical execution chain with audit trail |
| **Health** | Health check integration |

### Engine Lifecycle

```
CREATED → RUNNING → DRAINING → STOPPED
```

### Invariants Preserved

- **WHY preserved** through entire chain
- **Correlation ID propagated** everywhere
- **Business isolation** enforced at every boundary
- **Governance checked** before execution
- **Audit trail** captures every chain step
- **No autonomous action** without owner intent

---

## Layout

```
internal/core/
  context.go      RequestContext, Request, Response, AuditEntry
  engine.go       Engine (wires all foundation packages)
  chain.go        Canonical execution chain
  core_test.go    20 tests (TEST-CORE-001..020)
```

---

## Testing

- 20 tests covering context creation, clone, engine lifecycle, request submission, full chain execution, audit trail, validation, health, event bus, store, WHY chain preservation
- M0–M11 foundation tests unchanged and passing
- Race detector clean
- Locked-layer guard passes (no `contracts/` or `Core/` changes)
