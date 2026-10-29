# NEXUS — TrendSentry Live Activation Input Register

**Status:** `UNRESOLVED` — no live activation inputs have been supplied or verified.
**Prepared:** 2026-10-26
**Validated source baseline:** `origin/master` = `098267d279569e5f7ac6a934b8d78419a777baa8`.
**Companion documents:** [Owner handoff](owner-handoff.md) · [Live activation runbook](live-activation-runbook.md) · [Existing scripted runbook](../TRENDSENTRY_PILOT_RUNBOOK.md)

This register is the authoritative list of owner decisions and operator inputs required for live TrendSentry activation. It is deliberately a **register of decisions and secret names**, not a place to store secret values.

> **Do not write secret values in this file, in chat, in email, in tickets, or in commits.** Record only the environment-variable **name** that will carry each secret. The operator injects the value through the process supervisor or secret manager. Presence is verified with a non-printing check such as `printenv NAME >/dev/null`.

---

## 1. How to use this register

### 1.1 Status values

| Status | Meaning |
|---|---|
| `UNRESOLVED` | Not supplied. This is the default for every row. |
| `PROVIDED` | The owner supplied a value or decision, but it has not been independently verified. |
| `VERIFIED` | The operator or security reviewer verified the value against the environment, source, or provider. An evidence reference is required. |
| `REJECTED` | Supplied but not acceptable. Record the reason and the required correction. |
| `N/A` | Explicitly not applicable. A rationale is required. |

A row may not be marked `VERIFIED` without an evidence reference (command output, screenshot, signed record, ticket, or configuration hash). `PROVIDED` is not a pass.

### 1.2 Requirement levels

- **Required** — blocks Gate 2 (configuration/secret readiness) or the first live workflow.
- **Conditional** — required only if the owner chooses the option it belongs to (for example, a division-scoped execution or a Brave web-search provider).
- **Optional** — does not block the first workflow; the decision must still be recorded so it is not mistaken for an oversight.

### 1.3 Roles

| Role | Accountability |
|---|---|
| **Pilot Owner** | Business scope, first objective, acceptance criteria, risk acceptance, final GO/NO-GO. |
| **Platform Operator** | Configuration, secret injection, data directory, startup, backup/restore, evidence capture. |
| **Security Reviewer** | Production security gates, secret handling, network egress, policy/approval design, veto. |
| **Research Reviewer** | Human semantic review of the first live output and its cited sources. |
| **Engineering Lead** | Build/test integrity, source verification, defect escalation. |

---

## 2. Document control

| Field | Value | Status | Evidence / accountable |
|---|---|---|---|
| Pilot name | TrendSentry first live research workflow | `PROVIDED` | — |
| NEXUS installation ID (`nexus.id`) | `<UNRESOLVED>` | `UNRESOLVED` | Pilot Owner |
| Target environment | `production` | `UNRESOLVED` | Pilot Owner |
| Target host / deployment unit | `<UNRESOLVED>` | `UNRESOLVED` | Platform Operator |
| Change window | `<UNRESOLVED>` | `UNRESOLVED` | Pilot Owner |
| Data classification for first run | `<UNRESOLVED>` | `UNRESOLVED` | Pilot Owner + Security Reviewer |
| Written Gate 0 authorization | `<UNRESOLVED>` | `UNRESOLVED` | Pilot Owner |
| Register completed and verified | `<UNRESOLVED>` | `UNRESOLVED` | Platform Operator |

---

## 3. Required inputs — summary

| ID | Input | Level | Status | Evidence reference |
|---|---|---|---|---|
| `RF-01` | Installation ID (`nexus.id`) | Required | `UNRESOLVED` | |
| `RF-02` | Target environment is `production` | Required | `UNRESOLVED` | |
| `RF-03` | TrendSentry business display name | Required | `UNRESOLVED` | |
| `RF-04` | Stable business entity ID | Required for reproducibility | `UNRESOLVED` | |
| `RF-05` | Existing active human owner identity ID | Required | `UNRESOLVED` | |
| `RF-06` | Owner credential environment-variable **name** | Required | `UNRESOLVED` | |
| `RF-07` | Active research agent with `web.search` | Required | `UNRESOLVED` | |
| `RF-08` | Persistent `storage.data_dir`, host, and service account | Required | `UNRESOLVED` | |
| `RF-09` | Model provider registration | Required | `UNRESOLVED` | |
| `RF-10` | Provider fit, data terms, and spend control | Required | `UNRESOLVED` | |
| `RF-11` | Live web-search provider | Required | `UNRESOLVED` | |
| `RF-12` | Governance and approval decision | Required | `UNRESOLVED` | |
| `RF-13` | Control and bootstrap secret **names** | Required | `UNRESOLVED` | |
| `RF-14` | First live objective and human acceptance criteria | Required | `UNRESOLVED` | |
| `RF-15` | Backup, restore, and retention | Required for production | `UNRESOLVED` | |
| `RF-16` | Unknown-outcome owner and procedure | Required for production | `UNRESOLVED` | |
| `RF-17` | Success criteria | Required | `UNRESOLVED` | |
| `RF-18` | Stop and rollback criteria, and rollback authority | Required | `UNRESOLVED` | |
| `RF-19` | Final GO/NO-GO sign-off | Required | `UNRESOLVED` | |

---

## 4. Business, identity, division, and agent

### 4.1 `RF-01` — Installation identity

| Field | Value | Status |
|---|---|---|
| `nexus.id` | `<UNRESOLVED>` | `UNRESOLVED` |
| Stability requirement | Must remain stable across restarts and be unique to this installation | `PROVIDED` |
| Is it a secret? | No. Do not put credentials in it. | `PROVIDED` |

