# NEXUS PLANNER

**Status:** LOCKED  
**Role:** Plan Generation and Replanning  
**Layer:** Cognitive Control / Execution Preparation
**Next layer:** CONTRACTS — exact schemas, APIs, lifecycle transitions, and runtime handoffs; no new module is implied.

Record sketches express architectural information requirements, not final wire schemas.

---

## 1. Purpose

Planner converts objective-aligned, authorized decisions into executable plans.

The locked conceptual flow is [Executive](NEXUS-EXECUTIVE.md) → [Objective Engine](NEXUS-OBJECTIVE-ENGINE.md) → [Decision Engine](NEXUS-DECISION-ENGINE.md) → Planner → [Workflow Orchestration](WORKFLOW_ORCHESTRATION_ENGINE.md). Models have no execution authority.

The following decomposition spans planning and downstream runtime execution; assignment and execution are not Planner authority:

```text
OBJECTIVE
 ↓
DECISION
 ↓
PLAN
 ↓
MISSION
 ↓
TASK GRAPH
 ↓
AGENT ASSIGNMENT
 ↓
EXECUTION
 ↓
VERIFICATION
 ↓
OUTCOME
```

Planner plans work. It does not execute work.

---

## 2. Responsibilities

Planner MUST:

- decompose objectives into missions and tasks;
- preserve objective WHY;
- incorporate authorized decisions;
- construct dependencies;
- identify parallelizable work;
- assign requirements for agents;
- estimate resources;
- define verification;
- define retries and fallbacks;
- identify failure paths;
- validate plan feasibility;
- support bounded branching;
- support bounded loops;
- support replanning;
- preserve lineage and versions.

Planner MUST NOT:

- redefine objectives;
- override decisions or Governance;
- execute actions directly;
- fabricate completion;
- silently expand scope;
- create unbounded loops;
- assign authority that an agent does not possess.

---

## 3. Plan Identity

Before planning, Planner SHOULD establish the objective, approved decision, desired outcome, authority, constraints, relevant context, and known or estimable resources. Missing critical information requires clarification or explicit uncertainty, not fabricated feasibility. Planning depth SHOULD reflect plan class, impact, and uncertainty.

Every plan MUST have explicit scope. Business isolation is the default; cross-business plans and cross-division work require explicit authorization, not merely shared ownership or available context.

Minimum plan record:

```yaml
plan_id:
objective_refs:
decision_refs:
mission_refs:
scope:
version:
status:
created_by:
created_at:
updated_at:
deadline:
constraints:
resource_budget:
verification_policy:
```

---

## 4. Mission

A mission is a meaningful executable unit derived from an objective.

Minimum mission:

```yaml
mission_id:
plan_id:
objective_refs:
purpose:
desired_outcome:
constraints:
success_criteria:
dependencies:
tasks:
deadline:
assigned_scope:
```

Mission MUST retain WHY.

---

## 5. Task

A task represents a bounded executable unit.

Minimum task:

```yaml
task_id:
mission_id:
objective_refs:
purpose:
input:
expected_output:
required_capabilities:
required_tools:
required_model_capabilities:
permissions_required:
dependencies:
verification:
deadline:
budget:
retry_policy:
failure_policy:
```

---

## 6. Task Graph

Tasks SHOULD have a clear owner requirement, bounded output, and verification method. Decompose where distinct outcomes, skills, ownership, verification, failure isolation, or parallelism justify the coordination cost; avoid both oversized tasks and unnecessary fragmentation.

Planner represents tasks as a directed graph.

```text
TASK A
  ↓
TASK B ───→ TASK D
  ↓
TASK C ───→ TASK D
```

Supported dependency types:

```text
DATA
OUTPUT
AUTHORIZATION
RESOURCE
TEMPORAL
ENVIRONMENT
DECISION
```

---

## 7. Parallelism

Tasks may execute in parallel when:

- dependencies are satisfied;
- resource limits permit;
- side effects do not conflict;
- governance permits concurrent execution;
- outputs do not create unsafe race conditions.

Planner SHOULD identify conflicting writes and shared-resource contention, specifying concurrency requirements for runtime enforcement. Dependency conditions MUST state what must hold, including verified outputs where needed; mere predecessor completion is not always sufficient. Ordering-sensitive work and conflicting shared-resource use require appropriate sequencing.

