// Package researchcompare projects a deterministic, bounded comparison of two
// validated durable research snapshots.
//
// It is a pure read model. It does not write, migrate, backfill or infer
// historical facts, and it never reads process-local runtime state. The only
// inputs are the two immutable execjournal.ExecutionRecord values passed by the
// caller. Every result is derived from fields those records actually contain.
//
// The comparison is deliberately not a semantic truth engine. A changed source
// fingerprint proves that the recorded record changed; it does not prove which
// record is true. A changed synthesis statement set proves that the recorded
// synthesis changed; it does not prove a contradiction or an improvement.
package researchcompare

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Nomssky/NEXUS/internal/execjournal"
)

const (
	// Version is the comparison response schema version.
	Version = 1
	// Method is the only comparison implementation allowed to write version 1.
	Method = "deterministic_v1"

	// MaxDifferences bounds the returned difference list. Truncation is explicit
	// and deterministic: the canonical first N differences are returned and a
	// comparison_output_truncated finding is added.
	MaxDifferences = 512
	// MaxFindings bounds the compatibility/limitation findings list.
	MaxFindings = 64
	// MaxLimitations bounds the fixed limitation list.
	MaxLimitations = 16
	// MaxText bounds every free display value in a difference.
	MaxText = 512
	// MaxID bounds entity, id and field labels.
	MaxID = 128
	// MaxComparisonBytes is the hard upper bound on the encoded comparison.
	MaxComparisonBytes = 2 << 20
)

// Difference categories, in canonical output order.
const (
	CategoryPlan       = "plan"
	CategorySteps      = "steps"
	CategoryOperations = "operations"
	CategoryEvidence   = "evidence"
	CategorySynthesis  = "synthesis"
	CategoryEvaluation = "evaluation"
)

var categoryOrder = []string{
	CategoryPlan, CategorySteps, CategoryOperations,
	CategoryEvidence, CategorySynthesis, CategoryEvaluation,
}

// Difference kinds. "added" means present on the right and absent on the left;
// "removed" means present on the left and absent on the right.
const (
	KindAdded        = "added"
	KindRemoved      = "removed"
	KindChanged      = "changed"
	KindUnavailable  = "unavailable"
	KindIncompatible = "incompatible"
	KindLegacy       = "legacy"
)

// Provenance labels. They describe how a fact came to be recorded, never its
// truth or quality.
const (
	ProvenanceAuthoritative = "authoritative"
	ProvenanceDerived       = "derived"
	ProvenanceUnavailable   = "unavailable"
	ProvenanceLegacy        = "legacy"
)

// Compatibility statuses. "comparable" means every category could be compared;
// "partial" means at least one category was unavailable, legacy or otherwise
// limited; "incompatible" means no category could be meaningfully compared.
const (
	CompatibilityComparable   = "comparable"
	CompatibilityPartial      = "partial"
	CompatibilityIncompatible = "incompatible"

	CategoryComparable   = "comparable"
	CategoryPartial      = "partial"
	CategoryUnavailable  = "unavailable"
	CategoryIncompatible = "incompatible"
)

// Finding severities. These are comparison diagnostics, not research-quality
// verdicts.
const (
	SeverityInfo    = "info"
	SeverityWarning = "warning"
	SeverityError   = "error"
)

// Stable comparison finding codes.
const (
	FindingComparisonIncompatible        = "comparison_incompatible"
	FindingNoRecordedDifferences         = "comparison_no_recorded_differences"
	FindingOutputTruncated               = "comparison_output_truncated"
	FindingEvidenceMissingOnLeft         = "evidence_missing_on_left"
	FindingEvidenceMissingOnRight        = "evidence_missing_on_right"
	FindingEvidenceUnavailableBoth       = "evidence_unavailable_on_both_sides"
	FindingEvidenceVersionChanged        = "evidence_version_changed"
	FindingEvidenceFingerprintIncomp     = "evidence_fingerprint_incomparable"
	FindingSourceURLNotIdentity          = "source_url_not_identity"
	FindingPlanUnavailableOnLeft         = "plan_provenance_unavailable_on_left"
	FindingPlanUnavailableOnRight        = "plan_provenance_unavailable_on_right"
	FindingPlanUnavailableBoth           = "plan_provenance_unavailable_on_both_sides"
	FindingLegacyEvidenceNoProvenance    = "legacy_evidence_no_provenance"
	FindingLegacyPlanProvenance          = "legacy_plan_provenance_unavailable"
	FindingPlanObjectiveUnavailableLeft  = "plan_objective_unavailable_on_left"
	FindingPlanObjectiveUnavailableRight = "plan_objective_unavailable_on_right"
	FindingPlanCompletionUnavailableLeft = "plan_completion_unavailable_on_left"
	FindingPlanCompletionUnavailableRt   = "plan_completion_unavailable_on_right"
	FindingStepIdentityScope             = "step_identity_scope"
	FindingOperationIdentityScope        = "operation_identity_scope"
	FindingSynthesisMissingOnLeft        = "synthesis_missing_on_left"
	FindingSynthesisMissingOnRight       = "synthesis_missing_on_right"
	FindingSynthesisUnavailableBoth      = "synthesis_unavailable_on_both_sides"
	FindingSynthesisSchemaIncompatible   = "synthesis_schema_incompatible"
	FindingSynthesisPlanVersionLeft      = "synthesis_plan_version_unavailable_on_left"
	FindingSynthesisPlanVersionRight     = "synthesis_plan_version_unavailable_on_right"
	FindingSynthesisPlanVersionBoth      = "synthesis_plan_version_unavailable_on_both_sides"
	FindingEvaluationMissingOnLeft       = "evaluation_missing_on_left"
	FindingEvaluationMissingOnRight      = "evaluation_missing_on_right"
	FindingEvaluationUnavailableBoth     = "evaluation_unavailable_on_both_sides"
	FindingEvaluationSchemaIncompatible  = "evaluation_schema_incompatible"
	FindingEvaluationAbsentNotClean      = "evaluation_absent_not_clean"
	FindingEvaluationPlanVersionLeft     = "evaluation_plan_version_unavailable_on_left"
	FindingEvaluationPlanVersionRight    = "evaluation_plan_version_unavailable_on_right"
	FindingEvaluationPlanVersionBoth     = "evaluation_plan_version_unavailable_on_both_sides"
)

// Comparison is the versioned comparison response body.
type Comparison struct {
	Version       int           `json:"version"`
	Method        string        `json:"method"`
	Left          Side          `json:"left"`
	Right         Side          `json:"right"`
	Compatibility Compatibility `json:"compatibility"`
	Differences   []Difference  `json:"differences"`
	Findings      []Finding     `json:"findings"`
	Limitations   []string      `json:"limitations"`
	Truncated     bool          `json:"truncated"`
}

// Side is the allow-listed summary of one execution's durable snapshot.
type Side struct {
	Role        string             `json:"role"`
	ExecutionID string             `json:"execution_id"`
	DivisionID  string             `json:"division_id,omitempty"`
	State       string             `json:"state"`
	CreatedAt   time.Time          `json:"created_at"`
	CompletedAt *time.Time         `json:"completed_at,omitempty"`
	Evidence    *EvidenceSummary   `json:"evidence,omitempty"`
	Plan        *PlanSummary       `json:"plan,omitempty"`
	Synthesis   *SynthesisSummary  `json:"synthesis,omitempty"`
	Evaluation  *EvaluationSummary `json:"evaluation,omitempty"`
	Provenance  ProvenanceSummary  `json:"provenance"`
}

// EvidenceSummary describes the durable evidence snapshot itself. It carries
// no source content; source records are represented only by bounded labels and
// canonical fingerprints in differences.
type EvidenceSummary struct {
	Version        int    `json:"version"`
	Capability     string `json:"capability"`
	Operation      string `json:"operation"`
	Count          int    `json:"count"`
	Fingerprint    string `json:"fingerprint"`
	Attribution    string `json:"attribution,omitempty"`
	SourceCount    int    `json:"source_count"`
	StepCount      int    `json:"step_count"`
	OperationCount int    `json:"operation_count"`
}

// PlanSummary is the recorded plan identity and binding. PlanID is included
// for display only; it is execution-scoped and is never used to infer shared
// plan identity.
type PlanSummary struct {
	PlanID             string `json:"plan_id"`
	Version            int    `json:"version"`
	ObjectiveID        string `json:"objective_id,omitempty"`
	CompletionStepID   string `json:"completion_step_id,omitempty"`
	CompletionRecorded bool   `json:"completion_recorded"`
	Provenance         string `json:"provenance"`
}

