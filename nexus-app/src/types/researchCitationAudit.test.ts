import { describe, it, expect } from 'vitest';
import { parseResearchCitationAudit } from './researchCitationAudit';
import type {
  ResearchCitationAudit,
  ResearchCitationAuditCitation,
  ResearchCitationAuditClaim,
  ResearchCitationAuditFinding,
} from './api';

const EXECUTION_ID = 'exec-1';
const FINGERPRINT_A = 'a'.repeat(64);
const FINGERPRINT_B = 'b'.repeat(64);
const CLAIM_ID_A = 'claim-' + 'a'.repeat(64);
const CLAIM_ID_B = 'claim-' + 'b'.repeat(64);

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

function validAudit(overrides: Partial<ResearchCitationAudit> = {}): ResearchCitationAudit {
  const audit: ResearchCitationAudit = {
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
  };
  return { ...audit, ...overrides };
}

describe('parseResearchCitationAudit', () => {
  it('accepts a well-formed passed audit with resolved citations', () => {
    const parsed = parseResearchCitationAudit(validAudit());
    expect(parsed).toBeDefined();
    expect(parsed?.version).toBe(1);
    expect(parsed?.method).toBe('deterministic_v1');
    expect(parsed?.provenance).toBe('derived');
    expect(parsed?.status).toBe('passed');
    expect(parsed?.claims[0].citations[0].status).toBe('resolved');
  });

  it('accepts each stable audit status', () => {
    expect(parseResearchCitationAudit(validAudit({ status: 'passed' }))).toBeDefined();
    expect(
      parseResearchCitationAudit(
        validAudit({
          status: 'warnings',
          claims: [
            claim(1, CLAIM_ID_A, 'evidence_backed', 'warnings', [citation(1, 'src-1', 'resolved')]),
          ],
          findings: [finding('legacy_provenance', 'warning', { claim_id: CLAIM_ID_A, claim_index: 1 })],
        }),
      ),
    ).toBeDefined();
    expect(
      parseResearchCitationAudit(
        validAudit({
          status: 'failed',
          claims: [],
          findings: [finding('citation_unresolved', 'error')],
        }),
      ),
    ).toBeDefined();
    expect(
      parseResearchCitationAudit(
        validAudit({
          status: 'incomplete',
          claims: [],
          evidence_version: 2,
          plan_version: 0,
          plan_version_recorded: false,
          evidence_plan_version: 1,
          evidence_plan_recorded: true,
          findings: [finding('plan_revision_unavailable', 'warning')],
        }),
      ),
    ).toBeDefined();
    expect(
      parseResearchCitationAudit(
        validAudit({
          status: 'unavailable',
          synthesis_version: 0,
          synthesis_method: '',
          synthesis_fingerprint: '',
          synthesis_status: '',
          plan_version: 0,
          plan_version_recorded: false,
          claims: [],
          findings: [finding('synthesis_unavailable', 'warning')],
        }),
      ),
    ).toBeDefined();
    expect(
      parseResearchCitationAudit(
        validAudit({
          status: 'incompatible',
          claims: [
            {
              ...claim(1, CLAIM_ID_A, 'evidence_backed', 'unavailable', [
                citation(1, 'src-1', 'unavailable', 'unavailable'),
              ]),
              provenance: 'unavailable',
            },
          ],
          findings: [finding('synthesis_schema_incompatible', 'error')],
        }),
      ),
    ).toBeDefined();
  });

  it('rejects unknown source, version and method', () => {
    expect(parseResearchCitationAudit({ ...validAudit(), source: 'other' })).toBeUndefined();
    expect(parseResearchCitationAudit({ ...validAudit(), version: 2 })).toBeUndefined();
    expect(parseResearchCitationAudit({ ...validAudit(), method: 'semantic_v1' })).toBeUndefined();
    expect(parseResearchCitationAudit(null)).toBeUndefined();
    expect(parseResearchCitationAudit('not an audit')).toBeUndefined();
  });

  it('rejects malformed execution id and fingerprints', () => {
    expect(parseResearchCitationAudit({ ...validAudit(), execution_id: '' })).toBeUndefined();
    expect(
      parseResearchCitationAudit({ ...validAudit(), execution_id: 'a'.repeat(513) }),
    ).toBeUndefined();
    expect(
      parseResearchCitationAudit({ ...validAudit(), evidence_fingerprint: 'not-hex' }),
    ).toBeUndefined();
    expect(
      parseResearchCitationAudit({ ...validAudit(), synthesis_fingerprint: 'g'.repeat(64) }),
    ).toBeUndefined();
  });

  it('requires evidence version 2 or 3 when synthesis is present', () => {
    expect(
      parseResearchCitationAudit({ ...validAudit(), evidence_version: 1 }),
    ).toBeUndefined();
    expect(
      parseResearchCitationAudit({ ...validAudit(), evidence_version: 0 }),
    ).toBeUndefined();
  });

  it('rejects non-canonical claim order and non-canonical claim ids', () => {
    const outOfOrder = validAudit({
      claims: [
        claim(1, CLAIM_ID_A, 'unresolved_question', 'not_applicable', []),
        claim(2, CLAIM_ID_B, 'evidence_backed', 'passed', [citation(1, 'src-1')]),
      ],
    });
    expect(parseResearchCitationAudit(outOfOrder)).toBeUndefined();

    const badID = validAudit({
      claims: [claim(1, 'not-a-claim-id')],
    });
    expect(parseResearchCitationAudit(badID)).toBeUndefined();
  });

  it('rejects non-canonical finding order and over-bound collections', () => {
    const unordered = validAudit({
      status: 'warnings',
      claims: [],
      findings: [
        finding('legacy_provenance', 'warning', { claim_index: 2, claim_id: CLAIM_ID_B }),
        finding('legacy_provenance', 'warning', { claim_index: 1, claim_id: CLAIM_ID_A }),
      ],
    });
    expect(parseResearchCitationAudit(unordered)).toBeUndefined();

    const tooManyClaims = validAudit({
      claims: Array.from({ length: 65 }, (_, i) => claim(i + 1, 'claim-' + String(i).padStart(64, '0'))),
    });
    expect(parseResearchCitationAudit(tooManyClaims)).toBeUndefined();

    const tooManyCitations = validAudit({
      claims: [
        claim(1, CLAIM_ID_A, 'evidence_backed', 'passed', Array.from({ length: 13 }, (_, i) => citation(i + 1, 'src-' + i))),
      ],
    });
    expect(parseResearchCitationAudit(tooManyCitations)).toBeUndefined();
  });

  it('rejects an inconsistent truncation flag', () => {
    const truncated = validAudit({ status: 'warnings', truncated: true });
    expect(parseResearchCitationAudit(truncated)).toBeUndefined();

    const fixed = validAudit({
      status: 'warnings',
      truncated: true,
      findings: [finding('audit_output_truncated', 'warning')].concat(
        Array.from({ length: 255 }, () => finding('legacy_provenance', 'warning')),
      ),
    });
    expect(parseResearchCitationAudit(fixed)).toBeDefined();
  });

  it('rejects status/finding inconsistencies', () => {
    // Passed audit must not carry findings.
    expect(
      parseResearchCitationAudit(validAudit({ status: 'passed', findings: [finding('legacy_provenance', 'warning')] })),
    ).toBeUndefined();

    // Failed audit needs an error finding.
    expect(
      parseResearchCitationAudit(validAudit({ status: 'failed', findings: [finding('legacy_provenance', 'warning')] })),
    ).toBeUndefined();

    // Warnings audit needs a warning and no error.
    expect(
      parseResearchCitationAudit(
        validAudit({
          status: 'warnings',
          findings: [finding('citation_unresolved', 'error')],
        }),
      ),
    ).toBeUndefined();
  });

  it('rejects claim/citation inconsistencies', () => {
    // A passed claim needs at least one resolved citation.
    expect(
      parseResearchCitationAudit(
        validAudit({
          claims: [claim(1, CLAIM_ID_A, 'evidence_backed', 'passed', [])],
        }),
      ),
    ).toBeUndefined();

    // A not-required claim must carry no citations.
    expect(
      parseResearchCitationAudit(
        validAudit({
          claims: [claim(1, CLAIM_ID_A, 'inference', 'not_applicable', [citation(1, 'src-1')])],
        }),
      ),
    ).toBeUndefined();

    // A resolved citation must be evidence-available.
    expect(
      parseResearchCitationAudit(
        validAudit({
          status: 'failed',
          claims: [
            claim(1, CLAIM_ID_A, 'evidence_backed', 'failed', [
              { ...citation(1, 'src-1', 'resolved'), evidence_status: 'missing' },
            ]),
          ],
          findings: [finding('citation_unresolved', 'error', { claim_index: 1, claim_id: CLAIM_ID_A })],
        }),
      ),
    ).toBeUndefined();

    // A malformed citation must be evidence-unavailable.
    expect(
      parseResearchCitationAudit(
        validAudit({
          claims: [
            claim(1, CLAIM_ID_A, 'evidence_backed', 'failed', [
              { ...citation(1, 'src-1', 'malformed'), evidence_status: 'available' },
            ]),
          ],
          findings: [finding('citation_malformed', 'error', { claim_index: 1, claim_id: CLAIM_ID_A })],
        }),
      ),
    ).toBeUndefined();
  });

  it('rejects claim text that is empty for citation-bearing categories', () => {
    expect(
      parseResearchCitationAudit(
        validAudit({
          claims: [
            { ...claim(1, CLAIM_ID_A, 'evidence_backed', 'failed'), text: '' },
          ],
          status: 'failed',
          findings: [finding('claim_identity_malformed', 'error', { claim_index: 1, claim_id: CLAIM_ID_A })],
        }),
      ),
    ).toBeDefined();

    expect(
      parseResearchCitationAudit(
        validAudit({
          claims: [{ ...claim(1, CLAIM_ID_A), text: '' }],
        }),
      ),
    ).toBeUndefined();
  });

  it('rejects citations over the reference bound and with control characters', () => {
    expect(
      parseResearchCitationAudit(
        validAudit({
          claims: [
            claim(1, CLAIM_ID_A, 'evidence_backed', 'passed', [
              { ...citation(1, 'x'.repeat(513)) },
            ]),
          ],
        }),
      ),
    ).toBeUndefined();

    expect(
      parseResearchCitationAudit(
        validAudit({
          claims: [
            claim(1, CLAIM_ID_A, 'evidence_backed', 'failed', [
              { ...citation(1, 'line\nbreak') },
            ]),
          ],
          findings: [finding('citation_malformed', 'error', { claim_index: 1, claim_id: CLAIM_ID_A })],
        }),
      ),
    ).toBeUndefined();
  });

  it('preserves legacy provenance and marks unresolved citations', () => {
    const legacy = validAudit({
      evidence_version: 2,
      claims: [
        {
          ...claim(1, CLAIM_ID_A, 'evidence_backed', 'warnings', [
            citation(1, 'src-1', 'resolved', 'legacy'),
          ]),
          provenance: 'legacy',
        },
      ],
      findings: [finding('legacy_provenance', 'warning', { claim_index: 1, claim_id: CLAIM_ID_A })],
      status: 'warnings',
    });
    const parsed = parseResearchCitationAudit(legacy);
    expect(parsed).toBeDefined();
    expect(parsed?.claims[0].provenance).toBe('legacy');
    expect(parsed?.claims[0].citations[0].provenance).toBe('legacy');

    const unresolved = validAudit({
      status: 'failed',
      claims: [
        claim(1, CLAIM_ID_A, 'evidence_backed', 'failed', [
          citation(1, 'src-missing', 'unresolved', 'unavailable'),
        ]),
      ],
      findings: [finding('citation_unresolved', 'error', { claim_index: 1, claim_id: CLAIM_ID_A })],
    });
    const parsed2 = parseResearchCitationAudit(unresolved);
    expect(parsed2).toBeDefined();
    expect(parsed2?.claims[0].citations[0].status).toBe('unresolved');
    expect(parsed2?.claims[0].citations[0].evidence_status).toBe('missing');
  });

  it('rejects a finding that references an unknown claim or citation index', () => {
    const bad = validAudit({
      status: 'failed',
      claims: [claim(1, CLAIM_ID_A)],
      findings: [finding('citation_unresolved', 'error', { claim_index: 2, claim_id: CLAIM_ID_A })],
    });
    expect(parseResearchCitationAudit(bad)).toBeUndefined();
  });
});
