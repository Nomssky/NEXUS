package researchquality

import (
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/executor"
)

func source(id, title, url, sourceName, snippet string) execjournal.ResearchSource {
	return execjournal.ResearchSource{
		ID:      id,
		Title:   title,
		URL:     url,
		Source:  sourceName,
		Snippet: snippet,
	}
}

func evidenceV3(count int, sources []execjournal.ResearchSource, planID string, steps []execjournal.ResearchStepEvidence, ops []execjournal.ResearchOperationEvidence) *execjournal.ResearchEvidence {
	ev := &execjournal.ResearchEvidence{
		Version:     execjournal.ResearchEvidenceVersion,
		Capability:  execjournal.ResearchEvidenceCapability,
		Operation:   execjournal.ResearchEvidenceOperation,
		Count:       count,
		Sources:     sources,
		Attribution: execjournal.ResearchAttributionUnavailable,
	}
	if planID != "" || len(steps) > 0 || len(ops) > 0 {
		if planID == "" {
			planID = "exec-stub"
		}
		if len(steps) == 0 {
			steps = []execjournal.ResearchStepEvidence{step("s1", "completed")}
		}
		for i := range steps {
			if steps[i].Intent == "" {
				steps[i].Intent = "research step " + steps[i].StepID
			}
			if steps[i].PlanVersion == 0 {
				steps[i].PlanVersion = 1
			}
			steps[i].Provenance = execjournal.ResearchProvenanceAuthoritative
		}
		for i := range ops {
			ops[i].Provenance = execjournal.ResearchProvenanceAuthoritative
		}
		ev.Plan = &execjournal.ResearchPlanEvidence{
			PlanID: planID, Version: 1, Provenance: execjournal.ResearchProvenanceAuthoritative,
		}
		ev.Steps = steps
		ev.Operations = ops
		if len(ops) > 0 {
			ev.Attribution = execjournal.ResearchAttributionDerived
		}
	}
	return ev
}

func synthesis(status string, stmts []executor.ResearchEvidenceStatement, questions []string) *executor.ResearchSynthesis {
	return &executor.ResearchSynthesis{
		Status:              status,
		Summary:             "objective pursued deterministically",
		EvidenceBacked:      stmts,
		UnresolvedQuestions: questions,
		PlanVersion:         1,
	}
}

func statement(text string, refs ...string) executor.ResearchEvidenceStatement {
	return executor.ResearchEvidenceStatement{Statement: text, SourceIDs: refs}
}

func step(stepID, status string) execjournal.ResearchStepEvidence {
	return execjournal.ResearchStepEvidence{
		StepID: stepID, Intent: "research step " + stepID, PlanVersion: 1, Status: status,
		Provenance: execjournal.ResearchProvenanceAuthoritative,
	}
}

func dims(evaluation *execjournal.ResearchEvaluation) map[string]string {
	m := make(map[string]string, len(evaluation.Dimensions))
	for _, d := range evaluation.Dimensions {
		m[d.ID] = d.Status
	}
	return m
}

