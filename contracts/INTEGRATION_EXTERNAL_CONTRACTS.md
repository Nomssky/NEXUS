# NEXUS — Integration & External Boundary Contracts

**Layer:** Phase 4 — Integration, Provider & External Boundary Contracts  
**Status:** LOCKED  
**Branch:** contracts/integration-external  
**Depends on:** Phase 1 Core Interface Contracts, Phase 2 Data & Event Schemas, Phase 3 Runtime & Execution Contracts  

---

## 1. Purpose

This document defines **implementation-facing contracts** for everything that crosses the NEXUS internal/external boundary: connector registry, connector lifecycle, credential resolution, external API execution, webhooks, polling, streaming, external resource mapping, side-effect classification, idempotency, network boundary, browser/web boundary, database/storage boundary, sandbox mode, DLP, security, quarantine, and configuration.

The external world is **NEVER inherently trusted**.

---

## 2. Core Principle — External Input Pipeline

Every external input follows:

```
EXTERNAL INPUT → AUTHENTICATE → IDENTIFY SOURCE → VALIDATE → NORMALIZE → SECURITY CHECK → POLICY CHECK → SCOPE CHECK → TRUST/PROVENANCE EVALUATION → PROCESS → VERIFY → RECORD
```

Every outbound operation follows:

```
INTERNAL REQUEST → IDENTITY → AUTHORIZATION → GOVERNANCE → CREDENTIAL RESOLUTION → NETWORK POLICY → RESOURCE/BUDGET CHECK → IDEMPOTENCY → EXECUTE → VALIDATE RESPONSE → VERIFY SIDE EFFECT → RECORD OUTCOME
```

---

## 3. Connector Registry Contract

### 3.1 Connector Record

| Field | Type | Required | Description |
|---|---|---|---|
| `connector_id` | string | yes | Unique connector identifier |
| `connector_type` | enum | yes | One of: `api`, `webhook`, `polling`, `streaming`, `database`, `browser`, `file`, `custom` |
| `provider_id` | string | yes | Associated provider |
| `version` | string | yes | Connector version |
| `status` | enum | yes | Lifecycle state (see section 4) |
| `capabilities` | list[string] | yes | What this connector can do |
| `supported_operations` | list[string] | yes | Operations supported |
| `supported_events` | list[string] | no | Events this connector can receive |
| `auth_methods` | list[string] | yes | Authentication methods supported |
| `auth_scopes` | list[string] | no | Required auth scopes |
| `rate_limits` | RateLimitConfig | no | Rate limit configuration |
| `side_effect_capabilities` | list[enum] | yes | Side-effect types supported |
| `idempotency_capabilities` | IdempotencyCapability | no | Idempotency support declaration |
| `webhook_capabilities` | WebhookCapability | no | Inbound webhook support |
| `polling_capabilities` | PollingCapability | no | Polling support |
| `streaming_capabilities` | StreamingCapability | no | Streaming support |
| `health_state` | enum | yes | Current health |
| `configuration` | map[string,any] | no | Connector-specific config |
| `security_requirements` | list[string] | yes | Security requirements |
| `owner_id` | string | yes | Who owns this connector |
| `business_scope` | string | conditionally | Business scope (absent for global) |
| `classification` | enum | yes | Data classification |
| `created_at` | datetime | yes | When registered |
| `updated_at` | datetime | no | When last modified |
| `metadata` | map[string,string] | no | Extension data |

### 3.2 Connector Identity Separation

| Concept | Identity | Scope |
|---|---|---|
| Connector | `connector_id` | Integration boundary |
| Provider | `provider_id` | External service provider |
| Agent | `agent_id` | NEXUS agent |
| Tool | `tool_id` | Tool definition |
| Model | `model_id` | AI model |

These identities MUST remain distinct.

### 3.3 IdempotencyCapability

| Field | Type | Required | Description |
|---|---|---|---|
| `supported` | boolean | yes | Whether connector supports idempotency |
| `key_header` | string | no | Header name for idempotency key |
| `key_format` | string | no | Expected key format |
| `retention_hours` | integer | no | How long idempotency is retained |
| `scope` | enum | no | One of: `endpoint`, `operation`, `resource` |

