# NEXUS — Runtime & Execution Contracts

**Layer:** Phase 3 — Runtime & Execution Contracts  
**Status:** LOCKED  
**Branch:** contracts/runtime-execution  
**Depends on:** Phase 1 Core Interface Contracts, Phase 2 Data & Event Schemas  

---

## 1. Purpose

This document defines **implementation-facing behavioral contracts** for NEXUS runtime execution. It specifies what must happen during actual execution: admission, scheduling, resource allocation, worker lifecycle, workflow execution, task execution, agent runtime, model invocation, tool invocation, API execution, verification, and outcome recording.

These contracts answer: *"Given a valid request/schema, what must the runtime do, what states may occur, how does it recover, and what guarantees does the caller receive?"*

Architecture remains frozen. No code implementation in this phase.

---

## 2. Design Principle

Execution is decomposed into distinct, non-collapsed stages:

```
REQUEST → VALIDATE → IDENTITY → AUTHORIZATION → GOVERNANCE → APPROVAL → RESOURCE CHECK → SCHEDULING → ADMISSION → EXECUTION → VERIFICATION → OUTCOME → PERSISTENCE
```

Each stage is related but MUST NOT be collapsed into one operation. Execution cannot bypass Governance. A valid task does not imply authorization. An approved action does not imply successful execution. A successful execution does not imply verified success.

---

## 3. Runtime Admission Contract

### 3.1 Admission Pipeline

Every unit of work must pass through the admission pipeline before execution:

| Stage | Actor | Description | Failure |
|---|---|---|---|
| 1. RECEIVE | Ingress | Work request received | Reject with reason |
| 2. VALIDATE | Schema Validator | Schema validation | VALIDATION error |
| 3. IDENTITY | Identity | Actor identity verification | AUTH error |
| 4. AUTHORIZATION | Governance | Authority resolution | AUTHORIZATION error |
| 5. POLICY | Governance | Policy evaluation | POLICY_DENIED or REQUIRE_APPROVAL |
| 6. APPROVAL | Governance | Approval gate (if required) | Pending until approved/denied |
| 7. RESOURCE CHECK | Scheduler | Resource availability check | RESOURCE_UNAVAILABLE |
| 8. SCHEDULE | Scheduler | Queue placement and scheduling | QUEUED or DEFERRED |
| 9. ADMIT | Admission Controller | Final admission decision | BLOCKED or CANCELLED |
| 10. DISPATCH | Runtime | Dispatch to worker/executor | DEPENDENCY_FAILURE |

### 3.2 Admission Rules

| Rule | Description |
|---|---|
| No bypass | Admission MUST NOT skip any stage |
| No authority grant | Admission does not grant authority |
| No resource guarantee | Admission does not guarantee resources will be available at execution time |
| Scope isolation | Admission respects business/division scope |
| Priority handling | Higher priority work may preempt lower priority |
| Audit | Every admission decision is auditable |
| Rejection reason | Every rejection includes a specific reason code |

### 3.3 Admission Rejection Reasons

| Reason | Code | Retryable |
|---|---|---|
| Schema invalid | `ADM_VALIDATION` | No |
| Actor not authenticated | `ADM_AUTH` | No |
| Actor not authorized | `ADM_AUTHORIZATION` | No |
| Policy denied | `ADM_POLICY` | No |
| Approval required | `ADM_APPROVAL` | No (wait for approval) |
| Resources unavailable | `ADM_RESOURCES` | Yes (queue) |
| Queue full | `ADM_QUEUE_FULL` | Yes (backpressure) |
| Deadline passed | `ADM_DEADLINE` | No |
| Cancelled | `ADM_CANCELLED` | No |
| Scope violation | `ADM_SCOPE` | No |

### 3.4 Admission State Machine

```
RECEIVED → VALIDATING → IDENTIFYING → AUTHORIZING → POLICY_CHECK
                                                       ↓
                                               {APPROVED, DENIED, APPROVAL_REQUIRED}
                                                       ↓
                                               RESOURCE_CHECK → SCHEDULING → ADMITTED → DISPATCHED
                                                       ↓
                                               {BLOCKED, DEFERRED, CANCELLED}
```

