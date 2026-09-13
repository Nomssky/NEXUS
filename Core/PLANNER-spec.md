# NEXUS Planner

**Status:** PROPOSED → awaiting owner lock

## 1. Definition

The NEXUS Planner is the subsystem that transforms an approved decision or objective-driven requirement into an executable, observable, verifiable set of missions and tasks.

Its fundamental question is:

> **"What work must happen, in what order, under what constraints, by whom, and how do we know it succeeded?"**

Planner is not Executive, Decision Engine, Agent Runtime, or Tool Execution.

## 2. Core Flow

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

## 3. Responsibilities

Planner is responsible for:

1. translating approved decisions into executable plans;
2. decomposing missions into tasks;
3. identifying dependencies;
4. selecting execution order;
5. identifying parallelizable work;
6. assigning task requirements;
7. defining success criteria;
8. defining verification;
9. estimating resources;
10. estimating duration;
11. defining retry/fallback behavior;
12. monitoring plan validity;
13. replanning when assumptions materially change;
14. coordinating multi-agent work;
15. maintaining plan lineage;
16. reporting plan state.

## 4. Non-Responsibilities

Planner must not:

- redefine strategic objectives;
- silently change approved decisions;
- bypass authorization;
- directly become every worker agent;
- fabricate task completion;
- treat planning as proof of execution;
- ignore Governance;
- silently expand scope.

## 5. Plan

A plan is a structured representation of how an intended outcome will be achieved.

Conceptual fields:

```text
plan_id
business_id
scope
objective_refs
decision_refs
mission_refs
desired_outcome
constraints
assumptions
tasks
dependencies
resources
deadlines
verification_strategy
risk
fallbacks
status
created_at
updated_at
```

## 6. Plan Scope

Every plan must have explicit scope.

Possible scopes:

```text
BUSINESS
DIVISION
MISSION
OBJECTIVE
PROJECT
OPERATION
TASK_GROUP
```

Cross-business plans require explicit authorization.

## 7. Plan Preconditions

Before planning:

- objective or decision is known;
- desired outcome is defined;
- authority exists;
- constraints are available;
- relevant context is available;
- required resources are known or estimable.

If critical information is missing, Planner should request clarification or create an explicit uncertainty.

## 8. Plan Types

Possible plan classes:

```text
STRATEGIC
TACTICAL
OPERATIONAL
ROUTINE
EXPERIMENT
EMERGENCY
RECOVERY
```

Different classes may require different planning depth.

## 9. Mission

A mission is a meaningful unit of work that contributes to an objective or approved decision.

A mission should answer:

```text
WHAT outcome are we trying to produce?
WHY does it matter?
WHAT scope applies?
HOW will success be verified?
```

## 10. Task

A task is an executable unit of work.

A task should have:

```text
task_id
mission_id
description
purpose
inputs
expected_output
requirements
constraints
dependencies
assigned_agent
deadline
priority
verification
retry_policy
status
```

## 11. Task Granularity

Tasks should be neither:

```text
too broad
```

nor:

```text
unnecessarily microscopic
```

A good task has a clear owner, output, boundary, and verification method.

## 12. Task Decomposition

Planner may decompose:

```text
MISSION
   ↓
TASK A
TASK B
TASK C
```

until each task is practical for an authorized agent/runtime.

## 13. Decomposition Rule

Decompose further when:

- multiple independent outcomes exist;
- different skills are required;
- different agents should own components;
- verification differs;
- failure isolation is useful;
- parallel execution is beneficial.

Avoid decomposition when coordination overhead exceeds the benefit.

## 14. Dependency Graph

Tasks form a directed graph:

```text
A ──→ C
B ──→ C
C ──→ D
```

C cannot begin until required predecessors satisfy their dependency conditions.

## 15. Dependency Types

Possible dependency types:

```text
DATA
OUTPUT
AUTHORIZATION
RESOURCE
TEMPORAL
ENVIRONMENT
DECISION
```

## 16. Dependency Conditions

A dependency should specify what must be true.

Example:

```text
Task C depends on:
Task A.status == VERIFIED
```

Completion alone is not always enough.

## 17. Parallelism

Independent tasks should be parallelized when:

- resources permit;
- parallel execution does not introduce unacceptable risk;
- outputs do not conflict.

Example:

```text
        ┌→ Research A ─┐
Mission ├→ Research B ─┼→ Synthesis
        └→ Data C ─────┘
```

## 18. Sequential Work

Sequential execution is required when:

