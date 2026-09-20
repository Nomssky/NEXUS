package governance

import "time"

// FailSafe evaluates a governance request with fail-safe behavior.
// When the governance engine is unavailable (nil) or returns an error,
// the system defaults to DENY. This is the fail-safe invariant:
// "when governance is unavailable, deny."
//
// This function is the recommended entry point for governance evaluation
// because it guarantees a valid Decision is always returned, even when
// the engine fails.
func FailSafe(engine *Engine, req Request) Decision {
	if engine == nil {
		return Decision{
			Outcome:    DENY,
			Reason:     "governance engine unavailable (fail-safe: deny)",
			ScopeLevel: ScopeLevelGlobal,
			Timestamp:  time.Now(),
		}
	}

	return engine.Evaluate(req)
}

// FailSafeWithApproval evaluates a governance request and, if the outcome
// is REQUIRE_APPROVAL, submits it to the approval engine. Returns the
// final decision and any approval request created.
func FailSafeWithApproval(engine *Engine, approvalEngine *ApprovalEngine, req Request) (Decision, *ApprovalRequest, error) {
	decision := FailSafe(engine, req)

	if decision.Outcome == REQUIRE_APPROVAL && decision.ApprovalRequired != nil && approvalEngine != nil {
		ar, err := approvalEngine.RequestApproval(decision, req, *decision.ApprovalRequired)
		if err != nil {
			// If approval request fails, fail-safe to DENY
			return Decision{
				Outcome:    DENY,
				Reason:     "approval request failed (fail-safe: deny)",
				ScopeLevel: decision.ScopeLevel,
				Timestamp:  time.Now(),
			}, nil, err
		}
		return decision, ar, nil
	}

	return decision, nil, nil
}

// ValidateOutcome checks that an outcome is one of the 5 canonical outcomes.
// Returns an error if the outcome is invalid. This is a structural invariant check.
func ValidateOutcome(o Outcome) error {
	if !o.IsValid() {
		return &InvalidOutcomeError{Outcome: o}
	}
	return nil
}

// InvalidOutcomeError is returned when a non-canonical outcome is encountered.
type InvalidOutcomeError struct {
	Outcome Outcome
}

func (e *InvalidOutcomeError) Error() string {
	return "invalid governance outcome: must be one of ALLOW, DENY, REQUIRE_APPROVAL, ALLOW_WITH_CONSTRAINTS, ESCALATE"
}
