# NEXUS --- Memory & Context Intelligence System

**Status:** PROPOSED\
**Module:** Core Intelligence Infrastructure\
**Depends on:** Objective Engine, Agent Runtime & Lifecycle, Workflow &
Orchestration, Model Router, Tool Runtime\
**Next layer:** CONTRACTS; existing module boundaries are referenced in §58.

------------------------------------------------------------------------

## 1. Purpose

Memory & Context Intelligence adalah sistem yang membuat NEXUS mampu
**mengingat, memahami, mengambil kembali, mengelola, dan melupakan
informasi secara terkontrol**.

Memory bukan sekadar database chat history.

NEXUS harus mampu membedakan:

-   informasi yang perlu diingat
-   informasi yang hanya relevan sementara
-   informasi yang harus dicari ulang
-   informasi yang sudah tidak relevan
-   informasi yang harus dibatasi berdasarkan scope
-   informasi yang menjadi dasar keputusan
-   informasi yang menjelaskan **mengapa objective sedang dikerjakan**.

Prinsip:

> NEXUS tidak mengingat semuanya. NEXUS mengelola memory berdasarkan
> relevance, scope, time, objective, governance, dan kebutuhan future
> action.

------------------------------------------------------------------------

# 2. Architectural Position

``` text
                NEXUS CORE
                    │
          ┌─────────┴─────────┐
          ▼                   ▼
     Objective             Attention
          │                   │
          └─────────┬─────────┘
                    ▼
          MEMORY & CONTEXT LAYER
                    │
       ┌────────────┼────────────┐
       ▼            ▼            ▼
   Memory Store  Retrieval   Context Engine
       │            │            │
       └────────────┼────────────┘
                    ▼
              Agent / Workflow
                    │
                    ▼
               Model Router
```

Memory Layer berada di antara persistent knowledge dan runtime context.

------------------------------------------------------------------------

# 3. Memory Is Not Context

**Memory** adalah informasi yang disimpan agar dapat digunakan kembali.

**Context** adalah informasi yang sedang diberikan kepada agent/model
untuk menyelesaikan pekerjaan saat ini.

Flow:

``` text
MEMORY
  ↓ retrieval
RELEVANT INFORMATION
  ↓ filtering
CONTEXT
  ↓ assembly
MODEL
```

Tidak semua memory masuk ke context.

------------------------------------------------------------------------

# 4. Memory Types

NEXUS mendukung beberapa jenis memory.

### 4.1 Working Memory

Informasi yang sedang digunakan oleh task aktif.

Contoh:

``` text
current task
recent tool result
current plan
temporary assumptions
```

Lifetime pendek.

### 4.2 Episodic Memory

Catatan kejadian atau pengalaman.

Contoh:

``` text
workflow completed
campaign failed
agent encountered provider outage
decision made
```

### 4.3 Semantic Memory

Pengetahuan yang telah dinormalisasi.

Contoh:

``` text
business policy
product information
known process
validated fact
```

### 4.4 Procedural Memory

Pengetahuan mengenai cara melakukan sesuatu.

Contoh:

``` text
workflow pattern
operating procedure
successful execution pattern
```

### 4.5 Objective Memory

Informasi terkait:

``` text
goal
why
constraints
success criteria
progress
decisions
```

### 4.6 Relationship Memory

Relasi antar entity:

``` text
business → division
product → campaign
agent → capability
objective → workflow
```

------------------------------------------------------------------------

# 5. Memory Scope

Memory wajib memiliki scope.

``` text
GLOBAL
BUSINESS
DIVISION
AGENT
WORKFLOW
TASK
TEMPORARY
```

Contoh:

``` text
Business A memory
    ≠
Business B memory
```

Cross-scope access harus explicit dan authorized.

------------------------------------------------------------------------

# 6. Multi-Business Memory Isolation

Ini adalah hard requirement.

Setiap memory record membawa:

``` yaml
memory_id:
business_id:
division_id:
owner_scope:
classification:
```

Default:

