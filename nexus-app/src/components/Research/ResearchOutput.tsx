/**
 * ResearchOutput - Displays the final research result.
 *
 * Rendering contract:
 * - `result=` prose is agent-generated and rendered as text only.
 * - Runtime metrics come from the execution summary prefix.
 * - Structured output is recognized only when parseResearchResult finds the
 *   documented web.search envelope; sources are rendered by ResearchEvidence.
 * - Malformed or unrecognized output is shown verbatim as text.
 */

import React, { useMemo } from 'react';
import { Card, Badge, getStatusVariant } from '../primitives';
import type { ExecutionStatus } from '../../types/api';
import { parseResearchResult } from '../../types/research';
import styles from './ResearchOutput.module.css';

interface ResearchOutputProps {
  output?: string;
  status?: string;
  error?: ExecutionStatus['error'];
  durationMs?: number;
  isLoading?: boolean;
}

export const ResearchOutput: React.FC<ResearchOutputProps> = ({
  output,
  status,
  error,
  durationMs,
  isLoading = false,
}) => {
  const parsed = useMemo(() => parseResearchResult(output), [output]);

  if (isLoading) {
    return (
      <Card variant="default" padding="lg">
        <div className={styles.loading}>
          <div className={styles.spinner} aria-hidden="true">
            <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
              <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeDasharray="31.4 31.4" className={styles.spinnerCircle} />
            </svg>
          </div>
          <p>Research in progress…</p>
          <span className={styles.loadingHint}>The agent is gathering and synthesizing sources</span>
        </div>
      </Card>
    );
  }

  if (parsed.kind === 'empty') {
    const empty = emptyState(status, error?.message);
    return (
      <Card variant="outlined" padding="lg">
        <div className={styles.empty}>
          <svg width="48" height="48" viewBox="0 0 48 48" fill="none" aria-hidden="true">
            <circle cx="24" cy="24" r="20" stroke="var(--color-surface-border)" strokeWidth="2" />
            <path d="M24 16v16M16 24h16" stroke="var(--color-text-muted)" strokeWidth="2" strokeLinecap="round" />
          </svg>
          <p>{empty.title}</p>
          <span>{empty.description}</span>
        </div>
      </Card>
    );
  }

  const runtimeStatus = status || parsed.runtimeState || 'unknown';
  const isFailure =
    runtimeStatus === 'failed' || runtimeStatus === 'cancelled' || runtimeStatus === 'timed_out';
  const details = buildDetails(parsed.objective, parsed.agent, parsed.metrics, durationMs);

  return (
    <Card variant={isFailure ? 'outlined' : 'default'} padding="lg">
      <div className={styles.output}>
        <div className={styles.resultHeader}>
          <div className={styles.resultHeading}>
            <h3 className={styles.title}>
              {isFailure ? failureTitle(runtimeStatus) : titleForKind(parsed.kind)}
            </h3>
            <p className={styles.provenance}>
              {parsed.kind === 'summary'
                ? 'Agent-generated result. Source records are listed separately only when the execution provides them.'
                : parsed.kind === 'structured'
                  ? 'Structured execution output.'
                  : 'Execution output.'}
            </p>
          </div>
          <Badge variant={getStatusVariant(runtimeStatus)} size="sm">
            {statusLabel(runtimeStatus)}
          </Badge>
        </div>

        {isFailure && (
          <div className={styles.errorBanner} role="alert">
            <span className={styles.errorCode}>{error?.code || runtimeStatus}</span>
            <p>{error?.message || failureDescription(runtimeStatus)}</p>
          </div>
        )}

        {parsed.webSearch ? (
          <p className={styles.structuredResult}>{structuredSummary(parsed.webSearch.count)}</p>
        ) : parsed.resultText ? (
          <div className={styles.outputContent}>{formatOutput(parsed.resultText)}</div>
        ) : parsed.kind === 'text' ? (
          <div className={styles.outputContent}>{formatOutput(parsed.raw)}</div>
        ) : (
          <p className={styles.noResult}>
            {isFailure
              ? failureDescription(runtimeStatus)
              : 'The execution completed without a textual result.'}
          </p>
        )}

        {details.length > 0 && (
          <dl className={styles.metaGrid} aria-label="Execution details">
            {details.map((detail) => (
              <div key={detail.label} className={styles.metaItem}>
                <dt>{detail.label}</dt>
                <dd>{detail.value}</dd>
              </div>
            ))}
          </dl>
        )}

        {(parsed.kind === 'summary' || parsed.kind === 'structured') && (
          <details className={styles.rawDetails}>
            <summary>Execution summary</summary>
            <pre className={styles.rawSummary}>{parsed.raw}</pre>
          </details>
        )}
      </div>
    </Card>
  );
};

interface DetailItem {
  label: string;
  value: string;
}

