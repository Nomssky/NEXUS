# NEXUS --- Attention & Priority Intelligence System

**Status:** PROPOSED\
**Module:** Core Intelligence\
**Depends on:** Event Trigger System, Objective Engine, Memory & Context
Intelligence, Workflow & Orchestration, Agent Runtime\
**Next:** Governance, Policy & Safety Control System

------------------------------------------------------------------------

## 1. Purpose

NEXUS Attention adalah sistem yang menentukan **apa yang layak
diperhatikan NEXUS, seberapa penting, kapan harus bertindak, kapan harus
memberi tahu owner, dan kapan harus tetap diam**.

Attention bukan sekadar notification system.

> Attention adalah mekanisme pengalokasian fokus NEXUS terhadap keadaan
> yang memiliki nilai, urgensi, risiko, atau dampak terhadap objective.

NEXUS harus mampu beroperasi 24/7 tanpa membanjiri owner dengan
notifikasi.

------------------------------------------------------------------------

# 2. Core Principle

``` text
EVENT ≠ ATTENTION
```

Tidak semua event membutuhkan attention.

Flow:

``` text
EVENT
 ↓
RELEVANCE
 ↓
PRIORITY
 ↓
ATTENTION DECISION
 ↓
IGNORE / RECORD / MONITOR / ACT / ESCALATE
```

------------------------------------------------------------------------

# 3. Architectural Position

``` text
              EVENT TRIGGER SYSTEM
                       │
                       ▼
               ATTENTION ENGINE
                       │
       ┌───────────────┼───────────────┐
       ▼               ▼               ▼
   Priority        Attention         Escalation
   Engine          State              Engine
       │               │               │
       └───────────────┼───────────────┘
                       ▼
                NEXUS CORE
                       │
          ┌────────────┼────────────┐
          ▼            ▼            ▼
       Agent        Workflow      Owner
```

Attention menjadi jembatan antara autonomous operation dan human
awareness.

------------------------------------------------------------------------

# 4. Attention Outcomes

Setiap candidate attention harus berakhir pada salah satu:

``` text
IGNORE
RECORD
MONITOR
QUEUE
ACT_AUTONOMOUSLY
ESCALATE_TO_AGENT
ESCALATE_TO_OWNER
PAUSE
EMERGENCY
```

Attention tidak identik dengan interrupt.

------------------------------------------------------------------------

# 5. Attention Levels

Level:

``` text
0 — IGNORE
1 — BACKGROUND
2 — NORMAL
3 — IMPORTANT
4 — HIGH
5 — CRITICAL
6 — EMERGENCY
```

Semakin tinggi level, semakin kecil tolerance terhadap delay.

------------------------------------------------------------------------

# 6. Priority Dimensions

Priority tidak hanya berdasarkan urgency.

Minimal mempertimbangkan:

``` text
urgency
importance
objective impact
business impact
financial impact
risk
reversibility
confidence
deadline proximity
dependency impact
scope
owner relevance
```

------------------------------------------------------------------------

# 7. Urgency vs Importance

### Urgency

Seberapa cepat harus ditangani.

### Importance

Seberapa besar dampaknya.

Contoh:

``` text
URGENT + LOW IMPORTANCE
→ jangan otomatis interrupt owner

LOW URGENCY + HIGH IMPORTANCE
→ schedule / plan / monitor

HIGH URGENCY + HIGH IMPORTANCE
→ immediate attention
```

------------------------------------------------------------------------

# 8. Objective Impact

Objective Engine memberikan konteks:

``` text
objective_id
goal
why
priority
deadline
success criteria
```

Attention harus mengetahui:

> Jika masalah ini diabaikan, seberapa besar kemungkinan objective
> gagal?

------------------------------------------------------------------------

# 9. Attention Score

Conceptual score:

``` text
attention_score =
    urgency
  + objective_impact
  + business_impact
  + risk
  + deadline_pressure
  + dependency_impact
  + owner_relevance
  + irreversibility
  - confidence_penalty
  - noise_penalty
```

