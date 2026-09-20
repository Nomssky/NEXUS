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

## Unlocked Milestones

| Milestone | Status | Blocked By |
|-----------|--------|------------|
| M3 — Persistence + Events | Available | M2 locked ✅ |
| M4 — Core Cognition | Blocked | M3 |
| M5 — First Bootable NEXUS | Blocked | M4 |
| M6 — Model Router | Blocked | M5 |
| M7 — First Safe Autonomous Agent | Blocked | M6 |
| M8 — Memory + Knowledge | Blocked | M5 |
| M9 — Attention + Autonomous Workflow | Blocked | M8 |
| M10 — Multi-Business Parallel | Blocked | M9 |
| M11 — 24/7 Hardening | Blocked | M10 |

---

*Last updated: 2026-09-20*
