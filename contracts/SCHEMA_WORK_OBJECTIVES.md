# NEXUS — Objective, Workflow, Task, Decision, Approval & Outcome Schemas

**Layer:** Phase 2 — Data & Event Schemas  
**Status:** LOCKED  
**Branch:** contracts/data-event-schemas  
**Depends on:** SCHEMA_COMMON.md, SCHEMA_IDENTITIES_ORG.md, Phase 1 Core Interface Contracts  

---

## 1. Purpose

This document defines canonical schemas for the **work and objective domain**: objectives (WHY), workflows (durable execution), tasks (unit of work), decisions (choices), approvals (governance gates), and outcomes (evidence). These schemas make the NEXUS "purpose → decision → plan → execute → verify → evaluate" chain structurally unambiguous.

---

## 2. Objective Schema

### 2.1 Design Principle

The Objective Engine preserves **WHY**. An objective is not authority — it is purpose. Objectives never override Governance, Security, Authorization, or Policy.

### 2.2 Objective Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version |
| `entity_type` | string | yes | Always `"objective"` |
| `entity_id` | string | yes | Unique objective identifier |
| `nexus_id` | string | yes | NEXUS installation identifier |
| `business_id` | string | yes | Business scope |
| `division_id` | string | no | Division scope |
| `objective_type` | enum | yes | One of: `mission`, `goal`, `task_level`, `maintenance`, `reactive` |
| `statement` | string | yes | What this objective aims to achieve |
| `why` | string | yes | WHY this objective exists. Must be preserved through execution. |
| `why_chain` | list[string] | no | Chain of WHY from parent objectives. Updated as hierarchy deepens. |
| `desired_outcome` | string | yes | What success looks like in concrete terms |
| `constraints` | list[Constraint] | no | Limitations, boundaries, requirements |
| `priority` | enum | yes | One of: `critical`, `high`, `medium`, `low`, `background` |
| `status` | enum | yes | Lifecycle state (see §2.3) |
| `parent_id` | string | no | Parent objective (for decomposition) |
| `child_ids` | list[string] | no | Child objectives (after decomposition) |
| `workflow_id` | string | no | Associated workflow (set when execution begins) |
| `success_criteria` | list[SuccessCriterion] | no | Measurable conditions for success |
| `evaluation_context` | EvaluationContext | no | How this objective will be evaluated |
| `created_at` | datetime | yes | When objective was created |
| `updated_at` | datetime | no | When objective was last modified |
| `expires_at` | datetime | no | Objective deadline or relevance window |
| `provenance` | ProvenanceRef | yes | Origin of this objective |
| `classification` | enum | no | Data classification (default: `INTERNAL`) |
| `metadata` | map[string,string] | no | Extension data |

### 2.3 Objective State Machine

```
CREATED → DECOMPOSED → ASSIGNED → IN_PROGRESS → {COMPLETED, FAILED, CANCELLED}
                                                       ↓
                                                  EVALUATED → {REPRIORITIZED, TERMINATED}
```

| State | Description |
|---|---|
| `CREATED` | Objective defined, not yet decomposed |
| `DECOMPOSED` | Objective broken into sub-objectives or tasks |
| `ASSIGNED` | Objective assigned to agent/workflow |
| `IN_PROGRESS` | Actively being worked on |
| `COMPLETED` | Work finished (may not mean success) |
| `FAILED` | Work could not be completed |
| `CANCELLED` | Objective cancelled by authority |
| `EVALUATED` | Outcome assessed against success criteria |
| `REPRIORITIZED` | Priority changed, re-queued |
| `TERMINATED` | Permanently stopped, no further work |

### 2.4 Constraint

| Field | Type | Required | Description |
|---|---|---|---|
| `constraint_type` | enum | yes | One of: `time`, `resource`, `scope`, `quality`, `compliance`, `technical`, `business` |
| `description` | string | yes | What this constraint requires |
| `enforcement` | enum | yes | One of: `hard` (must not violate), `soft` (prefer not to violate) |
| `source` | string | no | Where this constraint originated |

### 2.5 SuccessCriterion

| Field | Type | Required | Description |
|---|---|---|---|
| `criterion_id` | string | yes | Unique identifier |
| `description` | string | yes | What must be true for success |
| `metric` | string | no | Measurable metric name |
| `target_value` | string | no | Target value for metric |
| `verification_method` | string | no | How to verify this criterion |

