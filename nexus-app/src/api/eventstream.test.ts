/**
 * Resilient EventStream tests.
 *
 * Covers reconnection with bounded backoff, auth-failure handling, cleanup,
 * malformed frames, and duplicate suppression. Uses deterministic fake timers
 * and a controllable mock fetch — no arbitrary sleeps.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { EventStream, apiClient } from './client';

const mockFetch = vi.fn() as unknown as typeof fetch;
global.fetch = mockFetch;

const encoder = new TextEncoder();

/** Build an SSE response whose body enqueues the given raw chunks. */
function sseResponse(rawChunks: string[], status = 200): Response {
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of rawChunks) {
        controller.enqueue(encoder.encode(chunk));
      }
      controller.close();
    },
  });
  return new Response(stream, {
    status,
    headers: { 'Content-Type': 'text/event-stream' },
  });
}

/**
 * Build an SSE response whose body stays OPEN after enqueuing the chunks.
 * Used for the "successful reconnect" side of a test so the stream does not
 * terminate again and trigger an unnecessary further reconnect.
 */
function openSseResponse(rawChunks: string[], status = 200): Response {
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of rawChunks) {
        controller.enqueue(encoder.encode(chunk));
      }
      // Intentionally not closed: the connection stays open.
    },
  });
  return new Response(stream, {
    status,
    headers: { 'Content-Type': 'text/event-stream' },
  });
}

/** A single complete SSE frame. */
function frame(eventType: string, extra: Record<string, unknown> = {}): string {
  return `event: ${eventType}\ndata: ${JSON.stringify({ event_type: eventType, ...extra })}\n\n`;
}

/** Flush pending microtasks (resolved fetch promises and reader callbacks). */
async function flush() {
  for (let i = 0; i < 10; i++) {
    await Promise.resolve();
  }
}

beforeEach(() => {
  mockFetch.mockReset();
  apiClient.setAuth('nx:human:test', 'test-credential');
});

afterEach(() => {
  vi.useRealTimers();
});

