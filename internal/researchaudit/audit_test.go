package researchaudit

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/Nomssky/NEXUS/internal/execjournal"
)

const auditExecutionID = "exec-audit-1"

func auditEvidence(executionID string) *execjournal.ResearchEvidence {
	return &execjournal.ResearchEvidence{
		Version:    execjournal.ResearchEvidenceVersion,
		Capability: execjournal.ResearchEvidenceCapability,
		Operation:  execjournal.ResearchEvidenceOperation,
		Count:      2,
		Sources: []execjournal.ResearchSource{
			{ID: "src-1", Title: "Source one", URL: "https://example.test/one", Source: "Example", Snippet: "First recorded excerpt."},
			{ID: "src-2", Title: "Source two", URL: "https://example.test/two", Source: "Example", Snippet: "Second recorded excerpt."},
		},
		Attribution: execjournal.ResearchAttributionUnavailable,
	}
}

func auditSynthesis(executionID string, evidence *execjournal.ResearchEvidence) *execjournal.ResearchSynthesis {
	return &execjournal.ResearchSynthesis{
		Version:     execjournal.ResearchSynthesisVersion,
		Method:      execjournal.ResearchSynthesisMethod,
		ExecutionID: executionID,
		Status:      execjournal.ResearchSynthesisStatusComplete,
		Summary:     "Synthesized from the recorded source snapshot.",
		EvidenceBacked: []execjournal.ResearchSynthesisStatement{{
			Statement: "The recorded sources describe the subject.",
			SourceIDs: []string{"src-1"},
		}},
		Inferences:          []execjournal.ResearchSynthesisInference{},
		Conflicts:           []execjournal.ResearchSynthesisStatement{},
		UnresolvedQuestions: []string{},
		EvidenceFingerprint: execjournal.ResearchEvidenceFingerprint(evidence),
	}
}

func planEvidence(executionID string, version int) *execjournal.ResearchEvidence {
	ev := auditEvidence(executionID)
	ev.Version = version
	if version == execjournal.ResearchEvidenceProvenanceVersion {
		ev.Attribution = ""
	}
	ev.Plan = &execjournal.ResearchPlanEvidence{PlanID: executionID, Version: 1}
	ev.Steps = []execjournal.ResearchStepEvidence{{StepID: "s1", Status: "completed"}}
	if version == execjournal.ResearchEvidenceVersion {
		ev.Plan.ObjectiveID = "objective-" + executionID
		ev.Plan.Provenance = execjournal.ResearchProvenanceAuthoritative
		ev.Steps[0].Intent = "Search the recorded sources"
		ev.Steps[0].PlanVersion = 1
		ev.Steps[0].Provenance = execjournal.ResearchProvenanceAuthoritative
	}
	return ev
}

func mustBuild(t *testing.T, synthesis *execjournal.ResearchSynthesis, evidence *execjournal.ResearchEvidence) Audit {
	t.Helper()
	audit, err := Build(auditExecutionID, synthesis, evidence)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := Validate(audit); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return audit
}

func findingCodes(a Audit) []string {
	codes := make([]string, 0, len(a.Findings))
	for _, finding := range a.Findings {
		codes = append(codes, finding.Code)
	}
	return codes
}

