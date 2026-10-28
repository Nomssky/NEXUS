/**
 * NEXUS App - Main application component
 */

import React, { useState, useEffect, useCallback } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { CommandCenter } from './pages/CommandCenter';
import { Research } from './pages/Research';
import { Development } from './pages/Development';
import { Media } from './pages/Media';
import { Meeting } from './pages/Meeting';
import { Settings } from './pages/Settings';
import { stripPersistedControlKey } from './authStorage';
import { apiClient } from './api/client';
import { queryKeys } from './api/hooks';
import './tokens/css-variables.css';
import './App.css';

// Route type for top-level navigation
export type AppRoute =
  | 'command'
  | 'research'
  | 'development'
  | 'media'
  | 'meeting'
  | 'settings';

// Global styles
const AppStyles = () => (
  <style>{`
    @import url('https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600&family=IBM+Plex+Sans:wght@400;500;600;700&display=swap');

    * {
      box-sizing: border-box;
    }

    html {
      font-size: 16px;
      -webkit-font-smoothing: antialiased;
      -moz-osx-font-smoothing: grayscale;
    }

    body {
      margin: 0;
      font-family: var(--font-family-ui);
      font-size: var(--font-size-base);
      line-height: var(--line-height-normal);
      color: var(--color-text-primary);
      background-color: var(--color-surface-base);
      min-height: 100vh;
    }

    #root {
      min-height: 100vh;
    }

    :focus-visible {
      outline: none;
      box-shadow: var(--shadow-focus);
      border-radius: var(--radius-sm);
    }

    ::selection {
      background-color: rgba(0, 212, 170, 0.3);
      color: var(--color-text-primary);
    }

    ::-webkit-scrollbar {
      width: 8px;
      height: 8px;
    }

    ::-webkit-scrollbar-track {
      background: var(--color-surface-base);
    }

    ::-webkit-scrollbar-thumb {
      background: var(--color-surface-border);
      border-radius: var(--radius-full);
    }

    ::-webkit-scrollbar-thumb:hover {
      background: var(--color-text-muted);
    }

    * {
      scrollbar-width: thin;
      scrollbar-color: var(--color-surface-border) var(--color-surface-base);
    }

    @media (prefers-reduced-motion: reduce) {
      *,
      *::before,
      *::after {
        animation-duration: 0.01ms !important;
        animation-iteration-count: 1 !important;
        transition-duration: 0.01ms !important;
        scroll-behavior: auto !important;
      }
    }
  `}</style>
);

// Create React Query client
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5000,
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
});

// Configuration is fetched with an operator key that is deliberately kept out
// of the query key. Drop or refetch the cached snapshot whenever that key
// changes so data authorized by a previous key is never shown as current.
function resetConfigurationQueries(refetch: boolean): void {
  for (const queryKey of [queryKeys.configuration, queryKeys.storageVolumes]) {
    if (refetch) {
      void queryClient.resetQueries({ queryKey });
    } else {
      queryClient.removeQueries({ queryKey });
    }
  }
}

interface AuthState {
  // Non-secret session state persisted across reloads. The credential is
  // deliberately NOT stored: it must be re-entered on every session.
  actorId: string;
  businessId: string;
  controlKey: string;
}

const DEFAULT_AUTH: AuthState = {
  actorId: 'nx:human:bootstrap',
  businessId: 'default',
  controlKey: '',
};

type FirstRunStage = 'signin' | 'bootstrap' | 'complete';

interface FirstRunState {
  stage: FirstRunStage;
  businessName: string;
  primaryObjective: string;
  divisions: string[];
  isSubmitting: boolean;
}

const DEFAULT_FIRST_RUN: FirstRunState = {
  stage: 'signin',
  businessName: '',
  primaryObjective: '',
  divisions: [],
  isSubmitting: false,
};

