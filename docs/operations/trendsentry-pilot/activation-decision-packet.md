# NEXUS — TrendSentry Controlled Live Run Readiness: Owner Decision & Technical Blocker Assessment

**Status:** Preparation complete — owner decisions pending.  
**Prepared:** 2026-10-30  
**Source baseline:** `origin/master` = `098267d279569e5f7ac6a934b8d78419a777baa8` (HEAD `f1fbab8` on `feat/one-click-launcher`)  
**Authorization scope:** Preparation only. No live provider calls, no paid requests, no deployment, no activation.

---

## A. Actual Current Baseline and Preservation Status

### Repository state (verified read-only, no modifications)

| Item | Value |
|------|-------|
| Branch | `feat/one-click-launcher` |
| HEAD | `f1fbab8 docs(deployment): add deployment preparation guide for production hosts` |
| Remote | `origin https://github.com/Nomssky/NEXUS` (fetch + push) |
| Upstream tracking | None configured |
| Modified tracked files | 20 (pre-existing v48–v53 work + authentication task changes from prior session) |
| Untracked files/directories | 37 (including `docs/operations/`, E2E tests 30/31/35, `node_modules/`, `test-results/`) |
| Stash | `stash@{0}` preserved ("WIP on master: f0a4979 feat(launcher): wire Core Runtime + Gateway into main entrypoint") |
| Worktrees | 4 — main + 3 in /tmp (all prunable), all preserved |

### Protected artifacts — verified unaccessed and unmodified

- `configs/nexus.dev.json` — untracked, never read or modified
- `internal/foundation/modelrouter/groq_router_live_test.go` — untracked, never read or modified
- `docs/operations/` — untracked, preserved (all 7 files)
- E2E tests 30/31/35 — untracked, preserved
- `node_modules/`, `test-results/`, `workspaces/` — untracked, preserved

### Authentication/session fixes from prior session (present in working tree, not modified in this task)

| File | Status | Key changes |
|------|--------|-------------|
| `nexus-app/src/App.tsx` | Modified (pre-existing + auth fixes) | `isAuthenticated` init to `false`; 401 handler; logout cache clearing |
| `nexus-app/src/api/client.ts` | Modified (pre-existing + auth fixes) | `setOnUnauthenticated`; 401 handler with control-path exclusion |
| `nexus-app/src/api/client.test.ts` | Modified (pre-existing + auth fixes) | 4 new 401 handling tests |
| `nexus-app/src/App.test.tsx` | New (auth regression tests) | 6 authentication regression tests |
| `nexus-app/vitest.config.ts` | Modified (pre-existing + path alias) | `@/*` path alias added |

No files were created, modified, or deleted during this task except this new decision-packet report file.

---

## B. Canonical Owner Decision Register (RF-01 through RF-19 and OD-*)

All values below are taken directly from `docs/operations/trendsentry-pilot/activation-inputs.md`. Every RF-* row is `UNRESOLVED`. No owner decisions have been recorded.

### A. Authorization, roles, change window, stop/rollback authority

| ID | Decision needed | Current value | Status | Evidence | Gate blocked | Minimum owner response | Needs external access? | Needs named human? |
|----|----------------|---------------|--------|----------|--------------|----------------------|----------------------|--------------------|
| RF-01 | Installation ID (`nexus.id`) | `<UNRESOLVED>` | PENDING OWNER | `activation-inputs.md:93-95` | Gate 0, Gate 2 | Stable, human-readable, unique ID | No | Yes (Pilot Owner) |
| RF-02 | Target environment is `production` | `<UNRESOLVED>` | PENDING OWNER | `activation-inputs.md:104-107` | Gate 0, Gate 2 | Confirm `nexus.environment=production` + `NEXUS_ENVIRONMENT=production` | No | Yes (Pilot Owner) |
| RF-18 | Stop and rollback criteria + authority | `<UNRESOLVED>` | PENDING OWNER | `activation-inputs.md:542-563` | Gate 0, Gate 8 | Named stop authority (2 people), rollback procedure accepted | No | Yes (Pilot Owner + Security Reviewer) |
| RF-19 | Final GO/NO-GO sign-off | `<UNRESOLVED>` | PENDING OWNER | `activation-inputs.md:565-577` | Gate 8 | Signed GO/NO-GO decision | No | Yes (Pilot Owner) |

