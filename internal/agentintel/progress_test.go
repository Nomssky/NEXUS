package agentintel

// Success-criteria regression tests (contracts/AGENT_INTELLIGENCE_CONTRACTS.md
// §7): the grammar, its fail-closed rejections, the evidence it accepts, and
// the progress/replan accounting that grows out of it.
//
// The rule under test throughout is the same one: criteria are checked against
// structured evidence the RUNTIME produced in THIS execution. Nothing here
// interprets prose, and nothing here treats "we could not tell" as "yes".

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/capability"
	"github.com/Nomssky/NEXUS/internal/control"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
	"github.com/Nomssky/NEXUS/internal/memory"
)

// ---- grammar ---------------------------------------------------------------

func TestCriterionGrammarAccepted(t *testing.T) {
	cases := []struct {
		raw   string
		field string
		op    string
		value string
		count int
		isCnt bool
	}{
		{"source==model", "source", opEqual, "model", 0, false},
		{"source!=model", "source", opNotEqual, "model", 0, false},
		{"action_type==tool_call", "action_type", opEqual, "tool_call", 0, false},
		{"status==ok", "status", opEqual, "ok", 0, false},
		{"status present", "status", opPresent, "", 0, false},
		{"status absent", "status", opAbsent, "", 0, false},
		{"outcome!=failed", "outcome", opNotEqual, "failed", 0, false},
		{"result.output==42", "result.output", opEqual, "42", 0, false},
		{"result.output absent", "result.output", opAbsent, "", 0, false},
		// A key may itself contain a dot: `Result` is a flat map, so the whole
		// remainder is the literal key.
		{"result.a.b==x", "result.a.b", opEqual, "x", 0, false},
		{"effects_completed>=2", "effects_completed", opAtLeast, "", 2, true},
		{"effects_completed<=0", "effects_completed", opAtMost, "", 0, true},
		// Whitespace is trimmed around the field, the operator and the value —
		// and only there. The value itself is compared verbatim.
		{"  source   ==   model  ", "source", opEqual, "model", 0, false},
		{"status != failed", "status", opNotEqual, "failed", 0, false},
		{"effects_completed >= 5", "effects_completed", opAtLeast, "", 5, true},
		{"result.output present ", "result.output", opPresent, "", 0, false},
		// Whitespace inside a value is preserved, because it is data. A value
		// may therefore end in a word that looks like an operator; the operator
		// was found first, so this is the literal value "ok present".
		{"source==tool:my tool", "source", opEqual, "tool:my tool", 0, false},
		{"status==ok present", "status", opEqual, "ok present", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			c, err := parseCriterion(tc.raw)
			if err != nil {
				t.Fatalf("criterion %q must parse: %v", tc.raw, err)
			}
			if c.field != tc.field || c.op != tc.op || c.value != tc.value || c.count != tc.count || c.isCnt != tc.isCnt {
				t.Fatalf("criterion %q parsed as field=%q op=%q value=%q count=%d isCnt=%v",
					tc.raw, c.field, c.op, c.value, c.count, c.isCnt)
			}
		})
	}
}

func TestCriterionGrammarRejected(t *testing.T) {
	// Every one of these is malformed or outside the grammar. None may parse,
	// because a criterion the runtime cannot evaluate must never be treated
	// as satisfied — and must never be silently skipped either.
	cases := []struct{ name, raw string }{
		{"empty", ""},
		{"whitespace only", "   "},
		{"no field", "==x"},
		{"no field bang", "!=x"},
		{"empty value", "source=="},
		{"empty value bang", "source!="},
		{"empty key", "result.==x"},
		{"unknown field", "bogus==x"},
		{"result without dot", "result==x"},
		{"prefix is not a field", "effects==1"},
		{"doubled operator", "status===ok"},
		{"doubled bang operator", "status!==ok"},
		{"ordering operator on field", "status>=1"},
		{"reversed ordering on field", "status<2"},
		{"bare present", "present"},
		{"bare absent", "absent"},
		{"leading operator word", " present"},
		{"trailing junk", "status present extra"},
		{"count without operator", "effects_completed"},
		{"count with equality", "effects_completed==1"},
		{"count missing bound", "effects_completed>="},
		{"count negative", "effects_completed>=-1"},
		{"count fractional", "effects_completed>=1.5"},
		{"count non numeric", "effects_completed>=many"},
		{"count signed", "effects_completed>=+1"},
		{"count two numbers", "effects_completed>=1 2"},
		{"count with present", "effects_completed present"},
		{"count with absent", "effects_completed absent"},
		{"prose", "the report should be convincing"},
		{"only an operator", "=="},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/"+tc.raw, func(t *testing.T) {
			if c, err := parseCriterion(tc.raw); err == nil {
				t.Fatalf("criterion %q must be rejected, parsed as %+v", tc.raw, c)
			}
		})
	}
}

func TestParseCriteriaIsAllOrNothing(t *testing.T) {
	// One malformed criterion fails the whole set: a list is not a bag of
	// best-effort hints.
	if _, err := parseCriteria([]string{"source==model", "nonsense"}); err == nil {
		t.Fatal("a malformed criterion must reject the whole list")
	}
	// An empty list is valid and means "no criteria".
	crits, err := parseCriteria(nil)
	if err != nil || len(crits) != 0 {
		t.Fatalf("empty criteria list: %v %v", crits, err)
	}
	// Duplicate criteria are permitted and each is kept: ANDing them is
	// idempotent, and dropping one would hide a typo.
	crits, err = parseCriteria([]string{"status==ok", "status==ok"})
	if err != nil || len(crits) != 2 {
		t.Fatalf("duplicate criteria must be preserved: %v %v", crits, err)
	}
}