- later work depends on earlier output;
- ordering affects correctness;
- authorization is sequential;
- shared resources create conflict.

## 19. Critical Path

Planner should identify the critical path for time-sensitive plans.

```text
A → C → D
```

may determine minimum completion time.

## 20. Resource Planning

Resources may include:

```text
agents
models
tokens
API calls
tools
compute
budget
human attention
time
external services
```

## 21. Resource Budget

Each plan may have budgets.

Examples:

```text
max_model_calls
max_research_calls
max_cost
max_runtime
max_parallel_agents
```

Planner should avoid plans that exceed approved budgets.

## 22. Agent Requirements

Planner should specify what a task needs rather than blindly naming an agent.

Examples:

```text
skill: copywriting
tool: web
model_class: reasoning
permission: social_media_draft
```

This allows the runtime to select the appropriate agent.

## 23. Agent Assignment

Assignment may consider:

- skills;
- capabilities;
- permissions;
- current load;
- reliability;
- model suitability;
- business scope;
- division scope;
- cost;
- availability.

## 24. Agent Specialization

NEXUS should prefer specialized agents when specialization materially improves quality.

Example:

```text
Research Agent
Copy Agent
Design Agent
Analytics Agent
QA Agent
```

## 25. Agent Independence

Planner should not assume all agents use the same model.

Task requirements may route to:

```text
local Ollama model
Hugging Face model
OpenRouter model
specialized model
deterministic service
human
```

## 26. Task Context

Each task receives only the context necessary for execution.

Context may include:

```text
mission
task purpose
relevant memory
inputs
constraints
output contract
verification criteria
```

Avoid indiscriminate context injection.

## 27. Context Provenance

Important task inputs should retain provenance.

Agents should know whether information is:

```text
owner instruction
objective
decision
memory
research
tool output
agent inference
assumption
```

## 28. Task Output Contract

Every meaningful task should define its expected output.

Examples:

```text
draft
dataset
research report
image
decision input
API result
analysis
approval request
```

## 29. Verification Contract

Every meaningful task should define how success is checked.

Example:

```text
Expected:
10 verified sources.

Verification:
source count >= 10
and each source passes authority criteria.
```

## 30. Verification Independence

When practical, verification should be performed by a separate mechanism or agent from the one that produced the result.

This reduces self-confirmation failure.

## 31. Task Status

Possible states:

```text
DRAFT
READY
BLOCKED
QUEUED
ASSIGNED
RUNNING
WAITING
VERIFYING
VERIFIED
FAILED
RETRYING
CANCELLED
SKIPPED
STALE
```

## 32. Mission Status

Possible states:

```text
PLANNING
READY
RUNNING
BLOCKED
PAUSED
VERIFYING
COMPLETED
FAILED
CANCELLED
REPLANNING
```

## 33. Plan Status

Possible states:

```text
DRAFT
APPROVED
READY
RUNNING
PAUSED
REPLANNING
COMPLETED
FAILED
CANCELLED
EXPIRED
```

## 34. Task Priority

Priority should derive from:

- objective importance;
- mission criticality;
- deadline;
- dependency impact;
- risk;
- opportunity cost.

Priority should not simply equal creation time.

## 35. Deadlines

Tasks may have:

```text
start_after
deadline
hard_deadline
soft_deadline
```

Planner should distinguish hard from preferred timing.

## 36. Scheduling

Scheduling should account for:

```text
dependencies
priority
resources
deadlines
agent availability
cost
risk
```

## 37. Backpressure

If execution capacity is limited, Planner should avoid creating unlimited runnable work.

It should maintain bounded queues.

## 38. Queue Management

Queues may be scoped by:

```text
business
division
priority
mission
agent capability
```

## 39. Concurrency Control

Tasks touching shared state require concurrency policies.

Possible policies:

```text
ALLOW
SERIALIZE
LOCK
MERGE
REJECT
```

## 40. Shared Resource Conflicts

Example:

```text
Task A edits campaign budget.
Task B edits campaign budget.
```

Planner/runtime must detect the conflict.

## 41. Idempotency

Tasks should be designed to be safely retried when possible.

Example:

```text
create_report()
```

should avoid accidentally creating duplicate reports on retry.

## 42. Retry Policy

Retry should specify:

```text
max_attempts
backoff
retryable_errors
non_retryable_errors
fallback
escalation
```

## 43. Retry Boundaries

Do not endlessly retry:

- invalid inputs;
- permission failures;
- policy violations;
- semantic impossibility.

