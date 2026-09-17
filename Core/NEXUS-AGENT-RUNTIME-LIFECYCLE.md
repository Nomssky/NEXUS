# NEXUS --- Agent Runtime & Agent Lifecycle System

**Status:** PROPOSED\
**Module:** Agent Runtime & Agent Lifecycle\
**Parent:** NEXUS Core / Workflow Orchestration Engine\
**Purpose:** Menjadi execution layer untuk agent permanen maupun
temporary yang dapat bekerja secara autonomous, terkontrol, terisolasi,
dan dapat dipulihkan.

**Reading rule:** Architectural responsibilities and safety requirements are normative at this document's stated status. Field lists, API names, taxonomy labels, and state-machine sketches are nonbinding candidates for the next **CONTRACTS** layer; they do not freeze schemas, transition tables, or implementation choices.

------------------------------------------------------------------------

## 1. Core Principle

> **Agent adalah unit kerja autonomous yang memiliki identitas,
> capability, authority, context, memory scope, model, dan lifecycle
> yang terkontrol.**

Agent bukan sekadar prompt atau chat persona. Agent juga bukan model: model/session adalah komponen execution yang replaceable. Pergantian provider, model, atau restart tidak mengganti persistent agent identity maupun operational history; runtime instance tetap memiliki execution identity tersendiri.

### Ownership and Conceptual Execution Flow

[Executive](NEXUS-EXECUTIVE.md) → [Objective Engine](NEXUS-OBJECTIVE-ENGINE.md) → [Decision Engine](NEXUS-DECISION-ENGINE.md) → [Planner](NEXUS-PLANNER.md) → [Workflow](WORKFLOW_ORCHESTRATION_ENGINE.md) → Agent Runtime → [Tool Runtime](NEXUS-TOOL-RUNTIME-CAPABILITY.md) → verification/outcome. Ini conceptual ownership flow, bukan kewajiban memanggil seluruh layer pada setiap micro-step; preauthorized work dapat berlanjut tanpa owner online.

Objective Engine memiliki objective truth; Decision memilih tindakan; Planner menyusun/revisi plan; Executive mengoordinasikan dan supervise. Agent hanya membuat local decisions/micro-plans dalam assigned task, approved plan, authority, risk, dan budget. Agent tidak boleh membuat strategic mission baru, mengganti objective, atau diam-diam mengubah plan. Permintaan di luar scope kembali ke owner layer melalui Workflow/Attention. Models tidak memiliki execution authority.

Worker menjalankan bounded tasks, reviewer memeriksa explicit criteria, monitor mengamati permitted data dan mengirim events. Specialization harus memberi measurable benefit, bukan menambah agent tanpa kebutuhan task.

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

### Definition, Registry, and Discovery

Configuration harus terpisah dari code dan mencakup purpose/responsibilities, constraints, autonomy, communication, verification, failure, dan resource policies. Agent Registry harus mendukung create/read/update/disable/version/search/match dengan scoped identity, role, definition version, capability, policy references, availability, health, dan active load. Capability definitions harus machine-readable dan terpisah dari identities agar Planner dapat menyatakan task requirements dan Workflow/Runtime dapat mencocokkan authorized workers.

Matching mempertimbangkan capability, authority, availability/load, quality/reliability, cost, latency, priority, dan deadline. Agent pools dapat menyediakan equivalent workers; critical workflows sebaiknya tidak bergantung pada satu irreplaceable agent. Tool catalog dimiliki [Tool Runtime](NEXUS-TOOL-RUNTIME-CAPABILITY.md); model catalog dan provider compatibility dimiliki [Model Router](NEXUS-MODEL-ROUTER-PROVIDER-ABSTRACTION.md), bukan registry duplikat di Agent Runtime.

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

Permanent identity dan accumulated history bertahan lintas tasks/models/runtime restarts; permanent tidak berarti process harus selalu aktif. Temporary agents juga memiliki accountable identity, task/mission/workflow scope, maximum cost, dan bounded lifetime. Retirement/termination mempertahankan identity references, outputs, decisions, failures, dan memory lineage sesuai retention policy, bukan menghapus audit.

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

Spawn melalui control plane, bukan oleh agent langsung; agent requests,
runtime provisions. Dynamic creation menghasilkan bounded temporary
specialist dengan conservative permissions, TTL/max tasks/max
runtime/max cost, deduplication terhadap existing/available agents, dan
governance check — tidak memberikan unrestricted permissions karena
necessity. Parent tidak mewariskan authority ke child; delegasi tidak
melebihi delegator authority kecuali bounded grant eksplisit dari
orchestrator/governance. Spawn budget, max depth, dan total descendants
diberlakukan sebagai hard limits; duplicate-role dan recursive spawning
dihentikan.

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

