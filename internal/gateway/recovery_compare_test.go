package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/researchcompare"
)

type comparisonResponse struct {
	Source string `json:"source"`
	researchcompare.Comparison
}

func gatewayComparisonEvidence(executionID string) *execjournal.ResearchEvidence {
	ev := recoverySampleEvidence()
	ev.Plan.PlanID = executionID
	ev.Plan.ObjectiveID = "objective-" + executionID
	return ev
}

func gatewayComparisonSynthesis(executionID string, ev *execjournal.ResearchEvidence) *execjournal.ResearchSynthesis {
	return recoverySampleSynthesis(executionID, ev)
}

func gatewayComparisonEvaluation(executionID string, ev *execjournal.ResearchEvidence, synthesis *execjournal.ResearchSynthesis) *execjournal.ResearchEvaluation {
	evaluation := recoverySampleEvaluation(executionID, ev)
	evaluation.SynthesisStatus = synthesis.Status
	evaluation.SynthesisFingerprint = execjournal.ResearchSynthesisFingerprint(synthesis)
	return evaluation
}

func gatewayComparisonRecord(executionID string) execjournal.ExecutionRecord {
	ev := gatewayComparisonEvidence(executionID)
	synthesis := gatewayComparisonSynthesis(executionID, ev)
	evaluation := gatewayComparisonEvaluation(executionID, ev, synthesis)
	now := time.Now().UTC()
	completed := now.Add(time.Second)
	return execjournal.ExecutionRecord{
		Version:            execjournal.SchemaVersion,
		ID:                 executionID,
		BusinessID:         recBiz,
		State:              execjournal.StateCompleted,
		CreatedAt:          now,
		UpdatedAt:          completed,
		CompletedAt:        &completed,
		ResearchEvidence:   ev,
		ResearchSynthesis:  synthesis,
		ResearchEvaluation: evaluation,
	}
}

func gatewayRebind(rec *execjournal.ExecutionRecord) {
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

func seedGatewayComparisonRecord(t *testing.T, j *execjournal.Journal, rec execjournal.ExecutionRecord) {
	t.Helper()
	if err := j.Accepted(rec.ID, rec.BusinessID, rec.DivisionID, "user-wide"); err != nil {
		t.Fatalf("seed accepted %s: %v", rec.ID, err)
	}
	switch rec.State {
	case execjournal.StateAccepted:
		return
	case execjournal.StateUnknown:
		if _, err := j.Recover(); err != nil {
			t.Fatalf("seed unknown %s: %v", rec.ID, err)
		}
		return
	default:
		if err := j.TerminalWithResearch(rec.ID, rec.State, rec.ErrorClass,
			rec.ResearchEvidence, rec.ResearchEvaluation, rec.ResearchSynthesis); err != nil {
			t.Fatalf("seed terminal %s: %v", rec.ID, err)
		}
	}
}

func comparisonPath(businessID, leftID, rightID string) string {
	q := url.Values{}
	q.Set("business_id", businessID)
	q.Set("left_execution_id", leftID)
	q.Set("right_execution_id", rightID)
	return "/api/v1/recovery/comparisons?" + q.Encode()
}

func decodeComparison(t *testing.T, w *httptest.ResponseRecorder) comparisonResponse {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var body comparisonResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode comparison: %v body=%s", err, w.Body.String())
	}
	return body
}

func comparisonHasDifference(c researchcompare.Comparison, category, entity, field, kind string) bool {
	for _, diff := range c.Differences {
		if diff.Category == category && diff.Entity == entity && diff.Field == field && diff.Kind == kind {
			return true
		}
	}
	return false
}

