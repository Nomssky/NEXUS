# NEXUS Workflow & Orchestration System

> **HISTORICAL / NONCANONICAL — superseded source specification.** Replacement: [Workflow & Orchestration Engine](WORKFLOW_ORCHESTRATION_ENGINE.md).
> The canonical owner governs; this source grants no competing authority. Planning remains with [Planner](NEXUS-PLANNER.md), not Workflow. Historical lifecycle alternatives, examples, schemas, API/interface drafts, acceptance sketches, and open questions are nonbinding candidates for the next CONTRACTS layer, not approved contracts or a mandate for new modules. Historical approval/lock wording below is not current status. The source body is preserved for traceability.

**Status:** HISTORICAL / NONCANONICAL (original proposal retained below)

## 1. Purpose

The Workflow & Orchestration System turns decisions and objectives into durable, stateful, executable autonomous processes.

It is the layer responsible for coordinating:

```text
Objective
 ↓
Decision
 ↓
Workflow
 ↓
Stages
 ↓
Tasks
 ↓
Agents
 ↓
Tools
 ↓
Verification
 ↓
Outcome
```

## 2. Core Principle

A workflow is not merely a list of prompts.

A workflow is a durable execution state with:

```text
intent
state
dependencies
tasks
agents
tools
policies
events
outputs
errors
recovery
```

## 3. Workflow vs Task

```text
Workflow = multi-step outcome-oriented process
Task     = one executable unit of work
```

A workflow may contain many tasks.

## 4. Workflow vs Mission

Suggested distinction:

```text
Mission  = strategic outcome / bounded objective
Workflow = operational process used to achieve it
Task    = atomic execution unit
```

Example:

```text
Mission:
  Launch social campaign

Workflow:
  Research → Strategy → Production → Review → Publish → Analyze

Tasks:
  research competitors
  draft hooks
  create assets
  schedule posts
```

## 5. Workflow State

A workflow must have durable state.

Suggested states:

```text
DRAFT
READY
RUNNING
WAITING
BLOCKED
PAUSED
REPLANNING
COMPLETED
FAILED
CANCELLED
EXPIRED
ARCHIVED
```

## 6. Durable Execution

Workflow state must survive:

```text
process restart
machine restart
provider failure
agent failure
temporary network failure
owner disconnect
```

## 7. Workflow Identity

Every workflow should have:

```text
workflow_id
business_id
division_id
mission_id
objective_id
parent_workflow_id
created_at
updated_at
version
status
```

## 8. Workflow Definition

A workflow definition describes:

```text
purpose
inputs
stages
tasks
dependencies
constraints
completion criteria
failure policy
timeout policy
compensation policy
```

## 9. Workflow Instance

A workflow definition is a template.

A workflow instance is one execution.

```text
Definition
   ↓
Instance
   ↓
Runtime state
```

## 10. Workflow Versioning

Running workflows should remain associated with their original definition/version unless explicitly migrated.

## 11. Workflow Context

Workflow context may include:

```text
objective
business
division
mission
relevant memory
events
artifacts
decisions
constraints
policies
```

## 12. Context Minimization

Do not inject the entire NEXUS memory into every workflow.

Retrieve only relevant context.

## 13. Workflow Inputs

Inputs should be explicit and validated.

Examples:

```text
campaign_id
research_scope
deadline
budget
target_platform
```

## 14. Workflow Outputs

Outputs should be typed artifacts or structured results where practical.

Examples:

```text
research_report
content_plan
creative_assets
published_post
analytics_report
```

## 15. Artifact-Oriented Execution

Tasks should communicate through durable artifacts/results rather than relying exclusively on agent conversational context.

## 16. Workflow Graph

Workflows should support a graph model:

```text
A
├── B
└── C
     ↓
     D
```

## 17. Sequential Steps

Simple workflows can execute sequentially.

```text
A → B → C
```

## 18. Parallel Steps

Independent tasks may execute concurrently.

```text
       ┌→ B ─┐
A ─────┤     ├→ D
       └→ C ─┘
```

