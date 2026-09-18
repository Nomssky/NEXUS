# NEXUS — Event Envelope, Event Semantics, Event Delivery & Trigger Schemas

**Layer:** Phase 2 — Data & Event Schemas  
**Status:** LOCKED  
**Branch:** contracts/data-event-schemas  
**Depends on:** SCHEMA_COMMON.md, Phase 1 Core Interface Contracts  

---

## 1. Purpose

This document defines canonical schemas for the **event and trigger domain**: the event envelope (how events are structured), event semantics (what different event types mean), event delivery (how events are transported), and triggers (what causes actions). Events represent facts/occurrences — they do not grant authority.

---

## 2. Event Envelope

### 2.1 Design Principle

An event is a **record of something that happened**. Events are facts, not commands. An event may trigger a decision but MUST NOT itself bypass Governance.

### 2.2 Event Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version |
| `entity_type` | string | yes | Always `"event"` |
| `event_id` | string | yes | Unique event identifier (UUID) |
| `event_type` | string | yes | Canonical event type (see §3) |
| `event_version` | string | yes | Version of this event type's schema |
| `nexus_id` | string | yes | NEXUS installation identifier |
| `business_id` | string | yes | Business scope |
| `division_id` | string | no | Division scope |
| `occurred_at` | datetime | yes | When the represented fact happened |
| `emitted_at` | datetime | yes | When this event was emitted |
| `producer` | EventProducer | yes | Who/what emitted this event (see §2.3) |
| `actor` | EventActor | conditionally | Who initiated the action that caused this event |
| `workflow_id` | string | conditionally | Related workflow |
| `task_id` | string | conditionally | Related task |
| `objective_id` | string | conditionally | Related objective |
| `correlation_id` | string | yes | End-to-end trace identifier |
| `causation_id` | string | conditionally | The event/action that directly caused this event |
| `parent_event_id` | string | conditionally | Parent event in a nested event structure |
| `payload` | any | yes | Event-specific data (see §2.4) |
| `payload_schema` | string | yes | Schema reference for payload validation |
| `provenance` | ProvenanceRef | yes | Origin record |
| `priority` | enum | no | One of: `critical`, `high`, `medium`, `low`, `background` |
| `classification` | enum | no | Data classification (default: `INTERNAL`) |
| `idempotency_key` | string | conditionally | Required for commands; ensures effectively-once processing |
| `deduplication_key` | string | conditionally | Key for deduplication within a time window |
| `ordering` | EventOrdering | no | Ordering metadata (see §6) |
| `ttl_seconds` | integer | no | Event validity window |
| `expires_at` | datetime | no | When this event should be discarded if not processed |

### 2.3 EventProducer

| Field | Type | Required | Description |
|---|---|---|---|
| `producer_id` | string | yes | ID of the producing module/entity |
| `producer_type` | enum | yes | One of: `module`, `agent`, `tool`, `external`, `human`, `system` |
| `module_name` | string | conditionally | Required when `producer_type` is `module` (e.g., `objective_engine`, `workflow_orchestration`) |

### 2.4 EventActor

| Field | Type | Required | Description |
|---|---|---|---|
| `actor_id` | string | yes | Who initiated the action |
| `actor_type` | enum | yes | One of: `human`, `agent`, `system`, `service` |

### 2.5 Payload Rules

- Payload must conform to `payload_schema`
- Payload must not contain raw credentials
- Payload must not contain authority grants
- Payload must not override governance decisions
- Large payloads should use references, not inline data
- Payload must be serializable (JSON-compatible)

---

## 3. Event Types

### 3.1 Event Type Registry

Event types follow the pattern: `{domain}.{entity}.{action}`

