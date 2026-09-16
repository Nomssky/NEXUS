# NEXUS EXECUTIVE

**Status:** LOCKED  
**Role:** System-Level Executive Orchestration  
**Layer:** Cognitive Control

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

---

## 3. Responsibilities

Executive MUST:

- interpret owner intent;
- maintain operational context;
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

Executive MUST NOT convert a question into authorization for a consequential action.

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

A recommendation is not automatically authorization.

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

Delegation MUST preserve objective lineage.

---

## 9. Executive Is Not a Universal Worker

Specialist execution belongs to the appropriate agent and runtime.

Example:

```text
Executive
  ↓
Social Media Objective
  ↓
Planner
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
7. EXECUTE WITHIN AUTHORITY
```

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

It should be concise enough for human decision-making.

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

Monitoring does not require continuous human interaction.

---

## 15. Cross-Division Coordination

Executive may coordinate:

```text
Media ↔ Business
Research → Media
Research → Business
Business → Media
```

Such coordination MUST preserve ownership, scope, and authorization.

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

The system does not require storage of private model chain-of-thought.

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

Executive cannot self-authorize expanded permissions.

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
