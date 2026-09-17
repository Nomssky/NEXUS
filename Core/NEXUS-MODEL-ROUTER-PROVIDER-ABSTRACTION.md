# NEXUS --- Model Router & Provider Abstraction System

**Status:** PROPOSED\
**Module:** Core Infrastructure\
**Depends on:** Agent Runtime & Lifecycle, Workflow & Orchestration,
Tool Runtime & Capability Execution\
**Next Layer:** CONTRACTS (all successor modules exist and are canonical)

------------------------------------------------------------------------

## 1. Purpose

Model Router & Provider Abstraction System adalah lapisan NEXUS yang
menentukan **model AI mana yang digunakan, melalui provider apa, dengan
konfigurasi apa, dan kapan harus berpindah model/provider**.

NEXUS tidak boleh mengikat agent ke satu model tertentu.

Prinsip utama:

> Agent meminta kemampuan AI. Model Router menentukan implementasi
> model/provider yang paling sesuai berdasarkan kebutuhan agent, task,
> objective, policy, resource, dan kondisi runtime.

------------------------------------------------------------------------

## 2. Core Requirements

Sistem wajib mendukung:

-   local-first inference
-   Ollama
-   local Hugging Face / compatible runtimes
-   OpenRouter
-   custom providers
-   model berbeda untuk agent berbeda
-   model berbeda untuk task berbeda
-   dynamic routing
-   fallback dan failover
-   model health monitoring
-   cost/latency/quality trade-off
-   context-window awareness
-   tool calling
-   structured output
-   streaming
-   multimodal model support bila tersedia
-   model/version pinning
-   privacy-aware routing
-   token/cost accounting
-   provider credential isolation
-   observability dan audit

Tidak ada asumsi bahwa satu model adalah model terbaik untuk seluruh
NEXUS.

------------------------------------------------------------------------

# 3. Architectural Position

``` text
                    NEXUS CORE
                        │
                        ▼
               AGENT / WORKFLOW
                        │
                        ▼
             ┌────────────────────┐
             │   MODEL ROUTER     │
             └────────────────────┘
                 │      │      │
                 ▼      ▼      ▼
              Policy  Registry  Health
                 │      │      │
                 └──────┼──────┘
                        ▼
              PROVIDER ABSTRACTION
                 │      │      │
          ┌──────┼──────┼──────┐
          ▼      ▼      ▼      ▼
       Ollama   HF   OpenRouter Custom
          │      │      │      │
          └──────┴──────┴──────┘
                        │
                        ▼
                 MODEL EXECUTION
```

Model Router adalah control plane. Provider adalah execution backend.

------------------------------------------------------------------------

# 4. Provider Abstraction

Semua provider harus mengikuti interface internal yang konsisten.

Conceptual interface:

``` text
Provider
├── identify()
├── health_check()
├── list_models()
├── get_model_capabilities()
├── generate()
├── stream()
├── count_tokens()
├── validate_request()
└── normalize_response()
```

Provider-specific implementation tidak boleh membocorkan format
internalnya ke agent.

Contoh:

``` text
Agent
  ↓
NEXUS AI Request
  ↓
Model Router
  ↓
Provider Adapter
  ↓
Native Provider API
```

Response kemudian dinormalisasi kembali menjadi format NEXUS.

------------------------------------------------------------------------

# 5. NEXUS AI Request

Setiap permintaan model harus memiliki metadata yang cukup untuk
routing.

``` yaml
request_id:
agent_id:
workflow_id:
task_id:
business_id:
division_id:

objective_id:
objective_priority:

requested_capabilities:
  - reasoning
  - structured_output
  - tool_calling

input:
context:

constraints:
  max_latency:
  max_cost:
  privacy_level:
  required_context_window:
  required_output_format:

routing_preferences:
  prefer_local:
  allowed_providers:
  allowed_models:
  fallback_enabled:

budget:
  max_tokens:
  max_cost:
  deadline:
```

Agent tidak memilih provider secara bebas jika policy melarangnya.

------------------------------------------------------------------------

# 6. Model Registry

NEXUS membutuhkan registry terpusat untuk mengetahui model yang
tersedia.

Setiap model memiliki:

``` yaml
model_id:
provider_id:
display_name:
version:

capabilities:
  reasoning:
  coding:
  tool_calling:
  structured_output:
  vision:
  audio:
  embedding:
  long_context:

context_window:
max_output_tokens:

latency_profile:
quality_profile:

pricing:
  input:
  output:

availability:
status:

privacy:
data_retention:
external_transfer:

runtime:
local:
remote:

tags:
```

