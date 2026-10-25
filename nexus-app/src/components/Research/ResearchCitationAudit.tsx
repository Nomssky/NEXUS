/**
 * Restrained claim-level citation integrity audit.
 *
 * The panel is an on-demand, read-only projection over one durable synthesis
 * and its exact evidence snapshot. It reports recorded citation identity,
 * evidence availability and provenance only. It never claims that evidence
 * supports a statement, and it has no score, percentage, chart or KPI.
 */

import React, { useMemo } from 'react';
import { Badge } from '../primitives';
import type { BadgeVariant } from '../primitives';
import { useRecoveryCitationAudit } from '../../api/hooks';
import type {
  ResearchCitationAudit as ResearchCitationAuditData,
  ResearchCitationAuditCitation,
  ResearchCitationAuditCitationStatus,
  ResearchCitationAuditClaim,
  ResearchCitationAuditClaimCategory,
  ResearchCitationAuditClaimStatus,
  ResearchCitationAuditEvidenceStatus,
  ResearchCitationAuditFinding,
  ResearchCitationAuditProvenance,
  ResearchCitationAuditStatus,
} from '../../types/api';
import { parseResearchCitationAudit } from '../../types/researchCitationAudit';
import styles from './ResearchCitationAudit.module.css';

const STATUS_LABELS: Record<ResearchCitationAuditStatus, string> = {
  passed: 'Citation audit passed',
  warnings: 'Citation audit warnings',
  failed: 'Structural citation failure',
  incomplete: 'Citation audit incomplete',
  unavailable: 'Citation audit unavailable',
  incompatible: 'Citation audit incompatible',
};

const STATUS_VARIANTS: Record<ResearchCitationAuditStatus, BadgeVariant> = {
  passed: 'success',
  warnings: 'warning',
  failed: 'error',
  incomplete: 'warning',
  unavailable: 'unknown',
  incompatible: 'error',
};

const STATUS_DESCRIPTIONS: Record<ResearchCitationAuditStatus, string> = {
  passed:
    'Every citation-bearing claim resolved within the exact evidence snapshot recorded for this execution.',
  warnings:
    'Citations resolved, but one or more evidence-availability or provenance warnings remain.',
  failed:
    'One or more citation-bearing claims have structural citation or claim-identity failures.',
  incomplete:
    'The audit is structurally valid, but the recorded plan revision is unavailable.',
  unavailable:
    'No durable synthesis is recorded for this execution. Absence is not an empty or clean synthesis.',
  incompatible:
    'The synthesis, evidence, execution or plan bindings are incompatible. Claims were not audited against a different snapshot.',
};

const CATEGORY_LABELS: Record<ResearchCitationAuditClaimCategory, string> = {
  evidence_backed: 'Evidence-backed statement',
  conflict: 'Conflict',
  inference: 'Inference',
  unresolved_question: 'Unresolved question',
};

const CLAIM_STATUS_LABELS: Record<ResearchCitationAuditClaimStatus, string> = {
  passed: 'Passed',
  warnings: 'Warnings',
  failed: 'Failed',
  unavailable: 'Not audited',
  not_applicable: 'Not applicable',
};

const CLAIM_STATUS_VARIANTS: Record<ResearchCitationAuditClaimStatus, BadgeVariant> = {
  passed: 'success',
  warnings: 'warning',
  failed: 'error',
  unavailable: 'unknown',
  not_applicable: 'default',
};

const CITATION_STATUS_LABELS: Record<ResearchCitationAuditCitationStatus, string> = {
  resolved: 'Citation resolved',
  unresolved: 'Citation unresolved',
  malformed: 'Citation malformed',
  duplicate: 'Citation duplicate',
  ambiguous: 'Citation ambiguous',
  unavailable: 'Citation unavailable',
};

const CITATION_STATUS_VARIANTS: Record<ResearchCitationAuditCitationStatus, BadgeVariant> = {
  resolved: 'success',
  unresolved: 'error',
  malformed: 'error',
  duplicate: 'error',
  ambiguous: 'error',
  unavailable: 'unknown',
};

const EVIDENCE_STATUS_LABELS: Record<ResearchCitationAuditEvidenceStatus, string> = {
  available: 'Evidence available',
  missing: 'Evidence missing',
  ambiguous: 'Evidence ambiguous',
  unavailable: 'Evidence unavailable',
};

const PROVENANCE_LABELS: Record<ResearchCitationAuditProvenance, string> = {
  authoritative: 'Authoritative provenance',
  derived: 'Derived provenance',
  legacy: 'Legacy provenance',
  unavailable: 'Provenance unavailable',
  not_applicable: 'Citations not required',
};

