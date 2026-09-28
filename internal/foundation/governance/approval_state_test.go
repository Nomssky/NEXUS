package governance

import (
	"strings"
	"testing"
)

// TEST-GOV-APR-01: Request.ApprovalState = approved satisfies a
// REQUIRE_APPROVAL policy (the P1 approval-resume wiring hook — the field
// existed for exactly this purpose). INV-10 compliance: the conversion
// happens only when the caller passes the explicit approved state, never on
// an event or silence. DENY and default-deny are deliberately NOT
// overridden; a pending state does not satisfy the gate. With ApprovalState
// unset every pre-wiring caller sees identical behavior.
func TestEvaluateApprovalStateSatisfiesRequireApproval(t *testing.T) {
	requireApproval := func() *Policy {
		return &Policy{
			PolicyID:   "req-appr",
			Name:       "Require Approval",
			Status:     PolicyStatusActive,
			Effect:     REQUIRE_APPROVAL,
			Subject:    Subject{SubjectType: "all"},
			Action:     Action{ActionType: "custom"},
			Resource:   Resource{ResourceType: "all"},
			Precedence: 0,
		}
	}
	baseReq := Request{
		Actor:      "user-1",
		Action:     "execute_request",
		Resource:   "core",
		BusinessID: "biz-1",
	}

	e := NewEngine([]*Policy{requireApproval()})

	// No approval on file → the gate holds.
	d := e.Evaluate(baseReq)
	if d.Outcome != REQUIRE_APPROVAL {
		t.Fatalf("expected REQUIRE_APPROVAL without approval state, got %s", d.Outcome)
	}

	// Approved record → satisfied.
	approved := ApprovalStateApproved
	withApproval := baseReq
	withApproval.ApprovalState = &approved
	d = e.Evaluate(withApproval)
	if d.Outcome != ALLOW {
		t.Fatalf("approved ApprovalState must satisfy REQUIRE_APPROVAL, got %s (%s)", d.Outcome, d.Reason)
	}
	if !strings.Contains(d.Reason, "approval granted") {
		t.Errorf("expected decision reason to record the approval grant, got %q", d.Reason)
	}

	// Pending does NOT satisfy (only APPROVED does).
	pending := ApprovalStatePending
	withPending := baseReq
	withPending.ApprovalState = &pending
	d = e.Evaluate(withPending)
	if d.Outcome != REQUIRE_APPROVAL {
		t.Errorf("pending ApprovalState must not satisfy the gate, got %s", d.Outcome)
	}

	// DENY is never overridden by an approval.
	denyE := NewEngine([]*Policy{
		{
			PolicyID:   "deny-all",
			Name:       "Deny All",
			Status:     PolicyStatusActive,
			Effect:     DENY,
			Subject:    Subject{SubjectType: "all"},
			Action:     Action{ActionType: "custom"},
			Resource:   Resource{ResourceType: "all"},
			Precedence: 0,
		},
	})
	d = denyE.Evaluate(withApproval)
	if d.Outcome != DENY {
		t.Errorf("approved ApprovalState must not override DENY, got %s", d.Outcome)
	}

	// Default-deny (no matching policy) is never overridden either.
	emptyE := NewEngine(nil)
	d = emptyE.Evaluate(withApproval)
	if d.Outcome != DENY {
		t.Errorf("approved ApprovalState must not override default-deny, got %s", d.Outcome)
	}
}