---

## 4. Scheduling Contract

### 4.1 Scheduler Responsibilities

The scheduler is responsible for:
- Placing work in appropriate queues
- Managing queue priority and fairness
- Handling dependencies between work items
- Respecting deadlines
- Managing resource reservations
- Enforcing business/division isolation
- Handling backpressure
- Retry scheduling

### 4.2 Scheduling State Machine

```
QUEUED → ELIGIBLE → SCHEDULED → ADMITTED
   ↓         ↓          ↓           ↓
BLOCKED   DEFERRED   EXPIRED    CANCELLED
```

| State | Description |
|---|---|
| `QUEUED` | Work is in queue, waiting for eligibility |
| `ELIGIBLE` | Work is ready to be scheduled (dependencies met, resources available) |
| `SCHEDULED` | Work has been assigned to a time slot or resource |
| `ADMITTED` | Work has been admitted for execution |
| `BLOCKED` | Work is blocked waiting for external input or resource |
| `DEFERRED` | Work is deferred to a later time |
| `EXPIRED` | Work exceeded its deadline |
| `CANCELLED` | Work was cancelled |

### 4.3 Priority Rules

| Priority | Preemption | Fairness | Queue Selection |
|---|---|---|---|
| `emergency` | Preempts all | Always first | Dedicated emergency queue |
| `critical` | Preempts high/medium/low | High fairness | Priority queue |
| `high` | Preempts medium/low | High fairness | Priority queue |
| `medium` | No preemption | Standard fairness | Standard queue |
| `low` | No preemption | Low fairness | Standard queue |
| `background` | No preemption | Minimal fairness | Background queue |

### 4.4 Fairness Rules

| Rule | Description |
|---|---|
| Business isolation | Each business gets fair share proportional to allocation |
| Starvation prevention | Work waiting > threshold gets priority boost |
| Age-based aging | Work priority increases with wait time |
| Emergency bypass | Emergency work bypasses all queues |
| Division isolation | Divisions within a business get fair share |

### 4.5 Dependency Handling

- Work with unresolved dependencies remains `QUEUED`
- When all dependencies complete, work transitions to `ELIGIBLE`
- If a dependency fails, dependent work may be cancelled or retried per policy
- Dependency chains must not create deadlocks (detected and reported)

### 4.6 Deadline Handling

- Work with deadline has scheduling priority boost as deadline approaches
- Work past deadline transitions to `EXPIRED`
- Deadline expiry generates Attention event
- Deadline expiry does NOT automatically cancel (depends on policy)

---

## 5. Resource Allocation Contract

### 5.1 Resource Types

| Resource |计量单位 | 分配方式 |
|---|---|---|
| CPU | cores | Reservation or sharing |
| RAM | bytes | Reservation |
| GPU | count | Reservation |
| VRAM | bytes | Reservation |
| Disk | bytes | Reservation |
| Network | bandwidth | Rate limiting |
| Model Inference | tokens | Quota |
| API Calls | calls | Rate limiting |
| Tool Execution | concurrent | Pool |
| Database Access | connections | Pool |

### 5.2 Resource Request

| Field | Type | Required | Description |
|---|---|---|---|
| `resource_type` | string | yes | Type of resource requested |
| `quantity` | number | yes | Amount requested |
| `unit` | string | yes | Unit of measurement |
| `duration_seconds` | integer | no | Expected duration |
| `exclusive` | boolean | no | Whether exclusive access is needed |
| `priority` | enum | yes | Resource allocation priority |

### 5.3 Resource Allocation State Machine

```
REQUESTED → CHECKING → {ALLOCATED, PARTIAL, DENIED}
                          ↓          ↓
                      USING → RELEASED
                          ↓
                      EXHAUSTED → {REALLOCATED, FAILED}
```

### 5.4 Resource Rules

| Rule | Description |
|---|---|
| Availability ≠ Authorization | Having resources does not grant permission to use them |
| Reservation | Resources may be reserved but must be released |
| Exhaustion handling | Resource exhaustion triggers backpressure or denial |
| Scope isolation | Resources are allocated per business/division |
| Monitoring | Resource usage is tracked and observable |
| Release | Resources must be released on completion or failure |

