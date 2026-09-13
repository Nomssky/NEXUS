# NEXUS --- Agent Runtime & Agent Lifecycle System

**Status:** PROPOSED\
**Module:** Agent Runtime & Agent Lifecycle\
**Parent:** NEXUS Core / Workflow Orchestration Engine\
**Purpose:** Menjadi execution layer untuk agent permanen maupun
temporary yang dapat bekerja secara autonomous, terkontrol, terisolasi,
dan dapat dipulihkan.

------------------------------------------------------------------------

## 1. Core Principle

> **Agent adalah unit kerja autonomous yang memiliki identitas,
> capability, authority, context, memory scope, model, dan lifecycle
> yang terkontrol.**

Agent bukan sekadar prompt atau chat persona.

NEXUS harus memisahkan:

-   **Agent Definition** --- blueprint/configuration agent.
-   **Agent Runtime Instance** --- proses agent yang benar-benar
    berjalan.
-   **Agent Task** --- pekerjaan spesifik yang diberikan kepada runtime.
-   **Agent Lifecycle** --- status hidup/mati agent.
-   **Agent Authority** --- apa yang boleh dilakukan agent.
-   **Agent Scope** --- business/division/person yang menjadi wilayah
    agent.

------------------------------------------------------------------------

# 2. Agent Definition

Setiap agent memiliki definition yang versioned.

Minimum:

``` yaml
agent_id:
name:
version:
description:
role:
type:
scope:
capabilities:
permissions:
model_policy:
tool_policy:
memory_policy:
context_policy:
budget_policy:
lifecycle_policy:
supervision_policy:
```

### Agent Type

Minimal:

-   `executive`
-   `specialist`
-   `worker`
-   `researcher`
-   `monitor`
-   `reviewer`
-   `temporary`
-   `custom`

NEXUS harus memungkinkan custom type di masa depan.

------------------------------------------------------------------------

# 3. Agent Runtime Instance

Satu definition dapat memiliki banyak runtime instance.

Contoh:

``` text
Content Writer Definition
        │
        ├── Runtime A — Business Clothing
        ├── Runtime B — Business Shoes
        └── Runtime C — Temporary Campaign
```

Runtime instance memiliki:

``` yaml
runtime_id:
agent_id:
agent_version:
business_id:
division_id:
owner_scope:
status:
created_at:
started_at:
last_heartbeat:
current_task:
parent_runtime_id:
child_runtime_ids:
resource_usage:
health:
```

Runtime tidak boleh mengubah definition global secara langsung.

------------------------------------------------------------------------

# 4. Lifecycle

Lifecycle canonical:

``` text
REQUESTED
   ↓
PROVISIONING
   ↓
READY
   ↓
ACTIVE
   ↓
IDLE
   ↓
PAUSED
   ↓
TERMINATING
   ↓
TERMINATED
```

Failure path:

``` text
PROVISIONING → FAILED
ACTIVE       → DEGRADED
ACTIVE       → FAILED
PAUSED       → FAILED
```

Recovery:

``` text
FAILED → RECOVERING → READY / ACTIVE / TERMINATED
DEGRADED → RECOVERING
```

------------------------------------------------------------------------

# 5. State Semantics

### REQUESTED

Agent diminta oleh workflow, event, user, atau NEXUS Core.

### PROVISIONING

Runtime menyiapkan model, tools, context, permissions, sandbox, dan
resource.

### READY

Agent siap menerima pekerjaan.

### ACTIVE

Agent sedang menjalankan task.

### IDLE

Agent hidup tetapi tidak memiliki task aktif.

### PAUSED

Execution dihentikan sementara tanpa menghapus runtime state.

### DEGRADED

Agent masih hidup tetapi sebagian capability mengalami gangguan.

### FAILED

Agent tidak dapat melanjutkan execution secara normal.

### RECOVERING

NEXUS sedang memulihkan agent.

### TERMINATING

Runtime sedang dihentikan secara graceful.

### TERMINATED

Runtime sudah dihentikan.

------------------------------------------------------------------------

# 6. Permanent vs Temporary Agent

NEXUS wajib mendukung dua mode utama.

## Permanent Agent

Agent berjalan sebagai bagian tetap dari organisasi.

Contoh:

``` text
Social Media Manager
Business Analyst
Research Agent
Executive Coordinator
```

Lifecycle permanent agent dapat terus hidup selama business/division
aktif.

## Temporary Agent

Agent dibuat untuk kebutuhan spesifik.

