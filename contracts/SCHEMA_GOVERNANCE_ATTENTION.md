# NEXUS — Governance, Policy, Attention, Error & Audit Schemas

**Layer:** Phase 2 — Data & Event Schemas  
**Status:** LOCKED  
**Branch:** contracts/data-event-schemas  
**Depends on:** SCHEMA_COMMON.md, SCHEMA_IDENTITIES_ORG.md, Phase 1 Core Interface Contracts  

---

## 1. Purpose

This document defines canonical schemas for the **governance and control domain**: policies (rules), policy decisions (evaluations), attention (interruption management), errors (failure representation), and audit events (traceability). Governance is the highest control layer in NEXUS.

---

## 2. Governance / Policy Schema

### 2.1 Design Principle

Governance is the highest control layer. Policy outcomes are exactly: `ALLOW`, `DENY`, `REQUIRE_APPROVAL`, `ALLOW_WITH_CONSTRAINTS`, `ESCALATE`. No competing vocabulary.

### 2.2 Policy Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version |
| `entity_type` | string | yes | Always `"policy"` |
| `policy_id` | string | yes | Unique policy identifier |
| `policy_version` | string | yes | Version of this policy |
| `nexus_id` | string | yes | NEXUS installation identifier |
| `business_id` | string | conditionally | Business scope. Absent for global policies. |
| `division_id` | string | no | Division scope |
| `policy_type` | enum | yes | One of: `access_control`, `data_governance`, `model_usage`, `tool_usage`, `approval_workflow`, `retention`, `security`, `compliance`, `custom` |
| `name` | string | yes | Human-readable policy name |
| `description` | string | yes | What this policy governs |
| `status` | enum | yes | One of: `active`, `disabled`, `draft`, `archived` |
| `subject` | PolicySubject | yes | Who/what this policy applies to (see §2.3) |
| `action` | PolicyAction | yes | What action this policy governs (see §2.4) |
| `resource` | PolicyResource | yes | What resource this policy protects (see §2.5) |
| `conditions` | list[PolicyCondition] | no | Conditions for policy evaluation |
| `effect` | enum | yes | One of: `ALLOW`, `DENY`, `REQUIRE_APPROVAL`, `ALLOW_WITH_CONSTRAINTS`, `ESCALATE` |
| `constraints` | list[Constraint] | no | Constraints when effect is `ALLOW_WITH_CONSTRAINTS` |
| `approval_config` | ApprovalConfig | conditionally | Required when effect is `REQUIRE_APPROVAL` |
| `precedence` | integer | yes | Higher number = higher precedence |
| `override_policy_ids` | list[string] | no | Policies this one overrides |
| `effective_from` | datetime | yes | When this policy becomes active |
| `effective_until` | datetime | no | When this policy expires |
| `created_at` | datetime | yes | When policy was created |
| `updated_at` | datetime | no | When policy was last modified |
| `created_by` | string | yes | Who created this policy |
| `approved_by` | string | conditionally | Who approved this policy (for critical policies) |
| `provenance` | ProvenanceRef | yes | Origin record |
| `metadata` | map[string,string] | no | Extension data |

### 2.3 PolicySubject

| Field | Type | Required | Description |
|---|---|---|---|
| `subject_type` | enum | yes | One of: `identity`, `agent_type`, `role`, `all` |
| `subject_ids` | list[string] | no | Specific subject IDs (empty = all of type) |
| `subject_scope` | string | no | Scope restriction |

### 2.4 PolicyAction

| Field | Type | Required | Description |
|---|---|---|---|
| `action_type` | enum | yes | One of: `execute_tool`, `invoke_model`, `access_data`, `create_workflow`, `approve_action`, `escalate`, `custom` |
| `action_ids` | list[string] | no | Specific action IDs (empty = all of type) |

### 2.5 PolicyResource

| Field | Type | Required | Description |
|---|---|---|---|
| `resource_type` | enum | yes | One of: `data`, `tool`, `model`, `workflow`, `agent`, `memory`, `configuration`, `all` |
| `resource_ids` | list[string] | no | Specific resource IDs (empty = all of type) |
| `resource_scope` | string | no | Scope restriction |

### 2.6 PolicyCondition

| Field | Type | Required | Description |
|---|---|---|---|
| `condition_id` | string | yes | Unique identifier |
| `condition_type` | enum | yes | One of: `time`, `scope`, `attribute`, `count`, `composite` |
| `expression` | string | yes | Condition expression |
| `negate` | boolean | no | Whether to negate this condition |

### 2.7 ApprovalConfig

