package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/researchaudit"
)

type citationAuditResponse struct {
	Source string `json:"source"`
	researchaudit.Audit
}

func gatewayAuditRecord(id, division string, evidence *execjournal.ResearchEvidence, synthesis *execjournal.ResearchSynthesis) execjournal.ExecutionRecord {
	now := time.Now().UTC()
	completed := now.Add(time.Second)
	return execjournal.ExecutionRecord{
		Version:           execjournal.SchemaVersion,
		ID:                id,
		BusinessID:        recBiz,
		DivisionID:        division,
		State:             execjournal.StateCompleted,
		CreatedAt:         now,
		UpdatedAt:         completed,
		CompletedAt:       &completed,
		ResearchEvidence:  evidence,
		ResearchSynthesis: synthesis,
	}
}

func gatewayAuditPath(executionID string) string {
	return "/api/v1/recovery/executions/" + executionID + "/audit?business_id=" + recBiz
}

func decodeCitationAudit(t *testing.T, w *httptest.ResponseRecorder) citationAuditResponse {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var body citationAuditResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode audit: %v body=%s", err, w.Body.String())
	}
	return body
}

func auditHasFinding(a researchaudit.Audit, code string) bool {
	for _, finding := range a.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func gatewayLegacyEvidence(executionID string) *execjournal.ResearchEvidence {
	ev := gatewayComparisonEvidence(executionID)
	ev.Version = execjournal.ResearchEvidenceProvenanceVersion
	ev.Attribution = ""
	ev.Plan.ObjectiveID = ""
	ev.Plan.CompletionStepID = ""
	ev.Plan.Provenance = ""
	for i := range ev.Steps {
		ev.Steps[i].Intent = ""
		ev.Steps[i].PlanVersion = 0
		ev.Steps[i].Provenance = ""
	}
	for i := range ev.Operations {
		ev.Operations[i].Provenance = ""
	}
	return ev
}

func TestRecoveryCitationAuditAuthorizedDeterministicAndReadOnly(t *testing.T) {
	f := newRecoveryFixture(t)
	j := f.engine.ExecutionJournal()
	evidence := gatewayComparisonEvidence("audit-clean")
	synthesis := recoverySampleSynthesis("audit-clean", evidence)
	rec := gatewayAuditRecord("audit-clean", "", evidence, synthesis)
	seedGatewayComparisonRecord(t, j, rec)

	before, err := j.ListByBusiness(recBiz, 0)
	if err != nil {
		t.Fatal(err)
	}
	beforeRaw, _ := json.Marshal(before)

	w1 := f.get(t, gatewayAuditPath("audit-clean"), wideHeaders())
	body := decodeCitationAudit(t, w1)
	w2 := f.get(t, gatewayAuditPath("audit-clean"), wideHeaders())
	if w1.Body.String() != w2.Body.String() {
		t.Fatal("identical audit requests returned different bodies")
	}
	if body.Source != recoverySource || body.Version != researchaudit.Version ||
		body.Method != researchaudit.Method || body.Status != researchaudit.StatusPassed {
		t.Fatalf("audit envelope wrong: %+v", body.Audit)
	}
	if body.ExecutionID != "audit-clean" || body.Provenance != researchaudit.ProvenanceDerived {
		t.Fatalf("audit binding wrong: %+v", body.Audit)
	}
	if body.EvidenceVersion != execjournal.ResearchEvidenceVersion ||
		body.SynthesisVersion != execjournal.ResearchSynthesisVersion ||
		body.SynthesisMethod != execjournal.ResearchSynthesisMethod ||
		body.SynthesisStatus != execjournal.ResearchSynthesisStatusComplete {
		t.Fatalf("audit artifact identity wrong: %+v", body.Audit)
	}
	if !body.PlanVersionRecorded || !body.EvidencePlanRecorded ||
		body.PlanVersion != body.EvidencePlanVersion {
		t.Fatalf("audit plan binding wrong: %+v", body.Audit)
	}
	if len(body.Claims) != 2 {
		t.Fatalf("claims = %+v", body.Claims)
	}
	if body.Claims[0].Category != researchaudit.CategoryEvidenceBacked ||
		body.Claims[0].Status != researchaudit.ClaimPassed ||
		body.Claims[0].Citations[0].Status != researchaudit.CitationResolved ||
		body.Claims[0].Citations[0].EvidenceStatus != researchaudit.EvidenceAvailable {
		t.Fatalf("cited claim = %+v", body.Claims[0])
	}
	if body.Claims[1].Category != researchaudit.CategoryInference ||
		body.Claims[1].Status != researchaudit.ClaimNotApplicable ||
		len(body.Claims[1].Citations) != 0 {
		t.Fatalf("inference claim = %+v", body.Claims[1])
	}
	if len(body.Findings) != 0 {
		t.Fatalf("clean audit findings = %+v", body.Findings)
	}
	if len(body.Limitations) == 0 {
		t.Fatal("audit limitations are missing")
	}

	for _, leaked := range []string{
		"The runtime decides; the model proposes.",
		"Inert | data",
		"https://docs.example/nexus",
		"https://example.org/safety",
		"docs.example",
		`"actor_id":`,
		`"research_evidence":`,
		`"research_synthesis":`,
		`"snippet":`,
		`"title":`,
		`"url":`,
		"user-wide",
		recoveryWideCred,
	} {
		if strings.Contains(w1.Body.String(), leaked) {
			t.Fatalf("audit leaked %q: %s", leaked, w1.Body.String())
		}
	}

	after, err := j.ListByBusiness(recBiz, 0)
	if err != nil {
		t.Fatal(err)
	}
	afterRaw, _ := json.Marshal(after)
	if string(beforeRaw) != string(afterRaw) {
		t.Fatal("audit read mutated durable records")
	}
}

func TestRecoveryCitationAuditWarningsAndIncomplete(t *testing.T) {
	f := newRecoveryFixture(t)
	j := f.engine.ExecutionJournal()

	t.Run("content warning", func(t *testing.T) {
		evidence := gatewayComparisonEvidence("audit-warning")
		evidence.Sources[0].Snippet = ""
		synthesis := recoverySampleSynthesis("audit-warning", evidence)
		seedGatewayComparisonRecord(t, j, gatewayAuditRecord("audit-warning", "", evidence, synthesis))

		body := decodeCitationAudit(t, f.get(t, gatewayAuditPath("audit-warning"), wideHeaders()))
		if body.Status != researchaudit.StatusWarnings ||
			!auditHasFinding(body.Audit, researchaudit.FindingEvidenceContentUnavailable) {
			t.Fatalf("audit = %+v", body.Audit)
		}
		if body.Claims[0].Status != researchaudit.ClaimWarnings {
			t.Fatalf("claim = %+v", body.Claims[0])
		}
	})

	t.Run("legacy plan revision unavailable", func(t *testing.T) {
		evidence := gatewayLegacyEvidence("audit-legacy")
		synthesis := recoverySampleSynthesis("audit-legacy", evidence)
		synthesis.PlanVersion = 0
		seedGatewayComparisonRecord(t, j, gatewayAuditRecord("audit-legacy", "", evidence, synthesis))

		body := decodeCitationAudit(t, f.get(t, gatewayAuditPath("audit-legacy"), wideHeaders()))
		if body.Status != researchaudit.StatusIncomplete ||
			!auditHasFinding(body.Audit, researchaudit.FindingPlanRevisionUnavailable) ||
			!auditHasFinding(body.Audit, researchaudit.FindingLegacyProvenance) {
			t.Fatalf("audit = %+v", body.Audit)
		}
		if body.Claims[0].Provenance != researchaudit.ProvenanceLegacy {
			t.Fatalf("claim provenance = %q", body.Claims[0].Provenance)
		}
	})
}

func TestRecoveryCitationAuditUnavailableIsNotClean(t *testing.T) {
	f := newRecoveryFixture(t)

	body := decodeCitationAudit(t, f.get(t, gatewayAuditPath(recWideID), wideHeaders()))
	if body.Status != researchaudit.StatusUnavailable ||
		!auditHasFinding(body.Audit, researchaudit.FindingSynthesisUnavailable) {
		t.Fatalf("audit = %+v", body.Audit)
	}
	if len(body.Claims) != 0 {
		t.Fatalf("unavailable audit carries claims: %+v", body.Claims)
	}

	// The journal deliberately drops an invalid synthesis instead of storing
	// it. The audit must report the missing artifact, never a fabricated failed
	// or clean audit.
	evidence := gatewayComparisonEvidence("audit-dropped")
	synthesis := recoverySampleSynthesis("audit-dropped", evidence)
	synthesis.EvidenceBacked[0].SourceIDs = []string{"src-from-another-execution"}
	seedGatewayComparisonRecord(t, f.engine.ExecutionJournal(),
		gatewayAuditRecord("audit-dropped", "", evidence, synthesis))
	body = decodeCitationAudit(t, f.get(t, gatewayAuditPath("audit-dropped"), wideHeaders()))
	if body.Status != researchaudit.StatusUnavailable {
		t.Fatalf("dropped synthesis audit = %+v", body.Audit)
	}
}

func TestRecoveryCitationAuditAuthorizationAndExistenceNonDisclosure(t *testing.T) {
	f := newRecoveryFixture(t)
	j := f.engine.ExecutionJournal()
	evidence := gatewayComparisonEvidence("audit-div")
	synthesis := recoverySampleSynthesis("audit-div", evidence)
	seedGatewayComparisonRecord(t, j, gatewayAuditRecord("audit-div", recDiv, evidence, synthesis))
	evidence = gatewayComparisonEvidence("audit-wide")
	synthesis = recoverySampleSynthesis("audit-wide", evidence)
	seedGatewayComparisonRecord(t, j, gatewayAuditRecord("audit-wide", "", evidence, synthesis))

	if w := f.get(t, gatewayAuditPath("audit-div"), divHeaders()); w.Code != http.StatusOK {
		t.Fatalf("division caller must read its own record, got %d body=%s", w.Code, w.Body.String())
	}
	if w := f.get(t, gatewayAuditPath("audit-div"), wideHeaders()); w.Code != http.StatusOK {
		t.Fatalf("business-wide caller must read a division record, got %d", w.Code)
	}

	// Missing and out-of-scope records are indistinguishable.
	missing := f.get(t, gatewayAuditPath(recGoneID), divHeaders())
	outOfScope := f.get(t, gatewayAuditPath("audit-wide"), divHeaders())
	if missing.Code != http.StatusNotFound || outOfScope.Code != http.StatusNotFound {
		t.Fatalf("missing=%d out-of-scope=%d", missing.Code, outOfScope.Code)
	}
	if missing.Body.String() != outOfScope.Body.String() {
		if comparisonErrorSignature(t, missing) != comparisonErrorSignature(t, outOfScope) {
			t.Fatalf("existence leak: missing=%s out-of-scope=%s", missing.Body.String(), outOfScope.Body.String())
		}
	}
	if w := f.get(t, "/api/v1/recovery/executions/audit-div/audit?business_id="+recOtherBiz, divHeaders()); w.Code != http.StatusForbidden {
		t.Fatalf("cross-business membership must be 403, got %d", w.Code)
	}
	if w := f.get(t, gatewayAuditPath("audit-div"), nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("missing credentials must be 401, got %d", w.Code)
	}
	if w := f.get(t, "/api/v1/recovery/executions/audit-div/audit", wideHeaders()); w.Code != http.StatusBadRequest {
		t.Fatalf("missing business_id must be 400, got %d", w.Code)
	}
	if w := f.get(t, "/api/v1/recovery/executions/"+strings.Repeat("x", recoveryExecutionMaxID+1)+"/audit?business_id="+recBiz, wideHeaders()); w.Code != http.StatusBadRequest {
		t.Fatalf("oversized execution id must be 400, got %d", w.Code)
	}
	for _, leaked := range []string{recoveryWideCred, "user-wide", "src-1", "The runtime decides"} {
		if strings.Contains(missing.Body.String(), leaked) || strings.Contains(outOfScope.Body.String(), leaked) {
			t.Fatalf("error body leaked %q: %s", leaked, missing.Body.String())
		}
	}
}

func TestRecoveryCitationAuditRestart(t *testing.T) {
	dir := t.TempDir()
	s1, e1, _ := newEvidenceServer(t, dir)
	evidence := gatewayComparisonEvidence("audit-restart")
	synthesis := recoverySampleSynthesis("audit-restart", evidence)
	seedGatewayComparisonRecord(t, e1.ExecutionJournal(),
		gatewayAuditRecord("audit-restart", "", evidence, synthesis))

	w1 := evidenceGet(t, s1, gatewayAuditPath("audit-restart"), wideHeaders())
	first := decodeCitationAudit(t, w1)
	if first.Status != researchaudit.StatusPassed {
		t.Fatalf("first audit = %+v", first.Audit)
	}

	s2, _, _ := newEvidenceServer(t, dir)
	w2 := evidenceGet(t, s2, gatewayAuditPath("audit-restart"), wideHeaders())
	second := decodeCitationAudit(t, w2)
	if w1.Body.String() != w2.Body.String() {
		t.Fatal("audit changed across restart")
	}
	if second.SynthesisFingerprint != first.SynthesisFingerprint ||
		second.EvidenceFingerprint != first.EvidenceFingerprint {
		t.Fatalf("fingerprint binding changed across restart: first=%+v second=%+v", first.Audit, second.Audit)
	}
}

func TestRecoveryCitationAuditConcurrent(t *testing.T) {
	f := newRecoveryFixture(t)
	evidence := gatewayComparisonEvidence("audit-concurrent")
	synthesis := recoverySampleSynthesis("audit-concurrent", evidence)
	seedGatewayComparisonRecord(t, f.engine.ExecutionJournal(),
		gatewayAuditRecord("audit-concurrent", "", evidence, synthesis))

	const workers = 16
	bodies := make(chan string, workers)
	codes := make(chan int, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := f.get(t, gatewayAuditPath("audit-concurrent"), wideHeaders())
			codes <- w.Code
			bodies <- w.Body.String()
		}()
	}
	wg.Wait()
	close(codes)
	close(bodies)
	var want string
	for code := range codes {
		if code != http.StatusOK {
			t.Fatalf("concurrent audit status = %d", code)
		}
	}
	for body := range bodies {
		if want == "" {
			want = body
			continue
		}
		if body != want {
			t.Fatal("concurrent audits returned different bodies")
		}
	}
}

