/**
 * DivisionSelector - Pill selector for the current division,
 * matching the Penpot design's division pill.
 */

import React from 'react';
import styles from './OfficeFloor.module.css';

interface DivisionSelectorProps {
  divisions: { id: string; name: string; color: string }[];
  activeDivisionId: string;
  onSelect: (id: string) => void;
}

const DIVISIONS: { id: string; name: string; color: string }[] = [
  { id: 'command', name: 'Command Center', color: 'var(--color-zone-command)' },
  { id: 'research', name: 'Research', color: 'var(--color-zone-research)' },
  { id: 'media', name: 'Media', color: 'var(--color-zone-media)' },
  { id: 'business', name: 'Business', color: 'var(--color-zone-development)' },
  { id: 'development', name: 'Development', color: 'var(--color-zone-development)' },
  { id: 'meeting', name: 'Meeting', color: 'var(--color-zone-meeting)' },
];

export const DivisionSelector: React.FC<DivisionSelectorProps> = ({
  divisions = DIVISIONS,
  activeDivisionId,
  onSelect,
}) => {
  return (
    <div className={styles.divisionBar}>
      <div className={styles.divisionPill}>
        <span className={styles.divisionDot} style={{ backgroundColor: divisions.find(d => d.id === activeDivisionId)?.color ?? 'var(--color-text-secondary)' }} />
        <span className={styles.divisionName}>{divisions.find(d => d.id === activeDivisionId)?.name ?? 'Command Center'}</span>
      </div>
      <button
        className={styles.newTaskButton}
        onClick={() => onSelect(activeDivisionId)}
        aria-label="New task"
      >
        <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
          <rect x="2" y="2" width="12" height="12" rx="2" stroke="currentColor" strokeWidth="1.5" />
          <path d="M6 9h4M8 6v4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
        </svg>
        <span className={styles.newTaskLabel}>New Task</span>
      </button>
    </div>
  );
};
