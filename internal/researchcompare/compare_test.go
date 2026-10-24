package researchcompare

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/execjournal"
)

var compareTestTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func baseEvidence(executionID string) *execjournal.ResearchEvidence {
	return &execjournal.ResearchEvidence{
		Version:    execjournal.ResearchEvidenceVersion,
		Capability: execjournal.ResearchEvidenceCapability,
		Operation:  execjournal.ResearchEvidenceOperation,
		Count:      2,
		Sources: []execjournal.ResearchSource{
			{ID: "src-1", Title: "Source One", URL: "https://example.com/one", Source: "Example", Snippet: "Snippet one."},
			{ID: "src-2", Title: "Source Two", URL: "https://example.com/two", Source: "Example", Snippet: "Snippet two."},
		},
		Plan: &execjournal.ResearchPlanEvidence{
			PlanID:           executionID,
			Version:          1,
			ObjectiveID:      "objective-1",
			CompletionStepID: "s2",
			Provenance:       execjournal.ResearchProvenanceAuthoritative,
		},
		Steps: []execjournal.ResearchStepEvidence{
			{StepID: "s1", Intent: "gather recorded sources", PlanVersion: 1, Status: "completed",
				ObservationID: "obs-1", Provenance: execjournal.ResearchProvenanceAuthoritative},
			{StepID: "s2", Intent: "summarize recorded sources", PlanVersion: 1, Status: "completed",
				ObservationID: "obs-2", Dependencies: []string{"s1"},
				Provenance: execjournal.ResearchProvenanceAuthoritative},
		},
		Operations: []execjournal.ResearchOperationEvidence{
			{OperationID: "obs-1", StepID: "s1", ToolID: execjournal.ResearchEvidenceCapability,
				Operation: execjournal.ResearchEvidenceOperation, SourceIDs: []string{"src-1"},
				Provenance: execjournal.ResearchProvenanceAuthoritative},
			{OperationID: "obs-2", StepID: "s2", ToolID: execjournal.ResearchEvidenceCapability,
				Operation: execjournal.ResearchEvidenceOperation, SourceIDs: []string{"src-2"},
				Provenance: execjournal.ResearchProvenanceAuthoritative},
		},
		Attribution: execjournal.ResearchAttributionDerived,
	}
}

func baseSynthesis(executionID string, evidence *execjournal.ResearchEvidence) *execjournal.ResearchSynthesis {
	return &execjournal.ResearchSynthesis{
		Version:     execjournal.ResearchSynthesisVersion,
		Method:      execjournal.ResearchSynthesisMethod,
		ExecutionID: executionID,
		Status:      execjournal.ResearchSynthesisStatusComplete,
		Summary:     "A bounded recorded synthesis summary.",
		EvidenceBacked: []execjournal.ResearchSynthesisStatement{
			{Statement: "Recorded statement one.", SourceIDs: []string{"src-1"}},
		},
		Inferences: []execjournal.ResearchSynthesisInference{
			{Statement: "Recorded inference one."},
		},
		Conflicts:           []execjournal.ResearchSynthesisStatement{},
		UnresolvedQuestions: []string{"Recorded question one."},
		EvidenceFingerprint: execjournal.ResearchEvidenceFingerprint(evidence),
		PlanVersion:         evidence.Plan.Version,
	}
}