### 2.6 EvaluationContext

| Field | Type | Required | Description |
|---|---|---|---|
| `evaluator` | string | yes | Who/what evaluates (human, agent, automated) |
| `evaluation_criteria` | list[string] | yes | Criteria for evaluation |
| `deadline` | datetime | no | When evaluation must occur |

### 2.7 WHY Preservation Rules

- `why` field must be present on every objective
- `why_chain` must be propagated to all child objectives, workflows, and tasks
- No module may discard `why` without explicit Governance approval
- `why` is NOT authority — it explains purpose, not permission
- `why` must survive long-running workflows (checkpointed with workflow state)

---

## 3. Workflow Schema

### 3.1 Workflow Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version |
| `entity_type` | string | yes | Always `"workflow"` |
| `entity_id` | string | yes | Unique workflow identifier |
| `nexus_id` | string | yes | NEXUS installation identifier |
| `business_id` | string | yes | Business scope |
| `division_id` | string | no | Division scope |
| `objective_id` | string | yes | Objective this workflow serves |
| `why` | string | yes | WHY context (propagated from objective) |
| `why_chain` | list[string] | yes | Full WHY chain |
| `workflow_type` | enum | yes | One of: `plan_driven`, `event_driven`, `hybrid` |
| `status` | enum | yes | Lifecycle state (see §3.2) |
| `priority` | enum | yes | Inherited from objective or overridden |
| `plan_id` | string | no | Reference to the plan that created this workflow |
| `nodes` | list[WorkflowNode] | yes | Steps in this workflow |
| `current_node` | string | no | Currently executing node ID |
| `input_ref` | string | no | Reference to workflow input data |
| `output_ref` | string | no | Reference to workflow output data |
| `resource_allocations` | list[string] | no | Resource allocation IDs |
| `created_at` | datetime | yes | When workflow was created |
| `updated_at` | datetime | no | When workflow state was last modified |
| `started_at` | datetime | no | When execution began |
| `completed_at` | datetime | no | When execution ended |
| `expires_at` | datetime | no | Workflow deadline |
| `checkpoint_ref` | string | no | Reference to latest checkpoint in Memory |
| `cancellation` | CancellationContext | no | Cancellation state if applicable |
| `retry_context` | RetryContext | no | Retry state if applicable |
| `provenance` | ProvenanceRef | yes | Origin record |
| `metadata` | map[string,string] | no | Extension data |

### 3.2 Workflow State Machine

```
PLANNED → DISPATCHED → RUNNING → {COMPLETED, FAILED, BLOCKED, CANCELLED}
                                       ↓
                                  PAUSED → RESUMED → RUNNING
```

| State | Description |
|---|---|
| `PLANNED` | Workflow defined, not yet dispatched |
| `DISPATCHED` | Workflow sent to execution |
| `RUNNING` | Actively executing nodes |
| `PAUSED` | Temporarily paused (operator or governance) |
| `RESUMED` | Resumed from pause |
| `COMPLETED` | All nodes completed successfully |
| `FAILED` | Unrecoverable failure |
| `BLOCKED` | Waiting for external input, resource, or approval |
| `CANCELLED` | Cancelled by authority |

### 3.3 WorkflowNode

| Field | Type | Required | Description |
|---|---|---|---|
| `node_id` | string | yes | Unique node identifier |
| `node_type` | enum | yes | One of: `task`, `decision`, `parallel`, `condition`, `subworkflow`, `approval_gate` |
| `name` | string | yes | Human-readable node name |
| `status` | enum | yes | Node lifecycle state |
| `agent_id` | string | no | Assigned agent (for task nodes) |
| `task_id` | string | no | Generated task ID |
| `depends_on` | list[string] | no | Node IDs this node depends on |
| `conditions` | list[Condition] | no | Conditions for conditional nodes |
| `timeout` | integer | no | Node timeout in seconds |
| `retry_policy` | RetryPolicy | no | Retry configuration |
| `input_ref` | string | no | Reference to node input data |
| `output_ref` | string | no | Reference to node output data |
| `started_at` | datetime | no | When node execution began |
| `completed_at` | datetime | no | When node execution ended |

### 3.4 CancellationContext

