# NEXUS — Common Schema Envelope

**Layer:** Phase 2 — Data & Event Schemas  
**Status:** LOCKED  
**Branch:** contracts/data-event-schemas  
**Depends on:** Phase 1 Core Interface Contracts  

---

## 1. Purpose

This document defines the **common metadata envelope** shared across all NEXUS data and event schemas. It establishes the reusable structures that every entity and event carries for scope, traceability, provenance, security, and lifecycle management.

---

## 2. Field Categories

Fields are classified into three categories:

| Category | Meaning |
|---|---|
| **Universally Required** | Must be present on every schema instance |
| **Conditionally Required** | Required only when a specific condition is met (documented per field) |
| **Optional** | May be present; semantics defined if present |

---

## 3. Common Metadata Envelope

### 3.1 Schema Identity

| Field | Type | Category | Description |
|---|---|---|---|
| `schema_version` | string | Universal Required | Version of this schema (e.g., `1.0.0`) |
| `entity_type` | string | Universal Required | Canonical entity type (e.g., `objective`, `workflow`, `agent`) |

### 3.2 Entity Identity

| Field | Type | Category | Description |
|---|---|---|---|
| `entity_id` | string | Universal Required | Unique identifier for this entity instance |
| `nexus_id` | string | Universal Required | Global NEXUS installation identifier |

### 3.3 Scope

| Field | Type | Category | Description |
|---|---|---|---|
| `business_id` | string | Conditionally Required | Required on all business-scoped objects. Absent only for truly global entities. |
| `division_id` | string | Optional | Division scope within a business. Present when entity is division-scoped. |
| `agent_id` | string | Conditionally Required | Required when entity is agent-specific (task assignment, tool execution, etc.) |
| `workflow_id` | string | Conditionally Required | Required when entity belongs to a workflow |
| `task_id` | string | Conditionally Required | Required when entity is task-scoped |
| `objective_id` | string | Conditionally Required | Required when entity serves an objective |

### 3.4 Actor

| Field | Type | Category | Description |
|---|---|---|---|
| `actor_id` | string | Conditionally Required | Required when an actor initiated the action. May be human, agent, or system. |
| `actor_type` | string | Conditionally Required | Required when `actor_id` is present. One of: `human`, `agent`, `system`, `service` |

### 3.5 Traceability

| Field | Type | Category | Description |
|---|---|---|---|
| `correlation_id` | string | Universal Required | End-to-end trace identifier. Generated at entry point, propagated through all calls. |
| `causation_id` | string | Conditionally Required | The event or action that directly caused this entity/event. Absent for entry points. |
| `parent_id` | string | Optional | Parent entity in a hierarchical relationship (e.g., parent objective, parent workflow) |

### 3.6 Timestamps

| Field | Type | Category | Description |
|---|---|---|---|
| `created_at` | datetime | Universal Required | When this entity was created |
| `updated_at` | datetime | Optional | When this entity was last modified. Absent for immutable events. |
| `occurred_at` | datetime | Conditionally Required | When the represented fact/occurrence happened (events). May differ from `created_at`. |
| `expires_at` | datetime | Optional | When this entity ceases to be valid or should be cleaned up |

### 3.7 Provenance

| Field | Type | Category | Description |
|---|---|---|---|
| `provenance` | ProvenanceRef | Universal Required | Reference to the origin and history of this data (see §4) |
| `source` | string | Universal Required | Where this data originated (e.g., `user_input`, `tool_result`, `model_inference`, `event`) |

### 3.8 Security

| Field | Type | Category | Description |
|---|---|---|---|
| `security_context` | SecurityContext | Optional | Security metadata when sensitivity is relevant (see §5) |
| `classification` | enum | Optional | One of: `PUBLIC`, `INTERNAL`, `CONFIDENTIAL`, `RESTRICTED`. Default: `INTERNAL`. |

### 3.9 Lifecycle

| Field | Type | Category | Description |
|---|---|---|---|
| `status` | string | Conditionally Required | Current lifecycle state. Values are entity-specific (see state machines in Phase 1). |
| `version` | integer | Optional | Entity version for optimistic concurrency. Starts at 1, increments on each update. |

### 3.10 Metadata

| Field | Type | Category | Description |
|---|---|---|---|
| `metadata` | map[string]string | Optional | Arbitrary key-value pairs for extension. Keys must be namespaced (e.g., `nexus.vendor.field`). |

---

## 4. Provenance Model

Provenance answers: *Where did this data come from?*

### 4.1 ProvenanceRef Structure

| Field | Type | Required | Description |
|---|---|---|---|
| `origin` | string | yes | Original source (e.g., `human`, `tool`, `model`, `external_api`, `system`) |
| `producer` | string | yes | Specific producer (e.g., `agent:research-001`, `tool:web_search`, `model:llama3`) |
| `produced_at` | datetime | yes | When this data was produced |
| `source_reference` | string | no | Reference to the source material (URL, file path, tool call ID) |
| `input_hash` | string | no | Hash of the input that produced this data |
| `chain` | list[ProvenanceStep] | no | Full provenance chain for derived data |

### 4.2 ProvenanceStep

| Field | Type | Required | Description |
|---|---|---|---|
| `step_id` | string | yes | Unique step identifier |
| `actor` | string | yes | Who/what performed this step |
| `action` | string | yes | What was done |
| `input_ref` | string | no | Reference to input data |
| `output_ref` | string | no | Reference to output data |
| `timestamp` | datetime | yes | When this step occurred |

### 4.3 Provenance Rules

- Provenance is **evidence**, not proof of truth.
- Model-inferred data must be distinguishable from observed data.
- Provenance chains must be append-only (no silent deletion of steps).
- Cross-business provenance requires explicit Governance policy.

