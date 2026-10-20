/**
 * Meeting Page — The Meeting Room / Coordination Workspace.
 *
 * Real coordination workflow over the existing governed execution path:
 *   1. Submit a coordination objective (standard intelligence schema).
 *   2. Poll the authoritative execution status.
 *   3. Inspect the trace: proposal → governance → dispatch → attempts →
 *      outcomes → terminal, with delegation evidence where it exists.
 *   4. Show the aggregate outcome and any real delegated tasks.
 *
 * There is no separate coordination endpoint, no client-side orchestration,
 * and no fabricated participants. Delegation is decided by the runtime.
 */

import React, { useState, useCallback, useMemo } from 'react';
import { VirtualHQ, type ZoneId } from '../components/VirtualHQ/VirtualHQ';
import { ExecutionDetail } from '../components/ExecutionDetail/ExecutionDetail';
import { ApprovalsPanel } from '../components/Approvals/ApprovalsPanel';
import { MeetingInput } from '../components/Meeting/MeetingInput';
import { CoordinationOutcome } from '../components/Meeting/CoordinationOutcome';
import { useIntelligence, useIntelligenceTrace, useAgents, useCancelIntelligence } from '../api/hooks';
import styles from './Meeting.module.css';

interface MeetingPageProps {
  businessId: string;
  actorId: string;
}

export const Meeting: React.FC<MeetingPageProps> = ({ businessId, actorId }) => {
  const [activeZone, setActiveZone] = useState<ZoneId>('meeting');
  const [selectedExecution, setSelectedExecution] = useState<string | null>(null);
  const [recentExecutions, setRecentExecutions] = useState<string[]>([]);
  const [currentExecution, setCurrentExecution] = useState<string | null>(null);

  const handleSubmit = useCallback((executionId: string) => {
    setRecentExecutions((prev) => [executionId, ...prev.slice(0, 9)]);
    setCurrentExecution(executionId);
    setSelectedExecution(executionId);
  }, []);

  const handleExecutionSelect = useCallback((executionId: string) => {
    setSelectedExecution(executionId);
    setCurrentExecution(executionId);
  }, []);

  const handleCloseTrace = useCallback(() => setSelectedExecution(null), []);

  const latestExecutionId = currentExecution || recentExecutions[0] || '';

  const { data: execution } = useIntelligence(latestExecutionId, businessId, {
    enabled: !!latestExecutionId,
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === 'pending' || status === 'running' ? 2000 : false;
    },
  });

  const { data: trace } = useIntelligenceTrace(
    latestExecutionId,
    businessId,
    { limit: 200 },
    { enabled: !!latestExecutionId }
  );

  // Real agents from the registry (only used to show what the runtime has to
  // coordinate with; never presented as live presence).
  const { data: agentsData } = useAgents(businessId, { enabled: !!businessId });

  const cancelMutation = useCancelIntelligence();

  const isTerminal = execution != null && !['pending', 'running'].includes(execution.status);
  const canCancel = execution?.status === 'pending' || execution?.status === 'running';

  const handleCancel = useCallback(() => {
    if (!canCancel || !currentExecution) return;
    cancelMutation.mutate({ id: currentExecution, businessId });
  }, [canCancel, currentExecution, businessId, cancelMutation]);

  const zoneStats = {
    command: { agents: undefined, executions: undefined },
    research: { agents: undefined, executions: undefined },
    media: { agents: undefined, executions: undefined },
    development: { agents: undefined, executions: undefined },
    meeting: {
      // The agent count comes from the registry query when loaded — never a
      // hardcoded live-presence number. Undefined until the query resolves.
      agents: agentsData?.agents?.length,
      executions: recentExecutions.length > 0 ? recentExecutions.length : undefined,
    },
  };

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div className={styles.headerLeft}>
          <h1 className={styles.logo}>
            <svg width="28" height="28" viewBox="0 0 28 28" fill="none" aria-hidden="true">
              <circle cx="9" cy="9" r="4" stroke="currentColor" strokeWidth="1.5" />
              <circle cx="19" cy="9" r="4" stroke="currentColor" strokeWidth="1.5" />
              <circle cx="9" cy="19" r="4" stroke="currentColor" strokeWidth="1.5" />
              <circle cx="19" cy="19" r="4" stroke="currentColor" strokeWidth="1.5" />
            </svg>
            NEXUS
          </h1>
          <span className={styles.tagline}>Meeting Room</span>
        </div>
        <div className={styles.headerRight}>
          <div className={styles.statusIndicator}>
            <span className={styles.statusDot} />
            <span>Connected</span>
          </div>
        </div>
      </header>

      <main className={styles.main}>
        <aside className={styles.sidebar}>
          <VirtualHQ activeZone={activeZone} onZoneChange={setActiveZone} zoneStats={zoneStats} />
        </aside>

        <div className={styles.content}>
          {activeZone === 'meeting' && (
            <div className={styles.zone}>
              <section className={styles.inputSection} aria-labelledby="coord-input-heading">
                <h2 id="coord-input-heading" className={styles.sectionTitle}>
                  Coordination Objective
                </h2>
                <p className={styles.sectionNote}>
                  NEXUS executes this through the standard governed path. The
                  runtime decides whether to delegate; the Meeting Room does not
                  select agents or orchestrate on the client. Delegation depth,
                  fan-out, and concurrency are bounded by the runtime.
                </p>
                <MeetingInput businessId={businessId} actorId={actorId} onSubmit={handleSubmit} />
              </section>

              {currentExecution && (
                <>
                  <section className={styles.outcomeSection} aria-labelledby="coord-outcome-heading">
                    <h2 id="coord-outcome-heading" className={styles.sectionTitle}>
                      Coordination
                    </h2>
                    <CoordinationOutcome
                      status={execution?.status}
                      entries={trace?.entries}
                      executionId={currentExecution}
                    />
                    <div className={styles.actions}>
                      <button
                        className={styles.actionButton}
                        onClick={() => setSelectedExecution(currentExecution)}
                      >
                        View full trace
                      </button>
                      {canCancel && (
                        <button
                          className={`${styles.actionButton} ${styles.danger}`}
                          onClick={handleCancel}
                          disabled={cancelMutation.isPending}
                        >
                          {cancelMutation.isPending ? 'Requesting…' : 'Request cancellation'}
                        </button>
                      )}
                      {isTerminal && execution?.status === 'cancelled' && (
                        <span className={styles.cancelNote}>Cancellation confirmed by the backend.</span>
                      )}
                    </div>
                    {cancelMutation.isError && (
                      <p className={styles.errorText} role="alert">
                        Cancellation request failed: {cancelMutation.error instanceof Error ? cancelMutation.error.message : 'unknown error'}
                      </p>
                    )}
                  </section>

                  <section className={styles.traceSection} aria-labelledby="coord-trace-heading">
                    <h2 id="coord-trace-heading" className={styles.sectionTitle}>
                      Execution Timeline
                    </h2>
                    <TraceTimeline entries={trace?.entries} />
                  </section>
                </>
              )}

              <section className={styles.approvalsSection} aria-labelledby="coord-approvals-heading">
                <h2 id="coord-approvals-heading" className={styles.sectionTitle}>
                  Pending Approvals
                </h2>
                <ApprovalsPanel businessId={businessId} />
              </section>

              <section className={styles.historySection} aria-labelledby="coord-history-heading">
                <h2 id="coord-history-heading" className={styles.sectionTitle}>
                  Recent Coordinations
                </h2>
                <HistoryList
                  executionIds={recentExecutions}
                  currentExecution={currentExecution}
                  onSelect={handleExecutionSelect}
                />
              </section>
            </div>
          )}

          {activeZone === 'command' && (
            <div className={styles.preview}>
              <p className={styles.previewNote}>Switch to the Command Center for direct objectives.</p>
            </div>
          )}
          {activeZone === 'research' && (
            <div className={styles.preview}>
              <p className={styles.previewNote}>Switch to the Research Workspace for web research.</p>
            </div>
          )}
          {activeZone === 'development' && (
            <div className={styles.preview}>
              <p className={styles.previewNote}>Switch to the Development Workspace for repository context.</p>
            </div>
          )}
          {activeZone === 'media' && (
            <div className={styles.preview}>
              <p className={styles.previewNote}>Switch to the Media Workspace for creative preparation.</p>
            </div>
          )}
        </div>
      </main>

      {selectedExecution && (
        <ExecutionDetail
          executionId={selectedExecution}
          businessId={businessId}
          onClose={handleCloseTrace}
        />
      )}
    </div>
  );
};

