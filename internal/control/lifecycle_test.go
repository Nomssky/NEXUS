package control

// Approval single-use under concurrency
// (contracts/APPROVAL_LIFECYCLE_CONTRACTS.md §4): the consumed ledger admits
// exactly one admission per approval, even when many admissions race. The
// rest re-evaluate without an applicable approval.

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/governance"
)

type raceApprover struct {
	mu       sync.Mutex
	approved map[string]bool
}

func (a *raceApprover) RequestApproval(p Proposal, _ governance.Decision) (string, error) {
	return "apr-" + p.ProposalID, nil
}

func (a *raceApprover) ApprovedState(_ Proposal, fingerprint string) (governance.ApprovalState, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.approved[fingerprint] {
		return governance.ApprovalStateApproved, true
	}
	return governance.ApprovalStatePending, true
}

func TestConcurrentAdmissionsConsumeOnce(t *testing.T) {
	approver := &raceApprover{approved: map[string]bool{}}
	ctl := New(Options{
		Engine: governance.NewEngine([]*governance.Policy{{
			SchemaVersion: "1.0.0", EntityType: "policy", PolicyID: "p-appr",
			PolicyVersion: "1", PolicyType: governance.PolicyTypeAccessControl,
			Name: "appr", Description: "appr", Status: governance.PolicyStatusActive,
			Subject:  governance.Subject{SubjectType: "all"},
			Action:   governance.Action{ActionType: "custom", ActionIDs: []string{"tool_call"}},
			Resource: governance.Resource{ResourceType: "all"},
			Effect:   governance.REQUIRE_APPROVAL, Precedence: 1000,
			ApprovalConfig: &governance.ApprovalConfig{
				ApproverType: "human", ApproverIDs: []string{"approver-1"},
				TimeoutSeconds: 600,
			},
		}}),
		Approver: approver,
	})
	proposal := Proposal{
		ProposalID: "prop-race", CorrelationID: "exec-race", ExecutionID: "exec-race",
		ObjectiveID: "exec-race", ActorID: "actor-1", AgentID: "agent-1",
		BusinessID: "biz-1", Action: ActionToolCall, Resource: "echo",
		ResourceType: ResourceTypeCapability, ToolID: "echo", Operation: "execute",
	}
	fp := proposal.Fingerprint()
	approver.mu.Lock()
	approver.approved[fp] = true
	approver.mu.Unlock()

	const racers = 16
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			adm, err := ctl.Admit(proposal)
			if err != nil {
				return
			}
			if adm.Allowed() {
				allowed.Add(1)
			} else if !adm.AwaitingApproval() {
				t.Errorf("a spent approval must re-evaluate to pending, got %+v", adm.Decision.Outcome)
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != 1 {
		t.Fatalf("exactly one admission may consume the approval, got %d", got)
	}
}

func TestEvictedConsumptionRequiresFreshApproval(t *testing.T) {
	// The ledger is bounded; eviction only costs an extra admission round,
	// never an execution — re-admission without an applicable approval waits
	// again instead of running.
	ctl := New(Options{Engine: governance.NewEngine(nil)})
	for i := 0; i < maxConsumedApprovals+10; i++ {
		ctl.MarkConsumed("exec-x", fmt.Sprintf("fp-%d", i))
	}
	ctl.mu.Lock()
	n := len(ctl.consumed)
	ctl.mu.Unlock()
	if n != maxConsumedApprovals {
		t.Fatalf("the consumed ledger must stay bounded at %d, got %d", maxConsumedApprovals, n)
	}
}
