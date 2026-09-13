# NEXUS --- Persistence, State & Data Infrastructure

**Status:** PROPOSED\
**Module:** Durable State & Data Plane\
**Depends on:** Identity, Access & Trust; Governance; Event Trigger;
Workflow; Agent Runtime; Tool Runtime; Memory; Attention; Model Router

------------------------------------------------------------------------

## 1. Purpose

Persistence, State & Data Infrastructure memastikan NEXUS dapat:

-   berjalan 24/7,
-   mempertahankan state setelah restart,
-   melanjutkan workflow yang terputus,
-   menyimpan event dan audit secara durable,
-   menyimpan memory dan artifacts,
-   melakukan recovery,
-   menjaga consistency,
-   dan menjalankan banyak business secara bersamaan tanpa data leakage.

NEXUS tidak boleh bergantung pada RAM sebagai sumber kebenaran utama.

------------------------------------------------------------------------

# 2. Core Principle

``` text
RUNTIME STATE ≠ DURABLE STATE
```

Runtime boleh hilang.

Durable state harus dapat dipulihkan.

``` text
PROCESS CRASH
SERVER RESTART
MODEL OUTAGE
WORKER FAILURE
NETWORK FAILURE
```

tidak boleh otomatis berarti:

``` text
NEXUS FORGETS EVERYTHING
```

------------------------------------------------------------------------

# 3. Data Plane Architecture

``` text
                 NEXUS DATA PLANE
                       │
        ┌──────────────┼──────────────┐
        ▼              ▼              ▼
   STATE STORE     EVENT STORE    MEMORY STORE
        │              │              │
        ├──────────────┼──────────────┤
        ▼              ▼              ▼
 WORKFLOW STATE   AUDIT/LOG       ARTIFACT STORE
        │
        ▼
   SNAPSHOT/BACKUP
```

------------------------------------------------------------------------

# 4. Source of Truth

Setiap domain harus memiliki authoritative source.

Contoh:

``` text
Identity
→ Identity Store

Workflow
→ Workflow State Store

Events
→ Event Store

Memory
→ Memory Store

Artifacts
→ Artifact Store

Audit
→ Append-oriented Audit Store
```

Cache tidak boleh dianggap source of truth.

------------------------------------------------------------------------

# 5. Persistence Domains

Minimal:

``` text
identity data
membership
policy metadata
agent definitions
agent runtime state
workflow state
task state
event records
attention state
objective state
memory
artifacts
tool execution records
model execution records
audit records
schedules
budgets
checkpoints
snapshots
```

------------------------------------------------------------------------

# 6. State Categories

### Ephemeral

Boleh hilang:

``` text
temporary cache
in-memory queue buffer
active process memory
temporary UI state
```

### Durable

Harus dipersist:

``` text
workflow state
objective state
agent lifecycle state
event records
approval state
audit
memory
credentials metadata
```

------------------------------------------------------------------------

# 7. Transaction Boundary

Perubahan state kritis harus memiliki transaction boundary yang jelas.

Contoh:

``` text
TASK COMPLETED
→ persist result
→ persist state transition
→ persist audit
→ emit completion event
```

NEXUS harus menghindari state setengah berubah.

------------------------------------------------------------------------

# 8. State Machine Persistence

Setiap state transition penting dicatat.

``` text
previous_state
→ transition
→ new_state
→ actor
→ timestamp
→ reason
→ correlation_id
```

Contoh:

``` text
RUNNING
→ PAUSED
→ owner_request
```

------------------------------------------------------------------------

# 9. Optimistic Concurrency

State version dapat digunakan:

``` yaml
entity_id:
version:
updated_at:
```

Update harus memastikan version masih valid.

Jika terjadi conflict:

``` text
REJECT
→ RELOAD
→ RECONCILE
→ RETRY
```

------------------------------------------------------------------------

# 10. Idempotency

Operation yang dapat diulang harus memiliki idempotency key.

Contoh:

``` text
workflow_start_id
tool_execution_id
payment_request_id
publish_request_id
```

Retry tidak boleh menggandakan side effect.

------------------------------------------------------------------------

# 11. Event Store