// ---- evidence semantics ----------------------------------------------------

func obs(source, actionType, status, outcome string, result map[string]string) Observation {
	return Observation{Source: source, ActionType: ActionType(actionType), Status: status, Outcome: outcome, Result: result}
}

func TestCriterionEvidence(t *testing.T) {
	known := obs("tool:calculator", "tool_call", "ok", "completed", map[string]string{"result": "42"})
	blank := obs("model", "model_call", "ok", "", nil)
	// noStatus carries no status value at all, which is what "absent" asks
	// about. `blank` has a status; it simply has no outcome.
	noStatus := obs("model", "model_call", "", "", nil)
	unknown := obs("tool:ledger", "tool_call", "unknown", "unknown", map[string]string{"result": "42"})

	cases := []struct {
		name    string
		raw     string
		obs     []Observation
		effects int
		want    bool
	}{
		// == : a known value that matches. Absence is never equality.
		{"== matches", "source==tool:calculator", []Observation{known}, 0, true},
		{"== does not match", "source==tool:ledger", []Observation{known}, 0, false},
		{"== against unset field", "outcome==completed", []Observation{blank}, 0, false},
		{"== existential over window", "status==ok", []Observation{obs("model", "model_call", "failed", "", nil), known}, 0, true},
		{"== repeated identical records", "status==ok", []Observation{known, known, known}, 0, true},
		// != : needs a KNOWN value that differs; absence proves nothing.
		{"!= known different", "source!=tool:ledger", []Observation{known}, 0, true},
		{"!= known equal", "source!=tool:calculator", []Observation{known}, 0, false},
		{"!= unset field only", "outcome!=completed", []Observation{blank}, 0, false},
		{"!= one known different in window", "status!=failed", []Observation{obs("model", "model_call", "failed", "", nil), known}, 0, true},
		{"!= every record equal", "status!=ok", []Observation{known, known}, 0, false},
		// present / absent
		{"present on set field", "outcome present", []Observation{known}, 0, true},
		{"present on unset field", "outcome present", []Observation{blank}, 0, false},
		{"absent on unset field", "outcome absent", []Observation{blank}, 0, true},
		{"absent on set field", "outcome absent", []Observation{known}, 0, false},
		{"present exists in window", "status present", []Observation{noStatus, known}, 0, true},
		{"absent exists in window", "status absent", []Observation{known, noStatus}, 0, true},
		{"absent needs an actual unset record", "status absent", []Observation{known, known}, 0, false},
		// result.<key>
		{"result key matches", "result.result==42", []Observation{known}, 0, true},
		{"result key missing", "result.result==42", []Observation{blank}, 0, false},
		{"result key present", "result.result present", []Observation{known}, 0, true},
		{"other result key absent", "result.other absent", []Observation{known}, 0, true},
		{"empty result value is not set", "result.result present", []Observation{obs("t", "tool_call", "ok", "", map[string]string{"result": ""})}, 0, false},
		// An unknown outcome is evidence that we could not tell. It satisfies
		// nothing, in either direction: not "it worked", not "it did not".
		{"unknown never satisfies ==", "status==unknown", []Observation{unknown}, 0, false},
		{"unknown never satisfies present", "outcome present", []Observation{unknown}, 0, false},
		{"unknown never satisfies !=", "status!=ok", []Observation{unknown}, 0, false},
		{"unknown never satisfies absent", "outcome absent", []Observation{unknown}, 0, false},
		// effects_completed
		{"count at least met", "effects_completed>=2", nil, 2, true},
		{"count at least unmet", "effects_completed>=3", nil, 2, false},
		{"count at most met", "effects_completed<=2", nil, 2, true},
		{"count at most unmet", "effects_completed<=1", nil, 2, false},
		{"count zero at most", "effects_completed<=0", nil, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := parseCriterion(tc.raw)
			if err != nil {
				t.Fatalf("parse %q: %v", tc.raw, err)
			}
			if got := c.satisfied(tc.obs, tc.effects); got != tc.want {
				t.Fatalf("%q against %d observation(s), %d effects: got %v want %v",
					tc.raw, len(tc.obs), tc.effects, got, tc.want)
			}
		})
	}
}

func TestCriteriaAreANDedAndUnmetIsNamed(t *testing.T) {
	ok := obs("tool:calculator", "tool_call", "ok", "completed", nil)
	crits, err := parseCriteria([]string{"source==tool:calculator", "status==failed"})
	if err != nil {
		t.Fatal(err)
	}
	st := evaluateCriteria(crits, []Observation{ok}, 0)
	if st.all {
		t.Fatal("one unmet criterion must keep the whole set unmet")
	}
	if len(st.unmet) != 1 || st.unmet[0] != "status==failed" {
		t.Fatalf("the terminal message must name the unmet criterion, got %v", st.unmet)
	}
	if st := evaluateCriteria(nil, []Observation{ok}, 0); !st.all {
		t.Fatal("no criteria is vacuously satisfied")
	}
}

func TestEvaluationIsBoundedByTheWindow(t *testing.T) {
	// The contract makes the window bound deliberate: evidence that aged out
	// stops counting. This is the memory bound doing its job, not a leak.
	w := NewWorkingMemory()
	for i := 0; i < defaultWorkingObs+4; i++ {
		w.AddObservation(obs("tool:calculator", "tool_call", "ok", "completed", nil))
	}
	crits, err := parseCriteria([]string{"source==tool:calculator"})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(observationsFor(w)); got != defaultWorkingObs {
		t.Fatalf("window must stay bounded at %d, got %d", defaultWorkingObs, got)
	}
	if st := evaluateCriteria(crits, observationsFor(w), 0); !st.all {
		t.Fatal("a criterion satisfied inside the window must hold")
	}
	// Evidence that has aged out of the window is no longer evidence.
	empty := NewWorkingMemory()
	if st := evaluateCriteria(crits, observationsFor(empty), 0); st.all {
		t.Fatal("nothing in the window means nothing is satisfied")
	}
}

