# NEXUS — Core Interface Contracts

**Layer:** Phase 1 — Core Interface Contracts  
**Status:** LOCKED  
**Branch:** contracts/core-interface-contracts  
**Canonical Cleanup Commit:** 7800758  

---

## 0. Purpose

This document defines the **implementation-facing contracts** between all 21 canonical NEXUS architecture modules. It is not a redesign — it formalizes the boundaries, message shapes, authority flows, and error semantics that modules must honor when code is written.

Architecture remains frozen. No new modules. No code implementation in this phase.

---

## 1. Contract Metadata Schema

Every contract must declare:

| Field | Type | Required | Description |
|---|---|---|---|
| `contract_id` | string | yes | Unique identifier (e.g., `CTR-EXEC-001`) |
| `producer` | string | yes | Module that produces the message/output |
| `consumer` | string | yes | Module that consumes the message/input |
| `purpose` | string | yes | What this contract achieves |
| `trigger` | string | yes | What initiates the flow |
| `input` | object | yes | Required and optional input fields |
| `output` | object | yes | Required and optional output fields |
| `success_semantics` | string | yes | What success means |
| `failure_semantics` | string | yes | What failure means |
| `authority_scope` | object | yes | Business, Division, Agent scope rules |
| `objective_context` | string | yes | Whether WHY is preserved |
| `correlation_id` | string | yes | How requests are traced |
| `idempotency` | string | yes | Idempotency guarantees |
| `timeout` | string | yes | Timeout behavior |
| `retry` | string | yes | Retry ownership |
| `cancellation` | string | yes | Cancellation semantics |
| `persistence` | string | yes | Durability requirements |
| `audit` | string | yes | Audit trail requirements |
| `observability` | string | yes | Metrics, logs, traces |
| `security` | string | yes | Security boundary rules |
| `version` | string | yes | Contract version |
| `compatibility` | string | yes | Backward/forward compat rules |

---

## 2. Async Semantics

All inter-module communication must declare its pattern:

| Pattern | Description | Guarantee |
|---|---|---|
| `sync_request_response` | Caller blocks until result | Exactly once per request |
| `async_command` | Caller dispatches, does not block | At-least-once delivery |
| `event` | Broadcast, no direct response expected | At-most-once delivery |
| `notification` | Acknowledged event | At-least-once with ack |
| `result` | Response to async command | Exactly once per command |
| `ack` | Acknowledgment of receipt | Exactly once per ack |

**Delivery guarantees** (must be declared per contract):
- `at_most_once`: Message may be lost, never duplicated
- `at_least_once`: Message may be duplicated, never lost
- `effectively_once`: Message processed exactly once (requires idempotency or transactional semantics)

---

## 3. Error Envelope

All contracts must use a common error envelope:

```yaml
error:
  code: string          # Machine-readable code
  category: enum        # One of: VALIDATION, AUTH, AUTHORIZATION, POLICY_DENIAL,
                        #   APPROVAL_REQUIRED, RESOURCE_UNAVAILABLE, TIMEOUT,
                        #   DEPENDENCY_FAILURE, RATE_LIMIT, CONFLICT,
                        #   UNKNOWN_OUTCOME, CANCELLATION, SECURITY_REJECTION,
                        #   INTERNAL_FAILURE
  message: string       # Human-readable description
  details: object       # Optional structured details
  retryable: boolean    # Whether caller should retry
  correlation_id: string # For tracing
  timestamp: datetime   # When error occurred
```

**Error categories:**

