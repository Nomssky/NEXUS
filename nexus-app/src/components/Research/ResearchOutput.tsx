/**
 * ResearchOutput - Displays the final research answer/summary
 */

import React from 'react';
import { Card, CardContent } from '../primitives';
import styles from './ResearchOutput.module.css';

interface ResearchOutputProps {
  output?: string;
  status?: string;
  isLoading?: boolean;
}

export const ResearchOutput: React.FC<ResearchOutputProps> = ({
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
          <p>Research in progress…</p>
          <span className={styles.loadingHint}>The agent is gathering and synthesizing sources</span>
        </div>
      </Card>
    );
  }

  if (!output || output.trim() === '') {
    const statusMessages: Record<string, { title: string; description: string }> = {
      pending: { title: 'Research queued', description: 'Waiting for execution to begin' },
      running: { title: 'Research in progress', description: 'The agent is gathering sources' },
      failed: { title: 'Research failed', description: 'An error occurred during execution' },
      cancelled: { title: 'Research cancelled', description: 'The execution was cancelled' },
      timed_out: { title: 'Research timed out', description: 'The execution exceeded its time limit' },
      unknown: { title: 'Outcome unknown', description: 'The execution result could not be determined' },
    };

    const statusKey = status || 'unknown';
    const msg = statusMessages[statusKey] || { title: 'No research output', description: 'The execution completed without producing output' };

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
  // Split by double newlines for paragraphs
  const paragraphs = text.split(/\n\s*\n/);

  return paragraphs.map((para, idx) => {
    const trimmed = para.trim();
    if (!trimmed) return null;

    // Check if it looks like a list item
    if (trimmed.match(/^[\-\*]\s/)) {
      return (
        <ul key={idx} className={styles.list}>
          {trimmed.split('\n').map((line, li) => (
            <li key={li}>{line.replace(/^[\-\*]\s/, '')}</li>
          ))}
        </ul>
      );
    }

    // Check if it looks like a numbered list
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