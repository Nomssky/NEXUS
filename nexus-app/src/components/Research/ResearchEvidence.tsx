/**
 * ResearchEvidence - Displays source records and search metadata.
 *
 * Sources are rendered only from the documented web.search output envelope
 * (`{ sources, count }`). The execution trace contributes tool-invocation
 * metadata only: the trace allowlist drops tool results, prompts, and model
 * output, so a trace entry can never be turned into a citation.
 *
 * The empty state is deliberately explicit. "No source records available" is
 * different from "the search found nothing", and the UI must not imply either
 * without evidence.
 */

import React, { useMemo } from 'react';
import { Card, Badge, Button, type BadgeVariant } from '../primitives';
import type { WebSearchResult } from '../../types/api';
import { parseWebSearchResult } from '../../types/api';
import type { ResearchToolActivity } from '../../types/research';
import styles from './ResearchEvidence.module.css';

interface ResearchEvidenceProps {
  result?: WebSearchResult;
  /** Terminal tool events from the execution trace. Metadata only. */
  toolActivity?: ResearchToolActivity[];
  status?: string;
  executionId?: string;
  onViewTrace?: (executionId: string) => void;
}

export const ResearchEvidence: React.FC<ResearchEvidenceProps> = ({
  result,
  toolActivity = [],
  status,
  executionId,
  onViewTrace,
}) => {
  const sources = useMemo(() => (result ? parseWebSearchResult(result) : []), [result]);
  const declaredCount = useMemo(() => declaredSourceCount(result), [result]);
  const missingCount =
    declaredCount !== undefined && declaredCount > sources.length
      ? declaredCount - sources.length
      : 0;

  if (sources.length === 0) {
    const empty = emptyEvidenceCopy(status, result, toolActivity);
    return (
      <Card variant="outlined" padding="md">
        <div className={styles.empty}>
          <svg width="48" height="48" viewBox="0 0 48 48" fill="none" aria-hidden="true">
            <circle cx="24" cy="24" r="20" stroke="var(--color-surface-border)" strokeWidth="2" />
            <path d="M24 16v16M16 24h16" stroke="var(--color-text-muted)" strokeWidth="2" strokeLinecap="round" />
          </svg>
          <p>{empty.title}</p>
          <span>{empty.description}</span>
          {executionId && (
            <Button variant="ghost" size="sm" onClick={() => onViewTrace?.(executionId)}>
              View Trace
            </Button>
          )}
        </div>
        {toolActivity.length > 0 && <ToolActivity activity={toolActivity} />}
      </Card>
    );
  }

  return (
    <div className={styles.evidenceContainer}>
      <div className={styles.header}>
        <h3 className={styles.title}>Research Sources</h3>
        <div className={styles.meta}>
          <Badge variant="info" size="sm">
            {sources.length} source{sources.length !== 1 ? 's' : ''}
          </Badge>
          {executionId && (
            <Button variant="ghost" size="sm" onClick={() => onViewTrace?.(executionId)}>
              View Trace
            </Button>
          )}
        </div>
      </div>

      {missingCount > 0 && (
        <p className={styles.partialNote}>
          The execution reported {declaredCount} source records; {missingCount} could not be
          displayed (duplicate or malformed).
        </p>
      )}

      <div className={styles.sources}>
        {sources.map((source) => (
          <article key={`${source.index}-${source.url || source.title}`} className={styles.sourceCard}>
            <div className={styles.sourceHeader}>
              <span className={styles.sourceIndex}>{source.index}.</span>
              <div className={styles.sourceHeading}>
                <h4 className={styles.sourceTitle}>{source.title}</h4>
                <div className={styles.sourceMeta}>
                  {source.source && <span className={styles.sourceDomain}>{source.source}</span>}
                  {source.linkable ? (
                    <a
                      href={source.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className={styles.sourceLink}
                      aria-label={`Open ${source.title} in new tab`}
                    >
                      <svg width="14" height="14" viewBox="0 0 14 14" fill="none" aria-hidden="true">
                        <path d="M4 10h6M10 4v6M7 11l4-4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
                      </svg>
                      <span>Open source</span>
                    </a>
                  ) : (
                    <span className={styles.sourceUnavailable}>No link available</span>
                  )}
                </div>
              </div>
            </div>
            {source.snippet && <p className={styles.sourceSnippet}>{source.snippet}</p>}
          </article>
        ))}
      </div>
    </div>
  );
};

const ToolActivity: React.FC<{ activity: ResearchToolActivity[] }> = ({ activity }) => (
  <div className={styles.activity}>
    <h4 className={styles.activityTitle}>Search tool activity</h4>
    <p className={styles.activityNote}>Recorded execution metadata — not citations.</p>
    <ul className={styles.activityList}>
      {activity.map((item, index) => (
        <li key={`${item.callId || item.toolId}-${index}`} className={styles.activityItem}>
          <span className={styles.activityTool}>{item.toolId}</span>
          <Badge variant={variantForOutcome(item.outcome)} size="sm">
            {item.outcome}
          </Badge>
          <span className={styles.activityMeta}>{activityMeta(item)}</span>
        </li>
      ))}
    </ul>
  </div>
);

function emptyEvidenceCopy(
  status: string | undefined,
  result: WebSearchResult | undefined,
  activity: ResearchToolActivity[]
): { title: string; description: string } {
  if (status === 'pending' || status === 'running') {
    return {
      title: 'Evidence pending',
      description:
        'The execution is still running. Source records appear only if the execution result includes them.',
    };
  }

  if (result) {
    return {
      title: 'No usable source records',
      description:
        'The execution returned a structured result, but it did not contain any valid source records.',
    };
  }

  const webSearch = activity.filter((item) => item.toolId === 'web.search');
  if (webSearch.some((item) => item.outcome === 'completed')) {
    return {
      title: 'No source records available',
      description:
        'The execution invoked web.search, but its source records are not available in the execution result. The result text is agent-generated and is not a citation list.',
    };
  }

  if (webSearch.length > 0) {
    return {
      title: 'Web search did not complete',
      description:
        'The web search tool did not complete successfully, so no source records were returned.',
    };
  }

  return {
    title: 'No source records available',
    description:
      'This execution did not report any source records. The result text is agent-generated and is not a citation list.',
  };
}

function declaredSourceCount(result: WebSearchResult | undefined): number | undefined {
  if (!result) return undefined;
  const count = Number(result.count);
  if (!Number.isFinite(count) || count < 0 || !Number.isInteger(count)) return undefined;
  return count;
}

function variantForOutcome(outcome: string): BadgeVariant {
  switch (outcome) {
    case 'completed':
      return 'success';
    case 'unknown':
      return 'unknown';
    case 'rejected':
      return 'warning';
    default:
      return 'error';
  }
}

function activityMeta(activity: ResearchToolActivity): string {
  const parts: string[] = [];
  if (activity.attempts !== undefined) {
    parts.push(`${activity.attempts} attempt${activity.attempts === 1 ? '' : 's'}`);
  }
  if (activity.durationMs !== undefined) {
    parts.push(formatDuration(activity.durationMs));
  }
  if (activity.resultBytes !== undefined) {
    parts.push(`${formatBytes(activity.resultBytes)} result`);
  }
  if (activity.errorKind) {
    parts.push(activity.errorKind);
  }
  return parts.join(' · ') || 'No additional metadata';
}

function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return 'Unknown duration';
  if (ms < 1000) return `${Math.round(ms)} ms`;
  const seconds = ms / 1000;
  return `${seconds >= 10 ? Math.round(seconds) : seconds.toFixed(1)} s`;
}

function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return 'Unknown size';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
