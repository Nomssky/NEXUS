// Package researchaudit projects a deterministic, bounded, claim-level audit
// of the citation integrity of one durable research synthesis.
//
// The audit is a pure read model over the exact execution-scoped
// execjournal.ResearchSynthesis and execjournal.ResearchEvidence passed by the
// caller. It does not write, migrate, backfill or infer historical facts, and
// it never reads another execution. It answers a deliberately narrow question:
// do the recorded citations point at source records that are present in this
// exact evidence snapshot, and is the provenance needed to make that claim
// available?
//
// It is not a truth engine. A resolved citation proves only that the reference
// matches a recorded source id. It does not prove that the source supports the
// statement, that the source is true, or that the statement is entailed by the
// source text. The fixed limitations say so explicitly.
package researchaudit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Nomssky/NEXUS/internal/execjournal"
)

const (
	// Version is the audit response schema version.
	Version = 1
	// Method is the only audit implementation. It is deterministic and makes no
	// model or network calls.
	Method = "deterministic_v1"

	// Status values. The audit is passed only when every citation-bearing claim
	// resolved against available evidence and no warning was recorded.
	StatusPassed       = "passed"
	StatusWarnings     = "warnings"
	StatusFailed       = "failed"
	StatusIncomplete   = "incomplete"
	StatusUnavailable  = "unavailable"
	StatusIncompatible = "incompatible"

	// Claim categories mirror the durable synthesis schema.
	CategoryEvidenceBacked = "evidence_backed"
	CategoryConflict       = "conflict"
	CategoryInference      = "inference"
	CategoryQuestion       = "unresolved_question"

	// Claim citation requirements. Only evidence-backed statements and
	// conflicts require citations under the existing synthesis contract.
	CitationRequired    = "required"
	CitationNotRequired = "not_required"

	// Claim statuses.
	ClaimPassed        = "passed"
	ClaimWarnings      = "warnings"
	ClaimFailed        = "failed"
	ClaimUnavailable   = "unavailable"
	ClaimNotApplicable = "not_applicable"

	// Citation statuses.
	CitationResolved    = "resolved"
	CitationUnresolved  = "unresolved"
	CitationMalformed   = "malformed"
	CitationDuplicate   = "duplicate"
	CitationAmbiguous   = "ambiguous"
	CitationUnavailable = "unavailable"

	// Evidence availability statuses. Available means a source record with the
	// cited id is present. It says nothing about the source content.
	EvidenceAvailable   = "available"
	EvidenceMissing     = "missing"
	EvidenceAmbiguous   = "ambiguous"
	EvidenceUnavailable = "unavailable"

	// Provenance values. The top-level audit is always derived. Per-claim and
	// per-citation provenance describes the recorded evidence, not the audit.
	ProvenanceAuthoritative = "authoritative"
	ProvenanceDerived       = "derived"
	ProvenanceLegacy        = "legacy"
	ProvenanceUnavailable   = "unavailable"
	ProvenanceNotApplicable = "not_applicable"

	// Finding severities. The audit deliberately has no "info" severity: every
	// observation either limits confidence (warning) or is a structural error.
	SeverityWarning = "warning"
	SeverityError   = "error"

	// Finding codes. These are stable within audit schema version 1.
	FindingCitationMissing             = "citation_missing"
	FindingCitationMalformed           = "citation_malformed"
	FindingCitationDuplicate           = "citation_duplicate"
	FindingCitationUnresolved          = "citation_unresolved"
	FindingCitationAmbiguous           = "citation_ambiguous"
	FindingEvidenceContentUnavailable  = "evidence_content_unavailable"
	FindingEvidenceMetadataUnavailable = "evidence_metadata_unavailable"
	FindingLegacyProvenance            = "legacy_provenance"
	FindingProvenanceUnavailable       = "provenance_unavailable"
	FindingSynthesisSnapshotMismatch   = "synthesis_snapshot_mismatch"
	FindingEvidenceSnapshotMismatch    = "evidence_snapshot_mismatch"
	FindingPlanRevisionUnavailable     = "plan_revision_unavailable"
	FindingPlanRevisionMismatch        = "plan_revision_mismatch"
	FindingSynthesisSchemaIncompatible = "synthesis_schema_incompatible"
	FindingEvidenceSchemaIncompatible  = "evidence_schema_incompatible"
	FindingSynthesisInvalid            = "synthesis_invalid"
	FindingEvidenceInvalid             = "evidence_invalid"
	FindingDuplicateClaimIdentity      = "duplicate_claim_identity"
	FindingClaimIdentityMalformed      = "claim_identity_malformed"
	FindingSynthesisUnavailable        = "synthesis_unavailable"
	FindingEvidenceUnavailable         = "evidence_unavailable"
	FindingNoCitedClaims               = "no_cited_claims"
	FindingAuditOutputTruncated        = "audit_output_truncated"

	// Bounds. Inputs outside these bounds are rejected before projection; a
	// valid durable synthesis is much smaller than every limit.
	MaxClaims            = 64
	MaxCitationsPerClaim = 12
	MaxCitationReference = 512
	MaxClaimText         = 2000
	MaxQuestionText      = 1000
	MaxFindings          = 256
	MaxLimitations       = 16
	MaxText              = 512
	MaxID                = 128
	MaxExecutionID       = 512
	MaxAuditBytes        = 2 * 1024 * 1024
	MaxPlanVersion       = execjournal.MaxResearchSynthesisPlanVersion
)

