/**
 * ResearchOutput rendering tests.
 *
 * Rendered with react-dom/server so the assertions cover real text escaping,
 * list formatting, and the absence of linkification without a DOM test stack.
 */

import React from 'react';
import { describe, it, expect } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { ResearchOutput } from './ResearchOutput';

const COMPLETED_SUMMARY =
  'state=completed | objective=search the web for nexus runtime capabilities | agent=probe-analyst | ' +
  'iterations=2 tool_calls=1 delegations=0 model_calls=2 replans=0 observations=1 | ' +
  'result=objective pursued deterministically';

function render(props: React.ComponentProps<typeof ResearchOutput>): string {
  return renderToStaticMarkup(<ResearchOutput {...props} />);
}

describe('ResearchOutput', () => {
  it('renders the parsed result, grounded metrics, and raw summary', () => {
    const markup = render({ output: COMPLETED_SUMMARY, status: 'completed', durationMs: 50 });

    expect(markup).toContain('Research result');
    expect(markup).toContain('objective pursued deterministically');
    expect(markup).toContain('search the web for nexus runtime capabilities');
    expect(markup).toContain('probe-analyst');
    expect(markup).toContain('50 ms');
    expect(markup).toContain('Tool calls');
    expect(markup).toContain('<dd>1</dd>');
    expect(markup).toContain('Execution summary');
    expect(markup).toContain('state=completed');
    expect(markup).toContain('Completed');
  });

  it('renders failed executions from authoritative status and error data', () => {
    const markup = render({
      output: 'state=failed',
      status: 'failed',
      error: { code: 'TOOL_FAILURE', category: 'DEPENDENCY_FAILURE', message: 'search backend unavailable' },
    });

    expect(markup).toContain('Research failed');
    expect(markup).toContain('TOOL_FAILURE');
    expect(markup).toContain('search backend unavailable');
    expect(markup).not.toContain('Completed');
  });

  it('renders the empty state without inventing a result', () => {
    expect(render({})).toContain('No research result yet');
    expect(render({ status: 'completed' })).toContain('No research output');
    expect(render({ status: 'timed_out' })).toContain('Research timed out');
  });

  it('renders malformed JSON as text rather than structured output', () => {
    const malformed = '{"sources": "1. A | https://example.com", "count":';
    const markup = render({ output: malformed, status: 'completed' });

    expect(markup).toContain('sources');
    expect(markup).toContain('count');
    expect(markup).not.toContain('Research Sources');
  });

  it('renders the documented structured web.search envelope as a summary only', () => {
    const output = JSON.stringify({
      sources: '1. Example | https://example.com/a | Example | A snippet',
      count: '1',
    });
    const markup = render({ output, status: 'completed' });

    expect(markup).toContain('Structured research result');
    expect(markup).toContain('1 source record');
    expect(markup).toContain('Sources &amp; Evidence');
    expect(markup).not.toContain('<a ');
  });

  it('escapes untrusted result text and never renders it as HTML', () => {
    const markup = render({
      output:
        'state=completed | objective=test | agent=analyst | iterations=1 tool_calls=0 | ' +
        'result=<script>alert(1)</script> <img src=x onerror=alert(2)>',
      status: 'completed',
    });

    expect(markup).not.toContain('<script>');
    expect(markup).not.toContain('<img');
    expect(markup).toContain('&lt;script&gt;');
  });

  it('does not linkify URLs found in model-generated result prose', () => {
    const markup = render({
      output:
        'state=completed | objective=test | agent=analyst | iterations=1 tool_calls=0 | ' +
        'result=See https://unverified.example/claim for details',
      status: 'completed',
    });

    expect(markup).toContain('https://unverified.example/claim');
    expect(markup).not.toContain('<a href="https://unverified.example/claim"');
  });

  it('preserves list formatting from result text', () => {
    const markup = render({
      output:
        'state=completed | objective=test | agent=analyst | iterations=1 tool_calls=0 | ' +
        'result=Findings:\n\n- first item\n- second item',
      status: 'completed',
    });

    expect(markup).toContain('<ul');
    expect(markup).toContain('first item');
    expect(markup).toContain('second item');
  });
});
