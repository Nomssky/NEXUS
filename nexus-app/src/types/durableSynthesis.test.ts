import { describe, it, expect } from 'vitest';
import { parseDurableResearchSynthesis } from './research';
import type { DurableResearchSynthesis } from './api';
import type { ParsedDurableEvidence } from './research';

const DURABLE_EVIDENCE: ParsedDurableEvidence = {
  present: true,
  version: 2,
  capability: 'web.search',
  operation: 'search',
  count: 1,
  sources: [
    {
      index: 1,
      id: 'src-1',
      title: 'Nexus capability boundary',
      url: 'https://docs.example/nexus',
      source: 'docs.example',
      snippet: 'The runtime decides; the model proposes.',
      linkable: true,
    },
  ],
  steps: [],
  unattributedSources: [],
  plan: { plan_id: 'exec-1', version: 1 },
};

function synthesis(overrides: Partial<DurableResearchSynthesis> = {}): DurableResearchSynthesis {
  return {
    version: 1,
    method: 'deterministic_v1',
    execution_id: 'exec-1',
    status: 'complete',
    summary: 'Synthesized from the captured source.',
    evidence_backed: [
      { statement: 'The runtime decides and the model proposes.', source_ids: ['src-1'] },
    ],
    inferences: [],
    conflicts: [],
    unresolved_questions: [],
    evidence_fingerprint: 'a'.repeat(64),
    plan_version: 0,
    ...overrides,
  } as DurableResearchSynthesis;
}

