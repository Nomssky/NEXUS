package agentintel

import (
	"context"
	"testing"
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
			`{"type":"complete","result":"done"}`,
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
}
