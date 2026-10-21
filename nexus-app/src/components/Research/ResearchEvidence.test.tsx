/**
 * ResearchEvidence rendering tests.
 *
 * Covers the evidence boundary: only documented source records become links,
 * invalid URLs never become anchors, duplicate records collapse, and the
 * empty state distinguishes "no records" from "search returned nothing".
 */

import React from 'react';
import { describe, it, expect } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { ResearchEvidence } from './ResearchEvidence';
import type { WebSearchResult } from '../../types/api';
import type { ResearchToolActivity } from '../../types/research';

const WEB_SEARCH_ACTIVITY: ResearchToolActivity = {
  callId: 'call-1',
  toolId: 'web.search',
  outcome: 'completed',
  attempts: 1,
  durationMs: 0,
  resultBytes: 294,
};

function render(props: React.ComponentProps<typeof ResearchEvidence>): string {
  return renderToStaticMarkup(<ResearchEvidence {...props} />);
}

function webSearchResult(count: string, ...lines: string[]): WebSearchResult {
  return { sources: lines.join('\n'), count };
}

describe('ResearchEvidence', () => {
  it('renders real source records with safe external links', () => {
    const markup = render({
      result: webSearchResult(
        '2',
        '1. NEXUS Runtime | https://example.com/runtime | Example | Runtime overview',
        '2. Tool Platform | https://example.com/tools | Docs | Tool contracts'
      ),
      executionId: 'exec-1',
      onViewTrace: () => {},
    });

    expect(markup).toContain('Research Sources');
    expect(markup).toContain('2 sources');
    expect(markup).toContain('NEXUS Runtime');
    expect(markup).toContain('href="https://example.com/runtime"');
    expect(markup).toContain('target="_blank"');
    expect(markup).toContain('rel="noopener noreferrer"');
    expect(markup).toContain('View Trace');
  });

  it('never turns an invalid URL into an anchor', () => {
    const markup = render({
      result: webSearchResult(
        '1',
        '1. Unsafe | javascript:alert(1) | Example | do not link'
      ),
    });

    expect(markup).toContain('Unsafe');
    expect(markup).toContain('No link available');
    expect(markup).not.toContain('href="javascript:');
    expect(markup).not.toContain('<a ');
  });

  it('deduplicates repeated source URLs', () => {
    const markup = render({
      result: webSearchResult(
        '2',
        '1. First | https://example.com/a | Example | first',
        '2. Duplicate | https://example.com/a | Example | second'
      ),
    });

    expect(markup.match(/<article/g)?.length ?? 0).toBe(1);
    expect(markup).toContain('First');
    expect(markup).not.toContain('Duplicate');
  });

  it('falls back for missing titles and sources', () => {
    const markup = render({
      result: webSearchResult('1', '1. | https://example.com/no-title | | snippet'),
    });

    expect(markup).toContain('example.com'); // title falls back to the host
    expect(markup).toContain('snippet');
  });

  it('explains that trace tool metadata is not a citation list', () => {
    const markup = render({
      toolActivity: [WEB_SEARCH_ACTIVITY],
      status: 'completed',
      executionId: 'exec-1',
      onViewTrace: () => {},
    });

    expect(markup).toContain('No source records available');
    expect(markup).toContain('web.search');
    expect(markup).toContain('294 B result');
    expect(markup).toContain('not citations');
    expect(markup).toContain('View Trace');
    expect(markup).not.toContain('<a ');
  });

  it('says no records were reported when no search tool ran', () => {
    const markup = render({ status: 'completed' });

    expect(markup).toContain('No source records available');
    expect(markup).toContain('did not report any source records');
  });

  it('renders a pending state without claiming a result', () => {
    const markup = render({ status: 'running' });

    expect(markup).toContain('Evidence pending');
    expect(markup).toContain('still running');
  });

  it('distinguishes a structured result with no usable sources', () => {
    const markup = render({ result: { sources: '', count: '0' }, status: 'completed' });

    expect(markup).toContain('No usable source records');
  });

  it('reports unparseable records without hiding the usable ones', () => {
    const markup = render({
      result: webSearchResult(
        '3',
        '1. One | https://example.com/one | Example | first',
        '2. Two | https://example.com/two | Example | second'
      ),
    });

    expect(markup).toContain('2 sources');
    expect(markup).toContain('3 source records');
    expect(markup).toContain('1 could not be displayed');
  });

  it('escapes untrusted source fields', () => {
    const markup = render({
      result: webSearchResult(
        '1',
        '1. <script>alert(1)</script> | https://example.com/x | <img src=x onerror=alert(2)> | <b>bold</b>'
      ),
    });

    expect(markup).not.toContain('<script>');
    expect(markup).not.toContain('<img');
    expect(markup).toContain('&lt;script&gt;');
  });
});