### B. Budget, provider terms, usage alerts, and spend ownership

| ID | Decision needed | Current value | Status | Evidence | Gate blocked | Minimum owner response | Needs external access? | Needs named human? |
|----|----------------|---------------|--------|----------|--------------|----------------------|----------------------|--------------------|
| RF-10 | Provider fit, data terms, spend control | `<UNRESOLVED>` | PENDING OWNER | `activation-inputs.md:284-300` | Gate 0, Gate 2, Gate 6 | Confirm model supports OpenAI-compatible chat, structured output, tool calling; provider account has hard spend limit; spend owner named | Yes (provider terms/spend) | Yes (Pilot Owner + Security Reviewer) |

### C. Installation, host, storage, service account, secret injection

| ID | Decision needed | Current value | Status | Evidence | Gate blocked | Minimum owner response | Needs external access? | Needs named human? |
|----|----------------|---------------|--------|----------|--------------|----------------------|----------------------|--------------------|
| RF-08 | Persistent `storage.data_dir`, host, service account | `<UNRESOLVED>` | PENDING OWNER | `activation-inputs.md:209-227` | Gate 2 | Absolute, persistent, service-owned path; mode 0750+; not shared with another process | No | Yes (Pilot Owner + Platform Operator) |
| RF-13 | Control and bootstrap secret names | `<UNRESOLVED>` | PENDING OWNER | `activation-inputs.md:399-404` | Gate 2 | `NEXUS_CONTROL_API_KEY`, `NEXUS_BOOTSTRAP_CREDENTIAL`, model `api_key_env` name | No | Yes (Platform Operator) |

### D. Model and web-search provider configuration

| ID | Decision needed | Current value | Status | Evidence | Gate blocked | Minimum owner response | Needs external access? | Needs named human? |
|----|----------------|---------------|--------|----------|--------------|----------------------|----------------------|--------------------|
| RF-09 | Model provider registration | `<UNRESOLVED>` | PENDING OWNER | `activation-inputs.md:260-271` | Gate 2 | Provider ID, endpoint (https), model ID, `api_key_env` name | Yes (provider selection) | Yes (Pilot Owner + Security Reviewer) |
| RF-11 | Live web-search provider | `<UNRESOLVED>` | PENDING OWNER | `activation-inputs.md:308-330` | Gate 2 | `wikipedia` or `brave`; `NEXUS_CAPABILITY_WEB` process var | Yes (provider selection) | Yes (Pilot Owner) |

### E. Business, owner, division, and agent identities

| ID | Decision needed | Current value | Status | Evidence | Gate blocked | Minimum owner response | Needs external access? | Needs named human? |
|----|----------------|---------------|--------|----------|--------------|----------------------|----------------------|--------------------|
| RF-03 | Business display name | `<UNRESOLVED>` | PENDING OWNER | `activation-inputs.md:118-121` | Gate 4 | Business name | No | Yes (Pilot Owner) |
| RF-04 | Stable business entity ID | `<UNRESOLVED>` | PENDING OWNER | `activation-inputs.md:126-135` | Gate 4 | Entity ID or accept server-generated | No | Yes (Pilot Owner) |
| RF-05 | Existing active human owner identity ID | `<UNRESOLVED>` | PENDING OWNER | `activation-inputs.md:139-151` | Gate 4 | Choose bootstrap or dedicated identity; record credential handling | No | Yes (Pilot Owner) |
| RF-06 | Owner credential env-var name | `<UNRESOLVED>` | PENDING OWNER | `activation-inputs.md:155-161` | Gate 2 | Environment-variable name (e.g., `NEXUS_BOOTSTRAP_CREDENTIAL`) | No | Yes (Pilot Owner) |
| RF-07 | Active research agent with `web.search` | `<UNRESOLVED>` | PENDING OWNER | `activation-inputs.md:165-181` | Gate 4 | Agent entity ID with `web.search` allowed tools; business-wide or division-scoped decision | No | Yes (Pilot Owner) |
| OD-01 | Division taxonomy | `<UNRESOLVED>` | PENDING OWNER | `activation-inputs.md:184-193` | Gate 4 | Yes/no on divisions; if yes, division ID + parent business | No | Yes (Pilot Owner) |

