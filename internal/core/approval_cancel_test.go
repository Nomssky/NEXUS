package core

// Approval lifecycle: cancellation and re-admission
// (contracts/APPROVAL_LIFECYCLE_CONTRACTS.md §7, §8): cancelling a waiting
// execution WINS — the approvals it opened are invalidated, the terminal
// result becomes cancelled, and nothing revives it — and an approved resume
// always re-evaluates CURRENT governance, which may by then deny or escalate.

import (
	"errors"
	"testing"

	"github.com/Nomssky/NEXUS/internal/control"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
)

// submitAndWaitingApproval runs one gated request to its waiting result and
// returns the approval id the envelope must carry.
func submitAndWaitingApproval(t *testing.T, e *Engine, h *actionGateHandler, id string) string {
	t.Helper()
	submitActionRequest(t, e, h, id, "user-1")
	res := awaitResult(t, e, id)
	if res.Status != "failed" || res.Error == nil || res.Error.Code != "APPROVAL_REQUIRED" {
		t.Fatalf("expected a waiting APPROVAL_REQUIRED result, got %+v", res)
	}
	return approvalIDFrom(res)
}

// The owner cancels a waiting execution: the wait ends as cancelled, the
// approval it opened is invalidated, and a late decision in either direction
// finds nothing — cancellation beats approval (APPROVAL_LIFECYCLE §8).
func TestCancelWaitingExecutionWinsOverItsApproval(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e := newActionControlEngine(t, h,
		actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil))
	approvalID := submitAndWaitingApproval(t, e, h, "exec-cancel-wait")
	if approvalID == "" {
		t.Fatal("expected an approval id")
	}

	// A foreign business never gets past scope: the cancellation is refused
	// and the record stays decidable (403/404 semantics intact).
	if err := e.CancelRequest("exec-cancel-wait", "biz-other", "user-1"); !errors.Is(err, ErrScopeMismatch) {
		t.Fatalf("expected scope mismatch for a foreign cancel, got %v", err)
	}
	if got := e.ListApprovals("biz-1"); len(got) != 1 {
		t.Fatalf("a refused cancel must leave the record pending, got %d", len(got))
	}

	// Cancel wins: the in-scope cancellation succeeds on a waiting execution.
	if err := e.CancelRequest("exec-cancel-wait", "biz-1", "user-1"); err != nil {
		t.Fatalf("cancel of a waiting execution: %v", err)
	}
	stored, ok := e.GetResult("exec-cancel-wait")
	if !ok || stored.Status != "cancelled" {
		t.Fatalf("the stored result must become cancelled, got ok=%v %+v", ok, stored)
	}
	if stored.Error == nil || stored.Error.Code != "CANCELLED" {
		t.Fatalf("expected a CANCELLED error, got %+v", stored.Error)
	}

	// The approval it opened is invalidated: nothing is actionable, and a
	// late decision in either direction is 404-class, never a resume.
	if got := e.ListApprovals("biz-1"); len(got) != 0 {
		t.Fatalf("cancellation must invalidate the pending approval, got %d", len(got))
	}
	if err := e.ApproveRequest(approvalID, "biz-1", "approver-1", "late"); !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("a late approve must find nothing, got %v", err)
	}
	if err := e.DenyRequest(approvalID, "biz-1", "approver-1", "late"); !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("a late deny must find nothing, got %v", err)
	}

	// Nothing revives: re-admission for the cancelled request is refused
	// (unbound — its request is finished — or cancelled), so no fresh
	// lifecycle opens and no effect can run.
	adm, err := e.Controller().Admit(control.Proposal{
		ProposalID: "prop-exec-cancel-wait", CorrelationID: "exec-cancel-wait",
		ExecutionID: "exec-cancel-wait", ObjectiveID: "exec-cancel-wait",
		ActorID: "user-1", AgentID: "agent-1", BusinessID: "biz-1",
		Action: control.ActionToolCall, Resource: "http.request",
		ResourceType: control.ResourceTypeCapability, ToolID: "http.request",
		Operation: "execute",
	})
	if err == nil && adm.Allowed() {
		t.Fatal("re-admission after cancellation must never authorize")
	}
	if got := e.ListApprovals("biz-1"); len(got) != 0 {
		t.Fatalf("re-admission after cancellation must open no approval, got %d", len(got))
	}
	if h.effectCount() != 0 {
		t.Fatalf("cancellation must execute nothing, effects=%d", h.effectCount())
	}

	// A repeat cancel is idempotent: the same terminal state, never a 409.
	if err := e.CancelRequest("exec-cancel-wait", "biz-1", "user-1"); err != nil {
		t.Fatalf("repeat cancel must be idempotent, got %v", err)
	}
}

// Approval never outranks current governance (§7): the policy becomes an
// unconditional DENY while the execution waits; the approve decision is
// accepted (it was valid), but the resume re-evaluates and denies — the
// effect never runs.
func TestApprovedResumeReAdmitsAgainstADeniedPolicy(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e := newActionControlEngine(t, h,
		actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil))
	approvalID := submitAndWaitingApproval(t, e, h, "exec-readmit-deny")

	// While it waits, the same action becomes unconditionally denied.
	e.Governance().SetPolicies([]*governance.Policy{
		e.Governance().Policies()[0],
		actionPolicy("p-nowdeny", governance.DENY, "http.request", nil),
	})
	if err := e.ApproveRequest(approvalID, "biz-1", "approver-1", "approved, but policy moved"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	res := awaitResult(t, e, "exec-readmit-deny")
	if res.Status != "failed" || res.Error == nil || res.Error.Code != "POLICY_DENIED" {
		t.Fatalf("re-admission must deny under the current policy, got %+v", res.Error)
	}
	if h.effectCount() != 0 {
		t.Fatalf("the denied resume must execute nothing, effects=%d", h.effectCount())
	}
}

// The same for escalation: policy moved to ESCALATE while waiting, so the
// approved resume escalates instead of executing — approval never converts
// an escalation into an effect.
func TestApprovedResumeReAdmitsAgainstAnEscalatingPolicy(t *testing.T) {
	h := &actionGateHandler{tool: "http.request"}
	e := newActionControlEngine(t, h,
		actionPolicy("p-gate", governance.REQUIRE_APPROVAL, "http.request", nil))
	approvalID := submitAndWaitingApproval(t, e, h, "exec-readmit-escalate")

	e.Governance().SetPolicies([]*governance.Policy{
		e.Governance().Policies()[0],
		actionPolicy("p-nowescalate", governance.ESCALATE, "http.request", nil),
	})
	if err := e.ApproveRequest(approvalID, "biz-1", "approver-1", "approved, but policy moved"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	res := awaitResult(t, e, "exec-readmit-escalate")
	if res.Status != "failed" || res.Error == nil || res.Error.Code != "ESCALATION_REQUIRED" {
		t.Fatalf("re-admission must escalate under the current policy, got %+v", res.Error)
	}
	if h.effectCount() != 0 {
		t.Fatalf("the escalated resume must execute nothing, effects=%d", h.effectCount())
	}
}
