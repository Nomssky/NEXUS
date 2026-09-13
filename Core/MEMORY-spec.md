# NEXUS Memory

**Status:** PROPOSED → awaiting owner lock

## 1. Definition

NEXUS Memory is the persistent cognitive context subsystem responsible for retaining, retrieving, updating, validating, relating, and decaying information that may improve future decisions and autonomous behavior.

Memory is not simply a conversation history and not simply a vector database.

Its fundamental question is:

> **"What does NEXUS need to remember so that future cognition is more informed without becoming trapped by stale or incorrect information?"**

## 2. Core Principle

Memory must preserve useful context while distinguishing certainty.

```text
EXPERIENCE
   ↓
OBSERVATION
   ↓
MEMORY
   ↓
RETRIEVAL
   ↓
CONTEXT
   ↓
DECISION
   ↓
OUTCOME
   ↓
LEARNING
   ↺
```

The memory system must support both:

- continuity across time;
- controlled forgetting and correction.

## 3. Memory Is Not Truth

A memory is not automatically a fact.

NEXUS should distinguish at minimum:

```text
FACT
OBSERVATION
INFERENCE
ASSUMPTION
PREFERENCE
DECISION
PLAN
EXPERIENCE
LESSON
EPISODIC RECORD
REFERENCE
```

Each memory should carry provenance and confidence appropriate to its type.

## 4. Responsibilities

Memory is responsible for:

1. storing durable cognitive information;
2. retrieving relevant information;
3. preserving provenance;
4. associating memories with scope;
5. associating memories with objectives and decisions;
6. tracking confidence;
7. tracking freshness;
8. supporting correction;
9. supporting supersession;
10. supporting contradiction detection;
11. supporting memory consolidation;
12. supporting controlled decay;
13. supporting archival;
14. providing context to authorized cognition;
15. preserving important historical experiences;
16. distinguishing current state from historical state.

## 5. Non-Responsibilities

Memory should not:

- make strategic decisions;
- define objectives;
- execute actions;
- authorize tools;
- silently override current evidence;
- replace the Objective Engine;
- replace Attention;
- replace the Event Store;
- treat every conversation message as permanent memory;
- assume retrieval relevance means truth.

## 6. Memory Categories

A useful conceptual taxonomy:

```text
SEMANTIC
EPISODIC
PROCEDURAL
PREFERENCE
FACTUAL
EXPERIENTIAL
DECISION
OBJECTIVE_CONTEXT
ENTITY
RELATIONSHIP
REFERENCE
```

### Semantic

General knowledge or stable concepts.

### Episodic

Specific past events or experiences.

### Procedural

Learned ways of performing recurring work.

### Preference

Owner or business preferences.

### Factual

Claims believed to represent facts.

### Experiential

Observed outcomes from NEXUS actions.

### Decision

Past decisions and their rationale/context.

### Objective Context

Information relevant to an objective.

### Entity

Information about businesses, products, agents, platforms, etc.

### Relationship

Connections among entities, objectives, memories, or events.

### Reference

Pointers to authoritative external/internal sources.

## 7. Memory Scope

Memory must be scoped.

Possible scopes:

```text
GLOBAL_NEXUS
BUSINESS
DIVISION
MISSION
OBJECTIVE
AGENT
PROJECT
ENTITY
OWNER
SESSION
```

The default should be the narrowest useful scope.

## 8. Multi-Business Isolation

One NEXUS installation may manage multiple businesses.

Memory must not accidentally leak context:

```text
Business A
   ≠
Business B
```

unless a memory is explicitly global or cross-business.

Example:

```text
Business A:
"Supplier X gives 10% discount."

This must not become context for Business B
unless explicitly applicable.
```

## 9. Memory Visibility

A memory may be:

```text
PRIVATE
SCOPED
SHARED
GLOBAL
```

Visibility must be governed by authority and scope.

Agents should receive only the memory required for their mission.

## 10. Memory Provenance

Every consequential memory should retain provenance.

Conceptual fields:

```text
memory_id
source_type
source_id
created_at
observed_at
scope
author
confidence
status
version
```

Sources may include:

```text
OWNER
AGENT
TOOL
EXTERNAL_SOURCE
EVENT
DECISION
EVALUATION
OBJECTIVE
SYSTEM
```

## 11. Temporal Semantics

NEXUS must distinguish:

```text
created_at
observed_at
valid_from
valid_until
updated_at
```

This matters because:

> "When NEXUS learned something" is not necessarily "when it became true."

