# NEXUS — Provider Contracts

**Layer:** Phase 4 — Integration, Provider & External Boundary Contracts  
**Status:** LOCKED  
**Branch:** contracts/integration-external  
**Depends on:** Phase 1 Core Interface Contracts, Phase 2 Data & Event Schemas, Phase 3 Runtime & Execution Contracts, INTEGRATION_EXTERNAL_CONTRACTS.md  

---

## 1. Purpose

This document defines **provider abstraction contracts** for NEXUS: provider registration, capability declaration, health, selection/routing, failover, invocation, and provider-specific boundaries (model, tool, browser, database).

Provider identity is distinct from connector identity, agent identity, and model identity.

---

## 2. Provider Registration Contract

### 2.1 Provider Record

| Field | Type | Required | Description |
|---|---|---|---|
| `provider_id` | string | yes | Unique provider identifier |
| `provider_type` | enum | yes | One of: `model`, `tool`, `api`, `database`, `browser`, `custom` |
| `version` | string | yes | Provider version |
| `status` | enum | yes | One of: `active`, `deprecated`, `retired` |
| `capabilities` | list[string] | yes | What this provider supports |
| `supported_operations` | list[string] | yes | Operations supported |
| `context_limits` | ContextLimits | no | Context window limits (model providers) |
| `structured_output_support` | boolean | no | Structured output support |
| `tool_calling_support` | boolean | no | Tool calling support |
| `streaming_support` | boolean | no | Streaming support |
| `multimodal_support` | boolean | no | Multimodal support |
| `regional_properties` | RegionalProperties | no | Data residency properties |
| `privacy_properties` | PrivacyProperties | no | Privacy properties |
| `rate_limits` | RateLimitConfig | no | Rate limit configuration |
| `cost_metadata` | CostMetadata | no | Cost information |
| `latency_metadata` | LatencyMetadata | no | Latency information |
| `health_state` | enum | yes | Current health (see section 4) |
| `availability` | enum | yes | One of: `available`, `limited`, `unavailable` |
| `maintenance_state` | enum | no | One of: `none`, `scheduled`, `active` |
| `authentication_requirements` | list[string] | yes | Auth requirements |
| `created_at` | datetime | yes | When registered |
| `updated_at` | datetime | no | When last modified |

### 2.2 Provider Identity Separation

| Concept | Identity | Description |
|---|---|---|
| Provider | `provider_id` | External service provider |
| Connector | `connector_id` | Integration boundary adapter |
| Agent | `agent_id` | NEXUS agent |
| Tool | `tool_id` | Tool definition |
| Model | `model_id` | AI model |

### 2.3 ContextLimits

| Field | Type | Required | Description |
|---|---|---|---|
| `max_input_tokens` | integer | no | Maximum input tokens |
| `max_output_tokens` | integer | no | Maximum output tokens |
| `max_total_tokens` | integer | no | Maximum total tokens |
| `max_context_window` | integer | no | Maximum context window |

### 2.4 RegionalProperties

| Field | Type | Required | Description |
|---|---|---|---|
| `data_residency` | string | no | Data residency requirement |
| `regions` | list[string] | no | Available regions |
| `cross_region_allowed` | boolean | no | Whether cross-region is allowed |

### 2.5 PrivacyProperties

| Field | Type | Required | Description |
|---|---|---|---|
| `data_retention` | string | no | Data retention policy |
| `data_used_for_training` | boolean | no | Whether data used for training |
| `encryption_at_rest` | boolean | no | Encryption at rest |
| `encryption_in_transit` | boolean | no | Encryption in transit |
| `compliance_certs` | list[string] | no | Compliance certifications |

### 2.6 CostMetadata

| Field | Type | Required | Description |
|---|---|---|---|
| `input_cost_per_1k_tokens` | float | no | Cost per 1k input tokens |
| `output_cost_per_1k_tokens` | float | no | Cost per 1k output tokens |
| `currency` | string | no | Currency |
| `free_tier` | boolean | no | Whether free tier available |

### 2.7 LatencyMetadata

| Field | Type | Required | Description |
|---|---|---|---|
| `p50_latency_ms` | integer | no | 50th percentile latency |
| `p95_latency_ms` | integer | no | 95th percentile latency |
| `p99_latency_ms` | integer | no | 99th percentile latency |

