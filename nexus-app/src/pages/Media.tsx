/**
 * Media Page - The Media Workspace
 *
 * The Media workspace supports creative preparation: briefs, reference
 * research, and review of what the current backend actually returns. The
 * backend has no image/video/audio generation tools, and this workspace
 * does not pretend that any asset was produced. The workflow is honest:
 *
 *   Edit a brief (client-side only)
 *   → Optionally run it through the governed execution API
 *   → Inspect the real execution status, output, and trace
 *   → Reuse the web.search evidence layout when a task actually searched
 */

import React, { useState, useCallback, useMemo } from 'react';
import { VirtualHQ, type ZoneId } from '../components/VirtualHQ/VirtualHQ';
import { ExecutionDetail } from '../components/ExecutionDetail/ExecutionDetail';
import { ResearchEvidence } from '../components/Research/ResearchEvidence';
import { MediaInput } from '../components/Media/MediaInput';
import { MediaOutput } from '../components/Media/MediaOutput';
import { BriefBuilder } from '../components/Media/BriefBuilder';
import { useIntelligence, useIntelligenceTrace } from '../api/hooks';
import type { WebSearchResult } from '../types/api';
import styles from './Media.module.css';

interface MediaPageProps {
  businessId: string;
  actorId: string;
}

export const Media: React.FC<MediaPageProps> = ({ businessId, actorId }) => {
  const [activeZone, setActiveZone] = useState<ZoneId>('media');
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

  const latestExecutionId = recentExecutions[0] || currentExecution;
  const { data: execution } = useIntelligence(latestExecutionId || '', businessId, {
    enabled: !!latestExecutionId,
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === 'pending' || status === 'running' ? 2000 : false;
    },
  });

  const { data: trace } = useIntelligenceTrace(
    latestExecutionId || '',
    businessId,
    { limit: 100 },
    { enabled: !!latestExecutionId }
  );

  // Extract web.search result when the agent actually searched (only to show
  // real references — not to simulate generation)
  const webSearchResult = useMemo((): WebSearchResult | undefined => {
    if (!execution?.output) return undefined;
    try {
      const parsed = JSON.parse(execution.output);
      if (parsed.sources && parsed.count) return parsed as WebSearchResult;
    } catch {
      // not JSON from output
    }
    if (trace?.entries) {
      for (const entry of trace.entries) {
        if (entry.type === 'tool.invocation.completed' && entry.data?.tool_id === 'web.search') {
          try {
            const data = JSON.parse(JSON.stringify(entry.data));
            if (data.sources && data.count) return { sources: data.sources, count: data.count };
          } catch {
            /* ignore */
          }
        }
      }
    }
    return undefined;
  }, [execution, trace]);

  const isLoading = execution?.status === 'pending' || execution?.status === 'running';

  const zoneStats = {
    command: { executions: 0 },
    research: { executions: 0 },
    media: { executions: recentExecutions.length },
    development: { executions: 0 },
    meeting: { executions: 0 },
  };

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div className={styles.headerLeft}>
          <h1 className={styles.logo}>
            <svg width="28" height="28" viewBox="0 0 28 28" fill="none" aria-hidden="true">
              <rect x="3" y="3" width="22" height="22" rx="4" stroke="currentColor" strokeWidth="2" />
              <circle cx="14" cy="14" r="4" stroke="currentColor" strokeWidth="1.5" />
              <path d="M10 14h8" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
            </svg>
            NEXUS
          </h1>
          <span className={styles.tagline}>Media Workspace</span>
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
          {activeZone === 'media' && (
            <MediaZone
              businessId={businessId}
              actorId={actorId}
              onSubmit={handleSubmit}
              recentExecutions={recentExecutions}
              onExecutionSelect={handleExecutionSelect}
              currentExecution={currentExecution}
              execution={execution}
              webSearchResult={webSearchResult}
              isLoading={isLoading}
              onViewTrace={setSelectedExecution}
            />
          )}

          {activeZone === 'command' && (
            <div className={styles.commandPreview}>
              <p className={styles.previewNote}>
                Switch to the Command Center to submit objectives and manage executions.
              </p>
            </div>
          )}

          {activeZone === 'research' && (
            <div className={styles.commandPreview}>
              <p className={styles.previewNote}>
                Switch to the Research Workspace to run focused web research.
              </p>
            </div>
          )}

          {activeZone === 'development' && (
            <div className={styles.commandPreview}>
              <p className={styles.previewNote}>
                Switch to the Development Workspace to inspect git context and run dev tasks.
              </p>
            </div>
          )}

          {activeZone === 'meeting' && (
            <ZonePreview zoneId="meeting" />
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

interface MediaZoneProps {
  businessId: string;
  actorId: string;
  onSubmit: (executionId: string) => void;
  onExecutionSelect: (executionId: string) => void;
  recentExecutions: string[];
  currentExecution: string | null;
  execution: { status?: string; output?: string } | undefined;
  webSearchResult: WebSearchResult | undefined;
  isLoading: boolean;
  onViewTrace: (executionId: string) => void;
}

const MediaZone: React.FC<MediaZoneProps> = ({
  businessId,
  actorId,
  onSubmit,
  recentExecutions,
  onExecutionSelect,
  currentExecution,
  execution,
  webSearchResult,
  isLoading,
  onViewTrace,
}) => {
  return (
    <div className={styles.zone}>
      <section className={styles.inputSection} aria-labelledby="media-input-heading">
        <h2 id="media-input-heading" className={styles.sectionTitle}>
          Media Objective
        </h2>
        <p className={styles.sectionNote}>
          The backend currently provides no image, video, or audio generation
          tool. This workspace supports creative preparation — briefs and
          reference-gathering — through the existing governed execution path.
        </p>
        <MediaInput businessId={businessId} actorId={actorId} onSubmit={onSubmit} />
      </section>

      <section className={styles.briefSection} aria-labelledby="brief-heading">
        <h2 id="brief-heading" className={styles.sectionTitle}>
          Creative Brief
        </h2>
        <BriefBuilder
          businessId={businessId}
          actorId={actorId}
          onRunBrief={onSubmit}
        />
      </section>

      <div className={styles.resultsGrid}>
        <section className={styles.outputSection} aria-labelledby="media-output-heading">
          <h2 id="media-output-heading" className={styles.sectionTitle}>
            Output
            {currentExecution && (
              <button
                className={styles.execLink}
                onClick={() => onViewTrace(currentExecution)}
              >
                {currentExecution.slice(0, 12)}…
              </button>
            )}
          </h2>
          <MediaOutput
            output={execution?.output}
            status={execution?.status}
            isLoading={isLoading}
          />
        </section>

        {webSearchResult && (
          <section className={styles.evidenceSection} aria-labelledby="media-evidence-heading">
            <h2 id="media-evidence-heading" className={styles.sectionTitle}>
              References (web research)
            </h2>
            <ResearchEvidence
              result={webSearchResult}
              executionId={currentExecution || undefined}
              onViewTrace={onViewTrace}
            />
          </section>
        )}
      </div>

      <section className={styles.historySection} aria-labelledby="media-history-heading">
        <h2 id="media-history-heading" className={styles.sectionTitle}>
          Recent Tasks
        </h2>
        <HistoryList
          executionIds={recentExecutions}
          currentExecution={currentExecution}
          onSelect={onExecutionSelect}
        />
      </section>
    </div>
  );
};

interface HistoryListProps {
  executionIds: string[];
  currentExecution: string | null;
  onSelect: (id: string) => void;
}

const HistoryList: React.FC<HistoryListProps> = ({ executionIds, currentExecution, onSelect }) => {
  if (executionIds.length === 0) {
    return (
      <div className={styles.emptyHistory}>
        <p>No media tasks yet</p>
        <span>Submit an objective to see history</span>
      </div>
    );
  }
  return (
    <ul className={styles.historyItems} aria-label="Recent media tasks">
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
          aria-label={`Task ${id}`}
        >
          <span className={styles.historyIndex}>{idx + 1}</span>
          <span className={styles.historyId}>{id.slice(0, 12)}…</span>
        </li>
      ))}
    </ul>
  );
};

const ZonePreview: React.FC<{ zoneId: ZoneId }> = ({ zoneId }) => (
  <div className={styles.zonePreview}>
    <div className={styles.previewContent}>
      <h2 className={styles.previewTitle}>Meeting Room</h2>
      <p className={styles.previewDescription}>Coordination and delegation management</p>
      <div className={styles.previewStatus}>
        <span className={styles.comingSoon}>Coming Soon</span>
        <p className={styles.previewNote}>
          Not implemented yet. Use the Command Center for ad-hoc orchestration.
        </p>
      </div>
    </div>
  </div>
);