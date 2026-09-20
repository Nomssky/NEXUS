# NEXUS — IMPLEMENTATION PLAN

**Layer:** Implementation Planning (post-architecture, post-contracts)
**Status:** PLANNING — no code
**Branch:** planning/implementation-blueprint
**Base commit:** `02f07cc6c15386ba98a96a02b63afa5bbfd8fde7` (docs(contracts): audit cross-phase contract consistency)
**Contracts baseline:** Phase 1–4 LOCKED + Cross-Phase Audit verdict PASS_WITH_DEFERRED_ITEMS
**Architecture baseline:** 21 canonical `NEXUS-*` Core modules LOCKED

> This document plans HOW to implement NEXUS. It does NOT implement it.
> It does not modify the locked architecture, contracts, or cross-phase audit.

---

## 0. Reading Order

This planning layer consists of five documents:

| Document | Purpose |
|---|---|
| `IMPLEMENTATION_PLAN.md` | This file — master blueprint (all 38 required topics) |
| `IMPLEMENTATION_COMPONENT_MAP.md` | Architecture module → implementation component → contracts → data → deps → tests → milestone |
| `IMPLEMENTATION_DEPENDENCY_GRAPH.md` | Dependency graph, prohibited directions, cycle risks |
| `IMPLEMENTATION_MILESTONES.md` | Dependency-ordered milestones with acceptance criteria |
| `TECHNOLOGY_DECISIONS.md` | Technology decision records (TDRs) |

---

## 1. Repository-First Findings (§1 of mission)

Inspected before planning; nothing assumed.

| Check | Finding |
|---|---|
| Repository structure | Three content directories: `Core/` (21 canonical modules + audit + 14 legacy specs), `contracts/` (17 files + audit), `Foundation spec/` (4 docs) |
| Current branch | `contracts/cross-phase-audit`; clean working tree |
| Latest commit | `02f07cc` docs(contracts): audit cross-phase contract consistency |
| Canonical architecture docs | 21 canonical modules; `NEXUS-ARCHITECTURE-AUDIT.md` declares "ARCHITECTURE LOCKED AT MODULE LEVEL" |
| Locked contracts | 17 files (Phase 1: 1, Phase 2: 9, Phase 3: 3, Phase 4: 4) |
| Cross-phase audit | Present; verdict PASS_WITH_DEFERRED_ITEMS; 6 corrections applied, 4 deferred |
| Existing source code | **NONE** |
| Package manifests / config | **NONE** (no pyproject/package.json/go.mod/Cargo.toml/etc.) |
| Existing tests | **NONE** |
| Existing infra/CI files | **NONE** (no Dockerfile, IaC, CI workflows) |
| Existing technical choices | **NONE** — no runtime/language/database decision has been made anywhere in the repo |

**Consequence:** This is a greenfield implementation. There are no existing
implementation decisions to preserve or overwrite. All technology choices are
open and are recorded as TDRs in `TECHNOLOGY_DECISIONS.md`, deliberately
minimal where the contracts do not require a specific technology.

The architecture explicitly leaves some choices open, e.g.
`Foundation spec/ARCHITECTURE.md §12`: *"The specific database technology is not
architecturally locked yet."*

---

## 2. Locked Baseline (§2)

Immutable inputs to this plan:

```text
ARCHITECTURE (21 canonical modules, LOCKED)
   ↓
CONTRACTS PHASE 1 (core interface contracts)
   ↓
CONTRACTS PHASE 2 (data & event schemas)
   ↓
CONTRACTS PHASE 3 (runtime & execution)
   ↓
CONTRACTS PHASE 4 (integration, provider, external boundary)
   ↓
CROSS-PHASE CONTRACT AUDIT (02f07cc, PASS_WITH_DEFERRED_ITEMS)
```

Fixed references:

- Audited Phase 4 base: `b1563887f4a8a38be23ef85ee3c4a9fda3c94ab8`
- Cross-phase audit commit: `02f07cc6c15386ba98a96a02b63afa5bbfd8fde7`
- Actual HEAD at planning time: `02f07cc` (matches the audit commit; verified)

This plan treats architecture and contracts as FROZEN. Where a genuine
implementation-blocking contradiction appears, the plan stops and reports it
rather than editing locked docs (see §27 / §31 of mission; none found — see §30).

---

## 3. Planning Principles (§3)

1. **Architecture modules describe responsibilities; implementation components
   are a separate, coarser mapping.** One implementation component may realize
   several architecture modules when the contracts permit, and one large module
   may split into several components when lifecycle/scaling/security demand it.
2. **No implementation component may cross a contract boundary in a way that
   violates the locked architecture or the governance/authority invariants.**
3. **Dependency direction is FOUNDATION → CORE → RUNTIME → EXTERNAL.** No
   reverse edges on authority-bearing concerns.
4. **Contract-first.** Every component is implemented against a locked contract
   file, and every locked invariant maps to a test.
