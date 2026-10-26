/**
 * Settings — read-only platform configuration.
 *
 * The backend surface is deliberately read-only: provider credentials are
 * environment-injected, configuration is loaded at boot, and applying a new
 * data directory is not implemented. This page reports that posture honestly
 * instead of pretending to save secrets or test live providers.
 */

import React, { useState } from 'react';
import { APIError } from '../api/client';
import {
  useConfiguration,
  useStorageVolumes,
  useValidateStoragePath,
} from '../api/hooks';
import { Badge, Button } from '../components/primitives';
import type {
  ConfigReadiness,
  ConfigurationSnapshot,
  CredentialStatus,
  ModelProviderView,
  StorageCheck,
  StorageValidation,
  StorageVolumeView,
  WebSearchKind,
  WebSearchView,
} from '../types/config';
import styles from './Settings.module.css';

interface SettingsProps {
  controlKey: string;
}

type SettingsTab = 'overview' | 'models' | 'search' | 'storage';

const TABS: Array<{ id: SettingsTab; label: string }> = [
  { id: 'overview', label: 'Overview' },
  { id: 'models', label: 'Model providers' },
  { id: 'search', label: 'Web search' },
  { id: 'storage', label: 'Storage' },
];

function errorStatus(error: unknown): number | undefined {
  return error instanceof APIError ? error.status : undefined;
}

function errorCode(error: unknown): string | undefined {
  return error instanceof APIError ? error.envelope?.code : undefined;
}

function errorCorrelation(error: unknown): string | undefined {
  return error instanceof APIError ? error.envelope?.correlation_id : undefined;
}

function formatBytes(bytes?: number): string {
  if (bytes === undefined || !Number.isFinite(bytes) || bytes < 0) return '—';
  if (bytes === 0) return '0 B';
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / Math.pow(1024, i);
  return `${value >= 100 ? value.toFixed(0) : value.toFixed(1)} ${units[i]}`;
}

function usedPercent(used?: number, total?: number): number | undefined {
  if (!used || !total || total <= 0) return undefined;
  return Math.min(100, Math.max(0, (used / total) * 100));
}

function credentialBadge(status: CredentialStatus) {
  switch (status) {
    case 'present':
      return <Badge variant="success" size="sm" dot>Credential present</Badge>;
    case 'missing':
      return <Badge variant="warning" size="sm" dot>Credential missing</Badge>;
    case 'not_required':
      return <Badge variant="default" size="sm">No key required</Badge>;
    case 'unknown':
      return <Badge variant="default" size="sm">Credential unknown</Badge>;
  }
}

/**
 * Derive the boot descriptor kind for servers that predate the `kind` field.
 * A missing kind is treated as injected/unknown rather than guessed live.
 */
function webSearchKind(ws: WebSearchView): WebSearchKind {
  if (ws.kind) return ws.kind;
  switch (ws.mode) {
    case 'scripted':
      return 'scripted';
    case 'live':
      return 'live';
    case 'injected':
      return 'injected';
    case 'invalid':
      return 'invalid';
  }
  if (ws.provider === 'scripted' || ws.provider === 'scripted-research') return 'scripted';
  if (ws.provider === 'disabled') return 'disabled';
  if (ws.provider === 'invalid') return 'invalid';
  return ws.live ? 'live' : 'injected';
}

/** Canonical adapter identifier; older servers send "scripted". */
function webSearchProvider(ws: WebSearchView): string {
  if (webSearchKind(ws) === 'scripted') return 'scripted-research';
  return ws.provider;
}

/** Older responses did not report registration; infer only the unambiguous cases. */
function webSearchRegistered(ws: WebSearchView): boolean {
  if (typeof ws.registered === 'boolean') return ws.registered;
  const kind = webSearchKind(ws);
  return kind !== 'disabled' && kind !== 'invalid';
}

/** Never present an undeclared credential requirement as "not required". */
function webSearchCredentialReference(ws: WebSearchView): string {
  if (ws.credential_env) return ws.credential_env;
  const kind = webSearchKind(ws);
  return kind === 'live' || kind === 'scripted' || kind === 'disabled'
    ? 'not required'
    : 'not declared';
}

function modeBadge(mode: WebSearchView['mode']) {
  switch (mode) {
    case 'live':
      // "Live" means a live adapter was selected at boot, not that it was
      // contacted. Verification is reported separately via live_verified.
      return <Badge variant="info" size="sm">Live adapter selected</Badge>;
    case 'scripted':
      return <Badge variant="warning" size="sm">Scripted test-only</Badge>;
    case 'injected':
      return <Badge variant="default" size="sm">Injected adapter</Badge>;
    case 'invalid':
      return <Badge variant="error" size="sm">Unsupported</Badge>;
    case 'disabled':
      return <Badge variant="unknown" size="sm">Disabled</Badge>;
  }
}

function checkBadge(status: StorageCheck['status']) {
  switch (status) {
    case 'pass':
      return <Badge variant="success" size="sm">Pass</Badge>;
    case 'warn':
      return <Badge variant="warning" size="sm">Warning</Badge>;
    case 'fail':
      return <Badge variant="error" size="sm">Failed</Badge>;
    case 'info':
      return <Badge variant="info" size="sm">Info</Badge>;
  }
}

const IconLock = () => (
  <svg width="18" height="18" viewBox="0 0 18 18" fill="none" aria-hidden="true">
    <rect x="3.5" y="8" width="11" height="8" rx="2" stroke="currentColor" strokeWidth="1.4" />
    <path d="M6 8V6a3 3 0 016 0v2" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
  </svg>
);