## 12. Current State vs Historical State

Memory should preserve historical truth without confusing it with current truth.

Example:

```text
2026-01:
Supplier price = X

2026-08:
Supplier price = Y
```

The old memory remains historically valid but should not be retrieved as the current price without temporal context.

## 13. Confidence

Memories should have confidence where uncertainty exists.

Possible representation:

```text
HIGH
MEDIUM
LOW
UNKNOWN
```

Confidence should be evidence-based.

Model confidence alone is not sufficient evidence.

## 14. Source Reliability

Confidence should be separable from source reliability.

Example:

```text
Memory confidence:
medium

Source reliability:
high
```

A reliable source can still provide incomplete information.

## 15. Evidence

A memory may reference supporting evidence.

Evidence can include:

- tool output;
- document;
- owner statement;
- external source;
- observed metric;
- execution result;
- decision record.

Evidence should be traceable where practical.

## 16. Memory Status

Conceptual states:

```text
ACTIVE
STALE
DISPUTED
SUPERSEDED
INVALIDATED
ARCHIVED
DELETED
```

Historical records should generally be invalidated/superseded rather than silently overwritten.

## 17. Memory Creation

Not every piece of information deserves persistent storage.

Memory formation should consider:

```text
future utility
durability
specificity
confidence
scope
cost
privacy
repetition
strategic relevance
```

Examples of likely persistent memory:

```text
Owner preference
Business policy
Successful recurring procedure
Important decision
Stable entity information
Repeated lesson
```

Examples of likely transient context:

```text
casual conversation
temporary intermediate reasoning
one-off low-value status
```

## 18. Memory Importance

Memory importance may depend on:

- future decision impact;
- recurrence;
- strategic relevance;
- owner importance;
- objective relevance;
- historical value;
- confidence.

High importance should not override incorrectness.

## 19. Memory Retrieval

Retrieval should combine multiple dimensions.

Conceptually:

```text
QUERY
  |
  +--> semantic relevance
  +--> scope
  +--> recency
  +--> importance
  +--> objective relevance
  +--> entity relevance
  +--> confidence
  +--> temporal validity
  +--> source reliability
  |
  v
RANKED MEMORIES
```

Pure vector similarity is insufficient for a cognitive memory system.

## 20. Retrieval Context

Retrieved memories should include enough metadata for the consuming system to interpret them.

Example:

```text
Memory:
"Weekend posts performed better."

Context:
Business = A
Observed = 2026-07
Confidence = medium
Source = analytics
Scope = Media Division
```

## 21. Retrieval Must Be Scope-Aware

An agent should not retrieve globally similar memories that belong to another business unless authorized.

Scope filtering should happen before or alongside semantic ranking.

## 22. Retrieval Must Be Time-Aware

For current-state questions, recent valid memories should generally outrank expired historical memories.

For historical questions, historical memories may be preferred.

Query intent determines temporal relevance.

## 23. Retrieval Must Be Confidence-Aware

A low-confidence memory should not silently dominate a high-confidence current observation.

Confidence is one ranking dimension, not a guarantee of truth.

## 24. Contradiction

Memory contradictions may occur.

Example:

```text
Memory A:
Posting at 20:00 performs best.

Memory B:
Posting at 12:00 performs best.
```

NEXUS should represent the conflict.

It should not silently choose one merely because it was retrieved first.

## 25. Contradiction Resolution

Potential resolution sources:

1. newer evidence;
2. stronger source;
3. larger sample;
4. controlled experiment;
5. owner correction;
6. objective-specific context.

If unresolved:

```text
Status = DISPUTED
```

and uncertainty should remain visible.

## 26. Memory Supersession

New information may supersede old information.

Example:

```text
Old:
Brand voice = formal.

New owner instruction:
Brand voice = conversational.
```

The old memory should remain historically traceable while the new memory becomes current.

## 27. Owner Correction

Owner correction has high authority within permitted scope.

Example:

> "That information is wrong. Remember the new rule."

The memory system should:

1. preserve the correction;
2. identify affected memories;
3. invalidate or supersede incorrect memories where appropriate;
4. preserve audit history.

## 28. Memory Consolidation

Repeated observations may be consolidated.

Example:

```text
Observation 1:
Weekend engagement +15%.

Observation 2:
Weekend engagement +18%.

Observation 3:
Weekend engagement +17%.
```

may become:

```text
Learned pattern:
Weekend content tends to outperform weekday content.
Confidence: medium.
```

Consolidation must preserve source evidence.

## 29. Memory Abstraction