## 19. Conditional Branching

Workflow steps may depend on conditions.

```text
A
 ↓
IF condition
 ├─ true  → B
 └─ false → C
```

## 20. Loops

Loops may be supported with strict bounds.

```text
research
 ↓
evaluate
 ↓
more research?
 ├─ yes → research
 └─ no  → continue
```

Never allow unbounded autonomous loops.

## 21. Loop Budget

Loops should have limits based on:

```text
iterations
time
cost
tool calls
model calls
```

## 22. Human Wait State

Workflows can pause awaiting owner input/approval.

```text
RUNNING
 ↓
WAITING_FOR_OWNER
 ↓
resume
```

## 23. External Wait State

A workflow may wait for:

```text
payment
webhook
customer reply
provider event
scheduled time
external processing
```

## 24. Event Resume

External events can resume waiting workflows.

## 25. Timeout

Every wait should have an appropriate timeout or expiration policy.

## 26. Workflow Deadlines

Workflows may define:

```text
deadline
soft deadline
hard deadline
```

## 27. Deadline Handling

Near-deadline workflows may trigger:

```text
reprioritization
parallelization
scope reduction
escalation
```

## 28. Task Model

A task should contain:

```text
task_id
workflow_id
parent_task_id
objective
instructions
inputs
outputs
assigned_agent
required_capabilities
dependencies
constraints
priority
risk
status
deadline
retry_policy
```

## 29. Task States

```text
PENDING
READY
RUNNING
WAITING
BLOCKED
RETRYING
COMPLETED
FAILED
CANCELLED
EXPIRED
```

## 30. Task Dependencies

Tasks can depend on:

```text
task completion
artifact availability
event occurrence
approval
condition
```

## 31. Dependency Graph

The orchestrator should determine which tasks are executable based on current state.

## 32. Task Idempotency

Tasks with external side effects should have idempotency mechanisms where possible.

## 33. Exactly-Once Side Effects

NEXUS should not assume exactly-once execution.

Use:

```text
idempotency keys
state checks
provider confirmation
post-action verification
```

## 34. Task Retry

Retries should be policy-driven.

Do not retry blindly.

## 35. Retry Classes

Possible failure classes:

```text
TRANSIENT
RATE_LIMIT
PROVIDER_UNAVAILABLE
AGENT_FAILURE
INVALID_INPUT
POLICY_BLOCK
PERMANENT
UNKNOWN
```

## 36. Retry Backoff

Transient failures should use bounded exponential/backoff strategies where appropriate.

## 37. Retry Budget

Each task should have retry limits.

## 38. Retry Safety

Never retry an uncertain external side effect without checking whether it already occurred.

## 39. Compensation

For workflows with multiple side effects, define compensating actions where practical.

Example:

```text
reserve resource
 ↓
later failure
 ↓
release resource
```

## 40. Saga-Like Workflows

Long-running workflows may use saga-style compensation rather than database transactions.

## 41. Compensation Is Not Undo

External actions may be irreversible.

The workflow must model that explicitly.

## 42. Failure Handling

Workflow failure can produce:

```text
retry
replan
compensate
pause
escalate
cancel
```

## 43. Partial Failure

A workflow should preserve successful outputs even when later tasks fail.

## 44. Replanning

When assumptions become invalid, Planner may generate a revised execution path.

```text
failure/new information
 ↓
replan
 ↓
new tasks
```

## 45. Replanning Boundary

Replanning cannot override Governance or expand authority.

## 46. Replanning Triggers

Examples:

```text
task failure
new event
objective change
resource unavailable
deadline risk
new evidence
```

## 47. Replanning Frequency

Repeated replanning should have limits to prevent endless deliberation.

## 48. Plan Stability

Minor noise should not constantly regenerate the entire workflow.

## 49. Plan Revision

Prefer local modifications when only one part of the workflow changes.

## 50. Workflow Checkpoints

Long-running workflows should checkpoint state.

## 51. Checkpoint Contents

May include:

```text
completed tasks
pending tasks
artifacts
decisions
agent assignments
external references
policy version
```

