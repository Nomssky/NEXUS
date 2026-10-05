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

## 10. Persistence of Credentials and Memberships (ADDENDUM)

**Status:** ADDENDUM — additive to the LOCKED sections above. Sections 1–9 are
unchanged. This section records what `storage.data_dir` makes durable beyond
the Identity/Business/Division records the sections above define, together with
the fail-closed hydration rule that durability adds. It creates no new entity,
no new status and no new event, and neither record type is exposed over HTTP.

### 10.1 Record types

| Store record type | Payload | Record id |
|---|---|---|
| `identity` / `business` / `division` | the §2 / §3 / §4 record | entity id |
| `credential` | `{schema_version, entity_type, identity_id, hash, method}` | `credential:<identity id>` |
| `membership` | `{schema_version, entity_type, identity_id, memberships: [...]}` | `membership:<identity id>` |

A `memberships` element is the membership object the code already carries:
`identity_id`, `business_id`, `division_id?`, `role`, `status`. One record per
identity keeps the payload to a single owner, and the two record ids are
namespaced by their type because `Store.Get`/`Delete` are keyed by id alone
across every type — reusing the identity id would shadow the identity record
in the store's index. The shape mirrors the SCHEMA_COMMON §7
`{prefix}:{type}:{unique}` convention.

### 10.2 Rules

| Rule | Detail |
|---|---|
| No raw secrets | The `credential` record stores a verification hash and the `method` that presents it. The raw credential never reaches the store, a log or a response. |
| Canonical values only | A `method` outside `{none, password, token, service, device}` is rejected at the write, not deferred to the next boot. |
| Write-through order | persist → memory. A store failure leaves the in-memory authenticator or membership set untouched; a successful mutation is never memory-only. |
| Fail-closed hydration | A record that cannot be decoded, that disagrees with its record id, that fails validation, or that carries a non-canonical `method` aborts boot (F4) — boot never continues on a partially restored set. |
| Bootstrap source | `NEXUS_BOOTSTRAP_CREDENTIAL` remains the only *source* for `nx:human:bootstrap`'s credential, and every boot with the variable set overwrites the stored hash. Unsetting it later stops refreshing that hash rather than removing it: revocation is the identity record's status transition, which the registry-bound authentication check enforces on every call. |
| Deny by default | `storage.data_dir` unset → all three record types stay in-memory and a restart starts empty, exactly as before. |

---

*This document defines the structural foundation for who exists in NEXUS. Identity is the root; authority is resolved separately.*

---

## 12. Authority, Admission & Scope Addendum (G1–G3, G5)

**Status:** ADDENDUM — additive to the LOCKED sections above. Sections 1–11
are unchanged. This section resolves the audit gaps G1, G2, G3 and the
org-side half of G5.

### 12.1 Identity lifecycle authority (G1)

* The authority boundary for identity/business/division lifecycle
  transitions is **membership of the record's business**: any active
  business-wide member of the record's business (which, per §12.3, is the
  surface where org records are visible at all) may transition a record,
  including a record of their own id. No role hierarchy is invented;
  `Role` labels remain descriptive (§10.1 MEMBERSHIP != AUTHORITY).
* Self-mutation is allowed — an identity may suspend/revoke/activate its
  own record. The resulting lockout is the operator's responsibility.
* A suspended or revoked identity is reactivated by any other authorized
  member through `active` (suspension) per the §5-state matrix; `revoked`
  is terminal and recoverable only by creating a new identity record.
* The bootstrap identity carries **no permanent privilege**. It is a
  normal business-wide member provisioned from `NEXUS_BOOTSTRAP_CREDENTIAL`;
  the env variable only refreshes its credential hash at boot (§10.2).
  Self-lockout of the bootstrap identity is reachable and intentional;
  recovery is a second active identity, or resetting the data directory,
  or the key-gated control surface.
* Authentication enforces identity status on every call (§2.4), so a
  suspended/revoked identity loses all API access immediately and keeps
  no privilege across a restart.

### 12.2 Lifecycle-aware admission (G2)

Status gates **admission**, never existing work:

| Status (business or division) | New submit | Existing execution | Read | Cancel | Governance evaluation |
|---|---|---|---|---|---|
| `active` | allowed | continues | allowed | allowed | evaluated |
| `suspended` / `archived` | rejected `409 CONFLICT` | continues | allowed | allowed | not re-run (nothing new admitted) |

* A request is rejected only when its **business** is not `active`, or —
  when the request carries `division_id` — when that **division** is not
  `active`. Division requests never enter a non-active division.
* Lifecycle status is not an authorization status: a suspended business's
  members keep authenticating, reading and cancelling.
* Suspended/archived organizations' records remain mutable by
  authorized members (suspend/archive/activate transitions and record
  reads are unaffected).

### 12.3 Division membership semantics (G3)

Membership scope is a **strict sub-scope** of business authority:

* A membership with `division_id` empty (business-wide) covers the whole
  business, including every division.
* A membership with `division_id == D` covers only division `D`'s
  resources — submitting `division_id == D`, reading/cancelling D-recorded
  results. It does **not** cover divisionless (business-level) resources:
  business-scope submissions, business-level results, identity/business/
  division records and transitions, approvals, escalations, and the SSE
  event stream all require a business-wide membership.
* Cross-division access is impossible by construction, not by an
  exception list: sibling divisions are invisible (404) and unsubmittable
  (403), per §12.4.
* This is the single rule enforced at the gateway admission path, the
  core chain's authorization stage, cancellation, and every org/approval/
  escalation/SSE surface. There are no endpoint-specific exceptions.

### 12.4 Visibility of foreign-scope records (G5, org half)

404 means "not found or not visible"; 403 is reserved for "you queried a
scope you cannot enter at all" (membership failure on the queried
`business_id`) and for action-level denials on a visible scope. See
`CORE_INTERFACE_CONTRACTS` §11.2.