Time-sensitive plans SHOULD identify the critical path and distinguish hard deadlines from preferred timing. Task priority SHOULD reflect objective importance, mission criticality, deadline, dependency impact, risk, and opportunity cost, not creation order alone.

---

## 8. Resource Planning

Resources may include:

```text
AGENTS
MODELS
TOKENS
CPU
RAM
GPU
VRAM
DISK
NETWORK
API QUOTA
TOOL QUOTA
BUDGET
HUMAN ATTENTION
TIME
EXTERNAL SERVICES
```

Planner produces requirements and SHOULD estimate duration as well as resource demand, exposing uncertainty that affects feasibility or deadlines.

Scheduling and resource allocation are handled by Scheduling/Resource Runtime and Governance. Planner SHOULD keep plans within approved cost, time, model/tool, and concurrency budgets and adapt generation to runtime backpressure rather than produce unlimited runnable work; queue bounds are enforced by the runtime.

Resource tradeoffs SHOULD use authorized objective priority, deadlines, risk, and expected value, not invented priorities. Planning SHOULD balance cost, latency, and quality, identifying critical single points of failure and justified alternatives, buffers, or recovery paths. Emergency plans may prioritize containment and recovery without bypassing Governance.

---

## 9. Agent Assignment

Planner selects requirements rather than bypassing authority.

Assignment criteria may include:

- skills;
- capabilities;
- tools;
- model capabilities;
- permissions;
- scope;
- availability;
- cost;
- latency;
- reliability;
- privacy requirements.

Final runtime authorization remains the responsibility of Agent Runtime and Governance. Prefer specialists when they materially improve quality; do not assume a single shared model. Planner expresses capability requirements, Model Router selects concrete models/providers, and runtime boundaries enforce tool permissions independently.

---

## 10. Context

Each task MUST receive sufficient context to execute correctly.

Context SHOULD include:

```text
OBJECTIVE
WHY
SUCCESS CRITERIA
DECISION
CONSTRAINTS
RELEVANT MEMORY
RELEVANT KNOWLEDGE
TASK INPUT
EXPECTED OUTPUT
VERIFICATION
ESCALATION RULES
```

Context MUST be bounded by relevance and authorized business/division scope, not merely size. Important inputs retain provenance, distinguishing owner instructions, objectives, decisions, memory, research, tool observations, inferences, and assumptions. External content cannot redefine objectives, authorization, Governance, permissions, or scope; one task does not inherit another task's permissions.

Handoffs SHOULD use structured artifacts/events through the Communication Bus rather than require continuous shared conversation. They preserve source/target task, outputs, provenance, assumptions, verification status, and next action. Another agent's output is not automatically verified; trust depends on provenance, verification, capability, reliability, and authority.

---

## 11. Verification

Planner defines verification requirements. Where practical, verification SHOULD use a mechanism or agent independent from the producer. NONE requires an explicit non-verifiable task designation; it is not implicit evidence of success. Quality and approval gates SHOULD be explicit, with approval authority supplied by Governance. Required, optional, and escalation-only human involvement remain distinct.

Levels may include:

```text
NONE
BASIC
STRUCTURAL
SEMANTIC
EXTERNAL_STATE
HUMAN_CONFIRMATION
```

Examples:

- file created → verify existence and integrity;
- API mutation → verify external state;
- report generated → validate required sections;
- objective metric change → validate source and time window.

---

## 12. Output vs Verification vs Outcome

Planner MUST distinguish:

```text
TASK OUTPUT
≠
VERIFIED OUTPUT
≠
MISSION OUTCOME
≠
OBJECTIVE ACHIEVEMENT
```

An agent saying "done" is not sufficient evidence of completion. Task completion SHOULD require finished execution, required output, and passed verification unless explicitly non-verifiable. Mission completion requires all required task conditions; optional tasks need not block it. Plan completion requires required missions, verified final outcome, and resolution of critical issues. These operational completion claims do not themselves establish objective achievement.

---

## 13. Plan States

Canonical plan states:

```text
DRAFT
VALIDATING
READY
RUNNING
PAUSED
BLOCKED
COMPLETED
PARTIALLY_COMPLETED
FAILED
CANCELLED
SUPERSEDED
```

