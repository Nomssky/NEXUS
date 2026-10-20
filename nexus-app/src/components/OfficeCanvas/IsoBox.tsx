/**
 * IsoBox — a rectangular prism rendered in isometric projection.
 * Used for desks, monitors, shelves, chairs, and other furniture.
 */

import React from 'react';
import { iso, pointsToString, type IsoPoint } from './isoMath';

export interface IsoBoxProps {
  gx: number;
  gy: number;
  w: number;
  d: number;
  h: number;
  z?: number; // base height offset (default 0)
  colorTop?: string;
  colorLeft?: string;
  colorRight?: string;
  stroke?: string;
  strokeWidth?: number;
  rx?: number; // corner radius (applied to top face via polygon — not true rounding)
  opacity?: number;
  className?: string;
}

export const IsoBox: React.FC<IsoBoxProps> = ({
  gx,
  gy,
  w,
  d,
  h,
  z = 0,
  colorTop = '#2A2E38',
  colorLeft = '#1E222B',
  colorRight = '#242833',
  stroke,
  strokeWidth = 0.5,
  opacity = 1,
  className = '',
}) => {
  // Top face
  const top: [IsoPoint, IsoPoint, IsoPoint, IsoPoint] = [
    iso(gx, gy, h + z),
    iso(gx + w, gy, h + z),
    iso(gx + w, gy + d, h + z),
    iso(gx, gy + d, h + z),
  ];

  // Left face (facing viewer-left, at y = gy+d)
  const left: [IsoPoint, IsoPoint, IsoPoint, IsoPoint] = [
    iso(gx, gy + d, z),
    iso(gx + w, gy + d, z),
    iso(gx + w, gy + d, h + z),
    iso(gx, gy + d, h + z),
  ];

  // Right face (facing viewer-right, at x = gx+w)
  const right: [IsoPoint, IsoPoint, IsoPoint, IsoPoint] = [
    iso(gx + w, gy, z),
    iso(gx + w, gy + d, z),
    iso(gx + w, gy + d, h + z),
    iso(gx + w, gy, h + z),
  ];

  return (
    <g className={className} opacity={opacity}>
      <polygon points={pointsToString(left)} fill={colorLeft} stroke={stroke} strokeWidth={strokeWidth} />
      <polygon points={pointsToString(right)} fill={colorRight} stroke={stroke} strokeWidth={strokeWidth} />
      <polygon points={pointsToString(top)} fill={colorTop} stroke={stroke} strokeWidth={strokeWidth} />
    </g>
  );
};
