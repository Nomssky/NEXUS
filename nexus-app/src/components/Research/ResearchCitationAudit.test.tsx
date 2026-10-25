/**
 * ResearchCitationAudit rendering tests.
 *
 * Rendered with react-dom/server so assertions cover the actual text, the
 * explicit missing/legacy/unavailable copy and the absence of any score,
 * percentage or semantic-support claim.
 */

import React from 'react';
import { describe, it, expect } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { ResearchCitationAuditResult } from './ResearchCitationAudit';
import type {
  ResearchCitationAudit,
  ResearchCitationAuditCitation,
  ResearchCitationAuditClaim,
  ResearchCitationAuditFinding,
} from '../../types/api';

const EXECUTION_ID = 'exec-1';
const FINGERPRINT_A = 'a'.repeat(64);
const FINGERPRINT_B = 'b'.repeat(64);
const CLAIM_ID_A = 'claim-' + 'a'.repeat(64);

function citation(
  index: number,
  reference: string,
  status: ResearchCitationAuditCitation['status'] = 'resolved',
  provenance: ResearchCitationAuditCitation['provenance'] = 'authoritative',
): ResearchCitationAuditCitation {
  const evidenceStatus =
    status === 'resolved'
      ? 'available'
      : status === 'unresolved'
      ? 'missing'
      : status === 'ambiguous'
      ? 'ambiguous'
      : 'unavailable';
  return {
    index,
    reference,
    status,
    evidence_status: evidenceStatus,
    content_available: status === 'resolved',
    metadata_available: status === 'resolved',
    provenance,
  };
}

function claim(
  index: number,
  claimID: string,
  category: ResearchCitationAuditClaim['category'] = 'evidence_backed',
  status: ResearchCitationAuditClaim['status'] = 'passed',
  citations: ResearchCitationAuditCitation[] = [citation(1, 'src-1')],
): ResearchCitationAuditClaim {
  return {
    claim_id: claimID,
    category,
    index,
    text: `Statement ${index}.`,
    citation_requirement: category === 'evidence_backed' || category === 'conflict' ? 'required' : 'not_required',
    status,
    provenance: category === 'evidence_backed' || category === 'conflict' ? 'authoritative' : 'not_applicable',
    citations,
  };
}

function finding(
  code: ResearchCitationAuditFinding['code'],
  severity: ResearchCitationAuditFinding['severity'],
  overrides: Partial<ResearchCitationAuditFinding> = {},
): ResearchCitationAuditFinding {
  return {
    code,
    severity,
    message: `Finding ${code}.`,
    ...overrides,
  };
}

function audit(overrides: Partial<ResearchCitationAudit> = {}): ResearchCitationAudit {
  return {
    source: 'durable_journal',
    version: 1,
    method: 'deterministic_v1',
    execution_id: EXECUTION_ID,
    evidence_version: 3,
    evidence_fingerprint: FINGERPRINT_A,
    synthesis_version: 1,
    synthesis_method: 'deterministic_v1',
    synthesis_fingerprint: FINGERPRINT_B,
    synthesis_status: 'complete',
    plan_version: 1,
    plan_version_recorded: true,
    evidence_plan_version: 1,
    evidence_plan_recorded: true,
    provenance: 'derived',
    status: 'passed',
    claims: [claim(1, CLAIM_ID_A)],
    findings: [],
    limitations: ['This audit checks recorded citation structure only.'],
    truncated: false,
    ...overrides,
  };
}

function renderAudit(overrides: Partial<ResearchCitationAudit> = {}): string {
  return renderToStaticMarkup(<ResearchCitationAuditResult audit={audit(overrides)} />);
}

