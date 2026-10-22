package agentintel

// Resume continuation (contracts/APPROVAL_LIFECYCLE_CONTRACTS.md §6, §10):
// the resumed run continues the waiting execution — original deadline,
// continued budgets, completed effects pruned — and concurrent resumes
// consume the snapshot once, so the approved effect executes exactly once.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/control"
	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/memory"
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
	// A mismatch is refused without consuming the snapshot, so the ORIGINAL
	// snapshot is still parked here when this run starts fresh. What the
	// registry holds once this run reaches its own pause is that run's OWN
	// state — the fresh run overwrote the slot rather than applying the
	// mismatched one.
	g.rt.mu.Lock()
	snap := g.rt.resumes["exec-1"]
	g.rt.mu.Unlock()
	if snap == nil {
		t.Fatal("the fresh run's own pause must leave a snapshot")
	}
	if snap.identity.description != "echo something else" {
		t.Fatalf("the rejected snapshot must be replaced, not applied: %q", snap.identity.description)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("nothing may execute across a mismatch, got %d", g.adapter.count())
	}
}

// ---- resume binds the whole objective, not just its description -------------
//
// A resume inherits the ORIGINAL plan, ledger, observations, completed-effect
// fingerprints and consumed counters. Matching on the description alone would
// let a re-submission swap in weaker criteria or a wider budget while keeping
// all of that authority. These tests pin the identity to what actually governs
// the execution.

// ---- FIFO eviction ---------------------------------------------------------

// The registry is bounded at maxResumeSnapshots and drops the OLDEST waiting
// execution first. Both halves matter and neither is observable from the
// consumption tests: a registry that evicted the newest entry, or one that
// stopped evicting at all, would pass every other resume test unchanged.
//
// Snapshots are inserted through the real snapshotRun, so this exercises the
// actual map and FIFO bookkeeping rather than a stand-in.
func TestResumeRegistryEvictsOldestAtCapacity(t *testing.T) {
	rt, _ := newIntelFixture(t, nil, nil)
	req := workRequest(t, Objective{Description: "wait for approval"}, "")

	obj, budget, err := parseObjective(EncodeObjective(Objective{Description: "wait for approval"}), rt.Caps)
	if err != nil {
		t.Fatal(err)
	}
	crits, err := parseCriteria(nil)
	if err != nil {
		t.Fatal(err)
	}
	// Every snapshot shares one identity and one scope, so identity and scope
	// verification can never be the reason an entry is missing.
	identity := newObjectiveIdentity(obj, budget, crits)

	steps, err := ParsePlan(goodPlan)
	if err != nil {
		t.Fatal(err)
	}
	execID := func(i int) string { return fmt.Sprintf("exec-%03d", i) }
	insert := func(i int) {
		t.Helper()
		plan := &Plan{PlanID: execID(i), Version: 1, Steps: steps}
		rt.snapshotRun(
			&run{rt: rt, req: req, execID: execID(i),
				businessID: req.BusinessID, divisionID: req.DivisionID},
			identity, plan, newPlanLedger(plan), nil, BudgetUsage{},
			NewWorkingMemory(), time.Now())
	}

	// order is a consistent snapshot of both registry structures under the
	// registry mutex: the FIFO queue and the map size.
	order := func() ([]string, int) {
		rt.mu.Lock()
		defer rt.mu.Unlock()
		return append([]string(nil), rt.resumeOrder...), len(rt.resumes)
	}

	if q, size := order(); size != 0 || len(q) != 0 {
		t.Fatalf("a fresh runtime must hold no snapshots: map=%d order=%d", size, len(q))
	}

	// Fill to exactly the bound, then push one past it.
	for i := 0; i < maxResumeSnapshots; i++ {
		insert(i)
	}
	q, size := order()
	if size != maxResumeSnapshots || len(q) != maxResumeSnapshots {
		t.Fatalf("at capacity: map=%d order=%d, want %d each", size, len(q), maxResumeSnapshots)
	}
	insert(maxResumeSnapshots) // the (maxResumeSnapshots+1)-th

	// (1) The oldest is gone from BOTH structures, and (2) they stay the same
	// size: the queue and the map are one registry, not two that can drift.
	q, size = order()
	inMap, inOrder := snapshotExists(rt, execID(0))
	if inMap || inOrder {
		t.Fatalf("the oldest execution %s must be evicted from both structures (map=%v order=%v)",
			execID(0), inMap, inOrder)
	}
	if size != maxResumeSnapshots || len(q) != maxResumeSnapshots {
		t.Fatalf("past capacity: map=%d order=%d, want %d each", size, len(q), maxResumeSnapshots)
	}

	// FIFO specifically, not "something was dropped": the queue must now start
	// at the SECOND oldest and end at the newest. A LIFO or arbitrary drop
	// would pass every other assertion above.
	if q[0] != execID(1) {
		t.Fatalf("FIFO: the head must be the second-oldest execution, got %q", q[0])
	}
	if last := q[len(q)-1]; last != execID(maxResumeSnapshots) {
		t.Fatalf("FIFO: the tail must be the newest execution, got %q", last)
	}

	// (3) The newest is still registered and consumable.
	newest := execID(maxResumeSnapshots)
	snap, ok := rt.takeResume(newest, identity, req)
	if !ok || snap == nil {
		t.Fatalf("the newest execution must still resume: ok=%v", ok)
	}

	// (4) Consuming an evicted id is a plain miss: no state, and no change to
	// the registry. Baseline is taken AFTER the successful resume above.
	qBefore, sizeBefore := order()
	snap, ok = rt.takeResume(execID(0), identity, req)
	if ok || snap != nil {
		t.Fatalf("an evicted id must not resume: snap=%v ok=%v", snap != nil, ok)
	}
	qAfter, sizeAfter := order()
	if sizeAfter != sizeBefore || len(qAfter) != len(qBefore) {
		t.Fatalf("a missed lookup must not disturb the registry: map %d->%d order %d->%d",
			sizeBefore, sizeAfter, len(qBefore), len(qAfter))
	}
	for i := range qBefore {
		if qAfter[i] != qBefore[i] {
			t.Fatalf("a missed lookup must not reorder the queue: %v -> %v", qBefore, qAfter)
		}
	}

	// (5) Consumption removed exactly one entry and kept map and order in
	// lockstep.
	if inMap, inOrder := snapshotExists(rt, newest); inMap || inOrder {
		t.Fatalf("a consumed execution must be gone from both structures: %s (map=%v order=%v)",
			newest, inMap, inOrder)
	}
	if sizeAfter != maxResumeSnapshots-1 || len(qAfter) != maxResumeSnapshots-1 {
		t.Fatalf("after one resume: map=%d order=%d, want %d each",
			sizeAfter, len(qAfter), maxResumeSnapshots-1)
	}
	if got, want := qAfter[0], execID(1); got != want {
		t.Fatalf("consumption must not reorder the surviving queue: head=%q want %q", got, want)
	}
}