// SynthesisSummary describes the durable synthesis without copying its prose.
type SynthesisSummary struct {
	Version             int    `json:"version"`
	Method              string `json:"method"`
	Status              string `json:"status"`
	PlanVersion         int    `json:"plan_version,omitempty"`
	PlanVersionRecorded bool   `json:"plan_version_recorded"`
	EvidenceFingerprint string `json:"evidence_fingerprint"`
	Fingerprint         string `json:"fingerprint"`
	EvidenceBackedCount int    `json:"evidence_backed_count"`
	InferenceCount      int    `json:"inference_count"`
	ConflictCount       int    `json:"conflict_count"`
	QuestionCount       int    `json:"question_count"`
}

// EvaluationSummary describes the deterministic quality verdict without
// copying its findings or limitations.
type EvaluationSummary struct {
	Version              int    `json:"version"`
	Method               string `json:"method"`
	Status               string `json:"status"`
	EvidenceVersion      int    `json:"evidence_version"`
	EvidenceCount        int    `json:"evidence_count"`
	EvidenceFingerprint  string `json:"evidence_fingerprint"`
	PlanVersion          int    `json:"plan_version,omitempty"`
	PlanVersionRecorded  bool   `json:"plan_version_recorded"`
	SynthesisStatus      string `json:"synthesis_status,omitempty"`
	SynthesisFingerprint string `json:"synthesis_fingerprint,omitempty"`
	DimensionCount       int    `json:"dimension_count"`
	FindingCount         int    `json:"finding_count"`
}

// ProvenanceSummary labels each artifact's recorded provenance class.
type ProvenanceSummary struct {
	Evidence          string `json:"evidence"`
	Plan              string `json:"plan"`
	Steps             string `json:"steps"`
	Operations        string `json:"operations"`
	SourceAttribution string `json:"source_attribution"`
	Synthesis         string `json:"synthesis"`
	Evaluation        string `json:"evaluation"`
}

// Compatibility summarizes which categories could actually be compared.
type Compatibility struct {
	Status     string                  `json:"status"`
	Categories []CategoryCompatibility `json:"categories"`
}

// CategoryCompatibility is one category's comparison status.
type CategoryCompatibility struct {
	Category string `json:"category"`
	Status   string `json:"status"`
}

// Difference is one bounded, deterministic recorded difference.
type Difference struct {
	Category         string `json:"category"`
	Kind             string `json:"kind"`
	Entity           string `json:"entity"`
	ID               string `json:"id,omitempty"`
	Field            string `json:"field,omitempty"`
	Left             string `json:"left,omitempty"`
	Right            string `json:"right,omitempty"`
	LeftFingerprint  string `json:"left_fingerprint,omitempty"`
	RightFingerprint string `json:"right_fingerprint,omitempty"`
	Provenance       string `json:"provenance,omitempty"`
}

// Finding is a stable compatibility/limitation diagnostic.
type Finding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Category string `json:"category,omitempty"`
	Message  string `json:"message"`
}

var fixedLimitations = []string{
	"This comparison reports recorded artifact differences only. It does not establish factual truth, source support, or which execution is better.",
	"Sources are paired only by a canonical fingerprint of the recorded fields (including a recorded id when present), never by URL, title, snippet, source name, or execution-scoped id alone. Identical text or URL does not prove the same underlying source.",
	"Step and operation ids are plan/execution-scoped labels. A matching id is not proof of the same logical step or operation.",
	"Plan and objective ids are execution-scoped and are never used to infer a shared plan or objective identity.",
	"Synthesis and evaluation differences describe recorded artifacts. They are not contradictions, correctness improvements, trust scores, or confidence values.",
	"An absent synthesis or evaluation is not an empty or clean artifact.",
	"Missing, legacy, or incompatible facts are reported as unavailable. They are never inferred from ordering, runtime state, similarity, or another execution.",
	"This comparison does not inspect prompts, credentials, hidden reasoning, model scratch text, or raw provider payloads.",
}

// Compare projects a deterministic comparison of two validated durable
// records. It fails closed if either record is not internally bound or if the
// records do not belong to the same non-empty business.
func Compare(left, right execjournal.ExecutionRecord) (Comparison, error) {
	if left.ID == "" || right.ID == "" {
		return Comparison{}, fmt.Errorf("researchcompare: both execution ids are required")
	}
	if left.ID == right.ID {
		return Comparison{}, fmt.Errorf("researchcompare: left and right execution ids must differ")
	}
	if left.Version != execjournal.SchemaVersion || right.Version != execjournal.SchemaVersion {
		return Comparison{}, fmt.Errorf("researchcompare: unsupported execution record version")
	}
	if !validExecutionState(left.State) || !validExecutionState(right.State) {
		return Comparison{}, fmt.Errorf("researchcompare: unsupported execution state")
	}
	if left.BusinessID == "" || left.BusinessID != right.BusinessID {
		return Comparison{}, fmt.Errorf("researchcompare: executions must belong to the same business")
	}
	if err := execjournal.ValidateResearchEvidenceForExecution(left.ID, left.ResearchEvidence); err != nil {
		return Comparison{}, fmt.Errorf("researchcompare: left evidence: %w", err)
	}
	if err := execjournal.ValidateResearchEvidenceForExecution(right.ID, right.ResearchEvidence); err != nil {
		return Comparison{}, fmt.Errorf("researchcompare: right evidence: %w", err)
	}
	if err := execjournal.ValidateResearchEvaluationForExecution(left.ID, left.ResearchEvaluation, left.ResearchEvidence); err != nil {
		return Comparison{}, fmt.Errorf("researchcompare: left evaluation: %w", err)
	}
	if err := execjournal.ValidateResearchEvaluationForExecution(right.ID, right.ResearchEvaluation, right.ResearchEvidence); err != nil {
		return Comparison{}, fmt.Errorf("researchcompare: right evaluation: %w", err)
	}
	if err := execjournal.ValidateResearchSynthesisForExecution(left.ID, left.ResearchSynthesis, left.ResearchEvidence); err != nil {
		return Comparison{}, fmt.Errorf("researchcompare: left synthesis: %w", err)
	}
	if err := execjournal.ValidateResearchSynthesisForExecution(right.ID, right.ResearchSynthesis, right.ResearchEvidence); err != nil {
		return Comparison{}, fmt.Errorf("researchcompare: right synthesis: %w", err)
	}

	b := &comparisonBuilder{
		c: Comparison{
			Version:     Version,
			Method:      Method,
			Left:        sideView("left", left),
			Right:       sideView("right", right),
			Differences: []Difference{},
			Findings:    []Finding{},
			Limitations: append([]string(nil), fixedLimitations...),
		},
		differences: map[string]struct{}{},
		findings:    map[string]struct{}{},
		categories:  map[string]string{},
	}

	b.comparePlan(left, right)
	b.compareSteps(left, right)
	b.compareOperations(left, right)
	b.compareEvidence(left, right)
	b.compareSynthesis(left, right)
	b.compareEvaluation(left, right)

	b.finish()
	if err := Validate(b.c); err != nil {
		return Comparison{}, err
	}
	return b.c, nil
}

type comparisonBuilder struct {
	c           Comparison
	differences map[string]struct{}
	findings    map[string]struct{}
	categories  map[string]string
}

func (b *comparisonBuilder) addDiff(d Difference) {
	d.Category = clip(d.Category, MaxID)
	d.Kind = clip(d.Kind, MaxID)
	d.Entity = clip(d.Entity, MaxID)
	d.ID = clip(d.ID, MaxID)
	d.Field = clip(d.Field, MaxID)
	d.Left = clip(d.Left, MaxText)
	d.Right = clip(d.Right, MaxText)
	d.Provenance = clip(d.Provenance, MaxID)
	key := strings.Join([]string{
		d.Category, d.Kind, d.Entity, d.ID, d.Field, d.Left, d.Right,
		d.LeftFingerprint, d.RightFingerprint, d.Provenance,
	}, "\x00")
	if _, ok := b.differences[key]; ok {
		return
	}
	b.differences[key] = struct{}{}
	b.c.Differences = append(b.c.Differences, d)
}

func (b *comparisonBuilder) addFinding(code, severity, category, message string) {
	code = clip(code, MaxID)
	severity = clip(severity, MaxID)
	category = clip(category, MaxID)
	message = clip(message, MaxText)
	key := strings.Join([]string{code, severity, category, message}, "\x00")
	if _, ok := b.findings[key]; ok {
		return
	}
	b.findings[key] = struct{}{}
	b.c.Findings = append(b.c.Findings, Finding{
		Code: code, Severity: severity, Category: category, Message: message,
	})
}

func (b *comparisonBuilder) setCategory(category, status string) {
	existing, ok := b.categories[category]
	if !ok {
		b.categories[category] = status
		return
	}
	b.categories[category] = worseCategoryStatus(existing, status)
}

