/**
 * Research Page - The Research Workspace
 *
 * Composed of ResearchInput, ResearchOutput, and ResearchEvidence.
 * All execution state comes from the authoritative durable recovery read,
 * with SSE providing real-time hints that are reconciled on reconnect.
 */

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { VirtualHQ, type ZoneId } from '../components/VirtualHQ/VirtualHQ';
import { Badge } from '../components/primitives';
import { ExecutionDetail } from '../components/ExecutionDetail/ExecutionDetail';
import { ResearchInput } from '../components/Research/ResearchInput';
import { ResearchOutput } from '../components/Research/ResearchOutput';
import { ResearchEvidence } from '../components/Research/ResearchEvidence';
import {
  queryKeys,
  useIntelligence,
  useIntelligenceTrace,
  useRecoveryExecution,
  useRecoveryExecutions,
  useEventStream,
} from '@/api/hooks';
import type { DurableExecution, ExecutionStatus, ResearchEvaluation } from '@/types/api';
import {
  extractToolActivity,
  parseDurableResearchEvidence,
  parseResearchResult,
  parseResearchSteps,
  buildResearchFindings,
  type ParsedDurableEvidence,
  type ResearchResult,
  type ResearchFindings,
  type ResearchStep,
  type ResearchToolActivity,
} from '@/types/research';
import { parseResearchEvaluation } from '@/types/researchEvaluation';
import styles from './Research.module.css';

interface ResearchPageProps {
  businessId: string;
  actorId: string;
}