// snapshotExists reports whether an execution id is registered.
func snapshotExists(rt *Runtime, execID string) (bool, bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	_, inMap := rt.resumes[execID]
	inOrder := false
	for _, eid := range rt.resumeOrder {
		if eid == execID {
			inOrder = true
			break
		}
	}
	return inMap, inOrder
}

// resumeIdentity reads the identity currently stored for an execution.
func resumeIdentity(t *testing.T, rt *Runtime, execID string) objectiveIdentity {
	t.Helper()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	snap := rt.resumes[execID]
	if snap == nil {
		t.Fatalf("no snapshot stored for %s", execID)
	}
	return snap.identity
}

// startPausedRun runs one objective to its pending_approval pause and leaves a
// snapshot behind.
func startPausedRun(t *testing.T, g *governedFixture, obj Objective) {
	t.Helper()
	g.rt.Decider = &obsAwareDecider{plan: goodPlan}
	first, _ := g.rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if first.Status != "pending_approval" {
		t.Fatalf("the first run must pause pending approval: %s (%s)", first.Status, first.Error)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("the paused run must execute nothing: %d", g.adapter.count())
	}
}

func TestResumeWithIdenticalCriteriaAndBudgetContinues(t *testing.T) {
	// The normal path: the same objective, re-submitted, continues. Criteria
	// and budget are bound but they match, so nothing is weakened.
	g := newGovernedFixture(t, seededAllowPolicy(), approvalPolicy(control.ActionToolCall, ""))
	obj := Objective{Description: "echo something",
		SuccessCriteria: []string{"source==tool:governed.echo", "status==ok"},
		Budget:          Budget{MaxToolCalls: 3}}
	startPausedRun(t, g, obj)
	g.approver.approveAll()

	out, _ := g.rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if out.Status != "completed" {
		t.Fatalf("an identical re-submission must resume and complete: %s (%s)", out.Status, out.Error)
	}
	if g.adapter.count() != 1 {
		t.Fatalf("the approved effect must run exactly once, got %d", g.adapter.count())
	}
}

func TestResumeWithNormalizedEquivalentObjectiveContinues(t *testing.T) {
	// Same meaning, different spelling. Criteria that differ only in whitespace
	// or order are the same conjunction, and a budget written out to the cap is
	// the same grant as omitting it (callers may only tighten, §9). Neither
	// difference is a change of objective.
	g := newGovernedFixture(t, seededAllowPolicy(), approvalPolicy(control.ActionToolCall, ""))
	startPausedRun(t, g, Objective{
		Description:     "echo something",
		SuccessCriteria: []string{"source==tool:governed.echo", "status==ok"},
		Budget:          Budget{MaxToolCalls: 3},
	})
	g.approver.approveAll()

	out, _ := g.rt.Run(context.Background(), workRequest(t, Objective{
		Description:     "echo something",
		SuccessCriteria: []string{"  status==ok  ", "source == tool:governed.echo"},
		Budget:          Budget{},
	}, ""), nil)
	if out.Status != "completed" {
		t.Fatalf("a semantically equivalent objective must still resume: %s (%s)", out.Status, out.Error)
	}
	if g.adapter.count() != 1 {
		t.Fatalf("the approved effect must run exactly once, got %d", g.adapter.count())
	}
}

