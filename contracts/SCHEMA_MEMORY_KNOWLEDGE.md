# NEXUS — Memory & Knowledge Schemas

**Layer:** Phase 2 — Data & Event Schemas  
**Status:** LOCKED  
**Branch:** contracts/data-event-schemas  
**Depends on:** SCHEMA_COMMON.md, SCHEMA_IDENTITIES_ORG.md, SCHEMA_WORK_OBJECTIVES.md, Phase 1 Core Interface Contracts  

---

## 1. Purpose

This document defines canonical schemas for **Memory** (what NEXUS remembers) and **Knowledge** (what NEXUS knows). These are distinct but connected systems. Memory stores experiences, observations, and context. Knowledge represents validated, normalized information.

---

## 2. Memory Schema

### 2.1 Design Principle

Memory is not a transcript archive. It is a governed intelligence layer that stores, retrieves, validates, contextualizes, updates, and forgets information according to scope, relevance, objective, time, confidence, privacy, and policy.

### 2.2 Memory Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version |
| `entity_type` | string | yes | Always `"memory"` |
| `entity_id` | string | yes | Unique memory identifier |
| `nexus_id` | string | yes | NEXUS installation identifier |
| `business_id` | string | yes | Business scope |
| `division_id` | string | no | Division scope |
| `agent_id` | string | conditionally | Agent-specific memory (scoped to agent) |
| `workflow_id` | string | conditionally | Workflow-specific memory |
| `task_id` | string | conditionally | Task-specific memory |
| `objective_id` | string | conditionally | Memory related to an objective |
| `memory_type` | enum | yes | One of: `working`, `episodic`, `semantic`, `procedural`, `objective`, `relationship` (see §2.3) |
| `content` | string | yes | Memory content |
| `summary` | string | no | Compressed summary |
| `source` | MemorySource | yes | Where this memory came from (see §2.4) |
| `scope` | enum | yes | One of: `global`, `business`, `division`, `agent`, `workflow`, `task`, `temporary` |
| `confidence` | enum | yes | One of: `factual`, `verified`, `provisional`, `inferred`, `uncertain`, `contradicted` |
| `importance` | enum | yes | One of: `critical`, `high`, `medium`, `low`, `ephemeral` |
| `relevance` | float | no | Relevance score (0.0-1.0), computed at retrieval time |
| `status` | enum | yes | One of: `current`, `stale`, `expired`, `superseded`, `invalid`, `archived`, `deleted` |
| `classification` | enum | yes | One of: `PUBLIC`, `INTERNAL`, `CONFIDENTIAL`, `RESTRICTED` |
| `sensitivity` | string | no | Additional sensitivity label (e.g., `PII`) |
| `valid_from` | datetime | yes | When this memory became valid |
| `valid_until` | datetime | no | When this memory expires |
| `created_at` | datetime | yes | When memory was captured |
| `updated_at` | datetime | no | When memory was last modified |
| `last_accessed_at` | datetime | no | When memory was last retrieved |
| `access_count` | integer | no | Number of times retrieved |
| `version` | integer | yes | Memory version (starts at 1) |
| `parent_version_id` | string | no | Previous version of this memory |
| `consolidated_from` | list[string] | no | Memory IDs this was consolidated from |
| `retention_policy` | RetentionPolicy | yes | How long to keep |
| `provenance` | ProvenanceRef | yes | Origin and history |
| `metadata` | map[string,string] | no | Extension data |

### 2.3 Memory Types

| Type | Description | Typical Lifetime |
|---|---|---|
| `working` | Currently in use by active task | Task duration |
| `episodic` | Events, experiences, occurrences | Medium-term |
| `semantic` | Normalized knowledge, facts, policies | Long-term |
| `procedural` | How to do things (patterns, procedures) | Long-term |
| `objective` | WHY, goals, constraints, progress | Until objective resolved |
| `relationship` | Connections between entities | Long-term |

### 2.4 MemorySource