| Domain | Entity | Actions |
|---|---|---|
| `objective` | `objective` | `created`, `decomposed`, `assigned`, `started`, `completed`, `failed`, `cancelled`, `evaluated`, `reprioritized` |
| `workflow` | `workflow` | `created`, `dispatched`, `started`, `paused`, `resumed`, `completed`, `failed`, `blocked`, `cancelled` |
| `task` | `task` | `created`, `assigned`, `started`, `completed`, `failed`, `timeout`, `cancelled`, `approval_requested`, `approval_decided` |
| `agent` | `agent` | `created`, `initialized`, `started`, `paused`, `suspended`, `terminated`, `error` |
| `decision` | `decision` | `proposed`, `reviewed`, `approved`, `denied`, `executed`, `succeeded`, `failed` |
| `approval` | `approval` | `requested`, `approved`, `denied`, `expired`, `cancelled`, `delegated` |
| `tool` | `tool_execution` | `started`, `completed`, `failed`, `timeout`, `cancelled` |
| `model` | `model_invocation` | `started`, `completed`, `failed`, `timeout` |
| `memory` | `memory` | `created`, `updated`, `invalidated`, `promoted`, `archived`, `deleted` |
| `knowledge` | `knowledge` | `created`, `updated`, `superseded`, `contradicted` |
| `attention` | `attention` | `triggered`, `escalated`, `acknowledged`, `resolved`, `expired` |
| `governance` | `policy_decision` | `evaluated`, `allowed`, `denied`, `escalated` |
| `security` | `security_event` | `violation_detected`, `threat_blocked`, `anomaly_found` |
| `configuration` | `config` | `changed`, `approved`, `rolled_back` |
| `system` | `system` | `startup`, `shutdown`, `health_check`, `error` |

### 3.2 Event Type Versioning

- Event type version is independent of schema version
- Breaking changes to event payload require new `event_version`
- New optional fields may be added without version bump
- Consumers must ignore unknown payload fields

---

## 4. Event Semantics

### 4.1 Semantic Categories

| Category | Pattern | Description | Response |
|---|---|---|---|
| **Command** | `domain.command` | Request to perform an action | Ack + Result or Failure |
| **Event** | `domain.entity.action` | Record of something that happened | No response expected |
| **Notification** | `domain.notification` | Acknowledged event requiring confirmation | Ack required |
| **Request** | `domain.request` | Synchronous request for data/action | Response required |
| **Response** | `domain.response` | Reply to a request | None (is the reply) |
| **Ack** | `domain.ack` | Acknowledgment of receipt | None |
| **Result** | `domain.result` | Outcome of a command | None (is the result) |
| **Failure** | `domain.failure` | Outcome of a failed operation | May trigger retry |
| **Cancellation** | `domain.cancel` | Request to cancel an operation | Ack + propagation |

### 4.2 Semantic Rules

| Rule | Description |
|---|---|
| Event ≠ Command | An event records; a command requests action |
| Event ≠ Approval | An event may trigger approval; it does not grant it |
| Event ≠ Authority | An event carries information, not permission |
| Event ≠ Truth | An event records a claim; verification is separate |
| Notification requires Ack | If ack is not received, retry per policy |
| Command requires Result/Failure | Every command must eventually produce a result or failure |
| Cancellation propagates | Cancellation must flow to all affected entities |

---

## 5. Event Payload Reference

For each event type, the payload must reference the appropriate schema:

| Event Type | Payload Schema |
|---|---|
| `objective.*` | Objective Schema (SCHEMA_WORK_OBJECTIVES.md) |
| `workflow.*` | Workflow Schema (SCHEMA_WORK_OBJECTIVES.md) |
| `task.*` | Task Schema (SCHEMA_WORK_OBJECTIVES.md) |
| `agent.*` | Agent Schema (SCHEMA_IDENTITIES_ORG.md) |
| `decision.*` | Decision Schema (SCHEMA_WORK_OBJECTIVES.md) |
| `approval.*` | Approval Schema (SCHEMA_WORK_OBJECTIVES.md) |
| `tool.*` | Tool Execution Schema (SCHEMA_EXECUTION.md) |
| `model.*` | Model Invocation Schema (SCHEMA_EXECUTION.md) |
| `memory.*` | Memory Schema (SCHEMA_MEMORY_KNOWLEDGE.md) |
| `knowledge.*` | Knowledge Schema (SCHEMA_MEMORY_KNOWLEDGE.md) |
| `attention.*` | Attention Schema (SCHEMA_GOVERNANCE_ATTENTION.md) |
| `governance.*` | Policy Decision Schema (SCHEMA_GOVERNANCE_ATTENTION.md) |
| `security.*` | Security Event (SCHEMA_GOVERNANCE_ATTENTION.md) |
| `configuration.*` | Configuration Schema (SCHEMA_OBSERVABILITY_CONFIG.md) |
| `system.*` | System-specific payload |

