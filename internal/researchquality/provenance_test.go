package researchquality

import (
	"testing"

	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/executor"
)

// v2Evidence builds the preserved v59 snapshot shape: plan identity, statuses
// and operations, but none of the v3 intent/objective/completion/authoritative
// markers. The evaluator must report the gap instead of inventing it.
func v2Evidence(executionID string) *execjournal.ResearchEvidence {
	return &execjournal.ResearchEvidence{
		Version:    execjournal.ResearchEvidenceProvenanceVersion,
		Capability: execjournal.ResearchEvidenceCapability,
		Operation:  execjournal.ResearchEvidenceOperation,
		Count:      1,
		Sources: []execjournal.ResearchSource{{
			ID: "src-1", Title: "Nexus runtime capabilities",
			URL: "https://docs.example/nexus", Source: "docs.example",
			Snippet: "Nexus runtime capabilities mediate tools.",
		}},
		Plan: &execjournal.ResearchPlanEvidence{PlanID: executionID, Version: 1},
		Steps: []execjournal.ResearchStepEvidence{{
			StepID: "s1", Status: "completed",
		}},
		Operations: []execjournal.ResearchOperationEvidence{{
			OperationID: "obs-1", StepID: "s1", ToolID: execjournal.ResearchEvidenceCapability,
			Operation: execjournal.ResearchEvidenceOperation, SourceIDs: []string{"src-1"},
		}},
	}
}

func provenanceSynthesis(status string, planVersion int) *executor.ResearchSynthesis {
	s := synthesis(status, []executor.ResearchEvidenceStatement{
		statement("Nexus runtime capabilities mediate tools.", "src-1"),
	}, []string{"Which capability is not covered?"})
	s.PlanVersion = planVersion
	return s
}

func TestEvaluatorUsesDurablePlanFacts(t *testing.T) {
	t.Parallel()
	ev := evidenceV3(1,
		[]execjournal.ResearchSource{source("src-1", "Nexus runtime capabilities",
			"https://docs.example/nexus", "docs.example", "Nexus runtime capabilities mediate tools.")},
		"exec-1",
		[]execjournal.ResearchStepEvidence{step("s1", "completed")},
		[]execjournal.ResearchOperationEvidence{{OperationID: "obs-1", StepID: "s1",
			ToolID: execjournal.ResearchEvidenceCapability, Operation: execjournal.ResearchEvidenceOperation,
			SourceIDs: []string{"src-1"}}},
	)
	// The process-local view says the step failed. The durable snapshot is
	// authoritative, so plan completeness must still pass.
	got := Evaluate(Input{
		ExecutionID: "exec-1",
		Question:    "nexus runtime capabilities",
		Evidence:    ev,
		Synthesis:   provenanceSynthesis(executor.ResearchSynthesisComplete, 1),
		Plan:        []Step{{StepID: "s1", Intent: "nexus runtime capabilities", Status: "failed"}},
	})
	if got == nil {
		t.Fatal("expected an evaluation")
	}
	if got.Status != execjournal.ResearchEvaluationPassed {
		t.Fatalf("durable facts must win: status=%s findings=%+v", got.Status, got.Findings)
	}
	if hasCode(got, findingPlanIncomplete) || hasCode(got, findingPlanStepFailed) {
		t.Fatalf("runtime status must not override the durable snapshot: %+v", got.Findings)
	}
	if dims(got)[execjournal.ResearchDimensionPlanCompleteness] != execjournal.ResearchDimensionPass {
		t.Fatalf("plan completeness = %q", dims(got)[execjournal.ResearchDimensionPlanCompleteness])
	}
	if err := execjournal.ValidateResearchEvaluationForExecution("exec-1", got, ev); err != nil {
		t.Fatalf("evaluation failed durable validation: %v", err)
	}
}

