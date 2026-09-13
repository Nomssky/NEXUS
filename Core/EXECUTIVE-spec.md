# NEXUS Executive

**Status:** PROPOSED → awaiting owner lock

## 1. Definition

The NEXUS Executive is the system-level executive intelligence responsible for translating owner intent and strategic objectives into coordinated autonomous action across businesses, divisions, and agents.

The Executive is the operational proxy of the owner at the NEXUS level.

It is **not** a universal worker and must not directly perform every specialized task.

Its primary function is:

```text
Understand
  -> Prioritize
  -> Decide
  -> Delegate
  -> Coordinate
  -> Monitor
  -> Evaluate
  -> Escalate / Adapt
```

## 2. Core Responsibility

The Executive answers:

> "Given everything NEXUS knows, what should happen next, why, and who should do it?"

It operates between high-level objectives and the orchestration system.

```text
OWNER
  |
  v
EXECUTIVE
  |
  +--> OBJECTIVE ENGINE
  +--> ATTENTION
  +--> MEMORY
  +--> DECISION ENGINE
  +--> PLANNER
  |
  v
ORCHESTRATOR
```

## 3. Responsibilities

The Executive is responsible for:

1. understanding owner intent;
2. maintaining strategic context;
3. interpreting objective hierarchy;
4. prioritizing competing objectives;
5. deciding whether action is necessary;
6. determining the appropriate level of intervention;
7. delegating work to divisions and agents;
8. coordinating cross-division work;
9. resolving conflicts within delegated authority;
10. monitoring important missions;
11. responding to significant events;
12. recognizing when escalation is required;
13. adapting plans when circumstances change;
14. ensuring actions remain aligned with objectives and policy;
15. evaluating whether autonomous work remains worthwhile.

## 4. Non-Responsibilities

The Executive must not become a giant agent that performs every job.

It should generally not:

- write every social-media caption itself;
- conduct every research task itself;
- execute external APIs directly;
- bypass the Tool System;
- bypass policy;
- silently change owner authority;
- invent objectives without a legitimate source;
- permanently modify trusted knowledge without governed learning;
- micromanage every task;
- require UI interaction for ordinary autonomous work.

Specialized execution belongs to specialist agents.

## 5. Relationship With the Owner

The Owner is the root authority.

The Executive interprets owner intent but does not supersede it.

Owner inputs may include:

- explicit objectives;
- preferences;
- constraints;
- policies;
- priorities;
- approvals;
- prohibitions;
- strategic direction;
- contextual information.

The Executive should distinguish between:

```text
OWNER OBJECTIVE
OWNER PREFERENCE
OWNER CONSTRAINT
OWNER POLICY
OWNER INFORMATION
OWNER QUESTION
OWNER REQUEST
```

These are not interchangeable.

For example, a casual statement should not automatically become a permanent policy.

## 6. Intent Interpretation

The Executive should transform owner input into structured intent.

Conceptually:

```text
Raw Owner Input
      |
      v
Intent Interpretation
      |
      +--> Objective?
      +--> Preference?
      +--> Constraint?
      +--> Policy?
      +--> Question?
      +--> Command?
      |
      v
Structured Context
```

Ambiguous intent should not be silently treated as high-authority instruction when the consequences are significant.

The Executive may ask for clarification or choose a safe bounded interpretation.

## 7. Objective Relationship

The Executive does not own the canonical objective data model.

The Objective Engine is the source of truth for objectives.

The Executive consumes objective state and proposes/initiates changes through defined interfaces.

```text
Executive
    |
    v
Objective Engine
    |
    +--> hierarchy
    +--> state
    +--> priority
    +--> dependencies
    +--> lineage
    +--> progress
```

Every consequential Executive decision should be explainable in relation to one or more objectives, or explicitly identified as maintenance/recovery/governance work.

## 8. Objective Hierarchy

The Executive may operate across:

```text
Owner Objective
  |
  +-- Business Objective
       |
       +-- Division Objective
            |
            +-- Mission
                 |
                 +-- Task
```

The Executive should preserve lineage when delegating.

Example:

```text
Increase monthly qualified traffic
    ->
Improve social discovery
    ->
Run content experiment
    ->
Media Team Mission
    ->
Content Research Task
```

The specialist should receive enough upstream context to understand the purpose of its work without receiving irrelevant global context.

## 9. Attention Relationship

The Executive should not continuously inspect every event.

Attention determines which signals deserve Executive cognition.

```text
EVENTS
  |
  v
ATTENTION
  |
  +--> ignore
  +--> aggregate
  +--> monitor
  +--> delegate
  +--> Executive review
  +--> urgent escalation
```

The Executive receives prioritized signals rather than raw noise wherever practical.

## 10. Decision Relationship

The Executive uses the Decision Engine rather than improvising an independent policy system.

Decision inputs may include:

- objective state;
- attention signals;
- memory;
- business context;
- current missions;
- resource availability;
- agent capabilities;
- policies;
- authority;
- risk;
- deadlines;
- expected outcomes;
- recent failures;
- environmental state.