func baseEvaluation(executionID string, evidence *execjournal.ResearchEvidence, synthesis *execjournal.ResearchSynthesis) *execjournal.ResearchEvaluation {
	return &execjournal.ResearchEvaluation{
		Version:             execjournal.ResearchEvaluationVersion,
		Method:              execjournal.ResearchEvaluationMethod,
		ExecutionID:         executionID,
		EvidenceVersion:     evidence.Version,
		EvidenceCount:       evidence.Count,
		EvidenceFingerprint: execjournal.ResearchEvidenceFingerprint(evidence),
		PlanVersion:         evidence.Plan.Version,
		SynthesisStatus:     synthesis.Status,
		SynthesisFingerprint: execjournal.ResearchSynthesisFingerprint(
			synthesis),
		Status: execjournal.ResearchEvaluationWarnings,
		Dimensions: []execjournal.ResearchEvaluationDimension{
			{ID: execjournal.ResearchDimensionEvidenceCoverage, Status: execjournal.ResearchDimensionPass},
			{ID: execjournal.ResearchDimensionCitationIntegrity, Status: execjournal.ResearchDimensionPass},
			{ID: execjournal.ResearchDimensionRelevance, Status: execjournal.ResearchDimensionWarn},
			{ID: execjournal.ResearchDimensionSourceQuality, Status: execjournal.ResearchDimensionPass},
			{ID: execjournal.ResearchDimensionSourceDiversity, Status: execjournal.ResearchDimensionPass},
			{ID: execjournal.ResearchDimensionConflictHandling, Status: execjournal.ResearchDimensionPass},
			{ID: execjournal.ResearchDimensionUncertainty, Status: execjournal.ResearchDimensionPass},
			{ID: execjournal.ResearchDimensionPlanCompleteness, Status: execjournal.ResearchDimensionPass},
		},
		Findings: []execjournal.ResearchEvaluationFinding{
			{Code: "relevance_warning", Dimension: execjournal.ResearchDimensionRelevance,
				Severity: execjournal.ResearchFindingWarning, Message: "Recorded relevance warning.",
				SourceID: "src-1"},
		},
		Limitations: []string{"Recorded limitation one."},
	}
}

func baseRecord(executionID string) execjournal.ExecutionRecord {
	ev := baseEvidence(executionID)
	synthesis := baseSynthesis(executionID, ev)
	evaluation := baseEvaluation(executionID, ev, synthesis)
	completed := compareTestTime.Add(time.Minute)
	return execjournal.ExecutionRecord{
		Version:            execjournal.SchemaVersion,
		ID:                 executionID,
		BusinessID:         "biz-1",
		DivisionID:         "div-a",
		State:              execjournal.StateCompleted,
		CreatedAt:          compareTestTime,
		UpdatedAt:          completed,
		CompletedAt:        &completed,
		ResearchEvidence:   ev,
		ResearchSynthesis:  synthesis,
		ResearchEvaluation: evaluation,
	}
}

func cloneRecord(t *testing.T, rec execjournal.ExecutionRecord) execjournal.ExecutionRecord {
	t.Helper()
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	var out execjournal.ExecutionRecord
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal record: %v", err)
	}
	return out
}

func retarget(rec *execjournal.ExecutionRecord, executionID string) {
	rec.ID = executionID
	if rec.ResearchEvidence != nil && rec.ResearchEvidence.Plan != nil {
		rec.ResearchEvidence.Plan.PlanID = executionID
	}
	if rec.ResearchSynthesis != nil {
		rec.ResearchSynthesis.ExecutionID = executionID
	}
	if rec.ResearchEvaluation != nil {
		rec.ResearchEvaluation.ExecutionID = executionID
	}
}

func rebind(rec *execjournal.ExecutionRecord) {
	ev := rec.ResearchEvidence
	if rec.ResearchSynthesis != nil {
		rec.ResearchSynthesis.EvidenceFingerprint = execjournal.ResearchEvidenceFingerprint(ev)
		if ev != nil && ev.Plan != nil {
			rec.ResearchSynthesis.PlanVersion = ev.Plan.Version
		} else {
			rec.ResearchSynthesis.PlanVersion = 0
		}
	}
	if rec.ResearchEvaluation != nil {
		rec.ResearchEvaluation.EvidenceFingerprint = execjournal.ResearchEvidenceFingerprint(ev)
		if ev != nil {
			rec.ResearchEvaluation.EvidenceVersion = ev.Version
			rec.ResearchEvaluation.EvidenceCount = ev.Count
		} else {
			rec.ResearchEvaluation.EvidenceVersion = 0
			rec.ResearchEvaluation.EvidenceCount = 0
		}
		if ev != nil && ev.Plan != nil {
			rec.ResearchEvaluation.PlanVersion = ev.Plan.Version
		} else {
			rec.ResearchEvaluation.PlanVersion = 0
		}
		if rec.ResearchSynthesis != nil {
			rec.ResearchEvaluation.SynthesisStatus = rec.ResearchSynthesis.Status
			rec.ResearchEvaluation.SynthesisFingerprint = execjournal.ResearchSynthesisFingerprint(rec.ResearchSynthesis)
		} else {
			rec.ResearchEvaluation.SynthesisStatus = ""
			rec.ResearchEvaluation.SynthesisFingerprint = ""
		}
	}
}

