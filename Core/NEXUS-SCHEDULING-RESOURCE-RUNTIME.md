# NEXUS --- Scheduling & Resource Runtime

**Status:** PROPOSED\
**Module:** 24/7 Scheduling, Queue & Resource Control Plane\
**Depends on:** Event Trigger, Workflow Orchestration, Agent Runtime,
Tool Runtime, Model Router, Governance, Identity, Persistence, Attention

------------------------------------------------------------------------

## 1. Purpose

Scheduling & Resource Runtime memastikan NEXUS dapat menjalankan banyak
workflow, agent, business, tool, dan model secara **24/7** tanpa
resource contention yang tidak terkendali.

Tujuan utamanya:

``` text
WHEN should something run?
WHO should run it?
WHERE should it run?
HOW MUCH resource may it consume?
WHAT gets priority?
WHAT happens when resources are unavailable?
```

------------------------------------------------------------------------

# 2. Core Principle

> **NEXUS must continuously schedule autonomous work while respecting
> priority, deadlines, business isolation, governance, resource budgets,
> concurrency limits, and system health. No single agent, workflow,
> business, model, or tool may monopolize the runtime.**

------------------------------------------------------------------------

# 3. Runtime Architecture

``` text
EVENT / SCHEDULE / WORKFLOW
            ↓
       JOB CREATION
            ↓
       PRIORITY QUEUE
            ↓
      SCHEDULER ENGINE
            ↓
   RESOURCE AVAILABILITY
            ↓
    POLICY / BUDGET CHECK
            ↓
       WORKER POOL
            ↓
 AGENT / MODEL / TOOL EXECUTION
            ↓
       STATE + METRICS
```

------------------------------------------------------------------------

# 4. Scheduling Objects

Minimal objects:

``` text
Schedule
Job
Queue Item
Execution Slot
Worker
Resource Pool
Lease
Priority
Budget
Deadline
```

------------------------------------------------------------------------

# 5. Job Identity

Every scheduled job should have:

``` yaml
job_id:
job_type:
business_id:
division_id:
workflow_id:
task_id:
agent_id:
priority:
created_at:
scheduled_at:
deadline:
status:
```

------------------------------------------------------------------------

# 6. Job Sources

Jobs may originate from:

``` text
event
workflow
agent
objective
scheduled task
retry
recovery
attention escalation
manual owner action
system maintenance
```

------------------------------------------------------------------------

# 7. Schedule Types

Support:

``` text
one-time
recurring
cron-like
interval
calendar-based
event-triggered
condition-triggered
deadline-triggered
dependency-triggered
```

------------------------------------------------------------------------

# 8. Durable Scheduling

Schedules must survive restart.

``` text
SCHEDULER CRASH
→ restore schedule state
→ recover due jobs
→ prevent duplicate execution
```

------------------------------------------------------------------------

# 9. Queue Architecture

Queues may be partitioned by:

``` text
priority
business
division
resource type
job type
risk
```

Example:

``` text
CRITICAL
HIGH
NORMAL
BACKGROUND
```

------------------------------------------------------------------------

# 10. Priority

Priority considers:

``` text
urgency
importance
objective relevance
risk
deadline
business impact
owner impact
resource cost
```

Attention and Governance remain authoritative for escalation and
approval decisions.

------------------------------------------------------------------------

# 11. Fair Scheduling

NEXUS must prevent starvation.

Possible policy:

``` text
priority + aging + fairness weight
```

A low-priority job can gradually gain scheduling eligibility without
overtaking critical work.

------------------------------------------------------------------------

# 12. Multi-Business Fairness

Example:

``` text
Business A has 1000 jobs
Business B has 10 jobs
```

Business A must not automatically consume 100% of the runtime.

Resource allocation can use:

``` text
business quotas
weights
reserved capacity
priority
fair-share scheduling
```

------------------------------------------------------------------------

# 13. Division Fairness

The same principle applies inside a business:

``` text
Media
Business
Research
Custom divisions
```

One division must not starve another unless explicitly allowed by
policy.

------------------------------------------------------------------------

# 14. Concurrency Control

Limits dapat diterapkan pada:

``` text
global
business
division
agent
workflow
task type
tool
provider
model
worker
```

Example:

``` text
Business A
→ max 20 concurrent jobs

Agent X
→ max 3 concurrent tasks
```

------------------------------------------------------------------------

# 15. Resource Types

NEXUS dapat mengelola:

``` text
CPU
RAM
GPU
VRAM
disk
network
model inference slots
API quota
tool execution slots
browser sessions
database connections
```

------------------------------------------------------------------------

# 16. Resource Pool

Concept:

``` text
RESOURCE POOL
 ├── CPU
 ├── GPU
 ├── memory
 ├── model capacity
 └── tool capacity
```

