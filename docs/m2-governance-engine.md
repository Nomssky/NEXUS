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
- **Precedence hierarchy:** `GLOBAL > BUSINESS > DIVISION > AGENT > WORKFLOW > TASK`. (An earlier draft listed a `SYSTEM_SAFETY` level above `GLOBAL`; no such level exists in the contract or the implementation and it has been removed.)
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

## Evaluation semantics (contracts §2.8 ↔ §9.5)

`contracts/SCHEMA_GOVERNANCE_ATTENTION.md` states the evaluation algorithm
twice: procedurally in §2.8 (locked — never rewritten to match the code) and
as a restatement in §9.5, which is the operative description of what
`governance.Engine` actually does:

1. collect active, in-scope, non-expired policies that match
   subject/action/resource;
2. order by restrictiveness `DENY > ESCALATE > REQUIRE_APPROVAL >
   ALLOW_WITH_CONSTRAINTS > ALLOW`;
3. break ties by higher `precedence`, then by narrower scope
   (SCHEMA_COMMON: on ambiguity, choose the narrowest possible scope);
4. no match → `DENY`.

The two sections agree on deny-first and on the default `DENY`; §9.5 adds the
tie-breaks §2.8 leaves implicit. Where they differ is step 5 of §2.8 ("if no
deny, first explicit effect wins"): read literally that ranks non-deny effects
by precedence alone, whereas the engine ranks them by restrictiveness *first*,
so a lower-precedence `REQUIRE_APPROVAL` still beats a higher-precedence
`ALLOW`. §9.5 states the implemented order, so §9.5 governs. §2.8's default
`DENY` is likewise qualified by §9.4: an unconfigured installation allows
because the read-only `default-allow` built-in is seeded at boot — without
that carve-out §2.8 step 6 applies verbatim.

The winning policy is observable on the wire: the `POLICY_DENIED` /
`APPROVAL_REQUIRED` / `ESCALATION_REQUIRED` error message carries
`matched policy <policy_id> (v<policy_version>, precedence <n>)`, and an
`ALLOW_WITH_CONSTRAINTS` decision surfaces as the terminal result's
`constraints` (`type:expression`, reported and never enforced).

Precedence *levels* (`GLOBAL > BUSINESS > DIVISION > AGENT > WORKFLOW > TASK`,
`engine.go`) are a separate axis from a policy's numeric `precedence`: they
disambiguate scope, not effect. There is no `SYSTEM_SAFETY` level — earlier
comments named one, and the corrected list is now identical everywhere
(`internal/foundation/governance/policy.go`).

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
