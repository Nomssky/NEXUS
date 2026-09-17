# NEXUS EXECUTIVE

**Status:** LOCKED  
**Role:** System-Level Executive Orchestration  
**Layer:** Cognitive Control
**Next layer:** CONTRACTS — exact schemas, APIs, state machines, and algorithms; no new module is implied.

Record sketches express architectural information requirements, not final wire schemas.

---

## 1. Purpose

NEXUS Executive acts as the operational proxy of the owner.

Its core question is:

> Given everything NEXUS knows, what should happen next, why, and who should do it?

Executive coordinates rather than becoming a universal worker.

---

## 2. Core Flow

```text
UNDERSTAND
    ↓
PRIORITIZE
    ↓
DECIDE
    ↓
DELEGATE
    ↓
COORDINATE
    ↓
MONITOR
    ↓
EVALUATE
    ↓
ESCALATE / ADAPT
```

Executive is continuously informed by objectives, decisions, events, attention, workflows, agent outcomes, and business state.

The locked conceptual flow is Executive → [Objective Engine](NEXUS-OBJECTIVE-ENGINE.md) → [Decision Engine](NEXUS-DECISION-ENGINE.md) → [Planner](NEXUS-PLANNER.md) → [Workflow Orchestration](WORKFLOW_ORCHESTRATION_ENGINE.md). Feedback and review do not bypass these ownership boundaries. Executive coordinates authorized work; models have no execution authority.

---

## 3. Responsibilities

Executive MUST:

- interpret owner intent;
- maintain strategic and operational context;
- understand current objectives;
- prioritize competing work;
- coordinate multiple divisions;
- coordinate multiple businesses;
- delegate work to appropriate specialists;
- monitor outcomes;
- intervene when necessary;
- initiate replanning;
- respond to important events;
- escalate issues requiring owner involvement.

Executive MUST NOT:

- perform every specialist task itself;
- bypass Governance;
- bypass Tool Runtime;
- invent owner intent;
- silently expand authority;
- require a UI session to operate.

---

## 4. Owner Intent Classification

Owner inputs SHOULD be classified as:

```text
OBJECTIVE
PREFERENCE
CONSTRAINT
POLICY
INFORMATION
QUESTION
REQUEST
COMMAND
```

Classification MUST preserve ambiguity when intent is uncertain.

Executive MUST NOT convert a question into authorization for a consequential action or a casual statement into permanent policy. Material ambiguity SHOULD lead to clarification or a safe, bounded interpretation, not assumed high-authority instruction.

---

## 5. Relationship to Objective Engine

Objective Engine is the source of truth for objectives.

Executive:

```text
READ OBJECTIVE
    ↓
UNDERSTAND PRIORITY / HEALTH
    ↓
COORDINATE RESPONSE
```

Executive does not silently rewrite objectives.

Material objective changes go through Objective Engine and Governance.

Consequential Executive decisions SHOULD reference objectives or explicitly identify authorized maintenance, recovery, or governance work. Executive SHOULD detect proxy optimization and redirect work when activity improves while intended outcomes deteriorate.

---

## 6. Relationship to Attention

Attention filters incoming signals before Executive receives interruption-worthy information.

```text
RAW EVENTS
 ↓
EVENT SYSTEM
 ↓
ATTENTION
 ↓
EXECUTIVE
```

Executive can request attention escalation but cannot fabricate urgency.

---

## 7. Relationship to Decision Engine

Executive may request a structured decision.

```text
EXECUTIVE
 ↓
DECISION ENGINE
 ↓
OPTIONS / EVIDENCE / RISKS
 ↓
RECOMMENDATION
 ↓
EXECUTIVE
```

A recommendation is not automatically authorization. Executive SHOULD use Decision Engine for structured evaluation rather than inventing an independent policy system.

### Planning Relationship

Executive specifies the desired accomplishment; Planner prepares the dependency-aware plan for the selected, authorized decision. Executive may reject or request revision/reprioritization when objectives, authority, risk, resources, or circumstances conflict; accepted work proceeds through Workflow, not direct model execution.