Scheduler memilih pool yang sesuai dengan requirement job.

------------------------------------------------------------------------

# 17. Resource Requirement

Job dapat mendeklarasikan:

``` yaml
cpu:
memory:
gpu:
vram:
network:
model:
tool:
estimated_duration:
priority:
```

Scheduler tidak wajib memberikan resource melebihi requirement.

------------------------------------------------------------------------

# 18. Resource Reservation

Untuk predictable workloads:

``` text
RESERVE
→ EXECUTE
→ RELEASE
```

Reservation harus memiliki expiration agar resource tidak terkunci
selamanya.

------------------------------------------------------------------------

# 19. Resource Budget

Setiap execution dapat memiliki:

``` text
time budget
token budget
model cost budget
tool-call budget
CPU budget
memory budget
storage budget
network budget
```

Jika budget habis:

``` text
STOP
DEFER
DOWNGRADE
ESCALATE
```

sesuai policy.

------------------------------------------------------------------------

# 20. Model Capacity

Model Router melaporkan:

``` text
availability
concurrency
latency
queue depth
rate limit
cost
health
```

Scheduler dapat menunda atau mengalihkan workload.

------------------------------------------------------------------------

# 21. Local Model Priority

Karena NEXUS local-first:

``` text
local model available
→ prefer local

local unavailable / unsuitable
→ allowed fallback
```

Fallback tetap tunduk pada Model Router + Governance.

------------------------------------------------------------------------

# 22. Provider Capacity

Provider dapat memiliki:

``` text
rate limit
concurrency limit
quota
cost limit
health state
```

Scheduler harus menghindari overload provider.

------------------------------------------------------------------------

# 23. Tool Capacity

Tools juga memiliki limits:

``` text
max concurrent executions
rate limit
session limit
API quota
cost
```

Tool Runtime tetap menjadi execution boundary.

------------------------------------------------------------------------

# 24. Worker Pool

Worker dapat dikelompokkan:

``` text
general worker
CPU worker
GPU worker
browser worker
research worker
media worker
local-model worker
```

Scheduler memilih worker berdasarkan capability.

------------------------------------------------------------------------

# 25. Worker Lifecycle

``` text
STARTING
→ READY
→ BUSY
→ IDLE
→ DRAINING
→ STOPPED
```

Failure:

``` text
BUSY
→ FAILED
→ RECOVERING
```

------------------------------------------------------------------------

# 26. Worker Health

Health signals:

``` text
heartbeat
CPU
RAM
GPU
queue latency
error rate
execution failures
provider connectivity
```

Unhealthy worker tidak menerima job baru.

------------------------------------------------------------------------

# 27. Lease

Job execution dapat memperoleh lease:

``` text
job
→ leased to worker
→ heartbeat
→ completed
```

Jika lease expired:

``` text
recovery
→ reconcile
→ requeue or mark unknown
```

------------------------------------------------------------------------

# 28. Backpressure

Jika workload lebih besar dari capacity:

``` text
QUEUE
→ BACKPRESSURE
→ DEFER
→ THROTTLE
```

NEXUS tidak boleh terus menerima pekerjaan tanpa batas.

------------------------------------------------------------------------

# 29. Queue Limits

Queue dapat memiliki:

``` text
max depth
max age
max memory
max jobs/business
```

Overflow policy:

``` text
reject
defer
compress
aggregate
escalate
```

------------------------------------------------------------------------

# 30. Event Storm Protection

Jika event menghasilkan ribuan jobs:

``` text
event storm
→ aggregate
→ deduplicate
→ rate limit
→ prioritize
→ schedule
```

Scheduler tidak boleh langsung mengeksekusi semuanya.

------------------------------------------------------------------------

# 31. Job Deduplication

Job dapat memiliki:

``` text
idempotency_key
deduplication_key
correlation_id
```

Duplicate scheduling harus terdeteksi.

------------------------------------------------------------------------

# 32. Dependency-Aware Scheduling

Jika:

``` text
Task B depends on Task A
```

scheduler tidak boleh menjalankan B sebelum dependency terpenuhi.

``` text
A COMPLETE
→ B ELIGIBLE
→ SCHEDULE
```

------------------------------------------------------------------------

# 33. Parallel Scheduling

Independent tasks dapat berjalan paralel:

``` text
        Workflow
        /      \
      Task A  Task B
        \      /
          Task C
```

Scheduler menentukan parallelism berdasarkan resource dan policy.

------------------------------------------------------------------------

# 34. Deadline Scheduling

Job dengan deadline dekat dapat memperoleh priority boost.

Tetapi deadline tidak boleh mengoverride:

``` text
Governance DENY
security restrictions
hard resource limits
approval requirements
```

------------------------------------------------------------------------