const TraceTimeline: React.FC<{ entries?: { seq: number; type: string; source: string; timestamp: string; data?: Record<string, string> }[] }> = ({ entries }) => {
  const shown = useMemo(() => (entries || []).slice(0, 50), [entries]);

  if (shown.length === 0) {
    return (
      <div className={styles.emptyTimeline}>
        <p>No trace entries yet</p>
        <span>The trace is captured at publish time; it appears once the execution emits events.</span>
      </div>
    );
  }

  return (
    <ol className={styles.timeline}>
      {shown.map((e) => (
        <li key={`${e.seq}-${e.type}`} className={styles.timelineItem}>
          <span className={styles.timelineSeq}>#{e.seq}</span>
          <span className={styles.timelineType}>{e.type}</span>
          <span className={styles.timelineSource}>{e.source}</span>
        </li>
      ))}
      {(entries?.length || 0) > shown.length && (
        <li className={styles.timelineMore}>
          Showing first {shown.length} of {entries?.length} entries — open the full trace for all.
        </li>
      )}
    </ol>
  );
};

const HistoryList: React.FC<{
  executionIds: string[];
  currentExecution: string | null;
  onSelect: (id: string) => void;
}> = ({ executionIds, currentExecution, onSelect }) => {
  if (executionIds.length === 0) {
    return (
      <div className={styles.emptyHistory}>
        <p>No coordinations yet</p>
        <span>History is client-side only for this session; nothing is persisted across reloads.</span>
      </div>
    );
  }
  return (
    <ul className={styles.historyItems} aria-label="Recent coordinations">
      {executionIds.map((id, idx) => (
        <li
          key={id}
          className={`${styles.historyItem} ${id === currentExecution ? styles.active : ''}`}
          onClick={() => onSelect(id)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault();
              onSelect(id);
            }
          }}
          role="button"
          tabIndex={0}
          aria-label={`Coordination ${id}`}
        >
          <span className={styles.historyIndex}>{idx + 1}</span>
          <span className={styles.historyId}>{id.slice(0, 12)}…</span>
        </li>
      ))}
    </ul>
  );
};