Retry transient infrastructure failures where appropriate.

## 44. Failure Classification

Failures should be categorized:

```text
AGENT_FAILURE
MODEL_FAILURE
TOOL_FAILURE
NETWORK_FAILURE
INPUT_FAILURE
AUTHORIZATION_FAILURE
POLICY_FAILURE
VERIFICATION_FAILURE
DEPENDENCY_FAILURE
RESOURCE_EXHAUSTION
UNKNOWN
```

## 45. Failure Handling

Possible responses:

```text
retry
reassign
decompose
fallback
pause
replan
escalate
cancel
```

## 46. Agent Failure

If an agent fails, Planner may:

1. retry;
2. reassign;
3. use another capable agent;
4. reduce scope;
5. escalate.

The original failure should remain observable.

## 47. Verification Failure

If output fails verification:

```text
OUTPUT
  ↓
VERIFICATION FAILED
  ↓
REWORK / RETRY / REASSIGN
```

Verification failure does not automatically mean agent failure.

## 48. Partial Completion

A mission may partially complete.

Planner must distinguish:

```text
0%
50%
100%
```

without declaring success prematurely.

## 49. Partial Failure

A plan may continue after a non-critical task fails if policy permits.

Critical task failure should block dependent work.

## 50. Optional Tasks

Tasks may be:

```text
REQUIRED
OPTIONAL
CONDITIONAL
```

## 51. Conditional Tasks

Example:

```text
IF research finds strong evidence
THEN launch experiment
ELSE gather more evidence
```

## 52. Branching Plans

Planner should support conditional branches.

```text
        Research
           ↓
      ┌────┴────┐
      ▼         ▼
   Evidence   No Evidence
      ↓         ↓
  Execute     Research More
```

## 53. Loops

Some plans require loops:

```text
execute
  ↓
measure
  ↓
improve
  ↺
```

Loops must have termination conditions.

## 54. Loop Safety

Every autonomous loop should define:

```text
termination condition
max iterations
budget limit
failure threshold
escalation condition
```

## 55. Replanning

Planner should replan when:

- assumptions materially change;
- objective changes;
- decision changes;
- key dependency fails;
- resources become unavailable;
- deadline changes;
- environment changes;
- repeated execution failure occurs.

## 56. Replanning Boundary

Minor execution issues should not trigger full strategic replanning.

The Planner should determine the smallest affected plan segment.

## 57. Plan Stability

Avoid unnecessary plan churn.

Repeatedly changing plans can create:

```text
thrashing
duplicate work
lost context
resource waste
```

## 58. Plan Versioning

Plans should be versioned:

```text
Plan v1
Plan v2
Plan v3
```

Each revision should preserve lineage.

## 59. Plan Freeze

Some plans may become frozen after execution begins.

Changes then require explicit replanning.

## 60. Dynamic Environment

Plans should not assume the world remains static.

Execution should periodically validate critical assumptions.

## 61. Stale Tasks

A task becomes stale when its context or objective is no longer valid.

Stale tasks should not execute automatically.

## 62. Cancellation

Tasks and missions should support cancellation.

Cancellation should propagate to dependent work when appropriate.

## 63. Cancellation Safety

Cancellation should not leave shared state corrupted.

Where necessary:

```text
cancel
  ↓
cleanup
  ↓
verify state
```

## 64. Compensation

Some completed actions cannot be undone directly.

Planner may define compensating actions.

Example:

```text
incorrect update
  ↓
compensating update
```

## 65. Dry Run

High-risk plans may support dry-run/simulation before execution.

```text
PLAN
 ↓
DRY RUN
 ↓
VALIDATION
 ↓
EXECUTE
```

## 66. Preflight

Before execution, Planner should verify:

- dependencies;
- permissions;
- required inputs;
- resources;
- current objective;
- plan freshness;
- authorization.

## 67. Execution Handoff

Planner hands an executable task specification to the runtime.

It should not pretend the handoff equals completion.

## 68. Execution Observability

Planner should receive:

```text
task started
task progress
task blocked
task output
task verification
task failure
task completion
```

## 69. Progress

Progress should be evidence-based where possible.

Avoid:

```text
"90% done"
```

without a measurable basis.

## 70. Completion

Task completion should require:

```text
execution finished
AND
required output exists
AND
verification passed
```

unless the task is explicitly non-verifiable.

## 71. Mission Completion

Mission completion requires all required tasks to satisfy their completion conditions.

