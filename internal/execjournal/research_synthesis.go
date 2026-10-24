// Package execjournal provides the durable research synthesis type for NEXUS v59.
//
// The validated typed synthesis (`executor.ResearchSynthesis`) is process-local:
// it lives only for the lifetime of the process that ran the execution. This
// file defines its durable counterpart (`ResearchSynthesis`), which is written
// through the existing execution journal, atomically with the evidence snapshot
// and quality verdict it describes.
//
// The durable synthesis is a snapshot of the validated completion contract, not
// a regeneration. It carries only what the backend validator already accepted:
// status, summary, evidence-backed statements with execution-scoped citations,
// inferences, conflicts and unresolved questions. It never carries raw model
// output, prompts, hidden reasoning, credentials or transport data.
//
// Every durable synthesis is bound to exactly one execution id and one evidence
// snapshot (via the evidence fingerprint). The journal validates this binding
// at write time and at decode time, so a synthesis can never be read against
// the wrong evidence or under the wrong execution id.
package execjournal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// ResearchSynthesisVersion is the durable synthesis schema version written
// today. It is independent of SchemaVersion, ResearchEvidenceVersion and
// ResearchEvaluationVersion. An unsupported version fails closed.
const ResearchSynthesisVersion = 1

// Completion statuses, mirroring executor.ResearchSynthesis. execjournal
// cannot import executor (executor imports execjournal), so the closed
// vocabulary is defined here and kept consistent with the executor's values.
const (
	ResearchSynthesisStatusComplete     = "complete"
	ResearchSynthesisStatusPartial      = "partial"
	ResearchSynthesisStatusInconclusive = "inconclusive"
)

// ResearchSynthesisMethod is the only writer of version 1. It identifies the
// deterministic, validator-backed synthesis, not a raw model completion.
const ResearchSynthesisMethod = "deterministic_v1"

// Bounds mirror internal/executor/synthesis.go. execjournal cannot import
// executor (executor imports execjournal), so these constants are defined here
// and kept consistent with the executor's validation limits.
const (
	MaxResearchSynthesisSummary      = 4000
	MaxResearchSynthesisStatements   = 12
	MaxResearchSynthesisInferences   = 12
	MaxResearchSynthesisConflicts    = 8
	MaxResearchSynthesisQuestions    = 8
	MaxResearchSynthesisStatementLen = 2000
	MaxResearchSynthesisQuestionLen  = 1000
	MaxResearchSynthesisCitationRefs = 12
	MaxResearchSynthesisPlanVersion  = 1_000_000 // mirrors MaxResearchProvenancePlanVersion

	MaxResearchSynthesisFindings = 256 // reserved; the durable synthesis carries no findings
	MaxResearchSynthesisText     = 512 // reserved
	MaxResearchSynthesisCode     = 64  // reserved
)

// ResearchSynthesisStatement is one claim plus the execution-scoped source ids
// that support it. SourceIDs are validated against the current execution's
// evidence; unknown, malformed or duplicate ids fail closed.
type ResearchSynthesisStatement struct {
	Statement string   `json:"statement"`
	SourceIDs []string `json:"source_ids"`
}

// ResearchSynthesisInference is a model conclusion that is not itself a source
// claim.
type ResearchSynthesisInference struct {
	Statement string `json:"statement"`
}

// ResearchSynthesis is the durable, versioned representation of a validated
// typed synthesis. ExecutionID binds it to exactly one execution record and
// EvidenceFingerprint binds it to the exact evidence snapshot it was validated
// against.
type ResearchSynthesis struct {
	Version int    `json:"version"`
	Method  string `json:"method"`
	// ExecutionID binds the synthesis to exactly one execution record.
	ExecutionID string `json:"execution_id"`
	// Status is the completion status: complete, partial or inconclusive.
	Status string `json:"status"`
	// Summary is the bounded model-authored overview. Rendered as text only.
	Summary string `json:"summary"`
	// EvidenceBacked statements must cite at least one source id that resolves
	// in this execution's evidence snapshot.
	EvidenceBacked []ResearchSynthesisStatement `json:"evidence_backed"`
	// Inferences are explicitly model reasoning, not source-backed claims.
	Inferences []ResearchSynthesisInference `json:"inferences"`
	// Conflicts record disagreements the captured sources support.
	Conflicts []ResearchSynthesisStatement `json:"conflicts"`
	// UnresolvedQuestions are gaps the execution could not settle.
	UnresolvedQuestions []string `json:"unresolved_questions"`
	// EvidenceFingerprint binds the synthesis to the exact evidence snapshot
	// used during validation. SHA-256 over the normalized evidence JSON.
	EvidenceFingerprint string `json:"evidence_fingerprint"`
	// PlanVersion is the plan version active when this synthesis was validated.
	// It must equal evidence.Plan.Version when a plan snapshot was captured;
	// the journal enforces this binding at write and decode time.
	PlanVersion int `json:"plan_version"`
}

