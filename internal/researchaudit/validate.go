package researchaudit

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
	claimIDPattern     = regexp.MustCompile(`^claim-[0-9a-f]{64}$`)
)

var knownFindingCodes = map[string]struct{}{
	FindingCitationMissing:             {},
	FindingCitationMalformed:           {},
	FindingCitationDuplicate:           {},
	FindingCitationUnresolved:          {},
	FindingCitationAmbiguous:           {},
	FindingEvidenceContentUnavailable:  {},
	FindingEvidenceMetadataUnavailable: {},
	FindingLegacyProvenance:            {},
	FindingProvenanceUnavailable:       {},
	FindingSynthesisSnapshotMismatch:   {},
	FindingEvidenceSnapshotMismatch:    {},
	FindingPlanRevisionUnavailable:     {},
	FindingPlanRevisionMismatch:        {},
	FindingSynthesisSchemaIncompatible: {},
	FindingEvidenceSchemaIncompatible:  {},
	FindingSynthesisInvalid:            {},
	FindingEvidenceInvalid:             {},
	FindingDuplicateClaimIdentity:      {},
	FindingClaimIdentityMalformed:      {},
	FindingSynthesisUnavailable:        {},
	FindingEvidenceUnavailable:         {},
	FindingNoCitedClaims:               {},
	FindingAuditOutputTruncated:        {},
}

var warningOnlyFindings = map[string]struct{}{
	FindingEvidenceContentUnavailable:  {},
	FindingEvidenceMetadataUnavailable: {},
	FindingLegacyProvenance:            {},
	FindingNoCitedClaims:               {},
	FindingAuditOutputTruncated:        {},
	FindingSynthesisUnavailable:        {},
}

var errorOnlyFindings = map[string]struct{}{
	FindingCitationMissing:             {},
	FindingCitationMalformed:           {},
	FindingCitationDuplicate:           {},
	FindingCitationUnresolved:          {},
	FindingCitationAmbiguous:           {},
	FindingProvenanceUnavailable:       {},
	FindingSynthesisSnapshotMismatch:   {},
	FindingEvidenceSnapshotMismatch:    {},
	FindingPlanRevisionMismatch:        {},
	FindingSynthesisSchemaIncompatible: {},
	FindingEvidenceSchemaIncompatible:  {},
	FindingSynthesisInvalid:            {},
	FindingEvidenceInvalid:             {},
	FindingDuplicateClaimIdentity:      {},
	FindingClaimIdentityMalformed:      {},
	FindingEvidenceUnavailable:         {},
}

var claimCategories = map[string]struct{}{
	CategoryEvidenceBacked: {},
	CategoryConflict:       {},
	CategoryInference:      {},
	CategoryQuestion:       {},
}

var claimStatuses = map[string]struct{}{
	ClaimPassed:        {},
	ClaimWarnings:      {},
	ClaimFailed:        {},
	ClaimUnavailable:   {},
	ClaimNotApplicable: {},
}

var citationStatuses = map[string]struct{}{
	CitationResolved:    {},
	CitationUnresolved:  {},
	CitationMalformed:   {},
	CitationDuplicate:   {},
	CitationAmbiguous:   {},
	CitationUnavailable: {},
}

var evidenceStatuses = map[string]struct{}{
	EvidenceAvailable:   {},
	EvidenceMissing:     {},
	EvidenceAmbiguous:   {},
	EvidenceUnavailable: {},
}

var provenanceValues = map[string]struct{}{
	ProvenanceAuthoritative: {},
	ProvenanceDerived:       {},
	ProvenanceLegacy:        {},
	ProvenanceUnavailable:   {},
	ProvenanceNotApplicable: {},
}

var auditStatuses = map[string]struct{}{
	StatusPassed:       {},
	StatusWarnings:     {},
	StatusFailed:       {},
	StatusIncomplete:   {},
	StatusUnavailable:  {},
	StatusIncompatible: {},
}

