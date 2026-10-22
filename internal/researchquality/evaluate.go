// Package researchquality implements NEXUS v58's deterministic research quality
// evaluation. It is a pure projection over the existing typed synthesis and the
// durable ResearchEvidence v2 snapshot; it has no model calls, no network, no
// independent evidence store and no authority to change either input.
//
// What each dimension measures in v1:
//
//   - evidence_coverage: every evidence-backed statement carries at least one
//     citation that resolves in this execution. This is structural coverage,
//     NOT semantic entailment. A citation proves reference resolution only.
//   - citation_integrity: statement/citation shape, uniqueness, bounds and
//     execution-scoped source resolution, plus evidence snapshot binding.
//   - relevance: deterministic meaningful-token overlap between a source record
//     and the objective (and an attributed step intent). It is a cheap lexical
//     signal, not a semantic relevance judgment.
//   - source_quality: absolute http(s) URL shape, usable title/excerpt and
//     raw-record information density. It never fetches a page.
//   - source_diversity: exact canonical-URL duplication, exact normalized
//     title+excerpt duplication, and host count. Host diversity is not proof of
//     independent corroboration.
//   - conflict_handling: only conflicts the synthesis explicitly declares can be
//     checked for citations. Undisclosed contradictions between excerpts are
//     not detected.
//   - uncertainty_handling: partial/inconclusive status must be accompanied by
//     unresolved questions, and a complete synthesis may not be asserted over a
//     plan the runtime recorded as incomplete.
//   - plan_completeness: final durable plan/step statuses, including failed,
//     blocked, cancelled and nonterminal steps.
//
// The evaluator deliberately emits no score, confidence percentage, source
// reputation, publication date or corroboration claim. Those properties are not
// available from the stored records.
package researchquality

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode"

	"github.com/Nomssky/NEXUS/internal/execjournal"
	"github.com/Nomssky/NEXUS/internal/executor"
)

// Step is the runtime-owned plan fact supplied to the evaluator. Status is the
// final ledger status; an empty status means no provable lifecycle fact exists.
type Step struct {
	StepID   string
	Intent   string
	Status   string
	Optional bool
}

// Input is one execution's evaluation input. The evaluator never mutates it.
type Input struct {
	ExecutionID string
	Question    string
	Evidence    *execjournal.ResearchEvidence
	Synthesis   *executor.ResearchSynthesis
	Plan        []Step
	// CurrentStepID is the step that was executing when a completed execution
	// settled. The runtime settles that step through the completion action
	// rather than through an observation, so its ledger status can still read
	// "executing". It is excluded from plan-completeness exactly like the
	// runtime's own completionIsPartial verdict excludes it. An empty value
	// means no step was in flight.
	CurrentStepID string
	// Attempted records that this execution invoked the mediated web.search
	// capability, even if no complete snapshot could be captured. It lets the
	// evaluator distinguish "not a research run" from "research input missing".
	Attempted bool
}

const (
	findingEvidenceUnavailable       = "evidence_unavailable"
	findingEvidenceInvalid           = "evidence_invalid"
	findingEvidenceExecutionMismatch = "evidence_execution_mismatch"
	findingLegacyEvidence            = "legacy_evidence_no_provenance"
	findingSynthesisUnavailable      = "synthesis_unavailable"
	findingSynthesisWithoutEvidence  = "synthesis_without_evidence"
	findingSynthesisWithoutStable    = "synthesis_without_stable_evidence"
	findingSynthesisInvalid          = "synthesis_invalid"
	findingSynthesisStatusInvalid    = "synthesis_status_invalid"
	findingSynthesisInconclusive     = "synthesis_inconclusive_with_claims"
	findingSynthesisPartial          = "synthesis_partial_without_grounding"
	findingSynthesisCompleteEmpty    = "synthesis_complete_without_evidence"
	findingSynthesisCompletePlan     = "synthesis_complete_with_incomplete_plan"
	findingStatementDuplicate        = "statement_duplicate"
	findingStatementEmpty            = "statement_empty"
	findingStatementTooLong          = "statement_too_long"
	findingStatementNoCitations      = "statement_no_citations"
	findingStatementTooManyCitations = "statement_too_many_citations"
	findingCitationMalformed         = "citation_malformed"
	findingCitationUnknown           = "citation_unknown_source"
	findingCitationDuplicate         = "citation_duplicate"
	findingNoEvidenceBackedClaims    = "no_evidence_backed_claims"
	findingSourceInvalidURL          = "source_invalid_url"
	findingSourceLowInformation      = "source_low_information"
	findingSourcesAllLowInformation  = "sources_all_low_information"
	findingNoSearchResults           = "no_search_results"
	findingSourceDuplicateURL        = "source_duplicate_url"
	findingSourceDuplicateContent    = "source_duplicate_content"
	findingDuplicateOnlyEvidence     = "duplicate_only_evidence"
	findingSingleHostEvidence        = "single_host_evidence"
	findingSourceUnattributed        = "source_unattributed"
	findingSourceNoQueryOverlap      = "source_no_query_overlap"
	findingNoRelevantSources         = "no_relevant_sources"
	findingSourceNoStepOverlap       = "source_no_step_overlap"
	findingUnresolvedQuestions       = "unresolved_questions_missing"
	findingPlanUnavailable           = "plan_unavailable"
	findingPlanStepState             = "plan_step_state_unavailable"
	findingPlanStepFailed            = "plan_step_failed"
	findingPlanIncomplete            = "plan_incomplete"
)

