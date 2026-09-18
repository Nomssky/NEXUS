# NEXUS — Failure, Recovery & Resilience Contracts

**Layer:** Phase 3 — Runtime & Execution Contracts  
**Status:** LOCKED  
**Branch:** contracts/runtime-execution  
**Depends on:** Phase 1 Core Interface Contracts, Phase 2 Data & Event Schemas, RUNTIME_EXECUTION_CONTRACTS.md  

---

## 1. Purpose

This document defines **failure handling, recovery, and resilience contracts** for NEXUS runtime execution. It covers retry, timeout, cancellation, leases/heartbeats, recovery, checkpointing, reconciliation, compensation, concurrency, backpressure, graceful shutdown, and failure isolation.

---

## 2. Retry Contract

### 2.1 Retry Ownership

Every operation MUST have **one authoritative retry owner**. There must be no nested independent retry loops (e.g., agent retry + tool retry + API retry + provider retry all independently retrying).

| Operation | Retry Owner | Notes |
|---|---|---|
| Task execution | Workflow Orchestration | Reassign or escalate |
| Tool execution | Agent Runtime | Retry with backoff |
| API call | API Gateway | Retry per endpoint policy |
| Model invocation | Model Router | Fallback to alternative provider |
| External webhook | API Gateway | Retry per webhook policy |
| Worker failure | Scheduler | Reassign to different worker |

### 2.2 Retry Classification

| Classification | Description | Safe to Retry? |
|---|---|---|
| `safe` | No side effects (read operations) | Yes |
| `conditionally_safe` | Side effects with idempotency_key | Yes (with key) |
| `unsafe` | Side effects without idempotency | No (reconcile first) |

### 2.3 Retry Configuration

| Field | Type | Required | Description |
|---|---|---|---|
| `max_attempts` | integer | yes | Maximum retry attempts (including initial) |
| `backoff_type` | enum | yes | One of: `fixed`, `exponential`, `custom` |
| `initial_delay_ms` | integer | yes | Initial delay before first retry |
| `max_delay_ms` | integer | no | Maximum delay cap |
| `jitter` | float | no | Random jitter factor (0.0-1.0) |
| `retry_budget` | integer | no | Maximum retries per time window |
| `retry_deadline` | datetime | no | Stop retrying after this time |
| `retry_owner` | string | yes | Who owns retry logic |
| `retryable_errors` | list[string] | no | Error categories that trigger retry |

### 2.4 Backoff Algorithms

| Algorithm | Formula | Use Case |
|---|---|---|
| `fixed` | delay = initial_delay | Simple retry |
| `exponential` | delay = initial_delay * 2^attempt | Network, provider retry |
| `custom` | User-defined | Specialized needs |

### 2.5 Retry Rules

| Rule | Description |
|---|---|
| Single owner | One retry owner per operation |
| No nested retry | No independent retry loops within retry loops |
| Budget | Retry budget prevents infinite retry |
| Deadline | Retry deadline prevents endless retry |
| Observe errors | Only retry retryable errors |
| Log retries | Every retry attempt is logged |
| Track attempts | Attempt count tracked and observable |
| Unknown outcome | UNKNOWN_OUTCOME must not be blindly retried |

---

## 3. Timeout Contract

### 3.1 Timeout Layers

| Layer | Owner | Default | Description |
|---|---|---|---|
| Queue timeout | Scheduler | Configurable | Time in queue before expiry |
| Admission timeout | Admission | Configurable | Time for admission pipeline |
| Execution timeout | Worker | Per-task | Time for task execution |
| Tool timeout | Tool Runtime | Per-tool | Time for tool execution |
| API timeout | API Gateway | Per-endpoint | Time for external API call |
| Model timeout | Model Router | Per-model | Time for model inference |
| Verification timeout | Verifier | Configurable | Time for verification |
| Workflow deadline | Workflow | Per-workflow | Overall workflow deadline |

### 3.2 Timeout Propagation

| Rule | Description |
|---|---|
| Parent-child | Child timeout ≤ Parent timeout - buffer |
| No infinite loop | Child timeout must not cause infinite parent execution |
| Buffer | Reserve time for post-execution stages |
| Cascade | Parent timeout cancels children |
| Observable | Timeout generates observable state/event |

### 3.3 Timeout Handling

| Scenario | Behavior |
|---|---|
| Execution timeout | Task marked TIMEOUT, may retry |
| Tool timeout | Tool marked TIMEOUT, agent decides next |
| API timeout | API marked TIMEOUT, may reconcile |
| Model timeout | Model marked TIMEOUT, fallback or fail |
| Workflow deadline | Workflow marked EXPIRED, cancel children |

### 3.4 Timeout States