const SEVERITY_LABELS: Record<ResearchCitationAuditFinding['severity'], string> = {
  warning: 'Warning',
  error: 'Error',
};

function claimFindings(
  audit: ResearchCitationAuditData,
  claim: ResearchCitationAuditClaim
): ResearchCitationAuditFinding[] {
  return audit.findings.filter(
    (finding) =>
      finding.claim_index === claim.index && finding.claim_id === claim.claim_id
  );
}

function FindingList({
  findings,
  label,
}: {
  findings: ResearchCitationAuditFinding[];
  label: string;
}) {
  if (findings.length === 0) return null;
  return (
    <div className={styles.findings}>
      <h5 className={styles.findingsHeading}>{label}</h5>
      <ul className={styles.findingList}>
        {findings.map((finding, index) => (
          <li
            key={`${finding.code}-${finding.claim_index ?? 0}-${finding.citation_index ?? 0}-${index}`}
            className={styles.finding}
          >
            <Badge
              variant={finding.severity === 'error' ? 'error' : 'warning'}
              size="sm"
            >
              {SEVERITY_LABELS[finding.severity]}
            </Badge>
            <div className={styles.findingText}>
              <code>{finding.code}</code>
              <p>{finding.message}</p>
              {finding.source_id && (
                <span className={styles.findingSource}>
                  Source identity: <code>{finding.source_id}</code>
                </span>
              )}
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}

function CitationRow({ citation }: { citation: ResearchCitationAuditCitation }) {
  return (
    <li
      className={styles.citation}
      data-citation-status={citation.status}
      data-evidence-status={citation.evidence_status}
    >
      <div className={styles.citationHeader}>
        <code className={styles.reference}>
          {citation.reference === '' ? 'Empty reference' : citation.reference}
        </code>
        <Badge variant={CITATION_STATUS_VARIANTS[citation.status]} size="sm">
          {CITATION_STATUS_LABELS[citation.status]}
        </Badge>
      </div>
      <div className={styles.citationMeta}>
        <span>{EVIDENCE_STATUS_LABELS[citation.evidence_status]}</span>
        <span>{citation.content_available ? 'Content recorded' : 'Content unavailable'}</span>
        <span>
          {citation.metadata_available ? 'Metadata recorded' : 'Metadata unavailable'}
        </span>
        <span>{PROVENANCE_LABELS[citation.provenance]}</span>
      </div>
    </li>
  );
}

function ClaimCard({
  audit,
  claim,
}: {
  audit: ResearchCitationAuditData;
  claim: ResearchCitationAuditClaim;
}) {
  const findings = claimFindings(audit, claim);
  const required = claim.citation_requirement === 'required';

  return (
    <article
      className={styles.claim}
      data-claim-status={claim.status}
      data-claim-category={claim.category}
    >
      <header className={styles.claimHeader}>
        <div className={styles.claimLabels}>
          <span className={styles.claimIndex}>Claim {claim.index}</span>
          <Badge variant="info" size="sm">
            {CATEGORY_LABELS[claim.category]}
          </Badge>
        </div>
        <Badge variant={CLAIM_STATUS_VARIANTS[claim.status]} size="sm">
          {CLAIM_STATUS_LABELS[claim.status]}
        </Badge>
      </header>

      <p className={styles.claimText}>{claim.text}</p>

      {required ? (
        claim.citations.length > 0 ? (
          <ul className={styles.citationList} aria-label={`Citations for claim ${claim.index}`}>
            {claim.citations.map((citation) => (
              <CitationRow key={citation.index} citation={citation} />
            ))}
          </ul>
        ) : (
          <p className={styles.notice}>No citation reference recorded.</p>
        )
      ) : (
        <p className={styles.notice}>
          Citations not required for this {CATEGORY_LABELS[claim.category].toLowerCase()}.
        </p>
      )}

      <FindingList findings={findings} label={`Claim ${claim.index} findings`} />
    </article>
  );
}

export interface ResearchCitationAuditResultProps {
  audit: ResearchCitationAuditData;
}

/**
 * Pure renderer used by the workspace and by server-rendered tests.
 */
export const ResearchCitationAuditResult: React.FC<ResearchCitationAuditResultProps> = ({
  audit,
}) => {
  const topLevelFindings = audit.findings.filter(
    (finding) => finding.claim_index === undefined
  );
  const planLabel = audit.plan_version_recorded
    ? `Plan revision ${audit.plan_version}`
    : 'Plan revision unavailable';

  return (
    <section
      className={styles.audit}
      aria-labelledby="research-citation-audit-heading"
      data-audit-status={audit.status}
    >
      <div className={styles.header}>
        <div className={styles.heading}>
          <h3 id="research-citation-audit-heading" className={styles.title}>
            Citation Integrity Audit
          </h3>
          <p className={styles.intro}>
            Structural check of recorded claim citations against this execution&apos;s exact
            evidence snapshot. It reports citation identity, evidence availability and
            provenance only; it does not verify facts or semantic entailment.
          </p>
          <p className={styles.semanticNote}>Semantic support not verified.</p>
        </div>
        <Badge variant={STATUS_VARIANTS[audit.status]}>{STATUS_LABELS[audit.status]}</Badge>
      </div>

      <div className={styles.statusBlock}>
        <p>{STATUS_DESCRIPTIONS[audit.status]}</p>
      </div>

      <dl className={styles.metadata}>
        <div>
          <dt>Execution</dt>
          <dd className={styles.mono}>{audit.execution_id}</dd>
        </div>
        <div>
          <dt>Evidence</dt>
          <dd>
            {audit.evidence_version > 0
              ? `Evidence schema v${audit.evidence_version}`
              : 'Evidence schema unavailable'}
          </dd>
        </div>
        <div>
          <dt>Synthesis</dt>
          <dd>
            {audit.synthesis_fingerprint
              ? `Synthesis schema v${audit.synthesis_version}${
                  audit.synthesis_status ? `, ${audit.synthesis_status}` : ''
                }`
              : 'Synthesis schema unavailable'}
          </dd>
        </div>
        <div>
          <dt>Plan</dt>
          <dd>{planLabel}</dd>
        </div>
      </dl>

      <FindingList findings={topLevelFindings} label="Audit findings" />

      {audit.claims.length > 0 && (
        <div className={styles.claims}>
          <h4 className={styles.sectionHeading}>Claims</h4>
          <div className={styles.claimList}>
            {audit.claims.map((claim) => (
              <ClaimCard key={claim.claim_id} audit={audit} claim={claim} />
            ))}
          </div>
        </div>
      )}

      {audit.claims.length === 0 && audit.status !== 'unavailable' && (
        <p className={styles.notice}>No claims were returned by the bounded audit.</p>
      )}

      {audit.truncated && (
        <p className={styles.truncated} role="status">
          Additional findings were omitted by the bounded audit output.
        </p>
      )}

      <div className={styles.limitations}>
        <h4 className={styles.sectionHeading}>Limits of this audit</h4>
        <ul>
          {audit.limitations.map((limitation) => (
            <li key={limitation}>{limitation}</li>
          ))}
        </ul>
      </div>
    </section>
  );
};

interface ResearchCitationAuditProps {
  businessId: string;
  executionId: string | null;
  synthesisAvailable: boolean;
}

function AuditFrame({ children }: { children: React.ReactNode }) {
  return (
    <section
      className={styles.audit}
      aria-labelledby="research-citation-audit-heading"
      data-audit-status="loading"
    >
      <div className={styles.header}>
        <div className={styles.heading}>
          <h3 id="research-citation-audit-heading" className={styles.title}>
            Citation Integrity Audit
          </h3>
          <p className={styles.semanticNote}>Semantic support not verified.</p>
        </div>
      </div>
      {children}
    </section>
  );
}

/**
 * Workspace wrapper. It fetches only when a durable synthesis is present, and
 * never polls: a settled synthesis and its evidence snapshot are immutable.
 */
export const ResearchCitationAudit: React.FC<ResearchCitationAuditProps> = ({
  businessId,
  executionId,
  synthesisAvailable,
}) => {
  const auditQuery = useRecoveryCitationAudit(
    synthesisAvailable ? executionId : null,
    businessId
  );

  const parsed = useMemo(
    () => (auditQuery.data ? parseResearchCitationAudit(auditQuery.data) : undefined),
    [auditQuery.data]
  );

  if (!synthesisAvailable || !executionId || !businessId) return null;

  if (auditQuery.isLoading) {
    return (
      <AuditFrame>
        <p className={styles.notice}>Checking recorded citation identities…</p>
      </AuditFrame>
    );
  }

  if (auditQuery.isError) {
    return (
      <AuditFrame>
        <p className={styles.error} role="alert">
          Citation audit could not be loaded. The recorded synthesis is unchanged.
        </p>
      </AuditFrame>
    );
  }

  if (!auditQuery.data) {
    return (
      <AuditFrame>
        <p className={styles.notice}>Citation audit is not available.</p>
      </AuditFrame>
    );
  }

  if (!parsed) {
    return (
      <AuditFrame>
        <p className={styles.error} role="alert">
          The citation audit response could not be validated, so it is not shown.
        </p>
      </AuditFrame>
    );
  }

  return <ResearchCitationAuditResult audit={parsed} />;
};