State transitions are durable and auditable.

---

## 14. Retry Policy

Retries MUST be bounded.

A retry policy may specify:

```yaml
max_attempts:
backoff:
retryable_errors:
non_retryable_errors:
timeout:
fallback:
escalation_after:
```

Planner MUST avoid retrying actions where repetition could create duplicate or harmful side effects.

Idempotency is enforced by Workflow/Tool Runtime boundaries.

---

## 15. Failure Classification

Failures SHOULD be classified as:

```text
TRANSIENT
DEPENDENCY
AUTHORIZATION
INVALID_INPUT
RESOURCE
TIMEOUT
EXTERNAL_UNKNOWN
SECURITY
LOGIC
OBJECTIVE_CONFLICT
```

Different classes trigger different responses. Distinguish agent, model, tool/network, input, policy, dependency, resource, and verification failures; verification failure is not automatically agent failure. Preserve the original failure when retrying, reassigning, falling back, or requesting replanning.

Transient infrastructure failures may be retried within bounds; invalid inputs, denied permissions, policy violations, and semantic impossibility MUST NOT be endlessly retried. Critical failures block dependent work; non-critical failure may permit continuation only under policy. Escalate insufficient authority, excessive risk, repeated failures, unavailable critical dependencies, objective conflict, or infeasible constraints with context, impact, attempts, options, recommendation, required decision, and deadline.

Tool failures SHOULD be classified for bounded retry, alternate capability, alternate agent, replanning, or escalation rather than automatically invalidating the plan.

---

## 16. Partial Completion

Planner MUST support partial completion.

Example:

```text
Task A  ✓
Task B  ✓
Task C  ✗
Task D  BLOCKED
```

The system MUST NOT report the entire mission as completed while required task conditions remain unmet. Required, optional, and conditional work MUST remain distinguishable.

Partial results may be preserved and reused after replanning.

---

## 17. Branching and Loops

Plans may contain bounded branches and loops.

Every loop MUST define:

```text
termination condition
maximum iterations
budget limit
failure condition
escalation condition
```

Unbounded autonomous loops are prohibited. Every autonomous plan MUST have a goal, scope, termination condition, and budget, preventing infinite task generation even outside an explicit loop. Deterministic workflow logic SHOULD use explicit rules rather than unnecessary model reasoning; routine execution scheduling remains lightweight and runtime-owned.

---

## 18. Replanning

Replanning may be triggered by:

- objective change;
- decision change;
- failed task;
- unexpected external state;
- dependency failure;
- resource exhaustion;
- deadline risk;
- new evidence;
- security event;
- objective drift;
- verification failure.

Replanning SHOULD modify the smallest affected portion of the plan when possible and avoid churn, duplicate work, and lost context. Conflicting plans SHOULD be detected and routed to Decision Engine/Executive, not silently resolved by Planner. Likely duplicate missions/tasks SHOULD be detected unless redundancy is justified for independent verification or review.

Planner SHOULD periodically revalidate critical assumptions and objective contribution using runtime feedback. Inactive/superseded objectives or revoked/superseded decisions require dependent-work reevaluation. Adjacent material work requires an explicit new task within authority or an approved plan revision; discovery alone does not expand scope.

---

## 19. Plan Versioning

Plans are immutable by version.

Example:

```text
PLAN v1
 ↓
REPLAN
 ↓
PLAN v2
```

The system retains:

- previous plan;
- reason for change;
- triggering event;
- affected tasks;
- actor;
- authorization.

Running tasks must not silently change underneath execution.

---

## 20. Staleness and Cancellation

A plan or task becomes stale when its assumptions are no longer valid.

Possible causes:

- objective superseded;
- decision revoked;
- policy changed;
- external state changed;
- deadline passed;
- required resource unavailable.

Stale or expired work MUST NOT execute automatically; it should stop, pause, or be reconciled according to policy. Cancellation SHOULD propagate to affected dependencies through Workflow, preserve actor/reason/time and incomplete-work records, and include cleanup and state verification where needed to avoid corrupting shared state.

---

## 21. Compensation

When partial execution creates side effects that cannot simply be retried, Planner SHOULD define compensation or recovery steps.

This may integrate with Workflow Saga/compensation mechanisms.

Compensation is not assumed to perfectly undo real-world effects.

---

