# NEXUS — Integration Invariants & Test Scenarios

**Layer:** Phase 4 — Integration, Provider & External Boundary Contracts  
**Status:** LOCKED  
**Branch:** contracts/integration-external  
**Depends on:** Phase 1 Core Interface Contracts, Phase 2 Data & Event Schemas, Phase 3 Runtime & Execution Contracts, INTEGRATION_EXTERNAL_CONTRACTS.md, PROVIDER_CONTRACTS.md, EXTERNAL_FAILURE_RECONCILIATION.md  

---

## 1. Purpose

This document defines **integration invariants** (non-negotiable behavioral properties for external boundaries) and **test scenarios** for validating NEXUS integration contracts.

---

## 2. Integration Invariants

### EXT-01: External Input Is Not Inherently Trusted

**Rule:** All external input must go through authentication, validation, security check, and policy check before processing.

**Enforcement:**
- External input pipeline enforced
- No external input processed without validation
- Default: untrusted until validated

**Violation:** External input processed without validation.

---

### EXT-02: External Events Do Not Grant Authority

**Rule:** External events (webhooks, messages, callbacks) are information, not authorization.

**Enforcement:**
- External events carry no authority fields
- Authority resolved separately by Governance
- External event may trigger decision, not execute action

**Violation:** Action taken based solely on external event.

---

### EXT-03: Successful Network Delivery != Successful Side Effect

**Rule:** A successful HTTP response does not guarantee the business side effect occurred as expected.

**Enforcement:**
- Network success = request delivered
- Business success = side effect confirmed
- Separate verification required

**Violation:** Assuming business success from network success.

---

### EXT-04: UNKNOWN_OUTCOME Is Not FAILED

**Rule:** UNKNOWN_OUTCOME represents uncertainty, not definitive failure.

**Enforcement:**
- UNKNOWN_OUTCOME state exists distinctly from FAILED
- UNKNOWN_OUTCOME triggers reconciliation
- UNKNOWN_OUTCOME is not automatically retried

**Violation:** Converting UNKNOWN_OUTCOME to FAILED without reconciliation.

---

### EXT-05: Unknown Side Effects Require Reconciliation

**Rule:** When side effect status is unknown, reconciliation is required before retry.

**Enforcement:**
- Unknown side effect = UNKNOWN_OUTCOME
- Reconciliation pipeline triggered
- No blind retry of side-effecting operations

**Violation:** Blind retry of operation with unknown side effect.

---

### EXT-06: One Operation Has One Retry Owner

**Rule:** Each operation has one authoritative retry owner.

**Enforcement:**
- Retry owner assigned per operation type
- No nested independent retry loops
- Single retry policy per operation

**Violation:** Multiple independent retry loops on same operation.

---

### EXT-07: Provider Failover Must Not Duplicate Unsafe Side Effects

**Rule:** Failing over to a fallback provider must not cause duplicate irreversible side effects.

**Enforcement:**
- Check primary outcome before failover
- Reconcile if primary outcome unknown
- Only failover for non-side-effecting or idempotent operations without reconciliation

**Violation:** Failing over and re-executing irreversible side effect.

---

### EXT-08: Network Reachability != Permission

**Rule:** Being able to reach an external endpoint does not grant permission to use it.

**Enforcement:**
- Network reachability checked separately from authorization
- Authorization required before external execution
- Network policy enforced

**Violation:** Executing external call based solely on network reachability.

---

### EXT-09: Connector Health != Authorization

**Rule:** A healthy connector does not automatically authorize operations.

**Enforcement:**
- Connector health checked separately from authorization
- Authorization required before operations
- Health is operational status, not permission

**Violation:** Operating on external system based solely on connector health.

---

### EXT-10: Provider Health != Authorization

**Rule:** A healthy provider does not automatically authorize operations.

**Enforcement:**
- Provider health checked separately from authorization
- Authorization required before operations
- Health is operational status, not permission