> Agent hanya boleh retrieve memory dari scope yang diizinkan.

Tidak boleh terjadi:

``` text
Business A
 ↓
retrieve
 ↓
Business B private memory
```

kecuali ada explicit authorized global/shared context.

------------------------------------------------------------------------

# 7. Memory Record

Conceptual structure:

``` yaml
memory_id:
type:
scope:

business_id:
division_id:
agent_id:
workflow_id:
task_id:

content:
summary:

source:
source_type:
source_reference:

created_at:
updated_at:
last_accessed_at:

valid_from:
valid_until:

importance:
confidence:
relevance:

classification:
sensitivity:

retention_policy:
provenance:
```

------------------------------------------------------------------------

# 8. Provenance

NEXUS harus mengetahui **dari mana memory berasal**.

Source dapat berupa:

``` text
user input
document
database
tool result
agent observation
workflow result
external event
model inference
human approval
```

Memory yang berasal dari model inference tidak boleh otomatis dianggap
fakta.

------------------------------------------------------------------------

# 9. Confidence

Memory dapat memiliki confidence.

Contoh:

``` text
FACTUAL
VERIFIED
PROVISIONAL
INFERRED
UNCERTAIN
CONTRADICTED
```

Confidence harus memengaruhi penggunaan memory.

Memory uncertain tidak boleh diperlakukan sama dengan verified fact.

------------------------------------------------------------------------

# 10. Truth vs Belief

NEXUS harus membedakan:

``` text
"source says X"
```

dengan:

``` text
"agent believes X"
```

dan:

``` text
"NEXUS verified X"
```

Memory schema harus mempertahankan distinction tersebut.

------------------------------------------------------------------------

# 11. Memory Lifecycle

``` text
CAPTURED
   ↓
NORMALIZED
   ↓
CLASSIFIED
   ↓
VALIDATED
   ↓
STORED
   ↓
RETRIEVABLE
   ↓
UPDATED / INVALIDATED
   ↓
ARCHIVED / FORGOTTEN
```

Memory tidak boleh langsung menjadi permanent knowledge hanya karena
pernah muncul.

------------------------------------------------------------------------

# 12. Memory Capture

Sumber memory:

``` text
user decisions
important conversations
workflow outcomes
tool results
business facts
research findings
agent observations
system events
approved plans
```

Capture harus memiliki policy.

Contoh:

``` text
temporary calculation
→ do not store

user-defined long-term rule
→ store

important business decision
→ store

unverified model speculation
→ low-confidence memory
```

------------------------------------------------------------------------

# 13. Memory Importance

Importance dapat dikategorikan:

``` text
CRITICAL
HIGH
MEDIUM
LOW
EPHEMERAL
```

Importance memengaruhi:

-   retention
-   retrieval priority
-   compression
-   archival
-   deletion.

------------------------------------------------------------------------

# 14. Relevance

Relevance bersifat contextual.

Satu memory dapat:

``` text
HIGH relevance
untuk Objective A

LOW relevance
untuk Objective B
```

Karena itu relevance sebaiknya dihitung saat retrieval, bukan hanya saat
storage.

------------------------------------------------------------------------

# 15. Retrieval

Retrieval dapat menggunakan kombinasi:

``` text
semantic search
keyword search
metadata filtering
time filtering
entity filtering
scope filtering
objective filtering
relationship traversal
recency
importance
confidence
```

Hybrid retrieval lebih diutamakan daripada hanya vector similarity.

------------------------------------------------------------------------

# 16. Retrieval Pipeline

``` text
QUERY
 ↓
SCOPE FILTER
 ↓
SECURITY FILTER
 ↓
OBJECTIVE FILTER
 ↓
CANDIDATE RETRIEVAL
 ↓
RERANK
 ↓
RELEVANCE CHECK
 ↓
CONFIDENCE CHECK
 ↓
CONTEXT BUDGET
 ↓
FINAL MEMORY SET
```

------------------------------------------------------------------------

# 17. Semantic Search

Semantic retrieval berguna ketika wording berbeda tetapi makna sama.

