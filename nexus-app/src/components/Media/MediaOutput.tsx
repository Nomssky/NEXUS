/**
 * MediaOutput - Displays the media task output as returned by the backend.
 *
 * A media task returns only what an execution produced: text from the model,
 * tool data from governed adapters, or nothing. This component never
 * fabricates generated assets. When a web.search task completed, it
 * displays the parsed sources from ResearchEvidence-style data. If the
 * execution returned text only (a brief), it is rendered as text. Empty
 * results are shown distinctly from failures or unknown outcomes.
 */

import React from 'react';
import { Card, Badge } from '../primitives';
import styles from './MediaOutput.module.css';

interface MediaOutputProps {
  output?: string;
  status?: string;
  isLoading?: boolean;
}

export const MediaOutput: React.FC<MediaOutputProps> = ({
  output,
  status,
  isLoading = false,
}) => {
  if (isLoading) {
    return (
      <Card variant="default" padding="lg">
        <div className={styles.loading}>
          <div className={styles.spinner} aria-hidden="true">
            <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
              <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeDasharray="31.4 31.4" className={styles.spinnerCircle} />
            </svg>
          </div>
          <p>Media task in progress…</p>
          <span className={styles.loadingHint}>The agent is preparing the result</span>
        </div>
      </Card>
    );
  }

  if (!output || output.trim() === '') {
    const statusMessages: Record<string, { title: string; description: string }> = {
      pending: { title: 'Task queued', description: 'Waiting for execution to begin' },
      running: { title: 'Task in progress', description: 'The agent is working on the brief' },
      failed: { title: 'Task failed', description: 'An error occurred during execution' },
      cancelled: { title: 'Task cancelled', description: 'The execution was cancelled' },
      timed_out: { title: 'Task timed out', description: 'The execution exceeded its time limit' },
      unknown: { title: 'Outcome unknown', description: 'The execution result could not be determined' },
    };
    const statusKey = status || 'unknown';
    const msg = statusMessages[statusKey] || { title: 'No output', description: 'The execution completed without producing output' };

    return (
      <Card variant="outlined" padding="lg">
        <div className={styles.empty}>
          <svg width="48" height="48" viewBox="0 0 48 48" fill="none" aria-hidden="true">
            <circle cx="24" cy="24" r="20" stroke="var(--color-surface-border)" strokeWidth="2" />
            <path d="M24 16v16M16 24h16" stroke="var(--color-text-muted)" strokeWidth="2" strokeLinecap="round" />
          </svg>
          <p>{msg.title}</p>
          <span>{msg.description}</span>
        </div>
      </Card>
    );
  }

  return (
    <Card variant="default" padding="lg">
      <div className={styles.output}>
        <div className={styles.outputContent}>
          {formatOutput(output)}
        </div>
      </div>
    </Card>
  );
};

function formatOutput(text: string): React.ReactNode {
  const paragraphs = text.split(/\n\s*\n/);
  return paragraphs.map((para, idx) => {
    const trimmed = para.trim();
    if (!trimmed) return null;
    if (trimmed.match(/^[\-\*]\s/)) {
      return (
        <ul key={idx} className={styles.list}>
          {trimmed.split('\n').map((line, li) => (
            <li key={li}>{line.replace(/^[\-\*]\s/, '')}</li>
          ))}
        </ul>
      );
    }
    if (trimmed.match(/^\d+\.\s/)) {
      return (
        <ol key={idx} className={styles.list}>
          {trimmed.split('\n').map((line, li) => (
            <li key={li}>{line.replace(/^\d+\.\s/, '')}</li>
          ))}
        </ol>
      );
    }
    return <p key={idx} className={styles.paragraph}>{trimmed}</p>;
  });
}