### 3.4 WebhookCapability

| Field | Type | Required | Description |
|---|---|---|---|
| `inbound_supported` | boolean | yes | Can receive webhooks |
| `outbound_supported` | boolean | yes | Can send webhooks |
| `signature_verification` | boolean | yes | Supports signature verification |
| `replay_protection` | boolean | yes | Supports replay protection |
| `ordering_guarantee` | enum | no | One of: `none`, `per_source`, `global` |

### 3.5 PollingCapability

| Field | Type | Required | Description |
|---|---|---|---|
| `supported` | boolean | yes | Supports polling |
| `cursor_types` | list[string] | no | Supported cursor types |
| `pagination` | enum | no | One of: `offset`, `cursor`, `token`, `none` |
| `max_page_size` | integer | no | Maximum page size |

### 3.6 StreamingCapability

| Field | Type | Required | Description |
|---|---|---|---|
| `supported` | boolean | yes | Supports streaming |
| `protocol` | enum | no | One of: `websocket`, `sse`, `grpc`, `custom` |
| `reconnect_supported` | boolean | yes | Can reconnect |
| `offset_resume` | boolean | yes | Can resume from offset |

---

## 4. Connector Lifecycle Contract

### 4.1 Lifecycle State Machine

```
DISCOVERED → REGISTERED → CONFIGURED → VALIDATED → ACTIVE
                                        ↓
                                    DEGRADED → RECOVERING → ACTIVE
                                        ↓
                                    QUARANTINED → {RECOVERING, RETIRED}
                                        ↓
                                    DRAINING → DISABLED → RETIRED
```

### 4.2 Lifecycle States

| State | Description |
|---|---|
| `DISCOVERED` | Connector identified but not yet registered |
| `REGISTERED` | Connector registered in registry |
| `CONFIGURED` | Configuration applied |
| `VALIDATED` | Configuration validated, connectivity tested |
| `ACTIVE` | Ready for use |
| `DEGRADED` | Operational but degraded |
| `RECOVERING` | Recovering from failure |
| `QUARANTINED` | Quarantined due to security/reliability issue |
| `DRAINING` | Finishing in-flight work, not accepting new |
| `DISABLED` | Disabled by operator/policy |
| `RETIRED` | Permanently retired |

### 4.3 Lifecycle Rules

| Rule | Description |
|---|---|
| No auto-activation | Connector does not become ACTIVE solely from configuration |
| Validation required | Activation requires validation + policy authorization |
| Quarantine | Security/reliability issues trigger quarantine |
| Drain before disable | Disable drains in-flight work first |
| Audit | All lifecycle transitions are auditable |

---

## 5. Credential Resolution Contract

### 5.1 Credential Resolution Pipeline

```
AGENT REQUEST → TOOL RUNTIME → CREDENTIAL RESOLVER → SCOPED CREDENTIAL → EXTERNAL CONNECTOR
```

### 5.2 Credential Record

| Field | Type | Required | Description |
|---|---|---|---|
| `credential_id` | string | yes | Unique credential identifier |
| `credential_type` | enum | yes | One of: `api_key`, `oauth2`, `basic`, `bearer`, `certificate`, `custom` |
| `secret_ref` | string | yes | Reference to secret (NEVER raw value) |
| `scope` | list[string] | yes | What this credential can access |
| `audience` | string | no | Intended audience |
| `expires_at` | datetime | yes | When credential expires |
| `rotation_policy` | string | no | Rotation schedule |
| `provider_binding` | string | yes | Bound to which provider |
| `business_binding` | string | yes | Bound to which business |
| `division_binding` | string | no | Bound to which division |
| `least_privilege` | boolean | yes | Whether minimal scope is enforced |
| `status` | enum | yes | One of: `active`, `expired`, `revoked`, `rotating` |
| `created_at` | datetime | yes | When created |
| `updated_at` | datetime | no | When last modified |
| `audit_ref` | string | yes | Audit trail reference |

### 5.3 Credential Rules