**Violation:** Operating on provider based solely on provider health.

---

### EXT-11: External Response != Trusted Fact Until Validated

**Rule:** External responses are untrusted until validated.

**Enforcement:**
- External responses start as untrusted
- Validation pipeline required
- Trust status assigned after validation

**Violation:** Treating external response as fact without validation.

---

### EXT-12: Tool Result != Authority

**Rule:** Tool execution results do not grant authority.

**Enforcement:**
- Tool results carry no authority fields
- Authority resolved separately by Governance
- Tool results are information, not permission

**Violation:** Acting on tool result without governance check.

---

### EXT-13: Credential Possession != Permission

**Rule:** Having a credential does not automatically authorize its use.

**Enforcement:**
- Credential possession separate from authorization
- Authorization required before credential use
- Credential scoped to specific operations

**Violation:** Using credential without authorization check.

---

### EXT-14: Objective/WHY != Authority

**Rule:** Objectives explain WHY, not what is permitted.

**Enforcement:**
- Objectives carry no authority fields
- Authority resolved separately by Governance
- Objectives explain purpose, not permission

**Violation:** Acting on objective without governance check.

---

### EXT-15: Attention != Authority

**Rule:** Attention items may request attention but do not grant permission.

**Enforcement:**
- Attention items carry no authority fields
- Attention triggers governance escalation
- Attention resolution != authorization

**Violation:** Acting on attention item without governance check.

---

### EXT-16: Sandbox/Dry-Run != Live Execution

**Rule:** Sandbox and dry-run modes prevent live side effects.

**Enforcement:**
- Execution mode propagated through chain
- Sandbox/dry-run blocks live external calls
- Mode visible in all records

**Violation:** Live side effect in sandbox/dry-run mode.

---

### EXT-17: Business Isolation Survives External Lifecycle

**Rule:** Business isolation must be maintained through entire external operation lifecycle.

**Enforcement:**
- Business scope on all external operations
- Credentials scoped to business
- Mappings scoped to business
- No cross-business leakage

**Violation:** Cross-business data leakage through external integration.

---

### EXT-18: External Configuration Must Be Versioned and Auditable

**Rule:** All external configuration changes are versioned and auditable.

**Enforcement:**
- Configuration version tracked
- Changes auditable
- No silent configuration changes
- Version traceable to execution

**Violation:** Configuration change without versioning/audit.

---

### EXT-19: Reconciliation Must Be Idempotent

**Rule:** Reconciliation operations must be idempotent.

**Enforcement:**
- Reconciliation produces same result on re-execution
- No duplicate side effects from reconciliation
- Evidence preserved across reconciliation attempts

**Violation:** Non-idempotent reconciliation causing duplicate effects.

---

### EXT-20: Secrets Must Not Leak

**Rule:** Secrets must not leak into ordinary model context or logs.

**Enforcement:**
- Credential references used, not raw values
- Credentials masked in logs
- Sensitive payloads not in logs
- DLP checks before egress

**Violation:** Secret found in model context or logs.

---

### EXT-21: Governance Outcomes Remain Canonical

**Rule:** Governance outcomes are exactly: ALLOW, DENY, REQUIRE_APPROVAL, ALLOW_WITH_CONSTRAINTS, ESCALATE.

**Enforcement:**
- No additional governance outcomes invented
- External contracts use canonical outcomes
- Governance is highest control layer

**Violation:** Additional governance outcome invented.

---

### EXT-22: Runtime State != Durable State

**Rule:** Runtime state (in-memory) is distinct from durable state (persisted).

**Enforcement:**
- Critical external state persisted
- Recovery from durable state
- Runtime reconstructable

**Violation:** Critical external state lost on crash.

---

### EXT-23: External Side Effects Must Be Classified Before Execution

**Rule:** Side-effect classification required before external execution.

