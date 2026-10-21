/**
 * ExecutionList - Shows recent executions with their status
 */

import React from 'react';
import { Card, CardContent, Badge, getStatusVariant } from '../primitives';
import { useIntelligence } from '../../api/hooks';
import styles from './ExecutionList.module.css';

interface ExecutionListProps {
  businessId: string;
  executionIds: string[];
  onSelect?: (executionId: string) => void;
}

export const ExecutionList: React.FC<ExecutionListProps> = ({
  businessId,
  executionIds,
  onSelect,
}) => {
  // Fetch status for each execution
  const queries = executionIds.map((id) =>
    useIntelligence(id, businessId, {
      enabled: !!id,
      staleTime: 5000,
    })
  );

  const isLoading = queries.some((q) => q.isLoading);
  const hasError = queries.some((q) => q.isError);

  if (executionIds.length === 0) {
    return (
      <div className={styles.empty}>
        <svg width="48" height="48" viewBox="0 0 48 48" fill="none" aria-hidden="true">
          <circle cx="24" cy="24" r="20" stroke="var(--color-surface-border)" strokeWidth="2" />
          <path d="M24 16v16M16 24h16" stroke="var(--color-text-muted)" strokeWidth="2" strokeLinecap="round" />
        </svg>
        <p>No executions yet</p>
        <span>Submit an objective to see executions here</span>
      </div>
    );
  }

  return (
    <div className={styles.list}>
      {queries.map((query, index) => {
        const execution = query.data;
        const execId = executionIds[index];

        if (query.isLoading || !execution) {
          return (
            <div key={execId} className={styles.itemSkeleton}>
              <div className={styles.skeletonRow}>
                <div className={styles.skeletonText} style={{ width: '60%' }} />
                <div className={styles.skeletonBadge} />
              </div>
            </div>
          );
        }

        const variant = getStatusVariant(execution.status);
        const isActive = execution.status === 'pending' || execution.status === 'running';

        return (
          <div
            key={execId}
            className={`${styles.item} ${isActive ? styles.active : ''}`}
            onClick={() => onSelect?.(execId)}
            role="button"
            tabIndex={0}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                onSelect?.(execId);
              }
            }}
          >
            <div className={styles.itemMain}>
              <div className={styles.itemInfo}>
                <div className={styles.itemId}>{execId.slice(0, 12)}…</div>
                <div className={styles.itemMeta}>
                  {execution.created_at ? (
                    <time dateTime={execution.created_at}>
                      {new Date(execution.created_at).toLocaleString()}
                    </time>
                  ) : (
                    <span>Time unavailable</span>
                  )}
                  {execution.duration_ms && (
                    <>
                      <span className={styles.separator} aria-hidden="true">·</span>
                      <span>{Math.round(execution.duration_ms / 1000)}s</span>
                    </>
                  )}
                </div>
              </div>
              <Badge variant={variant} size="md" dot={isActive}>
                {execution.status}
              </Badge>
            </div>
            {execution.error && (
              <div className={styles.itemError}>
                <svg width="14" height="14" viewBox="0 0 14 14" fill="none" aria-hidden="true">
                  <circle cx="7" cy="7" r="6" stroke="currentColor" strokeWidth="1.5" />
                  <path d="M7 4v4M7 10v.01" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
                </svg>
                <span>{execution.error.message}</span>
              </div>
            )}
          </div>
        );
      })}

      {hasError && (
        <div className={styles.error}>
          Failed to load some executions
        </div>
      )}
    </div>
  );
};