## 52. Recovery

After restart:

```text
load checkpoint
→ validate current world state
→ revalidate policies
→ identify incomplete work
→ resume safely
```

## 53. Stale State

A checkpoint does not guarantee the external world is unchanged.

External state should be revalidated before consequential actions.

## 54. Workflow Locking

Concurrent workers must avoid conflicting modifications to the same workflow state.

## 55. Optimistic Concurrency

Workflow updates should detect stale versions.

## 56. Workflow Ownership

Every workflow belongs to a business/mission/objective scope.

## 57. Cross-Business Workflow

Cross-business workflows require explicit authorization.

Default:

```text
Business A workflow
≠
Business B workflow
```

## 58. Cross-Division Workflow

Cross-division workflows are allowed when scope/policy permits.

## 59. Agent Assignment

Agent selection should consider:

```text
capability
authority
availability
cost
quality
latency
historical reliability
```

## 60. Agent Substitution

If an agent fails, orchestrator may substitute another compatible agent if authorized.

## 61. Agent Authority Recheck

Replacement agents must independently pass authority checks.

## 62. Dynamic Agent Creation

Workflows may request new temporary agents when authorized.

## 63. Dynamic Agent Scope

Created agents receive only the authority needed for the workflow/task.

## 64. Agent Concurrency

One agent may work on multiple tasks only when its runtime supports safe concurrency.

## 65. Tool Scheduling

Tool usage should respect:

```text
rate limits
budgets
dependencies
credentials
policy
```

## 66. Resource Scheduling

Workflows compete for:

```text
CPU
memory
GPU
model tokens
network
API quotas
money
agent slots
```

## 67. Resource Budgets

Budgets may exist at:

```text
workflow
mission
division
business
NEXUS
```

## 68. Budget Reservation

For consequential resource use, reserve budget before execution when appropriate.

## 69. Budget Release

Unused reserved budget should return to the available pool.

## 70. Budget Exhaustion

When a workflow exhausts budget:

```text
pause
reduce scope
replan
escalate
cancel
```

## 71. Cost-Aware Planning

Planner should consider cost before generating large execution graphs.

## 72. Model Routing

Different workflow tasks may use different models.

Example:

```text
classification → small local model
reasoning → stronger model
creative generation → specialized model
sensitive data → approved local model
```

## 73. Model Failure

Model unavailability should trigger fallback according to routing policy.

## 74. Model Fallback

Fallback must respect:

```text
data policy
quality requirement
cost limit
provider policy
```

## 75. Tool Failure

Tool failure should produce structured failure information.

## 76. Provider Failure

Provider outages may trigger:

```text
retry
fallback
wait
alternative workflow
escalation
```

## 77. Workflow Priority

Priorities:

```text
CRITICAL
HIGH
NORMAL
LOW
```

Priority does not override Governance.

## 78. Scheduling

Scheduler determines when READY work receives resources.

## 79. Fair Scheduling

One workflow should not starve all others.

## 80. Business Fairness

Multiple businesses should receive isolated resource budgets/quotas where configured.

## 81. Division Fairness

High-volume divisions should not starve other divisions.

## 82. Workflow Preemption

Low-priority workflows may be paused to preserve critical resources.

## 83. Safe Preemption

Tasks should be paused only at safe boundaries where possible.

## 84. Long-Running Tasks

Long tasks should periodically checkpoint if practical.

## 85. Workflow Heartbeat

Long-running execution should expose liveness.

## 86. Stuck Detection

Detect:

```text
no progress
repeated retries
dead agent
blocked dependency
```

## 87. Stuck Workflow Response

Possible:

```text
retry
reassign
replan
pause
escalate
cancel
```

## 88. Workflow Cancellation

Cancellation should propagate to dependent tasks.

## 89. Cancellation Safety

Already-completed external side effects are not magically undone.

Compensation should be attempted where defined.

## 90. Workflow Expiration

Expired workflows should stop generating new work.

## 91. Workflow Archival