**Notes**

- `nexus.id` is required by configuration validation; an empty value aborts startup.
- Use a stable, human-readable identifier chosen by the owner. Do not reuse another installation's ID.

### 4.2 `RF-02` — Target environment

| Field | Value | Status |
|---|---|---|
| `nexus.environment` | `production` | `UNRESOLVED` |
| `NEXUS_ENVIRONMENT` process variable | `production` | `UNRESOLVED` |

**Notes**

- Set `NEXUS_ENVIRONMENT=production` in the process environment. Also set `nexus.environment` to `production` in the file so the two layers agree. If only the environment variable is set, confirm it overrides the file and record the divergence.
- Production validation requires a real model provider, non-empty `storage.data_dir`, `audit_enabled`, `require_authentication`, `enforce_business_scope`, and `sandbox_enabled` true, and `dev_allow_unsafe_overrides` false (see the config skeleton in §14).
- The capability platform reads `NEXUS_ENVIRONMENT` directly to decide production HTTP policy; the config file's environment alone is not sufficient for that layer.

### 4.3 `RF-03` — Business display name

| Field | Value | Status |
|---|---|---|
| Business name | `<UNRESOLVED>` | `UNRESOLVED` |
| Owner of the name/trademark decision | `<UNRESOLVED>` | `UNRESOLVED` |

The business name is a human label in the business record. It is not used as an authorization boundary.

### 4.4 `RF-04` — Stable business entity ID

| Field | Value | Status |
|---|---|---|
| Business entity ID (`entity_id`) | `<UNRESOLVED>` | `UNRESOLVED` |
| If omitted, accept a server-generated ID? | `<UNRESOLVED>` | `UNRESOLVED` |

**Notes**

- `POST /api/v1/businesses` accepts an optional `entity_id`. If omitted, the server generates one.
- A stable owner-chosen ID makes evidence, runbooks, and later automation reproducible. If the owner prefers a generated ID, record that decision explicitly and capture the returned ID.
- Do not use a display name as the entity ID unless the owner explicitly accepts it.

### 4.5 `RF-05` — Owner identity

| Field | Value | Status |
|---|---|---|
| Owner identity ID | `<UNRESOLVED>` | `UNRESOLVED` |
| Identity type | Must be an existing active human identity | `PROVIDED` |
| Provisioning option | `bootstrap` or `dedicated` | `UNRESOLVED` |
| If dedicated: creation procedure and approver | `<UNRESOLVED>` | `UNRESOLVED` |

**Verified options**

1. **Bootstrap identity** — `nx:human:bootstrap`, created by the runtime when identity enforcement is on and `NEXUS_BOOTSTRAP_CREDENTIAL` is set. It is a global human identity; `NEXUS_BOOTSTRAP_CREDENTIAL` is re-armed on every boot. This is the path exercised by the scripted E2E workflow.
2. **Dedicated human identity** — created through `POST /api/v1/identities` with `identity_type: "human"`, optionally with a `business_id`, `division_id`, `role`, and `credential`. Creating an identity inside a business requires the actor to hold business-wide membership in that business. A credential passed in the request body is a secret in transit; the owner must approve the handling method and the operator must use TLS.

**Decision required:** choose one option, record why, and record the credential handling method. Do not guess an identity ID.

### 4.6 `RF-06` — Owner credential environment-variable name

| Field | Value | Status |
|---|---|---|
| Environment-variable **name** | `<UNRESOLVED>` | `UNRESOLVED` |
| Rotation owner | `<UNRESOLVED>` | `UNRESOLVED` |
| Presence-check command (no value output) | `printenv <NAME> >/dev/null` | `PROVIDED` |

**Do not record the credential value.** If the bootstrap identity is used, the name is normally `NEXUS_BOOTSTRAP_CREDENTIAL`; confirm that choice rather than assuming it.

### 4.7 `RF-07` — Active research agent

| Field | Value | Status |
|---|---|---|
| Agent display name | `<UNRESOLVED>` | `UNRESOLVED` |
| Agent entity ID (`entity_id`) | `<UNRESOLVED>` (optional; server can generate) | `UNRESOLVED` |
| Division ID (optional) | `<UNRESOLVED>` | `UNRESOLVED` |
| Capabilities | `<UNRESOLVED>` (the scripted workflow used `["analysis"]`) | `UNRESOLVED` |
| Allowed tools | Must include `web.search` | `UNRESOLVED` |
| Acting agent selection | Explicit `objective.context.agent_id` or deterministic selector | `UNRESOLVED` |

**Notes**

- The research workflow needs an active agent whose allowed tools include `web.search`. An execution with no eligible agent has no agent-scoped tool permission.
- An explicit `objective.context.agent_id` must be active, owned by the execution's business, and division-compatible:
  - a business-wide agent (no division) is compatible with a business-scoped or division-scoped execution;
  - a division-scoped agent requires the execution to carry the same `division_id`.
- If explicit selection fails, the runtime does **not** fall back to a different agent. The execution proceeds without an acting agent, so agent-scoped tools (including `web.search`) are unavailable and the research workflow cannot complete as intended. Do not rely on fallback.
- The owner must decide whether the first agent is business-wide or division-scoped. Do not infer it from the E2E fixture.

### 4.8 `OD-01` — Division taxonomy (conditional / optional)

