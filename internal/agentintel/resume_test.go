package agentintel

// Resume continuation (contracts/APPROVAL_LIFECYCLE_CONTRACTS.md §6, §10):
// the resumed run continues the waiting execution — original deadline,
// continued budgets, completed effects pruned — and concurrent resumes
// consume the snapshot once, so the approved effect executes exactly once.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/control"
)

// obsAwareDecider proposes the governed tool call until an observation shows
// it actually RAN (status ok), then completes. It is stateless, so it is safe
// under concurrent runs, and it mirrors the progress rule of the deterministic
// provider: a governance refusal is not an execution.
type obsAwareDecider struct{ plan string }

func (d *obsAwareDecider) Plan(_ context.Context, _ PlanPrompt) (string, error) {
	return d.plan, nil
}

func (d *obsAwareDecider) Decide(_ context.Context, p DecisionPrompt) (string, error) {
	for _, o := range p.Observations {
		if o.ActionType == ActionToolCall && o.Status == "ok" {
			return governedCompleteAnswer, nil
		}
	}
	return governedToolCallAnswer, nil
}

func TestConcurrentResumesExecuteTheApprovedEffectOnce(t *testing.T) {
	g := newGovernedFixture(t, seededAllowPolicy(), approvalPolicy(control.ActionToolCall, ""))
	g.rt.Decider = &obsAwareDecider{plan: goodPlan}

	req := workRequest(t, Objective{Description: "echo something"}, "")
	first, _ := g.rt.Run(context.Background(), req, nil)
	if first.Status != "pending_approval" {
		t.Fatalf("the first run must pause pending approval: %s (%s)", first.Status, first.Error)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("the paused run must execute nothing: %d", g.adapter.count())
	}
	g.approver.approveAll()

	// Two re-submissions of the SAME request race to resume it. The snapshot
	// is consumed once; the approval admits at most one admission, so the
	// effect runs exactly once no matter which run wins each race.
	const resumes = 2
	outcomes := make([]*struct {
		status string
		err    string
	}, resumes)
	var wg sync.WaitGroup
	for i := 0; i < resumes; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out, _ := g.rt.Run(context.Background(),
				workRequest(t, Objective{Description: "echo something"}, ""), nil)
			outcomes[i] = &struct {
				status string
				err    string
			}{out.Status, out.Error}
		}(i)
	}
	wg.Wait()
	if g.adapter.count() != 1 {
		t.Fatalf("concurrent resumes must execute the effect once, got %d", g.adapter.count())
	}
	completed := 0
	for _, o := range outcomes {
		if o == nil {
			t.Fatal("every resume must return an outcome")
		}
		switch o.status {
		case "completed":
			completed++
		case "pending_approval":
			// The loser re-evaluates without an applicable approval (the
			// single-use one was spent) and waits again — never executes.
		default:
			t.Errorf("unexpected loser outcome: %s (%s)", o.status, o.err)
		}
	}
	if completed != 1 {
		t.Fatalf("exactly one resume may complete, got %d (%+v)", completed, outcomes)
	}
}

func TestResumeContinuesTheOriginalBudget(t *testing.T) {
	// One tool call budget total: the pre-approval attempt already consumed
	// it, so the resumed re-proposal must fail honestly on the CONTINUED
	// counter instead of executing on a reset one.
	g := newGovernedFixture(t, seededAllowPolicy(), approvalPolicy(control.ActionToolCall, ""))
	g.rt.Decider = &obsAwareDecider{plan: goodPlan}
	obj := Objective{Description: "echo something", Budget: Budget{MaxToolCalls: 1}}

	first, _ := g.rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if first.Status != "pending_approval" {
		t.Fatalf("the first run must pause pending approval: %s (%s)", first.Status, first.Error)
	}
	g.approver.approveAll()

	resumed, _ := g.rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if resumed.Status != "failed" || !strings.Contains(resumed.Error, "budget") {
		t.Fatalf("a resumed budget must continue, not reset: %s (%s)", resumed.Status, resumed.Error)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("an exhausted resume must execute nothing, got %d", g.adapter.count())
	}
}

func TestResumeNeverExtendsTheOriginalDeadline(t *testing.T) {
	// The snapshot carries the ORIGINAL caller deadline. Once it has passed,
	// the resume ends honestly with deadline_exceeded and no effect — waiting
	// on approval never buys extra time.
	g := newGovernedFixture(t, seededAllowPolicy(), approvalPolicy(control.ActionToolCall, ""))
	g.rt.Decider = &obsAwareDecider{plan: goodPlan}

	first, _ := g.rt.Run(context.Background(),
		workRequest(t, Objective{Description: "echo something"}, ""), nil)
	if first.Status != "pending_approval" {
		t.Fatalf("the first run must pause pending approval: %s (%s)", first.Status, first.Error)
	}
	// Age the snapshot's deadline into the past (same-package white box: the
	// registry is process-local scratch, exactly what production writes).
	g.rt.mu.Lock()
	snap := g.rt.resumes["exec-1"]
	g.rt.mu.Unlock()
	if snap == nil {
		t.Fatal("a paused run must leave its resume snapshot")
	}
	snap.deadline = time.Now().Add(-time.Second)
	g.approver.approveAll()

	resumed, _ := g.rt.Run(context.Background(),
		workRequest(t, Objective{Description: "echo something"}, ""), nil)
	// The terminal state is echoed in the outcome's error (every non-
	// completed/cancelled terminal is a failed outcome naming its state).
	if resumed.Status != "failed" || !strings.Contains(resumed.Error, "state=deadline_exceeded") {
		t.Fatalf("an expired original deadline must end the resume: %s (%s)", resumed.Status, resumed.Error)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("an expired resume must execute nothing, got %d", g.adapter.count())
	}
}

// A snapshot is verified before it is applied: the same execution id resumed
// under a different request identity starts fresh instead of inheriting the
// waiting run's state.
func TestMismatchedResumeStartsFreshInsteadOfInheriting(t *testing.T) {
	g := newGovernedFixture(t, seededAllowPolicy(), approvalPolicy(control.ActionToolCall, ""))
	g.rt.Decider = &obsAwareDecider{plan: goodPlan}

	first, _ := g.rt.Run(context.Background(),
		workRequest(t, Objective{Description: "echo something"}, ""), nil)
	if first.Status != "pending_approval" {
		t.Fatalf("the first run must pause pending approval: %s (%s)", first.Status, first.Error)
	}

	// Same execution id, different objective: verification must reject the
	// snapshot rather than restore another run's state.
	req := workRequest(t, Objective{Description: "echo something else"}, "")
	out, _ := g.rt.Run(context.Background(), req, nil)
	if out.Status != "pending_approval" {
		t.Fatalf("a mismatched resume must not inherit the paused state: %s (%s)", out.Status, out.Error)
	}
	// The stale snapshot was consumed on load even when rejected: what the
	// registry now holds is the mismatched run's OWN pause, never the
	// original run's state.
	g.rt.mu.Lock()
	snap := g.rt.resumes["exec-1"]
	g.rt.mu.Unlock()
	if snap == nil {
		t.Fatal("the fresh run's own pause must leave a snapshot")
	}
	if snap.objective != "echo something else" {
		t.Fatalf("the rejected snapshot must be replaced, not applied: %q", snap.objective)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("nothing may execute across a mismatch, got %d", g.adapter.count())
	}
}