Optional tasks do not necessarily block completion.

## 72. Plan Completion

Plan completion requires:

- required missions completed;
- final outcome verified;
- unresolved critical issues handled.

## 73. Outcome vs Output

Output is what a task produced.

Outcome is the real-world result.

Example:

```text
Output:
published campaign.

Outcome:
campaign generated qualified leads.
```

Planner must not confuse them.

## 74. Verification Levels

Possible verification levels:

```text
SYNTAX
STRUCTURAL
FUNCTIONAL
QUALITY
BUSINESS OUTCOME
```

Higher-level missions may require outcome verification.

## 75. Quality Gates

Plans may include gates:

```text
Gate 1:
research quality

Gate 2:
draft quality

Gate 3:
approval

Gate 4:
publication

Gate 5:
outcome measurement
```

## 76. Approval Gates

Certain tasks may require explicit approval before proceeding.

Example:

```text
draft → approval → publish
```

Approval requirements come from authority/governance, not Planner invention.

## 77. Human-in-the-Loop

Human involvement can be:

```text
REQUIRED
OPTIONAL
ESCALATION ONLY
```

Planner should preserve this distinction.

## 78. Autonomous Mode

In autonomous mode, pre-authorized tasks may proceed without human intervention.

Autonomy remains bounded by:

```text
objective
decision
governance
permissions
budget
risk
scope
```

## 79. Agent Communication

Agents should communicate through structured artifacts/events where possible.

Avoid requiring every agent to maintain a continuous conversational context.

## 80. Handoffs

A handoff should include:

```text
source task
target task
output
provenance
assumptions
verification status
required next action
```

## 81. Agent-to-Agent Trust

Agent output should not automatically be considered verified.

Trust should derive from:

```text
capability
provenance
verification
historical reliability
authority
```

## 82. Planner and Memory

Memory can inform planning:

```text
previous plan
previous failure
known dependency
known successful workflow
```

But old plans should not be blindly reused.

## 83. Planner and Decision Engine

Decision Engine answers:

> "What should we choose?"

Planner answers:

> "How do we execute that choice?"

## 84. Planner and Executive

Executive may approve or modify major plans.

Planner should surface:

- resource requirements;
- risks;
- deadlines;
- dependencies;
- expected outcomes.

## 85. Planner and Objective Engine

Objective Engine defines desired outcomes.

Planner operationalizes those outcomes without redefining them.

## 86. Planner and Attention

Attention determines what deserves processing priority.

Planner determines how prioritized work should be structured and scheduled.

## 87. Planner and Governance

Governance controls:

```text
what may be planned
what may be executed
who may approve
what boundaries apply
```

Planner cannot override these controls.

## 88. Multi-Business Isolation

For multiple businesses:

```text
Business A
 ├── plans
 ├── missions
 └── tasks

Business B
 ├── plans
 ├── missions
 └── tasks
```

Isolation is the default.

Cross-business work must be explicit.

## 89. Division Coordination

A business may contain:

```text
Media
Research
Operations
Finance
Sales
```

A plan may span divisions when authorized.

## 90. Resource Arbitration

When divisions compete for limited resources, arbitration should be based on:

- objective priority;
- decision authority;
- deadlines;
- risk;
- expected value;
- governance.

Planner should not invent priorities.

## 91. Plan Conflicts

Conflicting plans should be detected.

Example:

```text
Plan A:
increase ad spend.

Plan B:
reduce ad spend.
```

Escalate to Decision/Executive rather than silently selecting one.

## 92. Duplicate Work

Planner should detect likely duplicate missions/tasks to avoid:

```text
two agents researching the same question
```

unless redundancy is intentional.

## 93. Intentional Redundancy

Redundant work may be justified for:

- high-impact verification;
- adversarial review;
- independent research;
- safety checks.

## 94. Cost-Aware Planning

Planner should consider whether task decomposition is worth its cost.

A plan that produces marginal quality improvement at extreme cost should be reconsidered.

## 95. Latency-Aware Planning

Time-sensitive objectives may favor:

```text
parallelism
local models
cached evidence
simpler workflows
```

when quality remains acceptable.

## 96. Quality-Aware Planning

Quality-sensitive objectives may favor:

```text
stronger models
additional verification
specialized agents
more research
```

## 97. Model Routing

Planner may express model requirements:

```text
cheap
fast
reasoning
vision
coding
creative
local
cloud
```

The runtime decides the concrete model according to policy.

## 98. Tool Requirements