func hasCode(evaluation *execjournal.ResearchEvaluation, code string) bool {
	for _, f := range evaluation.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

var goodPlan = []Step{{
	StepID:   "s1",
	Intent:   "nexus runtime capabilities",
	Status:   "completed",
	Optional: false,
}}

var incompletePlan = []Step{{
	StepID:   "s1",
	Intent:   "nexus runtime capabilities",
	Status:   "completed",
	Optional: false,
}, {
	StepID:   "s2",
	Intent:   "verify limits",
	Status:   "failed",
	Optional: false,
}}

func TestEvaluationCorpus(t *testing.T) {
	t.Parallel()
	oneSource := func(id, url, title, snippet string) []execjournal.ResearchSource {
		return []execjournal.ResearchSource{source(id, title, url, "docs.example", snippet)}
	}
	cases := []struct {
		name       string
		input      Input
		wantStatus string
		wantCodes  []string
		wantDims   map[string]string
	}{
		{
			name: "strongly supported",
			input: Input{ExecutionID: "exec-1", Question: "nexus runtime capabilities", Evidence: evidenceV3(2,
				[]execjournal.ResearchSource{
					source("src-1", "Nexus capability boundary", "https://docs.example/nexus", "docs.example", "Nexus runtime capabilities mediate tools."),
					source("src-2", "Nexus source two", "https://example.org/nexus", "example.org", "Nexus runtime mediates every capability call."),
				}, "exec-1",
				[]execjournal.ResearchStepEvidence{step("s1", "completed")},
				[]execjournal.ResearchOperationEvidence{{OperationID: "obs-1", StepID: "s1", ToolID: execjournal.ResearchEvidenceCapability, Operation: execjournal.ResearchEvidenceOperation, SourceIDs: []string{"src-1", "src-2"}}},
			), Synthesis: synthesis("complete",
				[]executor.ResearchEvidenceStatement{statement("one", "src-1"), statement("two", "src-2")}, nil),
				Plan: goodPlan},
			wantStatus: execjournal.ResearchEvaluationPassed,
			wantDims: map[string]string{
				execjournal.ResearchDimensionEvidenceCoverage:  execjournal.ResearchDimensionPass,
				execjournal.ResearchDimensionCitationIntegrity: execjournal.ResearchDimensionPass,
				execjournal.ResearchDimensionRelevance:         execjournal.ResearchDimensionPass,
				execjournal.ResearchDimensionSourceQuality:     execjournal.ResearchDimensionPass,
				execjournal.ResearchDimensionSourceDiversity:   execjournal.ResearchDimensionPass,
				execjournal.ResearchDimensionConflictHandling:  execjournal.ResearchDimensionNotMeasurable,
				execjournal.ResearchDimensionUncertainty:       execjournal.ResearchDimensionPass,
				execjournal.ResearchDimensionPlanCompleteness:  execjournal.ResearchDimensionPass,
			},
		},
		{
			name: "zero sources inconclusive",
			input: Input{ExecutionID: "exec-2", Question: "nexus runtime capabilities",
				Evidence:  evidenceV3(0, nil, "exec-2", nil, nil),
				Synthesis: synthesis("inconclusive", nil, []string{"why no sources"}),
				Plan:      goodPlan},
			wantStatus: execjournal.ResearchEvaluationWarnings,
			wantCodes:  []string{findingNoSearchResults},
			wantDims: map[string]string{
				execjournal.ResearchDimensionEvidenceCoverage:  execjournal.ResearchDimensionNotApplicable,
				execjournal.ResearchDimensionCitationIntegrity: execjournal.ResearchDimensionPass,
				execjournal.ResearchDimensionRelevance:         execjournal.ResearchDimensionNotApplicable,
				execjournal.ResearchDimensionSourceQuality:     execjournal.ResearchDimensionWarn,
				execjournal.ResearchDimensionSourceDiversity:   execjournal.ResearchDimensionNotApplicable,
				execjournal.ResearchDimensionConflictHandling:  execjournal.ResearchDimensionNotMeasurable,
				execjournal.ResearchDimensionUncertainty:       execjournal.ResearchDimensionPass,
				execjournal.ResearchDimensionPlanCompleteness:  execjournal.ResearchDimensionPass,
			},
		},
		{
			name:       "evidence unavailable for attempted research",
			input:      Input{ExecutionID: "exec-3", Question: "nexus runtime capabilities", Attempted: true},
			wantStatus: execjournal.ResearchEvaluationIncomplete,
			wantCodes:  []string{findingEvidenceUnavailable},
		},
		{
			name: "invalid url and low information",
			input: Input{ExecutionID: "exec-4", Question: "nexus runtime capabilities",
				Evidence:  evidenceV3(1, oneSource("src-1", "not a url", "", ""), "exec-4", nil, nil),
				Synthesis: synthesis("complete", []executor.ResearchEvidenceStatement{statement("one", "src-1")}, nil),
				Plan:      goodPlan},
			wantStatus: execjournal.ResearchEvaluationWarnings,
			wantCodes:  []string{findingSourceInvalidURL, findingSourceLowInformation},
		},
		{
			name: "duplicate url only evidence",
			input: Input{ExecutionID: "exec-5", Question: "nexus runtime capabilities",
				Evidence: evidenceV3(2, []execjournal.ResearchSource{
					source("src-1", "Same page", "https://docs.example/nexus", "docs.example", "Different title."),
					source("src-2", "Same page again", "HTTPS://DOCS.EXAMPLE/nexus", "docs.example", "Different excerpt."),
				}, "exec-5", nil, nil),
				Synthesis: synthesis("complete", []executor.ResearchEvidenceStatement{statement("one", "src-1"), statement("two", "src-2")}, nil),
				Plan:      goodPlan},
			wantStatus: execjournal.ResearchEvaluationWarnings,
			wantCodes:  []string{findingSourceDuplicateURL, findingDuplicateOnlyEvidence},
		},
		{
			name: "evidence belongs to another execution",
			input: Input{ExecutionID: "exec-B", Question: "nexus runtime capabilities",
				Evidence: evidenceV3(1,
					oneSource("src-1", "https://docs.example/nexus", "Nexus capability boundary", "Nexus runtime capabilities mediate tools."),
					"exec-A", nil, nil),
				Synthesis: synthesis("complete", []executor.ResearchEvidenceStatement{statement("one", "src-1")}, nil),
				Plan:      goodPlan},
			wantStatus: execjournal.ResearchEvaluationFailed,
			wantCodes:  []string{findingEvidenceExecutionMismatch},
		},
		{
			name: "plan step failed partial synthesis",
			input: Input{ExecutionID: "exec-6", Question: "nexus runtime capabilities",
				Evidence: evidenceV3(1,
					oneSource("src-1", "https://docs.example/nexus", "Nexus capability boundary", "Nexus runtime capabilities mediate tools."),
					"exec-6",
					[]execjournal.ResearchStepEvidence{step("s1", "completed"), step("s2", "failed")}, nil),
				Synthesis: synthesis("partial", []executor.ResearchEvidenceStatement{statement("one", "src-1")}, []string{"why s2 failed"}),
				Plan:      incompletePlan},
			wantStatus: execjournal.ResearchEvaluationWarnings,
			wantCodes:  []string{findingPlanStepFailed},
			wantDims: map[string]string{
				execjournal.ResearchDimensionPlanCompleteness: execjournal.ResearchDimensionWarn,
			},
		},
		{
			name: "citation cites source absent from this execution",
			input: Input{ExecutionID: "exec-7", Question: "nexus runtime capabilities",
				Evidence: evidenceV3(1,
					oneSource("src-1", "https://docs.example/nexus", "Nexus capability boundary", "Nexus runtime capabilities mediate tools."),
					"exec-7", nil, nil),
				Synthesis: synthesis("complete", []executor.ResearchEvidenceStatement{statement("one", "src-9")}, nil),
				Plan:      goodPlan},
			wantStatus: execjournal.ResearchEvaluationFailed,
			wantCodes:  []string{findingCitationUnknown},
		},
		{
			name: "legacy evidence without provenance",
			input: Input{ExecutionID: "exec-8", Question: "nexus runtime capabilities", Attempted: true,
				Evidence: &execjournal.ResearchEvidence{
					Version: execjournal.ResearchEvidenceLegacyVersion, Capability: execjournal.ResearchEvidenceCapability,
					Operation: execjournal.ResearchEvidenceOperation, Count: 1,
					Sources: []execjournal.ResearchSource{source("", "Nexus capability boundary", "https://docs.example/nexus", "docs.example", "Nexus runtime capabilities mediate tools.")},
				}},
			wantStatus: execjournal.ResearchEvaluationIncomplete,
			wantCodes:  []string{findingLegacyEvidence},
		},
		{
			name: "materially conflicting excerpts undeclared",
			input: Input{ExecutionID: "exec-9", Question: "nexus runtime capabilities",
				Evidence: evidenceV3(2, []execjournal.ResearchSource{
					source("src-1", "Nexus capability boundary", "https://docs.example/nexus", "docs.example", "Nexus runtime capabilities mediate tools."),
					source("src-2", "Nexus cannot call tools", "https://example.org/tools", "example.org", "Nexus runtime never mediates calls."),
				}, "exec-9", nil, nil),
				Synthesis: synthesis("complete", []executor.ResearchEvidenceStatement{statement("one", "src-1"), statement("two", "src-2")}, nil),
				Plan:      goodPlan},
			wantStatus: execjournal.ResearchEvaluationWarnings,
			wantCodes:  []string{findingSourceUnattributed},
			wantDims: map[string]string{
				execjournal.ResearchDimensionConflictHandling: execjournal.ResearchDimensionNotMeasurable,
			},
		},
		{
			name: "duplicate source ids in one statement",
			input: Input{ExecutionID: "exec-10", Question: "nexus runtime capabilities",
				Evidence: evidenceV3(1,
					oneSource("src-1", "https://docs.example/nexus", "Nexus capability boundary", "Nexus runtime capabilities mediate tools."),
					"exec-10", nil, nil),
				Synthesis: synthesis("complete", []executor.ResearchEvidenceStatement{statement("one", "src-1", "src-1")}, nil),
				Plan:      goodPlan},
			wantStatus: execjournal.ResearchEvaluationFailed,
			wantCodes:  []string{findingCitationDuplicate},
		},
		{
			name: "complete synthesis over incomplete plan",
			input: Input{ExecutionID: "exec-11", Question: "nexus runtime capabilities",
				Evidence: evidenceV3(1,
					oneSource("src-1", "https://docs.example/nexus", "Nexus capability boundary", "Nexus runtime capabilities mediate tools."),
					"exec-11",
					[]execjournal.ResearchStepEvidence{step("s1", "completed"), step("s2", "failed")}, nil),
				Synthesis: synthesis("complete", []executor.ResearchEvidenceStatement{statement("one", "src-1")}, nil),
				Plan:      incompletePlan},
			wantStatus: execjournal.ResearchEvaluationFailed,
			wantCodes:  []string{findingSynthesisCompletePlan},
		},
		{
			name: "partial synthesis without unresolved questions",
			input: Input{ExecutionID: "exec-12", Question: "nexus runtime capabilities",
				Evidence: evidenceV3(1,
					oneSource("src-1", "https://docs.example/nexus", "Nexus capability boundary", "Nexus runtime capabilities mediate tools."),
					"exec-12", nil, nil),
				Synthesis: synthesis("partial", []executor.ResearchEvidenceStatement{statement("one", "src-1")}, nil),
				Plan:      goodPlan},
			wantStatus: execjournal.ResearchEvaluationWarnings,
			wantCodes:  []string{findingUnresolvedQuestions},
		},
		{
			name: "statement without citations",
			input: Input{ExecutionID: "exec-13", Question: "nexus runtime capabilities",
				Evidence: evidenceV3(1,
					oneSource("src-1", "https://docs.example/nexus", "Nexus capability boundary", "Nexus runtime capabilities mediate tools."),
					"exec-13", nil, nil),
				Synthesis: synthesis("complete", []executor.ResearchEvidenceStatement{statement("one")}, nil),
				Plan:      goodPlan},
			wantStatus: execjournal.ResearchEvaluationFailed,
			wantCodes:  []string{findingStatementNoCitations},
		},
		{
			name: "content duplicate across hosts",
			input: Input{ExecutionID: "exec-14", Question: "nexus runtime capabilities",
				Evidence: evidenceV3(2, []execjournal.ResearchSource{
					source("src-1", "Same excerpt", "https://docs.example/nexus", "docs.example", "Nexus runtime capabilities mediate tools."),
					source("src-2", "Same excerpt", "https://example.org/nexus", "example.org", "Nexus runtime capabilities mediate tools."),
				}, "exec-14", nil, nil),
				Synthesis: synthesis("complete", []executor.ResearchEvidenceStatement{statement("one", "src-1"), statement("two", "src-2")}, nil),
				Plan:      goodPlan},
			wantStatus: execjournal.ResearchEvaluationWarnings,
			wantCodes:  []string{findingSourceDuplicateContent},
		},
		{
			name: "synthesis malformed empty summary",
			input: Input{ExecutionID: "exec-15", Question: "nexus runtime capabilities",
				Evidence: evidenceV3(1,
					oneSource("src-1", "https://docs.example/nexus", "Nexus capability boundary", "Nexus runtime capabilities mediate tools."),
					"exec-15", nil, nil),
				Synthesis: &executor.ResearchSynthesis{Status: "complete", Summary: "",
					EvidenceBacked: []executor.ResearchEvidenceStatement{statement("one", "src-1")}},
				Plan: goodPlan},
			wantStatus: execjournal.ResearchEvaluationFailed,
			wantCodes:  []string{findingSynthesisInvalid},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Evaluate(tc.input)
			if got == nil {
				t.Fatal("expected an evaluation")
			}
			if got.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q (findings: %+v)", got.Status, tc.wantStatus, got.Findings)
			}
			for _, code := range tc.wantCodes {
				if !hasCode(got, code) {
					t.Errorf("missing finding %q in %+v", code, got.Findings)
				}
			}
			for id, want := range tc.wantDims {
				if dims(got)[id] != want {
					t.Errorf("dimension %s = %q, want %q", id, dims(got)[id], want)
				}
			}
			if err := execjournal.ValidateResearchEvaluationForExecution(tc.input.ExecutionID, got, tc.input.Evidence); err != nil {
				t.Fatalf("evaluation failed durable validation: %v", err)
			}
		})
	}
}

func TestEvaluateReturnsNilWithoutResearch(t *testing.T) {
	t.Parallel()
	if got := Evaluate(Input{ExecutionID: "exec-empty"}); got != nil {
		t.Fatalf("expected no evaluation for a non-research execution, got %+v", got)
	}
}

func TestEvaluationDoesNotLeakSourceText(t *testing.T) {
	t.Parallel()
	got := Evaluate(Input{
		ExecutionID: "exec-secret",
		Question:    "SECRET-QUERY-TOKEN",
		Evidence: evidenceV3(1,
			[]execjournal.ResearchSource{source("src-1", "Secret title", "https://docs.example/secret?token=SECRET-URL", "docs.example", "SECRET-SNIPPET")},
			"exec-secret", nil, nil),
		Synthesis: synthesis("complete", []executor.ResearchEvidenceStatement{statement("one", "src-1")}, nil),
		Plan:      goodPlan,
	})
	if got == nil {
		t.Fatal("expected an evaluation")
	}
	raw, err := got.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, secret := range []string{"SECRET-QUERY-TOKEN", "SECRET-URL", "SECRET-SNIPPET", "Secret title"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("evaluation leaked %q", secret)
		}
	}
}
