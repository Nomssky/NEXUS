# NEXUS — Runtime Invariants & Test Scenarios

**Layer:** Phase 3 — Runtime & Execution Contracts  
**Status:** LOCKED  
**Branch:** contracts/runtime-execution  
**Depends on:** Phase 1 Core Interface Contracts, Phase 2 Data & Event Schemas, RUNTIME_EXECUTION_CONTRACTS.md, RUNTIME_FAILURE_RECOVERY.md  

---

## 1. Purpose

This document defines **runtime invariants** (non-negotiable behavioral properties) and **test scenarios** for validating NEXUS runtime execution contracts. Every implementation must enforce these invariants and pass these test scenarios.

---

## 2. Runtime Invariants

### RT-01: No Execution Without Valid Admission

**Rule:** No work may be executed without passing through the complete admission pipeline.

**Enforcement:**
- Admission pipeline stages: VALIDATE → IDENTITY → AUTHORIZATION → POLICY → APPROVAL → RESOURCE CHECK → SCHEDULE → ADMIT
- No stage may be skipped
- Admission rejection is explicit with reason code

**Violation:** Work executing without admission record.

---

### RT-02: No Admission Without Required Authorization/Governance

**Rule:** Admission MUST NOT proceed without required authorization and governance checks.

**Enforcement:**
- Authorization check before admission
- Governance policy evaluation before admission
- Approval gate before admission (if required)
- Default deny when authorization/governance unavailable

**Violation:** Work admitted without authorization record.

---

### RT-03: No Authority From Events

**Rule:** Events are facts, not commands. Events do not grant authority.

**Enforcement:**
- Events carry information, not permission
- Events may trigger decisions, not execute actions
- Governance must evaluate before any action based on event

**Violation:** Action taken based solely on event, without governance check.

---

### RT-04: No Authority From Attention

**Rule:** Attention items may request attention but do not grant permission.

**Enforcement:**
- Attention items carry no authority fields
- Attention may trigger governance escalation
- Attention resolution ≠ authorization

**Violation:** Action taken based solely on attention item.

---

### RT-05: No Authority From Objectives

**Rule:** Objectives explain WHY, not what is permitted.

**Enforcement:**
- Objectives carry no authority fields
- Objectives explain purpose
- Authority resolved separately by governance

**Violation:** Action taken based solely on objective context.

---

### RT-06: No Credentials in Generic Runtime Objects

**Rule:** Raw credentials (passwords, API keys, tokens, private keys) are prohibited in runtime objects.

**Enforcement:**
- Credential references used, not raw values
- Credential resolution at runtime
- Credential isolation in secure vault

**Violation:** Raw credential found in runtime object.

---

### RT-07: Unknown Outcome Remains Unknown Until Reconciled

**Rule:** When execution outcome is uncertain, it MUST remain `UNKNOWN_OUTCOME` until reconciled.

**Enforcement:**
- Timeout after potential side effect = UNKNOWN_OUTCOME
- UNKNOWN_OUTCOME not automatically converted to failure
- Reconciliation required before retry
- Blind retry prohibited for UNKNOWN_OUTCOME

**Violation:** UNKNOWN_OUTCOME converted to failure without reconciliation.

---

### RT-08: Retry Must Have One Authoritative Owner

**Rule:** Each operation has one retry owner. No nested independent retry loops.

**Enforcement:**
- Retry owner defined per operation type
- No agent retry + tool retry + API retry + provider retry independently
- Single retry policy per operation

**Violation:** Multiple independent retry loops on same operation.

---

### RT-09: Cancellation ≠ Failure

**Rule:** Cancellation is not the same as failure. They have different semantics.

**Enforcement:**
- Cancellation: requested by authority, graceful stop
- Failure: operation could not complete
- Different state machines, different event types
- Partial results preserved differently

**Violation:** Cancellation recorded as failure.

---

### RT-10: Verification ≠ Execution

**Rule:** Successful execution does not imply verified success. Verification is separate.

**Enforcement:**
- Execution produces result
- Verification validates result
- Different stages, different actors
- Verification may fail even if execution succeeded

**Violation:** Execution result treated as verified without verification stage.