// Validate checks the audit response at the projection boundary. It is
// deliberately stricter than the renderer: canonical claim and finding order,
// status/finding consistency, bounded strings, valid identities and a complete
// snapshot binding are all required. A malformed projection fails closed.
func Validate(a Audit) error {
	if a.Version != Version {
		return fmt.Errorf("researchaudit: unsupported audit version %d", a.Version)
	}
	if a.Method != Method {
		return fmt.Errorf("researchaudit: unsupported audit method %q", a.Method)
	}
	if a.ExecutionID == "" || len(a.ExecutionID) > MaxExecutionID || hasForbiddenControl(a.ExecutionID) {
		return fmt.Errorf("researchaudit: invalid execution id")
	}
	if a.Provenance != ProvenanceDerived {
		return fmt.Errorf("researchaudit: audit provenance must be derived")
	}
	if _, ok := auditStatuses[a.Status]; !ok {
		return fmt.Errorf("researchaudit: invalid audit status %q", a.Status)
	}
	if a.EvidenceVersion < 0 || a.EvidenceVersion > 1_000_000 {
		return fmt.Errorf("researchaudit: invalid evidence version %d", a.EvidenceVersion)
	}
	if a.EvidenceVersion == 0 {
		if a.EvidenceFingerprint != "" {
			return fmt.Errorf("researchaudit: evidence fingerprint without an evidence version")
		}
	} else if !fingerprintPattern.MatchString(a.EvidenceFingerprint) {
		return fmt.Errorf("researchaudit: invalid evidence fingerprint")
	}
	if a.SynthesisVersion < 0 || a.SynthesisVersion > 1_000_000 {
		return fmt.Errorf("researchaudit: invalid synthesis version %d", a.SynthesisVersion)
	}
	if a.SynthesisMethod != "" && (!isBoundedText(a.SynthesisMethod) || len(a.SynthesisMethod) > MaxID) {
		return fmt.Errorf("researchaudit: invalid synthesis method")
	}
	if a.SynthesisStatus != "" && (!isBoundedText(a.SynthesisStatus) || len(a.SynthesisStatus) > MaxID) {
		return fmt.Errorf("researchaudit: invalid synthesis status")
	}
	if a.SynthesisFingerprint == "" {
		if a.SynthesisVersion != 0 || a.SynthesisMethod != "" || a.SynthesisStatus != "" ||
			a.PlanVersion != 0 || a.PlanVersionRecorded || len(a.Claims) != 0 {
			return fmt.Errorf("researchaudit: absent synthesis carries synthesis fields")
		}
	} else if !fingerprintPattern.MatchString(a.SynthesisFingerprint) {
		return fmt.Errorf("researchaudit: invalid synthesis fingerprint")
	}
	if a.PlanVersion < 0 || a.PlanVersion > MaxPlanVersion {
		return fmt.Errorf("researchaudit: plan version out of range")
	}
	if a.PlanVersionRecorded != (a.PlanVersion > 0) {
		return fmt.Errorf("researchaudit: plan version recorded flag does not match its value")
	}
	if a.EvidencePlanVersion < 0 || a.EvidencePlanVersion > MaxPlanVersion {
		return fmt.Errorf("researchaudit: evidence plan version out of range")
	}
	if a.EvidencePlanRecorded != (a.EvidencePlanVersion > 0) {
		return fmt.Errorf("researchaudit: evidence plan version recorded flag does not match its value")
	}
	if a.EvidenceVersion == 0 && a.EvidencePlanVersion != 0 {
		return fmt.Errorf("researchaudit: evidence plan version without evidence")
	}
	if a.Status != StatusIncompatible && a.SynthesisFingerprint != "" {
		if a.SynthesisVersion != execjournal.ResearchSynthesisVersion ||
			a.SynthesisMethod != execjournal.ResearchSynthesisMethod {
			return fmt.Errorf("researchaudit: incompatible synthesis schema is not marked incompatible")
		}
		if a.EvidenceVersion > 0 &&
			a.EvidenceVersion != execjournal.ResearchEvidenceLegacyVersion &&
			a.EvidenceVersion != execjournal.ResearchEvidenceProvenanceVersion &&
			a.EvidenceVersion != execjournal.ResearchEvidenceVersion {
			return fmt.Errorf("researchaudit: incompatible evidence schema is not marked incompatible")
		}
		if a.EvidenceVersion == execjournal.ResearchEvidenceLegacyVersion {
			return fmt.Errorf("researchaudit: legacy evidence is not marked incompatible")
		}
		if _, ok := map[string]struct{}{
			execjournal.ResearchSynthesisStatusComplete:     {},
			execjournal.ResearchSynthesisStatusPartial:      {},
			execjournal.ResearchSynthesisStatusInconclusive: {},
		}[a.SynthesisStatus]; !ok {
			return fmt.Errorf("researchaudit: invalid synthesis completion status")
		}
		if a.PlanVersionRecorded && !a.EvidencePlanRecorded {
			return fmt.Errorf("researchaudit: plan version without an evidence plan")
		}
		if a.PlanVersionRecorded && a.EvidencePlanRecorded && a.PlanVersion != a.EvidencePlanVersion {
			return fmt.Errorf("researchaudit: plan revisions do not match")
		}
		if a.EvidenceVersion == execjournal.ResearchEvidenceVersion &&
			a.EvidencePlanRecorded && !a.PlanVersionRecorded {
			return fmt.Errorf("researchaudit: v3 plan evidence without a plan binding")
		}
	}

	if len(a.Claims) > MaxClaims {
		return fmt.Errorf("researchaudit: too many claims")
	}
	lastCategoryRank := -1
	claimByIndex := make(map[int]Claim, len(a.Claims))
	for i := range a.Claims {
		claim := a.Claims[i]
		if claim.Index != i+1 {
			return fmt.Errorf("researchaudit: claim index is not canonical")
		}
		if !claimIDPattern.MatchString(claim.ClaimID) {
			return fmt.Errorf("researchaudit: invalid claim id")
		}
		if _, ok := claimCategories[claim.Category]; !ok {
			return fmt.Errorf("researchaudit: invalid claim category %q", claim.Category)
		}
		rank := categoryRank(claim.Category)
		if rank < lastCategoryRank {
			return fmt.Errorf("researchaudit: claim categories are not canonically ordered")
		}
		lastCategoryRank = rank
		if _, ok := claimStatuses[claim.Status]; !ok {
			return fmt.Errorf("researchaudit: invalid claim status %q", claim.Status)
		}
		if _, ok := provenanceValues[claim.Provenance]; !ok {
			return fmt.Errorf("researchaudit: invalid claim provenance %q", claim.Provenance)
		}
		if claim.Text == "" && claim.Status != ClaimFailed {
			return fmt.Errorf("researchaudit: empty claim text is not marked failed")
		}
		maxText := MaxClaimText
		if claim.Category == CategoryQuestion {
			maxText = MaxQuestionText
		}
		if len(claim.Text) > maxText {
			return fmt.Errorf("researchaudit: claim text exceeds its bound")
		}
		required := claim.Category == CategoryEvidenceBacked || claim.Category == CategoryConflict
		expectedRequirement := CitationNotRequired
		if required {
			expectedRequirement = CitationRequired
		}
		if claim.CitationRequirement != expectedRequirement {
			return fmt.Errorf("researchaudit: claim citation requirement does not match its category")
		}
		if len(claim.Citations) > MaxCitationsPerClaim {
			return fmt.Errorf("researchaudit: too many citations")
		}
		if !required && len(claim.Citations) != 0 {
			return fmt.Errorf("researchaudit: non-citation-bearing claim carries citations")
		}
		if err := validateCitations(claim); err != nil {
			return err
		}
		claimByIndex[claim.Index] = claim
	}

	if len(a.Findings) > MaxFindings {
		return fmt.Errorf("researchaudit: too many findings")
	}
	hasTruncation := false
	for i := range a.Findings {
		finding := a.Findings[i]
		if !findingCodePattern.MatchString(finding.Code) {
			return fmt.Errorf("researchaudit: invalid finding code %q", finding.Code)
		}
		if _, ok := knownFindingCodes[finding.Code]; !ok {
			return fmt.Errorf("researchaudit: unknown finding code %q", finding.Code)
		}
		if finding.Severity != SeverityWarning && finding.Severity != SeverityError {
			return fmt.Errorf("researchaudit: invalid finding severity %q", finding.Severity)
		}
		if _, ok := warningOnlyFindings[finding.Code]; ok && finding.Severity != SeverityWarning {
			return fmt.Errorf("researchaudit: warning-only finding has severity %q", finding.Severity)
		}
		if _, ok := errorOnlyFindings[finding.Code]; ok && finding.Severity != SeverityError {
			return fmt.Errorf("researchaudit: error-only finding has severity %q", finding.Severity)
		}
		if finding.Code == FindingAuditOutputTruncated {
			hasTruncation = true
		}
		if !isBoundedText(finding.Message) {
			return fmt.Errorf("researchaudit: invalid finding message")
		}
		if finding.SourceID != "" && (!isSafeSourceID(finding.SourceID) || len(finding.SourceID) > execjournal.MaxResearchSourceID) {
			return fmt.Errorf("researchaudit: invalid finding source id")
		}
		if finding.ClaimIndex == 0 {
			if finding.ClaimID != "" {
				return fmt.Errorf("researchaudit: claim id without a claim index")
			}
			if finding.CitationIndex != 0 {
				return fmt.Errorf("researchaudit: citation index without a claim index")
			}
		} else {
			claim, ok := claimByIndex[finding.ClaimIndex]
			if !ok {
				return fmt.Errorf("researchaudit: finding references an unknown claim index")
			}
			if finding.ClaimID != claim.ClaimID {
				return fmt.Errorf("researchaudit: finding claim id does not match its claim")
			}
			if finding.CitationIndex < 0 || finding.CitationIndex > len(claim.Citations) {
				return fmt.Errorf("researchaudit: finding references an unknown citation index")
			}
		}
	}
	for j := 1; j < len(a.Findings); j++ {
		if compareFindings(a.Findings[j], a.Findings[j-1]) < 0 {
			return fmt.Errorf("researchaudit: findings are not canonically ordered")
		}
	}
	if a.Truncated != hasTruncation {
		return fmt.Errorf("researchaudit: truncation flag does not match the truncation finding")
	}
	if a.Truncated && len(a.Findings) != MaxFindings {
		return fmt.Errorf("researchaudit: truncated audit does not carry its bounded finding set")
	}
	if !a.Truncated {
		for _, finding := range a.Findings {
			if finding.Code == FindingAuditOutputTruncated {
				return fmt.Errorf("researchaudit: truncation finding without the truncation flag")
			}
		}
	}
	if len(a.Limitations) == 0 || len(a.Limitations) > MaxLimitations {
		return fmt.Errorf("researchaudit: invalid limitation count")
	}
	for _, limitation := range a.Limitations {
		if !isBoundedText(limitation) {
			return fmt.Errorf("researchaudit: invalid limitation")
		}
	}

	if err := validateStatusConsistency(a); err != nil {
		return err
	}
	if raw, err := json.Marshal(a); err != nil {
		return fmt.Errorf("researchaudit: audit cannot be encoded: %w", err)
	} else if len(raw) > MaxAuditBytes {
		return fmt.Errorf("researchaudit: audit exceeds its output bound")
	}
	return nil
}