Score digunakan untuk ranking.

Hard escalation rules dapat melewati scoring.

------------------------------------------------------------------------

# 10. Hard Attention Rules

Contoh kondisi yang dapat langsung menaikkan attention:

``` text
critical security event
irreversible high-risk action
major business failure
objective deadline imminent
critical workflow blocked
cross-business isolation violation
system corruption
governance violation
emergency condition
```

Namun hard rule tetap tunduk pada governance.

------------------------------------------------------------------------

# 11. Attention State

Setiap attention item memiliki lifecycle:

``` text
DETECTED
 ↓
EVALUATING
 ↓
RANKED
 ↓
QUEUED
 ↓
ACKNOWLEDGED
 ↓
IN_PROGRESS
 ↓
RESOLVED
```

Alternative:

``` text
DETECTED
 ↓
SUPPRESSED
 ↓
EXPIRED
```

------------------------------------------------------------------------

# 12. Attention Object

Conceptual structure:

``` yaml
attention_id:

source_event:
source_workflow:
source_agent:

business_id:
division_id:

objective_id:

level:
urgency:
importance:
risk:
confidence:

reason:
impact:
deadline:

recommended_action:
autonomous_action_allowed:

owner_notification:
escalation_policy:

created_at:
updated_at:
expires_at:
```

------------------------------------------------------------------------

# 13. Attention Reason

NEXUS harus mampu menjelaskan:

``` text
Why am I paying attention to this?
```

Contoh:

``` text
HIGH ATTENTION

Reason:
Campaign objective is behind schedule by 18%.
Three dependent tasks are blocked.
Deadline is in 6 hours.
```

------------------------------------------------------------------------

# 14. Attention Budget

Owner memiliki finite attention.

NEXUS juga memiliki finite computational attention.

Karena itu:

``` text
attention budget
```

harus diperhitungkan.

Contoh:

``` text
max owner interruptions / hour
max urgent notifications / day
max concurrent high-attention workflows
```

------------------------------------------------------------------------

# 15. Noise Suppression

NEXUS harus mencegah:

``` text
duplicate alerts
repeated alerts
low-value alerts
expected events
known transient errors
resolved alerts
```

Teknik:

``` text
deduplication
cooldown
aggregation
debouncing
batching
suppression
```

Event Trigger System tetap menjadi lapisan event normalization;
Attention menentukan apakah event tersebut layak mendapat fokus.

------------------------------------------------------------------------

# 16. Alert Aggregation

Contoh:

``` text
100 failed API requests
```

Jangan kirim:

``` text
100 notifications
```

NEXUS harus mengubahnya menjadi:

``` text
HIGH ATTENTION

API provider experiencing repeated failures:
100 failures in 4 minutes.

Impact:
3 workflows affected.
```

------------------------------------------------------------------------

# 17. Escalation Levels

Contoh:

``` text
Level 1:
agent handles

Level 2:
workflow manager handles

Level 3:
NEXUS Core reviews

Level 4:
owner notification

Level 5:
emergency owner notification / system control
```

Escalation dapat terjadi jika:

``` text
timeout
failure
risk increases
deadline approaches
autonomous resolution fails
```

------------------------------------------------------------------------

# 18. Autonomous Resolution

Jika policy mengizinkan:

``` text
Attention
 ↓
Agent
 ↓
Action
 ↓
Verification
 ↓
Resolve
```

Owner tidak perlu dilibatkan untuk masalah rutin.

Contoh:

``` text
provider temporarily unavailable
→ switch approved model
→ verify
→ record
```

------------------------------------------------------------------------

# 19. Owner Escalation

Owner hanya dilibatkan jika:

-   authority berada di owner
-   policy membutuhkan approval
-   risk terlalu tinggi
-   autonomous resolution gagal
-   decision memiliki strategic consequence
-   ambiguity terlalu tinggi.

Attention harus menyertakan:

``` text
what happened
why it matters
what NEXUS tried
what options exist
what decision is needed
deadline
```

------------------------------------------------------------------------

