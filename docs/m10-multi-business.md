# NEXUS — Development (M10 Multi-Business Parallel)

This document covers **only** M10: multi-business isolation hardening.
It does not duplicate architecture or contract docs.

> M10 extends M0–M9. It adds **isolation hardening across all layers**
> and verifies parallel execution of independent businesses. It does **not**
> implement 24/7 hardening (M11).

---

## Scope (All Layers Hardened)

| Layer | Isolation Verified |
|---|---|
| **Workflow** | business_id on workflows and tasks |
| **Scheduler** | business-scoped job leasing |
| **Agent** | business_id required, no cross-business task leak |
| **Memory** | scoped retrieval by business |
| **Knowledge** | scoped ingestion and retrieval |
| **Tool** | business-scoped tool access |
| **Model Router** | per-business accounting |
| **Attention** | business-scoped attention items |
| **Accounting** | per-business token/cost tracking |

### Invariants preserved by M10

- **business_id required** on all data structures
- **No cross-business data leak** — retrieval scoped to business
- **Parallel execution** — independent businesses run concurrently
- **UI switch never stops other businesses** — focus change is local
- **Cross-business requires explicit policy + audit**

---

## Layout

```
internal/foundation/isolation/
  isolation_test.go   15 tests (TEST-M10-001..015) cross-layer isolation
```

---

## Testing

- 15 new isolation tests covering TEST-M10-001..015
- Tests verify isolation across workflow, scheduler, agent, memory, knowledge, tool, model router, accounting, attention
- M0–M9 tests unchanged and passing
- Race detector clean
- Locked-layer guard passes (no `contracts/` or `Core/` changes)