| Field | Type | Required | Description |
|---|---|---|---|
| `cancelled_at` | datetime | yes | When cancellation occurred |
| `cancelled_by` | string | yes | Who initiated cancellation |
| `reason` | string | yes | Cancellation reason |
| `propagated` | boolean | yes | Whether cancellation has propagated to all downstream |
| `side_effects_recorded` | boolean | yes | Whether irreversible side effects are recorded |

### 3.5 RetryContext

| Field | Type | Required | Description |
|---|---|---|---|
| `attempt` | integer | yes | Current attempt number |
| `max_attempts` | integer | yes | Maximum allowed attempts |
| `last_failure_reason` | string | no | Why last attempt failed |
| `next_retry_at` | datetime | no | When next retry is scheduled |

### 3.6 RetryPolicy

| Field | Type | Required | Description |
|---|---|---|---|
| `max_attempts` | integer | yes | Maximum retry attempts |
| `backoff_type` | enum | yes | One of: `fixed`, `exponential`, `custom` |
| `initial_delay_seconds` | integer | yes | Initial delay |
| `max_delay_seconds` | integer | no | Maximum delay cap |
| `retryable_errors` | list[string] | no | Error categories that trigger retry |

---

## 4. Task Schema

### 4.1 Task Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version |
| `entity_type` | string | yes | Always `"task"` |
| `entity_id` | string | yes | Unique task identifier |
| `nexus_id` | string | yes | NEXUS installation identifier |
| `business_id` | string | yes | Business scope |
| `division_id` | string | no | Division scope |
| `workflow_id` | string | conditionally | Required when task belongs to a workflow |
| `objective_id` | string | yes | Objective this task serves |
| `why` | string | yes | WHY context |
| `task_type` | enum | yes | One of: `agent_action`, `tool_call`, `approval`, `review`, `observation` |
| `status` | enum | yes | Lifecycle state (see §4.2) |
| `priority` | enum | yes | Task priority |
| `assigned_agent_id` | string | conditionally | Agent assigned to execute this task |
| `input_ref` | string | no | Reference to task input data |
| `output_ref` | string | no | Reference to task output data |
| `dependencies` | list[string] | no | Task IDs this task depends on |
| `deadline` | datetime | no | Task deadline |
| `timeout` | integer | no | Timeout in seconds |
| `retry_policy` | RetryPolicy | no | Retry configuration |
| `idempotency_key` | string | no | Idempotency key for safe retry |
| `created_at` | datetime | yes | When task was created |
| `updated_at` | datetime | no | When task state was last modified |
| `started_at` | datetime | no | When execution began |
| `completed_at` | datetime | no | When execution ended |
| `cancellation` | CancellationContext | no | Cancellation state |
| `approval_ref` | string | no | Reference to approval record if blocked on approval |
| `verification_state` | enum | no | One of: `pending`, `verified`, `failed`, `skipped` |
| `provenance` | ProvenanceRef | yes | Origin record |
| `metadata` | map[string,string] | no | Extension data |

### 4.2 Task State Machine

```
ASSIGNED → RUNNING → {COMPLETED, FAILED, TIMEOUT, CANCELLED}
                          ↓
                     WAITING_FOR_APPROVAL → {APPROVED, DENIED}
```

| State | Description |
|---|---|
| `ASSIGNED` | Task assigned to agent, not yet started |
| `RUNNING` | Agent actively executing |
| `COMPLETED` | Task finished successfully |
| `FAILED` | Task could not be completed |
| `TIMEOUT` | Task exceeded allowed time |
| `CANCELLED` | Task cancelled by authority |
| `WAITING_FOR_APPROVAL` | Paused pending human/governance approval |
| `APPROVED` | Approval granted, task may proceed |
| `DENIED` | Approval denied, task cannot proceed |

---

## 5. Decision Schema

### 5.1 Design Principle

A Decision is a **recommendation**. It is NOT authorization. A decision must pass Governance/authorization controls before execution.