// MarshalJSON makes required collection fields serialize as arrays even when a
// producer left the Go slice nil. The receiver is a value and the operations
// are copied, so marshaling never mutates stored synthesis.
func (s ResearchSynthesis) MarshalJSON() ([]byte, error) {
	type researchSynthesisJSON ResearchSynthesis
	out := researchSynthesisJSON(s)
	if out.EvidenceBacked == nil {
		out.EvidenceBacked = []ResearchSynthesisStatement{}
	}
	if out.Inferences == nil {
		out.Inferences = []ResearchSynthesisInference{}
	}
	if out.Conflicts == nil {
		out.Conflicts = []ResearchSynthesisStatement{}
	}
	if out.UnresolvedQuestions == nil {
		out.UnresolvedQuestions = []string{}
	}
	return json.Marshal(out)
}

// ResearchSynthesisFingerprint is the stable identity of one validated
// synthesis snapshot. It covers the normalized synthesis JSON. A nil synthesis
// has a fixed fingerprint, not an empty string.
func ResearchSynthesisFingerprint(s *ResearchSynthesis) string {
	if s == nil {
		sum := sha256.Sum256([]byte("nexus.research.synthesis.none.v1"))
		return hex.EncodeToString(sum[:])
	}
	raw, err := json.Marshal(s)
	if err != nil {
		// ResearchSynthesis.MarshalJSON cannot fail for bounded values, but a
		// future field could. An empty value is rejected by validation rather
		// than silently matching another record.
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ValidateResearchSynthesisForExecution checks a durable synthesis at the
// write and decode boundaries. It binds the synthesis to the record id and the
// evidence snapshot, enforces shape, bounds and status semantics, and resolves
// every citation against the source ids captured by THIS execution.
//
// evidence may be nil only when synthesis is nil. A non-nil synthesis requires
// a non-nil evidence snapshot whose fingerprint matches.
func ValidateResearchSynthesisForExecution(executionID string, synthesis *ResearchSynthesis, evidence *ResearchEvidence) error {
	if synthesis == nil {
		return nil
	}
	if synthesis.Version != ResearchSynthesisVersion {
		return fmt.Errorf("execjournal: unsupported research synthesis version %d", synthesis.Version)
	}
	if synthesis.Method != ResearchSynthesisMethod {
		return fmt.Errorf("execjournal: unsupported research synthesis method %q", synthesis.Method)
	}
	if executionID == "" || synthesis.ExecutionID == "" || synthesis.ExecutionID != executionID {
		return fmt.Errorf("execjournal: research synthesis does not belong to execution %q", executionID)
	}
	if evidence == nil {
		return fmt.Errorf("execjournal: research synthesis requires an evidence snapshot")
	}
	if !validFingerprint(synthesis.EvidenceFingerprint) {
		return fmt.Errorf("execjournal: research synthesis has an invalid evidence fingerprint")
	}
	if synthesis.EvidenceFingerprint != ResearchEvidenceFingerprint(evidence) {
		return fmt.Errorf("execjournal: research synthesis does not match its evidence snapshot")
	}
	if !validResearchSynthesisStatus(synthesis.Status) {
		return fmt.Errorf("execjournal: research synthesis has invalid status %q", synthesis.Status)
	}
	// PlanVersion 0 means the plan revision was not captured (a v2 record
	// written before this field existed). It stays readable and is reported as
	// unavailable rather than invented. A captured version must be in range and
	// must match the evidence snapshot's plan version. A v3 evidence snapshot
	// with a plan requires the binding; a synthesis over evidence with no plan
	// must not claim a plan revision at all.
	if synthesis.PlanVersion != 0 {
		if synthesis.PlanVersion < 1 || synthesis.PlanVersion > MaxResearchSynthesisPlanVersion {
			return fmt.Errorf("execjournal: research synthesis plan version %d out of range", synthesis.PlanVersion)
		}
		if evidence.Plan == nil {
			return fmt.Errorf("execjournal: research synthesis claims a plan version without an evidence plan")
		}
		if synthesis.PlanVersion != evidence.Plan.Version {
			return fmt.Errorf("execjournal: research synthesis plan version %d does not match evidence plan version %d",
				synthesis.PlanVersion, evidence.Plan.Version)
		}
	} else if evidence.Version == ResearchEvidenceVersion && evidence.Plan != nil {
		return fmt.Errorf("execjournal: research synthesis over v3 plan evidence requires a plan version")
	}
	if strings.TrimSpace(synthesis.Summary) == "" {
		return fmt.Errorf("execjournal: research synthesis summary is required")
	}
	if len(synthesis.Summary) > MaxResearchSynthesisSummary {
		return fmt.Errorf("execjournal: research synthesis summary exceeds %d characters", MaxResearchSynthesisSummary)
	}
	if len(synthesis.EvidenceBacked) > MaxResearchSynthesisStatements {
		return fmt.Errorf("execjournal: research synthesis has too many evidence-backed statements")
	}
	if len(synthesis.Inferences) > MaxResearchSynthesisInferences {
		return fmt.Errorf("execjournal: research synthesis has too many inferences")
	}
	if len(synthesis.Conflicts) > MaxResearchSynthesisConflicts {
		return fmt.Errorf("execjournal: research synthesis has too many conflicts")
	}
	if len(synthesis.UnresolvedQuestions) > MaxResearchSynthesisQuestions {
		return fmt.Errorf("execjournal: research synthesis has too many unresolved questions")
	}

	known := make(map[string]struct{}, len(evidence.Sources))
	for _, source := range evidence.Sources {
		if source.ID != "" {
			known[source.ID] = struct{}{}
		}
	}

	seenStatements := make(map[string]struct{}, len(synthesis.EvidenceBacked)+len(synthesis.Conflicts))
	validateStatements := func(kind string, statements []ResearchSynthesisStatement) error {
		for i, statement := range statements {
			text := strings.TrimSpace(statement.Statement)
			if text == "" {
				return fmt.Errorf("%s statement %d is empty", kind, i)
			}
			if len(statement.Statement) > MaxResearchSynthesisStatementLen {
				return fmt.Errorf("%s statement %d exceeds %d characters", kind, i, MaxResearchSynthesisStatementLen)
			}
			if _, dup := seenStatements[text]; dup {
				return fmt.Errorf("%s statement %d duplicates an earlier statement", kind, i)
			}
			seenStatements[text] = struct{}{}
			if len(statement.SourceIDs) == 0 {
				return fmt.Errorf("%s statement %d needs at least one source id", kind, i)
			}
			if len(statement.SourceIDs) > MaxResearchSynthesisCitationRefs {
				return fmt.Errorf("%s statement %d has too many source ids", kind, i)
			}
			seenIDs := make(map[string]struct{}, len(statement.SourceIDs))
			for _, id := range statement.SourceIDs {
				if id == "" || len(id) > MaxResearchSourceID ||
					strings.ContainsAny(id, "\x00\n\r") {
					return fmt.Errorf("%s statement %d has a malformed source id", kind, i)
				}
				if _, dup := seenIDs[id]; dup {
					return fmt.Errorf("%s statement %d repeats source id %q", kind, i, id)
				}
				seenIDs[id] = struct{}{}
				if _, ok := known[id]; !ok {
					return fmt.Errorf("%s statement %d cites unknown source %q", kind, i, id)
				}
			}
		}
		return nil
	}
	if err := validateStatements("evidence-backed", synthesis.EvidenceBacked); err != nil {
		return err
	}
	if err := validateStatements("conflict", synthesis.Conflicts); err != nil {
		return err
	}

	for i, inference := range synthesis.Inferences {
		if strings.TrimSpace(inference.Statement) == "" {
			return fmt.Errorf("inference %d is empty", i)
		}
		if len(inference.Statement) > MaxResearchSynthesisStatementLen {
			return fmt.Errorf("inference %d exceeds %d characters", i, MaxResearchSynthesisStatementLen)
		}
	}
	for i, question := range synthesis.UnresolvedQuestions {
		if strings.TrimSpace(question) == "" {
			return fmt.Errorf("unresolved question %d is empty", i)
		}
		if len(question) > MaxResearchSynthesisQuestionLen {
			return fmt.Errorf("unresolved question %d exceeds %d characters", i, MaxResearchSynthesisQuestionLen)
		}
	}

	grounded := len(synthesis.EvidenceBacked) + len(synthesis.Conflicts)
	switch synthesis.Status {
	case ResearchSynthesisStatusInconclusive:
		if grounded != 0 {
			return fmt.Errorf("inconclusive synthesis cannot carry evidence-backed statements or conflicts")
		}
	case ResearchSynthesisStatusPartial:
		if grounded == 0 {
			return fmt.Errorf("partial synthesis needs at least one cited statement or conflict")
		}
	case ResearchSynthesisStatusComplete:
		if len(synthesis.EvidenceBacked) == 0 {
			return fmt.Errorf("complete synthesis needs at least one evidence-backed statement")
		}
	}
	if evidence.Count == 0 && synthesis.Status != ResearchSynthesisStatusInconclusive {
		return fmt.Errorf("synthesis over zero captured sources must be inconclusive")
	}
	return nil
}

// validResearchSynthesisStatus reports whether status is one of the three
// documented completion statuses.
func validResearchSynthesisStatus(value string) bool {
	switch value {
	case ResearchSynthesisStatusComplete, ResearchSynthesisStatusPartial, ResearchSynthesisStatusInconclusive:
		return true
	default:
		return false
	}
}
