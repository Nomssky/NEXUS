package executor

import (
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/execjournal"
)

func synthesisEvidence(ids ...string) *execjournal.ResearchEvidence {
	sources := make([]execjournal.ResearchSource, 0, len(ids))
	for _, id := range ids {
		sources = append(sources, execjournal.ResearchSource{
			ID: id, Title: "Source " + id, URL: "https://example.test/" + id,
			Source: "example.test", Snippet: "captured source",
		})
	}
	return &execjournal.ResearchEvidence{
		Version: execjournal.ResearchEvidenceVersion, Capability: execjournal.ResearchEvidenceCapability,
		Operation: execjournal.ResearchEvidenceOperation, Count: len(sources), Sources: sources,
	}
}

func validSynthesis(ids ...string) *ResearchSynthesis {
	return &ResearchSynthesis{
		Status:  ResearchSynthesisComplete,
		Summary: "A bounded synthesis.",
		EvidenceBacked: []ResearchEvidenceStatement{{
			Statement: "The captured sources support the statement.",
			SourceIDs: ids,
		}},
		Inferences:          []ResearchInference{{Statement: "This is model inference, not evidence."}},
		Conflicts:           []ResearchEvidenceStatement{},
		UnresolvedQuestions: []string{},
	}
}

func TestValidateResearchSynthesisAcceptsGroundedComplete(t *testing.T) {
	if err := ValidateResearchSynthesis(validSynthesis("src-1", "src-2"), synthesisEvidence("src-1", "src-2"), false); err != nil {
		t.Fatalf("valid synthesis rejected: %v", err)
	}
}

func TestValidateResearchSynthesisRejectsBadCitations(t *testing.T) {
	ev := synthesisEvidence("src-1", "src-2")
	cases := []struct {
		name string
		ids  []string
		want string
	}{
		{name: "unknown", ids: []string{"src-9"}, want: "unknown source"},
		{name: "cross execution", ids: []string{"src-3"}, want: "unknown source"},
		{name: "empty", ids: []string{""}, want: "malformed source id"},
		{name: "newline", ids: []string{"src-1\nsrc-2"}, want: "malformed source id"},
		{name: "duplicate", ids: []string{"src-1", "src-1"}, want: "repeats source id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateResearchSynthesis(validSynthesis(tc.ids...), ev, false)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q rejection, got %v", tc.want, err)
			}
		})
	}
}

func TestValidateResearchSynthesisStatusSemantics(t *testing.T) {
	ev := synthesisEvidence("src-1")
	complete := validSynthesis("src-1")
	if err := ValidateResearchSynthesis(complete, ev, true); err == nil ||
		!strings.Contains(err.Error(), "plan steps did not complete") {
		t.Fatalf("complete over a partial plan must fail, got %v", err)
	}

	partial := validSynthesis("src-1")
	partial.Status = ResearchSynthesisPartial
	if err := ValidateResearchSynthesis(partial, ev, true); err != nil {
		t.Fatalf("grounded partial must pass: %v", err)
	}

	barePartial := &ResearchSynthesis{
		Status: ResearchSynthesisPartial, Summary: "No citations.",
		EvidenceBacked: []ResearchEvidenceStatement{}, Inferences: []ResearchInference{},
		Conflicts: []ResearchEvidenceStatement{}, UnresolvedQuestions: []string{},
	}
	if err := ValidateResearchSynthesis(barePartial, ev, true); err == nil {
		t.Fatal("partial without a cited statement or conflict must fail")
	}

	badInconclusive := validSynthesis("src-1")
	badInconclusive.Status = ResearchSynthesisInconclusive
	if err := ValidateResearchSynthesis(badInconclusive, ev, false); err == nil {
		t.Fatal("inconclusive with citations must fail")
	}

	empty := synthesisEvidence()
	inconclusive := &ResearchSynthesis{
		Status: ResearchSynthesisInconclusive, Summary: "No sources were captured.",
		EvidenceBacked: []ResearchEvidenceStatement{}, Inferences: []ResearchInference{},
		Conflicts: []ResearchEvidenceStatement{}, UnresolvedQuestions: []string{"What evidence is missing?"},
	}
	if err := ValidateResearchSynthesis(inconclusive, empty, false); err != nil {
		t.Fatalf("zero-source inconclusive must pass: %v", err)
	}
	completeOnEmpty := validSynthesis("src-1")
	if err := ValidateResearchSynthesis(completeOnEmpty, empty, false); err == nil {
		t.Fatal("complete synthesis over zero sources must fail")
	}
}

