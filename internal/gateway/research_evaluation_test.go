package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/execjournal"
)

func recoverySampleEvaluation(executionID string, evidence *execjournal.ResearchEvidence) *execjournal.ResearchEvaluation {
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

// The durable detail read exposes the persisted quality verdict next to the
// evidence it evaluated, and the verdict itself never carries source content.
func TestRecoveryDetailExposesResearchEvaluation(t *testing.T) {
	dir := t.TempDir()
	srv, e, _ := newEvidenceServer(t, dir)
	j := e.ExecutionJournal()
	evidence := recoverySampleEvidence()
	evaluation := recoverySampleEvaluation("exec-evi", evidence)
	if err := j.Accepted("exec-evi", recBiz, "", "user-wide"); err != nil {
		t.Fatalf("seed accepted: %v", err)
	}
	if err := j.TerminalWithResearch("exec-evi", execjournal.StateCompleted, "", evidence, evaluation, nil); err != nil {
		t.Fatalf("seed terminal: %v", err)
	}

	w := evidenceGet(t, srv, "/api/v1/recovery/executions/exec-evi?business_id="+recBiz, wideHeaders())
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var v map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	raw, ok := v["research_evaluation"].(map[string]interface{})
	if !ok {
		t.Fatalf("evaluation missing from detail read: %v", v)
	}
	if raw["status"] != execjournal.ResearchEvaluationWarnings {
		t.Fatalf("evaluation status = %v", raw["status"])
	}
	if raw["execution_id"] != "exec-evi" {
		t.Fatalf("evaluation is not bound to the execution: %v", raw["execution_id"])
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"The runtime decides", "docs.example/nexus", "Nexus capability boundary"} {
		if strings.Contains(string(encoded), leaked) {
			t.Fatalf("evaluation leaked source content %q", leaked)
		}
	}
}

// The list stays bounded: the verdict is a detail-read field, like the source
// snapshot it describes.
func TestRecoveryListOmitsResearchEvaluation(t *testing.T) {
	dir := t.TempDir()
	srv, e, _ := newEvidenceServer(t, dir)
	j := e.ExecutionJournal()
	evidence := recoverySampleEvidence()
	evaluation := recoverySampleEvaluation("exec-evi", evidence)
	if err := j.Accepted("exec-evi", recBiz, "", "user-wide"); err != nil {
		t.Fatalf("seed accepted: %v", err)
	}
	if err := j.TerminalWithResearch("exec-evi", execjournal.StateCompleted, "", evidence, evaluation, nil); err != nil {
		t.Fatalf("seed terminal: %v", err)
	}
	w := evidenceGet(t, srv, "/api/v1/recovery/executions?business_id="+recBiz, wideHeaders())
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body struct {
		Executions []map[string]interface{} `json:"executions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Executions) == 0 {
		t.Fatalf("empty list: %s", w.Body.String())
	}
	for _, row := range body.Executions {
		if _, ok := row["research_evaluation"]; ok {
			t.Fatalf("list must not expose the evaluation, got %v", row["research_evaluation"])
		}
	}
}