---

## 6. Worker Lifecycle Contract

### 6.1 Worker Lifecycle State Machine

```
CREATED → STARTING → REGISTERING → READY → BUSY → DRAINING → STOPPED
                        ↓            ↓       ↓         ↓
                    FAILED       DEGRADED  RECOVERING
```

| State | Description |
|---|---|
| `CREATED` | Worker instance created |
| `STARTING` | Worker is initializing |
| `REGISTERING` | Worker is registering with scheduler |
| `READY` | Worker is ready to accept work |
| `BUSY` | Worker is executing work |
| `DRAINING` | Worker is finishing current work, not accepting new |
| `STOPPED` | Worker has stopped |
| `FAILED` | Worker has failed |
| `DEGRADED` | Worker is operational but degraded |
| `RECOVERING` | Worker is recovering from failure |

### 6.2 Worker Identity

| Field | Type | Required | Description |
|---|---|---|---|
| `worker_id` | string | yes | Unique worker identifier |
| `worker_type` | enum | yes | One of: `process`, `container`, `serverless`, `human` |
| `capabilities` | list[string] | yes | What this worker can execute |
| `resource_pool` | string | yes | Resource pool this worker belongs to |
| `business_scope` | string | no | Business scope (for business-specific workers) |

### 6.3 Lease Contract

| Field | Type | Required | Description |
|---|---|---|---|
| `lease_id` | string | yes | Unique lease identifier |
| `owner` | string | yes | Worker ID that holds this lease |
| `resource` | string | yes | What this lease covers |
| `issued_at` | datetime | yes | When lease was issued |
| `expires_at` | datetime | yes | When lease expires |
| `heartbeat_interval_ms` | integer | yes | Expected heartbeat interval |
| `fencing_token` | integer | yes | Monotonically increasing token for fencing |

### 6.4 Heartbeat Rules

| Rule | Description |
|---|---|
| Interval | Worker must send heartbeat at configured interval |
| Expiration | Lease expires if heartbeat not received within 2x interval |
| Renewal | Lease is renewed on heartbeat |
| Fencing | Fencing token must increase on each renewal |
| Split-brain | Expired lease owner must not continue execution |

### 6.5 Worker Health

| Check | Description | Failure Action |
|---|---|---|
| Reachable | Worker responds to ping | Mark as potentially unhealthy |
| Responsive | Worker responds within timeout | Mark as degraded |
| Lease valid | Worker lease is current | Worker owns work |
| State consistent | Worker state matches scheduler state | Flag inconsistency |

**Critical rule:** Worker reachability ≠ worker health. A reachable worker may be unhealthy, degraded, or in split-brain state.

---

## 7. Workflow Execution Contract

### 7.1 Workflow Runtime State Machine

```
CREATED → INITIALIZING → RUNNING → {COMPLETED, FAILED, CANCELLED}
                                      ↓
                                 PAUSED → RESUMED → RUNNING
                                      ↓
                                 BLOCKED → UNBLOCKED → RUNNING
```

### 7.2 Workflow Node Execution

For each node in the workflow DAG:

| Stage | Description | Failure |
|---|---|---|
| 1. Node eligible | All dependencies satisfied | Stay QUEUED |
| 2. Node admitted | Resources available, governance passed | BLOCKED |
| 3. Node executing | Task/agent/tool execution | FAILED |
| 4. Node verifying | Output verification | VERIFICATION_FAILED |
| 5. Node completed | Output persisted, next nodes eligible | N/A |

### 7.3 Workflow Context Preservation

Workflow execution MUST preserve:

| Field | Propagation | Checkpoint |
|---|---|---|
| `workflow_id` | All nodes | Yes |
| `task_id` | Current node | Yes |
| `objective_id` | All nodes | Yes |
| `why` | All nodes | Yes |
| `why_chain` | All nodes | Yes |
| `business_id` | All nodes | Yes |
| `division_id` | All nodes | Yes |
| `correlation_id` | All nodes | Yes |
| `causation_id` | Per node | Yes |

