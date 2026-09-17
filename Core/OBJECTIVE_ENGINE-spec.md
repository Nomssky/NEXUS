> **HISTORICAL / NON-CANONICAL — retained for reference only.**
> Replaced by [Objective Engine](NEXUS-OBJECTIVE-ENGINE.md); its canonical requirements govern objective truth and lifecycle.
> The canonical flow is Executive → Objective → Decision → Planner → Workflow; models have no execution authority. Retained examples, taxonomies, schemas, API/interface drafts, acceptance sketches, and open questions are nonbinding candidates for the next CONTRACTS layer, not approved contracts or a mandate for new modules. Historical approval/lock wording below is not current status. The legacy body is preserved; missing architectural requirements are consolidated in the canonical owner.

# NEXUS Objective Engine

**Historical status:** PROPOSED → awaiting owner lock

## 1. Definition

The Objective Engine is the authoritative subsystem responsible for representing, preserving, relating, prioritizing, tracking, validating, and evolving objectives throughout NEXUS.

It is the system's durable answer to:

> **What are we trying to accomplish, why does it matter, and how do we know whether we are still moving toward it?**

The Objective Engine is **not a task manager**.

A task describes work to perform. An objective describes an intended outcome and its purpose.

## 2. Core Principle

NEXUS must not lose the "why".

```text
OWNER INTENT
    |
    v
OBJECTIVE
    |
    +--> desired outcome
    +--> reason / purpose
    +--> constraints
    +--> success criteria
    +--> priority
    |
    v
MISSION
    |
    v
TASK
    |
    v
ACTION
    |
    v
RESULT
    |
    v
EVALUATION
    |
    +--> objective progress
    +--> learning
    +--> replanning
```

Every consequential mission should have a traceable objective relationship.

## 3. Responsibilities

The Objective Engine is responsible for:

1. creating structured objectives from authorized intent;
2. storing objective definitions;
3. preserving objective hierarchy;
4. preserving objective lineage;
5. tracking objective state;
6. tracking progress;
7. managing priorities;
8. recording constraints;
9. recording success criteria and metrics;
10. representing dependencies;
11. detecting objective conflicts;
12. supporting objective revision;
13. maintaining objective history;
14. validating completion;
15. identifying abandoned or stale objectives;
16. providing objective context to other NEXUS components;
17. supporting objective-aware planning and evaluation.

## 4. Non-Responsibilities

The Objective Engine should not:

- execute tasks;
- directly call external tools;
- choose models;
- replace the Decision Engine;
- replace the Planner;
- replace the Orchestrator;
- autonomously invent strategic owner intent without an authorized source;
- declare success solely because a task finished.

It owns objective truth, not execution.

## 5. Objective vs Task

### Objective

Describes an intended state/outcome.

Example:

> Increase qualified traffic to the business website.

### Mission

A coordinated effort to achieve an objective or objective-derived outcome.

Example:

> Run a four-week social content experiment.

### Task

A discrete unit of work.

Example:

> Research the top five competitor content formats.

### Action

A concrete execution step.

Example:

> Call the social analytics API.

The hierarchy is:

```text
Objective
  -> Mission
      -> Task
          -> Action
```

Not every objective needs a mission immediately.

## 6. Objective Hierarchy

The default hierarchy is:

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

NEXUS should permit additional objective relationships where necessary, but the semantic distinction must remain clear.

## 7. Owner Objective

An Owner Objective originates from the owner's authorized intent.

Examples:

- grow a business;
- reduce operational workload;
- improve customer retention;
- build a content system;
- research a market.

Owner objectives may contain strategic ambiguity.

The Executive and Objective Engine may structure them without changing their fundamental intent.

## 8. Business Objective

A Business Objective operationalizes an owner objective for a specific real-world business.

Example:

```text
Owner:
Build a sustainable online business.

Business:
Clothing Store.

Business Objective:
Increase profitable online sales.
```

Business context must be explicit.

## 9. Division Objective

A Division Objective scopes a business objective to a division.

Example:

```text
Business Objective:
Increase profitable online sales.

Media Division Objective:
Increase qualified organic traffic from social media.
```

A division objective cannot contradict a higher-level objective or policy.

## 10. Objective Identity