Event Store menyimpan event penting:

``` text
event_id
event_type
source
business_id
division_id
timestamp
payload
schema_version
correlation_id
causation_id
```

Event harus immutable setelah tercatat, kecuali mekanisme correction
resmi.

------------------------------------------------------------------------

# 12. Event Replay

NEXUS harus dapat melakukan replay untuk:

``` text
debugging
recovery
rebuilding projections
audit
testing
```

Replay harus aman dan tidak otomatis mengulang external side effects.

------------------------------------------------------------------------

# 13. Event Ordering

Jika ordering penting, event dapat memiliki:

``` text
sequence_number
stream_id
causation_id
```

NEXUS tidak boleh mengasumsikan semua event global memiliki total
ordering.

------------------------------------------------------------------------

# 14. Workflow State Store

Workflow menyimpan minimal:

``` text
workflow_id
workflow_version
status
business_scope
objective_id
current_nodes
completed_nodes
failed_nodes
pending_nodes
checkpoint
deadline
budget_state
created_at
updated_at
```

------------------------------------------------------------------------

# 15. Task State

Task menyimpan:

``` text
task_id
workflow_id
agent_id
status
attempt_count
input_reference
output_reference
checkpoint
lease
started_at
completed_at
```

------------------------------------------------------------------------

# 16. Agent Runtime State

Persist:

``` text
agent_id
runtime_id
lifecycle_state
scope
current_task
health
heartbeat
budget_state
checkpoint
```

Runtime process boleh restart tanpa membuat identity baru secara tidak
sah.

------------------------------------------------------------------------

# 17. Objective State

Karena Objective Engine adalah core requirement, objective state harus
durable.

Minimal:

``` text
objective_id
why
desired_outcome
success_criteria
priority
scope
constraints
status
parent_objective
created_at
updated_at
```

Bagian `why` harus dipertahankan.

------------------------------------------------------------------------

# 18. Objective History

Perubahan objective harus memiliki history:

``` text
old objective
new objective
reason
actor
timestamp
```

NEXUS tidak boleh diam-diam mengganti tujuan utama.

------------------------------------------------------------------------

# 19. Attention State

Persist:

``` text
attention_id
level
status
source
objective_reference
business_scope
owner_visibility
escalation_state
created_at
resolved_at
```

Agar restart tidak membuat critical attention hilang.

------------------------------------------------------------------------

# 20. Memory Persistence

Memory subsystem menyimpan:

``` text
memory record
provenance
scope
confidence
importance
validity
timestamps
relationships
version
retention policy
```

Memory lifecycle tetap mengikuti Memory & Context Intelligence System.

------------------------------------------------------------------------

# 21. Artifact Store

Artifacts dapat berupa:

``` text
documents
images
videos
reports
datasets
exports
generated files
workflow outputs
tool outputs
```

Metadata minimal:

``` text
artifact_id
type
owner
business
division
workflow
task
created_by
created_at
version
checksum
storage_reference
```

------------------------------------------------------------------------

# 22. Artifact Integrity

Artifact penting sebaiknya memiliki:

``` text
checksum
content hash
version
provenance
```

Perubahan menghasilkan version baru, bukan silent overwrite.

------------------------------------------------------------------------

# 23. Storage Isolation

Path/key namespace harus memisahkan:

``` text
global/
business/{business_id}/
division/{division_id}/
agent/{agent_id}/
workflow/{workflow_id}/
```

Cross-business storage access harus melalui authorization.

------------------------------------------------------------------------

# 24. Database Abstraction

NEXUS tidak boleh terlalu mengikat core logic ke satu database engine.

Conceptual interface:

``` text
StateStore
EventStore
MemoryStore
ArtifactStore
AuditStore
```

Implementation dapat diganti tanpa merusak domain logic.

------------------------------------------------------------------------

# 25. Consistency Model

Tentukan consistency berdasarkan domain.

### Stronger consistency

Untuk:

``` text
identity
authorization metadata
financial state
approval
critical workflow transitions
```

### Eventual consistency

Dapat digunakan untuk:

``` text
analytics
search indexes
derived dashboards
non-critical projections
```

