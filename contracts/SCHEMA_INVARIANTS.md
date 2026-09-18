# NEXUS — Cross-Schema Invariants

**Layer:** Phase 2 — Data & Event Schemas  
**Status:** LOCKED  
**Branch:** contracts/data-event-schemas  
**Depends on:** All Schema Documents, Phase 1 Core Interface Contracts  

---

## 1. Purpose

This document defines **invariant rules** that must hold true across all NEXUS schemas and at all times. These invariants are non-negotiable architectural properties that no implementation may violate.

---

## 2. Invariant Definitions

### INV-01: Multi-Business Isolation

**Rule:** Data, events, memory, workflows, and all entities scoped to a business MUST NOT be accessible by another business without explicit Governance policy + audit trail.

**Enforcement:**
- `business_id` is present on all business-scoped objects
- Cross-business access requires Governance policy evaluation
- Cross-business access must be logged in Audit schema
- Default: no cross-business access

**Violation example:** Business A's agent reading Business B's memory without policy.

---

### INV-02: WHY Preservation

**Rule:** The `why` field and `why_chain` must be preserved through objective decomposition, workflow execution, task assignment, and outcome evaluation. No module may discard WHY without explicit Governance approval.

**Enforcement:**
- `why` is required on Objective, Workflow, Task, Outcome schemas
- `why_chain` is propagated through child entities
- Discarding WHY requires Governance policy decision
- WHY is NOT authority — it explains purpose, not permission

**Violation example:** Agent executing task without knowing why.

---

### INV-03: Governance Precedence

**Rule:** Governance is the highest control layer. Governance decisions override Objective, Attention, Agent, Workflow, Tool, and Model behavior.

**Enforcement:**
- Policy decision outcomes are final: `ALLOW`, `DENY`, `REQUIRE_APPROVAL`, `ALLOW_WITH_CONSTRAINTS`, `ESCALATE`
- No module may override a `DENY` decision
- Governance may intervene across divisions when authorized
- Configuration changes may require Governance approval

**Violation example:** Workflow proceeding after Governance DENY.

---

### INV-04: Identity ≠ Authority

**Rule:** Identity establishes WHO something is. Identity does NOT grant permission to do anything.

**Enforcement:**
- Identity Schema has no authority fields
- Authority is resolved separately through Governance
- Identity records do not carry permission, capability, or trust

**Violation example:** Agent performing action based solely on identity, without authority check.

---

### INV-05: Authority ≠ Capability

**Rule:** Having authority to do something does not mean you have the capability to do it. Capability is a separate concept.

**Enforcement:**
- Agent Schema has separate `capabilities` and `authority_ref` fields
- Capabilities reference what CAN be done
- Authority references what MAY be done
- Both must be checked before execution

**Violation example:** Agent with authority but no tool capability attempting tool execution.

---

### INV-06: Capability ≠ Permission

**Rule:** Having a capability does not mean you are permitted to use it. Permission is a specific grant from Governance.

**Enforcement:**
- Agent Schema has separate `capabilities` and `permissions_ref` fields
- Capabilities are references to what exists
- Permissions are grants from Governance
- Both must be checked

**Violation example:** Agent with tool capability using tool without permission.

---

### INV-07: Permission ≠ Trust

**Rule:** Permission is a grant. Trust is contextual confidence. They are separate concepts.

**Enforcement:**
- Agent Schema has separate `permissions_ref` and `trust_ref` fields
- Trust is contextual (time-bounded, scope-limited)
- Trust never grants permission
- Only Governance grants permission

**Violation example:** Trusted agent performing action without explicit permission.

---

### INV-08: Attention ≠ Authority

**Rule:** An attention item may request owner attention but cannot grant permission to execute an action.

**Enforcement:**
- Attention Schema has no authority fields
- Attention may trigger Governance escalation
- Attention resolution does not equal authorization
- Silence on attention = no action (not approval)

**Violation example:** Agent acting on attention item without Governance check.

---

### INV-09: Event ≠ Command

**Rule:** An event is a record of something that happened. An event is not a command to do something.

**Enforcement:**
- Event Schema has no action/execution fields
- Events may trigger Triggers which may issue Commands
- Events do not bypass Governance
- Events are facts, not instructions