func validateCitations(claim Claim) error {
	for i := range claim.Citations {
		citation := claim.Citations[i]
		if citation.Index != i+1 {
			return fmt.Errorf("researchaudit: citation index is not canonical")
		}
		if len(citation.Reference) > MaxCitationReference {
			return fmt.Errorf("researchaudit: citation reference exceeds its bound")
		}
		if citation.Status != CitationMalformed {
			if !isSafeSourceID(citation.Reference) || len(citation.Reference) > execjournal.MaxResearchSourceID {
				return fmt.Errorf("researchaudit: non-malformed citation has an invalid reference")
			}
		}
		if _, ok := citationStatuses[citation.Status]; !ok {
			return fmt.Errorf("researchaudit: invalid citation status %q", citation.Status)
		}
		if _, ok := evidenceStatuses[citation.EvidenceStatus]; !ok {
			return fmt.Errorf("researchaudit: invalid evidence status %q", citation.EvidenceStatus)
		}
		if _, ok := provenanceValues[citation.Provenance]; !ok {
			return fmt.Errorf("researchaudit: invalid citation provenance %q", citation.Provenance)
		}
		switch citation.Status {
		case CitationResolved:
			if citation.EvidenceStatus != EvidenceAvailable {
				return fmt.Errorf("researchaudit: resolved citation is not marked evidence-available")
			}
			if citation.Provenance != ProvenanceAuthoritative && citation.Provenance != ProvenanceLegacy {
				return fmt.Errorf("researchaudit: resolved citation has invalid provenance")
			}
		case CitationUnresolved:
			if citation.EvidenceStatus != EvidenceMissing || citation.Provenance != ProvenanceUnavailable {
				return fmt.Errorf("researchaudit: unresolved citation has inconsistent evidence")
			}
		case CitationMalformed:
			if citation.EvidenceStatus != EvidenceUnavailable || citation.Provenance != ProvenanceUnavailable {
				return fmt.Errorf("researchaudit: malformed citation has inconsistent evidence")
			}
		case CitationAmbiguous:
			if citation.EvidenceStatus != EvidenceAmbiguous || citation.Provenance != ProvenanceUnavailable {
				return fmt.Errorf("researchaudit: ambiguous citation has inconsistent evidence")
			}
		case CitationUnavailable:
			if citation.EvidenceStatus != EvidenceUnavailable || citation.Provenance != ProvenanceUnavailable {
				return fmt.Errorf("researchaudit: unavailable citation has inconsistent evidence")
			}
		case CitationDuplicate:
			if citation.EvidenceStatus == EvidenceAvailable {
				if citation.Provenance != ProvenanceAuthoritative && citation.Provenance != ProvenanceLegacy {
					return fmt.Errorf("researchaudit: duplicate citation has invalid provenance")
				}
			} else if citation.Provenance != ProvenanceUnavailable {
				return fmt.Errorf("researchaudit: duplicate citation has invalid provenance")
			}
		}
		if citation.EvidenceStatus != EvidenceAvailable &&
			(citation.ContentAvailable || citation.MetadataAvailable) {
			return fmt.Errorf("researchaudit: unavailable evidence claims recorded content or metadata")
		}
	}
	return nil
}

