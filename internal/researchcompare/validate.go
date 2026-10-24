package researchcompare

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Nomssky/NEXUS/internal/execjournal"
)

var (
	fingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	findingCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

// Validate checks the comparison response at the projection boundary. It is
// deliberately stricter than the renderer: canonical category order, bounded
// strings, valid fingerprints, bounded collections and internally consistent
// side bindings are all required. A malformed projection fails closed instead
// of being rendered as an incomplete comparison.
func Validate(c Comparison) error {
	if c.Version != Version {
		return fmt.Errorf("researchcompare: unsupported comparison version %d", c.Version)
	}
	if c.Method != Method {
		return fmt.Errorf("researchcompare: unsupported comparison method %q", c.Method)
	}
	if err := validateSide("left", c.Left); err != nil {
		return err
	}
	if err := validateSide("right", c.Right); err != nil {
		return err
	}
	if c.Left.ExecutionID == c.Right.ExecutionID {
		return fmt.Errorf("researchcompare: left and right execution ids must differ")
	}
	if len(c.Differences) > MaxDifferences {
		return fmt.Errorf("researchcompare: too many differences: %d", len(c.Differences))
	}
	if len(c.Findings) > MaxFindings {
		return fmt.Errorf("researchcompare: too many findings: %d", len(c.Findings))
	}
	if len(c.Limitations) > MaxLimitations {
		return fmt.Errorf("researchcompare: too many limitations: %d", len(c.Limitations))
	}

	if err := validateCompatibility(c.Compatibility); err != nil {
		return err
	}
	for _, diff := range c.Differences {
		if err := validateDifference(diff); err != nil {
			return err
		}
	}
	for _, finding := range c.Findings {
		if err := validateFinding(finding); err != nil {
			return err
		}
	}
	for _, limitation := range c.Limitations {
		if !validText(limitation, MaxText) {
			return fmt.Errorf("researchcompare: invalid limitation")
		}
	}

	if !sortedDifferences(c.Differences) {
		return fmt.Errorf("researchcompare: differences are not in canonical order")
	}
	if !sortedFindings(c.Findings) {
		return fmt.Errorf("researchcompare: findings are not in canonical order")
	}
	if c.Truncated != hasFinding(c.Findings, FindingOutputTruncated) {
		return fmt.Errorf("researchcompare: truncated flag does not match output-truncation finding")
	}
	if err := validateSideBindings(c.Left); err != nil {
		return fmt.Errorf("researchcompare: left bindings: %w", err)
	}
	if err := validateSideBindings(c.Right); err != nil {
		return fmt.Errorf("researchcompare: right bindings: %w", err)
	}

	raw, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("researchcompare: comparison is not encodable: %w", err)
	}
	if len(raw) > MaxComparisonBytes {
		return fmt.Errorf("researchcompare: comparison exceeds %d bytes", MaxComparisonBytes)
	}
	return nil
}

func validateSide(role string, side Side) error {
	if side.Role != role {
		return fmt.Errorf("researchcompare: side role %q is not %q", side.Role, role)
	}
	if !validID(side.ExecutionID, 512) {
		return fmt.Errorf("researchcompare: invalid %s execution id", role)
	}
	if side.DivisionID != "" && !validID(side.DivisionID, MaxID) {
		return fmt.Errorf("researchcompare: invalid %s division id", role)
	}
	if !validExecutionState(execjournal.State(side.State)) {
		return fmt.Errorf("researchcompare: invalid %s execution state", role)
	}
	if err := validateProvenanceSummary(side.Provenance); err != nil {
		return fmt.Errorf("researchcompare: %s provenance: %w", role, err)
	}
	if side.Plan != nil && side.Evidence == nil {
		return fmt.Errorf("researchcompare: %s plan requires evidence", role)
	}
	if side.Synthesis != nil && side.Evidence == nil {
		return fmt.Errorf("researchcompare: %s synthesis requires evidence", role)
	}
	if side.Evidence != nil {
		if err := validateEvidenceSummary(*side.Evidence); err != nil {
			return fmt.Errorf("researchcompare: %s evidence: %w", role, err)
		}
	}
	if side.Plan != nil {
		if err := validatePlanSummary(*side.Plan); err != nil {
			return fmt.Errorf("researchcompare: %s plan: %w", role, err)
		}
	}
	if side.Synthesis != nil {
		if err := validateSynthesisSummary(*side.Synthesis); err != nil {
			return fmt.Errorf("researchcompare: %s synthesis: %w", role, err)
		}
	}
	if side.Evaluation != nil {
		if err := validateEvaluationSummary(*side.Evaluation); err != nil {
			return fmt.Errorf("researchcompare: %s evaluation: %w", role, err)
		}
	}
	return nil
}

