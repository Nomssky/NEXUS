/**
 * RepositoryContext - Shows git repository context
 * Displays git status, diff, and log results from actual execution evidence
 */

import React, { useMemo } from 'react';
import { Card, Badge } from '../primitives';
import { parseGitStatus, type ParsedGitStatus, parseGitDiff, type ParsedGitDiffFile, parseGitLog } from '../../types/api';
import styles from './RepositoryContext.module.css';

interface RepositoryContextProps {
  gitResult?: { output: string; truncated?: string; repo?: string };
  operation?: 'status' | 'diff' | 'log';
  executionId?: string;
  onViewTrace?: (executionId: string) => void;
}

export const RepositoryContext: React.FC<RepositoryContextProps> = ({
  gitResult,
  operation = 'status',
  executionId,
  onViewTrace,
}) => {
  const parsedStatus = useMemo(() => {
    if (!gitResult?.output || operation !== 'status') return null;
    return parseGitStatus(gitResult.output);
  }, [gitResult?.output, operation]);

  const parsedDiff = useMemo(() => {
    if (!gitResult?.output || operation !== 'diff') return null;
    return parseGitDiff(gitResult.output);
  }, [gitResult?.output, operation]);

  const parsedLog = useMemo(() => {
    if (!gitResult?.output || operation !== 'log') return null;
    return parseGitLog(gitResult.output);
  }, [gitResult?.output, operation]);

  if (!gitResult) {
    return (
      <Card variant="outlined" padding="md">
        <div className={styles.empty}>
          <svg width="48" height="48" viewBox="0 0 48 48" fill="none" aria-hidden="true">
            <circle cx="24" cy="24" r="20" stroke="var(--color-surface-border)" strokeWidth="2" />
            <path d="M24 16v16M16 24h16" stroke="var(--color-text-muted)" strokeWidth="2" strokeLinecap="round" />
          </svg>
          <p>No repository context available</p>
          <span>Run a git task (status, diff, log) to see repository state</span>
        </div>
      </Card>
    );
  }

  const isTruncated = gitResult.truncated === 'true';

  return (
    <Card variant="default" padding="none" className={styles.card}>
      <div className={styles.header}>
        <div className={styles.headerLeft}>
          <h3 className={styles.title}>
            Repository Context
            <span className={styles.repoName}>{gitResult.repo || 'workspace'}</span>
          </h3>
          <div className={styles.badges}>
            <Badge variant="info" size="sm">{operation}</Badge>
            {isTruncated && <Badge variant="warning" size="sm">Output truncated</Badge>}
          </div>
        </div>
        <div className={styles.headerRight}>
          {executionId && (
            <button
              className={styles.traceButton}
              onClick={() => onViewTrace?.(executionId)}
              aria-label="View execution trace"
            >
              <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
                <path d="M3 8L8 3M8 3l5 5M8 3v10" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
              </svg>
              Trace
            </button>
          )}
        </div>
      </div>

      <div className={styles.content}>
        {operation === 'status' && parsedStatus && (
          <GitStatusView status={parsedStatus} />
        )}

        {operation === 'diff' && parsedDiff && (
          <GitDiffView files={parsedDiff} />
        )}

        {operation === 'log' && parsedLog && (
          <GitLogView entries={parsedLog} />
        )}
      </div>

      {gitResult.truncated === 'true' && gitResult.output && (
        <div className={styles.truncatedNotice}>
          Output was truncated ({gitResult.output.length} chars). Increase output limit or narrow the query.
        </div>
      )}
    </Card>
  );
};

