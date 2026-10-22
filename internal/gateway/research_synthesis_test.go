package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Nomssky/NEXUS/internal/execjournal"
)

func recoverySampleSynthesis(executionID string, evidence *execjournal.ResearchEvidence) *execjournal.ResearchSynthesis {
	return &execjournal.ResearchSynthesis{
		Version:     execjournal.ResearchSynthesisVersion,
		Method:      execjournal.ResearchSynthesisMethod,
		ExecutionID: executionID,
		Status:      execjournal.ResearchSynthesisStatusComplete,
		Summary:     "Synthesized from 2 captured source(s) in this execution.",
		EvidenceBacked: []execjournal.ResearchSynthesisStatement{{
			Statement: "The captured source records (2) describe the requested subject.",
			SourceIDs: []string{"src-1", "src-2"},
		}},
		Inferences: []execjournal.ResearchSynthesisInference{{
			Statement: "The runtime boundary between model proposals and effects holds.",
		}},
		Conflicts:           []execjournal.ResearchSynthesisStatement{},
		UnresolvedQuestions: []string{},
		EvidenceFingerprint: execjournal.ResearchEvidenceFingerprint(evidence),
	}
}

// The durable detail read exposes the persisted synthesis next to the evidence
// and evaluation it describes, so a restarted process can still show the
// validated conclusion.
func TestRecoveryDetailExposesResearchSynthesis(t *testing.T) {
	dir := t.TempDir()
	srv, e, _ := newEvidenceServer(t, dir)
	j := e.ExecutionJournal()
	evidence := recoverySampleEvidence()
	evaluation := recoverySampleEvaluation("exec-evi", evidence)
	synthesis := recoverySampleSynthesis("exec-evi", evidence)
	if err := j.Accepted("exec-evi", recBiz, "", "user-wide"); err != nil {
		t.Fatalf("seed accepted: %v", err)
	}
	if err := j.TerminalWithResearch("exec-evi", execjournal.StateCompleted, "", evidence, evaluation, synthesis); err != nil {
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
	raw, ok := v["research_synthesis"].(map[string]interface{})
	if !ok {
		t.Fatalf("synthesis missing from detail read: %v", v)
	}
	if raw["status"] != execjournal.ResearchSynthesisStatusComplete {
		t.Fatalf("synthesis status = %v", raw["status"])
	}
	if raw["execution_id"] != "exec-evi" {
		t.Fatalf("synthesis is not bound to the execution: %v", raw["execution_id"])
	}
	if raw["evidence_fingerprint"] != execjournal.ResearchEvidenceFingerprint(evidence) {
		t.Fatalf("synthesis evidence binding = %v", raw["evidence_fingerprint"])
	}
	rows, ok := raw["evidence_backed"].([]interface{})
	if !ok || len(rows) != 1 {
		t.Fatalf("synthesis evidence_backed = %v", raw["evidence_backed"])
	}
	row := rows[0].(map[string]interface{})
	ids, ok := row["source_ids"].([]interface{})
	if !ok || len(ids) != 2 || ids[0] != "src-1" || ids[1] != "src-2" {
		t.Fatalf("synthesis citations = %v", row["source_ids"])
	}
	// The synthesis carries ids and statements only: no titles, URLs or
	// snippets are copied into the durable record.
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"Nexus capability boundary", "docs.example/nexus", "The runtime decides"} {
		if strings.Contains(string(encoded), leaked) {
			t.Fatalf("synthesis leaked source content %q", leaked)
		}
	}
}

// A record written before v59 has no durable synthesis. It stays readable and
// the field is simply absent: nothing is regenerated for it.
func TestRecoveryDetailOmitsSynthesisForLegacyRecord(t *testing.T) {
	dir := t.TempDir()
	srv, e, _ := newEvidenceServer(t, dir)
	j := e.ExecutionJournal()
	evidence := recoverySampleEvidence()
	if err := j.Accepted("exec-evi", recBiz, "", "user-wide"); err != nil {
		t.Fatalf("seed accepted: %v", err)
	}
	if err := j.TerminalWithEvidence("exec-evi", execjournal.StateCompleted, "", evidence); err != nil {
		t.Fatalf("seed terminal: %v", err)
	}

	w := evidenceGet(t, srv, "/api/v1/recovery/executions/exec-evi?business_id="+recBiz, wideHeaders())
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var v map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if _, ok := v["research_synthesis"]; ok {
		t.Fatalf("legacy record must omit research_synthesis, got %v", v["research_synthesis"])
	}
	if _, ok := v["research_evidence"]; !ok {
		t.Fatal("legacy record must still expose its evidence")
	}
}

// The list stays bounded: the synthesis is a detail-read field, like the
// evidence snapshot and the verdict it describes.
func TestRecoveryListOmitsResearchSynthesis(t *testing.T) {
	dir := t.TempDir()
	srv, e, _ := newEvidenceServer(t, dir)
	j := e.ExecutionJournal()
	evidence := recoverySampleEvidence()
	evaluation := recoverySampleEvaluation("exec-evi", evidence)
	synthesis := recoverySampleSynthesis("exec-evi", evidence)
	if err := j.Accepted("exec-evi", recBiz, "", "user-wide"); err != nil {
		t.Fatalf("seed accepted: %v", err)
	}
	if err := j.TerminalWithResearch("exec-evi", execjournal.StateCompleted, "", evidence, evaluation, synthesis); err != nil {
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
		if _, ok := row["research_synthesis"]; ok {
			t.Fatalf("list must not expose the synthesis, got %v", row["research_synthesis"])
		}
	}
}