| Category | Meaning | Retryable |
|---|---|---|
| `VALIDATION` | Input did not pass schema or business validation | No |
| `AUTH` | Authentication failed | No |
| `AUTHORIZATION` | Authenticated but not authorized | No |
| `POLICY_DENIAL` | Governance policy blocked the action | No |
| `APPROVAL_REQUIRED` | Action requires human approval | No |
| `RESOURCE_UNAVAILABLE` | Required resource is temporarily unavailable | Yes |
| `TIMEOUT` | Operation exceeded time limit | Maybe |
| `DEPENDENCY_FAILURE` | Upstream module failed | Maybe |
| `RATE_LIMIT` | Rate limit exceeded | Yes (after backoff) |
| `CONFLICT` | State conflict (e.g., concurrent modification) | Maybe |
| `UNKNOWN_OUTCOME` | Operation outcome is uncertain | No (escalate) |
| `CANCELLATION` | Operation was cancelled | No |
| `SECURITY_REJECTION` | Security boundary violation | No |
| `INTERNAL_FAILURE` | Unexpected internal error | Maybe |

---

## 4. Authority Boundary Contract

Authority in NEXUS is layered. No module may assume authority it does not hold.

### 4.1 Authority Hierarchy

```
GOVERNANCE (highest)
  └─ IDENTITY (establishes who)
       └─ CAPABILITY (what can be done)
            └─ PERMISSION (specific grants)
                 └─ TRUST (contextual confidence)
```

### 4.2 Contract: Authority Resolution

```
CTR-AUTH-001
Producer: IDENTITY
Consumer: GOVERNANCE, AGENT RUNTIME, TOOL RUNTIME
Purpose: Resolve authority for a requested action
Input:
  required:
    - actor_id: string
    - action: string
    - resource: string
    - business_id: string
    - division_id: string (optional)
  optional:
    - objective_id: string
    - trust_context: object
Output:
  required:
    - decision: enum [ALLOW, DENY, REQUIRE_APPROVAL, ALLOW_WITH_CONSTRAINTS, ESCALATE]
    - authority_chain: list[string]  # Who granted what
    - constraints: list[string]      # If ALLOW_WITH_CONSTRAINTS
    - reason: string
  optional:
    - approval_required_from: string
    - escalation_target: string
Failure:
  - AUTH → actor not found
  - AUTHORIZATION → identity established but no authority for action
  - INTERNAL_FAILURE → resolution service unavailable
```

### 4.3 Trust ≠ Authority

Trust is contextual confidence, not permission. A contract may declare:

```yaml
trust_context:
  confidence: float          # 0.0 - 1.0
  basis: enum [VERIFIED, INFERRED, EXTERNAL, UNKNOWN]
  scope: string              # What this trust applies to
  expires_at: datetime       # Trust is time-bounded
```

Trust never grants authority. Only Governance grants authority.

---

## 5. Contract Registry — All Boundaries (A–S)

### A. Executive ↔ Objective Engine

```
CTR-EXEC-001: Executive → Objective Engine
  Purpose: Request objective decomposition
  Trigger: Executive receives task/mission
  Pattern: sync_request_response
  Input: { mission, business_context, priority, constraints }
  Output: { objectives: list[Objective], why_chain: list[string] }
  Failure: DEPENDENCY_FAILURE if Objective Engine unavailable

CTR-EXEC-002: Objective Engine → Executive  
  Purpose: Report objective status/change
  Trigger: Objective completed, failed, or reprioritized
  Pattern: event
  Input: { objective_id, status, outcome, metrics }
  Output: ack
  Failure: Logged, does not block Objective Engine
```

### B. Objective Engine ↔ Decision Engine

```
CTR-OBJ-001: Objective Engine → Decision Engine
  Purpose: Request decision for objective resolution
  Trigger: Objective requires choice between alternatives
  Pattern: sync_request_response
  Input: { objective_id, alternatives: list, criteria, constraints, why }
  Output: { recommendation: Decision, confidence, rationale }
  Failure: DEPENDENCY_FAILURE → escalate to Executive

CTR-OBJ-002: Decision Engine → Objective Engine
  Purpose: Report decision outcome
  Trigger: Decision executed and result observed
  Pattern: notification
  Input: { decision_id, outcome, actual_vs_expected }
  Output: ack
```

### C. Decision Engine ↔ Planner