var limitationList = []string{
	"This audit checks recorded citation structure and evidence availability only. It does not verify factual truth, source quality, relevance or semantic entailment.",
	"Citation resolved means the reference exactly matches a source id in this execution's recorded evidence snapshot. It does not mean the source supports the statement.",
	"Evidence available means a source record is present. content_available and metadata_available report only whether recorded fields are non-empty, never whether they are correct.",
	"Cross-execution and out-of-scope citation ids are indistinguishable from unresolved ids in an exact snapshot. The audit never reads another execution.",
	"Legacy evidence is resolved by its recorded ids only. Missing v3 provenance remains unavailable and is never inferred from URLs, titles, order or newer snapshots.",
	"Claim text is the persisted user-facing statement. It is not re-evaluated, summarized, rewritten or compared semantically.",
	"The audit is a derived projection and is not persisted. A later audit can differ only when the durable snapshot differs.",
}

// Audit is the complete, bounded citation-integrity projection for one durable
// synthesis. It contains no source content, prompts, reasoning, credentials or
// raw provider payloads.
type Audit struct {
	Version              int       `json:"version"`
	Method               string    `json:"method"`
	ExecutionID          string    `json:"execution_id"`
	EvidenceVersion      int       `json:"evidence_version"`
	EvidenceFingerprint  string    `json:"evidence_fingerprint"`
	SynthesisVersion     int       `json:"synthesis_version"`
	SynthesisMethod      string    `json:"synthesis_method"`
	SynthesisFingerprint string    `json:"synthesis_fingerprint"`
	SynthesisStatus      string    `json:"synthesis_status"`
	PlanVersion          int       `json:"plan_version"`
	PlanVersionRecorded  bool      `json:"plan_version_recorded"`
	EvidencePlanVersion  int       `json:"evidence_plan_version"`
	EvidencePlanRecorded bool      `json:"evidence_plan_recorded"`
	Provenance           string    `json:"provenance"`
	Status               string    `json:"status"`
	Claims               []Claim   `json:"claims"`
	Findings             []Finding `json:"findings"`
	Limitations          []string  `json:"limitations"`
	Truncated            bool      `json:"truncated"`
}

// Claim is one persisted statement, inference or unresolved question. Index is
// the canonical 1-based position in the audit (evidence-backed statements,
// conflicts, inferences, unresolved questions). Citation-bearing claims keep
// their recorded citations in recorded order.
type Claim struct {
	ClaimID             string     `json:"claim_id"`
	Category            string     `json:"category"`
	Index               int        `json:"index"`
	Text                string     `json:"text"`
	CitationRequirement string     `json:"citation_requirement"`
	Status              string     `json:"status"`
	Provenance          string     `json:"provenance"`
	Citations           []Citation `json:"citations"`
}

// Citation is one recorded source id and its structural resolution. Reference
// is never trimmed, normalized, repaired or looked up in another execution.
type Citation struct {
	Index             int    `json:"index"`
	Reference         string `json:"reference"`
	Status            string `json:"status"`
	EvidenceStatus    string `json:"evidence_status"`
	ContentAvailable  bool   `json:"content_available"`
	MetadataAvailable bool   `json:"metadata_available"`
	Provenance        string `json:"provenance"`
}