Conceptually:

```text
Context
  + Objective
  + Policy
  + Authority
  + Resources
  + Memory
  + Signals
       |
       v
Decision Engine
       |
       v
Candidate Decisions
       |
       v
Executive Selection
```

## 11. Planning Relationship

The Executive determines what should be accomplished.

The Planner determines how that goal can be converted into an executable plan.

```text
Executive:
"Achieve X."

Planner:
"Here is a dependency-aware plan for X."
```

The Executive may reject, revise, or re-prioritize a plan if it conflicts with objectives, authority, risk, resources, or changing circumstances.

## 12. Delegation

Delegation is a primary Executive capability.

A delegation should contain enough information for an autonomous agent or division to operate without repeated Executive intervention.

Conceptual delegation:

```text
Delegation
├── mission
├── objective lineage
├── desired outcome
├── scope
├── constraints
├── authority
├── deadline / timing
├── success criteria
├── available resources
└── escalation conditions
```

Delegation should avoid unnecessary micromanagement.

## 13. Agent Selection

The Executive should not select an agent solely by name.

Selection should consider:

- capability;
- skills;
- current availability;
- workload;
- performance history;
- required modality;
- model availability;
- permissions;
- memory scope;
- business/division scope;
- mission requirements;
- cost/resource constraints.

Agent selection may be delegated to an Agent Registry/Orchestrator subsystem, with the Executive specifying mission requirements.

## 14. Cross-Division Coordination

The Executive is responsible for coordination when a mission spans divisions.

Example:

```text
Research
   |
   v
Media
   |
   v
Business
```

The Executive should preserve shared objective context while respecting division boundaries.

A division must not automatically gain access to unrelated business data merely because the Executive is coordinating the mission.

## 15. Multi-Business Coordination

The Executive may coordinate multiple businesses under one NEXUS installation.

Example:

```text
NEXUS Executive
   |
   +--> Business A
   |
   +--> Business B
   |
   +--> Business C
```

Business context must remain isolated by default.

Cross-business information sharing requires explicit permission or an applicable owner-level policy.

The Executive may prioritize work across businesses according to owner objectives and policy.

## 16. Conflict Resolution

Conflicts may occur between:

- objectives;
- missions;
- divisions;
- resource demands;
- deadlines;
- policies;
- agent recommendations;
- business priorities.

Resolution order should generally respect:

```text
Hard Safety / System Constraints
        >
Owner Policy
        >
Authority
        >
Higher-Level Objective
        >
Lower-Level Objective
        >
Optimization Preference
```

The exact policy precedence must ultimately be defined by Governance/Authority specifications.

The Executive must not invent a higher authority level.

## 17. Resource Allocation

The Executive may coordinate scarce resources such as:

- compute;
- GPU;
- model quotas;
- API budgets;
- tool rate limits;
- agent concurrency;
- human attention.

Resource allocation should be objective-aware.

For example, a low-priority task should not consume scarce GPU capacity needed for a high-priority mission without justification.

## 18. Autonomy Levels

The Executive should support different operational modes.

Conceptual levels:

```text
OBSERVE
  |
ASSIST
  |
BOUNDED AUTONOMY
  |
HIGH AUTONOMY
```

These are not permission levels by themselves.

Actual authority is determined by Governance.

An autonomous mode may determine how much initiative is expected, while policy determines what actions are permitted.

## 19. Escalation

Escalation is a valid autonomous decision.

The Executive should escalate when:

- authority is insufficient;
- policies conflict;
- high-risk action requires approval;
- uncertainty is consequential;
- repeated recovery attempts fail;
- objectives materially conflict;
- required resources are unavailable;
- owner judgment is uniquely required.

An escalation should contain:

```text
What happened
Why it matters
Relevant objective
Current state
Options
Recommendation
Risk
Exact decision required
```

The Executive should avoid vague alerts such as "Something went wrong."

## 20. Intervention Levels

The Executive should prefer the least disruptive intervention that can resolve a problem.

Conceptually:

```text
No action
   ->
Monitor
   ->
Agent adjustment
   ->
Task retry
   ->
Plan adjustment
   ->
Mission replanning
   ->
Cross-division coordination
   ->
Escalation
```

The system should not restart an entire mission when a local retry is sufficient.

## 21. Monitoring

The Executive should monitor mission health rather than every individual token/tool call.

Useful signals include:

- mission progress;
- blocked dependencies;
- deadline risk;
- repeated failures;
- unexpected cost;
- objective drift;
- policy violations;
- low confidence;
- external changes;
- resource starvation.

Detailed execution remains the responsibility of lower runtime/observability layers.

## 22. Objective Drift

The Executive should detect when execution begins optimizing a proxy instead of the intended objective.

Example:

```text
Objective:
Increase qualified leads.

Proxy:
Increase impressions.

Failure:
Agents maximize impressions while qualified leads decline.
```