5. **Greenfield, but reversible choices.** Prefer boring, swappable
   technologies behind interfaces the contracts already imply (Provider
   Interface, Persistence, Event Bus, Tool Runtime boundary).
6. **Multilingual reality.** Some canonical modules specify in Indonesian; the
   normative content is language-independent. Implementation must preserve the
   *semantics*, not the wording.

---

## 4. Implementation Layers (§4)

Derived from the repository (not assumed). Layers are logical, not necessarily
deployment packages.

| Layer | Name | Contents | Rationale |
|---|---|---|---|
| **L0** | Foundation / Process / Configuration | config control plane, process bootstrap, schema validation, logging bootstrap | Everything depends on validated config and a running process |
| **L1** | Identity / Security / Governance | Identity & Trust, Governance & Policy, Security & Threat Defense | Authority must exist before any execution; governance is the highest control layer |
| **L2** | Persistence / Event Infrastructure | Persistence & State, Event Trigger System, queue/workers substrate, Observability | Durable truth + event substrate precede all stateful cognition |
| **L3** | Core Intelligence | Objective Engine, Decision Engine, Planner, Executive, Memory, Knowledge Ingestion, Attention | Cognitive control; consumes L1/L2 only |
| **L4** | Execution Runtime | Workflow Orchestration, Scheduling & Resource Runtime, Agent Runtime, Model Router, Tool Runtime | Turns authorized plans into governed execution |
| **L5** | Integration / External Boundaries | API Integration Gateway, provider adapters, connectors, webhooks, polling, streaming, DLP/quarantine | The only sanctioned path to the external world |
| **L6** | Interfaces / Control Surface | Owner control surface (UI/API), Communication Bus as the internal interaction surface | UI is a control/observation surface; runtime is UI-independent |

Mapping to the mission's candidate layers: L0=Layer0, L1=Layer1, L2=Layer2,
L3=Layer3, L4=Layer4, L5=Layer5, L6=Layer6. These names are retained because
the architecture audit's "Recommended Final Architecture" already expresses this
same ordering (`Foundation → Identity/Governance/Security → Objective/Decision/
Executive/Planner → Event/Attention/Communication → Workflow/Scheduling/Agent
Runtime → Model Router/Tool Runtime/API Integration → Memory/Knowledge/
Persistence → Observability → Business/Division agents`). Note the audit lists
Memory/Knowledge/Persistence later than this plan does; this plan places
Persistence in L2 because durable truth is a precondition for Core cognition
(see `IMPLEMENTATION_DEPENDENCY_GRAPH.md §5` for the reconciliation).

---

## 5. Implementation Components (§5)

Twenty-one architecture modules are mapped to **13 implementation components**.
This is a deliberate grouping — the mission's warning against "one package per
module" is honored. Grouping criteria: coupling, dependency direction,
transaction boundaries, runtime lifecycle, security boundary, scaling profile,
persistence requirement, contract boundary.

| # | Implementation Component | Architecture Modules Realized | Layer |
|---|---|---|---|
| C01 | **Foundation & Config** | Configuration Control Plane | L0 |
| C02 | **Identity & Trust** | Identity, Access & Trust System | L1 |
| C03 | **Governance & Policy** | Governance, Policy & Safety Control | L1 |
| C04 | **Security & Threat Defense** | Security & Threat Defense | L1 (cross-cutting) |
| C05 | **Persistence & State** | Persistence, State & Data Infrastructure | L2 |
| C06 | **Event & Trigger Substrate** | Event Trigger System + queue/worker substrate | L2 |
| C07 | **Observability & Audit** | Observability, Audit & Telemetry | L2 (cross-cutting) |
| C08 | **Core Cognitive Control** | Executive + Objective Engine + Decision Engine + Planner | L3 |
| C09 | **Memory & Knowledge** | Memory & Context Intelligence + Knowledge & Information Ingestion | L3 |
| C10 | **Attention & Priority** | Attention & Priority Intelligence | L3 |
| C11 | **Workflow & Scheduling** | Workflow Orchestration Engine + Scheduling & Resource Runtime | L4 |
| C12 | **Agent & Model Runtime** | Agent Runtime & Lifecycle + Model Router & Provider Abstraction | L4 |
| C13 | **Tool & Integration Boundary** | Tool Runtime & Capability + API Integration Gateway | L5 |
| C14 | **Communication & Control Surface** | Communication & Interaction Bus + UI/control surface | L6 |

(14 components; numbering C01–C14.)

Grouping rationale (selected):

- **C08 groups four cognitive modules.** They share one lifecycle (intent →
  objective → decision → plan), one authority posture (none of them execute),
  and tight synchronous contracts. Splitting them early would create chatty
  inter-process boundaries with no isolation benefit. They MAY be split later
  behind the same contracts.
