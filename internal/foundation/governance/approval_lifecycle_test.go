package governance

// Approval decision atomicity and revocation
// (contracts/APPROVAL_LIFECYCLE_CONTRACTS.md §4-§5): concurrent decisions
// serialize (exactly one wins), revocation drops without deciding, and a
// revoked record authorizes nothing.

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func decidedEngine(t *testing.T) (*ApprovalEngine, Decision, Request) {
	t.Helper()
	ae := NewApprovalEngine()
	decision := Decision{Outcome: REQUIRE_APPROVAL, Reason: "needs human"}
	req := Request{Actor: "requester-1", Action: "tool_call", Resource: "echo", BusinessID: "biz-1"}
	return ae, decision, req
}

func TestConcurrentApprovalsDecideOnce(t *testing.T) {
	ae, decision, req := decidedEngine(t)
	ar, err := ae.RequestApproval(decision, req, ApprovalConfig{
		ApproverType: "human", ApproverIDs: []string{"approver-1"}, TimeoutSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	const racers = 16
	var approved atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ae.Approve(ar.DecisionID, "approver-1", "yes"); err == nil {
				approved.Add(1)
			} else if !errors.Is(err, ErrApprovalNotPending) && !errors.Is(err, ErrApprovalNotFound) {
				t.Errorf("a lost race must report not-pending, got %v", err)
			}
		}()
	}
	wg.Wait()
	if got := approved.Load(); got != 1 {
		t.Fatalf("exactly one decision may win, got %d", got)
	}
	// The winner's decision is immutable: a deny afterwards is refused.
	if err := ae.Deny(ar.DecisionID, "approver-1", "late"); !errors.Is(err, ErrApprovalNotFound) && !errors.Is(err, ErrApprovalNotPending) {
		t.Fatalf("a decided record must refuse re-decision, got %v", err)
	}
}

func TestRevokeDropsWithoutDeciding(t *testing.T) {
	ae, decision, req := decidedEngine(t)
	ar, err := ae.RequestApproval(decision, req, ApprovalConfig{ApproverType: "human"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ae.Revoke(ar.DecisionID); err != nil {
		t.Fatalf("revoking a pending record: %v", err)
	}
	// Revocation is not a decision: neither direction may resolve it after.
	if err := ae.Approve(ar.DecisionID, "approver-1", "late"); !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("a revoked record must not approve, got %v", err)
	}
	if err := ae.Deny(ar.DecisionID, "approver-1", "late"); !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("a revoked record must not deny, got %v", err)
	}
	if _, ok := ae.GetApproval(ar.DecisionID); ok {
		t.Fatal("a revoked record must be gone")
	}
	// Revoking twice, or revoking unknown ids, is a mismatch, never a change.
	if err := ae.Revoke(ar.DecisionID); !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("double revoke must mismatch, got %v", err)
	}
	if err := ae.Revoke("apr-nope"); !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("unknown revoke must mismatch, got %v", err)
	}
}