### 5.2 Decision Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version |
| `entity_type` | string | yes | Always `"decision"` |
| `entity_id` | string | yes | Unique decision identifier |
| `nexus_id` | string | yes | NEXUS installation identifier |
| `business_id` | string | yes | Business scope |
| `division_id` | string | no | Division scope |
| `decision_type` | enum | yes | One of: `strategic`, `tactical`, `operational`, `technical` |
| `status` | enum | yes | Lifecycle state (see §5.3) |
| `actor_id` | string | yes | Who/what made this decision |
| `actor_type` | enum | yes | One of: `human`, `agent`, `system` |
| `objective_id` | string | conditionally | Objective this decision serves |
| `why` | string | yes | WHY this decision was made |
| `inputs` | list[DecisionInput] | yes | Data/references that informed this decision |
| `alternatives` | list[Alternative] | no | Alternatives considered |
| `selected_action` | string | yes | What was chosen |
| `constraints` | list[Constraint] | no | Constraints applied |
| `confidence` | float | no | Decision confidence (0.0-1.0) |
| `rationale` | string | yes | Reasoning behind the choice |
| `policy_refs` | list[string] | no | Governance policies consulted |
| `approval_required` | boolean | yes | Whether Governance approval is needed |
| `approval_ref` | string | conditionally | Required when approval is needed |
| `created_at` | datetime | yes | When decision was made |
| `expires_at` | datetime | no | Decision validity window |
| `executed_at` | datetime | no | When decision was executed |
| `evaluation_ref` | string | no | Reference to outcome evaluation |
| `correlation_id` | string | yes | End-to-end trace |
| `provenance` | ProvenanceRef | yes | Origin record |
| `metadata` | map[string,string] | no | Extension data |

### 5.3 Decision State Machine

```
PROPOSED → REVIEWED → {APPROVED, DENIED, DEFERRED}
                         ↓
                    EXECUTED → {SUCCEEDED, FAILED}
```

| State | Description |
|---|---|
| `PROPOSED` | Decision recommendation made |
| `REVIEWED` | Decision reviewed by authority |
| `APPROVED` | Decision approved for execution |
| `DENIED` | Decision denied |
| `DEFERRED` | Decision postponed |
| `EXECUTED` | Decision being executed |
| `SUCCEEDED` | Execution succeeded |
| `FAILED` | Execution failed |

### 5.4 DecisionInput

| Field | Type | Required | Description |
|---|---|---|---|
| `input_id` | string | yes | Unique input identifier |
| `source` | string | yes | Where this input came from |
| `input_type` | enum | yes | One of: `data`, `recommendation`, `constraint`, `preference` |
| `content` | string | yes | Input content or reference |
| `weight` | float | no | Relative importance (0.0-1.0) |

### 5.5 Alternative

| Field | Type | Required | Description |
|---|---|---|---|
| `alternative_id` | string | yes | Unique identifier |
| `description` | string | yes | What this alternative proposes |
| `pros` | list[string] | no | Advantages |
| `cons` | list[string] | no | Disadvantages |
| `rejection_reason` | string | no | Why this alternative was not chosen |

---

## 6. Approval Schema

### 6.1 Design Principle

**Silence ≠ approval.** No self-approval where prohibited. Approval is a Governance gate, not a formality.

### 6.2 Approval Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version |
| `entity_type` | string | yes | Always `"approval"` |
| `entity_id` | string | yes | Unique approval identifier |
| `nexus_id` | string | yes | NEXUS installation identifier |
| `business_id` | string | yes | Business scope |
| `requested_action` | string | yes | What is being requested |
| `requester_id` | string | yes | Who is requesting approval |
| `requester_type` | enum | yes | One of: `agent`, `system`, `workflow` |
| `approver_id` | string | conditionally | Who can approve (may be set later by Governance) |
| `approver_type` | enum | conditionally | One of: `human`, `governance`, `delegated` |
| `authority_context` | string | yes | Why this approval is needed |
| `policy_ref` | string | no | Governance policy requiring this approval |
| `scope` | string | yes | Scope of the requested action |
| `constraints` | list[Constraint] | no | Constraints if approved |
| `status` | enum | yes | Lifecycle state (see §6.3) |
| `created_at` | datetime | yes | When approval was requested |
| `expires_at` | datetime | yes | When approval request expires (silence = denial) |
| `decided_at` | datetime | no | When approver made decision |
| `decision_rationale` | string | conditionally | Required when decision is made |
| `self_approval_prohibited` | boolean | yes | Whether requester can approve own request |
| `delegation_allowed` | boolean | yes | Whether approver can delegate |
| `escalation_ref` | string | no | Reference to Attention escalation |
| `audit_ref` | string | yes | Audit trail reference |
| `correlation_id` | string | yes | End-to-end trace |
| `provenance` | ProvenanceRef | yes | Origin record |
| `metadata` | map[string,string] | no | Extension data |