func hasCode(a Audit, code string) bool {
	for _, finding := range a.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func TestBuildPassedResolvedCitation(t *testing.T) {
	evidence := auditEvidence(auditExecutionID)
	synthesis := auditSynthesis(auditExecutionID, evidence)
	audit := mustBuild(t, synthesis, evidence)

	if audit.Status != StatusPassed {
		t.Fatalf("status = %q, want passed; findings=%v", audit.Status, findingCodes(audit))
	}
	if audit.Provenance != ProvenanceDerived {
		t.Fatalf("provenance = %q", audit.Provenance)
	}
	if len(audit.Claims) != 1 || audit.Claims[0].Status != ClaimPassed {
		t.Fatalf("claims = %+v", audit.Claims)
	}
	claim := audit.Claims[0]
	if claim.CitationRequirement != CitationRequired || claim.Provenance != ProvenanceAuthoritative {
		t.Fatalf("claim metadata = %+v", claim)
	}
	if len(claim.Citations) != 1 {
		t.Fatalf("citations = %+v", claim.Citations)
	}
	citation := claim.Citations[0]
	if citation.Status != CitationResolved || citation.EvidenceStatus != EvidenceAvailable ||
		!citation.ContentAvailable || !citation.MetadataAvailable {
		t.Fatalf("citation = %+v", citation)
	}
	if len(audit.Findings) != 0 {
		t.Fatalf("findings = %+v", audit.Findings)
	}
	if len(audit.Limitations) != len(limitationList) {
		t.Fatalf("limitations = %+v", audit.Limitations)
	}
}

func TestBuildWarningsContentAndMetadataAvailability(t *testing.T) {
	t.Run("content unavailable", func(t *testing.T) {
		evidence := auditEvidence(auditExecutionID)
		evidence.Sources[0].Snippet = ""
		synthesis := auditSynthesis(auditExecutionID, evidence)
		audit := mustBuild(t, synthesis, evidence)
		if audit.Status != StatusWarnings || !hasCode(audit, FindingEvidenceContentUnavailable) {
			t.Fatalf("audit = %+v", audit)
		}
		if audit.Claims[0].Status != ClaimWarnings {
			t.Fatalf("claim = %+v", audit.Claims[0])
		}
		if audit.Claims[0].Citations[0].ContentAvailable {
			t.Fatal("content must be unavailable")
		}
	})

	t.Run("metadata unavailable", func(t *testing.T) {
		evidence := auditEvidence(auditExecutionID)
		evidence.Sources[0].Title = ""
		evidence.Sources[0].URL = ""
		evidence.Sources[0].Source = ""
		synthesis := auditSynthesis(auditExecutionID, evidence)
		audit := mustBuild(t, synthesis, evidence)
		if audit.Status != StatusWarnings || !hasCode(audit, FindingEvidenceMetadataUnavailable) {
			t.Fatalf("audit = %+v", audit)
		}
		if audit.Claims[0].Citations[0].MetadataAvailable {
			t.Fatal("metadata must be unavailable")
		}
	})
}

func TestBuildCitationFailures(t *testing.T) {
	cases := []struct {
		name       string
		sourceIDs  []string
		wantCode   string
		wantStatus string
	}{
		{name: "missing", sourceIDs: nil, wantCode: FindingCitationMissing, wantStatus: CitationUnresolved},
		{name: "malformed empty", sourceIDs: []string{""}, wantCode: FindingCitationMalformed, wantStatus: CitationMalformed},
		{name: "malformed control", sourceIDs: []string{"src\n1"}, wantCode: FindingCitationMalformed, wantStatus: CitationMalformed},
		{name: "duplicate", sourceIDs: []string{"src-1", "src-1"}, wantCode: FindingCitationDuplicate, wantStatus: CitationDuplicate},
		{name: "unresolved", sourceIDs: []string{"src-missing"}, wantCode: FindingCitationUnresolved, wantStatus: CitationUnresolved},
		{name: "cross execution", sourceIDs: []string{"src-from-another-execution"}, wantCode: FindingCitationUnresolved, wantStatus: CitationUnresolved},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evidence := auditEvidence(auditExecutionID)
			synthesis := auditSynthesis(auditExecutionID, evidence)
			synthesis.EvidenceBacked[0].SourceIDs = tc.sourceIDs
			audit := mustBuild(t, synthesis, evidence)
			if audit.Status != StatusFailed {
				t.Fatalf("status = %q, want failed; findings=%v", audit.Status, findingCodes(audit))
			}
			if !hasCode(audit, tc.wantCode) {
				t.Fatalf("findings = %v, want %s", findingCodes(audit), tc.wantCode)
			}
			if audit.Claims[0].Status != ClaimFailed {
				t.Fatalf("claim status = %q", audit.Claims[0].Status)
			}
			if tc.wantStatus == CitationUnresolved && tc.name == "missing" {
				return
			}
			if len(audit.Claims[0].Citations) == 0 {
				if tc.name != "missing" {
					t.Fatalf("citations = %+v", audit.Claims[0].Citations)
				}
				return
			}
			last := audit.Claims[0].Citations[len(audit.Claims[0].Citations)-1]
			if last.Status != tc.wantStatus {
				t.Fatalf("citation status = %q, want %q", last.Status, tc.wantStatus)
			}
		})
	}
}