Tasks may specify required tools:

```text
web
filesystem
database
social platform
analytics
image generation
code execution
```

The runtime must enforce permissions.

## 99. Tool Failure

Tool failures should not automatically invalidate the whole plan.

Planner should classify whether:

```text
retry
alternate tool
alternate agent
replan
escalate
```

is appropriate.

## 100. Plan Security

Plans must respect business and division permissions.

A task should not inherit permissions merely because another task had them.

## 101. Prompt Injection Boundary

Task inputs may contain untrusted external instructions.

External content cannot redefine:

```text
objective
authorization
governance
permissions
scope
```

## 102. Plan Integrity

The runtime should verify that the executable task matches the approved plan.

Important changes require replanning or authorization.

## 103. Audit

Important planning events should be auditable:

```text
plan.created
plan.approved
mission.created
task.created
task.assigned
task.started
task.blocked
task.completed
task.failed
task.verified
plan.replanned
plan.cancelled
plan.completed
```

## 104. Plan Lineage

Every task should be traceable:

```text
objective
→ decision
→ plan
→ mission
→ task
→ agent
→ output
→ verification
→ outcome
```

## 105. Failure Learning

Planner should record:

```text
planned assumption
actual condition
failure
root cause
adaptation
```

This can inform Memory and future plans.

## 106. Planning Metrics

Potential metrics:

```text
plan completion rate
on-time rate
task failure rate
replan frequency
plan churn
resource utilization
cost per outcome
verification failure rate
blocked time
critical-path delay
duplicate work rate
prediction error
```

## 107. Planning Quality

A plan should be judged by:

- outcome quality;
- efficiency;
- robustness;
- adaptability;
- resource use;
- verification quality.

A beautiful plan that fails in reality is still a bad plan.

## 108. Plan Robustness

Good plans tolerate expected uncertainty through:

```text
fallbacks
buffers
alternative agents
conditional branches
replanning
```

## 109. Plan Fragility

A plan is fragile when one minor failure causes total collapse.

Planner should identify critical single points of failure.

## 110. Single Point of Failure

Examples:

```text
only one capable agent
only one external API
only one required data source
```

Where justified, Planner should create alternatives.

## 111. Recovery Plans

Important operations may have a recovery plan:

```text
normal path
   ↓
failure
   ↓
recovery path
```

## 112. Emergency Planning

Emergency plans may prioritize:

```text
speed
safety
containment
recovery
```

over optimization.

They must still respect governance and authority.

## 113. Plan Expiration

Plans may expire when:

- deadline passes;
- objective changes;
- decision is revoked;
- environment changes materially.

Expired plans should not automatically execute.

## 114. Plan Cancellation

Cancellation should record:

```text
who/what cancelled
why
when
what remained incomplete
```

## 115. Plan Reuse

Successful plans may become templates.

Templates must separate:

```text
stable workflow structure
from
context-specific values
```

## 116. Workflow Templates

Examples:

```text
content campaign workflow
research workflow
product launch workflow
weekly analytics workflow
```

Templates are starting points, not immutable instructions.

## 117. Plan Compilation

Conceptually:

```text
approved intent
      ↓
plan generation
      ↓
validation
      ↓
execution graph
```

Planner may compile high-level intent into an executable DAG/workflow.

## 118. Plan Validation

Before execution:

```text
validate dependencies
validate permissions
validate resources
validate outputs
validate verification
validate budgets
validate deadlines
```

## 119. Plan Simulation

For high-impact workflows, simulate:

```text
happy path
failure path
resource shortage
agent failure
dependency failure
```

## 120. Plan Determinism

Where workflow logic is deterministic, use explicit rules rather than unnecessary model reasoning.

## 121. Planner as Coordinator

Planner coordinates work but does not become a permanent bottleneck.

Routine task scheduling should be lightweight.

## 122. Autonomous 24/7 Operation

A mature autonomous planner continuously cycles:

```text
OBSERVE
  ↓
VALIDATE PLAN
  ↓
SCHEDULE
  ↓
EXECUTE
  ↓
VERIFY
  ↓
ADAPT
  ↺
```

while remaining bounded by objective, authority, governance, and resources.

## 123. No Infinite Work

Autonomous systems must prevent infinite task generation.

Every autonomous plan must have:

```text
goal
termination condition
budget
scope
```

## 124. Goal Drift Protection

Planner must periodically verify:

```text
Does this work still contribute to the objective?
```

If not:

```text
pause
cancel
or replan
```