export const Research: React.FC<ResearchPageProps> = ({ businessId, actorId }) => {
  const [activeZone, setActiveZone] = useState<ZoneId>('research');
  const [selectedExecution, setSelectedExecution] = useState<string | null>(null);
  const [currentExecution, setCurrentExecution] = useState<string | null>(null);
  const queryClient = useQueryClient();

  const handleSubmit = useCallback((executionId: string) => {
    // Show the submitted execution. Do not open the trace overlay: the result
    // must remain readable after submission.
    setCurrentExecution(executionId);
  }, []);

  const handleExecutionSelect = useCallback((executionId: string) => {
    setCurrentExecution(executionId);
  }, []);

  const handleViewTrace = useCallback((executionId: string) => {
    setSelectedExecution(executionId);
  }, []);

  const handleCloseTrace = useCallback(() => {
    setSelectedExecution(null);
  }, []);

  // Authoritative execution list from the durable recovery read. The newest
  // recovered execution is the fallback selection after a reload; an explicit
  // submission or history click takes precedence.
  const { data: recoveryData, isLoading: recoveryLoading, isError: recoveryError } =
    useRecoveryExecutions(businessId);

  const executions: DurableExecution[] = recoveryData?.executions ?? [];
  const recoveredExecutionId = executions[0]?.execution_id ?? null;
  const activeExecutionId = currentExecution ?? recoveredExecutionId;

  const { data: execution, isLoading: executionLoading } = useIntelligence(
    activeExecutionId || '',
    businessId,
    {
      enabled: !!activeExecutionId,
      refetchInterval: (query) => {
        const status = query.state.data?.status;
        return status === 'pending' || status === 'running' ? 2000 : false;
      },
    }
  );

  // Durable execution detail for the active execution: the only read that
  // exposes the persisted research evidence and the real terminal timestamp.
  // A failed read (e.g. a 404 from an older backend) is tolerated silently —
  // the components then fall back to the process-local execution output.
  const { data: recoveryDetail } = useRecoveryExecution(activeExecutionId, businessId);

  const { data: trace } = useIntelligenceTrace(
    activeExecutionId || '',
    businessId,
    { limit: 100 },
    { enabled: !!activeExecutionId }
  );

  // Refresh durable reads when the execution reaches a terminal state: that is
  // the moment the journal writes completed_at and the evidence snapshot, and
  // neither should wait for the next poll to appear.
  useEffect(() => {
    const status = execution?.status;
    const terminal =
      status === 'completed' ||
      status === 'failed' ||
      status === 'cancelled' ||
      status === 'timed_out' ||
      status === 'unknown';
    if (!terminal) return;
    queryClient.invalidateQueries({ queryKey: queryKeys.recoveryExecution(activeExecutionId || '', businessId) });
    queryClient.invalidateQueries({ queryKey: queryKeys.recoveryExecutions(businessId) });
  }, [execution?.status, activeExecutionId, businessId, queryClient]);

  // SSE: reconcile authoritative state on reconnect; SSE is a hint, never truth.
  const { connectionState, retryDelayMs } = useEventStream(businessId, {
    onChainCompleted: () => {},
    onChainFailed: () => {},
    onApprovalRequested: () => {},
    onApprovalApproved: () => {},
    onApprovalDenied: () => {},
    onStateChange: () => {},
    onReconnected: () => {},
    onError: () => {},
  });

  // Parse the real execution output. Source records are recognized only when
  // the whole output is the documented web.search envelope; trace metadata is
  // used only to explain tool activity, never as evidence.
  const parsedResult = useMemo(() => parseResearchResult(execution?.output), [execution?.output]);
  const toolActivity = useMemo(
    () => extractToolActivity(trace?.entries, 'web.search'),
    [trace?.entries]
  );

  // Step states reconstructed from the execution trace (metadata only — step
  // ids, statuses, observation refs; intent text is redacted at capture).
  const steps: ResearchStep[] = useMemo(
    () => parseResearchSteps(trace?.entries),
    [trace?.entries]
  );

  // Durable evidence from the recovery detail read, parsed defensively.
  const durableEvidence = useMemo(
    () => parseDurableResearchEvidence(recoveryDetail?.research_evidence ?? null),
    [recoveryDetail?.research_evidence]
  );

  // Findings: the typed synthesis is resolved against the durable evidence and
  // kept distinct from source records. It is process-local, so a recovered
  // execution without it never gets a regenerated or prose-fallback synthesis.
  const findings = useMemo(
    () =>
      buildResearchFindings(
        parsedResult,
        durableEvidence,
        steps,
        durableEvidence.steps,
        execution?.research_synthesis
      ),
    [parsedResult, durableEvidence, steps, execution?.research_synthesis]
  );

  // Deterministic quality evaluation. Prefer the live object, then fall back to
  // the persisted detail read (which is the only source after a restart). Both
  // are bound to the active execution id and, when durable evidence is present,
  // to its evidence version/count; a mismatch hides the panel.
  const evaluation = useMemo(() => {
    const live = parseResearchEvaluation(
      execution?.research_evaluation,
      activeExecutionId ?? undefined,
      durableEvidence
    );
    if (live) return live;
    return parseResearchEvaluation(
      recoveryDetail?.research_evaluation,
      activeExecutionId ?? undefined,
      durableEvidence
    );
  }, [
    execution?.research_evaluation,
    recoveryDetail?.research_evaluation,
    activeExecutionId,
    durableEvidence,
  ]);

  // Zone stats for VirtualHQ. Counts come from the authoritative recovery list.
  const zoneStats = {
    command: { executions: 0 },
    research: { executions: executions.length },
    media: { executions: 0 },
    development: { executions: 0 },
    meeting: { executions: 0 },
  };

  const isLoading =
    recoveryLoading ||
    executionLoading ||
    execution?.status === 'pending' ||
    execution?.status === 'running';

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div className={styles.headerLeft}>
          <h1 className={styles.logo}>
            <svg width="28" height="28" viewBox="0 0 28 28" fill="none" aria-hidden="true">
              <rect x="3" y="3" width="22" height="22" rx="4" stroke="currentColor" strokeWidth="2" />
              <path d="M9 14h10M14 9v10" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
            </svg>
            NEXUS
          </h1>
          <span className={styles.tagline}>Research Workspace</span>
        </div>
        <div className={styles.headerRight}>
          <div className={styles.statusIndicator} data-state={connectionState} title={`Live updates: ${connectionState}`}>
            <span className={styles.statusDot} data-state={connectionState} />
            <span>
              {connectionState === 'reconnecting' && retryDelayMs > 0
                ? `Reconnecting in ${Math.ceil(retryDelayMs / 1000)}s…`
                : connectionState === 'open'
                  ? 'Live'
                  : connectionState === 'connecting'
                    ? 'Connecting…'
                    : connectionState === 'reconnecting'
                      ? 'Reconnecting…'
                      : connectionState === 'failed'
                        ? 'Disconnected'
                        : 'Offline'}
            </span>
          </div>
        </div>
      </header>

      <main className={styles.main}>
        <aside className={styles.sidebar}>
          <VirtualHQ
            activeZone={activeZone}
            onZoneChange={setActiveZone}
            zoneStats={zoneStats}
          />
        </aside>

        <div className={styles.content}>
          {activeZone === 'research' && (
            <ResearchZone
              businessId={businessId}
              actorId={actorId}
              onSubmit={handleSubmit}
              executions={executions}
              activeExecutionId={activeExecutionId}
              execution={execution}
              parsedResult={parsedResult}
              toolActivity={toolActivity}
              steps={steps}
              findings={findings}
              durableEvidence={durableEvidence}
              isLoading={isLoading}
              recoveryError={recoveryError}
              recoveryDetail={recoveryDetail}
              traceExecutionId={selectedExecution}
              onViewTrace={handleViewTrace}
              onExecutionSelect={handleExecutionSelect}
              onCloseTrace={handleCloseTrace}
              evaluation={evaluation}
            />
          )}

          {activeZone !== 'research' && activeZone !== 'command' && (
            <ZonePreview zoneId={activeZone} />
          )}

          {activeZone === 'command' && (
            <div className={styles.commandPreview}>
              <p className={styles.previewNote}>
                Switch to the Command Center zone to submit objectives and manage executions.
              </p>
            </div>
          )}
        </div>
      </main>
    </div>
  );
};