Contoh:

``` text
Campaign Research Agent
Competitor Analysis Agent
One-time Product Description Agent
Launch Event Planner
```

Temporary agent harus memiliki:

``` yaml
ttl:
max_tasks:
max_runtime:
termination_condition:
cleanup_policy:
```

Temporary agent wajib terminated ketika objective selesai atau TTL
habis, kecuali NEXUS Core secara eksplisit memperpanjang lifecycle
berdasarkan governance.

------------------------------------------------------------------------

# 7. Agent Spawning

Agent dapat meminta child agent melalui orchestration layer.

Flow:

``` text
Parent Agent
     ↓
Need identified
     ↓
Delegation Request
     ↓
Workflow Engine
     ↓
Permission Check
     ↓
Agent Runtime Manager
     ↓
Child Agent
```

Agent **tidak boleh langsung membuat process agent baru tanpa melalui
control plane NEXUS**.

------------------------------------------------------------------------

# 8. Anti-Swarm Protection

Autonomous spawning harus dibatasi.

Controls:

-   maximum child agents
-   maximum depth
-   maximum total descendants
-   spawn rate limit
-   resource budget
-   token budget
-   task budget
-   TTL
-   duplicate-role detection
-   recursive spawning detection
-   objective relevance check

Contoh:

``` text
Agent A
 └── Agent B
      └── Agent C
           └── Agent D
```

Depth harus memiliki batas governance.

Agent tidak boleh menghasilkan swarm tanpa batas.

------------------------------------------------------------------------

# 9. Agent Authority

Setiap agent memiliki authority yang eksplisit.

Authority terdiri dari:

``` text
READ
WRITE
EXECUTE
DELEGATE
APPROVE
PUBLISH
DELETE
ADMIN
```

Default principle:

> **Agent hanya memiliki authority yang diperlukan untuk menjalankan
> tugasnya.**

Tidak ada self-escalation.

Agent tidak boleh:

-   memberikan permission kepada dirinya sendiri
-   menaikkan authority sendiri
-   mengubah governance
-   menghapus audit trail
-   melewati approval gate
-   mengakses business lain tanpa authorization

------------------------------------------------------------------------

# 10. Business Scope

Karena satu NEXUS mendukung banyak business, agent harus memiliki scope.

Contoh:

``` text
NEXUS
├── Business A
│   ├── Media
│   │   └── Content Agent
│   └── Business
│       └── Finance Agent
│
└── Business B
    ├── Media
    │   └── Content Agent
    └── Research
        └── Research Agent
```

Agent Business A tidak boleh otomatis membaca data Business B.

Cross-business access harus:

``` text
REQUEST
→ AUTHORIZATION
→ POLICY CHECK
→ APPROVAL IF REQUIRED
→ EXECUTION
→ AUDIT
```

------------------------------------------------------------------------

# 11. Division Scope

Agent juga memiliki division scope.

Contoh:

``` yaml
business_id: clothing
division_id: media
```

Agent Media tidak otomatis memiliki akses penuh ke Business division.

NEXUS Core dapat melakukan cross-division intervention ketika
diperlukan, tetapi specialist agent tetap bekerja sesuai authority-nya.

------------------------------------------------------------------------

# 12. Global Agents

NEXUS juga mendukung global agents.

Contoh:

``` text
NEXUS Executive
NEXUS Attention
NEXUS Security
NEXUS Governance
NEXUS System Monitor
```

Global agent memiliki scope khusus dan tidak boleh disamakan dengan
specialist agent business.

------------------------------------------------------------------------

# 13. Executive vs Specialist

### NEXUS Executive

Bertugas:

-   memahami objective
-   menentukan prioritas
-   melakukan delegation
-   mengoordinasikan workflow
-   melakukan escalation
-   melakukan replanning
-   melakukan cross-division intervention

Executive **bukan berarti mengerjakan seluruh pekerjaan sendiri**.

### Specialist Agent

Bertugas melakukan pekerjaan domain.

Contoh:

``` text
Content Writer
SEO Specialist
Market Researcher
Customer Analyst
Finance Analyst
Designer
```

------------------------------------------------------------------------

# 14. Capability System

Capability adalah kemampuan yang tersedia bagi agent.

Contoh:

``` text
web_search
file_read
database_query
social_media_publish
image_generation
code_execution
data_analysis
email_send
browser_action
```

Capability harus terpisah dari permission.

Contoh:

