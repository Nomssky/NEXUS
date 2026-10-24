package executor

import (
	"fmt"
	"strings"

	"github.com/Nomssky/NEXUS/internal/execjournal"
)

// ResearchSynthesis is the typed, evidence-grounded completion contract for a
// research execution. It deliberately carries no titles, URLs or other source
// metadata: the model may only cite stable execution-scoped source ids that
// already resolve in the current execution's validated durable evidence. The
// UI resolves those ids against the authoritative source records; a model can
// never alter what a source is.
//
// The contract is process-local on the live path: it is attached to the
// terminal executor outcome and the process-local execution response.
//
// The validated synthesis is ALSO persisted, as a durable snapshot, through the
// execution journal (v59). The durable copy is written atomically with the
// evidence snapshot and the quality verdict it describes, and is bound to that
// exact evidence by fingerprint. After a restart the UI reads the persisted
// synthesis back from the durable record; it is never regenerated from
// evidence or prose, and a record without one says so.
type ResearchSynthesis struct {
	Status string `json:"status"`
	// Summary is the bounded model-authored overview. It is rendered as text,
	// never linkified and never treated as source metadata.
	Summary string `json:"summary"`
	// EvidenceBacked statements must cite at least one source id that resolves
	// in this execution.
	EvidenceBacked []ResearchEvidenceStatement `json:"evidence_backed"`
	// Inferences are explicitly model reasoning, not source-backed claims.
	Inferences []ResearchInference `json:"inferences"`
	// Conflicts record disagreements the captured sources actually support.
	Conflicts []ResearchEvidenceStatement `json:"conflicts"`
	// UnresolvedQuestions are gaps the execution could not settle.
	UnresolvedQuestions []string `json:"unresolved_questions"`
	// PlanVersion is the plan version active when this synthesis was validated.
	// The runtime stamps it; the model never sets it. The durable journal
	// enforces binding to evidence.Plan.Version at persistence time.
	PlanVersion int `json:"plan_version"`
}

// ResearchEvidenceStatement is one claim plus the execution-scoped source ids
// that support it. SourceIDs are validated against the current execution's
// evidence; unknown, malformed or duplicate ids fail closed.
type ResearchEvidenceStatement struct {
	Statement string   `json:"statement"`
	SourceIDs []string `json:"source_ids"`
}

// ResearchInference is a model conclusion that is not itself a source claim.
type ResearchInference struct {
	Statement string `json:"statement"`
}

const (
	ResearchSynthesisComplete     = "complete"
	ResearchSynthesisPartial      = "partial"
	ResearchSynthesisInconclusive = "inconclusive"

	MaxResearchSynthesisSummary      = 4000
	MaxResearchSynthesisStatements   = 12
	MaxResearchSynthesisInferences   = 12
	MaxResearchSynthesisConflicts    = 8
	MaxResearchSynthesisQuestions    = 8
	MaxResearchSynthesisStatementLen = 2000
	MaxResearchSynthesisQuestionLen  = 1000
	MaxResearchSynthesisCitationRefs = 12
)