const GitStatusView: React.FC<{ status: ParsedGitStatus }> = ({ status }) => {
  const hasChanges = status.staged.length > 0 || status.unstaged.length > 0 || status.untracked.length > 0;

  return (
    <div className={styles.statusView}>
      <div className={styles.statusHeader}>
        <span className={styles.branch}>
          <svg width="14" height="14" viewBox="0 0 14 14" fill="none" aria-hidden="true">
            <path d="M3 3l4 4 4-4M7 3v11" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
          </svg>
          <span>{status.branch || 'detached'}</span>
        </span>
        {hasChanges ? (
          <Badge variant="warning" size="sm">Changes</Badge>
        ) : (
          <Badge variant="success" size="sm" dot>Clean</Badge>
        )}
      </div>

      {status.staged.length > 0 && (
        <div className={styles.section}>
          <h4 className={styles.sectionTitle}>Staged ({status.staged.length})</h4>
          <ul className={styles.fileList}>
            {status.staged.map((item, idx) => (
              <li key={idx} className={styles.fileItem}>
                <span className={getStatusBadgeClass(item.status)}>{item.status}</span>
                <span className={styles.filePath}>{item.path}</span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {status.unstaged.length > 0 && (
        <div className={styles.section}>
          <h4 className={styles.sectionTitle}>Unstaged ({status.unstaged.length})</h4>
          <ul className={styles.fileList}>
            {status.unstaged.map((item, idx) => (
              <li key={idx} className={styles.fileItem}>
                <span className={getStatusBadgeClass(item.status)}>{item.status}</span>
                <span className={styles.filePath}>{item.path}</span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {status.untracked.length > 0 && (
        <div className={styles.section}>
          <h4 className={styles.sectionTitle}>Untracked ({status.untracked.length})</h4>
          <ul className={styles.fileList}>
            {status.untracked.map((path, idx) => (
              <li key={idx} className={styles.fileItem}>
                <span className={styles.statusBadgeUntracked}>?</span>
                <span className={styles.filePath}>{path}</span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {!hasChanges && (
        <div className={styles.cleanMessage}>
          <svg width="24" height="24" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="1.5" />
            <path d="M8 12l3 3 7-7" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
          </svg>
          <p>Working tree clean. No changes to commit.</p>
        </div>
      )}
    </div>
  );
};

function getStatusBadgeClass(status: string): string {
  // Use string concatenation to build class name dynamically
  return `${styles.statusBadge} ${styles['status' + status.charAt(0).toUpperCase() + status.slice(1)] || styles.statusUnknown}`;
}

const GitDiffView: React.FC<{ files: ParsedGitDiffFile[] }> = ({ files }) => {
  if (files.length === 0) {
    return (
      <div className={styles.emptyDiff}>
        <svg width="48" height="48" viewBox="0 0 48 48" fill="none" aria-hidden="true">
          <circle cx="24" cy="24" r="20" stroke="var(--color-surface-border)" strokeWidth="2" />
          <path d="M16 24h16M24 16v16" stroke="var(--color-text-muted)" strokeWidth="2" strokeLinecap="round" />
        </svg>
        <p>No diff output</p>
        <span>No changes or diff is empty</span>
      </div>
    );
  }

  return (
    <div className={styles.diffView}>
      <div className={styles.diffSummary}>
        <Badge variant="info" size="sm">
          {files.length} file{files.length !== 1 ? 's' : ''} changed
        </Badge>
        {files.some(f => f.isNew) && <Badge variant="success" size="sm">New</Badge>}
        {files.some(f => f.isDeleted) && <Badge variant="error" size="sm">Deleted</Badge>}
      </div>
      <div className={styles.diffFiles}>
        {files.map((file, idx) => (
          <div key={idx} className={styles.diffFile}>
            <div className={styles.diffFileHeader}>
              <span className={styles.diffFilePath}>
                {file.isNew ? '+' : file.isDeleted ? '-' : ''}
                {file.newPath || file.oldPath}
              </span>
              {file.isBinary && <Badge variant="warning" size="sm">Binary</Badge>}
            </div>
            {file.hunks.map((hunk, hIdx) => (
              <div key={hIdx} className={styles.diffHunk}>
                <div className={styles.hunkHeader}>
                  @@ -{hunk.oldStart},{hunk.oldLines} +{hunk.newStart},{hunk.newLines} @@
                </div>
                <pre className={styles.hunkContent}>
                  {hunk.lines.map((line, lIdx) => (
                    <span key={lIdx} className={`${styles.diffLine} ${styles['diffLine' + line.type.charAt(0).toUpperCase() + line.type.slice(1)]}`}>
                      {line.type === 'add' ? '+' : line.type === 'remove' ? '-' : ' '}
                      {line.content}
                    </span>
                  ))}
                </pre>
              </div>
            ))}
          </div>
        ))}
      </div>
    </div>
  );
};

const GitLogView: React.FC<{ entries: Array<{ hash: string; author: string; message: string }> }> = ({ entries }) => {
  if (entries.length === 0) {
    return (
      <div className={styles.emptyDiff}>
        <svg width="48" height="48" viewBox="0 0 48 48" fill="none" aria-hidden="true">
          <circle cx="24" cy="24" r="20" stroke="var(--color-surface-border)" strokeWidth="2" />
          <path d="M16 24h16M24 16v16" stroke="var(--color-text-muted)" strokeWidth="2" strokeLinecap="round" />
        </svg>
        <p>No commit history</p>
        <span>No commits found or history is empty</span>
      </div>
    );
  }

  return (
    <div className={styles.logView}>
      {entries.map((entry, idx) => (
        <div key={idx} className={styles.logEntry}>
          <div className={styles.logEntryHeader}>
            <span className={styles.commitHash}>{entry.hash}</span>
            <span className={styles.commitAuthor}>{entry.author}</span>
          </div>
          <p className={styles.commitMessage}>{entry.message}</p>
        </div>
      ))}
    </div>
  );
};