interface ResearchZoneProps {
  businessId: string;
  actorId: string;
  onSubmit: (executionId: string) => void;
  executions: DurableExecution[];
  activeExecutionId: string | null;
  execution: ExecutionStatus | undefined;
  parsedResult: ResearchResult;
  toolActivity: ResearchToolActivity[];
  steps: ResearchStep[];
  findings: ResearchFindings;
  durableEvidence: ParsedDurableEvidence;
  isLoading: boolean;
  recoveryError: boolean;
  recoveryDetail: DurableExecution | undefined;
  traceExecutionId: string | null;
  onViewTrace: (executionId: string) => void;
  onExecutionSelect: (executionId: string) => void;
  onCloseTrace: () => void;
  evaluation?: ResearchEvaluation;
}

const ResearchZone: React.FC<ResearchZoneProps> = ({
  businessId,
  actorId,
  onSubmit,
  executions,
  activeExecutionId,
  execution,
  parsedResult,
  toolActivity,
  steps,
  findings,
  durableEvidence,
  isLoading,
  recoveryError,
  recoveryDetail,
  traceExecutionId,
  onViewTrace,
  onExecutionSelect,
  onCloseTrace,
  evaluation,
}) => {
  return (
    <div className={styles.zone}>
      <section className={styles.inputSection} aria-labelledby="research-input-heading">
        <h2 id="research-input-heading" className={styles.sectionTitle}>
          Research Query
        </h2>
        <ResearchInput
          businessId={businessId}
          actorId={actorId}
          onSubmit={onSubmit}
        />
      </section>

      {recoveryError && (
        <div className={styles.recoveryError} role="alert">
          Could not load recent research executions. The current result may be stale.
        </div>
      )}

      <div className={styles.resultsGrid}>
        <section className={styles.outputSection} aria-labelledby="research-output-heading">
          <h2 id="research-output-heading" className={styles.sectionTitle}>
            Research Output
            {activeExecutionId && (
              <span className={styles.executionBadge}>
                {activeExecutionId.slice(0, 12)}…
              </span>
            )}
          </h2>
          <ResearchOutput
            output={execution?.output}
            status={execution?.status}
            error={execution?.error}
            durationMs={execution?.duration_ms}
            isLoading={isLoading}
            durableState={recoveryDetail?.state}
            createdAt={recoveryDetail?.created_at}
            completedAt={recoveryDetail?.completed_at}
            steps={steps}
            findings={findings}
            durablePlan={durableEvidence.plan}
            durableSteps={durableEvidence.steps}
            evaluation={evaluation}
          />
        </section>

        <section className={styles.evidenceSection} aria-labelledby="research-evidence-heading">
          <h2 id="research-evidence-heading" className={styles.sectionTitle}>
            Sources & Evidence
          </h2>
          <ResearchEvidence
            result={parsedResult.webSearch}
            durableEvidence={recoveryDetail?.research_evidence}
            parsedEvidence={durableEvidence}
            toolActivity={toolActivity}
            status={execution?.status ?? recoveryDetail?.state}
            executionId={activeExecutionId ?? undefined}
            onViewTrace={onViewTrace}
          />
        </section>
      </div>

      <section className={styles.historySection} aria-labelledby="history-heading">
        <h2 id="history-heading" className={styles.sectionTitle}>
          Recent Research
        </h2>
        <ExecutionHistoryList
          executions={executions}
          currentExecution={activeExecutionId}
          onSelect={onExecutionSelect}
        />
      </section>

      {traceExecutionId && (
        <ExecutionDetail
          executionId={traceExecutionId}
          businessId={businessId}
          onClose={onCloseTrace}
        />
      )}
    </div>
  );
};