// Aging out is a real transition, not just a bound: a criterion satisfied by
// one record stops being satisfied the moment that record is evicted, even
// though nothing about the criterion or the execution changed.
//
// This is only reachable above defaultWorkingObs observations. Production caps
// MaxIterations at 8, so an execution can never reach it; the test drives the
// window directly through the same AddObservation seam the loop uses, with no
// change to any production budget.
func TestSatisfiedCriterionStopsHoldingWhenItsEvidenceAgesOut(t *testing.T) {
	crits, err := parseCriteria([]string{"source==tool:calculator"})
	if err != nil {
		t.Fatal(err)
	}
	// The single record that satisfies the criterion, then filler that will
	// eventually push it out.
	satisfying := obs("tool:calculator", "tool_call", "ok", "completed", nil)
	filler := obs("model", "model_call", "ok", "", nil)

	w := NewWorkingMemory()
	w.AddObservation(satisfying)
	if st := evaluateCriteria(crits, observationsFor(w), 0); !st.all {
		t.Fatal("the criterion must hold while its evidence is in the window")
	}

	// Exactly one short of eviction the criterion still holds, which pins the
	// bound rather than approximating it.
	for i := 0; i < defaultWorkingObs-1; i++ {
		w.AddObservation(filler)
	}
	if got := len(observationsFor(w)); got != defaultWorkingObs {
		t.Fatalf("the window must be exactly full at the boundary: %d", got)
	}
	if st := evaluateCriteria(crits, observationsFor(w), 0); !st.all {
		t.Fatal("the criterion must still hold one record before eviction")
	}

	// The next record evicts it. Nothing was withdrawn and no outcome changed:
	// the evidence simply aged out of the bounded window.
	w.AddObservation(filler)
	if st := evaluateCriteria(crits, observationsFor(w), 0); st.all {
		t.Fatal("a criterion whose only evidence aged out must no longer be satisfied")
	}

	// The tracker sees the same flip, so the flip registers as a loss of
	// progress rather than as a silent re-satisfaction.
	tr := newCriteriaTracker(crits)
	full := NewWorkingMemory()
	full.AddObservation(satisfying)
	tr.advance(observationsFor(full), 0) // baseline: satisfied
	if tr.advance(observationsFor(w), 0) {
		t.Fatal("evidence aging out must never register as progress")
	}

	// Re-satisfying the criterion after eviction is new evidence again.
	w.AddObservation(satisfying)
	if st := evaluateCriteria(crits, observationsFor(w), 0); !st.all {
		t.Fatal("fresh satisfying evidence must satisfy the criterion again")
	}
	if !tr.advance(observationsFor(w), 0) {
		t.Fatal("re-satisfying an aged-out criterion is genuine progress")
	}
}

// effects_completed is deliberately NOT window-bounded: it counts recorded
// effects for the whole execution. The two bounds must not be conflated.
func TestEffectsOutliveTheObservationWindow(t *testing.T) {
	w := NewWorkingMemory()
	for i := 0; i < defaultWorkingObs+8; i++ {
		w.AddObservation(obs("model", "model_call", "ok", "", nil))
	}
	crits, err := parseCriteria([]string{"effects_completed>=1"})
	if err != nil {
		t.Fatal(err)
	}
	wrapped := NewWorkingMemory()
	for i := 0; i < defaultWorkingObs+8; i++ {
		wrapped.AddObservation(obs("model", "model_call", "ok", "", nil))
	}
	if _, err := wrapped.NoteEffect("fp-1"); err != nil {
		t.Fatal(err)
	}
	if len(observationsFor(wrapped)) != defaultWorkingObs {
		t.Fatalf("the observation window must stay bounded: %d", len(observationsFor(wrapped)))
	}
	if wrapped.EffectCount() != 1 {
		t.Fatalf("the effect count must survive window eviction: %d", wrapped.EffectCount())
	}
	if st := evaluateCriteria(crits, observationsFor(wrapped), wrapped.EffectCount()); !st.all {
		t.Fatal("a completed effect must satisfy effects_completed regardless of the window")
	}
	if st := evaluateCriteria(crits, observationsFor(w), w.EffectCount()); st.all {
		t.Fatal("no effects means effects_completed is unmet")
	}
}

func TestEmptyResultMapIsNotEvidence(t *testing.T) {
	// Malformed evidence fails closed: an observation with no Result map can
	// satisfy a field criterion only when that criterion asks for absence.
	crits, err := parseCriteria([]string{"result.k present"})
	if err != nil {
		t.Fatal(err)
	}
	if st := evaluateCriteria(crits, []Observation{obs("t", "tool_call", "ok", "", nil)}, 0); st.all {
		t.Fatal("a missing Result map cannot satisfy a presence criterion")
	}
}

// ---- progress accounting ---------------------------------------------------

