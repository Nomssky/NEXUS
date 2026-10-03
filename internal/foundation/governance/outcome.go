// Package governance implements the NEXUS governance engine (C03).
//
// The governance engine is the highest control layer in NEXUS. It evaluates
// policies and produces exactly 5 canonical outcomes:
//
//   - ALLOW: action is permitted without conditions
//   - DENY: action is prohibited (default when no policy matches)
//   - REQUIRE_APPROVAL: action requires explicit approval before execution
//   - ALLOW_WITH_CONSTRAINTS: action is permitted with specific constraints
//   - ESCALATE: action cannot be resolved locally, must be escalated
//
// Governance is NEVER bypassed. When governance is unavailable, the system
// defaults to DENY (fail-safe).
package governance

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Outcome represents one of the 5 canonical governance outcomes.
// No other outcomes are permitted. This is a structural invariant.
type Outcome int

const (
	// ALLOW means the action is permitted without conditions.
	ALLOW Outcome = iota + 1

	// DENY means the action is prohibited. This is the default when no
	// policy matches or when governance is unavailable (fail-safe).
	DENY

	// REQUIRE_APPROVAL means the action requires explicit approval before
	// execution. The request must not proceed until approval is granted.
	REQUIRE_APPROVAL

	// ALLOW_WITH_CONSTRAINTS means the action is permitted but must comply
	// with specific constraints.
	ALLOW_WITH_CONSTRAINTS

	// ESCALATE means the action cannot be resolved at the current level
	// and must be escalated to a higher authority.
	ESCALATE
)

// String returns the canonical string representation of the outcome.
func (o Outcome) String() string {
	switch o {
	case ALLOW:
		return "ALLOW"
	case DENY:
		return "DENY"
	case REQUIRE_APPROVAL:
		return "REQUIRE_APPROVAL"
	case ALLOW_WITH_CONSTRAINTS:
		return "ALLOW_WITH_CONSTRAINTS"
	case ESCALATE:
		return "ESCALATE"
	default:
		return fmt.Sprintf("Outcome(%d)", int(o))
	}
}

// IsValid returns true if the outcome is one of the 5 canonical outcomes.
func (o Outcome) IsValid() bool {
	return o >= ALLOW && o <= ESCALATE
}

// MarshalJSON renders the outcome as its canonical enum name. SCHEMA_GOVERNANCE
// §2.1 fixes the vocabulary ("Policy outcomes are exactly ... No competing
// vocabulary") and §2.2/§3.1 type `effect`/`decision` as that enum, so the wire
// form is the name, never the ordinal. An outcome outside the five has no
// contract name — marshal fails rather than invent one.
func (o Outcome) MarshalJSON() ([]byte, error) {
	if !o.IsValid() {
		return nil, ValidateOutcome(o)
	}
	return json.Marshal(o.String())
}

// UnmarshalJSON parses the canonical enum name. The numeric ordinal is
// accepted as well so payloads encoded before the enum reached the wire still
// load — a decode must never be stricter than the encoder it follows.
func (o *Outcome) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		parsed, err := ParseOutcome(name)
		if err != nil {
			return err
		}
		*o = parsed
		return nil
	}
	var ordinal int
	if err := json.Unmarshal(data, &ordinal); err != nil {
		return &InvalidOutcomeError{}
	}
	candidate := Outcome(ordinal)
	if !candidate.IsValid() {
		return ValidateOutcome(candidate)
	}
	*o = candidate
	return nil
}

// ParseOutcome converts a string to an Outcome.
func ParseOutcome(s string) (Outcome, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "ALLOW":
		return ALLOW, nil
	case "DENY":
		return DENY, nil
	case "REQUIRE_APPROVAL":
		return REQUIRE_APPROVAL, nil
	case "ALLOW_WITH_CONSTRAINTS":
		return ALLOW_WITH_CONSTRAINTS, nil
	case "ESCALATE":
		return ESCALATE, nil
	default:
		return 0, fmt.Errorf("invalid outcome: %q (must be one of: ALLOW, DENY, REQUIRE_APPROVAL, ALLOW_WITH_CONSTRAINTS, ESCALATE)", s)
	}
}

// IsAllowing returns true if the outcome permits the action to proceed.
// ALLOW and ALLOW_WITH_CONSTRAINTS are allowing outcomes.
// DENY, REQUIRE_APPROVAL, and ESCALATE are non-allowing outcomes.
func (o Outcome) IsAllowing() bool {
	return o == ALLOW || o == ALLOW_WITH_CONSTRAINTS
}

// RequiresDecision returns true if the outcome requires further action
// before the request can proceed.
func (o Outcome) RequiresDecision() bool {
	return o == REQUIRE_APPROVAL || o == ESCALATE
}