// A rejected resume must carry NOTHING from the paused run: not its consumed
// counters, not its duplicate-suppression. This is the test that actually
// discriminates — the paused run spent its single tool-call budget and
// completed a durable write, so a run that inherited either would refuse to
// proceed. A fresh run has neither and completes normally.
func TestRejectedResumeCarriesNoCountersOrEffects(t *testing.T) {
	g := newGovernedFixture(t, seededAllowPolicy(), approvalPolicy(control.ActionToolCall, ""))
	const write = `{"type":"memory_write","key":"k","value":"v","memory_type":"fact"}`
	const echo = `{"type":"tool_call","tool":"governed.echo","operation":"execute","input":{"text":"x"}}`

	g.rt.Decider = &scriptedDecider{plan: goodPlan, answers: []string{write, echo}}
	obj := Objective{Description: "remember then echo",
		SuccessCriteria: []string{"status==ok"}, Budget: Budget{MaxToolCalls: 1}}
	first, _ := g.rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if first.Status != "pending_approval" {
		t.Fatalf("the first run must pause pending approval: %s (%s)", first.Status, first.Error)
	}
	g.approver.approveAll()

	// Only the criteria differ. The budget is identical, so a resume would be
	// indistinguishable here on budget alone — the criteria binding is what
	// must reject it.
	g.rt.Decider = &scriptedDecider{plan: goodPlan, answers: []string{write, echo, `{"type":"complete","result":"done"}`}}
	out, _ := g.rt.Run(context.Background(), workRequest(t, Objective{
		Description:     "remember then echo",
		SuccessCriteria: []string{"source==tool:governed.echo"},
		Budget:          Budget{MaxToolCalls: 1},
	}, ""), nil)

	if out.Status != "completed" {
		t.Fatalf("a rejected resume must start clean and be able to finish: %s (%s)", out.Status, out.Error)
	}
	if g.adapter.count() != 1 {
		t.Fatalf("the fresh run must dispatch its own effect exactly once, got %d", g.adapter.count())
	}
}

func TestResumeWithWeakerCriteriaStartsFresh(t *testing.T) {
	// Dropping a criterion would let a resumed run complete on less evidence
	// than the one that paused. The snapshot must not be applied.
	cases := []struct {
		name  string
		first []string
		again []string
	}{
		{"criteria dropped", []string{"source==tool:governed.echo", "status==ok"}, nil},
		{"criteria relaxed", []string{"status==ok"}, []string{"status==ok", "source==tool:governed.echo"}},
		{"criteria swapped", []string{"status==ok"}, []string{"source==tool:governed.echo"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newGovernedFixture(t, seededAllowPolicy(), approvalPolicy(control.ActionToolCall, ""))
			startPausedRun(t, g, Objective{
				Description: "echo something", SuccessCriteria: tc.first, Budget: Budget{MaxToolCalls: 3},
			})

			out, _ := g.rt.Run(context.Background(), workRequest(t, Objective{
				Description: "echo something", SuccessCriteria: tc.again, Budget: Budget{MaxToolCalls: 3},
			}, ""), nil)
			if out.Status != "pending_approval" {
				t.Fatalf("weaker criteria must not inherit the paused state: %s (%s)", out.Status, out.Error)
			}
			if g.adapter.count() != 0 {
				t.Fatalf("a rejected resume must dispatch nothing, got %d", g.adapter.count())
			}
			// The registry now holds the SECOND run's own pause, carrying the
			// second run's criteria — proof the first snapshot's state was
			// never applied to this run.
			stored := resumeIdentity(t, g.rt, "exec-1")
			if len(stored.criteria) != len(tc.again) {
				t.Fatalf("the stored identity must be the fresh run's, not the paused one: %+v", stored.criteria)
			}
		})
	}
}

func TestResumeWithChangedEffectiveBudgetStartsFresh(t *testing.T) {
	// A wider budget on the re-submission must not ride along with the
	// original run's consumed counters and completed effects.
	g := newGovernedFixture(t, seededAllowPolicy(), approvalPolicy(control.ActionToolCall, ""))
	startPausedRun(t, g, Objective{Description: "echo something", Budget: Budget{MaxToolCalls: 3}})

	out, _ := g.rt.Run(context.Background(), workRequest(t, Objective{
		Description: "echo something", Budget: Budget{MaxToolCalls: 6},
	}, ""), nil)
	if out.Status != "pending_approval" {
		t.Fatalf("a changed budget must not inherit the paused state: %s (%s)", out.Status, out.Error)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("a rejected resume must dispatch nothing, got %d", g.adapter.count())
	}
	if stored := resumeIdentity(t, g.rt, "exec-1"); stored.budget.MaxToolCalls != 6 {
		t.Fatalf("the stored identity must be the fresh run's budget: %+v", stored.budget)
	}
}

func TestResumeWithBudgetDroppedEntirelyStartsFresh(t *testing.T) {
	// Omitting the budget is not a no-op: it grants the full cap, which is a
	// wider grant than the paused run had. A weaker-looking request that is
	// actually a WIDER one must not resume.
	g := newGovernedFixture(t, seededAllowPolicy(), approvalPolicy(control.ActionToolCall, ""))
	startPausedRun(t, g, Objective{Description: "echo something", Budget: Budget{MaxToolCalls: 3}})

	out, _ := g.rt.Run(context.Background(), workRequest(t, Objective{Description: "echo something"}, ""), nil)
	if out.Status != "pending_approval" {
		t.Fatalf("an omitted budget must not inherit a narrowed paused state: %s (%s)", out.Status, out.Error)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("a rejected resume must dispatch nothing, got %d", g.adapter.count())
	}
}