func TestBuildAmbiguousSourceIdentity(t *testing.T) {
	evidence := auditEvidence(auditExecutionID)
	evidence.Sources = append(evidence.Sources, evidence.Sources[0])
	evidence.Count = 3
	synthesis := auditSynthesis(auditExecutionID, evidence)
	audit := mustBuild(t, synthesis, evidence)
	if audit.Status != StatusFailed {
		t.Fatalf("status = %q", audit.Status)
	}
	if !hasCode(audit, FindingEvidenceInvalid) || !hasCode(audit, FindingCitationAmbiguous) {
		t.Fatalf("findings = %v", findingCodes(audit))
	}
	citation := audit.Claims[0].Citations[0]
	if citation.Status != CitationAmbiguous || citation.EvidenceStatus != EvidenceAmbiguous {
		t.Fatalf("citation = %+v", citation)
	}
}

func TestBuildSameURLDifferentSourceIdentity(t *testing.T) {
	evidence := auditEvidence(auditExecutionID)
	evidence.Sources[0].URL = "https://example.test/shared"
	evidence.Sources[1].URL = "https://example.test/shared"
	evidence.Sources[0].Snippet = ""
	synthesis := auditSynthesis(auditExecutionID, evidence)
	synthesis.EvidenceBacked[0].SourceIDs = []string{"src-2"}
	audit := mustBuild(t, synthesis, evidence)
	if audit.Status != StatusPassed {
		t.Fatalf("audit = %+v", audit)
	}
	if !audit.Claims[0].Citations[0].ContentAvailable {
		t.Fatal("src-2 content must be available; URL equality must not select src-1")
	}
}

func TestBuildLegacyAndIncompleteProvenance(t *testing.T) {
	t.Run("v2 no plan", func(t *testing.T) {
		evidence := auditEvidence(auditExecutionID)
		evidence.Version = execjournal.ResearchEvidenceProvenanceVersion
		evidence.Attribution = ""
		synthesis := auditSynthesis(auditExecutionID, evidence)
		audit := mustBuild(t, synthesis, evidence)
		if audit.Status != StatusWarnings {
			t.Fatalf("status = %q; findings=%v", audit.Status, findingCodes(audit))
		}
		if !hasCode(audit, FindingLegacyProvenance) {
			t.Fatalf("findings = %v", findingCodes(audit))
		}
		if audit.Claims[0].Provenance != ProvenanceLegacy {
			t.Fatalf("claim provenance = %q", audit.Claims[0].Provenance)
		}
	})

	t.Run("v2 plan revision unavailable", func(t *testing.T) {
		evidence := planEvidence(auditExecutionID, execjournal.ResearchEvidenceProvenanceVersion)
		synthesis := auditSynthesis(auditExecutionID, evidence)
		synthesis.PlanVersion = 0
		audit := mustBuild(t, synthesis, evidence)
		if audit.Status != StatusIncomplete {
			t.Fatalf("status = %q; findings=%v", audit.Status, findingCodes(audit))
		}
		if !hasCode(audit, FindingPlanRevisionUnavailable) {
			t.Fatalf("findings = %v", findingCodes(audit))
		}
		if audit.Claims[0].Status != ClaimWarnings {
			t.Fatalf("claim = %+v", audit.Claims[0])
		}
	})

	t.Run("v2 plan revision bound", func(t *testing.T) {
		evidence := planEvidence(auditExecutionID, execjournal.ResearchEvidenceProvenanceVersion)
		synthesis := auditSynthesis(auditExecutionID, evidence)
		synthesis.PlanVersion = 1
		audit := mustBuild(t, synthesis, evidence)
		if audit.Status != StatusWarnings {
			t.Fatalf("status = %q; findings=%v", audit.Status, findingCodes(audit))
		}
		if hasCode(audit, FindingPlanRevisionUnavailable) {
			t.Fatal("bound plan revision must not be reported unavailable")
		}
	})

	t.Run("v1 incompatible", func(t *testing.T) {
		evidence := auditEvidence(auditExecutionID)
		evidence.Version = execjournal.ResearchEvidenceLegacyVersion
		evidence.Sources[0].ID = ""
		evidence.Sources[1].ID = ""
		synthesis := auditSynthesis(auditExecutionID, evidence)
		audit := mustBuild(t, synthesis, evidence)
		if audit.Status != StatusIncompatible || !hasCode(audit, FindingProvenanceUnavailable) {
			t.Fatalf("audit = %+v", audit)
		}
		if audit.Claims[0].Status != ClaimUnavailable {
			t.Fatalf("claim = %+v", audit.Claims[0])
		}
	})
}

