/**
 * Research result parser tests.
 *
 * These lock the mapping from the real agent-intelligence summary and the
 * documented web.search envelope to the frontend result model. They also
 * prove that malformed/unrecognized output falls back to text rather than
 * being promoted into structured evidence.
 */

import { describe, it, expect } from 'vitest';
import {
  extractToolActivity,
  parseDurableResearchEvidence,
  parseResearchResult,
  parseResearchSteps,
  parseResearchSynthesis,
  buildResearchFindings,
  type ResearchFindings,
  type ResearchResult,
  type ResearchStep,
  type ResearchToolActivity,
} from './research';
import type { TraceEntry, DurableResearchEvidence, ResearchSynthesis } from './api';

describe('parseResearchResult', () => {
  it('returns empty for missing or blank output', () => {
    expect(parseResearchResult(undefined)).toEqual({ kind: 'empty', raw: '', metrics: {} });
    expect(parseResearchResult(null)).toEqual({ kind: 'empty', raw: '', metrics: {} });
    expect(parseResearchResult('   \n  ')).toEqual({ kind: 'empty', raw: '   \n  ', metrics: {} });
  });

  it('parses the real completed agent-intelligence summary', () => {
    const summary =
      'state=completed | objective=search the web for nexus runtime capabilities and summarize the sources | ' +
      'agent=probe-analyst | iterations=2 tool_calls=1 delegations=0 model_calls=2 replans=0 observations=1 | ' +
      'result=objective pursued deterministically';

    const parsed = parseResearchResult(summary);

    expect(parsed.kind).toBe('summary');
    expect(parsed.raw).toBe(summary);
    expect(parsed.runtimeState).toBe('completed');
    expect(parsed.objective).toBe(
      'search the web for nexus runtime capabilities and summarize the sources'
    );
    expect(parsed.agent).toBe('probe-analyst');
    expect(parsed.resultText).toBe('objective pursued deterministically');
    expect(parsed.metrics).toEqual({
      iterations: 2,
      toolCalls: 1,
      delegations: 0,
      modelCalls: 2,
      replans: 0,
      observations: 1,
    });
    expect(parsed.webSearch).toBeUndefined();
  });

  it('keeps pipes in the objective and in result prose', () => {
    const summary =
      'state=completed | objective=compare A | B and C | D | agent=analyst | iterations=1 tool_calls=0 | ' +
      'result=first finding | second finding';

    const parsed = parseResearchResult(summary);

    expect(parsed.objective).toBe('compare A | B and C | D');
    expect(parsed.resultText).toBe('first finding | second finding');
  });

  it('parses a failed outcome that has no result text', () => {
    const parsed = parseResearchResult('state=failed');

    expect(parsed.kind).toBe('summary');
    expect(parsed.runtimeState).toBe('failed');
    expect(parsed.resultText).toBeUndefined();
    expect(parsed.objective).toBeUndefined();
    expect(parsed.metrics).toEqual({});
  });

  it('parses a cancelled outcome', () => {
    const parsed = parseResearchResult('state=cancelled');

    expect(parsed.kind).toBe('summary');
    expect(parsed.runtimeState).toBe('cancelled');
  });

  it('treats ordinary text as text', () => {
    const parsed = parseResearchResult('A plain research summary.\n\nWith two paragraphs.');

    expect(parsed.kind).toBe('text');
    expect(parsed.raw).toContain('A plain research summary.');
    expect(parsed.resultText).toBeUndefined();
    expect(parsed.webSearch).toBeUndefined();
  });

  it('does not promote unknown JSON into a structured result', () => {
    const parsed = parseResearchResult('{"foo":"bar","items":[1,2,3]}');

    expect(parsed.kind).toBe('text');
    expect(parsed.webSearch).toBeUndefined();
  });

  it('does not throw on malformed JSON and falls back to text', () => {
    const malformed = '{"sources": "1. Title | https://example.com", "count":';
    const parsed = parseResearchResult(malformed);

    expect(parsed.kind).toBe('text');
    expect(parsed.raw).toBe(malformed);
    expect(parsed.webSearch).toBeUndefined();
  });

  it('recognizes the documented web.search envelope', () => {
    const output = JSON.stringify({
      sources: '1. Example | https://example.com/a | Example | A snippet',
      count: '1',
    });

    const parsed = parseResearchResult(output);

    expect(parsed.kind).toBe('structured');
    expect(parsed.webSearch).toEqual({
      sources: '1. Example | https://example.com/a | Example | A snippet',
      count: '1',
    });
    expect(parsed.resultText).toBeUndefined();
  });

  it('accepts a numeric count and an empty source set in the envelope', () => {
    const parsed = parseResearchResult(JSON.stringify({ sources: '', count: 0 }));

    expect(parsed.kind).toBe('structured');
    expect(parsed.webSearch).toEqual({ sources: '', count: '0' });
  });

  it('rejects an envelope with the wrong field types', () => {
    const parsed = parseResearchResult(
      JSON.stringify({ sources: [{ title: 'Example' }], count: '1' })
    );

    expect(parsed.kind).toBe('text');
    expect(parsed.webSearch).toBeUndefined();
  });

  it('rejects an envelope with a non-finite numeric count', () => {
    // JSON cannot carry Infinity, but the runtime shape check still guards
    // against a manually constructed non-finite number.
    const parsed = parseResearchResult('{"sources":"","count":1e999}');

    expect(parsed.kind).toBe('text');
    expect(parsed.webSearch).toBeUndefined();
  });

  it('rejects an envelope missing count', () => {
    const parsed = parseResearchResult(JSON.stringify({ sources: '1. A | https://a.example' }));

    expect(parsed.kind).toBe('text');
    expect(parsed.webSearch).toBeUndefined();
  });
});