**Violation example:** Interpreting event as instruction to execute action.

---

### INV-10: Event ≠ Approval

**Rule:** An event may trigger an approval process but does not itself constitute approval.

**Enforcement:**
- Event Schema has no approval fields
- Approval requires explicit Approval Schema record
- Events are information, not authorization
- Governance must evaluate before approval

**Violation example:** Treating "approval_requested" event as approval granted.

---

### INV-11: Objective ≠ Authority

**Rule:** An objective explains WHY something should be done. It does not grant authority to do it.

**Enforcement:**
- Objective Schema has no authority fields
- Objectives explain purpose (WHY)
- Authority is resolved by Governance
- Objectives never override Governance

**Violation example:** Agent executing objective without authority check.

---

### INV-12: Model ≠ Agent Identity

**Rule:** Model output is inference, not the agent's own knowledge. Model is a tool, not the agent's identity.

**Enforcement:**
- Model Invocation Schema has separate `agent_id` and `model_id`
- Model output is recorded as model inference in provenance
- Agent must verify/model output before treating as fact
- Memory from model inference is marked `confidence: inferred`

**Violation example:** Agent treating model output as its own verified knowledge.

---

### INV-13: Runtime State ≠ Durable State

**Rule:** Runtime state (in-memory, ephemeral) is distinct from durable state (persisted, recoverable). NEXUS restart must not lose critical state.

**Enforcement:**
- Workflow Schema has `checkpoint_ref` for durable state
- Agent state must be recoverable
- Critical memory must be durable
- Runtime state must be reconstructable from durable state

**Violation example:** Losing workflow progress on restart.

---

### INV-14: Unknown Outcome ≠ Known Failure

**Rule:** When an operation's outcome is uncertain, it MUST be represented as `UNKNOWN_OUTCOME`, not automatically converted to failure.

**Enforcement:**
- Error Schema has `UNKNOWN_OUTCOME` category
- `UNKNOWN_OUTCOME` is NOT retryable (may cause duplicate side effects)
- Must be escalated to reconciliation process
- Must include `reconciliation_ref`

**Violation example:** Retrying timed-out tool call without checking if it succeeded.

---

### INV-15: Accessible Source ≠ Trusted Source

**Rule:** Just because data is accessible does not mean it is trustworthy. Source accessibility and source trustworthiness are separate concepts.

**Enforcement:**
- Memory Schema has `confidence` field (factual, inferred, uncertain, etc.)
- Knowledge Schema has `KnowledgeConfidence` structure
- Provenance records source but does not assert truth
- Trust is context-dependent and time-bounded

**Violation example:** Treating accessible memory as verified fact.

---

### INV-16: Silence ≠ Approval

**Rule:** Lack of response is not approval. Expired approvals default to denial.

**Enforcement:**
- Approval Schema has `expires_at` field
- Approval timeout = denial (configurable)
- Attention expiration = no action
- Policy decisions have explicit expiry

**Violation example:** Proceeding because approver did not respond.

---

### INV-17: UI Session ≠ Runtime Heartbeat

**Rule:** UI session status does not indicate system execution status. Background execution continues regardless of UI state.

**Enforcement:**
- Workflow state is independent of UI session
- Agent execution continues when UI is disconnected
- Background tasks are not affected by UI state
- System health is independent of user presence

**Violation example:** Stopping workflow because user closed UI.

---

### INV-18: Duplicate Delivery Must Be Safely Handled

**Rule:** Events may be delivered more than once (at-least-once). Consumers must handle duplicates safely.

**Enforcement:**
- Event Schema has `idempotency_key` and `deduplication_key`
- Consumers must check idempotency before processing
- Idempotency window is configurable
- Irreversible side effects require idempotency_key

**Violation example:** Processing same event twice, causing duplicate side effects.

---

### INV-19: Correlation ≠ Authorization

**Rule:** `correlation_id` is for tracing only. It does not carry authority or authorization.

**Enforcement:**
- Correlation ID is a UUID for tracing
- Correlation ID is not checked for authorization
- Authorization is resolved separately by Governance
- Correlation ID must not be used as access token