// Finding is a stable, bounded observation. ClaimID/ClaimIndex and
// CitationIndex identify the affected claim or citation when the finding is
// claim-scoped; SourceID is the recorded reference when it is safe to echo.
type Finding struct {
	Code          string `json:"code"`
	Severity      string `json:"severity"`
	ClaimID       string `json:"claim_id,omitempty"`
	ClaimIndex    int    `json:"claim_index,omitempty"`
	CitationIndex int    `json:"citation_index,omitempty"`
	SourceID      string `json:"source_id,omitempty"`
	Message       string `json:"message"`
}

// Build projects the audit for executionID over the exact synthesis and
// evidence snapshots. It returns an error only for invalid input bounds or an
// internally inconsistent projection. Missing artifacts, incompatible schemas
// and binding mismatches are represented as audit states, never as errors.
func Build(executionID string, synthesis *execjournal.ResearchSynthesis, evidence *execjournal.ResearchEvidence) (Audit, error) {
	if executionID == "" || len(executionID) > MaxExecutionID || hasForbiddenControl(executionID) {
		return Audit{}, fmt.Errorf("researchaudit: invalid execution id")
	}
	if synthesis != nil {
		if synthesis.Version < 0 || synthesis.Version > 1_000_000 {
			return Audit{}, fmt.Errorf("researchaudit: invalid synthesis version")
		}
		if len(synthesis.Method) > MaxID {
			return Audit{}, fmt.Errorf("researchaudit: synthesis method exceeds its bound")
		}
		if len(synthesis.Status) > MaxID {
			return Audit{}, fmt.Errorf("researchaudit: synthesis status exceeds its bound")
		}
		if synthesis.PlanVersion < 0 || synthesis.PlanVersion > MaxPlanVersion {
			return Audit{}, fmt.Errorf("researchaudit: synthesis plan version out of range")
		}
	}
	if evidence != nil {
		if evidence.Version < 0 || evidence.Version > 1_000_000 {
			return Audit{}, fmt.Errorf("researchaudit: invalid evidence version")
		}
		if evidence.Plan != nil && (evidence.Plan.Version < 0 || evidence.Plan.Version > MaxPlanVersion) {
			return Audit{}, fmt.Errorf("researchaudit: evidence plan version out of range")
		}
	}

	seeds, err := buildClaimSeeds(synthesis)
	if err != nil {
		return Audit{}, err
	}

	a := Audit{
		Version:     Version,
		Method:      Method,
		ExecutionID: executionID,
		Provenance:  ProvenanceDerived,
		Claims:      make([]Claim, 0, len(seeds)),
		Findings:    make([]Finding, 0),
		Limitations: append([]string(nil), limitationList...),
	}
	for _, seed := range seeds {
		a.Claims = append(a.Claims, seed.claim)
	}

	if synthesis != nil {
		a.SynthesisVersion = synthesis.Version
		a.SynthesisMethod = synthesis.Method
		a.SynthesisFingerprint = execjournal.ResearchSynthesisFingerprint(synthesis)
		a.SynthesisStatus = synthesis.Status
		a.PlanVersion = synthesis.PlanVersion
		a.PlanVersionRecorded = synthesis.PlanVersion > 0
	}
	if evidence != nil {
		a.EvidenceVersion = evidence.Version
		a.EvidenceFingerprint = execjournal.ResearchEvidenceFingerprint(evidence)
		if evidence.Plan != nil {
			a.EvidencePlanVersion = evidence.Plan.Version
			a.EvidencePlanRecorded = evidence.Plan.Version > 0
		}
	}

	b := &auditBuilder{audit: a, seenFindings: make(map[string]struct{})}
	if synthesis == nil {
		b.addFinding(FindingSynthesisUnavailable, SeverityWarning, "", 0, 0, "",
			"No durable synthesis is present for this execution; absence is not an empty or clean synthesis.")
		b.finish(seeds)
		return b.audit, b.err
	}

	incompatible := b.checkBindings(synthesis, evidence)
	if incompatible {
		b.incompatible = true
		b.markUnavailable(seeds)
		b.finish(seeds)
		return b.audit, b.err
	}

	b.auditClaims(seeds, synthesis, evidence)
	b.finish(seeds)
	return b.audit, b.err
}

