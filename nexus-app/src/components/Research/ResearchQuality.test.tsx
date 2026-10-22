/**
 * ResearchQuality rendering tests.
 *
 * Rendered with react-dom/server so the assertions cover the real text
 * escaping, the verdict wording and the absence of any trust percentage.
 */

import React from 'react';
import { describe, it, expect } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { ResearchQuality } from './ResearchQuality';
import type { ResearchEvaluation } from '../../types/api';

const DIMENSIONS: ResearchEvaluation['dimensions'] = [
  { id: 'evidence_coverage', status: 'pass' },
  { id: 'citation_integrity', status: 'pass' },
  { id: 'relevance', status: 'pass' },
  { id: 'source_quality', status: 'pass' },
  { id: 'source_diversity', status: 'pass' },
  { id: 'conflict_handling', status: 'not_measurable' },
  { id: 'uncertainty_handling', status: 'pass' },
  { id: 'plan_completeness', status: 'pass' },
];

function evaluation(overrides: Partial<ResearchEvaluation> = {}): ResearchEvaluation {
  return {
    version: 1,
    method: 'deterministic_v1',
    execution_id: 'exec-1',
    evidence_version: 2,
    evidence_count: 1,
    evidence_fingerprint: 'a'.repeat(64),
    synthesis_status: 'complete',
    synthesis_fingerprint: 'b'.repeat(64),
    status: 'passed',
    dimensions: DIMENSIONS,
    findings: [],
    limitations: ['Citation integrity proves reference resolution only.'],
    ...overrides,
  };
}

function render(props: React.ComponentProps<typeof ResearchQuality>): string {
  return renderToStaticMarkup(<ResearchQuality {...props} />);
}

describe('ResearchQuality', () => {
  it('renders a passed verdict with the dimension states and the limitations', () => {
    const markup = render({ evaluation: evaluation() });

    expect(markup).toContain('Quality checks');
    expect(markup).toContain('Checks passed');
    expect(markup).toContain('data-evaluation-status="passed"');
    expect(markup).toContain('Citation integrity');
    expect(markup).toContain('Not measured');
    expect(markup).toContain('What these checks do not verify');
    expect(markup).toContain('Citation integrity proves reference resolution only.');
    expect(markup).not.toMatch(/%/);
  });

  it('never shows a green summary for an integrity failure', () => {
    const markup = render({
      evaluation: evaluation({
        status: 'failed',
        dimensions: DIMENSIONS.map((dimension) =>
          dimension.id === 'citation_integrity' ? { ...dimension, status: 'fail' as const } : dimension
        ),
        findings: [
          {
            code: 'citation_unknown_source',
            dimension: 'citation_integrity',
            severity: 'error',
            message: 'Statement 1 cites a source not present in this execution.',
          },
        ],
      }),
    });

    expect(markup).toContain('Integrity failure');
    expect(markup).toContain('data-evaluation-status="failed"');
    expect(markup).toContain('Deterministic integrity checks failed');
    expect(markup).toContain('citation_unknown_source');
    expect(markup).not.toContain('Checks passed');
  });

  it('marks an incomplete evaluation as unavailable rather than passed', () => {
    const markup = render({
      evaluation: evaluation({
        status: 'incomplete',
        dimensions: DIMENSIONS.map((dimension) =>
          dimension.id === 'evidence_coverage'
            ? { ...dimension, status: 'incomplete' as const }
            : dimension
        ),
        findings: [
          {
            code: 'evidence_unavailable',
            dimension: 'evidence_coverage',
            severity: 'warning',
            message: 'No complete evidence snapshot was available for evaluation.',
          },
        ],
      }),
    });

    expect(markup).toContain('Incomplete');
    expect(markup).not.toContain('Checks passed');
  });

  it('states that a recorded verdict was read back from the durable record', () => {
    const markup = render({ evaluation: evaluation(), recorded: true });

    expect(markup).toContain('Recorded when this execution completed');
    expect(markup).toContain('The typed synthesis is not available for this record');
  });

  it('states that the shown synthesis is the same persisted snapshot', () => {
    const markup = render({
      evaluation: evaluation(),
      recorded: true,
      synthesisAvailable: true,
    });

    expect(markup).toContain('Recorded when this execution completed');
    expect(markup).toContain('the same persisted snapshot');
    expect(markup).toContain('it is not regenerated');
  });

  it('does not claim a synthesis is missing while the verdict is still live', () => {
    const markup = render({ evaluation: evaluation() });

    expect(markup).not.toContain('Recorded when this execution completed');
  });
});
