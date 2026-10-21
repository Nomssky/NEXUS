/**
 * Tests for activity aggregation and approval status mapping.
 */

import { describe, it, expect } from 'vitest';
import { summarizeExecutions, orderRecent } from './activity';
import { approvalContextItems, approvalStatusVariant, approvalStatusLabel } from '../types/api';
import type { Approval, DurableExecution } from '../types/api';

function execution(overrides: Partial<DurableExecution> = {}): DurableExecution {
  return {
    source: 'durable',
    execution_id: 'exec-1',
    business_id: 'biz-1',
    state: 'completed',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

describe('summarizeExecutions', () => {
  it('returns empty for no executions', () => {
    expect(summarizeExecutions([])).toEqual([]);
  });

  it('counts each supported state', () => {
    const summary = summarizeExecutions([
      execution({ state: 'running', execution_id: 'a' }),
      execution({ state: 'accepted', execution_id: 'b' }),
      execution({ state: 'pending_approval', execution_id: 'c' }),
      execution({ state: 'completed', execution_id: 'd' }),
      execution({ state: 'failed', execution_id: 'e' }),
      execution({ state: 'cancelled', execution_id: 'f' }),
      execution({ state: 'unknown', execution_id: 'g' }),
    ]);
    const byKey = Object.fromEntries(summary.map((b) => [b.key, b.count]));
    expect(byKey).toEqual({
      active: 2,
      approval: 1,
      completed: 1,
      failed: 1,
      cancelled: 1,
      unknown: 1,
    });
  });

  it('treats timed_out as failed', () => {
    const summary = summarizeExecutions([execution({ state: 'timed_out' })]);
    expect(summary).toEqual([{ key: 'failed', label: 'Failed', count: 1 }]);
  });

  it('omits empty buckets and keeps stable order', () => {
    const summary = summarizeExecutions([execution({ state: 'completed' })]);
    expect(summary.map((b) => b.key)).toEqual(['completed']);
  });
});

describe('orderRecent', () => {
  it('sorts newest first by updated_at', () => {
    const ordered = orderRecent([
      execution({ execution_id: 'old', updated_at: '2026-01-01T00:00:00Z' }),
      execution({ execution_id: 'new', updated_at: '2026-01-02T00:00:00Z' }),
    ], 10);
    expect(ordered.map((e) => e.execution_id)).toEqual(['new', 'old']);
  });

  it('respects the limit', () => {
    const list = Array.from({ length: 12 }, (_, i) =>
      execution({ execution_id: `e${i}`, updated_at: `2026-01-${String(i + 1).padStart(2, '0')}T00:00:00Z` })
    );
    const ordered = orderRecent(list, 5);
    expect(ordered).toHaveLength(5);
    expect(ordered[0].execution_id).toBe('e11');
  });
});

describe('approvalStatusVariant / approvalStatusLabel', () => {
  it('maps PENDING to pending_approval', () => {
    expect(approvalStatusVariant('PENDING')).toBe('pending_approval');
    expect(approvalStatusLabel('PENDING')).toBe('Pending');
  });

  it('maps APPROVED to completed', () => {
    expect(approvalStatusVariant('APPROVED')).toBe('completed');
    expect(approvalStatusLabel('APPROVED')).toBe('Approved');
  });

  it('maps DENIED to failed', () => {
    expect(approvalStatusVariant('DENIED')).toBe('failed');
    expect(approvalStatusLabel('DENIED')).toBe('Denied');
  });

  it('maps EXPIRED to warning', () => {
    expect(approvalStatusVariant('EXPIRED')).toBe('warning');
    expect(approvalStatusLabel('EXPIRED')).toBe('Expired');
  });

  it('falls back to default for unknown statuses', () => {
    expect(approvalStatusVariant('WEIRD')).toBe('default');
    expect(approvalStatusLabel('WEIRD')).toBe('WEIRD');
  });
});

describe('approvalContextItems', () => {
  const base: Approval = {
    entity_id: 'exec-1',
    status: 'PENDING',
    requested_action: 'execute_tool',
    requester_id: 'nx:human:alice',
    approver_type: 'any',
    created_at: '2026-01-01T00:00:00Z',
    self_approval_prohibited: true,
    delegation_allowed: false,
    audit_ref: 'exec-1',
  };

  it('returns all present context fields in order', () => {
    const approval: Approval = { ...base, scope: 'core', authority_context: 'execute requires approval', policy_ref: 'require-approval' };
    const items = approvalContextItems(approval);
    expect(items).toEqual([
      { label: 'Scope', value: 'core' },
      { label: 'Authority context', value: 'execute requires approval' },
      { label: 'Policy ref', value: 'require-approval' },
    ]);
  });

  it('returns only scope when other fields are absent', () => {
    const approval: Approval = { ...base, scope: 'biz-1' };
    expect(approvalContextItems(approval)).toEqual([
      { label: 'Scope', value: 'biz-1' },
    ]);
  });

  it('omits empty strings', () => {
    const approval: Approval = { ...base, scope: '', authority_context: '', policy_ref: '' };
    expect(approvalContextItems(approval)).toEqual([]);
  });

  it('returns an empty array when nothing is populated', () => {
    expect(approvalContextItems(base)).toEqual([]);
  });
});