```
CTR-DEC-001: Decision Engine → Planner
  Purpose: Request plan generation for approved decision
  Trigger: Decision approved for execution
  Pattern: sync_request_response
  Input: { decision_id, objective, constraints, resources, timeline }
  Output: { plan: Plan, steps: list[Step], dependencies, risks }
  Failure: INTERNAL_FAILURE → retry once, then escalate

CTR-DEC-002: Planner → Decision Engine
  Purpose: Report plan feasibility
  Trigger: Planner cannot generate viable plan
  Pattern: notification
  Input: { decision_id, blockers: list, alternatives }
  Output: ack
```

### D. Planner ↔ Workflow Orchestration

```
CTR-PLAN-001: Planner → Workflow Orchestration
  Purpose: Submit plan for execution
  Trigger: Plan approved
  Pattern: async_command
  Input: { plan_id, steps, schedule, resource_reqs, objective_id }
  Output: { workflow_id } (ack)
  Failure: RESOURCE_UNAVAILABLE → queue and retry

CTR-PLAN-002: Workflow Orchestration → Planner
  Purpose: Report execution status
  Trigger: Step completed, failed, or blocked
  Pattern: event
  Input: { workflow_id, step_id, status, result, blocker }
  Output: ack
```

### E. Workflow Orchestration ↔ Scheduling/Resource Runtime

```
CTR-WF-001: Workflow Orchestration → Scheduling
  Purpose: Request resource allocation for step
  Trigger: Step requires compute/human/external resource
  Pattern: sync_request_response
  Input: { step_id, resource_type, quantity, duration, priority, business_id }
  Output: { allocation_id, assigned_to, start_time, end_time }
  Failure: RESOURCE_UNAVAILABLE → queue, retry with backoff, notify Attention if critical

CTR-WF-002: Scheduling → Workflow Orchestration
  Purpose: Report resource availability change
  Trigger: Resource freed, failed, or preempted
  Pattern: event
  Input: { allocation_id, status, reason }
  Output: ack
```

### F. Workflow Orchestration ↔ Agent Runtime

```
CTR-WF-003: Workflow Orchestration → Agent Runtime
  Purpose: Dispatch task to agent
  Trigger: Step requires agent execution
  Pattern: async_command
  Input: { task_id, agent_id, objective_id, context, constraints, timeout }
  Output: { dispatch_id } (ack)
  Failure: DEPENDENCY_FAILURE → reassign or escalate

CTR-WF-004: Agent Runtime → Workflow Orchestration
  Purpose: Report task completion
  Trigger: Agent finishes task
  Pattern: result
  Input: { task_id, status, output, artifacts, metrics }
  Output: ack
```

### G. Agent Runtime ↔ Model Router

```
CTR-AGT-001: Agent Runtime → Model Router
  Purpose: Request model inference
  Trigger: Agent needs LLM generation
  Pattern: sync_request_response
  Input: { request_id, prompt_context, model_preferences, constraints, objective_id }
  Output: { response, model_used, tokens, cost }
  Failure: RESOURCE_UNAVAILABLE → fallback model, TIMEOUT → retry once

CTR-AGT-002: Model Router → Agent Runtime
  Purpose: Report model availability change
  Trigger: Provider outage or recovery
  Pattern: event
  Input: { provider, status, estimated_recovery }
  Output: ack
```

### H. Agent Runtime ↔ Tool Runtime

```
CTR-AGT-003: Agent Runtime → Tool Runtime
  Purpose: Execute tool call
  Trigger: Agent decides to use tool
  Pattern: sync_request_response
  Input: { tool_id, parameters, agent_id, business_id, objective_id, timeout }
  Output: { result, artifacts, side_effects }
  Failure: RESOURCE_UNAVAILABLE → retry with backoff, INTERNAL_FAILURE → escalate

CTR-AGT-004: Tool Runtime → Agent Runtime
  Purpose: Report async tool completion
  Trigger: Long-running tool finishes
  Pattern: result
  Input: { tool_call_id, status, result }
  Output: ack
```

### I. API Gateway ↔ External Systems

