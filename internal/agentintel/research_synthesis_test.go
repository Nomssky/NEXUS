package agentintel

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/executor"
)

// A completed research execution attaches the validated typed synthesis to the
// terminal outcome so the journal can persist it. The synthesis is the
// validator-accepted answer: it cites execution-scoped source ids that resolve
// in this run's own evidence, and it never carries source content.
func TestResearchRunAttachesValidatedSynthesis(t *testing.T) {
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
		ObjectiveID: "exec-synth", RequestID: "exec-synth",
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
	synthesis := out.ResearchSynthesis
	if synthesis == nil {
		t.Fatal("expected a validated synthesis on the terminal outcome")
	}
	if synthesis.Status != "complete" {
		t.Fatalf("synthesis status = %q", synthesis.Status)
	}
	if len(synthesis.EvidenceBacked) != 1 ||
		len(synthesis.EvidenceBacked[0].SourceIDs) != 2 ||
		synthesis.EvidenceBacked[0].SourceIDs[0] != "src-1" {
		t.Fatalf("synthesis statements/citations mismatch: %+v", synthesis.EvidenceBacked)
	}
	// The synthesis carries ids and statements only. Source content stays in
	// the evidence snapshot and is never copied into the synthesis.
	raw, err := json.Marshal(synthesis)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"Nexus capability boundary", "docs.example/nexus", "The runtime decides"} {
		if strings.Contains(string(raw), leaked) {
			t.Fatalf("synthesis leaked source content %q", leaked)
		}
	}
}

// The synthesis the loop attaches must be the one the durable validator
// accepts: it binds to the captured evidence and passes the same shape,
// citation-resolution and status rules the journal enforces.
func TestResearchRunSynthesisPassesDurableValidation(t *testing.T) {
	f := newEvidenceFixture(t)
	f.rt.Decider = &scriptedDecider{
		plan: evidencePlan,
		answers: []string{
			`{"type":"tool_call","tool":"web.search","input":{"query":"nexus capabilities"}}`,
			groundedCompletion("src-1"),
		},
	}
	f.provider.queue(envelopeOf(
		"1. Nexus capability boundary | https://docs.example/nexus | docs.example | The runtime decides; the model proposes.",
	))

	out, err := f.rt.Run(context.Background(), workRequest(t, Objective{
		ObjectiveID: "exec-synth-valid", RequestID: "exec-synth-valid",
		Description: "search the web for nexus runtime capabilities",
	}, ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.ResearchSynthesis == nil || out.ResearchEvidence == nil {
		t.Fatal("expected synthesis and evidence")
	}
	durable := executor.DurableResearchSynthesis(out.ResearchSynthesis, "exec-1", out.ResearchEvidence)
	if err := execjournal.ValidateResearchSynthesisForExecution("exec-1", durable, out.ResearchEvidence); err != nil {
		t.Fatalf("runtime synthesis failed durable validation: %v", err)
	}
	if durable.EvidenceFingerprint != execjournal.ResearchEvidenceFingerprint(out.ResearchEvidence) {
		t.Fatal("durable synthesis must be bound to the captured evidence")
	}
}

// A synthesis whose citation does not resolve in this execution is never
// accepted by the completion gate, so the runtime never hands a cross-execution
// citation to the journal.
func TestResearchRunRejectsSynthesisWithUnknownCitation(t *testing.T) {
	f := newEvidenceFixture(t)
	f.rt.Decider = &scriptedDecider{
		plan: evidencePlan,
		answers: []string{
			`{"type":"tool_call","tool":"web.search","input":{"query":"nexus capabilities"}}`,
			groundedCompletion("src-99"),
		},
	}
	f.provider.queue(envelopeOf(
		"1. Nexus capability boundary | https://docs.example/nexus | docs.example | The runtime decides; the model proposes.",
	))

	out, err := f.rt.Run(context.Background(), workRequest(t, Objective{
		ObjectiveID: "exec-synth-bad", RequestID: "exec-synth-bad",
		Description: "search the web for nexus runtime capabilities",
	}, ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status == "completed" {
		t.Fatal("a synthesis citing an unknown source must not complete the execution")
	}
	if out.ResearchSynthesis != nil {
		t.Fatalf("a rejected synthesis must never be attached: %+v", out.ResearchSynthesis)
	}
}