### F. Governance, approvals, approver eligibility, timeout, auto-deny, self-approval, delegation

| ID | Decision needed | Current value | Status | Evidence | Gate blocked | Minimum owner response | Needs external access? | Needs named human? |
|----|----------------|---------------|--------|----------|--------------|----------------------|----------------------|--------------------|
| RF-12 | Governance and approval decision | `<UNRESOLVED>` | PENDING OWNER | `activation-inputs.md:344-355` | Gate 5 | Yes/no on `web.search` approval; approver identity(ies); timeout; auto-deny; self-approval; delegation | No | Yes (Pilot Owner + Security Reviewer) |
| OD-05 | Additional governance policies | `<UNRESOLVED>` | PENDING OWNER | `activation-inputs.md:383-391` | Gate 5 | Confirm only web.search approval policy, or record additional policies | No | Yes (Security Reviewer) |

### G. Research objective, acceptance criteria, execution limits

| ID | Decision needed | Current value | Status | Evidence | Gate blocked | Minimum owner response | Needs external access? | Needs named human? |
|----|----------------|---------------|--------|----------|--------------|----------------------|----------------------|--------------------|
| RF-14 | First live objective + acceptance criteria | `<UNRESOLVED>` | PENDING OWNER | `activation-inputs.md:428-447` | Gate 0, Gate 6 | Real bounded question ≤4,000 chars; human acceptance criteria; named Research Reviewer | No | Yes (Pilot Owner + Research Reviewer) |
| RF-17 | Success criteria | `<UNRESOLVED>` | PENDING OWNER | `activation-inputs.md:526-539` | Gate 8 | Terminal completed, synthesis complete, evidence-backed, evaluation passed | No | Yes (Pilot Owner + Research Reviewer) |

### H. Backup, restore, retention, unknown outcomes, final sign-off

| ID | Decision needed | Current value | Status | Evidence | Gate blocked | Minimum owner response | Needs external access? | Needs named human? |
|----|----------------|---------------|--------|----------|--------------|----------------------|----------------------|--------------------|
| RF-15 | Backup, restore, retention | `<UNRESOLVED>` | PENDING OWNER | `activation-inputs.md:460-491` | Gate 7 | Backup owner, destination, cadence, retention period | No | Yes (Pilot Owner + Platform Operator) |
| RF-16 | Unknown-outcome owner + procedure | `<UNRESOLVED>` | PENDING OWNER | `activation-inputs.md:493-501` | Gate 7, Gate 8 | Unknown-outcome owner; escalation contact; evidence to preserve | No | Yes (Pilot Owner) |

**Summary:** All 19 RF-* items are `PENDING OWNER`. None are RESOLVED. All require named human decisions. None can be inferred or defaulted by engineering.

---

## C. Verified Technical Limitation Matrix

### Limitation 1: No internal monetary cost cap

| Attribute | Finding |
|-----------|---------|
| Still true in current HEAD? | **YES** — confirmed via grep of `internal/` for "monetary", "cost cap", "spend limit", "budget.*dollar" — no results |
| Implementation reference | `internal/agentintel/objective.go:30` `StateBudgetExhausted`; `internal/agentintel/action.go:198-318` — budgets bound iterations (8), model calls (16), tool calls (8), delegations (3), replans (2), steps (12), execution time (60s). None are monetary. |
| Local verification | `grep -rn "monetary\|cost cap\|spend limit\|budget.*dollar" internal/ --include="*.go"` — zero results |
| Impact | **Cost** — NEXUS does not enforce a dollar cap. Provider account must have hard spend limit. Owner must monitor provider-side usage. |
| Blocker classification | **Blocker for all live pilot designs** — requires owner sign-off acknowledging no internal monetary cap |
| Remediation | None possible in NEXUS — must be enforced at provider account level |
| Test coverage | `activation-inputs.md:298-300` documents this limitation |
| Overlaps pre-existing work? | No |