### 7.4 Parallel Execution

| Rule | Description |
|---|---|
| Independence | Parallel nodes execute independently |
| Failure isolation | One node failure does not automatically fail others |
| Resource allocation | Parallel nodes may require concurrent resources |
| Completion | All parallel nodes must complete for join |
| Cancellation | Cancelling parent cancels all parallel nodes |

### 7.5 Conditional Branching

| Rule | Description |
|---|---|
| Evaluation | Condition evaluated at runtime using current state |
| Branch selection | Only selected branches execute |
| State propagation | State flows to selected branch |
| False path | Unselected branches are marked as SKIPPED |

### 7.6 Dynamic Replanning

| Trigger | Action | Governance |
|---|---|---|
| Objective change | Replan remaining workflow | Requires approval if critical |
| Resource change | Adjust resource allocation | Automatic if within policy |
| Failure | Attempt replan before fail | Automatic for retryable failures |
| Block | Seek alternative path | Requires approval |

---

## 8. Task Execution Contract

### 8.1 Task Runtime State Machine

```
CREATED → QUEUED → ELIGIBLE → SCHEDULED → ADMITTED → RUNNING
   ↓         ↓         ↓          ↓           ↓         ↓
CANCELLED BLOCKED  DEFERRED   EXPIRED    DENIED    {WAITING, VERIFYING, SUCCEEDED, FAILED, CANCELLED, UNKNOWN}
                                                                      ↓
                                                                 WAITING_FOR_APPROVAL → {APPROVED, DENIED}
```

### 8.2 State Transitions

| From | To | Actor/System | Conditions | Side Effects | Persistence | Events |
|---|---|---|---|---|---|---|
| `CREATED` | `QUEUED` | System | Valid admission | Queue entry | Write queue record | `task.created` |
| `QUEUED` | `ELIGIBLE` | Scheduler | Dependencies met, resources available | Ready signal | Update status | `task.eligible` |
| `ELIGIBLE` | `SCHEDULED` | Scheduler | Worker assigned | Worker notified | Update status + worker | `task.scheduled` |
| `SCHEDULED` | `ADMITTED` | Admission | Final check passed | Admitted | Update status | `task.admitted` |
| `ADMITTED` | `RUNNING` | Worker | Worker starts execution | Execute | Update status + start time | `task.started` |
| `RUNNING` | `WAITING` | Worker | External dependency | Pause | Update status | `task.waiting` |
| `RUNNING` | `VERIFYING` | Worker | Execution complete, verification needed | Verify | Update status | `task.verifying` |
| `RUNNING` | `SUCCEEDED` | Worker | Execution complete, verified | Record outcome | Write outcome | `task.completed` |
| `RUNNING` | `FAILED` | Worker | Execution failed | Record failure | Write error | `task.failed` |
| `RUNNING` | `UNKNOWN` | System | Timeout or crash | Flag unknown | Write unknown outcome | `task.unknown` |
| `RUNNING` | `CANCELLED` | System/Operator | Cancellation requested | Stop execution | Update status | `task.cancelled` |
| `WAITING` | `RUNNING` | System | Dependency resolved | Resume | Update status | `task.resumed` |
| `VERIFYING` | `SUCCEEDED` | Verifier | Verification passed | Record success | Write outcome | `task.verified` |
| `VERIFYING` | `FAILED` | Verifier | Verification failed | Record failure | Write error | `task.verification_failed` |

### 8.3 Invalid Transitions

| Transition | Why Invalid |
|---|---|
| `SUCCEEDED` → `RUNNING` | Cannot un-succeed |
| `FAILED` → `RUNNING` | Cannot un-fail without retry |
| `CANCELLED` → `RUNNING` | Cannot un-cancel |
| `QUEUED` → `RUNNING` | Must go through scheduling |
| Any → `CREATED` | Cannot recreate |

---

## 9. Agent Runtime Contract

### 9.1 Agent Activation Pipeline