| Field | Value | Status |
|---|---|---|
| Use divisions for the first workflow? | `<UNRESOLVED>` | `UNRESOLVED` |
| Division name(s) | `<UNRESOLVED>` | `UNRESOLVED` |
| Division entity ID(s) | `<UNRESOLVED>` (optional; server can generate) | `UNRESOLVED` |
| Division owner identity ID(s) | `<UNRESOLVED>` | `UNRESOLVED` |
| Parent business ID(s) | `<UNRESOLVED>` | `UNRESOLVED` |

A division is **not required** for a business-scoped first workflow. It becomes required if the owner wants division-scoped agents, executions, policies, or evidence. If divisions are used, the agent and objective must agree on the division as described in §4.7.

### 4.9 `OD-02` — Additional agents and tools (optional)

| Field | Value | Status |
|---|---|---|
| Additional agents | `<UNRESOLVED>` | `UNRESOLVED` |
| Additional allowed tools | `<UNRESOLVED>` | `UNRESOLVED` |
| Capability review for each additional tool | `<UNRESOLVED>` | `UNRESOLVED` |

The first live workflow should use only `web.search`. Adding `http.request`, GitHub, filesystem sandbox, memory writes, or delegation expands the governance and security surface and requires a separate review.

---

## 5. Persistence, host, and service account

### 5.1 `RF-08` — Persistent data directory and host

| Field | Value | Status |
|---|---|---|
| `storage.data_dir` (absolute path) | `<UNRESOLVED>` | `UNRESOLVED` |
| Filesystem / mount | `<UNRESOLVED>` | `UNRESOLVED` |
| Service account (user:group) | `<UNRESOLVED>` | `UNRESOLVED` |
| Directory mode | `<UNRESOLVED>` (recommend `0750` or stricter) | `UNRESOLVED` |
| Config file mode | `<UNRESOLVED>` (recommend `0600`) | `UNRESOLVED` |
| Available capacity | `<UNRESOLVED>` | `UNRESOLVED` |
| Encryption at rest | `<UNRESOLVED>` | `UNRESOLVED` |
| Backup mount / destination | `<UNRESOLVED>` | `UNRESOLVED` |
| Host firewall / egress proxy | `<UNRESOLVED>` | `UNRESOLVED` |
| TLS termination | `<UNRESOLVED>` | `UNRESOLVED` |
| Process supervisor | `<UNRESOLVED>` | `UNRESOLVED` |
| Gateway bind address (`--http-addr`) | `<UNRESOLVED>` (default `health.port + 1`) | `UNRESOLVED` |
| Health bind address/port | `<UNRESOLVED>` (default `127.0.0.1:8080`) | `UNRESOLVED` |
| Log destination and retention | `<UNRESOLVED>` | `UNRESOLVED` |
| Time synchronization | `<UNRESOLVED>` | `UNRESOLVED` |

**Verified properties**

- `storage.data_dir` is the sole durability setting for organizational state and the execution journal. An empty value means in-memory operation and is rejected in production (`config.storage_missing`).
- The file store creates the directory with mode `0755` if it does not exist. The operator must tighten permissions after creation; the code does not enforce `0750` or `0600`.
- The store accepts relative paths, but an absolute, persistent, service-owned path is required operationally. Do not use a temporary directory, a container layer, or a path shared by multiple processes.
- Single process, single data directory. Multi-instance coordination is unsupported.
- Do not point the data directory at the repository, `workspaces/`, or a directory containing unrelated files.

### 5.2 Environment consistency

| Check | Required state | Status |
|---|---|---|
| `NEXUS_ENVIRONMENT` | `production` | `UNRESOLVED` |
| `NEXUS_DATA_DIR` | Unset, or identical to `storage.data_dir` | `UNRESOLVED` |
| `NEXUS_SEEDED_PROVIDER_MODE` | Unset or not `scripted` | `UNRESOLVED` |
| `NEXUS_CAPABILITY_WEB` | `wikipedia` or `brave`; never `scripted` | `UNRESOLVED` |
| `NEXUS_BOOTSTRAP_BUSINESS` | Unset (default `default`) unless explicitly decided | `UNRESOLVED` |

**Notes**

- `NEXUS_DATA_DIR` overrides `storage.data_dir`. Divergence is a configuration defect.
- `NEXUS_SEEDED_PROVIDER_MODE=scripted` is development-only. When a real provider is configured, the launcher registers the real provider and skips the seed, but the operator must still prevent scripted mode from being present in a production environment.
- `NEXUS_CAPABILITY_WEB=scripted` is **not** rejected by production configuration validation. The operator and Security Reviewer must enforce the live provider choice; a scripted search would produce plausible-looking but non-live evidence.
- `NEXUS_BOOTSTRAP_BUSINESS` changes the bootstrap-created business ID. Setting it to the TrendSentry business ID creates that business at boot with the bootstrap identity as owner and an `ADMIN` membership; it bypasses the explicit onboarding/membership path. The preferred pilot path is to leave it at `default` and onboard TrendSentry explicitly. Record any different decision.

---

## 6. Model provider

### 6.1 `RF-09` — Provider registration

| Field | Config key | Value | Status |
|---|---|---|---|
| Provider ID | `models.providers[].id` | `<UNRESOLVED>` | `UNRESOLVED` |
| Provider display name | `models.providers[].name` | `<UNRESOLVED>` (optional) | `UNRESOLVED` |
| Base endpoint | `models.providers[].endpoint` | `<UNRESOLVED>` | `UNRESOLVED` |
| Exact model ID | `models.providers[].model_id` | `<UNRESOLVED>` | `UNRESOLVED` |
| API-key environment-variable **name** | `models.providers[].api_key_env` | `<UNRESOLVED>` | `UNRESOLVED` |
| Request timeout | `models.providers[].timeout_ms` | `<UNRESOLVED>` (optional; default 30,000 ms) | `UNRESOLVED` |
| Allow insecure HTTP | `models.providers[].allow_insecure_http` | `false` (except approved loopback) | `UNRESOLVED` |
| Capabilities | `models.providers[].capabilities` | `<UNRESOLVED>` (optional; defaults below) | `UNRESOLVED` |
| Max request bytes | `models.providers[].max_request_bytes` | `<UNRESOLVED>` (optional) | `UNRESOLVED` |
| Max response bytes | `models.providers[].max_response_bytes` | `<UNRESOLVED>` (optional) | `UNRESOLVED` |