```
CTR-API-001: API Gateway → External System
  Purpose: Outbound API call
  Trigger: Workflow or agent needs external service
  Pattern: sync_request_response or async_command
  Input: { endpoint, method, headers, body, timeout, idempotency_key }
  Output: { status, headers, body }
  Failure: TIMEOUT, RESOURCE_UNAVAILABLE, RATE_LIMIT → retry per policy

CTR-API-002: External System → API Gateway
  Purpose: Inbound webhook/callback
  Trigger: External system sends notification
  Pattern: notification
  Input: { source, event_type, payload, signature }
  Output: { accepted: boolean }
  Failure: VALIDATION → reject, AUTH → reject
```

### J. Identity ↔ All Modules

```
CTR-IDN-001: Identity → Any Module
  Purpose: Provide actor identity context
  Trigger: Any module receives request from actor
  Pattern: sync (inline context)
  Input: { actor_id, token }
  Output: { identity: Identity, permissions: list, scope: Scope }
  Failure: AUTH → reject request

CTR-IDN-002: Identity → Governance
  Purpose: Request authority resolution
  Trigger: Module needs to verify authorization
  Pattern: sync_request_response
  Input: { actor_id, action, resource, context }
  Output: { decision, constraints, chain }
  Failure: INTERNAL_FAILURE → deny by default
```

### K. Governance ↔ All Modules

```
CTR-GOV-001: Governance → Any Module
  Purpose: Policy evaluation
  Trigger: Module needs to check if action is permitted
  Pattern: sync_request_response
  Input: { action, actor, resource, context, objective_id }
  Output: { decision: enum [ALLOW, DENY, REQUIRE_APPROVAL, ALLOW_WITH_CONSTRAINTS, ESCALATE], constraints }
  Failure: INTERNAL_FAILURE → deny by default

CTR-GOV-002: Governance → Attention
  Purpose: Escalate to human
  Trigger: Governance decision is ESCALATE or REQUIRE_APPROVAL
  Pattern: async_command
  Input: { escalation_id, reason, context, urgency, deadline }
  Output: { escalation_id } (ack)
  Failure: Logged, retry once
```

### L. Attention ↔ Human Operator

```
CTR-ATT-001: Attention → Human
  Purpose: Present decision for human review
  Trigger: Governance escalation or critical attention signal
  Pattern: notification
  Input: { alert_id, summary, context, options, deadline }
  Output: { ack }
  Failure: TIMEOUT → escalate to higher authority or auto-resolve per policy

CTR-ATT-002: Human → Attention
  Purpose: Human response to alert
  Trigger: Human provides decision
  Pattern: result
  Input: { alert_id, decision, reasoning }
  Output: { accepted: boolean }
```

### M. Memory ↔ Context Engine

```
CTR-MEM-001: Context Engine → Memory
  Purpose: Retrieve relevant memory for context assembly
  Trigger: Context needs to be built for agent/model
  Pattern: sync_request_response
  Input: { query, scope, objective_id, filters, budget }
  Output: { memories: list[MemoryRecord], total_tokens, truncated }
  Failure: RESOURCE_UNAVAILABLE → fallback to cached context, INTERNAL_FAILURE → degrade

CTR-MEM-002: Memory → Context Engine
  Purpose: Push new memory for context relevance
  Trigger: Important event or observation recorded
  Pattern: event
  Input: { memory_id, type, relevance_hint }
  Output: ack
```

### N. Memory ↔ Agent Runtime

```
CTR-MEM-003: Agent Runtime → Memory
  Purpose: Store agent observation as memory
  Trigger: Agent produces significant observation
  Pattern: async_command
  Input: { content, type, scope, confidence, source, objective_id }
  Output: { memory_id } (ack)
  Failure: Logged, retry once

CTR-MEM-004: Memory → Agent Runtime
  Purpose: Provide memory context for agent
  Trigger: Agent requests context
  Pattern: sync (via Context Engine)
  Input: { query, scope }
  Output: { relevant_memories }
```

### O. Memory ↔ Workflow Orchestration

