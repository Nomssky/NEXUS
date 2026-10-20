/**
 * NEXUS Design Tokens
 * A coherent design-token system for the Command Center & Virtual HQ
 */

// Color tokens - restrained, premium, quiet intelligence
export const colors = {
  // Base surfaces
  surface: {
    base: '#0B0D12',        // Deepest background
    elevated: '#11141A',    // Cards, panels
    overlay: '#161A22',     // Modals, dropdowns
    border: '#1E232E',      // Subtle borders
  },

  // Text hierarchy
  text: {
    primary: '#E8EBF0',     // Primary content
    secondary: '#9AA3AE',   // Secondary, descriptions
    muted: '#6B7480',       // Placeholders, disabled
    inverse: '#0B0D12',     // On primary actions
    accent: '#00D4AA',      // Accent links, highlights
  },

  // Semantic states
  state: {
    success: '#00D4AA',
    warning: '#F5A623',
    error: '#FF4D4F',
    info: '#1890FF',
    unknown: '#8C8C8C',
  },

  // Agent status colors
  agent: {
    idle: '#6B7480',
    running: '#1890FF',
    pending_approval: '#F5A623',
    completed: '#00D4AA',
    failed: '#FF4D4F',
    cancelled: '#8C8C8C',
    timed_out: '#FF4D4F',
    unknown: '#8C8C8C',
  },

  // Accent for interactive elements
  accent: {
    primary: '#00D4AA',
    primaryHover: '#00E8BB',
    primaryActive: '#00C099',
    focus: '#1890FF',
  },

  // Virtual HQ zones
  zones: {
    command: '#00D4AA',
    research: '#1890FF',
    media: '#722ED1',
    development: '#13C2C2',
    meeting: '#FA8C16',
  },
} as const;

// Spacing scale - 4px base unit
export const spacing = {
  0: '0',
  1: '4px',
  2: '8px',
  3: '12px',
  4: '16px',
  5: '20px',
  6: '24px',
  8: '32px',
  10: '40px',
  12: '48px',
  16: '64px',
  20: '80px',
  24: '96px',
} as const;

// Typography
export const typography = {
  fontFamilies: {
    ui: '"IBM Plex Sans", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
    mono: '"IBM Plex Mono", "SF Mono", "Fira Code", monospace',
    display: '"IBM Plex Sans", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
  },

  fontSizes: {
    xs: '12px',
    sm: '14px',
    base: '16px',
    lg: '18px',
    xl: '20px',
    '2xl': '24px',
    '3xl': '30px',
    '4xl': '36px',
    '5xl': '48px',
  },

  fontWeights: {
    regular: 400,
    medium: 500,
    semibold: 600,
    bold: 700,
  },

  lineHeights: {
    tight: 1.2,
    normal: 1.5,
    relaxed: 1.75,
  },

  letterSpacing: {
    tight: '-0.02em',
    normal: '0',
    wide: '0.02em',
  },
} as const;

// Border radius
export const radii = {
  none: '0',
  sm: '4px',
  md: '8px',
  lg: '12px',
  xl: '16px',
  full: '9999px',
} as const;

// Shadows - minimal, purposeful
export const shadows = {
  none: 'none',
  sm: '0 1px 2px rgba(0, 0, 0, 0.3)',
  md: '0 4px 8px rgba(0, 0, 0, 0.3)',
  lg: '0 8px 24px rgba(0, 0, 0, 0.4)',
  xl: '0 16px 48px rgba(0, 0, 0, 0.5)',
  focus: '0 0 0 3px rgba(24, 144, 255, 0.4)',
} as const;

// Transitions
export const transitions = {
  fast: '120ms ease-out',
  normal: '200ms ease-out',
  slow: '300ms ease-out',
} as const;

// Z-index layers
export const zIndex = {
  base: 0,
  dropdown: 100,
  sticky: 200,
  modal: 300,
  popover: 400,
  tooltip: 500,
  toast: 600,
} as const;

// Breakpoints
export const breakpoints = {
  sm: '640px',
  md: '768px',
  lg: '1024px',
  xl: '1280px',
  '2xl': '1536px',
} as const;

// Motion preferences
export const motion = {
  reduce: '(prefers-reduced-motion: reduce)',
} as const;

// Export all tokens as a single object for convenience
export const tokens = {
  colors,
  spacing,
  typography,
  radii,
  shadows,
  transitions,
  zIndex,
  breakpoints,
  motion,
} as const;

export type ColorToken = typeof colors;
export type SpacingToken = typeof spacing;
export type TypographyToken = typeof typography;
export type RadiiToken = typeof radii;
export type ShadowsToken = typeof shadows;
export type TransitionsToken = typeof transitions;
export type ZIndexToken = typeof zIndex;
export type BreakpointsToken = typeof breakpoints;