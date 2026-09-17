# NEXUS Workflow & Orchestration Engine

**Status:** PROPOSED → awaiting owner lock

## Purpose

The Workflow & Orchestration Engine turns objectives, decisions, events, and requests into durable autonomous work.

A workflow is **not a chat session**. It is a persistent execution process that can run for minutes, hours, days, or weeks, survive restarts, coordinate agents/tools/divisions, wait for events, recover from failures, and replan when reality changes.

```text
Objective / Decision / Event
          ↓
    Workflow Definition
          ↓
      Execution Plan
          ↓
       Task Graph
          ↓
     Queue / Scheduler
          ↓
    Agents + Tools
          ↓
      Verification
          ↓
 Next Tasks / Replan
          ↓
     Completion
```

## Core Principles

1. **Durable execution** — workflow state survives process/runtime restarts.
2. **Owner-independent** — closing the UI does not stop autonomous work.
3. **Objective-linked** — meaningful work retains the “why”.
4. **Governed** — workflow execution never bypasses authority, permissions, scope, budgets, or Tool Runtime.
5. **Replan-capable** — new evidence can invalidate and replace an existing plan.
6. **Recoverable** — failures, timeouts, crashes, and unknown side effects have explicit recovery paths.
7. **Multi-business** — multiple real businesses can run concurrently inside one NEXUS installation.
8. **Multi-division** — Media, Business, Research, and future divisions can execute concurrently.
9. **Model-independent** — different tasks may use different suitable models/providers.
10. **Observable** — workflow state and event-to-action causality are traceable.

## Workflow vs Task

A workflow is an outcome-oriented process. A task is one executable unit inside it.

```text
Workflow
├── Research
├── Analyze
├── Generate
├── Review
└── Publish
```

## Workflow Identity

Each execution should contain:

```text
workflow_id
definition_id
version
business_id
scope
objective_id
trigger_id (optional)
priority
status
created_at
updated_at
deadline (optional)
```

## Workflow States

```text
DRAFT
READY
QUEUED
RUNNING
WAITING
PAUSED
BLOCKED
REPLANNING
AT_RISK
FAILED
COMPLETED
CANCELLED
EXPIRED
```

## Task States

```text
PENDING
READY
QUEUED
RUNNING
WAITING
BLOCKED
RETRYING
VERIFYING
COMPLETED
FAILED
CANCELLED
SKIPPED
UNKNOWN
```

## Execution Graph

Prefer a directed acyclic graph for normal workflows.

```text
        Research
       /        \
Competitor     Audience
       \        /
        Strategy
            ↓
         Content
            ↓
          Review
            ↓
         Publish
```

Independent tasks may run in parallel. Dependent tasks wait for prerequisites.

## Static and Dynamic Workflows

Support both:

- **Static:** predefined task structure.
- **Dynamic:** planner determines subsequent tasks from current evidence/state.

Dynamic task generation must pass orchestration validation before execution.

## Task Contract

Every task should define:

```text
task_id
workflow_id
type
description
objective_ref
dependencies
agent_requirement
tool_requirements
input_refs
output_schema
verification_rules
priority
deadline
retry_policy
budget
side_effect_level
```

## Agent Assignment

Agent selection may consider:

```text
capability
division
business scope
availability
model suitability
tool access
cost
performance history
priority
```

The workflow does not assume every task uses the same model.

## Context Assembly

Task context can include:

```text
task input
objective / why
business context
division context
relevant memory
previous outputs
artifacts
current state
constraints
```

Only relevant context should be supplied. Large history should be retrieved/summarized rather than blindly injected.

## Artifact Flow

Large outputs should be stored as artifacts:

```text
Task → Artifact Store → artifact_id → Next Task
```

Do not move huge payloads through model context unnecessarily.

## Dependencies

Dependencies may be based on:

```text
task completion
task success
artifact availability
condition
approval
event
external state
```

## Conditional Branching

Workflows support:

```text
IF condition
  → path A
ELSE
  → path B
```

## Parallelism

Independent branches may execute concurrently, subject to resource and provider limits.

## Dynamic Delegation

Agents may request delegation through the orchestration layer.

Delegated work inherits:

```text
business scope
objective scope
permissions
budget
deadline
```

Agents must not freely spawn uncontrolled agent chains.

## Temporary Agents

A workflow may request temporary agents for bounded work.

Temporary agents require:

```text
scope
lifetime
capabilities
budget
termination condition
```

## Child Workflows

A workflow may invoke a bounded child workflow.

Parent execution tracks:

```text
child status
outputs
failures
cost
```

Nested workflow depth must be bounded.

## Workflow Start

Workflows may start from:

```text
objective
event
trigger
schedule
agent decision
owner request
another workflow
```

Starting a workflow never bypasses governance.

## Objective Alignment

Before major autonomous execution, NEXUS should know:

```text
What objective does this serve?
Why does it matter?
Is it still relevant?
What constraints apply?
```

A workflow should retain this context throughout execution.

## Decision Trace

Persist concise metadata:

