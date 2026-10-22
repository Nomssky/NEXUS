package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/executor"
	"github.com/Nomssky/NEXUS/internal/foundation/agent"
	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

func coreSampleEvaluation(executionID string, evidence *execjournal.ResearchEvidence) *execjournal.ResearchEvaluation {
	return &execjournal.ResearchEvaluation{
		Version:             execjournal.ResearchEvaluationVersion,
		Method:              execjournal.ResearchEvaluationMethod,
		ExecutionID:         executionID,
		EvidenceVersion:     evidence.Version,
		EvidenceCount:       evidence.Count,
		EvidenceFingerprint: execjournal.ResearchEvidenceFingerprint(evidence),
		Status:              execjournal.ResearchEvaluationWarnings,
		Dimensions: []execjournal.ResearchEvaluationDimension{
			{ID: execjournal.ResearchDimensionEvidenceCoverage, Status: execjournal.ResearchDimensionPass},
			{ID: execjournal.ResearchDimensionCitationIntegrity, Status: execjournal.ResearchDimensionPass},
			{ID: execjournal.ResearchDimensionRelevance, Status: execjournal.ResearchDimensionWarn},
			{ID: execjournal.ResearchDimensionSourceQuality, Status: execjournal.ResearchDimensionPass},
			{ID: execjournal.ResearchDimensionSourceDiversity, Status: execjournal.ResearchDimensionPass},
			{ID: execjournal.ResearchDimensionConflictHandling, Status: execjournal.ResearchDimensionNotMeasurable},
			{ID: execjournal.ResearchDimensionUncertainty, Status: execjournal.ResearchDimensionPass},
			{ID: execjournal.ResearchDimensionPlanCompleteness, Status: execjournal.ResearchDimensionPass},
		},
		Findings: []execjournal.ResearchEvaluationFinding{{
			Code: "source_no_query_overlap", Dimension: execjournal.ResearchDimensionRelevance,
			Severity: execjournal.ResearchFindingWarning,
			Message:  "Source text has no meaningful lexical overlap with the objective.",
			SourceID: "src-1",
		}},
		Limitations: []string{"Citation integrity proves reference resolution only."},
	}
}

// The deterministic evaluation is written in the same atomic update as the
// evidence it describes and survives a restart with its binding intact.
func TestEnginePersistsResearchEvaluationWithTerminalState(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	j := execjournal.New(st, nil)
	e, err := NewEngine(nil, WithExecutionJournal(j))
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	ctx := context.Background()
	if err := e.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = e.Stop(ctx) })

	evidence := coreSampleEvidence("Nexus capability boundary")
	evaluation := coreSampleEvaluation("exec-core-evaluation", evidence)
	if err := e.SubmitRequest(&Request{
		ID: "exec-core-evaluation", Context: NewRequestContext("exec-core-evaluation", "biz-1", "actor-1"),
		Intent: "research", Handler: func(ctx context.Context, req *executor.WorkRequest, ag *agent.Agent) (*executor.Outcome, error) {
			return &executor.Outcome{
				Status: "completed", Output: "done",
				ResearchEvidence: evidence, ResearchEvaluation: evaluation,
			}, nil
		},
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	res := awaitResult(t, e, "exec-core-evaluation")
	if res.Outcome == nil || res.Outcome.ResearchEvaluation == nil {
		t.Fatalf("live response must carry the evaluation: %+v", res.Outcome)
	}
	rec := waitForJournalState(t, j, "exec-core-evaluation", execjournal.StateCompleted)
	if rec.ResearchEvaluation == nil {
		t.Fatal("terminal record must carry the evaluation")
	}
	if rec.ResearchEvaluation.EvidenceFingerprint != execjournal.ResearchEvidenceFingerprint(rec.ResearchEvidence) {
		t.Fatal("persisted evaluation must stay bound to the persisted evidence")
	}
	raw, err := json.Marshal(rec.ResearchEvaluation)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "The runtime decides") || strings.Contains(string(raw), "docs.example") {
		t.Fatalf("evaluation must not carry source content: %s", raw)
	}

	if err := e.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
	st2, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	j2 := execjournal.New(st2, nil)
	if _, err := j2.Recover(); err != nil {
		t.Fatalf("recover: %v", err)
	}
	rec2, ok, err := j2.Get("exec-core-evaluation")
	if err != nil || !ok {
		t.Fatalf("reopened get: ok=%v err=%v", ok, err)
	}
	if rec2.ResearchEvaluation == nil || rec2.ResearchEvaluation.Status != execjournal.ResearchEvaluationWarnings {
		t.Fatalf("evaluation did not survive restart: %+v", rec2.ResearchEvaluation)
	}
}
