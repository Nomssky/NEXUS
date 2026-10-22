package execjournal

import (
	"strings"
	"sync"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

func evaluationEvidence(executionID string) *ResearchEvidence {
	return &ResearchEvidence{
		Version:    ResearchEvidenceVersion,
		Capability: ResearchEvidenceCapability,
		Operation:  ResearchEvidenceOperation,
		Count:      1,
		Sources: []ResearchSource{{
			ID: "src-1", Title: "Nexus capability boundary",
			URL: "https://docs.example/nexus", Source: "docs.example",
			Snippet: "The runtime decides; the model proposes.",
		}},
		Plan:  &ResearchPlanEvidence{PlanID: executionID, Version: 1},
		Steps: []ResearchStepEvidence{{StepID: "s1", Status: "completed"}},
		Operations: []ResearchOperationEvidence{{
			OperationID: "obs-1", StepID: "s1", ToolID: ResearchEvidenceCapability,
			Operation: ResearchEvidenceOperation, SourceIDs: []string{"src-1"},
		}},
	}
}

func evaluationFor(executionID string, evidence *ResearchEvidence) *ResearchEvaluation {
	return &ResearchEvaluation{
		Version:             ResearchEvaluationVersion,
		Method:              ResearchEvaluationMethod,
		ExecutionID:         executionID,
		EvidenceVersion:     evidence.Version,
		EvidenceCount:       evidence.Count,
		EvidenceFingerprint: ResearchEvidenceFingerprint(evidence),
		Status:              ResearchEvaluationPassed,
		Dimensions: []ResearchEvaluationDimension{
			{ID: ResearchDimensionEvidenceCoverage, Status: ResearchDimensionPass},
			{ID: ResearchDimensionCitationIntegrity, Status: ResearchDimensionPass},
			{ID: ResearchDimensionRelevance, Status: ResearchDimensionPass},
			{ID: ResearchDimensionSourceQuality, Status: ResearchDimensionPass},
			{ID: ResearchDimensionSourceDiversity, Status: ResearchDimensionPass},
			{ID: ResearchDimensionConflictHandling, Status: ResearchDimensionNotMeasurable},
			{ID: ResearchDimensionUncertainty, Status: ResearchDimensionPass},
			{ID: ResearchDimensionPlanCompleteness, Status: ResearchDimensionPass},
		},
		Findings:    []ResearchEvaluationFinding{},
		Limitations: []string{"Citation integrity proves reference resolution only."},
	}
}

func evaluationWithoutEvidence(executionID string) *ResearchEvaluation {
	return &ResearchEvaluation{
		Version:             ResearchEvaluationVersion,
		Method:              ResearchEvaluationMethod,
		ExecutionID:         executionID,
		EvidenceVersion:     0,
		EvidenceCount:       0,
		EvidenceFingerprint: ResearchEvidenceFingerprint(nil),
		Status:              ResearchEvaluationIncomplete,
		Dimensions: []ResearchEvaluationDimension{
			{ID: ResearchDimensionEvidenceCoverage, Status: ResearchDimensionIncomplete},
			{ID: ResearchDimensionCitationIntegrity, Status: ResearchDimensionIncomplete},
			{ID: ResearchDimensionRelevance, Status: ResearchDimensionNotApplicable},
			{ID: ResearchDimensionSourceQuality, Status: ResearchDimensionIncomplete},
			{ID: ResearchDimensionSourceDiversity, Status: ResearchDimensionNotApplicable},
			{ID: ResearchDimensionConflictHandling, Status: ResearchDimensionIncomplete},
			{ID: ResearchDimensionUncertainty, Status: ResearchDimensionIncomplete},
			{ID: ResearchDimensionPlanCompleteness, Status: ResearchDimensionIncomplete},
		},
		Findings: []ResearchEvaluationFinding{{
			Code: "evidence_unavailable", Dimension: ResearchDimensionEvidenceCoverage,
			Severity: ResearchFindingWarning,
			Message:  "No complete evidence snapshot was available for evaluation.",
		}},
		Limitations: []string{"Citation integrity proves reference resolution only."},
	}
}

func TestResearchEvidenceFingerprintDistinguishesSnapshots(t *testing.T) {
	first := evaluationEvidence("exec-1")
	second := evaluationEvidence("exec-1")
	if ResearchEvidenceFingerprint(first) != ResearchEvidenceFingerprint(second) {
		t.Fatal("fingerprint must be stable for the same snapshot")
	}
	second.Sources[0].Title = "Different title"
	if ResearchEvidenceFingerprint(first) == ResearchEvidenceFingerprint(second) {
		t.Fatal("fingerprint must change with the evidence")
	}
	if ResearchEvidenceFingerprint(nil) == "" {
		t.Fatal("absent evidence must have a stable, non-empty fingerprint")
	}
}

func TestValidateResearchEvaluationBindsExecutionAndEvidence(t *testing.T) {
	evidence := evaluationEvidence("exec-1")
	evaluation := evaluationFor("exec-1", evidence)

	if err := ValidateResearchEvaluationForExecution("exec-1", evaluation, evidence); err != nil {
		t.Fatalf("valid evaluation rejected: %v", err)
	}

	foreign := evaluationFor("exec-2", evidence)
	if err := ValidateResearchEvaluationForExecution("exec-1", foreign, evidence); err == nil {
		t.Fatal("an evaluation for another execution must be rejected")
	}

	other := evaluationEvidence("exec-1")
	other.Sources[0].Title = "Changed"
	mismatched := evaluationFor("exec-1", evidence)
	mismatched.EvidenceFingerprint = ResearchEvidenceFingerprint(other)
	if err := ValidateResearchEvaluationForExecution("exec-1", mismatched, evidence); err == nil {
		t.Fatal("an evaluation bound to different evidence must be rejected")
	}

	contradiction := evaluationFor("exec-1", evidence)
	contradiction.Status = ResearchEvaluationPassed
	contradiction.Findings = []ResearchEvaluationFinding{{
		Code: "source_invalid_url", Dimension: ResearchDimensionSourceQuality,
		Severity: ResearchFindingWarning, Message: "Source URL is not an absolute http(s) URL with a host.",
	}}
	if err := ValidateResearchEvaluationForExecution("exec-1", contradiction, evidence); err == nil {
		t.Fatal("a passed verdict with a warning finding must be rejected")
	}

	unknownDimension := evaluationFor("exec-1", evidence)
	unknownDimension.Dimensions[0].ID = "made_up_dimension"
	if err := ValidateResearchEvaluationForExecution("exec-1", unknownDimension, evidence); err == nil {
		t.Fatal("an unknown dimension must be rejected")
	}

	orphanSynthesis := evaluationFor("exec-1", evidence)
	orphanSynthesis.SynthesisStatus = "complete"
	orphanSynthesis.SynthesisFingerprint = ResearchEvidenceFingerprint(evidence)
	orphanSynthesis.EvidenceVersion = 0
	orphanSynthesis.EvidenceCount = 0
	orphanSynthesis.EvidenceFingerprint = ResearchEvidenceFingerprint(nil)
	if err := ValidateResearchEvaluationForExecution("exec-1", orphanSynthesis, nil); err == nil {
		t.Fatal("a synthesis verdict without evidence must be rejected")
	}
}

func TestTerminalWithResearchPersistsEvaluationAtomically(t *testing.T) {
	mem := store.NewMemStore()
	j := New(mem, fixedClock())
	if err := j.Accepted("exec-1", "biz-1", "div-1", "actor-1"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	evidence := evaluationEvidence("exec-1")
	evaluation := evaluationFor("exec-1", evidence)
	if err := j.TerminalWithResearch("exec-1", StateCompleted, "", evidence, evaluation, nil); err != nil {
		t.Fatalf("terminal: %v", err)
	}

	rec, ok, err := j.Get("exec-1")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if rec.ResearchEvidence == nil || rec.ResearchEvaluation == nil {
		t.Fatal("terminal write must persist evidence and evaluation together")
	}
	if rec.ResearchEvaluation.EvidenceFingerprint != ResearchEvidenceFingerprint(rec.ResearchEvidence) {
		t.Fatal("persisted evaluation must stay bound to the persisted evidence")
	}

	// A new journal over the same store is the restart boundary: the verdict
	// survives, and the decode path validates it again.
	restarted := New(mem, fixedClock())
	rec, ok, err = restarted.Get("exec-1")
	if err != nil || !ok {
		t.Fatalf("get after restart: ok=%v err=%v", ok, err)
	}
	if rec.ResearchEvaluation == nil || rec.ResearchEvaluation.Status != ResearchEvaluationPassed {
		t.Fatalf("evaluation did not survive restart: %+v", rec.ResearchEvaluation)
	}
}

// A verdict that does not bind to the snapshot being persisted is never
// persisted. It is auxiliary data: the evidence and the terminal lifecycle
// record are written, and the unusable verdict is dropped instead of blocking
// the write or being paired with the wrong evidence.
func TestTerminalDropsEvaluationThatDoesNotMatchEvidence(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("exec-1", "biz-1", "div-1", "actor-1"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	evidence := evaluationEvidence("exec-1")
	evaluation := evaluationFor("exec-1", evidence)
	evaluation.EvidenceFingerprint = strings.Repeat("a", 64)
	if err := j.TerminalWithResearch("exec-1", StateCompleted, "", evidence, evaluation, nil); err != nil {
		t.Fatalf("terminal: %v", err)
	}
	rec, ok, err := j.Get("exec-1")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if rec.State != StateCompleted {
		t.Fatalf("state = %q, the terminal record must still be written", rec.State)
	}
	if rec.ResearchEvidence == nil {
		t.Fatal("the evidence must still be written")
	}
	if rec.ResearchEvaluation != nil {
		t.Fatalf("a mismatched verdict must never be persisted: %+v", rec.ResearchEvaluation)
	}
}

// A verdict that belongs to another execution is never persisted under this
// execution id.
func TestTerminalDropsForeignEvaluation(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("exec-1", "biz-1", "div-1", "actor-1"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	evidence := evaluationEvidence("exec-1")
	evaluation := evaluationFor("exec-1", evidence)
	evaluation.ExecutionID = "exec-2"
	if err := j.TerminalWithResearch("exec-1", StateCompleted, "", evidence, evaluation, nil); err != nil {
		t.Fatalf("terminal: %v", err)
	}
	rec, _, err := j.Get("exec-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if rec.ResearchEvaluation != nil {
		t.Fatalf("a foreign verdict must never be persisted: %+v", rec.ResearchEvaluation)
	}
}

func TestSupersedeReplacesEvidenceAndEvaluationTogether(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("exec-1", "biz-1", "div-1", "actor-1"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	pauseEvidence := evaluationEvidence("exec-1")
	pauseEvaluation := evaluationFor("exec-1", pauseEvidence)
	if err := j.TerminalWithResearch("exec-1", StateFailed, ApprovalRequiredClass, pauseEvidence, pauseEvaluation, nil); err != nil {
		t.Fatalf("pause terminal: %v", err)
	}

	finalEvidence := evaluationEvidence("exec-1")
	finalEvidence.Sources[0].Title = "Final title"
	finalEvaluation := evaluationFor("exec-1", finalEvidence)
	if err := j.SupersedeApprovedPauseWithResearch("exec-1", StateCompleted, "", finalEvidence, finalEvaluation, nil); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	rec, _, err := j.Get("exec-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if rec.ResearchEvidence == nil || rec.ResearchEvidence.Sources[0].Title != "Final title" {
		t.Fatalf("final evidence not written: %+v", rec.ResearchEvidence)
	}
	if rec.ResearchEvaluation == nil ||
		rec.ResearchEvaluation.EvidenceFingerprint != ResearchEvidenceFingerprint(finalEvidence) {
		t.Fatalf("final evaluation not written: %+v", rec.ResearchEvaluation)
	}
}

func TestSupersedeKeepsPauseEvaluationWhenResumedRunHasNoEvidence(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("exec-1", "biz-1", "div-1", "actor-1"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	pauseEvidence := evaluationEvidence("exec-1")
	pauseEvaluation := evaluationFor("exec-1", pauseEvidence)
	if err := j.TerminalWithResearch("exec-1", StateFailed, ApprovalRequiredClass, pauseEvidence, pauseEvaluation, nil); err != nil {
		t.Fatalf("pause terminal: %v", err)
	}

	// The resumed run attempted research but captured nothing: its evaluation
	// describes "no evidence" and must not be paired with the pause's
	// snapshot. The pause's evidence-bound evaluation is kept, and the
	// terminal write still succeeds.
	resumedEvaluation := evaluationWithoutEvidence("exec-1")
	if err := j.SupersedeApprovedPauseWithResearch("exec-1", StateCompleted, "", nil, resumedEvaluation, nil); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	rec, _, err := j.Get("exec-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if rec.State != StateCompleted {
		t.Fatalf("state = %q", rec.State)
	}
	if rec.ResearchEvidence == nil {
		t.Fatal("the pause's evidence must be preserved")
	}
	if rec.ResearchEvaluation == nil ||
		rec.ResearchEvaluation.EvidenceFingerprint != ResearchEvidenceFingerprint(rec.ResearchEvidence) {
		t.Fatalf("the preserved evaluation must stay bound to the preserved evidence: %+v", rec.ResearchEvaluation)
	}
}

func TestTerminalResearchIsFirstWinsUnderConcurrency(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("exec-1", "biz-1", "div-1", "actor-1"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	evidence := evaluationEvidence("exec-1")
	evaluation := evaluationFor("exec-1", evidence)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = j.TerminalWithResearch("exec-1", StateCompleted, "", evidence, evaluation, nil)
		}()
	}
	wg.Wait()

	rec, _, err := j.Get("exec-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if rec.State != StateCompleted {
		t.Fatalf("state = %q", rec.State)
	}
	if rec.ResearchEvaluation == nil {
		t.Fatal("evaluation must be present after a concurrent terminal write")
	}
}