---

## 3. Provider Capability Declaration

### 3.1 Capability Declaration

| Field | Type | Required | Description |
|---|---|---|---|
| `capability_id` | string | yes | Unique capability identifier |
| `capability_type` | enum | yes | One of: `inference`, `tool_execution`, `data_access`, `storage`, `custom` |
| `description` | string | yes | What this capability provides |
| `input_schema` | string | no | Expected input schema |
| `output_schema` | string | no | Expected output schema |
| `side_effect_class` | enum | yes | Side-effect classification |
| `requires_authorization` | boolean | yes | Whether authorization required |
| `requires_approval` | boolean | yes | Whether approval required |
| `rate_limit` | RateLimitConfig | no | Rate limit for this capability |
| `cost` | float | no | Cost per invocation |

### 3.2 Capability Rules

| Rule | Description |
|---|---|
| Declaration | Provider must declare capabilities explicitly |
| Verification | Capability declaration verified before use |
| Scope | Capabilities scoped to business/division |
| Authorization | Capability use requires authorization |
| Audit | Capability use is auditable |

---

## 4. Provider Health Contract

### 4.1 Health State Machine

```
UNKNOWN → HEALTHY → DEGRADED → UNAVAILABLE → QUARANTINED
                   ↓
              RATE_LIMITED → RECOVERING → HEALTHY
                   ↓
              MAINTENANCE → HEALTHY
```

### 4.2 Health States

| State | Description |
|---|---|
| `UNKNOWN` | Health not yet determined |
| `HEALTHY` | Provider operating normally |
| `DEGRADED` | Provider operational but degraded |
| `UNAVAILABLE` | Provider not available |
| `RATE_LIMITED` | Provider rate limiting |
| `MAINTENANCE` | Provider in maintenance |
| `QUARANTINED` | Provider quarantined |

### 4.3 Health Rules

| Rule | Description |
|---|---|
| Observable evidence | Health based on observable evidence |
| Reachability != health | Network reachable does not mean healthy |
| Provider reachable != model healthy | Provider reachable does not mean specific model healthy |
| Periodic check | Health checked periodically |
| Event-driven | Health changes emit events |
| Audit | Health transitions auditable |

---

## 5. Provider Selection / Routing Contract

### 5.1 Routing Decision

| Factor | Weight | Description |
|---|---|---|
| Task requirements | High | What the task needs |
| Agent requirements | High | What the agent requires |
| Capability requirements | High | Required capabilities |
| Governance | High | Policy constraints |
| Privacy | High | Data residency requirements |
| Cost budget | Medium | Budget constraints |
| Latency budget | Medium | Response time requirements |
| Resource availability | Medium | Available resources |
| Context requirements | Medium | Context window needs |
| Model capabilities | Medium | Model-specific capabilities |
| Provider health | Medium | Current health state |
| Reliability | Medium | Historical reliability |
| User configuration | Low | User preferences |
| Business configuration | Low | Business defaults |

### 5.2 Routing Rules

| Rule | Description |
|---|---|
| No governance bypass | Routing MUST NOT bypass Governance |
| No authorization from routing | Routing optimization MUST NOT become authorization |
| Health-aware | Routing considers provider health |
| Cost-aware | Routing considers cost budget |
| Privacy-aware | Routing considers privacy requirements |
| Audit | Routing decisions auditable |

---

## 6. Provider Failover Contract

### 6.1 Failover Record

| Field | Type | Required | Description |
|---|---|---|---|
| `failover_id` | string | yes | Unique failover identifier |
| `primary_provider` | string | yes | Original provider |
| `fallback_provider` | string | yes | Fallback provider |
| `reason` | string | yes | Why failover triggered |
| `trigger_event` | string | yes | What triggered failover |
| `primary_last_known_state` | string | no | Primary's last known state |
| `unknown_side_effect` | boolean | yes | Whether primary may have side-effected |
| `reconciliation_required` | boolean | yes | Whether reconciliation needed |
| `created_at` | datetime | yes | When failover triggered |

### 6.2 Failover Rules