Each objective requires a stable unique identity.

Conceptual fields:

```text
objective_id
business_id
parent_objective_id
type
title
purpose
desired_outcome
status
priority
constraints
success_criteria
metrics
created_at
updated_at
created_by
source
version
```

The exact schema is an implementation concern, but stable identity and lineage are architectural requirements.

## 11. Objective Purpose

An objective should distinguish:

```text
WHAT:
desired outcome

WHY:
purpose / reason

SUCCESS:
how achievement is recognized
```

Example:

```text
WHAT:
Increase qualified social traffic.

WHY:
Create a reliable acquisition channel.

SUCCESS:
Qualified traffic and downstream conversion improve
without exceeding the defined acquisition constraints.
```

## 12. Objective Context

Objective context may include:

- business context;
- market context;
- strategic assumptions;
- relevant owner preferences;
- constraints;
- known risks;
- historical performance;
- related objectives.

Context should be scoped to the objective.

NEXUS should avoid injecting unrelated global memory into every objective decision.

## 13. Constraints

Constraints define what must not be violated or what conditions must hold.

Examples:

```text
Budget <= defined limit
Do not use prohibited channels
Maintain brand rules
Do not expose private customer information
Use approved account
```

Constraints are not suggestions.

The Governance/Authority system remains the final authority for permission.

## 14. Success Criteria

Every actionable objective should have success criteria whenever practical.

Success criteria may be:

- quantitative;
- qualitative;
- deterministic;
- outcome-based;
- threshold-based;
- comparative;
- time-bound.

Example:

```text
Metric:
Qualified leads

Target:
+20%

Period:
30 days
```

An objective should not be considered complete merely because all planned tasks completed.

## 15. Metrics

Metrics provide measurable evidence.

A metric may contain:

```text
metric_id
name
definition
source
unit
baseline
target
measurement_window
freshness
confidence
```

Metrics should distinguish between:

```text
activity metric
output metric
outcome metric
```

Example:

```text
Activity:
10 posts published.

Output:
Average engagement increased.

Outcome:
Qualified leads increased.
```

Outcome metrics should generally carry greater objective significance than activity metrics.

## 16. Proxy Metrics

NEXUS must explicitly recognize proxy metrics.

Example:

```text
Objective:
Increase qualified leads.

Proxy:
Increase impressions.
```

Optimizing the proxy while damaging the actual objective is objective drift.

The Objective Engine should preserve the distinction between objective outcomes and proxies.

## 17. Priority

Objectives require priority information.

Priority may depend on:

- owner-defined priority;
- strategic importance;
- urgency;
- dependency;
- risk;
- expected value;
- deadline;
- resource requirements;
- business impact.

Priority is not the same as urgency.

A highly urgent low-value objective should not automatically outrank a strategically critical objective.

## 18. Priority Inheritance

Child objectives inherit relevant strategic importance from parent objectives but may have their own operational priority.

The system must preserve both:

```text
local priority
strategic importance
```

A lower-level objective cannot silently override a higher-level constraint.

## 19. Objective Dependencies

Objectives may depend on other objectives.

Example:

```text
Market validation
      |
      v
Product positioning
      |
      v
Content strategy
```

Dependencies should be explicit.

Blocked dependencies should be visible to Attention and Executive systems.

## 20. Objective Relationships

Beyond parent/child relationships, objectives may be:

```text
SUPPORTS
DEPENDS_ON
CONFLICTS_WITH
SUPERSEDES
DERIVED_FROM
RELATED_TO
```

Relationships must have clear semantics.

## 21. Objective Conflict

Conflicts may occur when:

```text
Objective A:
maximize growth

Objective B:
minimize spending
```

The Objective Engine should represent the conflict rather than silently resolving it.

Resolution belongs to Executive/Decision/Governance according to authority and policy.

## 22. Objective Lineage

Lineage is mandatory for consequential work.

Example:

```text
Owner Objective #1
    ↓
Business Objective #7
    ↓
Media Objective #12
    ↓
Mission #84
    ↓
Task #912
    ↓
Action #1842
```

NEXUS should be able to answer:

> "Why is this action happening?"

by traversing the lineage.

## 23. Objective State

Conceptual states:

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

Exact transitions must be defined in implementation design.

