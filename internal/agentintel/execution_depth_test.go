package agentintel

// Loop-level execution-depth tests
// (contracts/AGENT_EXECUTION_DEPTH_CONTRACTS.md): the runtime owns step
// order, completion evidence, duplicate suppression, replan carry-over and
// terminal honesty. The model proposes; the ledger decides.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/capability"
	"github.com/Nomssky/NEXUS/internal/foundation/tool"
	"github.com/Nomssky/NEXUS/internal/memory"
)

// planSequenceDecider answers Plan with a different plan per call, so replan
// tests can move v1 → v2 deterministically.
type planSequenceDecider struct {
	plans   []string
	answers []string
	idx     int
}

func (s *planSequenceDecider) Plan(_ context.Context, _ PlanPrompt) (string, error) {
	if len(s.plans) == 0 {
		return goodPlan, nil
	}
	p := s.plans[0]
	if len(s.plans) > 1 {
		s.plans = s.plans[1:]
	}
	return p, nil
}

func (s *planSequenceDecider) Decide(_ context.Context, _ DecisionPrompt) (string, error) {
	if s.idx < len(s.answers) {
		a := s.answers[s.idx]
		s.idx++
		return a, nil
	}
	return `{"type":"complete","result":"done"}`, nil
}

// stepObservingDecider records the step id of every decision prompt.
type stepObservingDecider struct {
	steps   []string
	answers []string
	idx     int
}

func (s *stepObservingDecider) Plan(_ context.Context, _ PlanPrompt) (string, error) {
	return goodPlan, nil
}

func (s *stepObservingDecider) Decide(_ context.Context, p DecisionPrompt) (string, error) {
	s.steps = append(s.steps, p.StepID)
	if s.idx < len(s.answers) {
		a := s.answers[s.idx]
		s.idx++
		return a, nil
	}
	return `{"type":"complete","result":"done"}`, nil
}

func TestFakeCompletionWithoutEvidenceFails(t *testing.T) {
	// The model claims a GitHub issue was created, but no effect ever ran:
	// zero observations means zero evidence, so the execution fails instead
	// of falsely completing.
	rt, _ := newIntelFixture(t, nil, &scriptedDecider{
		plan:    goodPlan,
		answers: []string{`{"type":"complete","result":"GitHub issue created successfully"}`},
	})
	out, _ := rt.Run(context.Background(), workRequest(t, Objective{Description: "file it"}, ""), nil)
	if out.Status != "failed" || !strings.Contains(out.Error, "no runtime evidence") {
		t.Fatalf("an evidenced completion is required: %s / %s", out.Status, out.Error)
	}
}

func TestDuplicateMutatingEffectIsRejectedOnce(t *testing.T) {
	adapter := &outcomeStubAdapter{}
	rt, _ := newReliabilityFixture(t, adapter, tool.SideEffectExternalMutation)
	rt.Decider = &scriptedDecider{
		plan: goodPlan,
		answers: []string{
			`{"type":"tool_call","tool":"ledger","operation":"commit","input":{"amount":"10"}}`,
			`{"type":"tool_call","tool":"ledger","operation":"commit","input":{"amount":"10"}}`,
		},
	}
	out, _ := rt.Run(context.Background(), workRequest(t, Objective{Description: "post twice"}, ""), nil)
	if out.Status != "failed" || !strings.Contains(out.Error, "duplicate") {
		t.Fatalf("a repeated mutation must be rejected: %s / %s", out.Status, out.Error)
	}
	if adapter.attempts != 1 {
		t.Fatalf("the duplicate must never reach the adapter: attempts=%d", adapter.attempts)
	}
}

func TestReadEffectsMayRepeat(t *testing.T) {
	// Reads are harmless to repeat — the reliability layer itself retries
	// them — so the duplicate guard must not suppress a legitimate second
	// read-class call.
	g := newGovernedFixture(t, seededAllowPolicy())
	out := g.run(t, "echo twice", governedToolCallAnswer, governedToolCallAnswer, governedCompleteAnswer)
	if out.Status != "completed" {
		t.Fatalf("a repeated read must still complete: %s / %s", out.Status, out.Error)
	}
	if g.adapter.count() != 2 {
		t.Fatalf("both reads must reach the adapter, got %d", g.adapter.count())
	}
}