```
ACTIVATE → LOAD_IDENTITY → LOAD_AUTHORITY → LOAD_CAPABILITIES → ASSEMBLE_CONTEXT
    ↓                                                                    ↓
LOAD_OBJECTIVE/WHY ←←←←←←←←←←←←←←←←←←←←←←←←←←←←←←←←←←←←←←←←←←←←←←←←←
    ↓
LOAD_MEMORY → CHECK_TOOLS → SELECT_MODEL → READY → EXECUTE
```

### 9.2 Agent Execution Cycle

```
RECEIVE_TASK → UNDERSTAND → PLAN → {EXECUTE_TOOL, INVOKE_MODEL, DELEGATE, WAIT}
                                       ↓                        ↓
                                  RECEIVE_RESULT          RECEIVE_RESPONSE
                                       ↓                        ↓
                                  EVALUATE → {CONTINUE, COMPLETE, FAIL, ESCALATE}
```

### 9.3 Agent Runtime Rules

| Rule | Description |
|---|---|
| Identity first | Agent identity loaded from Identity system |
| Authority check | Agent authority verified before each action |
| Capability check | Agent capability verified before tool/model use |
| WHY preservation | Agent must maintain objective context throughout |
| Memory scope | Agent memory access follows scope rules |
| Delegation | Agent may delegate to child agents (with authority) |
| Child agent | Child agent inherits scope, not authority |
| Temporary TTL | Temporary agents must respect TTL |
| Max tasks | Agent must not exceed max concurrent tasks |

### 9.4 Agent Delegation

| Field | Type | Required | Description |
|---|---|---|---|
| `parent_agent_id` | string | yes | Delegating agent |
| `child_agent_id` | string | yes | Receiving agent |
| `task_id` | string | yes | Task being delegated |
| `objective_id` | string | yes | Objective being served |
| `constraints` | list[string] | no | Delegation constraints |
| `authority_transfer` | boolean | yes | Whether authority is transferred (usually false) |

### 9.5 Delegation Rules

| Rule | Description |
|---|---|
| Authority check | Delegation requires authority to delegate |
| Scope preservation | Child inherits parent's scope, not authority |
| WHY propagation | WHY chain includes delegation reason |
| Audit | Delegation is auditable |
| Reclaim | Parent may reclaim delegated work |

---

## 10. Temporary Agent Contract

### 10.1 Temporary Agent Lifecycle

```
SPAWN → INITIALIZE → READY → ACTIVE → {EXPIRED, TERMINATED}
                                   ↓
                              MAX_TASKS_REACHED → TERMINATED
```

### 10.2 Temporary Agent Properties

| Field | Type | Required | Description |
|---|---|---|---|
| `parent_id` | string | yes | Spawner agent or human |
| `creation_reason` | string | yes | Why this agent was created |
| `objective_id` | string | yes | Objective being served |
| `business_id` | string | yes | Business scope |
| `division_id` | string | no | Division scope |
| `ttl_seconds` | integer | yes | Maximum lifetime |
| `max_tasks` | integer | no | Maximum tasks to execute |
| `max_tokens` | integer | no | Maximum token budget |
| `resource_limits` | ResourceLimits | no | Resource consumption limits |
| `allowed_capabilities` | list[string] | yes | What this agent can do |
| `allowed_permissions` | list[string] | yes | What this agent is permitted to do |

### 10.3 Expiration Rules

| Rule | Description |
|---|---|
| Deterministic | Expiration is deterministic (TTL based) |
| No orphans | Expiration must not leave orphaned execution |
| Cleanup | On expiration: stop execution, release resources, transfer artifacts |
| Audit | Expiration is auditable |
| Force terminate | System may force terminate if cleanup fails |

### 10.4 Artifact Ownership

| Phase | Ownership |
|---|---|
| During execution | Temporary agent owns artifacts |
| On expiration | Artifacts transferred to parent or business scope |
| On failure | Artifacts transferred to parent (if recoverable) |

---

## 11. Model Invocation Contract

### 11.1 Model Invocation Pipeline

```
REQUEST → VALIDATE → ROUTE → SELECT_PROVIDER → SELECT_MODEL → PREPARE_CONTEXT → INVOKE → VALIDATE_OUTPUT → RETURN
```

### 11.2 Model Invocation State Machine