- **C11 groups Workflow + Scheduling.** Workflow owns durable execution;
  Scheduling owns WHEN/WHERE/resource. They share the queue/worker substrate and
  the lease model; combining avoids a distributed-transaction seam over leases.
- **C12 groups Agent Runtime + Model Router.** Agent Runtime owns agent identity
  and lifecycle; Model Router owns provider/model selection and is explicitly
  the *only* owner of the model catalog (Agent Runtime must not duplicate it).
  They are co-located but keep distinct internal interfaces.
- **C13 groups Tool Runtime + API Gateway.** Contracts place Tool Runtime as the
  authorization boundary and the Gateway as the external connectivity boundary;
  they are adjacent in one call chain and share credential-resolution and
  reconciliation concerns.
- **C04, C07 are cross-cutting.** Security and Observability are realized as
  cross-cutting concerns with their own components because they must be callable
  from every layer without inverting dependencies.

Full details in `IMPLEMENTATION_COMPONENT_MAP.md`.

---

## 6. Dependency Graph (§6)

Summary; full graph in `IMPLEMENTATION_DEPENDENCY_GRAPH.md`.

Direction: `L0 → L1 → L2 → L3 → L4 → L5`, with L6 (control surface) observing
all layers but granting no authority.

Key edges (abridged):

```text
C01 Foundation&Config
  └─> (everything reads validated config)

C02 Identity ─┐
C03 Governance┼─> C08 Core (authority/scope resolution)
C04 Security ─┘

C05 Persistence ─┐
C06 Event        ┼─> C08, C09, C10, C11, C12, C13
C07 Observability┘   (cross-cutting sink for telemetry)

C08 Core ─> C11 Workflow ─> C12 Agent/Model ─> C13 Tool/Integration
C09 Memory/Knowledge <-> C08, C12 (context assembly / evidence)
C10 Attention <-> C06, C08 (signals / review routing)
```

**Prohibited directions** (must never exist as edges):

- Model → Authority (a model output may never authorize)
- Tool result → Governance (a tool result may never mutate policy)
- Attention → Authorization (attention may never authorize)
- Objective/WHY → Security bypass
- External system → internal authority
- Memory/Knowledge → Authorization
- Observability → Authority
- Event → Command / Authority
- Provider health → Authorization
- Resource availability → Permission

The graph is **acyclic** at the component level. Where the architecture requires
bidirectional *data* flow (e.g. Memory ↔ Core, Attention ↔ Core), the edges are
directional by concern: Core issues queries/commands; Memory/Attention return
data/signals, never authority. See `IMPLEMENTATION_DEPENDENCY_GRAPH.md §4`.

---

## 7. Bootstrap Order (§7)

Derived from the contract dependency direction, not copied from the mission's
candidate list. Minimum order in which NEXUS can be built so each step compiles
against already-present contracts:

```text
 1. C01 Foundation & Config            (process, config schema, validation)
 2. C02 Identity & Trust (primitives)  (identity/scope/credential model)
 3. C04 Security (primitives)          (secret store interface, sandbox hooks)
 4. C03 Governance & Policy (engine)   (policy eval, 5 canonical outcomes)
 5. C05 Persistence & State            (durable store + recovery)
 6. C07 Observability & Audit          (correlation ids, audit sink)
 7. C06 Event & Trigger Substrate      (bus, queue, workers, dedup, DLQ)
 8. C08 Core — Objective               (objective truth)
 9. C08 Core — Decision                (structured decisions)
10. C08 Core — Planner                 (plans from authorized decisions)
11. C08 Core — Executive               (coordination over 8–10)
12. C11 Workflow Orchestration         (durable plan execution)
13. C11 Scheduling & Resource Runtime  (queue/resource/lease)
14. C12 Agent Runtime                  (governed agent lifecycle)
15. C12 Model Router                   (needs-based routing, providers)
16. C13 Tool Runtime                   (authorized capability execution)
17. C13 API Integration Gateway        (external connectivity boundary)
18. C09 Memory & Knowledge             (context + evidence; admitted durably)
19. C10 Attention                      (signal prioritization / review routing)
20. C14 Communication & Control Surface(owner control; UI-independent runtime)
21. Controlled autonomous operation    (first safe autonomous agent → 24/7)
```

Notes on deviations from the mission's candidate order:

- **Security primitives (C04) precede Governance engine (C03)** because the
  Governance contracts require credential isolation and secret-store interfaces
  to exist; the policy engine consumes those primitives, not vice versa.
- **Observability (C07) precedes the Event substrate (C06)** because durable
  audit and correlation IDs are needed by the event substrate from its first
  run; Observability's sink is a low-level dependency.
- **Memory & Knowledge (C09) come after execution runtime (C11–C13)** in
  *build* order because durable memory admission depends on the event substrate
  and persistence, and because the first bootable runtime can operate with
  working/objective memory only. This does NOT contradict the dependency graph:
  C09 is a dependency of *full* Core cognition and of agents; it is not required
  for the first controlled task.