**Verified adapter requirements**

- The endpoint is a **base URL**. NEXUS appends `/chat/completions`, so the final request is `POST {endpoint}/chat/completions`. Do not include `/chat/completions` in the endpoint value.
- The adapter sends `Authorization: Bearer <value of api_key_env>`.
- The endpoint must be `https` unless it is a loopback address and `allow_insecure_http` is explicitly true. Plain HTTP to a non-loopback host is refused at startup.
- `api_key_env` is required. The value is read from the process environment at provider construction and at invocation; it is never stored in the config file.
- The default capability set when `capabilities` is omitted is `reasoning`, `tool_calling`, `structured_output`. The agent intelligence loop requires structured output; omitting the field is acceptable only if the model really supports it.
- The file-config key is `timeout_ms` (milliseconds). The internal runtime duration is derived from it. Do not use a string such as `"30s"` in the file.
- The launcher logs `production model provider registered` with the provider ID, model ID, and endpoint when registration succeeds. Production refuses to start with no provider (`config.provider_missing`).

### 6.2 `RF-10` — Provider fit, data terms, and spend control

| Field | Value | Status |
|---|---|---|
| Model supports OpenAI-compatible chat completions | `<UNRESOLVED>` | `UNRESOLVED` |
| Model supports strict JSON-schema structured output | `<UNRESOLVED>` | `UNRESOLVED` |
| Model supports tool calling | `<UNRESOLVED>` | `UNRESOLVED` |
| Context window sufficient for the bounded prompt and search results | `<UNRESOLVED>` | `UNRESOLVED` |
| Provider data-retention terms accepted | `<UNRESOLVED>` | `UNRESOLVED` |
| Regional processing / data residency accepted | `<UNRESOLVED>` | `UNRESOLVED` |
| Provider account spend limit | `<UNRESOLVED>` | `UNRESOLVED` |
| Spend-limit owner and alert threshold | `<UNRESOLVED>` | `UNRESOLVED` |
| Provider-side usage-log access | `<UNRESOLVED>` | `UNRESOLVED` |
| Fallback provider (optional) | `<UNRESOLVED>` | `UNRESOLVED` |

**Critical:** NEXUS does not enforce a monetary cost cap. Runtime budgets bound iterations (default 8), model calls (16), tool calls (8), delegations (3), replans (2), steps (12), and execution time (60 seconds). They do not bound dollars. The provider account must have a hard spend limit, and the owner must monitor it.

If the provider does not offer usage logs, the owner must either choose a provider that does or explicitly accept NEXUS-side trace plus durable evidence as the available proof of live calls. Record that risk acceptance.

---

## 7. Live web search

### 7.1 `RF-11` — Provider choice

| Field | Value | Status |
|---|---|---|
| Provider | `wikipedia` or `brave` | `UNRESOLVED` |
| `NEXUS_CAPABILITY_WEB` value | Must match the choice above | `UNRESOLVED` |
| Provider terms / rate limits accepted | `<UNRESOLVED>` | `UNRESOLVED` |
| Coverage limitation accepted | `<UNRESOLVED>` | `UNRESOLVED` |

**Verified options**

| Provider | Credential | Default endpoint | Coverage |
|---|---|---|---|
| `wikipedia` | None | `https://en.wikipedia.org/w/api.php` | Wikipedia only. Language is configurable; the default is English. |
| `brave` | `BRAVE_API_KEY` | `https://api.search.brave.com/res/v1/web/search` | General web search, subject to the Brave account plan and terms. |
| `scripted` | None | Internal deterministic fixture | **Validation only. Not live.** |

**Additional optional settings**

| Field | Environment variable | Value | Status |
|---|---|---|---|
| Endpoint override | `NEXUS_CAPABILITY_WEB_ENDPOINT` | `<UNRESOLVED>` | `UNRESOLVED` |
| Wikipedia language | `NEXUS_CAPABILITY_WEB_LANGUAGE` | `<UNRESOLVED>` (default `en`) | `UNRESOLVED` |
| User agent | `NEXUS_CAPABILITY_WEB_USER_AGENT` | `<UNRESOLVED>` | `UNRESOLVED` |
| Allow insecure HTTP | `NEXUS_CAPABILITY_WEB_ALLOW_INSECURE` | `false` | `UNRESOLVED` |

**Notes**

- `NEXUS_CAPABILITY_WEB` is a process environment variable, not a config-file field.
- An unknown provider value fails boot. A typo cannot silently fall back to scripted retrieval.
- `NEXUS_CAPABILITY_WEB=scripted` is accepted by the launcher in any environment, including production. Production configuration validation does not block it. Gate 2 must explicitly verify the value and Gate 6 must verify live source evidence.
- The `web.search` tool registration event carries the provider name, but the current SSE stream forwards only events whose `business_id` matches the requested business; the web-search registration event is unscoped. Do not rely on SSE for provider proof. Use the process environment, live source domains, and provider-side logs.

---

## 8. Governance and approvals

### 8.1 `RF-12` — Approval decision