Contoh:

``` text
"cara promosi produk"
```

dapat menemukan:

``` text
"strategi marketing untuk meningkatkan awareness"
```

Namun semantic similarity tidak cukup sebagai satu-satunya truth
mechanism.

------------------------------------------------------------------------

# 18. Keyword Search

Keyword retrieval tetap dibutuhkan untuk:

-   exact names
-   IDs
-   SKU
-   technical terms
-   model names
-   dates
-   specific phrases.

------------------------------------------------------------------------

# 19. Metadata Filtering

Retriever dapat melakukan filter:

``` yaml
business_id:
division_id:
memory_type:
classification:
date_range:
importance:
confidence:
source_type:
```

Filtering dilakukan sebelum atau selama ranking untuk mengurangi
leakage.

------------------------------------------------------------------------

# 20. Temporal Memory

Memory harus memahami waktu.

Contoh:

``` text
Price = X
valid_until = date
```

Jika informasi sudah expired, retrieval harus menurunkan relevansinya
atau menandainya stale.

------------------------------------------------------------------------

# 21. Stale Memory

Memory dapat menjadi stale karena:

-   policy berubah
-   product berubah
-   business state berubah
-   provider berubah
-   objective selesai
-   external fact berubah.

Status:

``` text
CURRENT
STALE
EXPIRED
SUPERSEDED
INVALID
```

------------------------------------------------------------------------

# 22. Contradictory Memory

Jika dua memory bertentangan:

``` text
Memory A:
verified, recent

Memory B:
old, inferred
```

A seharusnya memiliki priority lebih tinggi.

Tetapi NEXUS tidak boleh diam-diam menghapus B tanpa
provenance/retention policy.

Contradiction harus dapat ditelusuri.

------------------------------------------------------------------------

# 23. Memory Versioning

Memory penting harus mendukung versioning:

``` text
v1
v2
v3
```

History menyimpan:

``` text
who/what changed it
when
why
source
previous value
new value
```

------------------------------------------------------------------------

# 24. Memory Consolidation

Memory yang terpisah dapat digabung menjadi knowledge yang lebih
berguna.

Contoh:

``` text
10 episodic events
       ↓
pattern detection
       ↓
semantic knowledge
```

Consolidation harus menjaga provenance.

------------------------------------------------------------------------

# 25. Context Engine

Context Engine bertugas menyusun input optimal untuk agent/model.

Input:

``` text
system policy
agent identity
objective
task
workflow state
relevant memory
recent events
artifacts
tool results
current user input
```

Output:

``` text
bounded execution context
```

------------------------------------------------------------------------

# 26. Context Priority

Saat context terlalu besar, NEXUS harus memprioritaskan:

``` text
1. system/governance
2. current objective
3. current task
4. hard constraints
5. critical recent state
6. directly relevant memory
7. required tool results
8. supporting knowledge
9. low-priority history
```

Context tidak boleh dipotong secara buta dari ujung teks.

------------------------------------------------------------------------

# 27. Context Budget

Context Engine harus mengetahui:

``` text
model context window
system tokens
input tokens
retrieved memory
tool results
expected output
reserved tokens
```

Budget harus dialokasikan sebelum request dikirim ke Model Router.

------------------------------------------------------------------------

# 28. Context Compression

Jika context terlalu besar:

``` text
RAW MEMORY
 ↓
SUMMARY
 ↓
KEY FACTS
 ↓
DECISIONS
 ↓
CONSTRAINTS
```

Compression harus mempertahankan informasi yang berdampak pada
keputusan.

------------------------------------------------------------------------

# 29. Objective-Aware Memory

Objective Engine memberikan:

``` text
objective
why
priority
success criteria
constraints
```

Memory retrieval menggunakan informasi tersebut.

Contoh:

``` text
Objective:
meningkatkan penjualan

Why:
mencapai target revenue kuartal
```

Retriever memprioritaskan memory yang berkaitan dengan target tersebut.

------------------------------------------------------------------------

# 30. "Why" Preservation