------------------------------------------------------------------------

# 26. Queue Persistence

Queue penting harus durable.

Jika worker mati:

``` text
queued task
→ remains recoverable
```

Task tidak hilang hanya karena worker crash.

------------------------------------------------------------------------

# 27. Lease & Recovery

Task execution dapat menggunakan lease:

``` text
LEASED
→ HEARTBEAT
→ COMPLETED
```

Jika heartbeat timeout:

``` text
LEASE EXPIRED
→ RECOVERY
→ REQUEUE / RECONCILE
```

------------------------------------------------------------------------

# 28. Checkpoints

Long-running workflow harus membuat checkpoint.

Contoh:

``` text
step 1 completed
step 2 completed
step 3 running
```

Jika process mati:

``` text
resume from safe checkpoint
```

bukan mengulang semuanya secara membabi buta.

------------------------------------------------------------------------

# 29. Unknown State

Jika external action status tidak diketahui:

``` text
UNKNOWN
```

NEXUS tidak boleh langsung menganggap:

``` text
FAILED
```

atau:

``` text
SUCCESS
```

Harus dilakukan reconciliation.

------------------------------------------------------------------------

# 30. Recovery Strategy

General flow:

``` text
DETECT FAILURE
→ LOAD LAST DURABLE STATE
→ VALIDATE INTEGRITY
→ IDENTIFY UNKNOWN OPERATIONS
→ RECONCILE
→ RESTORE
→ RESUME / ROLLBACK / COMPENSATE
→ AUDIT
```

------------------------------------------------------------------------

# 31. Snapshot

Snapshot dapat digunakan untuk:

``` text
fast recovery
large state restoration
migration
disaster recovery
testing
```

Snapshot harus memiliki:

``` text
snapshot_id
timestamp
schema_version
state_version
checksum
```

------------------------------------------------------------------------

# 32. Backup

Backup strategy minimal:

``` text
scheduled backup
incremental backup
full backup
retention policy
restore testing
```

Backup tidak dianggap valid hanya karena file backup berhasil dibuat.

Restore harus diuji.

------------------------------------------------------------------------

# 33. Disaster Recovery

NEXUS harus memiliki target konseptual:

``` text
RPO = acceptable data loss window
RTO = acceptable recovery time
```

Nilai final ditentukan berdasarkan deployment.

Critical state mendapat prioritas recovery tertinggi.

------------------------------------------------------------------------

# 34. Schema Versioning

Data harus memiliki schema version:

``` text
schema_version
```

Migration:

``` text
old schema
→ migration
→ new schema
```

Jangan mengandalkan perubahan schema manual tanpa migration strategy.

------------------------------------------------------------------------

# 35. Backward Compatibility

Selama migration, NEXUS harus menangani:

``` text
old event
old workflow state
old artifact metadata
old memory record
```

sesuai compatibility policy.

------------------------------------------------------------------------

# 36. Retention

Setiap data class memiliki:

``` text
retention period
archive policy
deletion policy
legal/policy constraint
```

Tidak semua data harus disimpan selamanya.

------------------------------------------------------------------------

# 37. Data Deletion

Deletion harus membedakan:

``` text
logical deletion
physical deletion
anonymization
expiration
archival
```

Deletion harus tetap mematuhi audit dan governance requirements.

------------------------------------------------------------------------

# 38. Encryption

Sensitive data sebaiknya menggunakan:

``` text
encryption at rest
encryption in transit
key separation
credential isolation
```

Encryption key tidak boleh disimpan bersama plaintext secret yang
dilindunginya.

------------------------------------------------------------------------

# 39. Secret Metadata

Persistence layer boleh menyimpan:

``` text
credential_id
provider
scope
status
created_at
expires_at
rotation_state
```

Tetapi secret plaintext harus berada di secure secret mechanism.

------------------------------------------------------------------------

# 40. Audit Persistence

Audit record minimal:

``` text
audit_id
timestamp
actor_identity
on_behalf_of
action
resource
business
division
workflow
task
decision
result
policy_version
correlation_id
```

Audit harus durable dan tamper-resistant sesuai deployment.

