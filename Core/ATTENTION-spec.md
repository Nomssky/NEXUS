> **HISTORICAL / NON-CANONICAL — retained for reference only.**
> Replaced by [Attention & Priority Intelligence](NEXUS-ATTENTION-PRIORITY-INTELLIGENCE.md); authority remains with [Governance, Policy & Safety Control](NEXUS-GOVERNANCE-POLICY-SAFETY-CONTROL.md).
> The canonical modules govern; this document grants no competing authority. Its computational-focus framing is historical: canonical Attention prioritizes awareness, especially human attention, and review routing, not execution or strategic decision authority. All retained examples, taxonomies, schemas, draft interfaces, lifecycle sketches, acceptance/test sketches, and open questions are nonbinding candidates for the next CONTRACTS layer, not approved contracts or a mandate for new modules. Historical approval/lock wording below is not current status.

# NEXUS Attention

**Historical status:** PROPOSED → awaiting owner lock

## 1. Definition

NEXUS Attention is the cognitive prioritization subsystem responsible for deciding which signals deserve computational focus, monitoring, delegation, escalation, or Executive cognition.

Its fundamental question is:

> **"Out of everything happening in and around NEXUS, what deserves attention right now?"**

Attention is not event storage.

```text
EVENT ≠ ATTENTION
```

An event is something that happened.

Attention is a decision about the significance of that event or signal.

## 2. Purpose

A continuously autonomous NEXUS may receive:

- system events;
- business events;
- tool results;
- agent reports;
- objective changes;
- metric changes;
- failures;
- external changes;
- scheduled triggers;
- user input.

Processing every event with expensive reasoning would create noise, latency, and unnecessary resource consumption.

Attention filters and prioritizes those signals.

## 3. Core Model

```text
RAW EVENTS
    |
    v
NORMALIZATION
    |
    v
SIGNAL EXTRACTION
    |
    v
RELEVANCE
    |
    v
ATTENTION SCORING
    |
    +--> IGNORE
    +--> AGGREGATE
    +--> MONITOR
    +--> DELEGATE
    +--> EXECUTIVE REVIEW
    +--> INTERRUPT / ESCALATE
```

## 4. Responsibilities

Attention is responsible for:

1. receiving relevant signals;
2. normalizing signals;
3. determining relevance;
4. evaluating urgency;
5. evaluating strategic importance;
6. detecting novelty;
7. considering objective alignment;
8. considering risk;
9. considering deadlines;
10. aggregating repetitive signals;
11. suppressing low-value noise;
12. maintaining attention queues;
13. allocating cognitive priority;
14. determining when Executive cognition is justified;
15. determining when an issue should be delegated;
16. triggering escalation when thresholds are met;
17. maintaining persistent watches;
18. expiring stale attention items.

## 5. Non-Responsibilities

Attention should not:

- execute business tasks;
- directly perform specialized work;
- redefine objectives;
- make final strategic decisions;
- bypass authorization;
- replace the Executive;
- replace the Event Bus;
- replace Observability;
- become a general-purpose notification system.

Attention decides **what deserves cognition**, not **what the final business decision should be**.

## 6. Event vs Signal vs Attention Item

These concepts must remain distinct.

### Event

A recorded occurrence.

```text
social.post.published
```

### Signal

A normalized interpretation relevant to system reasoning.

```text
"Social engagement dropped significantly."
```

### Attention Item

A prioritized cognitive concern.

```text
"Investigate possible content-performance degradation."
priority: HIGH
```

Conceptually:

```text
Event
  ->
Signal
  ->
Attention Item
```

## 7. Attention Sources

Potential sources include:

```text
OWNER
EXECUTIVE
OBJECTIVE_ENGINE
AGENT
MISSION
TASK
TOOL
EXTERNAL_SYSTEM
SCHEDULER
SYSTEM_RUNTIME
OBSERVABILITY
EVALUATION
MEMORY
SECURITY
```

Not every source has equal priority.

## 8. Relevance

Relevance asks:

> "Does this signal matter to anything NEXUS currently cares about?"

Relevant context may include:

- active objectives;
- active missions;
- business;
- division;
- policies;
- current incidents;
- owner instructions;
- monitored conditions.

A signal unrelated to any active concern may be safely ignored or archived.

## 9. Objective Alignment

Objective alignment is a primary attention dimension.

A signal affecting a high-priority active objective should generally receive more attention than an unrelated signal.

Example:

