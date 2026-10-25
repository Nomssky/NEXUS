/**
 * Defensive parser for the v62 research citation audit.
 *
 * The backend validates the audit at the projection boundary. This client
 * parser is independent and fails closed: an unknown schema, malformed claim
 * identity, inconsistent citation/evidence state, non-canonical order or an
 * over-bound collection hides the whole audit rather than rendering a partial
 * or misleading result.
 */

import type {
  ResearchCitationAudit,
  ResearchCitationAuditCitation,
  ResearchCitationAuditCitationRequirement,
  ResearchCitationAuditCitationStatus,
  ResearchCitationAuditClaim,
  ResearchCitationAuditClaimCategory,
  ResearchCitationAuditClaimStatus,
  ResearchCitationAuditEvidenceStatus,
  ResearchCitationAuditFinding,
  ResearchCitationAuditProvenance,
  ResearchCitationAuditSeverity,
  ResearchCitationAuditStatus,
  ResearchSynthesisStatus,
} from './api';

const AUDIT_VERSION = 1;
const AUDIT_METHOD = 'deterministic_v1';
const AUDIT_SOURCE = 'durable_journal';

const MAX_CLAIMS = 64;
const MAX_CITATIONS = 12;
const MAX_CITATION_REFERENCE = 512;
const MAX_CLAIM_TEXT = 2000;
const MAX_QUESTION_TEXT = 1000;
const MAX_FINDINGS = 256;
const MAX_LIMITATIONS = 16;
const MAX_TEXT = 512;
const MAX_ID = 128;
const MAX_EXECUTION_ID = 512;
const MAX_SCHEMA_VERSION = 1_000_000;

const FINGERPRINT = /^[0-9a-f]{64}$/;
const CLAIM_ID = /^claim-[0-9a-f]{64}$/;
const CODE = /^[a-z][a-z0-9_]{0,63}$/;

const STATUSES: ResearchCitationAuditStatus[] = [
  'passed',
  'warnings',
  'failed',
  'incomplete',
  'unavailable',
  'incompatible',
];
const CATEGORY_ORDER: ResearchCitationAuditClaimCategory[] = [
  'evidence_backed',
  'conflict',
  'inference',
  'unresolved_question',
];
const CLAIM_STATUSES: ResearchCitationAuditClaimStatus[] = [
  'passed',
  'warnings',
  'failed',
  'unavailable',
  'not_applicable',
];
const CITATION_STATUSES: ResearchCitationAuditCitationStatus[] = [
  'resolved',
  'unresolved',
  'malformed',
  'duplicate',
  'ambiguous',
  'unavailable',
];
const EVIDENCE_STATUSES: ResearchCitationAuditEvidenceStatus[] = [
  'available',
  'missing',
  'ambiguous',
  'unavailable',
];
const PROVENANCE_VALUES: ResearchCitationAuditProvenance[] = [
  'authoritative',
  'derived',
  'legacy',
  'unavailable',
  'not_applicable',
];
const SYNTHESIS_STATUSES: ResearchSynthesisStatus[] = ['complete', 'partial', 'inconclusive'];

const KNOWN_CODES = new Set([
  'citation_missing',
  'citation_malformed',
  'citation_duplicate',
  'citation_unresolved',
  'citation_ambiguous',
  'evidence_content_unavailable',
  'evidence_metadata_unavailable',
  'legacy_provenance',
  'provenance_unavailable',
  'synthesis_snapshot_mismatch',
  'evidence_snapshot_mismatch',
  'plan_revision_unavailable',
  'plan_revision_mismatch',
  'synthesis_schema_incompatible',
  'evidence_schema_incompatible',
  'synthesis_invalid',
  'evidence_invalid',
  'duplicate_claim_identity',
  'claim_identity_malformed',
  'synthesis_unavailable',
  'evidence_unavailable',
  'no_cited_claims',
  'audit_output_truncated',
]);
const WARNING_ONLY = new Set([
  'evidence_content_unavailable',
  'evidence_metadata_unavailable',
  'legacy_provenance',
  'no_cited_claims',
  'audit_output_truncated',
  'synthesis_unavailable',
]);
const ERROR_ONLY = new Set([
  'citation_missing',
  'citation_malformed',
  'citation_duplicate',
  'citation_unresolved',
  'citation_ambiguous',
  'provenance_unavailable',
  'synthesis_snapshot_mismatch',
  'evidence_snapshot_mismatch',
  'plan_revision_mismatch',
  'synthesis_schema_incompatible',
  'evidence_schema_incompatible',
  'synthesis_invalid',
  'evidence_invalid',
  'duplicate_claim_identity',
  'claim_identity_malformed',
  'evidence_unavailable',
]);

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isInteger(value: unknown, min: number, max: number): value is number {
  return typeof value === 'number' && Number.isInteger(value) && value >= min && value <= max;
}