func TestTrackerOnlyCountsNewlySatisfiedCriteria(t *testing.T) {
	crits, err := parseCriteria([]string{"status==ok", "source==tool:ledger"})
	if err != nil {
		t.Fatal(err)
	}
	tr := newCriteriaTracker(crits)

	// Baseline: nothing observed yet.
	if tr.advance(nil, 0) {
		t.Fatal("the first evaluation is a baseline, not progress")
	}
	// Evidence that satisfies the first criterion is progress.
	if !tr.advance([]Observation{obs("tool:calculator", "tool_call", "ok", "completed", nil)}, 0) {
		t.Fatal("newly satisfying evidence must count as progress")
	}
	// The same evidence again is not new.
	if tr.advance([]Observation{obs("tool:calculator", "tool_call", "ok", "completed", nil)}, 0) {
		t.Fatal("repeated evidence must not count as progress")
	}
	// Evidence that satisfies nothing new is not progress.
	if tr.advance([]Observation{obs("model", "model_call", "ok", "", nil)}, 0) {
		t.Fatal("evidence satisfying no unmet criterion must not count as progress")
	}
	// Satisfying the second criterion resets again.
	if !tr.advance([]Observation{obs("tool:ledger", "tool_call", "ok", "completed", nil)}, 1) {
		t.Fatal("a newly satisfied criterion must count as progress")
	}
}

func TestTrackerDoesNotGrantProgressOnEmptyEvidence(t *testing.T) {
	// effects_completed>=0 holds before and after: nothing changed, so
	// nothing progressed.
	crits, err := parseCriteria([]string{"effects_completed>=0"})
	if err != nil {
		t.Fatal(err)
	}
	tr := newCriteriaTracker(crits)
	if tr.advance(nil, 0) {
		t.Fatal("the baseline call never reports progress")
	}
	if tr.advance(nil, 0) {
		t.Fatal("an unchanged satisfied criterion is not progress")
	}
}

// ---- loop-level: the completion gate ---------------------------------------

// gatePlan is a one-step plan, so a criteria loop reaches `complete` on the
// second decision without dependency bookkeeping in the way.
const gatePlan = `{"steps":[{"step_id":"s1","intent":"gather"}]}`

func TestCompleteRejectedWhenCriteriaUnmet(t *testing.T) {
	// The model claims success, but the evidence in this execution says
	// otherwise. Under the strict policy the claim is terminal: the run fails
	// and never re-enters the loop.
	rt, _ := newIntelFixture(t, nil, &scriptedDecider{
		plan: gatePlan,
		answers: []string{
			`{"type":"model_call","prompt":"think"}`,
			`{"type":"complete","result":"all done"}`,
		},
	})
	obj := Objective{Description: "verify", SuccessCriteria: []string{"source==tool:calculator"}}
	out, _ := rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if out.Status != "failed" || !strings.Contains(out.Error, "complete rejected") {
		t.Fatalf("an unmet criterion must reject completion: %s / %s", out.Status, out.Error)
	}
	if !strings.Contains(out.Error, "source==tool:calculator") {
		t.Fatalf("the rejection must name the unmet criterion: %s", out.Error)
	}
	if strings.Contains(out.Output, "state=completed") {
		t.Fatal("a rejected completion must never report completed")
	}
	if rt.State("exec-1") != StateFailed {
		t.Fatalf("terminal state=%s", rt.State("exec-1"))
	}
}

func TestCompleteAcceptedWhenCriteriaSatisfied(t *testing.T) {
	rt, _ := newIntelFixture(t, nil, &scriptedDecider{
		plan: gatePlan,
		answers: []string{
			`{"type":"tool_call","tool":"calculator","input":{"a":"6","b":"7","op":"mul"}}`,
			`{"type":"complete","result":"42"}`,
		},
	})
	obj := Objective{Description: "verify", SuccessCriteria: []string{
		"source==tool:calculator", "action_type==tool_call", "status==ok", "result.result==42",
	}}
	out, _ := rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if out.Status != "completed" {
		t.Fatalf("satisfied criteria must complete: %s / %s", out.Status, out.Error)
	}
	if rt.State("exec-1") != StateCompleted {
		t.Fatalf("terminal state=%s", rt.State("exec-1"))
	}
}

func TestInvalidObjectiveCriteriaFailClosedBeforePlanning(t *testing.T) {
	// An objective nobody can check is a defect, not a free pass. It fails
	// before a plan exists and before any effect is possible.
	rt, _ := newIntelFixture(t, nil, &scriptedDecider{plan: gatePlan})
	obj := Objective{Description: "vague", SuccessCriteria: []string{"the report should be convincing"}}
	out, _ := rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if out.Status != "failed" || !strings.Contains(out.Error, "invalid success criteria") {
		t.Fatalf("malformed criteria must fail closed: %s / %s", out.Status, out.Error)
	}
}

func TestCriteriaFreeObjectiveKeepsV1Semantics(t *testing.T) {
	// No criteria: the v1 rule still applies — at least one observation, then
	// the model's `complete` is accepted.
	rt, _ := newIntelFixture(t, nil, &scriptedDecider{
		plan: gatePlan,
		answers: []string{
			`{"type":"tool_call","tool":"calculator","input":{"a":"6","b":"7","op":"mul"}}`,
			`{"type":"complete","result":"42"}`,
		},
	})
	out, _ := rt.Run(context.Background(), workRequest(t, Objective{Description: "no criteria"}, ""), nil)
	if out.Status != "completed" {
		t.Fatalf("a criteria-free objective must keep completing: %s / %s", out.Status, out.Error)
	}
}

func TestCompletionStillNeedsRuntimeEvidenceWithCriteria(t *testing.T) {
	// The v1 evidence rule is not superseded by criteria — it is checked
	// first. A criteria-bearing objective that completes with no observation
	// at all is rejected for having no evidence, not for its criteria.
	rt, _ := newIntelFixture(t, nil, &scriptedDecider{
		plan:    gatePlan,
		answers: []string{`{"type":"complete","result":"all done"}`},
	})
	obj := Objective{Description: "verify", SuccessCriteria: []string{"source==tool:calculator"}}
	out, _ := rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if out.Status != "failed" || !strings.Contains(out.Error, "no runtime evidence") {
		t.Fatalf("completion still needs evidence: %s / %s", out.Status, out.Error)
	}
}

