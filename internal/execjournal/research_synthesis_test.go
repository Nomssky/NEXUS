package execjournal

import (
	"strings"
	"sync"
	"testing"

	"github.com/Nomssky/NEXUS/internal/foundation/store"
)

func synthesisEvidence(executionID string) *ResearchEvidence {
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
		Plan: &ResearchPlanEvidence{
			PlanID: executionID, Version: 1, ObjectiveID: "obj-1",
			CompletionStepID: "s1", Provenance: ResearchProvenanceAuthoritative,
		},
		Steps: []ResearchStepEvidence{{
			StepID: "s1", Intent: "Search for the capability boundary.", PlanVersion: 1,
			Status: "completed", Provenance: ResearchProvenanceAuthoritative,
		}},
		Operations: []ResearchOperationEvidence{{
			OperationID: "obs-1", StepID: "s1", ToolID: ResearchEvidenceCapability,
			Operation: ResearchEvidenceOperation, SourceIDs: []string{"src-1"},
			Provenance: ResearchProvenanceAuthoritative,
		}},
		Attribution: ResearchAttributionDerived,
	}
}

func synthesisFor(executionID string, evidence *ResearchEvidence) *ResearchSynthesis {
	planVersion := 0
	if evidence != nil && evidence.Plan != nil {
		planVersion = evidence.Plan.Version
	}
	return &ResearchSynthesis{
		Version:     ResearchSynthesisVersion,
		Method:      ResearchSynthesisMethod,
		ExecutionID: executionID,
		Status:      ResearchSynthesisStatusComplete,
		Summary:     "Synthesized from the captured source.",
		EvidenceBacked: []ResearchSynthesisStatement{{
			Statement: "The runtime decides and the model proposes.",
			SourceIDs: []string{"src-1"},
		}},
		Inferences:          []ResearchSynthesisInference{},
		Conflicts:           []ResearchSynthesisStatement{},
		UnresolvedQuestions: []string{},
		EvidenceFingerprint: ResearchEvidenceFingerprint(evidence),
		PlanVersion:         planVersion,
	}
}

func TestResearchSynthesisFingerprintDistinguishesSnapshots(t *testing.T) {
	first := synthesisFor("exec-1", synthesisEvidence("exec-1"))
	second := synthesisFor("exec-1", synthesisEvidence("exec-1"))
	if ResearchSynthesisFingerprint(first) != ResearchSynthesisFingerprint(second) {
		t.Fatal("fingerprint must be stable for the same snapshot")
	}
	second.Summary = "A different summary."
	if ResearchSynthesisFingerprint(first) == ResearchSynthesisFingerprint(second) {
		t.Fatal("fingerprint must change with the synthesis")
	}
	if ResearchSynthesisFingerprint(nil) == "" {
		t.Fatal("absent synthesis must have a stable, non-empty fingerprint")
	}
}