Completed/expired workflows may be archived while preserving audit references.

## 92. Workflow Observability

Expose:

```text
current state
current stage
active tasks
blocked tasks
progress
cost
runtime
agents
tools
errors
```

## 93. Progress

Progress should be based on meaningful completion, not merely number of tasks.

## 94. Outcome Verification

Workflow completion requires outcome verification where appropriate.

## 95. False Completion

An agent saying "done" is not sufficient evidence for consequential workflows.

## 96. Verification Task

Important workflows should include explicit verification.

```text
execute
 ↓
verify
 ↓
complete
```

## 97. External Verification

Where possible, verify against the external system.

Example:

```text
publish request
→ provider confirmation
→ retrieve published state
```

## 98. Artifact Verification

Verify generated artifacts meet required schema/quality constraints.

## 99. Objective Verification

Workflow outcome should be evaluated against its objective.

## 100. Workflow Completion

A workflow should complete only when:

```text
required tasks complete
AND
required artifacts exist
AND
verification passes
AND
no unresolved critical blockers
```

## 101. Workflow Failure

Failure should contain:

```text
reason
failed task
evidence
retry status
recommended next action
```

## 102. Workflow Explainability

NEXUS should explain:

```text
why workflow started
why tasks were selected
why agents were chosen
why branches occurred
why actions were allowed
why workflow completed/failed
```

## 103. Workflow Audit

Record:

```text
workflow lifecycle
task lifecycle
agent assignments
tool calls
policy decisions
approvals
replans
outputs
errors
```

## 104. Causality

Maintain:

```text
event
→ decision
→ workflow
→ task
→ action
→ outcome
```

## 105. Workflow Tracing

Each workflow should have correlation IDs linking distributed operations.

## 106. Workflow Events

Emit lifecycle events:

```text
workflow.created
workflow.started
workflow.paused
workflow.resumed
workflow.replanned
workflow.completed
workflow.failed
workflow.cancelled
```

## 107. Task Events

Emit:

```text
task.created
task.ready
task.started
task.waiting
task.completed
task.failed
task.retried
task.cancelled
```

## 108. Event Integration

Workflow events feed the Event & Trigger System.

## 109. Trigger Integration

External events can start/resume workflows.

## 110. Attention Integration

Workflow failures, blockers, and important milestones can create Attention items.

## 111. Objective Integration

Workflow progress should update objective progress signals.

## 112. Memory Integration

Durable workflow learnings may become memory candidates.

## 113. Governance Integration

Every consequential task action must pass Governance/Policy.

## 114. Tool Runtime Integration

Workflow orchestration requests tool execution through Tool Runtime.

## 115. No Direct Side Effects

Orchestrator should not bypass Tool Runtime to perform external actions.

## 116. Planner Integration

Planner proposes workflow structure.

Orchestrator executes and monitors it.

## 117. Decision Integration

Decision Engine chooses whether a workflow is appropriate.

## 118. Owner Intervention

Owner may:

```text
pause
resume
cancel
approve
reject
reprioritize
modify objective
```

## 119. Owner Modification

Changes to a running workflow should be versioned/audited.

## 120. Autonomous Continuation

If owner disconnects, authorized workflows continue.

## 121. Owner Disconnect

UI/session loss must not terminate background workflows.

## 122. Multi-Business Runtime

Business A and Business B workflows may execute concurrently within one NEXUS installation.

## 123. Business Context Isolation

Each workflow carries explicit business context.

## 124. Session Independence

UI session selection does not change runtime workflow scope.

## 125. Workflow Templates

NEXUS should support reusable workflow templates.

Example:

```text
social_campaign_launch
competitor_monitoring
weekly_business_review
customer_issue_resolution
```

## 126. Template Parameters

Templates may expose configurable inputs.

## 127. Template Governance

Installing a template does not automatically grant permissions.

## 128. Template Versioning

Templates should be versioned.

## 129. Workflow Composition

Workflows may call child workflows when authorized.

## 130. Child Workflow Scope

Child workflows inherit bounded context.