NEXUS may derive higher-level lessons from multiple experiences.

```text
Experiences
   ↓
Pattern
   ↓
Lesson
```

Derived memories must remain distinguishable from direct observations.

## 30. Memory and Learning

Learning should create or update memory only when evidence supports it.

A failed action can produce:

```text
Experience:
Strategy X produced poor outcome.

Lesson:
Do not repeat X under similar conditions.
```

But the lesson should not become an absolute rule unless evidence warrants it.

## 31. Negative Memory

NEXUS should remember failures.

Examples:

- tool integration repeatedly failed;
- strategy underperformed;
- specific approach violated constraints;
- an assumption was disproven.

Negative memories are valuable for preventing repeated mistakes.

## 32. Memory Decay

Not all memories remain equally useful forever.

Decay may depend on:

```text
age
usage
confidence
volatility
importance
source type
domain
```

High-volatility information should decay faster.

Stable principles should decay slowly.

## 33. Forgetting

Forgetting may mean:

```text
deprioritize
archive
remove from active retrieval
invalidate
delete
```

These are distinct operations.

The system should prefer reversible forgetting when historical value remains.

## 34. Memory Refresh

Some memories require refresh.

Example:

```text
Current social platform API rules.
```

A memory may have:

```text
refresh_required = true
```

when it is likely to become stale.

## 35. Memory Validation

Before relying on volatile or high-impact memories, NEXUS may validate them against current evidence.

Example:

```text
Stored:
API supports feature X.

Before execution:
verify current API documentation.
```

Memory should accelerate cognition, not replace verification.

## 36. Memory and Current Evidence

Current evidence should generally outrank stale memory when they conflict.

```text
Memory:
"Platform allows feature X."

Current tool result:
"Feature X is unavailable."

Current evidence wins.
```

The contradiction should still be recorded for learning.

## 37. Memory and Objectives

Objectives provide authoritative strategic context.

Memory can inform how to achieve an objective.

Memory cannot silently change the objective.

Example:

```text
Objective:
Increase qualified leads.

Memory:
Previous short-form videos generated high-quality traffic.

Memory informs planning.
Objective remains unchanged.
```

## 38. Memory and Attention

Attention asks:

> "Does this matter now?"

Memory asks:

> "What do we know from before?"

A signal may retrieve historical memory to determine whether it is normal or anomalous.

## 39. Memory and Executive

Executive should receive relevant memories as context, not an undifferentiated memory dump.

Example:

```text
Current decision
   +
Relevant past decisions
   +
Relevant outcomes
   +
Relevant lessons
```

## 40. Memory and Agents

Agents should receive task-relevant memory.

Example:

```text
Media Agent
  -> content preferences
  -> brand rules
  -> previous campaign outcomes
  -> platform-specific lessons
```

It should not receive unrelated financial or personal context.

## 41. Memory and Decision Engine

Decision Engine may query:

```text
past decisions
past outcomes
known constraints
learned lessons
relevant patterns
```

Memory should provide evidence/context, not choose the decision.

## 42. Memory and Evaluation

Evaluation produces evidence that can update memory.

```text
Action
  ↓
Result
  ↓
Evaluation
  ↓
Lesson / Experience
  ↓
Memory
```

## 43. Memory and Objective Lineage

Important memories should optionally link to:

```text
objective
mission
decision
action
result
```

This allows NEXUS to answer:

> "What did we learn from pursuing this objective?"

## 44. Memory and Business Context

Each business can maintain its own memory space.

Example:

```text
NEXUS
├── Business A
│   ├── Memory
│   └── Divisions
└── Business B
    ├── Memory
    └── Divisions
```

Global memory should be intentionally limited.

## 45. Memory and Sessions

A user-facing session is not necessarily a memory boundary.

A conversation may create durable memory.

Conversely, a long conversation may contain no durable memory.

Session context and persistent memory are distinct.

## 46. Working Memory

NEXUS should have short-lived working context for active cognition.

Conceptually:

```text
Working Memory
    =
current task
current mission
recent observations
retrieved memories
current objective context
```

Working memory can expire after the active cognitive process.

## 47. Long-Term Memory

Long-term memory stores durable information.

Examples:

- owner preferences;
- business policies;
- recurring lessons;
- historical decisions;
- stable entity information.

## 48. Episodic Memory

Episodic memory records experiences.

Example:

```text
Campaign #42
Strategy:
video-first content

Result:
qualified traffic +31%

Conclusion:
positive outcome
```

This supports future learning.