func (b *comparisonBuilder) finish() {
	sortDifferences(b.c.Differences)
	if len(b.c.Differences) > MaxDifferences {
		b.c.Differences = b.c.Differences[:MaxDifferences]
		b.c.Truncated = true
		b.addFinding(FindingOutputTruncated, SeverityWarning, "",
			"Only the first 512 canonical differences are returned; additional recorded differences were omitted.")
	}

	categories := make([]CategoryCompatibility, 0, len(categoryOrder))
	overall := CompatibilityComparable
	anyComparable := false
	allUnavailableOrIncompatible := true
	for _, category := range categoryOrder {
		status := b.categories[category]
		if status == "" {
			status = CategoryUnavailable
		}
		if status == CategoryComparable {
			anyComparable = true
			allUnavailableOrIncompatible = false
		}
		if status != CategoryUnavailable && status != CategoryIncompatible {
			allUnavailableOrIncompatible = false
		}
		if status != CategoryComparable {
			overall = CompatibilityPartial
		}
		categories = append(categories, CategoryCompatibility{Category: category, Status: status})
	}
	if !anyComparable && allUnavailableOrIncompatible {
		overall = CompatibilityIncompatible
		b.addFinding(FindingComparisonIncompatible, SeverityError, "",
			"The two records do not share a comparable durable artifact category.")
	}
	b.c.Compatibility = Compatibility{Status: overall, Categories: categories}

	if len(b.c.Differences) == 0 && overall != CompatibilityIncompatible {
		b.addFinding(FindingNoRecordedDifferences, SeverityInfo, "",
			"No recorded differences were found in the compared categories; unavailable categories are not evidence of sameness.")
	}

	sortFindings(b.c.Findings)
	if len(b.c.Findings) > MaxFindings {
		b.c.Findings = b.c.Findings[:MaxFindings-1]
		b.c.Truncated = true
		// The dedup map may still remember a truncation finding that was just
		// sliced away. Forget it so the mandatory explicit-truncation finding is
		// always present in the retained output.
		delete(b.findings, FindingOutputTruncated)
		b.addFinding(FindingOutputTruncated, SeverityWarning, "",
			"Only the first 512 canonical differences and 63 findings are returned; additional recorded differences were omitted.")
		sortFindings(b.c.Findings)
	}

	if b.c.Differences == nil {
		b.c.Differences = []Difference{}
	}
	if b.c.Findings == nil {
		b.c.Findings = []Finding{}
	}
	if b.c.Limitations == nil {
		b.c.Limitations = []string{}
	}
}

func (b *comparisonBuilder) comparePlan(left, right execjournal.ExecutionRecord) {
	lev, rev := left.ResearchEvidence, right.ResearchEvidence
	switch {
	case lev == nil && rev == nil:
		b.setCategory(CategoryPlan, CategoryUnavailable)
		b.addFinding(FindingPlanUnavailableBoth, SeverityWarning, CategoryPlan,
			"No plan provenance is available on either side.")
		return
	case lev == nil:
		b.setCategory(CategoryPlan, CategoryPartial)
		b.addFinding(FindingPlanUnavailableOnLeft, SeverityWarning, CategoryPlan,
			"Plan provenance is absent on the left; absence is not an empty plan.")
		return
	case rev == nil:
		b.setCategory(CategoryPlan, CategoryPartial)
		b.addFinding(FindingPlanUnavailableOnRight, SeverityWarning, CategoryPlan,
			"Plan provenance is absent on the right; absence is not an empty plan.")
		return
	case lev.Version == execjournal.ResearchEvidenceLegacyVersion ||
		rev.Version == execjournal.ResearchEvidenceLegacyVersion:
		switch {
		case lev.Version == execjournal.ResearchEvidenceLegacyVersion &&
			rev.Version == execjournal.ResearchEvidenceLegacyVersion:
			b.setCategory(CategoryPlan, CategoryUnavailable)
			b.addFinding(FindingPlanUnavailableBoth, SeverityWarning, CategoryPlan,
				"No plan provenance is available on either side; v1 evidence records no plan.")
		case lev.Version == execjournal.ResearchEvidenceLegacyVersion && rev.Plan == nil:
			b.setCategory(CategoryPlan, CategoryUnavailable)
			b.addFinding(FindingPlanUnavailableBoth, SeverityWarning, CategoryPlan,
				"No plan provenance is available on either side; the left v1 evidence and the right record have no plan.")
		case lev.Version == execjournal.ResearchEvidenceLegacyVersion:
			b.setCategory(CategoryPlan, CategoryPartial)
			b.addFinding(FindingPlanUnavailableOnLeft, SeverityWarning, CategoryPlan,
				"Plan provenance is absent on the left; v1 evidence records no plan.")
		case lev.Plan == nil:
			b.setCategory(CategoryPlan, CategoryUnavailable)
			b.addFinding(FindingPlanUnavailableBoth, SeverityWarning, CategoryPlan,
				"No plan provenance is available on either side; the left record and the right v1 evidence have no plan.")
		default:
			b.setCategory(CategoryPlan, CategoryPartial)
			b.addFinding(FindingPlanUnavailableOnRight, SeverityWarning, CategoryPlan,
				"Plan provenance is absent on the right; v1 evidence records no plan.")
		}
		return
	case lev.Plan == nil && rev.Plan == nil:
		b.setCategory(CategoryPlan, CategoryUnavailable)
		b.addFinding(FindingPlanUnavailableBoth, SeverityWarning, CategoryPlan,
			"No plan provenance is available on either side.")
		return
	case lev.Plan == nil:
		b.setCategory(CategoryPlan, CategoryPartial)
		b.addFinding(FindingPlanUnavailableOnLeft, SeverityWarning, CategoryPlan,
			"Plan provenance is absent on the left; absence is not an empty plan.")
		return
	case rev.Plan == nil:
		b.setCategory(CategoryPlan, CategoryPartial)
		b.addFinding(FindingPlanUnavailableOnRight, SeverityWarning, CategoryPlan,
			"Plan provenance is absent on the right; absence is not an empty plan.")
		return
	}

	full := lev.Version == execjournal.ResearchEvidenceVersion &&
		rev.Version == execjournal.ResearchEvidenceVersion
	if full {
		b.setCategory(CategoryPlan, CategoryComparable)
	} else {
		b.setCategory(CategoryPlan, CategoryPartial)
		b.addFinding(FindingLegacyPlanProvenance, SeverityWarning, CategoryPlan,
			"At least one side carries v2 plan provenance without v3 intent, objective, completion or authoritative markers.")
	}

	if lev.Plan.Version != rev.Plan.Version {
		b.addDiff(Difference{
			Category: CategoryPlan, Kind: KindChanged, Entity: "plan", Field: "version",
			Left: fmt.Sprintf("%d", lev.Plan.Version), Right: fmt.Sprintf("%d", rev.Plan.Version),
			Provenance: combinedPlanProvenance(lev, rev),
		})
	}

	if full {
		// Objective references are execution-scoped ids. They remain visible in
		// the side summary but are not compared as content or shared identity.
		if lev.Plan.ObjectiveID == "" && rev.Plan.ObjectiveID != "" {
			b.addFinding(FindingPlanObjectiveUnavailableLeft, SeverityWarning, CategoryPlan,
				"The left plan did not record an objective reference; it is unavailable, not empty.")
		} else if lev.Plan.ObjectiveID != "" && rev.Plan.ObjectiveID == "" {
			b.addFinding(FindingPlanObjectiveUnavailableRight, SeverityWarning, CategoryPlan,
				"The right plan did not record an objective reference; it is unavailable, not empty.")
		}

		if lev.Plan.CompletionStepID != rev.Plan.CompletionStepID {
			b.addDiff(Difference{
				Category: CategoryPlan, Kind: KindChanged, Entity: "completion_step", Field: "id",
				Left:       displayOrNone(lev.Plan.CompletionStepID),
				Right:      displayOrNone(rev.Plan.CompletionStepID),
				Provenance: ProvenanceAuthoritative,
			})
		}
	}
}

