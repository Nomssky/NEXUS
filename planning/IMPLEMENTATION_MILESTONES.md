# NEXUS — Implementation Milestones

PLANNING ARTIFACT. Dependency-ordered milestones over components C01–C14,
derived from the LOCKED architecture + Phase 1–4 contracts. Does NOT modify
architecture or contracts. Companion to `IMPLEMENTATION_PLAN.md §25–§26` and
`IMPLEMENTATION_DEPENDENCY_GRAPH.md`.

Milestones are cumulative gates: each unlocks the next, and each has an explicit
exit criterion expressed as *observable, testable* behavior.

---

## M0 — Repository Foundation 🔒 LOCKED
- **Status:** LOCKED (PR #7, SHA `252f717`, merged 2026-09-20)
- **Scope:** language/tooling scaffold, CI, config schema (C01 skeleton),
  persistence/event/observability interface stubs, invariant test harness.
- **Components:** none functional (scaffold for C01+).
- **Exit:** builds from one command; CI green; config schema validates; invariant
  test harness runnable (even if mostly skipped).
- **Lock:** No modifications permitted without explicit unlock procedure.

## M1 — Identity + Security Primitives 🔒 LOCKED
- **Status:** LOCKED (PR #5, SHA `7efefd1`, merged 2026-09-20)
- **Scope:** C01 (full config), C02 (identity/scope/membership/businesses/
  divisions), C04 (secret store interface, sandbox hooks, egress allow-list).
- **Components:** C01, C02, C04.
- **Exit:** identity ≠ authority enforced; `secret_ref` resolution works; secrets
  never appear in logs; cross-business scope resolution tested.
- **Lock:** No modifications permitted without explicit unlock procedure.

## M2 — Governance Engine 🔒 LOCKED
- **Status:** LOCKED (PR #9, SHA `e85ccc7`, merged 2026-09-20)
- **Scope:** C03 policy engine + approval engine + constraint evaluation;
  exactly 5 canonical outcomes; precedence; fail-safe.
- **Components:** C03 (depends on C01, C02, C05-stub).
- **Exit:** all 5 outcomes producible; more-restrictive-wins; unresolved conflict
  → `DENY+ESCALATE`; fail-safe when governance unavailable; no self-approval.
- **Lock:** No modifications permitted without explicit unlock procedure.

## M3 — Persistence + Event Substrate + Observability
- **Scope:** C05 durable stores (all categories, per plan §13); C07 correlation/
  audit/metrics/traces; C06 event bus, priority queues, worker pool, dedup, DLQ,
  safe replay, triggers.
- **Components:** C05, C06, C07.
- **Exit:** record survives restart; correlation chain reconstructable; delivery
  semantics honored (no fake exactly-once); replay causes no external side effect.

## M4 — Core Cognition
- **Scope:** C08 Objective (WHY + success criteria + scope), Decision, Planner
  (plan + validation handoff), Executive (coordination).
- **Components:** C08.
- **Exit:** owner intent → objective → decision → validated plan, all governed
  and persisted; WHY lineage intact; none of these execute actions.

## M5 — FIRST BOOTABLE NEXUS
- **Scope:** C11 Workflow + Scheduling, minimal C12 (agent invocation), minimal
  C13 (READ-ONLY tool), wired to M0–M4.
- **Components:** C11 + minimal C12 + minimal C13.
- **Exit:** the 17-step boot sequence in `IMPLEMENTATION_PLAN.md §9` completes;
  a controlled owner request produces a task executed once, verified from
  evidence, recorded, and recoverable after restart. **Governance never bypassed.**

## M6 — Model Router + Providers
- **Scope:** C12 Model Router (needs-based, local-first), Provider Interface,
  capability registry, health registry, invocation accounting, failover with
  reconcile-before-failover for side-effecting ops.
- **Components:** C12.
- **Exit:** local provider invoked via router; a provider failure routes/fails
  over without unsafe duplicate; routing ≠ authorization enforced.

## M7 — FIRST SAFE AUTONOMOUS AGENT
- **Scope:** C12 agent lifecycle with the full M7 attribute set (plan §10),
  anti-spawn-storm controls, heartbeat/recovery, lease fencing; basic C10 needed
  for review routing.
- **Components:** C12 + basic C10.
- **Exit:** an agent runs a bounded, governed task autonomously with explicit
  capabilities ⊆ authority, resource/timeout budgets, single retry owner, and
  full audit; spawn storm prevented by hard limits.

## M8 — Memory + Knowledge
- **Scope:** C09 memory admission/retrieval with scope, context assembly,
  knowledge ingestion with provenance.
- **Components:** C09 (depends on C05, C06, C07).
- **Exit:** scoped retrieval (no "everything NEXUS knows"); observation ≠
  permanent memory; memory/knowledge can never authorize.

## M9 — Attention + Controlled Autonomous Workflow
- **Scope:** C10 full (priority, aggregation, dedup, suppression guardrails,
  cooldown, budget, quiet-hours, escalation, notification); autonomous workflow
  control loops.
- **Components:** C10.
- **Exit:** ATTENTION ≠ AUTHORITY enforced; suppression never hides severity
  increases/scope changes/new evidence/policy violations/critical security
  signals/direct owner messages.

## M10 — Multi-Business Parallel Operation
- **Scope:** isolation hardening across all layers via `business_id`
  (+`division_id`); parallel execution; UI switch never stops other businesses.
- **Components:** all (hardened).
- **Exit:** isolation tests green at DB, event, workflow, agent, memory,
  knowledge, tool, model, provider, credential, config, observability, attention
  layers; cross-business requires explicit policy + audit.

## M11 — 24/7 Hardening + Recovery + Chaos Validation
- **Scope:** full failure playbook (plan §19); process/machine/provider/network/
  DB failure; worker death; unknown-outcome; queue storm; backpressure.
- **Components:** all.
- **Exit:** chaos suite green; crash → restart → reconstruct → reconcile; no
  unsafe duplicate side effects; graceful shutdown/drain verified.

---

## Milestone → Component Matrix

| Milestone | C01 | C02 | C03 | C04 | C05 | C06 | C07 | C08 | C09 | C10 | C11 | C12 | C13 | C14 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| M0 | ◦ | | | | | | | | | | | | | |
| M1 | ● | ● | | ● | | | | | | | | | | |
| M2 | ● | ● | ● | ● | ◦ | | | | | | | | | |
| M3 | ● | ● | ● | ● | ● | ● | ● | | | | | | | |
| M4 | ● | ● | ● | ● | ● | ● | ● | ● | | | | | | |
| M5 | ● | ● | ● | ● | ● | ● | ● | ● | | | ● | ◦ | ◦ | |
| M6 | ● | ● | ● | ● | ● | ● | ● | ● | | | ● | ● | ◦ | |
| M7 | ● | ● | ● | ● | ● | ● | ● | ● | | ◦ | ● | ● | ● | |
| M8 | ● | ● | ● | ● | ● | ● | ● | ● | ● | ◦ | ● | ● | ● | |
| M9 | ● | ● | ● | ● | ● | ● | ● | ● | ● | ● | ● | ● | ● | ◦ |
| M10 | ● | ● | ● | ● | ● | ● | ● | ● | ● | ● | ● | ● | ● | ● |
| M11 | ● | ● | ● | ● | ● | ● | ● | ● | ● | ● | ● | ● | ● | ● |

● = delivered/hardened, ◦ = minimal/stub.

---

## Compliance

- Milestones are dependency-ordered and consistent with the component graph.
- Two "first" gates (M5 Bootable, M7 Safe Autonomous Agent) are explicit.
- No milestone requires modifying architecture or contracts.
- No milestone introduces reverse authority or cross-business leakage.
