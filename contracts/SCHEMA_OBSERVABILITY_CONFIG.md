# NEXUS — Observability & Configuration Schemas

**Layer:** Phase 2 — Data & Event Schemas  
**Status:** LOCKED  
**Branch:** contracts/data-event-schemas  
**Depends on:** SCHEMA_COMMON.md, Phase 1 Core Interface Contracts  

---

## 1. Purpose

This document defines canonical schemas for **Observability** (metrics, traces, logs) and **Configuration** (system settings). Observability provides visibility into system behavior without becoming an authority mechanism. Configuration provides controlled system settings with governance oversight.

---

## 2. Observability Schema

### 2.1 Design Principle

Observability provides **visibility**. Observability metadata must not become an authority mechanism. Observability data must not block business logic.

### 2.2 Trace Context

| Field | Type | Required | Description |
|---|---|---|---|
| `trace_id` | string | yes | Unique trace identifier (UUID) |
| `span_id` | string | yes | Unique span identifier within trace |
| `parent_span_id` | string | no | Parent span (for hierarchical tracing) |
| `nexus_id` | string | yes | NEXUS installation |
| `business_id` | string | yes | Business scope |
| `operation_name` | string | yes | Name of the operation being traced |
| `service_name` | string | yes | Module/component performing the operation |
| `start_time` | datetime | yes | When this span started |
| `end_time` | datetime | no | When this span ended |
| `duration_ms` | integer | no | Span duration in milliseconds |
| `status` | enum | yes | One of: `ok`, `error`, `cancelled` |
| `status_message` | string | conditionally | Error message if status is `error` |
| `tags` | map[string,string] | no | Key-value tags for filtering |
| `baggage` | map[string,string] | no | Context propagated across spans |
| `correlation_id` | string | yes | End-to-end trace identifier |
| `objective_id` | string | no | Objective being traced |
| `workflow_id` | string | no | Workflow being traced |
| `task_id` | string | no | Task being traced |
| `agent_id` | string | no | Agent being traced |

### 2.3 Metric Record

| Field | Type | Required | Description |
|---|---|---|---|
| `metric_name` | string | yes | Metric name (e.g., `tool_execution_duration_ms`) |
| `metric_type` | enum | yes | One of: `counter`, `gauge`, `histogram`, `summary` |
| `value` | float | yes | Metric value |
| `unit` | string | no | Unit of measurement |
| `labels` | map[string,string] | yes | Dimensional labels for filtering |
| `timestamp` | datetime | yes | When metric was recorded |
| `nexus_id` | string | yes | NEXUS installation |
| `business_id` | string | yes | Business scope |
| `service_name` | string | yes | Module/component |
| `trace_id` | string | no | Associated trace |
| `correlation_id` | string | no | Associated correlation |

### 2.4 Log Record

| Field | Type | Required | Description |
|---|---|---|---|
| `log_id` | string | yes | Unique log identifier |
| `timestamp` | datetime | yes | When log was created |
| `level` | enum | yes | One of: `debug`, `info`, `warn`, `error`, `fatal` |
| `service_name` | string | yes | Module/component |
| `message` | string | yes | Log message |
| `trace_id` | string | no | Associated trace |
| `correlation_id` | string | no | Associated correlation |
| `nexus_id` | string | yes | NEXUS installation |
| `business_id` | string | yes | Business scope |
| `error` | NEXUSError | conditionally | Error details if level is error/fatal |
| `context` | map[string,any] | no | Additional context |
| `tags` | map[string,string] | no | Tags for filtering |

### 2.5 Alert Record

| Field | Type | Required | Description |
|---|---|---|---|
| `alert_id` | string | yes | Unique alert identifier |
| `alert_type` | string | yes | What type of alert |
| `severity` | enum | yes | One of: `info`, `warning`, `critical`, `emergency` |
| `source` | string | yes | What generated this alert |
| `nexus_id` | string | yes | NEXUS installation |
| `business_id` | string | yes | Business scope |
| `details` | map[string,any] | yes | Alert details |
| `recommendation` | string | no | Suggested action |
| `correlation_id` | string | no | Associated correlation |
| `created_at` | datetime | yes | When alert was generated |
| `acknowledged_at` | datetime | no | When alert was acknowledged |
| `resolved_at` | datetime | no | When alert was resolved |
| `status` | enum | yes | One of: `firing`, `acknowledged`, `resolved` |

### 2.6 Observability Metrics Registry

| Metric Name | Type | Labels | Description |
|---|---|---|---|
| `objective_created_total` | counter | `business_id`, `priority` | Total objectives created |
| `objective_completed_total` | counter | `business_id`, `status` | Total objectives completed |
| `workflow_started_total` | counter | `business_id` | Total workflows started |
| `workflow_duration_ms` | histogram | `business_id`, `status` | Workflow execution duration |
| `task_assigned_total` | counter | `business_id`, `agent_id` | Total tasks assigned |
| `task_duration_ms` | histogram | `business_id`, `status` | Task execution duration |
| `agent_active_count` | gauge | `business_id`, `agent_type` | Currently active agents |
| `tool_execution_total` | counter | `business_id`, `tool_id`, `status` | Total tool executions |
| `tool_execution_duration_ms` | histogram | `business_id`, `tool_id` | Tool execution duration |
| `model_invocation_total` | counter | `business_id`, `provider_id`, `status` | Total model invocations |
| `model_invocation_tokens` | histogram | `business_id`, `provider_id` | Tokens per invocation |
| `model_invocation_cost` | histogram | `business_id`, `provider_id` | Cost per invocation |
| `memory_retrieval_total` | counter | `business_id` | Total memory retrievals |
| `memory_retrieval_duration_ms` | histogram | `business_id` | Memory retrieval duration |
| `governance_evaluation_total` | counter | `business_id`, `decision` | Total governance evaluations |
| `attention_triggered_total` | counter | `business_id`, `level` | Total attention items triggered |
| `error_total` | counter | `business_id`, `category` | Total errors |
| `policy_decision_total` | counter | `business_id`, `decision` | Total policy decisions |