The Executive should be able to recognize this mismatch through evaluation signals and redirect work.

## 23. Decision Memory

The Executive should retain useful operational history.

Examples:

- previous decisions;
- outcomes;
- recurring failures;
- owner preferences;
- successful strategies;
- rejected approaches;
- business-specific patterns.

However, memory is not automatically truth.

Historical information must be evaluated for freshness and confidence.

## 24. Decision Trace

Consequential Executive decisions should produce structured trace information.

Minimum conceptual trace:

```text
Decision ID
Objective
Context reference
Policy reference
Authority reference
Candidate action
Selected action
Reason summary
Expected outcome
Risk
Timestamp
Result
Evaluation
```

This is an audit/explanation record, not private chain-of-thought.

## 25. Executive State

The Executive should have an explicit operational state.

Conceptual states:

```text
IDLE
OBSERVING
ANALYZING
DECIDING
DELEGATING
MONITORING
REPLANNING
ESCALATING
PAUSED
SAFE_MODE
ERROR_RECOVERY
```

State transitions should be event-driven where possible.

The exact state machine will be defined during implementation design.

## 26. Failure Handling

Executive failures must not automatically corrupt mission state.

The runtime should support:

- persistence;
- restart;
- recovery;
- duplicate prevention;
- idempotency where possible;
- checkpointing;
- escalation after repeated failure.

A restarted Executive should reconstruct enough state from persistent storage to continue safely.

## 27. Concurrency

The Executive may coordinate multiple businesses, divisions, and missions concurrently.

It must not assume:

```text
one user session = one active mission
```

or:

```text
one selected business = only running business
```

Concurrency controls must prevent conflicting decisions from corrupting shared state.

## 28. UI Independence

The Executive must operate without an open UI session.

UI interactions are inputs/observations, not the heartbeat of autonomy.

The Executive should continue authorized work when:

- browser is closed;
- user changes business session;
- user changes dashboard;
- no UI is connected.

## 29. Human Interaction

Human involvement is a resource, not a required loop.

The Executive should request attention only when:

- authority requires it;
- risk justifies it;
- ambiguity is material;
- strategic input is valuable;
- recovery has exceeded autonomous limits.

The goal is not zero human interaction.

The goal is **high-value human interaction**.

## 30. Security Boundary

The Executive must never bypass:

- authentication;
- authorization;
- tool policy;
- business isolation;
- audit requirements;
- safety controls.

A more capable reasoning process does not grant more authority.

## 31. Invariants

The following Executive rules are intended to be locked after owner approval:

1. Executive is the operational proxy of the owner.
2. Owner remains root authority.
3. Executive coordinates rather than performing every specialized task.
4. Executive uses the Objective Engine as objective source of truth.
5. Executive uses Attention to prioritize signals.
6. Executive uses Decision Engine for structured decision evaluation.
7. Executive uses Planner/Orchestrator for execution.
8. Executive delegates specialized work to appropriate agents.
9. Executive preserves objective lineage.
10. Executive can coordinate multiple businesses concurrently.
11. Business context is isolated by default.
12. Executive cannot expand its own authority.
13. Executive can autonomously decide to escalate.
14. Executive does not require an active UI session.
15. Consequential decisions are observable.
16. Executive supports recovery after process/runtime failure.
17. Executive should prefer minimal intervention.
18. Executive must remain aligned with the owner's objectives and policies.

## 32. Acceptance Criteria

The Executive implementation should eventually demonstrate:

### A. Delegation

Given a business objective, the Executive creates or routes a mission to an appropriate specialist instead of executing the specialist work itself.

### B. Objective lineage

Given a task, the system can trace it back to its relevant division/business/owner objective.

### C. Autonomous continuation

A mission continues while the UI is closed.

### D. Multi-business concurrency

Business A can execute a mission while the user views Business B.

### E. Authority enforcement

The Executive cannot cause a tool action that exceeds delegated authority.

### F. Escalation

The Executive can identify an action requiring owner input and produce a structured escalation.

### G. Recovery

After an Executive/runtime restart, active missions can resume or enter a safe recoverable state.

### H. Observable decisions

A consequential Executive decision has structured audit metadata.

### I. Objective drift

The system can detect an outcome diverging from the intended objective and trigger replanning or escalation.

### J. No universal-agent behavior

Specialized work is demonstrably delegated to specialist agents.

## 33. Open Design Questions

These should be decided before implementation of the Executive runtime:

- exact Executive state machine;
- whether there is one Executive process or multiple cooperating Executive workers;
- exact decision object schema;
- objective priority algorithm;
- conflict resolution scoring;
- resource allocation algorithm;
- how much contextual memory is injected per decision;
- model selection policy for Executive reasoning;
- deterministic vs LLM decision boundaries;
- maximum autonomous planning horizon;
- escalation thresholds;
- checkpoint frequency;
- concurrency/locking strategy;
- exact event types consumed by Executive;
- exact events emitted by Executive.
