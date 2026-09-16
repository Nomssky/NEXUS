# NEXUS PLANNER

**Status:** LOCKED  
**Role:** Plan Generation and Replanning  
**Layer:** Cognitive Control / Execution Preparation

---

## 1. Purpose

Planner converts authorized objectives and decisions into executable plans.

Canonical flow:

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

Planner SHOULD identify conflicting writes and shared-resource contention.

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

Planner produces requirements.

Scheduling and resource allocation are handled by Scheduling/Resource Runtime and Governance.

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

Final runtime authorization remains the responsibility of Agent Runtime and Governance.

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

Context must be bounded to prevent unnecessary context growth.

---

## 11. Verification

Planner defines verification requirements.

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

An agent saying "done" is not sufficient evidence of completion.

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

Different classes trigger different responses.

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

The system MUST NOT report the entire mission as completed.

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

Unbounded autonomous loops are prohibited.

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

Replanning SHOULD modify the smallest affected portion of the plan when possible.

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

Stale work should stop, pause, or be reconciled according to policy.

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

A failed preflight SHOULD prevent unsafe execution.

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

Planner does not directly invoke tools or agents as an execution shortcut.

---

## 24. Integration

### Objective Engine
Provides WHAT, WHY, success criteria, hierarchy, and constraints.

### Decision Engine
Provides authorized decisions and tradeoffs.

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
- evaluate alternate path;
- create Plan v2;
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
