# NEXUS — Technology Decision Records (TDRs)

PLANNING ARTIFACT. These TDRs record *decisions, rationale, and reversibility*
for technology choices left open by the LOCKED architecture
(`Foundation spec/ARCHITECTURE.md §12` explicitly leaves database technology
unlocked). TDRs do NOT modify architecture or contracts. They are
implementation-planning guidance, and each is recorded as RECOMMENDED (adopt
unless a concrete blocker) or OPEN (criteria defined; choose at the milestone).

Guiding principle: prefer *reversible* choices behind clear interfaces. No vendor
is hard-coded across a boundary; adapters are swappable.

---

## TDR-001 — Primary Implementation Language
- **Status:** OPEN (recommend, not lock).
- **Decision:** implement the core (C01–C14) in a single primary language with a
  strong concurrency model, mature type/schema tooling, and durable worker
  support.
- **Rationale:** the architecture requires process isolation, leases, heartbeats,
  and schema validation; a single language reduces interface friction across the
  many component boundaries.
- **Reverse:** keep every cross-component boundary behind contracts (Phase 1–4),
  so an implementation language change does not change architecture.

## TDR-002 — Durable Store for Authoritative State
- **Status:** OPEN (criteria defined; select at M3).
- **Decision:** use a durable, transactional store for authoritative entity/
  config/governance/workflow/task state (plan §13 categories requiring strong
  consistency + transactions).
- **Criteria:** ACID transactions; lineage durability; per-`business_id`
  isolation; versioned configuration; recoverable after crash.
- **Reverse:** abstract behind C05's storage interface; engine choice is local to
  C05 and does not leak into other components.

## TDR-003 — Worker Isolation: Process-based
- **Status:** RECOMMENDED.
- **Decision:** workers/schedulers/executors run as isolated processes, not
  in-process threads, with leases + fencing tokens.
- **Rationale:** the 24/7 failure playbook (plan §19) requires independent
  restart, zombie stop, and stale-writer rejection; threads cannot deliver this
  reliably.
- **Reverse:** boundary is the scheduler/lease interface; swapping execution
  substrate does not change task semantics.

## TDR-004 — Append-only Event Log + Immutable Audit Store
- **Status:** RECOMMENDED.
- **Decision:** events and audit are append-only; audit is tamper-resistant;
  both keyed by correlation/causation IDs.
- **Rationale:** events = facts not commands; audit must reconstruct the full
  request→outcome chain and never be mutated.
- **Reverse:** replay is safe by contract; storage engine behind C06/C07
  interface.

## TDR-005 — Retrieval Index Rebuildable From Source of Truth
- **Status:** RECOMMENDED.
- **Decision:** semantic/keyword/graph indexes for C09 are *derived* and
  rebuildable from the durable source of truth; never the only copy.
- **Rationale:** memory admission durability is separate from index availability;
  index loss must not lose admitted memory/knowledge.
- **Reverse:** index engine is internal to C09.

## TDR-006 — Metrics/Telemetry Store (Time-series)
- **Status:** RECOMMENDED.
- **Decision:** telemetry/metrics in a time-series-capable store, tiered
  retention; critical events never dropped by sampling.
- **Reverse:** observability sink interface in C07.

## TDR-007 — Secret Store Abstraction
- **Status:** RECOMMENDED.
- **Decision:** all credentials resolved at runtime via `secret_ref` through a
  secret-store abstraction; raw credentials never embedded in prompts, memory,
  logs, artifacts, or model context.
- **Rationale:** locked invariant (raw credentials never embedded; secrets
  minimally exposed).
- **Reverse:** provider of the secret store is swappable behind the interface.

## TDR-008 — Local-first Model Providers
- **Status:** RECOMMENDED.
- **Decision:** local providers (e.g., Ollama, local Hugging Face) as default
  where practical; remote providers (e.g., OpenRouter, custom) behind the same
  Provider Interface; no hard-coded provider.
- **Rationale:** privacy, cost, and offline resilience; Model Router selects by
  need.
- **Reverse:** Provider Interface + capability registry make providers
  swappable.

## TDR-009 — Delivery Semantics Per Channel
- **Status:** RECOMMENDED.
- **Decision:** declare `at_most_once` / `at_least_once` / `effectively_once`
  explicitly per channel; `effectively_once` requires an idempotency key; never
  claim unsupported exactly-once.
- **Rationale:** locked invariant: no unsupported exactly-once guarantees.
- **Reverse:** semantics are a per-channel configuration of C06.

## TDR-010 — Scheduling/Lease Store With Fencing Tokens
- **Status:** RECOMMENDED.
- **Decision:** leases and their fencing tokens are durable; stale writers are
  rejected; heartbeat (liveness) ≠ progress.
- **Rationale:** prevents duplicate active workers and duplicate side effects
  after failover.
- **Reverse:** lease interface in C11.

---

## Summary Table

| TDR | Topic | Status |
|---|---|---|
| TDR-001 | Primary language | OPEN (recommend single language) |
| TDR-002 | Durable authoritative store | OPEN (criteria defined) |
| TDR-003 | Process-based workers | RECOMMENDED |
| TDR-004 | Append-only event + immutable audit | RECOMMENDED |
| TDR-005 | Rebuildable retrieval index | RECOMMENDED |
| TDR-006 | Time-series telemetry store | RECOMMENDED |
| TDR-007 | Secret store abstraction | RECOMMENDED |
| TDR-008 | Local-first providers | RECOMMENDED |
| TDR-009 | Per-channel delivery semantics | RECOMMENDED |
| TDR-010 | Durable lease + fencing | RECOMMENDED |

---

## Compliance

- No TDR changes architecture or contracts; each is a planning decision with
  rationale and reversibility.
- No TDR violates a locked invariant.
- OPEN items (TDR-001, TDR-002) are resolved at M0/M3 with the stated criteria;
  both are reversible behind component interfaces.