## 131. Workflow Recursion

Recursive workflow invocation requires explicit limits.

## 132. Workflow Depth

Limit nesting depth.

## 133. Workflow Fan-Out

Limit number of child workflows/tasks created by one workflow.

## 134. Cascade Protection

Prevent:

```text
workflow A
→ B
→ C
→ A
```

from becoming infinite.

## 135. Workflow Quotas

Support quotas:

```text
per agent
per division
per business
global
```

## 136. Workflow Cost Forecast

Estimate expected cost for large workflows where possible.

## 137. Workflow Dry Run

Support dry-run/simulation before execution.

## 138. Workflow Test Mode

Synthetic events and sandbox tools should be supported.

## 139. Production Boundary

Test workflows must not accidentally perform production side effects.

## 140. Workflow Security

Workflow inputs are untrusted unless validated.

## 141. Prompt Injection Boundary

External content inside workflow inputs cannot redefine system authority.

## 142. Instruction Hierarchy

Workflow data:

```text
content
```

does not become:

```text
system instruction
```

## 143. Artifact Security

Artifacts can carry provenance and trust metadata.

## 144. Artifact Versioning

Important artifacts should be versioned.

## 145. Artifact Lineage

Track:

```text
artifact
← task
← agent
← workflow
← objective
```

## 146. Artifact Access

Artifact access follows business/division/data governance.

## 147. Workflow Data Retention

Retention should follow policy.

## 148. Workflow Privacy

Sensitive task data should not be exposed to unrelated agents.

## 149. Context Transfer

When one agent hands work to another, transfer only necessary context/artifacts.

## 150. Handoff

A handoff should contain:

```text
objective
current state
completed work
remaining work
constraints
artifacts
risks
```

## 151. Agent Handoff Verification

Receiving agent should verify the handoff before continuing.

## 152. Human Handoff

Owner escalations should contain concise context:

```text
problem
why blocked
options
recommendation
required decision
```

## 153. Workflow Approval Gate

Workflow stages may include approval gates.

## 154. Approval Gate State

```text
WAITING_FOR_APPROVAL
APPROVED
REJECTED
EXPIRED
```

## 155. Approval Scope

Approval must be tied to a specific action/scope.

## 156. Approval Revalidation

Before consequential execution, approval must still be valid.

## 157. Policy Revalidation

Policy must be checked at action time.

## 158. World-State Revalidation

External state must be checked before irreversible actions.

## 159. Stale Plan Detection

If the world has materially changed, workflow may require replanning.

## 160. Decision Drift

If objective assumptions become invalid, Decision Engine should reconsider.

## 161. Workflow Learning

Post-workflow analysis can identify:

```text
bottlenecks
failures
cost waste
successful patterns
```

## 162. Learning Boundary

Learning should improve future recommendations, not silently change governance.

## 163. Postmortem

Failed important workflows may generate a structured postmortem.

## 164. Success Analysis

Successful workflows may generate reusable patterns.

## 165. Workflow Metrics

Track:

```text
success rate
completion time
cost
retries
replans
failure causes
human interventions
objective outcome
```

## 166. Autonomy Metrics

Track:

```text
autonomous completions
autonomous failures
escalations
owner interventions
```

## 167. Workflow Quality

Quality should consider outcome, not only speed.

## 168. Workflow SLA

Optional SLA metrics:

```text
time-to-start
time-to-complete
time-to-resolution
```

## 169. Critical Workflow SLA

Critical workflows may receive dedicated resource reservations.

## 170. Resource Starvation

Detect when workflows cannot progress due to resource contention.

## 171. Deadlock Detection

Detect dependency/resource deadlocks.

## 172. Deadlock Recovery

Possible:

```text
reorder
release resource
replan
escalate
```

## 173. Dependency Failure

If a dependency becomes impossible, downstream tasks should be invalidated/replanned rather than waiting forever.

## 174. Workflow Pause

Pause should preserve state.

## 175. Workflow Resume

Resume should revalidate:

```text
policy
credentials
dependencies
external state
deadlines
```

