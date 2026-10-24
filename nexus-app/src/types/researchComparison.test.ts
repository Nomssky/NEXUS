import { describe, it, expect } from 'vitest';
import { parseResearchComparison } from './researchComparison';
import type {
  ResearchComparison,
  ResearchComparisonCategory,
  ResearchComparisonSide,
} from './api';

const CATEGORY_ORDER: ResearchComparisonCategory[] = [
  'plan',
  'steps',
  'operations',
  'evidence',
  'synthesis',
  'evaluation',
];

const FINGERPRINT_A = 'a'.repeat(64);
const FINGERPRINT_B = 'b'.repeat(64);
const FINGERPRINT_C = 'c'.repeat(64);

function side(role: 'left' | 'right', executionID: string): ResearchComparisonSide {
  return {
    role,
    execution_id: executionID,
    division_id: 'div-a',
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

function validComparison(): ResearchComparison {
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
  };
}

function clone(): ResearchComparison {
  return JSON.parse(JSON.stringify(validComparison())) as ResearchComparison;
}

describe('parseResearchComparison', () => {
  it('accepts a well-formed comparison and preserves both sides', () => {
    const parsed = parseResearchComparison(validComparison());
    expect(parsed).toBeDefined();
    expect(parsed?.left.role).toBe('left');
    expect(parsed?.right.role).toBe('right');
    expect(parsed?.left.execution_id).toBe('exec-left');
    expect(parsed?.left.plan?.plan_id).toBe('exec-left');
    expect(parsed?.left.evidence?.version).toBe(3);
    expect(parsed?.compatibility.status).toBe('comparable');
  });

  it('rejects unknown versions, methods and malformed source metadata', () => {
    expect(parseResearchComparison({ ...validComparison(), version: 2 })).toBeUndefined();
    expect(
      parseResearchComparison({ ...validComparison(), method: 'semantic_v1' })
    ).toBeUndefined();
    expect(parseResearchComparison({ ...validComparison(), source: '' })).toBeUndefined();
  });

  it('rejects non-canonical compatibility category order or a mismatched overall status', () => {
    const reversed = clone();
    reversed.compatibility.categories = [...reversed.compatibility.categories].reverse();
    expect(parseResearchComparison(reversed)).toBeUndefined();

    const mismatched = clone();
    mismatched.compatibility.categories[3] = { category: 'evidence', status: 'partial' };
    expect(parseResearchComparison(mismatched)).toBeUndefined();
  });

  it('rejects a difference that is not in canonical order', () => {
    const reversed = clone();
    reversed.differences = [
      {
        category: 'evidence',
        kind: 'changed',
        entity: 'evidence',
        field: 'count',
        left: '1',
        right: '2',
        provenance: 'authoritative',
      },
      {
        category: 'plan',
        kind: 'changed',
        entity: 'plan',
        field: 'version',
        left: '1',
        right: '2',
        provenance: 'authoritative',
      },
    ];
    expect(parseResearchComparison(reversed)).toBeUndefined();
  });

  it('rejects malformed fingerprints, plan bindings and evidence bindings', () => {
    const badFingerprint = clone();
    badFingerprint.left.synthesis!.evidence_fingerprint = 'not-a-fingerprint';
    expect(parseResearchComparison(badFingerprint)).toBeUndefined();

    const badPlan = clone();
    badPlan.left.plan!.plan_id = 'another-execution';
    expect(parseResearchComparison(badPlan)).toBeUndefined();

    const badCompletion = clone();
    badCompletion.left.plan!.completion_recorded = false;
    expect(parseResearchComparison(badCompletion)).toBeUndefined();

    const badEvaluation = clone();
    badEvaluation.left.evaluation!.evidence_count = 1;
    expect(parseResearchComparison(badEvaluation)).toBeUndefined();
  });

  it('rejects over-bound collections and display text', () => {
    const tooMany = clone();
    tooMany.differences = Array.from({ length: 513 }, () => ({
      category: 'plan' as const,
      kind: 'changed' as const,
      entity: 'plan',
      field: 'version',
      left: '1',
      right: '2',
    }));
    expect(parseResearchComparison(tooMany)).toBeUndefined();

    const tooLong = clone();
    tooLong.differences = [
      {
        category: 'plan',
        kind: 'changed',
        entity: 'plan',
        field: 'version',
        left: 'x'.repeat(513),
        right: '2',
      },
    ];
    expect(parseResearchComparison(tooLong)).toBeUndefined();

    const control = clone();
    control.differences = [
      {
        category: 'plan',
        kind: 'changed',
        entity: 'plan',
        field: 'version',
        left: 'line\nbreak',
        right: '2',
      },
    ];
    expect(parseResearchComparison(control)).toBeUndefined();
  });

  it('rejects an inconsistent truncation flag', () => {
    const truncated = clone();
    truncated.truncated = true;
    expect(parseResearchComparison(truncated)).toBeUndefined();

    truncated.findings.push({
      code: 'comparison_output_truncated',
      severity: 'warning',
      message: 'The comparison was truncated.',
    });
    expect(parseResearchComparison(truncated)).toBeDefined();
  });

  it('preserves legacy evidence without inventing plan, synthesis or evaluation facts', () => {
    const legacy = clone();
    legacy.left = {
      role: 'left',
      execution_id: 'exec-legacy',
      state: 'completed',
      created_at: '2026-01-02T03:04:05Z',
      provenance: {
        evidence: 'legacy',
        plan: 'unavailable',
        steps: 'unavailable',
        operations: 'unavailable',
        source_attribution: 'unavailable',
        synthesis: 'unavailable',
        evaluation: 'unavailable',
      },
      evidence: {
        version: 1,
        capability: 'web.search',
        operation: 'search',
        count: 2,
        fingerprint: FINGERPRINT_C,
        source_count: 2,
        step_count: 0,
        operation_count: 0,
      },
    };
    legacy.compatibility = {
      status: 'partial',
      categories: [
        { category: 'plan', status: 'unavailable' },
        { category: 'steps', status: 'unavailable' },
        { category: 'operations', status: 'unavailable' },
        { category: 'evidence', status: 'partial' },
        { category: 'synthesis', status: 'partial' },
        { category: 'evaluation', status: 'partial' },
      ],
    };
    legacy.findings = [
      {
        code: 'legacy_evidence_no_provenance',
        severity: 'warning',
        category: 'evidence',
        message: 'The left evidence is legacy and has no plan provenance.',
      },
    ];
    const parsed = parseResearchComparison(legacy);
    expect(parsed).toBeDefined();
    expect(parsed?.left.evidence?.version).toBe(1);
    expect(parsed?.left.plan).toBeUndefined();
    expect(parsed?.left.synthesis).toBeUndefined();
    expect(parsed?.left.evaluation).toBeUndefined();
  });

  it('normalizes an omitted legacy plan revision to zero', () => {
    const legacy = clone();
    legacy.left.evidence = {
      version: 2,
      capability: 'web.search',
      operation: 'search',
      count: 2,
      fingerprint: FINGERPRINT_A,
      source_count: 2,
      step_count: 2,
      operation_count: 2,
    };
    legacy.left.plan = {
      plan_id: 'exec-left',
      version: 1,
      completion_recorded: false,
      provenance: 'legacy',
    };
    legacy.left.synthesis!.plan_version_recorded = false;
    delete (legacy.left.synthesis as { plan_version?: number }).plan_version;
    legacy.left.evaluation!.evidence_version = 2;
    legacy.left.evaluation!.plan_version_recorded = false;
    delete (legacy.left.evaluation as { plan_version?: number }).plan_version;

    const parsed = parseResearchComparison(legacy);
    expect(parsed).toBeDefined();
    expect(parsed?.left.synthesis?.plan_version).toBe(0);
    expect(parsed?.left.evaluation?.plan_version).toBe(0);
  });

  it('preserves missing synthesis/evaluation metadata as absent', () => {
    const missing = clone();
    delete missing.left.synthesis;
    delete missing.left.evaluation;
    missing.findings = [
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
    ];
    const parsed = parseResearchComparison(missing);
    expect(parsed).toBeDefined();
    expect(parsed?.left.synthesis).toBeUndefined();
    expect(parsed?.left.evaluation).toBeUndefined();
  });

  it('returns undefined for missing or non-object input', () => {
    expect(parseResearchComparison(undefined)).toBeUndefined();
    expect(parseResearchComparison(null)).toBeUndefined();
    expect(parseResearchComparison([])).toBeUndefined();
    expect(parseResearchComparison('nope')).toBeUndefined();
  });
});