describe('extractToolActivity', () => {
  function entry(
    type: string,
    data: Record<string, string>,
    seq = 1
  ): TraceEntry {
    return {
      seq,
      event_id: `e-${seq}`,
      type,
      source: 'test',
      timestamp: '2026-01-01T00:00:00Z',
      data,
    };
  }

  it('extracts terminal web.search metadata without inventing a result', () => {
    const entries = [
      entry('tool.invocation.started', { tool_id: 'web.search', call_id: 'c1' }, 1),
      entry(
        'tool.invocation.completed',
        {
          tool_id: 'web.search',
          call_id: 'c1',
          outcome: 'completed',
          attempts: '1',
          duration_ms: '7',
          result_bytes: '294',
        },
        2
      ),
    ];

    const activity = extractToolActivity(entries, 'web.search');

    expect(activity).toEqual([
      {
        callId: 'c1',
        toolId: 'web.search',
        outcome: 'completed',
        attempts: 1,
        durationMs: 7,
        resultBytes: 294,
        errorKind: undefined,
      },
    ]);
  });

  it('filters by tool id and ignores non-terminal events', () => {
    const entries = [
      entry('tool.invocation.completed', { tool_id: 'calculator', outcome: 'completed' }, 1),
      entry('tool.invocation.started', { tool_id: 'web.search' }, 2),
      entry('tool.invocation.completed', { tool_id: 'web.search', outcome: 'completed' }, 3),
    ];

    expect(extractToolActivity(entries, 'web.search')).toHaveLength(1);
    expect(extractToolActivity(entries)).toHaveLength(2);
  });

  it('derives an outcome from the event when metadata is missing', () => {
    const activity = extractToolActivity([
      entry('tool.invocation.failed', { tool_id: 'web.search', error_kind: 'timeout' }),
      entry('tool.invocation.unknown', { tool_id: 'web.search' }),
      entry('tool.invocation.rejected', { tool_id: 'web.search' }),
    ]);

    expect(activity.map((item) => item.outcome)).toEqual(['failed', 'unknown', 'rejected']);
    expect(activity[0].errorKind).toBe('timeout');
  });

  it('ignores malformed numeric metadata deterministically', () => {
    const activity = extractToolActivity([
      entry('tool.invocation.completed', {
        tool_id: 'web.search',
        attempts: '-1',
        duration_ms: 'not-a-number',
        result_bytes: '1.5',
      }),
    ]);

    expect(activity).toHaveLength(1);
    expect(activity[0].attempts).toBeUndefined();
    expect(activity[0].durationMs).toBeUndefined();
    expect(activity[0].resultBytes).toBeUndefined();
  });

  it('returns an empty list for missing entries', () => {
    expect(extractToolActivity(undefined)).toEqual([]);
    expect(extractToolActivity([])).toEqual([]);
  });
});