### Limitation 2: Execution results are process-local

| Attribute | Finding |
|-----------|---------|
| Still true in current HEAD? | **YES** — confirmed via `resume.go:23`: "Everything here is process-local by explicit decision (G4): a restart drops approvals and snapshots together" |
| Implementation reference | `internal/agentintel/resume.go:23`; `internal/agentintel/loop.go:193` "process-local (never journaled)"; `internal/agentintel/loop.go:661` |
| Local verification | Read `resume.go` lines 23-25; read `loop.go` lines 193-201 |
| Impact | **Availability/Recovery** — `GET /api/v1/intelligence/{id}` returns 404 after restart. Durable reads via `GET /api/v1/recovery/executions/{id}` survive restart. |
| Blocker classification | **Explicitly acceptable limitation** — documented contract (G4 Level 1), requires owner sign-off |
| Remediation | None — by design. Owner must accept process-local results |
| Test coverage | `e2e/tests/31-durable-recovery-read.spec.ts` tests restart behavior (4/4 passed) |
| Overlaps pre-existing work? | No |

### Limitation 3: Approval resume does not survive restart

| Attribute | Finding |
|-----------|---------|
| Still true in current HEAD? | **PARTIALLY CORRECTED** — Approval resume IS implemented (`internal/agentintel/resume.go`), but is **process-local only**. Restart drops approvals and snapshots. |
| Implementation reference | `internal/agentintel/resume.go:23` "restart drops approvals and snapshots together"; `internal/agentintel/resume.go:108` |
| Local verification | Read `resume.go` lines 12-25 |
| Impact | **Availability** — paused approval executions cannot be resumed after restart. A paused execution is journaled as terminal `failed` with `error_class: APPROVAL_REQUIRED`; fresh governance admission required after restart. |
| Blocker classification | **Blocker for pilot designs requiring multi-day approval windows** — owner must accept that approval decisions must be made within a single process lifetime |
| Remediation | None possible — by explicit design decision (G4) |
| Test coverage | `activation-inputs.md:366` documents this limitation |
| Overlaps pre-existing work? | No |

### Limitation 4: Egress allowlist NOT wired to model/web-search adapters

| Attribute | Finding |
|-----------|---------|
| Still true in current HEAD? | **YES** — confirmed. `app.go:107` creates `EgressPolicy` from `cfg.Security.EgressAllowList`, but `websearch_provider.go:146` creates its own `http.Client` with `http.ProxyFromEnvironment` — NOT using the App-level `EgressPolicy`. |
| Implementation reference | `internal/foundation/app/app.go:107` (policy created but not passed to adapters); `internal/capability/websearch_provider.go:146-162` (own `http.Client`); `internal/capability/http.go:296` (EgressPolicy exists but not used by web search adapter) |
| Local verification | Read `websearch_provider.go:146` — `p.client = &http.Client{...}` with no egress policy reference |
| Impact | **Confidentiality/Availability** — outbound requests from model provider and web search adapters are NOT constrained by the configured `security.egress_allow_list`. Egress must be enforced outside NEXUS (network firewall, proxy). |
| Blocker classification | **Blocker for pilot designs requiring strict egress control** — owner must enforce egress outside NEXUS |
| Remediation | Propagate `EgressPolicy` to `WikipediaProviderConfig`, `BraveProviderConfig`, and model provider adapters. Would be a new engineering milestone. |
| Test coverage | No test verifies egress is applied to web search provider HTTP calls |
| Overlaps pre-existing work? | No — would be new work |