Registry bukan sekadar daftar nama model. Registry adalah sumber
kebenaran mengenai kemampuan model.

------------------------------------------------------------------------

# 7. Model Identity

Model harus dapat dibedakan berdasarkan:

``` text
provider
+
model
+
version
+
configuration
```

Contoh:

``` text
ollama:qwen3:latest
```

berbeda dengan:

``` text
ollama:qwen3:2026-xx
```

Untuk workflow penting, NEXUS harus mendukung **version pinning** agar
hasil tidak berubah secara tidak terkontrol.

------------------------------------------------------------------------

# 8. Local-First Policy

Default NEXUS:

> Gunakan model lokal bila model tersebut memenuhi kebutuhan task secara
> memadai.

Prioritas awal:

``` text
LOCAL
  ↓
LOCAL ALTERNATIVE
  ↓
OPENROUTER / APPROVED REMOTE
  ↓
CUSTOM PROVIDER
```

Namun local-first bukan berarti local-only.

Jika model lokal:

-   tidak tersedia
-   terlalu lambat
-   context window tidak cukup
-   tidak memiliki capability yang dibutuhkan
-   kualitas tidak memenuhi threshold
-   sedang unhealthy
-   resource lokal sedang penuh

Router dapat memilih provider lain sesuai policy.

------------------------------------------------------------------------

# 9. Ollama

Ollama harus diperlakukan sebagai provider adapter, bukan sebagai bagian
langsung dari agent.

NEXUS harus dapat:

-   mendeteksi Ollama
-   membaca model yang tersedia
-   health check
-   mengetahui capability model
-   mengirim inference request
-   streaming
-   menghitung/estimasi token
-   menangani timeout
-   menangani model unavailable
-   melakukan failover

Contoh:

``` text
NEXUS
 ↓
Ollama Provider Adapter
 ↓
Ollama Runtime
 ↓
Local Model
```

------------------------------------------------------------------------

# 10. Hugging Face / Local Models

NEXUS harus mendukung model lokal yang berasal dari ecosystem Hugging
Face atau runtime compatible lainnya.

Abstraksi harus menghindari ketergantungan terhadap satu inference
engine.

Contoh backend:

``` text
Hugging Face
Transformers
vLLM
llama.cpp
compatible local runtime
```

Selama backend memenuhi provider interface, Model Router dapat
menggunakannya.

------------------------------------------------------------------------

# 11. OpenRouter

OpenRouter diperlakukan sebagai remote provider/gateway.

NEXUS dapat menggunakan OpenRouter ketika:

-   model lokal tidak cocok
-   capability tertentu hanya tersedia remote
-   quality threshold tidak tercapai
-   latency lokal terlalu tinggi
-   user/policy mengizinkan remote inference.

Remote inference wajib tunduk pada:

-   privacy policy
-   business scope
-   credential policy
-   cost budget
-   model allowlist
-   data-transfer restrictions.

------------------------------------------------------------------------

# 12. Custom Providers

NEXUS harus mendukung provider custom.

Contoh:

``` text
CustomProvider
├── endpoint
├── authentication
├── request adapter
├── response adapter
├── model discovery
├── health check
└── capability declaration
```

Custom provider tidak boleh mendapatkan akses lebih luas hanya karena
merupakan provider custom.

------------------------------------------------------------------------

# 13. Agent-Level Model Policy

Setiap agent dapat memiliki policy.

Contoh:

``` yaml
agent: research-agent

preferred_models:
  - local:reasoning-model
  - openrouter:reasoning-model

fallback_enabled: true

privacy:
  allow_remote: true

budget:
  max_cost_per_task: 0.05
```

Agent lain dapat memiliki konfigurasi berbeda.

``` yaml
agent: content-agent

preferred_models:
  - local:fast-model

max_latency: 5s
```

------------------------------------------------------------------------

# 14. Task-Level Routing

Agent preference bukan aturan absolut.

Task dapat membutuhkan model berbeda.

Contoh:

``` text
Content Agent
├── caption sederhana → fast local model
├── research synthesis → reasoning model
├── image analysis → vision model
└── complex strategy → high-quality reasoning model
```

Dengan demikian:

> Model selection mengikuti pekerjaan, bukan identitas agent semata.

------------------------------------------------------------------------

# 15. Capability Matching

Router pertama-tama harus memastikan capability.

Contoh:

``` text
Task:
"Analyze uploaded product image"

Required:
vision = true
```

