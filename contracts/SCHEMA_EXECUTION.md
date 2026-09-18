# NEXUS — Tool Execution, Model Invocation & Artifact Schemas

**Layer:** Phase 2 — Data & Event Schemas  
**Status:** LOCKED  
**Branch:** contracts/data-event-schemas  
**Depends on:** SCHEMA_COMMON.md, SCHEMA_IDENTITIES_ORG.md, SCHEMA_WORK_OBJECTIVES.md, Phase 1 Core Interface Contracts  

---

## 1. Purpose

This document defines canonical schemas for the **execution domain**: tool executions (controlled boundary for external actions), model invocations (LLM interactions), and artifacts (generated outputs). These schemas ensure that all side effects, model interactions, and produced artifacts are structurally traceable and governed.

---

## 2. Tool Execution Schema

### 2.1 Design Principle

Tool Runtime is the **controlled execution boundary**. Tool results are **untrusted external input** unless verified. Credentials are **never** embedded in execution records.

### 2.2 Tool Execution Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version |
| `entity_type` | string | yes | Always `"tool_execution"` |
| `entity_id` | string | yes | Unique tool execution identifier |
| `nexus_id` | string | yes | NEXUS installation identifier |
| `business_id` | string | yes | Business scope |
| `division_id` | string | no | Division scope |
| `tool_id` | string | yes | Which tool is being executed |
| `tool_version` | string | no | Tool version being used |
| `agent_id` | string | yes | Agent requesting execution |
| `task_id` | string | conditionally | Task this execution belongs to |
| `workflow_id` | string | conditionally | Workflow this execution belongs to |
| `objective_id` | string | yes | Objective being served |
| `why` | string | yes | WHY this tool is being called |
| `authorization_context` | AuthorizationContext | yes | Authority check before execution (see §2.3) |
| `policy_decision_ref` | string | conditionally | Reference to Governance policy decision |
| `request` | ToolRequest | yes | What the tool should do (see §2.4) |
| `input` | map[string,any] | yes | Tool input parameters |
| `output` | ToolOutput | no | Tool result (populated after execution) |
| `result_status` | enum | yes | One of: `success`, `failure`, `timeout`, `cancelled`, `unknown` |
| `side_effect_classification` | enum | yes | One of: `none`, `read_only`, `reversible`, `irreversible` |
| `idempotency_key` | string | conditionally | Required for irreversible side effects |
| `timeout` | integer | yes | Timeout in seconds |
| `retry_context` | RetryContext | no | Retry state if applicable |
| `resource_usage` | ResourceUsage | no | Resource consumption metrics |
| `credential_ref` | string | conditionally | Reference to credential (NEVER raw secret) |
| `created_at` | datetime | yes | When execution was initiated |
| `started_at` | datetime | no | When tool actually started |
| `completed_at` | datetime | no | When tool finished |
| `correlation_id` | string | yes | End-to-end trace |
| `causation_id` | string | no | What triggered this execution |
| `audit_ref` | string | yes | Audit trail reference |
| `reconciliation_state` | enum | no | One of: `pending`, `confirmed`, `reconciled`, `disputed` |
| `provenance` | ProvenanceRef | yes | Origin record |
| `metadata` | map[string,string] | no | Extension data |

### 2.3 AuthorizationContext

| Field | Type | Required | Description |
|---|---|---|---|
| `authorization_id` | string | yes | Authorization check identifier |
| `decision` | enum | yes | One of: `ALLOW`, `DENY`, `REQUIRE_APPROVAL`, `ALLOW_WITH_CONSTRAINTS` |
| `constraints` | list[string] | no | Constraints if ALLOW_WITH_CONSTRAINTS |
| `checked_at` | datetime | yes | When authorization was checked |
| `expires_at` | datetime | no | When authorization expires |
| `policy_version` | string | no | Policy version used for check |

### 2.4 ToolRequest

| Field | Type | Required | Description |
|---|---|---|---|
| `action` | string | yes | What action to perform |
| `parameters` | map[string,any] | yes | Action parameters |
| `dry_run` | boolean | no | Whether to simulate without side effects |
| `timeout_override` | integer | no | Override default timeout |

### 2.5 ToolOutput

| Field | Type | Required | Description |
|---|---|---|---|
| `success` | boolean | yes | Whether tool succeeded |
| `result` | any | no | Tool result data |
| `error` | NEXUSError | no | Error if failed |
| `artifacts` | list[string] | no | Artifact IDs produced |
| `side_effects` | list[SideEffect] | no | Side effects produced |

### 2.6 ResourceUsage