**Enforcement:**
- Side-effect class declared in request
- Classification verified before execution
- Conservative default if uncertain

**Violation:** External execution without side-effect classification.

---

### EXT-24: Cancellation Unsupported != Failure

**Rule:** When external system does not support cancellation, it is not a failure.

**Enforcement:**
- Cancellation unsupported state exists
- Not treated as failure
- Handled gracefully

**Violation:** Treating unsupported cancellation as failure.

---

### EXT-25: Provider/Model Identity != Agent Identity

**Rule:** Provider and model identities are distinct from agent identity.

**Enforcement:**
- Separate identity fields
- Separate authorization
- Separate audit

**Violation:** Conflating provider/model identity with agent identity.

---

### EXT-26: Connector Identity != Provider Identity

**Rule:** Connector identity is distinct from provider identity.

**Enforcement:**
- Separate identity fields
- Separate lifecycle
- Separate audit

**Violation:** Conflating connector identity with provider identity.

---

### EXT-27: Web Content Is Untrusted External Input

**Rule:** Web content is external/untrusted information.

**Enforcement:**
- Web content treated as data, not instruction
- Content sandboxed
- Prompt injection protection

**Violation:** Treating web content as trusted instruction.

---

### EXT-28: Observability Cannot Grant Authority

**Rule:** Observability data cannot be used as authority.

**Enforcement:**
- Observability is read-only
- Observability does not affect authorization
- Observability is for visibility only

**Violation:** Observability data used as authorization.

---

### EXT-29: Quarantine Preserves Evidence

**Rule:** Quarantined items preserve evidence and provenance.

**Enforcement:**
- Evidence preserved with quarantined items
- Provenance maintained
- No silent discard

**Violation:** Evidence lost during quarantine.

---

### EXT-30: External Integrations Cannot Bypass Governance

**Rule:** External integrations MUST NOT bypass Governance.

**Enforcement:**
- Governance check before external operations
- No integration-specific authority
- Governance is highest control layer

**Violation:** External operation bypassing governance.

---

## 3. Test Scenarios

### Scenario 1: Successful External Read

| Aspect | Expected |
|---|---|
| **Precondition** | Valid read request, authorized, resources available |
| **Input** | GET request to external API |
| **Transition** | REQUESTED → SENT → RESPONSE_RECEIVED → VERIFIED |
| **Postcondition** | Data retrieved, validated, recorded |
| **Security** | Authorization verified, scope enforced |

### Scenario 2: Successful External Write

| Aspect | Expected |
|---|---|
| **Precondition** | Valid write request, authorized, idempotency key |
| **Input** | POST/PUT request with idempotency key |
| **Transition** | REQUESTED → SENT → ACCEPTED → RESPONSE_RECEIVED → VERIFIED |
| **Postcondition** | Write completed, side effect confirmed |
| **Security** | Authorization verified, idempotency enforced |

### Scenario 3: Network Timeout After Possible Side Effect

| Aspect | Expected |
|---|---|
| **Precondition** | External write request sent |
| **Input** | Network timeout after request sent |
| **Transition** | SENT → UNKNOWN_OUTCOME |
| **Postcondition** | UNKNOWN_OUTCOME recorded, reconciliation triggered |
| **Security** | No blind retry |

### Scenario 4: Duplicate Webhook

| Aspect | Expected |
|---|---|
| **Precondition** | Webhook delivered twice |
| **Input** | Same event_id delivered twice |
| **Transition** | First processed, second detected as duplicate |
| **Postcondition** | Event processed once, duplicate ignored |
| **Security** | Duplicate cannot bypass security |

### Scenario 5: Forged Webhook

| Aspect | Expected |
|---|---|
| **Precondition** | Webhook with invalid signature |
| **Input** | Webhook with forged signature |
| **Transition** | RECEIVE → AUTHENTICATE → REJECTED |
| **Postcondition** | Webhook rejected, quarantined |
| **Security** | Forged webhook blocked |