---

### RT-11: Outcome ≠ Authorization

**Rule:** Outcome does not authorize further action. Authorization comes from Governance.

**Enforcement:**
- Outcome records what happened
- Further action requires new authorization
- Outcome feeds objective evaluation, not authorization

**Violation:** Outcome used as authorization for next action.

---

### RT-12: Runtime State ≠ Durable State

**Rule:** Runtime state (in-memory) is distinct from durable state (persisted). Crashes may lose runtime state.

**Enforcement:**
- Critical transitions persisted before considered durable
- Checkpoint for recovery
- Runtime reconstructable from durable state

**Violation:** Critical state lost on crash without durable record.

---

### RT-13: Worker Reachability ≠ Worker Health

**Rule:** A reachable worker may be unhealthy, degraded, or in split-brain state.

**Enforcement:**
- Health check includes reachability + responsiveness + lease validity + state consistency
- Lease-based ownership with fencing
- Worker status reported, not assumed

**Violation:** Worker assumed healthy solely because reachable.

---

### RT-14: Resource Availability ≠ Permission

**Rule:** Having resources does not grant permission to use them.

**Enforcement:**
- Resource allocation separate from authorization
- Authorization check before resource use
- Resource availability checked after authorization

**Violation:** Work executed with resources but without authorization.

---

### RT-15: Model Identity ≠ Agent Identity

**Rule:** Model output is inference, not the agent's own knowledge.

**Enforcement:**
- Model invocation tracked separately from agent
- Model output marked as inference in provenance
- Agent verifies/model output before treating as fact

**Violation:** Agent treating model output as its own verified knowledge.

---

### RT-16: Business Isolation Preserved

**Rule:** Business A's execution must not affect Business B's execution.

**Enforcement:**
- Business scope on all work items
- Resource allocation per business
- Failure isolation per business
- No cross-business side effects without governance

**Violation:** Business A's failure affecting Business B.

---

### RT-17: WHY Survives Long-Running Execution

**Rule:** WHY context must be preserved through long-running workflows.

**Enforcement:**
- WHY included in checkpoints
- WHY propagated to child workflows/tasks
- WHY not discarded without governance approval
- WHY available throughout execution

**Violation:** WHY lost during long-running workflow.

---

### RT-18: External Side Effects Require Idempotency/Reconciliation

**Rule:** External side-effecting operations must have idempotency or reconciliation strategy.

**Enforcement:**
- Irreversible side effects require idempotency_key
- UNKNOWN_OUTCOME triggers reconciliation
- No blind retry of side-effecting operations

**Violation:** Blind retry of external side-effecting operation.

---

### RT-19: Temporary Agents Cannot Outlive Defined Lifecycle

**Rule:** Temporary agents must expire deterministically.

**Enforcement:**
- TTL enforced at creation
- Expiration is deterministic
- No orphaned execution on expiration
- Cleanup on expiration

**Violation:** Temporary agent executing beyond TTL.

---

### RT-20: Observability Cannot Grant Authority

**Rule:** Observability data (metrics, logs, traces) cannot be used as authority.

**Enforcement:**
- Observability is read-only
- Observability does not affect authorization decisions
- Observability is for visibility only

**Violation:** Observability data used as authorization.

---

## 3. Test Scenarios

### Scenario 1: Normal Task Execution

| Aspect | Expected |
|---|---|
| **Precondition** | Valid task request, authorized actor, resources available |
| **Input** | Task with valid schema, objective, business scope |
| **Transition** | CREATED → QUEUED → ELIGIBLE → SCHEDULED → ADMITTED → RUNNING → VERIFYING → SUCCEEDED |
| **Postcondition** | Task completed, outcome recorded, events emitted |
| **Persistence** | Task state, outcome, audit record |
| **Events** | task.created, task.started, task.completed |
| **Security** | Authorization verified, scope enforced |

### Scenario 2: Task Timeout

| Aspect | Expected |
|---|---|
| **Precondition** | Task running, timeout configured |
| **Input** | Task exceeds execution timeout |
| **Transition** | RUNNING → TIMEOUT |
| **Postcondition** | Task marked TIMEOUT, partial results preserved |
| **Persistence** | Timeout state, partial results |
| **Events** | task.timeout |
| **Security** | Timeout does not bypass governance |