func TestCitationAuditForRecordStates(t *testing.T) {
	cleanEvidence := gatewayComparisonEvidence("audit-state")
	cleanSynthesis := recoverySampleSynthesis("audit-state", cleanEvidence)
	cases := []struct {
		name      string
		evidence  *execjournal.ResearchEvidence
		synthesis *execjournal.ResearchSynthesis
		status    string
		code      string
	}{
		{
			name:      "passed",
			evidence:  cleanEvidence,
			synthesis: cleanSynthesis,
			status:    researchaudit.StatusPassed,
		},
		{
			name: "warning",
			evidence: func() *execjournal.ResearchEvidence {
				ev := gatewayComparisonEvidence("audit-state")
				ev.Sources[0].Snippet = ""
				return ev
			}(),
			synthesis: func() *execjournal.ResearchSynthesis {
				ev := gatewayComparisonEvidence("audit-state")
				ev.Sources[0].Snippet = ""
				return recoverySampleSynthesis("audit-state", ev)
			}(),
			status: researchaudit.StatusWarnings,
			code:   researchaudit.FindingEvidenceContentUnavailable,
		},
		{
			name:     "failed unresolved citation",
			evidence: cleanEvidence,
			synthesis: func() *execjournal.ResearchSynthesis {
				syn := recoverySampleSynthesis("audit-state", cleanEvidence)
				syn.EvidenceBacked[0].SourceIDs = []string{"src-from-another-execution"}
				return syn
			}(),
			status: researchaudit.StatusFailed,
			code:   researchaudit.FindingCitationUnresolved,
		},
		{
			name:     "failed duplicate identity",
			evidence: cleanEvidence,
			synthesis: func() *execjournal.ResearchSynthesis {
				syn := recoverySampleSynthesis("audit-state", cleanEvidence)
				syn.Conflicts = []execjournal.ResearchSynthesisStatement{syn.EvidenceBacked[0]}
				return syn
			}(),
			status: researchaudit.StatusFailed,
			code:   researchaudit.FindingDuplicateClaimIdentity,
		},
		{
			name:     "failed malformed citation",
			evidence: cleanEvidence,
			synthesis: func() *execjournal.ResearchSynthesis {
				syn := recoverySampleSynthesis("audit-state", cleanEvidence)
				syn.EvidenceBacked[0].SourceIDs = []string{""}
				return syn
			}(),
			status: researchaudit.StatusFailed,
			code:   researchaudit.FindingCitationMalformed,
		},
		{
			name: "failed ambiguous evidence",
			evidence: func() *execjournal.ResearchEvidence {
				ev := gatewayComparisonEvidence("audit-state")
				ev.Sources = append(ev.Sources, ev.Sources[0])
				ev.Count = 3
				return ev
			}(),
			synthesis: func() *execjournal.ResearchSynthesis {
				ev := gatewayComparisonEvidence("audit-state")
				ev.Sources = append(ev.Sources, ev.Sources[0])
				ev.Count = 3
				return recoverySampleSynthesis("audit-state", ev)
			}(),
			status: researchaudit.StatusFailed,
			code:   researchaudit.FindingCitationAmbiguous,
		},
		{
			name:     "incomplete legacy plan",
			evidence: gatewayLegacyEvidence("audit-state"),
			synthesis: func() *execjournal.ResearchSynthesis {
				ev := gatewayLegacyEvidence("audit-state")
				syn := recoverySampleSynthesis("audit-state", ev)
				syn.PlanVersion = 0
				return syn
			}(),
			status: researchaudit.StatusIncomplete,
			code:   researchaudit.FindingPlanRevisionUnavailable,
		},
		{
			name:      "unavailable",
			evidence:  cleanEvidence,
			synthesis: nil,
			status:    researchaudit.StatusUnavailable,
			code:      researchaudit.FindingSynthesisUnavailable,
		},
		{
			name:      "incompatible missing evidence",
			evidence:  nil,
			synthesis: cleanSynthesis,
			status:    researchaudit.StatusIncompatible,
			code:      researchaudit.FindingEvidenceUnavailable,
		},
		{
			name:      "incompatible legacy evidence",
			evidence:  gatewayLegacyEvidenceV1("audit-state"),
			synthesis: cleanSynthesis,
			status:    researchaudit.StatusIncompatible,
			code:      researchaudit.FindingProvenanceUnavailable,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := gatewayAuditRecord("audit-state", "", tc.evidence, tc.synthesis)
			audit, err := citationAuditForRecord("audit-state", rec)
			if err != nil {
				t.Fatalf("citationAuditForRecord: %v", err)
			}
			if audit.Status != tc.status {
				t.Fatalf("status = %q, want %q; findings=%+v", audit.Status, tc.status, audit.Findings)
			}
			if tc.code != "" && !auditHasFinding(audit, tc.code) {
				t.Fatalf("findings = %+v, want %s", audit.Findings, tc.code)
			}
		})
	}
}

func gatewayLegacyEvidenceV1(executionID string) *execjournal.ResearchEvidence {
	ev := gatewayComparisonEvidence(executionID)
	ev.Version = execjournal.ResearchEvidenceLegacyVersion
	ev.Attribution = ""
	ev.Plan = nil
	ev.Steps = nil
	ev.Operations = nil
	for i := range ev.Sources {
		ev.Sources[i].ID = ""
	}
	return ev
}