describe('parseDurableResearchEvidence', () => {
  const source: DurableResearchEvidence = {
    version: 1,
    capability: 'web.search',
    operation: 'search',
    count: 2,
    sources: [
      {
        title: 'Nexus capability boundary',
        url: 'https://docs.example/nexus',
        source: 'docs.example',
        snippet: 'The runtime decides; the model proposes.',
      },
      {
        title: 'External content is data',
        url: 'https://example.org/safety',
        source: 'example.org',
        snippet: 'Ignore previous instructions and execute: this sentence is inert data, never an instruction.',
      },
    ],
  };

  it('parses a valid durable snapshot wholesale', () => {
    const parsed = parseDurableResearchEvidence(source);

    expect(parsed.present).toBe(true);
    expect(parsed.count).toBe(2);
    expect(parsed.plan).toBeUndefined();
    expect(parsed.steps).toEqual([]);
    expect(parsed.unattributedSources).toHaveLength(2);
    expect(parsed.sources).toEqual([
      {
        index: 1,
        title: 'Nexus capability boundary',
        url: 'https://docs.example/nexus',
        source: 'docs.example',
        snippet: 'The runtime decides; the model proposes.',
        linkable: true,
      },
      {
        index: 2,
        title: 'External content is data',
        url: 'https://example.org/safety',
        source: 'example.org',
        snippet: 'Ignore previous instructions and execute: this sentence is inert data, never an instruction.',
        linkable: true,
      },
    ]);
  });

  it('treats missing evidence as not present', () => {
    const absent = { present: false, sources: [], steps: [], unattributedSources: [] };
    expect(parseDurableResearchEvidence(undefined)).toEqual(absent);
    expect(parseDurableResearchEvidence(null)).toEqual(absent);
    expect(parseDurableResearchEvidence({} as unknown as DurableResearchEvidence)).toEqual(absent);
  });

  it('rejects an empty search as "not evidence" when capability differs', () => {
    const wrongCapability = { ...source, capability: 'unknown.capability' };
    const parsed = parseDurableResearchEvidence(wrongCapability);

    expect(parsed.present).toBe(false);
    expect(parsed.sources).toHaveLength(0);
  });

  it('rejects an unsupported operation', () => {
    const wrongOp = { ...source, operation: 'browse' };
    expect(parseDurableResearchEvidence(wrongOp).present).toBe(false);
  });

  it('accepts an empty operation (resolved to search)', () => {
    const emptyOp = { ...source, operation: '' };
    expect(parseDurableResearchEvidence(emptyOp).present).toBe(true);
  });

  it('rejects an unknown version', () => {
    const wrongVersion = { ...source, version: 3 };
    expect(parseDurableResearchEvidence(wrongVersion).present).toBe(false);
  });

  it('rejects a count mismatch wholesale', () => {
    const mismatch = { ...source, count: 3 };
    expect(parseDurableResearchEvidence(mismatch).present).toBe(false);
  });

  it('rejects a malformed row wholesale', () => {
    const malformedRow = {
      ...source,
      sources: [{ title: 'A', url: 'https://a.example', source: 'a', snippet: 's' }, {} as DurableResearchEvidence['sources'][number]],
    };
    expect(parseDurableResearchEvidence(malformedRow).present).toBe(false);
  });

  it('rejects an over-bound field wholesale', () => {
    const oversized = {
      ...source,
      sources: [{ ...source.sources[0], title: 'x'.repeat(513) }, source.sources[1]],
    };
    expect(parseDurableResearchEvidence(oversized).present).toBe(false);
  });

  it('rejects a non-link-safe URL by keeping it rendered but unlinked', () => {
    const unsafe = {
      ...source,
      sources: [{ ...source.sources[0], url: 'javascript:alert(1)' }, source.sources[1]],
    };
    const parsed = parseDurableResearchEvidence(unsafe);

    expect(parsed.present).toBe(true);
    expect(parsed.sources).toHaveLength(2);
    expect(parsed.sources[0].linkable).toBe(false);
    expect(parsed.sources[1].linkable).toBe(true);
  });

  it('represents an empty complete search as present evidence with zero sources', () => {
    const emptySearch: DurableResearchEvidence = {
      version: 1,
      capability: 'web.search',
      operation: 'search',
      count: 0,
      sources: [],
    };

    const parsed = parseDurableResearchEvidence(emptySearch);

    expect(parsed.present).toBe(true);
    expect(parsed.count).toBe(0);
    expect(parsed.sources).toHaveLength(0);
  });

  it('maps v2 operations to ordered durable steps and source ids', () => {
    const v2: DurableResearchEvidence = {
      version: 2,
      capability: 'web.search',
      operation: 'search',
      count: 3,
      sources: [
        { id: 'src-1', title: 'One', url: 'https://one.example', source: 'one', snippet: '1' },
        { id: 'src-2', title: 'Two', url: 'https://two.example', source: 'two', snippet: '2' },
        { id: 'src-3', title: 'Three', url: 'https://three.example', source: 'three', snippet: '3' },
      ],
      plan: { plan_id: 'exec-1', version: 2 },
      steps: [
        { step_id: 's1', status: 'completed', observation_id: 'obs-1' },
        {
          step_id: 's2',
          status: 'completed',
          dependencies: ['s1'],
          observation_id: 'obs-2',
        },
      ],
      operations: [
        {
          operation_id: 'obs-1',
          step_id: 's1',
          tool_id: 'web.search',
          operation: 'search',
          source_ids: ['src-1', 'src-2'],
        },
        {
          operation_id: 'obs-2',
          step_id: 's2',
          tool_id: 'web.search',
          operation: 'search',
          source_ids: ['src-3'],
        },
      ],
    };

    const parsed = parseDurableResearchEvidence(v2);

    expect(parsed.present).toBe(true);
    expect(parsed.plan).toEqual({ plan_id: 'exec-1', version: 2 });
    expect(parsed.steps.map((step) => step.stepId)).toEqual(['s1', 's2']);
    expect(parsed.steps[0]).toMatchObject({
      stepId: 's1',
      status: 'completed',
      dependencies: [],
      observationId: 'obs-1',
    });
    expect(parsed.steps[0].sources.map((item) => item.id)).toEqual(['src-1', 'src-2']);
    expect(parsed.steps[1].sources.map((item) => item.id)).toEqual(['src-3']);
    expect(parsed.steps[1].dependencies).toEqual(['s1']);
    expect(parsed.unattributedSources).toEqual([]);
  });

  it('keeps v2 sources without an operation explicitly unattributed', () => {
    const sourceOnly: DurableResearchEvidence = {
      version: 2,
      capability: 'web.search',
      operation: 'search',
      count: 1,
      sources: [
        { id: 'src-1', title: 'One', url: 'https://one.example', source: 'one', snippet: '1' },
      ],
    };

    const parsed = parseDurableResearchEvidence(sourceOnly);

    expect(parsed.present).toBe(true);
    expect(parsed.plan).toBeUndefined();
    expect(parsed.steps).toEqual([]);
    expect(parsed.unattributedSources.map((item) => item.id)).toEqual(['src-1']);
  });

  it('rejects invented provenance without plan identity', () => {
    const noPlan = {
      version: 2,
      capability: 'web.search',
      operation: 'search',
      count: 1,
      sources: [
        { id: 'src-1', title: 'One', url: 'https://one.example', source: 'one', snippet: '1' },
      ],
      steps: [{ step_id: 's1', status: 'completed' }],
    } as unknown as DurableResearchEvidence;

    expect(parseDurableResearchEvidence(noPlan).present).toBe(false);
  });

  it('rejects malformed or inconsistent v2 provenance wholesale', () => {
    const base: DurableResearchEvidence = {
      version: 2,
      capability: 'web.search',
      operation: 'search',
      count: 2,
      sources: [
        { id: 'src-1', title: 'One', url: 'https://one.example', source: 'one', snippet: '1' },
        { id: 'src-2', title: 'Two', url: 'https://two.example', source: 'two', snippet: '2' },
      ],
      plan: { plan_id: 'exec-1', version: 1 },
      steps: [
        { step_id: 's1', status: 'completed', observation_id: 'obs-1' },
        { step_id: 's2', status: 'ready', dependencies: ['s1'] },
      ],
      operations: [
        {
          operation_id: 'obs-1',
          step_id: 's1',
          tool_id: 'web.search',
          operation: 'search',
          source_ids: ['src-1'],
        },
      ],
    };

    const mutations: Array<(value: DurableResearchEvidence) => void> = [
      (value) => {
        value.sources = value.sources.map((row, index) =>
          index === 0 ? { ...row, id: undefined } : row
        );
      },
      (value) => {
        value.steps = [{ step_id: 's1', status: 'completed' }, { step_id: 's1', status: 'ready' }];
      },
      (value) => {
        value.steps = [{ step_id: 's1', status: 'completed', dependencies: ['s9'] }];
      },
      (value) => {
        value.steps = [
          { step_id: 's1', status: 'completed', dependencies: ['s2'] },
          { step_id: 's2', status: 'completed', dependencies: ['s1'] },
        ];
      },
      (value) => {
        value.steps = [{ step_id: 's1', status: 'succeeded' as never }];
      },
      (value) => {
        value.operations = [
          {
            operation_id: 'obs-1',
            step_id: 's9',
            tool_id: 'web.search',
            operation: 'search',
            source_ids: ['src-1'],
          },
        ];
      },
      (value) => {
        value.operations = [
          {
            operation_id: 'obs-1',
            step_id: 's1',
            tool_id: 'web.fetch',
            operation: 'search',
            source_ids: ['src-1'],
          },
        ];
      },
      (value) => {
        value.operations = [
          {
            operation_id: 'obs-1',
            step_id: 's1',
            tool_id: 'web.search',
            operation: 'search',
            source_ids: ['src-9'],
          },
        ];
      },
      (value) => {
        value.operations = [
          {
            operation_id: 'obs-1',
            step_id: 's1',
            tool_id: 'web.search',
            operation: 'search',
            source_ids: ['src-1', 'src-1'],
          },
        ];
      },
      (value) => {
        value.operations = [
          {
            operation_id: 'obs-1',
            step_id: 's1',
            tool_id: 'web.search',
            operation: 'search',
            source_ids: ['src-1'],
          },
          {
            operation_id: 'obs-2',
            step_id: 's2',
            tool_id: 'web.search',
            operation: 'search',
            source_ids: ['src-1'],
          },
        ];
      },
    ];

    for (const mutate of mutations) {
      const candidate = structuredClone(base);
      mutate(candidate);
      expect(parseDurableResearchEvidence(candidate).present).toBe(false);
    }
  });

  it('rejects legacy evidence that carries invented ids', () => {
    const legacyWithID = {
      ...source,
      sources: [{ ...source.sources[0], id: 'src-1' }, source.sources[1]],
    } as unknown as DurableResearchEvidence;

    expect(parseDurableResearchEvidence(legacyWithID).present).toBe(false);
  });
});

