# Doc Processor — Example NEXUS Agent

A minimal but complete NEXUS agent that demonstrates the full stack:
Core Runtime + HTTP Gateway + Agent + Tools.

## What It Does

Processes "document processing" requests through the canonical NEXUS chain:

```
OWNER REQUEST
  → VALIDATE
  → GOVERNANCE
  → OBJECTIVE → DECISION → PLAN
  → WORKFLOW → SCHEDULE → AGENT
  → TOOL (read → extract → validate → store)
  → VERIFY → OUTCOME
```

## Quick Start

```bash
# Run the agent
go run ./examples/doc-processor

# In another terminal, submit a request
curl -X POST http://localhost:9090/api/v1/requests \
  -H 'Content-Type: application/json' \
  -d '{
    "intent": "process invoice INV-2024-001",
    "business_id": "acme-corp",
    "actor_id": "user-1"
  }'

# Check the result (use the request_id from the response)
curl http://localhost:9090/api/v1/requests/{request_id}

# Health check
curl http://localhost:9090/health

# Engine status
curl http://localhost:9090/status
```

## Components Demonstrated

| Component | What It Shows |
|---|---|
| **Core Engine** | Wires all 24 foundation packages |
| **Execution Chain** | 12-step canonical flow with audit trail |
| **Governance** | Default allow policy for runtime operation |
| **Agent Runtime** | Agent provisioning, start, lifecycle |
| **Tool Registry** | 4 registered tools (read, extract, validate, store) |
| **HTTP Gateway** | REST API with health, ready, status endpoints |
| **Request Context** | Correlation ID, business ID, actor ID propagation |
| **Event Bus** | Completion events via MemBus |

## Architecture

```
examples/doc-processor/main.go
  ├─ core.Engine          ← orchestrator
  │   ├─ governance       ← policy check
  │   ├─ cognition        ← objective → decision → plan
  │   ├─ workflow         ← plan → workflow
  │   ├─ scheduler        ← workflow → job
  │   ├─ agent            ← job → agent execution
  │   ├─ tool             ← registered tools
  │   ├─ modelrouter      ← model routing
  │   ├─ memory           ← memory store
  │   └─ attention        ← attention scoring
  └─ gateway.Server       ← HTTP API
```

## Configuration

| Env Variable | Default | Description |
|---|---|---|
| `NEXUS_ADDR` | `:9090` | HTTP listen address |

## What's NOT Here (Intentionally)

- No external model calls (foundation only, no API keys)
- No persistent storage (in-memory for demo)
- No real document processing (simulated chain execution)
- No authentication (permissive governance for demo)

This is a **structural demo** — it proves the architecture works end-to-end.
Real document processing logic would be added in a domain-specific agent.
