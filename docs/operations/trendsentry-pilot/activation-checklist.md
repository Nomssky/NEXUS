# NEXUS — TrendSentry Pilot Activation Checklist

**Status:** Activation-readiness checklist. Items are separated into three
buckets:

1. **Scripted-safe** — can be validated with `NEXUS_SEEDED_PROVIDER_MODE=scripted`
   and `NEXUS_CAPABILITY_WEB=scripted` using no live credentials.
2. **Owner decision** — requires TrendSentry business/division/owner inputs
   from the pilot parameter worksheet.
3. **Live credentials** — requires real model provider and web-search API keys;
   NOT covered by scripted validation.
4. **Must remain disabled** — features not supported by the current
   implementation; must not be promised or enabled.
5. **Unsupported** — architecture features explicitly out of scope; do not
   promise or build workarounds.

Do not mark any item complete without the corresponding observed result.

---

## Stage 1 — Configuration (owner decision + scripted-safe)

| # | Checklist item | Bucket | Evidence required |
|---|---|---|---|
| S1.1 | `storage.data_dir` set to an absolute, persistent, service-owned directory | Owner decision | `configs/nexus.dev.json` unchanged; pilot config file references `<STORAGE_DATA_DIR>` |
| S1.2 | `NEXUS_DATA_DIR` environment variable set to the same path | Owner decision | Environment variable present at process start |
| S1.3 | `NEXUS_ENVIRONMENT=development` for pilot (production only with real provider) | Owner decision | Environment variable present |
| S1.4 | `NEXUS_BOOTSTRAP_CREDENTIAL` set (first-install identity credential) | Owner decision | Environment variable present; never committed |
| S1.5 | `NEXUS_CONTROL_API_KEY` set (control-plane endpoints) | Owner decision | Environment variable present; never committed |
| S1.6 | `NEXUS_SEEDED_PROVIDER_MODE=scripted` and `NEXUS_CAPABILITY_WEB=scripted` | Scripted-safe | Environment variables present at process start |
| S1.7 | No real model provider registered under `models.providers` for pilot | Scripted-safe | `models.providers` empty in pilot config |
| S1.8 | No live web-search credentials configured | Scripted-safe | `NEXUS_CAPABILITY_WEB=scripted`; no Brave/Wikipedia keys |

**Go/no-go for Stage 1:** All S1 items complete with scripted-safe values. No live credentials present. `configs/nexus.dev.json` unmodified.

---

## Stage 2 — Startup (scripted-safe)

| # | Checklist item | Bucket | Evidence required |
|---|---|---|---|
| S2.1 | NEXUS starts successfully with `storage.data_dir` set | Scripted-safe | Process starts; `/health` returns 200 `{"status":"ok"}` |
| S2.2 | `/ready` returns `200 {"status":"ready"}` after startup recovery | Scripted-safe | `curl http://127.0.0.1:<health_port+1>/ready` → 200 |
| S2.3 | Startup recovery ran cleanly (no corrupt/unsupported record abort) | Scripted-safe | Startup logs show recovery classified interrupted work; `GET /api/v1/control/metrics` reports recovery metrics |
| S2.4 | No second NEXUS process shares the same `storage.data_dir` | Scripted-safe | Process list checked; only one instance per data directory |

**Go/no-go for Stage 2:** All S2 items pass. If startup fails closed (corrupt store), do not proceed — restore from backup or fix the directory.

---

## Stage 3 — Business and membership (scripted-safe)

| # | Checklist item | Bucket | Evidence required |
|---|---|---|---|
| S3.1 | `POST /api/v1/businesses` creates a TrendSentry business with `owner_identity_id` | Scripted-safe | HTTP 200; `business_id` echoed; `owner_identity_id` matches bootstrap identity |
| S3.2 | `POST /api/v1/businesses/{id}/members` establishes owner membership | Scripted-safe | HTTP 200; `role: OWNER`, `status: active`, `division_id` empty |
| S3.3 | Business visible in `GET /api/v1/businesses` to owner | Scripted-safe | HTTP 200; business appears in list |
| S3.4 | Foreign (non-member) identity cannot read the business | Scripted-safe | HTTP 404 (G5 indistinguishable from missing) |
| S3.5 | Non-owner authenticated identity cannot claim membership | Scripted-safe | HTTP 403 AUTHORIZATION |

**Go/no-go for Stage 3:** All S3 items pass. Business + membership durable and restart-surviving (see Stage 5).

---

## Stage 4 — Division and agent (scripted-safe)

| # | Checklist item | Bucket | Evidence required |
|---|---|---|---|
| S4.1 | `POST /api/v1/divisions` creates a TrendSentry division under the business | Scripted-safe | HTTP 200; `business_id` matches; `parent_business_id` equals `business_id` |
| S4.2 | Business-wide member registers an agent with `allowed_tools: ["web.search"]` | Scripted-safe | HTTP 200; agent entity ID echoed |
| S4.3 | Agent visible in `GET /api/v1/agents?business_id=` | Scripted-safe | HTTP 200; agent appears in list |
| S4.4 | Division-scoped identity cannot register an agent (requires business-wide membership) | Scripted-safe | HTTP 403 |

