# NEXUS — External Failure, Reconciliation & Security Contracts

**Layer:** Phase 4 — Integration, Provider & External Boundary Contracts  
**Status:** LOCKED  
**Branch:** contracts/integration-external  
**Depends on:** Phase 1 Core Interface Contracts, Phase 2 Data & Event Schemas, Phase 3 Runtime & Execution Contracts, INTEGRATION_EXTERNAL_CONTRACTS.md, PROVIDER_CONTRACTS.md  

---

## 1. Purpose

This document defines **failure handling, reconciliation, security, and resilience contracts** for NEXUS external integrations: retry ownership, circuit breaker, rate limiting, unknown outcome handling, reconciliation, external security threats, DLP, data trust boundary, and graceful integration shutdown.

---

## 2. Retry Ownership Contract

### 2.1 Retry Owner Assignment

| Operation Type | Retry Owner | Notes |
|---|---|---|
| External API call | API Gateway | Single owner |
| Webhook delivery | API Gateway | Single owner |
| Tool execution | Tool Runtime | Single owner |
| Model invocation | Model Router | Single owner |
| Database query | Database Connector | Single owner |
| Browser navigation | Browser Connector | Single owner |
| Polling cycle | Poll Manager | Single owner |
| Stream reconnect | Stream Manager | Single owner |

### 2.2 Retry Rules

| Rule | Description |
|---|---|
| Single owner | One retry owner per operation |
| No nested retry | No agent retry + tool retry + connector retry + provider retry |
| Bounded | Retry bounded by budget, deadline, max attempts |
| Observable | Every retry attempt logged and traceable |
| Classified | Only retry retryable errors |
| Unknown outcome | UNKNOWN_OUTCOME triggers reconciliation, not retry |

### 2.3 Nested Retry Prohibition

```
ALLOWED:
  Agent → Tool Runtime (retry owner) → External API

NOT ALLOWED:
  Agent (retry) → Tool Runtime (retry) → Connector (retry) → Provider (retry)
```

Nested retries that accidentally multiply attempts are prohibited.

---

## 3. Circuit Breaker Contract

### 3.1 Circuit Breaker State Machine

```
CLOSED → OPEN → HALF_OPEN → {CLOSED, OPEN}
```

### 3.2 States

| State | Description |
|---|---|
| `CLOSED` | Normal operation, requests pass through |
| `OPEN` | Failures exceeded threshold, requests rejected |
| `HALF_OPEN` | Testing if service recovered |

### 3.3 Circuit Breaker Configuration

| Field | Type | Required | Description |
|---|---|---|---|
| `failure_threshold` | integer | yes | Failures to trigger open |
| `recovery_timeout_ms` | integer | yes | Time before half-open |
| `success_threshold` | integer | yes | Successes to close from half-open |
| `window_ms` | integer | yes | Failure counting window |

### 3.4 Circuit Breaker Rules

| Rule | Description |
|---|---|
| Per-provider | Circuit breaker per provider/endpoint |
| Observable | Circuit state changes auditable |
| Degrade gracefully | Open circuit = graceful degradation |
| Recovery | Automatic recovery via half-open |

---

## 4. Rate Limiting Contract

### 4.1 Rate Limit Levels

| Level | Scope | Description |
|---|---|---|
| Provider limit | Per provider | Provider-wide limit |
| Connector limit | Per connector | Connector-specific limit |
| Endpoint limit | Per endpoint | Endpoint-specific limit |
| Business limit | Per business | Business-wide limit |
| Division limit | Per division | Division-wide limit |
| Agent limit | Per agent | Agent-specific limit |
| Workflow limit | Per workflow | Workflow-specific limit |

### 4.2 Rate Limit Record

| Field | Type | Required | Description |
|---|---|---|---|
| `limit_id` | string | yes | Unique limit identifier |
| `scope` | enum | yes | Limit scope (see above) |
| `scope_id` | string | yes | Scope identifier |
| `max_requests` | integer | yes | Maximum requests per window |
| `window_seconds` | integer | yes | Time window |
| `current_usage` | integer | yes | Current usage |
| `retry_after_seconds` | integer | no | When to retry |
| `burst_limit` | integer | no | Burst capacity |

### 4.3 Rate Limit Rules

| Rule | Description |
|---|---|
| No governance bypass | Rate limiting MUST NOT bypass Governance |
| Respect headers | Respect Retry-After headers |
| Backoff | Exponential backoff on rate limit |
| Observable | Rate limit events auditable |
| Per-scope | Rate limits per business/division |

---

## 5. Unknown External Outcome Contract

### 5.1 Unknown Outcome Triggers