## 22. Preflight

Before execution, Planner SHOULD support:

```text
VALIDATE OBJECTIVE
VALIDATE DECISION
VALIDATE DEPENDENCIES
VALIDATE AUTHORITY
VALIDATE RESOURCES
VALIDATE TOOLS
VALIDATE MODELS
VALIDATE INPUTS
VALIDATE VERIFICATION
```

Preflight SHOULD also check freshness, budgets, deadlines, expected outputs, and consistency with the approved plan version. A failed preflight SHOULD prevent unsafe execution. Runtime MUST validate that executable tasks match the approved plan; material changes require replanning or authorization rather than silent execution drift.

High-impact workflows SHOULD use dry-run/simulation where practical to examine normal execution, failure paths, resource shortages, and agent/dependency failure. Simulation is not proof of execution or authorization.

---

## 23. Execution Handoff

Planner hands an executable plan to Workflow Orchestration.

```text
PLANNER
 ↓
VALIDATED PLAN
 ↓
WORKFLOW ORCHESTRATION
 ↓
SCHEDULER
 ↓
AGENT RUNTIME
 ↓
TOOL RUNTIME
```

Planner does not directly invoke tools or agents as an execution shortcut. Handoff is not completion. Workflow feedback SHOULD expose task starts, progress, blocks, outputs, verification, failures, and completion, allowing Planner to report health without owning execution state. Progress claims require measurable evidence rather than invented percentages.

### Learning, Reuse, and Visibility

Planner SHOULD record assumptions, actual conditions, failures, supported root causes, and adaptations for Memory and future planning. Reused plans/templates MUST separate stable structure from context-specific values and be revalidated, not blindly replayed.

Owner-facing records SHOULD trace objective → decision → plan → mission → task → agent → output → verification → outcome and explain task purpose, ordering, assignment, replanning, and stopping without raw internal logs. Planning quality SHOULD be evaluated by real outcome, efficiency, robustness, adaptability, resource use, and verification, with observable health, blocked time, failures, churn, duplicate work, and forecast error. Authorized work remains UI-independent through durable Workflow/runtime operation.

---

## 24. Integration

### Executive
Reviews major plans and coordinates decisions; Planner surfaces resources, risk, deadlines, dependencies, and expected outcomes.

### Objective Engine
Provides WHAT, WHY, success criteria, hierarchy, and constraints.

### Decision Engine
Provides evaluated decisions and tradeoffs with the required decision-maker and Governance authorization references; it does not grant execution permission.

### Attention
Determines processing priority; Planner structures prioritized work while Scheduling/Resource Runtime owns scheduling.

### Workflow Orchestration
Owns durable execution.

### Agent Runtime
Provides agent assignment and lifecycle.

### Tool Runtime
Controls actual capability invocation.

### Model Router
Selects model/provider capabilities.

### Scheduler
Allocates runtime resources.

### Governance
Controls authority and policy.

### Persistence
Stores plans, versions, states, and checkpoints.

### Observability
Records plan execution and replanning.

---

## 25. Replanning Example

```text
Objective:
Launch campaign.

Decision:
Use channel set X.

Plan v1:
A. Research
B. Produce assets
C. Validate
D. Publish
E. Measure

External state:
Channel X becomes unavailable.

Planner:
- preserve objective;
- preserve approved constraints;
- mark C/D affected;
- evaluate alternate path within the approved decision;
- route any channel-set change to Decision Engine/Executive for a revised decision and required authorization;
- create Plan v2 only within the resulting approved bounds;
- retain v1 history;
- submit changed actions through Governance.
```

---

## 26. Invariants

1. Planner does not redefine objectives.
2. Planner does not self-authorize execution.
3. Every plan retains objective lineage.
4. WHY survives decomposition.
5. Dependencies are explicit.
6. Parallel work is bounded by resource and conflict rules.
7. Retries are bounded.
8. Loops have termination conditions.
9. Partial completion is not full completion.
10. Plan versions are reconstructable.
11. Stale plans cannot silently continue.
12. Verification is distinct from agent claims.
13. Execution is handed to Workflow Orchestration.

---

## 27. Locked Principle

> **Planner converts authorized intent and decisions into bounded, verifiable, executable plans while preserving WHY. It prepares execution; it does not perform execution.**