func validateSideBindings(side Side) error {
	if side.Evaluation != nil {
		if (side.Evaluation.SynthesisStatus == "") != (side.Evaluation.SynthesisFingerprint == "") {
			return fmt.Errorf("evaluation synthesis status and fingerprint must be recorded together")
		}
	}
	if side.Evidence == nil {
		if side.Evaluation != nil {
			if side.Evaluation.EvidenceVersion != 0 || side.Evaluation.EvidenceCount != 0 {
				return fmt.Errorf("evaluation reports evidence that is absent")
			}
			if side.Evaluation.EvidenceFingerprint != execjournal.ResearchEvidenceFingerprint(nil) {
				return fmt.Errorf("evaluation is not bound to the absent evidence fingerprint")
			}
			if side.Evaluation.SynthesisStatus != "" {
				return fmt.Errorf("evaluation reports a synthesis without evidence")
			}
		}
		return nil
	}
	if side.Evidence.Version >= execjournal.ResearchEvidenceProvenanceVersion {
		if side.Plan == nil && (side.Evidence.StepCount > 0 || side.Evidence.OperationCount > 0) {
			return fmt.Errorf("evidence reports plan artifacts without a plan snapshot")
		}
		if side.Plan != nil && side.Evidence.StepCount == 0 {
			return fmt.Errorf("plan snapshot has no recorded steps")
		}
	}
	if side.Evidence.Version == execjournal.ResearchEvidenceVersion {
		expectedAttribution := execjournal.ResearchAttributionUnavailable
		if side.Evidence.OperationCount > 0 {
			expectedAttribution = execjournal.ResearchAttributionDerived
		}
		if side.Evidence.Attribution != expectedAttribution {
			return fmt.Errorf("v3 attribution is inconsistent with recorded operations")
		}
	}
	if side.Plan != nil {
		if side.Evidence.Version == execjournal.ResearchEvidenceVersion {
			if side.Plan.Provenance != ProvenanceAuthoritative || !side.Plan.CompletionRecorded {
				return fmt.Errorf("v3 plan must carry authoritative completion provenance")
			}
		} else {
			if side.Plan.Provenance != ProvenanceLegacy || side.Plan.CompletionRecorded {
				return fmt.Errorf("legacy plan cannot claim v3 provenance")
			}
		}
	}
	if side.Synthesis != nil {
		if side.Synthesis.EvidenceFingerprint != side.Evidence.Fingerprint {
			return fmt.Errorf("synthesis is not bound to the evidence fingerprint")
		}
		if side.Synthesis.PlanVersion != 0 {
			if side.Plan == nil ||
				side.Synthesis.PlanVersion != side.Plan.Version {
				return fmt.Errorf("synthesis plan version is not bound to the evidence plan")
			}
		} else if side.Evidence.Version == execjournal.ResearchEvidenceVersion &&
			side.Plan != nil {
			return fmt.Errorf("v3 synthesis over plan evidence must record its plan version")
		}
	}
	if side.Evaluation != nil {
		if side.Evaluation.EvidenceVersion != side.Evidence.Version ||
			side.Evaluation.EvidenceCount != side.Evidence.Count ||
			side.Evaluation.EvidenceFingerprint != side.Evidence.Fingerprint {
			return fmt.Errorf("evaluation is not bound to the evidence snapshot")
		}
		if side.Evaluation.PlanVersion != 0 {
			if side.Plan == nil ||
				side.Evaluation.PlanVersion != side.Plan.Version {
				return fmt.Errorf("evaluation plan version is not bound to the evidence plan")
			}
		} else if side.Evidence.Version == execjournal.ResearchEvidenceVersion &&
			side.Plan != nil {
			return fmt.Errorf("v3 evaluation over plan evidence must record its plan version")
		}
		// The evaluation's synthesis fingerprint is computed over the
		// pre-persistence executor synthesis, not the durable synthesis record.
		// It is retained as metadata but cannot be equated with the durable
		// synthesis fingerprint. The recorded status is comparable.
		if side.Synthesis != nil && side.Evaluation.SynthesisStatus != "" &&
			side.Evaluation.SynthesisStatus != side.Synthesis.Status {
			return fmt.Errorf("evaluation synthesis status does not match the synthesis")
		}
	}
	return nil
}

