package core

import (
	"context"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/governance"
)

// TEST-N1-01 (N1, P1): a division-scoped DENY policy must apply at the chain
// governance gate. Before the fix the request's DivisionID was never wired
// into governance.Request, so matchesScope rejected the pinned division
// policy, only the global default-allow matched, and the request was allowed
// — a silent authorization bypass of every division-scoped policy.
func TestGovernanceDivisionScopeAppliedAtChainGate(t *testing.T) {
	e, _ := NewEngine(nil)
	registerSimulatedProvider(t, e)
	e.Governance().SetPolicies(append(e.Governance().Policies(), &governance.Policy{
		PolicyID:   "deny-div-9",
		Name:       "Deny division div-9",
		Status:     governance.PolicyStatusActive,
		Effect:     governance.DENY,
		BusinessID: "biz-1",
		DivisionID: "div-9",
		Subject:    governance.Subject{SubjectType: "all"},
		Action:     governance.Action{ActionType: "custom"},
		Resource:   governance.Resource{ResourceType: "all"},
	}))

	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	rc := NewRequestContext("corr-n1-div", "biz-1", "user-1")
	rc.DivisionID = "div-9"
	req := &Request{
		ID:      "req-n1-div",
		Context: rc,
		Intent:  "action inside denied division",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}

	result := waitForResult(t, e, "req-n1-div")
	if result.Status != "failed" || result.Error == nil {
		t.Fatalf("expected division DENY to fail the request, got status=%s err=%v", result.Status, result.Error)
	}
	if result.Error.Code != "POLICY_DENIED" {
		t.Errorf("expected POLICY_DENIED, got %s", result.Error.Code)
	}
}

// TEST-N1-01b: the sibling case — the same division policy must NOT deny a
// request outside that division (guards against over-wiring).
func TestGovernanceDivisionScopeDoesNotLeakAcrossDivisions(t *testing.T) {
	e, _ := NewEngine(nil)
	registerSimulatedProvider(t, e)
	e.Governance().SetPolicies(append(e.Governance().Policies(), &governance.Policy{
		PolicyID:   "deny-div-9-b",
		Name:       "Deny division div-9",
		Status:     governance.PolicyStatusActive,
		Effect:     governance.DENY,
		BusinessID: "biz-1",
		DivisionID: "div-9",
		Subject:    governance.Subject{SubjectType: "all"},
		Action:     governance.Action{ActionType: "custom"},
		Resource:   governance.Resource{ResourceType: "all"},
	}))

	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	rc := NewRequestContext("corr-n1-other-div", "biz-1", "user-1")
	rc.DivisionID = "div-other"
	req := &Request{
		ID:      "req-n1-other-div",
		Context: rc,
		Intent:  "action outside denied division",
	}
	if err := e.SubmitRequest(req); err != nil {
		t.Fatalf("submit: %v", err)
	}

	result := waitForResult(t, e, "req-n1-other-div")
	if result.Status != "completed" {
		t.Fatalf("expected completion outside the denied division, got status=%s err=%v", result.Status, result.Error)
	}
}