func TestEvaluatorProvenanceFindings(t *testing.T) {
	t.Parallel()
	baseSource := source("src-1", "Nexus runtime capabilities",
		"https://docs.example/nexus", "docs.example", "Nexus runtime capabilities mediate tools.")
	op := execjournal.ResearchOperationEvidence{OperationID: "obs-1", StepID: "s1",
		ToolID: execjournal.ResearchEvidenceCapability, Operation: execjournal.ResearchEvidenceOperation,
		SourceIDs: []string{"src-1"}}

	cases := []struct {
		name      string
		input     Input
		wantCodes []string
		notCodes  []string
	}{
		{
			name: "v2 evidence reports provenance unavailable",
			input: Input{
				ExecutionID: "exec-v2",
				Question:    "nexus runtime capabilities",
				Evidence:    v2Evidence("exec-v2"),
				Synthesis:   provenanceSynthesis(executor.ResearchSynthesisComplete, 1),
				Plan:        goodPlan,
			},
			wantCodes: []string{findingEvidenceProvenanceUnavailable},
		},
		{
			name: "runtime step missing from snapshot",
			input: Input{
				ExecutionID: "exec-missing",
				Question:    "nexus runtime capabilities",
				Evidence: evidenceV3(1, []execjournal.ResearchSource{baseSource}, "exec-missing",
					[]execjournal.ResearchStepEvidence{step("s1", "completed")},
					[]execjournal.ResearchOperationEvidence{op}),
				Synthesis: provenanceSynthesis(executor.ResearchSynthesisPartial, 1),
				Plan: []Step{
					{StepID: "s1", Intent: "nexus runtime capabilities", Status: "completed"},
					{StepID: "s2", Intent: "summarize findings", Status: "completed"},
				},
			},
			wantCodes: []string{findingPlanStepMissing},
		},
		{
			name: "durable step absent from runtime plan",
			input: Input{
				ExecutionID: "exec-not-runtime",
				Question:    "nexus runtime capabilities",
				Evidence: evidenceV3(1, []execjournal.ResearchSource{baseSource}, "exec-not-runtime",
					[]execjournal.ResearchStepEvidence{step("s1", "completed"), step("s2", "completed")},
					[]execjournal.ResearchOperationEvidence{op}),
				Synthesis: provenanceSynthesis(executor.ResearchSynthesisComplete, 1),
				Plan:      []Step{{StepID: "s1", Intent: "nexus runtime capabilities", Status: "completed"}},
			},
			wantCodes: []string{findingPlanStepNotInRuntime},
		},
		{
			name: "unknown dependency in runtime plan",
			input: Input{
				ExecutionID: "exec-unknown",
				Question:    "nexus runtime capabilities",
				Attempted:   true,
				Plan: []Step{{
					StepID: "s1", Intent: "nexus runtime capabilities", Status: "completed",
					Dependencies: []string{"s9"},
				}},
			},
			wantCodes: []string{findingPlanStepUnknown},
		},
		{
			name: "completed step depends on a failed step",
			input: Input{
				ExecutionID: "exec-dep",
				Question:    "nexus runtime capabilities",
				Evidence: evidenceV3(1, []execjournal.ResearchSource{baseSource}, "exec-dep",
					[]execjournal.ResearchStepEvidence{
						step("s1", "failed"),
						func() execjournal.ResearchStepEvidence {
							s := step("s2", "completed")
							s.Dependencies = []string{"s1"}
							return s
						}(),
					},
					[]execjournal.ResearchOperationEvidence{op}),
				Synthesis: provenanceSynthesis(executor.ResearchSynthesisPartial, 1),
			},
			wantCodes: []string{findingPlanDependencyIncomplete},
		},
		{
			name: "completion step differs from runtime step",
			input: Input{
				ExecutionID:   "exec-completion",
				Question:      "nexus runtime capabilities",
				CurrentStepID: "s2",
				Evidence: func() *execjournal.ResearchEvidence {
					ev := evidenceV3(1, []execjournal.ResearchSource{baseSource}, "exec-completion",
						[]execjournal.ResearchStepEvidence{step("s1", "completed"), step("s2", "executing")},
						[]execjournal.ResearchOperationEvidence{op})
					ev.Plan.CompletionStepID = "s1"
					return ev
				}(),
				Synthesis: provenanceSynthesis(executor.ResearchSynthesisPartial, 1),
			},
			wantCodes: []string{findingPlanCompletionMismatch},
		},
		{
			name: "v3 source-only evidence reports attribution unavailable",
			input: Input{
				ExecutionID: "exec-source-only",
				Question:    "nexus runtime capabilities",
				Evidence:    evidenceV3(1, []execjournal.ResearchSource{baseSource}, "", nil, nil),
				Synthesis:   provenanceSynthesis(executor.ResearchSynthesisComplete, 0),
				Plan:        goodPlan,
			},
			wantCodes: []string{findingSourceAttributionUnavailable},
		},
		{
			name: "v3 plan without operations reports attribution unavailable",
			input: Input{
				ExecutionID: "exec-no-ops",
				Question:    "nexus runtime capabilities",
				Evidence:    evidenceV3(1, []execjournal.ResearchSource{baseSource}, "exec-no-ops", nil, nil),
				Synthesis:   provenanceSynthesis(executor.ResearchSynthesisComplete, 1),
				Plan:        goodPlan,
			},
			wantCodes: []string{findingSourceAttributionUnavailable},
		},
		{
			name: "synthesis plan revision mismatch",
			input: Input{
				ExecutionID: "exec-mismatch",
				Question:    "nexus runtime capabilities",
				Evidence: func() *execjournal.ResearchEvidence {
					ev := evidenceV3(1, []execjournal.ResearchSource{baseSource}, "exec-mismatch",
						[]execjournal.ResearchStepEvidence{step("s1", "completed")},
						[]execjournal.ResearchOperationEvidence{op})
					ev.Plan.Version = 2
					ev.Steps[0].PlanVersion = 2
					return ev
				}(),
				Synthesis: provenanceSynthesis(executor.ResearchSynthesisComplete, 1),
			},
			wantCodes: []string{findingSynthesisPlanVersionMismatch},
		},
		{
			name: "v3 synthesis lacks plan revision",
			input: Input{
				ExecutionID: "exec-unavailable",
				Question:    "nexus runtime capabilities",
				Evidence: evidenceV3(1, []execjournal.ResearchSource{baseSource}, "exec-unavailable",
					[]execjournal.ResearchStepEvidence{step("s1", "completed")},
					[]execjournal.ResearchOperationEvidence{op}),
				Synthesis: provenanceSynthesis(executor.ResearchSynthesisComplete, 0),
			},
			wantCodes: []string{findingSynthesisPlanVersionUnavailable},
		},
		{
			name: "v2 synthesis without revision stays readable",
			input: Input{
				ExecutionID: "exec-v2-unbound",
				Question:    "nexus runtime capabilities",
				Evidence:    v2Evidence("exec-v2-unbound"),
				Synthesis:   provenanceSynthesis(executor.ResearchSynthesisComplete, 0),
				Plan:        goodPlan,
			},
			wantCodes: []string{findingSynthesisPlanVersionUnavailable, findingEvidenceProvenanceUnavailable},
			notCodes:  []string{findingSynthesisPlanVersionMismatch},
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
			for _, code := range tc.wantCodes {
				if !hasCode(got, code) {
					t.Errorf("missing finding %q in %+v", code, got.Findings)
				}
			}
			for _, code := range tc.notCodes {
				if hasCode(got, code) {
					t.Errorf("unexpected finding %q in %+v", code, got.Findings)
				}
			}
			if err := execjournal.ValidateResearchEvaluationForExecution(tc.input.ExecutionID, got, tc.input.Evidence); err != nil {
				t.Fatalf("evaluation failed durable validation: %v", err)
			}
		})
	}
}
