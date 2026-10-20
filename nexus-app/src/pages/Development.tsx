/**
 * Development Page - The Development Workspace
 * Composed of DevelopmentInput, DevelopmentOutput, RepositoryContext, and trace inspection
 */

import React, { useState, useCallback, useEffect, useMemo } from 'react';
import { VirtualHQ, type ZoneId } from '../components/VirtualHQ/VirtualHQ';
import { ExecutionDetail } from '../components/ExecutionDetail/ExecutionDetail';
import { DevelopmentInput } from '../components/Development/DevelopmentInput';
import { DevelopmentOutput } from '../components/Development/DevelopmentOutput';
import { RepositoryContext } from '../components/Development/RepositoryContext';
import { useIntelligence, useIntelligenceTrace } from '../api/hooks';
import styles from './Development.module.css';

interface DevelopmentPageProps {
  businessId: string;
  actorId: string;
}

export const Development: React.FC<DevelopmentPageProps> = ({ businessId, actorId }) => {
  const [activeZone, setActiveZone] = useState<ZoneId>('development');
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

  const handleCloseTrace = useCallback(() => {
    setSelectedExecution(null);
  }, []);

  // Get the most recent execution that has development tool results
  const latestExecutionId = recentExecutions[0] || currentExecution;
  const { data: execution } = useIntelligence(latestExecutionId || '', businessId, {
    enabled: !!latestExecutionId,
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === 'pending' || status === 'running' ? 2000 : false;
    },
  });

  const { data: trace } = useIntelligenceTrace(latestExecutionId || '', businessId, {
    limit: 100,
  }, {
    enabled: !!latestExecutionId,
  });

  // Extract git tool results from execution output or trace
  const gitResult = useMemo((): { output: string; truncated?: string; repo?: string } | undefined => {
    if (!execution?.output) return undefined;
    try {
      // The git tool result is embedded in the execution output
      const parsed = JSON.parse(execution.output);
      if (parsed.output && (parsed.repo || parsed.truncated !== undefined)) {
        return parsed as { output: string; truncated?: string; repo?: string };
      }
    } catch {
      // Not JSON, try to extract from trace
    }

    // Try to find tool.invocation.completed with git in trace
    if (trace?.entries) {
      for (const entry of trace.entries) {
        if (entry.type === 'tool.invocation.completed' && entry.data?.tool_id === 'git') {
          try {
            const data = JSON.parse(JSON.stringify(entry.data));
            if (data.output && (data.repo || data.truncated !== undefined)) {
              return { output: data.output, truncated: data.truncated, repo: data.repo };
            }
          } catch {
            // Ignore parse errors
          }
        }
      }
    }

    return undefined;
  }, [execution, trace]);

  // Detect which git operation was used from trace
  const gitOperation = useMemo((): 'status' | 'diff' | 'log' => {
    if (!trace?.entries) return 'status';
    for (const entry of trace.entries) {
      if (entry.type === 'tool.invocation.started' && entry.data?.tool_id === 'git') {
        const op = entry.data.operation as 'status' | 'diff' | 'log' | undefined;
        if (op) return op;
      }
    }
    return 'status';
  }, [trace]);

  // Zone stats for VirtualHQ
  const zoneStats = {
    command: { executions: recentExecutions.length },
    research: { executions: recentExecutions.length },
    development: { executions: recentExecutions.length },
    media: { executions: 0 },
    meeting: { executions: 0 },
  };

  const isLoading = execution?.status === 'pending' || execution?.status === 'running';

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
          <span className={styles.tagline}>Development Workspace</span>
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
          <VirtualHQ
            activeZone={activeZone}
            onZoneChange={setActiveZone}
            zoneStats={zoneStats}
          />
        </aside>

        <div className={styles.content}>
          {activeZone === 'development' && (
            <DevelopmentZone
              businessId={businessId}
              actorId={actorId}
              onSubmit={handleSubmit}
              recentExecutions={recentExecutions}
              onExecutionSelect={handleExecutionSelect}
              currentExecution={currentExecution}
              execution={execution}
              trace={trace}
              gitResult={gitResult}
              gitOperation={gitOperation}
              isLoading={isLoading}
              onViewTrace={setSelectedExecution}
            />
          )}

          {activeZone !== 'development' && activeZone !== 'command' && activeZone !== 'research' && (
            <ZonePreview zoneId={activeZone} />
          )}

          {activeZone === 'command' && (
            <div className={styles.commandPreview}>
              <p className={styles.previewNote}>
                Switch to the Command Center zone to submit objectives and manage executions.
              </p>
            </div>
          )}

          {activeZone === 'research' && (
            <div className={styles.commandPreview}>
              <p className={styles.previewNote}>
                Switch to the Research Workspace to run web research queries.
              </p>
            </div>
          )}
        </div>
      </main>
    </div>
  );
};