```text
decision_id
objective
evidence_refs
reason_category
constraints
chosen_path
```

Do not require storing private model chain-of-thought.

## Event Integration

Events can:

```text
start workflow
pause workflow
resume workflow
invalidate assumptions
trigger replan
cancel workflow
```

## Scheduling

The scheduler handles ready work and time-based execution.

Use explicit timezone metadata.

The UI session is only a control/view layer; it is not the workflow runtime.

## Waiting

A workflow may wait for:

```text
time
event
approval
external job
dependency
human input
```

Waiting must not consume continuous model/tool resources.

## Queue and Workers

Ready tasks enter durable queues. Workers claim tasks and execute them.

Running tasks should use leases/heartbeats.

Expired leases require recovery evaluation rather than blind retry.

## Concurrency

Limits may exist per:

```text
NEXUS
business
division
workflow
agent
provider
tool
```

Scheduling should prevent one workload from monopolizing resources.

## Budgets

Workflows may have:

```text
time budget
model/token budget
tool-call budget
API cost budget
parallelism budget
```

When exhausted:

```text
stop → fallback/replan → Attention
```

## Model Routing

Tasks may use different models according to:

```text
complexity
reasoning need
latency
cost
privacy
specialization
availability
```

NEXUS may use local-first configured models such as Ollama/local Hugging Face models, with OpenRouter or custom providers as configured.

Models can be pinned for reproducibility or dynamically routed for flexibility.

## Tool Boundary

All consequential external actions go through Tool Runtime.

The workflow engine does not bypass:

```text
permissions
credentials
network policy
sandbox
idempotency
verification
audit
```

## Reliability

Tasks should support:

```text
timeout
retry
bounded backoff
fallback
circuit-breaker awareness
```

Retryability depends on the error and side-effect semantics.

## Idempotency

Do not assume exactly-once external execution.

Prefer:

```text
at-least-once execution
+
idempotency
+
external reconciliation
```

If an external action may already have happened, reconcile before retrying.

## Compensation

For multi-system processes, use compensation/Saga-style recovery where atomic distributed transactions are unavailable.

## Checkpoints

Long workflows create durable checkpoints containing relevant:

```text
state
completed tasks
pending tasks
artifacts
external references
decisions
budgets
```

## Crash Recovery

After restart:

```text
load workflow
→ inspect task leases
→ reconcile unknown side effects
→ recover safe tasks
→ resume
```

## Unknown State

If execution status is uncertain:

```text
UNKNOWN
→ reconcile
→ determine state
→ retry / continue / replan
```

Never blindly duplicate a consequential external action.

## Replanning

Replan when:

```text
new event
objective change
assumption invalidated
tool unavailable
deadline changed
resource shortage
unexpected result
provider failure
```

Replanning should preserve completed valid work where possible.

## Plan Versioning

Persist plan versions and concise metadata explaining why a new plan replaced an old one.

## Deadline Management

When a deadline is threatened:

```text
prioritize
→ simplify
→ parallelize
→ replan
→ Attention
```

## Verification

Task success is based on acceptance criteria, not merely agent output.

Verification may include:

```text
schema validation
quality checks
business rules
external state confirmation
artifact validation
```

Critical external actions require evidence from Tool Runtime.

## Workflow Completion

Before marking success:

```text
required tasks complete
required outputs valid
external side effects verified
acceptance criteria satisfied
```

Completion types:

```text
SUCCESS
PARTIAL_SUCCESS
FAILED
CANCELLED
EXPIRED
```

## Failure Handling

Failure path may be:

```text
retry
→ fallback
→ reassign
→ repair
→ replan
→ Attention
→ owner escalation
```

Unknown errors should fail safely and preserve diagnostic state.

## Stalled Workflows

Detect workflows with no meaningful progress.

Recovery:

```text
inspect
→ retry/reassign
→ replan
→ Attention
```

## Pause / Resume

Workflows may be paused by:

```text
owner
policy
system
dependency
provider outage
```

Resume requires checking that permissions, assumptions, credentials, and tools are still valid.

## Cancellation

Cancellation should propagate safely to dependent tasks.

Critical workflows may support emergency termination.

Prefer graceful stopping at safe boundaries.

## Approval Gates

Some tasks may enter:

```text
WAITING_FOR_APPROVAL
```

Approval must specify:

```text
task
action
business
limits
expiration
```

No blanket approval.

## Human Input

Ask the owner only when required by:

```text
policy
high consequence
high uncertainty
missing critical information
objective conflict
```

Avoid interrupting autonomous work for information that can be safely resolved from existing context.

## Attention

Create Attention for:

```text
blocked work
elevated risk
high uncertainty
approval requirement
objective conflict
repeated failure
deadline threat
```

Attention does not automatically mean all autonomous work must stop.

## Workflow Locks

Conflicting workflows may require logical/resource locks.

Locks must have expiration and recovery mechanisms.

Avoid circular dependencies/deadlocks.

## Duplicate Workflow Protection