const IconRefresh = () => (
  <svg width="15" height="15" viewBox="0 0 16 16" fill="none" aria-hidden="true">
    <path d="M13 6a5.2 5.2 0 10.4 3" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
    <path d="M13 2.5V6H9.5" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
  </svg>
);

const IconServer = () => (
  <svg width="18" height="18" viewBox="0 0 18 18" fill="none" aria-hidden="true">
    <rect x="2.5" y="3" width="13" height="5" rx="1.5" stroke="currentColor" strokeWidth="1.3" />
    <rect x="2.5" y="10" width="13" height="5" rx="1.5" stroke="currentColor" strokeWidth="1.3" />
    <circle cx="5.5" cy="5.5" r=".9" fill="currentColor" />
    <circle cx="5.5" cy="12.5" r=".9" fill="currentColor" />
  </svg>
);

const IconSpark = () => (
  <svg width="18" height="18" viewBox="0 0 18 18" fill="none" aria-hidden="true">
    <path d="M9 2l1.7 5.3L16 9l-5.3 1.7L9 16l-1.7-5.3L2 9l5.3-1.7L9 2z" stroke="currentColor" strokeWidth="1.3" strokeLinejoin="round" />
  </svg>
);

const IconSearch = () => (
  <svg width="18" height="18" viewBox="0 0 18 18" fill="none" aria-hidden="true">
    <circle cx="8" cy="8" r="5.2" stroke="currentColor" strokeWidth="1.3" />
    <path d="M12 12l3.2 3.2" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
  </svg>
);

interface SectionCardProps {
  title: string;
  subtitle?: string;
  icon?: React.ReactNode;
  actions?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}

const SectionCard: React.FC<SectionCardProps> = ({
  title,
  subtitle,
  icon,
  actions,
  children,
  className = '',
}) => (
  <section className={`${styles.card} ${className}`}>
    <header className={styles.cardHeader}>
      <div className={styles.cardTitleGroup}>
        {icon && <span className={styles.cardIcon}>{icon}</span>}
        <div>
          <h2 className={styles.cardTitle}>{title}</h2>
          {subtitle && <p className={styles.cardSubtitle}>{subtitle}</p>}
        </div>
      </div>
      {actions && <div className={styles.cardActions}>{actions}</div>}
    </header>
    <div className={styles.cardBody}>{children}</div>
  </section>
);

const InfoBanner: React.FC<{
  tone?: 'info' | 'warning' | 'blocked';
  title: string;
  children: React.ReactNode;
}> = ({ tone = 'info', title, children }) => (
  <div className={`${styles.banner} ${styles[`banner_${tone}`]}`} role="note">
    <div className={styles.bannerMarker} aria-hidden="true" />
    <div>
      <strong className={styles.bannerTitle}>{title}</strong>
      <div className={styles.bannerText}>{children}</div>
    </div>
  </div>
);

const EmptyState: React.FC<{
  title: string;
  children: React.ReactNode;
  action?: React.ReactNode;
}> = ({ title, children, action }) => (
  <div className={styles.emptyState}>
    <div className={styles.emptyTitle}>{title}</div>
    <div className={styles.emptyText}>{children}</div>
    {action && <div className={styles.emptyAction}>{action}</div>}
  </div>
);

const LoadingState: React.FC = () => (
  <div className={styles.loadingGrid} aria-busy="true" aria-label="Loading configuration">
    {[0, 1, 2, 3].map((i) => (
      <div className={styles.skeletonCard} key={i}>
        <div className={styles.skeletonLineShort} />
        <div className={styles.skeletonLine} />
        <div className={styles.skeletonLine} />
        <div className={styles.skeletonLineMedium} />
      </div>
    ))}
  </div>
);

const PermissionState: React.FC = () => (
  <div className={styles.statePanel}>
    <span className={styles.stateIcon}><IconLock /></span>
    <h2>Control key required</h2>
    <p>
      Configuration and storage inspection are operator-only. Set the control-plane API key in
      the header, then refresh. The key is held only in this tab's memory — it is never written
      to browser storage and never sent to a model or search provider.
    </p>
  </div>
);

const ErrorState: React.FC<{ error: unknown; onRetry: () => void }> = ({ error, onRetry }) => {
  const status = errorStatus(error);
  const code = errorCode(error);
  const correlation = errorCorrelation(error);
  const controlDisabled = code === 'CONTROL_DISABLED';
  const permission = !controlDisabled && (status === 401 || status === 403);
  const heading = controlDisabled
    ? 'Control plane disabled'
    : permission
      ? 'Control key rejected'
      : 'Configuration unavailable';
  return (
    <div className={styles.statePanel}>
      <span className={styles.stateIcon}><IconLock /></span>
      <h2>{heading}</h2>
      <p>
        {controlDisabled
          ? 'This gateway has no control API key configured, so control endpoints are disabled. Set NEXUS_CONTROL_API_KEY on the server and restart it; a browser key cannot enable this state.'
          : permission
            ? 'The control plane did not accept the current key. Set a valid key in the header and retry.'
            : 'The configuration surface could not be read. No secret or local path is shown in this error.'}
      </p>
      <div className={styles.errorDetail}>
        <span>{error instanceof Error ? error.message : 'Unknown error'}</span>
        {status !== undefined && <span className={styles.mono}>HTTP {status}</span>}
        {correlation && <span className={styles.mono}>ref {correlation}</span>}
      </div>
      <Button variant="secondary" onClick={onRetry}>Retry</Button>
    </div>
  );
};

