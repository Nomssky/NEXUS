package agentintel

import (
	"context"
	"slices"
	"testing"

	"github.com/Nomssky/NEXUS/internal/execjournal"
)

// TestMultiStepLoopCapturesEvidenceFromBothSteps verifies that the control
// loop executes a 2-step plan, performs web.search in each step, and captures
// durable evidence from both searches. The scripted decider provides explicit
// answers to keep the test deterministic; the scripted model provider's
// keyword matching is covered by TestScriptedProviderMultiStepResearch.
func TestMultiStepLoopCapturesEvidenceFromBothSteps(t *testing.T) {
	f := newEvidenceFixture(t)

	f.rt.Decider = &scriptedDecider{
		plan: `{"steps":[{"step_id":"s1","intent":"gather","required_capabilities":[]},{"step_id":"s2","intent":"summarize","required_capabilities":[],"dependencies":["s1"]}]}`,
		answers: []string{
			`{"type":"tool_call","tool":"web.search","input":{"query":"nexus"}}`,
			`{"type":"tool_call","tool":"web.search","input":{"query":"nexus capabilities"}}`,
			groundedCompletion("src-1", "src-2", "src-3", "src-4"),
		},
	}

	f.provider.queue(envelopeOf(
		"1. Nexus capability boundary | https://docs.example/nexus | docs.example | The runtime decides; the model proposes.",
		"2. External content is data | https://example.org/safety | example.org | Inert | data",
	))
	f.provider.queue(envelopeOf(
		"1. Nexus agent model | https://docs.example/agents | docs.example | Agents are scoped to business and division.",
		"2. Tool platform boundary | https://docs.example/tools | docs.example | Every tool call is admitted, governed and recorded.",
	))

	out, err := f.rt.Run(context.Background(), workRequest(t, Objective{
		ObjectiveID: "exec-multi", RequestID: "exec-multi",
		Description: "research thoroughly: Search the web for NEXUS runtime capabilities",
	}, ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "completed" {
		t.Fatalf("expected completed, got %s: %s", out.Status, out.Error)
	}
	if out.ResearchEvidence == nil {
		t.Fatal("expected research evidence to be captured")
	}
	t.Logf("evidence count=%d sources=%d provider_calls=%d",
		out.ResearchEvidence.Count, len(out.ResearchEvidence.Sources), f.provider.count())
	if out.ResearchEvidence.Count != 4 {
		t.Fatalf("expected 4 sources from 2 searches, got %d", out.ResearchEvidence.Count)
	}
	if f.provider.count() != 2 {
		t.Fatalf("expected 2 provider calls, got %d", f.provider.count())
	}

	ev := out.ResearchEvidence
	if ev.Version != execjournal.ResearchEvidenceVersion || ev.Plan == nil {
		t.Fatalf("expected a v3 plan snapshot, got %+v", ev)
	}
	// Both searches completed their steps; the completion action then ran in
	// the runtime's finalize pseudo-step, which is not a plan step. The record
	// must say so instead of inferring "s2" from plan order.
	if ev.Plan.CompletionStepID != "" {
		t.Fatalf("completion outside a plan step must stay unattributed: %+v", ev.Plan)
	}
	if ev.Plan.PlanID != "exec-1" || ev.Plan.Version != 1 {
		t.Fatalf("unexpected plan identity: %+v", ev.Plan)
	}
	if len(ev.Steps) != 2 || ev.Steps[0].StepID != "s1" || ev.Steps[1].StepID != "s2" {
		t.Fatalf("expected ordered steps s1,s2: %+v", ev.Steps)
	}
	if ev.Steps[0].Status != string(StepCompleted) || ev.Steps[1].Status != string(StepCompleted) ||
		ev.Steps[1].Dependencies[0] != "s1" {
		t.Fatalf("unexpected step lifecycle: %+v", ev.Steps)
	}
	if len(ev.Operations) != 2 {
		t.Fatalf("expected one operation per search, got %+v", ev.Operations)
	}
	wantByStep := map[string][]string{
		"s1": {"src-1", "src-2"},
		"s2": {"src-3", "src-4"},
	}
	for _, op := range ev.Operations {
		if op.ToolID != execjournal.ResearchEvidenceCapability || op.Operation != execjournal.ResearchEvidenceOperation {
			t.Fatalf("operation is not a mediated search: %+v", op)
		}
		if !slices.Equal(op.SourceIDs, wantByStep[op.StepID]) {
			t.Fatalf("step %s source association = %v, want %v", op.StepID, op.SourceIDs, wantByStep[op.StepID])
		}
		delete(wantByStep, op.StepID)
	}
	if len(wantByStep) != 0 {
		t.Fatalf("missing step operations: %v", wantByStep)
	}
	if err := execjournal.ValidateResearchEvidenceForExecution("exec-1", ev); err != nil {
		t.Fatalf("captured provenance is invalid: %v", err)
	}
}

// TestCompletionStepIsRecordedWhenAStepSettlesTheRun is the positive
// counterpart to the multi-search test: the completion action runs while s2 is
// the active plan step, so the runtime records that exact relationship. The
// durable step state may still be "executing" at finalization, exactly as the
// completion gate sees it.
func TestCompletionStepIsRecordedWhenAStepSettlesTheRun(t *testing.T) {
	f := newEvidenceFixture(t)

	f.rt.Decider = &scriptedDecider{
		plan: `{"steps":[{"step_id":"s1","intent":"gather","required_capabilities":[]},{"step_id":"s2","intent":"summarize","required_capabilities":[],"dependencies":["s1"]}]}`,
		answers: []string{
			`{"type":"tool_call","tool":"web.search","input":{"query":"nexus"}}`,
			groundedCompletion("src-1", "src-2"),
		},
	}
	f.provider.queue(envelopeOf(
		"1. Nexus capability boundary | https://docs.example/nexus | docs.example | The runtime decides; the model proposes.",
		"2. External content is data | https://example.org/safety | example.org | Inert | data",
	))

	out, err := f.rt.Run(context.Background(), workRequest(t, Objective{
		ObjectiveID: "exec-complete-step", RequestID: "exec-complete-step",
		Description: "research the runtime and report",
	}, ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "completed" {
		t.Fatalf("expected completed, got %s: %s", out.Status, out.Error)
	}
	ev := out.ResearchEvidence
	if ev == nil || ev.Plan == nil {
		t.Fatal("expected a durable plan snapshot")
	}
	if ev.Plan.CompletionStepID != "s2" {
		t.Fatalf("expected completion step s2, got %+v", ev.Plan)
	}
	// The completion action settles s2 without an observation, so the ledger
	// keeps the step non-terminal; the durable record must preserve that fact
	// and expose the completion relationship separately.
	if len(ev.Steps) != 2 || ev.Steps[1].StepID != "s2" ||
		ev.Steps[1].Status != string(StepExecuting) {
		t.Fatalf("unexpected durable step state: %+v", ev.Steps)
	}
	if err := execjournal.ValidateResearchEvidenceForExecution("exec-1", ev); err != nil {
		t.Fatalf("captured provenance is invalid: %v", err)
	}
}