function hasControlChar(value: string): boolean {
  for (let i = 0; i < value.length; i += 1) {
    const code = value.charCodeAt(i);
    if (code < 0x20 || code === 0x7f) return true;
  }
  return false;
}

function isBoundedID(value: unknown, max = MAX_ID): value is string {
  return (
    typeof value === 'string' &&
    value.length > 0 &&
    value.length <= max &&
    !hasControlChar(value)
  );
}

function isSafeReference(value: unknown): value is string {
  return isBoundedID(value, 64);
}

function isAnyReference(value: unknown): value is string {
  return typeof value === 'string' && value.length <= MAX_CITATION_REFERENCE;
}

function isFingerprint(value: unknown): value is string {
  return typeof value === 'string' && FINGERPRINT.test(value);
}

function isBoundedText(value: unknown, max = MAX_TEXT): value is string {
  return (
    typeof value === 'string' &&
    value.trim().length > 0 &&
    value.length <= max &&
    !hasControlChar(value)
  );
}

function isClaimText(value: unknown, max: number): value is string {
  return typeof value === 'string' && value.length <= max;
}

function isStatus<T extends string>(value: unknown, allowed: T[]): value is T {
  return typeof value === 'string' && (allowed as string[]).includes(value);
}

function hasFinding(findings: ResearchCitationAuditFinding[], code: string): boolean {
  return findings.some((finding) => finding.code === code);
}

function claimFindings(
  findings: ResearchCitationAuditFinding[],
  claimIndex: number,
  claimID: string
): { hasError: boolean; hasWarning: boolean; codes: Set<string> } {
  let hasError = false;
  let hasWarning = false;
  const codes = new Set<string>();
  for (const finding of findings) {
    if (finding.claim_index !== claimIndex || finding.claim_id !== claimID) continue;
    codes.add(finding.code);
    if (finding.severity === 'error') hasError = true;
    if (finding.severity === 'warning') hasWarning = true;
  }
  return { hasError, hasWarning, codes };
}

function compareFindings(a: ResearchCitationAuditFinding, b: ResearchCitationAuditFinding): number {
  const aClaim = a.claim_index ?? 0;
  const bClaim = b.claim_index ?? 0;
  if (aClaim !== bClaim) return aClaim - bClaim;
  const aCitation = a.citation_index ?? 0;
  const bCitation = b.citation_index ?? 0;
  if (aCitation !== bCitation) return aCitation - bCitation;
  if (a.code !== b.code) return a.code < b.code ? -1 : 1;
  const aClaimID = a.claim_id ?? '';
  const bClaimID = b.claim_id ?? '';
  if (aClaimID !== bClaimID) return aClaimID < bClaimID ? -1 : 1;
  const aSource = a.source_id ?? '';
  const bSource = b.source_id ?? '';
  if (aSource !== bSource) return aSource < bSource ? -1 : 1;
  if (a.message !== b.message) return a.message < b.message ? -1 : 1;
  return 0;
}

function categoryRank(category: ResearchCitationAuditClaimCategory): number {
  return CATEGORY_ORDER.indexOf(category);
}

function parseCitation(value: unknown): ResearchCitationAuditCitation | undefined {
  if (!isRecord(value)) return undefined;
  if (!isInteger(value.index, 1, MAX_CITATIONS)) return undefined;
  if (!isAnyReference(value.reference)) return undefined;
  if (!isStatus(value.status, CITATION_STATUSES)) return undefined;
  if (!isStatus(value.evidence_status, EVIDENCE_STATUSES)) return undefined;
  if (typeof value.content_available !== 'boolean') return undefined;
  if (typeof value.metadata_available !== 'boolean') return undefined;
  if (!isStatus(value.provenance, PROVENANCE_VALUES)) return undefined;

  if (value.status !== 'malformed' && !isSafeReference(value.reference)) return undefined;

  switch (value.status) {
    case 'resolved':
      if (value.evidence_status !== 'available') return undefined;
      if (value.provenance !== 'authoritative' && value.provenance !== 'legacy') return undefined;
      break;
    case 'unresolved':
      if (value.evidence_status !== 'missing' || value.provenance !== 'unavailable') return undefined;
      break;
    case 'malformed':
      if (value.evidence_status !== 'unavailable' || value.provenance !== 'unavailable') return undefined;
      break;
    case 'ambiguous':
      if (value.evidence_status !== 'ambiguous' || value.provenance !== 'unavailable') return undefined;
      break;
    case 'unavailable':
      if (value.evidence_status !== 'unavailable' || value.provenance !== 'unavailable') return undefined;
      break;
    case 'duplicate':
      if (value.evidence_status === 'available') {
        if (value.provenance !== 'authoritative' && value.provenance !== 'legacy') return undefined;
      } else if (value.provenance !== 'unavailable') {
        return undefined;
      }
      break;
  }
  if (value.evidence_status !== 'available' && (value.content_available || value.metadata_available)) {
    return undefined;
  }
  return {
    index: value.index,
    reference: value.reference,
    status: value.status,
    evidence_status: value.evidence_status,
    content_available: value.content_available,
    metadata_available: value.metadata_available,
    provenance: value.provenance,
  };
}

