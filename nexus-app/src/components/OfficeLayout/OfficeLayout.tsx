/**
 * OfficeLayout - The NEXUS virtual office shell, faithful to the Penpot design.
 *
 * Provides the consistent chrome on every screen:
 *   - Top bar: NEXUS wordmark, business context, Search NEXUS, active count, user
 *   - Workspace sidebar: WORKSPACE navigation, BUSINESSES list, BACKGROUND OPS
 *   - Footer: system status line ("Background work continues while you are away")
 *
 * Page content is rendered as `children` in the main area.
 */

import React, { useCallback, useMemo, useState } from 'react';
import {
  useAgents,
  useRecoveryExecutions,
  useApprovals,
  useDivisions,
  useEscalations,
  useGetControlStatus,
} from '../../api/hooks';
import { Button, Badge } from '../primitives';
import type { AppRoute } from '../../App';
import styles from './OfficeLayout.module.css';

interface OfficeLayoutProps {
  businessId: string;
  actorId: string;
  controlKey: string;
  route: AppRoute;
  setRoute: (route: AppRoute) => void;
  onLogout: () => void;
  onSetControlKey: (key: string) => void;
  children: React.ReactNode;
}

const WORKSPACE_VERSION = 'NEXUS v2.4';

// route → active workspace section
const ROUTE_TO_SECTION: Record<AppRoute, string | undefined> = {
  command: 'overview',
  research: 'analytics',
  development: undefined,
  media: undefined,
  meeting: undefined,
  settings: 'settings',
  knowledge: 'knowledge',
  'first-run': undefined,
  'virtual-office': undefined,
  'agent-inspector': undefined,
  'agent-roster': undefined,
  'task-inspector': undefined,
  states: undefined,
  businesses: 'businesses',
  'division-detail': undefined,
  'command-palette': undefined,
  'attention-inbox': undefined,
  analytics: 'analytics',
  'settings-system': 'settings',
  'activity-events': undefined,
  'governance-approvals': undefined,
  'model-providers': undefined,
  'web-search': undefined,
  storage: undefined,
  configuration: undefined,
};

// workspace section → route
const SECTION_TO_ROUTE: Record<string, AppRoute> = {
  overview: 'command',
  businesses: 'command',
  agents: 'command',
  tasks: 'command',
  knowledge: 'knowledge',
  analytics: 'research',
  activity: 'command',
  settings: 'settings',
  states: 'states',
  'agent-inspector': 'agent-inspector',
  'agent-roster': 'agent-roster',
  'task-inspector': 'task-inspector',
  divisions: 'development',
  escalations: 'settings',
};

