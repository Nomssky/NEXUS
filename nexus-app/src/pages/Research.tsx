/**
 * Research Page - The Research Workspace
 * Composed of ResearchInput, ResearchOutput, and ResearchEvidence
 */

import React, { useState, useCallback, useEffect, useMemo } from 'react';
import { VirtualHQ, type ZoneId } from '../components/VirtualHQ/VirtualHQ';
import { ExecutionDetail } from '../components/ExecutionDetail/ExecutionDetail';
import { ResearchInput } from '../components/Research/ResearchInput';
import { ResearchOutput } from '../components/Research/ResearchOutput';
import { ResearchEvidence } from '../components/Research/ResearchEvidence';
import { useIntelligence, useIntelligenceTrace } from '@/api/hooks';
import type { WebSearchResult } from '@/types/api';
import styles from './Research.module.css';

interface ResearchPageProps {
  businessId: string;
  actorId: string;
}

export const Research: React.FC<ResearchPageProps> = ({ businessId, actorId }) => {
  const [activeZone, setActiveZone] = useState<ZoneId>('research');
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

  // Get the most recent execution that has web.search results
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

  // Extract web.search result from execution output or trace
  const webSearchResult = useMemo((): WebSearchResult | undefined => {
    if (!execution?.output) return undefined;
    try {
      // The web.search result is embedded in the execution output
      // Try to parse it from the output
      const parsed = JSON.parse(execution.output);
      if (parsed.sources && parsed.count) {
        return parsed as WebSearchResult;
      }
    } catch {
      // Not JSON, try to extract from trace
    }

    // Try to find tool.invocation.completed with web.search in trace
    if (trace?.entries) {
      for (const entry of trace.entries) {
        if (entry.type === 'tool.invocation.completed' && entry.data?.tool_id === 'web.search') {
          try {
            const data = JSON.parse(JSON.stringify(entry.data));
            if (data.sources && data.count) {
              return { sources: data.sources, count: data.count };
            }
          } catch {
            // Ignore parse errors
          }
        }
      }
    }

    return undefined;
  }, [execution, trace]);

  // Zone stats for VirtualHQ
  const zoneStats = {
    command: { executions: recentExecutions.length },
    research: { executions: recentExecutions.length },
    media: { executions: 0 },
    development: { executions: 0 },
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
          <span className={styles.tagline}>Research Workspace</span>
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
          {activeZone === 'research' && (
            <ResearchZone
              businessId={businessId}
              actorId={actorId}
              onSubmit={handleSubmit}
              recentExecutions={recentExecutions}
              onExecutionSelect={handleExecutionSelect}
              currentExecution={currentExecution}
              execution={execution}
              trace={trace}
              webSearchResult={webSearchResult}
              isLoading={isLoading}
              onViewTrace={setSelectedExecution}
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

const ResearchZone: React.FC<{
  businessId: string;
  actorId: string;
  onSubmit: (executionId: string) => void;
  recentExecutions: string[];
  onExecutionSelect: (executionId: string) => void;
  currentExecution: string | null;
  execution: { status?: string; output?: string; duration_ms?: number; created_at?: string } | undefined;
  trace: { entries: { type: string; data?: Record<string, string> }[] } | undefined;
  webSearchResult: { sources: string; count: string } | undefined;
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
  webSearchResult,
  isLoading,
  onViewTrace,
}) => {
  const selectedExecutionData = useMemo(() => {
    if (!currentExecution || !trace) return null;
    return { executionId: currentExecution, trace };
  }, [currentExecution, trace]);

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

      <div className={styles.resultsGrid}>
        <section className={styles.outputSection} aria-labelledby="research-output-heading">
          <h2 id="research-output-heading" className={styles.sectionTitle}>
            Research Output
            {currentExecution && (
              <span className={styles.executionBadge}>
                {currentExecution.slice(0, 12)}…
              </span>
            )}
          </h2>
          <ResearchOutput
            output={execution?.output}
            status={execution?.status}
            isLoading={isLoading}
          />
        </section>

        <section className={styles.evidenceSection} aria-labelledby="research-evidence-heading">
          <h2 id="research-evidence-heading" className={styles.sectionTitle}>
            Sources & Evidence
          </h2>
          <ResearchEvidence
            result={webSearchResult}
            executionId={currentExecution || undefined}
            onViewTrace={onViewTrace}
          />
        </section>
      </div>

      <section className={styles.historySection} aria-labelledby="history-heading">
        <h2 id="history-heading" className={styles.sectionTitle}>
          Recent Research
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
          <p>No research executions yet</p>
          <span>Submit a research query to see history</span>
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