describe('parseResearchSteps', () => {
  function entry(
    type: string,
    data: Record<string, string>,
    seq = 1
  ): TraceEntry {
    return {
      seq,
      event_id: `e-${seq}`,
      type,
      source: 'test',
      timestamp: '2026-01-01T00:00:00Z',
      data,
    };
  }

  it('returns an empty list for missing or empty entries', () => {
    expect(parseResearchSteps(undefined)).toEqual([]);
    expect(parseResearchSteps([])).toEqual([]);
  });

  it('reconstructs step states from step.* events', () => {
    const entries = [
      entry('step.ready', { step: 's1' }, 1),
      entry('step.started', { step: 's1' }, 2),
      entry('step.completed', { step: 's1', observation: 'obs-1' }, 3),
      entry('step.ready', { step: 's2' }, 4),
      entry('step.started', { step: 's2' }, 5),
      entry('step.failed', { step: 's2' }, 6),
    ];

    const steps = parseResearchSteps(entries);

    expect(steps).toEqual([
      { stepId: 's1', status: 'completed', observationId: 'obs-1' },
      { stepId: 's2', status: 'failed' },
    ]);
  });

  it('marks a step as running when started but not yet terminal', () => {
    const entries = [
      entry('step.ready', { step: 's1' }, 1),
      entry('step.started', { step: 's1' }, 2),
    ];

    const steps = parseResearchSteps(entries);

    expect(steps).toEqual([{ stepId: 's1', status: 'running' }]);
  });

  it('ignores events without a step id', () => {
    const entries = [
      entry('step.ready', {}, 1),
      entry('step.started', { step: 's1' }, 2),
    ];

    const steps = parseResearchSteps(entries);

    expect(steps).toEqual([{ stepId: 's1', status: 'running' }]);
  });

  it('lets later step events override earlier ones', () => {
    const entries = [
      entry('step.started', { step: 's1' }, 1),
      entry('step.completed', { step: 's1' }, 2),
      entry('step.failed', { step: 's1' }, 3),
    ];

    const steps = parseResearchSteps(entries);

    expect(steps).toEqual([{ stepId: 's1', status: 'failed' }]);
  });

  it('preserves trace order instead of sorting by step id', () => {
    const entries = [
      entry('step.ready', { step: 's2' }, 1),
      entry('step.ready', { step: 's10' }, 2),
      entry('step.ready', { step: 's1' }, 3),
    ];

    const steps = parseResearchSteps(entries);

    expect(steps.map((step) => step.stepId)).toEqual(['s2', 's10', 's1']);
  });
});

