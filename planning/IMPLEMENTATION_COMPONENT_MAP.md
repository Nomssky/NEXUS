# NEXUS — Implementation Component Map

PLANNING ARTIFACT. Derived from the LOCKED architecture (21 canonical `Core/`
modules) and LOCKED Phase 1–4 contracts + `CONTRACTS_CROSS_PHASE_AUDIT.md`.
This document does NOT modify architecture or contracts. It maps architecture
modules → implementation components (C01–C14) and binds each locked invariant to
the component(s) and test layer(s) that preserve it.

Primary plan: `IMPLEMENTATION_PLAN.md`.

---

## 1. Module → Component Mapping (21 → 14)

| Architecture module (canonical) | Component | Layer |
|---|---|---|
| `NEXUS_CONFIGURATION_CONTROL_PLANE.md` | C01 Foundation & Config | L0 |
| `NEXUS-IDENTITY-TRUST-SYSTEM.md` | C02 Identity & Trust | L1 |
| `NEXUS-GOVERNANCE-POLICY-SAFETY-CONTROL.md` | C03 Governance & Policy | L1 |
| `NEXUS-SECURITY-THREAT-DEFENSE.md` | C04 Security & Threat Defense | L1 (x-cut) |
| `NEXUS-PERSISTENCE-STATE-DATA-INFRASTRUCTURE.md` | C05 Persistence & State | L2 |
| `NEXUS-EVENT-TRIGGER-SYSTEM.md` | C06 Event & Trigger Substrate | L2 |
| `NEXUS-OBSERVABILITY-AUDIT-TELEMETRY.md` | C07 Observability & Audit | L2 (x-cut) |
| `NEXUS-EXECUTIVE-CONTROL-PLANE.md`, `NEXUS-OBJECTIVE-ENGINE.md`, `NEXUS-DECISION-ENGINE.md`, `NEXUS-PLANNER-ENGINE.md` | C08 Core Cognitive Control | L3 |
| `NEXUS-MEMORY-CONTEXT-INTELLIGENCE.md`, `NEXUS-KNOWLEDGE-INFORMATION-INGESTION.md` | C09 Memory & Knowledge | L3 |
| `NEXUS-ATTENTION-PRIORITY-INTELLIGENCE.md` | C10 Attention & Priority | L3 |
| `WORKFLOW_ORCHESTRATION_ENGINE.md`, `NEXUS-SCHEDULING-RESOURCE-RUNTIME.md` | C11 Workflow & Scheduling | L4 |
| `NEXUS-AGENT-RUNTIME-LIFECYCLE.md`, `NEXUS-MODEL-ROUTER-PROVIDER-ABSTRACTION.md` | C12 Agent & Model Runtime | L4 |
| `NEXUS-TOOL-RUNTIME-CAPABILITY.md`, `NEXUS-API-INTEGRATION-GATEWAY.md` | C13 Tool & Integration Boundary | L5 |
| `NEXUS-COMMUNICATION-INTERACTION-BUS.md` (+ UI/control surface) | C14 Communication & Control Surface | L6 |

Note: the 14 legacy `*-spec.md` files in `Core/` are HISTORICAL/NON-CANONICAL and
MUST NOT be used as implementation sources (see cross-phase audit).

---

## 2. Component Responsibility Contracts

### C01 — Foundation & Config (L0)
- **Owns:** configuration lifecycle, schema validation, version pinning, atomic
  change + rollback, environment binding.
- **Boundary:** depends on nothing (L0). Every other component MAY read config.
- **Forbidden:** owning business logic, authority, or secrets in plaintext.

### C02 — Identity & Trust (L1)
- **Owns:** persistent identity, execution identity, membership, businesses,
  divisions, scopes, trust levels, `secret_ref` resolution coordination.
- **Boundary:** depends on C01, C05.
- **Forbidden:** granting authority (that is C03). Identity ≠ authority ≠ trust.

### C03 — Governance & Policy (L1)
- **Owns:** policy engine, approval engine, permission/constraint evaluation,
  escalation. Produces exactly 5 outcomes: `ALLOW`, `DENY`,
  `REQUIRE_APPROVAL`, `ALLOW_WITH_CONSTRAINTS`, `ESCALATE`.
- **Boundary:** depends on C01, C02, C05, C07.
- **Forbidden:** being bypassed by any model, tool, memory, or event.

### C04 — Security & Threat Defense (L1, cross-cutting)
- **Owns:** threat model, SSRF/injection/replay/DLP controls, sandbox posture,
  egress allow-lists, secret non-exposure enforcement.
- **Boundary:** callable from every layer; depends on C01, C02, C03.
- **Forbidden:** being an authority grant; being optional.

### C05 — Persistence & State (L2)
- **Owns:** durable stores for all authoritative categories (per plan §13),
  transactions, lineage durability, retention tiers.
- **Boundary:** depends on C01. Used by all L2+ components.
- **Forbidden:** making decisions; knowing domain semantics beyond storage.

### C06 — Event & Trigger Substrate (L2)
- **Owns:** event ingestion, append-only persistence, priority queues, worker
  pool, dedup/idempotency, DLQ/quarantine, safe replay, declarative triggers.
- **Boundary:** depends on C01, C05, C07.
- **Forbidden:** treating events as commands; generating external side effects on
  replay; claiming unsupported exactly-once.

### C07 — Observability & Audit (L2, cross-cutting)
- **Owns:** structured logs, metrics, traces, audit, decision traces, correlation
  and causation IDs, health, alerts, incident timeline.