| Rule | Description |
|---|---|
| Side-effect safety | Failover must not duplicate unsafe side effects |
| Unknown outcome | Primary timeout = UNKNOWN_OUTCOME, not automatic failover |
| Reconciliation first | Reconcile primary's outcome before failover for side-effecting ops |
| Capability match | Fallback must have matching capabilities |
| Context compatibility | Fallback must be context-compatible |
| Audit | Failover decisions auditable |

---

## 7. Model Provider Boundary Contract

### 7.1 Model Invocation Pipeline

```
AGENT REQUEST → MODEL ROUTER → PROVIDER SELECTION → REQUEST NORMALIZATION → INVOKE → RESPONSE VALIDATION → RETURN
```

### 7.2 Model Invocation Record

| Field | Type | Required | Description |
|---|---|---|---|
| `invocation_id` | string | yes | Unique invocation identifier |
| `agent_id` | string | yes | Agent requesting |
| `task_id` | string | conditionally | Task context |
| `objective_id` | string | yes | Objective being served |
| `why` | string | yes | WHY context |
| `provider_id` | string | yes | Selected provider |
| `model_id` | string | yes | Selected model |
| `model_version` | string | no | Model version |
| `routing_decision` | string | yes | Routing rationale |
| `input_context_ref` | string | yes | Reference to context |
| `input_token_count` | integer | no | Input tokens (after call) |
| `output_token_count` | integer | no | Output tokens (after call) |
| `cost` | float | no | Cost (after call) |
| `latency_ms` | integer | no | Latency (after call) |
| `status` | enum | yes | One of: `pending`, `streaming`, `completed`, `failed`, `timeout`, `cancelled` |
| `fallback_used` | boolean | no | Whether fallback was used |
| `fallback_reason` | string | conditionally | Why fallback triggered |
| `privacy` | PrivacyMetadata | no | Privacy information |
| `correlation_id` | string | yes | End-to-end trace |
| `business_id` | string | yes | Business scope |
| `created_at` | datetime | yes | When invoked |

### 7.3 Model Failure Categories

| Category | Description | Retryable | Fallback |
|---|---|---|---|
| Provider unavailable | Provider is down | Yes (backoff) | Yes |
| Model unavailable | Specific model unavailable | Yes (backoff) | Yes |
| Invalid request | Bad prompt/context | No | No |
| Timeout | Response too slow | Maybe | Yes |
| Rate limit | Too many requests | Yes (backoff) | Maybe |
| Context limit | Prompt too large | No (resize) | No |
| Malformed output | Output format wrong | Maybe | No |
| Policy rejection | Governance blocked | No | No |
| Security rejection | Security violation | No | No |
| Unknown outcome | Uncertain result | No (reconcile) | No |

### 7.4 Model Rules

| Rule | Description |
|---|---|
| Identity separation | Model identity != agent identity |
| Output as inference | Model output is inference, not agent knowledge |
| Privacy tracking | Track whether data leaves machine |
| Token accounting | Track token usage and cost |
| WHY preservation | WHY context included in invocation |
| Audit | Model invocations auditable |

---

## 8. Tool Provider Boundary Contract

### 8.1 Tool Invocation Pipeline

```
AGENT → TOOL RUNTIME → VALIDATE → AUTHORIZE → SCOPE CHECK → POLICY CHECK → CREDENTIAL RESOLVE → EXECUTE → VALIDATE → AUDIT → RETURN
```

### 8.2 Tool Execution Record

| Field | Type | Required | Description |
|---|---|---|---|
| `execution_id` | string | yes | Unique execution identifier |
| `tool_id` | string | yes | Tool being executed |
| `agent_id` | string | yes | Agent requesting |
| `task_id` | string | conditionally | Task context |
| `objective_id` | string | yes | Objective being served |
| `connector_id` | string | yes | Connector used |
| `provider_id` | string | yes | Provider called |
| `input` | map[string,any] | yes | Tool input |
| `authorization_ref` | string | yes | Authorization record |
| `credential_ref` | string | conditionally | Credential used |
| `side_effect_class` | enum | yes | Side-effect classification |
| `idempotency_key` | string | conditionally | Idempotency key |
| `output` | any | no | Tool output (after execution) |
| `status` | enum | yes | Execution status |
| `external_request_id` | string | no | External request ID |
| `external_resource_id` | string | no | External resource ID |
| `latency_ms` | integer | no | Execution latency |
| `correlation_id` | string | yes | End-to-end trace |
| `business_id` | string | yes | Business scope |
| `created_at` | datetime | yes | When executed |