## 24. State Semantics

### DRAFT
Defined but not activated.

### ACTIVE
Currently pursued.

### PAUSED
Intentionally stopped without being abandoned.

### BLOCKED
Cannot progress because a dependency or required condition is unavailable.

### AT_RISK
Still progressing but evidence indicates likely failure or deadline miss.

### ACHIEVED
Success criteria have been sufficiently verified.

### FAILED
The objective's defined success conditions cannot be achieved within its valid scope/time.

### ABANDONED
Explicitly discontinued.

### SUPERSEDED
Replaced by a newer objective.

### EXPIRED
Validity window ended without successful completion.

### ARCHIVED
Retained for historical/reference purposes and no longer operational.

## 25. Completion

Completion requires outcome evaluation.

```text
Tasks complete
    ≠
Objective achieved
```

The system should evaluate evidence against success criteria.

Example:

```text
Mission:
Publish 30 posts.

Result:
30 posts published.

Objective:
Increase qualified leads.

Conclusion:
Unknown until outcome data is evaluated.
```

## 26. Progress

Objective progress should be represented independently from task completion.

Possible signals:

- metric progress;
- milestone completion;
- evidence quality;
- outcome confidence;
- time remaining;
- dependency status.

Progress may be unknown.

NEXUS must not fabricate progress when evidence is missing.

## 27. Confidence

Objective status may include confidence.

Examples:

```text
high confidence
medium confidence
low confidence
unknown
```

Confidence should reflect evidence quality, not model self-assurance.

## 28. Objective Revision

Objectives can change.

Revision may happen when:

- owner changes strategy;
- market conditions change;
- assumptions become false;
- a higher-level objective changes;
- evidence invalidates the original plan;
- the objective becomes impossible;
- a better objective supersedes it.

Revisions should preserve history.

Do not silently mutate historical truth.

Conceptually:

```text
Objective v1
    |
    v
Objective v2
    |
    v
Objective v3
```

## 29. Immutable History

Important objective events should be append-only where practical.

Examples:

```text
created
activated
priority_changed
constraint_added
constraint_changed
paused
resumed
blocked
unblocked
revised
achieved
failed
abandoned
superseded
```

Current state is a projection of authoritative history.

## 30. Objective Source

Every objective should record its source.

Possible sources:

```text
OWNER
EXECUTIVE
BUSINESS_POLICY
DIVISION_POLICY
SYSTEM_MAINTENANCE
RECOVERY
DERIVED
```

Derived objectives must retain their parent/source lineage.

NEXUS should never confuse a generated sub-objective with direct owner intent.

## 31. Derived Objectives

The system may derive lower-level objectives when needed.

Example:

```text
Owner Objective:
Grow online revenue.

Derived Business Objective:
Increase profitable acquisition.

Derived Media Objective:
Improve qualified social acquisition.
```

Derived objectives must:

1. have a parent;
2. preserve lineage;
3. remain consistent with parent intent;
4. respect policy;
5. be revisable;
6. not silently become owner-level objectives.

## 32. Temporary Objectives

Some objectives may be temporary.

Examples:

- incident recovery;
- time-limited campaign;
- short-term experiment;
- investigation.

Temporary objectives should have explicit scope and expiry where applicable.

They should not pollute long-term strategic memory.

## 33. Objective Expiration

Objectives may have validity windows.

When an objective expires, NEXUS should evaluate whether it should:

- expire;
- renew;
- revise;
- supersede;
- escalate.

Automatic renewal must be policy-controlled.

## 34. Objective Activation

Creating an objective does not necessarily mean activating it.

Activation should require appropriate authority.

Example:

```text
Draft
  |
  | authorization
  v
Active
```

The exact authorization mechanism belongs to Governance.

## 35. Objective Cancellation

Cancellation should preserve history.

Cancelling an objective should not delete related historical missions or decisions.

Dependent active missions should be evaluated and safely stopped, redirected, or allowed to finish according to policy.

## 36. Objective Context for Agents

Agents should receive the minimum relevant objective context required for autonomous work.

Example:

```text
Objective:
Increase qualified social traffic.

Purpose:
Build reliable customer acquisition.

Relevant constraints:
Brand policy, budget, privacy.

Success:
Qualified traffic +20%.

Agent should NOT automatically receive:
unrelated financial,
personal,
or other-business context.
```