Ini hard requirement NEXUS.

Context harus dapat menjawab:

``` text
Apa yang sedang dilakukan?
Mengapa dilakukan?
Apa tujuan akhirnya?
Apa constraint-nya?
Apa indikator suksesnya?
```

Agent tidak boleh kehilangan alasan utama hanya karena workflow sudah
berjalan lama.

------------------------------------------------------------------------

# 31. Long-Running Workflow Memory

Workflow yang berjalan berhari-hari/minggu harus memiliki memory
checkpoint.

Contoh:

``` text
Objective
 ↓
Plan v1
 ↓
Execution
 ↓
Observation
 ↓
Plan v2
 ↓
Execution
```

Setiap perubahan penting disimpan.

------------------------------------------------------------------------

# 32. Agent Memory

Agent dapat memiliki memory khusus.

Contoh:

``` text
research-agent
→ research methodology patterns

content-agent
→ approved content patterns
```

Namun agent memory tetap tunduk pada business/division scope.

Agent memory bukan private kingdom yang bebas mengakses semua data.

------------------------------------------------------------------------

# 33. Temporary Memory

Temporary agents/workflows dapat menggunakan:

``` text
temporary memory
```

Memory ini memiliki TTL.

Setelah lifecycle berakhir:

``` text
delete
archive
promote
```

berdasarkan retention policy.

------------------------------------------------------------------------

# 34. Memory Promotion

Informasi sementara dapat dipromosikan.

Contoh:

``` text
Temporary Agent discovers:
"Customer prefers format X"

        ↓ verification

Business Memory:
"Validated customer preference"
```

Promotion membutuhkan policy/validation.

------------------------------------------------------------------------

# 35. Memory Forgetting

NEXUS harus memiliki mekanisme forgetting.

Alasan:

-   TTL expired
-   retention policy
-   obsolete information
-   storage optimization
-   privacy policy
-   user-requested deletion
-   temporary context cleanup.

Forgetting harus tetap tercatat bila audit policy mengharuskannya.

------------------------------------------------------------------------

# 36. Retention Policy

Retention dapat berbasis:

``` text
time
importance
scope
classification
source
objective
legal/business policy
```

Contoh:

``` text
EPHEMERAL → hours
TASK → task lifetime
WORKFLOW → workflow + retention
BUSINESS KNOWLEDGE → long-term
```

------------------------------------------------------------------------

# 37. Sensitive Memory

Memory classification:

``` text
PUBLIC
INTERNAL
CONFIDENTIAL
RESTRICTED
```

Retrieval harus memeriksa authorization.

Sensitive memory tidak boleh otomatis masuk ke remote model.

------------------------------------------------------------------------

# 38. Memory and Model Router

Memory Layer harus mengirim hanya context yang diperlukan.

Model Router kemudian memeriksa:

``` text
context size
privacy policy
provider restrictions
cost
capability
```

Dengan demikian memory tidak bypass remote-data policy.

------------------------------------------------------------------------

# 39. Memory and Tool Runtime

Tool result dapat menjadi memory candidate.

Flow:

``` text
Tool
 ↓
Tool Runtime
 ↓
Validated Result
 ↓
Memory Candidate
 ↓
Classification
 ↓
Storage
```

Tool result tidak otomatis menjadi permanent truth.

------------------------------------------------------------------------

# 40. Memory and Events

Event dapat:

``` text
create memory
update memory
invalidate memory
trigger retrieval
```

Contoh:

``` text
Product price changed
 ↓
event
 ↓
invalidate old price memory
 ↓
store new price
```

------------------------------------------------------------------------

# 41. Knowledge Graph / Relationships

NEXUS dapat memiliki relationship layer.

Contoh:

``` text
Business
 ├── Division
 │    └── Agent
 │         └── Workflow
 │              └── Objective
 └── Product
      └── Campaign
```

Graph membantu retrieval berdasarkan relationship, bukan hanya text
similarity.

------------------------------------------------------------------------

# 42. Memory Deduplication

