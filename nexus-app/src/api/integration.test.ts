/**
 * Integration tests for the submit → approve → resume → result journey.
 * These tests mock the fetch layer to simulate the real backend contract,
 * verifying that the frontend sends correct requests and handles responses
 * through the full lifecycle.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { apiClient } from './client';
import type { SubmitIntelligenceRequest, ExecutionStatus } from './types/api';

const mockFetch = vi.fn() as unknown as typeof fetch;
global.fetch = mockFetch;

const BUSINESS_ID = 'biz-1';
const ACTOR_ID = 'nx:human:alice';
const EXECUTION_ID = 'exec-123';
const APPROVAL_ID = 'apr-456';

beforeEach(() => {
  mockFetch.mockClear();
  apiClient.setAuth(ACTOR_ID, 'test-credential');
});

afterEach(() => {
  vi.restoreAllMocks();
});

function mockJson(status: number, body: unknown) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function lastRequest() {
  const calls = mockFetch.mock.calls;
  return {
    url: calls[calls.length - 1][0] as string,
    init: calls[calls.length - 1][1] as RequestInit,
  };
}

describe('submit → approve → resume → result journey', () => {
  it('completes the full approval lifecycle through the API client', async () => {
    // 1. Submit intelligence → 202 with execution_id
    mockFetch.mockResolvedValueOnce(mockJson(202, { execution_id: EXECUTION_ID }));
    const submitRes = await apiClient.submitIntelligence({
      business_id: BUSINESS_ID,
      actor_id: ACTOR_ID,
      objective: { description: 'Analyze the quarterly report' },
    });
    expect(submitRes.execution_id).toBe(EXECUTION_ID);

    // 2. Execution is in approval-required state
    mockFetch.mockResolvedValueOnce(
      mockJson(200, {
        execution_id: EXECUTION_ID,
        status: 'pending',
        error: {
          code: 'APPROVAL_REQUIRED',
          category: 'APPROVAL_REQUIRED',
          message: 'approval required',
        },
        created_at: '2026-01-01T00:00:00Z',
      })
    );
    const status1 = await apiClient.getIntelligence(EXECUTION_ID, BUSINESS_ID);
    expect(status1.status).toBe('pending');

    // 3. List approvals → pending approval exists
    mockFetch.mockResolvedValueOnce(
      mockJson(200, {
        approvals: [
          {
            entity_id: APPROVAL_ID,
            status: 'PENDING',
            audit_ref: EXECUTION_ID,
            approver_type: 'human',
            approver_id: 'approver-1',
            created_at: '2026-01-01T00:00:00Z',
          },
        ],
      })
    );
    const approvals = await apiClient.listApprovals(BUSINESS_ID);
    expect(approvals.approvals).toHaveLength(1);
    expect(approvals.approvals[0].entity_id).toBe(APPROVAL_ID);
    expect(approvals.approvals[0].status).toBe('PENDING');
    expect(approvals.approvals[0].audit_ref).toBe(EXECUTION_ID);

    // 4. Approve → 200 with approval_id and status
    mockFetch.mockResolvedValueOnce(
      mockJson(200, { approval_id: APPROVAL_ID, status: 'APPROVED' })
    );
    const approveRes = await apiClient.approveApproval(APPROVAL_ID, BUSINESS_ID, {
      reason: 'Looks good',
    });
    expect(approveRes.approval_id).toBe(APPROVAL_ID);
    expect(approveRes.status).toBe('APPROVED');

    // 5. Resumed execution → completed
    mockFetch.mockResolvedValueOnce(
      mockJson(200, {
        execution_id: EXECUTION_ID,
        status: 'completed',
        output: 'Report analysis complete',
        created_at: '2026-01-01T00:00:00Z',
        completed_at: '2026-01-01T00:01:00Z',
      })
    );
    const status2 = await apiClient.getIntelligence(EXECUTION_ID, BUSINESS_ID);
    expect(status2.status).toBe('completed');
    expect(status2.output).toBe('Report analysis complete');

    // Verify all requests used correct auth headers and endpoints
    const urls = mockFetch.mock.calls.map((c) => c[0] as string);
    expect(urls).toContain('/api/v1/intelligence/execute');
    expect(urls).toContain(`/api/v1/intelligence/${EXECUTION_ID}?business_id=${BUSINESS_ID}`);
    expect(urls).toContain(`/api/v1/approvals?business_id=${BUSINESS_ID}`);
    expect(urls).toContain(
      `/api/v1/approvals/${APPROVAL_ID}/approve?business_id=${BUSINESS_ID}`
    );

    // Verify all requests sent auth headers
    for (const call of mockFetch.mock.calls) {
      const init = call[1] as RequestInit;
      expect(init.headers).toMatchObject({
        'X-Actor-ID': ACTOR_ID,
        'X-Actor-Credential': 'test-credential',
      });
    }
  });

  it('denial path: submit → deny → execution stays failed', async () => {
    // 1. Submit
    mockFetch.mockResolvedValueOnce(mockJson(202, { execution_id: EXECUTION_ID }));
    await apiClient.submitIntelligence({
      business_id: BUSINESS_ID,
      actor_id: ACTOR_ID,
      objective: { description: 'Do something questionable' },
    });

    // 2. Execution is in approval-required state
    mockFetch.mockResolvedValueOnce(
      mockJson(200, {
        execution_id: EXECUTION_ID,
        status: 'pending',
        error: { code: 'APPROVAL_REQUIRED', category: 'APPROVAL_REQUIRED', message: 'approval required' },
        created_at: '2026-01-01T00:00:00Z',
      })
    );
    await apiClient.getIntelligence(EXECUTION_ID, BUSINESS_ID);

    // 3. List approvals
    mockFetch.mockResolvedValueOnce(
      mockJson(200, {
        approvals: [
          {
            entity_id: APPROVAL_ID,
            status: 'PENDING',
            audit_ref: EXECUTION_ID,
            approver_type: 'human',
            approver_id: 'approver-1',
            created_at: '2026-01-01T00:00:00Z',
          },
        ],
      })
    );
    await apiClient.listApprovals(BUSINESS_ID);

    // 4. Deny → 200
    mockFetch.mockResolvedValueOnce(
      mockJson(200, { approval_id: APPROVAL_ID, status: 'DENIED' })
    );
    const denyRes = await apiClient.denyApproval(APPROVAL_ID, BUSINESS_ID, {
      reason: 'Not authorized',
    });
    expect(denyRes.approval_id).toBe(APPROVAL_ID);
    expect(denyRes.status).toBe('DENIED');

    // 5. Execution remains failed/pending (no resume)
    mockFetch.mockResolvedValueOnce(
      mockJson(200, {
        execution_id: EXECUTION_ID,
        status: 'pending',
        error: {
          code: 'APPROVAL_REQUIRED',
          category: 'APPROVAL_REQUIRED',
          message: 'approval required',
        },
        created_at: '2026-01-01T00:00:00Z',
      })
    );
    const status = await apiClient.getIntelligence(EXECUTION_ID, BUSINESS_ID);
    expect(status.status).toBe('pending');

    // Verify deny used correct endpoint
    const urls = mockFetch.mock.calls.map((c) => c[0] as string);
    expect(urls).toContain(
      `/api/v1/approvals/${APPROVAL_ID}/deny?business_id=${BUSINESS_ID}`
    );
  });

  it('submit sends correct payload shape', async () => {
    mockFetch.mockResolvedValueOnce(mockJson(202, { execution_id: EXECUTION_ID }));
    const req: SubmitIntelligenceRequest = {
      business_id: BUSINESS_ID,
      actor_id: ACTOR_ID,
      objective: { description: 'Test objective' },
    };
    await apiClient.submitIntelligence(req);
    const body = JSON.parse(mockFetch.mock.calls[0][1]?.body as string);
    expect(body.business_id).toBe(BUSINESS_ID);
    expect(body.actor_id).toBe(ACTOR_ID);
    expect(body.objective.description).toBe('Test objective');
    expect(mockFetch.mock.calls[0][0]).toBe('/api/v1/intelligence/execute');
  });
});