func TestBuildUnavailableAndIncompatible(t *testing.T) {
	t.Run("no synthesis", func(t *testing.T) {
		evidence := auditEvidence(auditExecutionID)
		audit := mustBuild(t, nil, evidence)
		if audit.Status != StatusUnavailable || !hasCode(audit, FindingSynthesisUnavailable) {
			t.Fatalf("audit = %+v", audit)
		}
		if len(audit.Claims) != 0 {
			t.Fatalf("claims = %+v", audit.Claims)
		}
	})

	t.Run("no evidence", func(t *testing.T) {
		evidence := auditEvidence(auditExecutionID)
		synthesis := auditSynthesis(auditExecutionID, evidence)
		audit := mustBuild(t, synthesis, nil)
		if audit.Status != StatusIncompatible || !hasCode(audit, FindingEvidenceUnavailable) {
			t.Fatalf("audit = %+v", audit)
		}
		if audit.Claims[0].Status != ClaimUnavailable {
			t.Fatalf("claim = %+v", audit.Claims[0])
		}
	})

	t.Run("fingerprint mismatch", func(t *testing.T) {
		evidence := auditEvidence(auditExecutionID)
		synthesis := auditSynthesis(auditExecutionID, evidence)
		synthesis.EvidenceFingerprint = strings.Repeat("a", 64)
		audit := mustBuild(t, synthesis, evidence)
		if audit.Status != StatusIncompatible || !hasCode(audit, FindingSynthesisSnapshotMismatch) {
			t.Fatalf("audit = %+v", audit)
		}
	})

	t.Run("execution mismatch", func(t *testing.T) {
		evidence := auditEvidence(auditExecutionID)
		synthesis := auditSynthesis("exec-other", evidence)
		audit := mustBuild(t, synthesis, evidence)
		if audit.Status != StatusIncompatible || !hasCode(audit, FindingSynthesisSnapshotMismatch) {
			t.Fatalf("audit = %+v", audit)
		}
	})

	t.Run("plan id mismatch", func(t *testing.T) {
		evidence := planEvidence(auditExecutionID, execjournal.ResearchEvidenceVersion)
		evidence.Plan.PlanID = "exec-other"
		synthesis := auditSynthesis(auditExecutionID, evidence)
		synthesis.PlanVersion = 1
		audit := mustBuild(t, synthesis, evidence)
		if audit.Status != StatusIncompatible || !hasCode(audit, FindingEvidenceSnapshotMismatch) {
			t.Fatalf("audit = %+v", audit)
		}
	})

	t.Run("plan version mismatch", func(t *testing.T) {
		evidence := planEvidence(auditExecutionID, execjournal.ResearchEvidenceVersion)
		synthesis := auditSynthesis(auditExecutionID, evidence)
		synthesis.PlanVersion = 2
		audit := mustBuild(t, synthesis, evidence)
		if audit.Status != StatusIncompatible || !hasCode(audit, FindingPlanRevisionMismatch) {
			t.Fatalf("audit = %+v", audit)
		}
	})

	t.Run("v3 plan revision unavailable", func(t *testing.T) {
		evidence := planEvidence(auditExecutionID, execjournal.ResearchEvidenceVersion)
		synthesis := auditSynthesis(auditExecutionID, evidence)
		synthesis.PlanVersion = 0
		audit := mustBuild(t, synthesis, evidence)
		if audit.Status != StatusIncompatible || !hasCode(audit, FindingPlanRevisionUnavailable) {
			t.Fatalf("audit = %+v", audit)
		}
	})

	t.Run("synthesis schema incompatible", func(t *testing.T) {
		evidence := auditEvidence(auditExecutionID)
		synthesis := auditSynthesis(auditExecutionID, evidence)
		synthesis.Version = 2
		audit := mustBuild(t, synthesis, evidence)
		if audit.Status != StatusIncompatible || !hasCode(audit, FindingSynthesisSchemaIncompatible) {
			t.Fatalf("audit = %+v", audit)
		}
	})

	t.Run("evidence schema incompatible", func(t *testing.T) {
		evidence := auditEvidence(auditExecutionID)
		evidence.Version = 99
		synthesis := auditSynthesis(auditExecutionID, evidence)
		audit := mustBuild(t, synthesis, evidence)
		if audit.Status != StatusIncompatible || !hasCode(audit, FindingEvidenceSchemaIncompatible) {
			t.Fatalf("audit = %+v", audit)
		}
	})
}