func comparisonErrorSignature(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code      string `json:"code"`
			Category  string `json:"category"`
			Message   string `json:"message"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error envelope: %v body=%s", err, w.Body.String())
	}
	return body.Error.Code + "|" + body.Error.Category + "|" + body.Error.Message
}

func comparisonHasFinding(c researchcompare.Comparison, code string) bool {
	for _, finding := range c.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func TestRecoveryComparisonAuthorizedDeterministicAndReadOnly(t *testing.T) {
	f := newRecoveryFixture(t)
	j := f.engine.ExecutionJournal()
	left := gatewayComparisonRecord("cmp-left")
	right := gatewayComparisonRecord("cmp-right")
	seedGatewayComparisonRecord(t, j, left)
	seedGatewayComparisonRecord(t, j, right)

	before, err := j.ListByBusiness(recBiz, 0)
	if err != nil {
		t.Fatal(err)
	}
	beforeRaw, _ := json.Marshal(before)

	path := comparisonPath(recBiz, left.ID, right.ID)
	w1 := f.get(t, path, wideHeaders())
	body := decodeComparison(t, w1)
	w2 := f.get(t, path, wideHeaders())
	if w1.Body.String() != w2.Body.String() {
		t.Fatal("identical comparison requests returned different bodies")
	}
	if body.Source != recoverySource || body.Version != researchcompare.Version ||
		body.Method != researchcompare.Method {
		t.Fatalf("comparison envelope wrong: %+v", body)
	}
	if body.Left.Role != "left" || body.Right.Role != "right" {
		t.Fatalf("comparison roles wrong: %+v", body)
	}
	if body.Compatibility.Status != researchcompare.CompatibilityComparable {
		t.Fatalf("expected comparable, got %q findings=%+v", body.Compatibility.Status, body.Findings)
	}
	if len(body.Differences) != 0 {
		t.Fatalf("expected no differences, got %+v", body.Differences)
	}
	if !comparisonHasFinding(body.Comparison, researchcompare.FindingNoRecordedDifferences) {
		t.Fatalf("expected no-recorded-differences finding, got %+v", body.Findings)
	}
	if body.Left.Plan == nil || body.Right.Plan == nil ||
		body.Left.Plan.PlanID == body.Right.Plan.PlanID {
		t.Fatal("execution-scoped plan ids should be present and distinct")
	}

	for _, leaked := range []string{
		"The runtime decides; the model proposes.",
		"Inert | data",
		"Synthesized from 2 captured source(s)",
		"The captured source records",
		`"actor_id":`,
		`"research_evidence":`,
		`"research_synthesis":`,
		`"recovery":`,
		`"reason":`,
		`"snippet":`,
		"user-wide",
		recoveryWideCred,
	} {
		if strings.Contains(w1.Body.String(), leaked) {
			t.Fatalf("comparison leaked %q: %s", leaked, w1.Body.String())
		}
	}

	after, err := j.ListByBusiness(recBiz, 0)
	if err != nil {
		t.Fatal(err)
	}
	afterRaw, _ := json.Marshal(after)
	if string(beforeRaw) != string(afterRaw) {
		t.Fatal("comparison read mutated durable records")
	}
}

func TestRecoveryComparisonRecordedDifferences(t *testing.T) {
	f := newRecoveryFixture(t)
	j := f.engine.ExecutionJournal()
	left := gatewayComparisonRecord("cmp-left")
	right := gatewayComparisonRecord("cmp-right")
	right.ResearchEvidence.Plan.Version = 2
	right.ResearchEvidence.Steps[0].Status = "failed"
	right.ResearchEvidence.Steps[0].Intent = "changed recorded intent"
	right.ResearchEvidence.Sources[0].Title = "Changed source title"
	right.ResearchSynthesis.Status = execjournal.ResearchSynthesisStatusPartial
	right.ResearchSynthesis.Summary = "A changed recorded synthesis summary."
	right.ResearchSynthesis.EvidenceBacked[0].Statement = "A changed recorded statement."
	right.ResearchEvaluation.Status = execjournal.ResearchEvaluationFailed
	right.ResearchEvaluation.Dimensions[2].Status = execjournal.ResearchDimensionFail
	right.ResearchEvaluation.Findings = []execjournal.ResearchEvaluationFinding{{
		Code: "citation_integrity_error", Dimension: execjournal.ResearchDimensionCitationIntegrity,
		Severity: execjournal.ResearchFindingError, Message: "Recorded citation error.",
	}}
	gatewayRebind(&right)
	seedGatewayComparisonRecord(t, j, left)
	seedGatewayComparisonRecord(t, j, right)

	w := f.get(t, comparisonPath(recBiz, left.ID, right.ID), wideHeaders())
	body := decodeComparison(t, w)
	if body.Compatibility.Status != researchcompare.CompatibilityComparable {
		t.Fatalf("expected comparable v3 snapshots, got %q", body.Compatibility.Status)
	}
	for _, want := range []struct {
		category, entity, field, kind string
	}{
		{researchcompare.CategoryPlan, "plan", "version", researchcompare.KindChanged},
		{researchcompare.CategorySteps, "step", "status", researchcompare.KindChanged},
		{researchcompare.CategorySteps, "step", "intent", researchcompare.KindChanged},
		{researchcompare.CategoryEvidence, "source", "record", researchcompare.KindRemoved},
		{researchcompare.CategoryEvidence, "source", "record", researchcompare.KindAdded},
		{researchcompare.CategorySynthesis, "synthesis", "status", researchcompare.KindChanged},
		{researchcompare.CategoryEvaluation, "evaluation", "status", researchcompare.KindChanged},
	} {
		if !comparisonHasDifference(body.Comparison, want.category, want.entity, want.field, want.kind) {
			t.Errorf("missing difference %s/%s/%s/%s", want.category, want.entity, want.field, want.kind)
		}
	}
	if !comparisonHasFinding(body.Comparison, researchcompare.FindingSourceURLNotIdentity) {
		t.Fatal("expected same-URL/different-record finding")
	}
	for _, prose := range []string{
		"A changed recorded synthesis summary.",
		"A changed recorded statement.",
	} {
		if strings.Contains(w.Body.String(), prose) {
			t.Fatalf("comparison leaked synthesis prose %q", prose)
		}
	}
}

func TestRecoveryComparisonLegacyCompatibility(t *testing.T) {
	f := newRecoveryFixture(t)
	j := f.engine.ExecutionJournal()

	v1 := execjournal.ExecutionRecord{
		Version:    execjournal.SchemaVersion,
		ID:         "cmp-v1",
		BusinessID: recBiz,
		State:      execjournal.StateCompleted,
		CreatedAt:  time.Now().UTC(),
		ResearchEvidence: &execjournal.ResearchEvidence{
			Version:    execjournal.ResearchEvidenceLegacyVersion,
			Capability: execjournal.ResearchEvidenceCapability,
			Operation:  execjournal.ResearchEvidenceOperation,
			Count:      2,
			Sources: []execjournal.ResearchSource{
				{Title: "Legacy Source One", URL: "https://legacy.example/one", Source: "legacy", Snippet: "Legacy snippet one."},
				{Title: "Legacy Source Two", URL: "https://legacy.example/two", Source: "legacy", Snippet: "Legacy snippet two."},
			},
		},
	}
	seedGatewayComparisonRecord(t, j, v1)

	v2 := gatewayComparisonRecord("cmp-v2")
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
	gatewayRebind(&v2)
	v2.ResearchSynthesis.PlanVersion = 0
	v2.ResearchEvaluation.PlanVersion = 0
	v2.ResearchEvaluation.SynthesisFingerprint = execjournal.ResearchSynthesisFingerprint(v2.ResearchSynthesis)
	seedGatewayComparisonRecord(t, j, v2)

	v3 := gatewayComparisonRecord("cmp-v3")
	seedGatewayComparisonRecord(t, j, v3)

	w := f.get(t, comparisonPath(recBiz, v1.ID, v3.ID), wideHeaders())
	body := decodeComparison(t, w)
	if body.Compatibility.Status != researchcompare.CompatibilityPartial {
		t.Fatalf("v1 comparison must be partial, got %q", body.Compatibility.Status)
	}
	if body.Left.Plan != nil || body.Left.Synthesis != nil || body.Left.Evaluation != nil {
		t.Fatal("v1 comparison invented plan/synthesis/evaluation facts")
	}
	if !comparisonHasFinding(body.Comparison, researchcompare.FindingLegacyEvidenceNoProvenance) {
		t.Fatal("v1 comparison missed legacy finding")
	}

	w = f.get(t, comparisonPath(recBiz, v2.ID, v3.ID), wideHeaders())
	body = decodeComparison(t, w)
	if body.Compatibility.Status != researchcompare.CompatibilityPartial {
		t.Fatalf("v2 comparison must be partial, got %q", body.Compatibility.Status)
	}
	if body.Left.Plan == nil || body.Left.Plan.CompletionRecorded {
		t.Fatal("v2 comparison invented v3 completion provenance")
	}
	if !comparisonHasFinding(body.Comparison, researchcompare.FindingLegacyPlanProvenance) {
		t.Fatal("v2 comparison missed legacy plan finding")
	}
}

func TestRecoveryComparisonMissingArtifactsAndUnknown(t *testing.T) {
	f := newRecoveryFixture(t)
	j := f.engine.ExecutionJournal()
	left := gatewayComparisonRecord("cmp-left")
	right := gatewayComparisonRecord("cmp-right")
	right.ResearchSynthesis = nil
	right.ResearchEvaluation = nil
	gatewayRebind(&right)
	seedGatewayComparisonRecord(t, j, left)
	seedGatewayComparisonRecord(t, j, right)

	body := decodeComparison(t, f.get(t, comparisonPath(recBiz, left.ID, right.ID), wideHeaders()))
	if body.Compatibility.Status != researchcompare.CompatibilityPartial {
		t.Fatalf("missing artifacts must be partial, got %q", body.Compatibility.Status)
	}
	for _, code := range []string{
		researchcompare.FindingSynthesisMissingOnRight,
		researchcompare.FindingEvaluationMissingOnRight,
		researchcompare.FindingEvaluationAbsentNotClean,
	} {
		if !comparisonHasFinding(body.Comparison, code) {
			t.Errorf("missing artifact finding %s", code)
		}
	}

	// The fixture's division record is recovery-settled unknown and eligible;
	// the divisionless completed record is also eligible for a wide caller.
	body = decodeComparison(t, f.get(t, comparisonPath(recBiz, recWideID, recDivID), wideHeaders()))
	if body.Left.State != string(execjournal.StateCompleted) ||
		body.Right.State != string(execjournal.StateUnknown) {
		t.Fatalf("unknown state was not preserved: %+v", body)
	}
	if body.Compatibility.Status != researchcompare.CompatibilityIncompatible {
		t.Fatalf("two empty snapshots must be incompatible, got %q", body.Compatibility.Status)
	}
	if !comparisonHasFinding(body.Comparison, researchcompare.FindingComparisonIncompatible) {
		t.Fatal("empty comparison must say it is incompatible")
	}
}

func TestRecoveryComparisonSameURLDifferentIdentity(t *testing.T) {
	f := newRecoveryFixture(t)
	j := f.engine.ExecutionJournal()
	left := gatewayComparisonRecord("cmp-left")
	right := gatewayComparisonRecord("cmp-right")
	right.ResearchEvidence.Sources[0].ID = "src-1b"
	right.ResearchEvidence.Operations[0].SourceIDs = []string{"src-1b", "src-2"}
	right.ResearchSynthesis.EvidenceBacked[0].SourceIDs = []string{"src-1b", "src-2"}
	right.ResearchEvaluation.Findings[0].SourceID = "src-1b"
	gatewayRebind(&right)
	seedGatewayComparisonRecord(t, j, left)
	seedGatewayComparisonRecord(t, j, right)

	body := decodeComparison(t, f.get(t, comparisonPath(recBiz, left.ID, right.ID), wideHeaders()))
	if !comparisonHasFinding(body.Comparison, researchcompare.FindingSourceURLNotIdentity) {
		t.Fatal("same URL with a different recorded identity must be called out")
	}
	if !comparisonHasDifference(body.Comparison, researchcompare.CategoryEvidence, "source", "record", researchcompare.KindRemoved) ||
		!comparisonHasDifference(body.Comparison, researchcompare.CategoryEvidence, "source", "record", researchcompare.KindAdded) {
		t.Fatal("same URL must not be paired as the same source")
	}
	for _, diff := range body.Differences {
		if diff.Category == researchcompare.CategoryEvidence &&
			diff.Entity == "source" && diff.Kind == researchcompare.KindChanged {
			t.Fatal("source records were paired by URL")
		}
	}
}

func TestRecoveryComparisonAuthorizationAndExistenceNonDisclosure(t *testing.T) {
	f := newRecoveryFixture(t)

	// A division-scoped caller cannot compare a divisionless record on either
	// side, even when the other side is in its own division.
	if w := f.get(t, comparisonPath(recBiz, recWideID, recDivID), divHeaders()); w.Code != http.StatusNotFound {
		t.Fatalf("division caller with divisionless left must be 404, got %d", w.Code)
	}
	if w := f.get(t, comparisonPath(recBiz, recDivID, recWideID), divHeaders()); w.Code != http.StatusNotFound {
		t.Fatalf("division caller with divisionless right must be 404, got %d", w.Code)
	}
	// Both sides in the caller's division are authorized. Unknown is eligible.
	done := gatewayComparisonRecord("cmp-div-done")
	done.DivisionID = recDiv
	seedGatewayComparisonRecord(t, f.engine.ExecutionJournal(), done)
	if w := f.get(t, comparisonPath(recBiz, recDivID, done.ID), divHeaders()); w.Code != http.StatusOK {
		t.Fatalf("division-authorized comparison must be 200, got %d body=%s", w.Code, w.Body.String())
	}

	// Missing, foreign and unauthorized are indistinguishable. Pin the
	// correlation id so the generated envelopes can be compared byte-for-byte.
	missingHeaders := wideHeaders()
	missingHeaders["X-Correlation-ID"] = "comparison-404"
	foreignHeaders := wideHeaders()
	foreignHeaders["X-Correlation-ID"] = "comparison-404"
	missing := f.get(t, comparisonPath(recBiz, recWideID, recGoneID), missingHeaders)
	foreign := f.get(t, comparisonPath(recBiz, recWideID, recOtherID), foreignHeaders)
	if missing.Code != http.StatusNotFound || foreign.Code != http.StatusNotFound {
		t.Fatalf("missing/foreign must both be 404, got %d and %d", missing.Code, foreign.Code)
	}
	if comparisonErrorSignature(t, missing) != comparisonErrorSignature(t, foreign) {
		t.Fatalf("missing and foreign error signatures differ: %q vs %q", missing.Body.String(), foreign.Body.String())
	}
	// Cross-business membership is denied before any record is revealed.
	if w := f.get(t, comparisonPath(recOtherBiz, recOtherID, recWideID), wideHeaders()); w.Code != http.StatusForbidden {
		t.Fatalf("cross-business caller must be 403, got %d", w.Code)
	}
	if w := f.get(t, comparisonPath(recBiz, recWideID, recDivID), nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated comparison must be 401, got %d", w.Code)
	}
}

func TestRecoveryComparisonEligibilityAndValidation(t *testing.T) {
	f := newRecoveryFixture(t)
	j := f.engine.ExecutionJournal()

	accepted := gatewayComparisonRecord("cmp-accepted")
	accepted.State = execjournal.StateAccepted
	accepted.ResearchEvidence = nil
	accepted.ResearchSynthesis = nil
	accepted.ResearchEvaluation = nil
	seedGatewayComparisonRecord(t, j, accepted)

	paused := gatewayComparisonRecord("cmp-paused")
	paused.State = execjournal.StateFailed
	paused.ErrorClass = execjournal.ApprovalRequiredClass
	paused.ResearchEvidence = nil
	paused.ResearchSynthesis = nil
	paused.ResearchEvaluation = nil
	seedGatewayComparisonRecord(t, j, paused)

	if w := f.get(t, comparisonPath(recBiz, accepted.ID, recWideID), wideHeaders()); w.Code != http.StatusConflict {
		t.Fatalf("accepted comparison must be 409, got %d body=%s", w.Code, w.Body.String())
	}
	if w := f.get(t, comparisonPath(recBiz, paused.ID, recWideID), wideHeaders()); w.Code != http.StatusConflict {
		t.Fatalf("provisional approval pause must be 409, got %d body=%s", w.Code, w.Body.String())
	}

	if w := f.get(t, "/api/v1/recovery/comparisons?business_id="+recBiz, wideHeaders()); w.Code != http.StatusBadRequest {
		t.Fatalf("missing ids must be 400, got %d", w.Code)
	}
	if w := f.get(t, comparisonPath(recBiz, recWideID, recWideID), wideHeaders()); w.Code != http.StatusBadRequest {
		t.Fatalf("same ids must be 400, got %d", w.Code)
	}
	long := strings.Repeat("x", recoveryComparisonMaxID+1)
	if w := f.get(t, comparisonPath(recBiz, long, recWideID), wideHeaders()); w.Code != http.StatusBadRequest {
		t.Fatalf("oversized id must be 400, got %d", w.Code)
	}
	if w := f.get(t, comparisonPath(recBiz, "bad\x00id", recWideID), wideHeaders()); w.Code != http.StatusBadRequest {
		t.Fatalf("control-character id must be 400, got %d", w.Code)
	}
}

func TestRecoveryComparisonSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	s1, e1, st1 := newEvidenceServer(t, dir)
	j1 := e1.ExecutionJournal()
	left := gatewayComparisonRecord("cmp-left")
	right := gatewayComparisonRecord("cmp-right")
	seedGatewayComparisonRecord(t, j1, left)
	seedGatewayComparisonRecord(t, j1, right)
	path := comparisonPath(recBiz, left.ID, right.ID)
	w1 := evidenceGet(t, s1, path, wideHeaders())
	decodeComparison(t, w1)

	closeStore(t, st1)
	s2, _, _ := newEvidenceServer(t, dir)
	w2 := evidenceGet(t, s2, path, wideHeaders())
	decodeComparison(t, w2)
	if w1.Body.String() != w2.Body.String() {
		t.Fatalf("comparison changed after restart:\n%s\n%s", w1.Body.String(), w2.Body.String())
	}
}

func TestRecoveryComparisonConcurrentReadsStayBound(t *testing.T) {
	f := newRecoveryFixture(t)
	j := f.engine.ExecutionJournal()
	left := gatewayComparisonRecord("cmp-left")
	right := gatewayComparisonRecord("cmp-right")
	seedGatewayComparisonRecord(t, j, left)
	seedGatewayComparisonRecord(t, j, right)
	path := comparisonPath(recBiz, left.ID, right.ID)

	const workers = 12
	errs := make(chan string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 5; n++ {
				req := httptest.NewRequest(http.MethodGet, path, nil)
				req.Header.Set("X-Actor-ID", "user-wide")
				req.Header.Set("X-Actor-Credential", recoveryWideCred)
				w := httptest.NewRecorder()
				f.srv.Handler().ServeHTTP(w, req)
				if w.Code != http.StatusOK {
					errs <- w.Body.String()
					return
				}
				var body comparisonResponse
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					errs <- err.Error()
					return
				}
				if body.Left.Synthesis == nil || body.Right.Synthesis == nil ||
					body.Left.Synthesis.EvidenceFingerprint != body.Left.Evidence.Fingerprint ||
					body.Right.Synthesis.EvidenceFingerprint != body.Right.Evidence.Fingerprint {
					errs <- "comparison mixed a side's evidence and synthesis versions"
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for msg := range errs {
		t.Fatalf("concurrent comparison failed: %s", msg)
	}
}