const ExecutionHistoryList: React.FC<{
  executions: DurableExecution[];
  currentExecution: string | null;
  onSelect: (executionId: string) => void;
}> = ({ executions, currentExecution, onSelect }) => {
  return (
    <div className={styles.historyList}>
      {executions.length === 0 ? (
        <div className={styles.emptyHistory}>
          <svg width="48" height="48" viewBox="0 0 48 48" fill="none" aria-hidden="true">
            <circle cx="24" cy="24" r="20" stroke="var(--color-surface-border)" strokeWidth="2" />
            <path d="M24 16v16M16 24h16" stroke="var(--color-text-muted)" strokeWidth="2" strokeLinecap="round" />
          </svg>
          <p>No research executions yet</p>
          <span>Submit a research query to see history</span>
        </div>
      ) : (
        <ul className={styles.historyItems}>
          {executions.map((exec) => (
            <li
              key={exec.execution_id}
              className={`${styles.historyItem} ${exec.execution_id === currentExecution ? styles.active : ''}`}
              onClick={() => onSelect(exec.execution_id)}
              role="button"
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  onSelect(exec.execution_id);
                }
              }}
            >
              <span className={styles.historyIndex}>{executions.indexOf(exec) + 1}</span>
              <span className={styles.historyId}>{exec.execution_id.slice(0, 12)}…</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
};

const ZonePreview: React.FC<{ zoneId: ZoneId }> = ({ zoneId }) => {
  const zoneNames: Record<ZoneId, string> = {
    command: 'Command Center',
    research: 'Research',
    media: 'Media',
    development: 'Development',
    meeting: 'Meeting Room',
  };

  const zoneDescriptions: Record<ZoneId, string> = {
    command: 'Primary command interface for objectives and executions',
    research: 'Web research and information gathering agents',
    media: 'Content generation and media processing',
    development: 'Code execution and development tasks',
    meeting: 'Coordination and delegation management',
  };

  return (
    <div className={styles.zonePreview}>
      <div className={styles.previewContent}>
        <h2 className={styles.previewTitle}>{zoneNames[zoneId]}</h2>
        <p className={styles.previewDescription}>{zoneDescriptions[zoneId]}</p>
        <div className={styles.previewStatus}>
          <span className={styles.comingSoon}>Coming Soon</span>
          <p className={styles.previewNote}>
            This zone is not yet implemented. The Research Workspace is the primary
            functional area in this milestone.
          </p>
        </div>
      </div>
    </div>
  );
};
