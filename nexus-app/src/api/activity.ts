/**
 * Pure helpers for aggregating authoritative execution state.
 * Extracted from the UI so the aggregation is deterministically testable.
 */

import type { DurableExecution } from '../types/api';

export interface ActivityBucket {
  key: 'active' | 'approval' | 'completed' | 'failed' | 'cancelled' | 'unknown';
  label: string;
  count: number;
}

const MATCHERS: Array<{ key: ActivityBucket['key']; label: string; match: (state: string) => boolean }> = [
  { key: 'active', label: 'Active', match: (s) => s === 'running' || s === 'accepted' },
  { key: 'approval', label: 'Awaiting approval', match: (s) => s === 'pending_approval' },
  { key: 'completed', label: 'Completed', match: (s) => s === 'completed' },
  { key: 'failed', label: 'Failed', match: (s) => s === 'failed' || s === 'timed_out' },
  { key: 'cancelled', label: 'Cancelled', match: (s) => s === 'cancelled' },
  { key: 'unknown', label: 'Unknown', match: (s) => s === 'unknown' },
];

/**
 * Aggregate a durable execution list into per-state counts, preserving a
 * stable order and omitting empty buckets. Unknown states are counted as
 * 'unknown' rather than silently dropped, so the summary is truthful.
 */
export function summarizeExecutions(executions: DurableExecution[]): ActivityBucket[] {
  return MATCHERS.map((m) => ({
    key: m.key,
    label: m.label,
    count: executions.filter((e) => m.match(e.state)).length,
  })).filter((b) => b.count > 0);
}

/**
 * Most-recent-first ordering for the activity list, by updated_at, with the
 * execution id as a deterministic tie-break.
 */
export function orderRecent(executions: DurableExecution[], limit: number): DurableExecution[] {
  return [...executions]
    .sort((a, b) => {
      const at = new Date(a.updated_at).getTime();
      const bt = new Date(b.updated_at).getTime();
      if (at !== bt) return bt - at;
      return a.execution_id < b.execution_id ? 1 : a.execution_id > b.execution_id ? -1 : 0;
    })
    .slice(0, limit);
}