func TestNoWorkPlanStillRejectsComplete(t *testing.T) {
	// "The plan has no steps requiring work" is unchanged: with no current
	// step and no observation, `complete` is rejected at validation. The
	// criteria gate sits after this check and cannot weaken it.
	rt, _ := newIntelFixture(t, nil, &scriptedDecider{plan: gatePlan})
	err := ValidateAction(Action{Type: ActionComplete}, &ActionContext{
		Objective: Objective{Description: "idle"}, Iterations: 1,
		Used: BudgetUsage{}, Budget: Budget{MaxReplans: 1}, StepHasWork: false,
	}, rt.scope(), rt.Caps)
	if err == nil || !strings.Contains(err.Error(), "complete with no work performed") {
		t.Fatalf("a plan with no work requiring must reject complete: %v", err)
	}
}

func TestUnknownOutcomeNeverCompletesACriteriaObjective(t *testing.T) {
	// The tool's outcome could not be determined. Nothing about that may be
	// read as success, and the mutation is never re-dispatched to find out.
	adapter := &outcomeStubAdapter{err: fmt.Errorf("%w: response lost after dispatch", capability.ErrUnknownOutcome)}
	rt, _ := newReliabilityFixture(t, adapter, tool.SideEffectExternalMutation)
	rt.Decider = &scriptedDecider{
		plan: gatePlan,
		answers: []string{
			`{"type":"tool_call","tool":"ledger","operation":"commit","input":{"amount":"10"}}`,
			`{"type":"complete","result":"done"}`,
		},
	}
	obj := Objective{Description: "commit", SuccessCriteria: []string{"status==unknown", "outcome==unknown"}}
	out, _ := rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if out.Status != "failed" || !strings.Contains(out.Error, "unknown") {
		t.Fatalf("an unknown outcome must never complete the objective: %s / %s", out.Status, out.Error)
	}
	if strings.Contains(out.Output, "state=completed") {
		t.Fatal("an unknown outcome must never report completed")
	}
	if adapter.attempts != 1 {
		t.Fatalf("an unknown mutation is never re-dispatched: attempts=%d", adapter.attempts)
	}
}

func TestEffectsCompletedCountsOnlyCompletedMutations(t *testing.T) {
	// A read-class tool never registers an effect, so it cannot satisfy an
	// effects_completed criterion however often it runs.
	rt, _ := newIntelFixture(t, nil, &scriptedDecider{
		plan: gatePlan,
		answers: []string{
			`{"type":"tool_call","tool":"calculator","input":{"a":"1","b":"1","op":"add"}}`,
			`{"type":"complete","result":"done"}`,
		},
	})
	obj := Objective{Description: "reads", SuccessCriteria: []string{"effects_completed>=1"}}
	out, _ := rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if out.Status != "failed" || !strings.Contains(out.Error, "effects_completed>=1") {
		t.Fatalf("a read must not count as a completed effect: %s / %s", out.Status, out.Error)
	}
}

func TestCompletedMutationSatisfiesEffectsCompleted(t *testing.T) {
	// The positive half of the same rule: a write-class mutation that ran to
	// success is a completed effect and does satisfy the count.
	adapter := &outcomeStubAdapter{}
	rt, _ := newReliabilityFixture(t, adapter, tool.SideEffectExternalMutation)
	rt.Decider = &scriptedDecider{
		plan: gatePlan,
		answers: []string{
			`{"type":"tool_call","tool":"ledger","operation":"commit","input":{"amount":"10"}}`,
			`{"type":"complete","result":"done"}`,
		},
	}
	obj := Objective{Description: "commit", SuccessCriteria: []string{"effects_completed>=1", "effects_completed<=1"}}
	out, _ := rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if out.Status != "completed" {
		t.Fatalf("a completed mutation must satisfy effects_completed: %s / %s", out.Status, out.Error)
	}
}

// ---- loop-level: progress, replanning, budgets -----------------------------

// countingDecider answers Plan with the same plan per call, so the number of
// planning rounds is directly observable.
type countingDecider struct {
	planCalls int
	answers   []string
	idx       int
}

func (c *countingDecider) Plan(_ context.Context, _ PlanPrompt) (string, error) {
	c.planCalls++
	return gatePlan, nil
}

func (c *countingDecider) Decide(_ context.Context, _ DecisionPrompt) (string, error) {
	if c.idx < len(c.answers) {
		a := c.answers[c.idx]
		c.idx++
		return a, nil
	}
	return `{"type":"continue"}`, nil
}

func TestStalledProgressReplansWithinBudgetAndTerminates(t *testing.T) {
	// A loop that only ever says "continue" satisfies nothing. It must spend
	// replans from the shared budget and then stop — bounded, not spinning.
	d := &countingDecider{answers: []string{`{"type":"continue"}`}}
	rt, _ := newIntelFixture(t, nil, d)
	obj := Objective{Description: "stall", SuccessCriteria: []string{"source==never-happens"}}
	out, _ := rt.Run(context.Background(), workRequest(t, obj, ""), nil)

	if out.Status != "failed" || !strings.Contains(out.Error, "state=budget_exhausted") {
		t.Fatalf("a stalled loop must terminate as budget_exhausted: %s / %s", out.Status, out.Error)
	}
	if !strings.Contains(out.Error, "progress stalled") {
		t.Fatalf("the terminal message must name the stall: %s", out.Error)
	}
	// One initial plan plus exactly MaxReplans replans, and not one more.
	if want := 1 + rt.Caps.MaxReplans; d.planCalls != want {
		t.Fatalf("stalled progress must replan exactly MaxReplans times: plan calls=%d, want %d", d.planCalls, want)
	}
	if rt.State("exec-1") != StateBudgetExhausted {
		t.Fatalf("terminal state=%s", rt.State("exec-1"))
	}
}

