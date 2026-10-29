# v52 Implementation Plan — Agent Selection in Ask NEXUS

## Baseline (verified from v51)
- Branch: feat/one-click-launcher, HEAD f1fbab8 (uncommitted v48-v51 changes preserved)
- `CommandInput` submits `{business_id, actor_id, objective:{description}}` WITHOUT `context.agent_id`
- Backend contract: `SubmitIntelligenceRequest.context` is `Record<string, string>`, use `agent_id` to select a registered agent
- `useAgents(businessId)` → `GET /api/v1/agents?business_id=...` returns `{ agents: Agent[] }`
- Agent type: `{ entity_id, name, business_id, capabilities, allowed_tools, status: 'active'|'suspended'|'archived', ... }`
- Without agent_id: execution fails with "no runtime evidence of progress"
- With agent_id + web.search agent: execution completes successfully
- Duplicate submit prevention: isPending + submit counter (already works)

## Plan
1. Modify CommandInput.tsx: add `agents` prop, agent selector dropdown, pass `context: { agent_id }` on submit, empty state when no agents, clear stale selection on agent list change
2. Modify CommandCenter.tsx: pass `agents` prop to CommandInput
3. Modify CommandInput.module.css: add agent selector styling
4. Add regression tests for agent selection, request serialization, invalidation, duplicate prevention
5. Run: vitest, tsc, production build, E2E test 30
6. UI verification via CDP (auth → select agent → submit → completed result)

## Constraints
- Do not modify backend, tests 30/31, docs/operations, configs/nexus.dev.json, groq_router_live_test.go
- Preserve all uncommitted changes, worktrees, stash
- No commit/push/merge
- Scripted provider only, no external calls

## Status
Not started