| Field | Value | Status |
|---|---|---|
| Require human approval before `web.search`? | `yes` / `no` | `UNRESOLVED` |
| Policy ID | `<UNRESOLVED>` | `UNRESOLVED` |
| Approver type | `human` (recommended) | `UNRESOLVED` |
| Approver identity ID(s) | `<UNRESOLVED>` | `UNRESOLVED` |
| Approval timeout (seconds) | `<UNRESOLVED>` | `UNRESOLVED` |
| Auto-deny on timeout | `<UNRESOLVED>` | `UNRESOLVED` |
| Self-approval prohibited | `<UNRESOLVED>` | `UNRESOLVED` |
| Delegation allowed | `<UNRESOLVED>` | `UNRESOLVED` |
| If no approval: alternative controls and rationale | `<UNRESOLVED>` | `UNRESOLVED` |

**Verified policy behavior**

- Policies are managed through the control API: `PUT /api/v1/control/policies/{id}` with `X-API-Key`.
- Policies are **process-lifetime state**. They do not survive a restart. The built-in `default-allow` policy returns after restart. The operator must reapply the approval policy after every boot and verify it before admitting new work.
- The built-in `default-allow` policy is read-only; `PUT`/`DELETE` return `409`.
- Governance is default-deny when no policy matches. The seeded default-allow allows requests so an unconfigured installation can operate.
- The approval endpoint requires **business-wide membership**. If self-approval is prohibited, the requester cannot approve their own action; a second authorized human with business-wide membership must exist.
- An approval decision requires a non-empty `reason` in the request body.
- An approved execution resumes asynchronously; the approve endpoint returns `202`, and the final state is observed through the result or durable read.
- Approval pauses do not survive a restart. A paused execution is journaled as terminal `failed` with `error_class: APPROVAL_REQUIRED`; after restart the approval snapshot is gone and the paused execution is not resumable through that path. A fresh governance admission is required.

### 8.2 Verified matching policy shape

The following shape was verified against the governance matcher for a `web.search` tool call:

- action: `action_type: "custom"` with `action_ids: ["tool_call"]`
- resource: `resource_type: "tool"` with `resource_ids: ["web.search"]`
- effect: `"REQUIRE_APPROVAL"`
- `approval_config` is required when the effect is `REQUIRE_APPROVAL`

A complete example is in §15.1. The server derives `policy_id` (from the path if omitted), `entity_type`, `schema_version`, `nexus_id`, `policy_version`, `effective_from`, `provenance`, `created_at`, and `created_by`; the operator supplies the decision fields.

**Most-restrictive-wins:** `REQUIRE_APPROVAL` is more restrictive than `ALLOW`, so the approval policy wins over the built-in default-allow even when precedence differs. Precedence is still recorded for clarity. Do not use it to weaken a requirement.

### 8.3 `OD-05` — Additional policies (optional)

| Policy | Purpose | Status |
|---|---|---|
| Deny or escalate specific tools | `<UNRESOLVED>` | `UNRESOLVED` |
| Data-access restrictions | `<UNRESOLVED>` | `UNRESOLVED` |
| Model-usage restrictions | `<UNRESOLVED>` | `UNRESOLVED` |
| Time-window restrictions | `<UNRESOLVED>` | `UNRESOLVED` |
| Business/division/agent scoping | `<UNRESOLVED>` | `UNRESOLVED` |

The first live workflow needs only the `web.search` approval decision. Additional policies are optional but must not be assumed.

---

## 9. Secret names and non-secret environment variables

### 9.1 `RF-13` — Secret names (names only)

| Secret purpose | Environment-variable name | Injected by | Rotation owner | Presence check | Status |
|---|---|---|---|---|---|
| Control-plane API key | `NEXUS_CONTROL_API_KEY` | Platform Operator | `<UNRESOLVED>` | `printenv NEXUS_CONTROL_API_KEY >/dev/null` | `UNRESOLVED` |
| Bootstrap identity credential | `NEXUS_BOOTSTRAP_CREDENTIAL` | Platform Operator | `<UNRESOLVED>` | `printenv NEXUS_BOOTSTRAP_CREDENTIAL >/dev/null` | `UNRESOLVED` |
| Model provider API key | `<UNRESOLVED>` (must equal `api_key_env`) | Platform Operator | `<UNRESOLVED>` | `printenv <NAME> >/dev/null` | `UNRESOLVED` |
| Brave Search API key (if `brave`) | `BRAVE_API_KEY` | Platform Operator | `<UNRESOLVED>` | `printenv BRAVE_API_KEY >/dev/null` | `UNRESOLVED` |

No value belongs in this table. If a value is accidentally exposed, stop, rotate it, and record the incident.

### 9.2 Non-secret environment variables

| Variable | Required value | Status |
|---|---|---|
| `NEXUS_ENVIRONMENT` | `production` | `UNRESOLVED` |
| `NEXUS_DATA_DIR` | Unset, or equal to `storage.data_dir` | `UNRESOLVED` |
| `NEXUS_CAPABILITY_WEB` | `wikipedia` or `brave` | `UNRESOLVED` |
| `NEXUS_CAPABILITY_WEB_LANGUAGE` | Optional; default `en` | `UNRESOLVED` |
| `NEXUS_CAPABILITY_WEB_USER_AGENT` | Optional | `UNRESOLVED` |
| `NEXUS_CAPABILITY_WEB_ENDPOINT` | Optional; default per provider | `UNRESOLVED` |
| `NEXUS_CAPABILITY_WEB_ALLOW_INSECURE` | `false` (default) | `UNRESOLVED` |
| `NEXUS_SEEDED_PROVIDER_MODE` | Unset or not `scripted` | `UNRESOLVED` |
| `NEXUS_BOOTSTRAP_BUSINESS` | Unset unless explicitly decided | `UNRESOLVED` |