Model tanpa vision tidak boleh dipilih walaupun murah dan cepat.

Capability matching terjadi sebelum optimization.

``` text
REQUIREMENTS
    ↓
CAPABILITY FILTER
    ↓
POLICY FILTER
    ↓
HEALTH FILTER
    ↓
RESOURCE FILTER
    ↓
COST / LATENCY / QUALITY RANKING
    ↓
SELECT MODEL
```

------------------------------------------------------------------------

# 16. Routing Strategy

Router dapat menggunakan beberapa strategi:

### 16.1 Fixed Routing

Task tertentu selalu menggunakan model tertentu.

### 16.2 Capability Routing

Model dipilih berdasarkan capability.

### 16.3 Cost Routing

Memilih model yang memenuhi requirement dengan biaya terendah.

### 16.4 Latency Routing

Memilih model yang memenuhi deadline.

### 16.5 Quality Routing

Memprioritaskan kualitas.

### 16.6 Hybrid Routing

Menggabungkan:

``` text
quality
+
latency
+
cost
+
availability
+
privacy
```

### 16.7 Adaptive Routing

Routing dapat berubah berdasarkan hasil runtime.

------------------------------------------------------------------------

# 17. Routing Score

Model kandidat dapat diberi skor konseptual:

``` text
score =
  quality_weight
+ capability_fit
+ availability
+ latency_score
+ privacy_score
- cost_penalty
- risk_penalty
```

Bobot ditentukan policy.

Router tidak boleh memilih model hanya karena skor numeriknya tinggi
jika model melanggar hard constraint.

------------------------------------------------------------------------

# 18. Hard Constraints vs Soft Preferences

### Hard constraints

Harus dipenuhi:

``` text
privacy restriction
required capability
business scope
maximum cost
context window
allowed provider
security policy
deadline
```

### Soft preferences

Digunakan untuk ranking:

``` text
prefer local
prefer cheaper
prefer faster
prefer higher quality
prefer specific model
```

Hard constraint selalu menang.

------------------------------------------------------------------------

# 19. Context Window Awareness

Sebelum execution, Router harus mengetahui:

``` text
system tokens
+
agent context
+
objective context
+
memory
+
workflow state
+
tool results
+
user input
+
expected output
```

Jika melebihi kapasitas:

``` text
DO NOT blindly send
```

Router harus meminta Context/Memory layer melakukan:

-   compression
-   summarization
-   retrieval reduction
-   context prioritization
-   truncation berdasarkan policy.

------------------------------------------------------------------------

# 20. Structured Output

Router harus mendukung structured output bila model/provider
mendukungnya.

Contoh:

``` json
{
  "decision": "approve",
  "confidence": 0.91,
  "reason": "..."
}
```

Provider adapter bertanggung jawab mengubah format native menjadi NEXUS
standard.

Invalid structured output dapat memicu:

``` text
retry
repair
fallback model
Attention
failure
```

sesuai policy.

------------------------------------------------------------------------

# 21. Tool Calling

Model Router harus mengetahui apakah model mampu melakukan tool calling.

Namun:

> Model tidak pernah mengeksekusi tool secara langsung.

Flow:

``` text
MODEL
 ↓
TOOL CALL REQUEST
 ↓
TOOL RUNTIME
 ↓
AUTHORIZATION
 ↓
EXECUTION
 ↓
RESULT
 ↓
MODEL
```

Model Router hanya menangani interface inference.

------------------------------------------------------------------------

# 22. Streaming

Streaming harus didukung untuk model yang kompatibel.

Use cases:

-   interactive response
-   long reasoning output
-   monitoring
-   progressive generation.

Streaming harus tetap dapat diaudit sebagai satu logical inference
request.

------------------------------------------------------------------------

# 23. Multimodal

Router harus dapat memilih model berdasarkan modality:

``` text
text
image
audio
video
document
mixed
```

Jika task membutuhkan multimodal capability, model text-only tidak boleh
dipilih.

------------------------------------------------------------------------

# 24. Privacy-Aware Routing

Setiap request memiliki privacy classification.

Contoh:

``` text
PUBLIC
INTERNAL
BUSINESS
CONFIDENTIAL
HIGHLY_CONFIDENTIAL
```

Policy dapat menetapkan:

``` text
PUBLIC → local / remote
INTERNAL → approved providers
CONFIDENTIAL → local preferred
HIGHLY_CONFIDENTIAL → local only
```

Remote provider tidak boleh menerima data yang dilarang policy.

