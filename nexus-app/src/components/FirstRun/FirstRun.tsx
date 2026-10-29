/**
 * FirstRun - NEXUS first-run onboarding screen.
 *
 * Reproduces the provided Penpot design pixel-for-pixel and wires the
 * "Create business" button to the backend API (POST /api/v1/businesses).
 *
 * On success the parent is notified via onBusinessCreated so the app can
 * transition to the virtual office layout.
 */

import React, { useCallback, useRef, useState } from 'react';
import { apiClient } from '../../api/client';
import type { CreateBusinessRequest } from '../../types/api';
import styles from './FirstRun.module.css';

interface FirstRunProps {
  actorId: string;
  onBusinessCreated: (business: { entity_id: string; name: string }) => void;
}

const DIVISIONS = [
  { id: 'research', label: 'Research', color: '#4fd1c5' },
  { id: 'business', label: 'Business', color: '#f5b759' },
  { id: 'media', label: 'Media', color: '#161b26' },
  { id: 'development', label: 'Development', color: '#161b26' },
] as const;

type DivisionId = (typeof DIVISIONS)[number]['id'];

export function FirstRun({ actorId, onBusinessCreated }: FirstRunProps) {
  const [businessName, setBusinessName] = useState('AI Agency');
  const [objective, setObjective] = useState('Win three enterprise deals this quarter');
  const [selectedDivisions, setSelectedDivisions] = useState<Set<DivisionId>>(new Set(['research']));
  const [isCreating, setIsCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const nameInputRef = useRef<HTMLInputElement>(null);

  const handleCreateBusiness = useCallback(async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);

    const name = businessName.trim();
    if (!name) {
      setError('Business name is required');
      nameInputRef.current?.focus();
      return;
    }

    const request: CreateBusinessRequest = {
      name,
      owner_identity_id: actorId,
      metadata: {
        objective,
        divisions: JSON.stringify(Array.from(selectedDivisions)),
        created_via: 'first-run',
      },
    };

    setIsCreating(true);
    try {
      const created = await apiClient.createBusiness(request);
      onBusinessCreated({ entity_id: created.entity_id, name: created.name });
    } catch (err) {
      setIsCreating(false);
      if (err instanceof Error) {
        setError(err.message || 'Failed to create business');
      } else {
        setError('Failed to create business');
      }
    }
  }, [businessName, objective, selectedDivisions, actorId, onBusinessCreated]);

  const toggleDivision = (id: DivisionId) => {
    setSelectedDivisions((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const isDivisionSelected = (id: DivisionId) => selectedDivisions.has(id);

  return (
    <div className={styles.root}>
      <div className={styles.bgLayer} />
      <div className={styles.accentGlow} />

      {/* Sparkle icon */}
      <div className={styles.sparkle}>
        <svg width="14" height="14" viewBox="116 126 14 14" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
          <g id="sparkle" style={{ fill: '#000000', rx: 0, ry: 0 }}>
            <defs />
            <g className="fills">
              <path
                d="M123,127.75L124.109375,131.0749969482422L127.43359375,131.8333282470703L124.109375,132.94166564941406L123,136.26666259765625L121.890625,132.94166564941406L118.56640625,131.8333282470703L121.890625,130.72500610351562L123,127.75"
                fill="none"
                strokeLinejoin="round"
                strokeLinecap="round"
                style={{ fill: 'none' }}
              />
            </g>
            <g fill="none" strokeLinejoin="round" strokeLinecap="round" className="strokes">
              <g className="stroke-shape">
                <defs />
                <path
                  d="M123,127.75L124.109375,131.0749969482422L127.43359375,131.8333282470703L124.109375,132.94166564941406L123,136.26666259765625L121.890625,132.94166564941406L118.56640625,131.8333282470703L121.890625,130.72500610351562L123,127.75"
                  style={{
                    fill: 'none',
                    fillOpacity: 1,
                    strokeWidth: 2.2,
                    stroke: '#ffffff',
                    strokeOpacity: 1,
                  }}
                />
              </g>
            </g>
          </g>
        </svg>
      </div>

      {/* Wordmark */}
      <div className={styles.wordmark}>NEXUS</div>

      {/* Subtitle */}
      <div className={styles.subtitle}>personal AI operating system</div>

      {/* Step labels */}
      <div className={styles.stepLabel} data-step="1">Business</div>
      <div className={styles.stepLabel} data-step="2">Objective</div>
      <div className={styles.stepLabel} data-step="3">Authority</div>
      <div className={styles.stepLabel} data-step="4">Agents</div>

      {/* Step number circles */}
      <div className={styles.stepCircle} data-step="1">1</div>
      <div className={styles.stepCircle} data-step="2">2</div>
      <div className={styles.stepCircle} data-step="3">3</div>
      <div className={styles.stepCircle} data-step="4">4</div>

      {/* Connecting lines */}
      <div className={styles.stepLine} data-step="1" />
      <div className={styles.stepLine} data-step="2" />
      <div className={styles.stepLine} data-step="3" />

      {/* Headings */}
      <div className={styles.welcome}>Welcome</div>
      <div className={styles.tagline}>NEXUS runs the work.</div>

      {/* Description */}
      <div className={styles.description}>
        Give it one business, one objective and a clear limit of authority. It will
        plan, delegate and keep working while you are away.
      </div>

      {/* Info cards */}
      <div className={styles.cardContainer} />
      <div className={styles.cardAccent} data-accent="1" />
      <div className={styles.cardAccent} data-accent="2" />
      <div className={styles.cardAccent} data-accent="3" />

      <div className={styles.cardText} data-card="1">You stay the root authority</div>
      <div className={styles.cardText} data-card="2">It keeps a why for every task</div>
      <div className={styles.cardText} data-card="3">The office is only a view</div>

      <div className={styles.cardNote} data-card="1">Nothing runs outside delegated authority. Default deny holds.</div>
      <div className={styles.cardNote} data-card="2">Objective lineage survives every delegation.</div>
      <div className={styles.cardNote} data-card="3">Every action exists as a list and a table too.</div>

      {/* Right panel */}
      <div className={styles.rightPanel} />
      <div className={styles.rightPanelInner}>

        <div className={styles.pageTitle}>Create your first business</div>
        <div className={styles.pageSubtitle}>
          A business is the scope everything else hangs from.
        </div>

        {/* Business name */}
        <div className={styles.panelLabel} data-label="biz">BUSINESS NAME</div>
        <div className={styles.inputField} data-field="biz" />
        <div className={styles.dividerLine} />
        <input
          ref={nameInputRef}
          className={styles.inputText}
          data-field="biz"
          value={businessName}
          onChange={(e) => setBusinessName(e.target.value)}
          autoFocus
          autoComplete="off"
          spellCheck={false}
        />

        {/* Primary objective */}
        <div className={styles.panelLabel} data-label="obj">PRIMARY OBJECTIVE</div>
        <div className={styles.inputField} data-field="obj" />
        <input
          className={styles.inputText}
          data-field="obj"
          value={objective}
          onChange={(e) => setObjective(e.target.value)}
          autoComplete="off"
          spellCheck={false}
        />
        <div className={styles.panelHeading} data-heading="obj">
          The why every task will point back to.
        </div>

        {/* Divisions */}
        <div className={styles.panelLabel} data-label="div">DIVISIONS TO START WITH</div>
        {DIVISIONS.map((division) => {
          const selected = isDivisionSelected(division.id);
          const chipStyle = selected
            ? { ...styles.divChip, [`data-chip-${division.id}`]: true } as unknown as string
            : styles.divChip;
          return (
            <div
              key={division.id}
              className={chipStyle}
              data-chip={division.id.toString() as any}
              onClick={() => toggleDivision(division.id)}
              role="checkbox"
              aria-checked={selected}
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  toggleDivision(division.id);
                }
              }}
            >
              {division.label}
            </div>
          );
        })}
        <div className={styles.divDot} data-dot="1" />
        <div className={styles.divDot} data-dot="2" />

        <div className={styles.secondaryDivider} />

        <div className={styles.footerNote}>
          A division is a room with its own policy and memory scope.
        </div>

        {/* Action buttons */}
        <form onSubmit={handleCreateBusiness}>
          <button
            type="submit"
            className={styles.primaryButton}
            disabled={isCreating}
          >
            {isCreating ? 'Creating…' : 'Create business'}
          </button>
        </form>
        <button
          className={styles.secondaryButton}
          disabled={isCreating}
          onClick={() => {
            setBusinessName('NEXUS Sample');
            setObjective('Explore the platform capabilities');
            setSelectedDivisions(new Set(['research', 'media']));
          }}
        >
          Load a sample
        </button>
        <div className={styles.stepIndicator}>Step 1 of 4</div>

        <div className={styles.bottomNote}>
          You can change every one of these later — nothing here is permanent.
        </div>

        {error && (
          <div className={`${styles.errorMsg} visible`} role="alert">
            {error}
          </div>
        )}

        <div className={`${styles.loadingOverlay} ${isCreating ? styles.visible : ''}`}>
          Creating your business…
        </div>
      </div>

      <div className={styles.runtimeFooter}>
        Runtime runs locally. Nothing leaves this machine unless a policy allows it.
      </div>
    </div>
  );
}