func TestValidateResearchSynthesisBindsExecutionAndEvidence(t *testing.T) {
	evidence := synthesisEvidence("exec-1")
	synth := synthesisFor("exec-1", evidence)

	if err := ValidateResearchSynthesisForExecution("exec-1", synth, evidence); err != nil {
		t.Fatalf("valid synthesis rejected: %v", err)
	}

	foreign := synthesisFor("exec-2", evidence)
	foreign.ExecutionID = "exec-2"
	if err := ValidateResearchSynthesisForExecution("exec-1", foreign, evidence); err == nil {
		t.Fatal("a synthesis for another execution must be rejected")
	}

	other := synthesisEvidence("exec-1")
	other.Sources[0].Title = "Changed"
	mismatched := synthesisFor("exec-1", evidence)
	mismatched.EvidenceFingerprint = ResearchEvidenceFingerprint(other)
	if err := ValidateResearchSynthesisForExecution("exec-1", mismatched, evidence); err == nil {
		t.Fatal("a synthesis bound to different evidence must be rejected")
	}

	nilEvidence := synthesisFor("exec-1", nil)
	if err := ValidateResearchSynthesisForExecution("exec-1", nilEvidence, nil); err == nil {
		t.Fatal("a synthesis without evidence must be rejected")
	}

	unknownStatus := synthesisFor("exec-1", evidence)
	unknownStatus.Status = "made_up_status"
	if err := ValidateResearchSynthesisForExecution("exec-1", unknownStatus, evidence); err == nil {
		t.Fatal("an unknown status must be rejected")
	}

	inconclusiveWithClaims := synthesisFor("exec-1", evidence)
	inconclusiveWithClaims.Status = ResearchSynthesisStatusInconclusive
	inconclusiveWithClaims.EvidenceBacked = []ResearchSynthesisStatement{{
		Statement: "claim", SourceIDs: []string{"src-1"},
	}}
	if err := ValidateResearchSynthesisForExecution("exec-1", inconclusiveWithClaims, evidence); err == nil {
		t.Fatal("inconclusive synthesis with claims must be rejected")
	}

	completeWithoutClaims := synthesisFor("exec-1", evidence)
	completeWithoutClaims.Status = ResearchSynthesisStatusComplete
	completeWithoutClaims.EvidenceBacked = nil
	if err := ValidateResearchSynthesisForExecution("exec-1", completeWithoutClaims, evidence); err == nil {
		t.Fatal("complete synthesis without claims must be rejected")
	}

	zeroSourcesNotInconclusive := synthesisFor("exec-zero", synthesisEvidenceZero())
	zeroSourcesNotInconclusive.Status = ResearchSynthesisStatusComplete
	if err := ValidateResearchSynthesisForExecution("exec-zero", zeroSourcesNotInconclusive, synthesisEvidenceZero()); err == nil {
		t.Fatal("synthesis over zero sources must be inconclusive")
	}
}

func synthesisEvidenceZero() *ResearchEvidence {
	return &ResearchEvidence{
		Version:    ResearchEvidenceVersion,
		Capability: ResearchEvidenceCapability,
		Operation:  ResearchEvidenceOperation,
		Count:      0,
		Sources:    []ResearchSource{},
		Plan: &ResearchPlanEvidence{
			PlanID: "exec-zero", Version: 1, ObjectiveID: "obj-zero",
			Provenance: ResearchProvenanceAuthoritative,
		},
		Steps: []ResearchStepEvidence{{
			StepID: "s1", Intent: "Search for the capability boundary.", PlanVersion: 1,
			Status: "completed", Provenance: ResearchProvenanceAuthoritative,
		}},
		Attribution: ResearchAttributionUnavailable,
	}
}

func TestValidateResearchSynthesisPlanBinding(t *testing.T) {
	v3 := synthesisEvidence("exec-1")
	v3.Plan.Version = 2
	v3.Steps[0].PlanVersion = 2

	bound := synthesisFor("exec-1", v3)
	if err := ValidateResearchSynthesisForExecution("exec-1", bound, v3); err != nil {
		t.Fatalf("matching v3 plan binding rejected: %v", err)
	}

	unbound := synthesisFor("exec-1", v3)
	unbound.PlanVersion = 0
	if err := ValidateResearchSynthesisForExecution("exec-1", unbound, v3); err == nil ||
		!strings.Contains(err.Error(), "requires a plan version") {
		t.Fatalf("v3 plan evidence requires the binding, got %v", err)
	}

	mismatch := synthesisFor("exec-1", v3)
	mismatch.PlanVersion = 1
	if err := ValidateResearchSynthesisForExecution("exec-1", mismatch, v3); err == nil ||
		!strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched v3 plan binding must fail, got %v", err)
	}

	v2 := &ResearchEvidence{
		Version: ResearchEvidenceProvenanceVersion, Capability: ResearchEvidenceCapability,
		Operation: ResearchEvidenceOperation, Count: 1,
		Sources: []ResearchSource{{ID: "src-1", Title: "Legacy", URL: "https://legacy.example"}},
		Plan:    &ResearchPlanEvidence{PlanID: "exec-1", Version: 2},
		Steps:   []ResearchStepEvidence{{StepID: "s1", Status: "completed"}},
		Operations: []ResearchOperationEvidence{{
			OperationID: "obs-1", StepID: "s1", ToolID: ResearchEvidenceCapability,
			Operation: ResearchEvidenceOperation, SourceIDs: []string{"src-1"},
		}},
	}
	legacyUnbound := synthesisFor("exec-1", v2)
	legacyUnbound.PlanVersion = 0
	if err := ValidateResearchSynthesisForExecution("exec-1", legacyUnbound, v2); err != nil {
		t.Fatalf("v2 synthesis without a captured revision must stay readable: %v", err)
	}
	legacyBound := synthesisFor("exec-1", v2)
	if err := ValidateResearchSynthesisForExecution("exec-1", legacyBound, v2); err != nil {
		t.Fatalf("matching v2 plan binding rejected: %v", err)
	}
	legacyMismatch := synthesisFor("exec-1", v2)
	legacyMismatch.PlanVersion = 1
	if err := ValidateResearchSynthesisForExecution("exec-1", legacyMismatch, v2); err == nil {
		t.Fatal("mismatched v2 plan binding must fail")
	}

	sourceOnly := synthesisEvidence("exec-1")
	sourceOnly.Plan, sourceOnly.Steps, sourceOnly.Operations = nil, nil, nil
	sourceOnly.Attribution = ResearchAttributionUnavailable
	noPlan := synthesisFor("exec-1", sourceOnly)
	noPlan.PlanVersion = 1
	if err := ValidateResearchSynthesisForExecution("exec-1", noPlan, sourceOnly); err == nil ||
		!strings.Contains(err.Error(), "without an evidence plan") {
		t.Fatalf("a claimed revision without an evidence plan must fail, got %v", err)
	}
}

