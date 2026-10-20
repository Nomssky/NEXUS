/**
 * ExecutionDetail - Shows execution trace with proper evidence hierarchy
 * Preserves distinction between proposal, governance, dispatch, attempts, outcome
 */

import React, { useState, useCallback } from 'react';
import { Card, CardHeader, CardContent, Badge, getStatusVariant } from '../primitives';
import { useIntelligenceTrace } from '../../api/hooks';
import type { TraceEntry } from '../../types/api';
import styles from './ExecutionDetail.module.css';

interface ExecutionDetailProps {
  executionId: string;
  businessId: string;
  onClose: () => void;
}

export const ExecutionDetail: React.FC<ExecutionDetailProps> = ({
  executionId,
  businessId,
  onClose,
}) => {
  const [params, setParams] = useState<{ limit?: number; after_seq?: number }>({ limit: 100 });
  const [selectedEntry, setSelectedEntry] = useState<TraceEntry | null>(null);

  const { data: trace, isLoading, isError, error, isFetching } =
    useIntelligenceTrace(executionId, businessId, params);

  const hasNextPage = trace?.capture.has_more ?? false;
  const isFetchingNextPage = isFetching && params.after_seq !== undefined;

  const loadMore = useCallback(() => {
    if (hasNextPage && trace?.capture.last_seq) {
      setParams((prev) => ({
        ...prev,
        after_seq: trace.capture.last_seq,
      }));
    }
  }, [hasNextPage, trace?.capture.last_seq]);

  if (isLoading) {
    return (
      <div className={styles.overlay} onClick={onClose}>
        <div className={styles.panel} onClick={(e) => e.stopPropagation()}>
          <div className={styles.loading}>Loading trace…</div>
        </div>
      </div>
    );
  }

  if (isError) {
    return (
      <div className={styles.overlay} onClick={onClose}>
        <div className={styles.panel} onClick={(e) => e.stopPropagation()}>
          <div className={styles.error}>
            <h3>Failed to load trace</h3>
            <p>{error instanceof Error ? error.message : 'Unknown error'}</p>
            <button className={styles.closeButton} onClick={onClose}>
              Close
            </button>
          </div>
        </div>
      </div>
    );
  }

  if (!trace) {
    return null;
  }

  // Group entries by evidence class for better visualization
  const evidenceClasses = [
    { key: 'proposal', label: 'Proposals', types: ['action.proposed', 'action.rejected'] },
    { key: 'governance', label: 'Governance', types: ['governance.decided', 'approval.requested', 'approval.approved', 'approval.denied'] },
    { key: 'dispatch', label: 'Dispatch', types: ['tool.invocation.started', 'tool.invocation.rejected'] },
    { key: 'attempts', label: 'Physical Attempts', types: ['tool.attempt.started', 'tool.attempt.completed', 'tool.retry.scheduled'] },
    { key: 'outcomes', label: 'Outcomes', types: ['tool.invocation.completed', 'tool.invocation.failed', 'tool.invocation.unknown'] },
    { key: 'terminal', label: 'Terminal', types: ['objective.completed', 'chain.started', 'chain.completed', 'chain.failed'] },
    { key: 'other', label: 'Other', types: [] },
  ];

  const groupedEntries = trace.entries.reduce(
    (acc, entry) => {
      const group = evidenceClasses.find((g) => g.types.includes(entry.type as any)) || evidenceClasses[evidenceClasses.length - 1];
      if (!acc[group.key]) acc[group.key] = [];
      acc[group.key].push(entry);
      return acc;
    },
    {} as Record<string, TraceEntry[]>
  );

  const formatTimestamp = (ts: string) => {
    try {
      return new Date(ts).toLocaleTimeString(undefined, {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        fractionalSecondDigits: 3,
      });
    } catch {
      return ts;
    }
  };

  const formatData = (data?: Record<string, string>) => {
    if (!data || Object.keys(data).length === 0) return null;
    return Object.entries(data)
      .map(([k, v]) => `${k}: ${v}`)
      .join('\n');
  };

  return (
    <div className={styles.overlay} onClick={onClose} role="dialog" aria-modal="true" aria-labelledby="trace-title">
      <div className={styles.panel} onClick={(e) => e.stopPropagation()}>
        <header className={styles.header}>
          <div className={styles.headerLeft}>
            <h2 id="trace-title" className={styles.title}>
              Execution Trace
              <span className={styles.executionId}>{executionId}</span>
            </h2>
            <div className={styles.meta}>
              <Badge variant={getStatusVariant(trace.status)} size="md">
                {trace.status}
              </Badge>
              <span className={styles.durability}>{trace.durability}</span>
              <span className={styles.captureInfo}>
                {trace.capture.entries} entries
                {trace.capture.dropped > 0 && ` · ${trace.capture.dropped} dropped`}
              </span>
            </div>
          </div>
          <button className={styles.closeButton} onClick={onClose} aria-label="Close trace">
            <svg width="20" height="20" viewBox="0 0 20 20" fill="none" aria-hidden="true">
              <path d="M6 6l8 8M14 6l-8 8" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
            </svg>
          </button>
        </header>

        <div className={styles.content}>
          {evidenceClasses.map((group) => {
            const entries = groupedEntries[group.key] || [];
            if (entries.length === 0) return null;

            return (
              <section key={group.key} className={styles.section}>
                <h3 className={styles.sectionTitle}>
                  {group.label}
                  <span className={styles.sectionCount}>{entries.length}</span>
                </h3>
                <div className={styles.entryList}>
                  {entries.map((entry) => (
                    <div
                      key={entry.event_id}
                      className={`${styles.entry} ${entry.type === 'tool.invocation.unknown' ? styles.unknown : ''}`}
                      onClick={() => setSelectedEntry(entry)}
                    >
                      <div className={styles.entryHeader}>
                        <span className={styles.entrySeq}>#{entry.seq}</span>
                        <span className={styles.entryType}>{entry.type}</span>
                        <span className={styles.entrySource}>{entry.source}</span>
                        <time className={styles.entryTime} dateTime={entry.timestamp}>
                          {formatTimestamp(entry.timestamp)}
                        </time>
                      </div>
                      {entry.redacted_keys && entry.redacted_keys.length > 0 && (
                        <div className={styles.redacted}>
                          Redacted: {entry.redacted_keys.join(', ')}
                        </div>
                      )}
                      {entry.data_unparseable && (
                        <div className={styles.unparseable}>
                          Data unparseable
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              </section>
            );
          })}

          {hasNextPage && (
            <div className={styles.loadMore}>
              <button onClick={loadMore} disabled={isFetchingNextPage} className={styles.loadMoreButton}>
                {isFetchingNextPage ? 'Loading…' : 'Load more entries'}
              </button>
            </div>
          )}
        </div>
      </div>

      {selectedEntry && (
        <EntryDetailModal entry={selectedEntry} onClose={() => setSelectedEntry(null)} />
      )}
    </div>
  );
};

// Modal for entry detail
const EntryDetailModal: React.FC<{ entry: TraceEntry; onClose: () => void }> = ({ entry, onClose }) => {
  const formatData = (data?: Record<string, string>) => {
    if (!data || Object.keys(data).length === 0) return 'No payload data';
    return Object.entries(data)
      .map(([k, v]) => `${k}: ${v}`)
      .join('\n');
  };

  return (
    <div className={styles.modalOverlay} onClick={onClose} role="dialog" aria-modal="true" aria-labelledby="entry-detail-title">
      <div className={styles.modal} onClick={(e) => e.stopPropagation()}>
        <header className={styles.modalHeader}>
          <h3 id="entry-detail-title" className={styles.modalTitle}>
            Event Details
            <span className={styles.modalSeq}>#{entry.seq}</span>
          </h3>
          <button onClick={onClose} className={styles.modalClose} aria-label="Close">
            <svg width="20" height="20" viewBox="0 0 20 20" fill="none" aria-hidden="true">
              <path d="M6 6l8 8M14 6l-8 8" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
            </svg>
          </button>
        </header>

        <div className={styles.modalContent}>
          <div className={styles.modalField}>
            <label>Type</label>
            <code>{entry.type}</code>
          </div>
          <div className={styles.modalField}>
            <label>Source</label>
            <code>{entry.source}</code>
          </div>
          <div className={styles.modalField}>
            <label>Timestamp</label>
            <code>{new Date(entry.timestamp).toISOString()}</code>
          </div>
          <div className={styles.modalField}>
            <label>Event ID</label>
            <code>{entry.event_id}</code>
          </div>
          <div className={styles.modalField}>
            <label>Sequence</label>
            <code>{entry.seq}</code>
          </div>

          {entry.data && Object.keys(entry.data).length > 0 && (
            <div className={styles.modalField}>
              <label>Payload</label>
              <pre>{formatData(entry.data)}</pre>
            </div>
          )}

          {entry.redacted_keys && entry.redacted_keys.length > 0 && (
            <div className={styles.modalField}>
              <label>Redacted Keys</label>
              <ul>
                {entry.redacted_keys.map((key) => (
                  <li key={key}><code>{key}</code></li>
                ))}
              </ul>
            </div>
          )}

          {entry.data_unparseable && (
            <div className={styles.modalField}>
              <label>Status</label>
              <span className={styles.unparseable}>Data was unparseable at capture time</span>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};