func validateProvenanceSummary(p ProvenanceSummary) error {
	for _, value := range []string{
		p.Evidence, p.Plan, p.Steps, p.Operations,
		p.SourceAttribution, p.Synthesis, p.Evaluation,
	} {
		if !validProvenance(value) {
			return fmt.Errorf("invalid provenance label %q", value)
		}
	}
	return nil
}

func validateEvidenceSummary(e EvidenceSummary) error {
	if e.Version < execjournal.ResearchEvidenceLegacyVersion || e.Version > execjournal.ResearchEvidenceVersion {
		return fmt.Errorf("unsupported evidence version %d", e.Version)
	}
	if !validID(e.Capability, MaxID) || !validID(e.Operation, MaxID) {
		return fmt.Errorf("invalid evidence capability or operation")
	}
	if e.Count < 0 || e.Count > execjournal.MaxResearchEvidenceSources {
		return fmt.Errorf("evidence count out of range")
	}
	if !validFingerprint(e.Fingerprint) {
		return fmt.Errorf("invalid evidence fingerprint")
	}
	if e.Attribution != "" &&
		e.Attribution != execjournal.ResearchAttributionDerived &&
		e.Attribution != execjournal.ResearchAttributionUnavailable {
		return fmt.Errorf("invalid evidence attribution")
	}
	if e.SourceCount < 0 || e.SourceCount > execjournal.MaxResearchEvidenceSources {
		return fmt.Errorf("source count out of range")
	}
	if e.SourceCount != e.Count {
		return fmt.Errorf("source count does not match evidence count")
	}
	if e.StepCount < 0 || e.StepCount > execjournal.MaxResearchPlanSteps {
		return fmt.Errorf("step count out of range")
	}
	if e.OperationCount < 0 || e.OperationCount > execjournal.MaxResearchOperations {
		return fmt.Errorf("operation count out of range")
	}
	switch e.Version {
	case execjournal.ResearchEvidenceLegacyVersion:
		if e.Attribution != "" || e.StepCount != 0 || e.OperationCount != 0 {
			return fmt.Errorf("legacy evidence carries provenance fields")
		}
	case execjournal.ResearchEvidenceProvenanceVersion:
		if e.Attribution != "" {
			return fmt.Errorf("v2 evidence carries a v3 attribution marker")
		}
	case execjournal.ResearchEvidenceVersion:
		if e.Attribution != execjournal.ResearchAttributionDerived &&
			e.Attribution != execjournal.ResearchAttributionUnavailable {
			return fmt.Errorf("v3 evidence must record attribution availability")
		}
	}
	return nil
}

func validatePlanSummary(p PlanSummary) error {
	if !validID(p.PlanID, execjournal.MaxResearchPlanID) {
		return fmt.Errorf("invalid plan id")
	}
	if p.Version < 1 || p.Version > execjournal.MaxResearchProvenancePlanVersion {
		return fmt.Errorf("plan version out of range")
	}
	if p.ObjectiveID != "" && !validID(p.ObjectiveID, execjournal.MaxResearchObjectiveID) {
		return fmt.Errorf("invalid objective id")
	}
	if p.CompletionStepID != "" && !validID(p.CompletionStepID, execjournal.MaxResearchStepID) {
		return fmt.Errorf("invalid completion step id")
	}
	if p.Provenance != ProvenanceAuthoritative && p.Provenance != ProvenanceLegacy {
		return fmt.Errorf("invalid plan provenance")
	}
	return nil
}

func validateSynthesisSummary(s SynthesisSummary) error {
	if s.Version != execjournal.ResearchSynthesisVersion || s.Method != execjournal.ResearchSynthesisMethod {
		return fmt.Errorf("unsupported synthesis schema")
	}
	if !validSynthesisStatus(s.Status) {
		return fmt.Errorf("invalid synthesis status")
	}
	if s.PlanVersion < 0 || s.PlanVersion > execjournal.MaxResearchProvenancePlanVersion {
		return fmt.Errorf("synthesis plan version out of range")
	}
	if s.PlanVersionRecorded != (s.PlanVersion > 0) {
		return fmt.Errorf("synthesis plan-version marker is inconsistent")
	}
	if !validFingerprint(s.EvidenceFingerprint) || !validFingerprint(s.Fingerprint) {
		return fmt.Errorf("invalid synthesis fingerprint")
	}
	if s.EvidenceBackedCount < 0 || s.EvidenceBackedCount > execjournal.MaxResearchSynthesisStatements {
		return fmt.Errorf("synthesis statement count out of range")
	}
	if s.InferenceCount < 0 || s.InferenceCount > execjournal.MaxResearchSynthesisStatements {
		return fmt.Errorf("synthesis inference count out of range")
	}
	if s.ConflictCount < 0 || s.ConflictCount > execjournal.MaxResearchSynthesisStatements {
		return fmt.Errorf("synthesis conflict count out of range")
	}
	if s.QuestionCount < 0 || s.QuestionCount > execjournal.MaxResearchSynthesisStatements {
		return fmt.Errorf("synthesis question count out of range")
	}
	return nil
}