```text
Business Objective:
Increase qualified leads.

Signal A:
Qualified leads decreased 25%.

Signal B:
Internal dashboard theme changed.

Signal A:
HIGH attention.

Signal B:
LOW / IGNORE.
```

## 10. Urgency

Urgency measures how quickly attention is required.

Examples:

```text
Immediate:
security incident.

High:
objective deadline in 2 hours.

Medium:
performance degradation.

Low:
interesting market observation.
```

Urgency is not equivalent to importance.

## 11. Importance

Importance measures potential consequence.

A signal may be:

```text
high importance + low urgency
```

Example:

> A strategic market shift expected to matter next quarter.

It should remain visible without constantly interrupting current execution.

## 12. Risk

Risk represents potential negative consequence.

Risk dimensions may include:

- financial;
- operational;
- security;
- privacy;
- reputation;
- objective failure;
- policy violation;
- data integrity.

High-risk signals may bypass normal attention suppression.

## 13. Novelty

Novelty measures how different a signal is from established patterns.

Examples:

```text
Normal:
daily traffic fluctuates ±5%.

Novel:
traffic suddenly falls 60%.
```

Novelty should increase investigation priority when the change is meaningful.

Novelty alone should not create panic.

## 14. Deadline Pressure

Attention should consider time remaining.

Conceptually:

```text
deadline pressure =
function(time remaining, objective importance, required work)
```

A deadline approaching for a low-value objective may still rank below a critical incident.

## 15. Anomaly

Attention should support anomaly signals.

Examples:

- unusual metric change;
- unexpected agent behavior;
- repeated tool failure;
- sudden cost increase;
- abnormal latency;
- unexpected external response.

Anomaly detection itself may be performed by specialized systems; Attention consumes the resulting signal.

## 16. Attention Score

Attention may use a composite score.

Conceptually:

```text
AttentionScore =
    Relevance
  + Importance
  + Urgency
  + Risk
  + ObjectiveAlignment
  + Novelty
  + DeadlinePressure
  + Anomaly
  - Suppression
  - Redundancy
  - Staleness
```

This is a conceptual model, not a mandated mathematical formula.

The implementation may use deterministic scoring, learned scoring, model-assisted classification, or a hybrid.

## 17. Hard Overrides

Certain signals should bypass normal scoring.

Examples:

```text
critical security event
critical authorization violation
catastrophic system failure
explicit owner interruption
```

Hard overrides must be governed by policy.

The Attention system must not invent arbitrary emergency categories.

## 18. Attention Classes

A useful conceptual classification:

```text
IGNORE
BACKGROUND
MONITOR
DELEGATE
EXECUTIVE_REVIEW
URGENT_INTERRUPT
CRITICAL_ESCALATION
```

### IGNORE

No current cognitive value.

### BACKGROUND

Keep available but do not actively process.

### MONITOR

Maintain a watch for change.

### DELEGATE

A specialist can investigate without Executive cognition.

### EXECUTIVE_REVIEW

Executive should evaluate the issue.

### URGENT_INTERRUPT

Interrupt current lower-priority cognition.

### CRITICAL_ESCALATION

Require immediate governed response.

## 19. Attention Queue

Attention items should be maintained in priority-aware queues.

Conceptually:

```text
CRITICAL
HIGH
MEDIUM
LOW
BACKGROUND
```

Within a priority, ordering may use:

- age;
- deadline;
- objective impact;
- confidence;
- risk;
- dependency.

## 20. Attention Budget

Attention is a finite cognitive resource.

The system should maintain an attention budget across:

- Executive reasoning;
- model calls;
- active investigations;
- concurrent monitoring;
- expensive analysis.

The objective is not:

> process everything.

The objective is:

> spend cognition where it has the highest expected value.

## 21. Cognitive Cost

Different attention items may have different costs.

Examples:

```text
simple status check
    = low cognitive cost

multi-source investigation
    = high cognitive cost
```

Attention should consider expected value relative to cognitive/resource cost.

## 22. Focus

Attention may designate a current focus.

```text
Executive Focus
    |
    +--> primary issue
    +--> supporting context
    +--> blocked issues
    +--> pending interruptions
```

Focus does not permanently exclude other objectives.

## 23. Focus Switching

The system may switch focus when:

- higher-priority signals arrive;
- current work becomes blocked;
- a deadline becomes critical;
- a critical incident occurs;
- current work reaches a natural checkpoint.

Frequent low-value switching should be suppressed.

## 24. Interruption Policy

Interruptions should be expensive.

Before interrupting Executive cognition, Attention should consider:

