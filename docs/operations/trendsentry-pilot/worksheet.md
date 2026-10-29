# NEXUS — TrendSentry Pilot Parameter Worksheet

**Status:** Worksheet for operator-supplied values. All values marked `<PLACEHOLDER>` are **unresolved** and must be supplied by the pilot owner before the pilot can run. Do not invent values. Do not record real credentials.

This worksheet is paired with `docs/operations/trendsentry-pilot/config.example.json`. Every placeholder here corresponds to a placeholder in that configuration file.

---

## A. Business and identity

| Parameter | Placeholder | Source in config | Owner-supplied? |
|---|---|---|---|
| NEXUS installation ID | `<NEXUS_INSTALLATION_ID>` | `nexus.id` | Yes |
| TrendSentry business name | `<BUSINESS_NAME>` | `businesses[].name` (runtime, via API) | Yes |
| Business entity ID | `<BUSINESS_ENTITY_ID>` | `businesses[].entity_id` (runtime, via API) | System-generated (auto) |
| Owner identity ID | `<OWNER_IDENTITY_ID>` | `businesses[].owner_identity_id` (runtime, via API) | Yes — an existing active human identity |
| Owner identity credential | `<OWNER_CREDENTIAL>` | `NEXUS_BOOTSTRAP_CREDENTIAL` env or runtime-registered credential | Yes |

**Rule:** `owner_identity_id` must reference an existing, active, human identity (SCHEMA_IDENTITIES_ORG §3.1). It is descriptive, not authority — being "owner" does not grant permission. Governance resolves authority.

---

## B. Division and agent

| Parameter | Placeholder | Source in config | Owner-supplied? |
|---|---|---|---|
| Division name | `<DIVISION_NAME>` | `divisions[].name` (runtime, via API) | Yes |
| Division entity ID | `<DIVISION_ENTITY_ID>` | `divisions[].entity_id` (runtime, via API) | System-generated (auto) |
| Division owner identity | `<DIVISION_OWNER_IDENTITY_ID>` | `divisions[].owner_identity_id` | Yes — active identity in the same business |
| Agent name | `<AGENT_NAME>` | `agents[].name` (runtime, via API) | Yes |
| Agent entity ID | `<AGENT_ENTITY_ID>` | `agents[].entity_id` (runtime, via API) | System-generated (auto) |
| Agent allowed tools | `<AGENT_ALLOWED_TOOLS>` | `agents[].allowed_tools` (runtime, via API) | Yes — e.g. `["web.search"]` |

**Rule:** Division scope is narrower than business scope (SCHEMA_IDENTITIES_ORG §4.3). A division-scoped membership covers only its own division; it does NOT cover business-level resources.

---

## C. Model provider (live operation only; scripted for pilot validation)

| Parameter | Placeholder | Source in config | Owner-supplied? |
|---|---|---|---|
| Model provider ID | `<MODEL_PROVIDER_ID>` | `models.providers[].id` | Yes |
| Model provider endpoint | `<MODEL_PROVIDER_ENDPOINT>` | `models.providers[].endpoint` | Yes |
| Model ID | `<MODEL_ID>` | `models.providers[].model_id` | Yes |
| Credential environment variable name | `<API_KEY_ENV>` | `models.providers[].api_key_env` | Yes |
| Credential value | `<API_KEY_VALUE>` | Environment variable named by `api_key_env` | **Yes — never written to config** |
| Capabilities | `<MODEL_CAPABILITIES>` | `models.providers[].capabilities` | Yes |

**Rule:** Production requires at least one real model provider (`config.go:878-882`). Pilot validation uses the simulated/scripted provider (`seeded_provider_mode: "scripted"`).

---

## D. Web search (live operation only; scripted for pilot validation)

| Parameter | Placeholder | Source | Owner-supplied? |
|---|---|---|---|
| Web search provider | `<WEB_SEARCH_PROVIDER>` | `NEXUS_CAPABILITY_WEB` env | Yes |
| Web search endpoint | `<WEB_SEARCH_ENDPOINT>` | `NEXUS_CAPABILITY_WEB_ENDPOINT` env | Yes |
| Web search API key env name | `<WEB_SEARCH_API_KEY_ENV>` | Provider config | Yes |
| Web search API key value | `<WEB_SEARCH_API_KEY_VALUE>` | Environment variable | **Yes — never written to config** |

**Rule:** Pilot validation uses `NEXUS_CAPABILITY_WEB=scripted`. Live operation requires real credentials.

---

## E. Governance and approvals