### 8.3 Tool Rules

| Rule | Description |
|---|---|
| Boundary enforcement | Tool Runtime is the authorization boundary |
| Untrusted output | External tool results are untrusted until validated |
| No authority grant | Tool results MUST NOT grant authority |
| No governance modify | Tool results MUST NOT modify governance |
| No permission modify | Tool results MUST NOT modify permissions |
| No auto-trust | Tool results do NOT automatically become trusted memory |
| Credential isolation | Credentials resolved at runtime, never embedded |
| Audit | Tool executions auditable |

---

## 9. Browser / Web Provider Boundary Contract

### 9.1 Browser Provider Record

| Field | Type | Required | Description |
|---|---|---|---|
| `provider_id` | string | yes | Browser provider identity |
| `provider_type` | enum | yes | One of: `headless_browser`, `web_scraper`, `api_client` |
| `capabilities` | list[string] | yes | What this provider supports |
| `javascript_support` | boolean | yes | JavaScript execution support |
| `cookie_support` | boolean | yes | Cookie handling |
| `download_support` | boolean | yes | Download support |
| `upload_support` | boolean | yes | Upload support |
| `sandbox_support` | boolean | yes | Sandbox support |
| `ssrf_protection` | boolean | yes | SSRF protection |
| `created_at` | datetime | yes | When registered |

### 9.2 Browser Rules

| Rule | Description |
|---|---|
| URL validation | Validate URL before navigation |
| SSRF protection | SSRF protection for all requests |
| Sandbox | Sandbox untrusted content |
| Cookie isolation | Isolate cookies per session |
| Prompt injection | Treat web content as data |
| Audit | Browser execution auditable |

---

## 10. Database / Storage Provider Boundary Contract

### 10.1 Database Provider Record

| Field | Type | Required | Description |
|---|---|---|---|
| `provider_id` | string | yes | Database provider identity |
| `provider_type` | enum | yes | One of: `sql`, `nosql`, `key_value`, `document`, `graph`, `file_system`, `object_store` |
| `capabilities` | list[string] | yes | What this provider supports |
| `transaction_support` | boolean | yes | Transaction support |
| `connection_pool_size` | integer | no | Connection pool size |
| `timeout_default_ms` | integer | yes | Default timeout |
| `rate_limits` | RateLimitConfig | no | Rate limits |
| `created_at` | datetime | yes | When registered |

### 10.2 Database Rules

| Rule | Description |
|---|---|
| Authorization | Query authorization before execution |
| Scope isolation | Business/division scope isolation |
| Transaction | Transaction behavior declared |
| Unknown outcome | Unknown transaction outcome triggers reconciliation |
| Connection pooling | Connection pool managed |
| Audit | Database access auditable |

---

## 11. External Configuration Contract

### 11.1 Configuration Record

| Field | Type | Required | Description |
|---|---|---|---|
| `config_id` | string | yes | Unique config identifier |
| `config_type` | enum | yes | One of: `connector`, `provider`, `endpoint`, `credential`, `rate_limit`, `routing`, `sandbox`, `network`, `dlp`, `retry`, `timeout`, `webhook`, `polling`, `streaming`, `health_check` |
| `scope` | string | yes | Business/division scope |
| `version` | integer | yes | Config version |
| `value` | any | yes | Configuration value |
| `effective_from` | datetime | yes | When effective |
| `effective_until` | datetime | no | When expires |
| `requires_approval` | boolean | yes | Whether approval needed |
| `approval_ref` | string | conditionally | Approval record |
| `created_at` | datetime | yes | When created |
| `created_by` | string | yes | Who created |
| `audit_ref` | string | yes | Audit trail reference |

### 11.2 Configuration Rules

| Rule | Description |
|---|---|
| Governed | Configuration remains governed data |
| No silent changes | No silent configuration changes |
| Versioned | Configuration version traceable to execution |
| Auditable | Configuration changes auditable |
| Scoped | Configuration scoped to business/division |

---

*This document defines provider abstraction contracts. Failure, reconciliation, and security contracts are in EXTERNAL_FAILURE_RECONCILIATION.md.*