| Field | Type | Required | Description |
|---|---|---|---|
| `approver_type` | enum | yes | One of: `human`, `governance`, `delegated` |
| `approver_ids` | list[string] | no | Specific approvers (empty = any authorized) |
| `timeout_seconds` | integer | yes | Approval timeout |
| `auto_deny_on_timeout` | boolean | yes | Whether timeout = denial |
| `self_approval_prohibited` | boolean | yes | Whether requester can approve |
| `delegation_allowed` | boolean | yes | Whether approver can delegate |

### 2.8 Policy Evaluation

When a module requests policy evaluation:

1. Module sends action, actor, resource, context to Governance
2. Governance loads all applicable policies (sorted by precedence)
3. Each policy is evaluated against the request
4. First `DENY` wins (deny overrides allow)
5. If no deny, first explicit effect wins
6. If no explicit effect, default is `DENY`
7. Result returned to caller with reason and constraints

---

## 3. Policy Decision Schema

### 3.1 Policy Decision Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version |
| `entity_type` | string | yes | Always `"policy_decision"` |
| `decision_id` | string | yes | Unique decision identifier |
| `nexus_id` | string | yes | NEXUS installation identifier |
| `business_id` | string | yes | Business scope |
| `correlation_id` | string | yes | End-to-end trace |
| `requester_id` | string | yes | Who requested this evaluation |
| `requester_type` | enum | yes | One of: `agent`, `human`, `system`, `module` |
| `action` | string | yes | Action being evaluated |
| `resource` | string | yes | Resource being accessed |
| `context` | map[string,any] | no | Additional context for evaluation |
| `decision` | enum | yes | One of: `ALLOW`, `DENY`, `REQUIRE_APPROVAL`, `ALLOW_WITH_CONSTRAINTS`, `ESCALATE` |
| `constraints` | list[string] | no | Constraints if ALLOW_WITH_CONSTRAINTS |
| `reason` | string | yes | Why this decision was made |
| `applicable_policies` | list[string] | yes | Policy IDs that were evaluated |
| `evaluation_time_ms` | integer | yes | How long evaluation took |
| `created_at` | datetime | yes | When decision was made |
| `expires_at` | datetime | no | When this decision expires |
| `approval_ref` | string | conditionally | Required if decision is REQUIRE_APPROVAL |
| `escalation_ref` | string | conditionally | Required if decision is ESCALATE |
| `provenance` | ProvenanceRef | yes | Origin record |
| `metadata` | map[string,string] | no | Extension data |

### 3.2 Decision Rules

- `DENY` always wins over `ALLOW`
- Default decision when no policy matches is `DENY`
- Policy decisions are cached for performance but must respect TTL
- Policy decisions must be auditable
- Policy decisions do NOT carry authority — they ARE the authority check
- Expired policy decisions must be re-evaluated

---

## 4. Attention Schema

### 4.1 Design Principle

Attention manages **interruption and prioritization**. Attention is NOT execution authority. An attention item may request owner attention but cannot grant permission to execute an action.

### 4.2 Attention Item Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version |
| `entity_type` | string | yes | Always `"attention"` |
| `attention_id` | string | yes | Unique attention identifier |
| `nexus_id` | string | yes | NEXUS installation identifier |
| `business_id` | string | yes | Business scope |
| `division_id` | string | no | Division scope |
| `source` | AttentionSource | yes | What triggered this attention item (see §4.3) |
| `reason` | string | yes | Why attention is needed |
| `level` | enum | yes | One of: `ignore`, `background`, `normal`, `important`, `high`, `critical`, `emergency` (see §4.4) |
| `priority` | integer | yes | Numeric priority (higher = more important) |
| `objective_id` | string | conditionally | Related objective |
| `correlation_id` | string | yes | End-to-end trace |
| `deduplication_key` | string | no | For grouping similar attention items |
| `aggregation_key` | string | no | For aggregating related attention items |
| `suppressed` | boolean | yes | Whether this item is suppressed |
| `suppression_reason` | string | conditionally | Why suppressed |
| `cooldown_seconds` | integer | no | Minimum time before re-alerting |
| `owner_id` | string | conditionally | Who should respond |
| `owner_type` | enum | conditionally | One of: `human`, `agent`, `system` |
| `interruption_allowed` | boolean | yes | Whether this can interrupt active work |
| `escalation_state` | enum | yes | One of: `pending`, `acknowledged`, `escalated`, `resolved`, `expired` |
| `escalation_chain` | list[string] | no | Chain of escalation targets |
| `recommended_action` | string | no | Suggested response |
| `options` | list[AttentionOption] | no | Available response options |
| `created_at` | datetime | yes | When attention was triggered |
| `acknowledged_at` | datetime | no | When acknowledged |
| `resolved_at` | datetime | no | When resolved |
| `expires_at` | datetime | yes | When this attention item expires |
| `resolution` | string | no | How it was resolved |
| `audit_ref` | string | yes | Audit trail reference |
| `provenance` | ProvenanceRef | yes | Origin record |
| `metadata` | map[string,string] | no | Extension data |