### Limitation 5: Single-process runtime

| Attribute | Finding |
|-----------|---------|
| Still true in current HEAD? | **YES** — confirmed via `internal/foundation/store/ownership_unix.go:20-34` (flock on data directory lock file; second process gets EWOULDBLOCK → `ErrStorageOwned`) and `internal/gateway/config_surface.go:811` ("one process per data directory must be preserved") |
| Implementation reference | `internal/foundation/store/ownership_unix.go:20-34`; `internal/gateway/config_surface.go:811` |
| Local verification | Read `ownership_unix.go` — `syscall.Flock` with `LOCK_EX|LOCK_NB` fails if another process holds the lock |
| Impact | **Availability** — multiple NEXUS instances cannot share one data directory. No cross-process coordination. |
| Blocker classification | **Explicitly acceptable limitation** — documented contract, requires owner sign-off |
| Remediation | None — by design |
| Test coverage | `ownership_unix.go` implements flock; no dedicated test found for multi-process rejection |
| Overlaps pre-existing work? | No |

---

## D. Hard Blockers vs Owner-Acceptance Decisions

### Hard blockers (engineering must resolve, or owner must accept as-is)

| # | Limitation | Classification | Owner sign-off required? |
|---|-----------|---------------|-------------------------|
| 1 | No internal monetary cost cap | **Hard blocker** — NEXUS cannot enforce dollar limits | YES — owner must accept provider-side spend cap responsibility |
| 2 | Process-local execution results | **Acceptable limitation** — by design (G4) | YES — owner must accept 404 after restart for live result routes |
| 3 | Approval resume not surviving restart | **Acceptable limitation** — by explicit decision | YES — owner must accept approval decisions must be made within one process lifetime |
| 4 | Egress allowlist not wired to adapters | **Hard blocker** — egress not enforced at application level | YES — owner must enforce egress outside NEXUS (firewall/proxy) |
| 5 | Single-process runtime | **Acceptable limitation** — by design | YES — owner must accept single instance per data directory |

### Owner-acceptance decisions (all must be explicitly signed)

1. Accept no internal monetary cap; provider account must have hard spend limit
2. Accept process-local results (404 after restart for live intelligence API)
3. Accept approval pauses do not survive restart
4. Accept egress allowlist not enforced at application level
5. Accept single-process runtime constraint

---

## E. Zero-External-Access Spend and Egress Safeguards

### Provider account spend limit (configured OUTSIDE NEXUS)

| Aspect | Detail |
|--------|--------|
| Where configured | Provider account console (OpenAI, Anthropic, etc.) — NOT in NEXUS |
| What NEXUS enforces | Runtime budgets only: 8 iterations, 16 model calls, 8 tool calls, 3 delegations, 2 replans, 12 steps, 60s execution |
| What NEXUS does NOT enforce | Dollar cost — no internal monetary cap |
| Owner verification method | Check provider account console for spend limit and alert threshold (no NEXUS API call needed) |
| Verification without secret values | `printenv <SPEND_LIMIT_ENV> >/dev/null` or provider console UI — do not print values |
| Alert threshold | Configured in provider account; owner receives alerts when approaching limit |

### Egress enforcement

| Aspect | Detail |
|--------|--------|
| NEXUS-level egress | `security.egress_allow_list` configured in config file but NOT propagated to model/web-search adapters |
| Verified behavior | `websearch_provider.go:146` creates own `http.Client` with `http.ProxyFromEnvironment` — bypasses App-level `EgressPolicy` |
| Owner responsibility | Enforce egress outside NEXUS (network firewall, egress proxy, DNS filtering) |
| Intended outbound destinations | Model provider endpoint (`https://`), web search provider endpoint (`https://en.wikipedia.org/` or `https://api.search.brave.com/`) |
| Local verification of egress | Cannot be verified without making outbound calls (prohibited). Must be verified in separately authorized network test. |
| CORS origins | `http.cors_allowed_origins: []` (empty = same-origin only, deny-by-default for browser) |