func (b *comparisonBuilder) compareSteps(left, right execjournal.ExecutionRecord) {
	lev, rev := left.ResearchEvidence, right.ResearchEvidence
	if !bothPlans(lev, rev) {
		switch {
		case lev == nil || rev == nil:
			b.setCategory(CategorySteps, CategoryUnavailable)
		case lev.Version == execjournal.ResearchEvidenceLegacyVersion ||
			rev.Version == execjournal.ResearchEvidenceLegacyVersion:
			b.setCategory(CategorySteps, CategoryUnavailable)
		default:
			b.setCategory(CategorySteps, CategoryUnavailable)
		}
		return
	}

	full := lev.Version == execjournal.ResearchEvidenceVersion &&
		rev.Version == execjournal.ResearchEvidenceVersion
	if full {
		b.setCategory(CategorySteps, CategoryComparable)
	} else {
		b.setCategory(CategorySteps, CategoryPartial)
	}
	b.addFinding(FindingStepIdentityScope, SeverityInfo, CategorySteps,
		"Step ids are plan-scoped labels; a matching id is not proof of the same logical step.")

	provenance := combinedPlanProvenance(lev, rev)
	leftSteps := stepMap(lev.Steps)
	rightSteps := stepMap(rev.Steps)
	ids := unionKeys(leftSteps, rightSteps)
	for _, id := range ids {
		ls, lok := leftSteps[id]
		rs, rok := rightSteps[id]
		switch {
		case !rok:
			b.addDiff(Difference{
				Category: CategorySteps, Kind: KindRemoved, Entity: "step", ID: id,
				Field: "step", Left: ls.Status, Provenance: provenance,
			})
		case !lok:
			b.addDiff(Difference{
				Category: CategorySteps, Kind: KindAdded, Entity: "step", ID: id,
				Field: "step", Right: rs.Status, Provenance: provenance,
			})
		default:
			if ls.Status != rs.Status {
				b.addDiff(Difference{
					Category: CategorySteps, Kind: KindChanged, Entity: "step", ID: id,
					Field: "status", Left: ls.Status, Right: rs.Status,
					Provenance: provenance,
				})
			}
			compareDependencies(b, id, ls.Dependencies, rs.Dependencies, provenance)
			if full {
				if ls.Intent != rs.Intent {
					b.addDiff(Difference{
						Category: CategorySteps, Kind: KindChanged, Entity: "step", ID: id,
						Field: "intent", Left: ls.Intent, Right: rs.Intent,
						LeftFingerprint: textFingerprint(ls.Intent), RightFingerprint: textFingerprint(rs.Intent),
						Provenance: ProvenanceAuthoritative,
					})
				}
				if ls.PlanVersion != rs.PlanVersion {
					b.addDiff(Difference{
						Category: CategorySteps, Kind: KindChanged, Entity: "step", ID: id,
						Field: "plan_version", Left: fmt.Sprintf("%d", ls.PlanVersion),
						Right: fmt.Sprintf("%d", rs.PlanVersion), Provenance: ProvenanceAuthoritative,
					})
				}
			}
		}
	}
}

func (b *comparisonBuilder) compareOperations(left, right execjournal.ExecutionRecord) {
	lev, rev := left.ResearchEvidence, right.ResearchEvidence
	if !bothPlans(lev, rev) {
		b.setCategory(CategoryOperations, CategoryUnavailable)
		return
	}

	full := lev.Version == execjournal.ResearchEvidenceVersion &&
		rev.Version == execjournal.ResearchEvidenceVersion
	if full {
		b.setCategory(CategoryOperations, CategoryComparable)
	} else {
		b.setCategory(CategoryOperations, CategoryPartial)
	}
	b.addFinding(FindingOperationIdentityScope, SeverityInfo, CategoryOperations,
		"Operation ids are execution-scoped labels; a matching id is not proof of the same operation.")

	provenance := combinedPlanProvenance(lev, rev)
	leftOps := operationMap(lev.Operations)
	rightOps := operationMap(rev.Operations)
	leftSources := sourceIndexFor(lev)
	rightSources := sourceIndexFor(rev)
	ids := unionKeys(leftOps, rightOps)
	for _, id := range ids {
		lo, lok := leftOps[id]
		ro, rok := rightOps[id]
		switch {
		case !rok:
			b.addDiff(Difference{
				Category: CategoryOperations, Kind: KindRemoved, Entity: "operation", ID: id,
				Field: "operation", Left: operationLabel(lo), Provenance: provenance,
			})
		case !lok:
			b.addDiff(Difference{
				Category: CategoryOperations, Kind: KindAdded, Entity: "operation", ID: id,
				Field: "operation", Right: operationLabel(ro), Provenance: provenance,
			})
		default:
			if lo.StepID != ro.StepID {
				b.addDiff(Difference{
					Category: CategoryOperations, Kind: KindChanged, Entity: "operation", ID: id,
					Field: "step_id", Left: lo.StepID, Right: ro.StepID,
					Provenance: provenance,
				})
			}
			if lo.ToolID != ro.ToolID {
				b.addDiff(Difference{
					Category: CategoryOperations, Kind: KindChanged, Entity: "operation", ID: id,
					Field: "tool_id", Left: lo.ToolID, Right: ro.ToolID,
					Provenance: provenance,
				})
			}
			if lo.Operation != ro.Operation {
				b.addDiff(Difference{
					Category: CategoryOperations, Kind: KindChanged, Entity: "operation", ID: id,
					Field: "operation", Left: lo.Operation, Right: ro.Operation,
					Provenance: provenance,
				})
			}
			compareOperationSources(b, id, lo.SourceIDs, ro.SourceIDs, leftSources, rightSources)
		}
	}
}

func (b *comparisonBuilder) compareEvidence(left, right execjournal.ExecutionRecord) {
	lev, rev := left.ResearchEvidence, right.ResearchEvidence
	switch {
	case lev == nil && rev == nil:
		b.setCategory(CategoryEvidence, CategoryUnavailable)
		b.addFinding(FindingEvidenceUnavailableBoth, SeverityWarning, CategoryEvidence,
			"No complete evidence snapshot is available on either side; absence is not an empty search.")
		return
	case lev == nil:
		b.setCategory(CategoryEvidence, CategoryPartial)
		b.addFinding(FindingEvidenceMissingOnLeft, SeverityWarning, CategoryEvidence,
			"Evidence is absent on the left; absence is not an empty search.")
		return
	case rev == nil:
		b.setCategory(CategoryEvidence, CategoryPartial)
		b.addFinding(FindingEvidenceMissingOnRight, SeverityWarning, CategoryEvidence,
			"Evidence is absent on the right; absence is not an empty search.")
		return
	}

	if lev.Version == rev.Version {
		b.setCategory(CategoryEvidence, CategoryComparable)
	} else {
		b.setCategory(CategoryEvidence, CategoryPartial)
		b.addFinding(FindingEvidenceVersionChanged, SeverityWarning, CategoryEvidence,
			"Evidence schema versions differ; snapshot fingerprints are not comparable, but recorded source records are compared by their canonical fingerprints.")
		b.addFinding(FindingEvidenceFingerprintIncomp, SeverityInfo, CategoryEvidence,
			"The evidence fingerprints are recorded but are not compared across schema versions.")
	}

	// The full evidence fingerprint is deliberately not compared across
	// executions: v2/v3 plan ids are execution-scoped and must differ, so the
	// fingerprint would always differ even when every comparable recorded fact
	// matches. The side summaries keep each raw fingerprint for auditability;
	// differences are emitted only for facts that are safe to compare.
	provenance := combinedEvidenceProvenance(lev, rev)
	if lev.Capability != rev.Capability {
		b.addDiff(Difference{
			Category: CategoryEvidence, Kind: KindChanged, Entity: "evidence",
			Field: "capability", Left: lev.Capability, Right: rev.Capability,
			Provenance: provenance,
		})
	}
	if lev.Operation != rev.Operation {
		b.addDiff(Difference{
			Category: CategoryEvidence, Kind: KindChanged, Entity: "evidence",
			Field: "operation", Left: lev.Operation, Right: rev.Operation,
			Provenance: provenance,
		})
	}
	if lev.Count != rev.Count {
		b.addDiff(Difference{
			Category: CategoryEvidence, Kind: KindChanged, Entity: "evidence",
			Field: "count", Left: fmt.Sprintf("%d", lev.Count), Right: fmt.Sprintf("%d", rev.Count),
			Provenance: provenance,
		})
	}
	if lev.Version == execjournal.ResearchEvidenceVersion &&
		rev.Version == execjournal.ResearchEvidenceVersion && lev.Attribution != rev.Attribution {
		b.addDiff(Difference{
			Category: CategoryEvidence, Kind: KindChanged, Entity: "source_attribution",
			Field: "state", Left: displayOrNone(lev.Attribution), Right: displayOrNone(rev.Attribution),
			Provenance: ProvenanceDerived,
		})
	}
	if lev.Version == execjournal.ResearchEvidenceProvenanceVersion ||
		rev.Version == execjournal.ResearchEvidenceProvenanceVersion ||
		lev.Version == execjournal.ResearchEvidenceLegacyVersion ||
		rev.Version == execjournal.ResearchEvidenceLegacyVersion {
		b.addFinding(FindingLegacyEvidenceNoProvenance, SeverityWarning, CategoryEvidence,
			"At least one side carries legacy evidence without the full v3 provenance markers.")
	}

	ls := sourceIndexFor(lev)
	rs := sourceIndexFor(rev)
	compareSourceRecords(b, ls, rs)
	compareSourceURLs(b, ls, rs)
}