### Scenario 6: Replayed Webhook

| Aspect | Expected |
|---|---|
| **Precondition** | Webhook replayed |
| **Input** | Previously processed webhook replayed |
| **Transition** | RECEIVE → CHECK REPLAY → REJECTED |
| **Postcondition** | Replay detected, webhook rejected |
| **Security** | Replay blocked |

### Scenario 7: Malformed Webhook

| Aspect | Expected |
|---|---|
| **Precondition** | Webhook with invalid schema |
| **Input** | Webhook with malformed payload |
| **Transition** | RECEIVE → VALIDATE PAYLOAD → REJECTED |
| **Postcondition** | Malformed webhook rejected |
| **Security** | Invalid payload blocked |

### Scenario 8: Provider Outage

| Aspect | Expected |
|---|---|
| **Precondition** | Provider becomes unavailable |
| **Input** | Request during provider outage |
| **Transition** | Provider marked UNAVAILABLE, circuit breaker opens |
| **Postcondition** | Request fails gracefully, fallback considered |
| **Security** | Outage does not bypass governance |

### Scenario 9: Provider Rate Limit

| Aspect | Expected |
|---|---|
| **Precondition** | Rate limit exceeded |
| **Input** | Request when rate limited |
| **Transition** | RATE_LIMITED, backoff applied |
| **Postcondition** | Request queued for retry |
| **Security** | Rate limit does not bypass governance |

### Scenario 10: Connector Outage

| Aspect | Expected |
|---|---|
| **Precondition** | Connector becomes unavailable |
| **Input** | Request through connector |
| **Transition** | Connector marked DEGRADED/UNAVAILABLE |
| **Postcondition** | Request fails gracefully |
| **Security** | Outage does not bypass governance |

### Scenario 11: Provider Failover

| Aspect | Expected |
|---|---|
| **Precondition** | Primary provider fails |
| **Input** | Request during provider failover |
| **Transition** | Primary fails, fallback selected |
| **Postcondition** | Fallback used, side effects safe |
| **Security** | Failover authorized |

### Scenario 12: Failover After Unknown Side Effect

| Aspect | Expected |
|---|---|
| **Precondition** | Primary provider timeout with unknown side effect |
| **Input** | Failover request |
| **Transition** | UNKNOWN_OUTCOME → RECONCILING → FAILOVER |
| **Postcondition** | Reconciliation before failover |
| **Security** | No duplicate side effects |

### Scenario 13: Credential Expiration

| Aspect | Expected |
|---|---|
| **Precondition** | Credential expires |
| **Input** | Request with expired credential |
| **Transition** | CREDENTIAL_FAILURE |
| **Postcondition** | Credential rotated, request retried |
| **Security** | Expired credential blocked |

### Scenario 14: Credential Revocation

| Aspect | Expected |
|---|---|
| **Precondition** | Credential revoked |
| **Input** | Request with revoked credential |
| **Transition** | CREDENTIAL_FAILURE |
| **Postcondition** | Request denied |
| **Security** | Revoked credential blocked |

### Scenario 15: DLP Rejection

| Aspect | Expected |
|---|---|
| **Precondition** | Sensitive data in outbound request |
| **Input** | Request with classified data |
| **Transition** | DLP_REJECTED |
| **Postcondition** | Request blocked, audit recorded |
| **Security** | Data egress blocked |

### Scenario 16: SSRF Attempt

| Aspect | Expected |
|---|---|
| **Precondition** | Request to internal network |
| **Input** | URL targeting private network |
| **Transition** | SSRF_REJECTED |
| **Postcondition** | Request blocked |
| **Security** | SSRF blocked |

### Scenario 17: Malicious Web Content

| Aspect | Expected |
|---|---|
| **Precondition** | Web page with malicious content |
| **Input** | Browser navigation to malicious page |
| **Transition** | Content sandboxed |
| **Postcondition** | Malicious content contained |
| **Security** | Content treated as untrusted |