### 4.3 AttentionSource

| Field | Type | Required | Description |
|---|---|---|---|
| `source_type` | enum | yes | One of: `governance`, `observability`, `workflow`, `agent`, `tool`, `external`, `system` |
| `source_id` | string | yes | ID of the source entity |
| `source_module` | string | no | NEXUS module that generated this |

### 4.4 Attention Levels

| Level | Description | Interruption | Typical Source |
|---|---|---|---|
| `ignore` | Logged only | No | Debug, routine |
| `background` | Low priority | No | Informational |
| `normal` | Standard attention | No | Normal operations |
| `important` | Notable event | Soft | Significant outcome |
| `high` | Requires attention soon | Yes | Governance escalation |
| `critical` | Requires immediate attention | Yes | Security, failure |
| `emergency` | System-critical | Yes | System failure, data loss risk |

### 4.5 AttentionOption

| Field | Type | Required | Description |
|---|---|---|---|
| `option_id` | string | yes | Unique identifier |
| `label` | string | yes | Human-readable option |
| `description` | string | yes | What this option does |
| `requires_approval` | boolean | yes | Whether this option needs governance approval |

### 4.6 Attention Rules

- Attention ≠ Authority (an attention item does not grant permission)
- Attention may request human review
- Attention may trigger governance escalation
- Attention must be deduplicated
- Attention must respect cooldown
- Emergency attention may interrupt any work
- Attention must be auditable
- Attention expiration = denial (silence = no action)

---

## 5. Error / Failure Schema

### 5.1 Design Principle

The error envelope is shared across all NEXUS modules. It must distinguish between known failures and unknown outcomes.

### 5.2 NEXUSError Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version |
| `entity_type` | string | yes | Always `"error"` |
| `error_id` | string | yes | Unique error identifier |
| `category` | enum | yes | One of the 14 error categories (see §5.3) |
| `code` | string | yes | Machine-readable error code |
| `message` | string | yes | Human-readable description |
| `details` | map[string,any] | no | Structured error details |
| `retryable` | boolean | yes | Whether caller should retry |
| `source` | ErrorSource | yes | Where this error originated (see §5.4) |
| `operation` | string | yes | What operation failed |
| `correlation_id` | string | yes | End-to-end trace |
| `causation_id` | string | conditionally | What caused this error |
| `related_entity_ids` | list[string] | no | Entity IDs involved |
| `timestamp` | datetime | yes | When error occurred |
| `recovery` | RecoveryInfo | no | Recovery/reconciliation info (see §5.5) |
| `audit_ref` | string | yes | Audit trail reference |
| `provenance` | ProvenanceRef | yes | Origin record |
| `metadata` | map[string,string] | no | Extension data |

### 5.3 Error Categories

| Category | Meaning | Retryable | Notes |
|---|---|---|---|
| `VALIDATION` | Input failed schema or business validation | No | Fix input |
| `AUTH` | Authentication failed | No | Check credentials |
| `AUTHORIZATION` | Authenticated but not authorized | No | Check permissions |
| `POLICY_DENIED` | Governance policy blocked action | No | Policy is authoritative |
| `APPROVAL_REQUIRED` | Action needs human approval | No | Wait for approval |
| `RESOURCE_UNAVAILABLE` | Resource temporarily unavailable | Yes | Retry with backoff |
| `TIMEOUT` | Operation exceeded time limit | Maybe | Check if side effects occurred |
| `DEPENDENCY_FAILURE` | Upstream module failed | Maybe | Check dependency health |
| `RATE_LIMIT` | Rate limit exceeded | Yes | Backoff per Retry-After |
| `CONFLICT` | State conflict (concurrent modification) | Maybe | Refresh and retry |
| `UNKNOWN_OUTCOME` | Outcome uncertain | No (escalate) | Do NOT blind retry |
| `CANCELLATION` | Operation was cancelled | No | No retry |
| `SECURITY_REJECTION` | Security boundary violation | No | Security is authoritative |
| `INTERNAL_FAILURE` | Unexpected internal error | Maybe | Retry once, then escalate |

### 5.4 ErrorSource

| Field | Type | Required | Description |
|---|---|---|---|
| `module` | string | yes | NEXUS module where error occurred |
| `function` | string | no | Specific function/method |
| `entity_id` | string | no | Entity being processed |
| `entity_type` | string | no | Type of entity |

### 5.5 RecoveryInfo

| Field | Type | Required | Description |
|---|---|---|---|
| `recovery_type` | enum | yes | One of: `none`, `retry`, `fallback`, `reconciliation`, `manual_intervention` |
| `reconciliation_ref` | string | conditionally | Reference to reconciliation record |
| `fallback_ref` | string | conditionally | Reference to fallback action |
| `manual_intervention_required` | boolean | yes | Whether human intervention is needed |

