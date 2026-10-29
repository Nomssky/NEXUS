# NEXUS v51 — TrendSentry End-to-End Usability & Desktop Readiness Report

## A. Initial and Final Git State

**Branch:** `feat/one-click-launcher`, HEAD `f1fbab8` (unchanged)

**Pre-existing changes preserved (uncommitted, untouched):**
- Modified: `nexus-app/src/App.tsx`, `nexus-app/src/api/client.test.ts`, `nexus-app/src/api/client.ts`, `nexus-app/src/pages/CommandCenter.module.css`, `nexus-app/src/pages/CommandCenter.tsx`
- New (v48/v49/v50 artifacts): `scripts/launch-nexus-native.sh`, `nexus.desktop`, `nexus-native.desktop`, `configs/nexus.dev.json`, `docs/operations/`, `e2e/tests/30-trendsentry-research-workflow.spec.ts`, `e2e/tests/31-durable-recovery-read.spec.ts`, `internal/foundation/modelrouter/groq_router_live_test.go`, `nexus-app/src/components/OfficeFloor/`, `nexus-app/src/components/OfficeLayout/`, `node_modules/`, `test-results/`, `workspaces/`, `cmd/nexus/nexus`, plus v49–v51 report markdown files

**No commits, pushes, merges, resets, or stash mutations.** Stash `stash@{0}` (`601b233f…`) intact. All 4 worktrees preserved.

**No source files were modified in v51.** This milestone was verification + assessment only.

## B. End-to-End TrendSentry UI Workflow Evidence

**Environment:** Frontend served via vite dev server at `http://localhost:3000` (API proxy to gateway on `8081`), NEXUS backend running with `NEXUS_SEEDED_PROVIDER_MODE=scripted` and `NEXUS_CAPABILITY_WEB=scripted`.

**Verification method:** Chromium headless with Chrome DevTools Protocol — authenticated via `localStorage` (`nexus-auth` with actor `nx:human:bootstrap` + bootstrap credential), navigated to Virtual Office, submitted objective through Ask NEXUS, observed Live Feed and status badges.

**Workflow steps verified:**

| Step | Result | Evidence |
|------|--------|----------|
| Authentication → Office | ✅ | Office heading "Default", sidebar shows "1 agents" after API registration |
| Ask NEXUS input present | ✅ | `textarea[placeholder="Ask NEXUS anything…"]` found |
| Submit objective | ✅ | Submit button clicked, execution accepted |
| Execution ID tracked | ✅ | Live Feed shows `api-1793185263…` at `6:01:03 PM` |
| Terminal state displayed | ✅ | Status badges: `["Failed","failed"]` — honest terminal state |
| Error details | ✅ | Error envelope with code/category/message shown |
| Duplicate submission prevention | ✅ | Double-click submit → exactly 1 feed entry (dedup via `isPending` + submit counter) |
| Screenshot captured | ✅ | `/tmp/ui-workflow-result.png` and `/tmp/dup-test-result.png` |

**DOM evidence (Live Feed section):**
```
LIVE FEED
1
Failed
api-1793185263…
6:01:03 PM
failed
NEEDS ATTENTION
No pending approvals
```

**Terminal failure (UI):** The execution correctly reports `failed` — honest, not fabricated. This happens because `CommandInput` submits `{business_id, actor_id, objective:{description}}` without `context.agent_id`, and no agent is auto-assigned. The backend correctly rejects work without an executable agent.

**Terminal success (confirmed via same API path):** Registering an agent (`nx:agent:research-01` with `web.search`) and submitting via `POST /api/v1/intelligence/execute` with `context:{agent_id}` produces:
```json
{
  "status": "completed",
  "outcome": {
    "summary": "state=completed | objective=research thoroughly: search the web for nexus capabilities | agent=nx:agent:research-01 | iterations=3 tool_calls=2 delegations=0 model_calls=3 replans=0 observations=2 | result=source-grounded synthesis",
    "research_synthesis": {
      "status": "complete",
      "summary": "Synthesized from 4 captured source(s) in this execution.",
      "evidence_backed": [{"statement":"…","source_ids":["src-1","src-2","src-3","src-4"]}]
    },
    "research_evaluation": {…present…}
  }
}
```

**Limitation:** The UI's `CommandInput` does not pass `context.agent_id` or `division_id`, so a successful TrendSentry run through the UI alone requires prior agent registration via the API. The UI wiring (`CommandInput` → `POST /api/v1/intelligence/execute` → poll `GET /api/v1/intelligence/{id}` → `ExecutionDetail` inspector) is correct and identical to the E2E-tested path.

## C. Scripted Provider vs Real Web Research

**Verified (scripted provider):**
- `NEXUS_SEEDED_PROVIDER_MODE=scripted` + `NEXUS_CAPABILITY_WEB=scripted` (set by `launch-nexus-native.sh` and by E2E `startResearchNexus()`)
- Produces `status: completed`, `research_synthesis.status: complete`, `evidence_backed` with `source_ids`, `research_evaluation` present
- E2E test `e2e/tests/30-trendsentry-research-workflow.spec.ts`: **4/4 passed**
  - Owner onboards, establishes membership, executes research objective (completed)
  - Cross-business isolation (foreign identity denied 403)
  - Authentication and membership boundaries (401/403 fail-closed)
  - Restart persistence (business/membership durable; execution result process-local per G4 Level 1)

**Not verified (real external web research):**
- No external provider credentials are configured
- No paid provider calls made
- No external network requests triggered
- The scripted provider is deterministic test evidence, **not** proof that a live external research provider is configured
- Real web research would require provider configuration and credentials beyond scope of this milestone

