# NEXUS v52 — Agent Selection and End-to-End Execution Report

## A. Initial and Final Git State

**Branch:** `feat/one-click-launcher`, HEAD `f1fbab8` (unchanged)

**Pre-existing changes preserved (uncommitted, untouched):**
- Modified: `nexus-app/src/App.tsx`, `nexus-app/src/api/client.test.ts`, `nexus-app/src/api/client.ts`, `nexus-app/src/pages/CommandCenter.module.css`, `nexus-app/src/pages/CommandCenter.tsx`
- New (v48-v51 artifacts): `scripts/launch-nexus-native.sh`, `nexus.desktop`, `nexus-native.desktop`, `configs/nexus.dev.json`, `docs/operations/`, `e2e/tests/30-trendsentry-research-workflow.spec.ts`, `e2e/tests/31-durable-recovery-read.spec.ts`, `internal/foundation/modelrouter/groq_router_live_test.go`, `nexus-app/src/components/OfficeFloor/`, `nexus-app/src/components/OfficeLayout/`, `node_modules/`, `test-results/`, `workspaces/`, `cmd/nexus/nexus`, plus v49-v51 report markdown files

**New files in v52:**
- `nexus-app/src/components/CommandCenter/CommandInput.test.tsx` — regression tests for agent selection

**No commits, pushes, merges, resets, or stash mutations.** Stash `stash@{0}` (`601b233f…`) intact. All 4 worktrees preserved.

## B. Files Changed and Why

### Modified files:

1. **`nexus-app/src/components/CommandCenter/CommandInput.tsx`** — Added agent selection to Ask NEXUS:
   - Added `agents: Agent[]` prop, populated from `useAgents(businessId)` in CommandCenter
   - Filters to active agents only (`status === 'active'`)
   - Renders an `<select>` dropdown with active agent names
   - Passes `context: { agent_id: selectedAgentId }` in the `submitIntelligence` mutation
   - Shows empty state ("No active agents available") when no agents exist
   - Shows error ("Select an agent before submitting") when agents exist but none selected
   - Clears selection when agents change (business/division switch invalidates stale selection)
   - Duplicate submission prevention preserved (submitCount guard + isPending button disable)

2. **`nexus-app/src/components/CommandCenter/CommandCenter.tsx`** — Passes `agents={agents}` to `CommandInput`

3. **`nexus-app/src/components/CommandCenter/CommandInput.module.css`** — Added styles for agent selector dropdown and empty state banner

### New files:

4. **`nexus-app/src/components/CommandCenter/CommandInput.test.tsx`** — 8 regression tests covering:
   - Agent option rendering (active agents shown, suspended/archived excluded)
   - Empty state when no active agents
   - Submit disabled when no agent selected
   - Submit enabled when agent selected
   - Selection cleared on scope change (invalidation)
   - Request serialization: `context.agent_id` included in submit payload
   - No submission when no agent selected
   - Duplicate submission prevention (double-click = 1 call)

## C. API Contract

**`SubmitIntelligenceRequest`** (from `nexus-app/src/types/api.ts`):
```typescript
export interface SubmitIntelligenceRequest {
  business_id: string;
  actor_id: string;
  objective: { description: string };
  context?: Record<string, string>;  // Use agent_id to select a specific registered agent
}
```

The `context.agent_id` field routes the objective to a specific registered agent. Without it, the backend rejects the execution ("no runtime evidence of progress"). With it and a `web.search`-capable agent, the execution completes successfully.

## D. Actual Successful UI Execution Evidence

**Verified through Chromium CDP against the running frontend at `http://localhost:3000`:**

1. **Authentication:** `localStorage` set with `nx:human:bootstrap` + bootstrap credential, page reloaded into Office
2. **Office loaded:** Heading "Default", sidebar shows "1 agents"
3. **Ask NEXUS input:** Textarea and agent selector both present
4. **Agent selected:** "Research Agent" (`nx:agent:research-01`, with `web.search` tool) selected in dropdown
5. **Objective submitted:** "research thoroughly: search the web for nexus capabilities"
6. **Execution ID tracked:** `api-1793190540…` appears in Live Feed
7. **Terminal result:** Status shows `completed`
8. **Real result data:** Synthesis from 4 captured sources, 2 tool calls, 3 iterations

**Live Feed evidence:**
```
LIVE FEED
1
Completed
api-1793190540…
7:29:00 PM
completed
Failed
api-1793185263…
6:01:03 PM
failed
```

The earlier `failed` execution (without agent_id) is contrasted with the new `completed` execution (with agent_id), proving the fix works.

**Screenshot:** `/tmp/ui_workflow_v52_result.png`

## E. Test/Build Results

| Check | Result |
|-------|--------|
| Frontend unit tests (`vitest run`) | 18 files / 287 tests passed |
| TypeScript (`tsc --noEmit`) | Clean |
| Production build (`npm run build`) | Succeeded |
| TrendSentry E2E test 30 | 4/4 passed |
| CommandInput regression tests | 8/8 passed |
| Launcher `--status` | Backend + frontend running |
| UI workflow (Chromium CDP) | Auth → Office → select agent → submit → completed — all verified |

## F. Scripted Provider vs Real Web Research

**Verified (scripted provider):**
- `NEXUS_SEEDED_PROVIDER_MODE=scripted` + `NEXUS_CAPABILITY_WEB=scripted` (set by `launch-nexus-native.sh`)
- Produces `status: completed`, `research_synthesis.status: complete`, `evidence_backed` with `source_ids`
- E2E test `e2e/tests/30-trendsentry-research-workflow.spec.ts`: **4/4 passed**

**Not verified (real external web research):**
- No external provider credentials are configured
- No paid provider calls made
- No external network requests triggered
- The scripted provider is deterministic test evidence, **not** proof that a live external research provider is configured

## G. Recommended Next Step

The Office floor agent cards (`OfficeFloor.tsx`) currently show hardcoded static data (6 agents with fixed names/statuses) while the workspace sidebar shows live agent count from `useAgents(businessId)`. The natural next step is to wire `OfficeFloor` to the live `useAgents(businessId)` data so the floor reflects real registered agents, making the virtual office a true reflection of the running system.

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

## Final Verification Evidence Summary

| Aspect | Evidence |
|--------|----------|
| Auth → Office | ✅ Login → "Default" office, sidebar shows "1 agents" |
| Ask NEXUS input | ✅ Textarea + agent selector rendered |
| Agent selection | ✅ "Research Agent" selected from dropdown |
| Objective submission | ✅ `POST /api/v1/intelligence/execute` with `context.agent_id` |
| Execution tracking | ✅ `api-1793190540…` in Live Feed |
| Terminal result | ✅ `completed` status with synthesis from 4 sources |
| Duplicate prevention | ✅ Double-click = 1 mutate call |
| No agent → error | ✅ "Select an agent before submitting" shown |
| No agents → empty state | ✅ "No active agents available" shown |
| Scope change → selection cleared | ✅ `useEffect` invalidates stale selection |