------------------------------------------------------------------------

# 25. Credential Isolation

API key provider tidak boleh diberikan kepada agent.

Flow:

``` text
Agent
 ↓
Model Router
 ↓
Credential Manager
 ↓
Provider
```

Agent hanya mengetahui:

``` text
model request
model result
```

bukan secret provider.

------------------------------------------------------------------------

# 26. Health Monitoring

Setiap provider/model memiliki health state:

``` text
UNKNOWN
HEALTHY
DEGRADED
UNAVAILABLE
RECOVERING
```

Health signals:

-   latency
-   timeout rate
-   error rate
-   rate-limit frequency
-   malformed response
-   availability
-   resource pressure.

Router harus menghindari unhealthy models jika alternatif valid
tersedia.

------------------------------------------------------------------------

# 27. Circuit Breaker

Provider yang gagal berulang kali dapat masuk:

``` text
CLOSED
 ↓
OPEN
 ↓
HALF_OPEN
 ↓
CLOSED
```

Tujuannya mencegah NEXUS terus membuang waktu/cost ke provider yang
sedang bermasalah.

------------------------------------------------------------------------

# 28. Failover

Contoh:

``` text
Preferred:
Ollama Model A

        ↓ unavailable

Ollama Model B

        ↓ insufficient capability

OpenRouter Model C

        ↓ failed

Custom Provider D
```

Fallback harus tetap mematuhi policy.

Tidak boleh fallback ke provider yang dilarang hanya demi menyelesaikan
task.

------------------------------------------------------------------------

# 29. Retry Policy

Retry harus membedakan:

``` text
transient failure
permanent failure
rate limit
invalid request
context overflow
provider outage
unknown state
```

Tidak semua error boleh di-retry.

Exponential backoff dan retry budget harus tersedia.

------------------------------------------------------------------------

# 30. Cost Control

Setiap inference harus dapat dicatat:

``` text
provider
model
input tokens
output tokens
estimated cost
actual cost
latency
```

Budget dapat ditetapkan pada:

``` text
system
business
division
agent
workflow
task
request
```

Jika budget habis:

``` text
downgrade
fallback
pause
Attention
terminate
```

sesuai policy.

------------------------------------------------------------------------

# 31. Token Accounting

NEXUS harus memiliki satu format token accounting walaupun provider
menghitung token secara berbeda.

Data minimal:

``` yaml
input_tokens:
output_tokens:
cached_tokens:
estimated_tokens:
provider_reported_tokens:
```

Perbedaan estimation vs actual harus dapat dicatat.

------------------------------------------------------------------------

# 32. Caching

Caching boleh digunakan hanya jika aman.

Potential cache key:

``` text
model
version
normalized_prompt
relevant_context_hash
tool_state_hash
configuration
```

Caching harus mempertimbangkan:

-   privacy
-   freshness
-   nondeterminism
-   objective sensitivity
-   business isolation.

Jangan cache response yang seharusnya selalu fresh.

------------------------------------------------------------------------

# 33. Determinism & Reproducibility

Workflow penting harus dapat menyimpan:

``` text
provider
model
version
parameters
system prompt version
context hash
tool state
routing decision
```

Tujuannya memungkinkan debugging dan reproduction.

------------------------------------------------------------------------

# 34. Model Version Policy

Model dapat:

``` text
AUTO_UPDATE
PINNED
MIN_VERSION
MAX_VERSION
ALLOWLIST
```

Production-critical workflow sebaiknya mendukung pinned model version.

------------------------------------------------------------------------

# 35. Model Lifecycle

Model registry mendukung:

``` text
DISCOVERED
AVAILABLE
ACTIVE
DEGRADED
DEPRECATED
DISABLED
REMOVED
```

Model deprecated tidak otomatis digunakan untuk workflow baru.

Existing pinned workflow dapat tetap menggunakannya jika policy
mengizinkan.

------------------------------------------------------------------------

# 36. Resource-Aware Local Routing

Untuk local inference, Router harus memperhatikan:

``` text
CPU
RAM
VRAM
GPU utilization
loaded models
queue depth
concurrent requests
disk availability
```

Jika resource lokal penuh, Router dapat:

``` text
queue
select smaller model
select alternate local runtime
fallback remote
```

sesuai policy.

------------------------------------------------------------------------

# 37. Concurrency

Router harus mendukung:

-   per-model concurrency
-   per-provider concurrency
-   per-agent concurrency
-   per-business concurrency
-   global concurrency.

Tujuan:

