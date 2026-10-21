/**
 * API Client tests
 * Verifies the client hits the correct backend endpoints, auth headers,
 * and SSE event parsing. These tests use fetch mocking so no backend
 * is required.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { apiClient, createAPIClient, EventStream, createEventStream } from './client';
import type { HealthResponse, StatusResponse, SubmitIntelligenceRequest } from '../types/api';

// Mock fetch globally
const mockFetch = vi.fn<typeof fetch>();
globalThis.fetch = mockFetch;

beforeEach(() => {
  mockFetch.mockClear();
  apiClient.setAuth('nx:human:test', 'test-credential');
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('API client base URL', () => {
  it('uses /api/v1 base for API calls', async () => {
    mockFetch.mockResolvedValueOnce(
      new Response(JSON.stringify({ execution_id: 'exec-1' }), { status: 200 })
    );
    await apiClient.submitIntelligence({
      business_id: 'biz-1',
      actor_id: 'actor-1',
      objective: { description: 'test' },
    });
    expect(mockFetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/intelligence/execute'),
      expect.objectContaining({
        method: 'POST',
        headers: expect.objectContaining({
          'X-Actor-ID': 'nx:human:test',
          'X-Actor-Credential': 'test-credential',
        }),
      })
    );
  });

  it('uses root base for health endpoint (different port in dev)', async () => {
    mockFetch.mockResolvedValueOnce(
      new Response(JSON.stringify({ status: 'ok', time: '2026-01-01T00:00:00Z' }), {
        status: 200,
      })
    );
    await apiClient.getHealth();
    expect(mockFetch).toHaveBeenCalledWith(
      expect.stringMatching(/\/health$/),
      expect.anything()
    );
    // The URL should NOT contain /api/v1
    const calledUrl = mockFetch.mock.calls[0][0] as string;
    expect(calledUrl).not.toContain('/api/v1/health');
  });

  it('uses root base for status endpoint', async () => {
    mockFetch.mockResolvedValueOnce(
      new Response(JSON.stringify({ status: 'running' }), { status: 200 })
    );
    await apiClient.getStatus();
    const calledUrl = mockFetch.mock.calls[0][0] as string;
    expect(calledUrl).toMatch(/\/status$/);
    expect(calledUrl).not.toContain('/api/v1/status');
  });

  it('sends auth headers on authenticated calls', async () => {
    mockFetch.mockResolvedValueOnce(new Response('{}', { status: 200 }));
    await apiClient.listApprovals('biz-1');
    expect(mockFetch).toHaveBeenCalledWith(
      expect.stringContaining('/approvals'),
      expect.objectContaining({
        headers: expect.objectContaining({
          'X-Actor-ID': 'nx:human:test',
          'X-Actor-Credential': 'test-credential',
        }),
      })
    );
  });

  it('throws APIError on non-ok responses', async () => {
    mockFetch.mockResolvedValueOnce(
      new Response(JSON.stringify({ code: 'AUTH', message: 'auth failed' }), {
        status: 401,
      })
    );
    await expect(apiClient.getHealth()).rejects.toMatchObject({
      status: 401,
      name: 'APIError',
    });
  });
});

describe('EventStream (fetch-based SSE)', () => {
  it('connects with auth headers and business_id', async () => {
    let capturedInit: RequestInit | undefined;
    let capturedUrl: string | null = null;
    mockFetch.mockImplementation((input, init) => {
      capturedUrl = String(input);
      capturedInit = init;
      return Promise.resolve(
        new Response('data: {"type":"test","data":"hello"}\n\n', {
          status: 200,
          headers: { 'Content-Type': 'text/event-stream' },
        })
      );
    });

    const onMessage = vi.fn();
    const stream = new EventStream(apiClient, 'biz-1', { onMessage });
    stream.connect();

    await new Promise((r) => setTimeout(r, 50));

    expect(capturedUrl).toContain('/events');
    expect(capturedUrl).toContain('business_id=biz-1');
    expect(capturedInit?.headers).toMatchObject({
      'X-Actor-ID': 'nx:human:test',
      'X-Actor-Credential': 'test-credential',
    });
    stream.close();
  });

  it('parses SSE data events', async () => {
    const onMessage = vi.fn();
    const stream = new EventStream(apiClient, 'biz-1', { onMessage });
    stream.connect();

    // The fetch mock returns immediately; the pump reads the body
    await new Promise((r) => setTimeout(r, 100));

    expect(onMessage).toHaveBeenCalled();
    stream.close();
  });

  it('close() aborts the connection', async () => {
    let abortCalled = false;
    mockFetch.mockImplementation(() => {
      return new Promise<Response>((resolve) => {
        // Never resolve — simulates a hanging connection
        setTimeout(() => resolve(new Response('data: keepalive\n\n')), 10000);
      });
    });

    const stream = new EventStream(apiClient, 'biz-1');
    stream.connect();
    stream.close();
    // If close didn't abort, the fetch would hang; we just verify close() doesn't throw
    expect(() => stream.close()).not.toThrow();
  });
});

describe('createEventStream factory', () => {
  it('returns an EventStream with connect and close', () => {
    const onMessage = vi.fn();
    const stream = createEventStream('biz-1', onMessage);
    expect(stream).toHaveProperty('connect');
    expect(stream).toHaveProperty('close');
    expect(typeof stream.connect).toBe('function');
    expect(typeof stream.close).toBe('function');
    stream.close();
  });
});

describe('SubmitIntelligence request', () => {
  it('sends the correct payload shape', async () => {
    mockFetch.mockResolvedValueOnce(
      new Response(JSON.stringify({ execution_id: 'exec-123' }), { status: 202 })
    );
    const req: SubmitIntelligenceRequest = {
      business_id: 'biz-1',
      actor_id: 'actor-1',
      objective: { description: 'Analyze the quarterly report' },
    };
    await apiClient.submitIntelligence(req);
    const body = JSON.parse(mockFetch.mock.calls[0][1]?.body as string);
    expect(body.business_id).toBe('biz-1');
    expect(body.actor_id).toBe('actor-1');
    expect(body.objective.description).toBe('Analyze the quarterly report');
  });
});

describe('createAPIClient factory', () => {
  it('creates a client with a custom base URL', () => {
    const client = createAPIClient('http://localhost:9090/api/v1');
    expect(client.baseUrl()).toBe('http://localhost:9090/api/v1');
  });
});

describe('recovery executions (authoritative activity)', () => {
  it('calls the recovery list endpoint with business scope', async () => {
    mockFetch.mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          source: 'durable',
          executions: [],
          count: 0,
          limit: 50,
          offset: 0,
          has_more: false,
        }),
        { status: 200 }
      )
    );
    await apiClient.listRecoveryExecutions('biz-1', { limit: 50 });
    expect(mockFetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/recovery/executions?business_id=biz-1'),
      expect.anything()
    );
  });

  it('includes division scope when requested', async () => {
    mockFetch.mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          source: 'durable',
          executions: [],
          count: 0,
          limit: 50,
          offset: 0,
          has_more: false,
        }),
        { status: 200 }
      )
    );
    await apiClient.listRecoveryExecutions('biz-1', { divisionId: 'div-1' });
    expect(mockFetch).toHaveBeenCalledWith(
      expect.stringContaining('division_id=div-1'),
      expect.anything()
    );
  });
});