// A mismatching identity is not a resume. It must be refused WITHOUT applying
// the snapshot and WITHOUT consuming it: a caller that re-submitted the wrong
// objective has no standing to destroy a waiting execution's snapshot, and the
// original attempt must still be able to continue it.
func TestRejectedResumeLeavesTheSnapshotIntactAndRecoverable(t *testing.T) {
	g := newGovernedFixture(t, seededAllowPolicy(), approvalPolicy(control.ActionToolCall, ""))
	startPausedRun(t, g, Objective{Description: "echo something", SuccessCriteria: []string{"status==ok"}})

	// Read the stored identity and state before touching them, so the recovery
	// attempt below can be compared against exactly what was there.
	stored := resumeIdentity(t, g.rt, "exec-1")
	req := workRequest(t, Objective{Description: "echo something", SuccessCriteria: []string{"status==ok"}}, "")
	before := snapshotState(t, g.rt, "exec-1")

	weaker := newObjectiveIdentity(Objective{Description: "echo something"}, Budget{},
		mustParseCriteria(t, "status==ok", "source==tool:governed.echo"))
	if snap, ok := g.rt.takeResume("exec-1", weaker, req); ok || snap != nil {
		t.Fatalf("weaker criteria must not resume: snap=%v ok=%v", snap != nil, ok)
	}

	// Still there, and byte-for-byte the same snapshot: a refusal is not a
	// cleanup, and it must not touch counters, effects or observations.
	after := snapshotState(t, g.rt, "exec-1")
	if before != after {
		t.Fatalf("a rejected resume must leave the snapshot untouched: %+v -> %+v", before, after)
	}
	if again := resumeIdentity(t, g.rt, "exec-1"); !stored.matches(again) {
		t.Fatalf("a rejected resume must not rewrite the stored identity: %+v -> %+v", stored, again)
	}

	// Recovery: the original identity still resumes the ORIGINAL snapshot.
	g.rt.Decider = &obsAwareDecider{plan: goodPlan}
	g.approver.approveAll()
	out, _ := g.rt.Run(context.Background(), req, nil)
	if out.Status != "completed" {
		t.Fatalf("the original identity must still resume after a rejection: %s (%s)", out.Status, out.Error)
	}
	if g.adapter.count() != 1 {
		t.Fatalf("the resumed run must dispatch exactly once, got %d", g.adapter.count())
	}
}

// A rejected attempt inherits NOTHING from the waiting execution. This is the
// loop-level proof: the paused run completed a durable write and spent its only
// tool-call budget, so an attempt that inherited either would be refused as a
// duplicate write or as an exhausted budget. A refused attempt inherits
// neither, runs its own work, and pauses on its own approval.
func TestRejectedResumeInheritsNoCountersOrEffects(t *testing.T) {
	g := newGovernedFixture(t, seededAllowPolicy(), approvalPolicy(control.ActionToolCall, ""))
	const write = `{"type":"memory_write","key":"k","value":"v","memory_type":"fact"}`
	const echo = `{"type":"tool_call","tool":"governed.echo","operation":"execute","input":{"text":"x"}}`

	// Paused run: durable write first, then the gated call.
	g.rt.Decider = &scriptedDecider{plan: goodPlan, answers: []string{write, echo}}
	obj := Objective{Description: "remember then echo",
		SuccessCriteria: []string{"status==ok"}, Budget: Budget{MaxToolCalls: 1}}
	first, _ := g.rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if first.Status != "pending_approval" {
		t.Fatalf("the first run must pause pending approval: %s (%s)", first.Status, first.Error)
	}

	// Same budget, different criteria, and the SAME durable write re-proposed.
	// Inheriting the paused run's effect ledger would refuse this write.
	g.rt.Decider = &scriptedDecider{plan: goodPlan, answers: []string{write, echo}}
	out, _ := g.rt.Run(context.Background(), workRequest(t, Objective{
		Description:     "remember then echo",
		SuccessCriteria: []string{"source==tool:governed.echo"},
		Budget:          Budget{MaxToolCalls: 1},
	}, ""), nil)

	if strings.Contains(out.Error, "duplicate") {
		t.Fatalf("a refused resume must not inherit the paused run's effect ledger: %s", out.Error)
	}
	if strings.Contains(out.Error, "budget") {
		t.Fatalf("a refused resume must not inherit the paused run's counters: %s", out.Error)
	}
	// It ran its own write and then paused on its own approval.
	if out.Status != "pending_approval" {
		t.Fatalf("a refused resume must start clean and reach its own pause: %s (%s)", out.Status, out.Error)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("a refused resume must dispatch nothing, got %d", g.adapter.count())
	}
}