> Satu business yang sibuk tidak boleh membuat business lain mati hanya
> karena seluruh resource dikonsumsi.

------------------------------------------------------------------------

# 38. Multi-Business Isolation

Setiap inference request wajib membawa:

``` text
business_id
division_id
agent_id
workflow_id
task_id
```

Router harus mencegah:

``` text
Business A context
      ↓
Business B model request
```

tanpa explicit authorized cross-business operation.

Model cache, telemetry, budgets, dan context harus memiliki isolation
boundary.

------------------------------------------------------------------------

# 39. Model Selection Explainability

Setiap routing decision harus dapat menjawab:

``` text
Why this model?
Why not model X?
Which constraints were applied?
Which fallback policy was used?
What was the estimated cost?
```

Contoh:

``` text
Selected:
Local Model B

Reason:
- vision required
- local preferred
- Model A lacks vision
- remote disabled by privacy policy
- Model B healthy
```

Ini penting untuk debugging NEXUS.

------------------------------------------------------------------------

# 40. Routing Decision Record

Setiap keputusan routing disimpan:

``` yaml
request_id:
selected_provider:
selected_model:
selected_version:

candidate_models:
  - model_a
  - model_b
  - model_c

rejected_candidates:
  - model_a: missing_vision
  - model_c: privacy_restricted

policy_version:
routing_policy_version:

estimated_cost:
estimated_latency:

fallback_chain:
```

------------------------------------------------------------------------

# 41. Model Router and Objective Engine

Router tidak menentukan apakah objective layak dilakukan.

Objective Engine menentukan:

``` text
why
priority
goal
constraints
```

Model Router menentukan:

``` text
how to obtain AI inference
```

Dengan demikian:

``` text
Objective
   ↓
Workflow
   ↓
Task
   ↓
Agent
   ↓
Model Router
   ↓
Model
```

------------------------------------------------------------------------

# 42. Model Router and Attention

Attention dapat menaikkan kebutuhan kualitas model.

Contoh:

``` text
NORMAL:
fast local model

HIGH ATTENTION:
strong reasoning model

CRITICAL:
high-quality model + verification
```

Attention tidak boleh otomatis melewati governance.

------------------------------------------------------------------------

# 43. Model Router and Governance

Governance memiliki hak veto terhadap routing.

Contoh:

``` text
Agent requests remote model
        ↓
Governance:
REMOTE FORBIDDEN
        ↓
Router:
reject remote
        ↓
find compliant local model
```

------------------------------------------------------------------------

# 44. Model Router and Tool Runtime

Jika model membutuhkan tool:

``` text
Agent
 ↓
Model Router
 ↓
Model
 ↓
Tool Request
 ↓
Tool Runtime
 ↓
Result
 ↓
Model Router
 ↓
Model
```

Tool Runtime tetap menjadi security boundary.

------------------------------------------------------------------------

# 45. Failure Scenarios

### Provider unavailable

→ health update → fallback.

### Model overloaded

→ queue / alternate model.

### Context too large

→ context optimization → retry.

### Invalid structured output

→ repair/retry/fallback.

### Rate limit

→ backoff / alternate provider.

### Cost budget exceeded

→ downgrade / pause / Attention.

### Local GPU unavailable

→ alternate local model/provider.

### Remote provider blocked

→ local-only routing.

### All candidates unavailable

→ task becomes blocked and Workflow Engine handles escalation/recovery.

------------------------------------------------------------------------

# 46. Security Principles

1.  Agent tidak memegang provider secret.
2.  Provider tidak mendapatkan akses business context di luar request.
3.  Routing tidak boleh bypass Governance.
4.  Remote inference harus explicit-policy controlled.
5.  Cross-business context dilarang secara default.
6.  Model output dianggap untrusted data.
7.  Model tidak boleh mengubah routing policy sendiri.
8.  Model tidak boleh memberikan dirinya capability baru.
9.  Model Router tidak boleh menjadi jalur bypass Tool Runtime.
10. Semua inference harus traceable.

------------------------------------------------------------------------

# 47. Observability

Metrics minimal:

``` text
requests_total
successful_requests
failed_requests
latency
tokens
cost
fallback_count
provider_error_rate
model_error_rate
queue_depth
cache_hit_rate
context_overflow
structured_output_failure
tool_call_failure
```

Tracing:

``` text
Objective
 → Workflow
 → Task
 → Agent
 → Routing Decision
 → Provider
 → Model
 → Tool Runtime
```

------------------------------------------------------------------------