# 20. Interrupt Policy

Owner interruption harus memiliki policy.

Contoh:

``` yaml
quiet_hours:
  enabled: true

allow:
  critical: true
  emergency: true

suppress:
  normal: true
  low: true
```

Quiet mode tidak boleh memblok emergency events.

------------------------------------------------------------------------

# 21. Attention Channels

Potential channels:

``` text
NEXUS UI
dashboard
in-app notification
mobile notification
email
messaging integration
voice
webhook
```

Channel selection ditentukan policy.

------------------------------------------------------------------------

# 22. Channel Escalation

Contoh:

``` text
HIGH
 ↓
NEXUS UI

still unresolved after 30 min
 ↓
mobile notification

still unresolved
 ↓
email / secondary channel

CRITICAL
 ↓
approved immediate channels
```

------------------------------------------------------------------------

# 23. Attention Acknowledgement

Owner/agent dapat:

``` text
ACKNOWLEDGE
DEFER
SNOOZE
ASSIGN
RESOLVE
DISMISS
ESCALATE
```

Dismiss tidak selalu berarti masalah selesai.

------------------------------------------------------------------------

# 24. Snooze

Attention dapat ditunda:

``` text
snooze_until
```

Setelah waktunya tiba:

``` text
re-evaluate
```

Jika kondisi sudah berubah, attention dapat otomatis ditutup.

------------------------------------------------------------------------

# 25. Deferred Attention

Attention yang belum perlu ditangani sekarang dapat menjadi:

``` text
deferred queue
```

Tetapi harus memiliki:

``` text
reason
deadline
wake condition
```

------------------------------------------------------------------------

# 26. Attention Decay

Attention dapat berubah seiring waktu.

Contoh:

``` text
deadline semakin dekat
→ urgency naik

issue resolved
→ urgency turun / resolved

information becomes stale
→ confidence turun
```

Attention bukan static score.

------------------------------------------------------------------------

# 27. Attention Re-evaluation

Re-evaluate ketika:

``` text
new event
objective changes
deadline changes
workflow state changes
risk changes
owner response
agent result
external state changes
```

------------------------------------------------------------------------

# 28. Priority Inversion Protection

Low-priority event tidak boleh terus mengalahkan high-priority work
hanya karena jumlahnya banyak.

NEXUS harus memiliki:

``` text
priority queues
fair scheduling
aging control
critical lane
```

------------------------------------------------------------------------

# 29. Critical Lane

Critical attention memiliki jalur terpisah agar tidak tertahan oleh
backlog normal.

``` text
NORMAL QUEUE
HIGH QUEUE
CRITICAL QUEUE
EMERGENCY QUEUE
```

------------------------------------------------------------------------

# 30. Attention and Objective Engine

Objective Engine menjawab:

``` text
why
what matters
what success means
```

Attention menjawab:

``` text
what deserves focus now
```

Flow:

``` text
Objective
 ↓
Impact Evaluation
 ↓
Attention Priority
```

------------------------------------------------------------------------

# 31. Attention and Workflow

Workflow dapat menghasilkan attention ketika:

``` text
blocked
failed
deadline risk
unexpected result
dependency unavailable
verification failed
```

Attention dapat kemudian:

``` text
replan workflow
restart task
change model
spawn specialist
ask owner
```

------------------------------------------------------------------------

# 32. Attention and Agent Runtime

Agent health dapat menghasilkan attention:

``` text
agent crashed
agent stuck
agent loop detected
budget exceeded
authority conflict
unexpected behavior
```

Attention menentukan severity dan escalation.

------------------------------------------------------------------------

# 33. Attention and Memory

Attention dapat menggunakan historical memory:

``` text
"Last time this occurred, campaign failed after 2 hours."
```

Memory meningkatkan context, tetapi tidak boleh menciptakan false
urgency.

------------------------------------------------------------------------

# 34. Attention and Model Router

Attention level dapat memengaruhi model selection.

``` text
NORMAL
→ fast local model

HIGH
→ stronger reasoning model

CRITICAL
→ strongest approved model + verification
```