```
PENDING → ROUTING → INVOKING → {COMPLETED, FAILED, TIMEOUT, CANCELLED}
                                   ↓
                              STREAMING → COMPLETED
```

### 11.3 Routing Decision

| Factor | Weight | Description |
|---|---|---|
| Capability match | High | Model must support required capability |
| Privacy | High | Data residency requirements |
| Cost | Medium | Budget constraints |
| Latency | Medium | Response time requirements |
| Local preference | Configurable | Prefer local models when possible |
| Fallback | Automatic | Fallback to alternative on failure |

### 11.4 Model Failure Categories

| Category | Description | Retryable | Fallback |
|---|---|---|---|
| Provider unavailable | Provider is down | Yes (backoff) | Yes |
| Model unavailable | Specific model unavailable | Yes (backoff) | Yes |
| Invalid request | Bad prompt/context | No | No |
| Timeout | Response too slow | Maybe | Yes |
| Rate limit | Too many requests | Yes (backoff) | Maybe |
| Context limit | Prompt too large | No (resize) | No |
| Malformed output | Output doesn't match expected format | Maybe | No |
| Policy rejection | Governance blocked | No | No |
| Security rejection | Security boundary violation | No | No |
| Unknown outcome | Uncertain if succeeded | No (reconcile) | No |

### 11.5 Token Accounting

| Field | Type | Required | Description |
|---|---|---|---|
| `input_tokens` | integer | yes | Tokens in prompt |
| `output_tokens` | integer | yes | Tokens in response |
| `total_tokens` | integer | yes | Total tokens |
| `cost_estimate` | float | no | Estimated cost |
| `budget_remaining` | float | no | Remaining budget |

---

## 12. Tool Invocation Contract

### 12.1 Tool Invocation Pipeline

```
AGENT_REQUEST → VALIDATE_INPUT → AUTHORIZE → SCOPE_CHECK → POLICY_CHECK → RESOURCE_CHECK → CREDENTIAL_RESOLVE → EXECUTE → VALIDATE_OUTPUT → AUDIT → RETURN
```

### 12.2 Tool Execution State Machine

```
PENDING → VALIDATING → AUTHORIZING → EXECUTING → {COMPLETED, FAILED, TIMEOUT, CANCELLED}
                                                       ↓
                                                  VERIFYING → VERIFIED / REJECTED
```

### 12.3 Tool Execution Rules

| Rule | Description |
|---|---|
| Boundary enforcement | Tool Runtime is the controlled execution boundary |
| Authorization | Must pass authorization before execution |
| Credential isolation | Credentials resolved at runtime, never embedded |
| Untrusted output | Tool results are untrusted external input |
| Side-effect tracking | Side effects classified (none, read_only, reversible, irreversible) |
| Idempotency | Irreversible side effects require idempotency_key |
| Dry run | Support dry_run for previewing side effects |

### 12.4 Tool Result Classification

| Classification | Description | Handling |
|---|---|---|
| `none` | No side effects | Safe to retry |
| `read_only` | Read-only operation | Safe to retry |
| `reversible` | Can be undone | Retry with compensation |
| `irreversible` | Cannot be undone | Require idempotency_key, reconcile on unknown |

---

## 13. API / External Execution Contract

### 13.1 External API Pipeline

```
REQUEST → VALIDATE → AUTHENTICATE → AUTHORIZE → IDEMPOTENCY_CHECK → EXECUTE → VALIDATE_RESPONSE → RETURN
```

### 13.2 External Execution State Machine

```
PENDING → AUTHENTICATING → EXECUTING → {COMPLETED, FAILED, TIMEOUT, CANCELLED, UNKNOWN}
```

### 13.3 External Execution Rules

| Rule | Description |
|---|---|
| Authentication | External auth performed by API Gateway |
| Idempotency | Idempotency key sent with request |
| Timeout | Configurable per endpoint |
| Retry | Retry per policy (not automatic) |
| Rate limiting | Respect rate limits |
| Circuit breaking | Circuit breaker for failing endpoints |
| Response validation | Validate response against expected schema |
| Unknown outcome | Timeout after potential side effect = UNKNOWN_OUTCOME |