| Field | Type | Required | Description |
|---|---|---|---|
| `source_type` | enum | yes | One of: `user_input`, `document`, `database`, `tool_result`, `agent_observation`, `workflow_result`, `external_event`, `model_inference`, `human_approval` |
| `source_reference` | string | no | Reference to the specific source |
| `source_agent_id` | string | conditionally | Agent that produced this memory |
| `source_tool_id` | string | conditionally | Tool that produced this memory |
| `source_model_id` | string | conditionally | Model that inferred this memory |
| `verified` | boolean | yes | Whether this memory has been verified |

### 2.5 Memory Scope Rules

| Scope | Accessible By |
|---|---|
| `global` | All businesses (requires Governance policy) |
| `business` | Agents within the same business |
| `division` | Agents within the same division |
| `agent` | Only the owning agent |
| `workflow` | Agents participating in the workflow |
| `task` | Only the assigned agent |
| `temporary` | TTL-bounded, cleaned up after expiry |

### 2.6 Memory Lifecycle

```
CAPTURED → NORMALIZED → CLASSIFIED → VALIDATED → STORED → RETRIEVABLE
                                                              ↓
                                            UPDATED / INVALIDATED
                                                              ↓
                                            ARCHIVED / FORGOTTEN
```

### 2.7 Memory Rules

- All memories are scoped to a business
- Cross-business memory access requires Governance policy + audit trail
- Model-inferred memories must be marked `confidence: inferred`
- Memories are not automatically promoted to permanent knowledge
- Stale/expired memories must be detected and handled
- Contradictory memories must be traceable, not silently deleted
- Temporary memory must have TTL
- Prompt injection in stored content must be treated as data, not instruction

---

## 3. Knowledge Schema

### 3.1 Design Principle

Knowledge is **validated, normalized information** derived from memory, events, or external sources. Knowledge has a different semantic status than raw memory.

### 3.2 Knowledge Record

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | string | yes | Schema version |
| `entity_type` | string | yes | Always `"knowledge"` |
| `entity_id` | string | yes | Unique knowledge identifier |
| `nexus_id` | string | yes | NEXUS installation identifier |
| `business_id` | string | yes | Business scope |
| `division_id` | string | no | Division scope |
| `knowledge_type` | enum | yes | One of: `fact`, `claim`, `opinion`, `prediction`, `observation`, `inference` (see §3.3) |
| `statement` | string | yes | What this knowledge asserts |
| `confidence` | KnowledgeConfidence | yes | Confidence assessment (see §3.4) |
| `evidence` | list[EvidenceRef] | yes | Supporting evidence |
| `source` | KnowledgeSource | yes | Origin of this knowledge (see §3.5) |
| `scope` | enum | yes | Same scope rules as Memory |
| `status` | enum | yes | One of: `active`, `superseded`, `contradicted`, `archived` |
| `superseded_by` | string | no | Knowledge ID that replaced this |
| `contradicted_by` | list[string] | no | Knowledge IDs that contradict this |
| `version` | integer | yes | Knowledge version |
| `valid_from` | datetime | yes | When this knowledge became valid |
| `valid_until` | datetime | no | When this knowledge expires |
| `created_at` | datetime | yes | When knowledge was recorded |
| `updated_at` | datetime | no | When knowledge was last modified |
| `classification` | enum | yes | Data classification |
| `retention_policy` | RetentionPolicy | yes | Retention rules |
| `objective_relevance` | list[string] | no | Objective IDs this knowledge is relevant to |
| `provenance` | ProvenanceRef | yes | Full provenance chain |
| `metadata` | map[string,string] | no | Extension data |

### 3.3 Knowledge Types

| Type | Description | Reliability |
|---|---|---|
| `fact` | Verified, objective truth | High (if verified) |
| `claim` | Unverified assertion | Variable |
| `opinion` | Subjective assessment | Low (as fact) |
| `prediction` | Future expectation | Low (as fact) |
| `observation` | Directly witnessed event | High |
| `inference` | Derived reasoning | Variable |

### 3.4 KnowledgeConfidence