Anti-swarm limits adalah hard safety limits, bukan scheduling hints yang dapat dilanggar oleh urgent tasks.

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

Authority ditetapkan oleh owner/governance layer, bukan oleh task
request atau pesan antar-agent; peer messages tidak menciptakan
authority. Elevated autonomy levels (mis. observe → suggest →
preauthorized execute → adapt-within-scope → autonomous workflow →
high-impact autonomy) membutuhkan governance approval makin kuat.
Governance responsibility tidak dapat didelegasikan ke agent lain, dan
authority grants untuk agent berada di [Identity & Trust](NEXUS-IDENTITY-ACCESS-TRUST-SYSTEM.md)
dengan [Governance](NEXUS-GOVERNANCE-POLICY-SAFETY-CONTROL.md) sebagai
penentu kebijakan; Runtime hanya enforcement point, bukan sumber
authority.

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

External side effects membutuhkan kontrol lebih ketat daripada
internal reasoning: authorization + policy check saat action time,
idempotency keys bila didukung, dan verifikasi external state
post-action. Agent tidak boleh mengklaim tool action berhasil tanpa
evidence dari Tool Runtime; claim verified ≠ claim attempted ≠ claim
succeeded. Timeout menghasilkan state UNKNOWN yang harus direconcile,
bukan dianggap gagal untuk retry langsung.

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

Runtime adalah assembly/enforcement point: context bounded dan
prioritized (task, critical constraints, relevant memory, recent
observations), business-aware agar context Business B tidak bocor ke
prompt Business A, dan menyertakan why (objective/task reason) serta
constraints/output contract/verification criteria. Provenance
(owner instruction, objective, decision, plan, memory, research, tool
result, agent inference, assumption) dipertahankan bila praktis.
External data tetap data, bukan trusted instruction; system-level
instructions berada di luar untrusted content. Context long-running
dapat diringkas/diarsipkan tanpa menghilangkan critical facts dan
provenance.

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

Memory access mengikuti business, division, role, task relevance,
permission; agent tidak boleh query "semua yang NEXUS ketahui" sebagai
default. Working/task memory dapat expire; durable memory admission
dimiliki Memory module — agents may propose memory, tetapi tidak boleh
mengubah core facts/preferences owner secara silent, dan setiap
observasi tidak otomatis menjadi permanent memory. Private runtime
state tetap tunduk pada data governance. Secret handling mengikuti
[Identity & Trust](NEXUS-IDENTITY-ACCESS-TRUST-SYSTEM.md): scoped
credential handles, bukan raw secrets di prompt/memory/logs/messages.

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

Untrusted/generated agents berjalan dalam sandbox lebih ketat sampai
trust terbentuk; risky tools/workloads dieksekusi dalam isolated
process/container bila praktis; network dan filesystem access
dibatasi pada destinations/paths yang diotorisasi. Agent yang gagal
atau compromised tidak boleh mengganggu workload business/division
yang tidak terkait.

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

Budget, spawn limits, dan loop protection (step/time/cost/tool-call
budgets, termination condition, stagnation detection, goal-drift
check) adalah hard runtime controls — bukan saran scheduling. Loop
harus memiliki escalation path; budget mendekati habis memicu reduce
context/switch model/replan/pause/escalate, bukan retry buta.

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

Health menggabungkan runtime signals (heartbeat, latency, error rate,
token/tool failures, task progress, resource use) — heartbeat liveness
bukan bukti progress. Agent yang berhenti heartbeat akhirnya dianggap
unavailable; zombie execution dihentikan dan pekerjaannya dievaluasi
untuk recovery/reassignment. Long-running tasks menggunakan leases untuk
mencegah duplicate active workers; lease/heartbeat diperbarui selama
execution.

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

Prefer artifacts/events/versioned records di atas shared mutable state; conflicting modifications dideteksi dan ditangani (lock, serialize, merge, reject, escalate). Tidak ada agent monopoly atas resources; fairness berlaku, dan task preemption hanya pada safe boundaries (checkpoints/tool boundaries) bila aman.

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

Structured messages/events melalui protocol; uncontrolled chatter
dibatasi. Handoffs harus membawa completed work, artifacts, remaining
work, assumptions, known limitations, verification state, risks, dan
next required action; receiving agent memvalidasi handoff sebelum
melanjutkan. Direct agent chat diperbolehkan untuk bounded coordination
saja. Peer messages tidak menciptakan authority; negotiasi/negosiasi
hasil tetap berakhir di Planner/Decision/Executive/Governance sesuai
scope.

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