| Rule | Description |
|---|---|
| Never embed | Raw credentials prohibited in runtime objects |
| Reference only | Use `secret_ref` for vault reference |
| Scoped | Credentials scoped to business/division/provider |
| Least privilege | Minimal scope enforced |
| Rotation | Expiring credentials rotated per policy |
| Revocation | Revoked credentials immediately unusable |
| Masking | Credentials masked in logs/observability |
| Break-glass | Emergency access requires Governance approval |

---

## 6. External API Execution Contract

### 6.1 Execution State Machine

```
REQUESTED → AUTHORIZING → SENT → {ACCEPTED, REJECTED}
                                ↓
                        RESPONSE_RECEIVED → {VERIFIED, FAILED}
                                ↓
                        UNKNOWN_OUTCOME → RECONCILING → {RECONCILED, UNRESOLVABLE}
```

### 6.2 State Definitions

| State | Description |
|---|---|
| `REQUESTED` | External API request prepared |
| `AUTHORIZING` | Checking authorization |
| `SENT` | Request sent to external system |
| `ACCEPTED` | External system accepted request |
| `REJECTED` | External system rejected request |
| `RESPONSE_RECEIVED` | Response received from external |
| `VERIFIED` | Response verified, side effects confirmed |
| `FAILED` | Request definitively failed |
| `UNKNOWN_OUTCOME` | Outcome uncertain (timeout after potential side effect) |
| `RECONCILING` | Attempting to determine actual state |
| `RECONCILED` | State reconciled with external source |

### 6.3 External API Request

| Field | Type | Required | Description |
|---|---|---|---|
| `request_id` | string | yes | Unique request identifier |
| `connector_id` | string | yes | Connector to use |
| `provider_id` | string | yes | Provider to call |
| `operation` | string | yes | Operation to perform |
| `method` | string | yes | HTTP method or equivalent |
| `endpoint` | string | yes | Target endpoint |
| `headers` | map[string,string] | no | Request headers |
| `body` | any | no | Request body |
| `timeout_ms` | integer | yes | Request timeout |
| `idempotency_key` | string | conditionally | Required for side-effecting operations |
| `dry_run` | boolean | no | Whether to simulate |
| `business_id` | string | yes | Business scope |
| `division_id` | string | no | Division scope |
| `agent_id` | string | conditionally | Agent making request |
| `workflow_id` | string | conditionally | Workflow context |
| `task_id` | string | conditionally | Task context |
| `objective_id` | string | yes | Objective being served |
| `why` | string | yes | WHY context |
| `correlation_id` | string | yes | End-to-end trace |
| `causation_id` | string | no | What caused this request |
| `side_effect_class` | enum | yes | Side-effect classification |
| `classification` | enum | yes | Data classification |
| `created_at` | datetime | yes | When request was created |

### 6.4 External API Response

| Field | Type | Required | Description |
|---|---|---|---|
| `response_id` | string | yes | Unique response identifier |
| `request_id` | string | yes | Corresponding request |
| `status_code` | integer | yes | HTTP status or equivalent |
| `headers` | map[string,string] | no | Response headers |
| `body` | any | no | Response body |
| `latency_ms` | integer | yes | Response latency |
| `external_request_id` | string | no | External system's request ID |
| `external_resource_id` | string | no | External resource created/modified |
| `validation_status` | enum | yes | One of: `valid`, `invalid`, `schema_mismatch` |
| `trust_status` | enum | yes | One of: `untrusted`, `validated`, `trusted` |
| `received_at` | datetime | yes | When response received |

---

## 7. Side-Effect Classification Contract

### 7.1 Classification Categories

| Category | Description | Retry Safe | Idempotency Required |
|---|---|---|---|
| `READ_ONLY` | No side effects | Yes | No |
| `IDEMPOTENT_WRITE` | Write with idempotency | Yes (with key) | Yes |
| `NON_IDEMPOTENT_WRITE` | Write without idempotency | No | No (reconcile first) |
| `REVERSIBLE_SIDE_EFFECT` | Side effect that can be undone | With compensation | Recommended |
| `IRREVERSIBLE_SIDE_EFFECT` | Side effect that cannot be undone | No | Yes |
| `UNKNOWN_SIDE_EFFECT` | Unknown if side effect occurred | No | Reconcile first |