------------------------------------------------------------------------

# 41. Observability Data

Persist atau export metrics/logs yang diperlukan untuk:

``` text
workflow recovery
security investigation
performance analysis
cost tracking
model/provider health
tool reliability
```

Observability tidak boleh membocorkan secrets.

------------------------------------------------------------------------

# 42. Data Classification

Data dapat diklasifikasikan:

``` text
PUBLIC
INTERNAL
CONFIDENTIAL
SENSITIVE
RESTRICTED
```

Classification mempengaruhi:

``` text
storage
retrieval
model routing
tool access
logging
retention
export
```

------------------------------------------------------------------------

# 43. Multi-Business Data Isolation

Hard requirement:

``` text
Business A data
≠
Business B data
```

Isolation berlaku pada:

``` text
database query
cache
event
memory
artifact
workflow
agent
logs
search index
backup restore
```

------------------------------------------------------------------------

# 44. Cross-Business Operations

Cross-business operation hanya boleh terjadi jika:

``` text
explicit scope
authorized identity
governance approval/policy
auditable reason
```

Tidak boleh terjadi hanya karena model menemukan data business lain.

------------------------------------------------------------------------

# 45. Cache Safety

Cache harus memiliki:

``` text
scope key
tenant/business key
TTL
invalidation
authorization awareness
```

Cache global untuk data business-sensitive harus dilarang kecuali desain
benar-benar menjamin isolation.

------------------------------------------------------------------------

# 46. Search Indexes

Search/embedding indexes adalah derived data.

Jika index rusak:

``` text
rebuild from source of truth
```

Index tidak boleh menjadi satu-satunya copy memory atau critical state.

------------------------------------------------------------------------

# 47. Atomicity

Untuk critical transition:

``` text
state change
+
audit
+
required event
```

harus memiliki transactional/outbox strategy yang mencegah inconsistent
publication.

------------------------------------------------------------------------

# 48. Outbox Pattern

Untuk reliable event publication:

``` text
TRANSACTION
 ├─ update state
 └─ write outbox event
        ↓
   EVENT PUBLISHER
        ↓
     EVENT BUS
```

Jika publisher mati, event dapat dikirim ulang.

------------------------------------------------------------------------

# 49. Inbox / Idempotent Consumer

Consumer menyimpan processed event identifiers.

``` text
event_id
→ already processed?
```

Jika ya:

``` text
IGNORE DUPLICATE
```

------------------------------------------------------------------------

# 50. Garbage Collection

Temporary resources harus memiliki cleanup:

``` text
temporary agent state
temporary artifacts
expired checkpoints
expired sessions
expired tokens metadata
stale queues
temporary memory
```

Cleanup tidak boleh menghapus active resources.

------------------------------------------------------------------------

# 51. Storage Quotas

Resource budget dapat diterapkan untuk:

``` text
business
division
agent
workflow
artifact storage
memory storage
logs
```

Jika quota tercapai:

``` text
STOP / DEFER / COMPRESS / ARCHIVE / ESCALATE
```

sesuai policy.

------------------------------------------------------------------------

# 52. Data Integrity Checks

Periodic verification:

``` text
checksum
foreign references
schema validity
state consistency
orphan detection
duplicate detection
```

Integrity failure harus menghasilkan Attention jika berdampak
signifikan.

------------------------------------------------------------------------

# 53. Persistence ↔ Governance

Governance menentukan:

``` text
retention
deletion
export
access
classification
backup restrictions
cross-business rules
```

Persistence hanya mengeksekusi storage policy.

------------------------------------------------------------------------

# 54. Persistence ↔ Identity

Setiap sensitive data access harus dapat dikaitkan dengan:

``` text
identity
session
business
division
purpose
```

------------------------------------------------------------------------

# 55. Persistence ↔ Workflow

Workflow state harus durable agar:

``` text
restart ≠ workflow loss
```

Workflow engine dapat resume berdasarkan durable checkpoint.

------------------------------------------------------------------------

# 56. Persistence ↔ Agent Runtime

Agent runtime state dapat dipulihkan tanpa menghidupkan agent dengan
authority yang lebih besar dari sebelumnya.