const ReadinessRow: React.FC<{
  label: string;
  detail: string;
  state: 'ready' | 'blocked' | 'neutral';
}> = ({ label, detail, state }) => (
  <div className={styles.readinessRow}>
    <span className={`${styles.readinessDot} ${styles[`readiness_${state}`]}`} aria-hidden="true" />
    <div className={styles.readinessText}>
      <span className={styles.readinessLabel}>{label}</span>
      <span className={styles.readinessDetail}>{detail}</span>
    </div>
    <Badge variant={state === 'ready' ? 'success' : state === 'blocked' ? 'error' : 'default'} size="sm">
      {state === 'ready' ? 'Configured' : state === 'blocked' ? 'Blocked' : 'Not verified'}
    </Badge>
  </div>
);

function readinessItems(config: ConfigurationSnapshot): Array<{
  label: string;
  detail: string;
  state: 'ready' | 'blocked' | 'neutral';
}> {
  const r: ConfigReadiness = config.readiness;
  const ws = config.web_search;
  return [
    {
      label: 'Model provider',
      detail: r.model_provider_configured
        ? `${config.model_providers.length} OpenAI-compatible provider${config.model_providers.length === 1 ? '' : 's'}`
        : 'Simulated provider only — no live inference',
      state: r.model_provider_configured ? (r.model_credentials_present ? 'ready' : 'blocked') : 'blocked',
    },
    {
      label: 'Web search',
      detail: webSearchKind(ws) === 'disabled'
        ? 'Disabled'
        : `${webSearchProvider(ws)} · ${ws.mode}`,
      state: r.web_search_configured ? (r.web_search_live ? 'ready' : 'blocked') : 'blocked',
    },
    {
      label: 'Search credential',
      detail: ws.credential_env
        ? `${ws.credential_env} · ${ws.credential_status}`
        : ws.keyless
          ? 'Keyless provider'
          : webSearchKind(ws) === 'injected'
            ? 'Unknown — adapter did not declare requirements'
            : 'Not applicable',
      state: r.web_search_credential_ready ? 'ready' : 'blocked',
    },
    {
      label: 'Durable storage',
      detail: r.durable_storage_configured
        ? `${config.storage.data_dir} · ${r.durable_storage_writable ? 'writable' : 'not writable'}`
        : 'In-memory records; restart loses state',
      state: r.durable_storage_configured ? (r.durable_storage_writable ? 'ready' : 'blocked') : 'blocked',
    },
    {
      label: 'Gate 0 authorization',
      detail: r.gate_zero_authorized
        ? 'Recorded for this activation'
        : 'Not recorded in this process',
      state: r.gate_zero_authorized ? 'ready' : 'blocked',
    },
  ];
}

const ProviderCard: React.FC<{ provider: ModelProviderView; onRefresh: () => void }> = ({
  provider,
  onRefresh,
}) => {
  const label = provider.name || provider.id;
  return (
    <article className={styles.providerCard}>
      <div className={styles.providerTop}>
        <div>
          <div className={styles.providerName}>{label}</div>
          <div className={styles.providerMeta}>
            <span className={styles.mono}>{provider.id}</span>
            <span className={styles.dotSeparator}>·</span>
            <span>OpenAI-compatible adapter</span>
          </div>
        </div>
        <div className={styles.badgeRow}>
          {credentialBadge(provider.credential_status)}
          <Badge variant="default" size="sm">Not live-verified</Badge>
        </div>
      </div>

      <dl className={styles.definitionGrid}>
        <div><dt>Endpoint</dt><dd className={styles.mono}>{provider.endpoint}</dd></div>
        <div><dt>Model ID</dt><dd className={styles.mono}>{provider.model_id}</dd></div>
        <div><dt>Credential env</dt><dd className={styles.mono}>{provider.api_key_env || '—'}</dd></div>
        <div><dt>Timeout</dt><dd>{provider.timeout_ms} ms</dd></div>
      </dl>

      <div className={styles.capabilityRow}>
        {provider.capabilities.map((cap) => (
          <span className={styles.capability} key={cap}>{cap.replaceAll('_', ' ')}</span>
        ))}
      </div>

      <div className={styles.providerFooter}>
        <p className={styles.mutedText}>
          {provider.credential_status === 'missing'
            ? `The environment variable ${provider.api_key_env} is not set. The process would fail closed for this provider.`
            : provider.credential_status === 'unknown'
              ? 'The adapter did not declare a credential requirement. Presence is not inferred.'
              : 'The credential reference is present. This page has not contacted the endpoint.'}
        </p>
        <div className={styles.inlineActions}>
          <Button variant="ghost" size="sm" onClick={onRefresh} leftIcon={<IconRefresh />}>
            Re-check config
          </Button>
          <Button
            variant="secondary"
            size="sm"
            disabled
            title="Live connection tests require explicit Gate 0 authorization. This console never calls a provider."
          >
            Test connection
          </Button>
        </div>
      </div>
    </article>
  );
};

