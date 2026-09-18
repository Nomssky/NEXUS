# NEXUS — Identity, Business, Division & Agent Schemas

**Layer:** Phase 2 — Data & Event Schemas  
**Status:** LOCKED  
**Branch:** contracts/data-event-schemas  
**Depends on:** SCHEMA_COMMON.md, Phase 1 Core Interface Contracts  

---

## 1. Purpose

This document defines canonical schemas for **who exists in NEXUS**: identities, businesses, divisions, and agents. These schemas establish the structural foundation for scope isolation, authority resolution, and agent lifecycle management.

---

## 2. Identity Schema

### 2.1 Design Principle

**Identity ≠ Authority ≠ Capability ≠ Permission ≠ Trust**

An Identity establishes *who* something is. It does not grant permission to do anything. Authority, capability, permission, and trust are separate concepts resolved through Governance.

### 2.2 Identity Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version (inherited from Common Envelope) |
| `entity_type` | string | yes | Always `"identity"` |
| `entity_id` | string | yes | Unique identity identifier |
| `nexus_id` | string | yes | NEXUS installation identifier |
| `identity_type` | enum | yes | One of: `human`, `agent`, `service`, `device`, `system` |
| `display_name` | string | yes | Human-readable name |
| `status` | enum | yes | One of: `active`, `suspended`, `revoked`, `pending` |
| `created_at` | datetime | yes | When identity was created |
| `updated_at` | datetime | no | When identity was last modified |
| `expires_at` | datetime | no | When identity ceases to be valid (e.g., temporary agents) |
| `business_id` | string | conditionally | Required for business-scoped identities. Absent for global/system identities. |
| `division_id` | string | no | Division scope within a business |
| `parent_id` | string | no | Parent identity (e.g., spawner agent, managing human) |
| `provenance` | ProvenanceRef | yes | Origin of this identity |
| `metadata` | map[string,string] | no | Extension data |

### 2.3 Identity Types

| Type | Description | Scope |
|---|---|---|
| `human` | A human member/operator | Business + optional Division |
| `agent` | An AI agent (permanent or temporary) | Business + optional Division |
| `service` | An external service integration | Business or Global |
| `device` | A physical device (e.g., IoT sensor) | Business |
| `system` | NEXUS system itself | Global |

### 2.4 Identity Rules

- An identity is established through authentication (who are you?)
- An identity does NOT carry authority (what may you do?)
- An identity does NOT carry capability (what CAN you do?)
- An identity does NOT carry permission (what are you GRANTED?)
- An identity does NOT carry trust (how confident are we in you?)
- All four are resolved separately through Governance

---

## 3. Business Schema

### 3.1 Business Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version |
| `entity_type` | string | yes | Always `"business"` |
| `entity_id` | string | yes | Unique business identifier |
| `nexus_id` | string | yes | NEXUS installation identifier |
| `business_id` | string | yes | Self-reference for scope enforcement |
| `name` | string | yes | Business name |
| `status` | enum | yes | One of: `active`, `suspended`, `archived` |
| `created_at` | datetime | yes | When business was onboarded |
| `updated_at` | datetime | no | When business config was last modified |
| `owner_identity_id` | string | yes | Primary human owner identity |
| `divisions` | list[string] | no | Division IDs within this business |
| `settings` | BusinessSettings | no | Business-specific configuration |
| `provenance` | ProvenanceRef | yes | Origin record |
| `metadata` | map[string,string] | no | Extension data |

### 3.2 BusinessSettings

| Field | Type | Required | Description |
|---|---|---|---|
| `default_model_provider` | string | no | Default model provider for this business |
| `max_concurrent_agents` | integer | no | Agent concurrency limit |
| `data_residency` | string | no | Data residency requirement |
| `retention_policy` | string | no | Default retention policy |

### 3.3 Multi-Business Isolation

- `business_id` is the **primary isolation boundary**
- Data, events, memory, and workflows are scoped to a business by default
- Cross-business access requires explicit Governance policy + audit trail
- Business A's agents cannot access Business B's data without authorization
- Switching UI/business sessions MUST NOT stop background execution

---