type claimSeed struct {
	claim       Claim
	malformed   bool
	duplicateOf int // 1-based claim index of the first occurrence, or 0
}

func buildClaimSeeds(synthesis *execjournal.ResearchSynthesis) ([]claimSeed, error) {
	if synthesis == nil {
		return nil, nil
	}
	total := len(synthesis.EvidenceBacked) + len(synthesis.Conflicts) +
		len(synthesis.Inferences) + len(synthesis.UnresolvedQuestions)
	if total > MaxClaims {
		return nil, fmt.Errorf("researchaudit: synthesis has too many claims")
	}

	seeds := make([]claimSeed, 0, total)
	seen := make(map[string]int, total)

	appendStatement := func(category, text string, refs []string, cited bool) error {
		if len(text) > MaxClaimText {
			return fmt.Errorf("researchaudit: claim text exceeds %d bytes", MaxClaimText)
		}
		if len(refs) > MaxCitationsPerClaim {
			return fmt.Errorf("researchaudit: claim has too many citations")
		}
		for _, ref := range refs {
			if len(ref) > MaxCitationReference {
				return fmt.Errorf("researchaudit: citation reference exceeds %d bytes", MaxCitationReference)
			}
		}
		index := len(seeds) + 1
		id := claimIdentity(category, text, index, cited)
		requirement := CitationNotRequired
		if cited {
			requirement = CitationRequired
		}
		claim := Claim{
			ClaimID:             id,
			Category:            category,
			Index:               index,
			Text:                text,
			CitationRequirement: requirement,
			Citations:           make([]Citation, 0, len(refs)),
		}
		for i, ref := range refs {
			claim.Citations = append(claim.Citations, Citation{Index: i + 1, Reference: ref})
		}
		seed := claimSeed{claim: claim, malformed: strings.TrimSpace(text) == ""}
		if first, ok := seen[id]; ok {
			seed.duplicateOf = first
		} else {
			seen[id] = index
		}
		seeds = append(seeds, seed)
		return nil
	}

	for _, statement := range synthesis.EvidenceBacked {
		if err := appendStatement(CategoryEvidenceBacked, statement.Statement, statement.SourceIDs, true); err != nil {
			return nil, err
		}
	}
	for _, statement := range synthesis.Conflicts {
		if err := appendStatement(CategoryConflict, statement.Statement, statement.SourceIDs, true); err != nil {
			return nil, err
		}
	}
	for _, inference := range synthesis.Inferences {
		if err := appendStatement(CategoryInference, inference.Statement, nil, false); err != nil {
			return nil, err
		}
	}
	for _, question := range synthesis.UnresolvedQuestions {
		if len(question) > MaxQuestionText {
			return nil, fmt.Errorf("researchaudit: unresolved question exceeds %d bytes", MaxQuestionText)
		}
		if err := appendStatement(CategoryQuestion, question, nil, false); err != nil {
			return nil, err
		}
	}
	return seeds, nil
}

// claimIdentity is stable within one snapshot. Citation-bearing categories use
// the normalized statement text in one shared namespace because the existing
// synthesis contract requires uniqueness across evidence-backed statements and
// conflicts. Inferences and questions may legally repeat, so their identity
// includes the category and canonical ordinal.
func claimIdentity(category, text string, index int, cited bool) string {
	normalized := strings.TrimSpace(text)
	var material string
	if cited {
		material = "cited\x00" + normalized
	} else {
		material = category + "\x00" + normalized + "\x00" + strconv.Itoa(index)
	}
	sum := sha256.Sum256([]byte(material))
	return "claim-" + hex.EncodeToString(sum[:])
}

type auditBuilder struct {
	audit        Audit
	seenFindings map[string]struct{}
	hasError     bool
	hasWarning   bool
	incomplete   bool
	incompatible bool
	err          error
}

