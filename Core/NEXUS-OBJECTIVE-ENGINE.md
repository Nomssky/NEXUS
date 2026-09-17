# NEXUS OBJECTIVE ENGINE

**Status:** LOCKED  
**Role:** Authoritative Objective Intelligence  
**Layer:** Cognitive Control / Intent
**Next layer:** CONTRACTS — exact schemas, APIs, transitions, and algorithms; no new module is implied.

Record sketches express architectural information requirements, not final wire schemas.

---

## 1. Purpose

The Objective Engine is the authoritative subsystem responsible for representing, preserving, relating, prioritizing, tracking, validating, and evolving what NEXUS is trying to achieve.

It answers three fundamental questions:

1. **WHAT** is NEXUS trying to achieve?
2. **WHY** does that objective exist?
3. **HOW DO WE KNOW** whether progress is real?

The locked conceptual flow is [Executive](NEXUS-EXECUTIVE.md) → Objective Engine → [Decision Engine](NEXUS-DECISION-ENGINE.md) → [Planner](NEXUS-PLANNER.md) → [Workflow Orchestration](WORKFLOW_ORCHESTRATION_ENGINE.md).

Objective Engine owns objective truth; Planner prepares plans, not execution. Workflow and the governed Agent/Tool runtimes own execution and external integration handoffs. Models have no execution authority; Objective Engine neither calls external tools nor selects models.

---

## 2. Core Principle

```text
Owner Intent
    ↓
Objective
    ↓
Mission
    ↓
Task
    ↓
Action
    ↓
Result
    ↓
Evaluation
    ↓
Replanning
```

A completed task is not equivalent to an achieved objective.

NEXUS MUST preserve objective intent and WHY through every decomposition layer.

---

## 3. Responsibilities

The Objective Engine MUST:

- represent objectives as durable entities;
- preserve owner intent and purpose;
- maintain objective hierarchy and lineage;
- define desired outcomes and success criteria;
- track measurable and qualitative progress;
- identify dependencies and conflicts;
- detect objective drift;
- evaluate objective health;
- expose objective context to downstream systems;
- support objective revision through governed changes;
- retain historical versions and lineage;
- emit attention signals when objectives become blocked, at risk, or materially changed.

The Objective Engine MUST NOT:

- execute specialist work;
- bypass Governance;
- grant authority to agents;
- silently redefine owner intent;
- convert a task completion into objective success without evidence.

---

## 4. Objective Hierarchy

NEXUS supports hierarchical intent:

```text
OWNER
 └── BUSINESS
      └── DIVISION
           └── MISSION
                └── TASK
                     └── ACTION
```

An objective may have parent and child objectives.

Every derived objective MUST retain a parent and source lineage, remain revisable, and preserve parent intent, constraints, and policy. Generated objectives MUST remain distinguishable from direct owner intent and cannot silently become owner-level objectives.

Objectives describe outcomes; missions coordinate effort, tasks describe work, and actions perform concrete steps. Objectives may exist without an immediate mission or activation. Structuring ambiguous owner intent MUST NOT change its fundamental meaning; business and division objectives retain explicit scope.

---

## 5. Objective Identity

Each objective MUST have a stable unique identity and recorded source. Identity and lineage persist through revisions.

Minimum objective record:

```yaml
objective_id:
business_id:
parent_objective_id:
type:
title:
purpose:
desired_outcome:
constraints:
success_criteria:
metrics:
priority:
status:
health:
source:
version:
created_by:
created_at:
updated_at:
```

Optional fields:

```yaml
division_id:
owner_intent_ref:
deadline:
dependencies:
conflicts:
stakeholders:
risk_tolerance:
context_refs:
evidence_refs:
supersedes:
superseded_by:
```

---

## 6. WHAT / WHY / SUCCESS

Every meaningful objective SHOULD contain:

### WHAT
The desired outcome.

### WHY
The reason the objective exists.

### SUCCESS
Observable criteria proving the objective has been achieved.

Example:

```text
WHAT:
Increase qualified inbound leads.

WHY:
Support sustainable customer acquisition for the business.

SUCCESS:
Qualified leads increase by the defined target while remaining within
the approved acquisition cost and policy constraints.
```

WHY is mandatory for strategic and owner-originated objectives. Objective constraints are binding, not optimization preferences; Governance remains authoritative over permission.

---

## 7. Objective Relations

Supported relations:

- `PARENT_OF`
- `SUPPORTS`
- `DEPENDS_ON`
- `CONFLICTS_WITH`
- `SUPERSEDES`
- `DERIVED_FROM`
- `RELATED_TO`