func validateEvaluationSummary(e EvaluationSummary) error {
	if e.Version != execjournal.ResearchEvaluationVersion || e.Method != execjournal.ResearchEvaluationMethod {
		return fmt.Errorf("unsupported evaluation schema")
	}
	if !validEvaluationStatus(e.Status) {
		return fmt.Errorf("invalid evaluation status")
	}
	if e.EvidenceVersion < 0 || e.EvidenceVersion > execjournal.ResearchEvidenceVersion {
		return fmt.Errorf("evaluation evidence version out of range")
	}
	if e.EvidenceCount < 0 || e.EvidenceCount > execjournal.MaxResearchEvidenceSources {
		return fmt.Errorf("evaluation evidence count out of range")
	}
	if !validFingerprint(e.EvidenceFingerprint) {
		return fmt.Errorf("invalid evaluation evidence fingerprint")
	}
	if e.PlanVersion < 0 || e.PlanVersion > execjournal.MaxResearchProvenancePlanVersion {
		return fmt.Errorf("evaluation plan version out of range")
	}
	if e.PlanVersionRecorded != (e.PlanVersion > 0) {
		return fmt.Errorf("evaluation plan-version marker is inconsistent")
	}
	if e.SynthesisStatus != "" && !validSynthesisStatus(e.SynthesisStatus) {
		return fmt.Errorf("invalid evaluation synthesis status")
	}
	if e.SynthesisFingerprint != "" && !validFingerprint(e.SynthesisFingerprint) {
		return fmt.Errorf("invalid evaluation synthesis fingerprint")
	}
	if e.DimensionCount != 8 {
		return fmt.Errorf("evaluation must record all 8 dimensions")
	}
	if e.FindingCount < 0 || e.FindingCount > execjournal.MaxResearchEvaluationFindings {
		return fmt.Errorf("evaluation finding count out of range")
	}
	return nil
}

func validateCompatibility(c Compatibility) error {
	if c.Status != CompatibilityComparable &&
		c.Status != CompatibilityPartial &&
		c.Status != CompatibilityIncompatible {
		return fmt.Errorf("researchcompare: invalid compatibility status")
	}
	if len(c.Categories) != len(categoryOrder) {
		return fmt.Errorf("researchcompare: compatibility must list all categories")
	}
	for i, category := range categoryOrder {
		if c.Categories[i].Category != category {
			return fmt.Errorf("researchcompare: compatibility categories are not in canonical order")
		}
		status := c.Categories[i].Status
		if status != CategoryComparable && status != CategoryPartial &&
			status != CategoryUnavailable && status != CategoryIncompatible {
			return fmt.Errorf("researchcompare: invalid category status")
		}
	}
	if c.Status != overallCompatibility(c.Categories) {
		return fmt.Errorf("researchcompare: compatibility status does not match categories")
	}
	return nil
}

func validateDifference(d Difference) error {
	if categoryRank(d.Category) == len(categoryOrder) {
		return fmt.Errorf("researchcompare: invalid difference category")
	}
	switch d.Kind {
	case KindAdded, KindRemoved, KindChanged, KindUnavailable, KindIncompatible, KindLegacy:
	default:
		return fmt.Errorf("researchcompare: invalid difference kind")
	}
	if !validID(d.Entity, MaxID) || !validID(d.Field, MaxID) {
		return fmt.Errorf("researchcompare: invalid difference entity or field")
	}
	if d.ID != "" && !validID(d.ID, MaxID) {
		return fmt.Errorf("researchcompare: invalid difference id")
	}
	if !validOptionalText(d.Left) || !validOptionalText(d.Right) {
		return fmt.Errorf("researchcompare: invalid difference display value")
	}
	if d.LeftFingerprint != "" && !validFingerprint(d.LeftFingerprint) {
		return fmt.Errorf("researchcompare: invalid left fingerprint")
	}
	if d.RightFingerprint != "" && !validFingerprint(d.RightFingerprint) {
		return fmt.Errorf("researchcompare: invalid right fingerprint")
	}
	if d.Provenance != "" && !validProvenance(d.Provenance) {
		return fmt.Errorf("researchcompare: invalid difference provenance")
	}
	return nil
}

