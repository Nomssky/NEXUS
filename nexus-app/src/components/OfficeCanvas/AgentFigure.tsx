/**
 * AgentFigure — a small, understated isometric agent pawn.
 * Rendered as a shadow ellipse + capsule body + status-colored head.
 * Size is deliberately small relative to furniture.
 */

import React from 'react';
import { iso } from './isoMath';

export type AgentStatus = 'idle' | 'running' | 'pending_approval' | 'completed' | 'failed';

export const AGENT_STATUS_COLORS: Record<AgentStatus, string> = {
  idle: '#6B7480',
  running: '#1890FF',
  pending_approval: '#F5A623',
  completed: '#00D4AA',
  failed: '#FF4D4F',
};

export const AGENT_STATUS_LABELS: Record<AgentStatus, string> = {
  idle: 'Idle',
  running: 'Running',
  pending_approval: 'Pending approval',
  completed: 'Completed',
  failed: 'Failed',
};

export interface AgentFigureProps {
  gx: number;
  gy: number;
  status: AgentStatus;
  name?: string;
  selected?: boolean;
  onClick?: () => void;
  scale?: number;
}

export const AgentFigure: React.FC<AgentFigureProps> = ({
  gx,
  gy,
  status,
  name,
  selected = false,
  onClick,
  scale = 1,
}) => {
  const pos = iso(gx, gy);
  const color = AGENT_STATUS_COLORS[status];
  const s = scale;

  return (
    <g
      transform={`translate(${pos.x}, ${pos.y})`}
      onClick={onClick}
      style={{ cursor: onClick ? 'pointer' : 'default' }}
    >
      {/* Shadow */}
      <ellipse cx={0} cy={0} rx={5 * s} ry={2.5 * s} fill="rgba(0,0,0,0.35)" />

      {/* Selection ring */}
      {selected && (
        <ellipse
          cx={0}
          cy={0}
          rx={8 * s}
          ry={4 * s}
          fill="none"
          stroke={color}
          strokeWidth={1}
          opacity={0.8}
        />
      )}

      {/* Body capsule */}
      <rect
        x={-3 * s}
        y={-10 * s}
        width={6 * s}
        height={10 * s}
        rx={3 * s}
        fill="#2A2E38"
        stroke="#3A3F4A"
        strokeWidth={0.5}
      />

      {/* Head */}
      <circle cx={0} cy={-13 * s} r={3 * s} fill={color} />

      {/* Status glow for running agents */}
      {status === 'running' && (
        <circle cx={0} cy={-13 * s} r={5 * s} fill={color} opacity={0.25}>
          <animate
            attributeName="r"
            values={`${4 * s};${6 * s};${4 * s}`}
            dur="2s"
            repeatCount="indefinite"
          />
          <animate
            attributeName="opacity"
            values="0.3;0.1;0.3"
            dur="2s"
            repeatCount="indefinite"
          />
        </circle>
      )}

      {/* Name label on selection */}
      {selected && name && (
        <g transform={`translate(0, ${-20 * s})`}>
          <rect
            x={-20 * s}
            y={-8 * s}
            width={40 * s}
            height={14 * s}
            rx={3 * s}
            fill="rgba(11,13,18,0.85)"
            stroke={color}
            strokeWidth={0.5}
          />
          <text
            x={0}
            y={2 * s}
            textAnchor="middle"
            fontSize={7 * s}
            fontFamily="IBM Plex Mono, monospace"
            fill="#E8EBF0"
          >
            {name.length > 12 ? name.slice(0, 11) + '…' : name}
          </text>
        </g>
      )}
    </g>
  );
};