function parseClaim(value: unknown): ResearchCitationAuditClaim | undefined {
  if (!isRecord(value)) return undefined;
  if (!isBoundedID(value.claim_id, MAX_ID) || !CLAIM_ID.test(value.claim_id)) return undefined;
  if (!isStatus(value.category, CATEGORY_ORDER)) return undefined;
  if (!isInteger(value.index, 1, MAX_CLAIMS)) return undefined;
  const maxText = value.category === 'unresolved_question' ? MAX_QUESTION_TEXT : MAX_CLAIM_TEXT;
  if (!isClaimText(value.text, maxText)) return undefined;
  if (!isStatus(value.citation_requirement, ['required', 'not_required'])) return undefined;
  if (!isStatus(value.status, CLAIM_STATUSES)) return undefined;
  if (!isStatus(value.provenance, PROVENANCE_VALUES)) return undefined;
  if (!Array.isArray(value.citations) || value.citations.length > MAX_CITATIONS) return undefined;

  const required = value.category === 'evidence_backed' || value.category === 'conflict';
  if (value.citation_requirement !== (required ? 'required' : 'not_required')) return undefined;
  if (!required && value.citations.length !== 0) return undefined;

  const citations: ResearchCitationAuditCitation[] = [];
  for (let i = 0; i < value.citations.length; i += 1) {
    const citation = parseCitation(value.citations[i]);
    if (!citation || citation.index !== i + 1) return undefined;
    citations.push(citation);
  }
  if (value.text === '' && (value.status === 'passed' || value.status === 'warnings')) {
    return undefined;
  }

  if (value.citation_requirement === 'not_required') {
    if (value.provenance !== 'not_applicable') return undefined;
  } else if (value.status === 'unavailable') {
    if (value.provenance !== 'unavailable') return undefined;
  } else if (value.provenance !== 'authoritative' && value.provenance !== 'legacy') {
    return undefined;
  }

  return {
    claim_id: value.claim_id,
    category: value.category,
    index: value.index,
    text: value.text,
    citation_requirement: value.citation_requirement as ResearchCitationAuditCitationRequirement,
    status: value.status,
    provenance: value.provenance,
    citations,
  };
}

function parseFinding(value: unknown): ResearchCitationAuditFinding | undefined {
  if (!isRecord(value)) return undefined;
  if (typeof value.code !== 'string' || !CODE.test(value.code) || !KNOWN_CODES.has(value.code)) {
    return undefined;
  }
  if (!isStatus(value.severity, ['warning', 'error'] as ResearchCitationAuditSeverity[])) {
    return undefined;
  }
  if (WARNING_ONLY.has(value.code) && value.severity !== 'warning') return undefined;
  if (ERROR_ONLY.has(value.code) && value.severity !== 'error') return undefined;
  if (!isBoundedText(value.message)) return undefined;
  if (value.source_id !== undefined && !isSafeReference(value.source_id)) return undefined;
  if (value.claim_id !== undefined && !isBoundedID(value.claim_id, MAX_ID)) return undefined;
  if (value.claim_index !== undefined && !isInteger(value.claim_index, 1, MAX_CLAIMS)) {
    return undefined;
  }
  if (value.citation_index !== undefined && !isInteger(value.citation_index, 1, MAX_CITATIONS)) {
    return undefined;
  }
  if (value.claim_index === undefined) {
    if (value.claim_id !== undefined || value.citation_index !== undefined) return undefined;
  } else if (value.claim_id === undefined) {
    return undefined;
  }
  return {
    code: value.code,
    severity: value.severity,
    ...(value.claim_id !== undefined ? { claim_id: value.claim_id } : {}),
    ...(value.claim_index !== undefined ? { claim_index: value.claim_index } : {}),
    ...(value.citation_index !== undefined ? { citation_index: value.citation_index } : {}),
    ...(value.source_id !== undefined ? { source_id: value.source_id } : {}),
    message: value.message,
  };
}