func TestProgressTriggeredAndModelReplansShareOneBudget(t *testing.T) {
	// MaxReplans=1. The model spends it on a replan of its own; the next
	// stall must then find nothing left rather than quietly buying another.
	rt, _ := newIntelFixture(t, nil, &scriptedDecider{
		plan: gatePlan,
		answers: []string{
			`{"type":"replan","reason":"try another way"}`,
			`{"type":"continue"}`,
			`{"type":"continue"}`,
		},
	})
	rt.Caps.MaxReplans = 1
	obj := Objective{Description: "stall", SuccessCriteria: []string{"source==never-happens"}}
	out, _ := rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if out.Status != "failed" || !strings.Contains(out.Error, "replan budget exhausted (1)") {
		t.Fatalf("the shared replan budget must bind both triggers: %s / %s", out.Status, out.Error)
	}
}

func TestSatisfyingEvidenceThenCompleteSucceeds(t *testing.T) {
	// Observations that satisfy nothing beyond an already-satisfied criterion
	// do not trip the stall rule; the run still reaches `complete`.
	rt, _ := newIntelFixture(t, nil, &scriptedDecider{
		plan: gatePlan,
		answers: []string{
			`{"type":"model_call","prompt":"one"}`,
			`{"type":"model_call","prompt":"two"}`,
			`{"type":"complete","result":"done"}`,
		},
	})
	obj := Objective{Description: "mixed", SuccessCriteria: []string{"source==model"}}
	out, _ := rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if out.Status != "completed" {
		t.Fatalf("a satisfied criterion must let the run complete: %s / %s", out.Status, out.Error)
	}
}

func TestCriteriaDoNotExtendTheDeadline(t *testing.T) {
	// Progress accounting is bookkeeping; it must not buy more time.
	rt, _ := newIntelFixture(t, nil, &scriptedDecider{
		plan:    gatePlan,
		answers: []string{`{"type":"continue"}`},
	})
	obj := Objective{Description: "slow", SuccessCriteria: []string{"source==never-happens"},
		Budget: Budget{MaxExecutionTime: time.Nanosecond}}
	out, _ := rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if out.Status != "failed" || !strings.Contains(out.Error, "state=deadline_exceeded") {
		t.Fatalf("the deadline must still win: %s / %s", out.Status, out.Error)
	}
}

func TestCriteriaSurviveAnApprovalResume(t *testing.T) {
	// An approval pause splits one execution in two. The resumed run starts
	// with a fresh criteria tracker, so it must be able to see the evidence
	// the paused half produced — otherwise a criterion could never be met by
	// any execution that needed approval to finish.
	g := newGovernedFixture(t, seededAllowPolicy(), approvalPolicy(control.ActionToolCall, ""))
	g.rt.Decider = &obsAwareDecider{plan: goodPlan}
	obj := Objective{Description: "echo something", SuccessCriteria: []string{"source==tool:governed.echo", "status==ok"}}

	first, _ := g.rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if first.Status != "pending_approval" {
		t.Fatalf("the first run must pause pending approval: %s (%s)", first.Status, first.Error)
	}
	g.approver.approveAll()

	resumed, _ := g.rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if resumed.Status != "completed" {
		t.Fatalf("a resumed run must still be able to satisfy its criteria: %s (%s)", resumed.Status, resumed.Error)
	}
	if g.adapter.count() != 1 {
		t.Fatalf("the approved effect must run exactly once, got %d", g.adapter.count())
	}
}

func TestCancellationBeatsTheCompletionGate(t *testing.T) {
	// Cancelling a criteria loop ends it as cancelled — the gate never turns
	// a cancellation into a success or into a stall.
	rt, _ := newIntelFixture(t, nil, &scriptedDecider{plan: gatePlan})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	obj := Objective{Description: "cancel", SuccessCriteria: []string{"source==never-happens"}}
	out, _ := rt.Run(ctx, workRequest(t, obj, ""), nil)
	if out.Status != "cancelled" || !strings.Contains(out.Error, "state=cancelled") {
		t.Fatalf("cancellation must win over the gate: %s / %s", out.Status, out.Error)
	}
	if strings.Contains(out.Output, "state=completed") {
		t.Fatal("a cancelled run must never report completed")
	}
}

// ---- effects_completed across the approval boundary ------------------------
//
// Contract §7: `effects_completed` counts the completed consequential effects
// recorded by the runtime in this execution, and "excludes attempted,
// rejected, awaiting-approval, cancelled, failed and unknown outcomes". The
// approval pause is where that boundary is easiest to get wrong — it is the one
// place where an action really did start, really did reach the adapter, and
// still must not count as done.

const durableWriteAnswer = `{"type":"memory_write","key":"k","value":"v","memory_type":"fact"}`