const OverviewTab: React.FC<{ config: ConfigurationSnapshot; onRefresh: () => void }> = ({
  config,
  onRefresh,
}) => {
  const items = readinessItems(config);
  const ws = config.web_search;
  const storage = config.storage;
  const used = usedPercent(storage.used_bytes, storage.total_bytes);
  const sources = config.sources
    ? Object.entries(config.sources).sort(([a], [b]) => a.localeCompare(b))
    : [];
  const provenance = config.provenance;

  return (
    <div className={styles.tabStack}>
      <InfoBanner tone="info" title="Read-only by design">
        NEXUS reads provider, search and storage posture from its boot configuration. This
        console cannot save secrets or change a live process. Configuration changes require an
        operator edit and a controlled restart.
      </InfoBanner>

      <div className={styles.twoColumn}>
        <SectionCard
          title="Live readiness"
          subtitle="Preconditions from configuration only — no provider is contacted"
          icon={<IconLock />}
          actions={<Badge variant={config.readiness.live_preconditions_met ? 'warning' : 'error'} size="sm">
            {config.readiness.live_preconditions_met ? 'Preconditions met' : 'Blocked'}
          </Badge>}
        >
          <div className={styles.readinessList}>
            {items.map((item) => <ReadinessRow key={item.label} {...item} />)}
          </div>
          <p className={styles.footnote}>
            Live preconditions being met would still not authorize a run. A recorded Gate 0,
            owner inputs and the prerequisite gates are required. This build always reports Gate 0
            as not recorded.
          </p>
        </SectionCard>

        <SectionCard
          title="Model routing"
          subtitle="How inference is wired in this process"
          icon={<IconSpark />}
          actions={<Badge variant={config.model_routing.mode === 'simulated' ? 'warning' : 'info'} size="sm">
            {config.model_routing.mode === 'simulated' ? 'Simulated' : 'Configured'}
          </Badge>}
        >
          <div className={styles.summaryBlock}>
            <div className={styles.summaryNumber}>{config.model_providers.length}</div>
            <div>
              <div className={styles.summaryLabel}>real providers</div>
              <div className={styles.mutedText}>{config.model_routing.note}</div>
            </div>
          </div>
          <div className={styles.kvList}>
            <div><span>Per-purpose selector</span><span className={styles.mono}>not implemented</span></div>
            <div><span>Seeded provider</span><span className={styles.mono}>{config.model_routing.seeded ? 'active' : 'inactive'}</span></div>
            <div><span>Runtime mutation</span><span className={styles.mono}>unsupported</span></div>
          </div>
        </SectionCard>
      </div>

      <SectionCard
        title="Configuration provenance"
        subtitle="Section-level sources for the immutable boot snapshot"
        icon={<IconLock />}
        actions={<Badge variant={provenance?.immutable === false ? 'warning' : 'default'} size="sm">
          {provenance ? (provenance.immutable ? 'Immutable' : 'Mutable') : 'Not reported'}
        </Badge>}
      >
        {config.fingerprint ? (
          <div className={styles.kvList}>
            <div><span>Fingerprint</span><span className={`${styles.mono} ${styles.fingerprint}`}>{config.fingerprint}</span></div>
            <div><span>Schema</span><span className={styles.mono}>{config.fingerprint_version || 'not reported'}</span></div>
            <div><span>Provenance</span><span className={styles.mono}>{provenance?.kind || 'boot_snapshot'}</span></div>
            <div><span>Precedence</span><span className={styles.mono}>{provenance?.precedence || 'defaults < file < environment'}</span></div>
            <div><span>Captured</span><span className={styles.mono}>
              {provenance?.captured_at ? new Date(provenance.captured_at).toLocaleString() : 'not reported'}
            </span></div>
          </div>
        ) : (
          <EmptyState title="Fingerprint not reported">
            This server does not expose a boot-configuration fingerprint. The page does not invent
            one; upgrade the gateway to report the snapshot identifier.
          </EmptyState>
        )}
        {sources.length > 0 ? (
          <div className={styles.kvList}>
            {sources.map(([section, source]) => (
              <div key={section}><span>{section}</span><span className={styles.mono}>{source}</span></div>
            ))}
          </div>
        ) : (
          <p className={styles.footnote}>
            No section-level source metadata was reported. Values are shown from the snapshot only.
          </p>
        )}
      </SectionCard>

      <div className={styles.twoColumn}>
        <SectionCard
          title="Web search"
          subtitle="A separate capability from the model provider"
          icon={<IconSearch />}
          actions={modeBadge(ws.mode)}
        >
          <div className={styles.kvList}>
            <div><span>Provider</span><span className={styles.mono}>{webSearchProvider(ws)}</span></div>
            <div><span>Kind</span><span className={styles.mono}>{webSearchKind(ws)}</span></div>
            <div><span>Registered</span><span className={styles.mono}>{webSearchRegistered(ws) ? 'yes' : 'no'}</span></div>
            <div><span>Credential</span><span className={styles.mono}>{webSearchCredentialReference(ws)}</span></div>
            <div><span>Status</span><span>{credentialBadge(ws.credential_status)}</span></div>
            <div><span>Endpoint</span><span className={styles.mono}>{ws.endpoint || '—'}</span></div>
          </div>
          <p className={styles.footnote}>{ws.verification_note}</p>
        </SectionCard>

        <SectionCard
          title="Persistent storage"
          subtitle="Host-owned data directory"
          icon={<IconServer />}
          actions={<Badge variant={storage.configured ? (storage.writable ? 'success' : 'error') : 'warning'} size="sm">
            {storage.configured ? (storage.writable ? 'Writable' : 'Not writable') : 'In-memory'}
          </Badge>}
        >
          {storage.configured ? (
            <>
              <div className={styles.pathLine}>
                <span className={styles.mono}>{storage.data_dir}</span>
              </div>
              <div className={styles.capacityRow}>
                <div className={styles.capacityBar} aria-hidden="true">
                  <span style={{ width: `${used ?? 0}%` }} />
                </div>
                <span className={styles.capacityText}>
                  {formatBytes(storage.used_bytes)} used of {formatBytes(storage.total_bytes)}
                </span>
              </div>
              <div className={styles.kvList}>
                <div><span>Available</span><span className={styles.mono}>{formatBytes(storage.available_bytes)}</span></div>
                <div><span>Filesystem</span><span className={styles.mono}>{storage.filesystem || 'unknown'}</span></div>
                <div><span>Device</span><span className={styles.mono}>{storage.device || 'unknown'}</span></div>
              </div>
            </>
          ) : (
            <EmptyState title="Durable storage is off">
              Records stay in memory and disappear on restart. Set <code>storage.data_dir</code> in
              the host configuration, then restart NEXUS.
            </EmptyState>
          )}
        </SectionCard>
      </div>

      <div className={styles.actionsRow}>
        <Button variant="secondary" onClick={onRefresh} leftIcon={<IconRefresh />}>
          Refresh snapshot
        </Button>
        <span className={styles.mutedText}>
          No live calls are made by this page. Generated {new Date(config.generated_at).toLocaleString()}.
        </span>
      </div>
    </div>
  );
};