``` text
Capability: social_media_publish
Permission: WRITE
Scope: Business A / Media
```

Memiliki capability tidak otomatis berarti agent boleh menggunakannya.

------------------------------------------------------------------------

# 15. Tool Access

Tool access harus diberikan melalui Tool Runtime.

``` text
Agent
  ↓
Tool Request
  ↓
Tool Runtime
  ↓
Permission Check
  ↓
Scope Check
  ↓
Policy Check
  ↓
Tool Execution
  ↓
Result
  ↓
Agent
```

Agent tidak boleh bypass Tool Runtime.

------------------------------------------------------------------------

# 16. Model Routing

Model ditentukan berdasarkan kebutuhan agent/task.

Priority:

``` text
Task Requirement
     ↓
Agent Model Policy
     ↓
Available Providers
     ↓
Cost / Latency / Capability
     ↓
Health
     ↓
Model Selection
```

Provider yang harus didukung:

-   local models
-   Ollama
-   Hugging Face/local inference
-   OpenRouter
-   custom providers

Agent A dan Agent B boleh menggunakan model berbeda.

Task yang berbeda dari agent yang sama juga boleh menggunakan model
berbeda.

------------------------------------------------------------------------

# 17. Model Failover

Jika provider/model gagal:

``` text
Primary Model
      ↓
Failure
      ↓
Retry
      ↓
Fallback Model
      ↓
Fallback Provider
      ↓
Attention / Failure
```

Fallback harus tetap mematuhi capability dan policy agent.

------------------------------------------------------------------------

# 18. Context Assembly

Sebelum execution, runtime membangun context.

Context dapat terdiri dari:

``` text
System Policy
Agent Definition
Business Context
Division Context
Objective Context
Task Context
Relevant Memory
Workflow State
Recent Events
Artifacts
Tool Results
Previous Attempts
```

Context harus relevan, bukan memasukkan seluruh data secara membabi
buta.

------------------------------------------------------------------------

# 19. "Why" Context

Agent harus memahami alasan pekerjaan.

Contoh:

``` text
Task:
Buat 5 konten Instagram.

Why:
Meningkatkan awareness produk baru dalam campaign 14 hari.

Objective:
Mencapai peningkatan engagement campaign.

Business:
Clothing Store.

Division:
Media.
```

Dengan demikian agent tidak hanya menyelesaikan task, tetapi dapat
menilai apakah hasilnya masih sesuai objective.

------------------------------------------------------------------------

# 20. Memory Access

Memory harus memiliki scope.

Minimal:

``` text
Global Memory
Business Memory
Division Memory
Agent Memory
Task Memory
Temporary Memory
```

Agent hanya mendapatkan memory sesuai authorization.

Temporary agent dapat menggunakan temporary memory yang otomatis
dibersihkan setelah termination sesuai retention policy.

------------------------------------------------------------------------

# 21. Agent Sandbox

Runtime dapat memiliki sandbox untuk:

-   temporary files
-   generated artifacts
-   code execution
-   intermediate data
-   temporary state

Sandbox harus:

-   scoped
-   isolated
-   resource limited
-   auditable
-   disposable

------------------------------------------------------------------------

# 22. Budget

Setiap runtime memiliki budget.

Minimal:

``` text
time_budget
token_budget
model_cost_budget
tool_call_budget
spawn_budget
parallel_task_budget
storage_budget
```

Budget exhaustion harus menghasilkan controlled behavior:

``` text
STOP
REPLAN
FALLBACK
ESCALATE
```

bukan infinite retry.

------------------------------------------------------------------------

# 23. Heartbeat & Health

Agent runtime harus mengirim heartbeat.

Contoh:

``` text
heartbeat
health status
current task
resource usage
last successful action
last error
```

Jika heartbeat hilang:

``` text
Healthy
   ↓
Suspected
   ↓
Unresponsive
   ↓
Recovery
```

NEXUS harus dapat membedakan:

-   agent sedang bekerja lama
-   agent blocked
-   provider sedang lambat
-   runtime mati
-   network failure

------------------------------------------------------------------------

# 24. Concurrency

Satu agent dapat:

-   menjalankan satu task
-   menjalankan beberapa task jika definition mengizinkan
-   menggunakan parallel workers

Contoh:

``` yaml
max_concurrent_tasks: 3
```

Concurrency tidak boleh menyebabkan race condition pada state bersama.

------------------------------------------------------------------------

# 25. Agent Scheduling

