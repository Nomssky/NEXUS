# NEXUS — Planning (intelligence layer v1)

Contract: `contracts/AGENT_INTELLIGENCE_CONTRACTS.md` §3, §5.
Implementation: `internal/agentintel/planner.go`, `plan.go`.

## Planner

```
objective + context  ──▶  Planner.Plan  ──▶  Plan  ──▶  ValidatePlan  ──▶  execute
```

`Planner` is an interface with two implementations:

* **ModelPlanner** (default) — asks the routed model for a JSON plan. The
  model *proposes*; the runtime decides.
* **ScriptedPlanner** — a deterministic planner used by contract tests.

Planning and execution are deliberately separate so a future milestone can
insert human approval, plan inspection, simulation or replay between them
without touching the executor.

## Plan shape

```json
{
  "plan_id": "…", "version": 2, "parent_version": 1, "reason": "…",
  "steps": [
    { "step_id": "s1", "intent": "…", "required_capabilities": [],
      "dependencies": [], "preferred_agent": "", "allowed_tools": [],
      "success_criteria": [], "optional": false }
  ]
}
```

## Validation is fail-closed

`ValidatePlan` runs **before any step executes**, on the initial plan and on
every replan. It is pure (no side effects, no model input) and rejects:

| Rejection | Rule |
|---|---|
| unknown agent | `preferred_agent` not visible in the execution's business/division scope |
| unknown tool | tool id not registered, or not in the agent's allowlist |
| invalid dependency / cycle | unknown `depends_on`, self-dependency, or a cyclic graph |
| excessive steps | more than `max_steps` |
| invalid schema | blank/duplicate `step_id`, blank `intent` |
| cross-business / scope escalation | not expressible: the plan is stamped with the execution's business/division, and any agent/tool reference is resolved against that scope |

The strict policy: **one rejected step fails the whole objective**
(`failed`, `plan rejected: …`). Dropping rejected steps and continuing would be
a different product decision and would need its own contract change — it is not
a code toggle.

## Replanning

A model may propose `replan` with a reason. The runtime then:

1. checks the replan budget (`max_replans`, default 2);
2. asks the planner for a new plan;
3. stamps `version+1` and `parent_version`;
4. validates it exactly like the first plan;
5. emits `plan.replanned` with `version`, `parent_version`, `reason` and
   restarts step execution from the new plan.

Replanning is therefore bounded, versioned, and auditable; a rejected replan
fails the objective rather than silently keeping the old plan.