**Vocabulary mapping (cross-phase reconciliation):** the runtime/execution layer
(`SCHEMA_EXECUTION.md`, field `side_effect_classification`) uses the coarser values
`none`, `read_only`, `reversible`, `irreversible`. This external layer refines them
for boundary safety. Mapping:

| Runtime value | External classification(s) |
|---|---|
| `none` | `READ_ONLY` |
| `read_only` | `READ_ONLY` |
| `reversible` | `IDEMPOTENT_WRITE`, `REVERSIBLE_SIDE_EFFECT` |
| `irreversible` | `NON_IDEMPOTENT_WRITE`, `IRREVERSIBLE_SIDE_EFFECT` |
| (any, when outcome uncertain) | `UNKNOWN_SIDE_EFFECT` |

The runtime value governs internal retry eligibility; the external classification
governs boundary retry/reconciliation safety. Neither is a governance outcome and
neither grants authority.

### 7.2 Classification Rules

| Rule | Description |
|---|---|
| Pre-classify | Classify before execution |
| Conservative | Default to more dangerous category if uncertain |
| Never assume idempotent | Do not assume external API is idempotent |
| Declare capability | Connector declares idempotency capability |
| Verify capability | Verify connector's idempotency declaration |

---

## 8. Idempotency Contract

### 8.1 Idempotency Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `idempotency_key` | string | conditionally | Business-level idempotency |
| `operation_id` | string | yes | Operation identifier |
| `request_fingerprint` | string | no | Hash of request for dedup |
| `external_request_id` | string | no | External system's request ID |
| `external_resource_id` | string | no | External resource ID |
| `deduplication_key` | string | conditionally | Infrastructure-level dedup |
| `scope` | enum | yes | One of: `endpoint`, `operation`, `resource` |
| `retention_hours` | integer | yes | How long idempotency is retained |

### 8.2 Delivery Guarantees

| Guarantee | Description | Use Case |
|---|---|---|
| `AT_MOST_ONCE` | May lose, never duplicate | Non-critical notifications |
| `AT_LEAST_ONCE` | May duplicate, never lose | Most operations |
| `EFFECTIVELY_ONCE` | Processed exactly once | Through idempotent processing |

### 8.3 Idempotency Rules

| Rule | Description |
|---|---|
| No exactly-once claim | Do not claim exactly-once unless infrastructure guarantees it |
| Effectively-once | Use idempotent processing for effectively-once |
| Scope | Idempotency scope must be declared |
| Retention | Idempotency retention must be defined |
| Conflict | Duplicate request returns cached result |

---

## 9. Webhook Inbound Contract

### 9.1 Webhook Receipt Pipeline

```
RECEIVE → AUTHENTICATE → VALIDATE SIGNATURE → CHECK TIMESTAMP → CHECK REPLAY → VALIDATE PAYLOAD → NORMALIZE → DEDUPLICATE → SECURITY CHECK → POLICY CHECK → PROCESS → ACKNOWLEDGE
```

### 9.2 Webhook Record

| Field | Type | Required | Description |
|---|---|---|---|
| `webhook_id` | string | yes | Unique webhook identifier |
| `endpoint_id` | string | yes | Receiving endpoint |
| `connector_id` | string | yes | Connector identity |
| `source_identity` | string | yes | External source identity |
| `signature` | string | conditionally | Request signature |
| `timestamp` | datetime | yes | External event timestamp |
| `received_at` | datetime | yes | When received by NEXUS |
| `nonce` | string | conditionally | Replay protection nonce |
| `event_type` | string | yes | External event type |
| `event_id` | string | yes | External event ID |
| `delivery_id` | string | no | External delivery ID |
| `payload` | any | yes | Webhook payload |
| `payload_schema` | string | no | Expected payload schema |
| `correlation_id` | string | yes | End-to-end trace |
| `business_id` | string | yes | Business scope |
| `division_id` | string | no | Division scope |
| `deduplication_key` | string | yes | For dedup |
| `processing_state` | enum | yes | One of: `received`, `validating`, `processing`, `processed`, `failed`, `quarantined` |
| `classification` | enum | yes | Data classification |
| `provenance` | ProvenanceRef | yes | Origin record |

### 9.3 Webhook Rules

