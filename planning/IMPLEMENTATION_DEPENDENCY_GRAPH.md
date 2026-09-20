# NEXUS — Implementation Dependency Graph

PLANNING ARTIFACT. Graph over implementation components C01–C14, derived from the
LOCKED architecture + Phase 1–4 contracts. Does NOT modify architecture or
contracts. Companion to `IMPLEMENTATION_PLAN.md §6–§7` and
`IMPLEMENTATION_COMPONENT_MAP.md`.

---

## 1. Notation

- Edge `A -> B` means "B depends on A" (A must exist/be correct before B).
- Direction of *dependency* is `FOUNDATION -> CORE -> RUNTIME -> EXTERNAL`.
- Direction of *authority* is the same and MUST NOT be reversed.
- Bidirectional data flow is drawn as two concern-labelled edges (§4).

---

## 2. Full Component Graph

```text
L0  C01 Foundation & Config
     └─(reads anywhere)──────────────────────────────────────────────┐
                                                                      │
L1  C02 Identity & Trust ─┐                                          │
    C03 Governance ───────┼─> authority/scope resolution             │
    C04 Security (x-cut) ─┘                                          │
                                                                      │
L2  C05 Persistence ──────┐                                          │
    C06 Event Substrate ──┼─> durable state + events + telemetry    │
    C07 Observability ────┘   (x-cut sink)                           │
                                                                      │
L3  C08 Core Cognition <── C02,C03,C04 (authority) , C05,C06,C07     │
    C09 Memory/Knowledge <── C05,C06,C07                             │
    C10 Attention <── C05,C06,C07                                    │
                                                                      │
L4  C11 Workflow/Scheduling <── C05..C10                             │
    C12 Agent/Model Runtime <── C05..C11                             │
                                                                      │
L5  C13 Tool/Integration <── C05..C12                                │
                                                                      │
L6  C14 Communication/Control Surface <── C01..C13  (observes, no    │
                                                       authority) ◀───┘
```

---

## 3. Edge List (component-level dependencies)

| From | To | Purpose |
|---|---|---|
| C01 | (all) | validated, version-pinned config |
| C01, C02, C05 | C03 | policy needs identity, scopes, durable policy/approval state |
| C01, C02, C03 | C04 | security consumes identity + governance outcomes |
| C01 | C05 | store configuration/engines |
| C01, C05, C07 | C06 | event substrate needs durability + correlation/audit |
| C01, C05 | C07 | telemetry/audit sink durability |
| C02, C03, C04, C05, C06, C07, C09, C10 | C08 | cognition over authorized, durable, observed facts |
| C05, C06, C07 | C09 | memory admission + evidence durability |
| C05, C06, C07 | C10 | signal prioritization over durable events |
| C05…C10 | C11 | workflow over cognition + memory + attention + events |
| C05…C11 | C12 | agent/model runtime over workflow + governance |
| C05…C12 | C13 | tool/integration over runtime, with credentials |
| C01…C13 | C14 | control surface over all; grants no authority |

---

## 4. Bidirectional Data Flows (directional by concern)

| Pair | Core issues | Peer returns | Peer may NOT |
|---|---|---|---|
| C08 ↔ C09 | queries, admission requests | data, evidence, provenance | authorize, execute |
| C08 ↔ C10 | routing/review requests | prioritized signals | authorize |
| C11 ↔ C06 | event emission, subscriptions | events (facts) | command execution |
| C12 ↔ C13 | tool/model invocation | validated results, health | authorize |

In every bidirectional pair, only the downstream component returns *data*; it
never returns *authority*.

---

## 5. Prohibited Edges (must never exist)

```text
Model             -> Authority        (model output never authorizes)
Tool result       -> Governance       (tool result never mutates policy)
Attention         -> Authorization    (attention never authorizes)
Objective/WHY     -> Security bypass  (why never bypasses safety)
External system   -> internal authority
Memory/Knowledge  -> Authorization
Observability     -> Authority
Event             -> Command/Authority
Provider health   -> Authorization
Resource avail.   -> Permission
```

These prohibitions are enforced by invariant tests (see component map §4) and are
non-negotiable because they instantiate the locked invariant set.

---

## 6. Acyclicity

The component graph is acyclic: every edge points from a lower layer to a higher
layer (L0…L6). Cross-cutting components (C04, C07) are callable from all layers
but are drawn as sinks for *telemetry/security enforcement* without creating a
dep-return path that could invert authority.

---

## 7. Build-Order Cross-Check

The build order in `IMPLEMENTATION_PLAN.md §7` (C01, C02, C04, C03, C05, C07,
C06, C08-subparts, C11, C12, C13, C09, C10, C14) is a *linearization* of this
graph that respects every edge. Two deliberate linearization choices:

- C04 (security primitives) is built before C03 (governance engine) because the
  governance contracts consume the secret-store/credential-isolation primitives.
- C07 (observability) is built before C06 (event substrate) because the substrate
  depends on durable correlation/audit from its first run.

Neither choice introduces a cycle; both are consistent with the edge list above.

---

## 8. Compliance

- No edge grants authority upward or sideways.
- No prohibited edge exists.
- Graph is acyclic.
- Consistent with LOCKED architecture and contracts; no modification required.