### Scenario 3: Worker Crash

| Aspect | Expected |
|---|---|
| **Precondition** | Worker executing task, heartbeat configured |
| **Input** | Worker stops sending heartbeats |
| **Transition** | Worker lease expires, task reassigned |
| **Postcondition** | Task resumed on new worker or marked UNKNOWN |
| **Persistence** | Checkpoint state, lease expiry |
| **Events** | worker.expired, task.reassigned |
| **Security** | Expired worker cannot continue |

### Scenario 4: Duplicate Event

| Aspect | Expected |
|---|---|
| **Precondition** | Event delivered twice (at-least-once) |
| **Input** | Same event_id delivered twice |
| **Transition** | First delivery processed, second detected as duplicate |
| **Postcondition** | Event processed once, duplicate ignored |
| **Persistence** | Dedup record |
| **Events** | None for duplicate |
| **Security** | Duplicate cannot bypass security |

### Scenario 5: Duplicate Tool Request

| Aspect | Expected |
|---|---|
| **Precondition** | Agent sends same tool request twice |
| **Input** | Same idempotency_key |
| **Transition** | First request executed, second detected |
| **Postcondition** | Tool executed once, duplicate returned cached result |
| **Persistence** | Idempotency record |
| **Events** | tool.completed (once) |
| **Security** | Duplicate cannot bypass authorization |

### Scenario 6: API Timeout After Side Effect

| Aspect | Expected |
|---|---|
| **Precondition** | External API call with side effect |
| **Input** | API times out after potential side effect |
| **Transition** | Result marked UNKNOWN_OUTCOME |
| **Postcondition** | UNKNOWN_OUTCOME recorded, reconciliation triggered |
| **Persistence** | Unknown outcome, reconciliation record |
| **Events** | api.unknown_outcome |
| **Security** | No blind retry |

### Scenario 7: Model Provider Outage

| Aspect | Expected |
|---|---|
| **Precondition** | Model provider becomes unavailable |
| **Input** | Model invocation request during outage |
| **Transition** | Primary provider fails, fallback triggered |
| **Postcondition** | Fallback model used or task failed gracefully |
| **Persistence** | Provider status, fallback decision |
| **Events** | model.fallback, model.provider_outage |
| **Security** | Fallback provider authorized |

### Scenario 8: Resource Exhaustion

| Aspect | Expected |
|---|---|
| **Precondition** | All resources allocated |
| **Input** | New work request |
| **Transition** | Admission denied or deferred |
| **Postcondition** | Work queued with RESOURCE_UNAVAILABLE |
| **Persistence** | Resource state, admission denial |
| **Events** | resource.exhausted, admission.denied |
| **Security** | Exhaustion does not bypass governance |

### Scenario 9: Approval Required

| Aspect | Expected |
|---|---|
| **Precondition** | Work requires governance approval |
| **Input** | Work admission attempt |
| **Transition** | ADMITTED → WAITING_FOR_APPROVAL |
| **Postcondition** | Work waits for approval, timeout if expired |
| **Persistence** | Approval request, timeout |
| **Events** | approval.requested |
| **Security** | Approval gate enforced |

### Scenario 10: Governance Denial

| Aspect | Expected |
|---|---|
| **Precondition** | Governance policy denies action |
| **Input** | Work admission attempt |
| **Transition** | Admission DENIED |
| **Postcondition** | Work not admitted, denial recorded |
| **Persistence** | Denial record, audit |
| **Events** | governance.denied |
| **Security** | Denial is final |

### Scenario 11: Cancellation During Execution

| Aspect | Expected |
|---|---|
| **Precondition** | Task/workflow running |
| **Input** | Cancellation requested |
| **Transition** | RUNNING → CANCELLED (via PROPAGATING) |
| **Postcondition** | Execution stopped, partial results preserved |
| **Persistence** | Cancellation state, partial results |
| **Events** | task.cancelled, workflow.cancelled |
| **Security** | Cancellation respects governance |

### Scenario 12: Temporary Agent Expiration