func TestBuildClaimIdentityFindings(t *testing.T) {
	t.Run("duplicate cited identity", func(t *testing.T) {
		evidence := auditEvidence(auditExecutionID)
		synthesis := auditSynthesis(auditExecutionID, evidence)
		synthesis.EvidenceBacked = append(synthesis.EvidenceBacked, synthesis.EvidenceBacked[0])
		audit := mustBuild(t, synthesis, evidence)
		if audit.Status != StatusFailed || !hasCode(audit, FindingDuplicateClaimIdentity) {
			t.Fatalf("audit = %+v", audit)
		}
		if audit.Claims[1].Status != ClaimFailed {
			t.Fatalf("claim = %+v", audit.Claims[1])
		}
	})

	t.Run("duplicate across evidence-backed and conflict", func(t *testing.T) {
		evidence := auditEvidence(auditExecutionID)
		synthesis := auditSynthesis(auditExecutionID, evidence)
		synthesis.Conflicts = []execjournal.ResearchSynthesisStatement{synthesis.EvidenceBacked[0]}
		audit := mustBuild(t, synthesis, evidence)
		if audit.Status != StatusFailed || !hasCode(audit, FindingDuplicateClaimIdentity) {
			t.Fatalf("audit = %+v", audit)
		}
	})

	t.Run("repeated inference is allowed", func(t *testing.T) {
		evidence := auditEvidence(auditExecutionID)
		synthesis := auditSynthesis(auditExecutionID, evidence)
		synthesis.Inferences = []execjournal.ResearchSynthesisInference{
			{Statement: "A repeated inference."},
			{Statement: "A repeated inference."},
		}
		audit := mustBuild(t, synthesis, evidence)
		if audit.Status != StatusPassed {
			t.Fatalf("audit = %+v", audit)
		}
		if audit.Claims[1].ClaimID == audit.Claims[2].ClaimID {
			t.Fatal("inference identities must include their canonical ordinal")
		}
	})

	t.Run("empty cited claim", func(t *testing.T) {
		evidence := auditEvidence(auditExecutionID)
		synthesis := auditSynthesis(auditExecutionID, evidence)
		synthesis.EvidenceBacked[0].Statement = "   "
		audit := mustBuild(t, synthesis, evidence)
		if audit.Status != StatusFailed || !hasCode(audit, FindingClaimIdentityMalformed) {
			t.Fatalf("audit = %+v", audit)
		}
	})

	t.Run("empty inference", func(t *testing.T) {
		evidence := auditEvidence(auditExecutionID)
		synthesis := auditSynthesis(auditExecutionID, evidence)
		synthesis.Inferences = []execjournal.ResearchSynthesisInference{{Statement: " "}}
		audit := mustBuild(t, synthesis, evidence)
		if audit.Status != StatusFailed || !hasCode(audit, FindingClaimIdentityMalformed) {
			t.Fatalf("audit = %+v", audit)
		}
		if audit.Claims[1].Status != ClaimFailed {
			t.Fatalf("claim = %+v", audit.Claims[1])
		}
	})
}