| Rule | Description |
|---|---|
| No auto-authority | Webhook receipt MUST NOT grant authority |
| Signature verification | Verify signature before processing |
| Timestamp validation | Reject stale webhooks |
| Replay protection | Detect and reject replays |
| Deduplication | Deduplicate by event_id |
| Schema validation | Validate payload against schema |
| Prompt injection | Treat webhook content as data, not instruction |
| Audit | All webhook receipt is auditable |
| Quarantine | Suspicious webhooks quarantined |

---

## 10. Webhook Outbound Contract

### 10.1 Outbound Webhook Pipeline

```
PREPARE → AUTHENTICATE → GENERATE PAYLOAD → SIGN → SEND → VALIDATE RESPONSE → RECORD DELIVERY
```

### 10.2 Outbound Webhook Record

| Field | Type | Required | Description |
|---|---|---|---|
| `delivery_id` | string | yes | Unique delivery identifier |
| `target_url` | string | yes | Target webhook URL |
| `target_identity` | string | yes | Target identity |
| `payload` | any | yes | Outgoing payload |
| `payload_schema` | string | yes | Schema version |
| `signature` | string | yes | Signed payload |
| `idempotency_key` | string | yes | Dedup key |
| `timeout_ms` | integer | yes | Delivery timeout |
| `max_retries` | integer | yes | Maximum retries |
| `status` | enum | yes | Delivery status |
| `business_id` | string | yes | Business scope |
| `correlation_id` | string | yes | End-to-end trace |
| `created_at` | datetime | yes | When created |
| `delivered_at` | datetime | no | When delivered |
| `next_retry_at` | datetime | no | Next retry time |
| `retry_count` | integer | yes | Current retry count |

---

## 11. Polling / Pull Integration Contract

### 11.1 Polling Record

| Field | Type | Required | Description |
|---|---|---|---|
| `poll_id` | string | yes | Unique poll identifier |
| `connector_id` | string | yes | Connector to poll |
| `cursor` | string | no | Current cursor position |
| `watermark` | string | no | Last seen timestamp/ID |
| `checkpoint` | string | no | Last checkpoint |
| `page_size` | integer | no | Page size |
| `interval_seconds` | integer | yes | Poll interval |
| `business_id` | string | yes | Business scope |
| `status` | enum | yes | One of: `active`, `paused`, `failed`, `completed` |
| `last_polled_at` | datetime | no | Last poll time |
| `next_poll_at` | datetime | no | Next poll time |

### 11.2 Polling Rules

| Rule | Description |
|---|---|
| Cursor preservation | Preserve cursor across restarts |
| Deduplication | Deduplicate across poll boundaries |
| Rate limiting | Respect provider rate limits |
| Failure handling | Pause on failure, resume with backoff |
| Ordering | Do not claim ordering if provider does not guarantee it |

---

## 12. Streaming Integration Contract

### 12.1 Streaming Record

| Field | Type | Required | Description |
|---|---|---|---|
| `stream_id` | string | yes | Unique stream identifier |
| `connector_id` | string | yes | Connector identity |
| `connection_id` | string | yes | Connection identity |
| `protocol` | enum | yes | Streaming protocol |
| `offset` | string | no | Current offset/cursor |
| `state` | enum | yes | One of: `connecting`, `connected`, `reconnecting`, `disconnected`, `draining` |
| `heartbeat_interval_ms` | integer | yes | Expected heartbeat |
| `business_id` | string | yes | Business scope |
| `last_heartbeat_at` | datetime | no | Last heartbeat |
| `created_at` | datetime | yes | When stream started |

### 12.2 Streaming Rules

| Rule | Description |
|---|---|
| Reconnect | Auto-reconnect on disconnect |
| Resume | Resume from last offset |
| Backpressure | Handle backpressure from provider |
| Ordering | Do not claim ordering if provider does not guarantee it |
| Deduplication | Deduplicate across reconnects |
| Gap detection | Detect and handle gaps in stream |
| Drain | Graceful drain on shutdown |

---

## 13. External Resource Mapping Contract

### 13.1 Resource Mapping Record