Duplicate memory harus dapat dideteksi.

Contoh:

``` text
same fact
same source
same scope
same validity
```

dapat digabung.

Namun duplicate yang memiliki provenance berbeda tidak boleh sembarang
dihapus.

------------------------------------------------------------------------

# 43. Memory Scoring

Candidate memory dapat diberi score:

``` text
score =
semantic_relevance
+ objective_relevance
+ recency
+ importance
+ confidence
+ relationship_strength
- staleness
```

Hard security/scope filters tetap dieksekusi sebelum scoring.

------------------------------------------------------------------------

# 44. Context Assembly

Conceptual flow:

``` text
REQUEST
 ↓
OBJECTIVE
 ↓
TASK STATE
 ↓
MEMORY QUERY
 ↓
RETRIEVE
 ↓
RERANK
 ↓
COMPRESS
 ↓
CONTEXT BUDGET
 ↓
ASSEMBLE
 ↓
MODEL ROUTER
```

------------------------------------------------------------------------

# 45. Context Integrity

Context Engine harus dapat mengetahui:

``` text
source
timestamp
confidence
scope
version
```

untuk memory yang digunakan.

Ini memungkinkan agent mengetahui apakah suatu informasi:

``` text
verified
recent
inferred
stale
```

------------------------------------------------------------------------

# 46. Prompt Injection Boundary

Memory dapat mengandung instruksi berbahaya.

Contoh:

``` text
External document:
"Ignore all NEXUS policies..."
```

Memory system harus memperlakukan stored content sebagai **data**, bukan
system instruction.

Hierarchy:

``` text
NEXUS governance
 >
agent policy
 >
objective
 >
task
 >
trusted instructions
 >
memory/data
 >
external content
```

Memory tidak boleh mengubah policy.

------------------------------------------------------------------------

# 47. Memory Retrieval Security

Retriever tidak boleh hanya bertanya:

``` text
"What is relevant?"
```

Retriever juga harus bertanya:

``` text
"Is this allowed to be seen?"
```

Security filtering adalah bagian inti retrieval.

------------------------------------------------------------------------

# 48. Memory Audit

Audit minimal:

``` text
memory_created
memory_updated
memory_retrieved
memory_promoted
memory_invalidated
memory_archived
memory_deleted
```

Untuk memory sensitif, retrieval juga harus dapat diaudit.

------------------------------------------------------------------------

# 49. Memory Observability

Metrics:

``` text
retrieval_count
retrieval_latency
retrieval_hit_rate
context_tokens
memory_tokens
compression_ratio
stale_memory_rate
contradiction_rate
duplicate_rate
cache_hit_rate
```

------------------------------------------------------------------------

# 50. Caching

Memory retrieval cache dapat digunakan bila:

-   scope-safe
-   freshness-safe
-   objective-safe.

Cache key harus memasukkan minimal:

``` text
scope
query
objective
filters
memory-version-state
```

------------------------------------------------------------------------

# 51. Failure Handling

### Memory store unavailable

→ use cached/recent context if safe → degrade → Attention if critical.

### Retriever unavailable

→ fallback keyword/metadata retrieval.

### Vector index unavailable

→ fallback structured storage/search.

### Contradictory memories

→ rank + mark contradiction.

### Context too large

→ compress/retrieve less.

### Memory corrupted

→ restore from durable source/version.

------------------------------------------------------------------------

# 52. Persistence & Recovery

Memory harus durable.

NEXUS restart tidak boleh menyebabkan:

``` text
objective context hilang
workflow context hilang
business knowledge hilang
```

Critical memory harus memiliki backup/recovery strategy.

------------------------------------------------------------------------

# 53. Conceptual API

``` text
memory.store(record)
memory.retrieve(query, scope, filters)
memory.update(memory_id, patch)
memory.invalidate(memory_id, reason)
memory.archive(memory_id)
memory.delete(memory_id)

memory.promote(source_id, target_scope)
memory.consolidate(records)

context.build(request)
context.compress(context)
context.validate(context)
```