func (b *auditBuilder) addFinding(code, severity, claimID string, claimIndex, citationIndex int, sourceID, message string) {
	if b.err != nil {
		return
	}
	key := fmt.Sprintf("%s|%s|%s|%d|%d|%s|%s", code, severity, claimID, claimIndex, citationIndex, sourceID, message)
	if _, ok := b.seenFindings[key]; ok {
		return
	}
	b.seenFindings[key] = struct{}{}
	b.audit.Findings = append(b.audit.Findings, Finding{
		Code:          code,
		Severity:      severity,
		ClaimID:       claimID,
		ClaimIndex:    claimIndex,
		CitationIndex: citationIndex,
		SourceID:      sourceID,
		Message:       message,
	})
	switch severity {
	case SeverityError:
		b.hasError = true
	case SeverityWarning:
		b.hasWarning = true
	}
	if code == FindingPlanRevisionUnavailable && severity == SeverityWarning {
		b.incomplete = true
	}
}

func (b *auditBuilder) checkBindings(synthesis *execjournal.ResearchSynthesis, evidence *execjournal.ResearchEvidence) bool {
	incompatible := false

	if synthesis.Version != execjournal.ResearchSynthesisVersion ||
		synthesis.Method != execjournal.ResearchSynthesisMethod {
		b.addFinding(FindingSynthesisSchemaIncompatible, SeverityError, "", 0, 0, "",
			"The synthesis schema or method is not supported by audit version 1.")
		incompatible = true
	}
	if evidence == nil {
		b.addFinding(FindingEvidenceUnavailable, SeverityError, "", 0, 0, "",
			"No durable evidence snapshot is present; citations cannot be resolved against a snapshot.")
		incompatible = true
	} else {
		switch evidence.Version {
		case execjournal.ResearchEvidenceLegacyVersion,
			execjournal.ResearchEvidenceProvenanceVersion,
			execjournal.ResearchEvidenceVersion:
		default:
			b.addFinding(FindingEvidenceSchemaIncompatible, SeverityError, "", 0, 0, "",
				"The evidence schema is not supported by audit version 1.")
			incompatible = true
		}
	}

	if synthesis.ExecutionID == "" || synthesis.ExecutionID != b.audit.ExecutionID {
		b.addFinding(FindingSynthesisSnapshotMismatch, SeverityError, "", 0, 0, "",
			"The synthesis is not bound to this execution id.")
		incompatible = true
	}
	if evidence != nil {
		if synthesis.EvidenceFingerprint == "" ||
			synthesis.EvidenceFingerprint != execjournal.ResearchEvidenceFingerprint(evidence) {
			b.addFinding(FindingSynthesisSnapshotMismatch, SeverityError, "", 0, 0, "",
				"The synthesis is not bound to this exact evidence fingerprint.")
			incompatible = true
		}
		if evidence.Version >= execjournal.ResearchEvidenceProvenanceVersion &&
			evidence.Plan != nil && evidence.Plan.PlanID != b.audit.ExecutionID {
			b.addFinding(FindingEvidenceSnapshotMismatch, SeverityError, "", 0, 0, "",
				"The evidence plan belongs to a different execution id.")
			incompatible = true
		}
		if evidence.Version == execjournal.ResearchEvidenceLegacyVersion {
			b.addFinding(FindingProvenanceUnavailable, SeverityError, "", 0, 0, "",
				"Legacy evidence v1 has no stable source ids; citations cannot be resolved against it.")
			incompatible = true
		}
	}

	if synthesis.PlanVersion < 0 || synthesis.PlanVersion > MaxPlanVersion {
		b.addFinding(FindingSynthesisInvalid, SeverityError, "", 0, 0, "",
			"The synthesis records a plan revision outside the supported range.")
		incompatible = true
	}
	if evidence != nil && evidence.Version >= execjournal.ResearchEvidenceProvenanceVersion {
		switch {
		case evidence.Plan == nil && synthesis.PlanVersion != 0:
			b.addFinding(FindingPlanRevisionMismatch, SeverityError, "", 0, 0, "",
				"The synthesis records a plan revision but the evidence has no plan snapshot.")
			incompatible = true
		case evidence.Plan != nil && synthesis.PlanVersion == 0:
			severity := SeverityWarning
			if evidence.Version == execjournal.ResearchEvidenceVersion {
				severity = SeverityError
				incompatible = true
			}
			b.addFinding(FindingPlanRevisionUnavailable, severity, "", 0, 0, "",
				"The synthesis does not record the plan revision present in the evidence snapshot.")
		case evidence.Plan != nil && synthesis.PlanVersion != evidence.Plan.Version:
			b.addFinding(FindingPlanRevisionMismatch, SeverityError, "", 0, 0, "",
				"The synthesis plan revision does not match the evidence plan revision.")
			incompatible = true
		}
	}

	if !incompatible && evidence != nil && evidence.Version >= execjournal.ResearchEvidenceProvenanceVersion {
		if err := execjournal.ValidateResearchEvidenceForExecution(b.audit.ExecutionID, evidence); err != nil {
			b.addFinding(FindingEvidenceInvalid, SeverityError, "", 0, 0, "",
				"The evidence snapshot does not satisfy its durable schema; citation resolution is reported against the exact recorded snapshot.")
		}
	}
	return incompatible
}