**Distinction:** Scripted-provider success ✅ verified. External-provider research ❌ not configured, not claimed.

## D. Arch Linux Desktop Launcher

**Existing launcher:** `scripts/launch-nexus-native.sh` — already a complete desktop application launcher.

**Launcher behavior verified:**
| Feature | Status |
|---------|--------|
| Start backend + frontend | ✅ Backend ready at `http://127.0.0.1:8081/ready`, frontend at `http://127.0.0.1:8082/` |
| Dedicated app window | ✅ Firefox `--app="$SERVER_URL"` (no address bar, no tabs, no browser chrome) |
| Readiness detection | ✅ Polls `/ready` (gateway) and `/` (frontend) with timeouts |
| `--status` | ✅ Reports backend/frontend/Firefox state |
| `--stop` | ✅ Stops only launcher-owned processes (PID files) |
| Credential file permissions | ✅ `chmod 600` on `bootstrap-credential` |
| Secret handling | ✅ Credentials never printed to terminal/logs/browser URLs |
| `nexus.desktop` | ✅ `Exec=/home/pc/NEXUS/scripts/launch-nexus-native.sh`, `Terminal=false` |
| `nexus-native.desktop` | ✅ Same, alternative entry |

**Launcher --status output:**
```
NEXUS backend:    running (ready at http://127.0.0.1:8081/ready)
Frontend server:  running (http://127.0.0.1:8082)
Firefox app:      not running
Bootstrap credential: /home/pc/.nexus/bootstrap-credential (owner-only, not printed here)
```

**Assessment:** The launcher already opens a dedicated application window (Firefox `--app` mode) on Arch Linux. No Electron/Tauri or large new toolchain is needed. The smallest maintainable solution is already in place.

**No launcher changes were made** — the existing implementation is sufficient. If Firefox is not installed on a target Arch system, the launcher's `firefox --app=...` command will fail gracefully (no crash, clear error in log). A browser-availability pre-check could be added as a future improvement but is not required.

## E. Files Changed

**No files were modified in v51.** This milestone was verification + assessment only.

Build artifacts regenerated (no source changes):
- `nexus-app/dist/assets/index-Bu8CSXSn.js` (production rebuild, same source)
- `nexus-app/dist/assets/index-Qk1nq6xc.css`

Temporary verification artifacts (outside repo, not committed):
- `/tmp/ui-workflow-result.png` — authenticated Office screenshot
- `/tmp/dup-test-result.png` — duplicate-submission test screenshot
- `/tmp/ui_workflow.mjs`, `/tmp/dup_submit.mjs` — CDP verification scripts

## F. Test/Build Results and Remaining Gaps

| Check | Result |
|-------|--------|
| Frontend unit tests (`vitest run`) | 17 files / 279 tests passed |
| TypeScript (`tsc --noEmit`) | Clean |
| Production build (`npm run build`) | Succeeded |
| TrendSentry E2E test 30 | 4/4 passed |
| Launcher `--status` | Backend + frontend running |
| Launcher `--stop`/`--start` | Verified (processes owned by launcher) |
| UI workflow (Chromium CDP) | Auth → Office → Submit → Live Feed → terminal state — all verified |
| Duplicate submission | Verified (double-click = 1 entry) |

**Remaining gaps (documented, not blocked):**
1. **CommandInput doesn't pass `agent_id`/`division_id`:** UI submission produces `failed` (honest) unless an agent is pre-registered via API and the request includes agent context. Recommendation: add agent selection to CommandInput or auto-select a registered agent in a future milestone.
2. **Real web research not configured:** Scripted provider works; external provider requires credentials/configuration not in scope.
3. **Agent cards use static data:** OfficeFloor shows 6 hardcoded agent cards while workspace sidebar shows live agent count (`useAgents`). Recommend wiring OfficeFloor to live agent data.
4. **Viewport height mismatch:** Frontend at 1440×881 vs Penpot frame 1440×1024 (from v50).

## G. Smallest Next Step to Make TrendSentry Useful for Real Work

**Immediate next step (smallest, safe):** Add agent selection to `CommandInput` so the UI can pass `context.agent_id` when submitting an objective. This requires:
1. Fetch agents via `useAgents(businessId)` in `CommandInput`
2. Render an agent selector (dropdown) next to the objective input
3. Include `context: { agent_id: selectedAgentId }` in the `submitMutation.mutate()` call

This is a targeted UI change (not a redesign) that would allow a user to register an agent once (via API) and then successfully submit TrendSentry objectives through the Ask NEXUS interface, producing `completed` results with real evidence-backed synthesis through the UI.

**Launcher:** No change needed — Firefox `--app` mode already provides a dedicated desktop window on Arch Linux.

## Repository Safety

All protections honored:
- `configs/nexus.dev.json`: never read/modified
- `internal/foundation/modelrouter/groq_router_live_test.go`: never touched
- `docs/operations/`, e2e tests 30/31, `node_modules/`, `test-results/`, `workspaces/`: preserved
- Stash `stash@{0}` (`601b233f…`): intact
- All worktrees preserved (`/home/pc/NEXUS`, `/tmp/nexus-integration`, `/tmp/nexus-publish-phase10-14`, `/tmp/nexus-recovery-phase10-14`)
- No `git reset --hard`, `git clean`, forced switches, destructive cleanup, or stash mutation
- No credentials printed
- No commits, pushes, merges, releases, or deployments
- No paid provider calls, external network requests, or model downloads