func (b *comparisonBuilder) compareSynthesis(left, right execjournal.ExecutionRecord) {
	ls, rs := left.ResearchSynthesis, right.ResearchSynthesis
	switch {
	case ls == nil && rs == nil:
		b.setCategory(CategorySynthesis, CategoryUnavailable)
		b.addFinding(FindingSynthesisUnavailableBoth, SeverityInfo, CategorySynthesis,
			"No durable synthesis is present on either side; absence is not an empty synthesis.")
		return
	case ls == nil:
		b.setCategory(CategorySynthesis, CategoryPartial)
		b.addFinding(FindingSynthesisMissingOnLeft, SeverityWarning, CategorySynthesis,
			"Synthesis is absent on the left; absence is not an empty synthesis.")
		return
	case rs == nil:
		b.setCategory(CategorySynthesis, CategoryPartial)
		b.addFinding(FindingSynthesisMissingOnRight, SeverityWarning, CategorySynthesis,
			"Synthesis is absent on the right; absence is not an empty synthesis.")
		return
	}

	if ls.Version != rs.Version || ls.Method != rs.Method {
		b.setCategory(CategorySynthesis, CategoryIncompatible)
		b.addFinding(FindingSynthesisSchemaIncompatible, SeverityError, CategorySynthesis,
			"Synthesis schema version or method differs; the artifacts cannot be compared field-by-field.")
		return
	}
	b.setCategory(CategorySynthesis, CategoryComparable)

	if ls.Status != rs.Status {
		b.addDiff(Difference{
			Category: CategorySynthesis, Kind: KindChanged, Entity: "synthesis",
			Field: "status", Left: ls.Status, Right: rs.Status, Provenance: ProvenanceDerived,
		})
	}
	compareSynthesisPlanVersion(b, ls.PlanVersion, rs.PlanVersion)
	if ls.PlanVersion == 0 || rs.PlanVersion == 0 {
		b.setCategory(CategorySynthesis, CategoryPartial)
	}
	// Evidence/synthesis fingerprints are bindings to execution-scoped plan
	// ids. They remain visible in the side summaries but are not compared as
	// content, because their execution-scoped components must differ.
	compareCount(b, CategorySynthesis, "synthesis", "evidence_backed_count",
		len(ls.EvidenceBacked), len(rs.EvidenceBacked))
	compareCount(b, CategorySynthesis, "synthesis", "inference_count",
		len(ls.Inferences), len(rs.Inferences))
	compareCount(b, CategorySynthesis, "synthesis", "conflict_count",
		len(ls.Conflicts), len(rs.Conflicts))
	compareCount(b, CategorySynthesis, "synthesis", "question_count",
		len(ls.UnresolvedQuestions), len(rs.UnresolvedQuestions))

	lsIdx := sourceIndexFor(left.ResearchEvidence)
	rsIdx := sourceIndexFor(right.ResearchEvidence)
	compareSynthesisStatements(b, "synthesis_statement", ls.EvidenceBacked, rs.EvidenceBacked, lsIdx, rsIdx)
	compareSynthesisStatements(b, "synthesis_conflict", ls.Conflicts, rs.Conflicts, lsIdx, rsIdx)
	compareTextSets(b, CategorySynthesis, "synthesis_inference", inferenceTexts(ls.Inferences), inferenceTexts(rs.Inferences))
	compareTextSets(b, CategorySynthesis, "synthesis_question", ls.UnresolvedQuestions, rs.UnresolvedQuestions)

	if lf, rf := textFingerprint(ls.Summary), textFingerprint(rs.Summary); lf != rf {
		b.addDiff(Difference{
			Category: CategorySynthesis, Kind: KindChanged, Entity: "synthesis",
			Field: "summary_fingerprint", LeftFingerprint: lf, RightFingerprint: rf,
			Provenance: ProvenanceDerived,
		})
	}
	compareSynthesisCitations(b, ls, rs, lsIdx, rsIdx)
}

func (b *comparisonBuilder) compareEvaluation(left, right execjournal.ExecutionRecord) {
	le, re := left.ResearchEvaluation, right.ResearchEvaluation
	switch {
	case le == nil && re == nil:
		b.setCategory(CategoryEvaluation, CategoryUnavailable)
		b.addFinding(FindingEvaluationUnavailableBoth, SeverityInfo, CategoryEvaluation,
			"No quality evaluation is present on either side; absence is not a clean evaluation.")
		return
	case le == nil:
		b.setCategory(CategoryEvaluation, CategoryPartial)
		b.addFinding(FindingEvaluationMissingOnLeft, SeverityWarning, CategoryEvaluation,
			"Evaluation is absent on the left; absence is not a clean evaluation.")
		b.addFinding(FindingEvaluationAbsentNotClean, SeverityWarning, CategoryEvaluation,
			"An absent evaluation is not a clean verdict; the present evaluation is reported only as recorded.")
		return
	case re == nil:
		b.setCategory(CategoryEvaluation, CategoryPartial)
		b.addFinding(FindingEvaluationMissingOnRight, SeverityWarning, CategoryEvaluation,
			"Evaluation is absent on the right; absence is not a clean evaluation.")
		b.addFinding(FindingEvaluationAbsentNotClean, SeverityWarning, CategoryEvaluation,
			"An absent evaluation is not a clean verdict; the present evaluation is reported only as recorded.")
		return
	}

	if le.Version != re.Version || le.Method != re.Method {
		b.setCategory(CategoryEvaluation, CategoryIncompatible)
		b.addFinding(FindingEvaluationSchemaIncompatible, SeverityError, CategoryEvaluation,
			"Evaluation schema version or method differs; the verdicts cannot be compared field-by-field.")
		return
	}
	b.setCategory(CategoryEvaluation, CategoryComparable)

	if le.Status != re.Status {
		b.addDiff(Difference{
			Category: CategoryEvaluation, Kind: KindChanged, Entity: "evaluation",
			Field: "status", Left: le.Status, Right: re.Status, Provenance: ProvenanceDerived,
		})
	}
	compareEvaluationPlanVersion(b, le.PlanVersion, re.PlanVersion)
	if le.PlanVersion == 0 || re.PlanVersion == 0 {
		b.setCategory(CategoryEvaluation, CategoryPartial)
	}
	if le.EvidenceVersion != re.EvidenceVersion {
		b.addDiff(Difference{
			Category: CategoryEvaluation, Kind: KindChanged, Entity: "evaluation",
			Field: "evidence_version", Left: fmt.Sprintf("%d", le.EvidenceVersion),
			Right: fmt.Sprintf("%d", re.EvidenceVersion), Provenance: ProvenanceDerived,
		})
	}
	if le.EvidenceCount != re.EvidenceCount {
		b.addDiff(Difference{
			Category: CategoryEvaluation, Kind: KindChanged, Entity: "evaluation",
			Field: "evidence_count", Left: fmt.Sprintf("%d", le.EvidenceCount),
			Right: fmt.Sprintf("%d", re.EvidenceCount), Provenance: ProvenanceDerived,
		})
	}
	if le.SynthesisStatus != re.SynthesisStatus {
		b.addDiff(Difference{
			Category: CategoryEvaluation, Kind: KindChanged, Entity: "evaluation",
			Field: "synthesis_status", Left: displayOrNone(le.SynthesisStatus),
			Right: displayOrNone(re.SynthesisStatus), Provenance: ProvenanceDerived,
		})
	}
	// Evidence/synthesis fingerprints are execution-scoped bindings; they are
	// summarized on each side but not treated as comparable content.
	compareCount(b, CategoryEvaluation, "evaluation", "finding_count",
		len(le.Findings), len(re.Findings))
	compareDimensions(b, le.Dimensions, re.Dimensions)
	compareEvaluationFindings(b, le.Findings, re.Findings)
	compareTextSets(b, CategoryEvaluation, "evaluation_limitation", le.Limitations, re.Limitations)
}

func bothPlans(lev, rev *execjournal.ResearchEvidence) bool {
	return lev != nil && rev != nil && lev.Plan != nil && rev.Plan != nil &&
		lev.Version >= execjournal.ResearchEvidenceProvenanceVersion &&
		rev.Version >= execjournal.ResearchEvidenceProvenanceVersion
}

func planProvenance(ev *execjournal.ResearchEvidence) string {
	if ev != nil && ev.Version == execjournal.ResearchEvidenceVersion {
		return ProvenanceAuthoritative
	}
	return ProvenanceLegacy
}