/**
 * Parse and validate a citation audit response. Returns undefined for any
 * malformed, inconsistent, unsupported or over-bound response.
 */
export function parseResearchCitationAudit(value: unknown): ResearchCitationAudit | undefined {
  if (!isRecord(value)) return undefined;
  if (value.source !== AUDIT_SOURCE) return undefined;
  if (value.version !== AUDIT_VERSION || value.method !== AUDIT_METHOD) return undefined;
  if (!isBoundedID(value.execution_id, MAX_EXECUTION_ID)) return undefined;
  if (!isStatus(value.status, STATUSES)) return undefined;
  if (value.provenance !== 'derived') return undefined;

  if (!isInteger(value.evidence_version, 0, MAX_SCHEMA_VERSION)) return undefined;
  if (value.evidence_version === 0) {
    if (value.evidence_fingerprint !== '') return undefined;
  } else if (!isFingerprint(value.evidence_fingerprint)) {
    return undefined;
  }

  if (!isInteger(value.synthesis_version, 0, MAX_SCHEMA_VERSION)) return undefined;
  if (typeof value.synthesis_method !== 'string' || value.synthesis_method.length > MAX_ID) {
    return undefined;
  }
  if (typeof value.synthesis_status !== 'string' || value.synthesis_status.length > MAX_ID) {
    return undefined;
  }
  if (value.synthesis_fingerprint === '') {
    if (
      value.synthesis_version !== 0 ||
      value.synthesis_method !== '' ||
      value.synthesis_status !== '' ||
      value.plan_version !== 0 ||
      value.plan_version_recorded !== false
    ) {
      return undefined;
    }
  } else if (!isFingerprint(value.synthesis_fingerprint)) {
    return undefined;
  }

  if (!isInteger(value.plan_version, 0, MAX_SCHEMA_VERSION)) return undefined;
  if (value.plan_version_recorded !== (value.plan_version > 0)) return undefined;
  if (!isInteger(value.evidence_plan_version, 0, MAX_SCHEMA_VERSION)) return undefined;
  if (value.evidence_plan_recorded !== (value.evidence_plan_version > 0)) return undefined;
  if (value.evidence_version === 0 && value.evidence_plan_version !== 0) return undefined;

  const synthesisPresent = value.synthesis_fingerprint !== '';
  if (synthesisPresent && value.status !== 'incompatible') {
    if (value.synthesis_version !== 1 || value.synthesis_method !== AUDIT_METHOD) return undefined;
    if (!isStatus(value.synthesis_status, SYNTHESIS_STATUSES)) return undefined;
    if (value.evidence_version !== 2 && value.evidence_version !== 3) return undefined;
    if (value.plan_version_recorded && !value.evidence_plan_recorded) return undefined;
    if (
      value.plan_version_recorded &&
      value.evidence_plan_recorded &&
      value.plan_version !== value.evidence_plan_version
    ) {
      return undefined;
    }
    if (value.evidence_version === 3 && value.evidence_plan_recorded && !value.plan_version_recorded) {
      return undefined;
    }
  }

  if (!Array.isArray(value.claims) || value.claims.length > MAX_CLAIMS) return undefined;
  const claims: ResearchCitationAuditClaim[] = [];
  let lastRank = -1;
  for (let i = 0; i < value.claims.length; i += 1) {
    const claim = parseClaim(value.claims[i]);
    if (!claim || claim.index !== i + 1) return undefined;
    const rank = categoryRank(claim.category);
    if (rank < lastRank) return undefined;
    lastRank = rank;
    claims.push(claim);
  }

  if (!Array.isArray(value.findings) || value.findings.length > MAX_FINDINGS) return undefined;
  const findings: ResearchCitationAuditFinding[] = [];
  for (let i = 0; i < value.findings.length; i += 1) {
    const finding = parseFinding(value.findings[i]);
    if (!finding) return undefined;
    if (i > 0 && compareFindings(findings[i - 1], finding) > 0) return undefined;
    if (finding.claim_index !== undefined) {
      const claim = claims[finding.claim_index - 1];
      if (!claim || claim.claim_id !== finding.claim_id) return undefined;
      if (
        finding.citation_index !== undefined &&
        finding.citation_index > claim.citations.length
      ) {
        return undefined;
      }
    }
    findings.push(finding);
  }

  if (!Array.isArray(value.limitations) || value.limitations.length === 0 ||
    value.limitations.length > MAX_LIMITATIONS) {
    return undefined;
  }
  const limitations: string[] = [];
  for (const limitation of value.limitations) {
    if (!isBoundedText(limitation)) return undefined;
    limitations.push(limitation);
  }

  if (typeof value.truncated !== 'boolean') return undefined;
  const hasTruncation = hasFinding(findings, 'audit_output_truncated');
  if (value.truncated !== hasTruncation) return undefined;
  if (value.truncated && findings.length !== MAX_FINDINGS) return undefined;

  const hasError = findings.some((finding) => finding.severity === 'error');
  const hasWarning = findings.some((finding) => finding.severity === 'warning');
  const hasPlanIncomplete = findings.some(
    (finding) => finding.code === 'plan_revision_unavailable' && finding.severity === 'warning'
  );

  switch (value.status) {
    case 'passed':
      if (findings.length !== 0) return undefined;
      break;
    case 'warnings':
      if (hasError || !hasWarning || hasPlanIncomplete) return undefined;
      break;
    case 'failed':
      if (!hasError) return undefined;
      break;
    case 'incomplete':
      if (hasError || !hasPlanIncomplete) return undefined;
      break;
    case 'unavailable':
      if (
        synthesisPresent ||
        claims.length !== 0 ||
        hasError ||
        !hasFinding(findings, 'synthesis_unavailable')
      ) {
        return undefined;
      }
      break;
    case 'incompatible':
      if (!hasError) return undefined;
      for (const claim of claims) {
        if (claim.citation_requirement === 'not_required') {
          if (claim.status !== 'not_applicable') return undefined;
          continue;
        }
        if (claim.status !== 'unavailable') return undefined;
        if (claim.citations.some((citation) => citation.status !== 'unavailable')) {
          return undefined;
        }
      }
      break;
  }

  for (const claim of claims) {
    const { hasError: claimError, hasWarning: claimWarning, codes } = claimFindings(
      findings,
      claim.index,
      claim.claim_id
    );
    switch (claim.status) {
      case 'passed':
        if (
          claim.citation_requirement !== 'required' ||
          claim.citations.length === 0 ||
          claimError ||
          claimWarning ||
          claim.citations.some(
            (citation) =>
              citation.status !== 'resolved' || citation.evidence_status !== 'available'
          )
        ) {
          return undefined;
        }
        break;
      case 'warnings':
        if (
          claim.citation_requirement !== 'required' ||
          claim.citations.length === 0 ||
          claimError ||
          (!claimWarning && !value.truncated)
        ) {
          return undefined;
        }
        break;
      case 'failed':
        if (!claimError && !value.truncated) return undefined;
        if (claim.citation_requirement === 'not_required' && !value.truncated &&
          !codes.has('claim_identity_malformed')) {
          return undefined;
        }
        break;
      case 'unavailable':
        if (
          value.status !== 'incompatible' ||
          claim.citation_requirement !== 'required' ||
          claim.provenance !== 'unavailable' ||
          claim.citations.some((citation) => citation.status !== 'unavailable')
        ) {
          return undefined;
        }
        break;
      case 'not_applicable':
        if (
          claim.citation_requirement !== 'not_required' ||
          claim.citations.length !== 0 ||
          claim.provenance !== 'not_applicable' ||
          claimError ||
          claimWarning
        ) {
          return undefined;
        }
        break;
    }
  }

  return {
    source: AUDIT_SOURCE,
    version: AUDIT_VERSION,
    method: AUDIT_METHOD,
    execution_id: value.execution_id,
    evidence_version: value.evidence_version,
    evidence_fingerprint: value.evidence_fingerprint,
    synthesis_version: value.synthesis_version,
    synthesis_method: value.synthesis_method,
    synthesis_fingerprint: value.synthesis_fingerprint,
    synthesis_status: value.synthesis_status as ResearchSynthesisStatus | '',
    plan_version: value.plan_version,
    plan_version_recorded: value.plan_version_recorded,
    evidence_plan_version: value.evidence_plan_version,
    evidence_plan_recorded: value.evidence_plan_recorded,
    provenance: 'derived',
    status: value.status,
    claims,
    findings,
    limitations,
    truncated: value.truncated,
  };
}