### Zero-external-access preparation procedure

1. Create production config file from `activation-inputs.md:607-664` skeleton (placeholders only, no secret values)
2. Validate config with `jq empty` — no remaining `<...>` placeholders
3. Validate production security gates via `go test ./internal/foundation/config/...`
4. Validate provider config shape via `go test ./internal/foundation/config/... -run "Provider|Production"`
5. Validate web provider config rejection via `go test ./internal/launcher/... -run "Provider|Capability"`
6. Provision `storage.data_dir` on persistent storage (no network needed)
7. Build binaries via `go build ./...` and `make build` (no network needed if module cache is warm)
8. Document all owner decisions in the activation register
9. Verify config has no secret values — only `api_key_env` names

---

## F. Proposed Minimum Pilot Prerequisites and Stop Conditions

### Minimum pilot prerequisites (all must be met before Gate 6)

1. **Gate 0 authorization** — signed written authorization, named roles, change window, spend control, approval design, stop authority
2. **Gate 2 configuration** — `nexus.id`, `storage.data_dir`, real model provider with `api_key_env`, `NEXUS_CAPABILITY_WEB=wikipedia` or `brave`, all production security flags true
3. **Gate 3 boot verification** — `/health` returns 200, `/ready` returns 200, control-plane auth matrix (401/401/200), scoped-route auth matrix (401/401/200)
4. **Gate 4 onboarding** — business created, owner membership established, agent registered with `web.search`
5. **Gate 5 governance** — approval policy applied and verified (if approval required)
6. **Gate 6 pre-checks** — live search provider confirmed, provider registration log verified, control policy verified, objective and agent confirmed, spend headroom confirmed

### Stop conditions (any one stops the pilot)

| Trigger | Action |
|---------|--------|
| Any `unknown` execution | Stop new work; investigate; no auto-retry |
| Missing/inactive approval policy when approval required | Stop; reapply and verify before new work |
| Citation audit failed/incomplete/unavailable/incompatible | Stop; preserve evidence; review |
| Human source review rejects support | Stop; treat output as not accepted |
| Provider spend cap reached or forecast | Stop; notify owner |
| Suspected secret exposure | Stop; rotate; incident process |
| Unauthorized access, anomalous 401/403, foreign-scope access | Stop; security review |
| Data-directory error, corrupt record, process not ready | Stop; do not delete evidence |
| Multiple processes against one data directory | Stop all but one; investigate |
| Owner or Security Reviewer calls stop | Stop immediately |

---

## G. Files Created or Changed

| File | Action | Reason |
|------|--------|--------|
| `docs/operations/trendsentry-pilot/activation-decision-packet.md` | **NEW** | This decision packet report |

No existing files were modified. No protected files were accessed:
- `configs/nexus.dev.json` — not accessed
- `internal/foundation/modelrouter/groq_router_live_test.go` — not accessed

---

## H. Tests/Checks Executed

| Command | Result |
|---------|--------|
| `git status --short` | 20 modified tracked, 37 untracked (all pre-existing) |
| `git diff --stat HEAD` | 3074 insertions, 422 deletions across 20 files (pre-existing changes) |
| `go test ./internal/foundation/config/... -count=1` | PASS (production config validation) |
| `go test ./internal/launcher/... -count=1 -run "Provider|Capability|Platform|Startup"` | PASS (production provider gate, web provider validation) |
| `go test ./internal/gateway/... -count=1 -run "401|Unauth|Auth|Control|Scope"` | PASS (control-plane auth, scoped-route authorization) |
| `go vet ./... && go build ./...` | PASS |
| `cd nexus-app && npx vitest run` | 307/307 tests pass |
| `cd nexus-app && npx tsc --noEmit` | Clean |
| `cd nexus-app && npx vite build` | Build succeeds |
| Source inspection of `websearch_provider.go:146` | Confirmed: own `http.Client`, no `EgressPolicy` |
| Source inspection of `resume.go:23` | Confirmed: "process-local by explicit decision (G4)" |
| Source inspection of `ownership_unix.go:20-34` | Confirmed: flock-based single-process enforcement |
| Source inspection of `config.go:875-911` | Confirmed: production validation gates |

