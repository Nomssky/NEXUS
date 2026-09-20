# NEXUS — LOCKED Milestones

This document tracks which milestones are **LOCKED** — meaning their baseline,
scope, and integration sequence have been verified and the implementation is
frozen. No further modifications to LOCKED milestone code are permitted without
a formal unlock procedure.

---

## LOCKED Milestones

| Milestone | SHA | Merged | Locked Date | Status |
|-----------|-----|--------|-------------|--------|
| M0 — Foundation | `252f717` | PR #7 | 2026-09-20 | **LOCKED** |
| M1 — Identity & Security | `7efefd1` | PR #5 | 2026-09-20 | **LOCKED** |
| M2 — Governance Engine | `e85ccc7` | PR #9 | 2026-09-20 | **LOCKED** |
| M3 — Persistence + Events + Observability | `2580b58` | PR #11 | 2026-09-20 | **LOCKED** |
| M4 — Core Cognition | `ecc674f` | PR #13 | 2026-09-20 | **LOCKED** |
| M5 — First Bootable NEXUS | `a788918` | PR #15 | 2026-09-20 | **LOCKED** |
| M6 — Model Router + Providers | `9c654d7` | PR #17 | 2026-09-20 | **LOCKED** |
| M7 — First Safe Autonomous Agent | `9c56599` | PR #19 | 2026-09-20 | **LOCKED** |
| M8 — Memory + Knowledge | `50663e3` | PR #21 | 2026-09-20 | **LOCKED** |
| M9 — Attention + Autonomous Workflow | `335484c` | PR #23 | 2026-09-20 | **LOCKED** |
| M10 — Multi-Business Parallel | `cd969ce` | PR #25 | 2026-09-20 | **LOCKED** |

## Lock Criteria

A milestone is LOCKED when ALL of the following are true:

1. **Implementation PR merged** into `master`
2. **Predecessor milestones locked** (or milestone is M0)
3. **CI green** — all quality checks pass
4. **Tests pass** — `go test ./...` and `make test-race` clean
5. **Locked-layer guard** passes — no `Core/` or `contracts/` modifications
6. **Baseline audit complete** — ancestry, merge-base, and diff scope verified
7. **No scope violations** — PR diff matches declared milestone scope

## Lock Invariants

Once locked, a milestone's code:

- **Cannot be modified** except via explicit unlock + new PR
- **Cannot introduce** changes to `Core/` or `contracts/` directories
- **Cannot break** existing tests or invariants
- **Must preserve** all declared invariants from the milestone's scope

## M0 Lock Details