function AuthProvider({ children }: { children: React.ReactNode }) {
  const [auth, setAuth] = useState<AuthState>(DEFAULT_AUTH);
  const [isAuthenticated, setIsAuthenticated] = useState(false);
  const [authError, setAuthError] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [firstRun, setFirstRun] = useState<FirstRunState>(DEFAULT_FIRST_RUN);

  // Check for stored non-secret auth on mount. The credential is never
  // persisted: on reload the user must re-enter it. Stored actorId and
  // businessId are only presentation hints — they are never trusted as proof
  // of identity. The credential is always re-verified against the server.
  useEffect(() => {
    let mounted = true;
    (async () => {
      const stored = localStorage.getItem('nexus-auth');
      if (stored && mounted) {
        // The control-plane key is intentionally memory-only. Remove a legacy
        // persisted value even when the record has no usable actor credential.
        const { parsed, serialized } = stripPersistedControlKey(stored);
        if (serialized) {
          localStorage.setItem('nexus-auth', serialized);
        }
        if (
          parsed &&
          typeof parsed.actorId === 'string' &&
          parsed.actorId &&
          typeof parsed.businessId === 'string' &&
          parsed.businessId
        ) {
          const restored: AuthState = {
            ...DEFAULT_AUTH,
            actorId: parsed.actorId,
            businessId: parsed.businessId,
          };
          setAuth(restored);
        }
      }
      if (mounted) {
        setIsLoading(false);
      }
    })();
    return () => {
      mounted = false;
    };
  }, []);

  // Validate input before any network call.
  function validateInput(actorId: string, credential: string, businessId: string): string | null {
    const cleanActorId = actorId.trim();
    const cleanCredential = credential.trim();
    const cleanBusinessId = businessId.trim() || 'default';

    if (!cleanActorId) return 'Actor ID is required';
    if (!/^[\w:.-]+$/.test(cleanActorId)) return 'Actor ID contains invalid characters';
    if (cleanActorId.length > 128) return 'Actor ID is too long (max 128 characters)';
    if (!cleanCredential) return 'Credential is required';
    if (cleanCredential.length > 512) return 'Credential is too long (max 512 characters)';
    if (!/^[\w.-]*$/.test(cleanBusinessId)) return 'Business ID contains invalid characters';
    return null;
  }

  const login = useCallback(async (actorId: string, credential: string, businessId: string = 'default') => {
    setAuthError(null);

    const err = validateInput(actorId, credential, businessId);
    if (err) {
      setAuthError(err);
      return false;
    }

    const cleanActorId = actorId.trim();
    const cleanCredential = credential.trim();
    const cleanBusinessId = businessId.trim() || 'default';

    try {
      // Verify credentials server-side via an authenticated, business-scoped
      // endpoint. /health is unauthenticated and must never be used for login.
      apiClient.setAuth(cleanActorId, cleanCredential);
      await apiClient.verifyCredentials(cleanBusinessId);

      // Persist only non-secret state. The credential lives in this tab's
      // memory for the duration of the session and is cleared on unload.
      const newAuth: AuthState = { actorId: cleanActorId, businessId: cleanBusinessId, controlKey: '' };
      setAuth(newAuth);
      apiClient.setAuth(cleanActorId, cleanCredential);
      localStorage.setItem('nexus-auth', JSON.stringify(newAuth));
      setIsAuthenticated(true);
      setFirstRun({ ...DEFAULT_FIRST_RUN, stage: 'bootstrap' });
      return true;
    } catch (err) {
      apiClient.clearAuth();
      const apiErr = err as { status?: number; message?: string } | undefined;
      if (apiErr && apiErr.status === 401) {
        setAuthError('Invalid actor ID or credential');
      } else {
        setAuthError('Unable to reach NEXUS. Check that the backend is running.');
      }
      return false;
    }
  }, []);

  const provisionBusiness = useCallback(async (
    actorId: string,
    credential: string,
    businessId: string,
    businessName: string,
    primaryObjective: string,
    divisions: string[]
  ): Promise<boolean> => {
    setAuthError(null);
    setFirstRun(prev => ({ ...prev, isSubmitting: true }));

    try {
      // 1. Verify credentials server-side.
      // Only set auth headers if a credential was explicitly provided;
      // otherwise the credentials from sign-in are already in effect.
      if (credential) {
        apiClient.setAuth(actorId, credential);
      }
      await apiClient.verifyCredentials(businessId);

      // 2. Persist non-secret session state.
      // Only re-set auth headers if a credential was explicitly provided;
      // otherwise the credentials from sign-in are already in effect.
      if (credential) {
        apiClient.setAuth(actorId, credential);
      }
      const newAuth: AuthState = { actorId, businessId, controlKey: '' };
      setAuth(newAuth);
      localStorage.setItem('nexus-auth', JSON.stringify(newAuth));

      // 3. Create the business. If it already exists (conflict), proceed to
      // membership — the business was provisioned earlier or by bootstrap.
      try {
        await apiClient.createBusiness({
          entity_id: businessId,
          name: businessName,
          owner_identity_id: actorId,
          metadata: {
            objective: primaryObjective,
            divisions: divisions.join(','),
            provisioned_via: 'first-run',
          },
        });
      } catch (err) {
        const apiErr = err as { status?: number } | undefined;
        if (!(apiErr && apiErr.status === 409)) {
          throw err;
        }
        // Business already exists — continue to membership.
      }

      // 4. Establish initial membership. The actor must be the owner.
      try {
        await apiClient.createBusinessMember(businessId);
      } catch (err) {
        const apiErr = err as { status?: number } | undefined;
        if (!(apiErr && apiErr.status === 409)) {
          throw err;
        }
        // Membership already exists — proceed.
      }

      setIsAuthenticated(true);
      setFirstRun({ ...DEFAULT_FIRST_RUN, stage: 'complete' });
      return true;
    } catch (err) {
      apiClient.clearAuth();
      const apiErr = err as { status?: number; message?: string } | undefined;
      console.log('Provisioning error:', apiErr?.status, apiErr?.message, err);
      if (apiErr && apiErr.status === 401) {
        setAuthError('Invalid actor ID or credential');
      } else if (apiErr && apiErr.status === 403) {
        setAuthError('Not authorized to provision this business.');
      } else {
        setAuthError('Provisioning failed. Check that the backend is running.');
      }
      setFirstRun(prev => ({ ...prev, isSubmitting: false }));
      return false;
    }
  }, []);

  const setControlKey = useCallback((key: string) => {
    apiClient.setControlKey(key);
    setAuth((prev) => {
      const updated = { ...prev, controlKey: key };
      // Deliberately not persisted: the key lives in this tab's memory only.
      // Persisting an operator key in localStorage would leave the secret on
      // disk and survive sign-out.
      localStorage.setItem('nexus-auth', JSON.stringify({ ...updated, controlKey: '' }));
      return updated;
    });
    resetConfigurationQueries(key !== '');
  }, []);

  const logout = useCallback(() => {
    apiClient.clearAuth();
    localStorage.removeItem('nexus-auth');
    setAuth(DEFAULT_AUTH);
    setIsAuthenticated(false);
    resetConfigurationQueries(false);
  }, []);

  if (isLoading) {
    return (
      <>
        <AppStyles />
        <div className="loading-screen">
          <div className="loading-spinner">
            <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
              <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeDasharray="31.4 31.4" className="spin" />
            </svg>
          </div>
          <p>Initializing NEXUS…</p>
        </div>
      </>
    );
  }

  return (
    <AuthContext.Provider value={{ auth, login, provisionBusiness, setControlKey, logout, authError, isAuthenticated, firstRun, setFirstRun, setAuthError }}>
      {children}
    </AuthContext.Provider>
  );
}