---

## 8. Delegation

Every meaningful delegation SHOULD contain:

```yaml
mission:
objective_refs:
objective_why:
desired_outcome:
scope:
constraints:
authority:
deadline:
success_criteria:
resources:
escalation_conditions:
verification_requirements:
```

Delegation MUST preserve objective lineage. It SHOULD provide enough upstream purpose and constraints for independent work without micromanagement or unrelated global context.

Selection SHOULD be capability-based, considering skills, modality/model availability, workload, performance, permissions, memory and business/division scope, and resource constraints, not agent name alone. Executive specifies mission needs; Planner and the existing Agent Runtime/Workflow boundaries refine requirements and perform assignment.

---

## 9. Executive Is Not a Universal Worker

Specialist execution belongs to the appropriate agent and runtime.

Example:

```text
Executive
  ↓
Objective Engine: Social Media Objective
  ↓
Decision Engine: Evaluate Options
  ↓
Authorized Decision / Planner
  ↓
Workflow Orchestration
  ↓
Content Strategy Agent
  ↓
Copy Agent
  ↓
Design Agent
  ↓
Tool Runtime
```

Executive may intervene when coordination, conflict resolution, prioritization, or escalation requires it.

---

## 10. Multi-Business Operation

One NEXUS instance may operate multiple businesses concurrently.

Executive MUST maintain explicit business scope:

```text
NEXUS
 ├── Business A
 │    ├── Media
 │    ├── Business
 │    └── Research
 │
 └── Business B
      ├── Media
      ├── Business
      └── Research
```

Switching the UI does not stop background operations.

Cross-business coordination requires explicit reason and authorization.

---

## 11. Intervention Ladder

Executive SHOULD prefer the least invasive effective intervention:

```text
1. OBSERVE
2. RECOMMEND
3. REQUEST AGENT ACTION
4. REPLAN
5. COORDINATE DIVISIONS
6. ESCALATE TO OWNER
7. REQUEST WORKFLOW EXECUTION WITHIN AUTHORITY
```

These are intervention choices, not a requirement to escalate before ordinary authorized work. Prefer local adjustment or bounded retry over restarting a mission when sufficient; runtime layers perform the retry or execution.

The presence of autonomy does not grant unlimited authority.

---

## 12. Autonomy Modes

Possible operating modes:

```text
OBSERVE
ASSIST
DELEGATE
AUTONOMOUS
GUARDED_AUTONOMOUS
EMERGENCY
```

Autonomy mode controls behavior, not authority.

Governance always remains superior.

---

## 13. Escalation Package

When owner attention is required, Executive SHOULD provide:

```text
WHAT HAPPENED
WHY IT MATTERS
OBJECTIVE AFFECTED
CURRENT STATE
OPTIONS
RISKS
RECOMMENDED NEXT STEP
WHAT AUTHORIZATION IS REQUIRED
DEADLINE / CONSEQUENCE OF DELAY
```

It should be concise enough for human decision-making. Escalation SHOULD occur when authority is insufficient, policy or objectives materially conflict, approval is required, uncertainty is consequential, resources are unavailable, bounded recovery fails, or owner judgment is uniquely needed. Human attention is reserved for valuable decisions, not a mandatory step in ordinary authorized work.

---

## 14. Monitoring

Executive monitors:

- objective health;
- workflow state;
- agent health;
- critical failures;
- resource pressure;
- deadlines;
- business signals;
- unresolved decisions;
- repeated retries;
- security alerts;
- attention backlog.

Monitoring does not require continuous human interaction. Executive SHOULD monitor mission health and continued value rather than individual tokens/tool calls; execution detail belongs to runtime/observability layers. Cost anomalies, low confidence, resource starvation, external changes, and objective drift SHOULD inform intervention.