Model upgrade tetap dibatasi budget dan governance.

------------------------------------------------------------------------

# 35. Attention and Event Trigger System

Event Trigger System:

``` text
collects
normalizes
deduplicates
routes events
```

Attention:

``` text
determines focus
priority
escalation
interruption
```

Boundary harus jelas agar kedua sistem tidak tumpang tindih.

------------------------------------------------------------------------

# 36. Cross-Business Attention

Satu NEXUS dapat menangani banyak business.

Attention queue harus mendukung:

``` text
global
business
division
```

Contoh:

``` text
Business A → HIGH
Business B → NORMAL
Business C → CRITICAL
```

Critical Business C tidak boleh tertutup oleh aktivitas Business A/B.

------------------------------------------------------------------------

# 37. Attention Fairness

Selain priority, sistem harus mempertimbangkan fairness.

Contoh:

``` text
Business A tidak boleh terus mengambil seluruh compute/attention.
```

Fairness dapat menggunakan:

``` text
quotas
weights
aging
business priority
objective priority
```

Critical events tetap dapat bypass fairness queue.

------------------------------------------------------------------------

# 38. Attention Deduplication

Deduplicate berdasarkan:

``` text
event identity
workflow
root cause
business
time window
```

Contoh:

``` text
20 dependent failures
```

dapat menjadi:

``` text
1 root-cause attention
```

------------------------------------------------------------------------

# 39. Root Cause Grouping

Jika banyak events berasal dari satu masalah:

``` text
Provider outage
 ↓
50 workflow failures
 ↓
1 root-cause attention
```

Sub-events tetap disimpan untuk audit.

------------------------------------------------------------------------

# 40. Attention Suppression

Suppression dapat dilakukan ketika:

``` text
known maintenance
expected transient state
owner explicitly snoozed
issue already being handled
duplicate root cause
```

Suppression harus memiliki reason dan expiry.

------------------------------------------------------------------------

# 41. Emergency Control

Emergency attention dapat memicu:

``` text
pause workflow
disable agent
disable provider
freeze tool
isolate business
notify owner
```

Emergency control harus melalui Governance/Control Plane.

Attention sendiri tidak boleh menjadi unrestricted admin layer.

------------------------------------------------------------------------

# 42. False Positive Protection

High attention harus memiliki confidence threshold jika tidak termasuk
hard rule.

Jika confidence rendah:

``` text
monitor
gather evidence
retrieve memory
ask specialist
```

sebelum menginterupsi owner.

------------------------------------------------------------------------

# 43. False Negative Protection

Untuk critical conditions, threshold harus rendah enough agar kondisi
berbahaya tidak terlewat.

Contoh:

``` text
security breach suspicion
```

lebih baik masuk review queue daripada diabaikan sepenuhnya.

------------------------------------------------------------------------

# 44. Owner Preference

Owner dapat menentukan:

``` text
preferred channels
quiet periods
business priorities
notification limits
escalation preferences
```

Namun owner preference tidak boleh menonaktifkan mandatory
safety/emergency governance.

------------------------------------------------------------------------

# 45. Attention Digest

Low/normal attention dapat digabung menjadi digest.

Contoh:

``` text
Daily NEXUS Digest

12 background events
4 completed workflows
2 warnings
1 important decision
```

Tujuannya mengurangi interruption.

------------------------------------------------------------------------

# 46. Decision Pack

Untuk attention yang membutuhkan owner decision, NEXUS membuat compact
decision pack:

``` text
Situation
Impact
Evidence
What NEXUS tried
Options
Recommendation
Risk
Deadline
Decision required
```

Owner tidak perlu membaca seluruh execution history.

------------------------------------------------------------------------

# 47. Attention History

Semua attention penting disimpan:

``` text
detected_at
acknowledged_at
action_started_at
resolved_at
escalated_at
resolution
actor
```

Digunakan untuk:

-   audit
-   analytics
-   improving policies
-   learning recurring patterns.

------------------------------------------------------------------------