// ValidateResearchSynthesis is the single authority boundary for a model's
// synthesized answer. It checks the typed shape, bounds and status semantics,
// then resolves every citation against the source ids captured by THIS
// execution. Evidence records from another execution are not accepted because
// they are not in ev.
//
// v2 and v3 evidence are both accepted because both carry stable source ids.
// When the evidence already carries a finalized plan snapshot, a captured
// synthesis plan revision must match it exactly; a v3 plan snapshot requires
// the binding to be present. The runtime stamps PlanVersion before calling this
// validator, so the model can never choose the revision.
//
// partial is the runtime's verdict that some plan steps did not complete. A
// model may not claim a complete synthesis over a partial execution. When the
// execution captured no sources, only an inconclusive synthesis is honest.
func ValidateResearchSynthesis(s *ResearchSynthesis, ev *execjournal.ResearchEvidence, partial bool) error {
	if s == nil {
		return fmt.Errorf("synthesis is required when evidence was captured")
	}
	if ev == nil {
		return fmt.Errorf("synthesis requires a captured evidence snapshot")
	}
	if ev.Version != execjournal.ResearchEvidenceVersion &&
		ev.Version != execjournal.ResearchEvidenceProvenanceVersion {
		return fmt.Errorf("synthesis requires v2 or v3 evidence with stable source ids")
	}
	// Validate the source rows without their in-progress provenance: at
	// completion the plan/step snapshot has not necessarily been finalized yet.
	// The evidence-level attribution is also a finalization fact, so the base
	// copy must use the value that matches "no operations" for its schema.
	base := &execjournal.ResearchEvidence{
		Version: ev.Version, Capability: ev.Capability, Operation: ev.Operation,
		Count: ev.Count, Sources: ev.Sources,
	}
	if ev.Version == execjournal.ResearchEvidenceVersion {
		base.Attribution = execjournal.ResearchAttributionUnavailable
	}
	if err := execjournal.ValidateResearchEvidence(base); err != nil {
		return fmt.Errorf("invalid evidence: %w", err)
	}
	if ev.Count < 0 || ev.Count != len(ev.Sources) {
		return fmt.Errorf("invalid evidence count")
	}
	if s.PlanVersion < 0 || s.PlanVersion > execjournal.MaxResearchSynthesisPlanVersion {
		return fmt.Errorf("invalid synthesis plan version")
	}
	if ev.Plan != nil && s.PlanVersion != 0 && s.PlanVersion != ev.Plan.Version {
		return fmt.Errorf("synthesis plan version does not match the evidence plan")
	}
	if ev.Version == execjournal.ResearchEvidenceVersion && ev.Plan != nil && s.PlanVersion == 0 {
		return fmt.Errorf("synthesis over v3 plan evidence requires a plan version")
	}

	switch s.Status {
	case ResearchSynthesisComplete, ResearchSynthesisPartial, ResearchSynthesisInconclusive:
	default:
		return fmt.Errorf("unknown synthesis status %q", s.Status)
	}
	if strings.TrimSpace(s.Summary) == "" {
		return fmt.Errorf("synthesis summary is required")
	}
	if len(s.Summary) > MaxResearchSynthesisSummary {
		return fmt.Errorf("synthesis summary exceeds %d characters", MaxResearchSynthesisSummary)
	}
	if len(s.EvidenceBacked) > MaxResearchSynthesisStatements {
		return fmt.Errorf("synthesis has too many evidence-backed statements")
	}
	if len(s.Inferences) > MaxResearchSynthesisInferences {
		return fmt.Errorf("synthesis has too many inferences")
	}
	if len(s.Conflicts) > MaxResearchSynthesisConflicts {
		return fmt.Errorf("synthesis has too many conflicts")
	}
	if len(s.UnresolvedQuestions) > MaxResearchSynthesisQuestions {
		return fmt.Errorf("synthesis has too many unresolved questions")
	}

	known := make(map[string]struct{}, len(ev.Sources))
	for _, source := range ev.Sources {
		if source.ID != "" {
			known[source.ID] = struct{}{}
		}
	}

	seenStatements := make(map[string]struct{}, len(s.EvidenceBacked)+len(s.Conflicts))
	validateStatements := func(kind string, statements []ResearchEvidenceStatement) error {
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
				if !validSynthesisID(id) {
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
	if err := validateStatements("evidence-backed", s.EvidenceBacked); err != nil {
		return err
	}
	if err := validateStatements("conflict", s.Conflicts); err != nil {
		return err
	}

	for i, inference := range s.Inferences {
		if strings.TrimSpace(inference.Statement) == "" {
			return fmt.Errorf("inference %d is empty", i)
		}
		if len(inference.Statement) > MaxResearchSynthesisStatementLen {
			return fmt.Errorf("inference %d exceeds %d characters", i, MaxResearchSynthesisStatementLen)
		}
	}
	for i, question := range s.UnresolvedQuestions {
		if strings.TrimSpace(question) == "" {
			return fmt.Errorf("unresolved question %d is empty", i)
		}
		if len(question) > MaxResearchSynthesisQuestionLen {
			return fmt.Errorf("unresolved question %d exceeds %d characters", i, MaxResearchSynthesisQuestionLen)
		}
	}

	grounded := len(s.EvidenceBacked) + len(s.Conflicts)
	switch s.Status {
	case ResearchSynthesisInconclusive:
		if grounded != 0 {
			return fmt.Errorf("inconclusive synthesis cannot carry evidence-backed statements or conflicts")
		}
	case ResearchSynthesisPartial:
		if grounded == 0 {
			return fmt.Errorf("partial synthesis needs at least one cited statement or conflict")
		}
	case ResearchSynthesisComplete:
		if len(s.EvidenceBacked) == 0 {
			return fmt.Errorf("complete synthesis needs at least one evidence-backed statement")
		}
		if partial {
			return fmt.Errorf("complete synthesis is not allowed when plan steps did not complete")
		}
	}
	if ev.Count == 0 && s.Status != ResearchSynthesisInconclusive {
		return fmt.Errorf("synthesis over zero captured sources must be inconclusive")
	}
	return nil
}

func validSynthesisID(value string) bool {
	return value != "" && len(value) <= 64 &&
		!strings.ContainsAny(value, "\x00\n\r")
}

// DurableResearchSynthesis converts a validated, process-local synthesis into
// its durable journal representation, bound to the exact evidence snapshot it
// was validated against.
//
// The conversion is a snapshot, not a regeneration: it copies only the fields
// the backend validator already accepted. The evidence fingerprint is computed
// from the same snapshot the journal persists, so the durable synthesis and
// the durable evidence can never describe different captures.
//
// The result is nil when the synthesis is nil. Callers validate the result
// with execjournal.ValidateResearchSynthesisForExecution before persisting it.
func DurableResearchSynthesis(s *ResearchSynthesis, executionID string, evidence *execjournal.ResearchEvidence) *execjournal.ResearchSynthesis {
	if s == nil {
		return nil
	}
	durable := &execjournal.ResearchSynthesis{
		Version:             execjournal.ResearchSynthesisVersion,
		Method:              execjournal.ResearchSynthesisMethod,
		ExecutionID:         executionID,
		Status:              s.Status,
		Summary:             s.Summary,
		EvidenceFingerprint: execjournal.ResearchEvidenceFingerprint(evidence),
		PlanVersion:         s.PlanVersion,
	}
	for _, statement := range s.EvidenceBacked {
		durable.EvidenceBacked = append(durable.EvidenceBacked, execjournal.ResearchSynthesisStatement{
			Statement: statement.Statement,
			SourceIDs: append([]string(nil), statement.SourceIDs...),
		})
	}
	for _, inference := range s.Inferences {
		durable.Inferences = append(durable.Inferences, execjournal.ResearchSynthesisInference{
			Statement: inference.Statement,
		})
	}
	for _, conflict := range s.Conflicts {
		durable.Conflicts = append(durable.Conflicts, execjournal.ResearchSynthesisStatement{
			Statement: conflict.Statement,
			SourceIDs: append([]string(nil), conflict.SourceIDs...),
		})
	}
	durable.UnresolvedQuestions = append([]string(nil), s.UnresolvedQuestions...)
	return durable
}