func (b *auditBuilder) markUnavailable(seeds []claimSeed) {
	for i := range seeds {
		claim := &b.audit.Claims[i]
		claim.Provenance = ProvenanceUnavailable
		claim.Status = ClaimUnavailable
		if claim.CitationRequirement == CitationNotRequired {
			claim.Provenance = ProvenanceNotApplicable
			claim.Status = ClaimNotApplicable
		}
		for j := range claim.Citations {
			claim.Citations[j].Status = CitationUnavailable
			claim.Citations[j].EvidenceStatus = EvidenceUnavailable
			claim.Citations[j].ContentAvailable = false
			claim.Citations[j].MetadataAvailable = false
			claim.Citations[j].Provenance = ProvenanceUnavailable
		}
	}
}

func (b *auditBuilder) auditClaims(seeds []claimSeed, synthesis *execjournal.ResearchSynthesis, evidence *execjournal.ResearchEvidence) {
	byID := make(map[string][]execjournal.ResearchSource, len(evidence.Sources))
	for _, source := range evidence.Sources {
		if source.ID == "" {
			continue
		}
		byID[source.ID] = append(byID[source.ID], source)
	}

	claimProvenance := ProvenanceAuthoritative
	if evidence.Version == execjournal.ResearchEvidenceProvenanceVersion {
		claimProvenance = ProvenanceLegacy
	}

	b.checkSynthesisShape(synthesis, evidence)

	citedClaims := 0
	for i := range seeds {
		seed := &seeds[i]
		claim := &b.audit.Claims[i]
		if claim.CitationRequirement == CitationNotRequired {
			claim.Status = ClaimNotApplicable
			claim.Provenance = ProvenanceNotApplicable
			if seed.malformed {
				b.addFinding(FindingClaimIdentityMalformed, SeverityError, claim.ClaimID, claim.Index, 0, "",
					"The persisted claim text is empty or whitespace-only, so its identity is not well formed.")
				claim.Status = ClaimFailed
			}
			continue
		}

		citedClaims++
		claim.Provenance = claimProvenance
		if seed.malformed {
			b.addFinding(FindingClaimIdentityMalformed, SeverityError, claim.ClaimID, claim.Index, 0, "",
				"The persisted claim text is empty or whitespace-only, so its identity is not well formed.")
		}
		if seed.duplicateOf > 0 {
			b.addFinding(FindingDuplicateClaimIdentity, SeverityError, claim.ClaimID, claim.Index, 0, "",
				"This citation-bearing claim repeats the canonical identity of an earlier claim.")
		}
		if len(claim.Citations) == 0 {
			b.addFinding(FindingCitationMissing, SeverityError, claim.ClaimID, claim.Index, 0, "",
				"This citation-bearing claim records no citation references.")
		}

		seenRefs := make(map[string]struct{}, len(claim.Citations))
		for j := range claim.Citations {
			citation := &claim.Citations[j]
			ref := citation.Reference
			sourceID := ""
			if isSafeSourceID(ref) {
				sourceID = ref
			}
			if malformedCitation(ref) {
				citation.Status = CitationMalformed
				citation.EvidenceStatus = EvidenceUnavailable
				citation.ContentAvailable = false
				citation.MetadataAvailable = false
				citation.Provenance = ProvenanceUnavailable
				b.addFinding(FindingCitationMalformed, SeverityError, claim.ClaimID, claim.Index, citation.Index, sourceID,
					"This citation reference is empty, over the schema bound or contains a forbidden control character.")
				continue
			}
			if _, duplicate := seenRefs[ref]; duplicate {
				citation.Status = CitationDuplicate
				citation.EvidenceStatus, citation.ContentAvailable, citation.MetadataAvailable, citation.Provenance =
					b.resolveSource(byID[ref], evidence.Version)
				b.addFinding(FindingCitationDuplicate, SeverityError, claim.ClaimID, claim.Index, citation.Index, sourceID,
					"This claim repeats an earlier citation reference.")
				continue
			}
			seenRefs[ref] = struct{}{}

			sources := byID[ref]
			citation.EvidenceStatus, citation.ContentAvailable, citation.MetadataAvailable, citation.Provenance =
				b.resolveSource(sources, evidence.Version)
			switch {
			case len(sources) == 0:
				citation.Status = CitationUnresolved
				citation.EvidenceStatus = EvidenceMissing
				citation.ContentAvailable = false
				citation.MetadataAvailable = false
				citation.Provenance = ProvenanceUnavailable
				b.addFinding(FindingCitationUnresolved, SeverityError, claim.ClaimID, claim.Index, citation.Index, sourceID,
					"This citation reference does not resolve to a source id in this execution's exact evidence snapshot; no other execution is consulted.")
			case len(sources) > 1:
				citation.Status = CitationAmbiguous
				citation.EvidenceStatus = EvidenceAmbiguous
				citation.ContentAvailable = false
				citation.MetadataAvailable = false
				citation.Provenance = ProvenanceUnavailable
				b.addFinding(FindingCitationAmbiguous, SeverityError, claim.ClaimID, claim.Index, citation.Index, sourceID,
					"More than one recorded source record has this id, so the citation identity is ambiguous.")
			default:
				citation.Status = CitationResolved
				citation.EvidenceStatus = EvidenceAvailable
				if !citation.ContentAvailable {
					b.addFinding(FindingEvidenceContentUnavailable, SeverityWarning, claim.ClaimID, claim.Index, citation.Index, sourceID,
						"The cited source record is present, but its recorded content excerpt is empty.")
				}
				if !citation.MetadataAvailable {
					b.addFinding(FindingEvidenceMetadataUnavailable, SeverityWarning, claim.ClaimID, claim.Index, citation.Index, sourceID,
						"The cited source record is present, but no recorded title, URL or source name is available.")
				}
			}
		}

		if evidence.Version == execjournal.ResearchEvidenceProvenanceVersion {
			b.addFinding(FindingLegacyProvenance, SeverityWarning, claim.ClaimID, claim.Index, 0, "",
				"This citation resolves against legacy v2 evidence. Stable source ids are recorded, but v3 authoritative provenance markers are unavailable.")
		}
		claim.Status = claimStatusForFindings(b.audit.Findings, claim.Index, claim.ClaimID)
		if claim.Status == "" {
			claim.Status = ClaimPassed
		}
	}

	if citedClaims == 0 {
		b.addFinding(FindingNoCitedClaims, SeverityWarning, "", 0, 0, "",
			"The synthesis records no evidence-backed statement or conflict, so no citation was audited. This is not proof of support.")
	}
}

