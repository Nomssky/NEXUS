/**
 * OfficeTile - Individual isometric tile on the virtual office floor.
 * Renders a diamond-shaped tile matching the Penpot design.
 */

import React from 'react';
import styles from './OfficeFloor.module.css';
import type { TileColorVariant } from './OfficeFloor';

interface OfficeTileProps {
  x: number;
  y: number;
  colorVariant: TileColorVariant;
}

const TILE_COLORS: Record<string, string> = {
  slate: 'var(--color-surface-elevated)',
  teal: 'rgba(79, 209, 197, 0.15)',
  purple: 'rgba(177, 140, 255, 0.15)',
  orange: 'rgba(245, 183, 89, 0.15)',
  green: 'rgba(61, 214, 140, 0.15)',
  pink: 'rgba(255, 143, 163, 0.15)',
};

export const OfficeTile: React.FC<OfficeTileProps> = ({ x, y, colorVariant = 'slate' }) => {
  return (
    <div
      className={styles.tile}
      style={{
        left: x,
        top: y,
        backgroundColor: TILE_COLORS[colorVariant],
      }}
      aria-hidden="true"
    />
  );
};
