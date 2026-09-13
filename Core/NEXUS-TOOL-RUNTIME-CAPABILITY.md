# NEXUS --- Tool Runtime & Capability Execution System

**Status:** PROPOSED\
**Module:** Tool Runtime & Capability Execution\
**Parent:** NEXUS Core / Agent Runtime\
**Purpose:** Menjadi security-controlled execution boundary antara
autonomous agents dan tools/external systems.

------------------------------------------------------------------------

## 1. Core Principle

> **Agent tidak pernah berinteraksi langsung dengan external capability.
> Semua tool execution harus melewati Tool Runtime, policy,
> authorization, scope validation, credential isolation, execution
> limits, dan audit.**

Tool Runtime adalah execution layer, bukan tempat agent mengambil
keputusan governance.

Canonical flow:

``` text
AGENT
  ↓
TOOL REQUEST
  ↓
VALIDATE
  ↓
AUTHORIZE
  ↓
SCOPE CHECK
  ↓
POLICY CHECK
  ↓
RESOURCE / BUDGET CHECK
  ↓
CREDENTIAL RESOLUTION
  ↓
SANDBOX / EXECUTION
  ↓
RESULT VALIDATION
  ↓
AUDIT
  ↓
AGENT
```

------------------------------------------------------------------------

# 2. Tool Definition

Setiap tool harus memiliki definition versioned.

``` yaml
tool_id:
name:
version:
description:
category:
input_schema:
output_schema:
required_capabilities:
required_permissions:
supported_scopes:
risk_level:
execution_mode:
timeout:
rate_limit:
cost_policy:
credential_policy:
sandbox_policy:
approval_policy:
```

Tool definition tidak boleh memberikan authority lebih besar daripada
governance.

------------------------------------------------------------------------

# 3. Tool Categories

Minimal:

``` text
READ
WRITE
EXECUTE
COMMUNICATE
BROWSER
WEB
FILE
DATABASE
CODE
SYSTEM
EXTERNAL_API
```

Contoh:

``` text
web_search
file_read
file_write
database_query
browser_navigate
api_request
email_send
social_media_publish
code_execute
image_generate
```

------------------------------------------------------------------------

# 4. Capability vs Tool

Capability adalah kemampuan konseptual.

Tool adalah implementasi konkret.

Contoh:

``` text
Capability:
web_search

Tools:
search_provider_a
search_provider_b
local_search
```

Agent meminta capability; Tool Runtime memilih implementation yang
diizinkan.

------------------------------------------------------------------------

# 5. Tool Request

Canonical request:

``` yaml
request_id:
agent_id:
runtime_id:
task_id:
workflow_id:
business_id:
division_id:
tool_id:
action:
input:
requested_at:
reason:
```

`reason` harus dapat dikaitkan dengan task/objective bila diperlukan.

------------------------------------------------------------------------

# 6. Validation

Tool Runtime melakukan:

-   schema validation
-   input type validation
-   required field validation
-   payload size validation
-   scope validation
-   dangerous input detection
-   policy validation
-   budget validation

Invalid request:

``` text
REJECTED
```

Tidak boleh diteruskan ke tool.

------------------------------------------------------------------------

# 7. Authorization

Authorization harus memeriksa:

``` text
Agent
+
Capability
+
Permission
+
Business Scope
+
Division Scope
+
Resource Scope
+
Task Scope
+
Governance
```

Contoh:

``` text
Agent:
Content Agent

Capability:
social_media_publish

Permission:
WRITE

Business:
Clothing

Division:
Media

Result:
ALLOW
```

Jika salah satu boundary tidak terpenuhi:

``` text
DENY
```

------------------------------------------------------------------------

# 8. Risk Levels

Setiap tool memiliki risk level.

``` text
LOW
MEDIUM
HIGH
CRITICAL
```

Contoh:

``` text
file_read            → LOW
web_search            → LOW
database_write        → MEDIUM
email_send            → MEDIUM
social_publish        → HIGH
financial_transaction → CRITICAL
system_command        → CRITICAL
```

Risk level menentukan approval, sandbox, logging, dan execution policy.

------------------------------------------------------------------------

# 9. Approval Gates

High-risk tools dapat membutuhkan approval.

Flow:

``` text
Agent
 ↓
Tool Request
 ↓
Risk Evaluation
 ↓
Approval Required?
 ├── NO → Execute
 └── YES
      ↓
Attention / Approval
      ↓
Approved?
 ├── YES → Execute
 └── NO → Reject
```

Approval harus memiliki expiry agar tidak menjadi authorization
permanen.

------------------------------------------------------------------------

# 10. Credential Isolation

Agent tidak boleh menerima raw credentials secara default.

Contoh:

