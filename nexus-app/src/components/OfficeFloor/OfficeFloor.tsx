/**
 * OfficeFloor - The virtual office floor from Penpot design.
 *
 * Renders:
 *   - Floor grid lines (hairline white at low opacity)
 *   - Office desk/workstation elements arranged in rows matching Penpot design
 *   - Agent figures positioned on top of desk surfaces
 *
 * Layout follows the Penpot "02 · Virtual Office — Dark" frame:
 * - Floor grid with hairline borders
 *   - Desk/workstation elements (rounded rectangles) arranged in office layout
 *   - Agent figures (circles/dots) positioned on desk surfaces
 *
 * Colors: Dark premium surfaces (#0b0e14 base, #11141A elevated surfaces)
 *   - Tile surfaces: #1a2540 to #1f2a3e (dark blue-gray)
 *   - Agent dots: Subtle accent colors (not neon)
 *   - Text: #E8EBF0 primary, #9AA3AE secondary, #6B7480 muted
 */

import React from 'react';
import { OfficeTile } from './OfficeTile';
import styles from './OfficeFloor.module.css';

const FLOOR_WIDTH = 720;
const FLOOR_HEIGHT = 420;
const GRID_SPACING = 20;

export type TileColorVariant = 'slate' | 'teal' | 'purple' | 'orange' | 'green' | 'pink' | string;

/**
 * Office desk/workstation positions matching Penpot design.
 * Arranged in rows matching the Penpot layout:
 *   - Row 1 (back): 6 desks spaced evenly
 *   - Row 2: 5 desks offset
 *   Row 3: 5 desks offset opposite direction
 *   - Row 4: 6 desks (bottom row)
 *
 * Desks have depth and perspective in the isometric view.
 */
const DESK_POSITIONS = [
  // Row 1 - back row (top of floor)
  { x: 120, y: 120 },
  { x: 220, y: 118 },
  { x: 340, y: 116 },
  { x: 460, y: 114 },
  { x: 580, y: 112 },
  // Row 2
  { x: 80, y: 200 },
  { x: 240, y: 196 },
  { x: 400, y: 192 },
  { x: 560, y: 188 },
  // Row 3
  { x: 140, y: 280 },
  { x: 300, y: 274 },
  { x: 460, y: 268 },
  { x: 620, y: 262 },
  // Row 4 - front row
  { x: 180, y: 340 },
  { x: 380, y: 332 },
  { x: 580, y: 324 },
];

// Agent figures positioned on desk surfaces
// Positioned slightly offset from desk center
const AGENT_POSITIONS = [
  { id: 'agent-1', x: 130, y: 115, name: 'Research Agent', status: 'working', color: '#4e7a9f' },
  { id: 'agent-2', x: 250, y: 115, name: 'Media Agent', status: 'working', color: '#608ca0' },
  { id: 'agent-3', x: 350, y: 115, name: 'Business Agent', status: 'working', color: '#7864aa' },
  { id: 'agent-4', x: 490, y: 114, name: 'Dev Agent', status: 'working', color: '#50a08c' },
  { id: 'agent-5', x: 580, y: 113, name: 'QA Agent', status: 'pending', color: '#b48250' },
  { id: 'agent-6', x: 100, y: 188, name: 'Ops Agent', status: 'running', color: '#e67e22' },
  { id: 'agent-7', x: 280, y: 187, name: 'Data Agent', status: 'active', color: '#6ab04c' },
  { id: 'agent-8', x: 400, y: 185, name: 'Security Agent', status: 'idle', color: '#f1c40f' },
  { id: 'agent-9', x: 560, y: 185, name: 'UX Agent', status: 'working', color: '#9b59b6' },
  { id: 'agent-10', x: 480, y: 268, name: 'Ops Manager', status: 'working', color: '#e74c3c' },
];

export const OfficeFloor: React.FC = () => {
  // Grid vertical lines
  const verticalLines = [];
  for (let x = 0; x <= FLOOR_WIDTH; x += GRID_SPACING) {
    verticalLines.push(x);
  }

  // Grid horizontal lines
  const horizontalLines = [];
  for (let y = 0; y <= FLOOR_HEIGHT; y += GRID_SPACING) {
    horizontalLines.push(y);
  }

  return (
    <div className={styles.floorContainer}>
      <div className={styles.floor}>
        {/* Grid lines */}
        {verticalLines.map((x) => (
          <div key={`v-${x}`} className={styles.gridLineV} style={{ left: x }} />
        ))}
        {horizontalLines.map((y) => (
          <div key={`h-${y}`} className={styles.gridLineH} style={{ top: y }} />
        ))}

        {/* Desk tiles */}
        {DESK_POSITIONS.map((desk, idx) => (
          <OfficeTile
            key={`desk-${idx}`}
            x={desk.x}
            y={desk.y}
            colorVariant="slate"
            className={styles.desk}
          />
        ))}

        {/* Agent figures positioned on desks */}
        {AGENT_POSITIONS.map((agent) => (
          <OfficeTile
            key={agent.id}
            x={agent.x}
            y={agent.y}
            colorVariant={agent.color as TileColorVariant}
            className={styles.agentFigure}
          />
        ))}
      </div>
    </div>
  );
};