## 4. Division Schema

### 4.1 Division Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version |
| `entity_type` | string | yes | Always `"division"` |
| `entity_id` | string | yes | Unique division identifier |
| `nexus_id` | string | yes | NEXUS installation identifier |
| `business_id` | string | yes | Parent business (scope enforcement) |
| `name` | string | yes | Division name |
| `status` | enum | yes | One of: `active`, `suspended`, `archived` |
| `created_at` | datetime | yes | When division was created |
| `updated_at` | datetime | no | When division was last modified |
| `owner_identity_id` | string | yes | Division owner identity |
| `parent_business_id` | string | yes | Reference to parent business |
| `agents` | list[string] | no | Agent IDs in this division |
| `settings` | DivisionSettings | no | Division-specific configuration |
| `provenance` | ProvenanceRef | yes | Origin record |
| `metadata` | map[string,string] | no | Extension data |

### 4.2 DivisionSettings

| Field | Type | Required | Description |
|---|---|---|---|
| `allowed_model_providers` | list[string] | no | Restriction on model providers |
| `max_concurrent_agents` | integer | no | Division-specific agent limit |
| `data_classification` | enum | no | Default classification for division data |

### 4.3 Division Rules

- A Division exists within exactly one Business
- Division scope is narrower than Business scope
- Agents scoped to a Division cannot access other Divisions' data
- NEXUS Core may intervene across Divisions when authorized by Governance

---

## 5. Agent Schema

### 5.1 Agent Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version |
| `entity_type` | string | yes | Always `"agent"` |
| `entity_id` | string | yes | Unique agent identifier |
| `nexus_id` | string | yes | NEXUS installation identifier |
| `business_id` | string | yes | Business scope |
| `division_id` | string | no | Division scope (optional) |
| `agent_type` | enum | yes | One of: `permanent`, `temporary` |
| `agent_template` | string | no | Template this agent was instantiated from |
| `display_name` | string | yes | Human-readable agent name |
| `status` | enum | yes | One of: `idle`, `running`, `paused`, `suspended`, `terminated`, `error` |
| `created_at` | datetime | yes | When agent was created |
| `updated_at` | datetime | no | When agent state was last modified |
| `expires_at` | datetime | conditionally | Required for `temporary` agents. TTL boundary. |
| `lifecycle` | AgentLifecycle | yes | Lifecycle state machine (see §5.3) |
| `capabilities` | list[CapabilityRef] | yes | What this agent CAN do (references, not grants) |
| `permissions_ref` | string | no | Reference to permission set (resolved by Governance) |
| `authority_ref` | string | no | Reference to authority chain (resolved by Governance) |
| `trust_ref` | string | no | Reference to trust profile (contextual confidence) |
| `identity_ref` | string | yes | Reference to the Identity record for this agent |
| `model_routing` | ModelRoutingRef | no | Preferred model routing configuration |
| `memory_scope` | MemoryScopeRef | no | Memory access scope configuration |
| `resource_limits` | ResourceLimits | no | Resource consumption limits |
| `parent_id` | string | no | Spawner/parent agent or human |
| `max_concurrent_tasks` | integer | no | Maximum parallel tasks |
| `objective_context` | string | no | Current objective being served |
| `provenance` | ProvenanceRef | yes | Origin record |
| `metadata` | map[string,string] | no | Extension data |

### 5.2 CapabilityRef

| Field | Type | Required | Description |
|---|---|---|---|
| `capability_id` | string | yes | Reference to a defined capability |
| `capability_type` | enum | yes | One of: `tool`, `model`, `domain`, `custom` |
| `scope` | string | no | Specific scope of this capability |

**Critical rule:** Capability ≠ Permission. Having a capability does NOT mean you are authorized to use it. Authorization is resolved by Governance.

### 5.3 Agent Lifecycle

```
CREATED → INITIALIZING → READY → {RUNNING, PAUSED, SUSPENDED}
                                    ↓
                               COMPLETED
                                    ↓
                               TERMINATED
```