describe('parseDurableResearchSynthesis', () => {
  it('parses a valid durable synthesis and resolves citations', () => {
    const parsed = parseDurableResearchSynthesis(synthesis(), DURABLE_EVIDENCE, 'exec-1');
    expect(parsed.rejection).toBeUndefined();
    expect(parsed.synthesis).toBeDefined();
    expect(parsed.synthesis?.status).toBe('complete');
    expect(parsed.synthesis?.summary).toBe('Synthesized from the captured source.');
    expect(parsed.synthesis?.evidenceBacked).toHaveLength(1);
    expect(parsed.synthesis?.evidenceBacked[0].statement).toBe(
      'The runtime decides and the model proposes.',
    );
    expect(parsed.synthesis?.evidenceBacked[0].citations).toHaveLength(1);
    expect(parsed.synthesis?.evidenceBacked[0].citations[0].sourceId).toBe('src-1');
    expect(parsed.synthesis?.evidenceBacked[0].citations[0].source.title).toBe(
      'Nexus capability boundary',
    );
  });

  it('rejects synthesis bound to another execution', () => {
    const parsed = parseDurableResearchSynthesis(synthesis(), DURABLE_EVIDENCE, 'exec-2');
    expect(parsed.rejection).toBe('invalid');
  });

  it('rejects unsupported version or method', () => {
    expect(
      parseDurableResearchSynthesis(
        { ...synthesis(), version: 2 } as unknown as DurableResearchSynthesis,
        DURABLE_EVIDENCE,
        'exec-1',
      ).rejection,
    ).toBe('invalid');
    expect(
      parseDurableResearchSynthesis(
        { ...synthesis(), method: 'model_v1' } as unknown as DurableResearchSynthesis,
        DURABLE_EVIDENCE,
        'exec-1',
      ).rejection,
    ).toBe('invalid');
  });

  it('rejects unknown status', () => {
    const parsed = parseDurableResearchSynthesis(
      { ...synthesis(), status: 'made_up' as any },
      DURABLE_EVIDENCE,
      'exec-1',
    );
    expect(parsed.rejection).toBe('invalid');
  });

  it('rejects inconclusive synthesis that carries claims', () => {
    const parsed = parseDurableResearchSynthesis(
      {
        ...synthesis(),
        status: 'inconclusive',
        evidence_backed: [
          { statement: 'a claim', source_ids: ['src-1'] },
        ],
      },
      DURABLE_EVIDENCE,
      'exec-1',
    );
    expect(parsed.rejection).toBe('invalid');
  });

  it('rejects complete synthesis with no evidence-backed claims', () => {
    const parsed = parseDurableResearchSynthesis(
      { ...synthesis(), status: 'complete', evidence_backed: [] },
      DURABLE_EVIDENCE,
      'exec-1',
    );
    expect(parsed.rejection).toBe('invalid');
  });

  it('rejects unknown citation', () => {
    const parsed = parseDurableResearchSynthesis(
      {
        ...synthesis(),
        evidence_backed: [{ statement: 'claim', source_ids: ['src-9'] }],
      },
      DURABLE_EVIDENCE,
      'exec-1',
    );
    expect(parsed.rejection).toBe('invalid');
  });

  it('rejects duplicate statement', () => {
    const statement = 'The runtime decides and the model proposes.';
    const parsed = parseDurableResearchSynthesis(
      {
        ...synthesis(),
        evidence_backed: [
          { statement, source_ids: ['src-1'] },
          { statement, source_ids: ['src-1'] },
        ],
      },
      DURABLE_EVIDENCE,
      'exec-1',
    );
    expect(parsed.rejection).toBe('invalid');
  });

  it('rejects malformed evidence fingerprint', () => {
    const parsed = parseDurableResearchSynthesis(
      { ...synthesis(), evidence_fingerprint: 'not-a-fingerprint' },
      DURABLE_EVIDENCE,
      'exec-1',
    );
    expect(parsed.rejection).toBe('invalid');
  });

  it('reads a v59 synthesis without a plan revision as unavailable', () => {
    const legacy = { ...synthesis() };
    delete legacy.plan_version;
    const parsed = parseDurableResearchSynthesis(legacy, DURABLE_EVIDENCE, 'exec-1');
    expect(parsed.rejection).toBeUndefined();
    expect(parsed.synthesis?.planVersion).toBe(0);
  });

  it('accepts a matching plan revision on v2 evidence', () => {
    const parsed = parseDurableResearchSynthesis(
      synthesis({ plan_version: 1 }),
      DURABLE_EVIDENCE,
      'exec-1',
    );
    expect(parsed.rejection).toBeUndefined();
    expect(parsed.synthesis?.planVersion).toBe(1);
  });

  it('rejects a synthesis bound to another plan revision', () => {
    const parsed = parseDurableResearchSynthesis(
      synthesis({ plan_version: 2 }),
      DURABLE_EVIDENCE,
      'exec-1',
    );
    expect(parsed.rejection).toBe('invalid');
  });

  it('requires a plan revision on v3 plan evidence', () => {
    const v3Evidence: ParsedDurableEvidence = {
      ...DURABLE_EVIDENCE,
      version: 3,
      steps: [
        {
          stepId: 's1',
          intent: 'Gather sources.',
          planVersion: 1,
          status: 'completed',
          dependencies: [],
          sources: [],
          authoritative: true,
        },
      ],
      plan: { plan_id: 'exec-1', version: 1, provenance: 'authoritative' },
      attribution: 'unavailable',
    };
    expect(
      parseDurableResearchSynthesis(synthesis(), v3Evidence, 'exec-1').rejection,
    ).toBe('invalid');
    const parsed = parseDurableResearchSynthesis(
      synthesis({ plan_version: 1 }),
      v3Evidence,
      'exec-1',
    );
    expect(parsed.rejection).toBeUndefined();
    expect(parsed.synthesis?.planVersion).toBe(1);
  });

  it('rejects a plan revision without an evidence plan', () => {
    const parsed = parseDurableResearchSynthesis(
      synthesis({ plan_version: 1 }),
      { ...DURABLE_EVIDENCE, plan: undefined },
      'exec-1',
    );
    expect(parsed.rejection).toBe('invalid');
  });

  it('returns empty for absent synthesis', () => {
    const parsed = parseDurableResearchSynthesis(undefined, DURABLE_EVIDENCE, 'exec-1');
    expect(parsed.rejection).toBeUndefined();
    expect(parsed.synthesis).toBeUndefined();
  });
});