### 2.7 Observability Rules

- Observability data must not block business logic (fire-and-forget)
- Observability must not become an authority mechanism
- Traces must propagate correlation_id
- Metrics must be scoped to business_id
- Logs must not contain secrets or sensitive data
- Alert escalation follows the Attention schema
- Observability retention is separate from business data retention

---

## 3. Configuration Schema

### 3.1 Design Principle

Configuration is **system settings**. Configuration is NOT runtime state. Configuration changes must remain governed.

### 3.2 Configuration Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version |
| `entity_type` | string | yes | Always `"configuration"` |
| `config_id` | string | yes | Unique configuration identifier |
| `nexus_id` | string | yes | NEXUS installation identifier |
| `domain` | enum | yes | One of: `system`, `module`, `business`, `division`, `agent`, `workflow` |
| `scope` | ConfigScope | yes | What this config applies to (see §3.3) |
| `config_key` | string | yes | Configuration key (dot notation, e.g., `model_router.default_provider`) |
| `config_value` | any | yes | Configuration value |
| `config_type` | enum | yes | One of: `string`, `integer`, `float`, `boolean`, `json`, `list` |
| `version` | integer | yes | Config version (starts at 1) |
| `schema_version_ref` | string | no | Reference to config schema version |
| `effective_from` | datetime | yes | When this config becomes active |
| `effective_until` | datetime | no | When this config expires |
| `status` | enum | yes | One of: `active`, `pending`, `deprecated`, `rolled_back` |
| `validation_state` | enum | yes | One of: `valid`, `invalid`, `unvalidated` |
| `policy_state` | enum | yes | One of: `compliant`, `non_compliant`, `pending_review` |
| `approval_state` | enum | yes | One of: `not_required`, `pending`, `approved`, `denied` |
| `approval_ref` | string | conditionally | Required if approval is needed |
| `source` | ConfigSource | yes | Where this config came from (see §3.4) |
| `previous_version_id` | string | no | Reference to previous config version |
| `rollback_ref` | string | no | Reference to rollback target |
| `feature_flag_ref` | string | no | Associated feature flag |
| `requires_approval` | boolean | yes | Whether this config change needs governance approval |
| `created_at` | datetime | yes | When config was created |
| `updated_at` | datetime | no | When config was last modified |
| `created_by` | string | yes | Who created this config |
| `approved_by` | string | conditionally | Who approved this config |
| `audit_ref` | string | yes | Audit trail reference |
| `provenance` | ProvenanceRef | yes | Origin record |
| `metadata` | map[string,string] | no | Extension data |

### 3.3 ConfigScope

| Field | Type | Required | Description |
|---|---|---|---|
| `scope_type` | enum | yes | One of: `global`, `business`, `division`, `agent`, `workflow` |
| `scope_id` | string | conditionally | ID of the scoped entity (absent for global) |
| `business_id` | string | conditionally | Business scope (required for non-global) |

### 3.4 ConfigSource

| Field | Type | Required | Description |
|---|---|---|---|
| `source_type` | enum | yes | One of: `default`, `file`, `environment`, `ui`, `api`, `migration` |
| `source_reference` | string | no | Reference to source (file path, env var, etc.) |

### 3.5 Configuration Rules

- Configuration ≠ runtime state
- Critical configuration changes require Governance approval
- Configuration versioning supports rollback
- Configuration must be audited
- Configuration must be scoped (global, business, division)
- Default values must be documented
- Configuration changes must be propagated to affected modules
- Feature flags are a special type of configuration

### 3.6 Configuration Hierarchy

```
Global Configuration
  └── Business Configuration (overrides global)
       └── Division Configuration (overrides business)
            └── Agent Configuration (overrides division)
```

Higher scope configurations are overridden by lower scope configurations.

---

## 4. Relationships

```
Observability
 ├── Trace Context → correlates with → Events, Workflows, Tasks
 ├── Metrics → scoped to → Business, Module
 ├── Logs → scoped to → Business, Module
 └── Alerts → trigger → Attention Schema

Configuration
 ├── scoped to → Business, Division, Agent
 ├── may require → Approval Schema
 └── audited by → Audit Schema
```

---

## 5. Cross-Schema References

| Reference | Target Schema |
|---|---|
| `trace.correlation_id` | Common Schema Envelope (SCHEMA_COMMON.md) |
| `alert` → Attention | Attention Schema (SCHEMA_GOVERNANCE_ATTENTION.md) |
| `config.approval_ref` | Approval Schema (SCHEMA_WORK_OBJECTIVES.md) |
| `config.audit_ref` | Audit Schema (SCHEMA_GOVERNANCE_ATTENTION.md) |

---

*This document defines the observability and configuration domain of NEXUS. Observability provides visibility; Configuration provides controlled settings. Both are governed and auditable.*