```text
priority
risk
urgency
objective impact
current focus
switching cost
```

A low-value notification should not interrupt a high-value mission.

## 25. Attention Aggregation

Repeated events should be aggregated.

Instead of:

```text
500 API errors
```

the Executive should receive:

```text
API error rate increased significantly
over the last 10 minutes.
```

Aggregation reduces cognitive noise.

## 26. Deduplication

Identical or semantically equivalent signals should be deduplicated when safe.

Deduplication must preserve important information such as:

- count;
- first occurrence;
- latest occurrence;
- duration;
- severity;
- affected scope.

## 27. Suppression

Some signals should be temporarily suppressed.

Examples:

- known maintenance window;
- acknowledged incident;
- repeatedly observed non-actionable condition;
- expected scheduled behavior.

Suppression must be scoped and time-bounded where appropriate.

## 28. Suppression Safety

Suppression must not hide:

- newly increased severity;
- changed scope;
- new evidence;
- policy violations;
- critical security signals;
- owner messages.

A previously suppressed signal can regain attention when conditions change.

## 29. Cooldown

After an attention item is handled, a cooldown may prevent immediate re-triggering from identical noise.

Example:

```text
Metric alert handled.
Same alert repeats within 30 seconds.
Do not interrupt again.
```

Cooldown should be reset when meaningful state changes occur.

## 30. Persistent Watches

Attention should support persistent monitoring.

Example:

```text
Watch:
Notify if qualified traffic drops >20%
within a 24-hour window.
```

A watch is a standing condition, not a single event.

## 31. Watch Lifecycle

Conceptually:

```text
CREATED
  ->
ACTIVE
  ->
TRIGGERED
  ->
ACKNOWLEDGED
  ->
COOLDOWN / ACTIVE
  ->
EXPIRED / CANCELLED
```

Exact lifecycle belongs to implementation design.

## 32. Attention Ownership

An attention item may be owned by:

```text
EXECUTIVE
DIVISION
AGENT
MISSION
SYSTEM
OWNER
```

Ownership determines who is expected to respond.

Not every attention item belongs to the Executive.

## 33. Delegation

Attention can route a signal to a specialist.

Example:

```text
Signal:
Social engagement dropped.

Attention:
DELEGATE

Recipient:
Media Analytics Agent
```

The specialist reports findings back through normal system channels.

## 34. Executive Review

Attention should request Executive cognition when the issue requires:

- cross-objective reasoning;
- strategic prioritization;
- cross-division coordination;
- authority decisions;
- conflict resolution;
- significant replanning;
- escalation.

## 35. Escalation

Attention can escalate when thresholds are met.

However:

```text
Attention:
"this deserves urgent attention."

Executive:
"this is what NEXUS should do about it."
```

The distinction must remain clear.

## 36. Owner Attention

Owner messages should receive special handling.

Examples:

- direct instruction;
- question;
- correction;
- approval;
- strategic change;
- emergency interruption.

The Owner is not just another event source.

Owner authority must be respected by Governance.

## 37. Contextual Attention

The same signal may have different importance depending on context.

Example:

```text
Business A:
traffic -10% = normal.

Business B:
traffic -10% = critical because campaign launch
is currently dependent on that traffic.
```

Attention must therefore evaluate signals against active context.

## 38. Multi-Business Attention

One NEXUS installation may contain multiple businesses.

Attention must maintain business scope.

```text
Business A signal
    ≠
Business B signal
```

unless explicit cross-business relevance exists.

The Executive may still receive a cross-business summary when appropriate.

## 39. Cross-Division Attention

A signal may belong to multiple divisions.

Example:

```text
Research signal
    ->
Media impact
    ->
Business strategy impact
```

Attention may create coordinated attention items while preserving ownership and scope.

## 40. Temporal Decay

Attention should decay when a signal becomes stale.

For example:

```text
High attention yesterday
    ->
Low attention today
```

unless the underlying condition remains active.

Staleness should not erase historical evidence.

## 41. Relevance Decay

A signal may become irrelevant when:

- objective is achieved;
- mission is cancelled;
- business context changes;
- issue is resolved;
- watch expires.

Attention should automatically reduce or close such items.

## 42. Attention and Memory

Memory provides historical context.

Attention asks:

> "Does this matter now?"

Memory can answer:

> "Has this happened before?"

Example:

```text
Current anomaly
    +
Historical pattern
    ->
Attention confidence
```

Memory must inform attention without causing stale information to dominate current evidence.