Relations MUST have explicit semantics and be auditable. The engine MUST prevent circular parentage, impossible lineage, orphaned consequential missions, and silently accepted conflicting constraints. It represents objective conflicts; Executive/Decision/Governance resolve them within authority rather than the engine silently erasing or choosing an objective. Revision preserves history and does not silently mutate historical truth; hard deletion must not break audit lineage.

### Priority

Priority MUST distinguish local operational priority from inherited strategic importance and urgency. Owner priority, value, risk, dependencies, deadlines, resources, and business impact SHOULD inform prioritization; urgency alone does not outrank strategic importance. Lower-level priority cannot override higher-level constraints. Blocked dependencies SHOULD be visible to Attention and Executive.

---

## 8. Objective States

Canonical states:

```text
DRAFT
ACTIVE
PAUSED
BLOCKED
AT_RISK
ACHIEVED
FAILED
ABANDONED
SUPERSEDED
EXPIRED
ARCHIVED
```

State transitions MUST be governed and recorded. DRAFT is unactivated; ACTIVE is pursued; PAUSED is intentionally suspended; BLOCKED lacks a dependency or condition; AT_RISK indicates threatened achievement. ACHIEVED requires verified criteria; FAILED means criteria cannot be met within scope/time; ABANDONED is explicit discontinuation; SUPERSEDED is replacement; EXPIRED ends validity. ARCHIVED is retention, not a successful operational outcome.

---

## 9. Objective Health

Health describes progress independently from lifecycle state.

```text
ON_TRACK
AT_RISK
BLOCKED
OFF_TRACK
UNKNOWN
```

An objective can be `ACTIVE` while its health is `AT_RISK`.

---

## 10. Progress Evaluation

Progress MUST be evidence-based.

Possible evidence:

- verified task outcomes;
- business metrics;
- workflow results;
- external system state;
- agent observations;
- human confirmation;
- validated knowledge;
- approved derived metrics.

NEXUS MUST distinguish:

```text
OUTPUT
≠
VERIFIED OUTPUT
≠
OBJECTIVE PROGRESS
≠
OBJECTIVE ACHIEVEMENT
```

Proxy metrics MUST be explicitly marked as proxies. Activity, output, and outcome metrics SHOULD remain distinct, with outcomes generally carrying greater objective significance. Metric evidence SHOULD retain its definition, source, baseline/target, measurement window, freshness, and confidence.

Progress may be UNKNOWN and MUST NOT be fabricated from missing evidence or task counts. Confidence MUST reflect evidence quality, not model self-assurance. Evaluation SHOULD compare intent, plan, execution, and outcomes to expose ineffective strategies, side effects, and incomplete evidence as well as proxy drift.

---

## 11. Objective Drift

Objective drift occurs when execution increasingly optimizes something different from the original objective.

NEXUS SHOULD detect:

- changing optimization targets;
- loss of WHY;
- unauthorized scope expansion;
- proxy metric substitution;
- conflicting derived objectives;
- task success with declining objective progress.

When material drift is detected:

```text
DRIFT DETECTED
    ↓
OBJECTIVE EVALUATION
    ↓
ATTENTION / EXECUTIVE
    ↓
REPLAN OR GOVERNED REVISION
```

---

## 12. Revision and Versioning

Objectives are versioned.

A revision MUST record:

- previous version;
- changed fields;
- reason;
- actor;
- timestamp;
- authorization;
- affected descendants.

Changing WHAT, WHY, constraints, or success criteria is a material change and MUST pass Governance.

---

## 13. Objective Context

Downstream systems receive a bounded objective context containing at minimum:

```text
Objective
Purpose / WHY
Desired Outcome
Success Criteria
Constraints
Priority
Dependencies
Conflicts
Current State
Health
Relevant Evidence
```

The Planner MUST preserve this context during decomposition.

Agents MUST NOT receive an objective without sufficient lineage to understand its purpose and constraints. Access and queries MUST be scope-authorized and provide only relevant objective context, not unrelated global or other-business memory. Consumers SHOULD be able to retrieve lineage, scoped active/blocked/at-risk objectives, dependencies, conflicts, criteria, metrics, and health without prescribing API signatures.

---

## 14. Objective-Aware Planning

Planner uses the Objective Engine to answer:

- What outcome matters?
- Why does it matter?
- What constraints apply?
- What constitutes success?
- Which objectives are dependencies?
- Which objectives conflict?
- What evidence already exists?

Planner converts objective-aligned, authorized decisions into executable plans without redefining intent or bypassing Decision Engine.

---

