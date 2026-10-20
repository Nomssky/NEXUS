/**
 * ResearchEvidence - Displays web search sources and evidence
 * Parses and presents the structured web.search output
 */

import React, { useMemo } from 'react';
import { Card, CardContent, Badge, Button } from '../primitives';
import type { ParsedWebSearchSource, WebSearchResult } from '../../types/api';
import { parseWebSearchResult } from '../../types/api';
import styles from './ResearchEvidence.module.css';

interface ResearchEvidenceProps {
  result?: WebSearchResult;
  executionId?: string;
  onViewTrace?: (executionId: string) => void;
}

export const ResearchEvidence: React.FC<ResearchEvidenceProps> = ({
  result,
  executionId,
  onViewTrace,
}) => {
  const sources = useMemo(() => {
    if (!result) return [];
    return parseWebSearchResult(result);
  }, [result]);

  if (!result || sources.length === 0) {
    return (
      <Card variant="outlined" padding="md">
        <div className={styles.empty}>
          <svg width="48" height="48" viewBox="0 0 48 48" fill="none" aria-hidden="true">
            <circle cx="24" cy="24" r="20" stroke="var(--color-surface-border)" strokeWidth="2" />
            <path d="M24 16v16M16 24h16" stroke="var(--color-text-muted)" strokeWidth="2" strokeLinecap="round" />
          </svg>
          <p>No research sources found</p>
          <span>The search returned no results or the execution has not completed</span>
        </div>
      </Card>
    );
  }

  return (
    <div className={styles.evidenceContainer}>
      <div className={styles.header}>
        <h3 className={styles.title}>Research Sources</h3>
        <div className={styles.meta}>
          <Badge variant="info" size="sm">{sources.length} source{sources.length !== 1 ? 's' : ''}</Badge>
          {executionId && (
            <Button variant="ghost" size="sm" onClick={() => onViewTrace?.(executionId)}>
              View Trace
            </Button>
          )}
        </div>
      </div>

      <div className={styles.sources}>
        {sources.map((source) => (
          <article key={source.index} className={styles.sourceCard}>
            <div className={styles.sourceHeader}>
              <span className={styles.sourceIndex}>{source.index}.</span>
              <h4 className={styles.sourceTitle}>{source.title}</h4>
            </div>
            <div className={styles.sourceMeta}>
              <span className={styles.sourceDomain}>{source.source}</span>
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
                <span>Open</span>
              </a>
            </div>
            <p className={styles.sourceSnippet}>{source.snippet}</p>
          </article>
        ))}
      </div>
    </div>
  );
};