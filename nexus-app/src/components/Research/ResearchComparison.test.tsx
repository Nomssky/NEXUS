/**
 * ResearchComparison rendering tests.
 *
 * Rendered with react-dom/server so assertions cover the actual text, the
 * explicit missing/legacy copy and the absence of any score, percentage or
 * ranking claim.
 */

import React from 'react';
import { describe, it, expect } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { ResearchComparisonResult } from './ResearchComparison';
import { isComparisonEligible } from './comparisonEligibility';
import type {
  DurableExecution,
  ResearchComparison,
  ResearchComparisonCategory,
  ResearchComparisonSide,
} from '../../types/api';

const FINGERPRINT_A = 'a'.repeat(64);
const FINGERPRINT_B = 'b'.repeat(64);
const CATEGORY_ORDER: ResearchComparisonCategory[] = [
  'plan',
  'steps',
  'operations',
  'evidence',
  'synthesis',
  'evaluation',
];

function side(role: 'left' | 'right', executionID: string): ResearchComparisonSide {
  return {
    role,
    execution_id: executionID,
    state: 'completed',
    created_at: '2026-01-02T03:04:05Z',
    completed_at: '2026-01-02T03:05:05Z',
    provenance: {
      evidence: 'authoritative',
      plan: 'authoritative',
      steps: 'authoritative',
      operations: 'authoritative',
      source_attribution: 'derived',
      synthesis: 'derived',
      evaluation: 'derived',
    },
    evidence: {
      version: 3,
      capability: 'web.search',
      operation: 'search',
      count: 2,
      fingerprint: FINGERPRINT_A,
      attribution: 'derived',
      source_count: 2,
      step_count: 2,
      operation_count: 2,
    },
    plan: {
      plan_id: executionID,
      version: 1,
      objective_id: `objective-${executionID}`,
      completion_step_id: 's2',
      completion_recorded: true,
      provenance: 'authoritative',
    },
    synthesis: {
      version: 1,
      method: 'deterministic_v1',
      status: 'complete',
      plan_version: 1,
      plan_version_recorded: true,
      evidence_fingerprint: FINGERPRINT_A,
      fingerprint: FINGERPRINT_B,
      evidence_backed_count: 1,
      inference_count: 1,
      conflict_count: 0,
      question_count: 1,
    },
    evaluation: {
      version: 1,
      method: 'deterministic_v1',
      status: 'warnings',
      evidence_version: 3,
      evidence_count: 2,
      evidence_fingerprint: FINGERPRINT_A,
      plan_version: 1,
      plan_version_recorded: true,
      synthesis_status: 'complete',
      synthesis_fingerprint: FINGERPRINT_B,
      dimension_count: 8,
      finding_count: 1,
    },
  };
}

function comparison(overrides: Partial<ResearchComparison> = {}): ResearchComparison {
  return {
    source: 'durable_journal',
    version: 1,
    method: 'deterministic_v1',
    left: side('left', 'exec-left'),
    right: side('right', 'exec-right'),
    compatibility: {
      status: 'comparable',
      categories: CATEGORY_ORDER.map((category) => ({ category, status: 'comparable' })),
    },
    differences: [],
    findings: [
      {
        code: 'comparison_no_recorded_differences',
        severity: 'info',
        message: 'No recorded differences in comparable fields.',
      },
    ],
    limitations: ['This comparison reports recorded artifact differences only.'],
    truncated: false,
    ...overrides,
  };
}

function render(value: ResearchComparison): string {
  return renderToStaticMarkup(<ResearchComparisonResult comparison={value} />);
}