``` text
Agent
 ↓
Request Tool
 ↓
Credential Broker
 ↓
Scoped Credential
 ↓
Tool
```

Agent tidak perlu mengetahui:

-   API key
-   password
-   OAuth refresh token
-   secret
-   private credential

Credential access harus:

-   scoped
-   short-lived bila memungkinkan
-   auditable
-   revocable
-   provider-specific

------------------------------------------------------------------------

# 11. Secret Handling

Secrets:

-   tidak boleh masuk prompt agent
-   tidak boleh disimpan dalam normal memory
-   tidak boleh muncul di logs
-   harus di-redact dari error
-   tidak boleh dimasukkan ke artifact tanpa policy

------------------------------------------------------------------------

# 12. Execution Modes

Tool Runtime mendukung:

### Synchronous

``` text
request → execute → result
```

Untuk operasi cepat.

### Asynchronous

``` text
request
 ↓
job queued
 ↓
worker
 ↓
result
```

Untuk operasi lama.

### Streaming

Untuk output bertahap bila dibutuhkan.

### Scheduled

Tool execution dijalankan pada waktu tertentu melalui
scheduler/workflow.

------------------------------------------------------------------------

# 13. Sandbox

Tool berisiko harus dijalankan dalam sandbox sesuai kebutuhan.

Sandbox dapat membatasi:

``` text
filesystem
network
process
CPU
RAM
runtime duration
ports
environment variables
credentials
```

Code execution harus isolated dari host system.

------------------------------------------------------------------------

# 14. Network Policy

Setiap tool dapat memiliki network policy.

Contoh:

``` yaml
network:
  mode: allowlist
  domains:
    - approved-api.example
```

Default principle:

> **Deny by default, allow by policy.**

Agent tidak boleh memperluas network access sendiri.

------------------------------------------------------------------------

# 15. Filesystem Policy

Tool file harus menggunakan scoped filesystem.

Contoh:

``` text
/workspaces/business-a/media/
```

Agent Business A tidak otomatis dapat membaca:

``` text
/workspaces/business-b/
```

Cross-business access harus melalui authorization.

------------------------------------------------------------------------

# 16. Browser Tool

Browser execution harus memiliki:

-   isolated browser context
-   session isolation
-   domain policy
-   download policy
-   upload policy
-   credential isolation
-   navigation restrictions
-   action audit

Browser content dianggap **untrusted input**.

------------------------------------------------------------------------

# 17. Web Tool

Web data dapat mengandung:

-   prompt injection
-   malicious instructions
-   misleading content
-   unsafe links
-   malicious downloads

Tool Runtime harus memperlakukan hasil web sebagai data, bukan
authority.

Canonical boundary:

``` text
WEB CONTENT
     ↓
UNTRUSTED DATA
     ↓
AGENT CONTEXT
```

Bukan:

``` text
WEB CONTENT → SYSTEM INSTRUCTION
```

------------------------------------------------------------------------

# 18. File Tool

File input juga dianggap untrusted.

File Runtime harus mendukung:

-   type detection
-   size limit
-   malware scanning integration
-   parsing isolation
-   content extraction
-   access scope
-   retention policy

File instructions tidak boleh mengubah agent authority.

------------------------------------------------------------------------

# 19. Database Tool

Database access harus menggunakan scoped credentials.

Contoh:

``` text
Research Agent
→ READ
→ Business A
→ Research DB
```

Tidak otomatis:

``` text
WRITE
DELETE
Business B
```

SQL/tool execution harus memiliki query/resource limits.

------------------------------------------------------------------------

# 20. API Tool

External API calls harus memiliki:

``` text
provider
endpoint
method
authentication
timeout
retry
rate limit
cost
response validation
```

External API failures harus diklasifikasikan.

------------------------------------------------------------------------

# 21. Rate Limiting

Tool Runtime harus memiliki limits berdasarkan:

``` text
agent
runtime
business
division
tool
provider
global system
```

Contoh:

``` yaml
max_requests_per_minute:
max_requests_per_hour:
max_concurrent_requests:
```

------------------------------------------------------------------------

# 22. Cost Control

Tool calls dapat memiliki cost.

Runtime harus dapat menghitung:

``` text
API cost
model cost
compute cost
storage cost
bandwidth cost
```

Budget dapat berada di:

``` text
Task
Workflow
Agent
Business
Global NEXUS
```

------------------------------------------------------------------------

# 23. Timeout

Semua external execution harus memiliki timeout.

``` text
REQUESTED
 ↓
EXECUTING
 ↓
TIMEOUT
```

Timeout tidak boleh otomatis berarti task berhasil.

State harus:

``` text
UNKNOWN
```

