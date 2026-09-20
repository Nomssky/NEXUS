# NEXUS — Development (M4 Core Cognition)

This document covers **only** M4: the core cognitive control layer (C08).
It does not duplicate architecture or contract docs.

> M4 extends M0–M3. It adds the **Objective Engine, Decision Engine,
> Planner, and Executive** — the cognitive pipeline that converts owner
> intent into validated plans. It does **not** execute actions, own
> model/provider selection, or own workflow durability.

---

## Scope (C08 Core Cognitive Control)

| Component | What M4 adds |
|---|---|
| **Objective Engine** | objective lifecycle, WHY preservation, hierarchy, decomposition, success criteria |
| **Decision Engine** | structured decisions, options, evidence, evaluation, recommend/abstain/escalate |
| **Planner** | plan production, mission decomposition, task dependencies, validation |
| **Executive** | intent classification, cognitive pipeline coordination |

### Invariants preserved by M4

- **WHY preserved through decomposition:** every child objective inherits parent purpose
- **Objective ≠ Authorization:** creating an objective does not grant authority
- **Outcome ≠ Authorization:** completing a task does not authorize actions
- **C08 does not execute:** Executive prepares for workflow, never executes
- **Models have no execution authority:** cognition produces plans, not actions
- **Business isolation:** plans and objectives respect scope boundaries

---

## Layout

```
internal/foundation/cognition/
  objective.go     Objective types, lifecycle, hierarchy, decomposition, WHY
  decision.go      Decision types, options, evidence, lifecycle
  planner.go       Plan, Mission, Task, dependencies, validation
  executive.go     Executive coordination: intent → objective → decision → plan
```

---

## Usage

```go
import "github.com/Nomssky/NEXUS/internal/foundation/cognition"

// Create engines
oe := cognition.NewObjectiveEngine()
de := cognition.NewDecisionEngine()
pl := cognition.NewPlanner()
ex := cognition.NewExecutive(oe, de, pl)

// Owner intent → full cognitive chain
req, err := ex.ReceiveIntent(
    "owner-1",
    "Increase revenue",
    "Revenue growth",
    "Q4 revenue target",  // WHY
    "20% increase",
    "biz-1",
)

// req.ObjectiveID, req.DecisionID, req.PlanID are all created
// req.Status == ExecutiveStatusReady (ready for workflow)
```

---

## Testing

- 24 tests covering TEST-M4-001..024
- M0–M3 tests unchanged and passing
- Race detector clean
- Locked-layer guard passes (no `contracts/` or `Core/` changes)

---

## Invariants Preserved

- WHY mandatory for all objectives (tested)
- WHY preserved through decomposition (tested)
- Cannot decompose inactive objectives (tested)
- Plan validation checks WHY presence (tested)
- Executive does not execute (structural: no Execute method)
- Decision options have reversibility and blast radius (tested)
- Plan scope enforces business isolation (tested)