| Field | Type | Required | Description |
|---|---|---|---|
| `level` | enum | yes | One of: `verified`, `probable`, `possible`, `uncertain`, `disputed` |
| `score` | float | no | Numerical confidence (0.0-1.0) |
| `basis` | enum | yes | One of: `direct_observation`, `verified_source`, `inference`, `model_output`, `human_report` |
| `last_assessed` | datetime | yes | When confidence was last evaluated |

> **Deferred reconciliation (audit finding P2):** memory-level `confidence`
> (`factual`, `verified`, `provisional`, `inferred`, `uncertain`, `contradicted`)
> and knowledge-level `KnowledgeConfidence.level` (`verified`, `probable`,
> `possible`, `uncertain`, `disputed`) are two distinct vocabularies. They are
> intentionally NOT unified in this contracts layer; unifying them is deferred to
> a dedicated memory/knowledge phase. Until then, memory confidence describes
> *provenance strength of a stored item* and knowledge confidence describes
> *epistemic certainty of a belief*. Both are advisory and neither grants
> authority. See `contracts/CONTRACTS_CROSS_PHASE_AUDIT.md`.

### 3.5 KnowledgeSource

| Field | Type | Required | Description |
|---|---|---|---|
| `source_type` | enum | yes | One of: `memory_consolidation`, `event_aggregation`, `external_import`, `human_input`, `model_inference`, `knowledge_graph` |
| `source_ids` | list[string] | yes | References to source records |
| `consolidation_method` | string | no | How sources were combined |
| `verified_by` | string | no | Who/what verified this knowledge |

### 3.6 Knowledge Rules

- Knowledge must have at least one evidence source
- Model-inferred knowledge must be distinguishable from observed knowledge
- Contradicted knowledge must not be silently deleted
- Knowledge scope follows same rules as Memory
- Knowledge ≠ truth — it is the best current understanding
- Knowledge promotion from Memory requires validation
- Cross-business knowledge access requires Governance policy

---

## 4. Retention Policy (Shared)

| Field | Type | Required | Description |
|---|---|---|---|
| `retain_until` | datetime | no | Keep until this date |
| `retain_for_days` | integer | no | Or keep for this many days |
| `importance_based` | boolean | no | Whether retention depends on importance |
| `classification_based` | boolean | no | Whether retention depends on classification |
| `archivable` | boolean | yes | Whether this can be archived |
| `deletable` | boolean | yes | Whether this can be deleted |
| `legal_hold` | boolean | no | Whether legal hold prevents deletion |

---

## 5. Memory ↔ Knowledge Boundary

| Aspect | Memory | Knowledge |
|---|---|---|
| **Nature** | Experience, observation, context | Validated, normalized information |
| **Confidence** | Variable, often provisional | Assessed, often higher |
| **Provenance** | Direct source | Consolidated from sources |
| **Lifecycle** | Capture → Store → Retrieve → Forget | Consolidate → Validate → Store → Supersede |
| **Use** | Context for agent/model | Basis for decisions |
| **Trust** | Treated as data | Treated as (qualified) truth |

---

## 6. Relationships

```
Events / Observations
       ↓
   Memory (captured)
       ↓ (consolidation + validation)
   Knowledge (validated)
       ↓ (retrieval)
   Context Engine (assembled)
       ↓
   Agent / Model (consumed)
```

---

## 7. Cross-Schema References

| Reference | Target Schema |
|---|---|
| `memory.objective_id` | Objective Schema (SCHEMA_WORK_OBJECTIVES.md) |
| `memory.workflow_id` | Workflow Schema (SCHEMA_WORK_OBJECTIVES.md) |
| `knowledge.objective_relevance` | Objective Schema (SCHEMA_WORK_OBJECTIVES.md) |
| `memory.source.source_agent_id` | Agent Schema (SCHEMA_IDENTITIES_ORG.md) |
| `memory.source.source_tool_id` | Tool Execution Schema (SCHEMA_EXECUTION.md) |
| `memory.source.source_model_id` | Model Invocation Schema (SCHEMA_EXECUTION.md) |

---

*This document defines the intelligence layer of NEXUS. Memory captures experience; Knowledge represents validated understanding. Both are scoped, governed, and traceable.*