- **Attention (C10) comes late** because it prioritizes signals from systems
  that must already emit them; it is required for *autonomous* operation but not
  for the first controlled request.

---

## 8. Minimum Viable Runtime (§8)

The smallest implementation that safely executes the canonical chain:

```text
OWNER REQUEST
 → EXECUTIVE
 → OBJECTIVE
 → DECISION
 → PLANNER
 → WORKFLOW
 → TASK
 → AGENT
 → MODEL
 → TOOL
 → VERIFICATION
 → OUTCOME
```

...while preserving all of the following (each is a hard requirement drawn from
locked contracts, and each maps to a test):

| Requirement | Realized by | Contract |
|---|---|---|
| Identity | C02 | Phase 1 §4, Phase 2 Identity schema |
| Authorization | C03 + C02 | Phase 2 Governance schema |
| Governance (5 outcomes, enforced before execution) | C03 | Phase 2 governance outcomes |
| Persistence | C05 | Phase 2 common envelope, Phase 3 state |
| Observability (correlation/causation) | C07 | Phase 2 observability schema |
| Objective / WHY propagation | C08 | Phase 2 work/objectives |
| Multi-business scope (`business_id`) | all | Phase 2 invariants |
| Error handling (14 categories) | all | Phase 1 error envelope |
| Unknown-outcome semantics | C11/C12/C13 | Phase 3 recovery, Phase 4 reconciliation |

**Out of scope for MVR** (deliberately): Attention (C10), Knowledge ingestion
beyond raw retrieval (C09 partial), 24/7 hardening, multi-business parallelism
beyond isolation enforcement, full provider failover matrix. MVR must still
*enforce* isolation and unknown-outcome semantics even if it does not exercise
them heavily.

MVR corresponds to Milestone M5 in `IMPLEMENTATION_MILESTONES.md`.

---

## 9. First Bootable NEXUS

**Milestone M5 — FIRST BOOTABLE NEXUS.**

| # | Capability | Success condition |
|---|---|---|
| 1 | Start | Process initiates from a single command; refuses to start on invalid config |
| 2 | Load configuration | Config Control Plane loads + schema-validates + version-pins config (C01) |
| 3 | Establish identity | Owner + system identities exist; scopes resolve (C02) |
| 4 | Establish persistence | Durable store connected; a record survives restart (C05) |
| 5 | Establish governance | Policy engine answers with one of the 5 canonical outcomes (C03) |
| 6 | Establish observability | Correlation ID minted/propagated; audit record written (C07) |
| 7 | Accept controlled owner request | Executive classifies owner intent (C08) |
| 8 | Create an objective | Objective persisted with WHY + success criteria + business scope (C08) |
| 9 | Create a workflow | Planner produces validated plan; Workflow accepts it (C08+C11) |
| 10 | Execute a controlled task | Scheduler leases the task to an agent (C11) |
| 11 | Invoke an agent | Agent runtime provisions, runs bounded task, heartbeats (C12) |
| 12 | Route to a model | Model Router selects provider/model by need; invocation recorded (C12) |
| 13 | Perform a safe tool operation | Tool Runtime validates -> authorizes -> executes a READ-ONLY tool (C13) |
| 14 | Verify | Task verification state set from evidence, not assumption (C11) |
| 15 | Record outcome | Outcome persisted with evidence + WHY lineage intact (C08) |
| 16 | Shut down safely | stop new -> drain -> checkpoint -> release -> persist (C11) |
| 17 | Recover state | Restart reconstructs objective/task/unknown-outcome state (C05+C11) |

**Governing invariant:** none of the above may bypass C02/C03/C04. If governance
is unavailable during boot, high-risk actions are DENIED (fail-safe).

---

## 10. First Safe Autonomous Agent

**Milestone M7 — FIRST SAFE AUTONOMOUS AGENT.** The agent MUST NOT receive
unrestricted authority.

| Attribute | Requirement |
|---|---|
| Identity | distinct `agent_id`; execution identity != persistent identity; survives restart |
| Capabilities | explicitly declared capability set |
| Permissions | `permission ⊆ authority ⊆ parent authority`; capability != permission |
| Governance constraints | highest control layer; agent cannot modify policy/audit/approval |
| Tool access | only via Tool Runtime; no direct external access |
| Model routing | via Model Router only; model identity != agent identity |
| Memory scope | scoped retrieval; no "everything NEXUS knows" default; observation != permanent memory |
| Business scope | explicit `business_id`; no cross-business leak |
| Division scope | explicit `division_id` where applicable |
| Resource limits | CPU/RAM/GPU + token + task + cost budgets |
| Timeout | per-task; timeout -> UNKNOWN, not failure |
| Retry | single retry owner; bounded; UNKNOWN -> reconcile, not retry |
| Cancellation | `cancel != failure`; safe-boundary preemption |
| Observability | full correlation chain agent -> model/tool -> outcome |
| Audit | immutable, attributable records for every side effect |
| Recovery | Heartbeat -> Suspected -> Unresponsive -> Recovery; lease fencing; zombie stop |