describe('EventStream reconnection', () => {
  it('reconnects after a transient stream termination and resets backoff', async () => {
    vi.useFakeTimers();

    // First attempt: stream opens then immediately terminates.
    mockFetch.mockResolvedValueOnce(sseResponse([frame('chain.started', { task_id: 't1' })]));
    // Second attempt: successful stream that stays open.
    mockFetch.mockResolvedValueOnce(openSseResponse([frame('chain.completed', { task_id: 't1' })]));

    const onOpen = vi.fn();
    const onReconnected = vi.fn();
    const onMessage = vi.fn();

    const stream = new EventStream(apiClient, 'biz-1', {
      onOpen,
      onReconnected,
      onMessage,
    });
    stream.connect();

    await flush();
    expect(onOpen).toHaveBeenCalledTimes(1);

    // Stream ended → reconnect scheduled with backoff.
    expect(vi.getTimerCount()).toBeGreaterThan(0);
    await vi.advanceTimersByTimeAsync(2000);

    await flush();
    expect(onOpen).toHaveBeenCalledTimes(2);
    expect(onReconnected).toHaveBeenCalledTimes(1);
    expect(onMessage).toHaveBeenCalledTimes(2);
    expect(stream.getState()).toBe('open');

    stream.close();
  });

  it('does not retry permanent authorization failures (401/403)', async () => {
    vi.useFakeTimers();
    mockFetch.mockResolvedValue(new Response('unauthorized', { status: 401 }));

    const onOpen = vi.fn();
    const onError = vi.fn();

    const stream = new EventStream(apiClient, 'biz-1', { onOpen, onError });
    stream.connect();

    await flush();
    expect(onOpen).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledTimes(1);
    expect(stream.getState()).toBe('failed');
    // No retry timer pending — auth failures are terminal.
    expect(vi.getTimerCount()).toBe(0);
  });

  it('retries transient HTTP failures (500) with backoff', async () => {
    vi.useFakeTimers();
    mockFetch
      .mockResolvedValueOnce(new Response('server error', { status: 500 }))
      .mockResolvedValueOnce(sseResponse([frame('chain.started', { task_id: 't1' })]));

    const onOpen = vi.fn();

    const stream = new EventStream(apiClient, 'biz-1', { onOpen });
    stream.connect();

    await flush();
    expect(onOpen).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBeGreaterThan(0);

    await vi.advanceTimersByTimeAsync(1000);
    await flush();
    expect(onOpen).toHaveBeenCalledTimes(1);
  });

  it('caps backoff delay at maxDelayMs', async () => {
    vi.useFakeTimers();
    const delays: number[] = [];
    const realSetTimeout = globalThis.setTimeout;
    const spy = vi.spyOn(globalThis, 'setTimeout').mockImplementation((fn, ms, ...args) => {
      delays.push(ms as number);
      return realSetTimeout(fn, ms, ...args);
    });

    mockFetch.mockRejectedValue(new TypeError('network error'));

    const stream = new EventStream(apiClient, 'biz-1', {}, {
      baseDelayMs: 100,
      maxDelayMs: 500,
      jitterMs: 0,
    });
    stream.connect();

    // Drive several reconnect attempts.
    for (let i = 0; i < 8; i++) {
      await vi.advanceTimersByTimeAsync(2000);
      await flush();
    }

    // Every scheduled delay must be <= maxDelayMs.
    expect(delays.length).toBeGreaterThan(0);
    expect(Math.max(...delays)).toBeLessThanOrEqual(500);

    spy.mockRestore();
  });

  it('cleans up the pending retry timer on close', async () => {
    vi.useFakeTimers();
    mockFetch.mockRejectedValue(new TypeError('network error'));

    const stream = new EventStream(apiClient, 'biz-1');
    stream.connect();
    await flush();

    expect(vi.getTimerCount()).toBeGreaterThan(0);
    stream.close();
    expect(vi.getTimerCount()).toBe(0);
    expect(stream.getState()).toBe('closed');
  });

  it('aborts the in-flight fetch on close', async () => {
    vi.useFakeTimers();
    let abortSignal: AbortSignal | null = null;
    mockFetch.mockImplementation((_url: string, init?: RequestInit) => {
      abortSignal = init?.signal as AbortSignal;
      // Never resolve — simulates a hanging request.
      return new Promise(() => {});
    });

    const stream = new EventStream(apiClient, 'biz-1');
    stream.connect();
    await flush();

    expect(abortSignal).not.toBeNull();
    expect(abortSignal!.aborted).toBe(false);
    stream.close();
    expect(abortSignal!.aborted).toBe(true);
  });

  it('handles malformed frames without crashing', async () => {
    vi.useFakeTimers();
    mockFetch.mockResolvedValueOnce(
      sseResponse([
        'data: not-json\n\n',
        'data: {"event_type":"chain.completed","event_id":"e1"}\n\n',
      ])
    );

    const onMessage = vi.fn();
    const stream = new EventStream(apiClient, 'biz-1', { onMessage });
    stream.connect();
    await flush();
    await vi.advanceTimersByTimeAsync(0);

    // Malformed frame emitted raw; valid frame emitted parsed.
    expect(onMessage).toHaveBeenCalledTimes(2);
    expect(onMessage.mock.calls[0][0].data).toBe('not-json');
    expect(onMessage.mock.calls[1][0].data).toMatchObject({
      event_type: 'chain.completed',
      event_id: 'e1',
    });
  });

  it('suppresses duplicate event_ids', async () => {
    vi.useFakeTimers();
    mockFetch.mockResolvedValueOnce(
      sseResponse([
        'data: {"event_type":"chain.completed","event_id":"e1"}\n\n',
        'data: {"event_type":"chain.completed","event_id":"e1"}\n\n',
        'data: {"event_type":"chain.completed","event_id":"e2"}\n\n',
      ])
    );

    const onMessage = vi.fn();
    const stream = new EventStream(apiClient, 'biz-1', { onMessage });
    stream.connect();
    await flush();

    expect(onMessage).toHaveBeenCalledTimes(2);
    expect(onMessage.mock.calls[0][0].data.event_id).toBe('e1');
    expect(onMessage.mock.calls[1][0].data.event_id).toBe('e2');
  });

  it('preserves auth headers and business scope across reconnects', async () => {
    vi.useFakeTimers();
    const seen: Array<{ url: string; headers: Record<string, string> }> = [];
    mockFetch.mockImplementation((url: string, init?: RequestInit) => {
      seen.push({ url: String(url), headers: (init?.headers as Record<string, string>) ?? {} });
      return Promise.resolve(sseResponse([frame('chain.started', { task_id: 't1' })]));
    });

    const stream = new EventStream(apiClient, 'biz-1');
    stream.connect();
    await flush();
    await vi.advanceTimersByTimeAsync(1000);
    await flush();

    expect(seen.length).toBeGreaterThanOrEqual(2);
    for (const req of seen) {
      expect(req.url).toContain('/events');
      expect(req.url).toContain('business_id=biz-1');
      expect(req.headers['X-Actor-ID']).toBe('nx:human:test');
      expect(req.headers['X-Actor-Credential']).toBe('test-credential');
    }
  });
});