| State | Description | Next |
|---|---|---|
| `NOT_STARTED` | Timeout not yet counting | Running |
| `RUNNING` | Timeout counting | Expired or Cancelled |
| `EXPIRED` | Timeout reached | Handler |
| `CANCELLED` | Timeout cancelled (operation completed) | Terminal |

---

## 4. Cancellation Contract

### 4.1 Cancellation State Machine

```
REQUESTED → ACCEPTED → PROPAGATING → CANCELLED
                         ↓
                    CANCELLATION_TIMEOUT → CANCELLATION_FAILED
                         ↓
                    ALREADY_COMPLETED / ALREADY_UNKNOWN
```

### 4.2 Cancellation States

| State | Description |
|---|---|
| `REQUESTED` | Cancellation requested |
| `ACCEPTED` | Cancellation accepted for processing |
| `PROPAGATING` | Cancellation propagating to children/workers |
| `CANCELLED` | All work cancelled |
| `CANCELLATION_TIMEOUT` | Cancellation propagation timed out |
| `CANCELLATION_FAILED` | Cancellation could not complete |
| `ALREADY_COMPLETED` | Work already completed before cancellation |
| `ALREADY_UNKNOWN` | Work outcome unknown before cancellation |

### 4.3 Cancellation Rules

| Rule | Description |
|---|---|
| Not failure | Cancellation is not the same as failure |
| Governance | Cancellation must respect Governance |
| Irreversible ops | External irreversible operations may not be cancellable |
| Propagation | Cancellation propagates downstream |
| Acknowledge | Each component must acknowledge cancellation |
| Partial | Partial results preserved unless policy dictates otherwise |
| Audit | Cancellation is auditable |
| WHY preserved | WHY context preserved through cancellation |

### 4.4 Cancellation Handling

| Component | Behavior on Cancellation |
|---|---|
| Workflow | Cancel all active nodes, preserve checkpoint |
| Task | Stop execution, record partial results |
| Agent | Stop current action, release resources |
| Tool | Attempt graceful stop, record side effects |
| Model | Cancel inference, discard partial output |
| External API | Attempt cancellation if supported, else wait |

---

## 5. Lease + Heartbeat Contract

### 5.1 Lease Semantics

| Field | Type | Required | Description |
|---|---|---|---|
| `lease_id` | string | yes | Unique lease identifier |
| `owner` | string | yes | Worker/entity holding lease |
| `resource` | string | yes | What lease covers |
| `issued_at` | datetime | yes | Issue time |
| `expires_at` | datetime | yes | Expiration time |
| `heartbeat_interval_ms` | integer | yes | Expected heartbeat interval |
| `fencing_token` | integer | yes | Monotonically increasing token |
| `renewable` | boolean | yes | Whether lease can be renewed |

### 5.2 Heartbeat Semantics

| Field | Type | Required | Description |
|---|---|---|---|
| `lease_id` | string | yes | Lease being renewed |
| `fencing_token` | integer | yes | New fencing token (must increase) |
| `worker_status` | enum | yes | One of: `healthy`, `degraded`, `busy` |
| `progress` | float | no | Execution progress (0.0-1.0) |

### 5.3 Lease Rules

| Rule | Description |
|---|---|
| Expiration | Lease expires if heartbeat not received |
| Fencing | Fencing token must increase on renewal |
| Split-brain | Expired lease owner must not continue |
| Recovery | Expired lease resources recoverable |
| Observable | Lease state is observable |

### 5.4 Split-Brain Prevention

| Mechanism | Description |
|---|---|
| Fencing token | Workers must present fencing token for operations |
| Token check | Storage/resources reject operations with stale token |
| Lease expiry | Expired lease invalidates all associated tokens |
| Recovery | New lease gets higher fencing token |

---

## 6. Recovery Contract

### 6.1 Recovery Scenarios

| Scenario | Detection | Recovery |
|---|---|---|
| Worker crash | Heartbeat failure | Reassign lease, resume or restart |
| Process crash | Process monitoring | Recover from checkpoint |
| Machine restart | Health check | Recover from durable state |
| Network failure | Connection timeout | Retry with backoff |
| Provider outage | Health check | Fallback to alternative |
| Database interruption | Connection pool | Queue operations, retry |
| Queue interruption | Queue health | Buffer locally, retry |
| External dependency failure | Dependency check | Circuit breaker, degrade |

### 6.2 Recovery State Classification

| State | Description | Action |
|---|---|---|
| `KNOWN_COMPLETED` | Execution completed successfully | No action needed |
| `KNOWN_FAILED` | Execution failed definitively | Record failure |
| `IN_PROGRESS` | Execution still running | Reattach or wait |
| `UNKNOWN` | Outcome uncertain | Reconcile |

### 6.3 Recovery Rules