const ModelsTab: React.FC<{
  config: ConfigurationSnapshot;
  onRefresh: () => void;
}> = ({ config, onRefresh }) => (
  <div className={styles.tabStack}>
    <InfoBanner tone="warning" title="Credentials are environment-only">
      NEXUS does not store API keys in JSON or in this UI. A provider references an environment
      variable through <code>api_key_env</code>; the value must be injected into the host process
      before boot. Replacing a key means updating the environment and restarting.
    </InfoBanner>

    <SectionCard
      title="Configured providers"
      subtitle="Only the OpenAI-compatible Chat Completions adapter is implemented"
      icon={<IconSpark />}
      actions={<Badge variant="default" size="sm">{config.model_providers.length} configured</Badge>}
    >
      {config.model_providers.length === 0 ? (
        <EmptyState title="No real model provider configured">
          The seeded simulator is active. To enable live inference, add a provider to
          <code> models.providers</code> in the host configuration:
          <pre className={styles.codeBlock}>{`{
  "id": "primary",
  "endpoint": "https://your-endpoint.example/v1",
  "model_id": "your-model",
  "api_key_env": "MODEL_API_KEY"
}`}</pre>
          Then inject <code>MODEL_API_KEY</code> in the process environment and restart. Never
          put the key value in the JSON file.
        </EmptyState>
      ) : (
        <div className={styles.providerList}>
          {config.model_providers.map((p) => (
            <ProviderCard key={p.id} provider={p} onRefresh={onRefresh} />
          ))}
        </div>
      )}
    </SectionCard>

    <div className={styles.twoColumn}>
      <SectionCard title="Adapter support" subtitle="What this build can actually speak">
        <ul className={styles.plainList}>
          <li><strong>Supported:</strong> OpenAI-compatible <code>/chat/completions</code> over HTTPS, or loopback HTTP with explicit opt-in.</li>
          <li><strong>Supported:</strong> any compatible endpoint, including self-hosted gateways, subject to the endpoint and model fields.</li>
          <li><strong>Not implemented:</strong> Anthropic's native Messages API, provider-specific SDKs, OAuth, or UI-managed credential vaults.</li>
          <li><strong>Not implemented:</strong> live connection probes, per-purpose model routing, or runtime provider changes.</li>
        </ul>
      </SectionCard>

      <SectionCard title="Routing posture" subtitle="Configuration order, no hidden failover">
        <div className={styles.kvList}>
          <div><span>Mode</span><span className={styles.mono}>{config.model_routing.mode}</span></div>
          <div><span>Seeded provider</span><span className={styles.mono}>{config.model_routing.seeded_provider_mode || 'default'}</span></div>
          <div><span>Per-purpose selection</span><span className={styles.mono}>unsupported</span></div>
        </div>
        <p className={styles.footnote}>{config.model_routing.note}</p>
      </SectionCard>
    </div>

    <div className={styles.blockedAction}>
      <div>
        <div className={styles.blockedTitle}>Live connection test is blocked</div>
        <p className={styles.mutedText}>
          Testing a provider would make a real external call and may incur cost. It requires a
          recorded Gate 0 authorization. This console intentionally does not offer that action.
        </p>
      </div>
      <Button variant="secondary" disabled>Test connection</Button>
    </div>
  </div>
);