# 48. Configuration Hierarchy

Configuration dapat diwariskan:

``` text
GLOBAL
 ↓
BUSINESS
 ↓
DIVISION
 ↓
AGENT
 ↓
WORKFLOW
 ↓
TASK
 ↓
REQUEST
```

Level bawah boleh mempersempit policy, tetapi tidak boleh melanggar hard
constraint level atas.

------------------------------------------------------------------------

# 49. Conceptual API

``` text
modelRouter.route(request)
modelRouter.execute(request)

providerRegistry.register(provider)
providerRegistry.list()
providerRegistry.get(providerId)

modelRegistry.register(model)
modelRegistry.get(modelId)
modelRegistry.findByCapability(capability)

healthMonitor.status(providerId, modelId)

routingPolicy.evaluate(request, candidate)

costTracker.record(inference)
```

------------------------------------------------------------------------

# 50. Testing Requirements

Minimal test:

-   provider registration
-   model discovery
-   capability matching
-   local-first routing
-   OpenRouter fallback
-   custom provider
-   model pinning
-   context overflow
-   cost budget
-   token accounting
-   provider outage
-   model outage
-   circuit breaker
-   rate limiting
-   structured output
-   tool calling
-   multimodal routing
-   privacy restrictions
-   credential isolation
-   multi-business isolation
-   concurrent workloads
-   routing explainability
-   reproducibility.

------------------------------------------------------------------------

# 51. Acceptance Criteria

Module dianggap selesai jika:

-   [ ] agent tidak bergantung langsung pada provider
-   [ ] Ollama dapat digunakan
-   [ ] local Hugging Face-compatible model dapat digunakan
-   [ ] OpenRouter dapat digunakan
-   [ ] custom provider dapat ditambahkan
-   [ ] model dapat dipilih per agent
-   [ ] model dapat dipilih per task
-   [ ] capability matching bekerja
-   [ ] local-first policy bekerja
-   [ ] fallback bekerja
-   [ ] health monitoring bekerja
-   [ ] cost/token accounting tersedia
-   [ ] privacy routing bekerja
-   [ ] credentials terisolasi
-   [ ] model version dapat dipin
-   [ ] structured output didukung
-   [ ] tool calling terintegrasi melalui Tool Runtime
-   [ ] multi-business isolation terjamin
-   [ ] routing decision dapat diaudit
-   [ ] provider/model failure dapat dipulihkan.

------------------------------------------------------------------------

# 52. Locked Design Principle

> **NEXUS does not belong to one model or provider. Agents request
> intelligence; the Model Router decides which model and provider should
> supply it. Routing is capability-aware, policy-aware, objective-aware,
> resource-aware, privacy-aware, and budget-aware. Local models are
> preferred by default, while OpenRouter and custom providers remain
> first-class alternatives.**

------------------------------------------------------------------------

# 53. Existing Boundaries and CONTRACTS Next Layer

Architecture remains locked; no new modules from this cleanup.

-   [Memory & Context Intelligence](NEXUS-MEMORY-CONTEXT-INTELLIGENCE.md) owns memory, context, retrieval, knowledge, short-term state, long-term memory, business/agent/objective memory, context compression, forgetting/retention.
-   [Knowledge & Information Ingestion](NEXUS_KNOWLEDGE_INFORMATION_INGESTION.md) owns source-based extraction and knowledge formation; Memory owns durable admission and context assembly.
-   [Agent Runtime](NEXUS-AGENT-RUNTIME-LIFECYCLE.md), [Tool Runtime](NEXUS-TOOL-RUNTIME-CAPABILITY.md), [API Gateway](NEXUS-API-INTEGRATION-GATEWAY.md), [Workflow](WORKFLOW_ORCHESTRATION_ENGINE.md), [Governance](NEXUS-GOVERNANCE-POLICY-SAFETY-CONTROL.md), [Identity](NEXUS-IDENTITY-ACCESS-TRUST-SYSTEM.md), [Security](NEXUS_SECURITY_THREAT_DEFENSE.md), [Observability](NEXUS_OBSERVABILITY_AUDIT_TELEMETRY.md), [Configuration](NEXUS_CONFIGURATION_CONTROL_PLANE.md) boundaries unchanged.

Next layer is **CONTRACTS**: detailed schemas, state machines, request/result/error contracts, policy/credential interfaces, adapter protocols, queue/reconciliation handoffs, and test implementations. Field lists, API names, and state-machine sketches in this document are nonbinding contract candidates; normative boundary requirements remain in effect.