### Scenario 18: Prompt Injection From External

| Aspect | Expected |
|---|---|
| **Precondition** | External content with prompt injection |
| **Input** | External data containing instructions |
| **Transition** | Content treated as data |
| **Postcondition** | Injection blocked |
| **Security** | Injection not executed |

### Scenario 19: Unknown External Outcome

| Aspect | Expected |
|---|---|
| **Precondition** | External operation with unknown outcome |
| **Input** | Timeout after side-effecting operation |
| **Transition** | UNKNOWN_OUTCOME |
| **Postcondition** | Reconciliation triggered |
| **Security** | No blind retry |

### Scenario 20: Reconciliation Success

| Aspect | Expected |
|---|---|
| **Precondition** | Unknown outcome, authoritative source available |
| **Input** | Reconciliation query |
| **Transition** | UNKNOWN → RECONCILING → RECONCILED |
| **Postcondition** | State reconciled, evidence recorded |
| **Security** | Reconciliation idempotent |

### Scenario 21: Reconciliation Failure

| Aspect | Expected |
|---|---|
| **Precondition** | Unknown outcome, no authoritative source |
| **Input** | Reconciliation attempt |
| **Transition** | UNKNOWN → RECONCILING → UNRESOLVABLE |
| **Postcondition** | Escalated to human |
| **Security** | No blind retry |

### Scenario 22: Duplicate External Request

| Aspect | Expected |
|---|---|
| **Precondition** | Same request sent twice |
| **Input** | Duplicate request with same idempotency key |
| **Transition** | First processed, second returned cached |
| **Postcondition** | Request processed once |
| **Security** | Duplicate blocked |

### Scenario 23: Non-Idempotent External Operation

| Aspect | Expected |
|---|---|
| **Precondition** | External operation without idempotency |
| **Input** | Non-idempotent request |
| **Transition** | Executed with UNKNOWN_SIDE_EFFECT classification |
| **Postcondition** | Side effect classified conservatively |
| **Security** | Reconcile before retry |

### Scenario 24: Sandbox Mode Preventing Live Side Effect

| Aspect | Expected |
|---|---|
| **Precondition** | Sandbox/dry-run mode active |
| **Input** | External write request in sandbox mode |
| **Transition** | Request blocked/simulated |
| **Postcondition** | No live side effect |
| **Security** | Sandbox enforced |

### Scenario 25: Cross-Business Access Attempt

| Aspect | Expected |
|---|---|
| **Precondition** | Business A attempting Business B's external resource |
| **Input** | Cross-business external request |
| **Transition** | DENIED |
| **Postcondition** | Cross-business access blocked |
| **Security** | Business isolation enforced |

### Scenario 26: External Response Schema Mismatch

| Aspect | Expected |
|---|---|
| **Precondition** | External response with wrong schema |
| **Input** | Response with schema_mismatch |
| **Transition** | VALIDATION_FAILED |
| **Postcondition** | Response rejected |
| **Security** | Invalid response blocked |

### Scenario 27: Provider Version Incompatibility

| Aspect | Expected |
|---|---|
| **Precondition** | Provider version incompatible |
| **Input** | Request to incompatible version |
| **Transition** | VERSION_INCOMPATIBLE |
| **Postcondition** | Request fails gracefully |
| **Security** | Incompatible version blocked |

### Scenario 28: Connector Quarantine

| Aspect | Expected |
|---|---|
| **Precondition** | Connector security issue detected |
| **Input** | Security violation detected |
| **Transition** | ACTIVE → QUARANTINED |
| **Postcondition** | Connector quarantined, evidence preserved |
| **Security** | Quarantine enforced |

### Scenario 29: Graceful Integration Shutdown

