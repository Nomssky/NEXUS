/**
 * ActivityPanel - Real execution activity from the authoritative durable
 * recovery read.
 *
 * Aggregates the durable execution list into per-state counts and renders the
 * most recent executions with inspectable status. This is the source of truth
 * for "what is happening" — it is derived from the backend's durable records,
 * never from fabricated or session-local placeholders. When no executions
 * exist, a truthful idle state is shown.
 */

import React from 'react';
import { Card, Badge, getStatusVariant } from '../primitives';
import { useRecoveryExecutions } from '../../api/hooks';
import { summarizeExecutions, orderRecent } from '../../api/activity';
import type { DurableExecution } from '../../types/api';
import styles from './ActivityPanel.module.css';

interface ActivityPanelProps {
  businessId: string;
  onSelect?: (executionId: string) => void;
}

const RECENT_LIMIT = 8;

const BUCKET_VARIANT: Record<string, React.ComponentProps<typeof Badge>['variant']> = {
  active: 'running',
  approval: 'pending_approval',
  completed: 'completed',
  failed: 'failed',
  cancelled: 'cancelled',
  unknown: 'unknown',
};

export const ActivityPanel: React.FC<ActivityPanelProps> = ({ businessId, onSelect }) => {
  const { data, isLoading, isError, error, refetch } = useRecoveryExecutions(businessId);
  const executions = data?.executions ?? [];

  if (isLoading) {
    return (
      <Card variant="default" padding="md">
        <div className={styles.loading}>
          <div className={styles.skeleton} style={{ width: '45%' }} />
          <div className={styles.skeleton} style={{ width: '70%' }} />
          <div className={styles.skeleton} style={{ width: '55%' }} />
        </div>
      </Card>
    );
  }

  if (isError) {
    return (
      <Card variant="default" padding="md">
        <div className={styles.error}>
          <svg width="20" height="20" viewBox="0 0 20 20" fill="none" aria-hidden="true">
            <circle cx="10" cy="10" r="8" stroke="currentColor" strokeWidth="1.5" />
            <path d="M10 6v4M10 14v.01" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
          </svg>
          <p>Activity unavailable</p>
          <span>{error instanceof Error ? error.message : 'Unknown error'}</span>
          <button className={styles.retryButton} onClick={() => refetch()}>Retry</button>
        </div>
      </Card>
    );
  }

  if (executions.length === 0) {
    return (
      <Card variant="default" padding="md">
        <div className={styles.empty}>
          <svg width="40" height="40" viewBox="0 0 40 40" fill="none" aria-hidden="true">
            <circle cx="20" cy="20" r="16" stroke="var(--color-surface-border)" strokeWidth="2" />
            <path d="M14 20h12M20 14v12" stroke="var(--color-text-muted)" strokeWidth="2" strokeLinecap="round" />
          </svg>
          <p>No recorded executions yet</p>
          <span>Submit an objective with Ask NEXUS and its activity will appear here</span>
        </div>
      </Card>
    );
  }

  const counts = summarizeExecutions(executions);
  const recent = orderRecent(executions, RECENT_LIMIT);

  return (
    <Card variant="default" padding="md">
      <div className={styles.summary}>
        {counts.map((bucket) => (
          <div key={bucket.key} className={styles.summaryItem} title={`${bucket.count} ${bucket.label.toLowerCase()}`}>
            <Badge variant={BUCKET_VARIANT[bucket.key]} size="sm" dot={bucket.key === 'active'}>
              {bucket.count}
            </Badge>
            <span className={styles.summaryLabel}>{bucket.label}</span>
          </div>
        ))}
      </div>

      <div className={styles.list} role="list">
        {recent.map((execution) => (
          <ActivityRow
            key={execution.execution_id}
            execution={execution}
            onClick={() => onSelect?.(execution.execution_id)}
          />
        ))}
      </div>

      {data && data.has_more && (
        <div className={styles.more}>
          <span>{data.count} shown · more available</span>
        </div>
      )}
    </Card>
  );
};

const ActivityRow: React.FC<{
  execution: DurableExecution;
  onClick?: () => void;
}> = ({ execution, onClick }) => {
  const variant = getStatusVariant(execution.state);
  const isApproval = execution.state === 'pending_approval';
  const hasApprovalHistory = (execution.approvals?.length ?? 0) > 0;

  return (
    <button
      type="button"
      className={styles.row}
      onClick={onClick}
      aria-label={`Inspect execution ${execution.execution_id}`}
    >
      <span className={styles.rowId}>{execution.execution_id.slice(0, 14)}…</span>
      <span className={styles.rowMeta}>
        <time dateTime={execution.updated_at}>
          {new Date(execution.updated_at).toLocaleTimeString()}
        </time>
        {hasApprovalHistory && (
          <span className={styles.rowApprovals} title="Has recorded approval history">
            {execution.approvals!.length} approval{execution.approvals!.length === 1 ? '' : 's'}
          </span>
        )}
      </span>
      <Badge variant={variant} size="sm" dot={!isApproval && variant === 'running'}>
        {execution.state.replace(/_/g, ' ')}
      </Badge>
    </button>
  );
};