// The matching resume restores the paused run's completed-effect fingerprints,
// so a re-proposal is refused as a duplicate rather than executed a second time.
func TestMatchingResumeDoesNotRedispatchCompletedEffects(t *testing.T) {
	g := newGovernedFixture(t, seededAllowPolicy(), approvalPolicy(control.ActionToolCall, ""))
	const write = `{"type":"memory_write","key":"k","value":"v","memory_type":"fact"}`
	const echo = `{"type":"tool_call","tool":"governed.echo","operation":"execute","input":{"text":"x"}}`

	g.rt.Decider = &scriptedDecider{plan: goodPlan, answers: []string{write, echo}}
	obj := Objective{Description: "remember then echo", SuccessCriteria: []string{"status==ok"}}
	first, _ := g.rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if first.Status != "pending_approval" {
		t.Fatalf("the first run must pause pending approval: %s (%s)", first.Status, first.Error)
	}
	g.approver.approveAll()

	// Resumed: run the approved effect, then re-propose the SAME durable write.
	g.rt.Decider = &scriptedDecider{plan: goodPlan, answers: []string{echo, write}}
	out, _ := g.rt.Run(context.Background(), workRequest(t, obj, ""), nil)

	if out.Status != "failed" || !strings.Contains(out.Error, "duplicate") {
		t.Fatalf("a resumed completed effect must not be re-executed: %s (%s)", out.Status, out.Error)
	}
	if g.adapter.count() != 1 {
		t.Fatalf("the approved effect must dispatch exactly once, got %d", g.adapter.count())
	}
	res, _ := g.rt.Memory.Query(
		memory.Identity{ActorID: "owner-1", BusinessID: "biz-1", AgentID: "governed-agent"},
		memory.Query{BusinessID: "biz-1"})
	if len(res.Records) != 1 {
		t.Fatalf("the durable write must exist exactly once, got %d", len(res.Records))
	}
}

// Two concurrent matching callers must not both consume the snapshot. The
// registry lookup, the identity verification and the delete share one critical
// section, so exactly one wins regardless of scheduling — this asserts the
// invariant, not a particular interleaving.
func TestConcurrentMatchingResumesConsumeTheSnapshotOnce(t *testing.T) {
	g := newGovernedFixture(t, seededAllowPolicy(), approvalPolicy(control.ActionToolCall, ""))
	startPausedRun(t, g, Objective{Description: "echo something", SuccessCriteria: []string{"status==ok"}})

	stored := resumeIdentity(t, g.rt, "exec-1")
	req := workRequest(t, Objective{Description: "echo something", SuccessCriteria: []string{"status==ok"}}, "")

	const attempts = 8
	gate := make(chan struct{})
	results := make([]bool, attempts)
	snaps := make([]*runSnapshot, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-gate // release all callers together
			snaps[i], results[i] = g.rt.takeResume("exec-1", stored, req)
		}(i)
	}
	close(gate)
	wg.Wait()

	winners := 0
	for i := range results {
		if results[i] {
			winners++
			if snaps[i] == nil {
				t.Fatalf("a winning caller must receive the snapshot: attempt %d", i)
			}
		} else if snaps[i] != nil {
			t.Fatalf("a losing caller must receive no state at all: attempt %d", i)
		}
	}
	if winners != 1 {
		t.Fatalf("exactly one concurrent resume may consume the snapshot, got %d", winners)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("consuming a snapshot must dispatch nothing on its own, got %d", g.adapter.count())
	}
}

// snapshotState is a comparable fingerprint of everything a snapshot holds, so
// a refusal can be proven to change none of it.
type snapshotFingerprint struct {
	present      bool
	description  string
	criteria     int
	budget       Budget
	usage        BudgetUsage
	effects      int
	observations int
	steps        int
	hasPlan      bool
}

func snapshotState(t *testing.T, rt *Runtime, execID string) snapshotFingerprint {
	t.Helper()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	snap := rt.resumes[execID]
	if snap == nil {
		return snapshotFingerprint{}
	}
	fp := snapshotFingerprint{
		present: true, description: snap.identity.description,
		criteria: len(snap.identity.criteria), budget: snap.identity.budget,
		usage: snap.usage, effects: len(snap.effects), observations: len(snap.observations),
		hasPlan: snap.plan != nil,
	}
	if snap.plan != nil {
		fp.steps = len(snap.plan.Steps)
	}
	return fp
}

func TestMatchingResumeRestoresWithoutRedispatching(t *testing.T) {
	// The positive control for the consumption rule: an identity that really
	// matches loads the snapshot and continues it, executing the approved
	// effect exactly once.
	g := newGovernedFixture(t, seededAllowPolicy(), approvalPolicy(control.ActionToolCall, ""))
	obj := Objective{Description: "echo something", SuccessCriteria: []string{"status==ok"}}
	startPausedRun(t, g, obj)
	stored := resumeIdentity(t, g.rt, "exec-1")
	req := workRequest(t, obj, "")

	snap, ok := g.rt.takeResume("exec-1", stored, req)
	if !ok || snap == nil {
		t.Fatalf("a matching identity must resume: ok=%v", ok)
	}
	if snap.usage.Iterations == 0 {
		t.Fatalf("the restored snapshot must carry the paused run's counters: %+v", snap.usage)
	}
	if len(snap.plan.Steps) == 0 {
		t.Fatal("the restored snapshot must carry the paused run's plan")
	}
	// Re-register so the fixture's own pause bookkeeping stays coherent for any
	// later assertion in this test.
	g.rt.snapshotRun(&run{rt: g.rt, req: req, execID: "exec-1", businessID: req.BusinessID,
		divisionID: req.DivisionID, planVersion: snap.planVersion}, stored, snap.plan, snap.ledger,
		snap.observations, snap.usage, restoreWorking(snap), snap.deadline)
	if g.adapter.count() != 0 {
		t.Fatalf("verification itself must dispatch nothing, got %d", g.adapter.count())
	}
}