Agent runtime tidak selalu harus aktif.

Scheduler dapat:

-   wake agent
-   assign task
-   pause agent
-   resume agent
-   terminate temporary agent
-   rebalance workloads

Untuk permanent autonomous agents, scheduler dapat menjaga runtime tetap
available 24/7 sesuai policy.

------------------------------------------------------------------------

# 26. Agent Collaboration

Agent-to-agent collaboration harus melalui protocol.

Contoh:

``` text
Agent A
  ↓
Collaboration Request
  ↓
Workflow / Message Bus
  ↓
Agent B
  ↓
Result / Artifact
  ↓
Agent A
```

Agent tidak boleh bergantung pada hidden direct communication yang tidak
tercatat.

------------------------------------------------------------------------

# 27. Shared Artifacts

Agent dapat menghasilkan:

-   documents
-   datasets
-   reports
-   images
-   code
-   analysis
-   decisions

Artifact harus memiliki:

``` yaml
artifact_id:
creator:
business_id:
division_id:
workflow_id:
task_id:
created_at:
version:
access_policy:
```

------------------------------------------------------------------------

# 28. Failure Recovery

Jika agent gagal:

``` text
Failure Detected
      ↓
Capture State
      ↓
Classify Failure
      ↓
Retry?
  ├── YES → Retry
  └── NO
       ↓
Recover?
  ├── YES → Recover Runtime
  └── NO
       ↓
Reassign Task
       ↓
Fallback Agent
       ↓
Attention
```

Task state harus tetap durable.

------------------------------------------------------------------------

# 29. Graceful Shutdown

Saat agent dihentikan:

1.  stop accepting new tasks
2.  finish safe operations
3.  checkpoint state
4.  release resources
5.  persist required artifacts
6.  close tools
7.  terminate runtime

Emergency shutdown dapat memotong execution berdasarkan governance.

------------------------------------------------------------------------

# 30. Versioning

Agent definition harus versioned.

Contoh:

``` text
Content Agent v1
Content Agent v2
Content Agent v3
```

Runtime yang sedang berjalan tetap menggunakan version yang telah dipin.

Update definition tidak boleh diam-diam mengubah execution yang sedang
berlangsung.

------------------------------------------------------------------------

# 31. Agent Templates

NEXUS harus mendukung template.

Contoh:

``` text
Research Agent Template
Content Agent Template
Analyst Agent Template
Monitor Agent Template
Reviewer Agent Template
Custom Agent Template
```

Template dapat di-clone dan dikustomisasi.

------------------------------------------------------------------------

# 32. Per-Person Customization

Sistem harus memungkinkan agent set berbeda untuk orang berbeda.

Contoh:

``` text
Person A
 ├── Executive
 ├── Content
 └── Research

Person B
 ├── Business Analyst
 └── Finance
```

Agent configuration harus mengikuti ownership/scope tanpa mengganggu
agent milik orang lain.

------------------------------------------------------------------------

# 33. Agent Governance

Governance mengontrol:

-   siapa yang dapat membuat agent
-   siapa yang dapat menghapus agent
-   capability yang boleh diberikan
-   permission maksimum
-   model/provider yang boleh digunakan
-   budget maksimum
-   cross-business access
-   spawning limit
-   approval requirements
-   retention
-   emergency stop

Agent tidak memiliki authority untuk mengubah governance.

------------------------------------------------------------------------

# 34. Observability

Setiap runtime harus menghasilkan:

``` text
Agent Created
Agent Started
Agent Paused
Agent Resumed
Task Assigned
Task Started
Tool Called
Model Called
Artifact Created
Delegation Requested
Permission Denied
Error
Recovery
Agent Terminated
```

Semua event penting harus dapat ditelusuri ke:

``` text
Business
Division
Objective
Workflow
Task
Agent
Runtime
Tool
Model
```

------------------------------------------------------------------------

# 35. Audit Trail

Audit trail harus immutable sesuai governance.

Minimal:

``` yaml
timestamp:
actor:
agent_id:
runtime_id:
business_id:
division_id:
action:
target:
reason:
policy_decision:
result:
```

------------------------------------------------------------------------

# 36. Security Boundary

Agent Runtime tidak boleh menjadi security authority.

Security decision berada di control/policy layer.

Architecture:

``` text
Agent
 ↓
Runtime
 ↓
Policy / Authorization
 ↓
Tool Runtime
 ↓
External System
```

Agent harus menganggap semua external input sebagai untrusted data.