| Rule | Description |
|---|---|
| No duplicate side effects | Recovery must not cause duplicate irreversible operations |
| Checkpoint first | Recover from last checkpoint |
| Reconcile unknown | UNKNOWN state must be reconciled |
| Preserve context | Recovery preserves objective/WHY context |
| Audit | Recovery is auditable |
| Observable | Recovery generates observability signals |

---

## 7. Checkpoint + Resume Contract

### 7.1 Checkpoint Structure

| Field | Type | Required | Description |
|---|---|---|---|
| `checkpoint_id` | string | yes | Unique checkpoint identifier |
| `workflow_id` | string | yes | Workflow being checkpointed |
| `task_id` | string | no | Task being checkpointed (if task-level) |
| `objective_id` | string | yes | Objective context |
| `why` | string | yes | WHY context |
| `completed_work` | list[string] | yes | IDs of completed work items |
| `pending_work` | list[string] | yes | IDs of pending work items |
| `state` | map[string,any] | yes | Current state snapshot |
| `artifacts` | list[string] | no | Artifact IDs produced |
| `context_refs` | list[string] | no | Context references |
| `side_effect_refs` | list[string] | no | External side-effect references |
| `version` | integer | yes | Checkpoint version |
| `created_at` | datetime | yes | Checkpoint time |

### 7.2 Checkpoint Rules

| Rule | Description |
|---|---|
| Durability | Checkpoint must be durable before considered saved |
| Frequency | Checkpoint at configurable intervals |
| Critical transitions | Checkpoint before critical state changes |
| WHY preserved | WHY context included in checkpoint |
| Compatibility | Resume verifies compatibility with current config/policy |
| Version | Checkpoint version for resume compatibility |

### 7.3 Resume Rules

| Rule | Description |
|---|---|
| Verify compatibility | Check workflow/task/agent still compatible |
| Verify policy | Check governance/policy still valid |
| Verify resources | Check resources still available |
| Verify scope | Check business/division scope still valid |
| Reconnect | Reconnect to external dependencies |
| Continue | Resume from checkpoint state |

---

## 8. Reconciliation Contract

### 8.1 Reconciliation Trigger

Reconciliation is triggered when:
- Local state ≠ expected state
- API timeout after potential side effect
- Worker crash after external operation
- Lost acknowledgement
- Duplicate request uncertainty
- Provider interruption

### 8.2 Reconciliation Pipeline

```
DETECT_MISMATCH → QUERY_AUTHORITATIVE → COMPARE → RESOLVE → RECORD_EVIDENCE → EMIT
```

### 8.3 Reconciliation States

| State | Description |
|---|---|
| `PENDING` | Reconciliation needed |
| `QUERYING` | Querying authoritative source |
| `COMPARING` | Comparing expected vs actual |
| `RESOLVING` | Applying resolution |
| `RESOLVED` | State reconciled |
| `UNRESOLVABLE` | Cannot reconcile automatically |

### 8.4 Reconciliation Rules

| Rule | Description |
|---|---|
| Identify unknown | Clearly identify unknown state |
| Query source | Query authoritative source where possible |
| Compare | Compare expected vs actual state |
| Resolve | Apply resolution (accept, reject, adjust) |
| Record evidence | Record reconciliation evidence |
| Prevent duplicates | Prevent unsafe duplicate execution |
| Audit | Reconciliation is auditable |

---

## 9. Compensation Contract

### 9.1 Compensation vs Rollback vs Retry

| Mechanism | Description | When Used |
|---|---|---|
| Rollback | Undo to previous state | When possible and safe |
| Compensation | Apply inverse operation | When rollback impossible |
| Reconciliation | Query and adjust state | When uncertain |
| Retry | Re-execute operation | When safe and retryable |

### 9.2 Compensation Rules

| Rule | Description |
|---|---|
| Not rollback | Compensation is not equivalent to rollback |
| Governance | Compensation must pass Governance |
| Authorization | Compensation requires authorization |
| Audit | Compensation is auditable |
| Evidence | Compensation produces evidence |
| Scope | Compensation respects business/division scope |

### 9.3 Compensation Record

| Field | Type | Required | Description |
|---|---|---|---|
| `compensation_id` | string | yes | Unique identifier |
| `original_operation` | string | yes | What was originally done |
| `compensation_operation` | string | yes | What compensates |
| `reason` | string | yes | Why compensation is needed |
| `authorization_ref` | string | yes | Authorization for compensation |
| `status` | enum | yes | One of: `pending`, `executing`, `completed`, `failed` |
| `evidence` | string | no | Evidence of compensation |

---

## 10. Concurrency + Locking Contract

### 10.1 Concurrency Control Mechanisms

| Mechanism | Description | Use Case |
|---|---|---|
| Optimistic concurrency | Version check on write | Most entity updates |
| Lease-based | Lease ownership | Worker task assignment |
| Fencing token | Token-based access | Distributed coordination |
| Pessimistic lock | Exclusive lock | Rare, critical sections |