func TestBuildNoCitedClaims(t *testing.T) {
	evidence := auditEvidence(auditExecutionID)
	synthesis := &execjournal.ResearchSynthesis{
		Version:             execjournal.ResearchSynthesisVersion,
		Method:              execjournal.ResearchSynthesisMethod,
		ExecutionID:         auditExecutionID,
		Status:              execjournal.ResearchSynthesisStatusInconclusive,
		Summary:             "The snapshot did not settle the question.",
		Inferences:          []execjournal.ResearchSynthesisInference{{Statement: "A cautious inference."}},
		UnresolvedQuestions: []string{"What remains unverified?"},
		EvidenceFingerprint: execjournal.ResearchEvidenceFingerprint(evidence),
	}
	audit := mustBuild(t, synthesis, evidence)
	if audit.Status != StatusWarnings || !hasCode(audit, FindingNoCitedClaims) {
		t.Fatalf("audit = %+v", audit)
	}
	for _, claim := range audit.Claims {
		if claim.Status != ClaimNotApplicable || claim.Provenance != ProvenanceNotApplicable || len(claim.Citations) != 0 {
			t.Fatalf("claim = %+v", claim)
		}
	}
}

func TestBuildDeterministicAndReadOnly(t *testing.T) {
	evidence := auditEvidence(auditExecutionID)
	evidence.Sources[0].Snippet = ""
	evidence.Sources[1].Title = ""
	synthesis := auditSynthesis(auditExecutionID, evidence)
	synthesis.EvidenceBacked = append(synthesis.EvidenceBacked, execjournal.ResearchSynthesisStatement{
		Statement: "A second recorded statement.",
		SourceIDs: []string{"src-2", "src-missing"},
	})
	synthesis.Inferences = []execjournal.ResearchSynthesisInference{{Statement: "An inference."}}

	beforeEvidence, _ := json.Marshal(evidence)
	beforeSynthesis, _ := json.Marshal(synthesis)

	first := mustBuild(t, synthesis, evidence)
	second := mustBuild(t, synthesis, evidence)
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatal("audit output is not deterministic")
	}
	afterEvidence, _ := json.Marshal(evidence)
	afterSynthesis, _ := json.Marshal(synthesis)
	if string(beforeEvidence) != string(afterEvidence) || string(beforeSynthesis) != string(afterSynthesis) {
		t.Fatal("Build mutated its inputs")
	}
}