var evaluationLimitations = []string{
	"Citation integrity proves that cited source ids resolve in this execution; it does not prove that the cited evidence entails the statement.",
	"Conflict handling can only check conflicts the synthesis explicitly declares. Undisclosed contradictions between source excerpts are not detected.",
	"Relevance is deterministic meaningful-token overlap with the objective or step intent; it is not a semantic relevance judgment.",
	"Source reputation, authorship, publication dates, full page content and independent corroboration are not available from the stored records.",
	"No result page is fetched and no redirect is followed, so page content, freshness and source reputation are unknown.",
}

// Evaluate returns nil for an execution that neither attempted research nor
// carried a research synthesis/evidence snapshot. Otherwise it always returns a
// complete v1 evaluation, including an honest incomplete/failed verdict.
func Evaluate(in Input) *execjournal.ResearchEvaluation {
	if !in.Attempted && in.Evidence == nil && in.Synthesis == nil {
		return nil
	}
	b := &builder{
		in: in,
		dimensions: map[string]string{
			execjournal.ResearchDimensionEvidenceCoverage:  execjournal.ResearchDimensionIncomplete,
			execjournal.ResearchDimensionCitationIntegrity: execjournal.ResearchDimensionIncomplete,
			execjournal.ResearchDimensionRelevance:         execjournal.ResearchDimensionNotApplicable,
			execjournal.ResearchDimensionSourceQuality:     execjournal.ResearchDimensionIncomplete,
			execjournal.ResearchDimensionSourceDiversity:   execjournal.ResearchDimensionNotApplicable,
			execjournal.ResearchDimensionConflictHandling:  execjournal.ResearchDimensionIncomplete,
			execjournal.ResearchDimensionUncertainty:       execjournal.ResearchDimensionIncomplete,
			execjournal.ResearchDimensionPlanCompleteness:  execjournal.ResearchDimensionIncomplete,
		},
	}
	b.evidenceVersion = 0
	if in.Evidence != nil {
		b.evidenceVersion = in.Evidence.Version
		b.evidenceCount = in.Evidence.Count
	}
	b.evidenceFingerprint = execjournal.ResearchEvidenceFingerprint(in.Evidence)
	if in.Synthesis != nil {
		b.synthesisStatus = in.Synthesis.Status
		b.synthesisFingerprint = synthesisFingerprint(in.Synthesis)
	}
	b.assessEvidence()
	evaluation := &execjournal.ResearchEvaluation{
		Version:              execjournal.ResearchEvaluationVersion,
		Method:               execjournal.ResearchEvaluationMethod,
		ExecutionID:          in.ExecutionID,
		EvidenceVersion:      b.evidenceVersion,
		EvidenceCount:        b.evidenceCount,
		EvidenceFingerprint:  b.evidenceFingerprint,
		SynthesisStatus:      b.synthesisStatus,
		SynthesisFingerprint: b.synthesisFingerprint,
		Status:               b.overallStatus(),
		Dimensions:           b.orderedDimensions(),
		Findings:             append([]execjournal.ResearchEvaluationFinding(nil), b.findings...),
		Limitations:          append([]string(nil), evaluationLimitations...),
	}
	return evaluation
}

type builder struct {
	in                   Input
	evidence             *execjournal.ResearchEvidence
	evidenceVersion      int
	evidenceCount        int
	evidenceFingerprint  string
	synthesisStatus      string
	synthesisFingerprint string
	synthesisValid       bool
	dimensions           map[string]string
	findings             []execjournal.ResearchEvaluationFinding
}