// mustParseCriteria parses criteria for a test identity, failing the test on
// malformed input rather than silently comparing against an empty set.
func mustParseCriteria(t *testing.T, raw ...string) []criterion {
	t.Helper()
	crits, err := parseCriteria(raw)
	if err != nil {
		t.Fatalf("parse criteria %v: %v", raw, err)
	}
	return crits
}

// The identity has three independent dimensions. Each must reject on its own,
// and each must accept its own semantically-equivalent spellings.
func TestObjectiveIdentityMatchesOnlyOnTheSameObjective(t *testing.T) {
	base := Objective{Description: "d", SuccessCriteria: []string{"source==model", "status==ok"}}
	baseBudget := Budget{MaxToolCalls: 3}
	id := newObjectiveIdentity(base, baseBudget, mustParseCriteria(t, base.SuccessCriteria...))

	same := []struct {
		name   string
		obj    Objective
		budget Budget
		crits  []string
	}{
		{"identical", base, baseBudget, []string{"source==model", "status==ok"}},
		{"criteria reordered", base, baseBudget, []string{"status==ok", "source==model"}},
		{"criteria respelled", base, baseBudget, []string{" source == model ", "status==ok"}},
		{"criteria duplicated", base, baseBudget, []string{"source==model", "status==ok", "source==model"}},
	}
	for _, tc := range same {
		if !id.matches(newObjectiveIdentity(tc.obj, tc.budget, mustParseCriteria(t, tc.crits...))) {
			t.Fatalf("%s is the same objective and must match", tc.name)
		}
	}

	different := []struct {
		name   string
		obj    Objective
		budget Budget
		crits  []string
	}{
		{"different description", Objective{Description: "other", SuccessCriteria: base.SuccessCriteria}, baseBudget, []string{"source==model", "status==ok"}},
		{"criteria dropped", base, baseBudget, []string{"source==model"}},
		{"criteria added", base, baseBudget, []string{"source==model", "status==ok", "status present"}},
		{"criteria relaxed", base, baseBudget, []string{"status==ok"}},
		{"criteria swapped", base, baseBudget, []string{"source==tool", "status==ok"}},
		{"criteria operator changed", base, baseBudget, []string{"source!=model", "status==ok"}},
		{"criteria count changed", base, baseBudget, []string{"source==model", "status==ok", "effects_completed>=1"}},
		{"budget widened", base, Budget{MaxToolCalls: 4}, []string{"source==model", "status==ok"}},
		{"budget narrowed", base, Budget{MaxToolCalls: 2}, []string{"source==model", "status==ok"}},
		{"budget dropped", base, Budget{}, []string{"source==model", "status==ok"}},
		{"deadline widened", base, Budget{MaxToolCalls: 3, MaxExecutionTime: time.Hour}, []string{"source==model", "status==ok"}},
		{"depth widened", base, Budget{MaxToolCalls: 3, MaxDepth: 7}, []string{"source==model", "status==ok"}},
	}
	for _, tc := range different {
		if id.matches(newObjectiveIdentity(tc.obj, tc.budget, mustParseCriteria(t, tc.crits...))) {
			t.Fatalf("%s is a DIFFERENT objective and must not match", tc.name)
		}
	}
}

// An unset budget and one written out to the cap are the same grant, because
// callers may only tighten autonomy (§9). Normalizing through Clamp is what
// makes a defaulted re-submission a resume rather than a false mismatch.
func TestObjectiveIdentityNormalizesDefaultedBudget(t *testing.T) {
	caps := DefaultCaps()
	crits := mustParseCriteria(t, "status==ok")
	unset := newObjectiveIdentity(Objective{Description: "d"}, Budget{}.Clamp(caps), crits)
	explicit := newObjectiveIdentity(Objective{Description: "d"},
		Budget{MaxToolCalls: caps.MaxToolCalls, MaxIterations: caps.MaxIterations}.Clamp(caps), crits)
	if !unset.matches(explicit) {
		t.Fatal("an unset budget and one written out to the cap are the same grant")
	}
	// Over-cap values clamp to the cap, so an absurd request is also the same
	// grant rather than a mismatch — and it can never widen a resume.
	absurd := newObjectiveIdentity(Objective{Description: "d"},
		Budget{MaxToolCalls: 100000, MaxIterations: 100000}.Clamp(caps), crits)
	if !unset.matches(absurd) {
		t.Fatal("over-cap requests clamp to the cap and must match the default grant")
	}
	if absurd.budget.MaxToolCalls > caps.MaxToolCalls {
		t.Fatal("clamping must never grant more than the cap")
	}
}