```
CTR-MEM-005: Workflow Orchestration → Memory
  Purpose: Store workflow checkpoint
  Trigger: Workflow state change
  Pattern: async_command
  Input: { workflow_id, checkpoint, state, objective_id }
  Output: { checkpoint_id } (ack)
  Failure: Retry with backoff

CTR-MEM-006: Memory → Workflow Orchestration
  Purpose: Restore workflow from checkpoint
  Trigger: Workflow resume after restart
  Pattern: sync_request_response
  Input: { workflow_id }
  Output: { checkpoint, state, last_modified }
  Failure: DEPENDENCY_FAILURE → restart from last known good
```

### P. Persistence ↔ All Stateful Modules

```
CTR-PER-001: Any Module → Persistence
  Purpose: Durable state storage
  Trigger: Module needs to persist state
  Pattern: sync_request_response
  Input: { key, value, ttl, scope, version }
  Output: { stored: boolean, version }
  Failure: INTERNAL_FAILURE → retry, then queue for later

CTR-PER-002: Persistence → Any Module
  Purpose: State recovery
  Trigger: Module restart or recovery
  Pattern: sync_request_response
  Input: { key, scope }
  Output: { value, version, last_modified }
  Failure: NOT_FOUND → use defaults, INTERNAL_FAILURE → escalate
```

### Q. Observability ↔ All Modules

```
CTR-OBS-001: Any Module → Observability
  Purpose: Emit telemetry
  Trigger: Any significant event
  Pattern: fire-and-forget (at-most-once)
  Input: { metric_name, value, labels, timestamp, trace_id }
  Output: none (fire-and-forget)
  Failure: silently dropped (observability must not block business logic)

CTR-OBS-002: Observability → Attention
  Purpose: Alert on anomalies
  Trigger: Metric threshold breached
  Pattern: async_command
  Input: { alert_type, severity, details, recommendation }
  Output: ack
```

### R. Security ↔ All Modules

```
CTR-SEC-001: Security → Any Module
  Purpose: Security boundary check
  Trigger: Module processes untrusted input
  Pattern: sync_request_response
  Input: { input, policy, context }
  Output: { safe: boolean, violations: list }
  Failure: SECURITY_REJECTION → block operation

CTR-SEC-002: Security → Observability
  Purpose: Security event logging
  Trigger: Security violation detected
  Pattern: fire-and-forget
  Input: { event_type, severity, source, details }
  Output: none
```

### S. Configuration ↔ All Modules

```
CTR-CFG-001: Configuration → Any Module
  Purpose: Provide configuration values
  Trigger: Module startup or config change
  Pattern: sync_request_response (startup), event (change)
  Input: { module_id, config_key }
  Output: { value, source, last_modified }
  Failure: NOT_FOUND → use defaults, INTERNAL_FAILURE → use cached

CTR-CFG-002: Configuration → Governance
  Purpose: Report config change requiring approval
  Trigger: Critical config change requested
  Pattern: async_command
  Input: { change_id, current, proposed, impact }
  Output: ack
```

---

## 6. Cross-Cutting Contract Rules

### 6.1 Correlation ID

Every contract must propagate a `correlation_id`. This ID must:
- Be generated at the entry point (API Gateway or Human input)
- Be passed through all subsequent calls
- Be included in all audit logs, error reports, and observability events
- Be a UUID or equivalent globally unique identifier

### 6.2 Objective Context Preservation

Every contract that processes work must carry:
- `objective_id`: The objective being served
- `why_chain`: The chain of reasoning from mission to current task

No module may discard the WHY without explicit Governance approval.

### 6.3 Multi-Business Isolation

Every contract must declare its `business_id` scope. Cross-business data access requires:
- Explicit Governance policy allowing it
- Identity verification for both businesses
- Audit trail of the cross-business access

### 6.4 Cancellation Propagation

When a workflow or objective is cancelled:
1. Cancellation propagates downstream through all active contracts
2. Each module must acknowledge cancellation within its timeout
3. Side effects that cannot be undone must be recorded
4. Partial results are preserved unless Governance policy dictates otherwise

