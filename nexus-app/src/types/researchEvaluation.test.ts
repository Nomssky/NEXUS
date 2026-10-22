import { describe, it, expect } from 'vitest';
import { parseResearchEvaluation } from './researchEvaluation';
import type { ResearchEvaluation } from './api';
import type { ParsedDurableEvidence } from './research';

const FINGERPRINT = 'a'.repeat(64);

function evaluation(overrides: Partial<ResearchEvaluation> = {}): ResearchEvaluation {
  return {
    version: 1,
    method: 'deterministic_v1',
    execution_id: 'exec-1',
    evidence_version: 2,
    evidence_count: 1,
    evidence_fingerprint: FINGERPRINT,
    synthesis_status: 'complete',
    synthesis_fingerprint: 'b'.repeat(64),
    status: 'warnings',
    dimensions: [
      { id: 'evidence_coverage', status: 'pass' },
      { id: 'citation_integrity', status: 'pass' },
      { id: 'relevance', status: 'warn' },
      { id: 'source_quality', status: 'pass' },
      { id: 'source_diversity', status: 'pass' },
      { id: 'conflict_handling', status: 'not_measurable' },
      { id: 'uncertainty_handling', status: 'pass' },
      { id: 'plan_completeness', status: 'pass' },
    ],
    findings: [
      {
        code: 'source_no_query_overlap',
        dimension: 'relevance',
        severity: 'warning',
        message: 'Source text has no meaningful lexical overlap with the objective.',
        source_id: 'src-1',
      },
    ],
    limitations: ['Citation integrity proves reference resolution only.'],
    ...overrides,
  };
}

const durableEvidence: ParsedDurableEvidence = {
  present: true,
  version: 2,
  count: 1,
  sources: [],
  steps: [],
  unattributedSources: [],
};

describe('parseResearchEvaluation', () => {
  it('accepts a well-formed evaluation for the active execution', () => {
    const parsed = parseResearchEvaluation(evaluation(), 'exec-1', durableEvidence);
    expect(parsed).toBeDefined();
    expect(parsed?.status).toBe('warnings');
    expect(parsed?.findings[0].code).toBe('source_no_query_overlap');
  });

  it('rejects an evaluation bound to another execution', () => {
    expect(parseResearchEvaluation(evaluation(), 'exec-2', durableEvidence)).toBeUndefined();
  });

  it('rejects an evaluation whose evidence binding does not match the durable evidence', () => {
    const mismatched = evaluation({ evidence_count: 7 });
    expect(parseResearchEvaluation(mismatched, 'exec-1', durableEvidence)).toBeUndefined();
  });

  it('rejects an unknown method or version', () => {
    expect(
      parseResearchEvaluation({ ...evaluation(), method: 'model_v1' }, 'exec-1', durableEvidence)
    ).toBeUndefined();
    expect(parseResearchEvaluation({ ...evaluation(), version: 2 }, 'exec-1', durableEvidence)).toBeUndefined();
  });

  it('rejects an unknown or repeated dimension', () => {
    const unknown = evaluation();
    unknown.dimensions[0] = { id: 'made_up' as never, status: 'pass' };
    expect(parseResearchEvaluation(unknown, 'exec-1', durableEvidence)).toBeUndefined();

    const repeated = evaluation();
    repeated.dimensions[1] = { id: 'evidence_coverage', status: 'pass' };
    expect(parseResearchEvaluation(repeated, 'exec-1', durableEvidence)).toBeUndefined();
  });

  it('rejects a status that contradicts its findings', () => {
    const passedWithWarning = evaluation({ status: 'passed' });
    expect(parseResearchEvaluation(passedWithWarning, 'exec-1', durableEvidence)).toBeUndefined();

    const failedWithoutFailure = evaluation({ status: 'failed' });
    expect(parseResearchEvaluation(failedWithoutFailure, 'exec-1', durableEvidence)).toBeUndefined();
  });

  it('rejects a finding with an unknown code shape or dimension', () => {
    const badCode = evaluation();
    badCode.findings[0].code = 'Not A Code';
    expect(parseResearchEvaluation(badCode, 'exec-1', durableEvidence)).toBeUndefined();

    const badDimension = evaluation();
    badDimension.findings[0].dimension = 'made_up' as never;
    expect(parseResearchEvaluation(badDimension, 'exec-1', durableEvidence)).toBeUndefined();
  });

  it('rejects a synthesis fingerprint without a synthesis status', () => {
    const orphan = evaluation({ synthesis_status: undefined });
    expect(parseResearchEvaluation(orphan, 'exec-1', durableEvidence)).toBeUndefined();
  });

  it('accepts an evaluation with no evidence and no synthesis', () => {
    const noEvidence = evaluation({
      evidence_version: 0,
      evidence_count: 0,
      synthesis_status: undefined,
      synthesis_fingerprint: undefined,
      status: 'incomplete',
      findings: [
        {
          code: 'evidence_unavailable',
          dimension: 'evidence_coverage',
          severity: 'warning',
          message: 'No complete evidence snapshot was available for evaluation.',
        },
      ],
      dimensions: [
        { id: 'evidence_coverage', status: 'incomplete' },
        { id: 'citation_integrity', status: 'incomplete' },
        { id: 'relevance', status: 'not_applicable' },
        { id: 'source_quality', status: 'incomplete' },
        { id: 'source_diversity', status: 'not_applicable' },
        { id: 'conflict_handling', status: 'incomplete' },
        { id: 'uncertainty_handling', status: 'incomplete' },
        { id: 'plan_completeness', status: 'incomplete' },
      ],
    });
    expect(parseResearchEvaluation(noEvidence, 'exec-1')).toBeDefined();
  });

  it('returns undefined for missing or non-object input', () => {
    expect(parseResearchEvaluation(undefined, 'exec-1')).toBeUndefined();
    expect(parseResearchEvaluation('nope', 'exec-1')).toBeUndefined();
    expect(parseResearchEvaluation([], 'exec-1')).toBeUndefined();
  });
});