func (b *builder) assessEvidence() {
	raw := b.in.Evidence
	if raw == nil {
		if b.in.Synthesis != nil {
			b.addError(execjournal.ResearchDimensionCitationIntegrity, findingSynthesisWithoutEvidence,
				"Synthesis was supplied without a captured evidence snapshot.", "", "")
		}
		if b.in.Attempted {
			b.addWarning(execjournal.ResearchDimensionEvidenceCoverage, findingEvidenceUnavailable,
				"No complete evidence snapshot was available for evaluation.", "", "")
		}
		b.assessPlan()
		return
	}

	planMismatch := raw.Version == execjournal.ResearchEvidenceVersion && raw.Plan != nil &&
		raw.Plan.PlanID != b.in.ExecutionID
	if err := execjournal.ValidateResearchEvidenceForExecution(b.in.ExecutionID, raw); err != nil {
		if planMismatch {
			b.addError(execjournal.ResearchDimensionCitationIntegrity, findingEvidenceExecutionMismatch,
				"The evidence snapshot belongs to a different execution.", "", "")
		} else {
			b.addError(execjournal.ResearchDimensionCitationIntegrity, findingEvidenceInvalid,
				"The evidence snapshot is not structurally valid.", "", "")
		}
		b.set(execjournal.ResearchDimensionCitationIntegrity, execjournal.ResearchDimensionFail)
		b.assessPlan()
		return
	}
	b.evidence = raw

	if raw.Version == execjournal.ResearchEvidenceLegacyVersion {
		b.addWarning(execjournal.ResearchDimensionCitationIntegrity, findingLegacyEvidence,
			"Legacy evidence has no stable source ids or provenance; citation and plan checks are unavailable.", "", "")
		b.set(execjournal.ResearchDimensionCitationIntegrity, execjournal.ResearchDimensionIncomplete)
		if b.in.Synthesis != nil {
			b.addError(execjournal.ResearchDimensionCitationIntegrity, findingSynthesisWithoutStable,
				"Typed synthesis cannot be checked against legacy evidence without stable source ids.", "", "")
			b.set(execjournal.ResearchDimensionCitationIntegrity, execjournal.ResearchDimensionFail)
		}
		b.assessSources()
		b.assessRelevance()
		b.assessPlan()
		return
	}

	b.assessSources()
	b.assessRelevance()
	b.assessPlan()

	if b.in.Synthesis == nil {
		b.addWarning(execjournal.ResearchDimensionCitationIntegrity, findingSynthesisUnavailable,
			"No typed synthesis was available; claim and citation checks are unavailable.", "", "")
		b.set(execjournal.ResearchDimensionCitationIntegrity, execjournal.ResearchDimensionIncomplete)
		b.set(execjournal.ResearchDimensionEvidenceCoverage, execjournal.ResearchDimensionIncomplete)
		b.set(execjournal.ResearchDimensionConflictHandling, execjournal.ResearchDimensionIncomplete)
		b.set(execjournal.ResearchDimensionUncertainty, execjournal.ResearchDimensionIncomplete)
		return
	}

	b.synthesisValid = b.validateSynthesis(raw)
	b.assessCoverage()
	b.assessConflictHandling()
	b.assessUncertainty()
	if b.synthesisValid {
		b.set(execjournal.ResearchDimensionCitationIntegrity, execjournal.ResearchDimensionPass)
	}
}