# 35. Deadline Miss

Jika deadline tidak mungkin tercapai:

``` text
detect
→ recalculate
→ replan
→ notify/attention if material
```

Jangan berpura-pura job masih on-track.

------------------------------------------------------------------------

# 36. Preemption

High-priority jobs dapat melakukan preemption jika aman.

Preemption policy harus membedakan:

``` text
pauseable
checkpointable
non-interruptible
irreversible
```

External side effects yang sedang berlangsung tidak boleh diputus secara
sembarangan.

------------------------------------------------------------------------

# 37. Graceful Drain

Saat shutdown:

``` text
STOP ACCEPTING NEW JOBS
→ finish safe jobs
→ checkpoint running jobs
→ release leases
→ persist state
→ shutdown workers
```

------------------------------------------------------------------------

# 38. Emergency Pause

Integrasi Governance:

``` text
GLOBAL PAUSE
BUSINESS PAUSE
DIVISION PAUSE
AGENT PAUSE
WORKFLOW PAUSE
TOOL FREEZE
PROVIDER FREEZE
```

Scheduler wajib menghormati pause state.

------------------------------------------------------------------------

# 39. Resume

Setelah pause:

``` text
validate policy
→ validate resource
→ restore queue
→ reschedule eligible jobs
```

Tidak semua job harus otomatis resume.

------------------------------------------------------------------------

# 40. Maintenance Windows

Scheduler mendukung:

``` text
maintenance window
resource maintenance
model update
worker restart
database maintenance
```

Workload dapat dipindahkan atau ditunda.

------------------------------------------------------------------------

# 41. Business Resource Policy

Setiap business dapat memiliki:

``` text
max concurrency
priority weight
resource quota
reserved capacity
allowed schedules
```

Business configuration tidak boleh melewati global governance.

------------------------------------------------------------------------

# 42. Agent Resource Policy

Agent dapat memiliki:

``` text
max concurrency
token budget
spawn budget
tool-call budget
runtime budget
```

Ini melengkapi Agent Runtime governance.

------------------------------------------------------------------------

# 43. Temporary Agent Scheduling

Temporary agent harus memiliki:

``` text
TTL
max runtime
max tasks
resource budget
```

Scheduler wajib menghentikan agent ketika termination condition
terpenuhi.

------------------------------------------------------------------------

# 44. Spawn Storm Protection

Jika agent terus membuat child agents:

``` text
spawn rate limit
depth limit
descendant limit
resource limit
objective relevance
```

Scheduler dapat menolak scheduling child baru.

------------------------------------------------------------------------

# 45. Objective-Aware Scheduling

Scheduling mempertimbangkan:

``` text
objective priority
objective deadline
objective relevance
objective resource budget
```

Tetapi objective tidak boleh bypass governance.

------------------------------------------------------------------------

# 46. Attention-Aware Scheduling

Critical attention dapat meningkatkan scheduling priority untuk
remediation.

Contoh:

``` text
critical failure
→ attention
→ remediation workflow
→ priority scheduling
```

------------------------------------------------------------------------

# 47. Owner Workload Protection

Owner-facing tasks harus mempertimbangkan:

``` text
notification budget
approval queue
attention budget
quiet mode
```

NEXUS tidak boleh menghasilkan endless approval requests.

------------------------------------------------------------------------

# 48. Approval Queue

Approval-required jobs masuk:

``` text
WAITING_APPROVAL
```

Resource tidak boleh terus dikonsumsi tanpa batas.

Approval timeout:

``` text
EXPIRE
→ CANCEL / REPLAN / ESCALATE
```

------------------------------------------------------------------------

# 49. Cost-Aware Scheduling

Scheduler dapat mempertimbangkan:

``` text
API cost
model cost
GPU cost
tool cost
expected value
deadline
```

Jika budget tidak mencukupi:

``` text
choose cheaper route
defer
request approval
```

------------------------------------------------------------------------

# 50. Latency-Aware Scheduling

Untuk time-sensitive jobs:

``` text
queue latency
model latency
tool latency
network latency
```

dapat mempengaruhi worker/provider selection.

------------------------------------------------------------------------

# 51. Energy-Aware Scheduling

Jika deployment menggunakan resource terbatas:

``` text
GPU availability
thermal state
power constraints
```

dapat menjadi scheduler signal.

Ini optional dan deployment-dependent.

------------------------------------------------------------------------

# 52. Retry Scheduling

Retry harus mempertimbangkan:

``` text
backoff
attempt count
error type
deadline
resource availability
idempotency
```

Jangan melakukan tight retry loop.

------------------------------------------------------------------------

# 53. Retry Storm Protection

Jika provider/tool gagal massal:

``` text
circuit breaker
exponential backoff
queue throttling
fallback
```