| Field | Type | Required | Description |
|---|---|---|---|
| `execution_time_ms` | integer | yes | Wall clock time |
| `cpu_time_ms` | integer | no | CPU time |
| `memory_bytes` | integer | no | Peak memory usage |
| `network_bytes_in` | integer | no | Network bytes received |
| `network_bytes_out` | integer | no | Network bytes sent |
| `api_calls` | integer | no | External API calls made |
| `cost` | float | no | Monetary cost if applicable |

### 2.7 Tool Execution Rules

- Tool results are **untrusted external input** until verified
- Irreversible side effects require `idempotency_key`
- `dry_run` must be supported for previewing side effects
- Credentials referenced by `credential_ref`, never embedded
- Unknown outcomes (`result_status: unknown`) require reconciliation, not blind retry
- Authorization must be checked before execution, not after

---

## 3. Model Invocation Schema

### 3.1 Design Principle

Model Router handles provider abstraction. Model outputs are **inference**, not fact. Privacy and data residency must be tracked.

### 3.2 Model Invocation Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version |
| `entity_type` | string | yes | Always `"model_invocation"` |
| `entity_id` | string | yes | Unique invocation identifier |
| `nexus_id` | string | yes | NEXUS installation identifier |
| `business_id` | string | yes | Business scope |
| `division_id` | string | no | Division scope |
| `agent_id` | string | yes | Agent requesting inference |
| `task_id` | string | conditionally | Task context |
| `workflow_id` | string | conditionally | Workflow context |
| `objective_id` | string | yes | Objective being served |
| `provider_id` | string | yes | Model provider (e.g., `ollama`, `openrouter`, `openai`) |
| `model_id` | string | yes | Specific model (e.g., `llama3`, `gpt-4`) |
| `model_version` | string | no | Model version/tag |
| `routing_decision` | RoutingDecision | yes | Why this model was chosen (see §3.3) |
| `capability_requirements` | list[string] | no | Capabilities required (e.g., `code_generation`, `reasoning`) |
| `input_context_ref` | string | yes | Reference to the prompt/context assembled by Context Engine |
| `input_token_count` | integer | no | Input tokens (populated after call) |
| `output` | ModelOutput | no | Model response (populated after call) |
| `output_token_count` | integer | no | Output tokens |
| `structured_output_status` | enum | no | One of: `none`, `valid`, `invalid`, `partial` |
| `latency_ms` | integer | no | Response latency |
| `cost` | float | no | Monetary cost |
| `fallback_used` | boolean | no | Whether a fallback model was used |
| `fallback_reason` | string | conditionally | Why fallback was triggered |
| `privacy` | PrivacyMetadata | no | Data residency and privacy info (see §3.4) |
| `status` | enum | yes | One of: `pending`, `streaming`, `completed`, `failed`, `timeout`, `cancelled` |
| `error` | NEXUSError | no | Error if failed |
| `created_at` | datetime | yes | When invocation was initiated |
| `completed_at` | datetime | no | When invocation completed |
| `correlation_id` | string | yes | End-to-end trace |
| `provenance` | ProvenanceRef | yes | Origin record |
| `metadata` | map[string,string] | no | Extension data |

### 3.3 RoutingDecision

| Field | Type | Required | Description |
|---|---|---|---|
| `strategy` | enum | yes | One of: `capability_match`, `cost_optimize`, `latency_optimize`, `privacy_first`, `local_first`, `manual_override` |
| `candidates` | list[string] | yes | Models considered |
| `selected_reason` | string | yes | Why this model was selected |
| `constraints_applied` | list[string] | no | Routing constraints |

### 3.4 PrivacyMetadata

| Field | Type | Required | Description |
|---|---|---|---|
| `data_leaves_machine` | boolean | yes | Whether data was sent to external provider |
| `provider jurisdiction` | string | no | Provider's data jurisdiction |
| `retention_policy` | string | no | Provider's data retention |
| `user_consent` | boolean | no | Whether user consented to external model |
| `sensitive_data_included` | boolean | no | Whether sensitive data was in prompt |

### 3.5 ModelOutput

| Field | Type | Required | Description |
|---|---|---|---|
| `content` | string | yes | Model response text |
| `finish_reason` | enum | yes | One of: `stop`, `length`, `tool_call`, `error` |
| `tool_calls` | list[ToolCall] | no | Tool calls requested by model |
| `confidence` | float | no | Model's self-reported confidence (if available) |

### 3.6 ToolCall

| Field | Type | Required | Description |
|---|---|---|---|
| `call_id` | string | yes | Unique tool call identifier |
| `tool_name` | string | yes | Tool to call |
| `parameters` | map[string,any] | yes | Tool parameters |

### 3.7 Model Invocation Rules