const SearchTab: React.FC<{ config: ConfigurationSnapshot }> = ({ config }) => {
  const ws = config.web_search;
  const kind = webSearchKind(ws);
  const selectedProvider = webSearchProvider(ws);
  const options: Array<{
    id: string;
    title: string;
    detail: string;
    mode: WebSearchView['mode'];
    selected: boolean;
  }> = [
    { id: 'brave', title: 'Brave Search', detail: 'Live general web search · BRAVE_API_KEY required', mode: 'live', selected: kind === 'live' && selectedProvider === 'brave' },
    { id: 'wikipedia', title: 'Wikipedia', detail: 'Live MediaWiki search · keyless', mode: 'live', selected: kind === 'live' && selectedProvider === 'wikipedia' },
    { id: 'scripted', title: 'Scripted', detail: 'Deterministic fixtures · test-only, never live evidence', mode: 'scripted', selected: kind === 'scripted' },
    { id: 'injected', title: 'Injected adapter', detail: 'Supplied by the embedding process · liveness and credentials unknown', mode: 'injected', selected: kind === 'injected' },
    { id: 'disabled', title: 'Disabled', detail: 'No web.search capability is registered', mode: 'disabled', selected: kind === 'disabled' || kind === 'invalid' },
  ];

  return (
    <div className={styles.tabStack}>
      <InfoBanner tone={ws.live ? 'info' : 'warning'} title="Search is a separate capability">
        The model provider and web-search provider are configured independently. Only the
        selected provider is registered at boot; an unknown value fails closed. This page cannot
        switch providers at runtime.
      </InfoBanner>

      <SectionCard
        title="Selected provider"
        subtitle={`Selected by ${ws.selected_by}`}
        icon={<IconSearch />}
        actions={modeBadge(ws.mode)}
      >
        <div className={styles.selectedProvider}>
          <div>
            <div className={styles.selectedName}>{selectedProvider}</div>
            <p className={styles.mutedText}>{ws.verification_note}</p>
          </div>
          {credentialBadge(ws.credential_status)}
        </div>
        <div className={styles.kvList}>
          <div><span>Adapter kind</span><span className={styles.mono}>{kind}</span></div>
          <div><span>Registered at boot</span><span className={styles.mono}>{webSearchRegistered(ws) ? 'yes' : 'no'}</span></div>
          <div><span>Endpoint</span><span className={styles.mono}>{ws.endpoint || '—'}</span></div>
          <div><span>Credential env</span><span className={styles.mono}>{webSearchCredentialReference(ws)}</span></div>
          <div><span>Language</span><span className={styles.mono}>{ws.language || '—'}</span></div>
          <div><span>Live verified</span><span className={styles.mono}>{ws.live_verified ? 'yes' : 'no'}</span></div>
        </div>
      </SectionCard>

      <div className={styles.providerOptions}>
        {options.map((opt) => (
          <div
            key={opt.id}
            className={`${styles.providerOption} ${opt.selected ? styles.providerOptionSelected : ''}`}
            aria-current={opt.selected ? 'true' : undefined}
          >
            <div className={styles.providerOptionTop}>
              <span className={styles.optionTitle}>{opt.title}</span>
              {modeBadge(opt.mode)}
            </div>
            <p className={styles.optionDetail}>{opt.detail}</p>
            <span className={styles.optionState}>{opt.selected ? 'Currently selected' : 'Not selected'}</span>
          </div>
        ))}
      </div>

      <div className={styles.twoColumn}>
        <SectionCard title="Live search test" subtitle="No search has been run">
          <EmptyState title="Sample results unavailable">
            A safe live search would transmit a query to the provider and may consume quota. It
            requires a recorded Gate 0 authorization and is not offered by this console.
          </EmptyState>
          <div className={styles.disabledRow}>
            <Button variant="secondary" disabled>Run test search</Button>
            <span className={styles.mutedText}>Blocked until Gate 0</span>
          </div>
        </SectionCard>

        <SectionCard title="Scripted versus live" subtitle="Evidence quality depends on the provider">
          <ul className={styles.plainList}>
            <li><strong>Live:</strong> Brave or Wikipedia records come from an external service and can support citations.</li>
            <li><strong>Scripted:</strong> deterministic fixture records are useful for regression tests but are never proof of live evidence.</li>
            <li><strong>Injected:</strong> an embedding process supplied the adapter directly; this page cannot verify its liveness or credential requirements.</li>
            <li><strong>No silent fallback:</strong> the runtime never substitutes a scripted provider during a live run.</li>
          </ul>
        </SectionCard>
      </div>
    </div>
  );
};