const AuthContext = React.createContext<{
  auth: AuthState;
  login: (actorId: string, credential: string, businessId?: string) => Promise<boolean>;
  provisionBusiness: (
    actorId: string,
    credential: string,
    businessId: string,
    businessName: string,
    primaryObjective: string,
    divisions: string[]
  ) => Promise<boolean>;
  setControlKey: (key: string) => void;
  logout: () => void;
  authError: string | null;
  isAuthenticated: boolean;
  firstRun: FirstRunState;
  setFirstRun: React.Dispatch<React.SetStateAction<FirstRunState>>;
  setAuthError: (message: string) => void;
} | null>(null);

function useAuth() {
  const context = React.useContext(AuthContext);
  if (!context) {
    throw new Error('useAuth must be used within AuthProvider');
  }
  return context;
}

function SignInScreen() {
  const { login, authError, auth } = useAuth();
  const [actorId, setActorId] = useState(auth.actorId || 'nx:human:bootstrap');
  const [credential, setCredential] = useState('');
  const [businessId, setBusinessId] = useState(auth.businessId || 'default');
  const [isSubmitting, setIsSubmitting] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsSubmitting(true);
    const success = await login(actorId, credential, businessId);
    setIsSubmitting(false);
  };

  return (
    <div className="login-container">
      <div className="login-card">
        <aside className="login-brand">
          <div className="logo-wrap">
            <div className="logo-mark" aria-hidden="true">
              <svg width="14" height="14" viewBox="0 0 48 48" fill="none">
                <rect x="5" y="5" width="38" height="38" rx="8" stroke="currentColor" strokeWidth="2" />
                <path d="M16 24h16M24 16v16" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
              </svg>
            </div>
            <div>
              <h1>NEXUS</h1>
              <p className="login-tagline">personal AI operating system</p>
            </div>
          </div>

          <div className="login-steps">
            <div className="login-step login-step-current">
              <span className="login-step-num">1</span>
              <span>Business</span>
            </div>
            <div className="login-step">
              <span className="login-step-num">2</span>
              <span>Objective</span>
            </div>
            <div className="login-step">
              <span className="login-step-num">3</span>
              <span>Authority</span>
            </div>
            <div className="login-step">
              <span className="login-step-num">4</span>
              <span>Agents</span>
            </div>
          </div>

          <h2>Welcome</h2>
          <p>NEXUS runs the work.</p>
          <p className="login-lead">
            Give it one business, one objective and a clear limit of authority.
            It will plan, delegate and keep working while you are away.
          </p>

          <div className="login-pillars">
            <div className="login-pillar">
              <p className="login-pillar-title">You stay the root authority</p>
              <p className="login-pillar-desc">
                Nothing runs outside delegated authority. Default deny holds.
              </p>
            </div>
            <div className="login-pillar">
              <p className="login-pillar-title">It keeps a why for every task</p>
              <p className="login-pillar-desc">
                Objective lineage survives every delegation.
              </p>
            </div>
            <div className="login-pillar">
              <p className="login-pillar-title">The office is only a view</p>
              <p className="login-pillar-desc">
                Every action exists as a list and a table too.
              </p>
            </div>
          </div>
        </aside>

        <div className="login-form-card">
          <p className="login-eyebrow">Sign in to NEXUS</p>
          <h3>Enter your credentials</h3>
          <p className="login-hint">
            Credentials are verified by the backend before a session is created.
            The credential is not stored — you will enter it again on reload.
          </p>

          {authError && (
            <div className="login-error" role="alert">
              <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
                <circle cx="8" cy="8" r="7" stroke="currentColor" strokeWidth="2" />
                <path d="M8 4v6M8 12v.01" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
              </svg>
              {authError}
            </div>
          )}

          <form onSubmit={handleSubmit} className="login-form">
            <div className="login-field">
              <label htmlFor="actor-id">Actor ID</label>
              <div className="field-wrap">
                <div className="field-lead" aria-hidden="true" />
                <input
                  id="actor-id"
                  type="text"
                  value={actorId}
                  onChange={(e) => setActorId(e.target.value)}
                  required
                  autoComplete="username"
                  placeholder="e.g. nx:human:bootstrap"
                  disabled={isSubmitting}
                />
              </div>
            </div>

            <div className="login-field credential">
              <label htmlFor="credential">Credential</label>
              <div className="field-wrap">
                <div className="field-lead" aria-hidden="true" />
                <input
                  id="credential"
                  type="password"
                  value={credential}
                  onChange={(e) => setCredential(e.target.value)}
                  required
                  autoComplete="current-password"
                  placeholder="••••••••"
                  disabled={isSubmitting}
                />
              </div>
            </div>

            <div className="login-field business">
              <label htmlFor="business-id">Business ID</label>
              <div className="field-wrap">
                <div className="field-lead" aria-hidden="true" />
                <input
                  id="business-id"
                  type="text"
                  value={businessId}
                  onChange={(e) => setBusinessId(e.target.value)}
                  required
                  placeholder="default"
                  disabled={isSubmitting}
                />
              </div>
            </div>

            <button type="submit" className="login-submit login-submit-primary" disabled={isSubmitting}>
              {isSubmitting ? 'Signing in…' : 'Sign in'}
            </button>
          </form>

          <div className="login-footer">
            <p>
              Default bootstrap actor: <code>nx:human:bootstrap</code>. The
              credential is sourced from{' '}
              <code>NEXUS_BOOTSTRAP_CREDENTIAL</code> (env-only, never echoed
              here).
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}

