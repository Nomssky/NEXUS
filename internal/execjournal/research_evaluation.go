package execjournal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// Research quality evaluation is a deterministic, versioned verdict over one
// execution's synthesis and durable evidence snapshot. It never stores model
// prose, source titles, URLs, snippets, queries or credentials: findings carry
// stable codes, generic messages and execution-scoped ids only.
//
// The evaluation is persisted atomically with the terminal execution record and
// the evidence snapshot it evaluated. Its evidence fingerprint is recomputed on
// every durable read, so an evaluation can never be reused for different
// evidence under the same execution id. A superseding terminal write (the
// single, narrow approval-resume path) replaces evidence, evaluation and
// synthesis together.
//
// The evaluation stores only the verdict's reference to the synthesis (its
// status and fingerprint). The synthesis itself is a separate durable
// artifact (v59), written in the same atomic update.
const (
	// ResearchEvaluationVersion is the evaluation schema version written today.
	// It is independent of SchemaVersion and ResearchEvidenceVersion.
	ResearchEvaluationVersion = 1
	// ResearchEvaluationMethod is the only evaluator implementation allowed to
	// write this version. It identifies deterministic rule evaluation, not a
	// model judgment.
	ResearchEvaluationMethod = "deterministic_v1"

	// Bounds keep one evaluation record small and prevent an evaluator defect
	// from growing the durable record or the API response without limit.
	MaxResearchEvaluationFindings    = 256
	MaxResearchEvaluationLimitations = 16
	MaxResearchEvaluationText        = 512
	MaxResearchEvaluationCode        = 64
)

// Evaluation statuses. These are intentionally not a boolean: incomplete
// inputs and integrity failures have different meanings and must never be
// collapsed into "passed".
const (
	ResearchEvaluationPassed     = "passed"
	ResearchEvaluationWarnings   = "warnings"
	ResearchEvaluationIncomplete = "incomplete"
	ResearchEvaluationFailed     = "failed"
)

// Dimension ids. The set is closed for evaluation version 1.
const (
	ResearchDimensionEvidenceCoverage  = "evidence_coverage"
	ResearchDimensionCitationIntegrity = "citation_integrity"
	ResearchDimensionRelevance         = "relevance"
	ResearchDimensionSourceQuality     = "source_quality"
	ResearchDimensionSourceDiversity   = "source_diversity"
	ResearchDimensionConflictHandling  = "conflict_handling"
	ResearchDimensionUncertainty       = "uncertainty_handling"
	ResearchDimensionPlanCompleteness  = "plan_completeness"
)

// Dimension statuses. not_measurable means the available data cannot establish
// the property; not_applicable means the dimension does not apply to this
// result (for example relevance when no sources were returned).
const (
	ResearchDimensionPass          = "pass"
	ResearchDimensionWarn          = "warn"
	ResearchDimensionIncomplete    = "incomplete"
	ResearchDimensionFail          = "fail"
	ResearchDimensionNotMeasurable = "not_measurable"
	ResearchDimensionNotApplicable = "not_applicable"
)

// Finding severities. Only warning and error are used; an informational
// finding would add noise without changing the verdict.
const (
	ResearchFindingWarning = "warning"
	ResearchFindingError   = "error"
)

