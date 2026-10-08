package agentintel

// Unit tests for the runtime-owned step ledger
// (contracts/AGENT_EXECUTION_DEPTH_CONTRACTS.md §3, §4, §7): readiness,
// ordering, terminality, blocked propagation, carry-over and cancellation.

import (
	"testing"
)

func ledgerPlan(version int, steps ...Step) *Plan {
	return &Plan{PlanID: "p1", Version: version, Steps: steps}
}

func TestLedgerStartsPendingAndReadiesRoots(t *testing.T) {
	l := newPlanLedger(ledgerPlan(1,
		Step{StepID: "a", Intent: "first"},
		Step{StepID: "b", Intent: "second", Dependencies: []string{"a"}},
	))
	if got := l.get("a").Status; got != StepPending {
		t.Fatalf("a starts %s, want pending", got)
	}
	changed := l.refresh()
	if len(changed) != 1 || changed[0] != "a" || l.get("a").Status != StepReady {
		t.Fatalf("only the root becomes ready: %v", changed)
	}
	if l.get("b").Status != StepPending {
		t.Fatalf("b waits for a, got %s", l.get("b").Status)
	}
	if cur := l.nextReady(); cur == nil || cur.StepID != "a" {
		t.Fatalf("next ready must be a, got %+v", cur)
	}
}

func TestLedgerExecutesInDependencyOrderNotArrayOrder(t *testing.T) {
	// The model listed b first, but b depends on a: the runtime still runs a.
	l := newPlanLedger(ledgerPlan(1,
		Step{StepID: "b", Intent: "second", Dependencies: []string{"a"}},
		Step{StepID: "a", Intent: "first"},
	))
	l.refresh()
	if cur := l.nextReady(); cur == nil || cur.StepID != "a" {
		t.Fatalf("dependency order wins over array order, got %+v", cur)
	}
}

func TestLedgerFullLifecycle(t *testing.T) {
	l := newPlanLedger(ledgerPlan(1, Step{StepID: "a", Intent: "x"}))
	l.refresh()
	if err := l.begin("a"); err != nil {
		t.Fatal(err)
	}
	if err := l.observe("a"); err != nil {
		t.Fatal(err)
	}
	if err := l.finish("a", "obs-1", "done", "completed", 1, false); err != nil {
		t.Fatal(err)
	}
	rec := l.get("a")
	if rec.Status != StepCompleted || rec.ObservationID != "obs-1" || rec.Outcome != "completed" {
		t.Fatalf("completed record must carry its evidence: %+v", rec)
	}
	if !l.allTerminal() {
		t.Fatal("a single finished step is all-terminal")
	}
}

func TestLedgerRefusesIllegalTransitions(t *testing.T) {
	l := newPlanLedger(ledgerPlan(1, Step{StepID: "a", Intent: "x"}))
	l.refresh()
	if err := l.begin("nope"); err == nil {
		t.Fatal("beginning an unknown step must fail")
	}
	// begin twice: second begin is not from ready.
	if err := l.begin("a"); err != nil {
		t.Fatal(err)
	}
	if err := l.begin("a"); err == nil {
		t.Fatal("re-beginning an executing step must fail")
	}
	// finish without observing first.
	if err := l.finish("a", "obs-1", "x", "completed", 1, false); err == nil {
		t.Fatal("finishing a non-observing step must fail")
	}
	// completion without an observation id.
	if err := l.observe("a"); err != nil {
		t.Fatal(err)
	}
	if err := l.finish("a", "", "x", "completed", 1, false); err == nil {
		t.Fatal("completing without an observation must fail")
	}
	if err := l.finish("a", "obs-1", "x", "completed", 1, false); err != nil {
		t.Fatal(err)
	}
	// terminal records are immutable.
	if err := l.finish("a", "obs-2", "rewritten", "completed", 1, false); err == nil {
		t.Fatal("rewriting a completed step must fail")
	}
	if got := l.get("a").ObservationID; got != "obs-1" {
		t.Fatalf("the original evidence must survive: %q", got)
	}
}

func TestLedgerBlocksTransitiveDependents(t *testing.T) {
	l := newPlanLedger(ledgerPlan(1,
		Step{StepID: "a", Intent: "x"},
		Step{StepID: "b", Intent: "y", Dependencies: []string{"a"}},
		Step{StepID: "c", Intent: "z", Dependencies: []string{"b"}},
		Step{StepID: "free", Intent: "w"},
	))
	l.refresh()
	mustBegin := func(id string) {
		t.Helper()
		if err := l.begin(id); err != nil {
			t.Fatal(err)
		}
		if err := l.observe(id); err != nil {
			t.Fatal(err)
		}
	}
	mustBegin("a")
	if err := l.finish("a", "obs-a", "a failed", "failed", 1, true); err != nil {
		t.Fatal(err)
	}
	changed := l.refresh()
	blocked := map[string]bool{}
	for _, id := range changed {
		if l.get(id).Status == StepBlocked {
			blocked[id] = true
		}
	}
	if !blocked["b"] || !blocked["c"] {
		t.Fatalf("failure must block the whole downstream chain: %v", changed)
	}
	if l.get("free").Status != StepReady {
		t.Fatalf("an independent step stays executable, got %s", l.get("free").Status)
	}
	if cur := l.nextReady(); cur == nil || cur.StepID != "free" {
		t.Fatalf("only the independent step is offered, got %+v", cur)
	}
}