func validateStatusConsistency(a Audit) error {
	hasError := false
	hasWarning := false
	hasPlanIncomplete := false
	for _, finding := range a.Findings {
		if finding.Severity == SeverityError {
			hasError = true
		}
		if finding.Severity == SeverityWarning {
			hasWarning = true
		}
		if finding.Code == FindingPlanRevisionUnavailable && finding.Severity == SeverityWarning {
			hasPlanIncomplete = true
		}
	}
	switch a.Status {
	case StatusUnavailable:
		if a.SynthesisFingerprint != "" || len(a.Claims) != 0 {
			return fmt.Errorf("researchaudit: unavailable audit carries synthesis or claims")
		}
		if hasError || !hasFinding(a.Findings, FindingSynthesisUnavailable) {
			return fmt.Errorf("researchaudit: unavailable audit has inconsistent findings")
		}
	case StatusIncompatible:
		if !hasError {
			return fmt.Errorf("researchaudit: incompatible audit has no error finding")
		}
		for _, claim := range a.Claims {
			if claim.CitationRequirement == CitationNotRequired {
				if claim.Status != ClaimNotApplicable {
					return fmt.Errorf("researchaudit: incompatible audit has an audited non-cited claim")
				}
				continue
			}
			if claim.Status != ClaimUnavailable {
				return fmt.Errorf("researchaudit: incompatible audit audited a citation-bearing claim")
			}
			for _, citation := range claim.Citations {
				if citation.Status != CitationUnavailable {
					return fmt.Errorf("researchaudit: incompatible audit resolved a citation")
				}
			}
		}
	case StatusFailed:
		if !hasError {
			return fmt.Errorf("researchaudit: failed audit has no error finding")
		}
	case StatusIncomplete:
		if hasError || !hasPlanIncomplete {
			return fmt.Errorf("researchaudit: incomplete audit has inconsistent findings")
		}
	case StatusWarnings:
		if hasError || !hasWarning || hasPlanIncomplete {
			return fmt.Errorf("researchaudit: warning audit has inconsistent findings")
		}
	case StatusPassed:
		if len(a.Findings) != 0 {
			return fmt.Errorf("researchaudit: passed audit carries findings")
		}
	}

	for _, claim := range a.Claims {
		errorFinding, warningFinding := claimFindings(a.Findings, claim.Index, claim.ClaimID)
		switch claim.Status {
		case ClaimPassed:
			if claim.CitationRequirement != CitationRequired || len(claim.Citations) == 0 ||
				errorFinding || warningFinding {
				return fmt.Errorf("researchaudit: passed claim has inconsistent findings or citations")
			}
			for _, citation := range claim.Citations {
				if citation.Status != CitationResolved || citation.EvidenceStatus != EvidenceAvailable {
					return fmt.Errorf("researchaudit: passed claim has an unresolved citation")
				}
			}
			if claim.Provenance != ProvenanceAuthoritative && claim.Provenance != ProvenanceLegacy {
				return fmt.Errorf("researchaudit: passed claim has invalid provenance")
			}
		case ClaimWarnings:
			if claim.CitationRequirement != CitationRequired || len(claim.Citations) == 0 ||
				errorFinding || (!warningFinding && !a.Truncated) {
				return fmt.Errorf("researchaudit: warning claim has inconsistent findings or citations")
			}
		case ClaimFailed:
			if !errorFinding && !a.Truncated {
				return fmt.Errorf("researchaudit: failed claim has no error finding")
			}
			if claim.CitationRequirement == CitationNotRequired {
				if !a.Truncated && !hasClaimFinding(a.Findings, claim.Index, claim.ClaimID, FindingClaimIdentityMalformed) {
					return fmt.Errorf("researchaudit: non-cited failed claim has an unsupported finding")
				}
			}
		case ClaimUnavailable:
			if a.Status != StatusIncompatible || claim.CitationRequirement != CitationRequired ||
				claim.Provenance != ProvenanceUnavailable {
				return fmt.Errorf("researchaudit: unavailable claim is inconsistent with the audit")
			}
			for _, citation := range claim.Citations {
				if citation.Status != CitationUnavailable {
					return fmt.Errorf("researchaudit: unavailable claim carries a resolved citation")
				}
			}
		case ClaimNotApplicable:
			if claim.CitationRequirement != CitationNotRequired || len(claim.Citations) != 0 ||
				claim.Provenance != ProvenanceNotApplicable || errorFinding || warningFinding {
				return fmt.Errorf("researchaudit: not-applicable claim has inconsistent fields")
			}
		}
	}
	return nil
}