func (b *builder) validateSynthesis(ev *execjournal.ResearchEvidence) bool {
	s := b.in.Synthesis
	if s == nil {
		return false
	}
	ok := true
	fail := func(dimension, code, message string) {
		ok = false
		b.addError(dimension, code, message, "", "")
	}
	if s.Status != executor.ResearchSynthesisComplete &&
		s.Status != executor.ResearchSynthesisPartial &&
		s.Status != executor.ResearchSynthesisInconclusive {
		fail(execjournal.ResearchDimensionCitationIntegrity, findingSynthesisStatusInvalid,
			"Synthesis has an unknown status.")
	}
	if strings.TrimSpace(s.Summary) == "" || len(s.Summary) > executor.MaxResearchSynthesisSummary {
		fail(execjournal.ResearchDimensionCitationIntegrity, findingSynthesisInvalid,
			"Synthesis summary is empty or exceeds its bound.")
	}
	if len(s.EvidenceBacked) > executor.MaxResearchSynthesisStatements ||
		len(s.Inferences) > executor.MaxResearchSynthesisInferences ||
		len(s.Conflicts) > executor.MaxResearchSynthesisConflicts ||
		len(s.UnresolvedQuestions) > executor.MaxResearchSynthesisQuestions {
		fail(execjournal.ResearchDimensionCitationIntegrity, findingSynthesisInvalid,
			"Synthesis exceeds a collection bound.")
	}

	known := make(map[string]struct{}, len(ev.Sources))
	for _, source := range ev.Sources {
		known[source.ID] = struct{}{}
	}
	seenStatements := make(map[string]struct{}, len(s.EvidenceBacked)+len(s.Conflicts))
	checkStatements := func(rows []executor.ResearchEvidenceStatement) bool {
		rowsOK := true
		for i, row := range rows {
			text := strings.TrimSpace(row.Statement)
			if text == "" || len(row.Statement) > executor.MaxResearchSynthesisStatementLen {
				rowsOK = false
				fail(execjournal.ResearchDimensionCitationIntegrity, findingStatementEmpty,
					fmt.Sprintf("Statement %d is empty or exceeds its bound.", i+1))
				continue
			}
			if _, dup := seenStatements[text]; dup {
				rowsOK = false
				fail(execjournal.ResearchDimensionCitationIntegrity, findingStatementDuplicate,
					fmt.Sprintf("Statement %d repeats an earlier statement.", i+1))
				continue
			}
			seenStatements[text] = struct{}{}
			if len(row.SourceIDs) == 0 {
				rowsOK = false
				fail(execjournal.ResearchDimensionCitationIntegrity, findingStatementNoCitations,
					fmt.Sprintf("Statement %d has no citations.", i+1))
				continue
			}
			if len(row.SourceIDs) > executor.MaxResearchSynthesisCitationRefs {
				rowsOK = false
				fail(execjournal.ResearchDimensionCitationIntegrity, findingStatementTooManyCitations,
					fmt.Sprintf("Statement %d exceeds the citation bound.", i+1))
				continue
			}
			seenIDs := make(map[string]struct{}, len(row.SourceIDs))
			for _, id := range row.SourceIDs {
				if id == "" || len(id) > execjournal.MaxResearchSourceID ||
					strings.ContainsAny(id, "\x00\n\r") {
					rowsOK = false
					fail(execjournal.ResearchDimensionCitationIntegrity, findingCitationMalformed,
						fmt.Sprintf("Statement %d contains a malformed source id.", i+1))
					continue
				}
				if _, dup := seenIDs[id]; dup {
					rowsOK = false
					fail(execjournal.ResearchDimensionCitationIntegrity, findingCitationDuplicate,
						fmt.Sprintf("Statement %d repeats a source id.", i+1))
					continue
				}
				seenIDs[id] = struct{}{}
				if _, found := known[id]; !found {
					rowsOK = false
					fail(execjournal.ResearchDimensionCitationIntegrity, findingCitationUnknown,
						fmt.Sprintf("Statement %d cites a source not present in this execution.", i+1))
				}
			}
		}
		return rowsOK
	}
	checkStatements(s.EvidenceBacked)
	conflictOK := checkStatements(s.Conflicts)

	for i, inference := range s.Inferences {
		if strings.TrimSpace(inference.Statement) == "" ||
			len(inference.Statement) > executor.MaxResearchSynthesisStatementLen {
			ok = false
			b.addError(execjournal.ResearchDimensionCitationIntegrity, findingSynthesisInvalid,
				fmt.Sprintf("Inference %d is empty or exceeds its bound.", i+1), "", "")
		}
	}
	for i, question := range s.UnresolvedQuestions {
		if strings.TrimSpace(question) == "" || len(question) > executor.MaxResearchSynthesisQuestionLen {
			ok = false
			b.addError(execjournal.ResearchDimensionCitationIntegrity, findingSynthesisInvalid,
				fmt.Sprintf("Unresolved question %d is empty or exceeds its bound.", i+1), "", "")
		}
	}

	grounded := len(s.EvidenceBacked) + len(s.Conflicts)
	planIncomplete := b.planIsIncomplete()
	switch s.Status {
	case executor.ResearchSynthesisInconclusive:
		if grounded != 0 {
			ok = false
			b.addError(execjournal.ResearchDimensionUncertainty, findingSynthesisInconclusive,
				"Inconclusive synthesis carries evidence-backed statements or conflicts.", "", "")
		}
	case executor.ResearchSynthesisPartial:
		if grounded == 0 {
			ok = false
			b.addError(execjournal.ResearchDimensionUncertainty, findingSynthesisPartial,
				"Partial synthesis carries no cited statement or conflict.", "", "")
		}
	case executor.ResearchSynthesisComplete:
		if len(s.EvidenceBacked) == 0 {
			ok = false
			b.addError(execjournal.ResearchDimensionUncertainty, findingSynthesisCompleteEmpty,
				"Complete synthesis carries no evidence-backed statement.", "", "")
		}
		if planIncomplete {
			ok = false
			b.addError(execjournal.ResearchDimensionUncertainty, findingSynthesisCompletePlan,
				"Complete synthesis was asserted while the recorded plan still contains incomplete steps.", "", "")
		}
	}
	if ev.Count == 0 && s.Status != executor.ResearchSynthesisInconclusive {
		ok = false
		b.addError(execjournal.ResearchDimensionUncertainty, findingSynthesisCompleteEmpty,
			"Synthesis is not inconclusive even though no source records were captured.", "", "")
	}
	if !conflictOK {
		b.set(execjournal.ResearchDimensionConflictHandling, execjournal.ResearchDimensionFail)
	}
	if !ok {
		b.set(execjournal.ResearchDimensionCitationIntegrity, execjournal.ResearchDimensionFail)
		// A defensive independent re-check catches shape/status issues not
		// covered by the stable-code checks above. The validator's raw error is
		// never exposed or persisted.
		partial := b.planIsIncomplete()
		if err := executor.ValidateResearchSynthesis(s, ev, partial); err != nil && !b.hasError() {
			b.addError(execjournal.ResearchDimensionCitationIntegrity, findingSynthesisInvalid,
				"Synthesis did not satisfy the completion contract.", "", "")
		}
	}
	return ok
}

