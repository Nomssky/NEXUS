/**
 * VirtualHQ - The spatial workspace with zones
 * Command Center, Research, Media, Development, Meeting Room
 */

import React from 'react';
import { Badge, Card, CardContent } from '../primitives';
import styles from './VirtualHQ.module.css';

export type ZoneId = 'command' | 'research' | 'media' | 'development' | 'meeting';

export interface Zone {
  id: ZoneId;
  name: string;
  description: string;
  icon: React.ReactNode;
  color: string;
  isActive: boolean;
  isAvailable: boolean;
  agentCount?: number;
  executionCount?: number;
}

const ZONES: Zone[] = [
  {
    id: 'command',
    name: 'Command Center',
    description: 'Primary command interface for objectives and executions',
    icon: (
      <svg width="20" height="20" viewBox="0 0 20 20" fill="none" aria-hidden="true">
        <rect x="2" y="2" width="16" height="16" rx="2" stroke="currentColor" strokeWidth="1.5" />
        <path d="M6 10h8M10 6v8" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      </svg>
    ),
    color: 'var(--color-zone-command)',
    isActive: true,
    isAvailable: true,
  },
  {
    id: 'research',
    name: 'Research',
    description: 'Web research and information gathering agents',
    icon: (
      <svg width="20" height="20" viewBox="0 0 20 20" fill="none" aria-hidden="true">
        <circle cx="10" cy="10" r="7" stroke="currentColor" strokeWidth="1.5" />
        <path d="M10 15v5M15 15l-5-5 5-5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      </svg>
    ),
    color: 'var(--color-zone-research)',
    isActive: false,
    isAvailable: true,
  },
  {
    id: 'media',
    name: 'Media',
    description: 'Creative preparation and media task workspace',
    icon: (
      <svg width="20" height="20" viewBox="0 0 20 20" fill="none" aria-hidden="true">
        <rect x="3" y="4" width="14" height="12" rx="2" stroke="currentColor" strokeWidth="1.5" />
        <circle cx="10" cy="10" r="3" stroke="currentColor" strokeWidth="1.5" />
        <path d="M7 10h6M10 7v6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      </svg>
    ),
    color: 'var(--color-zone-media)',
    isActive: true,
    isAvailable: true,
  },
  {
    id: 'development',
    name: 'Development',
    description: 'Repository context, git operations, and development tasks',
    icon: (
      <svg width="20" height="20" viewBox="0 0 20 20" fill="none" aria-hidden="true">
        <path d="M6 4h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2z" stroke="currentColor" strokeWidth="1.5" />
        <path d="M8 8h4M8 12h4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      </svg>
    ),
    color: 'var(--color-zone-development)',
    isActive: true,
    isAvailable: true,
  },
  {
    id: 'meeting',
    name: 'Meeting Room',
    description: 'Coordination and delegation management',
    icon: (
      <svg width="20" height="20" viewBox="0 0 20 20" fill="none" aria-hidden="true">
        <circle cx="6" cy="6" r="4" stroke="currentColor" strokeWidth="1.5" />
        <circle cx="14" cy="6" r="4" stroke="currentColor" strokeWidth="1.5" />
        <circle cx="6" cy="14" r="4" stroke="currentColor" strokeWidth="1.5" />
        <circle cx="14" cy="14" r="4" stroke="currentColor" strokeWidth="1.5" />
        <path d="M10 6v8M6 10h8" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      </svg>
    ),
    color: 'var(--color-zone-meeting)',
    isActive: true,
    isAvailable: true,
  },
];

interface VirtualHQProps {
  activeZone: ZoneId;
  onZoneChange: (zone: ZoneId) => void;
  zoneStats?: Partial<Record<ZoneId, { agents?: number; executions?: number }>>;
}

export const VirtualHQ: React.FC<VirtualHQProps> = ({
  activeZone,
  onZoneChange,
  zoneStats,
}) => {
  return (
    <nav className={styles.hq} aria-label="Virtual HQ zones">
      <div className={styles.header}>
        <h2 className={styles.title}>Virtual HQ</h2>
        <p className={styles.subtitle}>Your digital workspace</p>
      </div>

      <ul className={styles.zoneList} role="tablist">
        {ZONES.map((zone) => {
          const stats = zoneStats?.[zone.id];
          const isActive = zone.id === activeZone;
          const isAvailable = zone.isAvailable;

          return (
            <li key={zone.id} role="tab" aria-selected={isActive}>
              <button
                className={`${styles.zoneButton} ${isActive ? styles.active : ''} ${!isAvailable ? styles.unavailable : ''}`}
                onClick={() => isAvailable && onZoneChange(zone.id)}
                disabled={!isAvailable}
                style={{ '--zone-color': zone.color } as React.CSSProperties}
                aria-label={`${zone.name} ${isActive ? '(active)' : ''} ${!isAvailable ? '(unavailable)' : ''}`}
              >
                <span className={styles.zoneIcon} style={{ color: zone.color }}>{zone.icon}</span>
                <div className={styles.zoneContent}>
                  <span className={styles.zoneName}>{zone.name}</span>
                  <span className={styles.zoneDescription}>{zone.description}</span>
                </div>
                {isAvailable && (
                  <div className={styles.zoneStats}>
                    {typeof stats?.agents === 'number' && (
                      <Badge variant="default" size="sm">
                        {stats.agents} registered
                      </Badge>
                    )}
                    {typeof stats?.executions === 'number' && stats.executions > 0 && (
                      <Badge variant="info" size="sm">
                        {stats.executions} this session
                      </Badge>
                    )}
                    {isActive && (
                      <Badge variant="success" size="sm" dot>
                        Active
                      </Badge>
                    )}
                  </div>
                )}
                {!isAvailable && (
                  <Badge variant="unknown" size="sm">
                    Unavailable
                  </Badge>
                )}
              </button>
            </li>
          );
        })}
      </ul>

      <div className={styles.legend}>
        <span className={styles.legendItem}>
          <span className={styles.legendDot} style={{ backgroundColor: 'var(--color-zone-command)' }} />
          Command
        </span>
        <span className={styles.legendItem}>
          <span className={styles.legendDot} style={{ backgroundColor: 'var(--color-zone-research)' }} />
          Research
        </span>
        <span className={styles.legendItem}>
          <span className={styles.legendDot} style={{ backgroundColor: 'var(--color-zone-media)' }} />
          Media
        </span>
        <span className={styles.legendItem}>
          <span className={styles.legendDot} style={{ backgroundColor: 'var(--color-zone-development)' }} />
          Development
        </span>
        <span className={styles.legendItem}>
          <span className={styles.legendDot} style={{ backgroundColor: 'var(--color-zone-meeting)' }} />
          Meeting
        </span>
      </div>
    </nav>
  );
};