// combinedPlanProvenance labels a plan/step/operation difference
// conservatively. A fact is authoritative only when both snapshots are v3;
// if either side is legacy, the difference is legacy.
func combinedPlanProvenance(left, right *execjournal.ResearchEvidence) string {
	if left == nil || right == nil {
		return ProvenanceUnavailable
	}
	if left.Version == execjournal.ResearchEvidenceVersion &&
		right.Version == execjournal.ResearchEvidenceVersion {
		return ProvenanceAuthoritative
	}
	return ProvenanceLegacy
}

// combinedEvidenceProvenance labels an evidence-metadata difference. The
// source-attribution marker is only authoritative when both snapshots are v3.
func combinedEvidenceProvenance(left, right *execjournal.ResearchEvidence) string {
	return combinedPlanProvenance(left, right)
}

func compareDependencies(b *comparisonBuilder, stepID string, left, right []string, provenance string) {
	leftSet := stringSet(left)
	rightSet := stringSet(right)
	for _, dep := range sortedSet(leftSet) {
		if _, ok := rightSet[dep]; !ok {
			b.addDiff(Difference{
				Category: CategorySteps, Kind: KindRemoved, Entity: "step_dependency",
				ID: stepID, Field: "dependency", Left: dep, Provenance: provenance,
			})
		}
	}
	for _, dep := range sortedSet(rightSet) {
		if _, ok := leftSet[dep]; !ok {
			b.addDiff(Difference{
				Category: CategorySteps, Kind: KindAdded, Entity: "step_dependency",
				ID: stepID, Field: "dependency", Right: dep, Provenance: provenance,
			})
		}
	}
}

func compareOperationSources(b *comparisonBuilder, opID string, left, right []string, leftIdx, rightIdx sourceIndex) {
	leftSet := map[string]struct{}{}
	rightSet := map[string]struct{}{}
	for _, id := range left {
		fp := leftIdx.byID[id]
		if fp == "" {
			fp = textFingerprint("missing\x00" + id)
		}
		leftSet[fp] = struct{}{}
	}
	for _, id := range right {
		fp := rightIdx.byID[id]
		if fp == "" {
			fp = textFingerprint("missing\x00" + id)
		}
		rightSet[fp] = struct{}{}
	}
	for _, fp := range sortedSet(leftSet) {
		if _, ok := rightSet[fp]; !ok {
			b.addDiff(Difference{
				Category: CategoryOperations, Kind: KindRemoved, Entity: "operation_source",
				ID: opID, Field: "source", Left: leftIdx.label(fp), LeftFingerprint: fp,
				Provenance: ProvenanceDerived,
			})
		}
	}
	for _, fp := range sortedSet(rightSet) {
		if _, ok := leftSet[fp]; !ok {
			b.addDiff(Difference{
				Category: CategoryOperations, Kind: KindAdded, Entity: "operation_source",
				ID: opID, Field: "source", Right: rightIdx.label(fp), RightFingerprint: fp,
				Provenance: ProvenanceDerived,
			})
		}
	}
}

func compareSourceRecords(b *comparisonBuilder, left, right sourceIndex) {
	for _, fp := range sortedCountKeys(left.counts, right.counts) {
		lc, rc := left.counts[fp], right.counts[fp]
		for i := 0; i < lc-rc; i++ {
			b.addDiff(Difference{
				Category: CategoryEvidence, Kind: KindRemoved, Entity: "source", ID: fp,
				Field: "record", Left: left.label(fp), LeftFingerprint: fp,
				Provenance: ProvenanceDerived,
			})
		}
		for i := 0; i < rc-lc; i++ {
			b.addDiff(Difference{
				Category: CategoryEvidence, Kind: KindAdded, Entity: "source", ID: fp,
				Field: "record", Right: right.label(fp), RightFingerprint: fp,
				Provenance: ProvenanceDerived,
			})
		}
	}
}

func compareSourceURLs(b *comparisonBuilder, left, right sourceIndex) {
	urls := map[string]struct{}{}
	for u := range left.urls {
		urls[u] = struct{}{}
	}
	for u := range right.urls {
		urls[u] = struct{}{}
	}
	for _, u := range sortedSet(urls) {
		lset, lok := left.urls[u]
		rset, rok := right.urls[u]
		if !lok || !rok {
			continue
		}
		if !sameStringSet(lset, rset) {
			b.addFinding(FindingSourceURLNotIdentity, SeverityWarning, CategoryEvidence,
				"An identical source URL appears on both sides, but the recorded source records differ; URL equality is not evidence identity.")
		}
	}
}

func compareSynthesisPlanVersion(b *comparisonBuilder, left, right int) {
	switch {
	case left == 0 && right == 0:
		b.addFinding(FindingSynthesisPlanVersionBoth, SeverityWarning, CategorySynthesis,
			"Neither synthesis recorded a plan revision; the binding is unavailable, not zero-valued.")
	case left == 0:
		b.addFinding(FindingSynthesisPlanVersionLeft, SeverityWarning, CategorySynthesis,
			"The left synthesis did not record a plan revision; the binding is unavailable.")
	case right == 0:
		b.addFinding(FindingSynthesisPlanVersionRight, SeverityWarning, CategorySynthesis,
			"The right synthesis did not record a plan revision; the binding is unavailable.")
	case left != right:
		b.addDiff(Difference{
			Category: CategorySynthesis, Kind: KindChanged, Entity: "synthesis",
			Field: "plan_version", Left: fmt.Sprintf("%d", left), Right: fmt.Sprintf("%d", right),
			Provenance: ProvenanceDerived,
		})
	}
}

func compareEvaluationPlanVersion(b *comparisonBuilder, left, right int) {
	switch {
	case left == 0 && right == 0:
		b.addFinding(FindingEvaluationPlanVersionBoth, SeverityWarning, CategoryEvaluation,
			"Neither evaluation recorded a plan revision; the binding is unavailable, not zero-valued.")
	case left == 0:
		b.addFinding(FindingEvaluationPlanVersionLeft, SeverityWarning, CategoryEvaluation,
			"The left evaluation did not record a plan revision; the binding is unavailable.")
	case right == 0:
		b.addFinding(FindingEvaluationPlanVersionRight, SeverityWarning, CategoryEvaluation,
			"The right evaluation did not record a plan revision; the binding is unavailable.")
	case left != right:
		b.addDiff(Difference{
			Category: CategoryEvaluation, Kind: KindChanged, Entity: "evaluation",
			Field: "plan_version", Left: fmt.Sprintf("%d", left), Right: fmt.Sprintf("%d", right),
			Provenance: ProvenanceDerived,
		})
	}
}

func compareCount(b *comparisonBuilder, category, entity, field string, left, right int) {
	if left == right {
		return
	}
	b.addDiff(Difference{
		Category: category, Kind: KindChanged, Entity: entity, Field: field,
		Left: fmt.Sprintf("%d", left), Right: fmt.Sprintf("%d", right),
		Provenance: ProvenanceDerived,
	})
}

func compareSynthesisStatements(b *comparisonBuilder, entity string, left, right []execjournal.ResearchSynthesisStatement, leftIdx, rightIdx sourceIndex) {
	leftSet := map[string]int{}
	rightSet := map[string]int{}
	leftLabels := map[string]string{}
	rightLabels := map[string]string{}
	for _, st := range left {
		fp, label := statementFingerprint(st, leftIdx)
		leftSet[fp]++
		leftLabels[fp] = label
	}
	for _, st := range right {
		fp, label := statementFingerprint(st, rightIdx)
		rightSet[fp]++
		rightLabels[fp] = label
	}
	for _, fp := range sortedCountKeys(leftSet, rightSet) {
		lc, rc := leftSet[fp], rightSet[fp]
		for i := 0; i < lc-rc; i++ {
			b.addDiff(Difference{
				Category: CategorySynthesis, Kind: KindRemoved, Entity: entity, ID: fp,
				Field: "statement", Left: leftLabels[fp], LeftFingerprint: fp,
				Provenance: ProvenanceDerived,
			})
		}
		for i := 0; i < rc-lc; i++ {
			b.addDiff(Difference{
				Category: CategorySynthesis, Kind: KindAdded, Entity: entity, ID: fp,
				Field: "statement", Right: rightLabels[fp], RightFingerprint: fp,
				Provenance: ProvenanceDerived,
			})
		}
	}
}

