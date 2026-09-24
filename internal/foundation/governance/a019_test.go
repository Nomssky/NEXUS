package governance

import (
	"testing"
	"time"
)

// A-019: Self-approval invariant lives in Approve() using actual requester
// identity; RequestApproval's check is an identity prerequisite only.
// ApprovalEngine is intentionally not runtime-wired (E-004 residual) —
// these tests prove API-level behavior.

// a019Fixture builds a minimal pending approval for self-approval tests.
func a019Fixture(t *testing.T, actor string, prohibitSelf bool) (*ApprovalEngine, *ApprovalRequest) {
	t.Helper()
	now := time.Now()
	ae := NewApprovalEngineWithClock(fixedClock(now))
	decision := Decision{Outcome: REQUIRE_APPROVAL, Reason: "a019", Timestamp: now}
	req := Request{Actor: actor, Action: "execute_tool", Resource: "tool-1", ResourceType: "tool", Timestamp: now}
	config := ApprovalConfig{
		ApproverType:           "human",
		TimeoutSeconds:         3600,
		AutoDenyOnTimeout:      true,
		SelfApprovalProhibited: prohibitSelf,
	}
	ar, err := ae.RequestApproval(decision, req, config)
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	return ae, ar
}

// RED/GREEN: RequestApproval rejects empty actor ONLY when self-approval prohibited.
func TestA019RequestApprovalEmptyActorWhenProhibited(t *testing.T) {
	now := time.Now()
	ae := NewApprovalEngineWithClock(fixedClock(now))
	decision := Decision{Outcome: REQUIRE_APPROVAL, Reason: "a019", Timestamp: now}
	req := Request{Actor: "", Action: "execute_tool", Resource: "tool-1", ResourceType: "tool", Timestamp: now}
	config := ApprovalConfig{SelfApprovalProhibited: true, TimeoutSeconds: 60}

	_, err := ae.RequestApproval(decision, req, config)
	if err == nil {
		t.Fatal("expected error: empty actor + SelfApprovalProhibited requires requester identity")
	}
}

// RequestApproval SUCCEEDS with non-empty actor when prohibited — proves
// RequestApproval is not the self-approval gate (identity prerequisite only).
func TestA019RequestApprovalNonEmptyActorWhenProhibited(t *testing.T) {
	_, ar := a019Fixture(t, "agent-1", true)
	if ar.Requester != "agent-1" {
		t.Errorf("Requester: want agent-1, got %q", ar.Requester)
	}
	if ar.Status != ApprovalStatePending {
		t.Errorf("Status: want pending, got %v", ar.Status)
	}
}

// Actual self-approval invariant: Approve rejects approver == requester when prohibited.
func TestA019ApproveRejectsSelfApprovalWhenProhibited(t *testing.T) {
	ae, ar := a019Fixture(t, "agent-1", true)
	err := ae.Approve(ar.DecisionID, "agent-1", "self-approved")
	if err == nil {
		t.Fatal("expected Approve to reject self-approval when prohibited")
	}
}

// Approve allows a DIFFERENT actor when prohibited (separation of duties).
func TestA019ApproveAllowsDifferentActorWhenProhibited(t *testing.T) {
	ae, ar := a019Fixture(t, "agent-1", true)
	if err := ae.Approve(ar.DecisionID, "human-admin", "ok"); err != nil {
		t.Fatalf("Approve by different actor: %v", err)
	}
}

// When prohibition is DISABLED, self-approval is allowed by config.
func TestA019ApproveAllowsSelfApprovalWhenNotProhibited(t *testing.T) {
	ae, ar := a019Fixture(t, "agent-1", false)
	if err := ae.Approve(ar.DecisionID, "agent-1", "self-allowed"); err != nil {
		t.Fatalf("Approve self when not prohibited: %v", err)
	}
}

// Empty actor is allowed at RequestApproval when prohibition is disabled
// (no identity prerequisite without the self-approval constraint).
func TestA019RequestApprovalEmptyActorWhenNotProhibited(t *testing.T) {
	now := time.Now()
	ae := NewApprovalEngineWithClock(fixedClock(now))
	decision := Decision{Outcome: REQUIRE_APPROVAL, Reason: "a019", Timestamp: now}
	req := Request{Actor: "", Action: "execute_tool", Resource: "tool-1", ResourceType: "tool", Timestamp: now}
	config := ApprovalConfig{SelfApprovalProhibited: false, TimeoutSeconds: 60}

	ar, err := ae.RequestApproval(decision, req, config)
	if err != nil {
		t.Fatalf("RequestApproval empty actor when not prohibited: %v", err)
	}
	if ar.Requester != "" {
		t.Errorf("Requester: want empty, got %q", ar.Requester)
	}
}