- **Commit:** `252f717` (merge of PR #7)
- **Scope:** C01 Foundation & Config (layer L0)
- **Components:** config, logging, nerrors, lifecycle, health, app, version
- **Tests:** TEST-M0-001..015 (38 tests)
- **Invariants:** UNKNOWN_OUTCOME ≠ FAILURE, CANCELLATION ≠ failure
- **Locked-layer guard:** TEST-M0-015 (baseline: `12a3eb3`)

## M1 Lock Details

- **Commit:** `7efefd1` (merge of PR #5)
- **Scope:** C01 Configuration + C02 Identity + C04 Security Primitives
- **Components:** config (snapshot, security section), identity (types, scopes, memberships, authn, authz), security (SecretRef, Secret, Resolver, egress)
- **Tests:** TEST-M1-001..036 (plus M0 tests unchanged)
- **Invariants:**
  - IDENTITY ≠ AUTHORITY ≠ CAPABILITY ≠ PERMISSION ≠ TRUST
  - AUTHENTICATION ≠ AUTHORIZATION
  - AUTHORIZATION ≠ GOVERNANCE
  - CAPABILITY ≠ PERMISSION
  - SECRET_REF ≠ SECRET_VALUE
  - BUSINESS_SCOPE ≠ GLOBAL_ACCESS
  - CONFIGURATION ≠ AUTHORITY
  - RESOURCE ≠ PERMISSION
  - EXTERNAL_INPUT ≠ TRUSTED_INPUT

---

## M2 Lock Details

- **Commit:** `e85ccc7` (merge of PR #9)
- **Scope:** C03 Governance & Policy Engine
- **Components:** outcome (5 canonical outcomes), policy (records, matching), engine (evaluation, precedence, override), approval (request/approve/deny, timeout), failsafe (default deny)
- **Tests:** TEST-M2-001..030 (30 tests)
- **Invariants:**
  - Governance outcomes exactly 5 (structural type constraint)
  - Fail-safe default deny
  - More-restrictive-wins
  - No self-approval
  - Precedence hierarchy preserved

## M3 Lock Details

- **Commit:** `2580b58` (merge of PR #11)
- **Scope:** C05 Persistence + C06 Event Substrate + C07 Observability
- **Components:** store (Record, Store interface, MemStore), event (Event, Bus, priority queue, dedup), observability (Tracer, MetricsCollector, AuditLog, MemRecorder)
- **Tests:** TEST-M3-001..044 (44 tests)
- **Invariants:**
  - Events are facts never commands
  - Observability cannot grant authority
  - Secrets redacted before storage
  - Optimistic concurrency
  - Business isolation

## M4 Lock Details

- **Commit:** `ecc674f` (merge of PR #13)
- **Scope:** C08 Core Cognitive Control (Objective, Decision, Planner, Executive)
- **Components:** objective (lifecycle, hierarchy, WHY, decomposition), decision (framing, evidence, options, recommend/abstain/escalate), planner (plan, mission, task, dependencies, validation), executive (intent → objective → decision → plan)
- **Tests:** TEST-M4-001..024 (24 tests)
- **Invariants:**
  - WHY mandatory for all objectives
  - WHY preserved through decomposition
  - Objective ≠ Authorization
  - C08 does not execute actions
  - Business isolation enforced

## M5 Lock Details

- **Commit:** `a788918` (merge of PR #15)
- **Scope:** C11 Workflow + Scheduling + minimal C12 Agent + minimal C13 Tool
- **Components:** workflow (lifecycle, task graph, dependencies, verification), scheduler (job queue, priority, leasing, deadline, retry), agent (definition, provisioning, lifecycle, heartbeat), tool (READ-ONLY registry, validation, business isolation)
- **Tests:** TEST-M5-001..038 (38 tests)
- **Invariants:**
  - WHY mandatory for all workflows
  - Business isolation across all components
  - Governance never bypassed
  - Read-only tools only (M5 scope)
  - Task dependencies enforced
  - Verification from evidence

## M6 Lock Details

- **Commit:** `9c654d7` (merge of PR #17)
- **Scope:** C12 Model Router & Provider Abstraction
- **Components:** registry (model definitions, capabilities, pricing), provider (interface, local, remote), router (routing, local-first, failover), health (monitoring), accounting (tokens/cost)
- **Tests:** TEST-M6-001..022 (22 tests)
- **Invariants:**
  - Routing ≠ Authorization
  - Local-first policy
  - Failover without unsafe duplicates
  - Privacy-aware routing
  - Provider credential isolation
  - Business isolation in accounting

## M7 Lock Details

- **Commit:** `9c56599` (merge of PR #19)
- **Scope:** C12 Agent Lifecycle (enhanced) + C10 Attention (basic)
- **Components:** agent (identity, authority chain, budget, spawn controls, heartbeat/recovery, lease fencing, cancel semantics, termination), attention (items, priority, escalation, review routing)
- **Tests:** TEST-M7-001..024 (24 tests)
- **Invariants:**
  - capabilities ⊆ authority ⊆ parent authority
  - capability ≠ permission
  - Agent cannot modify policy/audit/approval
  - Cancel ≠ failure
  - Timeout → UNKNOWN, not failure
  - Spawn storm prevented by hard limits
  - Business isolation
  - Full audit trail

## M8 Lock Details

- **Commit:** `50663e3` (merge of PR #21)
- **Scope:** C09 Memory & Context Intelligence
- **Components:** memory (admission, scoped retrieval, archive, forget), context (assembly, token budget), knowledge (ingestion pipeline, provenance)
- **Tests:** TEST-M8-001..020 (20 tests)
- **Invariants:**
  - Scoped retrieval (no "everything NEXUS knows")
  - Observation ≠ permanent memory
  - Memory/knowledge can never authorize
  - Knowledge untrusted until validated
  - Provenance always tracked
  - Business isolation

## M9 Lock Details

- **Commit:** `335484c` (merge of PR #23)
- **Scope:** C10 Full Attention & Priority Intelligence
- **Components:** attention (priority scoring, suppression guardrails, cooldown, budget, quiet-hours, escalation levels, notification routing)
- **Tests:** TEST-M9-001..020 (20 tests)
- **Invariants:**
  - ATTENTION ≠ AUTHORITY
  - Suppression never hides severity increases
  - Suppression never hides scope changes
  - Suppression never hides new evidence
  - Suppression never hides policy violations
  - Suppression never hides critical security signals
  - Suppression never hides direct owner messages

## M10 Lock Details

- **Commit:** `cd969ce` (merge of PR #25)
- **Scope:** Multi-Business Parallel Operation (all layers hardened)
- **Components:** isolation tests across workflow, scheduler, agent, memory, knowledge, tool, model router, accounting, attention
- **Tests:** TEST-M10-001..015 (15 tests)
- **Invariants:**
  - business_id required on all structures
  - No cross-business data leak
  - Parallel execution
  - UI switch never stops other businesses
  - Cross-business requires explicit policy + audit

## Unlocked Milestones

| Milestone | Status | Blocked By |
|-----------|--------|------------|
| M11 — 24/7 Hardening | Available | M10 locked ✅ |

---

*Last updated: 2026-09-20*