func TestCriteriaIdentityIgnoresSpellingAndOrder(t *testing.T) {
	// Unit-level guard on the identity itself: two spellings of the same
	// conjunction are one identity, and a genuinely different conjunction is
	// not.
	parse := func(t *testing.T, raw ...string) objectiveIdentity {
		t.Helper()
		crits, err := parseCriteria(raw)
		if err != nil {
			t.Fatalf("parse %v: %v", raw, err)
		}
		return newObjectiveIdentity(Objective{Description: "d"}, Budget{}, crits)
	}
	base := parse(t, "source==model", "status==ok")
	same := []struct {
		name string
		raw  []string
	}{
		{"reordered", []string{"status==ok", "source==model"}},
		{"whitespace", []string{"  status==ok", " source==model  "}},
		{"both", []string{"source == model", "status==ok "}},
		// ANDing a criterion twice says what saying it once says, so a repeat
		// is the same conjunction, not a stricter one.
		{"duplicated", []string{"source==model", "status==ok", "status==ok"}},
	}
	for _, tc := range same {
		if !base.matches(parse(t, tc.raw...)) {
			t.Fatalf("%s must be the same identity as the base", tc.name)
		}
	}
	different := [][]string{
		{"source==model"},
		{"source==model", "status==failed"},
		{"source!=model", "status==ok"},
		{"source==model", "status present"},
		{"source==model", "status==ok", "effects_completed>=1"},
		{"result.model==x", "status==ok"},
	}
	for _, raw := range different {
		if base.matches(parse(t, raw...)) {
			t.Fatalf("%v must NOT share the base identity", raw)
		}
	}
}

// ---- external duplicate protection across a pause -------------------------
//
// An effect fingerprint is process-local: it stops the RUNTIME from re-running
// the same mutation inside one process. It cannot help once the process is
// gone, and it cannot help a remote system that never saw the fingerprint. The
// mechanism that carries dedupe ACROSS a pause and across a restart is the
// idempotency key, which the capability layer derives from the logical call id
// (OPERATIONAL_RELIABILITY_CONTRACTS §7). So the logical call id must be the
// execution identity — deterministic, and identical on every dispatch of the
// same execution.

// TestToolIdempotencyKeyIsTheExecutionIdentity pins that relationship. The loop
// accepts a request whose CorrelationID is empty and falls back to TaskID for
// its own identity; the dispatch must use that SAME resolved identity, or the
// capability layer falls back to a timestamp and mints a fresh key per
// dispatch — which is precisely the case where a remote system cannot dedupe.
func TestToolIdempotencyKeyIsTheExecutionIdentity(t *testing.T) {
	g := newGovernedFixture(t, seededAllowPolicy())

	// CorrelationID deliberately empty; TaskID is the execution's real identity.
	req := func() *executor.WorkRequest {
		return &executor.WorkRequest{
			TaskID:     "wf-identity", // CorrelationID intentionally omitted
			BusinessID: "biz-1", ActorID: "owner-1", Intent: "echo",
			Input: map[string]string{"agent_objective": EncodeObjective(Objective{Description: "echo twice"})},
		}
	}
	for i := 0; i < 2; i++ {
		g.rt.Decider = &scriptedDecider{plan: goodPlan, answers: []string{governedToolCallAnswer, governedCompleteAnswer}}
		out, _ := g.rt.Run(context.Background(), req(), nil)
		if out.Status != "completed" {
			t.Fatalf("run %d must complete: %s (%s)", i, out.Status, out.Error)
		}
	}
	invs := g.adapter.invocations()
	if len(invs) != 2 {
		t.Fatalf("expected one dispatch per run, got %d", len(invs))
	}
	// The key must be the execution identity, so the same execution always
	// presents the same dedupe token to a remote system.
	if invs[0].IdempotencyKey != "wf-identity" {
		t.Fatalf("the idempotency key must be the execution identity, got %q", invs[0].IdempotencyKey)
	}
	if invs[0].IdempotencyKey != invs[1].IdempotencyKey {
		t.Fatalf("the same execution must present one stable idempotency key: %q then %q",
			invs[0].IdempotencyKey, invs[1].IdempotencyKey)
	}
}

