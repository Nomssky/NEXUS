/**
 * Tests for activity aggregation and approval status mapping.
 */

import { describe, it, expect } from 'vitest';
import { summarizeExecutions, orderRecent } from './activity';
import { approvalStatusVariant, approvalStatusLabel } from '../types/api';
import type { DurableExecution } from '../types/api';

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