## 176. Workflow Migration

Long-lived workflows may require migration between workflow versions.

Migration must be explicit and validated.

## 177. Workflow Snapshot

Support inspectable snapshots for debugging.

## 178. Workflow Replay

Replay should be simulation/read-only unless explicit side effects are authorized.

## 179. Workflow Determinism

Orchestration decisions should be reproducible enough for audit, even if agent reasoning is nondeterministic.

## 180. Agent Reasoning Record

Store concise decision artifacts rather than unrestricted hidden chain-of-thought.

## 181. Workflow Security Boundary

The authoritative boundary remains:

```text
Governance
+
Tool Runtime
+
Credential Isolation
```

## 182. Orchestrator Trust

The orchestrator coordinates execution but cannot grant itself authority.

## 183. Scheduler Trust

Scheduler allocates execution opportunities but cannot bypass policy.

## 184. Worker Trust

Workers execute assigned tasks within their granted scope.

## 185. Workflow API

Future internal API should support:

```text
create_workflow
start_workflow
pause_workflow
resume_workflow
cancel_workflow
get_workflow
list_tasks
retry_task
replan_workflow
approve_workflow
simulate_workflow
```

## 186. Workflow Event API

Support:

```text
emit_event
wait_for_event
subscribe
resume_on_event
```

## 187. Workflow State API

Support atomic state transitions.

## 188. Workflow Storage

Persist:

```text
definitions
instances
tasks
dependencies
artifacts
checkpoints
events
audit references
```

## 189. Transaction Boundary

State transitions and task scheduling should be designed to minimize duplicate execution.

## 190. Outbox Pattern

An outbox/event publication pattern may be used to reliably connect state changes with events.

## 191. Queue Pattern

Work queues should support durable delivery and acknowledgment.

## 192. Worker Lease

Workers may lease tasks with expiration.

## 193. Worker Recovery

Expired leases make tasks eligible for reassignment after safe checks.

## 194. Task Claiming

Task claiming must avoid duplicate active execution.

## 195. Task Heartbeat

Long-running tasks should renew their lease/heartbeat.

## 196. Zombie Worker

A worker that stops heartbeating should eventually lose its lease.

## 197. Duplicate Worker Safety

Even with leases, external side effects require idempotency/reconciliation.

## 198. Acceptance Criteria

Implementation should demonstrate:

### A. Durable Workflows
State survives restart.

### B. Task Graph
Sequential, parallel, conditional, and bounded loops work.

### C. Autonomous Continuation
Workflows continue without UI connection.

### D. Recovery
Failed workers/tasks can recover safely.

### E. Retry
Transient failures retry within policy.

### F. Replanning
Workflows can adapt to new information.

### G. Verification
Completion requires outcome verification where configured.

### H. Governance
Consequential actions cannot bypass policy.

### I. Multi-Business
Workflows remain scoped to their business.

### J. Multi-Division
Authorized cross-division workflows work safely.

### K. Resource Management
Budgets and quotas are enforced.

### L. Event Integration
Events can start/resume workflows.

### M. Attention Integration
Important blockers/failures surface to Attention.

### N. Objective Integration
Workflow execution remains linked to objectives.

### O. Auditability
Workflow → task → agent → tool → outcome is traceable.

### P. Cancellation
Workflows can be safely cancelled.

### Q. Simulation
Workflows can be dry-run/tested.

### R. Cascade Protection
Recursive workflow generation is bounded.

## 199. Open Design Questions

Before implementation:

- workflow definition format;
- workflow state machine;
- task graph engine;
- durable queue;
- scheduler;
- worker leases;
- checkpoint store;
- artifact store;
- event integration;
- retry engine;
- compensation model;
- replan mechanism;
- resource scheduler;
- budget engine;
- workflow DSL;
- template system;
- workflow migration;
- simulation engine;
- workflow API;
- state transition atomicity;
- outbox/event publication;
- concurrency control;
- distributed locking;
- observability;
- tracing;
- workflow metrics;
- business/division isolation;
- owner approval gates.