describe('EventStream state transitions', () => {
  it('reports connecting → open → reconnecting → open', async () => {
    vi.useFakeTimers();
    const states: string[] = [];

    mockFetch
      .mockResolvedValueOnce(sseResponse([frame('chain.started', { task_id: 't1' })]))
      .mockResolvedValueOnce(openSseResponse([frame('chain.completed', { task_id: 't1' })]));

    const stream = new EventStream(apiClient, 'biz-1', {
      onStateChange: (s) => states.push(s),
    });
    stream.connect();
    await flush();
    await vi.advanceTimersByTimeAsync(1000);
    await flush();

    expect(states).toContain('connecting');
    expect(states).toContain('open');
    expect(states).toContain('reconnecting');
    expect(stream.getState()).toBe('open');

    stream.close();
  });

  it('reports failed on auth error and closed on close', async () => {
    vi.useFakeTimers();
    mockFetch.mockResolvedValue(new Response('forbidden', { status: 403 }));

    const states: string[] = [];
    const stream = new EventStream(apiClient, 'biz-1', {
      onStateChange: (s) => states.push(s),
    });
    stream.connect();
    await flush();

    expect(states).toContain('failed');
    expect(stream.getState()).toBe('failed');

    stream.close();
    expect(stream.getState()).toBe('closed');
  });

  describe('EventStream retry delay exposure', () => {
    it('returns null when no reconnect is pending', async () => {
      const stream = new EventStream(apiClient, 'biz-1');
      expect(stream.getRetryDelayMs()).toBeNull();
      expect(stream.getRetryAttempt()).toBe(0);
      stream.close();
    });

    it('returns null while connected', async () => {
      vi.useFakeTimers();
      mockFetch.mockReset();
      mockFetch.mockResolvedValue(openSseResponse([frame('chain.started', { task_id: 't1' })]));
      const stream = new EventStream(apiClient, 'biz-1');
      stream.connect();
      await flush();
      expect(stream.getRetryDelayMs()).toBeNull();
      expect(stream.getState()).toBe('open');
      stream.close();
    });

    it('reports a positive delay after a transient failure', async () => {
      vi.useFakeTimers();
      mockFetch.mockReset();
      mockFetch.mockRejectedValue(new TypeError('network error'));
      const stream = new EventStream(apiClient, 'biz-1', {}, { baseDelayMs: 50, maxDelayMs: 100, jitterMs: 0 });
      stream.connect();
      await flush();

      const delay = stream.getRetryDelayMs();
      expect(delay).toBeGreaterThan(0);
      expect(delay).toBeLessThanOrEqual(50);
      expect(stream.getRetryAttempt()).toBe(1);
      stream.close();
    });

    it('clears the delay after a successful reconnect', async () => {
      vi.useFakeTimers();
      mockFetch.mockReset();
      mockFetch.mockRejectedValue(new TypeError('network error'));
      const stream = new EventStream(apiClient, 'biz-1', {}, { baseDelayMs: 50, maxDelayMs: 100, jitterMs: 0 });
      stream.connect();
      await flush();
      expect(stream.getRetryDelayMs()).toBeGreaterThan(0);

      // A successful reconnect clears the pending retry delay.
      mockFetch.mockResolvedValue(openSseResponse([frame('chain.started', { task_id: 't1' })]));
      stream.connect();
      await flush();
      expect(stream.getRetryDelayMs()).toBeNull();
      expect(stream.getState()).toBe('open');
      stream.close();
    });

    it('returns null after close', async () => {
      vi.useFakeTimers();
      mockFetch.mockReset();
      mockFetch.mockRejectedValue(new TypeError('network error'));
      const stream = new EventStream(apiClient, 'biz-1');
      stream.connect();
      await flush();
      expect(stream.getRetryDelayMs()).toBeGreaterThan(0);
      stream.close();
      expect(stream.getRetryDelayMs()).toBeNull();
    });
  });
});