func TestLedgerCancelledDependencyBlocks(t *testing.T) {
	l := newPlanLedger(ledgerPlan(1,
		Step{StepID: "a", Intent: "x"},
		Step{StepID: "b", Intent: "y", Dependencies: []string{"a"}},
	))
	l.refresh()
	if err := l.begin("a"); err != nil {
		t.Fatal(err)
	}
	// Cancellation lands mid-step: the dependent can never run under this plan.
	l.records["a"].Status = StepCancelled
	l.refresh()
	if got := l.get("b").Status; got != StepBlocked {
		t.Fatalf("a cancelled dependency blocks, got %s", got)
	}
}

func TestLedgerCancelMarksOnlyNonTerminal(t *testing.T) {
	l := newPlanLedger(ledgerPlan(1,
		Step{StepID: "a", Intent: "x"},
		Step{StepID: "b", Intent: "y"},
	))
	l.refresh()
	if err := l.begin("a"); err != nil {
		t.Fatal(err)
	}
	if err := l.observe("a"); err != nil {
		t.Fatal(err)
	}
	if err := l.finish("a", "obs-a", "done", "completed", 1, false); err != nil {
		t.Fatal(err)
	}
	changed := l.cancel()
	if len(changed) != 1 || changed[0] != "b" {
		t.Fatalf("cancel touches only non-terminal steps: %v", changed)
	}
	if l.get("a").Status != StepCompleted || l.get("b").Status != StepCancelled {
		t.Fatalf("completed stays completed: %+v %+v", l.get("a"), l.get("b"))
	}
}

func TestLedgerCarryCompletedIsImmutable(t *testing.T) {
	old := newPlanLedger(ledgerPlan(1,
		Step{StepID: "a", Intent: "x"},
		Step{StepID: "b", Intent: "y"},
	))
	old.refresh()
	if err := old.begin("a"); err != nil {
		t.Fatal(err)
	}
	if err := old.observe("a"); err != nil {
		t.Fatal(err)
	}
	if err := old.finish("a", "obs-a", "did a", "completed", 1, false); err != nil {
		t.Fatal(err)
	}
	if err := old.begin("b"); err != nil {
		t.Fatal(err)
	}
	if err := old.observe("b"); err != nil {
		t.Fatal(err)
	}
	if err := old.finish("b", "obs-b", "b failed", "failed", 2, true); err != nil {
		t.Fatal(err)
	}
	next := old.carryCompleted(ledgerPlan(2,
		Step{StepID: "a", Intent: "x"},
		Step{StepID: "b", Intent: "y again"},
		Step{StepID: "c", Intent: "new"},
	))
	a := next.get("a")
	if a.Status != StepCompleted || a.ObservationID != "obs-a" || a.Result != "did a" || a.PlanVersion != 1 {
		t.Fatalf("completed steps carry forward with original evidence: %+v", a)
	}
	if next.get("b").Status != StepPending {
		t.Fatalf("a failed step restarts pending under the new plan, got %s", next.get("b").Status)
	}
	if next.get("c").Status != StepPending {
		t.Fatalf("a new step starts pending, got %s", next.get("c").Status)
	}
	// Carried completion is still immutable in the new version.
	if err := next.finish("a", "obs-x", "rewritten", "completed", 1, false); err == nil {
		t.Fatal("a carried completion cannot be rewritten")
	}
	next.refresh()
	if cur := next.nextReady(); cur == nil || (cur.StepID != "b" && cur.StepID != "c") {
		t.Fatalf("only unfinished steps are offered, got %+v", cur)
	}
}

func TestLedgerDescribeAndCounts(t *testing.T) {
	l := newPlanLedger(ledgerPlan(3,
		Step{StepID: "a", Intent: "x"},
		Step{StepID: "b", Intent: "y", Dependencies: []string{"a"}},
	))
	l.refresh()
	desc := l.describe()
	if desc == "" || !containsStr(desc, "a=ready") || !containsStr(desc, "b=pending") {
		t.Fatalf("describe must render authoritative state: %q", desc)
	}
	counts := l.counts()
	if counts[StepReady] != 1 || counts[StepPending] != 1 {
		t.Fatalf("counts must summarize: %v", counts)
	}
	if got := l.settledIDs(StepReady); len(got) != 1 || got[0] != "a" {
		t.Fatalf("settled ids must list plan order: %v", got)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