// TestResumeReDispatchKeepsTheExecutionIdempotencyKey pins the same
// relationship AT the approval boundary. The paused half and the resumed half
// are separate runs of one execution, so the dispatch the resumed run makes
// must carry the execution's identity — the token a remote system needs to
// recognise it as the same logical call rather than a new one.
func TestResumeReDispatchKeepsTheExecutionIdempotencyKey(t *testing.T) {
	g := newGovernedFixture(t, seededAllowPolicy(), approvalPolicy(control.ActionToolCall, ""))
	obj := Objective{Description: "echo something"}

	g.rt.Decider = &scriptedDecider{plan: goodPlan, answers: []string{governedToolCallAnswer}}
	first, _ := g.rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if first.Status != "pending_approval" {
		t.Fatalf("the first run must pause pending approval: %s (%s)", first.Status, first.Error)
	}
	if n := len(g.adapter.invocations()); n != 0 {
		t.Fatalf("an approval-pending action must not be dispatched, got %d", n)
	}
	g.approver.approveAll()

	g.rt.Decider = &obsAwareDecider{plan: goodPlan}
	resumed, _ := g.rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if resumed.Status != "completed" {
		t.Fatalf("the resumed run must complete: %s (%s)", resumed.Status, resumed.Error)
	}
	invs := g.adapter.invocations()
	if len(invs) != 1 {
		t.Fatalf("the approved action must dispatch exactly once, got %d", len(invs))
	}
	if invs[0].IdempotencyKey != "exec-1" {
		t.Fatalf("a resumed dispatch must keep the execution's idempotency key, got %q", invs[0].IdempotencyKey)
	}
	if invs[0].CallID != "exec-1" {
		t.Fatalf("a resumed dispatch must keep the execution's logical call id, got %q", invs[0].CallID)
	}
}

// TestRestoredEffectIsNotCountedTwiceAcrossResume pins the upper bound as well
// as the lower one. A resumed run must remember the effect that completed
// before the pause, and must not re-register it when it restores: exactly one
// completed effect, never two.
func TestRestoredEffectIsNotCountedTwiceAcrossResume(t *testing.T) {
	g := newGovernedFixture(t, seededAllowPolicy(), approvalPolicy(control.ActionToolCall, ""))
	g.rt.Decider = &scriptedDecider{plan: goodPlan, answers: []string{durableWriteAnswer, governedToolCallAnswer}}
	obj := Objective{Description: "store then echo",
		SuccessCriteria: []string{"effects_completed>=1", "effects_completed<=1"}}

	first, _ := g.rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if first.Status != "pending_approval" {
		t.Fatalf("the first run must pause pending approval: %s (%s)", first.Status, first.Error)
	}
	g.approver.approveAll()

	g.rt.Decider = &obsAwareDecider{plan: goodPlan}
	resumed, _ := g.rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	// effects_completed<=1 can only hold if the restored fingerprint is counted
	// exactly once: a double count would reject this completion.
	if resumed.Status != "completed" {
		t.Fatalf("a restored effect must be counted exactly once: %s (%s)", resumed.Status, resumed.Error)
	}
	res, _ := g.rt.Memory.Query(
		memory.Identity{ActorID: "owner-1", BusinessID: "biz-1", AgentID: "governed-agent"},
		memory.Query{BusinessID: "biz-1"})
	if len(res.Records) != 1 {
		t.Fatalf("the durable write must exist exactly once across the resume, got %d", len(res.Records))
	}
}

// A paused run snapshots the settled plan/steps/operations so resuming cannot
// inherit stale or aliased provenance. Mutating the resumed copy must never
// mutate the paused snapshot.
func TestCopyResearchEvidenceDeepCopiesProvenance(t *testing.T) {
	original := &execjournal.ResearchEvidence{
		Version:    execjournal.ResearchEvidenceVersion,
		Capability: execjournal.ResearchEvidenceCapability,
		Operation:  execjournal.ResearchEvidenceOperation,
		Count:      1,
		Sources: []execjournal.ResearchSource{
			{ID: "src-1", Title: "One", URL: "https://one.example"},
		},
		Plan: &execjournal.ResearchPlanEvidence{PlanID: "exec-1", Version: 1},
		Steps: []execjournal.ResearchStepEvidence{
			{StepID: "s1", Status: "completed", ObservationID: "obs-1", Dependencies: []string{}},
		},
		Operations: []execjournal.ResearchOperationEvidence{
			{OperationID: "obs-1", StepID: "s1", ToolID: execjournal.ResearchEvidenceCapability,
				Operation: execjournal.ResearchEvidenceOperation, SourceIDs: []string{"src-1"}},
		},
	}
	copied := copyResearchEvidence(original)
	if copied == nil {
		t.Fatal("copy must be present")
	}
	copied.Plan.PlanID = "exec-2"
	copied.Steps[0].Dependencies = append(copied.Steps[0].Dependencies, "s2")
	copied.Operations[0].SourceIDs[0] = "src-9"
	if original.Plan.PlanID != "exec-1" ||
		len(original.Steps[0].Dependencies) != 0 ||
		original.Operations[0].SourceIDs[0] != "src-1" {
		t.Fatalf("copy aliased the paused provenance: %+v", original)
	}

	empty := copyResearchEvidence(&execjournal.ResearchEvidence{
		Version: execjournal.ResearchEvidenceVersion, Capability: execjournal.ResearchEvidenceCapability,
		Operation: execjournal.ResearchEvidenceOperation, Sources: []execjournal.ResearchSource{},
		Operations: []execjournal.ResearchOperationEvidence{{
			OperationID: "obs-empty", StepID: "s1",
			ToolID: execjournal.ResearchEvidenceCapability, Operation: execjournal.ResearchEvidenceOperation,
			SourceIDs: []string{},
		}},
	})
	if empty.Sources == nil {
		t.Fatal("an empty source capture must remain a non-nil JSON array")
	}
	if empty.Operations[0].SourceIDs == nil {
		t.Fatal("an empty source association must remain a non-nil JSON array")
	}
}