- **Boundary:** callable from every layer; depends on C01, C05.
- **Forbidden:** granting authority; leaking secrets; dropping critical events.

### C08 — Core Cognitive Control (L3)
- **Owns:** executive control, objective lifecycle (+WHY, success criteria,
  scope), decision framing, planning (plan production + validation handoff).
- **Boundary:** depends on C01–C07, C09, C10.
- **Forbidden:** executing actions; owning model/provider selection; owning
  workflow durability.

### C09 — Memory & Knowledge (L3)
- **Owns:** memory admission/retrieval with scope, context assembly, knowledge
  ingestion with provenance, semantic/keyword/graph index.
- **Boundary:** depends on C01, C05, C07.
- **Forbidden:** authorizing actions; auto-trusting unverified content.

### C10 — Attention & Priority (L3)
- **Owns:** priority levels, queues, aggregation, dedup, suppression with
  guardrails, cooldown, budget, quiet-hours, escalation, notification.
- **Boundary:** depends on C01, C05, C06, C07.
- **Forbidden:** granting authority; suppression hiding critical signals.

### C11 — Workflow & Scheduling (L4)
- **Owns:** durable workflow state machine, task lifecycle, scheduling
  (when/where/resource), leases + fencing, heartbeat, recovery, graceful
  shutdown/drain, verification state from evidence.
- **Boundary:** depends on C01–C10, C12, C13.
- **Forbidden:** bypassing governance; nested/multi retry ownership.

### C12 — Agent & Model Runtime (L4)
- **Owns:** agent definitions/runtime lifecycle, temporary-agent spawn controls,
  heartbeat/recovery; Model Router (only owner of model catalog), provider
  selection (health-aware), invocation, failover.
- **Boundary:** depends on C01–C11, C13.
- **Forbidden:** model identity = agent identity; routing = authorization;
  reachable = healthy.

### C13 — Tool & Integration Boundary (L5)
- **Owns:** tool validation, authorization enforcement point, sandbox, execution,
  result validation, credential resolution via `secret_ref`; API gateway
  (external boundary, webhooks, reconciliation).
- **Boundary:** depends on C01–C12.
- **Forbidden:** tool results granting authority; sandbox = live; embedding
  credentials.

### C14 — Communication & Control Surface (L6)
- **Owns:** owner interaction, notifications, control UI surface, business
  switching (never stops other businesses).
- **Boundary:** depends on C01–C13.
- **Forbidden:** being an authority source; bypassing C03.

---

## 3. Layer Authority Direction

```text
L0 Foundation ─▶ L1 Trust/Governance/Security ─▶ L2 Persistence/Event/Obs ─▶
L3 Cognition/Memory/Attention ─▶ L4 Workflow/Agent/Model ─▶
L5 Tool/Integration ─▶ L6 Surface
```

Authority flows downward only. No component may grant authority to a peer or
ancestor. Security (C04) and Observability (C07) are callable across all layers
without inverting dependencies.

---

## 4. Invariant → Component → Test Traceability

| Locked invariant | Primary component(s) | Test layer(s) |
|---|---|---|
| IDENTITY ≠ AUTHORITY ≠ CAPABILITY ≠ PERMISSION ≠ TRUST | C02, C03 | 8, 3, 1 |
| events = facts not commands | C06 | 4, 1 |
| ATTENTION ≠ AUTHORITY | C10 | 3, 4 |
| Objective/WHY ≠ Authorization | C08, C03 | 3, 4 |
| Memory/Knowledge ≠ Authorization | C09, C03 | 3, 8 |
| Tool Result ≠ Authorization | C13, C03 | 8, 11 |
| External Response ≠ Trusted Fact | C13 | 10, 8 |
| Provider Health ≠ Authorization | C12, C03 | 4, 6 |
| UNKNOWN_OUTCOME ≠ FAILURE | C11, C13 | 7, 11 |
| UNKNOWN_OUTCOME ≠ SUCCESS | C11, C13 | 7, 11 |
| Reconciliation idempotent | C11, C13 | 11, 5 |
| Single retry owner | C11, C12, C13 | 7, 4 |
| No nested retry | C11, C12, C13 | 7 |
| cancel ≠ failure | C11 | 6, 7 |
| verification ≠ execution | C11 | 3, 6 |
| outcome ≠ authorization | C08, C03 | 3, 4 |
| one NEXUS → many businesses (`business_id`) | C02, C05, all | 9 |
| WHY propagation preserved | C08, C11 | 12, 5 |
| raw credentials never embedded (`secret_ref`) | C04, C13 | 8 |
| secrets minimally exposed | C04, C07 | 8 |
| sandbox/dry-run ≠ live | C04, C13 | 8, 10 |
| no unsupported exactly-once | C06 | 4, 1 |
| Governance outcomes exactly 5 | C03 | 1, 3 |
| Fail-safe when governance unavailable | C03, C02 | 1, 7 |
| Lease fencing prevents stale writer | C11 | 6, 7 |
| Anti-spawn-storm hard limits | C12 | 6, 9 |

Test layer numbers refer to plan §24 Testing Strategy.

---

## 5. Coverage Statement

- All 21 canonical modules are realized by C01–C14 (no module unassigned).
- All locked invariants (Phase 2, Phase 3, Phase 4, architecture) have a primary
  owning component and ≥1 test layer.
- No component introduces a reverse authority edge.
- No contract or architecture modification is required.