| Trigger | Description |
|---|---|
| Timeout after send | Network timeout after request may have been sent |
| Partial response | Response received but incomplete |
| Connection reset | Connection reset after request sent |
| Provider crash | Provider crashed during processing |
| Ambiguous status | Status code does not clearly indicate result |

### 5.2 Unknown Outcome Record

| Field | Type | Required | Description |
|---|---|---|---|
| `outcome_id` | string | yes | Unique outcome identifier |
| `operation_type` | string | yes | What operation |
| `external_request_id` | string | yes | External request ID |
| `last_known_state` | string | yes | Last known state |
| `trigger_cause` | string | yes | What caused unknown state |
| `side_effect_class` | enum | yes | Side-effect classification |
| `reconciliation_strategy` | enum | yes | How to reconcile |
| `created_at` | datetime | yes | When detected |
| `resolved_at` | datetime | no | When resolved |
| `resolution` | string | no | How it was resolved |

### 5.3 Unknown Outcome Rules

| Rule | Description |
|---|---|
| Not failure | UNKNOWN_OUTCOME is NOT FAILED |
| No blind retry | Do NOT blindly retry side-effecting operations |
| Reconcile first | Reconcile before retry |
| Preserve state | Preserve all available state |
| Audit | Unknown outcomes auditable |
| Escalate | If reconciliation fails, escalate |

---

## 6. External Reconciliation Contract

### 6.1 Reconciliation Pipeline

```
DETECT UNKNOWN → IDENTIFY AUTHORITATIVE SOURCE → QUERY → NORMALIZE → COMPARE → DETERMINE STATE → RECORD EVIDENCE → RESOLVE → {CONTINUE, COMPENSATE, ESCALATE}
```

### 6.2 Reconciliation Record

| Field | Type | Required | Description |
|---|---|---|---|
| `reconciliation_id` | string | yes | Unique reconciliation identifier |
| `outcome_id` | string | yes | Unknown outcome being reconciled |
| `authoritative_source` | string | yes | What source is authoritative |
| `query_performed` | string | yes | What was queried |
| `expected_state` | string | yes | What was expected |
| `actual_state` | string | yes | What was found |
| `state_match` | boolean | yes | Whether states match |
| `resolution` | enum | yes | One of: `confirmed_success`, `confirmed_failure`, `partial`, `unresolvable` |
| `evidence` | string | yes | Supporting evidence |
| `side_effects_verified` | boolean | yes | Whether side effects verified |
| `compensation_required` | boolean | yes | Whether compensation needed |
| `created_at` | datetime | yes | When reconciliation started |
| `completed_at` | datetime | no | When reconciliation completed |

### 6.3 Reconciliation Rules

| Rule | Description |
|---|---|
| Idempotent | Reconciliation must be idempotent |
| No blind re-execute | Do NOT blindly re-execute side effects |
| Query authoritative | Query authoritative external source |
| Record evidence | Record all reconciliation evidence |
| Audit | Reconciliation auditable |
| Escalate failure | If unresolvable, escalate |

---

## 7. External Security Contract

### 7.1 Security Threat Categories

| Threat | Description | Mitigation |
|---|---|---|
| Forged webhook | Webhook from impersonator | Signature verification |
| Replay | Replayed legitimate request | Replay protection, nonce |
| Credential theft | Stolen credentials | Credential rotation, least privilege |
| SSRF | Server-side request forgery | SSRF protection, URL validation |
| Malicious redirects | Redirect to malicious site | Redirect policy |
| Malicious provider | Compromised provider | Provider verification, quarantine |
| Compromised connector | Tampered connector | Connector integrity verification |
| Malicious external content | Hostile web content | Content sandboxing |
| Prompt injection | External content as instruction | Content treated as data |
| Response poisoning | Poisoned external response | Response validation |
| Data exfiltration | Data theft through integration | DLP, egress controls |
| Provider impersonation | Fake provider | Provider identity verification |
| Endpoint spoofing | Fake endpoint | Endpoint verification |
| DNS manipulation | DNS hijacking | DNS security, certificate pinning |
| Token leakage | Token exposure | Token masking, rotation |
| Cross-business access | Unauthorized cross-business | Business isolation enforcement |
| Resource exhaustion | DoS through integration | Rate limiting, backpressure |
| Supply chain | Compromised dependency | Dependency verification |

### 7.2 Security Rules

| Rule | Description |
|---|---|
| Never trust external | External input is never inherently trusted |
| Verify signatures | Verify all external signatures |
| Validate timestamps | Reject stale external requests |
| Protect against replay | Replay protection for all state-changing operations |
| SSRF protection | SSRF protection for all outbound requests |
| Credential isolation | Credentials isolated from agents/models |
| Content sandboxing | Sandbox untrusted external content |
| Audit | Security events auditable |

---

## 8. External Response Trust Boundary

### 8.1 Trust Evaluation Pipeline