### 10.2 Optimistic Concurrency

| Field | Type | Required | Description |
|---|---|---|---|
| `entity_id` | string | yes | Entity being updated |
| `expected_version` | integer | yes | Version expected |
| `actual_version` | integer | yes | Version found |
| `conflict` | boolean | yes | Whether conflict occurred |

### 10.3 Concurrency Rules

| Rule | Description |
|---|---|
| Version check | Optimistic concurrency check on all writes |
| Conflict handling | Conflict → retry or fail per policy |
| No global locks | Avoid global locks unless necessary |
| Isolation | Business isolation prevents cross-business races |
| Observable | Lock state is observable |

### 10.4 Race Condition Prevention

| Race | Prevention |
|---|---|
| Double execution | Idempotency key + version check |
| Stale writes | Version check on write |
| Lost updates | Optimistic concurrency |
| Cross-business | Business scope isolation |

---

## 11. Backpressure Contract

### 11.1 Backpressure Signals

| Signal | Description | Response |
|---|---|---|
| Queue growth | Queue exceeding threshold | Slow admission |
| Resource exhaustion | Resources fully allocated | Deny new admission |
| Worker saturation | All workers busy | Queue work |
| Rate limit | Rate limit exceeded | Throttle |
| Memory pressure | Memory usage high | Reduce context |
| Cost budget | Budget approaching limit | Reduce non-critical work |

### 11.2 Backpressure Actions

| Action | Description | When |
|---|---|---|
| Throttle | Reduce admission rate | Moderate pressure |
| Defer | Defer non-critical work | High pressure |
| Shed | Drop lowest priority work | Critical pressure |
| Degrade | Reduce quality/capability | Sustained pressure |
| Alert | Generate Attention event | Critical pressure |

### 11.3 Backpressure Rules

| Rule | Description |
|---|---|
| Observable | Backpressure signals are observable |
| Explicit | Dropped/deferred work has explicit semantics |
| Priority | Higher priority work protected longer |
| Business isolation | Backpressure per business, not global |
| Emergency | Emergency work bypasses backpressure |

---

## 12. Graceful Shutdown Contract

### 12.1 Shutdown Pipeline

```
STOP_ACCEPTING → DRAIN → CHECKPOINT → FINISH_SAFE → CANCEL_IF_NEEDED → RELEASE_LEASES → PERSIST_STATE → REPORT → STOP
```

### 12.2 Shutdown States

| State | Description |
|---|---|
| `STOP_ACCEPTING` | No new work accepted |
| `DRAINING` | Finishing current work |
| `CHECKPOINTING` | Saving state |
| `FINISHING` | Completing safe operations |
| `CANCELLING` | Cancelling unsafe operations |
| `RELEASING` | Releasing leases and resources |
| `PERSISTING` | Persisting final state |
| `REPORTING` | Reporting shutdown status |
| `STOPPED` | Fully stopped |

### 12.3 Shutdown Rules

| Rule | Description |
|---|---|
| No new work | Stop accepting new work first |
| Drain | Finish work that can be safely completed |
| Checkpoint | Checkpoint before stopping |
| Release leases | Release all leases |
| Persist state | Persist all durable state |
| Report | Report shutdown status |
| No ambiguity | Shutdown must not create ambiguous ownership |
| Observable | Shutdown is observable |

---

## 13. Failure Isolation Contract

### 13.1 Isolation Boundaries

| Boundary | Isolation Rule |
|---|---|
| Business | Business failure does not affect other businesses |
| Division | Division failure does not affect other divisions |
| Workflow | Workflow failure does not affect other workflows |
| Agent | Agent failure does not necessarily fail parent workflow |
| Worker | Worker failure does not affect other workers |
| Tool | Tool failure does not affect other tools |
| Model provider | Provider failure triggers fallback |
| API connector | Connector failure triggers circuit breaker |

### 13.2 Isolation Rules

| Rule | Description |
|---|---|
| Scope isolation | Failures respect business/division scope |
| Blast radius | Minimize failure blast radius |
| Degradation | Prefer degradation over total failure |
| Recovery | Isolated failures recover independently |
| Observable | Failure isolation is observable |

### 13.3 Failure Propagation

| Scenario | Propagation |
|---|---|
| Tool fails | Agent decides next (retry, alternative, fail task) |
| Model fails | Agent decides next (fallback model, fail task) |
| Worker fails | Scheduler reassigns |
| Temporary agent fails | Parent workflow may fail or replan |
| Workflow fails | Objective may be reprioritized |

---

*This document defines failure handling, recovery, and resilience contracts. Runtime invariants and test scenarios are in RUNTIME_INVARIANTS.md.*