This supports both effectiveness and least-privilege context.

## 37. Objective-Aware Planning

The Planner should be able to query:

```text
What objective is this plan serving?
What constraints apply?
What success criteria apply?
What dependencies exist?
What evidence is required?
```

Plans without objective linkage should be treated carefully.

## 38. Objective-Aware Evaluation

Evaluation should compare:

```text
Intent
  vs
Plan
  vs
Execution
  vs
Outcome
```

This enables detection of:

- objective drift;
- proxy optimization;
- ineffective strategies;
- unintended side effects;
- incomplete evidence.

## 39. Objective Drift

Objective drift occurs when execution gradually diverges from the intended outcome.

Signals may include:

- proxy metric optimization;
- increasing activity without outcome improvement;
- constraint erosion;
- changing assumptions;
- mission proliferation without progress;
- local optimization damaging strategic goals.

When detected:

```text
Detect
  ->
Evaluate
  ->
Notify Attention
  ->
Executive decision
  ->
Replan / revise / escalate
```

## 40. Objective Health

Objective health may summarize:

```text
ON_TRACK
AT_RISK
BLOCKED
OFF_TRACK
UNKNOWN
```

Health is not the same as state.

For example:

```text
State = ACTIVE
Health = AT_RISK
```

## 41. Objective Attention Signals

The Objective Engine should emit signals when important objective conditions change.

Examples:

```text
objective.created
objective.activated
objective.priority_changed
objective.blocked
objective.at_risk
objective.progress_changed
objective.metric_changed
objective.conflict_detected
objective.drift_detected
objective.expiring
objective.achieved
objective.failed
objective.superseded
```

Exact event schema belongs to Event System specification.

## 42. Objective Query Interface

Other components should be able to query:

```text
get(objective_id)
get_children(objective_id)
get_parent(objective_id)
get_lineage(objective_id)
get_active_objectives(scope)
get_blocked_objectives(scope)
get_at_risk_objectives(scope)
get_dependencies(objective_id)
get_conflicts(objective_id)
get_success_criteria(objective_id)
get_metrics(objective_id)
get_current_health(objective_id)
```

These are conceptual interfaces, not final API signatures.

## 43. Authorization

Objective mutation must be authorized.

Examples:

```text
Owner:
can create/modify strategic objectives.

Executive:
can propose/derive operational objectives within authority.

Division:
can manage authorized division objectives.

Agent:
normally cannot redefine its parent objective.
```

The exact permission matrix belongs to Governance.

## 44. Objective Integrity

The Objective Engine must prevent:

- orphaned consequential missions;
- impossible lineage;
- circular parent relationships;
- unauthorized strategic mutation;
- child objective authority escalation;
- deletion that destroys audit history;
- success declaration without valid evidence;
- conflicting constraints being silently accepted.

## 45. Concurrency

Multiple agents may update progress simultaneously.

The Objective Engine should support concurrency-safe updates.

Examples:

```text
Agent A reports metric update.
Agent B reports milestone completion.
Executive changes priority.
```

These events must not silently overwrite one another.

Event ordering, versioning, or transactional mechanisms should preserve consistency.

## 46. Failure Recovery

If the Objective Engine restarts:

- objective definitions must survive;
- current states must be recoverable;
- history must remain intact;
- lineage must remain intact;
- pending updates must be recoverable or safely rejected;
- no objective should silently disappear.

## 47. Objective Deletion

Hard deletion should be exceptional.

Historical objectives should generally be archived or superseded rather than deleted.

Deletion must not break audit lineage.

## 48. Privacy and Scope

Objective context may contain sensitive business information.

Access should be scoped by:

```text
NEXUS
  -> Business
      -> Division
          -> Agent
```

An agent should receive only the objective context necessary for its authorized mission.

## 49. Objective and Memory

Objectives are authoritative operational context.

Memory may contain observations and learned information about objectives.

Memory must not silently rewrite objective truth.

Example:

```text
Memory:
"Previous campaigns performed better on weekends."

Objective:
"Increase qualified traffic."

Memory informs planning.
It does not change the objective.
```