// TestAwaitingApprovalEffectIsNotACompletedEffect proves an action that is only
// AWAITING approval is not yet a completed effect. The run below performs a
// durable write (which succeeds and is fingerprinted) and then proposes a
// governed tool call that stops at REQUIRE_APPROVAL. The snapshot must carry
// exactly one effect: the write that finished, not the tool call that paused.
func TestAwaitingApprovalEffectIsNotACompletedEffect(t *testing.T) {
	g := newGovernedFixture(t, seededAllowPolicy(), approvalPolicy(control.ActionToolCall, ""))
	g.rt.Decider = &scriptedDecider{plan: goodPlan, answers: []string{durableWriteAnswer, governedToolCallAnswer}}
	obj := Objective{Description: "store then echo",
		SuccessCriteria: []string{"effects_completed>=1"}}

	out, _ := g.rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if out.Status != "pending_approval" {
		t.Fatalf("the run must pause pending approval: %s (%s)", out.Status, out.Error)
	}

	g.rt.mu.Lock()
	snap := g.rt.resumes["exec-1"]
	g.rt.mu.Unlock()
	if snap == nil {
		t.Fatal("the paused run must leave a snapshot")
	}
	// Exactly the completed durable write. The governed tool call is still
	// awaiting approval: it ran no effect, so it must not be counted.
	if len(snap.effects) != 1 {
		t.Fatalf("only the completed write may be a completed effect, got %d: %v", len(snap.effects), snap.effects)
	}
	if n := restoreWorking(snap).EffectCount(); n != 1 {
		t.Fatalf("restored working memory must carry exactly one completed effect, got %d", n)
	}
	// The write really did execute: the paused run is not vacuously empty.
	res, _ := g.rt.Memory.Query(
		memory.Identity{ActorID: "owner-1", BusinessID: "biz-1", AgentID: "governed-agent"},
		memory.Query{BusinessID: "biz-1"})
	if len(res.Records) != 1 {
		t.Fatalf("the durable write must have executed, got %d records", len(res.Records))
	}
}

// TestCompletedEffectsSurviveApprovalResume proves the completed-effect count
// is scoped to the EXECUTION, not to a single process step of it. The write
// completed before the pause, so the resumed run must still count it. If the
// restored working memory forgot the fingerprint, the resumed run would see an
// empty effect ledger, reject `effects_completed>=1`, and fail an objective
// that had in fact already done its work.
func TestCompletedEffectsSurviveApprovalResume(t *testing.T) {
	g := newGovernedFixture(t, seededAllowPolicy(), approvalPolicy(control.ActionToolCall, ""))
	g.rt.Decider = &scriptedDecider{plan: goodPlan, answers: []string{durableWriteAnswer, governedToolCallAnswer}}
	obj := Objective{Description: "store then echo",
		SuccessCriteria: []string{"effects_completed>=1", "source==memory"}}

	first, _ := g.rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if first.Status != "pending_approval" {
		t.Fatalf("the first run must pause pending approval: %s (%s)", first.Status, first.Error)
	}
	g.approver.approveAll()

	// Resume: the approved tool call runs, then the model completes. The
	// criteria are satisfied by the PRE-PAUSE write plus the restored window.
	g.rt.Decider = &obsAwareDecider{plan: goodPlan}
	resumed, _ := g.rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if resumed.Status != "completed" {
		t.Fatalf("a resumed run must still count the effect completed before the pause: %s (%s)",
			resumed.Status, resumed.Error)
	}
	// The durable write must not have been repeated by the resumed run.
	res, _ := g.rt.Memory.Query(
		memory.Identity{ActorID: "owner-1", BusinessID: "biz-1", AgentID: "governed-agent"},
		memory.Query{BusinessID: "biz-1"})
	if len(res.Records) != 1 {
		t.Fatalf("the completed write must exist exactly once across the resume, got %d", len(res.Records))
	}
}

// TestResumedRunEarnsNoFreeProgressReset proves progress accounting is honest
// across the pause. The resumed run starts with a FRESH criteria tracker, so
// without seeding it would re-evaluate already-satisfied criteria as newly
// satisfied and take a free no-progress reset it never earned — masking a
// genuinely stalled execution. With seeding, the first barren iteration of the
// resumed run must stall and spend a replan, exactly as it would have without
// any pause.
func TestResumedRunEarnsNoFreeProgressReset(t *testing.T) {
	g := newGovernedFixture(t, seededAllowPolicy(), approvalPolicy(control.ActionToolCall, ""))
	d := &planCountingDecider{answers: []string{durableWriteAnswer, governedToolCallAnswer}}
	g.rt.Decider = d
	// One barren iteration is a stall, so the reset cannot hide behind the
	// default grace of two.
	g.rt.Caps.MaxConsecutiveNoProgress = 1
	obj := Objective{Description: "store then echo",
		SuccessCriteria: []string{"source==memory"}}

	first, _ := g.rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if first.Status != "pending_approval" {
		t.Fatalf("the first run must pause pending approval: %s (%s)", first.Status, first.Error)
	}
	if d.plans != 1 {
		t.Fatalf("the first run must not replan, planning rounds=%d", d.plans)
	}
	g.approver.approveAll()

	// Resume: the approved tool call produces an observation that satisfies
	// nothing new, so it must count as no progress.
	d.script(governedToolCallAnswer, governedCompleteAnswer)
	resumed, _ := g.rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if resumed.Status != "completed" {
		t.Fatalf("the resumed run must still complete: %s (%s)", resumed.Status, resumed.Error)
	}
	// 1 initial plan + exactly 1 stall-driven replan. Without seeding the
	// resumed run would credit its restored criteria as new progress and stop
	// at 1 planning round.
	if d.plans != 2 {
		t.Fatalf("a resumed run must not take a free progress reset: planning rounds=%d, want 2", d.plans)
	}
}

// planCountingDecider answers from a replaceable script and counts planning
// rounds, so a test can observe replanning without reaching into internals.
type planCountingDecider struct {
	plans   int
	answers []string
	idx     int
}

func (d *planCountingDecider) Plan(context.Context, PlanPrompt) (string, error) {
	d.plans++
	return goodPlan, nil
}