---

## 6. Event Ordering

### 6.1 Ordering Metadata

| Field | Type | Required | Description |
|---|---|---|---|
| `sequence_number` | integer | conditionally | Monotonic sequence within a stream |
| `stream_id` | string | conditionally | Stream/partition identifier |
| `causal_chain` | list[string] | no | List of causation_ids showing causal ancestry |

### 6.2 Ordering Semantics

| Scope | Ordering Guarantee | Description |
|---|---|---|
| **Global** | NOT guaranteed | Events across the entire NEXUS may arrive out of order |
| **Business** | Best-effort | Events within a business are typically ordered but not guaranteed |
| **Workflow** | Guaranteed within stream | Events within a workflow stream are ordered |
| **Entity** | Guaranteed | Events for a single entity are ordered by sequence_number |
| **Causal** | Guaranteed | If A causes B, B's `causation_id` references A, ensuring causal ordering is reconstructable |

### 6.3 Ordering Rules

- Do not assume global ordering
- Use `causation_id` to reconstruct causal chains
- Use `sequence_number` for entity-level ordering
- Use `stream_id` for workflow-level ordering
- Consumers must handle out-of-order delivery within business scope

---

## 7. Event Delivery

### 7.1 Delivery Patterns

| Pattern | Description | Guarantee |
|---|---|---|
| `at_most_once` | Fire and forget; message may be lost | No retry, no ack |
| `at_least_once` | Message may be duplicated, never lost | Retry until ack |
| `effectively_once` | Processed exactly once | Requires idempotency_key |

### 7.2 Delivery Configuration

| Field | Type | Required | Description |
|---|---|---|---|
| `delivery_guarantee` | enum | yes | One of: `at_most_once`, `at_least_once`, `effectively_once` |
| `max_retries` | integer | no | Maximum retry attempts (default: 3) |
| `retry_delay_ms` | integer | no | Initial retry delay |
| `backoff_multiplier` | float | no | Retry backoff multiplier |
| `max_delay_ms` | integer | no | Maximum retry delay |
| `dedup_window_seconds` | integer | no | Deduplication time window |

### 7.3 Consumer Deduplication Rules

- If `delivery_guarantee` is `at_least_once`, consumers MUST handle duplicates
- Use `event_id` for basic deduplication
- Use `idempotency_key` for business-level idempotency
- Dedup window should be configurable per consumer
- Duplicates beyond the dedup window must be processed as new events

### 7.4 Delivery Channels

| Channel | Description | Use Case |
|---|---|---|
| `queue` | Point-to-point message queue | Commands, async results |
| `pub_sub` | Publish-subscribe broadcast | Events, notifications |
| `stream` | Ordered event stream | Workflow events, audit |
| `webhook` | HTTP callback | External system integration |
| `polling` | Consumer polls for events | Legacy integration |

---

## 8. Deduplication & Idempotency

### 8.1 Deduplication Key

- Used for infrastructure-level deduplication
- Typically `event_id` or a hash of (event_type, entity_id, occurred_at)
- Applied within a time window

### 8.2 Idempotency Key

- Used for business-level idempotency
- Set by the producer to ensure the same action is not applied twice
- Must be unique per logical operation
- Consumer must check idempotency_key before processing
- Idempotency window is typically longer than dedup window

### 8.3 Rules

- `event_id` alone may not be sufficient for business idempotency
- Use `idempotency_key` for operations with side effects
- Idempotency is the producer's responsibility
- Deduplication is the infrastructure's responsibility
- Both must be supported in schema

---

## 9. Trigger Schema

### 9.1 Design Principle

Triggers define **what causes actions**. Trigger definitions must not directly bypass Governance.