| Aspect | Expected |
|---|---|
| **Precondition** | Temporary agent with TTL |
| **Input** | TTL expires |
| **Transition** | ACTIVE → EXPIRED |
| **Postcondition** | Agent stopped, artifacts transferred, resources released |
| **Persistence** | Expiration record, artifact transfer |
| **Events** | agent.expired |
| **Security** | No orphaned execution |

### Scenario 13: Workflow Resume

| Aspect | Expected |
|---|---|
| **Precondition** | Workflow paused, checkpoint exists |
| **Input** | Resume command |
| **Transition** | PAUSED → RUNNING |
| **Postcondition** | Workflow resumed from checkpoint |
| **Persistence** | Checkpoint state restored |
| **Events** | workflow.resumed |
| **Security** | Resume authorized, scope verified |

### Scenario 14: Cross-Business Isolation Failure

| Aspect | Expected |
|---|---|
| **Precondition** | Business A executing |
| **Input** | Business A encounters failure |
| **Transition** | Business A fails, Business B unaffected |
| **Postcondition** | Business B continues normally |
| **Persistence** | Business-scoped state isolated |
| **Events** | business_a.failed |
| **Security** | No cross-business impact |

### Scenario 15: Unknown Outcome Reconciliation

| Aspect | Expected |
|---|---|
| **Precondition** | Execution outcome unknown |
| **Input** | Reconciliation triggered |
| **Transition** | UNKNOWN → QUERYING → COMPARING → RESOLVED |
| **Postcondition** | State reconciled, evidence recorded |
| **Persistence** | Reconciliation record, evidence |
| **Events** | reconciliation.completed |
| **Security** | Reconciliation authorized |

---

## 4. Invariant Cross-Reference

| Invariant | Primary Enforcer | Test Scenarios |
|---|---|---|
| RT-01: No execution without admission | Admission Controller | 1, 2, 3, 9, 10 |
| RT-02: No admission without auth/governance | Governance | 9, 10 |
| RT-03: No authority from events | All modules | 4, 5 |
| RT-04: No authority from attention | Attention | 9 |
| RT-05: No authority from objectives | Objective Engine | 1, 11 |
| RT-06: No credentials in runtime | Security | 1, 6, 7 |
| RT-07: Unknown outcome remains unknown | Reconciliation | 6, 15 |
| RT-08: Single retry owner | Retry System | 2, 3, 8 |
| RT-09: Cancellation ≠ failure | Cancellation System | 11 |
| RT-10: Verification ≠ execution | Verification | 1, 10 |
| RT-11: Outcome ≠ authorization | Governance | 1, 11 |
| RT-12: Runtime ≠ durable state | Persistence | 3, 13 |
| RT-13: Reachability ≠ health | Worker System | 3 |
| RT-14: Resources ≠ permission | Governance | 8 |
| RT-15: Model ≠ agent identity | Model Router | 7 |
| RT-16: Business isolation | Scope System | 14 |
| RT-17: WHY survives | Workflow | 1, 13 |
| RT-18: Idempotency for side effects | Idempotency System | 5, 6 |
| RT-19: Temporary agent lifecycle | Agent Runtime | 12 |
| RT-20: Observability ≠ authority | Observability | All |

---

## 5. Validation Checklist

Before commit, verify:

- [ ] All 20 runtime invariants defined
- [ ] All 15 test scenarios defined
- [ ] Each invariant has enforcement mechanism
- [ ] Each test scenario has preconditions, inputs, transitions, postconditions
- [ ] Phase 1 compatibility verified
- [ ] Phase 2 compatibility verified
- [ ] No architecture redesign
- [ ] No runtime code implemented
- [ ] Governance boundaries preserved
- [ ] Attention boundaries preserved
- [ ] WHY propagation preserved
- [ ] Multi-business isolation preserved
- [ ] Unknown outcome semantics preserved
- [ ] Identity/Authority/Capability/Permission/Trust remain distinct
- [ ] No credentials exposed
- [ ] Event semantics compatible
- [ ] Schema compatibility checked

---

*This document defines the non-negotiable runtime invariants and testable scenarios for NEXUS execution contracts.*