## 50. Objective and Attention

Objective state determines significance.

Attention can use:

- strategic importance;
- urgency;
- health;
- risk;
- deadline;
- dependency;
- recent changes.

Objective Engine remains the source of objective truth; Attention decides what deserves immediate cognitive resources.

## 51. Objective and Executive

Executive uses objectives to decide what deserves action.

Objective Engine provides:

```text
What
Why
Priority
Constraints
Success
State
Lineage
```

Executive decides:

```text
What should NEXUS do next?
```

## 52. Objective and Decision Engine

Decision Engine evaluates options against objectives.

```text
Objective Engine:
defines desired outcome and constraints.

Decision Engine:
evaluates possible actions against them.
```

## 53. Objective and Planner

Planner converts objective-aligned decisions into plans.

```text
Objective
  ->
Decision
  ->
Plan
```

## 54. Objective and Evaluation

Evaluation determines whether observed outcomes satisfy the objective.

```text
Execution
  ->
Evidence
  ->
Evaluation
  ->
Objective progress/state
```

## 55. Objective Lifecycle

Conceptual lifecycle:

```text
DRAFT
  |
  v
ACTIVE
  |
  +--> PAUSED --> ACTIVE
  |
  +--> BLOCKED --> ACTIVE
  |
  +--> AT_RISK --> ACTIVE
  |
  +--> ACHIEVED
  |
  +--> FAILED
  |
  +--> ABANDONED
  |
  +--> SUPERSEDED
  |
  +--> EXPIRED
        |
        +--> RENEWED / REVISED
```

ARCHIVED is a retention state rather than an operational outcome.

## 56. Invariants

The following Objective Engine rules are intended to be locked after owner approval:

1. Objective Engine is the source of truth for objectives.
2. Objectives are not equivalent to tasks.
3. Consequential work preserves objective lineage.
4. Objectives preserve both "what" and "why".
5. Owner objectives remain distinguishable from derived objectives.
6. Lower-level objectives cannot expand higher-level authority.
7. Objective success requires outcome evidence where practical.
8. Task completion does not equal objective achievement.
9. Objective history is preserved.
10. Important revisions are traceable.
11. Objective conflicts are represented rather than silently erased.
12. Objective context is scoped.
13. Objective state is recoverable.
14. Objective progress may be unknown.
15. Confidence reflects evidence quality.
16. Proxy metrics must remain distinguishable from actual outcomes.
17. Objective mutation is authorized.
18. Objective deletion must not destroy required lineage/audit history.
19. Objectives can exist independently of immediate execution.
20. Derived objectives retain parent lineage.
21. Objective Engine does not execute external work.
22. Objective Engine does not replace Executive, Decision Engine, Planner, or Orchestrator.

## 57. Acceptance Criteria

The eventual implementation should demonstrate:

### A. Why Traceability

Given any consequential task, NEXUS can identify the relevant objective and explain the objective lineage.

### B. Multi-Level Context

A task in a division can trace:

```text
Task
-> Mission
-> Division Objective
-> Business Objective
-> Owner Objective
```

### C. Outcome-Based Completion

Completing all tasks does not automatically mark the objective achieved.

### D. Revision History

Changing an objective preserves previous versions/history.

### E. Conflict Representation

Conflicting objectives can coexist without silent deletion.

### F. Scoped Context

An agent receives only authorized objective context relevant to its mission.

### G. Drift Detection

The system can represent and signal divergence between objective outcomes and proxy activity.

### H. Recovery

After restart, objective state and lineage remain intact.

### I. Concurrency

Concurrent updates do not silently overwrite authoritative state.

### J. Derived Objectives

NEXUS can create a lower-level objective with explicit parent lineage without treating it as direct owner intent.

## 58. Open Design Questions

Before implementation:

- exact objective schema;
- objective ID strategy;
- event-sourced vs state-plus-history persistence;
- priority calculation;
- metric model;
- progress calculation;
- confidence model;
- objective conflict resolution interface;
- exact authorization matrix;
- revision/version semantics;
- objective dependency graph implementation;
- derived-objective generation rules;
- objective drift detection mechanism;
- evidence model;
- completion evaluator;
- expiration behavior;
- objective context serialization;
- retention policy.