func TestValidateResearchSynthesisRequiresV2Evidence(t *testing.T) {
	s := validSynthesis("src-1")
	if err := ValidateResearchSynthesis(s, nil, false); err == nil {
		t.Fatal("nil evidence must fail")
	}
	legacy := &execjournal.ResearchEvidence{
		Version: execjournal.ResearchEvidenceLegacyVersion, Capability: execjournal.ResearchEvidenceCapability,
		Operation: execjournal.ResearchEvidenceOperation, Count: 1,
		Sources: []execjournal.ResearchSource{{Title: "Legacy", URL: "https://legacy.example"}},
	}
	if err := ValidateResearchSynthesis(s, legacy, false); err == nil {
		t.Fatal("v1 evidence must not be upgraded into citations")
	}
}

func TestValidateResearchSynthesisBindsToEvidencePlanRevision(t *testing.T) {
	ev := synthesisEvidence("src-1")
	ev.Plan = &execjournal.ResearchPlanEvidence{
		PlanID: "exec-1", Version: 2, Provenance: execjournal.ResearchProvenanceAuthoritative,
	}
	ev.Steps = []execjournal.ResearchStepEvidence{{
		StepID: "s1", Intent: "gather", PlanVersion: 2, Status: "completed",
		Provenance: execjournal.ResearchProvenanceAuthoritative,
	}}

	matching := validSynthesis("src-1")
	matching.PlanVersion = 2
	if err := ValidateResearchSynthesis(matching, ev, false); err != nil {
		t.Fatalf("matching v3 plan revision rejected: %v", err)
	}

	missing := validSynthesis("src-1")
	if err := ValidateResearchSynthesis(missing, ev, false); err == nil ||
		!strings.Contains(err.Error(), "requires a plan version") {
		t.Fatalf("v3 plan evidence requires a plan revision, got %v", err)
	}

	mismatch := validSynthesis("src-1")
	mismatch.PlanVersion = 1
	if err := ValidateResearchSynthesis(mismatch, ev, false); err == nil ||
		!strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched v3 plan revision must fail, got %v", err)
	}

	v2 := &execjournal.ResearchEvidence{
		Version:    execjournal.ResearchEvidenceProvenanceVersion,
		Capability: execjournal.ResearchEvidenceCapability, Operation: execjournal.ResearchEvidenceOperation,
		Count: 1, Sources: ev.Sources,
		Plan:  &execjournal.ResearchPlanEvidence{PlanID: "exec-1", Version: 2},
		Steps: []execjournal.ResearchStepEvidence{{StepID: "s1", Status: "completed"}},
		Operations: []execjournal.ResearchOperationEvidence{{
			OperationID: "obs-1", StepID: "s1", ToolID: execjournal.ResearchEvidenceCapability,
			Operation: execjournal.ResearchEvidenceOperation, SourceIDs: []string{"src-1"},
		}},
	}
	legacyUnbound := validSynthesis("src-1")
	if err := ValidateResearchSynthesis(legacyUnbound, v2, false); err != nil {
		t.Fatalf("v2 synthesis without a captured revision must stay readable: %v", err)
	}
	legacyMismatch := validSynthesis("src-1")
	legacyMismatch.PlanVersion = 1
	if err := ValidateResearchSynthesis(legacyMismatch, v2, false); err == nil {
		t.Fatal("mismatched v2 plan revision must fail")
	}
}

func TestValidateResearchSynthesisRejectsUnboundedCitations(t *testing.T) {
	ids := make([]string, MaxResearchSynthesisCitationRefs+1)
	evIDs := make([]string, 0, len(ids))
	for i := range ids {
		ids[i] = "src-" + string(rune('a'+i))
		evIDs = append(evIDs, ids[i])
	}
	if err := ValidateResearchSynthesis(validSynthesis(ids...), synthesisEvidence(evIDs...), false); err == nil {
		t.Fatal("too many citations must fail")
	}
}