const StorageTab: React.FC<{
  config: ConfigurationSnapshot;
  volumes: StorageVolumeView[];
  volumesLoading: boolean;
  volumesError: unknown;
  onRefresh: () => void;
}> = ({ config, volumes, volumesLoading, volumesError, onRefresh }) => {
  const storage = config.storage;
  // `editedPath === null` means "follow the current data directory". This
  // avoids copying server state into React state through an effect.
  const [editedPath, setEditedPath] = useState<string | null>(null);
  const validation = useValidateStoragePath();
  const path = editedPath ?? storage.data_dir ?? '';

  const dirty = path.trim() !== (storage.data_dir || '');
  const currentUsed = usedPercent(storage.used_bytes, storage.total_bytes);

  const updatePath = (next: string) => {
    setEditedPath(next);
    validation.reset();
  };

  const runValidation = () => {
    if (!path.trim()) return;
    validation.mutate(path.trim());
  };

  return (
    <div className={styles.tabStack}>
      <InfoBanner tone="warning" title="Storage changes are not applied here">
        Inspecting a path is safe and read-only. Changing <code>storage.data_dir</code> does not
        move existing records, and NEXUS never migrates or deletes data automatically. One
        process must own one data directory.
      </InfoBanner>

      <SectionCard
        title="Current data directory"
        subtitle="This is the directory the running process uses"
        icon={<IconServer />}
        actions={<Badge variant={storage.configured ? (storage.writable ? 'success' : 'error') : 'warning'} size="sm">
          {storage.configured ? (storage.writable ? 'Writable' : 'Not writable') : 'In-memory'}
        </Badge>}
      >
        {storage.configured ? (
          <>
            <div className={styles.pathLine}>
              <span className={styles.mono}>{storage.data_dir}</span>
            </div>
            <div className={styles.capacityRow}>
              <div className={styles.capacityBar} aria-hidden="true">
                <span style={{ width: `${currentUsed ?? 0}%` }} />
              </div>
              <span className={styles.capacityText}>
                {formatBytes(storage.available_bytes)} available of {formatBytes(storage.total_bytes)}
              </span>
            </div>
            <dl className={styles.definitionGrid}>
              <div><dt>Filesystem</dt><dd className={styles.mono}>{storage.filesystem || 'unknown'}</dd></div>
              <div><dt>Device</dt><dd className={styles.mono}>{storage.device || 'unknown'}</dd></div>
              <div><dt>Device type</dt><dd>{storage.device_type && storage.device_type !== 'unknown' ? storage.device_type : 'not exposed'}</dd></div>
              <div><dt>Bus</dt><dd className={styles.mono}>{storage.bus || 'unknown'}</dd></div>
            </dl>
          </>
        ) : (
          <EmptyState title="No durable directory configured">
            Set <code>storage.data_dir</code> in the host configuration and restart. Until then,
            organizational records are process-local.
          </EmptyState>
        )}
        {storage.warnings?.map((w) => (
          <p className={styles.warningLine} key={w}>{w}</p>
        ))}
      </SectionCard>

      <SectionCard
        title="Detected volumes"
        subtitle="Belong to the NEXUS host, not the browser device"
        actions={<Button variant="ghost" size="sm" onClick={onRefresh} leftIcon={<IconRefresh />}>Refresh</Button>}
      >
        {volumesLoading ? (
          <div className={styles.inlineLoading}>Reading host volumes…</div>
        ) : volumesError ? (
          <div className={styles.inlineError}>
            Volumes could not be read. {errorCorrelation(volumesError) && <span className={styles.mono}>ref {errorCorrelation(volumesError)}</span>}
          </div>
        ) : volumes.length === 0 ? (
          <EmptyState title="No volumes detected">
            The host did not expose a bounded storage candidate. Enter an explicit absolute path
            below or set <code>NEXUS_STORAGE_ROOTS</code> for a server deployment.
          </EmptyState>
        ) : (
          <div className={styles.volumeList}>
            {volumes.map((vol) => (
              <div className={styles.volumeRow} key={vol.path}>
                <div className={styles.volumeMain}>
                  <div className={styles.volumeTitleLine}>
                    <span className={styles.volumeName}>{vol.label}</span>
                    <Badge variant={vol.role === 'data_dir' ? 'info' : 'default'} size="sm">{vol.role.replaceAll('_', ' ')}</Badge>
                    {vol.is_data_dir && <Badge variant="success" size="sm">active</Badge>}
                  </div>
                  <div className={`${styles.mono} ${styles.volumePath}`}>{vol.path}</div>
                  <div className={styles.volumeMeta}>
                    <span>{vol.filesystem || 'filesystem unknown'}</span>
                    <span>{vol.device || 'device unknown'}</span>
                    {vol.device_type && vol.device_type !== 'unknown' && <span>{vol.device_type}</span>}
                    {vol.bus && vol.bus !== 'unknown' && <span>{vol.bus}</span>}
                  </div>
                </div>
                <div className={styles.volumeCapacity}>
                  <span className={styles.mono}>{formatBytes(vol.available_bytes)} free</span>
                  <span className={styles.volumeTotal}>of {formatBytes(vol.total_bytes)}</span>
                  <Badge variant={vol.writable ? 'success' : 'error'} size="sm">
                    {vol.writable ? 'writable' : 'read-only'}
                  </Badge>
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => updatePath(vol.path)}
                >
                  Use path
                </Button>
              </div>
            ))}
          </div>
        )}
        <p className={styles.footnote}>
          Device type is reported only when the host exposes it. NVMe is a storage interface, not
          a media category; a device may be NVMe and SSD at the same time.
        </p>
      </SectionCard>

      <SectionCard
        title="Validate a data directory"
        subtitle="Server-side checks, validation only — nothing is created or applied"
      >
        <div className={styles.formRow}>
          <label className={styles.fieldLabel} htmlFor="storage-path">Absolute path on the NEXUS host</label>
          <div className={styles.inputRow}>
            <input
              id="storage-path"
              className={styles.textInput}
              value={path}
              onChange={(e) => updatePath(e.target.value)}
              placeholder="/srv/nexus/data"
              spellCheck={false}
              autoComplete="off"
            />
            <Button variant="secondary" onClick={runValidation} isLoading={validation.isPending} disabled={!path.trim()}>
              Validate path
            </Button>
            {storage.data_dir && (
              <Button variant="ghost" onClick={() => updatePath(storage.data_dir || '')}>
                Use current
              </Button>
            )}
          </div>
          <div className={styles.fieldMeta}>
            <span>{dirty ? 'Unsaved path — validation does not change the running process.' : 'Matches the current data directory.'}</span>
            {dirty && <Badge variant="warning" size="sm">unsaved</Badge>}
          </div>
        </div>

        {validation.isError && (
          <div className={styles.inlineError}>
            Validation failed: {validation.error instanceof Error ? validation.error.message : 'unknown error'}
          </div>
        )}

        {validation.data && <ValidationResult result={validation.data} />}

        <div className={styles.blockedAction}>
          <div>
            <div className={styles.blockedTitle}>Apply and restart is not implemented</div>
            <p className={styles.mutedText}>
              A validated path is not saved. To change it, an operator must edit
              <code> storage.data_dir</code>, stop the process, preserve the old directory, and
              start NEXUS under the new path. Migration and rollback are separate operations.
            </p>
          </div>
          <Button variant="secondary" disabled>Apply &amp; restart</Button>
        </div>
      </SectionCard>

      <SectionCard title="Remote and VPS deployments" subtitle="Same contract, different host">
        <ul className={styles.plainList}>
          <li>Volumes shown here belong to the machine running NEXUS. A browser cannot grant access to its own disks.</li>
          <li>On a VPS or container, the host may expose only a mounted volume or an overlay filesystem. The UI labels unknown device details instead of inventing them.</li>
          <li>Use an explicitly configured persistent volume when automatic discovery is unavailable. Add it with <code>NEXUS_STORAGE_ROOTS</code> (colon-separated absolute paths) and restart.</li>
          <li>Changing the data directory must not create split-brain: stop the old process before starting a new one on a different directory.</li>
        </ul>
      </SectionCard>

      <p className={styles.footnote}>
        Volume discovery is bounded; there is no arbitrary filesystem browser and no
        unauthenticated path endpoint.
      </p>
    </div>
  );
};