## 43. Attention and Objective Engine

Objective Engine provides:

- active objectives;
- priority;
- health;
- deadlines;
- dependencies;
- constraints.

Attention uses these to determine significance.

## 44. Attention and Executive

Attention determines when Executive cognition is warranted.

```text
Attention:
"Executive should inspect this."

Executive:
"What should NEXUS do?"
```

## 45. Attention and Agent System

Attention may route lower-level issues directly to agents.

This prevents the Executive from becoming a bottleneck.

```text
Signal
  ->
Attention
  ->
Specialist Agent
  ->
Result
  ->
Attention
  ->
Executive only if needed
```

## 46. Attention and Event System

The Event System records events.

Attention consumes events/signals and produces prioritized cognitive work.

```text
Event Bus
    |
    v
Attention
    |
    v
Attention Queue
```

Attention should not replace durable event history.

## 47. Attention and Scheduler

Scheduler creates time-based triggers.

Attention determines whether the resulting signal deserves action.

```text
Scheduler:
"It's 09:00."

Attention:
"Is the scheduled task relevant and worth processing now?"
```

## 48. Attention and Evaluation

Evaluation may create signals such as:

```text
objective drift
poor outcome
strategy failure
unexpected improvement
```

Attention determines their urgency and destination.

## 49. Attention and Governance

Governance defines:

- hard overrides;
- prohibited suppression;
- escalation rules;
- owner priority;
- authority boundaries.

Attention must operate inside these rules.

## 50. Attention Lifecycle

Conceptual lifecycle:

```text
SIGNAL_CREATED
      |
      v
CLASSIFIED
      |
      v
SCORED
      |
      +--> SUPPRESSED
      |
      +--> AGGREGATED
      |
      +--> MONITORED
      |
      +--> DELEGATED
      |
      +--> EXECUTIVE_REVIEW
      |
      +--> INTERRUPTED
      |
      v
HANDLED
      |
      +--> RESOLVED
      +--> COOLDOWN
      +--> REOPENED
      +--> ESCALATED
      +--> EXPIRED
```

## 51. Attention State

Conceptual states:

```text
NEW
QUEUED
FOCUSED
DELEGATED
MONITORED
ACKNOWLEDGED
SUPPRESSED
COOLDOWN
RESOLVED
ESCALATED
EXPIRED
```

Exact state semantics will be defined during implementation.

## 52. Attention Trace

Important attention decisions should be observable.

Conceptual trace:

```text
attention_id
signal_id
scope
classification
score / priority
relevance
urgency
risk
objective_alignment
decision
destination
suppression_reason
timestamp
outcome
```

This is an operational audit record, not hidden model reasoning.

## 53. Attention Quality

Attention quality should be evaluated.

Important metrics include:

```text
false positive rate
false negative rate
interruptions per hour
duplicate rate
stale attention rate
time-to-detection
time-to-routing
attention cost
objective-impact coverage
```

A system that notices everything is not necessarily a good attention system.

## 54. False Positives

False positive:

> Attention interrupts cognition for an issue that was not worth the interruption.

Too many false positives create alert fatigue.

## 55. False Negatives

False negative:

> A consequential signal was ignored or under-prioritized.

False negatives are potentially more dangerous than false positives for critical signals.

Governance should define acceptable thresholds by signal class.

## 56. Learning

Attention behavior may improve over time.

Potential learning signals:

- owner dismissals;
- owner escalations;
- repeated false positives;
- missed incidents;
- successful routing;
- objective outcomes.

Learning must not silently change critical attention policy.

Governed configuration or learned ranking may be separated.

## 57. Owner Feedback

Owner feedback can provide explicit attention supervision.

Examples:

```text
"Don't bother me with these."
"Always tell me when this happens."
"This is important."
"Ignore this class of alert."
```

Such feedback should be classified as preference vs policy according to authority semantics.

## 58. Resource Awareness

Attention should consider available resources.

If compute is constrained:

```text
High-value cognition
    >
low-value analysis
```

The system may postpone expensive investigation for lower-priority issues.

## 59. Attention Starvation

High-priority recurring signals must not permanently starve lower-priority but necessary work.

The system should support fairness mechanisms such as:

- aging;
- bounded queues;
- reserved capacity;
- periodic background processing.

## 60. Attention Storms

An attention storm occurs when one event generates many downstream signals.

Example:

```text
API outage
  ->
500 failed tasks
  ->
500 alerts
```

Attention should collapse correlated events into a coherent incident signal.

## 61. Causal Grouping