------------------------------------------------------------------------

# 57. Persistence ↔ Memory

Memory store adalah source of truth untuk durable memory.

Embedding/index/cache hanyalah derived representation.

------------------------------------------------------------------------

# 58. Persistence ↔ Event System

Event Store menyediakan:

``` text
durability
replay
correlation
recovery
audit trail
```

Event pipeline tetap harus menjaga idempotency.

------------------------------------------------------------------------

# 59. Persistence ↔ Attention

Critical Attention state harus survive restart.

Contoh:

``` text
CRITICAL SECURITY EVENT
→ NEXUS restart
→ event recovered
→ attention restored
→ escalation continues
```

------------------------------------------------------------------------

# 60. Persistence ↔ Model Router

Model execution records dapat menyimpan:

``` text
model
provider
request reference
latency
token usage
cost
result status
fallback
```

Sensitive prompt content mengikuti data classification policy.

------------------------------------------------------------------------

# 61. Persistence ↔ Tool Runtime

Tool execution records:

``` text
tool_execution_id
actor
tool
action
scope
request hash
status
result reference
idempotency key
timestamp
```

External side effects harus dapat direkonsiliasi.

------------------------------------------------------------------------

# 62. Recovery Priority

Prioritas recovery:

``` text
CRITICAL SECURITY
→ IDENTITY
→ GOVERNANCE
→ OBJECTIVES
→ WORKFLOW
→ AGENTS
→ EVENTS
→ ATTENTION
→ MEMORY
→ ARTIFACTS
→ ANALYTICS
```

Urutan final dapat disesuaikan deployment.

------------------------------------------------------------------------

# 63. Testing Requirements

Wajib diuji:

-   process crash
-   database restart
-   worker crash
-   queue recovery
-   workflow resume
-   duplicate event
-   duplicate tool execution
-   unknown external state
-   transaction rollback
-   outbox recovery
-   schema migration
-   backup restore
-   snapshot restore
-   corruption detection
-   cache leakage
-   cross-business leakage
-   authorization after restore
-   secret leakage
-   retention enforcement
-   deletion behavior
-   quota enforcement
-   concurrent state update
-   event replay safety.

------------------------------------------------------------------------

# 64. Acceptance Criteria

-   [ ] critical state durable
-   [ ] event store tersedia
-   [ ] workflow state durable
-   [ ] task state durable
-   [ ] agent runtime state recoverable
-   [ ] objective state durable
-   [ ] attention state durable
-   [ ] memory persistence tersedia
-   [ ] artifact storage tersedia
-   [ ] idempotency tersedia
-   [ ] checkpoint tersedia
-   [ ] recovery tersedia
-   [ ] unknown-state reconciliation tersedia
-   [ ] snapshot tersedia
-   [ ] backup tersedia
-   [ ] restore test tersedia
-   [ ] schema versioning tersedia
-   [ ] retention tersedia
-   [ ] encryption strategy tersedia
-   [ ] audit persistence tersedia
-   [ ] multi-business isolation tersedia
-   [ ] cache isolation tersedia
-   [ ] outbox/inbox reliability tersedia
-   [ ] data integrity checks tersedia.

------------------------------------------------------------------------

# 65. Locked Design Principle

> **NEXUS must treat durable state as a first-class system capability.
> Processes may crash, workers may disappear, providers may fail, and
> machines may restart, but critical identity, governance, objective,
> workflow, agent, event, attention, memory, artifact, and audit state
> must remain recoverable. Persistence must preserve consistency, scope,
> provenance, idempotency, and multi-business isolation while enabling
> NEXUS to resume autonomous work safely rather than starting from
> zero.**

------------------------------------------------------------------------

# 66. Next Module

Setelah module ini di-lock:

**NEXUS_SCHEDULING_RESOURCE_RUNTIME.md**

Fokus:

``` text
24/7 Scheduler
Job Queue
Worker Pool
Concurrency
Prioritization
Resource Allocation
CPU/GPU
Model Capacity
Tool Capacity
Business Fairness
Agent Scheduling
Backpressure
Rate Limits
Deadlines
```
