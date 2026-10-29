/**
 * KnowledgeMemory Page
 *
 * Implementation of the Penpot design "12 · Knowledge & Memory"
 * (source: /home/pc/NEXUS/html+css-penpot).
 *
 * Renders inside the OfficeLayout main area. Reproduces the Penpot layout
 * shell (top bar, 860px main, 344px right panel, 44px status bar) and the
 * knowledge/memory panels with the Penpot's exact colors and measurements.
 */

import React from 'react';
import styles from './KnowledgeMemory.module.css';

const Sparkle = () => (
  <svg width="18" height="18" viewBox="0 0 18 18" fill="none" aria-hidden="true">
    <path d="M12.67 12.5l5 5M3.67 2.5l12.67 12.17" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
  </svg>
);

const Shield = () => (
  <svg width="18" height="18" viewBox="0 0 18 18" fill="none" aria-hidden="true">
    <path d="M3 2.25l12 14.1" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
  </svg>
);

interface KnowledgeMemoryProps {
  businessId: string;
  actorId: string;
}

export const KnowledgeMemory: React.FC<KnowledgeMemoryProps> = ({ businessId, actorId }) => {
  return (
    <div className={styles.page}>
      <div className={styles.canvas}>
        {/* Top bar */}
        <header className={styles.topbar}>
          <div className={styles.brand}>
            <div className={styles.brandIcon}>
              <Sparkle />
            </div>
            <span className={styles.brandTitle}>Knowledge &amp; Memory</span>
          </div>
          <div className={styles.actions}>
            <div className={`${styles.chip} ${styles.chipAccent}`}>
              <span className={styles.chipLabel}>Knowledge</span>
            </div>
            <div className={styles.chip}>
              <span className={styles.chipLabel}>Memory</span>
            </div>
            <div className={styles.chip}>
              <span className={styles.chipLabel}>Evidence</span>
            </div>
          </div>
          <div>
            <span>{actorId.split(':').pop()?.charAt(0).toUpperCase() || 'N'}</span>
          </div>
        </header>

        {/* Main area */}
        <main className={styles.main}>
          {/* Head panel */}
          <div className={`${styles.panel} ${styles.panelHead}`}>
            <div className={styles.accentPill} />
            <span className={`${styles.accentLabel}`} style={{ top: 3 }}>
              Knowledge
            </span>
            <span className={`${styles.accentLabel} ${styles.inverse}`} style={{ left: 152, top: 3 }}>
              Memory
            </span>
            <div className={`${styles.dot} ${styles.dotOk}`} />
            <p className={styles.panelTitle} style={{ left: 58, top: 400 }}>
              Knowledge &amp; Memory
            </p>
            <p className={styles.panelNote}>
              Captured context, decisions, and source material from prior runs.
            </p>
          </div>

          {/* Section label */}
          <span className={styles.sectionLabel} style={{ top: 454 }}>
            Active memory
          </span>

          {/* Active panel */}
          <div className={`${styles.panel} ${styles.panelActive}`}>
            <div className={styles.accentPill} />
            <span className={styles.accentLabel} style={{ top: 264 }}>
              Active
            </span>
            <span className={`${styles.accentLabel} ${styles.inverse}`} style={{ left: 152, top: 264 }}>
              Inbox
            </span>
            <div className={`${styles.dot} ${styles.dotOk}`} />
            <p className={styles.panelTitle} style={{ left: 58, top: 400 }}>
              Active Inbox
            </p>
            <p className={styles.panelNote}>
              Items awaiting a decision or a governing approval.
            </p>
          </div>

          {/* List items (faithful to Penpot y-positions 628..778) */}
          <div className={styles.listItem} style={{ top: 628 }}>
            <span>Design evidence is not a live run</span>
            <span className={styles.meta} style={{ left: 620 }}>
              pending &middot; 02:14
            </span>
          </div>
          <div className={`${styles.divider} ${styles.dividerY664}`} />

          <div className={styles.listItem} style={{ top: 666 }}>
            <span>Only a live run is live evidence</span>
            <span className={styles.meta} style={{ left: 620 }}>
              ok &middot; 02:14
            </span>
          </div>
          <div className={`${styles.divider} ${styles.dividerY702}`} />

          <div className={styles.listItem} style={{ top: 704 }}>
            <span>No state claims a provider was contacted</span>
            <span className={styles.meta} style={{ left: 620 }}>
              warn &middot; 02:14
            </span>
          </div>
          <div className={`${styles.divider} ${styles.dividerY740}`} />

          <div className={styles.listItem} style={{ top: 742 }}>
            <span>Gate 0 is not recorded in this process</span>
            <span className={styles.meta} style={{ left: 620 }}>
              error &middot; 02:14
            </span>
          </div>
          <div className={`${styles.divider} ${styles.dividerY778}`} />

          <div className={styles.listItem} style={{ top: 780 }}>
            <span>A state gallery is not a live workflow</span>
            <span className={styles.meta} style={{ left: 620 }}>
              ok &middot; 02:14
            </span>
          </div>
        </main>

        {/* Right panel */}
        <aside className={styles.rightPanel}>
          <div className={styles.panelCard} />
          <div className={styles.panelHeader}>
            <div className={styles.iconBox}>
              <Shield />
            </div>
            <span className={styles.panelTitle}>Memory vault</span>
          </div>
          <p className={styles.panelSubtitle}>
            Stores verified knowledge and source material so a run can cite
            real evidence instead of reconstructing it from memory.
          </p>
          <span className={styles.panelTag}>readonly &middot; local-first</span>
          <p className={styles.panelFooter}>
            Evidence is only meaningful when it is produced by a live execution.
          </p>
          <div className={styles.dividerR} />
        </aside>

        {/* Status bar */}
        <footer className={styles.statusBar}>
          <div className={styles.statusLeft}>
            <span>All systems operational</span>
            <span className={`${styles.statusText} ${styles.statusAccent}`}>
              12 agents working
            </span>
            <span className={`${styles.statusText} ${styles.statusWarn}`}>
              3 tasks running
            </span>
          </div>
          <div className={styles.statusRight}>
            <span>NEXUS v2.4</span>
            <span>build 4821</span>
            <span>Background work continues while you are away</span>
          </div>
        </footer>
      </div>
    </div>
  );
};