const ValidationResult: React.FC<{ result: StorageValidation }> = ({ result }) => (
  <div className={`${styles.validationResult} ${result.valid ? styles.validationValid : styles.validationInvalid}`}>
    <div className={styles.validationHeader}>
      <div>
        <div className={styles.validationTitle}>
          {result.valid ? 'Path passes validation' : 'Path cannot be applied'}
        </div>
        <div className={`${styles.mono} ${styles.validationPath}`}>{result.path}</div>
      </div>
      <Badge variant={result.valid ? 'success' : 'error'} size="sm">
        {result.valid ? 'valid' : 'invalid'}
      </Badge>
    </div>
    <div className={styles.validationMeta}>
      <span>{result.exists ? 'exists' : 'does not exist'}</span>
      <span>{formatBytes(result.available_bytes)} available</span>
      <span>{result.filesystem || 'filesystem unknown'}</span>
      {result.same_as_current_data_dir && <span>current data directory</span>}
    </div>
    <div className={styles.checkList}>
      {result.checks.map((check) => (
        <div className={styles.checkRow} key={`${check.id}-${check.message}`}>
          {checkBadge(check.status)}
          <span>{check.message}</span>
        </div>
      ))}
    </div>
    {result.notes?.map((note) => (
      <p className={styles.footnote} key={note}>{note}</p>
    ))}
  </div>
);

export const Settings: React.FC<SettingsProps> = ({ controlKey }) => {
  const [tab, setTab] = useState<SettingsTab>('overview');
  const configuration = useConfiguration(controlKey);
  const volumes = useStorageVolumes(controlKey);

  const refresh = () => {
    void configuration.refetch();
    void volumes.refetch();
  };

  const body = (() => {
    if (!controlKey) return <PermissionState />;
    if (configuration.isLoading) return <LoadingState />;
    if (configuration.isError || !configuration.data) {
      return <ErrorState error={configuration.error} onRetry={refresh} />;
    }
    const config = configuration.data;

    switch (tab) {
      case 'models':
        return <ModelsTab config={config} onRefresh={refresh} />;
      case 'search':
        return <SearchTab config={config} />;
      case 'storage':
        return (
          <StorageTab
            config={config}
            volumes={volumes.data?.volumes ?? []}
            volumesLoading={volumes.isLoading}
            volumesError={volumes.error}
            onRefresh={refresh}
          />
        );
      case 'overview':
      default:
        return <OverviewTab config={config} onRefresh={refresh} />;
    }
  })();

  const config = configuration.data;

  return (
    <div className={styles.page}>
      <header className={styles.pageHeader}>
        <div>
          <div className={styles.eyebrow}>PLATFORM / SETTINGS</div>
          <h1 className={styles.pageTitle}>Configuration</h1>
          <p className={styles.pageSubtitle}>
            AI model providers, web search, persistent storage and live-readiness — read-only.
          </p>
        </div>
        <div className={styles.headerActions}>
          {config && (
            <>
              <Badge variant="default" size="sm">{config.environment}</Badge>
              <Badge variant="default" size="sm">{config.nexus_id}</Badge>
            </>
          )}
          <Button
            variant="secondary"
            size="sm"
            onClick={refresh}
            leftIcon={<IconRefresh />}
            isLoading={configuration.isFetching}
            disabled={!controlKey}
          >
            Refresh
          </Button>
        </div>
      </header>

      <div className={styles.tabBar} role="tablist" aria-label="Settings sections">
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={tab === t.id}
            className={`${styles.tab} ${tab === t.id ? styles.tabActive : ''}`}
            onClick={() => setTab(t.id)}
          >
            {t.label}
          </button>
        ))}
      </div>

      <main className={styles.content} role="tabpanel">
        {body}
      </main>

      <footer className={styles.pageFooter}>
        <span>Boot configuration: defaults &lt; file &lt; environment</span>
        <span>Secrets are environment-injected and never returned by the API.</span>
        <span>No provider calls are made from this console.</span>
      </footer>
    </div>
  );
};
