/**
 * Research result parser tests.
 *
 * These lock the mapping from the real agent-intelligence summary and the
 * documented web.search envelope to the frontend result model. They also
 * prove that malformed/unrecognized output falls back to text rather than
 * being promoted into structured evidence.
 */

import { describe, it, expect } from 'vitest';
import { extractToolActivity, parseResearchResult } from './research';
import type { TraceEntry } from './api';

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