| Field | Type | Required | Description |
|---|---|---|---|
| `mapping_id` | string | yes | Unique mapping identifier |
| `provider_id` | string | yes | External provider |
| `connector_id` | string | yes | Connector identity |
| `nexus_resource_type` | string | yes | NEXUS resource type |
| `nexus_resource_id` | string | yes | NEXUS resource ID |
| `external_resource_type` | string | yes | External resource type |
| `external_resource_id` | string | yes | External resource ID |
| `scope` | string | yes | Business/division scope |
| `source_of_truth` | enum | yes | One of: `nexus`, `external`, `synced` |
| `created_at` | datetime | yes | When mapping created |
| `updated_at` | datetime | no | When mapping last updated |
| `stale` | boolean | yes | Whether mapping is stale |

### 13.2 Mapping Rules

| Rule | Description |
|---|---|
| Scope isolation | Mappings scoped to business |
| Stale detection | Detect and handle stale mappings |
| Conflict | Conflict resolution when source of truth differs |
| Reconciliation | Periodic reconciliation of mappings |
| Deletion | Mappings cleaned up when resources deleted |

---

## 14. Network / Egress Contract

### 14.1 Network Policy Record

| Field | Type | Required | Description |
|---|---|---|---|
| `policy_id` | string | yes | Unique policy identifier |
| `destination` | string | yes | Target host/domain |
| `protocol` | enum | yes | One of: `https`, `http`, `ws`, `wss`, `tcp`, `custom` |
| `port` | integer | no | Target port |
| `dns_policy` | enum | yes | One of: `allow_all`, `allowlist`, `denylist` |
| `ip_restrictions` | list[string] | no | IP allowlist/denylist |
| `tls_required` | boolean | yes | Whether TLS is required |
| `certificate_validation` | enum | yes | One of: `strict`, `skip`, `custom` |
| `redirect_policy` | enum | yes | One of: `follow`, `no_follow`, `same_host` |
| `ssrf_protection` | boolean | yes | Whether SSRF protection enabled |
| `private_network_allowed` | boolean | yes | Whether private network access allowed |
| `business_scope` | string | yes | Business scope |
| `created_at` | datetime | yes | When policy created |

### 14.2 Network Rules

| Rule | Description |
|---|---|
| TLS required | TLS required for all external connections |
| Certificate validation | Strict certificate validation by default |
| SSRF protection | SSRF protection enabled by default |
| Private network | Private network access disabled by default |
| Egress budget | Egress budget per business |
| Rate limiting | Rate limiting per destination |
| Audit | All egress is auditable |

---

## 15. Browser / Web Boundary Contract

### 15.1 Browser Execution Record

| Field | Type | Required | Description |
|---|---|---|---|
| `execution_id` | string | yes | Unique execution identifier |
| `destination_url` | string | yes | Target URL |
| `url_validated` | boolean | yes | Whether URL validated |
| `dns_policy_checked` | boolean | yes | Whether DNS policy checked |
| `ssrf_protected` | boolean | yes | Whether SSRF protected |
| `redirect_count` | integer | yes | Number of redirects followed |
| `cookies_cleared` | boolean | yes | Whether cookies cleared |
| `sandbox_enabled` | boolean | yes | Whether sandbox enabled |
| `javascript_disabled` | boolean | no | Whether JS disabled |
| `downloads_allowed` | boolean | yes | Whether downloads allowed |
| `business_id` | string | yes | Business scope |
| `classification` | enum | yes | Data classification |
| `created_at` | datetime | yes | When execution started |

### 15.2 Browser Rules

| Rule | Description |
|---|---|
| URL validation | Validate URL before navigation |
| SSRF protection | SSRF protection for all web requests |
| Sandbox | Sandbox untrusted web content |
| Cookie isolation | Isolate cookies per business/session |
| Prompt injection | Treat web content as data, not instruction |
| Data exfiltration | Prevent data exfiltration through web |
| Audit | All browser execution is auditable |

---

## 16. Database / Storage Boundary Contract

### 15.1 Database Access Record