func TestValidateResearchSynthesisBounds(t *testing.T) {
	evidence := synthesisEvidence("exec-1")

	longStatement := strings.Repeat("a", MaxResearchSynthesisStatementLen+1)
	longSynth := synthesisFor("exec-1", evidence)
	longSynth.EvidenceBacked = []ResearchSynthesisStatement{{Statement: longStatement, SourceIDs: []string{"src-1"}}}
	if err := ValidateResearchSynthesisForExecution("exec-1", longSynth, evidence); err == nil {
		t.Fatal("oversized statement must be rejected")
	}

	emptyStatement := synthesisFor("exec-1", evidence)
	emptyStatement.EvidenceBacked = []ResearchSynthesisStatement{{Statement: "", SourceIDs: []string{"src-1"}}}
	if err := ValidateResearchSynthesisForExecution("exec-1", emptyStatement, evidence); err == nil {
		t.Fatal("empty statement must be rejected")
	}

	duplicateStatement := synthesisFor("exec-1", evidence)
	dup := "The runtime decides and the model proposes."
	duplicateStatement.EvidenceBacked = []ResearchSynthesisStatement{
		{Statement: dup, SourceIDs: []string{"src-1"}},
		{Statement: dup, SourceIDs: []string{"src-1"}},
	}
	if err := ValidateResearchSynthesisForExecution("exec-1", duplicateStatement, evidence); err == nil {
		t.Fatal("duplicate statement must be rejected")
	}

	unknownCitation := synthesisFor("exec-1", evidence)
	unknownCitation.EvidenceBacked = []ResearchSynthesisStatement{{Statement: "claim", SourceIDs: []string{"src-9"}}}
	if err := ValidateResearchSynthesisForExecution("exec-1", unknownCitation, evidence); err == nil {
		t.Fatal("unknown citation must be rejected")
	}

	malformedID := synthesisFor("exec-1", evidence)
	malformedID.EvidenceBacked = []ResearchSynthesisStatement{{Statement: "claim", SourceIDs: []string{"\n"}}}
	if err := ValidateResearchSynthesisForExecution("exec-1", malformedID, evidence); err == nil {
		t.Fatal("malformed citation id must be rejected")
	}

	tooManyRefs := synthesisFor("exec-1", evidence)
	refs := make([]string, MaxResearchSynthesisCitationRefs+1)
	for i := range refs {
		refs[i] = "src-1"
	}
	tooManyRefs.EvidenceBacked = []ResearchSynthesisStatement{{Statement: "claim", SourceIDs: refs}}
	if err := ValidateResearchSynthesisForExecution("exec-1", tooManyRefs, evidence); err == nil {
		t.Fatal("too many citation refs must be rejected")
	}

	noRefs := synthesisFor("exec-1", evidence)
	noRefs.EvidenceBacked = []ResearchSynthesisStatement{{Statement: "claim", SourceIDs: nil}}
	if err := ValidateResearchSynthesisForExecution("exec-1", noRefs, evidence); err == nil {
		t.Fatal("statement with no citations must be rejected")
	}
}

