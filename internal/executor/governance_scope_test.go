package executor

import (
	"context"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/foundation/agent"
	"github.com/Nomssky/NEXUS/internal/foundation/event"
	"github.com/Nomssky/NEXUS/internal/foundation/governance"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
)

// TEST-N1-02 (N1): division id must reach the executor's governance gate —
// a division-pinned DENY policy otherwise never matches (unset request field
// can never equal a pinned policy id) and the work is allowed through.
func TestGovernanceGateWiresDivisionScope(t *testing.T) {
	now := time.Now()
	govEngine := governance.NewEngine([]*governance.Policy{
		{
			PolicyID: "n1-default-allow",
			Name:     "Default Allow",
			Status:   governance.PolicyStatusActive,
			Effect:   governance.ALLOW,
			Subject:  governance.Subject{SubjectType: "all"},
			Action:   governance.Action{ActionType: "custom"},
			Resource: governance.Resource{ResourceType: "all"},
		},
		{
			PolicyID:   "n1-deny-div-9",
			Name:       "Deny division div-9",
			Status:     governance.PolicyStatusActive,
			Effect:     governance.DENY,
			BusinessID: "biz-1",
			DivisionID: "div-9",
			Subject:    governance.Subject{SubjectType: "all"},
			Action:     governance.Action{ActionType: "custom"},
			Resource:   governance.Resource{ResourceType: "all"},
		},
	})

	e := New(
		agent.NewAgentRuntime(),
		tool.NewToolRegistry(),
		govEngine,
		event.NewMemBus(),
		nil,
		DefaultConfig(),
		WithClock(func() time.Time { return now }),
	)

	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := testRequest("task-n1-div")
	req.BusinessID = "biz-1"
	req.DivisionID = "div-9"
	if err := e.Submit(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	outcome := waitForOutcome(t, e, "task-n1-div")
	if outcome.Status != "denied" {
		t.Errorf("expected denied by division policy, got %s", outcome.Status)
	}
}

// TEST-N1-03 (N1): workflow id must reach the executor's governance gate so a
// workflow-pinned DENY policy applies (it never matched before because
// WorkflowID was never wired).
func TestGovernanceGateWiresWorkflowScope(t *testing.T) {
	now := time.Now()
	govEngine := governance.NewEngine([]*governance.Policy{
		{
			PolicyID: "n1-default-allow-wf",
			Name:     "Default Allow",
			Status:   governance.PolicyStatusActive,
			Effect:   governance.ALLOW,
			Subject:  governance.Subject{SubjectType: "all"},
			Action:   governance.Action{ActionType: "custom"},
			Resource: governance.Resource{ResourceType: "all"},
		},
		{
			PolicyID:   "n1-deny-wf-7",
			Name:       "Deny workflow wf-7",
			Status:     governance.PolicyStatusActive,
			Effect:     governance.DENY,
			BusinessID: "biz-1",
			WorkflowID: "wf-7",
			Subject:    governance.Subject{SubjectType: "all"},
			Action:     governance.Action{ActionType: "custom"},
			Resource:   governance.Resource{ResourceType: "all"},
		},
	})

	e := New(
		agent.NewAgentRuntime(),
		tool.NewToolRegistry(),
		govEngine,
		event.NewMemBus(),
		nil,
		DefaultConfig(),
		WithClock(func() time.Time { return now }),
	)

	ctx := context.Background()
	e.Start(ctx)
	defer e.Stop(ctx)

	req := testRequest("task-n1-wf")
	req.BusinessID = "biz-1"
	req.WorkflowID = "wf-7"
	if err := e.Submit(req); err != nil {
		t.Fatalf("submit: %v", err)
	}
	outcome := waitForOutcome(t, e, "task-n1-wf")
	if outcome.Status != "denied" {
		t.Errorf("expected denied by workflow policy, got %s", outcome.Status)
	}
}