jika hasil external execution tidak dapat dipastikan.

------------------------------------------------------------------------

# 24. Retry Policy

Retry harus berdasarkan error class.

``` text
Transient → retry
Rate limit → backoff
Authentication → do not blindly retry
Validation → reject
Permission → deny
Unknown → reconcile
```

Exponential backoff dapat digunakan.

------------------------------------------------------------------------

# 25. Idempotency

Tool execution yang memiliki side effect harus mendukung idempotency
bila memungkinkan.

Contoh:

``` yaml
idempotency_key:
request_id:
```

Tujuannya mencegah:

``` text
duplicate email
duplicate publish
duplicate transaction
duplicate record
```

------------------------------------------------------------------------

# 26. Unknown External State

Jika NEXUS tidak tahu apakah action berhasil:

``` text
EXECUTING
   ↓
CONNECTION LOST
   ↓
UNKNOWN
```

NEXUS tidak boleh langsung mengulang action berisiko.

Flow:

``` text
UNKNOWN
 ↓
RECONCILE
 ↓
CONFIRMED SUCCESS
   OR
CONFIRMED FAILURE
   OR
ATTENTION
```

------------------------------------------------------------------------

# 27. Result Validation

Tool result harus divalidasi.

``` text
Raw Result
 ↓
Schema Validation
 ↓
Security Sanitization
 ↓
Secret Redaction
 ↓
Normalization
 ↓
Agent Result
```

Tool tidak boleh memasukkan arbitrary system-level instructions ke agent
context.

------------------------------------------------------------------------

# 28. Tool Output Trust

Output tool memiliki trust level.

``` text
TRUSTED SYSTEM RESULT
CONTROLLED TOOL RESULT
EXTERNAL DATA
UNTRUSTED CONTENT
```

External content tidak boleh memiliki authority lebih tinggi daripada
NEXUS policy.

------------------------------------------------------------------------

# 29. Tool Chaining

Agent/workflow dapat melakukan:

``` text
Tool A
 ↓
Result
 ↓
Tool B
 ↓
Result
 ↓
Tool C
```

Tool chaining harus tetap melewati policy checks setiap kali.

Tidak boleh ada:

``` text
A → B → C
```

yang melewati authorization karena sudah berada dalam workflow yang
sama.

------------------------------------------------------------------------

# 30. Tool Composition

Complex capability dapat dibangun dari beberapa tools.

Contoh:

``` text
Publish Campaign
 ├── file_read
 ├── content_validate
 ├── image_prepare
 ├── social_media_publish
 └── verify_publish
```

Setiap sub-tool tetap diauthorize secara individual.

------------------------------------------------------------------------

# 31. Human-in-the-Loop

Tool Runtime harus dapat menghentikan execution sebelum side effect.

Contoh:

``` text
Generate Post
 ↓
Review
 ↓
Approval
 ↓
Publish
```

Approval state harus durable.

------------------------------------------------------------------------

# 32. Autonomous Operation

Untuk operasi yang sudah di-authorize:

``` text
Event
 ↓
Workflow
 ↓
Agent
 ↓
Tool Runtime
 ↓
Execution
 ↓
Verification
```

Tidak diperlukan manusia pada setiap langkah.

Namun high-risk action tetap mengikuti governance.

------------------------------------------------------------------------

# 33. Tool Discovery

Agent tidak otomatis melihat semua tools.

Runtime menyediakan hanya tools yang:

``` text
relevant
+
authorized
+
available
+
within scope
```

Dengan demikian tool catalog dapat besar tanpa memberikan seluruh
capability ke setiap agent.

------------------------------------------------------------------------

# 34. Tool Selection

Tool selection dapat mempertimbangkan:

``` text
capability
scope
risk
availability
latency
cost
quality
provider health
task requirement
```

Jika ada beberapa implementation:

``` text
Capability
 ↓
Eligible Tools
 ↓
Policy Filter
 ↓
Health
 ↓
Cost / Latency / Quality
 ↓
Selected Tool
```

------------------------------------------------------------------------

# 35. Tool Health

Runtime memonitor:

-   availability
-   latency
-   error rate
-   rate limit state
-   provider status
-   authentication status

Tool dapat masuk:

``` text
HEALTHY
DEGRADED
UNAVAILABLE
```

------------------------------------------------------------------------

# 36. Tool Failover

Jika implementation gagal:

``` text
Tool A
 ↓
Failure
 ↓
Tool B
 ↓
Success
```

Failover hanya boleh dilakukan jika Tool B memiliki capability dan
authorization yang setara.

------------------------------------------------------------------------

# 37. Tool Versioning

Tool definition harus versioned.

``` text
tool_x v1
tool_x v2
```