func (b *builder) assessCoverage() {
	if !b.synthesisValid {
		return
	}
	s := b.in.Synthesis
	if s.Status == executor.ResearchSynthesisInconclusive {
		b.set(execjournal.ResearchDimensionEvidenceCoverage, execjournal.ResearchDimensionNotApplicable)
		return
	}
	if len(s.EvidenceBacked) == 0 {
		b.addWarning(execjournal.ResearchDimensionEvidenceCoverage, findingNoEvidenceBackedClaims,
			"Synthesis contains no evidence-backed conclusion; only conflicts or qualifications are present.", "", "")
		b.set(execjournal.ResearchDimensionEvidenceCoverage, execjournal.ResearchDimensionWarn)
		return
	}
	b.set(execjournal.ResearchDimensionEvidenceCoverage, execjournal.ResearchDimensionPass)
}

func (b *builder) assessConflictHandling() {
	if !b.synthesisValid {
		return
	}
	if len(b.in.Synthesis.Conflicts) == 0 {
		b.set(execjournal.ResearchDimensionConflictHandling, execjournal.ResearchDimensionNotMeasurable)
		return
	}
	b.set(execjournal.ResearchDimensionConflictHandling, execjournal.ResearchDimensionPass)
}

func (b *builder) assessUncertainty() {
	if !b.synthesisValid {
		return
	}
	s := b.in.Synthesis
	if (s.Status == executor.ResearchSynthesisPartial || s.Status == executor.ResearchSynthesisInconclusive) &&
		len(s.UnresolvedQuestions) == 0 {
		b.addWarning(execjournal.ResearchDimensionUncertainty, findingUnresolvedQuestions,
			"Partial or inconclusive synthesis does not state any unresolved question.", "", "")
		b.set(execjournal.ResearchDimensionUncertainty, execjournal.ResearchDimensionWarn)
		return
	}
	b.set(execjournal.ResearchDimensionUncertainty, execjournal.ResearchDimensionPass)
}

func (b *builder) assessSources() {
	if b.evidence == nil {
		return
	}
	sources := b.evidence.Sources
	if len(sources) == 0 {
		b.addWarning(execjournal.ResearchDimensionSourceQuality, findingNoSearchResults,
			"The search returned no source records.", "", "")
		b.set(execjournal.ResearchDimensionSourceQuality, execjournal.ResearchDimensionWarn)
		b.set(execjournal.ResearchDimensionSourceDiversity, execjournal.ResearchDimensionNotApplicable)
		return
	}

	b.set(execjournal.ResearchDimensionSourceQuality, execjournal.ResearchDimensionPass)
	b.set(execjournal.ResearchDimensionSourceDiversity, execjournal.ResearchDimensionPass)
	urlSeen := make(map[string]string, len(sources))
	contentSeen := make(map[string]string, len(sources))
	hosts := make(map[string]struct{}, len(sources))
	duplicateURLs, duplicateContent, lowInformation := 0, 0, 0
	validURLs := 0

	for _, source := range sources {
		canonical, host, urlOK := canonicalURL(source.URL)
		if !urlOK {
			b.addWarning(execjournal.ResearchDimensionSourceQuality, findingSourceInvalidURL,
				"Source URL is not an absolute http(s) URL with a host.", source.ID, "")
		} else {
			validURLs++
			hosts[host] = struct{}{}
			if first, dup := urlSeen[canonical]; dup {
				duplicateURLs++
				b.addWarning(execjournal.ResearchDimensionSourceDiversity, findingSourceDuplicateURL,
					fmt.Sprintf("Source URL duplicates another captured record (%s).", first), source.ID, "")
			} else {
				urlSeen[canonical] = source.ID
			}
		}
		title := strings.TrimSpace(source.Title)
		snippet := strings.TrimSpace(source.Snippet)
		if title == "" && snippet == "" {
			lowInformation++
			b.addWarning(execjournal.ResearchDimensionSourceQuality, findingSourceLowInformation,
				"Source has no usable title or excerpt.", source.ID, "")
		}
		if snippet != "" {
			key := normalizeText(title) + "\x00" + normalizeText(snippet)
			if first, dup := contentSeen[key]; dup {
				duplicateContent++
				b.addWarning(execjournal.ResearchDimensionSourceDiversity, findingSourceDuplicateContent,
					fmt.Sprintf("Source carries the same normalized title and excerpt as %s.", first), source.ID, "")
			} else {
				contentSeen[key] = source.ID
			}
		}
	}
	if validURLs == len(sources) && len(hosts) == 1 && len(sources) > 1 {
		b.addWarning(execjournal.ResearchDimensionSourceDiversity, findingSingleHostEvidence,
			"All captured sources share one host; host diversity is not proof of independent corroboration.", "", "")
	}
	if len(sources) > 1 && len(urlSeen) == 1 && duplicateURLs > 0 {
		b.addWarning(execjournal.ResearchDimensionSourceDiversity, findingDuplicateOnlyEvidence,
			"Every captured source repeats the same canonical URL.", "", "")
	}
	if lowInformation == len(sources) || validURLs == 0 {
		b.addWarning(execjournal.ResearchDimensionSourceQuality, findingSourcesAllLowInformation,
			"No captured source has both a usable URL and usable text.", "", "")
	}
	if b.evidence.Version == execjournal.ResearchEvidenceVersion && b.evidence.Plan != nil {
		referenced := make(map[string]struct{}, len(b.evidence.Sources))
		for _, operation := range b.evidence.Operations {
			for _, sourceID := range operation.SourceIDs {
				referenced[sourceID] = struct{}{}
			}
		}
		for _, source := range b.evidence.Sources {
			if _, ok := referenced[source.ID]; !ok {
				b.addWarning(execjournal.ResearchDimensionSourceQuality, findingSourceUnattributed,
					"At least one source is not attributed to a recorded plan step.", source.ID, "")
				break
			}
		}
	}
}