- Model output is **inference**, not fact — provenance must record this
- Privacy metadata required when data leaves the machine
- Local-first strategy: prefer local models unless capability requires remote
- Fallback must be recorded with reason
- Token counts and cost must be tracked for budgeting
- Model ≠ Agent identity — model output is not agent's own knowledge

---

## 4. Artifact Schema

### 4.1 Design Principle

Artifacts are **generated outputs** that persist beyond the execution that created them. They must be traceable, versioned, and governed.

### 4.2 Artifact Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version |
| `entity_type` | string | yes | Always `"artifact"` |
| `entity_id` | string | yes | Unique artifact identifier |
| `nexus_id` | string | yes | NEXUS installation identifier |
| `business_id` | string | yes | Business scope |
| `division_id` | string | no | Division scope |
| `artifact_type` | enum | yes | One of: `file`, `document`, `report`, `structured_data`, `code`, `model_output`, `intermediate` |
| `name` | string | yes | Human-readable name |
| `description` | string | no | What this artifact contains |
| `version` | integer | yes | Artifact version (starts at 1) |
| `content_ref` | string | yes | Reference to content (path, URI, or storage key) |
| `content_type` | string | yes | MIME type (e.g., `application/json`, `text/markdown`) |
| `hash` | string | yes | Content hash for integrity verification |
| `hash_algorithm` | string | yes | Hash algorithm (e.g., `sha256`) |
| `size_bytes` | integer | no | Content size |
| `creator_id` | string | yes | Who/what created this artifact |
| `creator_type` | enum | yes | One of: `agent`, `tool`, `model`, `human`, `system` |
| `owner_id` | string | yes | Who owns this artifact |
| `workflow_id` | string | conditionally | Workflow that produced this artifact |
| `task_id` | string | conditionally | Task that produced this artifact |
| `objective_id` | string | conditionally | Objective this artifact serves |
| `classification` | enum | yes | One of: `PUBLIC`, `INTERNAL`, `CONFIDENTIAL`, `RESTRICTED` |
| `retention` | RetentionPolicy | no | How long to keep (see §4.3) |
| `lifecycle` | enum | yes | One of: `active`, `archived`, `deleted` |
| `dependencies` | list[string] | no | Other artifact IDs this depends on |
| `verification_status` | enum | yes | One of: `unverified`, `verified`, `failed` |
| `verification_ref` | string | no | Reference to verification record |
| `created_at` | datetime | yes | When artifact was created |
| `updated_at` | datetime | no | When artifact was last modified |
| `expires_at` | datetime | no | When artifact should be cleaned up |
| `provenance` | ProvenanceRef | yes | Origin record |
| `metadata` | map[string,string] | no | Extension data |

### 4.3 RetentionPolicy

| Field | Type | Required | Description |
|---|---|---|---|
| `retain_until` | datetime | no | Keep until this date |
| `retain_for_days` | integer | no | Or keep for this many days |
| `archivable` | boolean | yes | Whether this can be archived |
| `deletable` | boolean | yes | Whether this can be deleted |
| `legal_hold` | boolean | no | Whether legal hold prevents deletion |

### 4.4 Artifact Rules

- Large binary data should not be embedded in event envelopes — use `content_ref`
- Integrity verified via `hash` comparison
- Artifacts inherit scope from their creator (business_id, division_id)
- Verification status must be tracked — unverified artifacts should be treated with caution
- Retention policy must be set at creation time
- Cross-business artifact access requires Governance policy

---

## 5. Relationships

```
Task
 ├── Tool Execution(s)
 │    ├── Authorization Context
 │    ├── Tool Output
 │    └── Artifact(s) produced
 └── Model Invocation(s)
      ├── Routing Decision
      ├── Model Output
      └── Tool Calls (if any)

Workflow
 ├── Task(s)
 └── Artifact(s) produced
```

---

## 6. Cross-Schema References

| Reference | Target Schema |
|---|---|
| `tool_execution.agent_id` | Agent Schema (SCHEMA_IDENTITIES_ORG.md) |
| `tool_execution.task_id` | Task Schema (SCHEMA_WORK_OBJECTIVES.md) |
| `tool_execution.policy_decision_ref` | Governance/Policy Schema |
| `model_invocation.input_context_ref` | Memory/Context (SCHEMA_MEMORY_KNOWLEDGE.md) |
| `artifact.workflow_id` | Workflow Schema (SCHEMA_WORK_OBJECTIVES.md) |
| `artifact.task_id` | Task Schema (SCHEMA_WORK_OBJECTIVES.md) |

---

*This document defines the execution domain of NEXUS. Tool Runtime is the controlled boundary; Model Router handles provider abstraction; Artifacts persist generated outputs.*
