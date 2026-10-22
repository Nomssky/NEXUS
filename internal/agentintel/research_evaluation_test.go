package agentintel

import (
	"context"
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/execjournal"
)

// The control loop attaches a deterministic quality evaluation to the terminal
// outcome of every execution that actually invoked the mediated web.search
// capability. The verdict is bound to the captured evidence and never carries
// model prose or source content.
func TestResearchRunAttachesDeterministicEvaluation(t *testing.T) {
	f := newEvidenceFixture(t)
	f.rt.Decider = &scriptedDecider{
		plan: evidencePlan,
		answers: []string{
			`{"type":"tool_call","tool":"web.search","input":{"query":"nexus capabilities"}}`,
			groundedCompletion("src-1", "src-2"),
		},
	}
	f.provider.queue(envelopeOf(
		"1. Nexus capability boundary | https://docs.example/nexus | docs.example | The runtime decides; the model proposes.",
		"2. Nexus agent model | https://example.org/agents | example.org | Agents are scoped to business and division.",
	))

	out, err := f.rt.Run(context.Background(), workRequest(t, Objective{
		ObjectiveID: "exec-eval", RequestID: "exec-eval",
		Description: "search the web for nexus runtime capabilities",
	}, ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "completed" {
		t.Fatalf("expected completed, got %s: %s", out.Status, out.Error)
	}
	if out.ResearchEvidence == nil {
		t.Fatal("expected research evidence")
	}
	evaluation := out.ResearchEvaluation
	if evaluation == nil {
		t.Fatal("expected a research evaluation on the terminal outcome")
	}
	if evaluation.ExecutionID != "exec-1" {
		t.Fatalf("evaluation is not bound to the execution: %+v", evaluation)
	}
	if evaluation.Version != execjournal.ResearchEvaluationVersion ||
		evaluation.Method != execjournal.ResearchEvaluationMethod {
		t.Fatalf("unexpected evaluation identity: %+v", evaluation)
	}
	if evaluation.EvidenceFingerprint != execjournal.ResearchEvidenceFingerprint(out.ResearchEvidence) {
		t.Fatal("evaluation must be bound to the captured evidence")
	}
	switch evaluation.Status {
	case execjournal.ResearchEvaluationPassed, execjournal.ResearchEvaluationWarnings:
	default:
		t.Fatalf("unexpected evaluation status %q: %+v", evaluation.Status, evaluation.Findings)
	}
	if err := execjournal.ValidateResearchEvaluationForExecution("exec-1", evaluation, out.ResearchEvidence); err != nil {
		t.Fatalf("evaluation failed durable validation: %v", err)
	}
	raw, err := evaluation.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"Nexus capability boundary", "docs.example/nexus", "The runtime decides"} {
		if strings.Contains(string(raw), leaked) {
			t.Fatalf("evaluation leaked source content %q", leaked)
		}
	}
}

// A malformed provider envelope produces no evidence at all. The execution
// must still report an honest, incomplete evaluation instead of pretending the
// research never happened.
func TestMalformedSearchResultYieldsIncompleteEvaluation(t *testing.T) {
	f := newEvidenceFixture(t)
	f.rt.Decider = &scriptedDecider{
		plan: evidencePlan,
		answers: []string{
			`{"type":"tool_call","tool":"web.search","input":{"query":"nexus capabilities"}}`,
			groundedCompletion("src-1"),
		},
	}
	// count disagrees with the number of source rows: the whole snapshot is
	// rejected, exactly as a truncated envelope would be.
	f.provider.queue(map[string]string{"sources": "1. Nexus capability boundary | https://docs.example/nexus | docs.example | x", "count": "2"})

	out, err := f.rt.Run(context.Background(), workRequest(t, Objective{
		ObjectiveID: "exec-malformed", RequestID: "exec-malformed",
		Description: "search the web for nexus runtime capabilities",
	}, ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.ResearchEvidence != nil {
		t.Fatal("a malformed envelope must not become durable evidence")
	}
	evaluation := out.ResearchEvaluation
	if evaluation == nil {
		t.Fatal("expected an evaluation for an attempted research run")
	}
	if evaluation.Status != execjournal.ResearchEvaluationIncomplete {
		t.Fatalf("status = %q, want incomplete", evaluation.Status)
	}
	found := false
	for _, finding := range evaluation.Findings {
		if finding.Code == "evidence_unavailable" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an evidence_unavailable finding: %+v", evaluation.Findings)
	}
	if err := execjournal.ValidateResearchEvaluationForExecution("exec-1", evaluation, nil); err != nil {
		t.Fatalf("evaluation failed durable validation: %v", err)
	}
}

// An execution that never invoked web.search is not a research execution and
// must not receive an evaluation.
func TestNonResearchRunHasNoEvaluation(t *testing.T) {
	f := newEvidenceFixture(t)
	f.rt.Decider = &scriptedDecider{
		plan: evidencePlan,
		answers: []string{
			`{"type":"model_call","prompt":"think"}`,
			`{"type":"complete","result":"report ready"}`,
		},
	}
	out, err := f.rt.Run(context.Background(), workRequest(t, Objective{
		ObjectiveID: "exec-plain", RequestID: "exec-plain",
		Description: "summarize local notes",
	}, ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.ResearchEvaluation != nil {
		t.Fatalf("a non-research execution must not carry an evaluation: %+v", out.ResearchEvaluation)
	}
}