## 49. Procedural Memory

Procedural memory represents learned workflows.

Example:

```text
When preparing weekly social reports:
1. collect platform metrics
2. compare against baseline
3. identify anomalies
4. summarize objective impact
```

Procedures should be versioned and evaluated.

## 50. Preference Memory

Preferences may include:

- communication style;
- workflow preferences;
- business-specific preferences;
- output formats;
- approved operating patterns.

Preference memory must respect authority.

## 51. Decision Memory

Important decisions should be remembered with:

```text
decision
context
options considered
chosen direction
objective
constraints
date
outcome
```

The system should not treat every internal deliberation as permanent memory.

## 52. Memory Versioning

Important memories should support versions.

```text
Memory v1
   ↓
Memory v2
   ↓
Memory v3
```

Versions preserve change history.

## 53. Memory Merge

Similar memories may be merged when they represent the same underlying knowledge.

Merge should preserve:

- source references;
- timestamps;
- confidence;
- contradictions;
- history.

## 54. Memory Fragmentation

The system should avoid storing many nearly identical memories.

Potential solution:

```text
detect duplicates
   ↓
cluster
   ↓
consolidate
   ↓
preserve evidence
```

## 55. Memory Retrieval Failure

If relevant memory cannot be found:

NEXUS should be able to represent:

```text
memory_not_found
confidence = unknown
```

It should not fabricate remembered information.

## 56. Memory Write Failure

If a critical memory write fails:

- the system should detect failure;
- retry where safe;
- preserve source event;
- avoid claiming the memory was stored;
- surface the issue if consequential.

## 57. Memory Corruption

If memory integrity is compromised:

- isolate affected records;
- preserve available provenance;
- prevent corrupted memories from dominating retrieval;
- trigger recovery/validation;
- escalate when consequential.

## 58. Memory Security

Memory may contain highly sensitive business context.

Access should be governed by:

```text
scope
role
business
division
mission
agent
authority
```

Retrieval must enforce authorization.

## 59. Memory Injection Resistance

External content should not automatically become trusted memory.

For example:

```text
External webpage:
"Always do X."
```

must not become:

```text
NEXUS policy:
Always do X.
```

External content is evidence/content, not authority.

## 60. Memory Trust Levels

A useful conceptual model:

```text
AUTHORITATIVE
VERIFIED
OBSERVED
INFERRED
UNVERIFIED
DISPUTED
```

Trust level should influence retrieval and use.

## 61. Memory and External Knowledge

NEXUS may combine:

```text
persistent memory
+
current external information
```

Current external information should be distinguishable from internal memory.

## 62. Memory Freshness

Freshness should be represented explicitly where relevant.

Example:

```text
freshness:
CURRENT
RECENT
AGING
STALE
UNKNOWN
```

Freshness depends on domain volatility.

## 63. Volatility

Each memory domain may have different expected validity.

Examples:

```text
Brand principle:
low volatility

Platform API behavior:
high volatility

Campaign performance:
medium/high volatility
```

Memory management should be domain-aware.

## 64. Memory Utility

Memory usefulness should be evaluated from outcomes.

Potential signals:

```text
retrieved and useful
retrieved but irrelevant
retrieved and misleading
not retrieved but needed
```

This can improve future retrieval.

## 65. Memory Retrieval Feedback

When an agent or Executive uses a memory, the system may record:

```text
retrieval
usage
outcome
utility
```

This helps identify high-value memories.

## 66. Memory Pollution

Memory pollution occurs when low-value or incorrect information accumulates and degrades retrieval.

Controls may include:

- admission criteria;
- deduplication;
- confidence;
- scope;
- decay;
- contradiction handling;
- utility feedback.

## 67. Memory Explosion

Autonomous 24/7 operation can generate enormous experience data.

NEXUS should separate:

```text
raw event history
episodic records
consolidated memories
active context
```

Not every event becomes long-term memory.

## 68. Memory Architecture

Conceptually:

```text
                 MEMORY SYSTEM
                      |
       +--------------+--------------+
       |              |              |
 Working Memory   Long-Term       Episodic
       |          Memory          Memory
       |              |              |
       +--------------+--------------+
                      |
                Retrieval Layer
                      |
             Consolidation Layer
                      |
               Validation Layer
                      |
                Storage Layer
```

Implementation may use multiple storage technologies.

## 69. Storage Independence

The architecture should not require one specific database.

Potential implementations may include:

- relational storage;
- document storage;
- vector indexes;
- graph storage;
- object storage;
- event logs.