Related signals should be grouped when they likely share a cause.

Example:

```text
API latency ↑
API errors ↑
Agent failures ↑
```

could become:

```text
Potential API incident affecting multiple missions.
```

Causal grouping should preserve underlying evidence.

## 62. Attention Priority and Objective Priority

Objective priority influences attention but does not fully determine it.

A lower-priority objective can still produce a critical system signal.

Conversely, a high-priority objective may not require constant attention when healthy.

## 63. Attention Must Not Create Objectives

Attention may identify:

> "This deserves investigation."

It should not automatically create:

> "This is now a strategic objective."

Objective creation remains governed by Objective Engine + authorized decision flow.

## 64. Attention Must Not Become a Hidden Decision Maker

Attention may decide:

```text
who should look
when they should look
how urgently they should look
```

It should not silently decide:

```text
what strategic outcome the business should pursue
```

That belongs to Executive/Decision/Governance.

## 65. Security

Attention signals may contain sensitive information.

Access should follow scope:

```text
NEXUS
  -> Business
      -> Division
          -> Mission
              -> Agent
```

Sensitive signals must not be broadcast globally without authorization.

## 66. Failure Handling

If Attention fails:

- durable events must not be lost;
- pending signals should be recoverable;
- duplicate processing should be controlled;
- critical watches should be restored;
- queues should recover;
- Executive should be able to detect degraded Attention service.

Attention is a prioritization layer, not the sole source of truth for events.

## 67. Restart Recovery

After restart, Attention should reconstruct:

- active watches;
- unresolved attention items;
- cooldowns where necessary;
- suppression state;
- queue priorities;
- ownership;
- relevant scope.

The system should avoid replaying every historical event as a fresh interruption.

## 68. Invariants

The following Attention rules are intended to be locked after owner approval:

1. Event and Attention are different concepts.
2. Attention prioritizes cognitive work; it does not replace execution.
3. Attention does not redefine objectives.
4. Attention does not replace Executive decision-making.
5. Objective relevance is a primary attention dimension.
6. Attention is a finite resource.
7. Not every event deserves cognition.
8. Repeated signals should be aggregated when safe.
9. Suppression must be scoped and governed.
10. Critical signals may bypass normal scoring under policy.
11. Business context is isolated by default.
12. Attention can delegate issues directly to specialists.
13. Attention can request Executive review.
14. Attention can trigger escalation according to policy.
15. Attention operates independently of the UI.
16. Attention must survive runtime restart.
17. Durable event history must not depend solely on Attention.
18. Attention quality must be measurable.
19. Attention must balance false positives and false negatives.
20. Attention should minimize unnecessary context switching.
21. Attention must not become a hidden strategic decision-maker.
22. Attention decisions should be observable.
23. Stale attention should decay without destroying historical evidence.
24. Owner authority takes precedence within governed attention rules.

## 69. Acceptance Criteria

The eventual implementation should demonstrate:

### A. Noise Filtering

Thousands of low-value events do not produce thousands of Executive interruptions.

### B. Priority

A critical event affecting a high-value objective outranks ordinary background events.

### C. Aggregation

Hundreds of correlated failures become a manageable incident signal.

### D. Delegation

A specialist can receive an attention item without requiring Executive intervention.

### E. Escalation

A governed critical signal can interrupt normal processing.

### F. Suppression

Known non-actionable signals can be suppressed without hiding meaningful changes.

### G. Persistent Watch

NEXUS can monitor a condition continuously and trigger attention when its threshold is crossed.

### H. Multi-Business Isolation

A signal from Business A does not automatically expose Business A context to Business B.

### I. Objective Context

Attention ranking changes appropriately when objective priority/context changes.

### J. Restart Recovery

Active watches and unresolved attention items survive runtime restart.

### K. Attention Budget

The system can limit expensive cognitive processing under resource pressure.

### L. Attention Storm Handling

A cascading failure does not create an unbounded interruption storm.

## 70. Open Design Questions

Before implementation:

- exact attention score model;
- deterministic vs learned scoring;
- signal normalization schema;
- attention item schema;
- priority queue architecture;
- aggregation/correlation strategy;
- causal grouping strategy;
- suppression policy;
- cooldown semantics;
- watch schema and evaluator;
- attention budget algorithm;
- starvation prevention;
- interruption thresholds;
- hard override taxonomy;
- owner attention policy;
- model usage boundaries;
- persistence strategy;
- event-to-signal transformation;
- attention quality evaluation;
- learning boundaries.