---

## 10. First live objective and acceptance criteria

### 10.1 `RF-14` — Objective

| Field | Value | Status |
|---|---|---|
| Business ID | `<UNRESOLVED>` | `UNRESOLVED` |
| Division ID (optional) | `<UNRESOLVED>` | `UNRESOLVED` |
| Acting agent ID | `<UNRESOLVED>` | `UNRESOLVED` |
| Objective description (≤ 4,000 characters) | `<UNRESOLVED>` | `UNRESOLVED` |
| Human acceptance criteria | `<UNRESOLVED>` | `UNRESOLVED` |
| Research Reviewer | `<UNRESOLVED>` | `UNRESOLVED` |
| Evidence-redaction requirements | `<UNRESOLVED>` | `UNRESOLVED` |

The objective description is required and is bounded at 4,000 characters. It must be a real, bounded research question with a named human reviewer. Do not use a synthetic question for the production first run unless the owner explicitly accepts that limitation.

### 10.2 Optional objective fields

| Field | Value | Status |
|---|---|---|
| Success criteria expressions | `<UNRESOLVED>` | `UNRESOLVED` |
| Constraints | `<UNRESOLVED>` | `UNRESOLVED` |
| Budget tightening | `<UNRESOLVED>` | `UNRESOLVED` |

**Notes**

- The runtime caps are: 8 iterations, 16 model calls, 8 tool calls, 3 delegations, 2 replans, 12 steps, 60 seconds execution, depth 2, 4 parallel nodes. A request budget can only tighten these caps, never raise them.
- If `max_execution_time` is set in JSON, it is a Go duration integer in nanoseconds, not a string. Omitting the budget is simpler and uses the caps.
- Success-criteria expressions are contract-defined. Do not invent an expression syntax; if the owner wants criteria beyond human review, the Engineering Lead must confirm the supported expressions first.

---

## 11. Backup, restore, retention, and unknown outcomes

### 11.1 `RF-15` — Backup and restore

| Field | Value | Status |
|---|---|---|
| Backup owner | `<UNRESOLVED>` | `UNRESOLVED` |
| Backup destination | `<UNRESOLVED>` | `UNRESOLVED` |
| Backup cadence | `<UNRESOLVED>` | `UNRESOLVED` |
| Retention period | `<UNRESOLVED>` | `UNRESOLVED` |
| Encryption at rest / key owner | `<UNRESOLVED>` | `UNRESOLVED` |
| Restore-test window | `<UNRESOLVED>` | `UNRESOLVED` |
| Restore-test approver | `<UNRESOLVED>` | `UNRESOLVED` |
| Quiescence procedure | Stop the process before backup | `PROVIDED` |

**Verified backup commands**

```bash
# Build once
make build-backup

# The application must be stopped first.
./bin/nexus-backup backup  -data-dir "$DATA_DIR" -out "$BACKUP_DIR/nexus-$(date -u +%Y%m%dT%H%M%SZ).tar"
./bin/nexus-backup verify  -in "$BACKUP_DIR/nexus-....tar"
./bin/nexus-backup restore -in "$BACKUP_DIR/nexus-....tar" -dest "$RESTORE_DIR"
```

**Verified properties**

- The backup artifact is a tar with a manifest and store files. It never includes `workspaces/`, temp files, `.env` files, or raw secrets. It does contain credential verification hashes, so protect the artifact as sensitive.
- The tool refuses to overwrite an existing artifact and refuses a symlinked store root.
- Restore requires an empty or missing destination and never overwrites data.
- Restore reconstructs durable organization state and the execution journal, but **not** running executions, in-memory results, approval/resume snapshots, cancellation state, escalation state, event buffers, or traces.
- Retention is operator-run and offline; never run it against a live store.

A backup is not accepted until `verify` succeeds and a restore into an isolated directory has been exercised.

### 11.2 `RF-16` — Unknown-outcome owner and procedure

| Field | Value | Status |
|---|---|---|
| Unknown-outcome owner | `<UNRESOLVED>` | `UNRESOLVED` |
| Escalation contact | `<UNRESOLVED>` | `UNRESOLVED` |
| Evidence to preserve | Durable record, audit, trace (before restart), logs, provider usage | `UNRESOLVED` |
| Decision authority for a fresh admission | `<UNRESOLVED>` | `UNRESOLVED` |
| Customer/owner notification requirement | `<UNRESOLVED>` | `UNRESOLVED` |

**Verified semantics**

- `unknown` means the runtime cannot know whether an in-flight effect occurred. It is terminal for recovery and is never auto-retried or auto-resumed.
- A human must inspect the durable record and invocation evidence, decide whether the effect may have occurred, and make a fresh governance decision before resubmitting.
- Do not delete the record, journal, or evidence to make the state disappear.

### 11.3 `OD-07` — Retention (optional)

| Field | Value | Status |
|---|---|---|
| Retention enabled? | `<UNRESOLVED>` | `UNRESOLVED` |
| Minimum age | `<UNRESOLVED>` | `UNRESOLVED` |
| Operator/approver | `<UNRESOLVED>` | `UNRESOLVED` |
| Quiescence window | `<UNRESOLVED>` | `UNRESOLVED` |

Retention is disabled by default and has no background scheduler. `unknown` and nonterminal records are always retained.

---

## 12. Success, stop, rollback, and sign-off

### 12.1 `RF-17` — Success criteria