describe('ResearchCitationAuditResult', () => {
  it('renders the passed state with a resolved citation', () => {
    const markup = renderAudit();
    expect(markup).toContain('Citation Integrity Audit');
    expect(markup).toContain('Citation audit passed');
    expect(markup).toContain('Every citation-bearing claim resolved');
    expect(markup).toContain('Claim 1');
    expect(markup).toContain('Citation resolved');
    expect(markup).toContain('Evidence available');
    expect(markup).toContain('Authoritative provenance');
    expect(markup).toContain('src-1');
    expect(markup).toContain('Semantic support not verified.');
  });

  it('renders warnings state with evidence availability caveats', () => {
    const markup = renderAudit({
      status: 'warnings',
      claims: [
        claim(1, CLAIM_ID_A, 'evidence_backed', 'warnings', [
          { ...citation(1, 'src-1', 'resolved'), content_available: false },
        ]),
      ],
      findings: [finding('evidence_content_unavailable', 'warning', { claim_index: 1, claim_id: CLAIM_ID_A })],
    });
    expect(markup).toContain('Citation audit warnings');
    expect(markup).toContain('Claim 1');
    expect(markup).toContain('Warnings');
    expect(markup).toContain('Content unavailable');
    expect(markup).not.toContain('Structural citation failure');
  });

  it('renders failed state with unresolved citation', () => {
    const markup = renderAudit({
      status: 'failed',
      claims: [
        claim(1, CLAIM_ID_A, 'evidence_backed', 'failed', [citation(1, 'src-missing', 'unresolved')]),
      ],
      findings: [finding('citation_unresolved', 'error', { claim_index: 1, claim_id: CLAIM_ID_A })],
    });
    expect(markup).toContain('Structural citation failure');
    expect(markup).toContain('Citation unresolved');
    expect(markup).toContain('Evidence missing');
    expect(markup).not.toContain('verified facts');
  });

  it('renders incomplete state with unavailable plan revision', () => {
    const markup = renderAudit({
      status: 'incomplete',
      claims: [],
      plan_version: 0,
      plan_version_recorded: false,
      evidence_plan_version: 1,
      evidence_plan_recorded: true,
      findings: [finding('plan_revision_unavailable', 'warning')],
    });
    expect(markup).toContain('Citation audit incomplete');
    expect(markup).toContain('Plan revision unavailable');
    expect(markup).not.toContain('error');
  });

  it('renders unavailable state when no synthesis is recorded', () => {
    const markup = renderAudit({
      status: 'unavailable',
      claims: [],
      synthesis_version: 0,
      synthesis_method: '',
      synthesis_fingerprint: '',
      synthesis_status: '',
      plan_version: 0,
      plan_version_recorded: false,
      findings: [finding('synthesis_unavailable', 'warning')],
    });
    expect(markup).toContain('Citation audit unavailable');
    expect(markup).toContain('No durable synthesis is recorded');
    expect(markup).toContain('Absence is not an empty or clean synthesis.');
    expect(markup).not.toContain('Claims');
  });

  it('renders incompatible state without auditing claims', () => {
    const markup = renderAudit({
      status: 'incompatible',
      claims: [
        claim(1, CLAIM_ID_A, 'evidence_backed', 'unavailable', [citation(1, 'src-1', 'unavailable')]),
      ],
      findings: [finding('synthesis_schema_incompatible', 'error')],
    });
    expect(markup).toContain('Citation audit incompatible');
    expect(markup).toContain('Claims were not audited against a different snapshot.');
    expect(markup).not.toContain('Citation resolved');
  });

  it('renders not-required claims for inferences and unresolved questions', () => {
    const markup = renderAudit({
      claims: [
        claim(1, CLAIM_ID_A, 'inference', 'not_applicable', []),
        claim(2, 'claim-' + 'b'.repeat(64), 'unresolved_question', 'not_applicable', []),
      ],
    });
    expect(markup).toContain('Inference');
    expect(markup).toContain('Unresolved question');
    expect(markup).toContain('Citations not required');
  });

  it('renders duplicate, malformed and ambiguous citation statuses', () => {
    const markup = renderAudit({
      status: 'failed',
      claims: [
        claim(1, CLAIM_ID_A, 'evidence_backed', 'failed', [
          citation(1, 'src-1', 'duplicate'),
          citation(2, 'bad', 'malformed'),
          citation(3, 'src-3', 'ambiguous'),
        ]),
      ],
      findings: [
        finding('citation_duplicate', 'error', { claim_index: 1, claim_id: CLAIM_ID_A, citation_index: 1 }),
        finding('citation_malformed', 'error', { claim_index: 1, claim_id: CLAIM_ID_A, citation_index: 2 }),
        finding('citation_ambiguous', 'error', { claim_index: 1, claim_id: CLAIM_ID_A, citation_index: 3 }),
      ],
    });
    expect(markup).toContain('Citation duplicate');
    expect(markup).toContain('Citation malformed');
    expect(markup).toContain('Citation ambiguous');
    expect(markup).toContain('Evidence unavailable');
  });

  it('renders legacy provenance explicitly', () => {
    const markup = renderAudit({
      evidence_version: 2,
      claims: [
        claim(1, CLAIM_ID_A, 'evidence_backed', 'warnings', [
          citation(1, 'src-1', 'resolved', 'legacy'),
        ]),
      ],
      findings: [finding('legacy_provenance', 'warning', { claim_index: 1, claim_id: CLAIM_ID_A })],
      limitations: ['Legacy evidence is resolved by its recorded ids only. Missing v3 provenance remains unavailable and is never inferred from URLs, titles, order or newer snapshots.'],
      status: 'warnings',
    });
    expect(markup).toContain('Legacy provenance');
    expect(markup).toContain('Missing v3 provenance remains unavailable');
  });

  it('renders findings and limitations without scores', () => {
    const markup = renderAudit({
      status: 'failed',
      findings: [finding('citation_missing', 'error', { claim_index: 1, claim_id: CLAIM_ID_A })],
      limitations: ['Limit one.', 'Limit two.'],
    });
    expect(markup).toContain('citation_missing');
    expect(markup).toContain('Limit one.');
    expect(markup).toContain('Limit two.');
    expect(markup).not.toContain('%');
    expect(markup).not.toContain('score');
    expect(markup).not.toContain('verified facts');
    expect(markup).not.toMatch(/trust|confidence|rank/i);
  });

  it('marks truncated output', () => {
    const markup = renderAudit({
      status: 'warnings',
      truncated: true,
      findings: Array.from({ length: 256 }, (_, i) =>
        finding('legacy_provenance', 'warning', { claim_index: (i % 4) + 1, claim_id: CLAIM_ID_A }),
      ).concat(finding('audit_output_truncated', 'warning')),
    });
    expect(markup).toContain('Additional findings were omitted');
  });

  it('has no KPI cards, charts or rankings', () => {
    const markup = renderAudit();
    expect(markup).not.toMatch(/chart|kpi|rank|trust|confidence|verified fact/i);
  });
});