const DevelopmentZone: React.FC<{
  businessId: string;
  actorId: string;
  onSubmit: (executionId: string) => void;
  recentExecutions: string[];
  onExecutionSelect: (executionId: string) => void;
  currentExecution: string | null;
  execution: { status?: string; output?: string; duration_ms?: number; created_at?: string } | undefined;
  trace: { entries: { type: string; data?: Record<string, string> }[] } | undefined;
  gitResult: { output: string; truncated?: string; repo?: string } | undefined;
  gitOperation: 'status' | 'diff' | 'log';
  isLoading: boolean;
  onViewTrace: (executionId: string) => void;
}> = ({
  businessId,
  actorId,
  onSubmit,
  recentExecutions,
  onExecutionSelect,
  currentExecution,
  execution,
  trace,
  gitResult,
  gitOperation,
  isLoading,
  onViewTrace,
}) => {
  const selectedExecutionData = useMemo(() => {
    if (!currentExecution || !trace) return null;
    return { executionId: currentExecution, trace };
  }, [currentExecution, trace]);

  return (
    <div className={styles.zone}>
      <section className={styles.inputSection} aria-labelledby="dev-input-heading">
        <h2 id="dev-input-heading" className={styles.sectionTitle}>
          Development Task
        </h2>
        <DevelopmentInput
          businessId={businessId}
          actorId={actorId}
          onSubmit={onSubmit}
        />
      </section>

      <div className={styles.resultsGrid}>
        <section className={styles.outputSection} aria-labelledby="dev-output-heading">
          <h2 id="dev-output-heading" className={styles.sectionTitle}>
            Task Output
            {currentExecution && (
              <span className={styles.executionBadge}>
                {currentExecution.slice(0, 12)}…
              </span>
            )}
          </h2>
          <DevelopmentOutput
            output={execution?.output}
            status={execution?.status}
            isLoading={isLoading}
          />
        </section>

        <section className={styles.contextSection} aria-labelledby="dev-context-heading">
          <h2 id="dev-context-heading" className={styles.sectionTitle}>
            Repository Context
          </h2>
          <RepositoryContext
            gitResult={gitResult}
            operation={gitOperation}
            executionId={currentExecution || undefined}
            onViewTrace={onViewTrace}
          />
        </section>
      </div>

      <section className={styles.historySection} aria-labelledby="dev-history-heading">
        <h2 id="dev-history-heading" className={styles.sectionTitle}>
          Recent Tasks
        </h2>
        <ExecutionHistoryList
          businessId={businessId}
          executionIds={recentExecutions}
          currentExecution={currentExecution}
          onSelect={onExecutionSelect}
        />
      </section>

      {selectedExecutionData && (
        <ExecutionDetail
          executionId={selectedExecutionData.executionId}
          businessId={businessId}
          onClose={() => {}}
        />
      )}
    </div>
  );
};

const ExecutionHistoryList: React.FC<{
  businessId: string;
  executionIds: string[];
  currentExecution: string | null;
  onSelect: (executionId: string) => void;
}> = ({ businessId, executionIds, currentExecution, onSelect }) => {
  return (
    <div className={styles.historyList}>
      {executionIds.length === 0 ? (
        <div className={styles.emptyHistory}>
          <svg width="48" height="48" viewBox="0 0 48 48" fill="none" aria-hidden="true">
            <circle cx="24" cy="24" r="20" stroke="var(--color-surface-border)" strokeWidth="2" />
            <path d="M24 16v16M16 24h16" stroke="var(--color-text-muted)" strokeWidth="2" strokeLinecap="round" />
          </svg>
          <p>No development tasks yet</p>
          <span>Submit a development task to see history</span>
        </div>
      ) : (
        <ul className={styles.historyItems}>
          {executionIds.map((id, idx) => (
            <li
              key={id}
              className={`${styles.historyItem} ${id === currentExecution ? styles.active : ''}`}
              onClick={() => onSelect(id)}
              role="button"
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  onSelect(id);
                }
              }}
            >
              <span className={styles.historyIndex}>{idx + 1}</span>
              <span className={styles.historyId}>{id.slice(0, 12)}…</span>
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
    development: 'Development',
    media: 'Media',
    meeting: 'Meeting Room',
  };

  const zoneDescriptions: Record<ZoneId, string> = {
    command: 'Primary command interface for objectives and executions',
    research: 'Web research and information gathering agents',
    development: 'Repository context, git operations, and development tasks',
    media: 'Content generation and media processing',
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
            This zone is not yet implemented. The Development Workspace is the primary
            functional area in this milestone.
          </p>
        </div>
      </div>
    </div>
  );
};