The semantic memory model must remain independent of the storage technology.

## 70. Hybrid Retrieval

A robust memory system should support hybrid retrieval.

Conceptually:

```text
keyword
+
semantic
+
graph relationship
+
metadata filtering
+
temporal filtering
+
scope filtering
+
confidence ranking
```

## 71. Graph Relationships

Memory relationships may include:

```text
DERIVED_FROM
SUPPORTS
CONTRADICTS
SUPERSEDES
RELATED_TO
ABOUT
CAUSED_BY
RESULTED_IN
APPLIES_TO
```

Graph semantics are useful for reasoning over history.

## 72. Memory Lineage

Derived memories must retain lineage.

Example:

```text
3 campaign observations
      ↓
learned pattern
      ↓
procedure
```

NEXUS should be able to trace the pattern back to evidence.

## 73. Memory Auditability

Important memory changes should be auditable.

Examples:

```text
memory.created
memory.updated
memory.superseded
memory.invalidated
memory.archived
memory.restored
memory.corrected
memory.consolidated
```

## 74. Memory and Autonomous Operation

In 24/7 autonomous mode, memory prevents NEXUS from repeatedly rediscovering the same information.

Example:

```text
Agent learns:
API rate limit = X.

Future missions:
retrieve memory first
    ↓
avoid unnecessary rediscovery
```

But volatile information should still be verified when necessary.

## 75. Memory and Recovery

After restart:

- durable memories remain;
- active working memory may be reconstructed;
- unresolved episodic records remain;
- memory indexes recover;
- no false claim of remembered context should be made if storage failed.

## 76. Memory Retention

Retention should be policy-driven.

Different classes may have different retention:

```text
strategic decisions:
long-term

operational lessons:
long-term/medium-term

temporary task context:
short-term

raw noisy events:
event retention policy
```

## 77. Memory Deletion

Deletion should be exceptional and governed.

When historical value matters, prefer:

```text
invalidate
supersede
archive
```

over destructive deletion.

## 78. Memory and Privacy

Memory must minimize unnecessary retention.

Information should be stored only when there is legitimate system utility and authorized scope.

Sensitive information should have stricter access and retention controls.

## 79. Memory and Owner

The Owner may explicitly establish durable memory.

Examples:

```text
"Remember this preference."
"From now on, use this brand voice."
"Do not repeat this strategy."
```

Owner-authorized memory should receive appropriate authority metadata.

## 80. Memory and Objective Context

When an objective changes, associated memories should not automatically be rewritten.

Instead:

```text
Objective changed
    ↓
old memories remain historical
    ↓
retrieval ranking/context changes
```

This preserves historical learning.

## 81. Memory and Attention

Attention may use memory to determine:

```text
Is this unusual?
Has this happened before?
Was this previously resolved?
Did we already investigate this?
```

This reduces duplicate cognitive work.

## 82. Memory and Planning

Planner may query memory for:

- prior plans;
- successful strategies;
- failed strategies;
- recurring constraints;
- operational procedures.

Memory should inform planning, not guarantee success.

## 83. Memory and Decision Quality

Memory quality should be evaluated by downstream outcomes.

Useful metrics:

```text
retrieval precision
retrieval recall
memory usefulness
stale-memory usage
contradiction rate
duplicate rate
memory pollution rate
correction rate
decision improvement
```

## 84. Memory Quality Loop

```text
STORE
  ↓
RETRIEVE
  ↓
USE
  ↓
OUTCOME
  ↓
EVALUATE
  ↓
UPDATE MEMORY QUALITY
  ↺
```

## 85. Memory Failure Modes

Important failure modes:

```text
false memory
stale memory
wrong scope
memory leakage
over-retention
under-retention
duplicate memory
contradictory memory
retrieval failure
retrieval overload
memory pollution
memory injection
incorrect consolidation
```

Each should be observable.

## 86. False Memory

NEXUS must never claim certainty about information it does not possess.

If uncertain:

```text
"I don't have reliable memory of this."
```

is preferable to fabrication.

## 87. Retrieval Overload

Returning too many memories is itself a failure.

The system should provide a compact, relevant context set rather than dumping the entire memory store.

## 88. Memory Compression

Long histories may be compressed into summaries or learned patterns.

Compression must preserve:

- important exceptions;
- uncertainty;
- provenance;
- temporal boundaries;
- contradictions.

A compressed summary must not erase important nuance.

## 89. Memory Rehydration

When a compressed memory becomes important, NEXUS should be able to retrieve supporting lower-level evidence where available.

