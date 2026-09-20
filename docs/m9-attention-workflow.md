# NEXUS — Development (M9 Attention + Autonomous Workflow)

This document covers **only** M9: full attention and autonomous workflow.
It does not duplicate architecture or contract docs.

> M9 extends M0–M8. It adds **Full Attention Engine** with priority
> scoring, aggregation, dedup, suppression guardrails, cooldown, budget,
> quiet-hours, escalation, and notification routing. It does **not**
> implement multi-business parallel (M10) or 24/7 hardening (M11).

---

## Scope (C10 full)

| Component | What M9 adds |
|---|---|
| **Full Attention Engine** | priority scoring, suppression guardrails, cooldown, budget, quiet-hours, escalation levels |
| **Suppression Guardrails** | never hide security/policy/owner/severity/scope/evidence |
| **Budget Enforcement** | max interruptions/hour, max urgent/day |
| **Quiet Hours** | suppress low-level notifications during off-hours |

### Invariants preserved by M9

- **ATTENTION ≠ AUTHORITY** — attention recommends, governance authorizes
- **Suppression never hides severity increases**
- **Suppression never hides scope changes**
- **Suppression never hides new evidence**
- **Suppression never hides policy violations**
- **Suppression never hides critical security signals**
- **Suppression never hides direct owner messages**

---

## Layout

```
internal/foundation/attention/
  attention.go       Full attention: scoring, suppression, budget, quiet-hours
  attention_test.go  20 tests (TEST-M9-001..020)
```

---

## Usage

```go
import "github.com/Nomssky/NEXUS/internal/foundation/attention"

// 1. Create full attention engine
fae := attention.NewFullAttentionEngine(
    attention.AttentionBudget{MaxOwnerInterruptionsPerHour: 10},
    attention.QuietHours{Enabled: true, MinLevel: attention.LevelCritical},
    attention.SuppressionGuard{
        NeverSuppressSecuritySignals:  true,
        NeverSuppressPolicyViolations: true,
        NeverSuppressOwnerMessages:    true,
    },
)

// 2. Submit attention item
item, _ := fae.SubmitItem("Critical issue", "Desc", "biz-1", "security",
    9, 8, 7, 0.9, true) // isSecuritySignal=true

// 3. Check suppression
if fae.ShouldSuppress(item) {
    // suppressed by guardrails or cooldown
}

// 4. Check budget
if fae.CheckBudget() {
    // send notification
    fae.RecordInterruption()
}

// 5. Acknowledge and resolve
fae.Acknowledge(item.ID)
fae.Resolve(item.ID)
```

---

## Testing

- 20 new tests covering TEST-M9-001..020
- M0–M8 tests unchanged and passing
- Race detector clean
- Locked-layer guard passes (no `contracts/` or `Core/` changes)