func compareSynthesisCitations(b *comparisonBuilder, left, right *execjournal.ResearchSynthesis, leftIdx, rightIdx sourceIndex) {
	leftSet := map[string]int{}
	rightSet := map[string]int{}
	for _, st := range append(append([]execjournal.ResearchSynthesisStatement(nil), left.EvidenceBacked...), left.Conflicts...) {
		for _, id := range st.SourceIDs {
			fp := leftIdx.byID[id]
			if fp == "" {
				fp = textFingerprint("missing\x00" + id)
			}
			leftSet[fp]++
		}
	}
	for _, st := range append(append([]execjournal.ResearchSynthesisStatement(nil), right.EvidenceBacked...), right.Conflicts...) {
		for _, id := range st.SourceIDs {
			fp := rightIdx.byID[id]
			if fp == "" {
				fp = textFingerprint("missing\x00" + id)
			}
			rightSet[fp]++
		}
	}
	for _, fp := range sortedCountKeys(leftSet, rightSet) {
		lc, rc := leftSet[fp], rightSet[fp]
		for i := 0; i < lc-rc; i++ {
			b.addDiff(Difference{
				Category: CategorySynthesis, Kind: KindRemoved, Entity: "synthesis_citation",
				ID: fp, Field: "source", Left: leftIdx.label(fp), LeftFingerprint: fp,
				Provenance: ProvenanceDerived,
			})
		}
		for i := 0; i < rc-lc; i++ {
			b.addDiff(Difference{
				Category: CategorySynthesis, Kind: KindAdded, Entity: "synthesis_citation",
				ID: fp, Field: "source", Right: rightIdx.label(fp), RightFingerprint: fp,
				Provenance: ProvenanceDerived,
			})
		}
	}
}

func compareTextSets(b *comparisonBuilder, category, entity string, left, right []string) {
	leftSet := map[string]int{}
	rightSet := map[string]int{}
	for _, value := range left {
		leftSet[textFingerprint(value)]++
	}
	for _, value := range right {
		rightSet[textFingerprint(value)]++
	}
	for _, fp := range sortedCountKeys(leftSet, rightSet) {
		lc, rc := leftSet[fp], rightSet[fp]
		for i := 0; i < lc-rc; i++ {
			b.addDiff(Difference{
				Category: category, Kind: KindRemoved, Entity: entity, ID: fp,
				Field: "text", Provenance: ProvenanceDerived,
			})
		}
		for i := 0; i < rc-lc; i++ {
			b.addDiff(Difference{
				Category: category, Kind: KindAdded, Entity: entity, ID: fp,
				Field: "text", Provenance: ProvenanceDerived,
			})
		}
	}
}

func compareDimensions(b *comparisonBuilder, left, right []execjournal.ResearchEvaluationDimension) {
	leftMap := map[string]string{}
	rightMap := map[string]string{}
	for _, d := range left {
		leftMap[d.ID] = d.Status
	}
	for _, d := range right {
		rightMap[d.ID] = d.Status
	}
	for _, id := range unionKeys(leftMap, rightMap) {
		ls, lok := leftMap[id]
		rs, rok := rightMap[id]
		switch {
		case !rok:
			b.addDiff(Difference{
				Category: CategoryEvaluation, Kind: KindRemoved, Entity: "evaluation_dimension",
				ID: id, Field: "status", Left: ls, Provenance: ProvenanceDerived,
			})
		case !lok:
			b.addDiff(Difference{
				Category: CategoryEvaluation, Kind: KindAdded, Entity: "evaluation_dimension",
				ID: id, Field: "status", Right: rs, Provenance: ProvenanceDerived,
			})
		case ls != rs:
			b.addDiff(Difference{
				Category: CategoryEvaluation, Kind: KindChanged, Entity: "evaluation_dimension",
				ID: id, Field: "status", Left: ls, Right: rs, Provenance: ProvenanceDerived,
			})
		}
	}
}

func compareEvaluationFindings(b *comparisonBuilder, left, right []execjournal.ResearchEvaluationFinding) {
	// A finding is compared as the complete recorded artifact: stable code,
	// dimension, severity, message fingerprint and any recorded source/step
	// labels. Using a multiset avoids silently dropping duplicate or
	// same-code/different-message findings through a map key.
	leftSet := map[string]int{}
	rightSet := map[string]int{}
	leftLabels := map[string]string{}
	rightLabels := map[string]string{}
	for _, f := range left {
		fp := evaluationFindingFingerprint(f)
		leftSet[fp]++
		leftLabels[fp] = evaluationFindingLabel(f)
	}
	for _, f := range right {
		fp := evaluationFindingFingerprint(f)
		rightSet[fp]++
		rightLabels[fp] = evaluationFindingLabel(f)
	}
	for _, fp := range sortedCountKeys(leftSet, rightSet) {
		lc, rc := leftSet[fp], rightSet[fp]
		for i := 0; i < lc-rc; i++ {
			b.addDiff(Difference{
				Category: CategoryEvaluation, Kind: KindRemoved, Entity: "evaluation_finding",
				ID: fp, Field: "finding", Left: leftLabels[fp],
				LeftFingerprint: fp, Provenance: ProvenanceDerived,
			})
		}
		for i := 0; i < rc-lc; i++ {
			b.addDiff(Difference{
				Category: CategoryEvaluation, Kind: KindAdded, Entity: "evaluation_finding",
				ID: fp, Field: "finding", Right: rightLabels[fp],
				RightFingerprint: fp, Provenance: ProvenanceDerived,
			})
		}
	}
}

func evaluationFindingFingerprint(f execjournal.ResearchEvaluationFinding) string {
	raw, err := json.Marshal(struct {
		Code      string `json:"code"`
		Dimension string `json:"dimension"`
		Severity  string `json:"severity"`
		Message   string `json:"message"`
		SourceID  string `json:"source_id,omitempty"`
		StepID    string `json:"step_id,omitempty"`
	}{
		Code: f.Code, Dimension: f.Dimension, Severity: f.Severity,
		Message: f.Message, SourceID: f.SourceID, StepID: f.StepID,
	})
	if err != nil {
		return textFingerprint(f.Code + "\x00" + f.Dimension + "\x00" + f.Severity)
	}
	return sha256Hex(raw)
}

func evaluationFindingLabel(f execjournal.ResearchEvaluationFinding) string {
	parts := []string{f.Code, f.Dimension, f.Severity}
	if f.SourceID != "" {
		parts = append(parts, "source "+f.SourceID)
	}
	if f.StepID != "" {
		parts = append(parts, "step "+f.StepID)
	}
	return strings.Join(parts, " / ")
}

func inferenceTexts(inferences []execjournal.ResearchSynthesisInference) []string {
	out := make([]string, 0, len(inferences))
	for _, inference := range inferences {
		out = append(out, inference.Statement)
	}
	return out
}

type sourceIndex struct {
	counts map[string]int
	labels map[string]string
	urls   map[string]map[string]struct{}
	byID   map[string]string
}

func (s sourceIndex) label(fingerprint string) string {
	if label, ok := s.labels[fingerprint]; ok && label != "" {
		return label
	}
	return fingerprint
}

func sourceIndexFor(ev *execjournal.ResearchEvidence) sourceIndex {
	idx := sourceIndex{
		counts: map[string]int{},
		labels: map[string]string{},
		urls:   map[string]map[string]struct{}{},
		byID:   map[string]string{},
	}
	if ev == nil {
		return idx
	}
	for _, source := range ev.Sources {
		fp := sourceFingerprint(source)
		idx.counts[fp]++
		idx.labels[fp] = sourceLabel(source)
		if idx.urls[source.URL] == nil {
			idx.urls[source.URL] = map[string]struct{}{}
		}
		idx.urls[source.URL][fp] = struct{}{}
		if source.ID != "" {
			idx.byID[source.ID] = fp
		}
	}
	return idx
}

func sourceFingerprint(source execjournal.ResearchSource) string {
	raw, err := json.Marshal(struct {
		ID      string `json:"id,omitempty"`
		Title   string `json:"title"`
		URL     string `json:"url"`
		Source  string `json:"source"`
		Snippet string `json:"snippet"`
	}{
		ID: source.ID, Title: source.Title, URL: source.URL,
		Source: source.Source, Snippet: source.Snippet,
	})
	if err != nil {
		return textFingerprint("source\x00" + source.ID + "\x00" + source.Title)
	}
	return sha256Hex(raw)
}

func sourceLabel(source execjournal.ResearchSource) string {
	switch {
	case source.Title != "" && source.URL != "":
		return clip(source.Title+" - "+source.URL, MaxText)
	case source.Title != "":
		return clip(source.Title, MaxText)
	case source.URL != "":
		return clip(source.URL, MaxText)
	case source.Source != "":
		return clip(source.Source, MaxText)
	default:
		return "source record"
	}
}