| Parameter | Placeholder | Source | Owner-supplied? |
|---|---|---|---|
| Approver type | `<APPROVER_TYPE>` | `policies[].approval_config.approver_type` | Yes |
| Approver IDs | `<APPROVER_IDS>` | `policies[].approval_config.approver_ids` | Yes |
| Self-approval prohibited | `<SELF_APPROVAL_PROHIBITED>` | `policies[].approval_config.self_approval_prohibited` | Yes — recommend `true` |
| Delegation allowed | `<DELEGATION_ALLOWED>` | `policies[].approval_config.delegation_allowed` | Yes |
| Approval timeout seconds | `<TIMEOUT_SECONDS>` | `policies[].approval_config.timeout_seconds` | Yes |
| Auto-deny on timeout | `<AUTO_DENY_ON_TIMEOUT>` | `policies[].approval_config.auto_deny_on_timeout` | Yes |

**Rule:** `SelfApprovalProhibited` defaults to `false`. For a pilot, consider setting it to `true` so no identity can approve its own action. `ApproverIDs` empty means any authorized actor can approve; populate it to restrict.

---

## F. Persistence and operations

| Parameter | Placeholder | Source | Owner-supplied? |
|---|---|---|---|
| Durable data directory | `<STORAGE_DATA_DIR>` | `storage.data_dir` / `NEXUS_DATA_DIR` env | Yes |
| Bootstrap credential | `<NEXUS_BOOTSTRAP_CREDENTIAL>` | `NEXUS_BOOTSTRAP_CREDENTIAL` env | Yes — first-install identity credential |
| Control API key | `<NEXUS_CONTROL_API_KEY>` | `NEXUS_CONTROL_API_KEY` env | Yes — gates /api/v1/control/* |
| Environment | `<NEXUS_ENVIRONMENT>` | `nexus.environment` / `NEXUS_ENVIRONMENT` env | Yes — "development" for pilot |
| Backup owner | `<BACKUP_OWNER>` | Operational process | Yes |
| Recovery operator | `<RECOVERY_OPERATOR>` | Operational process | Yes |

**Rule:** `storage.data_dir` is required for durability. Without it, all state (identities, businesses, divisions, memberships, execution journal) is in-memory and lost on restart.

---

## G. Pilot success criteria

| Criterion | How verified | Owner-supplied threshold? |
|---|---|---|
| Bootstrap identity authenticates | `GET /health` with bootstrap credential returns 200 | Yes — credential value |
| Business + membership survive restart | Restart NEXUS with same `NEXUS_DATA_DIR`; `GET /api/v1/businesses` returns the business; `GET /api/v1/businesses/{id}` returns 200 | Yes — same data directory |
| Research objective reaches terminal state | `POST /api/v1/intelligence/execute` → 202; poll `GET /api/v1/intelligence/{id}` → status `completed` | Yes — scripted provider for pilot |
| Evidence + synthesis present | `GET /api/v1/intelligence/{id}` outcome includes `research_evidence` and `research_synthesis` | Yes — scripted provider |
| Cross-business isolation | Foreign identity GET on the business's execution → 404 | Yes — second business + identity |
| Division-scoped isolation | Division-scoped identity reads another division's record → 404 | Yes — second division + narrow identity |
| Unauthenticated access rejected | Requests without credential → 401 | Yes |
| Restart recovery classification known | `GET /api/v1/recovery/executions/{id}` returns `state` and `error_class` | Yes — interruption scenario |

**Go/no-go:** All green-scripted criteria must pass with the scripted provider before considering live-provider configuration. Scripted success does NOT prove live-provider readiness.

---

## H. Rollback / stop conditions

| Condition | Action |
|---|---|
| `storage.data_dir` corrupt or unreadable at startup | NEXUS aborts startup (fail closed). Restore from backup or fix the directory. |
| `storage.data_dir` missing in production | NEXUS refuses to start (`config.storage_missing`). Set the variable or do not run production. |
| Real model provider unreachable | Objective execution fails honestly at model invocation; does not corrupt durable state. Remove provider config or fix credentials. |
| Approval pause abandoned | After restart, the execution remains `failed`/`APPROVAL_REQUIRED`; the approval snapshot is process-local and gone. Fresh governance admission required. |
| `unknown` execution outcome after restart | Recovery classified an interrupted execution as `unknown`. Do not blindly retry; investigate the external effect manually. |

---

## H. Reminder: what this worksheet is NOT

- It is NOT a live production configuration.
- It does NOT contain real credentials.
- It does NOT grant TrendSentry any implicit superuser authority.
- It does NOT hardcode TrendSentry into NEXUS core; all values are runtime parameters via the public HTTP API.
- It does NOT replace `configs/nexus.dev.json`, which is protected local state.