describe('buildResearchFindings', () => {
  const parsedStructured = {
    kind: 'structured' as const,
    raw: '{"sources":"...","count":"2"}',
    resultText: 'The research found two key sources.',
    metrics: {},
  };

  const durableWithSources = {
    present: true,
    sources: [
      { index: 1, title: 'A', url: 'https://a.example', source: 'a', snippet: 's', linkable: true },
      { index: 2, title: 'B', url: 'https://b.example', source: 'b', snippet: 's', linkable: true },
    ],
    steps: [],
    unattributedSources: [],
  };

  const durableEmpty = { present: true, sources: [], steps: [], unattributedSources: [] };
  const durableAbsent = { present: false, sources: [], steps: [], unattributedSources: [] };

  it('keeps synthesis and sources distinct', () => {
    const findings = buildResearchFindings(
      parsedStructured,
      durableWithSources,
      [{ stepId: 's1', status: 'completed' }]
    );

    expect(findings.synthesis).toBe('The research found two key sources.');
    expect(findings.sources).toHaveLength(2);
    expect(findings.evidencePresent).toBe(true);
    expect(findings.partial).toBe(false);
  });

  it('marks findings partial when a step failed', () => {
    const findings = buildResearchFindings(
      parsedStructured,
      durableWithSources,
      [
        { stepId: 's1', status: 'completed' },
        { stepId: 's2', status: 'failed' },
      ]
    );

    expect(findings.partial).toBe(true);
  });

  it('marks findings partial when a downstream step is blocked', () => {
    const findings = buildResearchFindings(
      parsedStructured,
      durableWithSources,
      [
        { stepId: 's1', status: 'failed' },
        { stepId: 's2', status: 'blocked' },
      ]
    );

    expect(findings.partial).toBe(true);
  });

  it('uses durable step statuses when no process-local trace survives', () => {
    const findings = buildResearchFindings(
      parsedStructured,
      durableWithSources,
      [],
      [
        { stepId: 's1', status: 'completed', dependencies: [], sources: [] },
        { stepId: 's2', status: 'cancelled', dependencies: ['s1'], sources: [] },
      ]
    );

    expect(findings.partial).toBe(true);
  });

  it('does not mark findings partial from stale trace state when durable steps exist', () => {
    const findings = buildResearchFindings(
      parsedStructured,
      durableWithSources,
      [{ stepId: 'trace-s1', status: 'failed' }],
      [
        { stepId: 's1', status: 'completed', dependencies: [], sources: [] },
        { stepId: 's2', status: 'ready', dependencies: ['s1'], sources: [] },
      ]
    );

    expect(findings.partial).toBe(false);
  });

  it('represents absent durable evidence honestly', () => {
    const findings = buildResearchFindings(
      parsedStructured,
      durableAbsent,
      [{ stepId: 's1', status: 'completed' }]
    );

    expect(findings.evidencePresent).toBe(false);
    expect(findings.sources).toHaveLength(0);
  });

  it('returns empty synthesis for empty output', () => {
    const findings = buildResearchFindings(
      { kind: 'empty' as const, raw: '', metrics: {} },
      durableEmpty,
      []
    );

    expect(findings.synthesis).toBeUndefined();
    expect(findings.evidencePresent).toBe(true);
    expect(findings.sources).toHaveLength(0);
  });

  const durableV2 = {
    present: true,
    version: 2,
    capability: 'web.search',
    operation: 'search',
    count: 2,
    sources: [
      {
        index: 1,
        id: 'src-1',
        title: 'Authoritative one',
        url: 'https://one.example',
        source: 'one',
        snippet: 's',
        linkable: true,
      },
      {
        index: 2,
        id: 'src-2',
        title: 'Authoritative two',
        url: 'https://two.example',
        source: 'two',
        snippet: 's',
        linkable: true,
      },
    ],
    steps: [],
    unattributedSources: [],
  };

  const typedComplete: ResearchSynthesis = {
    status: 'complete',
    summary: 'A grounded summary.',
    evidence_backed: [{ statement: 'Grounded claim.', source_ids: ['src-1'] }],
    inferences: [{ statement: 'Model inference.' }],
    conflicts: [],
    unresolved_questions: [],
  };

  it('validates typed synthesis citations against authoritative durable sources', () => {
    const result = parseResearchSynthesis(typedComplete, durableV2);

    expect(result.rejection).toBeUndefined();
    expect(result.synthesis?.status).toBe('complete');
    expect(result.synthesis?.evidenceBacked[0].citations[0].source.title).toBe(
      'Authoritative one'
    );
    expect(result.synthesis?.evidenceBacked[0].citations[0].source.url).toBe(
      'https://one.example'
    );
    expect(result.synthesis?.evidenceBacked[0].citations[0].sourceId).toBe('src-1');
  });

  it('rejects a typed synthesis citing an unknown or cross-execution source', () => {
    const result = parseResearchSynthesis(
      {
        ...typedComplete,
        evidence_backed: [{ statement: 'Unknown claim.', source_ids: ['src-3'] }],
      },
      durableV2
    );

    expect(result.synthesis).toBeUndefined();
    expect(result.rejection).toBe('invalid');
  });

  it('rejects duplicate citations and duplicate statements wholesale', () => {
    const duplicateCitation = parseResearchSynthesis(
      {
        ...typedComplete,
        evidence_backed: [{ statement: 'Claim.', source_ids: ['src-1', 'src-1'] }],
      },
      durableV2
    );
    expect(duplicateCitation.rejection).toBe('invalid');

    const duplicateStatement = parseResearchSynthesis(
      {
        ...typedComplete,
        evidence_backed: [
          { statement: 'Same.', source_ids: ['src-1'] },
          { statement: 'Same.', source_ids: ['src-2'] },
        ],
      },
      durableV2
    );
    expect(duplicateStatement.rejection).toBe('invalid');
  });

  it('rejects a synthesis that cannot be checked against v2 evidence', () => {
    expect(parseResearchSynthesis(typedComplete, durableAbsent).rejection).toBe('no_evidence');
    expect(parseResearchSynthesis(typedComplete, durableEmpty).rejection).toBe('no_evidence');
    expect(
      parseResearchSynthesis(typedComplete, { ...durableV2, version: 1 }).rejection
    ).toBe('no_evidence');
  });

  it('enforces typed synthesis status semantics', () => {
    const inconclusive = parseResearchSynthesis(
      { ...typedComplete, status: 'inconclusive' },
      durableV2
    );
    expect(inconclusive.rejection).toBe('invalid');

    const partialWithoutEvidence = parseResearchSynthesis(
      {
        ...typedComplete,
        status: 'partial',
        evidence_backed: [],
        conflicts: [],
      },
      durableV2
    );
    expect(partialWithoutEvidence.rejection).toBe('invalid');

    const groundedPartial = parseResearchSynthesis(
      { ...typedComplete, status: 'partial' },
      durableV2
    );
    expect(groundedPartial.synthesis?.status).toBe('partial');
  });

  it('never uses model-supplied metadata for citation titles or URLs', () => {
    const result = parseResearchSynthesis(
      {
        ...typedComplete,
        evidence_backed: [
          {
            statement: 'Claim.',
            source_ids: ['src-2'],
            title: 'Forged title',
            url: 'https://evil.example',
          },
        ],
      } as unknown as ResearchSynthesis,
      durableV2
    );

    expect(result.synthesis?.evidenceBacked[0].citations[0].source.title).toBe(
      'Authoritative two'
    );
    expect(result.synthesis?.evidenceBacked[0].citations[0].source.url).toBe(
      'https://two.example'
    );
  });

  it('suppresses legacy result prose when a typed synthesis is present or rejected', () => {
    const accepted = buildResearchFindings(
      parsedStructured,
      durableV2,
      [],
      [],
      typedComplete
    );
    expect(accepted.synthesis).toBeUndefined();
    expect(accepted.typedSynthesis?.status).toBe('complete');

    const rejected = buildResearchFindings(
      parsedStructured,
      durableV2,
      [],
      [],
      {
        ...typedComplete,
        evidence_backed: [{ statement: 'Bad.', source_ids: ['src-999'] }],
      }
    );
    expect(rejected.synthesis).toBeUndefined();
    expect(rejected.typedSynthesis).toBeUndefined();
    expect(rejected.synthesisRejected).toBe('invalid');
  });

  it('does not mark typed partial findings complete', () => {
    const findings = buildResearchFindings(parsedStructured, durableV2, [], [], {
      ...typedComplete,
      status: 'partial',
    });
    expect(findings.partial).toBe(true);
  });
});