function buildDetails(
  objective: string | undefined,
  agent: string | undefined,
  metrics: ReturnType<typeof parseResearchResult>['metrics'],
  durationMs: number | undefined
): DetailItem[] {
  const details: DetailItem[] = [];
  if (objective) details.push({ label: 'Objective', value: objective });
  if (agent) details.push({ label: 'Agent', value: agent });
  if (durationMs !== undefined) details.push({ label: 'Duration', value: formatDuration(durationMs) });
  if (metrics.iterations !== undefined) {
    details.push({ label: 'Iterations', value: String(metrics.iterations) });
  }
  if (metrics.toolCalls !== undefined) {
    details.push({ label: 'Tool calls', value: String(metrics.toolCalls) });
  }
  if (metrics.observations !== undefined && metrics.observations > 0) {
    details.push({ label: 'Observations', value: String(metrics.observations) });
  }
  if (metrics.modelCalls !== undefined && metrics.modelCalls > 0) {
    details.push({ label: 'Model calls', value: String(metrics.modelCalls) });
  }
  if (metrics.delegations !== undefined && metrics.delegations > 0) {
    details.push({ label: 'Delegations', value: String(metrics.delegations) });
  }
  if (metrics.replans !== undefined && metrics.replans > 0) {
    details.push({ label: 'Replans', value: String(metrics.replans) });
  }
  return details;
}

function emptyState(
  status: string | undefined,
  errorMessage: string | undefined
): { title: string; description: string } {
  if (!status) {
    return {
      title: 'No research result yet',
      description: 'Submit a research query to see its result.',
    };
  }

  const messages: Record<string, { title: string; description: string }> = {
    pending: { title: 'Research queued', description: 'Waiting for execution to begin.' },
    running: { title: 'Research in progress', description: 'The agent is gathering sources.' },
    failed: { title: 'Research failed', description: 'An error occurred during execution.' },
    cancelled: { title: 'Research cancelled', description: 'The execution was cancelled.' },
    timed_out: { title: 'Research timed out', description: 'The execution exceeded its time limit.' },
    unknown: { title: 'Outcome unknown', description: 'The execution result could not be determined.' },
    completed: {
      title: 'No research output',
      description: 'The execution completed without producing output.',
    },
  };

  const message = messages[status] ?? {
    title: 'No research output',
    description: 'The execution did not produce output.',
  };

  return {
    title: message.title,
    description: errorMessage || message.description,
  };
}

function titleForKind(kind: string): string {
  if (kind === 'structured') return 'Structured research result';
  if (kind === 'text') return 'Research output';
  return 'Research result';
}

function statusLabel(status: string): string {
  switch (status) {
    case 'pending':
      return 'Queued';
    case 'running':
      return 'Running';
    case 'completed':
      return 'Completed';
    case 'failed':
      return 'Failed';
    case 'cancelled':
      return 'Cancelled';
    case 'timed_out':
      return 'Timed out';
    case 'unknown':
      return 'Unknown';
    default:
      return status;
  }
}

function failureTitle(status: string): string {
  switch (status) {
    case 'failed':
      return 'Research failed';
    case 'cancelled':
      return 'Research cancelled';
    case 'timed_out':
      return 'Research timed out';
    default:
      return 'Research did not complete';
  }
}

function failureDescription(status: string): string {
  switch (status) {
    case 'failed':
      return 'The execution did not complete successfully.';
    case 'cancelled':
      return 'The execution was cancelled before producing a result.';
    case 'timed_out':
      return 'The execution exceeded its time limit.';
    default:
      return 'The execution outcome could not be determined.';
  }
}

function structuredSummary(countValue: string): string {
  const count = Number(countValue);
  if (Number.isFinite(count) && Number.isInteger(count) && count >= 0) {
    if (count === 0) return 'The structured execution output contained no source records.';
    return `The execution returned ${count} source record${count === 1 ? '' : 's'}. They are listed in Sources & Evidence.`;
  }
  return 'The execution returned a structured web search result. Source records are listed in Sources & Evidence.';
}

function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return 'Unknown';
  if (ms < 1000) return `${Math.round(ms)} ms`;
  const seconds = ms / 1000;
  return `${seconds >= 10 ? Math.round(seconds) : seconds.toFixed(1)} s`;
}

/**
 * Render plain text without interpreting it as HTML or Markdown. Paragraphs
 * and list items use `white-space: pre-wrap`, so single line breaks survive.
 */
function formatOutput(text: string): React.ReactNode {
  const blocks = text.replace(/\r\n/g, '\n').split(/\n\s*\n/);

  return blocks.map((block, index) => {
    const trimmed = block.trim();
    if (!trimmed) return null;

    const lines = trimmed.split('\n');
    if (lines.every((line) => /^\s*[-*]\s+/.test(line))) {
      return (
        <ul key={index} className={styles.list}>
          {lines.map((line, lineIndex) => (
            <li key={lineIndex}>{line.replace(/^\s*[-*]\s+/, '')}</li>
          ))}
        </ul>
      );
    }

    if (lines.every((line) => /^\s*\d+[.)]\s+/.test(line))) {
      return (
        <ol key={index} className={styles.list}>
          {lines.map((line, lineIndex) => (
            <li key={lineIndex}>{line.replace(/^\s*\d+[.)]\s+/, '')}</li>
          ))}
        </ol>
      );
    }

    return (
      <p key={index} className={styles.paragraph}>
        {trimmed}
      </p>
    );
  });
}
