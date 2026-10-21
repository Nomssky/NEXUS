/**
 * Integration tests for the submit → approve → resume → result journey.
 * These tests mock the fetch layer to simulate the real backend contract,
 * verifying that the frontend sends correct requests and handles responses
 * through the full lifecycle.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { apiClient } from './client';
import type { SubmitIntelligenceRequest, ExecutionStatus } from './types/api';

const mockFetch = vi.fn() as unknown as ReturnType<typeof fetch> & ReturnType<typeof vi.fn>;
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
    // Backend returns { request_id, status, outcome.summary, ... }
    mockFetch.mockResolvedValueOnce(
      mockJson(200, {
        request_id: EXECUTION_ID,
        business_id: BUSINESS_ID,
        status: 'pending',
        error: {
          code: 'APPROVAL_REQUIRED',
          category: 'APPROVAL_REQUIRED',
          message: 'approval required',
        },
        audit_trace: [],
        duration: 1234,
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
    // Backend returns { request_id, status, outcome: { summary }, duration, ... }
    mockFetch.mockResolvedValueOnce(
      mockJson(200, {
        request_id: EXECUTION_ID,
        business_id: BUSINESS_ID,
        status: 'completed',
        outcome: {
          summary: 'Report analysis complete',
          artifacts: [],
          metrics: { duration_ms: 1234 },
        },
        error: null,
        audit_trace: [],
        duration: 1234,
      })
    );
    const status2 = await apiClient.getIntelligence(EXECUTION_ID, BUSINESS_ID);
    expect(status2.status).toBe('completed');
    expect(status2.output).toBe('Report analysis complete');
    expect(status2.duration_ms).toBe(1234);

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

    // 2. Execution is in approval-required state.
    mockFetch.mockResolvedValueOnce(
      mockJson(200, {
        request_id: EXECUTION_ID,
        business_id: BUSINESS_ID,
        status: 'pending',
        error: { code: 'APPROVAL_REQUIRED', category: 'APPROVAL_REQUIRED', message: 'approval required' },
        audit_trace: [],
        duration: 1234,
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
        request_id: EXECUTION_ID,
        business_id: BUSINESS_ID,
        status: 'pending',
        error: {
          code: 'APPROVAL_REQUIRED',
          category: 'APPROVAL_REQUIRED',
          message: 'approval required',
        },
        audit_trace: [],
        duration: 1234,
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

describe('reconnect → reconcile → approve → final result', () => {
  it('reconciles authoritative recovery state after an event-stream interruption', async () => {
    // 1. Submit.
    mockFetch.mockResolvedValueOnce(mockJson(202, { execution_id: EXECUTION_ID }));
    const submitted = await apiClient.submitIntelligence({
      business_id: BUSINESS_ID,
      actor_id: ACTOR_ID,
      objective: { description: 'Quarterly analysis' },
    });
    expect(submitted.execution_id).toBe(EXECUTION_ID);

    // 2. Observe running state.
    mockFetch.mockResolvedValueOnce(
      mockJson(200, {
        request_id: EXECUTION_ID,
        business_id: BUSINESS_ID,
        status: 'running',
        outcome: null,
        error: null,
        audit_trace: [],
        duration: 500,
      })
    );
    const running = await apiClient.getIntelligence(EXECUTION_ID, BUSINESS_ID);
    expect(running.status).toBe('running');

    // 3. Authoritative recovery list reflects the running execution.
    mockFetch.mockResolvedValueOnce(
      mockJson(200, {
        source: 'durable',
        executions: [
          {
            source: 'durable',
            execution_id: EXECUTION_ID,
            business_id: BUSINESS_ID,
            state: 'running',
            created_at: '2026-01-01T00:00:00Z',
            updated_at: '2026-01-01T00:00:00Z',
          },
        ],
        count: 1,
        limit: 50,
        offset: 0,
        has_more: false,
      })
    );
    const beforeReconnect = await apiClient.listRecoveryExecutions(BUSINESS_ID);
    expect(beforeReconnect.executions[0].state).toBe('running');

    // 4. Event stream interrupted → reconnect reconciles from the authoritative
    //    recovery read (this is the useEventStream onReconnected behavior).
    mockFetch.mockResolvedValueOnce(
      mockJson(200, {
        source: 'durable',
        executions: [
          {
            source: 'durable',
            execution_id: EXECUTION_ID,
            business_id: BUSINESS_ID,
            state: 'pending_approval',
            error_class: 'APPROVAL_REQUIRED',
            created_at: '2026-01-01T00:00:00Z',
            updated_at: '2026-01-01T00:00:05Z',
          },
        ],
        count: 1,
        limit: 50,
        offset: 0,
        has_more: false,
      })
    );
    const reconciled = await apiClient.listRecoveryExecutions(BUSINESS_ID);
    expect(reconciled.executions[0].state).toBe('pending_approval');
    expect(reconciled.executions[0].error_class).toBe('APPROVAL_REQUIRED');

    // 5. Approval is required; approve it.
    mockFetch.mockResolvedValueOnce(
      mockJson(200, {
        approvals: [
          {
            entity_id: APPROVAL_ID,
            status: 'PENDING',
            requested_action: 'execute_tool',
            requester_id: ACTOR_ID,
            approver_type: 'human',
            created_at: '2026-01-01T00:00:05Z',
            audit_ref: EXECUTION_ID,
            self_approval_prohibited: true,
            delegation_allowed: false,
          },
        ],
      })
    );
    const pending = await apiClient.listApprovals(BUSINESS_ID);
    expect(pending.approvals[0].status).toBe('PENDING');

    mockFetch.mockResolvedValueOnce(
      mockJson(200, { approval_id: APPROVAL_ID, status: 'APPROVED' })
    );
    const approved = await apiClient.approveApproval(APPROVAL_ID, BUSINESS_ID, {
      reason: 'Looks good',
    });
    expect(approved.status).toBe('APPROVED');

    // 6. Reconcile again — the resumed execution completed.
    mockFetch.mockResolvedValueOnce(
      mockJson(200, {
        request_id: EXECUTION_ID,
        business_id: BUSINESS_ID,
        status: 'completed',
        outcome: {
          summary: 'Analysis complete',
          artifacts: [],
          metrics: { duration_ms: 1234 },
        },
        error: null,
        audit_trace: [],
        duration: 1234,
      })
    );
    const final = await apiClient.getIntelligence(EXECUTION_ID, BUSINESS_ID);
    expect(final.status).toBe('completed');
    expect(final.output).toBe('Analysis complete');

    // 7. The authoritative recovery read agrees.
    mockFetch.mockResolvedValueOnce(
      mockJson(200, {
        source: 'durable',
        executions: [
          {
            source: 'durable',
            execution_id: EXECUTION_ID,
            business_id: BUSINESS_ID,
            state: 'completed',
            created_at: '2026-01-01T00:00:00Z',
            updated_at: '2026-01-01T00:01:00Z',
            approvals: [
              { approval_id: APPROVAL_ID, outcome: 'granted', decided_at: '2026-01-01T00:00:30Z' },
            ],
          },
        ],
        count: 1,
        limit: 50,
        offset: 0,
        has_more: false,
      })
    );
    const finalList = await apiClient.listRecoveryExecutions(BUSINESS_ID);
    expect(finalList.executions[0].state).toBe('completed');
    expect(finalList.executions[0].approvals?.[0].outcome).toBe('granted');
  });
});