describe('ResearchComparisonResult', () => {
  it('renders side metadata, compatibility categories and the no-difference caveat', () => {
    const markup = render(comparison());

    expect(markup).toContain('Comparable records');
    expect(markup).toContain('data-comparison-status="comparable"');
    expect(markup).toContain('Left run');
    expect(markup).toContain('Right run');
    expect(markup).toContain('exec-left');
    expect(markup).toContain('Evidence v3');
    expect(markup).toContain('Plan revision 1');
    expect(markup).toContain('Source-to-step attribution recorded as derived');
    expect(markup).toContain('No recorded differences in comparable fields');
    expect(markup).toContain('not proof that the runs are factually equivalent');
    expect(markup).not.toMatch(/%/);
    expect(markup.toLowerCase()).not.toContain('trust score');
    expect(markup.toLowerCase()).not.toContain('confidence score');
    expect(markup.toLowerCase()).not.toContain('better run');
  });

  it('groups recorded differences and shows bounded left/right values', () => {
    const markup = render(
      comparison({
        differences: [
          {
            category: 'plan',
            kind: 'changed',
            entity: 'plan',
            field: 'version',
            left: '1',
            right: '2',
            provenance: 'authoritative',
          },
          {
            category: 'evidence',
            kind: 'removed',
            entity: 'source',
            field: 'record',
            id: 'source-record',
            left: 'Example source',
            provenance: 'derived',
          },
        ],
      })
    );

    expect(markup).toContain('Recorded differences');
    expect(markup).toContain('Plan');
    expect(markup).toContain('Evidence');
    expect(markup).toContain('Left only');
    expect(markup).toContain('Changed');
    expect(markup).toContain('Example source');
    expect(markup).toContain('source-record');
  });

  it('states that missing synthesis and evaluation are not clean artifacts', () => {
    const missing = comparison();
    const left = { ...missing.left };
    delete left.synthesis;
    delete left.evaluation;
    const markup = render(
      comparison({
        left,
        compatibility: {
          status: 'partial',
          categories: CATEGORY_ORDER.map((category) =>
            category === 'synthesis' || category === 'evaluation'
              ? { category, status: 'unavailable' as const }
              : { category, status: 'comparable' as const }
          ),
        },
        findings: [
          {
            code: 'synthesis_missing_on_left',
            severity: 'warning',
            category: 'synthesis',
            message: 'The left synthesis is not recorded.',
          },
          {
            code: 'evaluation_missing_on_left',
            severity: 'warning',
            category: 'evaluation',
            message: 'The left evaluation is not recorded.',
          },
        ],
      })
    );

    expect(markup).toContain('Partial comparison');
    expect(markup).toContain('Not recorded. This is not an empty or clean synthesis.');
    expect(markup).toContain('Not recorded. This is not a clean verdict.');
    expect(markup).toContain('synthesis_missing_on_left');
    expect(markup).toContain('evaluation_missing_on_left');
    expect(markup).not.toContain('Checks passed');
  });

  it('marks legacy evidence and unavailable v3 fields explicitly', () => {
    const legacy = comparison();
    legacy.left = {
      role: 'left',
      execution_id: 'exec-legacy',
      state: 'completed',
      created_at: '2026-01-02T03:04:05Z',
      provenance: {
        evidence: 'legacy',
        plan: 'legacy',
        steps: 'legacy',
        operations: 'legacy',
        source_attribution: 'legacy',
        synthesis: 'unavailable',
        evaluation: 'unavailable',
      },
      evidence: {
        version: 2,
        capability: 'web.search',
        operation: 'search',
        count: 2,
        fingerprint: FINGERPRINT_A,
        source_count: 2,
        step_count: 2,
        operation_count: 1,
      },
      plan: {
        plan_id: 'exec-legacy',
        version: 1,
        completion_recorded: false,
        provenance: 'legacy',
      },
    };
    const markup = render(
      comparison({
        left: legacy.left,
        compatibility: {
          status: 'partial',
          categories: CATEGORY_ORDER.map((category) => ({
            category,
            status: 'partial' as const,
          })),
        },
      })
    );

    expect(markup).toContain('Legacy provenance: missing v3 fields stay unavailable.');
    expect(markup).toContain('legacy provenance');
    expect(markup).toContain('Completion-step provenance unavailable.');
    expect(markup).toContain('Not recorded. This is not an empty or clean synthesis.');
  });

  it('renders findings, fixed limitations and explicit truncation', () => {
    const markup = render(
      comparison({
        truncated: true,
        findings: [
          {
            code: 'source_url_not_identity',
            severity: 'warning',
            category: 'evidence',
            message: 'The same URL does not establish source identity.',
          },
          {
            code: 'comparison_output_truncated',
            severity: 'warning',
            message: 'The comparison was truncated.',
          },
        ],
        limitations: [
          'Sources are paired by recorded fingerprints, not URL.',
          'Missing facts are never inferred.',
        ],
      })
    );

    expect(markup).toContain('The comparison was truncated');
    expect(markup).toContain('source_url_not_identity');
    expect(markup).toContain('The same URL does not establish source identity.');
    expect(markup).toContain('Fixed limitations (2)');
    expect(markup).toContain('Missing facts are never inferred.');
  });
});

describe('isComparisonEligible', () => {
  function execution(overrides: Partial<DurableExecution> = {}): DurableExecution {
    return {
      source: 'durable_journal',
      execution_id: 'exec-1',
      business_id: 'biz-1',
      state: 'completed',
      created_at: '2026-01-02T03:04:05Z',
      updated_at: '2026-01-02T03:05:05Z',
      ...overrides,
    };
  }

  it('admits settled states including recovery-settled unknown', () => {
    expect(isComparisonEligible(execution())).toBe(true);
    expect(isComparisonEligible(execution({ state: 'unknown' }))).toBe(true);
    expect(isComparisonEligible(execution({ state: 'timed_out' }))).toBe(true);
    expect(isComparisonEligible(execution({ state: 'failed', error_class: 'POLICY_DENIED' }))).toBe(true);
  });

  it('rejects in-progress runs and provisional approval pauses', () => {
    expect(isComparisonEligible(execution({ state: 'accepted' }))).toBe(false);
    expect(isComparisonEligible(execution({ state: 'running' }))).toBe(false);
    expect(isComparisonEligible(execution({ state: 'pending_approval' }))).toBe(false);
    expect(
      isComparisonEligible(execution({ state: 'failed', error_class: 'APPROVAL_REQUIRED' }))
    ).toBe(false);
  });
});
