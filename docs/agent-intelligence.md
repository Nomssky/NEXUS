# NEXUS — Agent Intelligence Layer v1

**Status:** implemented (baseline `5264d75` → this milestone).
Normative contract: `contracts/AGENT_INTELLIGENCE_CONTRACTS.md`.
It is a **layer above** the Agent Execution Layer
(`contracts/AGENT_EXECUTION_CONTRACTS.md`); G1–G5 remain authoritative and
are inherited unchanged — intelligence never admits work, never grants
authority, and never widens scope.

Companion documents: [planning.md](planning.md),
[autonomous-tool-use.md](autonomous-tool-use.md),
[intelligent-delegation.md](intelligent-delegation.md),
[agent-memory.md](agent-memory.md), [agent-execution.md](agent-execution.md).

---

## 1. What changed

| | Agent Execution v1 | Agent Intelligence v1 |
|---|---|---|
| input | a declared task + tools/delegates/workflow | an **objective** |
| flow | select → tools → model → result | plan → validate → decide → act → observe → (replan) → terminate |
| model role | answers one generation call | **proposes** one structured action per iteration |
| autonomy | none (declared composition) | bounded by runtime-enforced budgets |
| termination | request completes/fails/cancels | the loop reaches one of five terminal states |

## 2. Architecture

```
POST /api/v1/intelligence/execute
      │ admission: identity (G1), scope (G3), active org (G2), visibility (G5)
      ▼
core request pipeline ─ identity → authorization → governance → approval/escalation
      ▼ (handler = agentintel.Runtime)
 ┌──────────── control loop (bounded, observable) ─────────────┐
 │ created → planning → executing ⇄ observing → replanning → …  │
 │   plan      planner proposes   model proposes   observation  │
 │   validation: unknown agent/tool/deps/cycles/scope ⇒ reject   │
 │   action validation: schema → type → scope → permissions →   │
 │                       budgets → cancellation                  │
 │   mediated effects ONLY: tool registry · delegated child ·    │
 │                           memory store · model call · finish  │
 └───────────────────────────────────────────────────────────────┘
      ▼
terminal: completed | failed | cancelled | budget_exhausted | deadline_exceeded
      ▼
request result: outcome.summary carries state=… and the loop counters
```

The loop reuses the Agent Execution Layer's tool boundary
(`agentexec.InvokeTool`) and delegation primitive
(`agentexec.DelegateChild`) — there is no second framework and no second
authorization path.

## 3. Semantics at a glance

| Question | Answer |
|---|---|
| Who may run an objective? | Exactly the callers who may run a request: authenticated identity, membership covering the declared scope (G3), active business/division (G2). |
| What may the model do? | Propose one structured action. Nothing else. |
| What may the model *not* do? | Change identity/business/division/actor, grant itself a tool, bypass governance/approval, touch foreign records, execute host commands. |
| How long can it run? | `max_iterations` (8), `max_model_calls` (16), `max_tool_calls` (8), `max_delegations` (3), `max_replans` (2), `max_steps` (12), `max_depth` (2), `max_execution_time` (60s), `max_consecutive_no_progress` (2) — all enforced in the runtime; callers may only tighten them. |
| What if it never finishes? | Iteration budget or no-progress rule terminates it. There is no unbounded loop. |
| What if the plan is bad? | Rejected before any action (`failed`, `plan rejected: …`). |
| What if the action is bad? | Rejected before any executor sees it (`failed`, `action rejected: …`). |
| What if a tool fails? | Terminal failure with the tool id. Tool failure never becomes success. |
| What if a child fails? | Terminal failure naming the child. |
| What if it is cancelled? | Loop stops at the next boundary and the request ends `cancelled`. |
| What survives a restart? | Agent definitions and agent memory (durable records). Objectives, plans, observations, working memory: **nothing** — the execution id answers `404` (G4). |

## 4. Terminal state on the wire

The Agent Execution response contract is unchanged; the intelligence terminal
state is carried in `outcome.summary` as `state=<state>` plus counters
(`iterations`, `tool_calls`, `delegations`, `model_calls`, `replans`,
`observations`), and every non-`completed`/`cancelled` state is a `failed`
result whose `error.message` starts with `state=<state>:`. A model asserting
"everything is fine" cannot overturn any of it.

## 5. Prompt-injection boundary

```
RUNTIME AUTHORITY > MODEL PROPOSAL > TOOL / MEMORY / CHILD DATA > EXTERNAL DATA
```

* Tool results, memory values and child results enter the prompt inside a
  clearly delimited **observation block**, labelled as data.
* Only the model's structured `action` field is ever parsed; prose is never
  interpreted as intent, and no observation text is ever parsed for actions.
* Scope, tools, policy, identity and configuration are decided *outside* the
  loop, so no observation can change them — the guarantee is structural, not a
  claim of perfect prompt-injection prevention.

## 6. Test providers (simulation, not production)

The shipped binary seeds a *simulated* provider that answers with empty
completions; the control loop therefore fails honestly ("planner failed: no
JSON plan") until a provider that answers structurally is configured. For
deterministic, LLM-free exercise of the loop there is exactly one additional
simulation provider — `modelrouter.ScriptedProvider`, seeded by
`models.seeded_provider_mode = "scripted"` (env
`NEXUS_SEEDED_PROVIDER_MODE`). Its answers are a documented decision table over
the prompt (see `internal/foundation/modelrouter/scripted.go`); it is not
intelligence, it is a test double behind the provider abstraction, and
production providers plug into the same interface.

## 7. Routes

| Method | Path |
|---|---|
| `POST` | `/api/v1/intelligence/execute` → `202 {execution_id, correlation_id, status}` |
| `GET` | `/api/v1/intelligence/{execution_id}?business_id=` → `200` / `202` / `404` |
| `POST` | `/api/v1/intelligence/{execution_id}/cancel?business_id=` → E-005 |

## 8. Known limitations (v1, explicit)

* Success criteria are recorded, not machine-verified (contract §7).
* Children inherit the parent's governance decision; per-node governance
  re-evaluation is deliberately *not* introduced (open question from Agent
  Execution v1, left open by instruction).
* Delegates run sequentially; parallel fan-out is expressed with workflow
  nodes in the execution layer.
* Working memory is process-local scratch state, never persisted.
* No vector/embedding memory, no learned capability acquisition, no multi-agent
  debate, no cross-business delegation, no durable execution recovery.