Prompt injection dari tool/web/file tidak boleh mengubah authority
agent.

------------------------------------------------------------------------

# 37. Objective Alignment

Sebelum execution:

``` text
Task
 ↓
Objective Context
 ↓
Agent Scope
 ↓
Authority
 ↓
Capability
 ↓
Budget
 ↓
Execute
```

Jika task tidak relevan dengan objective:

``` text
IGNORE
REPLAN
ASK/ATTENTION
```

sesuai governance.

------------------------------------------------------------------------

# 38. Agent Runtime API Concept

Conceptual API:

``` text
createAgent()
getAgent()
updateAgentDefinition()
deleteAgent()

spawnRuntime()
startRuntime()
pauseRuntime()
resumeRuntime()
terminateRuntime()

assignTask()
cancelTask()

getHealth()
getRuntimeState()

requestDelegation()
requestCapability()

checkpoint()
recoverRuntime()
```

Implementasi API final mengikuti architecture repository saat coding.

------------------------------------------------------------------------

# 39. Runtime State Machine

Canonical:

``` text
REQUESTED
   ↓
PROVISIONING
   ↓
READY
   ↓
ACTIVE ↔ IDLE
   ↓      ↓
DEGRADED  PAUSED
   ↓        ↓
RECOVERING ┘
   ↓
ACTIVE / READY / TERMINATED

ACTIVE / PAUSED / READY
        ↓
   TERMINATING
        ↓
   TERMINATED
```

Failure state harus selalu dapat dijelaskan melalui error classification
dan recovery policy.

------------------------------------------------------------------------

# 40. Integration

Agent Runtime terhubung dengan:

``` text
NEXUS Core
├── Objective Engine
├── Attention
├── Memory
├── Event Trigger System
├── Workflow Orchestration
├── Agent Runtime ← CURRENT MODULE
├── Tool Runtime
├── Model Router
├── Governance
├── Observability
└── Persistence
```

Agent Runtime bukan pusat seluruh sistem. Ia adalah execution layer.

------------------------------------------------------------------------

# 41. Testing Requirements

Minimum tests:

### Lifecycle

-   create
-   start
-   pause
-   resume
-   terminate
-   failure
-   recovery

### Temporary Agents

-   TTL
-   objective completion
-   cleanup
-   forced termination

### Security

-   permission denial
-   scope isolation
-   cross-business denial
-   cross-division denial
-   prompt injection resistance
-   self-escalation prevention

### Reliability

-   crash recovery
-   provider outage
-   tool outage
-   heartbeat timeout
-   duplicate execution

### Concurrency

-   multiple agents
-   multiple businesses
-   multiple divisions
-   parallel tasks
-   spawn limits

### Model

-   local model
-   OpenRouter
-   custom provider
-   fallback
-   provider failure

------------------------------------------------------------------------

# 42. Acceptance Criteria

Module dianggap selesai secara arsitektur jika:

-   permanent agents supported
-   temporary agents supported
-   agent/runtime dipisahkan
-   lifecycle jelas
-   agent dapat spawn melalui control plane
-   spawning dibatasi
-   business isolation enforced
-   division isolation enforced
-   permission enforced
-   capability terpisah dari permission
-   model routing fleksibel
-   local/OpenRouter/custom provider supported
-   memory scoped
-   context objective-aware
-   heartbeat tersedia
-   recovery tersedia
-   budgets tersedia
-   concurrency dikontrol
-   collaboration tercatat
-   artifacts dapat ditelusuri
-   audit trail tersedia
-   versioning tersedia
-   governance tidak dapat diubah agent
-   24/7 autonomous runtime didukung
-   temporary runtime dapat dibersihkan otomatis.

------------------------------------------------------------------------

# 43. Locked Design Principle

> **NEXUS agents are autonomous execution units, not chat personas.
> Every agent has a controlled identity, lifecycle, scope, capability,
> authority, context, memory, model policy, budget, and runtime state.
> Agents may operate continuously, collaborate, delegate, recover, and
> adapt, but they can never escape NEXUS governance, business
> boundaries, or objective alignment.**

------------------------------------------------------------------------

# 44. Next Module

Setelah module ini dikunci, layer berikutnya:

**Tool Runtime & Capability Execution System**

Fokusnya adalah bagaimana agent benar-benar menggunakan tools secara
aman: web, files, browser, APIs, code execution, external services,
credentials, sandboxing, permission checks, rate limits, dan audit.