Repeated identical triggers/requests should detect existing compatible executions.

Possible actions:

```text
reuse
coalesce
ignore
start separately
```

## Multi-Business Architecture

One NEXUS installation can contain multiple real businesses.

```text
NEXUS
├── Business A
│   ├── Media
│   ├── Business
│   └── Research
│
└── Business B
    ├── Media
    ├── Business
    └── Research
```

Business A and B workflows run concurrently.

Changing UI session from A to B does not stop A.

Cross-business workflows require explicit authorization.

## Multi-Division Coordination

Divisions can coordinate through workflows.

Example:

```text
Research
  ↓
Media
  ↓
Business Analytics
  ↓
Executive
```

Each division retains its own scope and permissions.

## State Machine

Workflow and task transitions must be explicit.

Invalid transitions must fail safely.

Critical state changes should be persisted atomically where possible.

## Observability

Track:

```text
queue time
execution time
workflow duration
task duration
retries
failures
model usage
tool usage
cost
parallelism
replanning
```

## Audit

Consequential workflow transitions should be durably auditable.

Recommended events:

```text
WORKFLOW_CREATED
WORKFLOW_STARTED
TASK_READY
TASK_STARTED
TASK_COMPLETED
TASK_FAILED
TASK_RETRYING
TASK_BLOCKED
WORKFLOW_PAUSED
WORKFLOW_RESUMED
WORKFLOW_REPLANNED
WORKFLOW_COMPLETED
WORKFLOW_CANCELLED
```

## Security

Task outputs cannot grant themselves permissions.

Artifacts and external results remain untrusted until validated.

Workflow context must respect business/division isolation.

## Resource Failure

If a dependency, model, provider, credential, or tool becomes unavailable:

```text
detect
→ classify
→ fallback/retry
→ replan
→ Attention
```

Do not continue using invalid assumptions.

## Long-Running Work

Workflows may run for days/weeks using durable wait states, checkpoints, queues, and event-driven wakeups.

## Testing

Required tests:

```text
happy path
branching
parallelism
failure
retry
timeout
pause/resume
restart
duplicate trigger
partial completion
replan
approval
budget exhaustion
unknown side effect
worker crash
provider outage
permission violation
cross-business isolation
```

## Acceptance Criteria

- Workflow state survives restart.
- Work continues without the owner UI being open.
- Tasks support dependencies, branching, parallelism, and sequencing.
- Failures and unknown side effects have safe recovery.
- Replanning is supported.
- Completion uses explicit verification.
- Governance and permissions are enforced.
- Multiple businesses can execute concurrently with isolation.
- Multiple divisions can execute concurrently and coordinate.
- Tasks can use different suitable models/providers.
- External actions go through Tool Runtime.
- Events can start/resume/pause/replan workflows.
- Attention can surface important exceptions without unnecessary interruption.
- Cost and resource budgets are enforceable.
- Workflow causality is observable.
- Duplicate executions are safely handled.
- Long-running workflows can wait without busy-looping.
- Human input is requested only when necessary.

## Locked Architectural Principle

> **A NEXUS workflow is a durable autonomous execution process, not a chat session. It coordinates agents, tools, events, memory, and objectives across time; survives interruption; recovers from failure; and replans when reality changes, while remaining bounded by governance and business scope.**

## Existing Boundaries and CONTRACTS Next Layer

Architecture remains locked; no new modules from this cleanup.

-   [Agent Runtime & Lifecycle](NEXUS-AGENT-RUNTIME-LIFECYCLE.md) owns creation, configuration, spawning, assignment, isolation, model/tool access, collaboration, supervision, pause/resume, and termination of agents.
-   [Planner](NEXUS-PLANNER.md) owns objective-aligned, authorized plan construction and decomposition; Workflow executes, does not re-plan autonomously.
-   [Tool Runtime](NEXUS-TOOL-RUNTIME-CAPABILITY.md) owns external action boundary; [API Gateway](NEXUS-API-INTEGRATION-GATEWAY.md) owns connectors/auth/provider reconciliation; Workflow coordinates via Tool Runtime.
-   [Scheduling](NEXUS-SCHEDULING-RESOURCE-RUNTIME.md) owns priority/dependency/resource scheduling; [Event & Trigger](EVENT_TRIGGER_SYSTEM.md) owns wake/evaluate/route; Workflow consumes both.
-   [Governance](NEXUS-GOVERNANCE-POLICY-SAFETY-CONTROL.md), [Memory](NEXUS-MEMORY-CONTEXT-INTELLIGENCE.md), [Identity](NEXUS-IDENTITY-ACCESS-TRUST-SYSTEM.md), [Observability](NEXUS_OBSERVABILITY_AUDIT_TELEMETRY.md) boundaries unchanged.

Next layer is **CONTRACTS**: detailed schemas, state machines, request/result/error contracts, policy/credential interfaces, adapter protocols, queue/reconciliation handoffs, and test implementations. Field lists, API names, and state-machine sketches in this document are nonbinding contract candidates; normative boundary requirements remain in effect.