func (b *auditBuilder) checkSynthesisShape(synthesis *execjournal.ResearchSynthesis, evidence *execjournal.ResearchEvidence) {
	invalid := func(message string) {
		b.addFinding(FindingSynthesisInvalid, SeverityError, "", 0, 0, "", message)
	}
	if strings.TrimSpace(synthesis.Summary) == "" || len(synthesis.Summary) > execjournal.MaxResearchSynthesisSummary {
		invalid("The synthesis summary is empty or outside its schema bound.")
	}
	switch synthesis.Status {
	case execjournal.ResearchSynthesisStatusInconclusive:
		if len(synthesis.EvidenceBacked)+len(synthesis.Conflicts) != 0 {
			invalid("An inconclusive synthesis carries a citation-bearing claim.")
		}
	case execjournal.ResearchSynthesisStatusPartial:
		if len(synthesis.EvidenceBacked)+len(synthesis.Conflicts) == 0 {
			invalid("A partial synthesis carries no citation-bearing claim.")
		}
	case execjournal.ResearchSynthesisStatusComplete:
		if len(synthesis.EvidenceBacked) == 0 {
			invalid("A complete synthesis carries no evidence-backed statement.")
		}
	default:
		invalid("The synthesis completion status is not recognized.")
	}
	if len(synthesis.EvidenceBacked) > execjournal.MaxResearchSynthesisStatements ||
		len(synthesis.Inferences) > execjournal.MaxResearchSynthesisInferences ||
		len(synthesis.Conflicts) > execjournal.MaxResearchSynthesisConflicts ||
		len(synthesis.UnresolvedQuestions) > execjournal.MaxResearchSynthesisQuestions {
		invalid("The synthesis carries more claims than its schema allows.")
	}
	if evidence.Count == 0 && synthesis.Status != execjournal.ResearchSynthesisStatusInconclusive {
		invalid("A synthesis over zero captured sources is not inconclusive.")
	}
}