### 6.5 Timeout Ownership

The **caller** owns timeout. The **callee** owns execution duration.
- Caller sets `timeout` on request
- Callee must respect timeout and return partial results if timeout is hit
- Callee must not silently continue after timeout

### 6.6 Retry Ownership

| Scenario | Retry Owner | Strategy |
|---|---|---|
| Transient network failure | Caller | Exponential backoff, max 3 |
| Resource temporarily unavailable | Caller | Queue and retry |
| Dependency failure | Workflow Orchestration | Reassign or escalate |
| Rate limit | Caller | Backoff per Retry-After header |
| Internal failure | Caller | Retry once, then escalate |
| Validation error | Caller | Never retry (fix input) |
| Auth/AuthZ error | Caller | Never retry (fix identity) |

---

## 7. State Machine Contracts

### 7.1 Objective State Machine

```
CREATED → DECOMPOSED → ASSIGNED → IN_PROGRESS → {COMPLETED, FAILED, CANCELLED}
                                                      ↓
                                                 EVALUATED → {REPRIORITIZED, TERMINATED}
```

### 7.2 Workflow State Machine

```
PLANNED → DISPATCHED → RUNNING → {COMPLETED, FAILED, BLOCKED, CANCELLED}
                                       ↓
                                  PAUSED → RESUMED → RUNNING
```

### 7.3 Agent Task State Machine

```
ASSIGNED → RUNNING → {COMPLETED, FAILED, TIMEOUT, CANCELLED}
                          ↓
                     WAITING_FOR_APPROVAL → {APPROVED, DENIED}
```

### 7.4 Decision State Machine

```
PROPOSED → REVIEWED → {APPROVED, DENIED, DEFERRED}
                         ↓
                    EXECUTED → {SUCCEEDED, FAILED}
```

---

## 8. Versioning & Compatibility

### 8.1 Version Format

```
MAJOR.MINOR.PATCH
```

- **MAJOR**: Breaking change (new required fields, changed semantics)
- **MINOR**: Backward-compatible addition (new optional fields)
- **PATCH**: Bug fix or clarification

### 8.2 Compatibility Rules

- Consumers must ignore unknown fields
- Producers must not remove or rename existing required fields
- New optional fields may be added without version bump
- Breaking changes require new contract ID (e.g., `CTR-EXEC-001` → `CTR-EXEC-002`)

---

## 9. Trust/Confidence Vocabulary Reconciliation

> **CONTRACTS TODO**: The following vocabularies need reconciliation in a dedicated phase:
> - Identity trust levels (VERIFIED, INFERRED, EXTERNAL, UNKNOWN)
> - Memory confidence levels (FACTUAL, VERIFIED, PROVISIONAL, INFERRED, UNCERTAIN, CONTRADICTED)
> - Governance decision confidence
> - Model output confidence
> - Tool result confidence
>
> Until reconciliation, each module may use its own vocabulary but must map to a common set at contract boundaries.

---

## 10. Acceptance Criteria

- [ ] All 21 modules have defined contracts for their inbound/outbound boundaries
- [ ] Every contract has a unique ID, producer, consumer, and purpose
- [ ] Error envelope covers all 14 error categories
- [ ] Async semantics are declared for every contract
- [ ] Correlation ID propagation is specified
- [ ] Objective context (WHY) is preserved in all work-carrying contracts
- [ ] Multi-business isolation is enforced in all contracts
- [ ] Cancellation propagation is defined
- [ ] Timeout and retry ownership is clear
- [ ] State machines are defined for key entities
- [ ] Versioning rules are established
- [ ] Trust/confidence vocabulary reconciliation is flagged as TODO
- [ ] Security boundary checks are defined for all untrusted input paths
- [ ] Audit requirements are specified per contract
- [ ] Observability metrics are defined per contract

---

*This document is the authoritative reference for NEXUS core interface contracts. Architecture modules remain frozen; code implementation will reference this document.*