func validateFinding(f Finding) error {
	if !findingCodePattern.MatchString(f.Code) {
		return fmt.Errorf("researchcompare: invalid finding code")
	}
	if f.Severity != SeverityInfo && f.Severity != SeverityWarning && f.Severity != SeverityError {
		return fmt.Errorf("researchcompare: invalid finding severity")
	}
	if f.Category != "" && categoryRank(f.Category) == len(categoryOrder) {
		return fmt.Errorf("researchcompare: invalid finding category")
	}
	if !validText(f.Message, MaxText) {
		return fmt.Errorf("researchcompare: invalid finding message")
	}
	return nil
}

func overallCompatibility(categories []CategoryCompatibility) string {
	overall := CompatibilityComparable
	anyComparable := false
	allUnavailableOrIncompatible := true
	for _, category := range categories {
		if category.Status == CategoryComparable {
			anyComparable = true
			allUnavailableOrIncompatible = false
		}
		if category.Status != CategoryUnavailable && category.Status != CategoryIncompatible {
			allUnavailableOrIncompatible = false
		}
		if category.Status != CategoryComparable {
			overall = CompatibilityPartial
		}
	}
	if !anyComparable && allUnavailableOrIncompatible {
		return CompatibilityIncompatible
	}
	return overall
}

func sortedDifferences(diffs []Difference) bool {
	for i := 1; i < len(diffs); i++ {
		if differenceLess(diffs[i], diffs[i-1]) {
			return false
		}
	}
	return true
}

func differenceLess(a, b Difference) bool {
	if categoryRank(a.Category) != categoryRank(b.Category) {
		return categoryRank(a.Category) < categoryRank(b.Category)
	}
	if a.Entity != b.Entity {
		return a.Entity < b.Entity
	}
	if a.ID != b.ID {
		return a.ID < b.ID
	}
	if a.Field != b.Field {
		return a.Field < b.Field
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if a.Left != b.Left {
		return a.Left < b.Left
	}
	if a.Right != b.Right {
		return a.Right < b.Right
	}
	if a.LeftFingerprint != b.LeftFingerprint {
		return a.LeftFingerprint < b.LeftFingerprint
	}
	return a.RightFingerprint < b.RightFingerprint
}

func sortedFindings(findings []Finding) bool {
	for i := 1; i < len(findings); i++ {
		a, b := findings[i], findings[i-1]
		if categoryRank(a.Category) != categoryRank(b.Category) {
			if categoryRank(a.Category) < categoryRank(b.Category) {
				return false
			}
			continue
		}
		if a.Code != b.Code {
			if a.Code < b.Code {
				return false
			}
			continue
		}
		if a.Severity != b.Severity {
			if a.Severity < b.Severity {
				return false
			}
			continue
		}
		if a.Message < b.Message {
			return false
		}
	}
	return true
}

func hasFinding(findings []Finding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func validID(value string, max int) bool {
	return value != "" && len(value) <= max && !strings.ContainsAny(value, "\x00\n\r\t")
}

func validOptionalText(value string) bool {
	return len(value) <= MaxText && !strings.ContainsAny(value, "\x00\n\r\t")
}

func validText(value string, max int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= max &&
		!strings.ContainsAny(value, "\x00\n\r\t")
}

func validFingerprint(value string) bool {
	return fingerprintPattern.MatchString(value)
}

func validProvenance(value string) bool {
	switch value {
	case ProvenanceAuthoritative, ProvenanceDerived, ProvenanceUnavailable, ProvenanceLegacy:
		return true
	default:
		return false
	}
}

func validSynthesisStatus(status string) bool {
	switch status {
	case execjournal.ResearchSynthesisStatusComplete,
		execjournal.ResearchSynthesisStatusPartial,
		execjournal.ResearchSynthesisStatusInconclusive:
		return true
	default:
		return false
	}
}

func validEvaluationStatus(status string) bool {
	switch status {
	case execjournal.ResearchEvaluationPassed,
		execjournal.ResearchEvaluationWarnings,
		execjournal.ResearchEvaluationIncomplete,
		execjournal.ResearchEvaluationFailed:
		return true
	default:
		return false
	}
}