func (b *builder) assessRelevance() {
	if b.evidence == nil || len(b.evidence.Sources) == 0 {
		b.set(execjournal.ResearchDimensionRelevance, execjournal.ResearchDimensionNotApplicable)
		return
	}
	questionTokens := meaningfulTokens(b.in.Question)
	if len(questionTokens) == 0 {
		b.set(execjournal.ResearchDimensionRelevance, execjournal.ResearchDimensionNotMeasurable)
		return
	}
	b.set(execjournal.ResearchDimensionRelevance, execjournal.ResearchDimensionPass)
	noOverlap := 0
	for _, source := range b.evidence.Sources {
		tokens := meaningfulTokens(source.Title + " " + source.Snippet + " " + source.Source)
		if len(tokens) == 0 {
			continue
		}
		if !intersects(tokens, questionTokens) {
			noOverlap++
			b.addWarning(execjournal.ResearchDimensionRelevance, findingSourceNoQueryOverlap,
				"Source text has no meaningful lexical overlap with the objective.", source.ID, "")
		}
	}
	if noOverlap == len(b.evidence.Sources) {
		b.addWarning(execjournal.ResearchDimensionRelevance, findingNoRelevantSources,
			"No captured source has meaningful lexical overlap with the objective.", "", "")
	}

	intentByStep := make(map[string][]string, len(b.in.Plan))
	for _, step := range b.in.Plan {
		tokens := meaningfulTokens(step.Intent)
		if len(tokens) < 2 {
			continue
		}
		if !intersects(tokens, questionTokens) {
			// The step intent does not describe the objective, so it cannot
			// establish relevance. Comparing a source against a generic
			// instruction would produce a meaningless warning, so the check is
			// not measurable for that step.
			continue
		}
		intentByStep[step.StepID] = tokens
	}
	for _, operation := range b.evidence.Operations {
		stepTokens := intentByStep[operation.StepID]
		if len(stepTokens) == 0 {
			continue
		}
		for _, sourceID := range operation.SourceIDs {
			source := b.sourceByID(sourceID)
			if source == nil {
				continue
			}
			tokens := meaningfulTokens(source.Title + " " + source.Snippet + " " + source.Source)
			if len(tokens) > 0 && !intersects(tokens, stepTokens) {
				b.addWarning(execjournal.ResearchDimensionRelevance, findingSourceNoStepOverlap,
					"Source text has no meaningful lexical overlap with the step that retrieved it.",
					source.ID, operation.StepID)
			}
		}
	}
}