```
RECEIVE → VALIDATE SCHEMA → CHECK SIGNATURE → CHECK TIMESTAMP → EVALUATE SOURCE → ASSIGN TRUST STATUS → PROCESS
```

### 8.2 Trust Status

| Status | Description |
|---|---|
| `untrusted` | Default, not yet validated |
| `validated` | Schema and signature validated |
| `trusted` | Trusted for processing (based on source reputation + validation) |

### 8.3 Trust Rules

| Rule | Description |
|---|---|
| Default untrusted | All external responses start as untrusted |
| Validate first | Validate before trusting |
| HTTP status != trust | Successful HTTP status does not mean trustworthy content |
| Source reputation | Trust based on source reputation + validation |
| Time-bounded | Trust is time-bounded |

---

## 9. Integration Quarantine / DLQ Contract

### 9.1 Quarantine Triggers

| Trigger | Description |
|---|---|
| Invalid signature | Signature verification failed |
| Invalid schema | Payload schema mismatch |
| Repeated failure | Processing failed repeatedly |
| Suspicious payload | Payload flagged as suspicious |
| Security violation | Security boundary violation |
| Unknown provider state | Provider state uncertain |
| Poisoned event | Event causes repeated failures |
| Unsupported version | Payload version unsupported |
| Rate limit exhaustion | Rate limit repeatedly exceeded |
| Reconciliation failure | Reconciliation could not resolve |

### 9.2 Dead Letter Queue Record

| Field | Type | Required | Description |
|---|---|---|---|
| `dlq_id` | string | yes | Unique DLQ entry identifier |
| `item_type` | string | yes | What was queued |
| `item_id` | string | yes | ID of queued item |
| `reason` | string | yes | Why queued |
| `error_category` | string | yes | Error category |
| `retry_count` | integer | yes | Retries attempted |
| `last_error` | string | yes | Last error message |
| `evidence` | string | yes | Supporting evidence |
| `provenance` | ProvenanceRef | yes | Origin record |
| `created_at` | datetime | yes | When queued |
| `status` | enum | yes | One of: `queued`, `reviewing`, `retried`, `released`, `destroyed` |

### 9.3 Quarantine Rules

| Rule | Description |
|---|---|
| Preserve evidence | Preserve evidence and provenance |
| No silent discard | Do not silently discard |
| Review required | Quarantined items require review |
| Audit | Quarantine decisions auditable |
| Configurable retention | Retention policy configurable |

---

## 10. Graceful Integration Shutdown Contract

### 10.1 Shutdown Pipeline

```
STOP NEW → DRAIN IN-FLIGHT → FINISH SAFE → CANCEL CANCELLABLE → RECONCILE UNKNOWN → RELEASE RESOURCES → PERSIST STATE → EMIT OBSERVABILITY
```

### 10.2 Shutdown Rules

| Rule | Description |
|---|---|
| Stop new | Stop accepting new external operations |
| Drain | Finish in-flight operations where safe |
| Reconcile | Reconcile unknown outcomes before shutdown |
| Release | Release connections, leases, resources |
| Persist | Persist final state |
| Observable | Shutdown generates observability signals |
| No ambiguity | Shutdown must not create ambiguous ownership |

---

## 11. External Configuration Audit Contract

### 11.1 Audit Requirements

Every external operation MUST be traceable. At minimum:

| Field | Description |
|---|---|
| `operation_id` | Operation identifier |
| `connector_id` | Connector used |
| `provider_id` | Provider called |
| `actor_id` | Who initiated |
| `agent_id` | Agent context |
| `workflow_id` | Workflow context |
| `task_id` | Task context |
| `objective_id` | Objective context |
| `correlation_id` | End-to-end trace |
| `causation_id` | What caused this |
| `business_id` | Business scope |
| `division_id` | Division scope |
| `request_timestamp` | When request sent |
| `response_timestamp` | When response received |
| `execution_mode` | Live/sandbox/dry-run |
| `side_effect_class` | Side-effect classification |
| `authorization_decision` | Auth decision |
| `governance_decision` | Governance decision |
| `credential_ref` | Credential used |
| `external_request_id` | External request ID |
| `external_resource_id` | External resource ID |
| `final_state` | Final state |
| `unknown_outcome_state` | Unknown outcome state |
| `reconciliation_result` | Reconciliation result |

### 11.2 Audit Rules

| Rule | Description |
|---|---|
| Complete | All external operations auditable |
| Secrets redacted | Secrets redacted from logs |
| Sensitive payload | Sensitive payloads NOT in logs |
| Retention | Audit retention per policy |
| Immutable | Audit records immutable |

---

*This document defines external failure, reconciliation, and security contracts. Runtime invariants and test scenarios are in INTEGRATION_INVARIANTS.md.*