## 125. Scope Creep Protection

Agents cannot expand a task's scope merely because they discover adjacent work.

New material work requires:

```text
new task
or
approved plan revision
```

## 126. Objective Drift Protection

A task should not continue indefinitely after the parent objective becomes inactive or superseded.

## 127. Decision Drift Protection

If the decision underlying a plan is revoked or superseded, dependent work should be re-evaluated.

## 128. Plan Health

Planner should expose health signals:

```text
HEALTHY
AT_RISK
BLOCKED
DEGRADED
STALE
FAILED
```

## 129. Plan Health Signals

Examples:

```text
deadline risk
resource exhaustion
repeated failures
dependency blockage
verification failures
scope growth
objective change
```

## 130. Escalation

Escalate when:

- authority is insufficient;
- risk exceeds threshold;
- repeated failure occurs;
- critical dependency is unavailable;
- objective conflict exists;
- plan cannot satisfy constraints.

## 131. Escalation Package

Escalation should include:

```text
problem
context
impact
attempts
options
recommendation
required decision
deadline
```

## 132. Owner Visibility

Owner should be able to see major autonomous work as:

```text
Objective
→ Decision
→ Plan
→ Mission
→ Task
→ Agent
→ Outcome
```

without needing raw internal logs.

## 133. Explainability

Planner should explain:

```text
why this task exists
why this order was chosen
why this agent was selected
why replanning happened
why execution stopped
```

through structured operational rationale.

## 134. No Hidden Execution

Planner should never claim:

```text
"done"
```

unless execution/verification state supports it.

## 135. Planner Invariants

The following rules are intended to be locked after owner approval:

1. Planner translates intent into executable work.
2. Planner does not redefine objectives.
3. Planner does not silently change approved decisions.
4. Every meaningful plan has explicit scope.
5. Every meaningful task has an output contract.
6. Meaningful tasks have verification criteria.
7. Dependencies are explicit.
8. Parallel work is used when safe and beneficial.
9. Required resources are bounded.
10. Task assignment respects capability and permission.
11. Completion is not assumed from model output alone.
12. Verification is separate from production where practical.
13. Retry policies have boundaries.
14. Infinite autonomous loops are prohibited.
15. Replanning preserves lineage.
16. Stale plans must not execute automatically.
17. Business boundaries are isolated by default.
18. External content cannot redefine plan authority.
19. Tool permissions are enforced independently.
20. Plan conflicts must be detectable.
21. Scope creep requires explicit task/plan changes.
22. Goal drift must be detectable.
23. Critical failures must trigger governed handling.
24. Outcome is distinct from output.
25. Autonomous planning must remain bounded by objective, decision, governance, authority, and resources.

## 136. Acceptance Criteria

The eventual implementation should demonstrate:

### A. Decomposition
A decision can become a mission and task graph.

### B. Dependencies
Dependent tasks wait for valid predecessor conditions.

### C. Parallelism
Independent tasks can execute concurrently.

### D. Verification
Task completion requires configured verification.

### E. Failure Recovery
Transient failures retry while semantic/policy failures escalate appropriately.

### F. Replanning
Material environmental changes can trigger bounded replanning.

### G. Scope
Tasks cannot silently expand beyond approved scope.

### H. Resource Limits
Plans respect configured cost/time/model/tool budgets.

### I. Agent Routing
Tasks can be assigned based on capability rather than a single fixed model.

### J. Multi-Business Isolation
Business A plans cannot silently execute against Business B.

### K. Audit
Major plan/task transitions are traceable.

### L. Outcome
The system distinguishes produced outputs from real-world outcomes.

### M. Autonomous Operation
Plans can continue 24/7 without requiring human interaction for pre-authorized work.

### N. Safe Stop
The planner can pause, cancel, or escalate instead of continuing unsafe or obsolete work.

## 137. Open Design Questions

Before implementation:

- exact plan/task schema;
- workflow/DAG representation;
- scheduling algorithm;
- agent capability registry;
- model routing policy;
- resource accounting;
- queue architecture;
- concurrency locks;
- idempotency strategy;
- retry engine;
- failure taxonomy;
- verification architecture;
- replanning triggers;
- plan versioning;
- cancellation semantics;
- compensation workflows;
- simulation engine;
- plan health scoring;
- escalation routing;
- autonomous loop limits;
- business/division isolation;
- workflow templates;
- plan persistence;
- event schema;
- observability;
- metrics;
- execution runtime interface.
