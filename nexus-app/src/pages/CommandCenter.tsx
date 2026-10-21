/**
 * CommandCenter Page - The primary user-facing page
 * Combines VirtualHQ, CommandInput, ExecutionList, ApprovalsPanel
 */

import React, { useState, useCallback, useEffect } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { VirtualHQ, type ZoneId } from '../components/VirtualHQ/VirtualHQ';
import { CommandInput } from '../components/CommandCenter/CommandInput';
import { ExecutionList } from '../components/CommandCenter/ExecutionList';
import { ActivityPanel } from '../components/CommandCenter/ActivityPanel';
import { ApprovalsPanel } from '../components/Approvals/ApprovalsPanel';
import { ExecutionDetail } from '../components/ExecutionDetail/ExecutionDetail';
import { useAgents, useTools, useIntelligence, useEventStream } from '../api/hooks';
import styles from './CommandCenter.module.css';

interface CommandCenterProps {
  businessId: string;
  actorId: string;
}

export const CommandCenter: React.FC<CommandCenterProps> = ({ businessId, actorId }) => {
  const [activeZone, setActiveZone] = useState<ZoneId>('command');
  const [selectedExecution, setSelectedExecution] = useState<string | null>(null);
  const [recentExecutions, setRecentExecutions] = useState<string[]>([]);

  const { data: agentsData } = useAgents(businessId);
  const { data: toolsData } = useTools();
  const queryClient = useQueryClient();

  // SSE: refetch executions, approvals, and authoritative activity when real-time
  // events arrive. The connection state is surfaced so the UI can tell the user
  // whether live updates are connected.
  const { connectionState, retryDelayMs } = useEventStream(businessId, {
    onChainCompleted: () => {
      queryClient.invalidateQueries({ queryKey: ['intelligence'] });
      queryClient.invalidateQueries({ queryKey: ['approvals'] });
      queryClient.invalidateQueries({ queryKey: ['recoveryExecutions'] });
    },
    onChainFailed: () => {
      queryClient.invalidateQueries({ queryKey: ['intelligence'] });
      queryClient.invalidateQueries({ queryKey: ['recoveryExecutions'] });
    },
    onApprovalRequested: () => {
      queryClient.invalidateQueries({ queryKey: ['approvals'] });
      queryClient.invalidateQueries({ queryKey: ['recoveryExecutions'] });
    },
    onApprovalApproved: () => {
      queryClient.invalidateQueries({ queryKey: ['approvals'] });
      queryClient.invalidateQueries({ queryKey: ['intelligence'] });
      queryClient.invalidateQueries({ queryKey: ['recoveryExecutions'] });
    },
    onApprovalDenied: () => {
      queryClient.invalidateQueries({ queryKey: ['approvals'] });
      queryClient.invalidateQueries({ queryKey: ['recoveryExecutions'] });
    },
  });

  // Track recent executions (in real app, this would come from a subscriptions/queries)
  // For now we'll use a simple approach - the user sees their submissions

  const handleSubmit = useCallback((executionId: string) => {
    setRecentExecutions((prev) => [executionId, ...prev.slice(0, 9)]);
    setSelectedExecution(executionId);
  }, []);

  const handleExecutionSelect = useCallback((executionId: string) => {
    setSelectedExecution(executionId);
  }, []);

  const handleCloseTrace = useCallback(() => {
    setSelectedExecution(null);
  }, []);

  // Zone stats for VirtualHQ. Agent counts come from the live registry query
  // (undefined until it resolves) — never a hardcoded number. Execution counts
  // are only the current client session's submitted executions.
  const zoneStats = {
    command: { agents: agentsData?.agents?.length, executions: recentExecutions.length },
    research: { executions: 0 },
    media: { executions: 0 },
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
              <path d="M9 14h10M14 9v10" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
            </svg>
            NEXUS
          </h1>
          <span className={styles.tagline}>Command Center</span>
        </div>
        <div className={styles.headerRight}>
          <div
            className={styles.statusIndicator}
            data-state={connectionState}
            title={`Live updates: ${connectionState}`}
          >
            <span
              className={`${styles.statusDot} ${
                connectionState === 'open'
                  ? styles.statusDotOpen
                  : connectionState === 'reconnecting' || connectionState === 'connecting'
                    ? styles.statusDotReconnecting
                    : styles.statusDotOffline
              }`}
            />
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
          {activeZone === 'command' && (
            <CommandCenterZone
              businessId={businessId}
              actorId={actorId}
              onSubmit={handleSubmit}
              recentExecutions={recentExecutions}
              onExecutionSelect={handleExecutionSelect}
            />
          )}

          {activeZone !== 'command' && (
            <ZonePreview zoneId={activeZone} />
          )}
        </div>
      </main>
    </div>
  );
};

const CommandCenterZone: React.FC<{
  businessId: string;
  actorId: string;
  onSubmit: (executionId: string) => void;
  recentExecutions: string[];
  onExecutionSelect: (executionId: string) => void;
}> = ({
  businessId,
  actorId,
  onSubmit,
  recentExecutions,
  onExecutionSelect,
}) => {
  return (
    <div className={styles.zone}>
      <section className={styles.commandSection} aria-labelledby="command-heading">
        <h2 id="command-heading" className={styles.sectionTitle}>
          Command Input
        </h2>
        <CommandInput
          businessId={businessId}
          actorId={actorId}
          onSubmit={onSubmit}
        />
      </section>

      <section className={styles.executionsSection} aria-labelledby="activity-heading">
        <div className={styles.sectionHeader}>
          <h2 id="activity-heading" className={styles.sectionTitle}>
            Activity
          </h2>
        </div>
        <ActivityPanel businessId={businessId} onSelect={onExecutionSelect} />
      </section>

      <section className={styles.executionsSection} aria-labelledby="executions-heading">
        <div className={styles.sectionHeader}>
          <h2 id="executions-heading" className={styles.sectionTitle}>
            Recent Executions
          </h2>
        </div>
        <ExecutionList
          businessId={businessId}
          executionIds={recentExecutions}
          onSelect={onExecutionSelect}
        />
      </section>

      <section className={styles.approvalsSection} aria-labelledby="approvals-heading">
        <h2 id="approvals-heading" className={styles.sectionTitle}>
          Approvals
        </h2>
        <ApprovalsPanel businessId={businessId} />
      </section>
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
            This zone is not yet implemented. The Command Center is the primary
            functional area in this milestone.
          </p>
        </div>
      </div>
    </div>
  );
};