function BootstrapScreen() {
  const { provisionBusiness, firstRun, setFirstRun, authError, auth, setAuthError } = useAuth();
  const [businessName, setBusinessName] = useState('');
  const [primaryObjective, setPrimaryObjective] = useState('');
  const [divisions, setDivisions] = useState<string[]>([]);
  const [isSubmitting, setIsSubmitting] = useState(false);

  const divisionOptions = [
    { id: 'research', name: 'Research', color: '#4FD1C5' },
    { id: 'business', name: 'Business', color: '#F5B759' },
    { id: 'media', name: 'Media', color: '#B18CFF' },
    { id: 'development', name: 'Development', color: '#3DD68C' },
  ];

  const handleToggleDivision = (id: string) => {
    setDivisions((prev) =>
      prev.includes(id) ? prev.filter((d) => d !== id) : [...prev, id]
    );
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setIsSubmitting(true);
    const success = await provisionBusiness(
      auth.actorId,
      '', // credential already verified at sign-in
      auth.businessId,
      businessName,
      primaryObjective,
      divisions
    );
    setIsSubmitting(false);
    if (!success) {
      console.log('Provisioning failed');
    }
    if (success) {
      setFirstRun({ ...DEFAULT_FIRST_RUN, stage: 'complete' });
    }
  };

  return (
    <div className="login-container">
      <div className="login-card">
        <aside className="login-brand">
          <div className="logo-wrap">
            <div className="logo-mark" aria-hidden="true">
              <svg width="14" height="14" viewBox="0 0 48 48" fill="none">
                <rect x="5" y="5" width="38" height="38" rx="8" stroke="currentColor" strokeWidth="2" />
                <path d="M16 24h16M24 16v16" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
              </svg>
            </div>
            <div>
              <h1>NEXUS</h1>
              <p className="login-tagline">personal AI operating system</p>
            </div>
          </div>

          <div className="login-steps">
            <div className="login-step login-step-current">
              <span className="login-step-num">1</span>
              <span>Business</span>
            </div>
            <div className="login-step">
              <span className="login-step-num">2</span>
              <span>Objective</span>
            </div>
            <div className="login-step">
              <span className="login-step-num">3</span>
              <span>Authority</span>
            </div>
            <div className="login-step">
              <span className="login-step-num">4</span>
              <span>Agents</span>
            </div>
          </div>

          <h2>Welcome</h2>
          <p>NEXUS runs the work.</p>
          <p className="login-lead">
            Give it one business, one objective and a clear limit of authority.
            It will plan, delegate and keep working while you are away.
          </p>

          <div className="login-pillars">
            <div className="login-pillar">
              <p className="login-pillar-title">You stay the root authority</p>
              <p className="login-pillar-desc">
                Nothing runs outside delegated authority. Default deny holds.
              </p>
            </div>
            <div className="login-pillar">
              <p className="login-pillar-title">It keeps a why for every task</p>
              <p className="login-pillar-desc">
                Objective lineage survives every delegation.
              </p>
            </div>
            <div className="login-pillar">
              <p className="login-pillar-title">The office is only a view</p>
              <p className="login-pillar-desc">
                Every action exists as a list and a table too.
              </p>
            </div>
          </div>
        </aside>

        <div className="login-form-card">
          <p className="login-eyebrow">Create your first business</p>
          <h3>A business is the scope everything else hangs from.</h3>
          <p className="login-hint">This becomes the business_id on every record.</p>

          {authError && (
            <div className="login-error" role="alert">
              <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
                <circle cx="8" cy="8" r="7" stroke="currentColor" strokeWidth="2" />
                <path d="M8 4v6M8 12v.01" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
              </svg>
              {authError}
            </div>
          )}

          <form onSubmit={handleSubmit} className="login-form">
            <div className="login-field">
              <label htmlFor="business-name">Business Name</label>
              <input
                id="business-name"
                type="text"
                value={businessName}
                onChange={(e) => setBusinessName(e.target.value)}
                required
                placeholder="e.g. AI Agency"
                disabled={isSubmitting}
              />
              <p className="login-field-note">This becomes the business_id on every record.</p>
            </div>

            <div className="login-field">
              <label htmlFor="primary-objective">Primary Objective</label>
              <input
                id="primary-objective"
                type="text"
                value={primaryObjective}
                onChange={(e) => setPrimaryObjective(e.target.value)}
                required
                placeholder="e.g. Win three enterprise deals this quarter"
                disabled={isSubmitting}
              />
              <p className="login-field-note">The why every task will point back to.</p>
            </div>

            <div className="login-field">
              <label>Divisions</label>
              <p className="login-field-note">A division is a room with its own policy and memory scope.</p>
              <div className="division-chips" role="group" aria-label="Divisions">
                {divisionOptions.map((d) => (
                  <button
                    key={d.id}
                    type="button"
                    onClick={() => handleToggleDivision(d.id)}
                    className={`division-chip ${divisions.includes(d.id) ? 'division-chip-selected' : ''}`}
                    style={{ borderColor: d.color }}
                  >
                    {d.name}
                  </button>
                ))}
              </div>
            </div>

            <div className="login-actions">
              <button type="button" className="login-submit login-submit-secondary" disabled={isSubmitting} onClick={() => {
                setAuthError('Load a sample is not available through the current backend contracts.');
              }}>
                Load a sample
              </button>
              <button type="submit" className="login-submit login-submit-primary" disabled={isSubmitting}>
                {isSubmitting ? 'Creating business…' : 'Create business'}
              </button>
            </div>

            <p className="login-step-indicator">Step 1 of 4</p>
          </form>

          <div className="login-footer">
            <p>
              Runtime runs locally. Nothing leaves this machine unless a policy allows it.
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}

function AppContent() {
  const { isAuthenticated, auth, logout, firstRun } = useAuth();
  const [route, setRoute] = useState<AppRoute>('command');

  if (!isAuthenticated) {
    return <SignInScreen />;
  }

  if (firstRun.stage === 'bootstrap') {
    return <BootstrapScreen />;
  }

  return (
    <div className="app-layout">
      <header className="app-header">
        <div className="app-header-left">
          <button onClick={logout} className="logout-button">
            <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
              <path d="M10 4L6 8M6 8l4 4M6 8h8" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
            </svg>
            Sign out
          </button>
          <span className="app-user">
            {auth.actorId} @ {auth.businessId}
          </span>
          <nav className="app-nav" role="tablist" aria-label="Main navigation">
            <button
              role="tab"
              aria-selected={route === 'command'}
              onClick={() => setRoute('command')}
              className={`app-nav-item ${route === 'command' ? 'active' : ''}`}
            >
              <svg width="18" height="18" viewBox="0 0 18 18" fill="none" aria-hidden="true">
                <rect x="2" y="2" width="14" height="14" rx="3" stroke="currentColor" strokeWidth="1.5" />
                <path d="M6 9h6M9 6v6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
              </svg>
              Command
            </button>
            <button
              role="tab"
              aria-selected={route === 'research'}
              onClick={() => setRoute('research')}
              className={`app-nav-item ${route === 'research' ? 'active' : ''}`}
            >
              <svg width="18" height="18" viewBox="0 0 18 18" fill="none" aria-hidden="true">
                <circle cx="9" cy="9" r="6" stroke="currentColor" strokeWidth="1.5" />
                <path d="M9 14v4M14 14l-5-5 5-5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
              </svg>
              Research
            </button>
            <button
              role="tab"
              aria-selected={route === 'development'}
              onClick={() => setRoute('development')}
              className={`app-nav-item ${route === 'development' ? 'active' : ''}`}
            >
              <svg width="18" height="18" viewBox="0 0 18 18" fill="none" aria-hidden="true">
                <path d="M3 15l4-4 4 4M7 11v8M11 11v8" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
              </svg>
              Development
            </button>
            <button
              role="tab"
              aria-selected={route === 'media'}
              onClick={() => setRoute('media')}
              className={`app-nav-item ${route === 'media' ? 'active' : ''}`}
            >
              <svg width="18" height="18" viewBox="0 0 18 18" fill="none" aria-hidden="true">
                <rect x="3" y="4" width="12" height="10" rx="2" stroke="currentColor" strokeWidth="1.5" />
                <circle cx="9" cy="9" r="2.5" stroke="currentColor" strokeWidth="1.5" />
              </svg>
              Media
            </button>
            <button
              role="tab"
              aria-selected={route === 'meeting'}
              onClick={() => setRoute('meeting')}
              className={`app-nav-item ${route === 'meeting' ? 'active' : ''}`}
            >
              <svg width="18" height="18" viewBox="0 0 18 18" fill="none" aria-hidden="true">
                <circle cx="6" cy="6" r="3" stroke="currentColor" strokeWidth="1.5" />
                <circle cx="12" cy="6" r="3" stroke="currentColor" strokeWidth="1.5" />
                <circle cx="6" cy="12" r="3" stroke="currentColor" strokeWidth="1.5" />
                <circle cx="12" cy="12" r="3" stroke="currentColor" strokeWidth="1.5" />
              </svg>
              Meeting
            </button>
            <button
              role="tab"
              aria-selected={route === 'settings'}
              onClick={() => setRoute('settings')}
              className={`app-nav-item ${route === 'settings' ? 'active' : ''}`}
            >
              <svg width="18" height="18" viewBox="0 0 18 18" fill="none" aria-hidden="true">
                <circle cx="9" cy="9" r="2.4" stroke="currentColor" strokeWidth="1.5" />
                <path
                  d="M9 2.4v1.7M9 13.9v1.7M2.4 9h1.7M13.9 9h1.7M4.3 4.3l1.2 1.2M12.5 12.5l1.2 1.2M13.7 4.3l-1.2 1.2M5.5 12.5l-1.2 1.2"
                  stroke="currentColor"
                  strokeWidth="1.4"
                  strokeLinecap="round"
                />
              </svg>
              Settings
            </button>
          </nav>
        </div>
        <div className="app-header-right">
          <ControlKeyInput />
        </div>
      </header>
      <main className="app-main">
        {route === 'command' ? (
          <CommandCenter businessId={auth.businessId} actorId={auth.actorId} />
        ) : route === 'research' ? (
          <Research businessId={auth.businessId} actorId={auth.actorId} />
        ) : route === 'development' ? (
          <Development businessId={auth.businessId} actorId={auth.actorId} />
        ) : route === 'media' ? (
          <Media businessId={auth.businessId} actorId={auth.actorId} />
        ) : route === 'settings' ? (
          <Settings controlKey={auth.controlKey} />
        ) : (
          <Meeting businessId={auth.businessId} actorId={auth.actorId} />
        )}
      </main>
    </div>
  );
}

function ControlKeyInput() {
  const { auth, setControlKey } = useAuth();
  const [key, setKey] = useState('');
  const [showInput, setShowInput] = useState(false);

  if (!showInput && !auth.controlKey) {
    return (
      <button onClick={() => setShowInput(true)} className="control-key-trigger">
        <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
          <rect x="2" y="4" width="12" height="8" rx="1" stroke="currentColor" strokeWidth="1.5" />
          <path d="M6 12v-4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
        </svg>
        Set Control Key
      </button>
    );
  }

  return (
    <div className="control-key-input">
      {showInput && (
        <input
          type="password"
          value={key}
          onChange={(e) => setKey(e.target.value)}
          placeholder="Control plane API key"
          className="control-key-field"
          onBlur={() => {
            if (key.trim()) setControlKey(key.trim());
            setShowInput(false);
            setKey('');
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && key.trim()) {
              setControlKey(key.trim());
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
      {auth.controlKey && !showInput && (
        <span className="control-key-display">
          <svg width="14" height="14" viewBox="0 0 14 14" fill="none" aria-hidden="true">
            <rect x="1" y="3" width="12" height="8" rx="1" stroke="currentColor" strokeWidth="1.5" />
            <path d="M5 11v-4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
          </svg>
          Control key set
          <button onClick={() => setControlKey('')} className="control-key-clear" aria-label="Clear control key">
            <svg width="12" height="12" viewBox="0 0 12 12" fill="none" aria-hidden="true">
              <path d="M3 3l6 6M9 3l-6 6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
            </svg>
          </button>
        </span>
      )}
    </div>
  );
}

const App: React.FC = () => {
  return (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <AppStyles />
        <AppContent />
      </AuthProvider>
    </QueryClientProvider>
  );
};

export default App;