**Go/no-go for Stage 4:** All S4 items pass. Division + agent exist; scope boundaries enforced.

---

## Stage 5 — Restart persistence (scripted-safe)

| # | Checklist item | Bucket | Evidence required |
|---|---|---|---|
| S5.1 | Restart NEXUS with the same `storage.data_dir` | Scripted-safe | Process restarts; `/ready` → 200 |
| S5.2 | Business still readable after restart | Scripted-safe | `GET /api/v1/businesses/{id}` → 200 |
| S5.3 | Membership survives restart | Scripted-safe | `GET /api/v1/businesses` lists the business; owner membership intact |
| S5.4 | Division survives restart | Scripted-safe | `GET /api/v1/divisions?business_id=` lists the division |
| S5.5 | Agent survives restart | Scripted-safe | `GET /api/v1/agents?business_id=` lists the agent |
| S5.6 | Live request-result API → 404 after restart (process-local) | Scripted-safe | `GET /api/v1/requests/{id}` → 404 |
| S5.7 | Durable read API → 200 after restart with `state: completed` | Scripted-safe | `GET /api/v1/recovery/executions/{id}` → 200; evidence + synthesis intact |

**Go/no-go for Stage 5:** All S5 items pass. Durable journal evidence survives restart; live result surface is correctly process-local.

---

## Stage 6 — Research objective execution (scripted-safe)

| # | Checklist item | Bucket | Evidence required |
|---|---|---|---|
| S6.1 | Submit a research objective scoped to business + division | Scripted-safe | `POST /api/v1/intelligence/execute` → 202; `execution_id` returned |
| S6.2 | Objective reaches terminal `completed` state | Scripted-safe | Poll `GET /api/v1/intelligence/{id}` → `status: completed` |
| S6.3 | Durable evidence present (`research_evidence`) | Scripted-safe | `GET /api/v1/recovery/executions/{id}` includes `research_evidence` |
| S6.4 | Validated typed synthesis present | Scripted-safe | `research_synthesis.status == "complete"`; `evidence_backed` non-empty with `source_ids` |
| S6.5 | Quality evaluation present | Scripted-safe | `research_evaluation` present in durable record |
| S6.6 | No `actor_id`, prompts, credentials, or model output in durable read | Scripted-safe | `actor_id` absent; no `sk-`/`credential`/`secret`/`prompt` substrings |
| S6.7 | Division-scoped member reads own division's record → 200 | Scripted-safe | `GET /api/v1/recovery/executions/{id}` with narrow identity → 200 |
| S6.8 | Division-scoped member reads another division's record → 404 | Scripted-safe | `GET /api/v1/recovery/executions/{other_id}` → 404 |

**Go/no-go for Stage 6:** All S6 items pass. Research workflow executes end-to-end under scripted provider; evidence and synthesis durable; division isolation enforced.

---

## Stage 7 — Approval governance (scripted-safe)

| # | Checklist item | Bucket | Evidence required |
|---|---|---|---|
| S7.1 | Approval history present on durable read when an approval gate was exercised | Scripted-safe | `approvals` array present in durable record; `approval_id`, `outcome`, `decided_at` populated |
| S7.2 | `approvals` omitted when no approval gate exercised | Scripted-safe | Field absent when no governance approval was required |
| S7.3 | `approver_id` NOT exposed in durable read | Scripted-safe | `approver_id` absent from `approvals[]` entries |
| S7.4 | Self-approval prohibited if configured (`SelfApprovalProhibited: true`) | Owner decision | `policies[].approval_config.self_approval_prohibited == true`; self-approve attempt rejected |

**Go/no-go for Stage 7:** All applicable S7 items pass. Approval history durable; approver identity not exposed in durable read.

---

## Stage 8 — Go/no-go decision

| Checkpoint | Requirement | Status |
|---|---|---|
| C1 | `storage.data_dir` configured and persistent | ☐ |
| C2 | Scripted research workflow passes end-to-end (S6) | ☐ |
| C3 | Restart persistence verified (S5) | ☐ |
| C4 | Authorization boundaries enforced (S3, S4, S6) | ☐ |
| C5 | No live credentials present or tested | ☐ |
| C6 | No TrendSentry-specific logic hardcoded into core | ☐ (by inspection) |
| C7 | General-purpose platform architecture unchanged | ☐ (by inspection) |

**GO:** All C1–C7 complete. The pilot is ready for live-provider configuration (owner-supplied credentials) as a separate, later decision.

**NO-GO:** Any C item unmet. Do not proceed to live-provider configuration until resolved.

---

## Reminder: what this checklist is NOT

- It does NOT configure or test a live model provider.
- It does NOT configure or test live web search.
- It does NOT contain real credentials.
- It does NOT modify `configs/nexus.dev.json` or any existing protected file.
- It does NOT grant TrendSentry implicit superuser authority or bypass governance.
- It does NOT enable resume-across-restart (process-local by design) or multi-instance coordination (unsupported).
