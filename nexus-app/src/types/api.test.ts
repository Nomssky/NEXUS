/**
 * Web-search source parsing tests.
 *
 * Provider data is untrusted. Parsing must be deterministic, must not create
 * links from invalid or non-http URLs, and must handle missing/duplicate
 * fields without throwing.
 */

import { describe, it, expect } from 'vitest';
import { isSafeExternalUrl, parseWebSearchResult } from './api';

describe('isSafeExternalUrl', () => {
  it.each([
    'https://example.com/article',
    'http://example.com',
    'https://example.com/path?q=1#frag',
  ])('accepts %s', (url) => {
    expect(isSafeExternalUrl(url)).toBe(true);
  });

  it.each([
    '',
    'javascript:alert(1)',
    'data:text/html,<script>alert(1)</script>',
    'mailto:someone@example.com',
    'file:///etc/passwd',
    '/relative/path',
    'example.com',
    'https://user:pass@example.com/',
    'not a url',
  ])('rejects %s', (url) => {
    expect(isSafeExternalUrl(url)).toBe(false);
  });
});

describe('parseWebSearchResult', () => {
  it('parses the documented line format', () => {
    const sources = parseWebSearchResult({
      sources:
        '1. NEXUS Runtime | https://example.com/runtime | Example | Runtime overview\n' +
        '2. Tool Platform | https://example.com/tools | Example | Tool contracts',
      count: '2',
    });

    expect(sources).toHaveLength(2);
    expect(sources[0]).toMatchObject({
      index: 1,
      title: 'NEXUS Runtime',
      url: 'https://example.com/runtime',
      source: 'Example',
      snippet: 'Runtime overview',
      linkable: true,
    });
    expect(sources[1].index).toBe(2);
  });

  it('deduplicates by URL and keeps the first occurrence', () => {
    const sources = parseWebSearchResult({
      sources:
        '1. First | https://example.com/a | Example | first snippet\n' +
        '2. Duplicate | https://example.com/a | Example | second snippet\n' +
        '3. Other | https://example.com/b | Example | other snippet',
      count: '3',
    });

    expect(sources).toHaveLength(2);
    expect(sources[0].title).toBe('First');
    expect(sources[1].title).toBe('Other');
    expect(sources[1].index).toBe(2);
  });

  it('keeps source records with an invalid URL but marks them unlinkable', () => {
    const sources = parseWebSearchResult({
      sources: '1. Unsafe | javascript:alert(1) | Example | do not link',
      count: '1',
    });

    expect(sources).toHaveLength(1);
    expect(sources[0].linkable).toBe(false);
    expect(sources[0].url).toBe('javascript:alert(1)');
  });

  it('falls back deterministically for missing fields', () => {
    const sources = parseWebSearchResult({
      sources:
        '1. | https://example.com/no-title | Example | \n' +
        '2. Only URL | https://example.com/only-url\n' +
        '3. No URL |  | Source Name | snippet',
      count: '3',
    });

    expect(sources[0].title).toBe('Example');
    expect(sources[1].title).toBe('Only URL');
    expect(sources[1].source).toBe('example.com');
    expect(sources[1].snippet).toBe('');
    expect(sources[2].title).toBe('No URL');
    expect(sources[2].source).toBe('Source Name');
    expect(sources[2].url).toBe('');
    expect(sources[2].linkable).toBe(false);
  });

  it('preserves pipes in the snippet', () => {
    const sources = parseWebSearchResult({
      sources: '1. Title | https://example.com | Source | a | b | c',
      count: '1',
    });

    expect(sources[0].snippet).toBe('a | b | c');
  });

  it('skips lines without a title/url separator', () => {
    const sources = parseWebSearchResult({
      sources: 'not a source line\n\n1. Good | https://example.com | Source | snippet',
      count: '2',
    });

    expect(sources).toHaveLength(1);
    expect(sources[0].title).toBe('Good');
  });

  it('handles an empty source string', () => {
    expect(parseWebSearchResult({ sources: '', count: '0' })).toEqual([]);
  });
});