| Aspect | Expected |
|---|---|
| **Precondition** | Shutdown signal received |
| **Input** | Shutdown request |
| **Transition** | STOP NEW → DRAIN → RECONCILE → RELEASE → PERSIST |
| **Postcondition** | Clean shutdown, state preserved |
| **Security** | No ambiguous ownership |

### Scenario 30: External Stream Reconnect/Resume

| Aspect | Expected |
|---|---|
| **Precondition** | Stream disconnected |
| **Input** | Reconnect attempt |
| **Transition** | DISCONNECTED → RECONNECTING → CONNECTED |
| **Postcondition** | Stream resumed from last offset |
| **Security** | Reconnection authorized |

---

## 4. Invariant Cross-Reference

| Invariant | Primary Enforcer | Test Scenarios |
|---|---|---|
| EXT-01: External input not trusted | Security, Validation | 1-7, 17, 18 |
| EXT-02: External events not authority | Governance | 4, 5, 6, 7 |
| EXT-03: Network success != side effect | Verification | 3, 19 |
| EXT-04: UNKNOWN != FAILED | Reconciliation | 3, 19, 20, 21 |
| EXT-05: Unknown side effects reconcile | Reconciliation | 3, 12, 19 |
| EXT-06: Single retry owner | Retry System | 8, 9, 10 |
| EXT-07: Failover no duplicate effects | Failover | 11, 12 |
| EXT-08: Reachability != permission | Authorization | 1, 2 |
| EXT-09: Connector health != auth | Authorization | 10 |
| EXT-10: Provider health != auth | Authorization | 8, 9 |
| EXT-11: Response != trusted fact | Trust | 1, 2, 26 |
| EXT-12: Tool result != authority | Governance | 1, 2 |
| EXT-13: Credential != permission | Authorization | 13, 14 |
| EXT-14: Objective != authority | Governance | 1, 2 |
| EXT-15: Attention != authority | Governance | 8, 9 |
| EXT-16: Sandbox != live | Sandbox | 24 |
| EXT-17: Business isolation | Scope System | 25 |
| EXT-18: Config versioned/auditable | Configuration | 28 |
| EXT-19: Reconciliation idempotent | Reconciliation | 20, 21 |
| EXT-20: Secrets not leaked | Security | 13, 14, 15 |
| EXT-21: Canonical governance | Governance | All |
| EXT-22: Runtime != durable | Persistence | 3, 29 |
| EXT-23: Side-effect classified | Classification | 2, 23 |
| EXT-24: Cancel unsupported != fail | Cancellation | 29 |
| EXT-25: Provider != agent identity | Identity | 11, 27 |
| EXT-26: Connector != provider identity | Identity | 10, 28 |
| EXT-27: Web content untrusted | Security | 17, 18 |
| EXT-28: Observability != authority | Observability | All |
| EXT-29: Quarantine preserves evidence | Quarantine | 5, 28 |
| EXT-30: External cannot bypass governance | Governance | All |

---

## 5. Validation Checklist

Before commit, verify:

- [ ] All 30 integration invariants defined
- [ ] All 30 test scenarios defined
- [ ] Each invariant has enforcement mechanism
- [ ] Each test scenario has preconditions, inputs, transitions, postconditions
- [ ] Phase 1 compatibility verified
- [ ] Phase 2 compatibility verified
- [ ] Phase 3 compatibility verified
- [ ] No architecture redesign
- [ ] No runtime code implemented
- [ ] Governance boundaries preserved
- [ ] Attention boundaries preserved
- [ ] WHY propagation preserved
- [ ] Multi-business isolation preserved
- [ ] Unknown outcome semantics preserved
- [ ] Identity/Authority/Capability/Permission/Trust remain distinct
- [ ] No credentials exposed
- [ ] No nested unbounded retries
- [ ] No hidden authority escalation
- [ ] No unsupported exactly-once guarantees

---

*This document defines the non-negotiable integration invariants and testable scenarios for NEXUS external boundary contracts.*