```text
Summary
  ↓
Supporting memories
  ↓
Original evidence
```

## 90. Memory Confidence Updating

Confidence may change when new evidence arrives.

Example:

```text
Initial:
medium confidence

Repeated successful observations:
high confidence
```

Contradictory evidence should reduce confidence or mark the memory disputed.

## 91. Memory Authority

Authority and confidence are distinct.

Example:

```text
Owner preference:
high authority

But:
it may still be outdated if owner explicitly changes it.
```

The system should not equate "authoritative source" with "permanently true."

## 92. Memory Conflict With Owner

If stored memory conflicts with a current explicit Owner instruction:

```text
Current Owner instruction
    >
old preference memory
```

The old memory should be superseded or scoped appropriately.

## 93. Memory Conflict With Policy

Policy is authoritative within its domain.

Memory cannot override policy.

## 94. Memory Conflict With Objective

Memory cannot silently override an active objective.

If memory suggests a strategy inconsistent with the objective:

```text
objective remains authoritative
memory becomes strategy evidence
```

## 95. Memory Lifecycle

Conceptual lifecycle:

```text
CAPTURE
  ↓
VALIDATE
  ↓
STORE
  ↓
RETRIEVE
  ↓
USE
  ↓
EVALUATE
  ↓
REFRESH / UPDATE / CONSOLIDATE
  ↓
STALE / SUPERSEDED / INVALIDATED
  ↓
ARCHIVE
```

## 96. Invariants

The following Memory rules are intended to be locked after owner approval:

1. Memory is not automatically truth.
2. Memory is distinct from event history.
3. Memory is distinct from working/session context.
4. Every consequential memory should have provenance.
5. Memory is scope-aware.
6. Cross-business memory leakage is prohibited by default.
7. Current evidence can supersede stale memory.
8. Owner corrections have appropriate authority.
9. Historical memory should remain traceable where required.
10. Contradictions must not be silently erased.
11. Derived memories retain lineage.
12. Memory confidence is distinct from source authority.
13. External content is not automatically trusted memory.
14. Not every event becomes persistent memory.
15. Memory retrieval must be relevance- and scope-aware.
16. Memory retrieval must be time-aware where necessary.
17. NEXUS must not fabricate missing memories.
18. Memory can decay without necessarily being deleted.
19. Important memories should support versioning/history.
20. Memory does not redefine objectives.
21. Memory does not make strategic decisions.
22. Memory does not authorize actions.
23. Sensitive memory access is governed.
24. Memory quality should be measurable.
25. Autonomous operation requires durable memory but not infinite retention.
26. Memory should preserve lessons from failures.
27. Working memory and long-term memory remain conceptually distinct.
28. Compression must preserve important uncertainty and provenance.

## 97. Acceptance Criteria

The eventual implementation should demonstrate:

### A. Scope Isolation

Business A memories cannot be retrieved by Business B without explicit applicability.

### B. Provenance

A consequential memory can be traced to its source.

### C. Temporal Awareness

A historical fact does not incorrectly override a current fact.

### D. Contradiction

Conflicting memories can be represented and surfaced.

### E. Owner Correction

An explicit owner correction supersedes conflicting preference/context appropriately.

### F. Learning

Repeated experiences can produce a derived lesson with traceable evidence.

### G. Failure Learning

A failed strategy can be remembered and considered in future planning.

### H. Retrieval

Relevant memory can be retrieved using more than simple text similarity.

### I. Decay

Stale information becomes less prominent without destroying necessary history.

### J. Security

Unauthorized agents cannot retrieve scoped memories.

### K. Restart

Durable memory survives runtime restart.

### L. No Fabrication

Missing memory produces uncertainty rather than invented recall.

### M. Compression

Long experience histories can be consolidated without losing critical evidence.

## 98. Open Design Questions

Before implementation:

- exact memory schema;
- memory type taxonomy;
- storage architecture;
- vector index strategy;
- graph strategy;
- relational/document strategy;
- hybrid retrieval ranking;
- memory admission policy;
- confidence model;
- source reliability model;
- temporal validity model;
- contradiction detection;
- contradiction resolution;
- consolidation algorithm;
- memory compression;
- rehydration strategy;
- decay algorithm;
- refresh policy;
- retention policy;
- privacy classification;
- access-control integration;
- memory utility measurement;
- embedding/model strategy;
- external knowledge boundary;
- owner correction workflow;
- event-to-memory pipeline;
- memory quality evaluation.
