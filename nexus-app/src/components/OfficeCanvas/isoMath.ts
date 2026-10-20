/**
 * Isometric math utilities for the office canvas.
 * Grid coordinates (gx, gy) map to screen coordinates via standard
 * 30° isometric projection. Z is vertical height in pixels.
 */

export const TILE = 36;
export const ISO_COS = 0.866; // cos(30°)
export const ISO_SIN = 0.5;   // sin(30°)

export interface IsoPoint {
  x: number;
  y: number;
}

export function iso(gx: number, gy: number, gz = 0): IsoPoint {
  return {
    x: (gx - gy) * TILE * ISO_COS,
    y: (gx + gy) * TILE * ISO_SIN - gz,
  };
}

/** Four floor corners of a room at grid position (gx, gy) with size (w, d). */
export function roomFloor(
  gx: number,
  gy: number,
  w: number,
  d: number
): [IsoPoint, IsoPoint, IsoPoint, IsoPoint] {
  return [
    iso(gx, gy),
    iso(gx + w, gy),
    iso(gx + w, gy + d),
    iso(gx, gy + d),
  ];
}

/** Back-left wall polygon (along x=gx, from y=gy to y=gy+d). */
export function wallLeft(
  gx: number,
  gy: number,
  d: number,
  h: number
): [IsoPoint, IsoPoint, IsoPoint, IsoPoint] {
  return [
    iso(gx, gy),
    iso(gx, gy + d),
    iso(gx, gy + d, h),
    iso(gx, gy, h),
  ];
}

/** Back-right wall polygon (along y=gy, from x=gx to x=gx+w). */
export function wallRight(
  gx: number,
  gy: number,
  w: number,
  h: number
): [IsoPoint, IsoPoint, IsoPoint, IsoPoint] {
  return [
    iso(gx, gy),
    iso(gx + w, gy),
    iso(gx + w, gy, h),
    iso(gx, gy, h),
  ];
}

export function pointsToString(points: IsoPoint[]): string {
  return points.map((p) => `${p.x},${p.y}`).join(' ');
}
