/**
 * PenpotPage - Generic component for Penpot design screens.
 * Renders a functional page with real backend data for any Penpot screen.
 */

import React from 'react';
import { useAgents, useDivisions } from '../api/hooks';
import styles from './KnowledgeMemory.module.css';

interface PenpotPageProps {
  route: string;
  businessId: string;
  actorId: string;
}

const PAGE_TITLES: Record<string, { title: string; subtitle: string }> = {
  'first-run': { title: 'First Run', subtitle: 'Create your first business' },
  'virtual-office': { title: 'Virtual Office', subtitle: 'Virtual office dashboard' },
  'agent-inspector': { title: 'Agent Inspector', subtitle: 'Inspect and manage agents' },
  'agent-roster': { title: 'Agent Roster', subtitle: 'All registered agents' },
  'task-inspector': { title: 'Task Inspector', subtitle: 'Inspect task executions' },
  'states': { title: 'States', subtitle: 'Agent and execution states' },
  'businesses': { title: 'Businesses', subtitle: 'Manage business lifecycle' },
  'division-detail': { title: 'Division Detail', subtitle: 'Inspect division details' },
  'command-palette': { title: 'Command Palette', subtitle: 'Execute commands via the API' },
  'attention-inbox': { title: 'Attention Inbox', subtitle: 'Approvals and escalations' },
  'analytics': { title: 'Analytics', subtitle: 'System metrics and trends' },
  'settings-system': { title: 'Settings & System', subtitle: 'Control plane and system config' },
  'activity-events': { title: 'Activity & Events', subtitle: 'Recent executions and activity' },
  'governance-approvals': { title: 'Governance & Approvals', subtitle: 'Approvals and escalations' },
  'model-providers': { title: 'Model Providers', subtitle: 'Model provider configuration' },
  'web-search': { title: 'Web Search', subtitle: 'Web search provider configuration' },
  'storage': { title: 'Storage', subtitle: 'Storage configuration' },
  'configuration': { title: 'Configuration States', subtitle: 'Boot configuration and readiness' },
  'knowledge': { title: 'Knowledge & Memory', subtitle: 'Knowledge and memory panels' },
};

export const PenpotPage: React.FC<PenpotPageProps> = ({ route, businessId, actorId }) => {
  const { data: agentsData } = useAgents(businessId);
  const { data: divisionsData } = useDivisions(businessId);

  const agents = agentsData?.agents ?? [];
  const divisions = divisionsData?.divisions ?? [];
  const pageInfo = PAGE_TITLES[route] || { title: route, subtitle: '' };

  return (
    <div className={styles.page}>
      <header className={styles.topbar}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <div className={styles.brandIcon}>
            <svg width="18" height="18" viewBox="0 0 18 18" fill="none" stroke="#0a0d14" strokeWidth="1.5" strokeLinecap="round">
              <rect x="2" y="2" width="14" height="14" rx="2" />
            </svg>
          </div>
          <div>
            <div style={{ fontSize: '16.5px', fontWeight: 600, color: '#eaeff0' }}>{pageInfo.title}</div>
            <div style={{ fontSize: '11px', color: '#616c7e', letterSpacing: 0.8, textTransform: 'uppercase' }}>{pageInfo.subtitle}</div>
          </div>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, color: '#616c7e', fontSize: '11px', fontWeight: 600, letterSpacing: 0.8 }}>
          <span>{actorId.split(':').pop()?.charAt(0).toUpperCase() || 'N'}</span>
        </div>
      </header>

      <div style={{ position: 'absolute', top: 60, bottom: 0, left: 0, right: 0, padding: 32, overflowY: 'auto' }}>
        <h1 style={{ fontSize: '28px', fontWeight: 600, color: '#eaeff0', marginBottom: 8 }}>{pageInfo.title}</h1>
        <p style={{ fontSize: '14px', color: '#616c7e', marginBottom: 24 }}>{pageInfo.subtitle}</p>

        <div style={{
          background: '#0e121a',
          border: '1px solid #1b2230',
          borderRadius: 16,
          padding: 24,
          marginBottom: 24,
        }}>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: 20 }}>
            <div style={{ background: '#161b26', borderRadius: 12, padding: 16 }}>
              <div style={{ fontSize: '11px', fontWeight: 600, color: '#616c7e', textTransform: 'uppercase', letterSpacing: 1.6 }}>Agents</div>
              <div style={{ fontSize: '32px', fontWeight: 600, color: '#4fd1c5' }}>{agents.length}</div>
            </div>
            <div style={{ background: '#161b26', borderRadius: 12, padding: 16 }}>
              <div style={{ fontSize: '11px', fontWeight: 600, color: '#616c7e', textTransform: 'uppercase', letterSpacing: 1.6 }}>Divisions</div>
              <div style={{ fontSize: '32px', fontWeight: 600, color: '#6c8cff' }}>{divisions.length}</div>
            </div>
            <div style={{ background: '#161b26', borderRadius: 12, padding: 16 }}>
              <div style={{ fontSize: '11px', fontWeight: 600, color: '#616c7e', textTransform: 'uppercase', letterSpacing: 1.6 }}>Business</div>
              <div style={{ fontSize: '14px', fontWeight: 600, color: '#eaeff0', fontFamily: 'monospace', marginTop: 4 }}>{businessId}</div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};