func TestDuplicateMemoryWriteIsRejected(t *testing.T) {
	g := newGovernedFixture(t, seededAllowPolicy())
	write := `{"type":"memory_write","key":"k","value":"v","memory_type":"fact"}`
	out := g.run(t, "remember twice", write, write, `{"type":"complete","result":"done"}`)
	if out.Status != "failed" || !strings.Contains(out.Error, "duplicate") {
		t.Fatalf("a repeated durable write must be rejected: %s / %s", out.Status, out.Error)
	}
	res, _ := g.rt.Memory.Query(
		memory.Identity{ActorID: "owner-1", BusinessID: "biz-1", AgentID: "governed-agent"},
		memory.Query{BusinessID: "biz-1"})
	if len(res.Records) != 1 {
		t.Fatalf("exactly the first write must exist, got %d", len(res.Records))
	}
}

func TestLoopExecutesDependenciesBeforeDependents(t *testing.T) {
	obs := &stepObservingDecider{answers: []string{
		`{"type":"tool_call","tool":"echo","input":{"text":"a"}}`,
		`{"type":"tool_call","tool":"echo","input":{"text":"b"}}`,
		`{"type":"complete","result":"done"}`,
	}}
	rt, _ := newIntelFixture(t, nil, obs)
	// b is listed first but depends on a: the runtime must still decide a first.
	rt.Decider = &planAndStepDecider{
		plan: `{"steps":[
			{"step_id":"b","intent":"second","dependencies":["a"]},
			{"step_id":"a","intent":"first"}]}`,
		inner: obs,
	}
	// The planner defaults to the decider, which already serves the fixed plan.
	out, _ := rt.Run(context.Background(), workRequest(t, Objective{Description: "ordered"}, ""), nil)
	if out.Status != "completed" {
		t.Fatalf("dependency-ordered plan must complete: %s / %s", out.Status, out.Error)
	}
	if len(obs.steps) < 2 || obs.steps[0] != "a" || obs.steps[1] != "b" {
		t.Fatalf("a must decide before b regardless of array order: %v", obs.steps)
	}
}

// planAndStepDecider serves one fixed plan while delegating decisions.
type planAndStepDecider struct {
	plan  string
	inner *stepObservingDecider
}

func (s *planAndStepDecider) Plan(_ context.Context, _ PlanPrompt) (string, error) {
	return s.plan, nil
}

func (s *planAndStepDecider) Decide(ctx context.Context, p DecisionPrompt) (string, error) {
	return s.inner.Decide(ctx, p)
}

func TestReplanCarriesCompletedStepsForward(t *testing.T) {
	v1 := `{"steps":[{"step_id":"pa","intent":"do A"},{"step_id":"pb","intent":"do B"}]}`
	v2 := `{"steps":[{"step_id":"pa","intent":"do A"},{"step_id":"pc","intent":"do C"}]}`
	rt, _ := newIntelFixture(t, nil, &planSequenceDecider{
		plans: []string{v1, v2},
		answers: []string{
			`{"type":"tool_call","tool":"echo","input":{"text":"A"}}`,
			`{"type":"replan","reason":"B is obsolete, do C"}`,
			`{"type":"tool_call","tool":"echo","input":{"text":"C"}}`,
			`{"type":"complete","result":"A then C"}`,
		},
	})
	out, _ := rt.Run(context.Background(), workRequest(t, Objective{Description: "adapt"}, ""), nil)
	if out.Status != "completed" {
		t.Fatalf("replan with carried completion must complete: %s / %s", out.Status, out.Error)
	}
	if !strings.Contains(out.Output, "replans=1") || !strings.Contains(out.Output, "observations=2") {
		t.Fatalf("exactly A and C ran plus completion evidence: %q", out.Output)
	}
}