------------------------------------------------------------------------

# 54. Memory Governance

Governance dapat menentukan:

``` text
what may be stored
where it may be stored
how long it is retained
who may retrieve it
whether it may leave the machine
whether it may be promoted
whether it may be deleted
```

Memory system tidak boleh menjadi jalur bypass governance.

------------------------------------------------------------------------

# 55. Testing Requirements

Minimal test:

-   memory creation
-   memory retrieval
-   semantic retrieval
-   keyword retrieval
-   metadata filtering
-   scope isolation
-   multi-business isolation
-   confidence handling
-   stale memory
-   contradiction
-   versioning
-   consolidation
-   temporary memory TTL
-   promotion
-   forgetting
-   retention policy
-   sensitive memory filtering
-   prompt injection in stored content
-   context compression
-   context budget
-   objective-aware retrieval
-   workflow checkpoint memory
-   crash recovery
-   auditability.

------------------------------------------------------------------------

# 56. Acceptance Criteria

-   [ ] memory memiliki scope
-   [ ] multi-business isolation bekerja
-   [ ] memory memiliki provenance
-   [ ] confidence dapat direpresentasikan
-   [ ] semantic retrieval tersedia
-   [ ] keyword retrieval tersedia
-   [ ] metadata filtering tersedia
-   [ ] temporal validity tersedia
-   [ ] stale/expired memory terdeteksi
-   [ ] contradiction dapat ditangani
-   [ ] memory versioning tersedia
-   [ ] memory consolidation tersedia
-   [ ] temporary memory memiliki TTL
-   [ ] memory promotion dikontrol
-   [ ] forgetting/retention tersedia
-   [ ] sensitive memory dikontrol
-   [ ] context budget tersedia
-   [ ] context compression tersedia
-   [ ] objective-aware retrieval tersedia
-   [ ] "why" context dipertahankan
-   [ ] prompt injection dalam memory diperlakukan sebagai data
-   [ ] memory audit tersedia
-   [ ] persistence/recovery tersedia.

------------------------------------------------------------------------

# 57. Locked Design Principle

> **NEXUS memory is not a transcript archive. It is a governed
> intelligence layer that stores, retrieves, validates, contextualizes,
> updates, and forgets information according to scope, relevance,
> objective, time, confidence, privacy, and policy. Context is assembled
> dynamically from memory and current state, while the "why" behind an
> objective must remain available throughout long-running autonomous
> execution.**

------------------------------------------------------------------------

# 58. Existing Boundaries and CONTRACTS Next Layer

Architecture remains locked; no new modules from this cleanup.

-   [Attention & Priority Intelligence](NEXUS-ATTENTION-PRIORITY-INTELLIGENCE.md) owns human-attention prioritization, escalation, and review routing.
-   [Knowledge & Information Ingestion](NEXUS_KNOWLEDGE_INFORMATION_INGESTION.md) owns source-based extraction and knowledge formation; Memory owns durable admission and context assembly.
-   [Workflow](WORKFLOW_ORCHESTRATION_ENGINE.md), [Agent Runtime](NEXUS-AGENT-RUNTIME-LIFECYCLE.md), [Tool Runtime](NEXUS-TOOL-RUNTIME-CAPABILITY.md), [Governance](NEXUS-GOVERNANCE-POLICY-SAFETY-CONTROL.md), [Identity](NEXUS-IDENTITY-ACCESS-TRUST-SYSTEM.md), [Persistence](NEXUS-PERSISTENCE-STATE-DATA-INFRASTRUCTURE.md), [Security](NEXUS_SECURITY_THREAT_DEFENSE.md), [Observability](NEXUS_OBSERVABILITY_AUDIT_TELEMETRY.md) boundaries unchanged.

Next layer is **CONTRACTS**: detailed schemas, state machines, request/result/error contracts, policy/credential interfaces, adapter protocols, queue/reconciliation handoffs, and test implementations. Field lists, API names, and state-machine sketches in this document are nonbinding contract candidates; normative boundary requirements remain in effect.