### 9.2 Trigger Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version |
| `entity_type` | string | yes | Always `"trigger"` |
| `trigger_id` | string | yes | Unique trigger identifier |
| `nexus_id` | string | yes | NEXUS installation identifier |
| `business_id` | string | yes | Business scope |
| `trigger_type` | enum | yes | One of: `event`, `schedule`, `condition`, `state`, `sequence`, `webhook`, `threshold`, `change_detection` (see §9.3) |
| `status` | enum | yes | One of: `enabled`, `disabled`, `paused` |
| `source` | TriggerSource | yes | What this trigger watches (see §9.4) |
| `condition` | TriggerCondition | no | Additional conditions for firing |
| `target` | TriggerTarget | yes | What to invoke when triggered (see §9.5) |
| `objective_id` | string | conditionally | Objective context for the trigger |
| `priority` | enum | no | Trigger priority |
| `cooldown_seconds` | integer | no | Minimum time between firings |
| `debounce_seconds` | integer | no | Debounce window |
| `aggregation` | AggregationConfig | no | How to aggregate multiple triggers |
| `rate_limit` | RateLimitConfig | no | Rate limiting |
| `expiration` | datetime | no | When this trigger expires |
| `max_firings` | integer | no | Maximum number of firings |
| `firing_count` | integer | yes | Current firing count |
| `created_at` | datetime | yes | When trigger was created |
| `updated_at` | datetime | no | When trigger was last modified |
| `last_fired_at` | datetime | no | When trigger last fired |
| `provenance` | ProvenanceRef | yes | Origin record |
| `metadata` | map[string,string] | no | Extension data |

### 9.3 Trigger Types

| Type | Description | Source |
|---|---|---|
| `event` | Fires when a specific event occurs | Event type filter |
| `schedule` | Fires on a schedule (cron-like) | Schedule expression |
| `condition` | Fires when a condition becomes true | Condition expression |
| `state` | Fires when entity state changes | Entity type + state filter |
| `sequence` | Fires when a sequence of events occurs | Event sequence definition |
| `webhook` | Fires on external HTTP callback | Webhook URL |
| `threshold` | Fires when a metric crosses threshold | Metric + threshold |
| `change_detection` | Fires when data changes | Data source + change filter |

### 9.4 TriggerSource

| Field | Type | Required | Description |
|---|---|---|---|
| `source_type` | enum | yes | One of: `event`, `schedule`, `entity`, `metric`, `external` |
| `event_type` | string | conditionally | Required for event triggers |
| `entity_type` | string | conditionally | Required for state/entity triggers |
| `schedule` | string | conditionally | Required for schedule triggers (cron expression) |
| `metric_name` | string | conditionally | Required for threshold triggers |
| `webhook_path` | string | conditionally | Required for webhook triggers |

### 9.5 TriggerTarget

| Field | Type | Required | Description |
|---|---|---|---|
| `target_type` | enum | yes | One of: `workflow`, `task`, `agent`, `governance`, `attention`, `custom` |
| `target_id` | string | yes | ID of the target entity or action |
| `input_template` | map[string,any] | no | Template for input data |
| `async` | boolean | yes | Whether to invoke asynchronously |

### 9.6 Trigger Rules

- Triggers MUST NOT bypass Governance
- Triggers that cause side effects must be auditable
- Trigger cooldown/debounce must be respected
- Expired triggers must not fire
- Trigger firing must be logged
- Rate limiting must be enforced

---

## 10. Relationships

```
Trigger
 ├── watches → Event / Schedule / Condition / State
 └── invokes → Workflow / Task / Agent / Governance / Attention

Event
 ├── caused_by → causation_id (prior event/action)
 ├── correlated_with → correlation_id (same request chain)
 └── triggers → Trigger → Action
```

---

## 11. Cross-Schema References

| Reference | Target Schema |
|---|---|
| `event.payload` (objective.*) | Objective Schema (SCHEMA_WORK_OBJECTIVES.md) |
| `event.payload` (workflow.*) | Workflow Schema (SCHEMA_WORK_OBJECTIVES.md) |
| `event.payload` (task.*) | Task Schema (SCHEMA_WORK_OBJECTIVES.md) |
| `event.payload` (agent.*) | Agent Schema (SCHEMA_IDENTITIES_ORG.md) |
| `event.payload` (tool.*) | Tool Execution Schema (SCHEMA_EXECUTION.md) |
| `event.payload` (model.*) | Model Invocation Schema (SCHEMA_EXECUTION.md) |
| `trigger.target` | Target entity schemas |

---

*This document defines the event and trigger domain of NEXUS. Events are facts, not commands. Triggers cause actions but must not bypass Governance.*