### 6.3 Approval State Machine

```
REQUESTED → PENDING → {APPROVED, DENIED, EXPIRED, CANCELLED}
                         ↓
                    DELEGATED → {APPROVED, DENIED}
```

| State | Description |
|---|---|
| `REQUESTED` | Approval request created |
| `PENDING` | Waiting for approver action |
| `APPROVED` | Approver granted permission |
| `DENIED` | Approver denied permission |
| `EXPIRED` | Approval request expired (silence = denial) |
| `CANCELLED` | Requester cancelled the request |
| `DELEGATED` | Approver delegated to another |

---

## 7. Outcome Schema

### 7.1 Design Principle

Outcome is **evidence** for objective evaluation. It is NOT automatically proof of success.

### 7.2 Outcome Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version |
| `entity_type` | string | yes | Always `"outcome"` |
| `entity_id` | string | yes | Unique outcome identifier |
| `nexus_id` | string | yes | NEXUS installation identifier |
| `business_id` | string | yes | Business scope |
| `source_type` | enum | yes | One of: `workflow`, `task`, `agent`, `decision` |
| `source_id` | string | yes | ID of the source entity |
| `objective_id` | string | yes | Objective this outcome relates to |
| `expected_outcome_ref` | string | no | Reference to expected outcome definition |
| `actual_result` | string | yes | What actually happened |
| `success_state` | enum | yes | One of: `success`, `failure`, `partial`, `unknown` |
| `verification_state` | enum | yes | One of: `unverified`, `verified`, `disputed` |
| `evidence` | list[Evidence] | no | Supporting evidence |
| `confidence` | float | no | Confidence in this outcome (0.0-1.0) |
| `side_effects` | list[SideEffect] | no | Irreversible side effects produced |
| `business_impact` | string | no | Description of business impact |
| `follow_up_actions` | list[string] | no | References to follow-up objectives/workflows |
| `replan_required` | boolean | yes | Whether replanning is needed |
| `created_at` | datetime | yes | When outcome was recorded |
| `evaluated_at` | datetime | no | When outcome was evaluated |
| `correlation_id` | string | yes | End-to-end trace |
| `provenance` | ProvenanceRef | yes | Origin record |
| `metadata` | map[string,string] | no | Extension data |

### 7.3 Evidence

| Field | Type | Required | Description |
|---|---|---|---|
| `evidence_id` | string | yes | Unique identifier |
| `evidence_type` | enum | yes | One of: `tool_result`, `observation`, `metric`, `human_report`, `system_log` |
| `content` | string | yes | Evidence content or reference |
| `source` | string | yes | Where evidence came from |
| `timestamp` | datetime | yes | When evidence was gathered |
| `confidence` | float | no | Confidence in this evidence |

### 7.4 SideEffect

| Field | Type | Required | Description |
|---|---|---|---|
| `effect_id` | string | yes | Unique identifier |
| `effect_type` | enum | yes | One of: `data_written`, `data_deleted`, `external_api_call`, `financial_transaction`, `communication_sent` |
| `description` | string | yes | What changed |
| `reversible` | boolean | yes | Whether this can be undone |
| `reversal_ref` | string | conditionally | Required if reversible: how to undo |

---

## 8. Relationships

```
Objective
 ├── child Objectives (decomposition)
 ├── Workflow (execution)
 │    └── WorkflowNodes
 │         └── Tasks
 │              ├── Tool Executions
 │              ├── Model Invocations
 │              └── Approvals
 ├── Decisions (choices made)
 ├── Approvals (governance gates)
 └── Outcomes (evidence)
```

---

## 9. Cross-Schema References

| Reference | Target Schema |
|---|---|
| `objective_id` → `workflow_id` | Workflow Schema |
| `workflow_id` → `task_id` | Task Schema |
| `decision_id` → `approval_ref` | Approval Schema |
| `outcome_id` → `objective_id` | Objective Schema |
| `task_id` → `agent_id` | Agent Schema (SCHEMA_IDENTITIES_ORG.md) |

---

*This document defines the work and purpose domain of NEXUS. Objectives preserve WHY; workflows execute plans; tasks are units of work; decisions recommend; approvals gate; outcomes provide evidence.*
