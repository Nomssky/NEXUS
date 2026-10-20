/**
 * Badge primitive component
 * For status labels and small indicators
 */

import React from 'react';
import styles from './Badge.module.css';

export type BadgeVariant =
  | 'default'
  | 'success'
  | 'warning'
  | 'error'
  | 'info'
  | 'unknown'
  | 'idle'
  | 'running'
  | 'pending_approval'
  | 'completed'
  | 'failed'
  | 'cancelled'
  | 'timed_out';

export interface BadgeProps extends React.HTMLAttributes<HTMLSpanElement> {
  variant?: BadgeVariant;
  size?: 'sm' | 'md';
  dot?: boolean;
}

const variantMap: Record<BadgeVariant, string> = {
  default: styles.default,
  success: styles.success,
  warning: styles.warning,
  error: styles.error,
  info: styles.info,
  unknown: styles.unknown,
  idle: styles.idle,
  running: styles.running,
  pending_approval: styles.pending_approval,
  completed: styles.completed,
  failed: styles.failed,
  cancelled: styles.cancelled,
  timed_out: styles.timed_out,
};

export const Badge: React.FC<BadgeProps> = ({
  children,
  variant = 'default',
  size = 'md',
  dot = false,
  className = '',
  ...props
}) => {
  const classNames = [
    styles.badge,
    styles[size],
    variantMap[variant],
    dot ? styles.dot : '',
    className,
  ].filter(Boolean).join(' ');

  return (
    <span className={classNames} {...props}>
      {dot && <span className={styles.dotIndicator} aria-hidden="true" />}
      {children}
    </span>
  );
};

export const getStatusVariant = (status: string): BadgeVariant => {
  switch (status) {
    case 'idle':
      return 'idle';
    case 'running':
      return 'running';
    case 'pending_approval':
      return 'pending_approval';
    case 'completed':
      return 'completed';
    case 'failed':
      return 'failed';
    case 'cancelled':
      return 'cancelled';
    case 'timed_out':
      return 'timed_out';
    case 'unknown':
      return 'unknown';
    default:
      return 'default';
  }
};