**Spawn safety:** agent request -> Agent Runtime (control plane) provisions,
bounded by anti-swarm controls (max children/depth/descendants, spawn rate
limit, budgets, TTL, duplicate-role detection, recursive-spawn detection,
objective-relevance check). Agents never spawn processes directly.

**Terminate safety:** `TERMINATING -> TERMINATED`; graceful shutdown; audit
retained; lineage preserved.

---

## 11. Agent / Model / Provider Model

Three identities remain distinct:

| Concept | Identity | Owns | Never owns |
|---|---|---|---|
| Agent | `agent_id` (+`runtime_id`) | task-local decisions, lifecycle | model/provider selection, authority |
| Model | `model_id` (+`model_version`) | inference capability | agent identity, authority, execution |
| Provider | `provider_id` | endpoint, health, cost | model identity, agent identity, authority |

```text
Agent
 -> Model Router   (capability match, privacy, cost, latency, budget, governance)
   -> Provider Selection (health-aware; routing != authorization)
     -> Model Invocation (invoke, timeout, retry-ownership, token accounting)
       -> Response Validation (untrusted until validated; inference != knowledge)
```

Internal interfaces (adapters live behind them): Provider Interface; Model
Interface; Model Router (needs-based, local-first); Capability Registry; Health
Registry (reachable != healthy); Credential Resolution (`secret_ref` only);
Invocation Interface (`invocation_id`, `why`, `correlation_id`, `business_id`,
tokens/cost, status in pending/streaming/completed/failed/timeout/cancelled).
Failover: primary timeout = `UNKNOWN_OUTCOME`, reconcile before failover for
side-effecting ops.

---

## 12. Tool Execution Model

```text
Agent -> Tool Runtime (authorization boundary) -> Tool -> API Gateway (external
boundary) -> External System (untrusted)
```

| Responsibility | Lives in |
|---|---|
| Validation | Tool Runtime |
| Authorization | Tool Runtime + Governance (whether/where/when/constraints) |
| Governance enforcement | Governance; enforced outside the model |
| Credentials | runtime-only resolution via `secret_ref` |
| Sandbox | Tool Runtime / Security |
| Execution | Tool Runtime -> API Gateway -> external |
| Result validation | Tool Runtime (external results untrusted) |
| Audit | Observability |
| Observability | Observability (correlation chain) |
| Reconciliation | Tool Runtime + API Gateway (unknown outcome) |

Hard rules: agent never bypasses Tool Runtime; tool results never grant
authority, never modify governance/permissions, never auto-become trusted
memory; `claim verified != claim attempted != claim succeeded`; sandbox/dry-run
!= live.

---

## 13. Persistence Strategy

The architecture leaves the database technology open (`ARCHITECTURE.md §12`).
This plan defines categories, requirements, and decision criteria — not vendors.

| Category | Source of truth | Consistency | Transaction | Index/search | Retention |
|---|---|---|---|---|---|
| Configuration | Config Control Plane | strong | atomic + rollback | version lookup | long (versioned) |
| Identity/membership/credentials | Identity | strong | yes | by scope/id | long |
| Businesses/divisions | Identity | strong | yes | by scope | long |
| Objectives | Objective Engine | strong | yes (lineage) | by lineage/scope/status | long (archive) |
| Workflows/plans/missions | Planner + Workflow | strong | yes (state machine) | by ids | long |
| Tasks | Workflow | strong | yes (lease/state) | by status/deadline | medium-long |
| Agents (defs/runtime) | Agent Runtime | strong (defs)/eventual (runtime) | yes | by capability/scope | long |
| Executions | Tool/Model | strong | yes | by correlation | medium-long |
| Events | Event substrate | append-only | yes (dedup) | by type/time/correlation | policy |
| Memory | Memory | strong admission; eventual index | yes | semantic+metadata+keyword | policy |
| Knowledge | Knowledge Ingestion | strong provenance | yes | semantic+graph | policy |
| Attention | Attention | strong queue state | yes | by priority/scope | medium |
| Approvals | Governance | strong | yes | by decision | long |
| Governance decisions | Governance | strong | yes | by policy_version | long |
| Artifacts | producer | strong | no | by ref | policy |
| Outcomes | Objective/Workflow | strong | yes | by objective | long |
| Reconciliation | Tool/API | strong | yes | by outcome_id | long |
| Audit | Observability | append-only, tamper-resistant | no (immutable) | by correlation | long |
| Observability | Observability | eventual | no | time-series | tiered |

Decision criteria (see `TECHNOLOGY_DECISIONS.md`): (1) durable relational store
for authoritative entity/config/governance state; (2) append-only log/event
store for events + audit; (3) search/vector index for retrieval, rebuildable
from source of truth; (4) time-series store for metrics. One engine MAY serve
several categories if it meets the strongest requirement.