func mustCompare(t *testing.T, left, right execjournal.ExecutionRecord) Comparison {
	t.Helper()
	c, err := Compare(left, right)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if err := Validate(c); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return c
}

func hasDifference(c Comparison, category, entity, field, kind string) bool {
	for _, diff := range c.Differences {
		if diff.Category == category && diff.Entity == entity && diff.Field == field && diff.Kind == kind {
			return true
		}
	}
	return false
}

func hasFindingCode(c Comparison, code string) bool {
	for _, finding := range c.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func TestCompareIdenticalSnapshotsDeterministicAndUnmutated(t *testing.T) {
	left := baseRecord("exec-left")
	right := baseRecord("exec-right")
	beforeLeft, _ := json.Marshal(left)
	beforeRight, _ := json.Marshal(right)

	c := mustCompare(t, left, right)
	if len(c.Differences) != 0 {
		t.Fatalf("expected no differences, got %+v", c.Differences)
	}
	if c.Compatibility.Status != CompatibilityComparable {
		t.Fatalf("expected comparable, got %q", c.Compatibility.Status)
	}
	if !hasFindingCode(c, FindingNoRecordedDifferences) {
		t.Fatalf("expected no-recorded-differences finding, got %+v", c.Findings)
	}
	if c.Left.Plan == nil || c.Right.Plan == nil {
		t.Fatal("expected both plan summaries")
	}
	if c.Left.Plan.PlanID == c.Right.Plan.PlanID {
		t.Fatal("test fixtures should have distinct execution-scoped plan ids")
	}

	first, _ := json.Marshal(c)
	for i := 0; i < 20; i++ {
		next := mustCompare(t, left, right)
		raw, _ := json.Marshal(next)
		if string(raw) != string(first) {
			t.Fatalf("comparison is not deterministic on run %d", i)
		}
	}

	afterLeft, _ := json.Marshal(left)
	afterRight, _ := json.Marshal(right)
	if string(beforeLeft) != string(afterLeft) || string(beforeRight) != string(afterRight) {
		t.Fatal("Compare mutated an input record")
	}
}

func TestCompareExecutionScopedPlanAndObjectiveIDsAreNotDifferences(t *testing.T) {
	left := baseRecord("exec-left")
	right := baseRecord("exec-right")
	right.ResearchEvidence.Plan.ObjectiveID = "a-different-execution-scoped-objective"
	rebind(&right)
	c := mustCompare(t, left, right)
	if len(c.Differences) != 0 {
		t.Fatalf("execution-scoped ids must not be differences: %+v", c.Differences)
	}
	if c.Left.Plan.ObjectiveID == c.Right.Plan.ObjectiveID {
		t.Fatal("fixture should expose different objective references")
	}
}

func TestComparePlanStepDependencyAndCompletionChanges(t *testing.T) {
	left := baseRecord("exec-left")
	right := cloneRecord(t, left)
	retarget(&right, "exec-right")
	right.ResearchEvidence.Plan.Version = 2
	right.ResearchEvidence.Plan.CompletionStepID = "s1"
	right.ResearchEvidence.Steps[0].Status = "failed"
	right.ResearchEvidence.Steps[0].Intent = "changed recorded intent"
	right.ResearchEvidence.Steps[1].Dependencies = []string{}
	rebind(&right)

	c := mustCompare(t, left, right)
	for _, want := range []struct {
		category, entity, field, kind string
	}{
		{CategoryPlan, "plan", "version", KindChanged},
		{CategoryPlan, "completion_step", "id", KindChanged},
		{CategorySteps, "step", "status", KindChanged},
		{CategorySteps, "step", "intent", KindChanged},
		{CategorySteps, "step_dependency", "dependency", KindRemoved},
		{CategorySynthesis, "synthesis", "plan_version", KindChanged},
		{CategoryEvaluation, "evaluation", "plan_version", KindChanged},
	} {
		if !hasDifference(c, want.category, want.entity, want.field, want.kind) {
			t.Errorf("missing difference %s/%s/%s/%s", want.category, want.entity, want.field, want.kind)
		}
	}

	// Reversing the pair must reverse added/removed without changing the
	// underlying recorded facts.
	reversed := mustCompare(t, right, left)
	if !hasDifference(reversed, CategorySteps, "step_dependency", "dependency", KindAdded) {
		t.Fatal("expected the dependency difference to reverse to added")
	}
}

func TestCompareOperationsAndAttribution(t *testing.T) {
	left := baseRecord("exec-left")
	right := cloneRecord(t, left)
	retarget(&right, "exec-right")
	right.ResearchEvidence.Operations[0].StepID = "s2"
	right.ResearchEvidence.Operations[0].SourceIDs = []string{"src-2"}
	right.ResearchEvidence.Operations[1].SourceIDs = []string{"src-1"}
	rebind(&right)

	c := mustCompare(t, left, right)
	if !hasDifference(c, CategoryOperations, "operation", "step_id", KindChanged) {
		t.Fatal("expected operation step change")
	}
	if !hasDifference(c, CategoryOperations, "operation_source", "source", KindRemoved) ||
		!hasDifference(c, CategoryOperations, "operation_source", "source", KindAdded) {
		t.Fatal("expected operation source reference changes")
	}
	if !hasFindingCode(c, FindingOperationIdentityScope) {
		t.Fatal("expected operation identity-scope finding")
	}

	noOps := cloneRecord(t, left)
	retarget(&noOps, "exec-right-no-ops")
	noOps.ResearchEvidence.Operations = nil
	noOps.ResearchEvidence.Attribution = execjournal.ResearchAttributionUnavailable
	rebind(&noOps)

	c = mustCompare(t, left, noOps)
	if !hasDifference(c, CategoryEvidence, "source_attribution", "state", KindChanged) {
		t.Fatal("expected source attribution change")
	}
	if !hasDifference(c, CategoryOperations, "operation", "operation", KindRemoved) {
		t.Fatal("expected removed operation")
	}
}

func TestCompareEvidenceSourceIdentityAndURLNotIdentity(t *testing.T) {
	left := baseRecord("exec-left")
	right := cloneRecord(t, left)
	retarget(&right, "exec-right")
	right.ResearchEvidence.Sources[0].ID = "src-1b"
	right.ResearchEvidence.Operations[0].SourceIDs = []string{"src-1b"}
	right.ResearchSynthesis.EvidenceBacked[0].SourceIDs = []string{"src-1b"}
	right.ResearchEvaluation.Findings[0].SourceID = "src-1b"
	rebind(&right)

	c := mustCompare(t, left, right)
	if !hasDifference(c, CategoryEvidence, "source", "record", KindRemoved) ||
		!hasDifference(c, CategoryEvidence, "source", "record", KindAdded) {
		t.Fatal("expected source record identity change")
	}
	if !hasFindingCode(c, FindingSourceURLNotIdentity) {
		t.Fatal("expected same-URL/different-identity finding")
	}
	for _, diff := range c.Differences {
		if diff.Category == CategoryEvidence && diff.Entity == "source" && diff.Kind == KindChanged {
			t.Fatal("sources must not be paired as changed by URL")
		}
	}

	added := cloneRecord(t, left)
	retarget(&added, "exec-right-added")
	added.ResearchEvidence.Sources = append(added.ResearchEvidence.Sources, execjournal.ResearchSource{
		ID: "src-3", Title: "Source Three", URL: "https://example.com/three", Source: "Example", Snippet: "Snippet three.",
	})
	added.ResearchEvidence.Count = 3
	rebind(&added)
	c = mustCompare(t, left, added)
	if !hasDifference(c, CategoryEvidence, "source", "record", KindAdded) {
		t.Fatal("expected added source record")
	}
	if !hasDifference(c, CategoryEvidence, "evidence", "count", KindChanged) {
		t.Fatal("expected evidence count change")
	}
}

func TestCompareSynthesisAndCitationsWithoutProse(t *testing.T) {
	left := baseRecord("exec-left")
	right := cloneRecord(t, left)
	retarget(&right, "exec-right")
	right.ResearchSynthesis.Status = execjournal.ResearchSynthesisStatusPartial
	right.ResearchSynthesis.Summary = "A changed recorded summary."
	right.ResearchSynthesis.EvidenceBacked = []execjournal.ResearchSynthesisStatement{
		{Statement: "A changed recorded statement.", SourceIDs: []string{"src-2"}},
	}
	right.ResearchSynthesis.Inferences = append(right.ResearchSynthesis.Inferences,
		execjournal.ResearchSynthesisInference{Statement: "A changed recorded inference."})
	right.ResearchSynthesis.Conflicts = []execjournal.ResearchSynthesisStatement{
		{Statement: "A changed recorded conflict.", SourceIDs: []string{"src-2"}},
	}
	right.ResearchSynthesis.UnresolvedQuestions = []string{"A changed recorded question."}
	rebind(&right)

	c := mustCompare(t, left, right)
	for _, want := range []struct {
		entity, field, kind string
	}{
		{"synthesis", "status", KindChanged},
		{"synthesis", "summary_fingerprint", KindChanged},
		{"synthesis_statement", "statement", KindRemoved},
		{"synthesis_statement", "statement", KindAdded},
		{"synthesis_conflict", "statement", KindAdded},
		{"synthesis_citation", "source", KindRemoved},
		{"synthesis_citation", "source", KindAdded},
		{"synthesis_inference", "text", KindAdded},
		{"synthesis_question", "text", KindRemoved},
		{"synthesis_question", "text", KindAdded},
	} {
		if !hasDifference(c, CategorySynthesis, want.entity, want.field, want.kind) {
			t.Errorf("missing synthesis difference %s/%s/%s", want.entity, want.field, want.kind)
		}
	}
	raw, _ := json.Marshal(c)
	for _, prose := range []string{
		"A changed recorded statement.",
		"A changed recorded conflict.",
		"A changed recorded inference.",
		"A changed recorded question.",
		"A changed recorded summary.",
	} {
		if string(raw) == "" || contains(raw, []byte(prose)) {
			t.Fatalf("comparison leaked synthesis prose %q", prose)
		}
	}
}

func TestCompareEvaluationDimensionsFindingsAndLimitations(t *testing.T) {
	left := baseRecord("exec-left")
	right := cloneRecord(t, left)
	retarget(&right, "exec-right")
	right.ResearchEvaluation.Status = execjournal.ResearchEvaluationFailed
	right.ResearchEvaluation.Dimensions[2].Status = execjournal.ResearchDimensionFail
	right.ResearchEvaluation.Findings = []execjournal.ResearchEvaluationFinding{
		{Code: "citation_integrity_error", Dimension: execjournal.ResearchDimensionCitationIntegrity,
			Severity: execjournal.ResearchFindingError, Message: "Recorded citation error."},
	}
	right.ResearchEvaluation.Limitations = []string{"A changed recorded limitation."}
	rebind(&right)

	c := mustCompare(t, left, right)
	if !hasDifference(c, CategoryEvaluation, "evaluation", "status", KindChanged) {
		t.Fatal("expected evaluation status change")
	}
	if !hasDifference(c, CategoryEvaluation, "evaluation_dimension", "status", KindChanged) {
		t.Fatal("expected evaluation dimension change")
	}
	if !hasDifference(c, CategoryEvaluation, "evaluation_finding", "finding", KindRemoved) ||
		!hasDifference(c, CategoryEvaluation, "evaluation_finding", "finding", KindAdded) {
		t.Fatal("expected evaluation finding set change")
	}
	if !hasDifference(c, CategoryEvaluation, "evaluation_limitation", "text", KindRemoved) ||
		!hasDifference(c, CategoryEvaluation, "evaluation_limitation", "text", KindAdded) {
		t.Fatal("expected evaluation limitation set change")
	}
}

func TestCompareLegacyV1AndV2Compatibility(t *testing.T) {
	v1 := execjournal.ExecutionRecord{
		Version:    execjournal.SchemaVersion,
		ID:         "exec-v1",
		BusinessID: "biz-1",
		State:      execjournal.StateCompleted,
		CreatedAt:  compareTestTime,
		ResearchEvidence: &execjournal.ResearchEvidence{
			Version:    execjournal.ResearchEvidenceLegacyVersion,
			Capability: execjournal.ResearchEvidenceCapability,
			Operation:  execjournal.ResearchEvidenceOperation,
			Count:      2,
			Sources: []execjournal.ResearchSource{
				{Title: "Source One", URL: "https://example.com/one", Source: "Example", Snippet: "Snippet one."},
				{Title: "Source Two", URL: "https://example.com/two", Source: "Example", Snippet: "Snippet two."},
			},
		},
	}
	v3 := baseRecord("exec-v3")
	c := mustCompare(t, v1, v3)
	if c.Compatibility.Status != CompatibilityPartial {
		t.Fatalf("expected partial legacy compatibility, got %q", c.Compatibility.Status)
	}
	if c.Left.Plan != nil || c.Left.Synthesis != nil || c.Left.Evaluation != nil {
		t.Fatal("v1 side must not invent plan/synthesis/evaluation")
	}
	for _, code := range []string{
		FindingLegacyEvidenceNoProvenance,
		FindingEvidenceVersionChanged,
		FindingSynthesisMissingOnLeft,
		FindingEvaluationMissingOnLeft,
		FindingEvaluationAbsentNotClean,
	} {
		if !hasFindingCode(c, code) {
			t.Errorf("missing legacy finding %s", code)
		}
	}

	v2 := cloneRecord(t, baseRecord("exec-v2"))
	v2.ResearchEvidence.Version = execjournal.ResearchEvidenceProvenanceVersion
	v2.ResearchEvidence.Plan.Provenance = ""
	v2.ResearchEvidence.Plan.ObjectiveID = ""
	v2.ResearchEvidence.Plan.CompletionStepID = ""
	for i := range v2.ResearchEvidence.Steps {
		v2.ResearchEvidence.Steps[i].Intent = ""
		v2.ResearchEvidence.Steps[i].PlanVersion = 0
		v2.ResearchEvidence.Steps[i].Provenance = ""
	}
	for i := range v2.ResearchEvidence.Operations {
		v2.ResearchEvidence.Operations[i].Provenance = ""
	}
	v2.ResearchEvidence.Attribution = ""
	rebind(&v2)
	v2.ResearchSynthesis.PlanVersion = 0
	v2.ResearchEvaluation.PlanVersion = 0
	v2.ResearchEvaluation.SynthesisFingerprint = execjournal.ResearchSynthesisFingerprint(v2.ResearchSynthesis)

	c = mustCompare(t, v2, v3)
	if c.Compatibility.Status != CompatibilityPartial {
		t.Fatalf("expected partial v2 compatibility, got %q", c.Compatibility.Status)
	}
	if c.Left.Plan == nil || c.Left.Plan.CompletionRecorded {
		t.Fatal("v2 plan must not claim v3 completion provenance")
	}
	if c.Left.Evidence.Attribution != "" {
		t.Fatal("v2 evidence must not invent an attribution marker")
	}
	for _, code := range []string{
		FindingLegacyPlanProvenance,
		FindingSynthesisPlanVersionLeft,
		FindingEvaluationPlanVersionLeft,
	} {
		if !hasFindingCode(c, code) {
			t.Errorf("missing v2 finding %s", code)
		}
	}
}

func TestCompareMissingArtifactsAreNotCleanOrEmpty(t *testing.T) {
	left := baseRecord("exec-left")
	right := cloneRecord(t, left)
	retarget(&right, "exec-right")
	right.ResearchSynthesis = nil
	right.ResearchEvaluation = nil
	rebind(&right)

	c := mustCompare(t, left, right)
	if !hasFindingCode(c, FindingSynthesisMissingOnRight) ||
		!hasFindingCode(c, FindingEvaluationMissingOnRight) ||
		!hasFindingCode(c, FindingEvaluationAbsentNotClean) {
		t.Fatalf("missing artifact findings not explicit: %+v", c.Findings)
	}
	if c.Compatibility.Status != CompatibilityPartial {
		t.Fatalf("expected partial compatibility, got %q", c.Compatibility.Status)
	}

	leftNo := cloneRecord(t, left)
	leftNo.ResearchSynthesis = nil
	leftNo.ResearchEvaluation = nil
	rightNo := cloneRecord(t, right)
	rightNo.ResearchSynthesis = nil
	rightNo.ResearchEvaluation = nil
	c = mustCompare(t, leftNo, rightNo)
	if !hasFindingCode(c, FindingSynthesisUnavailableBoth) ||
		!hasFindingCode(c, FindingEvaluationUnavailableBoth) {
		t.Fatalf("both-missing findings not explicit: %+v", c.Findings)
	}
}

func TestCompareBoundsTruncation(t *testing.T) {
	left := baseRecord("exec-left")
	right := cloneRecord(t, left)
	retarget(&right, "exec-right")
	left.ResearchEvaluation.Status = execjournal.ResearchEvaluationFailed
	right.ResearchEvaluation.Status = execjournal.ResearchEvaluationFailed
	for i := range left.ResearchEvaluation.Dimensions {
		left.ResearchEvaluation.Dimensions[i].Status = execjournal.ResearchDimensionFail
		right.ResearchEvaluation.Dimensions[i].Status = execjournal.ResearchDimensionFail
	}
	right.ResearchEvaluation.Dimensions[2].Status = execjournal.ResearchDimensionPass
	left.ResearchEvaluation.Findings = make([]execjournal.ResearchEvaluationFinding, 0, execjournal.MaxResearchEvaluationFindings)
	right.ResearchEvaluation.Findings = make([]execjournal.ResearchEvaluationFinding, 0, execjournal.MaxResearchEvaluationFindings)
	for i := 0; i < execjournal.MaxResearchEvaluationFindings; i++ {
		left.ResearchEvaluation.Findings = append(left.ResearchEvaluation.Findings, execjournal.ResearchEvaluationFinding{
			Code:      fmt.Sprintf("left_finding_%03d", i),
			Dimension: execjournal.ResearchDimensionCitationIntegrity,
			Severity:  execjournal.ResearchFindingError,
			Message:   "Recorded integrity finding.",
		})
		right.ResearchEvaluation.Findings = append(right.ResearchEvaluation.Findings, execjournal.ResearchEvaluationFinding{
			Code:      fmt.Sprintf("right_finding_%03d", i),
			Dimension: execjournal.ResearchDimensionCitationIntegrity,
			Severity:  execjournal.ResearchFindingError,
			Message:   "Recorded integrity finding.",
		})
	}
	rebind(&right)

	c := mustCompare(t, left, right)
	if !c.Truncated {
		t.Fatal("expected explicit truncation")
	}
	if len(c.Differences) != MaxDifferences {
		t.Fatalf("expected %d bounded differences, got %d", MaxDifferences, len(c.Differences))
	}
	if !hasFindingCode(c, FindingOutputTruncated) {
		t.Fatal("expected output-truncation finding")
	}
}

func TestCompareRejectsMalformedInputs(t *testing.T) {
	left := baseRecord("exec-left")
	right := baseRecord("exec-right")

	if _, err := Compare(left, left); err == nil {
		t.Fatal("expected same-id rejection")
	}
	foreign := cloneRecord(t, right)
	foreign.BusinessID = "biz-2"
	if _, err := Compare(left, foreign); err == nil {
		t.Fatal("expected cross-business rejection")
	}
	wrongPlan := cloneRecord(t, right)
	wrongPlan.ResearchEvidence.Plan.PlanID = "not-this-execution"
	if _, err := Compare(left, wrongPlan); err == nil {
		t.Fatal("expected plan identity rejection")
	}
	badEvidence := cloneRecord(t, right)
	badEvidence.ResearchEvidence.Version = 99
	if _, err := Compare(left, badEvidence); err == nil {
		t.Fatal("expected unsupported evidence rejection")
	}
	badSynthesis := cloneRecord(t, right)
	badSynthesis.ResearchSynthesis.EvidenceFingerprint = "not-a-fingerprint"
	if _, err := Compare(left, badSynthesis); err == nil {
		t.Fatal("expected synthesis binding rejection")
	}
	badEvaluation := cloneRecord(t, right)
	badEvaluation.ResearchEvaluation.Status = execjournal.ResearchEvaluationPassed
	if _, err := Compare(left, badEvaluation); err == nil {
		t.Fatal("expected evaluation status rejection")
	}
}

func contains(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == string(needle) {
			return true
		}
	}
	return false
}