| Field | Type | Required | Description |
|---|---|---|---|
| `access_id` | string | yes | Unique access identifier |
| `connection_id` | string | yes | Database connection |
| `database_id` | string | yes | Database identity |
| `namespace` | string | no | Database namespace |
| `business_id` | string | yes | Business scope |
| `division_id` | string | no | Division scope |
| `operation_type` | enum | yes | One of: `read`, `write`, `delete`, `ddl` |
| `query_authorized` | boolean | yes | Whether query authorized |
| `transaction_behavior` | enum | yes | One of: `none`, `auto`, `explicit` |
| `timeout_ms` | integer | yes | Query timeout |
| `created_at` | datetime | yes | When access started |

### 15.2 Database Rules

| Rule | Description |
|---|---|
| Authorization | Query authorization before execution |
| Scope isolation | Business/division scope isolation |
| Transaction | Transaction behavior declared |
| Timeout | Query timeout enforced |
| Unknown outcome | Unknown transaction outcome triggers reconciliation |
| Audit | All database access is auditable |
| DLP | DLP check before data egress |

---

## 17. Sandbox / Test Mode Contract

### 17.1 Execution Modes

| Mode | Description | Side Effects |
|---|---|---|
| `LIVE` | Production execution | Real side effects |
| `SANDBOX` | Isolated test environment | Sandbox side effects only |
| `DRY_RUN` | Simulate without execution | No side effects |
| `SIMULATION` | Full simulation | No real side effects |

### 17.2 Mode Rules

| Rule | Description |
|---|---|
| Explicit | Mode must be explicitly set |
| Propagate | Mode propagated through execution chain |
| Prevent live | Sandbox/dry-run prevents live side effects |
| Observable | Mode visible in all records |
| Audit | Mode transitions auditable |

---

## 18. DLP / Data Egress Contract

### 18.1 DLP Evaluation

Before external transmission, evaluate:

| Factor | Check |
|---|---|
| Data classification | PUBLIC, INTERNAL, CONFIDENTIAL, RESTRICTED |
| Destination | Is destination allowed? |
| Business scope | Is cross-business? |
| Purpose | Is purpose declared? |
| Policy | Does policy allow? |
| Sensitive fields | Are sensitive fields present? |
| Credential exposure | Are credentials exposed? |
| Personal data | Is personal/confidential data present? |
| Minimization | Is data minimized? |
| Redaction | Are sensitive fields redacted? |
| Approval | Is approval required? |

### 18.2 DLP Rules

| Rule | Description |
|---|---|
| Block on failure | DLP failure prevents transmission where policy requires |
| Classified data | Classified data requires DLP check |
| Audit | DLP decisions are auditable |
| Exceptions | DLP exceptions require Governance approval |

---

## 19. Integration Quarantine Contract

### 19.1 Quarantine Triggers

| Trigger | Description |
|---|---|
| Invalid signature | Webhook signature verification failed |
| Invalid schema | Payload does not match expected schema |
| Repeated failure | Processing failed repeatedly |
| Suspicious payload | Payload flagged as suspicious |
| Security violation | Security boundary violation |
| Unknown provider state | Provider state uncertain |
| Poisoned event | Event causes repeated failures |
| Unsupported version | Payload version not supported |
| Rate limit exhaustion | Rate limit repeatedly exhausted |
| Reconciliation failure | Reconciliation could not resolve |

### 19.2 Quarantine Record

| Field | Type | Required | Description |
|---|---|---|---|
| `quarantine_id` | string | yes | Unique quarantine identifier |
| `item_type` | string | yes | What was quarantined |
| `item_id` | string | yes | ID of quarantined item |
| `reason` | string | yes | Why quarantined |
| `evidence` | string | yes | Supporting evidence |
| `provenance` | ProvenanceRef | yes | Origin record |
| `created_at` | datetime | yes | When quarantined |
| `status` | enum | yes | One of: `quarantined`, `reviewing`, `released`, `destroyed` |

### 19.3 Quarantine Rules

| Rule | Description |
|---|---|
| Preserve evidence | Quarantine preserves evidence and provenance |
| No silent discard | Do not silently discard quarantined items |
| Review | Quarantined items require review |
| Audit | Quarantine decisions are auditable |

---

*This document defines the integration and external boundary contracts. Provider contracts are in PROVIDER_CONTRACTS.md.*