func statementFingerprint(statement execjournal.ResearchSynthesisStatement, idx sourceIndex) (string, string) {
	citations := make([]string, 0, len(statement.SourceIDs))
	labels := make([]string, 0, len(statement.SourceIDs))
	for _, id := range statement.SourceIDs {
		fp := idx.byID[id]
		if fp == "" {
			fp = textFingerprint("missing\x00" + id)
		}
		citations = append(citations, fp)
		label := idx.label(fp)
		if label == "" || label == fp {
			label = id
		}
		labels = append(labels, label)
	}
	sort.Strings(citations)
	sort.Strings(labels)
	raw, err := json.Marshal(struct {
		Statement string   `json:"statement"`
		Citations []string `json:"citations"`
	}{Statement: statement.Statement, Citations: citations})
	if err != nil {
		return textFingerprint(statement.Statement), clip(strings.Join(labels, " | "), MaxText)
	}
	return sha256Hex(raw), clip(strings.Join(labels, " | "), MaxText)
}

func sideView(role string, rec execjournal.ExecutionRecord) Side {
	view := Side{
		Role:        role,
		ExecutionID: rec.ID,
		DivisionID:  rec.DivisionID,
		State:       string(rec.State),
		CreatedAt:   rec.CreatedAt,
		CompletedAt: rec.CompletedAt,
		Provenance:  provenanceSummary(rec),
	}
	if rec.ResearchEvidence != nil {
		ev := rec.ResearchEvidence
		view.Evidence = &EvidenceSummary{
			Version:        ev.Version,
			Capability:     ev.Capability,
			Operation:      ev.Operation,
			Count:          ev.Count,
			Fingerprint:    execjournal.ResearchEvidenceFingerprint(ev),
			Attribution:    ev.Attribution,
			SourceCount:    len(ev.Sources),
			StepCount:      len(ev.Steps),
			OperationCount: len(ev.Operations),
		}
		if ev.Plan != nil {
			view.Plan = &PlanSummary{
				PlanID:             ev.Plan.PlanID,
				Version:            ev.Plan.Version,
				ObjectiveID:        ev.Plan.ObjectiveID,
				CompletionStepID:   ev.Plan.CompletionStepID,
				CompletionRecorded: ev.Version == execjournal.ResearchEvidenceVersion,
				Provenance:         planProvenance(ev),
			}
		}
	}
	if rec.ResearchSynthesis != nil {
		s := rec.ResearchSynthesis
		view.Synthesis = &SynthesisSummary{
			Version:             s.Version,
			Method:              s.Method,
			Status:              s.Status,
			PlanVersion:         s.PlanVersion,
			PlanVersionRecorded: s.PlanVersion > 0,
			EvidenceFingerprint: s.EvidenceFingerprint,
			Fingerprint:         execjournal.ResearchSynthesisFingerprint(s),
			EvidenceBackedCount: len(s.EvidenceBacked),
			InferenceCount:      len(s.Inferences),
			ConflictCount:       len(s.Conflicts),
			QuestionCount:       len(s.UnresolvedQuestions),
		}
	}
	if rec.ResearchEvaluation != nil {
		e := rec.ResearchEvaluation
		view.Evaluation = &EvaluationSummary{
			Version:              e.Version,
			Method:               e.Method,
			Status:               e.Status,
			EvidenceVersion:      e.EvidenceVersion,
			EvidenceCount:        e.EvidenceCount,
			EvidenceFingerprint:  e.EvidenceFingerprint,
			PlanVersion:          e.PlanVersion,
			PlanVersionRecorded:  e.PlanVersion > 0,
			SynthesisStatus:      e.SynthesisStatus,
			SynthesisFingerprint: e.SynthesisFingerprint,
			DimensionCount:       len(e.Dimensions),
			FindingCount:         len(e.Findings),
		}
	}
	return view
}

func provenanceSummary(rec execjournal.ExecutionRecord) ProvenanceSummary {
	p := ProvenanceSummary{
		Evidence: ProvenanceUnavailable, Plan: ProvenanceUnavailable,
		Steps: ProvenanceUnavailable, Operations: ProvenanceUnavailable,
		SourceAttribution: ProvenanceUnavailable, Synthesis: ProvenanceUnavailable,
		Evaluation: ProvenanceUnavailable,
	}
	if rec.ResearchSynthesis != nil {
		p.Synthesis = ProvenanceDerived
	}
	if rec.ResearchEvaluation != nil {
		p.Evaluation = ProvenanceDerived
	}
	ev := rec.ResearchEvidence
	if ev == nil {
		return p
	}
	switch ev.Version {
	case execjournal.ResearchEvidenceVersion:
		p.Evidence = ProvenanceAuthoritative
		if ev.Plan != nil {
			p.Plan = ProvenanceAuthoritative
			p.Steps = ProvenanceAuthoritative
			p.Operations = ProvenanceAuthoritative
		}
		if ev.Attribution == execjournal.ResearchAttributionDerived {
			p.SourceAttribution = ProvenanceDerived
		} else if ev.Attribution == execjournal.ResearchAttributionUnavailable {
			p.SourceAttribution = ProvenanceUnavailable
		}
	case execjournal.ResearchEvidenceProvenanceVersion:
		p.Evidence = ProvenanceLegacy
		if ev.Plan != nil {
			p.Plan = ProvenanceLegacy
			p.Steps = ProvenanceLegacy
			p.Operations = ProvenanceLegacy
			if len(ev.Operations) > 0 {
				p.SourceAttribution = ProvenanceLegacy
			}
		}
	case execjournal.ResearchEvidenceLegacyVersion:
		p.Evidence = ProvenanceLegacy
	}
	return p
}

func stepMap(steps []execjournal.ResearchStepEvidence) map[string]execjournal.ResearchStepEvidence {
	out := make(map[string]execjournal.ResearchStepEvidence, len(steps))
	for _, step := range steps {
		out[step.StepID] = step
	}
	return out
}

func operationMap(ops []execjournal.ResearchOperationEvidence) map[string]execjournal.ResearchOperationEvidence {
	out := make(map[string]execjournal.ResearchOperationEvidence, len(ops))
	for _, op := range ops {
		out[op.OperationID] = op
	}
	return out
}

func operationLabel(op execjournal.ResearchOperationEvidence) string {
	return clip(strings.Join([]string{op.StepID, op.ToolID, op.Operation}, "/"), MaxText)
}

func unionKeys[V any](left, right map[string]V) []string {
	seen := make(map[string]struct{}, len(left)+len(right))
	for key := range left {
		seen[key] = struct{}{}
	}
	for key := range right {
		seen[key] = struct{}{}
	}
	return sortedSet(seen)
}

func sortedCountKeys(left, right map[string]int) []string {
	seen := make(map[string]struct{}, len(left)+len(right))
	for key := range left {
		seen[key] = struct{}{}
	}
	for key := range right {
		seen[key] = struct{}{}
	}
	return sortedSet(seen)
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func sortedSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sameStringSet(left, right map[string]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for value := range left {
		if _, ok := right[value]; !ok {
			return false
		}
	}
	return true
}

func displayOrNone(value string) string {
	if value == "" {
		return "(none)"
	}
	return value
}

func textFingerprint(value string) string {
	return sha256Hex([]byte(value))
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func clip(value string, max int) string {
	if value == "" {
		return ""
	}
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r == 0 || r == '\n' || r == '\r' || r == '\t':
			return ' '
		case r < 0x20 || r == 0x7f:
			return -1
		default:
			return r
		}
	}, value)
	if len(cleaned) <= max {
		return cleaned
	}
	if max <= 3 {
		return cleaned[:max]
	}
	limit := max - 3
	for limit > 0 && !utf8.ValidString(cleaned[:limit]) {
		limit--
	}
	return cleaned[:limit] + "..."
}

func validExecutionState(state execjournal.State) bool {
	switch state {
	case execjournal.StateAccepted, execjournal.StateRunning, execjournal.StatePending,
		execjournal.StateCompleted, execjournal.StateFailed, execjournal.StateCancelled,
		execjournal.StateTimedOut, execjournal.StateUnknown:
		return true
	default:
		return false
	}
}

func categoryRank(category string) int {
	for i, value := range categoryOrder {
		if value == category {
			return i
		}
	}
	return len(categoryOrder)
}

func worseCategoryStatus(left, right string) string {
	rank := func(value string) int {
		switch value {
		case CategoryComparable:
			return 0
		case CategoryUnavailable:
			return 1
		case CategoryPartial:
			return 2
		case CategoryIncompatible:
			return 3
		default:
			return 2
		}
	}
	if rank(right) > rank(left) {
		return right
	}
	return left
}

func sortDifferences(diffs []Difference) {
	sort.SliceStable(diffs, func(i, j int) bool {
		a, b := diffs[i], diffs[j]
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
	})
}

func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if categoryRank(a.Category) != categoryRank(b.Category) {
			return categoryRank(a.Category) < categoryRank(b.Category)
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Severity != b.Severity {
			return a.Severity < b.Severity
		}
		return a.Message < b.Message
	})
}