Operational history SHOULD preserve useful decisions, outcomes, failures, preferences, successful strategies, and rejected approaches through Memory. Freshness and confidence must be evaluated before reuse; Executive cannot permanently promote trusted knowledge without governed learning.

---

## 15. Cross-Division Coordination

Executive may coordinate:

```text
Media ↔ Business
Research → Media
Research → Business
Business → Media
```

Such coordination MUST preserve ownership, scope, and authorization. Shared objective context does not grant access to unrelated division or business data; cross-business sharing requires explicit permission or applicable owner policy.

### Conflicts and Resources

Executive SHOULD resolve competing objectives, missions, recommendations, deadlines, and resource demands within delegated authority. Governance defines policy precedence; applicable hard constraints, owner policy, and authority bound objective tradeoffs and optimization preferences. Executive MUST NOT invent a higher authority.

Scarce compute, model/API budgets, rate limits, concurrency, and human attention SHOULD be coordinated according to objective priority; consuming resources needed by higher-priority work requires justification. Scheduling/Resource Runtime performs allocation rather than Executive implementing a competing scheduler.

---

## 16. Communication

Executive communicates through the Communication Bus.

Communication carries information and intent.

It does not itself grant authority.

Authority is evaluated by Governance and Identity systems.

---

## 17. UI Independence

NEXUS Executive MUST operate independently from UI sessions.

UI is a control surface.

The runtime heartbeat is:

```text
PERSISTENCE
+
SCHEDULER
+
WORKERS
+
EVENTS
+
WORKFLOWS
```

not the browser or desktop interface.

---

## 18. Decision Trace

Executive decisions MUST be explainable through structured records:

```text
INPUT
OBJECTIVE
CONSTRAINTS
EVIDENCE
DECISION REFERENCE
AUTHORITY
ACTION
OUTCOME
```

Trace records SHOULD retain decision identity/time, context, policy and authority references, considered and selected actions, a concise reason, expected outcome, risk, actual result, and evaluation. These are operational explanations, not private model chain-of-thought; its storage is not required.

---

## 19. Governance

Executive actions remain subject to:

```text
SYSTEM
 ↓
GLOBAL
 ↓
USER
 ↓
BUSINESS
 ↓
DIVISION
 ↓
AGENT
 ↓
WORKFLOW
 ↓
TASK
 ↓
TOOL
 ↓
MODEL
```

More restrictive applicable policy wins.

Executive cannot self-authorize expanded permissions or bypass authentication, authorization, tool policy, business isolation, audit requirements, or safety controls. Stronger reasoning grants no additional authority.

---

## 20. Failure Handling

Executive SHOULD distinguish:

- temporary failure;
- retryable failure;
- dependency failure;
- authority failure;
- objective risk;
- external unknown state;
- security incident;
- owner decision required.

Repeated failure should trigger bounded escalation or replanning.

Executive SHOULD expose explicit operational state with event-driven transitions where practical; the exact state machine remains for CONTRACTS. Failures MUST NOT corrupt mission state. Persistent checkpoints, restart reconstruction, duplicate prevention, and idempotency where possible SHOULD allow safe continuation or a recoverable pause through existing runtime/persistence boundaries.

Concurrent business, division, and mission coordination MUST NOT depend on a selected UI session or assume one session equals one mission. Concurrency controls MUST prevent conflicting Executive decisions from corrupting shared state.

---

## 21. Invariants

1. Owner remains root authority.
2. Executive coordinates rather than becoming a universal worker.
3. Executive cannot self-expand authority.
4. Objective truth comes from Objective Engine.
5. Decisions are distinct from authorization.
6. UI sessions do not control system liveness.
7. Multi-business execution remains isolated by default.
8. Specialist capabilities remain delegated to specialist agents.
9. Governance cannot be bypassed by urgency.
10. Executive behavior remains observable and auditable.

---

## 22. Locked Principle

> **NEXUS Executive is the owner's operational proxy: it decides what should happen next, coordinates who should do it, monitors outcomes, and escalates when necessary. It is not the universal worker of NEXUS.**