| Criterion | Required value | Status |
|---|---|---|
| Execution reaches terminal `completed` | yes | `UNRESOLVED` |
| `outcome.summary` contains `state=completed` | yes | `UNRESOLVED` |
| `research_synthesis.status` is `complete` | yes | `UNRESOLVED` |
| At least one evidence-backed statement with `source_ids` | yes | `UNRESOLVED` |
| `research_evaluation.status` is `passed` or accepted `warnings` | yes | `UNRESOLVED` |
| Citation audit returns `passed` or accepted `warnings` | yes | `UNRESOLVED` |
| Human reviewer accepts the source support | yes | `UNRESOLVED` |
| No policy bypass, no unauthorized tool, no secret exposure | yes | `UNRESOLVED` |
| Spend within the provider cap | yes | `UNRESOLVED` |
| Latency within the owner's threshold | `<UNRESOLVED>` | `UNRESOLVED` |

`warnings` is not an automatic pass. The Research Reviewer and Pilot Owner must explicitly accept the specific warnings and record why.

### 12.2 `RF-18` — Stop and rollback criteria

| Trigger | Action | Status |
|---|---|---|
| Any `unknown` execution | Stop new work; investigate; no auto-retry | `UNRESOLVED` |
| Missing or inactive approval policy when approval is required | Stop; reapply and verify before new work | `UNRESOLVED` |
| Citation audit `failed`, `incomplete`, `unavailable`, or `incompatible` | Stop; preserve evidence; review | `UNRESOLVED` |
| Human source review rejects support | Stop; treat output as not accepted | `UNRESOLVED` |
| Provider spend cap reached or forecast to be reached | Stop; notify owner | `UNRESOLVED` |
| Suspected secret exposure | Stop; rotate; incident process | `UNRESOLVED` |
| Unauthorized access, anomalous 401/403, or foreign-scope access | Stop; security review | `UNRESOLVED` |
| Data-directory error, corrupt record, or process not ready | Stop; do not delete evidence | `UNRESOLVED` |
| Multiple processes against one data directory | Stop all but one; investigate | `UNRESOLVED` |
| Owner or Security Reviewer calls stop | Stop immediately | `UNRESOLVED` |

**Rollback procedure (to be verified in Gate 8)**

1. Stop the NEXUS process gracefully; confirm no listener remains.
2. Preserve logs, durable records, audits, traces, and provider usage evidence. Do not delete the data directory.
3. If the store is corrupt or the activation must be reversed, restore the last verified backup into an isolated directory and start with that directory.
4. If credential exposure is suspected, rotate the model key, `BRAVE_API_KEY` (if used), `NEXUS_CONTROL_API_KEY`, and `NEXUS_BOOTSTRAP_CREDENTIAL`.
5. Reapply governance policies after any restart; verify the approval policy is active before admitting new work.
6. Notify the Pilot Owner and Security Reviewer. Record the incident and the decision.

### 12.3 `RF-19` — Final sign-off

| Decision | Name | Role | Date | Record reference |
|---|---|---|---|---|
| Inputs complete and verified | | Platform Operator | | |
| Security review complete | | Security Reviewer | | |
| First objective and acceptance criteria approved | | Pilot Owner + Research Reviewer | | |
| Backup/restore verified | | Platform Operator | | |
| Gate 0 authorized | | Pilot Owner | | |
| Final production GO/NO-GO | | Pilot Owner | | |

**Current decision: NO-GO.** No live provider call, deployment, or production data use is authorized until Gate 0 and Gates 1–8 pass.

---

## 13. Optional decisions

| ID | Decision | Default if not decided | Status |
|---|---|---|---|
| `OD-01` | Division taxonomy | Business-scoped first workflow; no divisions | `UNRESOLVED` |
| `OD-02` | Additional agents/tools | Only the research agent and `web.search` | `UNRESOLVED` |
| `OD-03` | Rate limits | Keep enabled with the shipped values (60 submit/min, 600 request/min, 6000 per-IP/min) | `UNRESOLVED` |
| `OD-04` | CORS allowed origins | Empty (same-origin only) | `UNRESOLVED` |
| `OD-05` | Additional governance policies | Only the web.search approval decision | `UNRESOLVED` |
| `OD-06` | Secondary model provider/fallback | None | `UNRESOLVED` |
| `OD-07` | Retention | Disabled; operator-run only | `UNRESOLVED` |
| `OD-08` | Memory settings | Shipped defaults; no memory writes in first workflow | `UNRESOLVED` |
| `OD-09` | Logging level/format | `info` / `json` for evidence capture | `UNRESOLVED` |
| `OD-10` | `NEXUS_BOOTSTRAP_BUSINESS` override | Unset (default `default`) | `UNRESOLVED` |
| `OD-11` | Provider payload bounds | Omit; adapter defaults | `UNRESOLVED` |
| `OD-12` | Monitoring/on-call | `<UNRESOLVED>` | `UNRESOLVED` |
| `OD-13` | Evidence redaction/data classification | `<UNRESOLVED>` | `UNRESOLVED` |
| `OD-14` | Validation-spec reproducibility | Accept the recorded workspace evidence; commit the two E2E specs only under a separately authorized change | `UNRESOLVED` |

Defaulting is not the same as deciding. The owner should record the default explicitly if it is acceptable.

---

## 14. Redacted live configuration skeleton

This skeleton shows the verified config-file field names for a live production shape. It is **not** a runnable file while placeholders remain. It contains no secret values: the model key is referenced by the name of an environment variable.