func claimFindings(findings []Finding, claimIndex int, claimID string) (hasError, hasWarning bool) {
	for _, finding := range findings {
		if finding.ClaimIndex != claimIndex || finding.ClaimID != claimID {
			continue
		}
		if finding.Severity == SeverityError {
			hasError = true
		}
		if finding.Severity == SeverityWarning {
			hasWarning = true
		}
	}
	return hasError, hasWarning
}

func hasFinding(findings []Finding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func hasClaimFinding(findings []Finding, claimIndex int, claimID, code string) bool {
	for _, finding := range findings {
		if finding.ClaimIndex == claimIndex && finding.ClaimID == claimID && finding.Code == code {
			return true
		}
	}
	return false
}

func compareFindings(a, b Finding) int {
	if a.ClaimIndex != b.ClaimIndex {
		return compareInt(a.ClaimIndex, b.ClaimIndex)
	}
	if a.CitationIndex != b.CitationIndex {
		return compareInt(a.CitationIndex, b.CitationIndex)
	}
	if a.Code != b.Code {
		return compareStrings(a.Code, b.Code)
	}
	if a.ClaimID != b.ClaimID {
		return compareStrings(a.ClaimID, b.ClaimID)
	}
	if a.SourceID != b.SourceID {
		return compareStrings(a.SourceID, b.SourceID)
	}
	return compareStrings(a.Message, b.Message)
}

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func categoryRank(category string) int {
	switch category {
	case CategoryEvidenceBacked:
		return 0
	case CategoryConflict:
		return 1
	case CategoryInference:
		return 2
	case CategoryQuestion:
		return 3
	default:
		return -1
	}
}

func isBoundedText(value string) bool {
	return strings.TrimSpace(value) != "" && len(value) <= MaxText && !hasForbiddenControl(value)
}