### 5.6 Unknown Outcome Handling

`UNKNOWN_OUTCOME` is a **critical** category:

- The operation may or may not have succeeded
- Side effects may or may not have occurred
- **Blind retry is prohibited** — it may cause duplicate side effects
- Must be escalated to human or reconciliation process
- Must include `reconciliation_ref` for tracking
- Consumer must check state before retrying

---

## 6. Audit Event Schema

### 6.1 Design Principle

Audit events support **reconstruction of important decisions and actions**. Audit records must not become an authority mechanism.

### 6.2 Audit Event Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version |
| `entity_type` | string | yes | Always `"audit_event"` |
| `audit_id` | string | yes | Unique audit identifier |
| `nexus_id` | string | yes | NEXUS installation identifier |
| `business_id` | string | yes | Business scope |
| `division_id` | string | no | Division scope |
| `event_type` | enum | yes | One of: `action_performed`, `decision_made`, `policy_evaluated`, `approval_requested`, `approval_decided`, `access_attempted`, `access_granted`, `access_denied`, `configuration_changed`, `security_event`, `error_occurred`, `cancellation_initiated`, `escalation_triggered` |
| `actor` | AuditActor | yes | Who performed this action (see §6.3) |
| `action` | string | yes | What was done |
| `resource` | AuditResource | yes | What was affected (see §6.4) |
| `decision` | string | no | Decision that was made |
| `policy_ref` | string | no | Policy involved |
| `approval_ref` | string | no | Approval involved |
| `workflow_id` | string | no | Workflow context |
| `task_id` | string | no | Task context |
| `tool_execution_id` | string | no | Tool execution context |
| `model_invocation_id` | string | no | Model invocation context |
| `correlation_id` | string | yes | End-to-end trace |
| `causation_id` | string | conditionally | What caused this audit event |
| `result` | enum | yes | One of: `success`, `failure`, `denied`, `pending` |
| `result_details` | string | no | Additional result information |
| `timestamp` | datetime | yes | When this audit event was recorded |
| `security_context` | SecurityContext | no | Security metadata |
| `provenance` | ProvenanceRef | yes | Origin record |
| `metadata` | map[string,string] | no | Extension data |

### 6.3 AuditActor

| Field | Type | Required | Description |
|---|---|---|---|
| `actor_id` | string | yes | Who performed the action |
| `actor_type` | enum | yes | One of: `human`, `agent`, `system`, `service` |
| `identity_verified` | boolean | yes | Whether identity was verified |
| `authority_verified` | boolean | yes | Whether authority was checked |

### 6.4 AuditResource

| Field | Type | Required | Description |
|---|---|---|---|
| `resource_type` | string | yes | Type of resource affected |
| `resource_id` | string | yes | ID of resource affected |
| `resource_scope` | string | no | Scope of the resource |
| `field_changes` | list[FieldChange] | no | What fields changed |

### 6.5 FieldChange

| Field | Type | Required | Description |
|---|---|---|---|
| `field` | string | yes | Field name |
| `old_value` | string | no | Previous value (null if created) |
| `new_value` | string | no | New value (null if deleted) |

### 6.6 Audit Rules

- Audit events are append-only (never delete or modify)
- Audit events must be stored durably
- Audit events must support reconstruction of decisions
- Sensitive audit data must be classified appropriately
- Audit retention must comply with business/legal requirements
- Audit events must not be used as authority mechanism

---

## 7. Relationships

```
Governance
 ├── Policy(s)
 │    ├── Subject
 │    ├── Action
 │    ├── Resource
 │    └── Conditions
 ├── Policy Decision(s)
 │    ├── references → Policy
 │    └── triggers → Approval / Escalation

Attention
 ├── triggered by → Governance / Workflow / Agent / System
 ├── may request → Approval
 └── may escalate → Human

Audit Event
 ├── records → Action / Decision / Access
 ├── references → Policy / Approval / Workflow / Task
 └── traces → correlation_id
```

---

## 8. Cross-Schema References

| Reference | Target Schema |
|---|---|
| `policy_decision.approval_ref` | Approval Schema (SCHEMA_WORK_OBJECTIVES.md) |
| `attention.source` | Various source schemas |
| `audit_event.workflow_id` | Workflow Schema (SCHEMA_WORK_OBJECTIVES.md) |
| `audit_event.task_id` | Task Schema (SCHEMA_WORK_OBJECTIVES.md) |
| `audit_event.tool_execution_id` | Tool Execution Schema (SCHEMA_EXECUTION.md) |
| `audit_event.model_invocation_id` | Model Invocation Schema (SCHEMA_EXECUTION.md) |

---

*This document defines the governance and control domain of NEXUS. Governance is the highest authority. Attention manages interruption. Errors represent failures. Audit provides traceability.*