**Violation example:** Using correlation_id to bypass authorization.

---

### INV-20: Provenance ≠ Truth

**Rule:** Provenance records where data came from but does not assert that data is true or correct.

**Enforcement:**
- Provenance records origin, producer, timestamp
- Truth/value is assessed separately (confidence, verification)
- Model-inferred data is marked as inference
- Provenance is evidence, not proof

**Violation example:** Assuming data is true because provenance shows known source.

---

### INV-21: Legacy Specs ≠ Canonical Architecture

**Rule:** Legacy `*-spec.md` files are HISTORICAL/NON-CANONICAL. They must not override canonical modules or Phase 1 contracts.

**Enforcement:**
- Legacy specs are marked HISTORICAL in file headers
- Canonical modules (NEXUS-*.md) are the source of truth
- Phase 1 contracts are the interface truth
- Phase 2 schemas are the data/event truth

**Violation example:** Implementing based on legacy spec instead of canonical module.

---

## 3. Invariant Enforcement

### 3.1 Schema-Level Enforcement

Each schema document includes validation rules that enforce invariants. Schema validation must be performed:
- At schema creation time
- At data ingestion time
- At contract boundary crossing

### 3.2 Runtime Enforcement

At runtime, invariants are enforced by:
- Governance module (INV-03, INV-04 through INV-11)
- Identity module (INV-04)
- Security module (INV-15)
- Memory/Context module (INV-02, INV-12, INV-20)
- Observability module (INV-17)
- All modules (INV-01, INV-18, INV-19)

### 3.3 Invariant Violation Response

When an invariant is violated:
1. Operation must be blocked (if detected at enforcement time)
2. Error must be reported with appropriate category
3. Audit event must be recorded
4. Attention may be triggered (for critical invariants)
5. Governance review may be required

---

## 4. Invariant Cross-Reference

| Invariant | Primary Enforcer | Schema Enforcement |
|---|---|---|
| INV-01: Multi-Business Isolation | Governance, All Modules | `business_id` required on scoped objects |
| INV-02: WHY Preservation | Objective Engine, All Work Modules | `why` required on work entities |
| INV-03: Governance Precedence | Governance | Policy decision is final |
| INV-04: Identity ≠ Authority | Identity, Governance | Identity has no authority fields |
| INV-05: Authority ≠ Capability | Governance, Agent Runtime | Separate authority and capability refs |
| INV-06: Capability ≠ Permission | Governance, Tool Runtime | Separate capability and permission refs |
| INV-07: Permission ≠ Trust | Governance | Separate permission and trust refs |
| INV-08: Attention ≠ Authority | Attention | Attention has no authority fields |
| INV-09: Event ≠ Command | Event System | Event has no action fields |
| INV-10: Event ≠ Approval | Governance | Approval requires explicit record |
| INV-11: Objective ≠ Authority | Objective Engine, Governance | Objective has no authority fields |
| INV-12: Model ≠ Agent Identity | Model Router, Agent Runtime | Separate agent and model IDs |
| INV-13: Runtime ≠ Durable | Persistence, Workflow | Checkpoint reference required |
| INV-14: Unknown ≠ Failure | Error System | UNKNOWN_OUTCOME category exists |
| INV-15: Accessible ≠ Trusted | Memory, Knowledge | Confidence field required |
| INV-16: Silence ≠ Approval | Approval, Attention | Expiry defaults to denial |
| INV-17: UI ≠ Runtime | Workflow, Agent | Execution independent of UI |
| INV-18: Duplicate Safe | Event System | Idempotency key supported |
| INV-19: Correlation ≠ Auth | All Modules | Correlation is tracing only |
| INV-20: Provenance ≠ Truth | Memory, Knowledge | Provenance is evidence only |
| INV-21: Legacy ≠ Canonical | All Modules | Canonical modules are source of truth |

---

## 5. Invariant Testing Requirements

Each invariant must have corresponding tests:
- Positive test: invariant holds under normal conditions
- Negative test: invariant is enforced when violation is attempted
- Edge case test: invariant holds under boundary conditions

---

*These 21 invariants are the non-negotiable architectural properties of NEXUS. Any implementation that violates any invariant is non-conformant.*