---

## 5. Security Context

### 5.1 SecurityContext Structure

| Field | Type | Required | Description |
|---|---|---|---|
| `classification` | enum | yes | `PUBLIC`, `INTERNAL`, `CONFIDENTIAL`, `RESTRICTED` |
| `sensitivity` | string | no | Additional sensitivity label (e.g., `PII`, `PHI`, `financial`) |
| `access_scope` | list[string] | no | Explicit list of entities allowed to access this data |
| `retention` | string | no | Retention requirement (e.g., `30d`, `1y`, `indefinite`) |
| `encrypted` | boolean | no | Whether data is encrypted at rest |
| `credential_ref` | string | no | Reference to a credential/secret. **Never** embed raw secrets. |

### 5.2 Security Rules

- Raw credentials (passwords, API keys, tokens, private keys) are **prohibited** in all schema objects.
- Use `credential_ref` to reference secrets stored in a secure vault.
- `classification` defaults to `INTERNAL` when absent.
- Cross-classification access requires Governance policy.

---

## 6. Scope Semantics

### 6.1 Scope Hierarchy

```
GLOBAL (nexus_id only)
  └─ BUSINESS (business_id)
       └─ DIVISION (division_id)
            └─ AGENT (agent_id)
                 └─ WORKFLOW (workflow_id)
                      └─ TASK (task_id)
                           └─ TEMPORARY (expires_at bounded)
```

### 6.2 Scope Rules

| Rule | Description |
|---|---|
| **向下可见** | A parent scope can see its children. Business can see Division, Division can see Agent, etc. |
| **向上不可见** | A child scope cannot see parent data unless explicitly authorized. |
| **横向隔离** | Siblings (e.g., Business A and Business B) cannot see each other's data. |
| **跨域需授权** | Cross-scope access requires explicit Governance policy + audit trail. |
| **默认最小权限** | When scope is ambiguous, default to the narrowest possible scope. |

### 6.3 Scope Field Requirements by Entity Type

| Entity | `business_id` | `division_id` | `agent_id` | `workflow_id` | `task_id` |
|---|---|---|---|---|---|
| Global Config | optional | prohibited | prohibited | prohibited | prohibited |
| Business | required | optional | prohibited | prohibited | prohibited |
| Division | required | required | prohibited | prohibited | prohibited |
| Agent | required | optional | required | optional | optional |
| Workflow | required | optional | optional | required | optional |
| Task | required | optional | required | optional | required |
| Objective | required | optional | optional | optional | optional |
| Memory | required | optional | optional | optional | optional |
| Event | required | optional | optional | optional | optional |

---

## 7. ID Format Convention

All IDs in NEXUS follow the format:

```
{nexus_prefix}:{entity_type}:{unique_part}
```

Examples:
- `nx:objective:abc123`
- `nx:workflow:def456`
- `nx:agent:ghi789`
- `nx:task:jkl012`

The `nexus_prefix` (`nx`) identifies the NEXUS installation. The `entity_type` enables type-safe lookups. The `unique_part` is a UUID or equivalent.

For multi-business isolation, the `business_id` is a separate field, not embedded in the entity ID. This prevents accidental cross-business addressability.

---

## 8. Timestamp Rules

| Rule | Description |
|---|---|
| **UTC only** | All timestamps must be in UTC (ISO 8601 format) |
| **Precision** | Millisecond precision minimum |
| **Clock skew** | Acceptable skew: ≤500ms. Events outside this window must be flagged. |
| **Monotonicity** | Within a single entity, `updated_at` must be monotonically non-decreasing |
| **Immutability** | `created_at` is immutable after initial write |
| **Occurrence vs Creation** | `occurred_at` (when it happened) may differ from `created_at` (when NEXUS recorded it) |

---

## 9. Versioning Rules

### 9.1 Schema Version

Format: `MAJOR.MINOR.PATCH`

- **MAJOR**: Breaking change (field removed, type changed, semantics changed)
- **MINOR**: Backward-compatible addition (new optional field)
- **PATCH**: Clarification or bug fix (no semantic change)

### 9.2 Entity Version

- Integer, starts at 1
- Increments on every mutation
- Used for optimistic concurrency control
- Absent on immutable entities (events, audit records)

### 9.3 Compatibility Rules

- Consumers **must** ignore unknown fields
- Producers **must not** remove or rename existing required fields
- New optional fields may be added without MAJOR version bump
- Breaking changes require a new schema document or MAJOR version increment

---

## 10. Correlation vs Causation

| Concept | Field | Meaning |
|---|---|---|
| **Correlation** | `correlation_id` | Groups all events/actions related to a single end-to-end request. Generated once at entry point. |
| **Causation** | `causation_id` | Identifies the direct cause of this event/action. Links to a specific prior event. |
| **Parent** | `parent_id` | Hierarchical relationship (e.g., parent objective, parent workflow). |

### 10.1 Rules

- `correlation_id` is the same across an entire request chain
- `causation_id` changes with each step (A causes B, B causes C)
- `correlation_id` ≠ authorization
- `correlation_id` ≠ authority
- `correlation_id` is for tracing only

---

## 11. Metadata Extension Rules

- Keys must be namespaced: `{vendor}.{domain}.{field}`
- Example: `acme.crm.lead_score`
- Metadata must not override or contradict canonical fields
- Metadata must not be used to bypass Governance or Security
- Maximum key length: 128 characters
- Maximum value length: 1024 characters
- Maximum entries per entity: 50

---

*This document is the foundational envelope for all NEXUS data and event schemas. All subsequent schema documents reference this envelope.*