func (b *auditBuilder) resolveSource(sources []execjournal.ResearchSource, evidenceVersion int) (status string, content, metadata bool, provenance string) {
	if len(sources) == 0 {
		return EvidenceMissing, false, false, ProvenanceUnavailable
	}
	if len(sources) > 1 {
		return EvidenceAmbiguous, false, false, ProvenanceUnavailable
	}
	source := sources[0]
	content = strings.TrimSpace(source.Snippet) != ""
	metadata = strings.TrimSpace(source.Title) != "" ||
		strings.TrimSpace(source.URL) != "" ||
		strings.TrimSpace(source.Source) != ""
	provenance = ProvenanceAuthoritative
	if evidenceVersion == execjournal.ResearchEvidenceProvenanceVersion {
		provenance = ProvenanceLegacy
	}
	return EvidenceAvailable, content, metadata, provenance
}

func claimStatusForFindings(findings []Finding, claimIndex int, claimID string) string {
	hasWarning := false
	for _, finding := range findings {
		if finding.ClaimIndex != claimIndex || finding.ClaimID != claimID {
			continue
		}
		if finding.Severity == SeverityError {
			return ClaimFailed
		}
		if finding.Severity == SeverityWarning {
			hasWarning = true
		}
	}
	if hasWarning {
		return ClaimWarnings
	}
	return ""
}

func (b *auditBuilder) finish(seeds []claimSeed) {
	if b.err != nil {
		return
	}
	sortFindings(b.audit.Findings)
	if len(b.audit.Findings) > MaxFindings {
		b.audit.Truncated = true
		filtered := b.audit.Findings[:0]
		for _, finding := range b.audit.Findings {
			if finding.Code != FindingAuditOutputTruncated {
				filtered = append(filtered, finding)
			}
		}
		b.audit.Findings = filtered
		if len(b.audit.Findings) > MaxFindings-1 {
			b.audit.Findings = b.audit.Findings[:MaxFindings-1]
		}
		b.audit.Findings = append(b.audit.Findings, Finding{
			Code:     FindingAuditOutputTruncated,
			Severity: SeverityWarning,
			Message:  "Only the first canonical findings are returned; additional findings were omitted by the bounded output.",
		})
		b.hasWarning = true
		sortFindings(b.audit.Findings)
	}

	switch {
	case b.audit.SynthesisFingerprint == "":
		b.audit.Status = StatusUnavailable
	case b.incompatible:
		b.audit.Status = StatusIncompatible
	case b.hasError:
		b.audit.Status = StatusFailed
	case b.incomplete:
		b.audit.Status = StatusIncomplete
	case b.hasWarning:
		b.audit.Status = StatusWarnings
	default:
		b.audit.Status = StatusPassed
	}

	if err := Validate(b.audit); err != nil {
		b.err = err
	}
}

func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.ClaimIndex != b.ClaimIndex {
			return a.ClaimIndex < b.ClaimIndex
		}
		if a.CitationIndex != b.CitationIndex {
			return a.CitationIndex < b.CitationIndex
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.ClaimID != b.ClaimID {
			return a.ClaimID < b.ClaimID
		}
		if a.SourceID != b.SourceID {
			return a.SourceID < b.SourceID
		}
		return a.Message < b.Message
	})
}

func malformedCitation(ref string) bool {
	return ref == "" || len(ref) > execjournal.MaxResearchSourceID || strings.ContainsAny(ref, "\x00\n\r")
}

func isSafeSourceID(ref string) bool {
	return ref != "" && len(ref) <= execjournal.MaxResearchSourceID && !strings.ContainsAny(ref, "\x00\n\r")
}

func hasForbiddenControl(value string) bool {
	return strings.ContainsAny(value, "\x00\n\r")
}

func marshalSize(value interface{}) (int, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	return len(raw), nil
}