---

## 14. Event Infrastructure

- **Ingestion:** all events via Event substrate (C06); immutable facts, never commands.
- **Persistence:** append-only, durable where required; event history not owned by Attention alone.
- **Queues:** priority queues with critical lane; per-scope fairness.
- **Workers:** bounded worker pool.
- **Delivery:** preserve `at_most_once`, `at_least_once`, `effectively_once` (requires idempotency key). No unsupported exactly-once claims.
- **Dedup/idempotency:** by identity + root cause + business + window; `at_least_once` consumers handle duplicates.
- **Ordering:** per-key where required; no global ordering assumption.
- **Retry:** single owner per operation; bounded; retryable-classified only.
- **DLQ/quarantine:** invalid sig/schema, repeated failure, poisoned event, reconciliation failure -> quarantine with evidence + provenance.
- **Replay:** safe; never auto-generates external side effects.
- **Triggers:** event/schedule/condition/state/sequence/webhook/threshold/change-detection, declarative.

---

## 15. Runtime Worker Model

Recommended: **process-based workers** (TDR-003), not in-process threads, for
isolation and restart. Primitives: worker, scheduler, executor, lease (fencing),
heartbeat (liveness != progress), recovery, graceful shutdown, drain, resource
allocation. Lease prevents duplicate active workers; fencing rejects stale
writers.

---

## 16. Model Provider Strategy

Local-first (Ollama, Hugging Face/local) as default where practical; remote
(OpenRouter, custom) behind the same Provider Interface; no hard-coded provider;
adapters swappable behind the boundary. Deliverables in M6.

---

## 17. Temporary Agents

Fields: parent, spawner (control plane), reason, objective, WHY, business,
division, scope, TTL, max tasks, resource/capability/permission limits,
lifecycle, expiration, cleanup, artifact transfer, audit. Permission `⊆` parent
authority. **Anti-spawn-storm:** max children/depth/descendants, spawn rate
limit, budgets, TTL, duplicate-role detection, recursive-spawn detection,
objective-relevance check — hard safety limits.

---

## 18. Multi-Business Runtime

One NEXUS -> many businesses -> many divisions -> many agents -> parallel
execution. UI switch never stops other businesses. Isolation carried via
`business_id` (+`division_id`) at database, event, workflow, agent, memory,
knowledge, tool, model, provider, credential, configuration, observability,
attention layers. Cross-business requires explicit Governance policy + audit.

---

## 19. 24/7 Autonomous Operation

Required components realized by C06+C11+C05+C07. Failure playbook:

| Failure | Required behavior |
|---|---|
| Process crash | restart; reconstruct; reconcile in-flight |
| Machine crash | restart elsewhere; lease fencing; recover |
| Provider disappears | health->unavailable; route/failover; UNKNOWN->reconcile |
| Network disappears | outbound -> UNKNOWN/timeout; reconcile; no blind retry |
| Database unavailable | fail-safe high-risk; buffer telemetry; degrade safely |
| Worker dies | heartbeat miss -> unresponsive -> zombie stop -> reassign |
| Task becomes unknown | mark UNKNOWN; reconcile before retry |
| External API timeout | mark UNKNOWN; prevent unsafe duplicate; reconcile |
| Queue grows uncontrollably | backpressure, queue limits, storm protection, circuit breaker |

---

## 20. Attention Implementation

Levels `IGNORE BACKGROUND NORMAL IMPORTANT HIGH CRITICAL EMERGENCY`. Supports
queue/aggregation/dedup/suppression/cooldown/budget/quiet-hours/escalation(1-5)/
notification. **ATTENTION != AUTHORITY:** outcomes are recommendations/handoffs,
not grants. Suppression never hides severity increases, scope changes, new
evidence, policy violations, critical security signals, or direct owner
messages.

---

## 21. Governance Implementation

Policy Engine, Approval Engine, permission evaluation, constraint evaluation,
escalation. Outcomes exactly: `ALLOW DENY REQUIRE_APPROVAL ALLOW_WITH_CONSTRAINTS
ESCALATE`. Precedence `SYSTEM SAFETY > GLOBAL > BUSINESS > DIVISION > AGENT >
WORKFLOW > TASK`; more-restrictive wins; unresolved conflict -> `DENY+ESCALATE`.
Enforced **before execution** and re-validated at the execution boundary (queued
actions re-validate current policy/authority/approval/risk/budget). Enforcement
lives outside the model. No self-approval. Fail-safe when governance unavailable.

---

## 22. Security Implementation