| State | Description |
|---|---|
| `CREATED` | Agent record exists, not yet initialized |
| `INITIALIZING` | Agent is loading configuration, connecting to model provider |
| `READY` | Agent is initialized and waiting for tasks |
| `RUNNING` | Agent is actively executing a task |
| `PAUSED` | Agent is temporarily paused (operator action) |
| `SUSPENDED` | Agent is suspended (Governance action) |
| `COMPLETED` | Agent finished its assigned work |
| `TERMINATED` | Agent lifecycle ended (normal or forced) |
| `ERROR` | Agent encountered an unrecoverable error |

### 5.4 Temporary Agent Rules

- `agent_type: temporary` requires `expires_at`
- `expires_at` must be set at creation time
- Upon expiry: agent transitions to `TERMINATED`
- Temporary agent memory may be deleted, archived, or promoted per retention policy
- Temporary agents may have resource limits (max tasks, max tokens, max duration)
- Temporary agents MUST NOT bypass Governance

### 5.5 ResourceLimits

| Field | Type | Required | Description |
|---|---|---|---|
| `max_tokens` | integer | no | Maximum tokens per session |
| `max_tool_calls` | integer | no | Maximum tool calls per task |
| `max_duration_seconds` | integer | no | Maximum runtime duration |
| `max_cost` | float | no | Maximum cost (if cost tracking available) |
| `max_concurrent_tasks` | integer | no | Maximum parallel tasks |

### 5.6 ModelRoutingRef

| Field | Type | Required | Description |
|---|---|---|---|
| `preferred_provider` | string | no | Preferred model provider |
| `preferred_model` | string | no | Preferred model |
| `fallback_providers` | list[string] | no | Fallback provider chain |
| `local_first` | boolean | no | Whether to prefer local models |

### 5.7 MemoryScopeRef

| Field | Type | Required | Description |
|---|---|---|---|
| `scope` | enum | yes | One of: `agent_only`, `division`, `business`, `global` |
| `read_access` | list[string] | no | Explicit read access grants |
| `write_access` | list[string] | no | Explicit write access grants |

---

## 6. Relationships

```
NEXUS Installation (nexus_id)
 ├── Business (business_id)
 │    └── Division (division_id)
 │         └── Agent (agent_id)
 │              ├── Identity (identity_ref)
 │              ├── Capabilities (capabilities)
 │              ├── Permissions (permissions_ref)
 │              ├── Authority (authority_ref)
 │              └── Trust (trust_ref)
 └── Global System (system identity)
```

### 6.1 Ownership Rules

- A Business owns its Divisions
- A Division owns its Agents (scoped)
- An Agent has one Identity
- An Agent has many Capabilities (references)
- Permissions, Authority, and Trust are resolved by Governance, not stored on Agent

---

## 7. Schema Cross-References

| Reference | Target Schema | Required |
|---|---|---|
| `identity_ref` | Identity Record | Yes |
| `permissions_ref` | Governance/Policy Schema | No |
| `authority_ref` | Governance/Policy Schema | No |
| `trust_ref` | Trust/Confidence (see SCHEMA_COMMON.md) | No |
| `parent_id` | Agent Schema (self-reference) | No |
| `owner_identity_id` | Identity Schema | Yes |

---

## 8. Validation Rules

| Rule | Description |
|---|---|
| `business_id` must be valid | Agent/Division must reference existing Business |
| `division_id` must be within `business_id` | Division must belong to the same Business |
| `expires_at` required for temporary agents | Temporary agents must have TTL |
| `expires_at` > `created_at` | TTL must be in the future |
| `identity_ref` must be valid | Agent must reference existing Identity |
| `capabilities` must reference defined types | No arbitrary capability strings |
| Cross-business `parent_id` prohibited | Agents cannot span businesses |

---

## 9. Audit Requirements

| Event | Audit Required |
|---|---|
| Identity created | Yes |
| Identity suspended/revoked | Yes |
| Business onboarded | Yes |
| Division created/deleted | Yes |
| Agent created/terminated | Yes |
| Agent capability changed | Yes |
| Cross-scope access attempted | Yes (always) |

---

*This document defines the structural foundation for who exists in NEXUS. Identity is the root; authority is resolved separately.*