### 13.4 Circuit Breaker States

```
CLOSED → OPEN → HALF_OPEN → {CLOSED, OPEN}
```

| State | Description |
|---|---|
| `CLOSED` | Normal operation, requests pass through |
| `OPEN` | Failures exceeded threshold, requests rejected |
| `HALF_OPEN` | Testing if service recovered |

---

## 14. Verification Contract

### 14.1 Verification Pipeline

```
EXECUTED → VERIFY → {VERIFIED_SUCCESS, VERIFIED_FAILURE, UNKNOWN}
```

### 14.2 Verification Methods

| Method | Description | When Used |
|---|---|---|
| Direct response | Check execution response | Simple operations |
| State query | Query external state | State-changing operations |
| Artifact inspection | Verify produced artifacts | Generation tasks |
| Invariant check | Check system invariants | Critical operations |
| Business rule | Validate business rules | Domain operations |
| Independent validation | Separate validation step | High-stakes operations |

### 14.3 Verification Rules

| Rule | Description |
|---|---|
| Separation | Verification is separate from execution |
| Independence | Verifier should be independent of executor |
| Time-bound | Verification must complete within timeout |
| Evidence | Verification produces evidence |
| Audit | Verification result is auditable |

---

## 15. Outcome Contract

### 15.1 Outcome Recording Pipeline

```
VERIFIED → RECORD_OUTCOME → PERSIST → EMIT_EVENT → NOTIFY
```

### 15.2 Outcome Composition

| Field | Source | Required |
|---|---|---|
| `execution_ref` | Execution record | Yes |
| `task_ref` | Task schema | Yes |
| `workflow_ref` | Workflow schema | Conditionally |
| `objective_ref` | Objective schema | Yes |
| `expected_result` | Original request | Yes |
| `actual_result` | Execution output | Yes |
| `verification` | Verification record | Yes |
| `evidence` | Verification evidence | Yes |
| `provenance` | Execution provenance | Yes |
| `side_effects` | Side-effect tracker | Yes |
| `failures` | Error records | Conditionally |
| `follow_up` | Replan/reference | Conditionally |

### 15.3 Outcome Rules

| Rule | Description |
|---|---|
| Evidence-based | Outcome requires evidence |
| Not authority | Outcome does not authorize further action |
| Feeds evaluation | Outcome feeds Objective evaluation |
| Audit | Outcome is auditable |
| Immutable | Outcome is immutable once recorded |

---

## 16. Event Emission Contract

### 16.1 Runtime Event Requirements

Every significant runtime transition must emit an event compatible with Phase 2 Event Schemas.

### 16.2 Required Events

| Transition | Event Type | Required Fields |
|---|---|---|
| Task created | `task.created` | task_id, workflow_id, objective_id |
| Task started | `task.started` | task_id, agent_id, worker_id |
| Task completed | `task.completed` | task_id, outcome_id |
| Task failed | `task.failed` | task_id, error_id |
| Task timeout | `task.timeout` | task_id, timeout_duration |
| Task cancelled | `task.cancelled` | task_id, cancellation_reason |
| Workflow started | `workflow.started` | workflow_id, objective_id |
| Workflow completed | `workflow.completed` | workflow_id, outcome_id |
| Workflow failed | `workflow.failed` | workflow_id, error_id |
| Agent activated | `agent.started` | agent_id, objective_id |
| Tool executed | `tool.completed` | tool_execution_id, result_status |
| Model invoked | `model.completed` | model_invocation_id, tokens |
| Approval requested | `approval.requested` | approval_id, requester_id |
| Approval decided | `approval.approved` or `approval.denied` | approval_id, decision |

### 16.3 Event Rules

| Rule | Description |
|---|---|
| Preserve context | Events carry correlation_id, causation_id, business_id |
| WHY preserved | Events carry objective_id |
| No authority | Events do not grant authority |
| Async emission | Events emitted async, must not block execution |
| Durable | Critical events persisted before considered emitted |

---

*This document defines the core runtime execution contracts. Failure, recovery, and resilience contracts are in RUNTIME_FAILURE_RECOVERY.md.*