func TestBuildConcurrentDeterminism(t *testing.T) {
	evidence := auditEvidence(auditExecutionID)
	synthesis := auditSynthesis(auditExecutionID, evidence)
	want := mustBuild(t, synthesis, evidence)
	wantJSON, _ := json.Marshal(want)

	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := Build(auditExecutionID, synthesis, evidence)
			if err != nil {
				errs <- err
				return
			}
			gotJSON, _ := json.Marshal(got)
			if string(gotJSON) != string(wantJSON) {
				errs <- jsonError("non-deterministic audit")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

type jsonError string

func (e jsonError) Error() string { return string(e) }

func TestBuildBounds(t *testing.T) {
	evidence := auditEvidence(auditExecutionID)
	t.Run("too many claims", func(t *testing.T) {
		synthesis := auditSynthesis(auditExecutionID, evidence)
		synthesis.Inferences = make([]execjournal.ResearchSynthesisInference, MaxClaims)
		for i := range synthesis.Inferences {
			synthesis.Inferences[i].Statement = "inference"
		}
		if _, err := Build(auditExecutionID, synthesis, evidence); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("too many citations", func(t *testing.T) {
		synthesis := auditSynthesis(auditExecutionID, evidence)
		synthesis.EvidenceBacked[0].SourceIDs = make([]string, MaxCitationsPerClaim+1)
		for i := range synthesis.EvidenceBacked[0].SourceIDs {
			synthesis.EvidenceBacked[0].SourceIDs[i] = "src-1"
		}
		if _, err := Build(auditExecutionID, synthesis, evidence); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("citation reference", func(t *testing.T) {
		synthesis := auditSynthesis(auditExecutionID, evidence)
		synthesis.EvidenceBacked[0].SourceIDs = []string{strings.Repeat("a", MaxCitationReference+1)}
		if _, err := Build(auditExecutionID, synthesis, evidence); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("claim text", func(t *testing.T) {
		synthesis := auditSynthesis(auditExecutionID, evidence)
		synthesis.EvidenceBacked[0].Statement = strings.Repeat("a", MaxClaimText+1)
		if _, err := Build(auditExecutionID, synthesis, evidence); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("question text", func(t *testing.T) {
		synthesis := auditSynthesis(auditExecutionID, evidence)
		synthesis.UnresolvedQuestions = []string{strings.Repeat("a", MaxQuestionText+1)}
		if _, err := Build(auditExecutionID, synthesis, evidence); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func TestBuildTruncatesFindings(t *testing.T) {
	evidence := auditEvidence(auditExecutionID)
	evidence.Count = 1
	evidence.Sources = evidence.Sources[:1]
	synthesis := auditSynthesis(auditExecutionID, evidence)
	synthesis.EvidenceBacked = make([]execjournal.ResearchSynthesisStatement, MaxClaims)
	for i := range synthesis.EvidenceBacked {
		statement := execjournal.ResearchSynthesisStatement{
			Statement: "Unique statement " + strings.Repeat("x", i+1),
			SourceIDs: make([]string, MaxCitationsPerClaim),
		}
		for j := range statement.SourceIDs {
			statement.SourceIDs[j] = "missing-" + strings.Repeat("y", i+1) + "-" + strings.Repeat("z", j+1)
		}
		synthesis.EvidenceBacked[i] = statement
	}
	audit := mustBuild(t, synthesis, evidence)
	if audit.Status != StatusFailed {
		t.Fatalf("status = %q", audit.Status)
	}
	if !audit.Truncated || len(audit.Findings) != MaxFindings {
		t.Fatalf("truncated=%v findings=%d", audit.Truncated, len(audit.Findings))
	}
	if !hasCode(audit, FindingAuditOutputTruncated) {
		t.Fatalf("findings = %v", findingCodes(audit))
	}
}

func TestValidateRejectsTampering(t *testing.T) {
	evidence := auditEvidence(auditExecutionID)
	evidence.Sources[0].Snippet = ""
	evidence.Sources[1].Snippet = ""
	synthesis := auditSynthesis(auditExecutionID, evidence)
	synthesis.EvidenceBacked = append(synthesis.EvidenceBacked, execjournal.ResearchSynthesisStatement{
		Statement: "A second recorded statement.",
		SourceIDs: []string{"src-2"},
	})
	clean := mustBuild(t, synthesis, evidence)
	if len(clean.Findings) < 2 || len(clean.Claims) < 2 {
		t.Fatalf("fixture is too small: findings=%d claims=%d", len(clean.Findings), len(clean.Claims))
	}

	t.Run("status", func(t *testing.T) {
		tampered := clean
		tampered.Status = StatusPassed
		if err := Validate(tampered); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("finding order", func(t *testing.T) {
		tampered := clean
		tampered.Findings = append([]Finding(nil), clean.Findings...)
		if len(tampered.Findings) < 2 {
			t.Fatal("fixture needs at least two findings")
		}
		tampered.Findings[0], tampered.Findings[1] = tampered.Findings[1], tampered.Findings[0]
		if err := Validate(tampered); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("claim order", func(t *testing.T) {
		tampered := clean
		tampered.Claims = append([]Claim(nil), clean.Claims...)
		if len(tampered.Claims) < 2 {
			t.Fatal("fixture needs at least two claims")
		}
		tampered.Claims[0], tampered.Claims[1] = tampered.Claims[1], tampered.Claims[0]
		if err := Validate(tampered); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("citation status", func(t *testing.T) {
		tampered := clean
		tampered.Claims = append([]Claim(nil), clean.Claims...)
		tampered.Claims[0].Citations = append([]Citation(nil), clean.Claims[0].Citations...)
		tampered.Claims[0].Citations[0].Status = CitationResolved
		tampered.Claims[0].Citations[0].EvidenceStatus = EvidenceUnavailable
		if err := Validate(tampered); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func TestValidateRejectsUnknownFinding(t *testing.T) {
	evidence := auditEvidence(auditExecutionID)
	synthesis := auditSynthesis(auditExecutionID, evidence)
	audit := mustBuild(t, synthesis, evidence)
	audit.Findings = append(audit.Findings, Finding{
		Code: "unknown_finding", Severity: SeverityWarning, Message: "Unknown.",
	})
	if err := Validate(audit); err == nil {
		t.Fatal("expected an error")
	}
}