func (d *planCountingDecider) Decide(context.Context, DecisionPrompt) (string, error) {
	if d.idx < len(d.answers) {
		a := d.answers[d.idx]
		d.idx++
		return a, nil
	}
	return governedCompleteAnswer, nil
}

// script replaces the answer script and rewinds, so a second run of the same
// execution can be driven from the start.
func (d *planCountingDecider) script(answers ...string) {
	d.answers = answers
	d.idx = 0
}

// ---- cancellation vs completion in the same execution window --------------
//
// The v18 gate proves a cancellation that is ALREADY visible when the run
// starts wins. This proves the harder ordering: evidence that satisfied the
// objective was already collected, and only THEN the caller cancelled — while
// the model was mid-decision. A satisfied criterion must never rescue a
// completion that a later cancellation has already invalidated.

// cancellingDecider cancels the caller's context as a side effect of deciding,
// so cancellation lands at a known point in the iteration: after the model has
// answered, before the action is dispatched. No sleeps, no timing.
type cancellingDecider struct {
	plan    string
	answers []string
	idx     int
	cancel  context.CancelFunc
}

func (d *cancellingDecider) Plan(context.Context, PlanPrompt) (string, error) { return d.plan, nil }

func (d *cancellingDecider) Decide(context.Context, DecisionPrompt) (string, error) {
	a := d.answers[d.idx]
	if d.idx < len(d.answers)-1 {
		d.idx++
	} else {
		// The final decision is the completion claim; cancelling here means the
		// cancellation and the completion claim become eligible in the same
		// iteration, with the cancellation first.
		d.cancel()
	}
	return a, nil
}

// TestCancellationDuringIterationBeatsAlreadySatisfiedCriteria proves a
// cancellation arriving mid-iteration ends the run cancelled even though the
// objective's criteria were already satisfied by real runtime evidence.
func TestCancellationDuringIterationBeatsAlreadySatisfiedCriteria(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &cancellingDecider{
		plan: goodPlan,
		// Iteration 1 produces evidence satisfying every criterion below.
		// Iteration 2 claims completion, and cancels on the way out.
		answers: []string{
			`{"type":"tool_call","tool":"calculator","input":{"a":"6","b":"7","op":"mul"}}`,
			`{"type":"complete","result":"42"}`,
		},
		cancel: cancel,
	}
	rt, _ := newIntelFixture(t, nil, d)
	obj := Objective{Description: "multiply then finish",
		SuccessCriteria: []string{"source==tool:calculator", "action_type==tool_call", "status==ok"}}

	out, _ := rt.Run(ctx, workRequest(t, obj, ""), nil)

	// The criteria WERE satisfiable — proven by running the same script with no
	// cancellation at all (below). So a completed result here would mean the
	// cancellation lost to evidence.
	if out.Status != "cancelled" {
		t.Fatalf("a cancellation accepted mid-iteration must end the run cancelled: %s (%s)", out.Status, out.Error)
	}
	if strings.Contains(out.Output, "state=completed") || strings.Contains(out.Error, "complete rejected") == false && strings.Contains(out.Error, "state=completed") {
		t.Fatalf("a cancelled run must not report a completed terminal state: %s", out.Output)
	}

	// Control: the identical objective and script WITHOUT cancellation really
	// does complete, so the assertions above are about ordering, not about
	// criteria that could never be met.
	ok := &scriptedDecider{plan: goodPlan, answers: []string{
		`{"type":"tool_call","tool":"calculator","input":{"a":"6","b":"7","op":"mul"}}`,
		`{"type":"complete","result":"42"}`,
	}}
	rt2, _ := newIntelFixture(t, nil, ok)
	ctrl, _ := rt2.Run(context.Background(), workRequest(t, obj, ""), nil)
	if ctrl.Status != "completed" {
		t.Fatalf("the control run must complete, proving the criteria are satisfiable: %s (%s)", ctrl.Status, ctrl.Error)
	}
}

// TestRerunningAnExecutionIDInheritsNoEvidence proves an execution cannot be
// "reactivated" by reusing its id. The loop's scratch state is per-execution:
// a second run under the same execution id starts with no observations, no
// completed-effect ledger and zeroed counters, so criteria that the first run
// satisfied are NOT satisfied by the second.
func TestRerunningAnExecutionIDInheritsNoEvidence(t *testing.T) {
	first := &scriptedDecider{plan: goodPlan, answers: []string{
		`{"type":"tool_call","tool":"calculator","input":{"a":"6","b":"7","op":"mul"}}`,
		`{"type":"complete","result":"42"}`,
	}}
	rt, _ := newIntelFixture(t, nil, first)
	// `workRequest` always uses execution id exec-1, so both runs share an id.
	satisfied := Objective{Description: "multiply", SuccessCriteria: []string{"source==tool:calculator"}}
	out, _ := rt.Run(context.Background(), workRequest(t, satisfied, ""), nil)
	if out.Status != "completed" {
		t.Fatalf("the first run must complete: %s (%s)", out.Status, out.Error)
	}

	// Same execution id, a criterion this run cannot satisfy. If any evidence
	// carried over, this would complete on the FIRST run's observation.
	second := &scriptedDecider{plan: goodPlan, answers: []string{
		`{"type":"model_call","prompt":"think"}`,
		`{"type":"complete","result":"done"}`,
	}}
	rt.Decider = second
	unsatisfiable := Objective{Description: "multiply", SuccessCriteria: []string{"source==tool:ledger"}}
	out2, _ := rt.Run(context.Background(), workRequest(t, unsatisfiable, ""), nil)
	if out2.Status != "failed" || !strings.Contains(out2.Error, "source==tool:ledger") {
		t.Fatalf("a re-used execution id must inherit no evidence: %s (%s)", out2.Status, out2.Error)
	}
}