func TestTerminalWithResearchPersistsSynthesisAtomically(t *testing.T) {
	mem := store.NewMemStore()
	j := New(mem, fixedClock())
	if err := j.Accepted("exec-1", "biz-1", "div-1", "actor-1"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	evidence := synthesisEvidence("exec-1")
	synth := synthesisFor("exec-1", evidence)
	if err := j.TerminalWithResearch("exec-1", StateCompleted, "", evidence, nil, synth); err != nil {
		t.Fatalf("terminal: %v", err)
	}

	rec, ok, err := j.Get("exec-1")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if rec.ResearchEvidence == nil || rec.ResearchSynthesis == nil {
		t.Fatal("terminal write must persist evidence and synthesis together")
	}
	if rec.ResearchSynthesis.EvidenceFingerprint != ResearchEvidenceFingerprint(rec.ResearchEvidence) {
		t.Fatal("persisted synthesis must stay bound to the persisted evidence")
	}

	restarted := New(mem, fixedClock())
	rec, ok, err = restarted.Get("exec-1")
	if err != nil || !ok {
		t.Fatalf("get after restart: ok=%v err=%v", ok, err)
	}
	if rec.ResearchSynthesis == nil || rec.ResearchSynthesis.Status != ResearchSynthesisStatusComplete {
		t.Fatalf("synthesis did not survive restart: %+v", rec.ResearchSynthesis)
	}
}

func TestTerminalDropsSynthesisThatDoesNotMatchEvidence(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("exec-1", "biz-1", "div-1", "actor-1"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	evidence := synthesisEvidence("exec-1")
	synth := synthesisFor("exec-1", evidence)
	synth.EvidenceFingerprint = strings.Repeat("a", 64)
	if err := j.TerminalWithResearch("exec-1", StateCompleted, "", evidence, nil, synth); err != nil {
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
	if rec.ResearchSynthesis != nil {
		t.Fatalf("a mismatched synthesis must never be persisted: %+v", rec.ResearchSynthesis)
	}
}

func TestSupersedeReplacesEvidenceEvaluationAndSynthesisTogether(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("exec-1", "biz-1", "div-1", "actor-1"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	pauseEvidence := synthesisEvidence("exec-1")
	pauseSynth := synthesisFor("exec-1", pauseEvidence)
	if err := j.TerminalWithResearch("exec-1", StateFailed, ApprovalRequiredClass, pauseEvidence, nil, pauseSynth); err != nil {
		t.Fatalf("pause terminal: %v", err)
	}

	finalEvidence := synthesisEvidence("exec-1")
	finalEvidence.Sources[0].Title = "Final title"
	finalSynth := synthesisFor("exec-1", finalEvidence)
	if err := j.SupersedeApprovedPauseWithResearch("exec-1", StateCompleted, "", finalEvidence, nil, finalSynth); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	rec, _, err := j.Get("exec-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if rec.ResearchEvidence == nil || rec.ResearchEvidence.Sources[0].Title != "Final title" {
		t.Fatalf("final evidence not written: %+v", rec.ResearchEvidence)
	}
	if rec.ResearchSynthesis == nil ||
		rec.ResearchSynthesis.EvidenceFingerprint != ResearchEvidenceFingerprint(finalEvidence) {
		t.Fatalf("final synthesis not written: %+v", rec.ResearchSynthesis)
	}
}

func TestSupersedeKeepsPauseSynthesisWhenResumedRunHasNoEvidence(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("exec-1", "biz-1", "div-1", "actor-1"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	pauseEvidence := synthesisEvidence("exec-1")
	pauseSynth := synthesisFor("exec-1", pauseEvidence)
	if err := j.TerminalWithResearch("exec-1", StateFailed, ApprovalRequiredClass, pauseEvidence, nil, pauseSynth); err != nil {
		t.Fatalf("pause terminal: %v", err)
	}

	resumedSynth := synthesisFor("exec-1", nil)
	if err := j.SupersedeApprovedPauseWithResearch("exec-1", StateCompleted, "", nil, nil, resumedSynth); err != nil {
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
	if rec.ResearchSynthesis == nil ||
		rec.ResearchSynthesis.EvidenceFingerprint != ResearchEvidenceFingerprint(rec.ResearchEvidence) {
		t.Fatalf("the preserved synthesis must stay bound to the preserved evidence: %+v", rec.ResearchSynthesis)
	}
}

func TestSupersedeNewEvidenceDropsUnboundAuxiliaryArtifacts(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("exec-1", "biz-1", "div-1", "actor-1"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	pauseEvidence := synthesisEvidence("exec-1")
	pauseSynth := synthesisFor("exec-1", pauseEvidence)
	pauseEval := evaluationFor("exec-1", pauseEvidence)
	if err := j.TerminalWithResearch("exec-1", StateFailed, ApprovalRequiredClass, pauseEvidence, pauseEval, pauseSynth); err != nil {
		t.Fatalf("pause terminal: %v", err)
	}

	finalEvidence := synthesisEvidence("exec-1")
	finalEvidence.Sources[0].Title = "Replaced evidence"
	// The caller did not supply auxiliary artifacts for the new snapshot. The
	// pause's synthesis and evaluation describe different evidence and must not
	// survive against it merely because the execution id still matches.
	if err := j.SupersedeApprovedPauseWithResearch("exec-1", StateCompleted, "", finalEvidence, nil, nil); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	rec, _, err := j.Get("exec-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if rec.ResearchEvidence == nil || rec.ResearchEvidence.Sources[0].Title != "Replaced evidence" {
		t.Fatalf("final evidence not written: %+v", rec.ResearchEvidence)
	}
	if rec.ResearchSynthesis != nil || rec.ResearchEvaluation != nil {
		t.Fatalf("old auxiliary artifacts must be invalidated: synth=%+v eval=%+v",
			rec.ResearchSynthesis, rec.ResearchEvaluation)
	}
}

func TestTerminalWithResearchWriteFailureLeavesNoDurableClaim(t *testing.T) {
	base := store.NewMemStore()
	failing := evidenceWriteFailStore{Store: base, marker: `"research_evidence"`}
	j := New(failing, fixedClock())
	if err := j.Accepted("exec-fail-research", "biz-1", "", "actor-1"); err != nil {
		t.Fatal(err)
	}
	evidence := synthesisEvidence("exec-fail-research")
	evaluation := evaluationFor("exec-fail-research", evidence)
	synth := synthesisFor("exec-fail-research", evidence)
	if err := j.TerminalWithResearch("exec-fail-research", StateCompleted, "", evidence, evaluation, synth); err == nil {
		t.Fatal("the injected terminal write must fail")
	}
	rec, ok, err := j.Get("exec-fail-research")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if rec.State.Terminal() || rec.CompletedAt != nil || rec.ResearchEvidence != nil ||
		rec.ResearchEvaluation != nil || rec.ResearchSynthesis != nil {
		t.Fatalf("failed write must not claim durable completion: %+v", rec)
	}
}

func TestTerminalSynthesisIsFirstWinsUnderConcurrency(t *testing.T) {
	j := New(store.NewMemStore(), fixedClock())
	if err := j.Accepted("exec-1", "biz-1", "div-1", "actor-1"); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	evidence := synthesisEvidence("exec-1")
	synth := synthesisFor("exec-1", evidence)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = j.TerminalWithResearch("exec-1", StateCompleted, "", evidence, nil, synth)
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
	if rec.ResearchSynthesis == nil {
		t.Fatal("synthesis must be present after a concurrent terminal write")
	}
}