func TestUnknownOutcomeStaysUnknownAtLoopLevel(t *testing.T) {
	adapter := &outcomeStubAdapter{err: fmt.Errorf("%w: response lost after dispatch", capability.ErrUnknownOutcome)}
	rt, _ := newReliabilityFixture(t, adapter, tool.SideEffectExternalMutation)
	rt.Decider = &scriptedDecider{
		plan: goodPlan,
		answers: []string{
			`{"type":"tool_call","tool":"ledger","operation":"commit","input":{"amount":"10"}}`,
		},
	}
	out, _ := rt.Run(context.Background(), workRequest(t, Objective{Description: "post"}, ""), nil)
	if out.Status != "failed" || !strings.Contains(out.Error, "unknown") {
		t.Fatalf("unknown must stay unknown and terminal: %s / %s", out.Status, out.Error)
	}
	if adapter.attempts != 1 {
		t.Fatalf("an unknown mutation is never re-dispatched: attempts=%d", adapter.attempts)
	}
}

// A stalled loop must still terminate and still fail closed. Stalls spend the
// shared replan budget first (contract §7), so the terminal reason is replan
// exhaustion — never a success, never an unbounded spin.
func TestBudgetExhaustionTerminates(t *testing.T) {
	rt, _ := newIntelFixture(t, nil, &scriptedDecider{
		plan:    goodPlan,
		answers: []string{`{"type":"continue"}`},
	})
	out, _ := rt.Run(context.Background(), workRequest(t, Objective{Description: "stall"}, ""), nil)
	if out.Status != "failed" || !strings.Contains(out.Error, "replan budget exhausted") {
		t.Fatalf("a stalled loop must fail closed: %s / %s", out.Status, out.Error)
	}
	if !strings.Contains(out.Error, "state=budget_exhausted") {
		t.Fatalf("stall exhaustion must name its terminal state: %s / %s", out.Status, out.Error)
	}
}

func TestDeadlineExceededTerminates(t *testing.T) {
	rt, _ := newIntelFixture(t, nil, &scriptedDecider{
		plan:    goodPlan,
		answers: []string{`{"type":"continue"}`},
	})
	obj := Objective{Description: "slow", Budget: Budget{MaxExecutionTime: time.Nanosecond}}
	out, _ := rt.Run(context.Background(), workRequest(t, obj, ""), nil)
	if out.Status != "failed" || !strings.Contains(out.Error, "deadline_exceeded") {
		t.Fatalf("an expired deadline must terminate honestly: %s / %s", out.Status, out.Error)
	}
}

func TestDuplicateDependencyRejected(t *testing.T) {
	rt, _ := newIntelFixture(t, nil, &scriptedDecider{
		plan:    `{"steps":[{"step_id":"a","intent":"x","dependencies":["b","b"]},{"step_id":"b","intent":"y"}]}`,
		answers: []string{`{"type":"complete","result":"x"}`},
	})
	out, _ := rt.Run(context.Background(), workRequest(t, Objective{Description: "do"}, ""), nil)
	if out.Status != "failed" || !strings.Contains(out.Error, "duplicate dependency") {
		t.Fatalf("a duplicated dependency must fail the plan: %s / %s", out.Status, out.Error)
	}
}

func TestCyclicPlanRunsNoEffect(t *testing.T) {
	// A → B → A is rejected at validation, before any decision or effect.
	g := newGovernedFixture(t, seededAllowPolicy())
	g.rt.Decider = &planSequenceDecider{
		plans: []string{`{"steps":[
			{"step_id":"a","intent":"x","dependencies":["b"]},
			{"step_id":"b","intent":"y","dependencies":["a"]}]}`},
		answers: []string{governedToolCallAnswer},
	}
	out, _ := g.rt.Run(context.Background(), workRequest(t, Objective{Description: "cycle"}, ""), nil)
	if out.Status != "failed" || !strings.Contains(out.Error, "cycle") {
		t.Fatalf("a cyclic plan must fail before any effect: %s / %s", out.Status, out.Error)
	}
	if g.adapter.count() != 0 {
		t.Fatalf("a rejected plan must reach no adapter, got %d", g.adapter.count())
	}
}