```json
{
  "nexus": {
    "id": "<NEXUS_INSTALLATION_ID>",
    "environment": "production"
  },
  "logging": {
    "level": "info",
    "format": "json"
  },
  "health": {
    "enabled": true,
    "host": "127.0.0.1",
    "port": 8080
  },
  "lifecycle": {
    "shutdown_timeout_seconds": 30
  },
  "storage": {
    "data_dir": "<ABSOLUTE_DATA_DIR>"
  },
  "security": {
    "audit_enabled": true,
    "require_authentication": true,
    "enforce_business_scope": true,
    "dev_allow_unsafe_overrides": false,
    "egress_allow_list": [],
    "sandbox_enabled": true
  },
  "models": {
    "providers": [
      {
        "id": "<PROVIDER_ID>",
        "name": "<PROVIDER_DISPLAY_NAME>",
        "endpoint": "https://<PROVIDER_BASE_URL>",
        "model_id": "<EXACT_MODEL_ID>",
        "api_key_env": "<PROVIDER_API_KEY_ENV_NAME>",
        "timeout_ms": 30000,
        "allow_insecure_http": false,
        "capabilities": [
          "reasoning",
          "tool_calling",
          "structured_output"
        ]
      }
    ]
  },
  "rate_limit": {
    "enabled": true,
    "submit_per_minute": 60,
    "request_per_minute": 600,
    "per_ip_per_minute": 6000
  },
  "http": {
    "cors_allowed_origins": []
  }
}
```

**Checklist for this skeleton**

- Replace every `<...>` placeholder. Gate 2 rejects any remaining placeholder.
- Keep `seeded_provider_mode` absent (or explicitly not `scripted`). A live provider is registered from `models.providers`.
- `endpoint` is a base URL; do not append `/chat/completions`.
- `api_key_env` names an environment variable; it is not the key.
- `timeout_ms` is milliseconds. Omit it to use the 30-second adapter default.
- `allow_insecure_http: true` is only acceptable for a Security-approved loopback exception.
- `security.egress_allow_list` is shown empty because it is not wired to the model/web-search adapters. Enforce egress outside NEXUS.
- Do not add unknown fields: the config loader rejects them.

---

## 15. Governance policy example

### 15.1 `PUT` request

```bash
curl -sS -X PUT "$BASE/api/v1/control/policies/trendsentry-websearch-approval" \
  -H "X-API-Key: ${NEXUS_CONTROL_API_KEY:?}" \
  -H "Content-Type: application/json" \
  -d '{
    "policy_type": "tool_usage",
    "name": "TrendSentry web.search approval",
    "description": "Require a human decision before any live web.search tool call.",
    "status": "active",
    "subject": { "subject_type": "all" },
    "action": { "action_type": "custom", "action_ids": ["tool_call"] },
    "resource": { "resource_type": "tool", "resource_ids": ["web.search"] },
    "effect": "REQUIRE_APPROVAL",
    "approval_config": {
      "approver_type": "human",
      "timeout_seconds": 3600,
      "auto_deny_on_timeout": true,
      "self_approval_prohibited": true,
      "delegation_allowed": false
    },
    "precedence": 1000
  }'
```

### 15.2 Verified matching semantics

- `action_type: "custom"` with `action_ids: ["tool_call"]` matches the runtime's `tool_call` action.
- `resource_type: "tool"` with `resource_ids: ["web.search"]` matches the `web.search` capability.
- `REQUIRE_APPROVAL` produces a pending approval and an `APPROVAL_REQUIRED` failure with `error.details.approval_id` on the execution result.
- The approve endpoint is `POST /api/v1/approvals/{id}/approve?business_id=...` with `{"reason": "..."}`; it returns `202` and resumes asynchronously.
- The list endpoint is `GET /api/v1/approvals?business_id=...`; the approval identifier is `approvals[].entity_id`.
- Policies are process-lifetime state. Reapply after every restart.

### 15.3 Owner decisions embedded in the example

- `self_approval_prohibited: true` requires a second authorized human with business-wide membership. If the pilot has only one human, the owner must either provide a second approver or explicitly set this to `false` and accept the weaker control.
- `timeout_seconds: 3600` and `auto_deny_on_timeout: true` are examples. The owner must choose and record the timeout.
- `approver_ids` is omitted, which means any authorized business-wide member can decide. If the owner wants named approvers, add `approver_ids` and verify the behavior with the Security Reviewer.

---

## 16. Register completion checklist

Before Gate 2 can pass:

- [ ] Every `RF-*` row is `VERIFIED` or `N/A` with a recorded rationale.
- [ ] Every `OD-*` row has a recorded decision or an explicitly accepted default.
- [ ] No secret value appears in this register, the config file, chat, tickets, or commits.
- [ ] Every secret environment-variable name in the config exists in the process environment; presence was checked without printing values.
- [ ] The config file has no remaining `<...>` placeholders and passes `jq empty`.
- [ ] `nexus.environment` is `production`; `NEXUS_ENVIRONMENT` is consistent.
- [ ] `storage.data_dir` is absolute, persistent, service-owned, and not shared with another process.
- [ ] The model provider uses HTTPS (or an approved loopback exception), an exact model ID, and `api_key_env`.
- [ ] The model provider account has a hard spend limit.
- [ ] `NEXUS_CAPABILITY_WEB` is `wikipedia` or `brave`, never `scripted`.
- [ ] The governance decision is recorded; if approval is required, a second authorized approver exists or self-approval is explicitly allowed.
- [ ] The first objective and human acceptance criteria are approved.
- [ ] Backup, restore, unknown-outcome, stop, and rollback procedures are approved.
- [ ] The final GO/NO-GO decision is signed.

Until this checklist is complete, live-provider readiness is **BLOCKED** and production activation remains **NO-GO**.