// ResearchEvaluationDimension is one measured/declared dimension result.
type ResearchEvaluationDimension struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// ResearchEvaluationFinding is one stable, machine-readable finding. Message is
// a fixed explanation and never contains model prose, source content or
// transport detail. SourceID/StepID are execution-scoped identifiers only.
type ResearchEvaluationFinding struct {
	Code      string `json:"code"`
	Dimension string `json:"dimension"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	SourceID  string `json:"source_id,omitempty"`
	StepID    string `json:"step_id,omitempty"`
}

// ResearchEvaluation is the typed, versioned quality verdict persisted with an
// execution's terminal record.
type ResearchEvaluation struct {
	Version int    `json:"version"`
	Method  string `json:"method"`
	// ExecutionID binds the evaluation to exactly one execution record.
	ExecutionID string `json:"execution_id"`
	// EvidenceVersion and EvidenceCount describe the snapshot that was
	// evaluated. EvidenceVersion 0 means no evidence snapshot was available.
	EvidenceVersion     int    `json:"evidence_version"`
	EvidenceCount       int    `json:"evidence_count"`
	EvidenceFingerprint string `json:"evidence_fingerprint"`
	// SynthesisStatus and SynthesisFingerprint bind the verdict to the typed
	// synthesis that was evaluated. They are absent when no synthesis existed.
	// The synthesis itself is not stored here: this record carries only the
	// verdict's reference to it, never its text.
	SynthesisStatus      string                        `json:"synthesis_status,omitempty"`
	SynthesisFingerprint string                        `json:"synthesis_fingerprint,omitempty"`
	Status               string                        `json:"status"`
	Dimensions           []ResearchEvaluationDimension `json:"dimensions"`
	Findings             []ResearchEvaluationFinding   `json:"findings"`
	Limitations          []string                      `json:"limitations"`
}

// MarshalJSON makes required collections serialize as arrays instead of null.
// The receiver is a value, so marshaling never mutates a stored evaluation.
func (e ResearchEvaluation) MarshalJSON() ([]byte, error) {
	type researchEvaluationJSON ResearchEvaluation
	out := researchEvaluationJSON(e)
	if out.Dimensions == nil {
		out.Dimensions = []ResearchEvaluationDimension{}
	}
	if out.Findings == nil {
		out.Findings = []ResearchEvaluationFinding{}
	}
	if out.Limitations == nil {
		out.Limitations = []string{}
	}
	return json.Marshal(out)
}

var researchEvaluationDimensionIDs = []string{
	ResearchDimensionEvidenceCoverage,
	ResearchDimensionCitationIntegrity,
	ResearchDimensionRelevance,
	ResearchDimensionSourceQuality,
	ResearchDimensionSourceDiversity,
	ResearchDimensionConflictHandling,
	ResearchDimensionUncertainty,
	ResearchDimensionPlanCompleteness,
}

var validResearchEvaluationDimension = map[string]struct{}{
	ResearchDimensionEvidenceCoverage:  {},
	ResearchDimensionCitationIntegrity: {},
	ResearchDimensionRelevance:         {},
	ResearchDimensionSourceQuality:     {},
	ResearchDimensionSourceDiversity:   {},
	ResearchDimensionConflictHandling:  {},
	ResearchDimensionUncertainty:       {},
	ResearchDimensionPlanCompleteness:  {},
}

var validResearchDimensionStatus = map[string]struct{}{
	ResearchDimensionPass: {}, ResearchDimensionWarn: {},
	ResearchDimensionIncomplete: {}, ResearchDimensionFail: {},
	ResearchDimensionNotMeasurable: {}, ResearchDimensionNotApplicable: {},
}

var validResearchEvaluationStatus = map[string]struct{}{
	ResearchEvaluationPassed: {}, ResearchEvaluationWarnings: {},
	ResearchEvaluationIncomplete: {}, ResearchEvaluationFailed: {},
}

// ResearchEvidenceFingerprint is the stable identity of one evidence snapshot.
// It covers the normalized evidence JSON (including plan/step/operation
// provenance). A nil snapshot has a fixed fingerprint, not an empty string.
func ResearchEvidenceFingerprint(ev *ResearchEvidence) string {
	if ev == nil {
		sum := sha256.Sum256([]byte("nexus.research.evidence.none.v1"))
		return hex.EncodeToString(sum[:])
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		// ResearchEvidence.MarshalJSON cannot fail for bounded values, but a
		// future field could. An empty value is rejected by validation rather
		// than silently matching another record.
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ValidateResearchEvaluationForExecution checks an evaluation at the durable
// write and decode boundaries. It binds the evaluation to the record id, the
// evidence snapshot and (when present) the evidence version/count, and rejects
// statuses that misrepresent the findings.
func ValidateResearchEvaluationForExecution(executionID string, evaluation *ResearchEvaluation, evidence *ResearchEvidence) error {
	if evaluation == nil {
		return nil
	}
	if evaluation.Version != ResearchEvaluationVersion {
		return fmt.Errorf("execjournal: unsupported research evaluation version %d", evaluation.Version)
	}
	if evaluation.Method != ResearchEvaluationMethod {
		return fmt.Errorf("execjournal: unsupported research evaluation method %q", evaluation.Method)
	}
	if executionID == "" || evaluation.ExecutionID == "" || evaluation.ExecutionID != executionID {
		return fmt.Errorf("execjournal: research evaluation does not belong to execution %q", executionID)
	}
	if !validFingerprint(evaluation.EvidenceFingerprint) {
		return fmt.Errorf("execjournal: research evaluation has an invalid evidence fingerprint")
	}
	if evaluation.EvidenceFingerprint != ResearchEvidenceFingerprint(evidence) {
		return fmt.Errorf("execjournal: research evaluation does not match its evidence snapshot")
	}
	if evidence == nil {
		if evaluation.EvidenceVersion != 0 || evaluation.EvidenceCount != 0 {
			return fmt.Errorf("execjournal: research evaluation reports evidence that is absent")
		}
	} else {
		if evaluation.EvidenceVersion != evidence.Version || evaluation.EvidenceCount != evidence.Count {
			return fmt.Errorf("execjournal: research evaluation evidence version/count mismatch")
		}
	}
	if evaluation.SynthesisStatus == "" {
		if evaluation.SynthesisFingerprint != "" {
			return fmt.Errorf("execjournal: research evaluation has a synthesis fingerprint without a status")
		}
	} else {
		if !validSynthesisStatus(evaluation.SynthesisStatus) {
			return fmt.Errorf("execjournal: research evaluation has invalid synthesis status %q", evaluation.SynthesisStatus)
		}
		if !validFingerprint(evaluation.SynthesisFingerprint) {
			return fmt.Errorf("execjournal: research evaluation has an invalid synthesis fingerprint")
		}
		if evidence == nil {
			return fmt.Errorf("execjournal: research evaluation reports a synthesis without evidence")
		}
	}
	if _, ok := validResearchEvaluationStatus[evaluation.Status]; !ok {
		return fmt.Errorf("execjournal: research evaluation has invalid status %q", evaluation.Status)
	}
	if len(evaluation.Dimensions) != len(researchEvaluationDimensionIDs) {
		return fmt.Errorf("execjournal: research evaluation must carry %d dimensions", len(researchEvaluationDimensionIDs))
	}
	seenDimensions := make(map[string]struct{}, len(evaluation.Dimensions))
	for _, dimension := range evaluation.Dimensions {
		if _, ok := validResearchEvaluationDimension[dimension.ID]; !ok {
			return fmt.Errorf("execjournal: research evaluation has unknown dimension %q", dimension.ID)
		}
		if _, dup := seenDimensions[dimension.ID]; dup {
			return fmt.Errorf("execjournal: research evaluation repeats dimension %q", dimension.ID)
		}
		seenDimensions[dimension.ID] = struct{}{}
		if _, ok := validResearchDimensionStatus[dimension.Status]; !ok {
			return fmt.Errorf("execjournal: research evaluation dimension %q has invalid status %q",
				dimension.ID, dimension.Status)
		}
	}
	if len(evaluation.Findings) > MaxResearchEvaluationFindings {
		return fmt.Errorf("execjournal: research evaluation has too many findings")
	}
	hasError, hasWarning := false, false
	for _, finding := range evaluation.Findings {
		if !validEvaluationCode(finding.Code) {
			return fmt.Errorf("execjournal: research evaluation finding has an invalid code")
		}
		if _, ok := validResearchEvaluationDimension[finding.Dimension]; !ok {
			return fmt.Errorf("execjournal: research evaluation finding has unknown dimension %q", finding.Dimension)
		}
		switch finding.Severity {
		case ResearchFindingWarning:
			hasWarning = true
		case ResearchFindingError:
			hasError = true
		default:
			return fmt.Errorf("execjournal: research evaluation finding has invalid severity %q", finding.Severity)
		}
		if !validEvaluationText(finding.Message) {
			return fmt.Errorf("execjournal: research evaluation finding has an invalid message")
		}
		if finding.SourceID != "" && !validProvenanceField(finding.SourceID, MaxResearchSourceID) {
			return fmt.Errorf("execjournal: research evaluation finding has an invalid source id")
		}
		if finding.StepID != "" && !validProvenanceField(finding.StepID, MaxResearchStepID) {
			return fmt.Errorf("execjournal: research evaluation finding has an invalid step id")
		}
	}
	if len(evaluation.Limitations) > MaxResearchEvaluationLimitations {
		return fmt.Errorf("execjournal: research evaluation has too many limitations")
	}
	for _, limitation := range evaluation.Limitations {
		if !validEvaluationText(limitation) {
			return fmt.Errorf("execjournal: research evaluation limitation is invalid")
		}
	}

	hasIncomplete, hasWarnDimension, hasFailDimension := false, false, false
	for _, dimension := range evaluation.Dimensions {
		switch dimension.Status {
		case ResearchDimensionIncomplete:
			hasIncomplete = true
		case ResearchDimensionWarn:
			hasWarnDimension = true
		case ResearchDimensionFail:
			hasFailDimension = true
		}
	}
	switch evaluation.Status {
	case ResearchEvaluationPassed:
		if hasError || hasWarning || hasIncomplete || hasWarnDimension || hasFailDimension {
			return fmt.Errorf("execjournal: research evaluation status passed contradicts its findings")
		}
	case ResearchEvaluationWarnings:
		if hasError || hasFailDimension || hasIncomplete || (!hasWarning && !hasWarnDimension) {
			return fmt.Errorf("execjournal: research evaluation status warnings is inconsistent")
		}
	case ResearchEvaluationIncomplete:
		if hasError || hasFailDimension || !hasIncomplete {
			return fmt.Errorf("execjournal: research evaluation status incomplete is inconsistent")
		}
	case ResearchEvaluationFailed:
		if !hasError && !hasFailDimension {
			return fmt.Errorf("execjournal: research evaluation status failed has no integrity failure")
		}
	}
	return nil
}

func validFingerprint(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func validSynthesisStatus(value string) bool {
	switch value {
	case "complete", "partial", "inconclusive":
		return true
	default:
		return false
	}
}

func validEvaluationCode(value string) bool {
	if value == "" || len(value) > MaxResearchEvaluationCode {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

func validEvaluationText(value string) bool {
	return strings.TrimSpace(value) != "" && len(value) <= MaxResearchEvaluationText &&
		!strings.ContainsAny(value, "\x00\n\r")
}