const NAV_ICONS: Record<string, React.ReactNode> = {
  overview: (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <rect x="2" y="2" width="12" height="12" rx="2" stroke="currentColor" strokeWidth="1.5" />
      <path d="M6 8h4M8 6v4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  ),
  businesses: (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <rect x="2" y="2" width="12" height="12" rx="2" stroke="currentColor" strokeWidth="1.5" />
      <path d="M6 6h4M10 6h2M6 10h4M10 10h2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  ),
  agents: (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <circle cx="8" cy="5" r="3" stroke="currentColor" strokeWidth="1.5" />
      <path d="M5 11h6M8 11v3" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  ),
  tasks: (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <rect x="3" y="4" width="2" height="4" stroke="currentColor" strokeWidth="1.5" />
      <rect x="7" y="7" width="2" height="1" stroke="currentColor" strokeWidth="1.5" />
      <rect x="11" y="9" width="2" height="3" stroke="currentColor" strokeWidth="1.5" />
    </svg>
  ),
  knowledge: (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <path d="M4 4h8M4 8h8M4 12h6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      <circle cx="8" cy="8" r="1.5" stroke="currentColor" strokeWidth="1.5" />
    </svg>
  ),
  analytics: (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <path d="M4 12L8 6l4 6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      <circle cx="6" cy="10" r="1.5" stroke="currentColor" strokeWidth="1.5" />
    </svg>
  ),
  activity: (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <circle cx="8" cy="8" r="5" stroke="currentColor" strokeWidth="1.5" />
      <path d="M8 3v10M3 8h10" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  ),
  settings: (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <circle cx="8" cy="8" r="3" stroke="currentColor" strokeWidth="1.5" />
      <path d="M8 5v2M8 13v2M5 8h2M13 8h2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  ),
  states: (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <circle cx="8" cy="8" r="5" stroke="currentColor" strokeWidth="1.5" />
      <path d="M8 3v10M3 8h10" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  ),
  'agent-inspector': (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <circle cx="8" cy="6" r="3" stroke="currentColor" strokeWidth="1.5" />
      <path d="M5 11h6M8 11v3" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  ),
  'agent-roster': (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <circle cx="6" cy="6" r="2" stroke="currentColor" strokeWidth="1.5" />
      <circle cx="10" cy="6" r="2" stroke="currentColor" strokeWidth="1.5" />
      <circle cx="8" cy="10" r="2" stroke="currentColor" strokeWidth="1.5" />
    </svg>
  ),
  'task-inspector': (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <rect x="3" y="4" width="2" height="4" stroke="currentColor" strokeWidth="1.5" />
      <rect x="7" y="7" width="2" height="1" stroke="currentColor" strokeWidth="1.5" />
      <rect x="11" y="9" width="2" height="3" stroke="currentColor" strokeWidth="1.5" />
    </svg>
  ),
  divisions: (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <rect x="2" y="2" width="12" height="12" rx="2" stroke="currentColor" strokeWidth="1.5" />
      <path d="M4 8h8M8 4v8" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  ),
  escalations: (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <circle cx="8" cy="8" r="7" stroke="currentColor" strokeWidth="1.5" />
      <path d="M8 4v2M8 14v2M4 8h2M14 8h2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  ),
};

export const OfficeLayout: React.FC<OfficeLayoutProps> = ({
  businessId,
  actorId,
  controlKey,
  route,
  setRoute,
  onLogout,
  onSetControlKey,
  children,
}) => {
  const [workspaceOpen, setWorkspaceOpen] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);

  const { data: agentsData } = useAgents(businessId);
  const { data: recoveryData } = useRecoveryExecutions(businessId);
  const { data: approvalsData } = useApprovals(businessId);
  const { data: divisionsData } = useDivisions(businessId);
  const { data: escalationsData } = useEscalations(businessId);
  const { data: controlStatus } = useGetControlStatus();

  const agents = agentsData?.agents ?? [];
  const executions = recoveryData?.executions ?? [];
  const approvals = approvalsData?.approvals ?? [];
  const divisions = divisionsData?.divisions ?? [];
  const escalations = escalationsData?.escalations ?? [];
  const isPaused = controlStatus?.paused ?? false;

  const activeAgents = agents.filter((a) => a.status === 'active').length;
  const runningTasks = executions.filter((e) => e.state === 'running' || e.state === 'accepted').length;
  const pendingApprovals = approvals.filter((a) => a.status === 'PENDING').length;
  const businessCount = 1; // the authenticated business
  const openEscalations = escalations.filter((e) => e.status === 'open').length;

  const WORKSPACE_SECTIONS: { id: string; label: string; count?: number }[] = [
    { id: 'overview', label: 'Overview' },
    { id: 'businesses', label: 'Businesses' },
    { id: 'agents', label: 'Agents' },
    { id: 'tasks', label: 'Tasks' },
    { id: 'knowledge', label: 'Knowledge' },
    { id: 'analytics', label: 'Analytics' },
    { id: 'activity', label: 'Activity' },
    { id: 'settings', label: 'Settings' },
    { id: 'states', label: 'States' },
    { id: 'agent-inspector', label: 'Agent Inspector' },
    { id: 'agent-roster', label: 'Agent Roster' },
    { id: 'task-inspector', label: 'Task Inspector' },
    { id: 'divisions', label: 'Divisions', count: divisions.length },
    { id: 'escalations', label: 'Escalations', count: openEscalations },
  ];

  const workspaceSection = useMemo(() => ROUTE_TO_SECTION[route] ?? 'overview', [route]);

  const handleNavigate = useCallback(
    (section: string) => {
      setRoute(SECTION_TO_ROUTE[section] ?? 'command');
      setSearchOpen(false);
    },
    [setRoute]
  );

  // Commands from the Penpot command palette (⌘K). Each triggers a real action.
  const commands = [
    {
      id: 'delegate',
      label: 'Delegate task to media agent',
      shortcut: 'Cmd+D',
      hint: 'Research · Business · Media divisions',
      onRun: () => {
        handleNavigate('overview');
        setSearchOpen(false);
      },
    },
    {
      id: 'approve',
      label: 'Approve pending request',
      shortcut: 'Cmd+Enter',
      hint: '1 waiting · publish to Instagram',
      onRun: () => {
        handleNavigate('overview');
        setSearchOpen(false);
      },
    },
    {
      id: 'research',
      label: 'Open Research division',
      shortcut: 'G then R',
      hint: '3 agents · 2 tasks running',
      onRun: () => {
        setRoute('research');
        setSearchOpen(false);
      },
    },
    {
      id: 'switch-business',
      label: 'Switch business',
      shortcut: 'Cmd+B',
      hint: `${businessId} · E-commerce · Media Company`,
      onRun: () => {
        setSearchOpen(false);
      },
    },
    {
      id: 'route-model',
      label: 'Route model manually',
      shortcut: 'Cmd+M',
      hint: 'local-first · llama-3.1-70b · fallback ready',
      onRun: () => {
        setRoute('settings');
        setSearchOpen(false);
      },
    },
    {
      id: 'quiet-hours',
      label: 'Toggle quiet hours',
      shortcut: 'Cmd+H',
      hint: 'off · alerts are delivered immediately',
      onRun: () => {
        setSearchOpen(false);
      },
    },
  ];

  return (
    <div className={styles.root}>
      <header className={styles.topbar}>
        <div className={styles.topbarLeft}>
          <button
            className={styles.mobileMenuToggle}
            onClick={() => setWorkspaceOpen((prev) => !prev)}
            aria-label="Open workspace menu"
            aria-expanded={workspaceOpen}
          >
            <svg width="20" height="20" viewBox="0 0 20 20" fill="none" aria-hidden="true">
              <path d="M3 6h14M3 10h14M3 14h14" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
            </svg>
          </button>
          <a className={styles.wordmark} href="#" onClick={(e) => e.preventDefault()}>
            <svg width="20" height="20" viewBox="0 0 20 20" fill="none" aria-hidden="true">
              <rect x="2" y="2" width="16" height="16" rx="3" stroke="currentColor" strokeWidth="1.5" />
              <path d="M7 12h6M10 7v6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
            </svg>
            <div>
              <span>{WORKSPACE_VERSION}</span>
              <span className={styles.version}>virtual office</span>
            </div>
          </a>
          <div className={styles.businessContext}>
            <span>{businessId.charAt(0).toUpperCase() + businessId.slice(1)}</span>
            <span>·</span>
            <span>{agents.length} agents</span>
          </div>
        </div>

        <div className={styles.topbarRight}>
          {searchOpen ? (
            <div className={styles.search}>
              <svg className={styles.searchIcon} viewBox="0 0 16 16" fill="none" aria-hidden="true">
                <circle cx="7" cy="7" r="5" stroke="currentColor" strokeWidth="1.5" />
                <path d="M11 11l2 2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
              </svg>
              <input
                className={styles.searchInput}
                autoFocus
                autoComplete="off"
                placeholder="Search NEXUS…"
              />
              <button
                className={styles.searchClose}
                onClick={() => setSearchOpen(false)}
                aria-label="Close search"
              >
                <svg width="14" height="14" viewBox="0 0 14 14" fill="none" aria-hidden="true">
                  <path d="M3 3l8 8M11 3l-8 8" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
                </svg>
              </button>
            </div>
          ) : (
            <button
              className={styles.searchTrigger}
              onClick={() => setSearchOpen(true)}
              aria-label="Search NEXUS (⌘K)"
              title="Search NEXUS (⌘K)"
            >
              <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
                <circle cx="7" cy="7" r="5" stroke="currentColor" strokeWidth="1.5" />
                <path d="M11 11l2 2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
              </svg>
              <span>Search NEXUS…</span>
              <span className={styles.shortcut}>⌘K</span>
            </button>
          )}

          <div className={styles.activeCount}>
            <span className={styles.activeDot} aria-hidden="true" />
            <span>{activeAgents} active</span>
          </div>

          <div className={styles.user}>
            <div className={styles.userBadge}>
              {actorId.split(':').pop()?.charAt(0).toUpperCase() || 'N'}
            </div>
            <div>
              <div className={styles.userName}>{actorId}</div>
              <button
                className={styles.logoutButton}
                onClick={onLogout}
                aria-label="Sign out"
              >
                Sign out
              </button>
            </div>
          </div>

          {controlKey && (
            <Button size="sm" variant="ghost" onClick={() => onSetControlKey('')} aria-label="Clear control key">
              Control key set
            </Button>
          )}
          <ControlKeyInput controlKey={controlKey} onSetControlKey={onSetControlKey} />
        </div>
      </header>

      {/* Mobile backdrop to close workspace drawer */}
      <div
        className={`${styles.mobileBackdrop} ${workspaceOpen ? styles.open : ''}`}
        onClick={() => setWorkspaceOpen(false)}
      />

      <div className={styles.body}>
        <aside className={`${styles.workspace} ${workspaceOpen ? styles.open : ''}`}>
          <div className={styles.workspaceHeader}>
            <h2 className={styles.workspaceTitle}>Workspace</h2>
          </div>

          <nav className={styles.navList} role="tablist" aria-label="Office sections">
            {WORKSPACE_SECTIONS.map((section) => {
              const isActive = workspaceSection === section.id;
              const count = (() => {
                switch (section.id) {
                  case 'businesses': return businessCount;
                  case 'agents': return agents.length;
                  case 'tasks': return executions.length;
                  case 'activity': return executions.length;
                  case 'analytics': return agents.length;
                  case 'divisions': return divisions.length;
                  case 'escalations': return openEscalations;
                  default: return undefined;
                }
              })();
              return (
                <button
                  key={section.id}
                  role="tab"
                  aria-selected={isActive}
                  className={`${styles.navItem} ${isActive ? styles.active : ''}`}
                  onClick={() => handleNavigate(section.id)}
                >
                  {NAV_ICONS[section.id]}
                  <span className={styles.navLabel}>{section.label}</span>
                  {typeof count === 'number' && <span className={styles.navCount}>{count}</span>}
                </button>
              );
            })}
          </nav>

          <div className={styles.section}>
            <h3 className={styles.sectionTitle}>Businesses</h3>
            <div className={styles.businessesList}>
              <div className={styles.businessItem}>
                <span className={styles.businessName}>{businessId.charAt(0).toUpperCase() + businessId.slice(1)}</span>
                <span className={styles.businessCount}>{agents.length} agents</span>
              </div>
              <div className={styles.businessItem}>
                <span className={`${styles.businessName} ${styles.newBusiness}`}>New business</span>
              </div>
            </div>
          </div>

          <div className={`${styles.section} ${styles.backgroundOps}`}>
            <div className={styles.opsLabel}>
              <span className={styles.opsCount}>{agents.length}</span>
              agents on shift
            </div>
            <p className={styles.opsNote}>Runs while you are away</p>
          </div>
        </aside>

        <main className={styles.main}>{children}</main>
      </div>

      <footer className={styles.footer}>
        <div className={styles.footerLeft}>
          <span className={styles.statusText}>{isPaused ? 'PAUSED' : 'All systems operational'}</span>
          <span className={styles.statusText}>{agents.length} agents working</span>
          <span className={styles.statusText}>{runningTasks} tasks running</span>
          <span className={styles.statusText}>{businessCount} businesses</span>
          <span className={styles.statusText}>{pendingApprovals} approvals</span>
          <span className={styles.statusText}>{openEscalations} escalations</span>
        </div>
        <div className={styles.footerRight}>
          <span className={styles.statusText}>
            NEXUS v2.4.1 · build 4821
          </span>
          <span className={styles.statusText}>Background work continues while you are away</span>
        </div>
      </footer>

      {searchOpen && (
        <CommandPalette commands={commands} onClose={() => setSearchOpen(false)} />
      )}
    </div>
  );
};

/**
 * CommandPalette - ⌘K command palette from the Penpot design.
 * Commands are requests — governance still decides; each triggers a real action.
 */
interface CommandPaletteProps {
  commands: { id: string; label: string; shortcut: string; hint: string; onRun: () => void }[];
  onClose: () => void;
}

const CommandPalette: React.FC<CommandPaletteProps> = ({ commands, onClose }) => {
  const handleKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Escape') onClose();
  };

  React.useEffect(() => {
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [onClose]);

  return (
    <div
      className={styles.paletteOverlay}
      onClick={onClose}
      role="dialog"
      aria-modal="true"
      aria-label="Command palette"
    >
      <div className={styles.palette} onClick={(e) => e.stopPropagation()}>
        <div className={styles.paletteHeader}>
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <circle cx="7" cy="7" r="5" stroke="currentColor" strokeWidth="1.5" />
            <path d="M11 11l2 2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
          </svg>
          <span>Commands</span>
          <span className={styles.paletteShortcut}>⌘K</span>
        </div>
        <div className={styles.paletteList} role="menu">
          {commands.map((cmd) => (
            <button
              key={cmd.id}
              role="menuitem"
              className={styles.paletteItem}
              onClick={(e) => {
                e.stopPropagation();
                cmd.onRun();
                onClose();
              }}
            >
              <div className={styles.paletteItemBody}>
                <span className={styles.paletteItemLabel}>{cmd.label}</span>
                <span className={styles.paletteItemHint}>{cmd.hint}</span>
              </div>
              <span className={styles.paletteItemShortcut}>{cmd.shortcut}</span>
            </button>
          ))}
        </div>
        <div className={styles.paletteFooter}>
          <span>Enter to run · Esc to close</span>
          <span className={styles.paletteFooterNote}>Commands are requests — governance still decides</span>
        </div>
      </div>
    </div>
  );
};

/**
 * ControlKeyInput - Operator control-plane key entry.
 * The key lives in memory only (never persisted to localStorage).
 */
interface ControlKeyInputProps {
  controlKey: string;
  onSetControlKey: (key: string) => void;
}

const ControlKeyInput: React.FC<ControlKeyInputProps> = ({
  controlKey,
  onSetControlKey,
}) => {
  const [key, setKey] = useState('');
  const [showInput, setShowInput] = useState(false);

  if (!showInput && !controlKey) {
    return (
      <button onClick={() => setShowInput(true)} className={styles.controlKeyTrigger}>
        <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
          <rect x="2" y="4" width="12" height="8" rx="1" stroke="currentColor" strokeWidth="1.5" />
          <path d="M6 12v-4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
        </svg>
        Set Control Key
      </button>
    );
  }

  return (
    <div className={styles.controlKeyInput}>
      {showInput && (
        <input
          type="password"
          value={key}
          onChange={(e) => setKey(e.target.value)}
          placeholder="Control plane API key"
          className={styles.controlKeyField}
          onBlur={() => {
            if (key.trim()) onSetControlKey(key.trim());
            setShowInput(false);
            setKey('');
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && key.trim()) {
              onSetControlKey(key.trim());
              setShowInput(false);
              setKey('');
            } else if (e.key === 'Escape') {
              setShowInput(false);
              setKey('');
            }
          }}
          autoFocus
        />
      )}
      {controlKey && !showInput && (
        <span className={styles.controlKeyDisplay}>
          <svg width="14" height="14" viewBox="0 0 14 14" fill="none" aria-hidden="true">
            <rect x="1" y="3" width="12" height="8" rx="1" stroke="currentColor" strokeWidth="1.5" />
            <path d="M5 11v-4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
          </svg>
          Control key set
          <button onClick={() => onSetControlKey('')} className={styles.controlKeyClear} aria-label="Clear control key">
            <svg width="12" height="12" viewBox="0 0 12 12" fill="none" aria-hidden="true">
              <path d="M3 3l6 6M9 3l-6 6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
            </svg>
          </button>
        </span>
      )}
    </div>
  );
};