| Domain | Plan |
|---|---|
| Secrets/credentials/tokens | secret store; `secret_ref` only; never in prompt/memory/logs/artifacts/model context |
| Identity/authorization | C02+C03; identity != authority; trust != bypass |
| Network | outbound allow-list; agents cannot select arbitrary destinations |
| Sandbox | untrusted content sandboxed; sandbox != live |
| Filesystem | scoped access; no cross-business paths |
| Browser | SSRF protection, URL validation, cookie isolation, content-as-data |
| API/webhook | signature verification, replay protection, timestamp validation |
| Model input/output | untrusted; prompt injection treated as data; outputs = inference |
| Tool output | untrusted until validated; no auto-trust |
| External content | data, never instruction; authority = none |
| Prompt injection | guardrails outside model reasoning |
| SSRF | protection for all outbound requests |
| DLP | egress controls; sensitive payloads excluded from logs |
| Replay | protection for all state-changing ops |
| Cross-business isolation | enforced at every layer |
| Audit | immutable security events |

Cross-cutting component C04 owns the threat model; enforcement points sit in
C03/C13.

---

## 23. Observability Implementation

Structured logging, metrics, traces, audit events, decision traces, correlation
IDs, causation IDs, lifecycle tracking, health, alerts, incident timeline.
Correlation chain: `Event -> Workflow -> Task -> Agent -> Model -> Tool ->
Result -> Verification -> State Change`. Must reconstruct `REQUEST -> DECISION ->
WORKFLOW -> TASK -> AGENT -> MODEL -> TOOL -> EXTERNAL OPERATION ->
VERIFICATION -> OUTCOME`. Audit is append-only/tamper-resistant; secrets
redacted before storage; critical events never dropped by sampling; replay never
causes side effects; **OBSERVABILITY CANNOT GRANT AUTHORITY**.

---

## 24. Testing Strategy

| # | Layer | Focus |
|---|---|---|
| 1 | Contract tests | every Phase 1-4 contract clause |
| 2 | Schema validation tests | Phase 2 schemas + envelopes |
| 3 | Unit tests | component internals |
| 4 | Integration tests | component boundaries |
| 5 | Persistence tests | durability, restart, lineage |
| 6 | Runtime lifecycle tests | state machines (task/agent/workflow/provider) |
| 7 | Failure/recovery tests | crash, lease, heartbeat, zombie |
| 8 | Security tests | secrets, injection, SSRF, replay, cross-business |
| 9 | Multi-business isolation tests | no leakage across every layer |
| 10 | External boundary tests | gateway, webhooks, providers |
| 11 | Reconciliation tests | unknown-outcome, idempotency |
| 12 | End-to-end tests | full request->outcome chain |
| 13 | Chaos/failure tests | process/machine/provider/network/db failure |

**Every locked invariant maps to >=1 test** (traceability matrix in
`IMPLEMENTATION_COMPONENT_MAP.md`, column "Tests").

---

## 25. Development Milestones

Full detail in `IMPLEMENTATION_MILESTONES.md`. Summary (dependency-ordered):

| ID | Milestone | Components |
|---|---|---|
| M0 | Repository Foundation | none (scaffold, CI, config schema) |
| M1 | Identity + Security primitives | C01, C02, C04 |
| M2 | Governance engine | C03 |
| M3 | Persistence + Event substrate + Observability | C05, C06, C07 |
| M4 | Core cognition | C08 |
| M5 | **FIRST BOOTABLE NEXUS** | C11 (workflow) + minimal C12 + minimal C13 |
| M6 | Model Router + Providers | C12 |
| M7 | **FIRST SAFE AUTONOMOUS AGENT** | C12 + C10 (basic) |
| M8 | Memory + Knowledge | C09 |
| M9 | Attention + controlled autonomous workflow | C10 |
| M10 | Multi-business parallel operation | all (isolation hardened) |
| M11 | 24/7 hardening + recovery + chaos validation | all |

---

## 26. Recommended Implementation Stages

| Stage | Scope | Exit criterion |
|---|---|---|
| S0 | Repository + tooling scaffold | builds, CI green, config schema validated |
| S1 | Identity, Trust, Security primitives | identity != authority enforced by tests |
| S2 | Governance engine | exactly 5 outcomes; fail-safe; precedence |
| S3 | Persistence + Event substrate + Observability | record survives restart; correlation chain complete |
| S4 | Core cognition (Executive/Objective/Decision/Planner) | objective->plan->workflow testable |
| S5 | Workflow/Runtime/Scheduler | task lifecycle + lease + recovery |
| S6 | Model Router + Provider adapters | local-first invocation + failover/reconcile |
| S7 | Tool Runtime + API Gateway | READ tool end-to-end with validation+audit |
| S8 | Memory + Knowledge | scoped retrieval + provenance |
| S9 | Attention + autonomy controls | priority/escalation; anti-swarm limits |
| S10 | Multi-business + 24/7 hardening | isolation + chaos suite green |

---

## 27. Definition of Done (per component)

A component is DONE only when ALL hold:

