# NEXUS — TrendSentry Owner Handoff

**Status:** Owner review package. Live activation has **not** been authorized, deployed, or exercised against a live model or live web-search provider.
**Prepared:** 2026-10-26
**Validated source baseline:** `origin/master` = `098267d279569e5f7ac6a934b8d78419a777baa8` (includes merged PR #67).
**Audience:** TrendSentry pilot owner, platform operator, security reviewer, and engineering lead.

**Companion documents**

- [Activation input register](activation-inputs.md) — the fillable list of owner decisions and secret **names**.
- [Live activation runbook](live-activation-runbook.md) — Gates 0–8, exact checks, pass/fail, evidence, accountable roles.
- [Existing scripted pilot runbook](../TRENDSENTRY_PILOT_RUNBOOK.md) — verified behavior of the current build under the deterministic scripted provider.
- [Existing parameter worksheet](worksheet.md) and [activation checklist](activation-checklist.md) — background artifacts. For live activation, the input register and Gates 0–8 are authoritative where they differ.

> This document does **not** authorize a deployment, a live provider call, or the use of production data. Gate 0 in the live activation runbook is the authorization gate.

---

## 1. Bottom line

| Area | Status | Meaning |
|---|---|---|
| **SCRIPTED VALIDATION** | **PASS** | The platform was exercised end-to-end through its real HTTP gateway with the deterministic scripted model and scripted web-search providers. |
| **ARTIFACT INTEGRITY** | **PASS** | No tracked source changes were made for this handoff. The new documents are untracked additions under `docs/operations/trendsentry-pilot/`. The scripted configuration example is strict JSON and passes the configuration package tests. |
| **LIVE-PROVIDER READINESS** | **BLOCKED** | No live model provider or live web-search provider has been configured, credentialed, or called. Owner inputs are unresolved. |
| **FIRST LIVE WORKFLOW** | **BLOCKED** | No live research objective has been executed. The first live workflow depends on Gate 0 authorization, owner inputs, and live-provider readiness. |
| **PRODUCTION ACTIVATION** | **NO-GO** | Production activation is not approved. The blockers are listed in §5 and tracked in the input register. |

**The central distinction:** a scripted validation pass proves platform mechanics, authorization boundaries, persistence, and evidence plumbing. It does **not** prove live-provider connectivity, live search quality, provider quotas, latency, semantic citation support, or production host security. Scripted evidence must never be promoted to live readiness.

---

## 2. What has actually been validated

The following was verified against the real compiled `cmd/nexus` binary and the repository test suites. The scripted provider is deterministic simulation; no live credentials were used.

### 2.1 Black-box workflow validation

- `e2e/tests/30-trendsentry-research-workflow.spec.ts` passed. It drives the real HTTP gateway to:
  - authenticate an owner identity;
  - create a business without automatic membership;
  - prove a non-member is refused before membership is established;
  - establish the owner's initial business-wide membership through the public endpoint;
  - create a division and register an agent with `web.search`;
  - submit a research objective and poll it to a terminal `completed` result;
  - verify evidence-backed synthesis and deterministic research evaluation;
  - verify business listing and scope behavior.
- `e2e/tests/31-durable-recovery-read.spec.ts` passed. It verifies the durable recovery read and citation-audit surfaces, including restart behavior and authorization boundaries.
- The full Playwright suite passed with **267 tests**.
- `go build ./...`, `go vet ./...`, `go test -count=1 ./...`, `go test -race` (key packages), and `make smoke` passed in the recorded validation run.

**Evidence-reproducibility note.** The two workflow specs above are workspace artifacts: at the validated baseline `098267d`, they are **not tracked** in `origin/master`. The product code they exercise is tracked, and the recorded run is valid evidence, but a clean export of the validated commit does not contain the specs. Gate 1 of the live runbook records this gap, and Gate 8 requires the owner to accept it or require a separately authorized change to commit the specs.

### 2.2 What those results establish

- The gateway enforces identity-bound business scope and membership on the tested paths.
- Business onboarding, owner membership, division creation, agent registration, objective submission, result polling, durable execution records, and citation audit work through public HTTP routes.
- Executions survive a restart in the durable journal; the process-local live result routes return `404` after restart, as designed.
- The scripted research path produces validated evidence, typed synthesis, and a deterministic evaluation.
- The backup/restore operator tool and release smoke path work in the tested environment.

### 2.3 What those results do not establish

- That any real OpenAI-compatible model endpoint accepts NEXUS requests, honors the strict JSON-schema response format, calls tools correctly, or stays within quota.
- That a real web-search provider (`wikipedia` or `brave`) returns usable results under the pilot's queries, rate limits, and terms.
- That the first live research output is semantically supported by its cited sources; the citation audit is structural and deterministic, not a truth judgment.
- That the target production host, network, TLS termination, process supervisor, secret injection, disk permissions, backup destination, and monitoring are correctly configured.
- That the owner's business, division, agent, governance, and approver decisions are acceptable.
- That approval/resume behavior survives a restart; it does not. See §6.1.

---

## 3. Owner responsibilities vs engineering responsibilities

| Responsibility | Accountable role | Notes |
|---|---|---|
| Define the pilot's business scope, first research question, and human acceptance criteria | **Pilot Owner** | The first objective must be a real, bounded question with a named human reviewer. |
| Decide whether `web.search` requires human approval, and name the approver(s) | **Pilot Owner + Security Reviewer** | If self-approval is prohibited, a second authorized human must exist. |
| Supply business/division/agent taxonomy and identity choices | **Pilot Owner** | Do not guess names or IDs; record them in the input register. |
| Supply the persistent `storage.data_dir` and service account | **Pilot Owner + Platform Operator** | Absolute, persistent, service-owned, single-process. |
| Select the model provider, model, endpoint, and provider spend cap | **Pilot Owner + Security Reviewer** | NEXUS does not enforce a monetary cap. The provider account must. |
| Select the live web-search provider and accept its coverage limits | **Pilot Owner** | `wikipedia` is Wikipedia-only; `brave` requires `BRAVE_API_KEY`. |
| Inject secrets through the process environment or secret manager | **Platform Operator** | Never in the config file, docs, chat, tickets, or shell history. |
| Build the artifact, run Gates 1–7, and capture evidence | **Engineering Lead + Platform Operator** | Scripted tests are not a substitute for live gates. |
| Review source support for the first live output | **Research Reviewer** | Human semantic review is required even when the structural audit passes. |
| Approve backup, restore, unknown-outcome, stop, and rollback procedures | **Pilot Owner + Platform Operator** | A backup is not a backup until a restore has been verified. |
| Final production GO/NO-GO | **Pilot Owner** | Security Reviewer holds a veto. The current decision is **NO-GO**. |

Roles may be combined in a small pilot, but the combination must be recorded explicitly, and no one may approve their own gated action when self-approval is prohibited.

---

## 4. What the owner must provide

The authoritative list is the [activation input register](activation-inputs.md). In summary, the blocking inputs are:

1. **Installation identity:** stable `nexus.id` and target environment `production`.
2. **Business and owner identity:** business display name, optional stable business entity ID, existing active human owner identity, and the **name** of the environment variable holding that identity's credential.
3. **Agent:** at least one active agent in the business with `web.search` in its allowed tools. Division scoping is optional for a business-scoped first run; if used, the agent and the objective must use a compatible division.
4. **Persistence:** an absolute, persistent, service-owned `storage.data_dir` on a single-process host.
5. **Model provider:** provider ID, display name, HTTPS base endpoint (or a Security-approved loopback exception), exact model ID, the **name** of the environment variable holding the API key, and confirmation that the model supports OpenAI-compatible structured output and tool calling.
6. **Web search:** `wikipedia` or `brave`; for `brave`, confirmation that `BRAVE_API_KEY` is provisioned. `scripted` is validation-only and must not be used for live activation.
7. **Governance:** whether `web.search` requires approval, the approver identity/identities, timeout, auto-deny behavior, self-approval rule, and delegation rule.
8. **Operations:** control API key **name**, backup owner/destination/retention, restore-test window, unknown-outcome owner, success/stop/rollback criteria, and final sign-off.

### 4.1 Secret-handling rules

**Do not send secret values in this document, chat, email, tickets, or commits.** The owner or operator should:

- Store secret values in the organization's secret manager or the process supervisor's secret mechanism.
- Provide only the **environment-variable name** that will carry each secret.
- Confirm presence with a non-printing check such as `printenv NAME >/dev/null`; never print the value or put it in shell history.
- Restrict the config file to the service account (for example, mode `600`). The config schema has no raw API-key field; the model API key is referenced only by `api_key_env`.
- Rotate credentials after the pilot and revoke them on rollback or suspected exposure.

Secret names used by this build:

| Secret | How it is supplied | Notes |
|---|---|---|
| Model provider API key | Any environment variable named by `models.providers[].api_key_env` | The adapter reads it from the process environment at invocation time. |
| Brave Search API key | `BRAVE_API_KEY` | Required only when `NEXUS_CAPABILITY_WEB=brave`. |
| Control-plane API key | `NEXUS_CONTROL_API_KEY` | Required for `/api/v1/control/*`; without it those endpoints are disabled with `403 CONTROL_DISABLED`. |
| Bootstrap identity credential | `NEXUS_BOOTSTRAP_CREDENTIAL` | Re-armed on every boot when the bootstrap identity is used. |

No other secret should be placed in a config file. The configuration file is not a secret store.

---

## 5. Why production activation is currently NO-GO

Each blocker is tracked in the input register. None is a defect in the scripted-validated code path.

| Blocker | Why it blocks activation | Owner input |
|---|---|---|
| No production `storage.data_dir` | Production configuration refuses to start without one (`config.storage_missing`). Without durable storage, organizational state and execution evidence are lost on restart. | `RF-08` |
| No live model provider | Production configuration refuses to start without at least one real provider (`config.provider_missing`). No live inference has been tested. | `RF-09`, `RF-10` |
| No live web-search provider | A research workflow needs a live `web.search` provider. `scripted` is validation-only, and this build does not reject `NEXUS_CAPABILITY_WEB=scripted` in production; the operator must prevent it. | `RF-11` |
| No owner business/identity/division/agent decisions | The workflow cannot be scoped, authorized, or reviewed without them. | `RF-03`–`RF-07` |
| No governance/approver decision | If approval is required, the policy, approver, timeout, and self-approval rules must be explicit. Policies are not in the config file and do not survive restart. | `RF-12` |
| No production security/deployment verification | Host hardening, TLS, network egress controls, process supervision, secret injection, disk permissions, and monitoring are outside the scripted validation. | `RF-08`, `RF-13`, `RF-15` |
| No backup/restore/unknown-outcome/rollback sign-off | Production operation requires a verified restore, a named unknown-outcome owner, stop criteria, and rollback authority. | `RF-15`–`RF-19` |
| No human GO decision | Gate 8 requires an explicit, signed GO. The current decision is NO-GO. | `RF-19` |

**Do not interpret the scripted pass as permission to proceed.** The correct next step is to complete the input register, then execute Gate 0 and Gates 1–8 in order.

---

## 6. Operating boundaries the owner must accept

These are properties of the current build, not configuration choices. If the pilot requires behavior outside these boundaries, the requirement must be escalated as product work; it must not be promised as an activation step.

### 6.1 Durability and recovery

- **Live result routes are process-local.** `GET /api/v1/requests/{id}`, `GET /api/v1/executions/{id}`, and `GET /api/v1/intelligence/{id}` return `404` after a restart. The durable inspection surface is `GET /api/v1/recovery/executions/{id}?business_id=...`.
- **The durable read is read-only.** There is no resume, replay, reconciliation write, or repair endpoint.
- **`unknown` is a terminal recovery classification.** It means the runtime cannot know whether an in-flight effect occurred. It is never auto-retried. A human must make a fresh governance decision before resubmitting.
- **Approval pauses do not survive restart.** A paused execution is journaled as terminal `failed` with `error_class: APPROVAL_REQUIRED`; the process-local approval snapshot is lost. After restart the execution is not resumable through the paused path; fresh governance admission is required.
- **Multi-instance coordination is unsupported.** Run one process against one data directory.
- **Retention is operator-run and offline.** Do not run retention against a live store.

### 6.2 Governance

- **Policies are managed through the control API, not the config file.** Use `PUT /api/v1/control/policies/{id}` with `X-API-Key`.
- **Policies are process-lifetime state.** Custom policies do not survive a restart; the built-in `default-allow` policy returns. The operator must reapply approval policies after every boot and verify them before admitting new work.
- **The built-in `default-allow` policy is read-only.** `PUT`/`DELETE` against it return `409`.
- **Governance is default-deny when no policy matches.** The seeded default-allow exists so an unconfigured installation can operate; it is not a substitute for the pilot's approval design.
- **The approval endpoint requires business-wide membership.** Approval decisions are made by a business-wide member; if self-approval is prohibited, a second authorized human must exist.
- **`security.audit_enabled` is not the citation audit.** The former is a production security gate; the latter is the deterministic `/audit` projection.

### 6.3 Providers and cost

- **The only model adapter is OpenAI-compatible.** It sends `POST {endpoint}/chat/completions` with a bearer credential from `api_key_env`. It is not a vendor SDK.
- **Structured output is required.** The agent intelligence loop requests strict JSON-schema structured output. A model that cannot honor it will fail; NEXUS will not silently downgrade to prose parsing.
- **The live web-search providers are `wikipedia` and `brave`.** `wikipedia` is Wikipedia-only and requires no API key. `brave` requires `BRAVE_API_KEY`. `scripted` is validation-only.
- **NEXUS does not enforce a monetary cost cap.** Runtime budgets bound iterations, model calls, tool calls, delegations, replans, steps, and execution time, but not dollars. The provider account must have a spend limit and the owner must monitor it.
- **`security.egress_allow_list` is constructed but is not wired to the model or web-search adapters in this build.** Do not rely on it to restrict outbound traffic. Enforce egress with host firewalls, network policy, or an egress proxy.
- **Only the `web.search` tool is used by the first research workflow.** Other capabilities (`http.request`, GitHub, filesystem sandbox, memory) are outside the first-workflow scope unless separately configured and reviewed.

### 6.4 Evidence and audit

- **The citation audit is structural.** It checks claims, citations, source IDs, fingerprints, and bindings. It does not prove that a source semantically supports a claim.
- **Human source review is mandatory for the first live workflow.** The Research Reviewer must open the cited sources and record a decision.
- **Provider-side usage evidence may be limited.** NEXUS exposes provider registration and execution/tool evidence, but not a general provider billing export. If the provider does not offer call logs, the owner must explicitly accept NEXUS-side trace plus durable evidence as the available proof, or choose a provider that does.

### 6.5 Build and runtime

- **The current workspace is not the production artifact.** The workspace HEAD `3bfbd68feca5d54d97c86837fd252639ff1e15ef` is an ancestor of the validated `origin/master` `098267d279569e5f7ac6a934b8d78419a777baa8`. A production artifact must be built from a checkout that contains the validated baseline; Gate 1 enforces this.
- **Gateway address:** default gateway port is `health.port + 1` (for example, health `8080`, gateway `8081`). Use `--http-addr` to override.
- **Configuration precedence:** defaults < config file < environment. `NEXUS_ENVIRONMENT` overrides the file's environment; `NEXUS_DATA_DIR` overrides `storage.data_dir`. Set them consistently, or leave the environment overrides unset.
- **No production data in the first live workflow.** Use a dedicated pilot business and a synthetic or owner-approved research question.

---

## 7. What the owner is being asked to decide

The owner is asked to:

1. **Read and accept** the operating boundaries in §6, especially process-local results, no approval resume across restart, policy reapplication after every boot, no monetary cap, and no enforced egress allow list.
2. **Complete** the [activation input register](activation-inputs.md), providing only secret **names**, never values.
3. **Name** the Pilot Owner, Platform Operator, Security Reviewer, Research Reviewer, and Engineering Lead.
4. **Choose** the first live research question and its human acceptance criteria.
5. **Decide** whether `web.search` requires human approval, and who may approve it.
6. **Authorize or decline Gate 0.** If authorized, execute Gates 1–8 in order. If any gate fails, stop and record the failure; do not proceed on a partial pass.
7. **Sign the final GO/NO-GO.** Until then, production activation remains **NO-GO**.

### Sign-off block

| Decision | Name | Role | Date | Signature / record reference |
|---|---|---|---|---|
| Accept operating boundaries | | Pilot Owner | | |
| Inputs complete and verified | | Platform Operator | | |
| Security review complete | | Security Reviewer | | |
| First objective and acceptance criteria approved | | Pilot Owner + Research Reviewer | | |
| Gate 0 authorized | | Pilot Owner | | |
| Final production GO/NO-GO | | Pilot Owner | | |

**Current decision: NO-GO.** No live provider call, deployment, or production data use is authorized by this handoff.
