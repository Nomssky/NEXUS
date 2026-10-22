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
// The contract is process-local. It is attached to the terminal executor
// outcome and the process-local execution response; it is NOT a durable journal
// field, so after a restart the UI reports synthesis as unavailable rather than
// regenerating it.
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
	if ev.Version != execjournal.ResearchEvidenceVersion {
		return fmt.Errorf("synthesis requires v2 evidence with stable source ids")
	}
	// Validate the source rows without their in-progress provenance: at
	// completion the plan/step snapshot has not necessarily been finalized yet.
	base := *ev
	base.Plan, base.Steps, base.Operations = nil, nil, nil
	if err := execjournal.ValidateResearchEvidence(&base); err != nil {
		return fmt.Errorf("invalid evidence: %w", err)
	}
	if ev.Count < 0 || ev.Count != len(ev.Sources) {
		return fmt.Errorf("invalid evidence count")
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
