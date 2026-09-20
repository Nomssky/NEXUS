# NEXUS — Development (M2 Governance Engine)

This document covers **only** M2: the governance engine layered on top of the
M0 foundation and M1 identity/security primitives. It does not duplicate
architecture or contract docs.

> M2 extends M1. It adds the **governance policy engine, approval engine,
> constraint evaluation, and the 5 canonical governance outcomes**. It does
> **not** implement persistence, events, cognition, agents, tools, or any
> autonomous behavior (those are M3+).

---

## Scope (C03 Governance & Policy)

| Component | What M2 adds |
|---|---|
| **C03 Governance** | policy engine, 5 canonical outcomes, precedence rules, approval engine, constraint evaluation, fail-safe default deny, no self-approval |

### Invariants preserved by M2

These are structural, tested properties — not conventions:

- **Governance outcomes exactly 5:** `ALLOW`, `DENY`, `REQUIRE_APPROVAL`, `ALLOW_WITH_CONSTRAINTS`, `ESCALATE`. No other outcomes exist.
- **Fail-safe default deny:** when no policy matches or governance is unavailable, the outcome is `DENY`.
- **More-restrictive-wins:** when policies conflict, the most restrictive outcome takes precedence.
- **Precedence hierarchy:** `SYSTEM_SAFETY > GLOBAL > BUSINESS > DIVISION > AGENT > WORKFLOW > TASK`.
- **No self-approval:** requesters cannot approve their own actions.
- **Governance never bypassed:** enforcement lives outside the model.

---

## Layout

```
internal/foundation/governance/
  outcome.go       5 canonical outcomes (Outcome type, parsing, validation)
  policy.go        Policy record, Subject/Action/Resource, Conditions, Constraints, ApprovalConfig
  request.go       Request and Decision types, RiskLevel, ApprovalState
  engine.go        Core policy engine (evaluate, match, precedence, override)
  approval.go      Approval engine (request, approve, deny, timeout)
  failsafe.go      Fail-safe wrappers, outcome validation
```

---

## Usage

```go
import "github.com/Nomssky/NEXUS/internal/foundation/governance"

// Create policies
policies := []*governance.Policy{...}

// Create engine
engine := governance.NewEngine(policies)

// Evaluate a request
req := governance.Request{
    Actor:       "agent-1",
    Action:      "execute_tool",
    Resource:    "tool-1",
    ResourceType: "tool",
    BusinessID:  "business-1",
    Timestamp:   time.Now(),
}

decision := engine.Evaluate(req)

switch decision.Outcome {
case governance.ALLOW:
    // proceed
case governance.DENY:
    // blocked
case governance.REQUIRE_APPROVAL:
    // submit for approval
case governance.ALLOW_WITH_CONSTRAINTS:
    // proceed with constraints
case governance.ESCALATE:
    // escalate to higher authority
}
```

---

## Fail-Safe

Always use the fail-safe wrapper when governance availability is uncertain:

```go
decision := governance.FailSafe(engine, req)
// Guarantees a valid Decision, defaults to DENY if engine is nil
```

---

## Testing

- 30 tests covering TEST-M2-001..030
- M0 tests (TEST-M0-001..015) unchanged and passing
- M1 tests (TEST-M1-001..036) unchanged and passing
- Race detector clean
- Locked-layer guard passes (no `contracts/` or `Core/` changes)

---

## Invariants Preserved

- Governance outcomes exactly 5 (structural type constraint)
- Fail-safe default deny (tested with nil engine)
- More-restrictive-wins (tested with conflicting policies)
- No self-approval (tested with SelfApprovalProhibited)
- Precedence hierarchy (tested with scope-level ordering)
- Policy override relationships (tested with OverridePolicyIDs)
- Expired/disabled policies ignored (tested with time-based exclusion)