## 15. Objective-Aware Decision Making

Decision Engine MUST reference objective IDs for objective-driven work. Authorized maintenance, recovery, or governance exceptions require an explicit purpose and authority basis, not invented owner intent.

A decision is valid only when its relationship to the objective, or its governed exception, is understandable.

The system SHOULD detect decisions that optimize local task metrics while harming higher-level objectives.

---

## 16. Objective-Aware Attention

Attention SHOULD be influenced by:

- objective criticality;
- objective health;
- deadline proximity;
- blocked dependencies;
- material deviation;
- unexpected external events;
- objective conflicts;
- evidence of drift.

Attention does not modify objective truth.

---

## 17. Objective Lifecycle

```text
CREATE
 ↓
VALIDATE
 ↓
ACTIVATE
 ↓
DECOMPOSE
 ↓
EXECUTE
 ↓
MEASURE
 ↓
EVALUATE
 ├── CONTINUE
 ├── REPLAN
 ├── PAUSE
 ├── ESCALATE
 ├── REVISE
 └── COMPLETE
```

Completion MUST be evidence-based. Decomposition and execution above occur through Planner and Workflow, not Objective Engine.

Creation is distinct from activation; activation requires Governance-defined authority. Temporary objectives SHOULD have explicit scope and applicable expiry, remain distinguishable from enduring strategy, and not automatically enter long-term strategic memory. Expiry SHOULD prompt evaluation for renewal, revision, replacement, or escalation; automatic renewal MUST be policy-controlled. Stale or abandoned objectives SHOULD be identifiable rather than silently pursued.

Cancellation MUST retain related mission/decision history. Affected active work must be evaluated for safe stopping, redirection, or completion through Executive/Planner/Workflow according to policy, not directly terminated by an objective record change.

---

## 18. Multi-Business Isolation

Each objective MUST belong to an explicit scope.

Business and division references MUST be present where applicable. Owner/system-level objectives retain their explicit higher scope rather than inventing a business or division; a division reference is not mandatory for a business-wide objective.

Cross-business objectives require explicit authorization.

One business MUST NOT inherit another business's objectives merely because the same owner operates both.

---

## 19. Governance

Objective creation and mutation are governed.

Governance MUST control:

- who may create objectives;
- who may modify them;
- which scopes may be affected;
- materiality thresholds;
- approval requirements;
- expiration;
- archival;
- emergency changes.

Agents cannot elevate their authority by creating a higher-level objective.

---

## 20. Persistence and Audit

Objective state MUST be durable.

The system MUST preserve:

- objective history;
- state transitions;
- revisions;
- lineage;
- progress evidence;
- evaluation results;
- actor identity;
- authorization;
- timestamps.

Objective truth MUST survive process restart with definitions, state, lineage, and history intact; pending updates MUST be recoverable or safely rejected, never silently lost. Concurrent progress, metric, and priority updates MUST NOT silently overwrite one another; ordering/versioning or transactions preserve consistency.

Important events SHOULD be append-only where practical, with current state consistent with authoritative history; this does not mandate event-sourced storage. Archive or supersede historical objectives by default. Hard deletion is exceptional and MUST NOT destroy required audit lineage.

Material lifecycle, priority, progress/metric, dependency, conflict, drift, and expiry changes SHOULD emit scoped attention signals through Event Trigger System; event contracts remain its responsibility.

---

## 21. Integration

### Executive
Decides what deserves action and prioritizes but reads objective truth from Objective Engine.

### Decision Engine
Evaluates options against objectives and constraints.

### Planner
Transforms objective-aligned, authorized decisions into executable plans.

### Workflow Orchestration
Executes durable plans.

### Agent Runtime
Provides governed agent execution.

### Attention
Receives objective-related signals.

### Memory
Stores relevant contextual and historical information but MUST NOT silently rewrite authoritative objective truth.

### Event Trigger System
Can activate evaluation or workflows related to objectives.

### Governance
Controls authority and mutations.

### Persistence
Stores durable objective state.

### Observability
Records objective-related telemetry and decision traces.

---

## 22. Invariants

1. WHY survives decomposition.
2. Objective lineage is never silently broken.
3. Task completion does not prove objective achievement.
4. Agents cannot redefine higher-level intent.
5. Objective scope is explicit.
6. Material changes are governed.
7. Objective state is reconstructable.
8. Progress requires evidence.
9. Proxy metrics are identified as proxies.
10. Objective Engine owns objective truth, not execution.

---

## 23. Locked Principle

> **The Objective Engine owns the truth of what NEXUS is trying to achieve and why. It does not own execution.**
