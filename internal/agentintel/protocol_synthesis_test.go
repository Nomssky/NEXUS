package agentintel

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/execjournal"
)

func TestParseActionTypedSynthesis(t *testing.T) {
	raw := `{"type":"complete","result":"done","synthesis":{` +
		`"status":"complete","summary":"grounded",` +
		`"evidence_backed":[{"statement":"claim","source_ids":["src-1"]}],` +
		`"inferences":[],"conflicts":[],"unresolved_questions":[]}}`
	action, err := ParseAction(raw)
	if err != nil {
		t.Fatal(err)
	}
	if action.Synthesis == nil || action.Synthesis.Status != "complete" ||
		len(action.Synthesis.EvidenceBacked) != 1 ||
		action.Synthesis.EvidenceBacked[0].SourceIDs[0] != "src-1" {
		t.Fatalf("typed synthesis not decoded: %+v", action.Synthesis)
	}
}

func TestSynthesisCannotSmuggleSourceMetadata(t *testing.T) {
	raw := `{"type":"complete","synthesis":{` +
		`"status":"complete","summary":"grounded","title":"Fake source","url":"https://evil.example",` +
		`"evidence_backed":[],"inferences":[],"conflicts":[],"unresolved_questions":[]}}`
	if _, err := ParseAction(raw); err == nil {
		t.Fatal("unknown synthesis metadata fields must fail closed")
	}
}

func TestDuplicateKeysAreRejectedRecursively(t *testing.T) {
	cases := map[string]string{
		"nested synthesis": `{"type":"complete","synthesis":{"status":"complete","status":"partial",` +
			`"summary":"x","evidence_backed":[],"inferences":[],"conflicts":[],"unresolved_questions":[]}}`,
		"nested array item": `{"type":"complete","synthesis":{"status":"complete","summary":"x",` +
			`"evidence_backed":[{"statement":"a","source_ids":["src-1"],"source_ids":["src-2"]}],` +
			`"inferences":[],"conflicts":[],"unresolved_questions":[]}}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseAction(raw); err == nil || !strings.Contains(err.Error(), "duplicate field") {
				t.Fatalf("duplicate key must fail, got %v", err)
			}
		})
	}
}

func TestPlanSchemaDeclaresAllowedOperations(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(PlanJSONSchema(), &schema); err != nil {
		t.Fatalf("plan schema is not valid JSON: %v", err)
	}
	raw := string(PlanJSONSchema())
	if !strings.Contains(raw, `"allowed_operations"`) || !strings.Contains(raw, `"operation"`) {
		t.Fatalf("plan schema must declare allowed_operations: %s", raw)
	}
	plan, err := ParsePlan(`{"steps":[{"step_id":"s1","intent":"search","allowed_operations":[{"tool":"web.search","operation":"search"}]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan[0].AllowedOperations) != 1 || plan[0].AllowedOperations[0].Operation != "search" {
		t.Fatalf("allowed_operations not decoded: %+v", plan[0])
	}
}

func TestDecisionEnvelopeRendersValidatedEvidenceIdsOnly(t *testing.T) {
	ev := &execjournal.ResearchEvidence{
		Version: execjournal.ResearchEvidenceVersion, Capability: execjournal.ResearchEvidenceCapability,
		Operation: execjournal.ResearchEvidenceOperation, Count: 1,
		Sources: []execjournal.ResearchSource{{
			ID: "src-1", Title: "Authoritative title", URL: "https://docs.example/nexus",
			Source: "docs.example", Snippet: "captured snippet",
		}},
		Operations: []execjournal.ResearchOperationEvidence{{
			OperationID: "obs-1", StepID: "s1", ToolID: "web.search", Operation: "search",
			SourceIDs: []string{"src-1"},
		}},
	}
	msgs := buildDecisionEnvelope(DecisionPrompt{StepID: "s2", Evidence: ev})
	content := msgs[1].Content
	for _, want := range []string{
		"version=3", "source_id=src-1", "step=s1", "Authoritative title",
		"https://docs.example/nexus", "captured snippet",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("evidence prompt missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "src-2") {
		t.Fatalf("evidence prompt must not invent source ids:\n%s", content)
	}
}

func TestDecisionPromptAllowsTypedSynthesisForStableIDLegacyProvenance(t *testing.T) {
	ev := &execjournal.ResearchEvidence{
		Version:    execjournal.ResearchEvidenceProvenanceVersion,
		Capability: execjournal.ResearchEvidenceCapability, Operation: execjournal.ResearchEvidenceOperation,
		Count: 1, Plan: &execjournal.ResearchPlanEvidence{PlanID: "exec-1", Version: 1},
		Sources: []execjournal.ResearchSource{{
			ID: "src-1", Title: "Legacy stable source", URL: "https://docs.example/legacy",
			Source: "docs.example", Snippet: "captured",
		}},
	}
	content := buildDecisionEnvelope(DecisionPrompt{StepID: "s2", Evidence: ev})[1].Content
	if strings.Contains(content, "do not return a typed synthesis") {
		t.Fatalf("v2 stable-id evidence must remain synthesizable:\n%s", content)
	}
	if !strings.Contains(content, "source_id=src-1") {
		t.Fatalf("v2 evidence prompt must expose stable source ids:\n%s", content)
	}

	ev.Version = execjournal.ResearchEvidenceLegacyVersion
	ev.Plan = nil
	content = buildDecisionEnvelope(DecisionPrompt{StepID: "s2", Evidence: ev})[1].Content
	if !strings.Contains(content, "do not return a typed synthesis") {
		t.Fatalf("provenance-free v1 evidence must block typed synthesis:\n%s", content)
	}
}

func TestDecisionSchemaDeclaresTypedSynthesis(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(DecisionJSONSchema(), &schema); err != nil {
		t.Fatalf("decision schema is not valid JSON: %v", err)
	}
	raw := string(DecisionJSONSchema())
	for _, want := range []string{`"synthesis"`, `"evidence_backed"`, `"unresolved_questions"`, `"inconclusive"`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("decision schema missing %q: %s", want, raw)
		}
	}
}