Workflow yang sedang berjalan dapat dipin ke version tertentu bila
diperlukan.

------------------------------------------------------------------------

# 38. Audit

Setiap tool execution menghasilkan audit event:

``` yaml
request_id:
timestamp:
agent_id:
runtime_id:
business_id:
division_id:
workflow_id:
task_id:
tool_id:
tool_version:
action:
risk_level:
authorization_result:
approval_id:
execution_status:
duration:
cost:
result_hash:
error_class:
```

Secrets dan sensitive payload harus direduksi/redact.

------------------------------------------------------------------------

# 39. Observability

Metrics minimum:

``` text
tool_calls_total
tool_calls_success
tool_calls_failed
tool_calls_denied
tool_calls_timeout
tool_calls_unknown
tool_latency
tool_cost
rate_limit_events
approval_wait_time
```

Tracing harus menghubungkan:

``` text
Objective
→ Event
→ Workflow
→ Task
→ Agent
→ Tool Request
→ Tool Execution
→ Result
```

------------------------------------------------------------------------

# 40. Emergency Controls

NEXUS harus dapat:

``` text
disable tool
disable provider
disable capability
pause all external actions
pause business
pause division
global emergency stop
```

Emergency controls berada di governance/control plane.

Agent tidak dapat menonaktifkan emergency stop.

------------------------------------------------------------------------

# 41. Multi-Business Isolation

Tool Runtime harus memastikan:

``` text
Business A
  ↓
Authorized Tools
  ↓
Business A Resources
```

dan bukan:

``` text
Business A
  ↓
Any Available Resource
```

Credentials, filesystem, databases, browser sessions, and external
accounts harus terisolasi berdasarkan scope.

------------------------------------------------------------------------

# 42. Integration

Tool Runtime terhubung dengan:

``` text
NEXUS Core
├── Governance
├── Objective Engine
├── Attention
├── Event Trigger System
├── Workflow Engine
├── Agent Runtime
├── Tool Runtime ← CURRENT MODULE
├── Model Router
├── Memory
├── Persistence
└── Observability
```

------------------------------------------------------------------------

# 43. Security Principle

> **Every tool call is an untrusted boundary crossing until NEXUS policy
> explicitly authorizes it.**

Agent intelligence tidak menggantikan authorization.

------------------------------------------------------------------------

# 44. Testing Requirements

### Authorization

-   allowed tool
-   denied tool
-   wrong business
-   wrong division
-   insufficient permission
-   expired approval

### Security

-   prompt injection
-   secret leakage
-   path traversal
-   malicious file
-   unsafe URL
-   command injection
-   unauthorized network access

### Reliability

-   timeout
-   retry
-   rate limit
-   provider outage
-   unknown external state
-   duplicate execution

### Isolation

-   business A vs B
-   division A vs B
-   agent A vs B
-   credential isolation
-   browser session isolation
-   filesystem isolation

### Cost

-   budget exhaustion
-   rate limit
-   excessive tool chain
-   runaway execution

------------------------------------------------------------------------

# 45. Acceptance Criteria

Module dianggap selesai secara arsitektur jika:

-   semua tool execution melewati Tool Runtime
-   capability dan tool terpisah
-   authorization enforced
-   business/division scope enforced
-   credential isolation tersedia
-   sandbox tersedia
-   network policy tersedia
-   risk classification tersedia
-   approval gate tersedia
-   rate limiting tersedia
-   cost control tersedia
-   timeout tersedia
-   retry policy tersedia
-   idempotency tersedia
-   unknown-state reconciliation tersedia
-   result validation tersedia
-   prompt injection boundary tersedia
-   tool discovery terkontrol
-   tool health tersedia
-   failover tersedia
-   versioning tersedia
-   audit trail tersedia
-   observability tersedia
-   emergency controls tersedia
-   24/7 autonomous execution tetap dapat berjalan tanpa mengorbankan
    governance.

------------------------------------------------------------------------

# 46. Locked Design Principle

> **NEXUS agents may be autonomous, but tools are never trusted merely
> because an agent requested them. Every capability crosses a controlled
> Tool Runtime boundary where identity, scope, permission, risk,
> credentials, budget, execution environment, result integrity, and
> auditability are enforced.**

------------------------------------------------------------------------

# 47. Next Module

Setelah Tool Runtime dikunci, layer berikutnya:

**Model Router & Provider Abstraction System**

Fokus:

-   local-first inference
-   Ollama
-   Hugging Face/local models
-   OpenRouter
-   custom providers
-   per-agent/per-task model selection
-   fallback
-   model health
-   cost/latency/quality routing
-   context limits
-   structured output
-   model capability registry
-   provider credentials
-   privacy policy
-   inference observability