Scheduler harus mencegah retry storm.

------------------------------------------------------------------------

# 54. Circuit Breaker

Provider/resource state:

``` text
HEALTHY
→ DEGRADED
→ OPEN
→ HALF_OPEN
→ HEALTHY
```

Scheduler menghindari resource yang sedang OPEN.

------------------------------------------------------------------------

# 55. Starvation Detection

Monitor:

``` text
queue age
waiting time
resource denial
priority
business
```

Jika job terlalu lama menunggu:

``` text
aging
re-prioritize
attention
```

------------------------------------------------------------------------

# 56. Scheduling Decision Trace

Setiap keputusan penting harus dapat menjawab:

``` text
WHY scheduled?
WHY delayed?
WHY rejected?
WHY this worker?
WHY this model?
WHY this priority?
```

Trace terhubung ke audit.

------------------------------------------------------------------------

# 57. Observability

Metrics:

``` text
queue depth
queue latency
job throughput
job failure rate
worker utilization
CPU utilization
GPU utilization
memory utilization
model utilization
tool utilization
deadline miss rate
retry rate
preemption rate
```

------------------------------------------------------------------------

# 58. Persistence Integration

Scheduler state harus durable:

``` text
jobs
queues
leases
schedules
resource reservations
priority
retry state
```

Restart harus memungkinkan recovery.

------------------------------------------------------------------------

# 59. Identity Integration

Setiap job execution harus memiliki:

``` text
actor identity
business scope
division scope
workflow
task
delegation context
```

------------------------------------------------------------------------

# 60. Governance Integration

Sebelum scheduling high-risk work:

``` text
identity
→ scope
→ policy
→ risk
→ budget
→ approval
→ schedule
```

Scheduler tidak boleh menjadi bypass Governance.

------------------------------------------------------------------------

# 61. Security Requirements

Scheduler harus mencegah:

``` text
priority spoofing
queue injection
cross-business scheduling
resource exhaustion
lease theft
worker impersonation
unauthorized job cancellation
unauthorized resource reservation
```

------------------------------------------------------------------------

# 62. Testing Requirements

Wajib diuji:

-   24/7 continuous operation
-   scheduler restart
-   worker crash
-   queue recovery
-   duplicate jobs
-   event storm
-   retry storm
-   starvation
-   deadline pressure
-   resource exhaustion
-   GPU exhaustion
-   provider rate limit
-   tool rate limit
-   business fairness
-   division fairness
-   priority enforcement
-   preemption
-   checkpoint recovery
-   lease expiration
-   emergency pause
-   resume
-   unauthorized scheduling
-   cross-business leakage
-   temporary agent TTL
-   spawn storm
-   cost budget exhaustion.

------------------------------------------------------------------------

# 63. Acceptance Criteria

-   [ ] durable scheduler tersedia
-   [ ] durable queues tersedia
-   [ ] priority scheduling tersedia
-   [ ] fair scheduling tersedia
-   [ ] multi-business fairness tersedia
-   [ ] concurrency limits tersedia
-   [ ] resource pools tersedia
-   [ ] resource reservation tersedia
-   [ ] resource budgets tersedia
-   [ ] worker pools tersedia
-   [ ] worker health tersedia
-   [ ] lease/recovery tersedia
-   [ ] backpressure tersedia
-   [ ] event storm protection tersedia
-   [ ] dependency scheduling tersedia
-   [ ] parallel execution tersedia
-   [ ] deadline scheduling tersedia
-   [ ] preemption policy tersedia
-   [ ] graceful drain tersedia
-   [ ] emergency pause tersedia
-   [ ] retry/backoff tersedia
-   [ ] circuit breaker tersedia
-   [ ] objective-aware scheduling tersedia
-   [ ] attention-aware scheduling tersedia
-   [ ] cost-aware scheduling tersedia
-   [ ] scheduling decision trace tersedia
-   [ ] observability tersedia.

------------------------------------------------------------------------

# 64. Locked Design Principle

> **NEXUS scheduling is a continuous resource-allocation system, not a
> simple cron runner. It must coordinate autonomous work across multiple
> businesses, divisions, agents, models, tools, and workers while
> preserving fairness, priority, deadlines, budgets, governance, and
> system health. When demand exceeds capacity, NEXUS must apply
> backpressure, defer, replan, or escalate rather than allowing
> uncontrolled execution.**

------------------------------------------------------------------------

# 65. Next Module

Setelah module ini di-lock:

**NEXUS_OBSERVABILITY_AUDIT_TELEMETRY.md**

Fokus:

``` text
Logs
Metrics
Traces
Audit
Decision Trace
Agent Trace
Workflow Trace
Cost Tracking
Health Monitoring
Anomaly Detection
Dashboards
Alerts
Forensics
```