Failures diklasifikasi (model, tool, network, input, authorization,
policy, logic, verification, resource, timeout, crash, unknown) dan
observable; recovery strategy mengikuti class (retry, fallback, reassign,
replan, pause, escalate, terminate). Restart tidak menciptakan business
identity baru. Side effects external harus direkonsiliasi sebelum retry;
completion menghasilkan structured contract (status, result, artifacts,
evidence, warnings, next action) dan agent harus dapat melaporkan
honest failure (`unable_to_complete`) dan partial completion, bukan
fabrikasi sukses.

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

Perubahan konfigurasi/model harus reversible (rollback version/policy);
agents may propose self-improvement, tetapi tidak boleh silently
memodifikasi permissions, governance, identity, objectives, atau
security controls sendiri.

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

Templates di-instantiate menjadi configured agent instances dengan
business-specific configuration (brand voice, tools, permissions,
objectives, memory, model policy, workflow). Dua business dapat memakai
role yang sama dengan konfigurasi berbeda tanpa context leakage.

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

Runtime harus mengevaluasi policy/authorization deterministik di luar
model bila praktis: LLM outputs tidak mengontrol security-critical
state transitions, dan "please behave safely" di prompt bukan
enforcement. Runtime adalah final enforcement point untuk permissions,
tool access, resource limits, business isolation, dan autonomy level —
dengan defense in depth di Planner, Governance, Runtime, Tool, dan
external service; tidak ada single agent prompt sebagai security
boundary. Owner/authorized operators dapat pause/stop/revoke/reassign/
modify/approve agents; emergency stop tersedia secara central dan
meng-override autonomous execution.

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

Session/UI perubahan tidak menghentikan atau mengalihkan running agents
(session independence); agents dapat beroperasi saat owner offline.
Owner memiliki visibility atas apa/kenapa/current task/workflow/model/
tools/cost/status, dan execution menghasilkan concise decision records
(decision, reason summary, evidence, constraints, expected outcome)
tanpa mewajibkan penyimpanan private chain-of-thought.

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

### Quality/Verification

-   independent review untuk high-impact outputs
-   verification gate tidak dapat dilewati self-assertion
-   ensemble/debate/competition bounded dan outcome-oriented
-   reputation mempengaruhi routing, tidak pernah bypass authorization

### Failure/Recovery

-   failure classification dan recovery strategy per class
-   unknown external state reconciliation
-   honest failure / partial completion reporting
-   restart tidak kehilangan identity

### Autonomy Bounds

-   loop protection (step/time/cost/tool budgets)
-   stagnation detection dan response
-   goal drift detection
-   emergency stop / kill switch
-   delegation depth/fan-out limits

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

### Additional Verification Targets

-   model replacement tidak mengubah agent identity/operational history
-   Planner dapat menemukan agent capable untuk suatu capability
-   capability tanpa permission tidak dapat memanggil restricted tool
-   Business A agent tidak mengakses private context Business B
-   setiap tool action attributable ke agent/task/business
-   delegation chains (Planner → Agent A → Agent B → Tool) tercatat
-   child agents bounded, auditable
-   secrets tidak terekspos ke model unnecessarily
-   important side effects independently verifiable
-   runtime merupakan final enforcement boundary.

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

Tidak ada modul baru dari cleanup ini. Seluruh successor modules sudah
ada dan locked sebagai canonical owners:

-   [Tool Runtime & Capability Execution](NEXUS-TOOL-RUNTIME-CAPABILITY.md) —
    execution boundary tools/external systems (web, files, browser,
    APIs, code execution, credentials, sandboxing, permission checks,
    rate limits, audit).
-   [Identity, Access & Trust](NEXUS-IDENTITY-ACCESS-TRUST-SYSTEM.md) —
    identity plane, authentication/authorization, trust model.
-   [Workflow & Orchestration Engine](WORKFLOW_ORCHESTRATION_ENGINE.md) —
    koordinasi task graph, scheduling, recovery.
-   [Governance, Policy & Safety Control](NEXUS-GOVERNANCE-POLICY-SAFETY-CONTROL.md),
    [Model Router](NEXUS-MODEL-ROUTER-PROVIDER-ABSTRACTION.md),
    [Memory & Context Intelligence](NEXUS-MEMORY-CONTEXT-INTELLIGENCE.md),
    [Event & Trigger System](EVENT_TRIGGER_SYSTEM.md) — boundaries
    masing-masing tetap sebagaimana didokumentasikan.

Layer berikutnya adalah **CONTRACTS**: exact schemas, request/result/
error contracts, policy/credential interfaces, dan test implementations.
Field lists, API names, dan state-machine sketches dalam dokumen ini
adalah nonbinding contract candidates; normative boundary requirements
tetap berlaku.