func (b *builder) assessPlan() {
	steps := b.planSteps()
	if len(steps) == 0 {
		b.addWarning(execjournal.ResearchDimensionPlanCompleteness, findingPlanUnavailable,
			"No durable plan/step status was available for completeness assessment.", "", "")
		b.set(execjournal.ResearchDimensionPlanCompleteness, execjournal.ResearchDimensionIncomplete)
		return
	}
	b.set(execjournal.ResearchDimensionPlanCompleteness, execjournal.ResearchDimensionPass)
	for _, step := range steps {
		if step.StepID == b.in.CurrentStepID {
			// The completion action settled this step; its ledger status has
			// no observation of its own.
			continue
		}
		switch step.Status {
		case "completed":
		case "failed", "blocked", "cancelled":
			b.addWarning(execjournal.ResearchDimensionPlanCompleteness, findingPlanStepFailed,
				"A research step did not complete successfully.", "", step.StepID)
		case "pending", "ready", "executing", "observing":
			b.addWarning(execjournal.ResearchDimensionPlanCompleteness, findingPlanIncomplete,
				"A research step was still nonterminal when the execution settled.", "", step.StepID)
		default:
			b.addWarning(execjournal.ResearchDimensionPlanCompleteness, findingPlanStepState,
				"A research step has no provable lifecycle status.", "", step.StepID)
		}
	}
}

func (b *builder) planSteps() []Step {
	if len(b.in.Plan) > 0 {
		return b.in.Plan
	}
	if b.evidence == nil || b.evidence.Version != execjournal.ResearchEvidenceVersion {
		return nil
	}
	steps := make([]Step, 0, len(b.evidence.Steps))
	for _, step := range b.evidence.Steps {
		steps = append(steps, Step{StepID: step.StepID, Status: step.Status})
	}
	return steps
}

func (b *builder) planIsIncomplete() bool {
	for _, step := range b.planSteps() {
		if step.StepID == b.in.CurrentStepID {
			// The step that carried the completion is settled by the
			// completion action itself, not by an observation.
			continue
		}
		if step.Status != "completed" {
			return true
		}
	}
	return false
}

func (b *builder) sourceByID(id string) *execjournal.ResearchSource {
	if b.evidence == nil {
		return nil
	}
	for i := range b.evidence.Sources {
		if b.evidence.Sources[i].ID == id {
			return &b.evidence.Sources[i]
		}
	}
	return nil
}

func (b *builder) set(dimension, status string) {
	b.dimensions[dimension] = status
}

func (b *builder) addWarning(dimension, code, message, sourceID, stepID string) {
	b.add(dimension, code, execjournal.ResearchFindingWarning, message, sourceID, stepID)
}

func (b *builder) addError(dimension, code, message, sourceID, stepID string) {
	b.add(dimension, code, execjournal.ResearchFindingError, message, sourceID, stepID)
}

func (b *builder) add(dimension, code, severity, message, sourceID, stepID string) {
	if b.hasFinding(dimension, code, sourceID, stepID) {
		return
	}
	if len(b.findings) >= execjournal.MaxResearchEvaluationFindings {
		return
	}
	b.findings = append(b.findings, execjournal.ResearchEvaluationFinding{
		Code: code, Dimension: dimension, Severity: severity, Message: message,
		SourceID: sourceID, StepID: stepID,
	})
	if severity == execjournal.ResearchFindingError {
		b.set(dimension, execjournal.ResearchDimensionFail)
		return
	}
	switch b.dimensions[dimension] {
	case execjournal.ResearchDimensionFail, execjournal.ResearchDimensionIncomplete:
	default:
		b.set(dimension, execjournal.ResearchDimensionWarn)
	}
}

func (b *builder) hasFinding(dimension, code, sourceID, stepID string) bool {
	for _, finding := range b.findings {
		if finding.Dimension == dimension && finding.Code == code &&
			finding.SourceID == sourceID && finding.StepID == stepID {
			return true
		}
	}
	return false
}

func (b *builder) hasError() bool {
	for _, finding := range b.findings {
		if finding.Severity == execjournal.ResearchFindingError {
			return true
		}
	}
	return false
}

func (b *builder) overallStatus() string {
	hasError, hasWarning := false, false
	hasIncomplete, hasWarnDimension, hasFailDimension := false, false, false
	for _, finding := range b.findings {
		switch finding.Severity {
		case execjournal.ResearchFindingError:
			hasError = true
		case execjournal.ResearchFindingWarning:
			hasWarning = true
		}
	}
	for _, status := range b.dimensions {
		switch status {
		case execjournal.ResearchDimensionIncomplete:
			hasIncomplete = true
		case execjournal.ResearchDimensionWarn:
			hasWarnDimension = true
		case execjournal.ResearchDimensionFail:
			hasFailDimension = true
		}
	}
	switch {
	case hasError || hasFailDimension:
		return execjournal.ResearchEvaluationFailed
	case hasIncomplete:
		return execjournal.ResearchEvaluationIncomplete
	case hasWarning || hasWarnDimension:
		return execjournal.ResearchEvaluationWarnings
	default:
		return execjournal.ResearchEvaluationPassed
	}
}