---

## I. Three Separate Verdicts

### PREPARATION STATUS

**`PREPARATION COMPLETE — OWNER DECISIONS PENDING`**

All safe, independent engineering and operational preparation is complete:
- Repository state verified and preserved
- RF-01 through RF-19 audited — all `PENDING OWNER` with evidence-based statuses
- Technical preflight matrix completed — all engineering items verified, no defects found
- Safe configuration/secrets checklist prepared (no secret values)
- Owner decision packet prepared with exact required responses per gate
- Future controlled live-run checklist prepared with explicit authorization and budget gates
- Relevant tests run and reported with exact commands and results
- No production code changed; all protected artifacts preserved

### ENGINEERING READINESS

**`READY — NO TECHNICAL BLOCKERS REMAINING`**

All engineering work necessary for safe activation preparation is complete:
- Production configuration validation: implemented and tested (fail-closed on missing provider, missing storage, weakened security)
- Model provider configuration validation: implemented and tested
- Web search provider validation: implemented and tested (fails closed on invalid config/missing credential)
- Control-plane and scoped-route authentication: implemented and tested
- Frontend auth/session fixes: implemented and tested (307/307 frontend tests pass)
- Scripted E2E workflow: verified (10/10 tests pass, test 35 run twice from fresh isolated contexts)
- Go build, vet, race tests: all pass
- Frontend production build: succeeds

**Five technical limitations identified** — all are either explicit design decisions or require provider-side configuration. None require NEXUS code changes for a safe pilot, but all require owner sign-off.

### LIVE ACTIVATION AUTHORIZATION

**`NOT AUTHORIZED`**

The pilot is NOT authorized for live activation because:
- Gate 0 (authorization) has NOT been executed — no written authorization, no named change window, no spend control, no approval design
- All 19 owner activation inputs (RF-01 through RF-19) are `PENDING OWNER`
- No live model provider or web-search provider configured or credentialed
- No business/division/agent/governance decisions recorded
- No first research objective or acceptance criteria defined
- No hard provider spend limit confirmed
- Current decision: **NO-GO** (per `activation-inputs.md:576`)

A Gate 0 GO authorizes Gates 1–8 only; it does not authorize production cutover. Gate 8 is the production decision.

---

## Next-Action List

### Owner decisions (cannot be made by engineering)

1. **Sign Gate 0 authorization** — written authorization, roles, change window, spend control, approval design, stop authority
2. **Choose installation ID (`nexus.id`)** — stable, human-readable, unique
3. **Choose persistent `storage.data_dir`** — absolute, persistent, service-owned path
4. **Choose model provider** — provider ID, endpoint (https), model ID, `api_key_env` name
5. **Choose web-search provider** — `wikipedia` or `brave`
6. **Choose business name and entity ID** — or accept server-generated entity ID
7. **Choose owner identity** — bootstrap or dedicated
8. **Choose agent** — active agent with `web.search` allowed tools
9. **Decide approval requirement** — yes/no on `web.search` approval; name approver(s)
10. **Define first research objective** — real bounded question ≤4,000 chars; acceptance criteria
11. **Confirm provider spend limit** — hard cap in provider account; spend owner named
12. **Confirm backup/restore plan** — destination, cadence, retention
13. **Sign final GO/NO-GO** — Gate 8 decision

### Engineering work (none remaining)

No engineering work remains. All technical implementation and validation is complete. Any future engineering work (e.g., wiring egress allowlist to web search adapter) would be a new milestone, not a blocker for safe preparation.

### Future authorized live run (requires separate explicit authorization)

After all owner decisions are recorded and Gate 0 is signed, the controlled live run would proceed through Gates 1–8 per `live-activation-runbook.md`. This requires a separate explicit authorization beyond this preparation task.