# 48. Attention Analytics

Metrics:

``` text
attention_total
attention_by_level
owner_interruptions
false_positive_rate
false_negative_incidents
mean_time_to_acknowledge
mean_time_to_resolve
escalation_rate
suppression_rate
autonomous_resolution_rate
notification_rate
```

------------------------------------------------------------------------

# 49. Learning from Attention

NEXUS dapat menemukan pattern:

``` text
same warning
→ repeatedly harmless
```

atau:

``` text
same warning
→ repeatedly precedes major failure
```

Pattern tersebut dapat menjadi input untuk policy improvement.

Automatic policy changes tetap harus melalui governance.

------------------------------------------------------------------------

# 50. Security Principles

1.  Attention tidak boleh bypass authorization.
2.  Attention tidak boleh memberikan agent authority baru.
3.  External content tidak otomatis menjadi trusted emergency
    instruction.
4.  Critical classification harus memiliki provenance.
5.  Cross-business escalation harus scope-safe.
6.  Owner notification tidak boleh membocorkan business lain.
7.  Emergency controls harus audit-able.
8.  Model tidak boleh menaikkan attention level untuk memanipulasi
    owner.

------------------------------------------------------------------------

# 51. Conceptual API

``` text
attention.detect(candidate)
attention.evaluate(candidate)
attention.score(candidate)
attention.create(candidate)
attention.acknowledge(attention_id)
attention.defer(attention_id, until)
attention.snooze(attention_id, until)
attention.escalate(attention_id)
attention.resolve(attention_id)
attention.dismiss(attention_id)

attention.get_queue(scope)
attention.get_digest(scope, period)
attention.build_decision_pack(attention_id)
```

------------------------------------------------------------------------

# 52. Testing Requirements

Minimal test:

-   priority scoring
-   urgency vs importance
-   objective impact
-   hard critical rules
-   deduplication
-   aggregation
-   suppression
-   cooldown
-   attention decay
-   re-evaluation
-   escalation
-   autonomous resolution
-   owner notification
-   quiet hours
-   emergency bypass
-   fairness
-   cross-business isolation
-   root-cause grouping
-   false-positive handling
-   false-negative protection
-   decision pack
-   digest
-   audit
-   recovery after restart.

------------------------------------------------------------------------

# 53. Acceptance Criteria

-   [ ] event tidak otomatis menjadi attention
-   [ ] attention memiliki lifecycle
-   [ ] urgency dan importance dibedakan
-   [ ] objective impact diperhitungkan
-   [ ] critical lane tersedia
-   [ ] attention budget tersedia
-   [ ] noise suppression tersedia
-   [ ] aggregation tersedia
-   [ ] root-cause grouping tersedia
-   [ ] autonomous resolution tersedia
-   [ ] escalation tersedia
-   [ ] owner interruption dikontrol
-   [ ] quiet mode tersedia
-   [ ] emergency event tetap dapat menembus quiet mode
-   [ ] attention dapat berubah berdasarkan waktu/state
-   [ ] multi-business attention terisolasi
-   [ ] fairness tersedia
-   [ ] decision pack tersedia
-   [ ] digest tersedia
-   [ ] audit dan analytics tersedia
-   [ ] Attention tidak dapat bypass Governance.

------------------------------------------------------------------------

# 54. Locked Design Principle

> **NEXUS does not interrupt the owner because something happened; it
> interrupts only when something deserves attention. Attention is
> continuously evaluated against urgency, importance, objective impact,
> risk, confidence, deadlines, and governance. NEXUS should autonomously
> resolve routine issues, aggregate noise, preserve critical signals,
> and escalate to the owner only when human awareness or authority is
> genuinely required.**

------------------------------------------------------------------------

# 55. Next Module

Setelah module ini di-lock:

**GOVERNANCE_POLICY_SAFETY_CONTROL.md**

Fokus:

``` text
Governance
Policy Engine
Permission
Authority
Approval
Risk
Safety
Human control
Emergency controls
Agent boundaries
Business isolation
Audit
Compliance
```