func (b *builder) orderedDimensions() []execjournal.ResearchEvaluationDimension {
	ids := []string{
		execjournal.ResearchDimensionEvidenceCoverage,
		execjournal.ResearchDimensionCitationIntegrity,
		execjournal.ResearchDimensionRelevance,
		execjournal.ResearchDimensionSourceQuality,
		execjournal.ResearchDimensionSourceDiversity,
		execjournal.ResearchDimensionConflictHandling,
		execjournal.ResearchDimensionUncertainty,
		execjournal.ResearchDimensionPlanCompleteness,
	}
	out := make([]execjournal.ResearchEvaluationDimension, 0, len(ids))
	for _, id := range ids {
		out = append(out, execjournal.ResearchEvaluationDimension{ID: id, Status: b.dimensions[id]})
	}
	return out
}

func synthesisFingerprint(s *executor.ResearchSynthesis) string {
	if s == nil {
		return ""
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// canonicalURL returns a duplicate-detection key, host and validity. It is
// deliberately conservative: it lowercases scheme/host, removes default ports
// and fragments, drops only well-known tracking parameters, and sorts the
// remaining query. Path case, meaningful query values and www/non-www remain
// distinct. The original URL is never rewritten in evidence or displayed.
func canonicalURL(raw string) (string, string, bool) {
	trimmed := strings.TrimSpace(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return "", "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return "", "", false
	}
	port := parsed.Port()
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}
	authority := host
	if strings.Contains(host, ":") {
		authority = "[" + host + "]"
	}
	if port != "" {
		authority += ":" + port
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	query := parsed.Query()
	for key := range query {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "utm_") || lower == "gclid" || lower == "fbclid" ||
			lower == "mc_cid" || lower == "mc_eid" {
			query.Del(key)
		}
	}
	canonical := strings.ToLower(parsed.Scheme) + "://" + authority + path
	if encoded := query.Encode(); encoded != "" {
		canonical += "?" + encoded
	}
	return canonical, host, true
}

func normalizeText(value string) string {
	tokens := wordTokens(value)
	sort.Strings(tokens)
	return strings.Join(tokens, " ")
}

func meaningfulTokens(value string) []string {
	tokens := wordTokens(value)
	out := make([]string, 0, len(tokens))
	seen := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		if len(token) < 3 || isStopWord(token) {
			continue
		}
		if _, dup := seen[token]; dup {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

func wordTokens(value string) []string {
	var out []string
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		out = append(out, strings.ToLower(current.String()))
		current.Reset()
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(unicode.ToLower(r))
		} else {
			flush()
		}
	}
	flush()
	return out
}

func isStopWord(value string) bool {
	_, ok := stopWords[value]
	return ok
}

func intersects(a, b []string) bool {
	set := make(map[string]struct{}, len(a))
	for _, value := range a {
		set[value] = struct{}{}
	}
	for _, value := range b {
		if _, ok := set[value]; ok {
			return true
		}
	}
	return false
}

var stopWords = map[string]struct{}{
	"about": {}, "after": {}, "again": {}, "against": {}, "also": {}, "among": {},
	"and": {}, "any": {}, "are": {}, "because": {}, "been": {}, "before": {},
	"being": {}, "between": {}, "both": {}, "but": {}, "can": {}, "could": {},
	"did": {}, "does": {}, "doing": {}, "down": {}, "during": {}, "each": {},
	"few": {}, "for": {}, "from": {}, "further": {}, "had": {}, "has": {},
	"have": {}, "having": {}, "her": {}, "here": {}, "hers": {}, "herself": {},
	"him": {}, "himself": {}, "his": {}, "how": {}, "into": {}, "its": {},
	"itself": {}, "just": {}, "more": {}, "most": {}, "myself": {}, "nor": {},
	"not": {}, "now": {}, "off": {}, "once": {}, "only": {}, "other": {},
	"our": {}, "ours": {}, "ourselves": {}, "out": {}, "over": {}, "own": {},
	"same": {}, "she": {}, "should": {}, "some": {}, "such": {}, "than": {},
	"that": {}, "the": {}, "their": {}, "theirs": {}, "them": {}, "themselves": {},
	"then": {}, "there": {}, "these": {}, "they": {}, "this": {}, "those": {},
	"through": {}, "too": {}, "under": {}, "until": {}, "very": {}, "was": {},
	"were": {}, "what": {}, "when": {}, "where": {}, "which": {}, "while": {},
	"who": {}, "whom": {}, "why": {}, "will": {}, "with": {}, "would": {},
	"you": {}, "your": {}, "yours": {}, "yourself": {}, "yourselves": {},
	"search": {}, "summarize": {}, "summary": {}, "source": {}, "sources": {},
	"find": {}, "information": {}, "research": {}, "tell": {}, "give": {},
	"use": {}, "using": {}, "get": {}, "make": {}, "made": {}, "based": {},
}
