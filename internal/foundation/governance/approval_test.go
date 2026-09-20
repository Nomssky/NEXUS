package governance

import (
	"testing"
	"time"
)

// TEST-M2-023: Request approval creates pending request
func TestApprovalRequestApproval(t *testing.T) {
	now := time.Now()
	ae := NewApprovalEngineWithClock(fixedClock(now))

	decision := Decision{
		Outcome:   REQUIRE_APPROVAL,
		Reason:    "policy requires approval",
		Timestamp: now,
	}

	req := Request{
		Actor:        "agent-1",
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
		Timestamp:    now,
	}

	config := ApprovalConfig{
		ApproverType:           "human",
		TimeoutSeconds:         3600,
		AutoDenyOnTimeout:      true,
		SelfApprovalProhibited: true,
	}

	ar, err := ae.RequestApproval(decision, req, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ar.Status != ApprovalStatePending {
		t.Errorf("expected pending status, got %v", ar.Status)
	}
	if ar.Requester != "agent-1" {
		t.Errorf("expected requester agent-1, got %v", ar.Requester)
	}
}

// TEST-M2-024: Approve request
func TestApprovalApprove(t *testing.T) {
	now := time.Now()
	ae := NewApprovalEngineWithClock(fixedClock(now))

	decision := Decision{
		Outcome:   REQUIRE_APPROVAL,
		Reason:    "policy requires approval",
		Timestamp: now,
	}

	req := Request{
		Actor:        "agent-1",
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
		Timestamp:    now,
	}

	config := ApprovalConfig{
		ApproverType:           "human",
		TimeoutSeconds:         3600,
		AutoDenyOnTimeout:      true,
		SelfApprovalProhibited: true,
	}

	ar, err := ae.RequestApproval(decision, req, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = ae.Approve(ar.DecisionID, "human-admin", "approved because safe")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be removed from pending
	_, ok := ae.GetApproval(ar.DecisionID)
	if ok {
		t.Error("approved request should be removed from pending")
	}
}

// TEST-M2-025: Self-approval prohibited
func TestApprovalSelfApprovalProhibited(t *testing.T) {
	now := time.Now()
	ae := NewApprovalEngineWithClock(fixedClock(now))

	decision := Decision{
		Outcome:   REQUIRE_APPROVAL,
		Reason:    "policy requires approval",
		Timestamp: now,
	}

	req := Request{
		Actor:        "agent-1",
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
		Timestamp:    now,
	}

	config := ApprovalConfig{
		ApproverType:           "human",
		TimeoutSeconds:         3600,
		AutoDenyOnTimeout:      true,
		SelfApprovalProhibited: true,
	}

	ar, err := ae.RequestApproval(decision, req, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Try self-approval
	err = ae.Approve(ar.DecisionID, "agent-1", "self-approved")
	if err == nil {
		t.Error("expected error for self-approval, got nil")
	}
}

// TEST-M2-026: Deny approval
func TestApprovalDeny(t *testing.T) {
	now := time.Now()
	ae := NewApprovalEngineWithClock(fixedClock(now))

	decision := Decision{
		Outcome:   REQUIRE_APPROVAL,
		Reason:    "policy requires approval",
		Timestamp: now,
	}

	req := Request{
		Actor:        "agent-1",
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
		Timestamp:    now,
	}

	config := ApprovalConfig{
		ApproverType:           "human",
		TimeoutSeconds:         3600,
		AutoDenyOnTimeout:      true,
		SelfApprovalProhibited: true,
	}

	ar, err := ae.RequestApproval(decision, req, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = ae.Deny(ar.DecisionID, "human-admin", "too risky")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be removed from pending
	// Should be removed from pending
	_, ok := ae.GetApproval(ar.DecisionID)
	if ok {
		t.Error("denied request should be removed from pending")
	}
}

// TEST-M2-027: Auto-deny on timeout
func TestApprovalAutoDenyOnTimeout(t *testing.T) {
	start := time.Now()
	ae := NewApprovalEngineWithClock(fixedClock(start))

	decision := Decision{
		Outcome:   REQUIRE_APPROVAL,
		Reason:    "policy requires approval",
		Timestamp: start,
	}

	req := Request{
		Actor:        "agent-1",
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
		Timestamp:    start,
	}

	config := ApprovalConfig{
		ApproverType:           "human",
		TimeoutSeconds:         60,
		AutoDenyOnTimeout:      true,
		SelfApprovalProhibited: true,
	}

	_, err := ae.RequestApproval(decision, req, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Simulate time passing
	ae.now = fixedClock(start.Add(2 * time.Minute))

	timedOut := ae.CheckTimeouts()
	if len(timedOut) != 1 {
		t.Fatalf("expected 1 timed-out request, got %d", len(timedOut))
	}

	if timedOut[0].Status != ApprovalStateDenied {
		t.Errorf("expected auto-denied status, got %v", timedOut[0].Status)
	}
}

// TEST-M2-028: Pending approvals list
func TestApprovalPendingList(t *testing.T) {
	start := time.Now()
	counter := 0
	ae := NewApprovalEngineWithClock(func() time.Time {
		counter++
		return start.Add(time.Duration(counter) * time.Nanosecond)
	})

	decision := Decision{
		Outcome:   REQUIRE_APPROVAL,
		Reason:    "policy requires approval",
		Timestamp: start,
	}

	for i := 0; i < 3; i++ {
		req := Request{
			Actor:        "agent-1",
			Action:       "execute_tool",
			Resource:     "tool-1",
			ResourceType: "tool",
			Timestamp:    start,
		}

		config := ApprovalConfig{
			ApproverType:           "human",
			TimeoutSeconds:         3600,
			AutoDenyOnTimeout:      true,
			SelfApprovalProhibited: true,
		}

		_, err := ae.RequestApproval(decision, req, config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	pending := ae.PendingApprovals()
	if len(pending) != 3 {
		t.Errorf("expected 3 pending, got %d", len(pending))
	}
}

// TEST-M2-029: Approve non-existent request
func TestApprovalNonExistent(t *testing.T) {
	ae := NewApprovalEngine()

	err := ae.Approve("non-existent", "admin", "reason")
	if err == nil {
		t.Error("expected error for non-existent approval, got nil")
	}
}

// TEST-M2-020: FailSafeWithApproval integration
func TestFailSafeWithApproval(t *testing.T) {
	now := time.Now()
	engine := NewEngineWithClock([]*Policy{
		{
			PolicyID:      "pol-approval",
			PolicyVersion: "1.0",
			Status:        PolicyStatusActive,
			Subject:       Subject{SubjectType: "all"},
			Action:        Action{ActionType: "execute_tool"},
			Resource:      Resource{ResourceType: "tool"},
			Effect:        REQUIRE_APPROVAL,
			Precedence:    10,
			ApprovalConfig: &ApprovalConfig{
				ApproverType:           "human",
				TimeoutSeconds:         3600,
				AutoDenyOnTimeout:      true,
				SelfApprovalProhibited: true,
			},
			EffectiveFrom: now.Add(-time.Hour),
			CreatedAt:     now.Add(-time.Hour),
			CreatedBy:     "admin",
		},
	}, fixedClock(now))

	ae := NewApprovalEngineWithClock(fixedClock(now))

	req := Request{
		Actor:        "agent-1",
		Action:       "execute_tool",
		Resource:     "tool-1",
		ResourceType: "tool",
		Timestamp:    now,
	}

	decision, ar, err := FailSafeWithApproval(engine, ae, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.Outcome != REQUIRE_APPROVAL {
		t.Errorf("expected REQUIRE_APPROVAL, got %v", decision.Outcome)
	}

	if ar == nil {
		t.Fatal("expected approval request to be created")
	}

	if ar.Status != ApprovalStatePending {
		t.Errorf("expected pending status, got %v", ar.Status)
	}
}