1. Implements exactly its locked architecture responsibilities (no scope creep).
2. Implements its Phase 1-4 contract clauses; contract tests pass.
3. Emits/consumes only schema-valid messages (Phase 2).
4. Invariant tests pass (its slice of Phase 2/3/4 + architecture invariants).
5. Failure/recovery behavior implemented (Phase 3/4).
6. Observability: correlation + audit emitted; no secret leakage.
7. No reverse authority edge introduced.
8. Multi-business isolation preserved (`business_id`/`division_id`).
9. Documented interface boundary matching the cross-phase contracts.
10. Integrated into the dependency-ordered milestone that owns it.

---

## 28. Technology Decision Records (TDRs)

Detailed in `TECHNOLOGY_DECISIONS.md`. Summary:

| TDR | Decision | Status |
|---|---|---|
| TDR-001 | Primary implementation language (single language for core) | OPEN — recommend, not lock |
| TDR-002 | Durable relational store for authoritative state | OPEN — criteria defined |
| TDR-003 | Process-based (not thread-based) worker isolation | RECOMMENDED |
| TDR-004 | Append-only event log + immutable audit store | RECOMMENDED |
| TDR-005 | Vector/search index rebuildable from source of truth | RECOMMENDED |
| TDR-006 | Time-series store for metrics | RECOMMENDED |
| TDR-007 | Secret store abstraction (`secret_ref` only) | RECOMMENDED |
| TDR-008 | Local-first model providers behind Provider Interface | RECOMMENDED |
| TDR-009 | Message delivery semantics per channel (no fake exactly-once) | RECOMMENDED |
| TDR-010 | Scheduling/lease store (fencing tokens) | RECOMMENDED |

TDRs are PLANNING artifacts: they state decisions + rationale + reversibility.
They do not modify architecture or contracts.

---

## 29. Risk Register

| ID | Risk | Impact | Mitigation |
|---|---|---|---|
| R-01 | Authority inversion (impl grants what architecture forbids) | Critical | invariant tests + review gate per component |
| R-02 | Duplicate side effects on UNKNOWN_OUTCOME | High | reconciliation-before-retry; idempotency keys |
| R-03 | Cross-business data leak | Critical | isolation tests at every layer |
| R-04 | Secret leakage via logs/prompts/memory | Critical | `secret_ref` only + redaction tests |
| R-05 | Nested/multi retry owners | High | single-owner retry tests |
| R-06 | Fake exactly-once guarantee | High | delivery-semantics TDR + tests |
| R-07 | Governance bypass by model/tool output | Critical | enforcement outside model + tests |
| R-08 | Spawn storm (runaway agents) | High | anti-swarm hard limits + tests |
| R-09 | Observability accidentally used as authority | High | invariant test: obs cannot grant |
| R-10 | Stale writer after failover | High | lease fencing tokens + tests |
| R-11 | Contract drift during implementation | Medium | contract tests as gate |
| R-12 | Ambiguous DB/vendor choice stalls build | Medium | TDR-002 criteria + reversible adapter |

No risk requires modifying contracts; all are handled within planning.

---

## 30. Invariant Compliance Check

Every locked invariant is preserved by this plan (each maps to a component +
test in `IMPLEMENTATION_COMPONENT_MAP.md`):

- IDENTITY != AUTHORITY != CAPABILITY != PERMISSION != TRUST
- events = facts not commands
- ATTENTION != AUTHORITY
- Objective/WHY != Authorization
- Memory/Knowledge != Authorization
- Tool Result != Authorization
- External Response != Trusted Fact
- Provider Health != Authorization
- UNKNOWN_OUTCOME != FAILURE; UNKNOWN_OUTCOME != SUCCESS (reconcile first)
- Reconciliation is idempotent; single retry owner; no nested retry
- cancel != failure; verification != execution; outcome != authorization
- one NEXUS -> many businesses via `business_id` isolation
- WHY propagation preserved end-to-end
- raw credentials never embedded (`secret_ref` only)
- sandbox/dry-run != live
- no unsupported exactly-once guarantee

**Validation result:** all invariants are honored; no contract or architecture
modification required; no implementation-blocking contradiction found.

---

## 31. Validation Checklist (self-check before commit)

- [x] Derived from LOCKED architecture (21 canonical modules) only
- [x] Derived from LOCKED Phase 1-4 contracts + cross-phase audit only
- [x] No architecture or contract modified
- [x] No source code, runtime code, or Phase 6 produced
- [x] Planning-only artifacts in `planning/` layer
- [x] Dependency direction FOUNDATION -> CORE -> RUNTIME -> EXTERNAL honored
- [x] No reverse authority edges
- [x] All invariants preserved (§30)
- [x] 21 modules -> 14 components mapping explicit
- [x] Milestones dependency-ordered; first bootable + first safe agent defined
- [x] TDRs captured without locking unneeded vendor choices
- [x] Risk register complete; no contract change required

**END OF IMPLEMENTATION PLAN**
