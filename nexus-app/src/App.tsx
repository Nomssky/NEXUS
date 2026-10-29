/**
 * NEXUS App - Main application component
 */

import React, { useState, useEffect, useCallback } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { OfficeLayout } from './components/OfficeLayout/OfficeLayout';
import { CommandCenter } from './pages/CommandCenter';
import { Research } from './pages/Research';
import { Development } from './pages/Development';
import { Media } from './pages/Media';
import { Meeting } from './pages/Meeting';
import { Settings } from './pages/Settings';
import { PenpotPage } from './pages/PenpotPage';
import { stripPersistedControlKey } from './authStorage';
import { parseLocalSessionConfig } from './localSession';
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
  | 'settings'
  | 'knowledge'
  | 'first-run'
  | 'virtual-office'
  | 'agent-inspector'
  | 'agent-roster'
  | 'task-inspector'
  | 'states'
  | 'businesses'
  | 'division-detail'
  | 'command-palette'
  | 'attention-inbox'
  | 'analytics'
  | 'settings-system'
  | 'activity-events'
  | 'governance-approvals'
  | 'model-providers'
  | 'web-search'
  | 'storage'
  | 'configuration';

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
export const queryClient = new QueryClient({
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
  actorId: string;
  credential: string;
  businessId: string;
  controlKey: string;
}

const DEFAULT_AUTH: AuthState = {
  actorId: 'nx:human:bootstrap',
  credential: '',
  businessId: 'default',
  controlKey: '',
};

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [auth, setAuth] = useState<AuthState>(DEFAULT_AUTH);
  const [isAuthenticated, setIsAuthenticated] = useState(false);
  const [authError, setAuthError] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(false);

  // When the API client receives a 401, clear protected React Query caches and
  // reset authenticated UI state so stale authorized data is never presented
  // after the session is rejected.
  apiClient.setOnUnauthenticated(() => {
    queryClient.clear();
    setIsAuthenticated(false);
    setAuthError('Session expired or invalid. Please sign in again.');
  });

  // Restore an authenticated session from a stored record, validating the
  // credentials against a real endpoint before marking the app authenticated.
  // Returns true when a usable session was restored.
  const restoreStoredSession = useCallback(async (): Promise<boolean> => {
    const stored = localStorage.getItem('nexus-auth');
    if (!stored) return false;
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
      typeof parsed.credential === 'string' &&
      parsed.credential
    ) {
      const restored: AuthState = {
        ...DEFAULT_AUTH,
        ...(parsed as Partial<AuthState>),
        controlKey: '',
      };
      setAuth(restored);
      apiClient.setAuth(restored.actorId, restored.credential);
      try {
        await apiClient.listBusinesses();
      } catch {
        // Stored credentials no longer valid; fall through to login.
        apiClient.clearAuth();
        localStorage.removeItem('nexus-auth');
        setAuth(DEFAULT_AUTH);
        setIsAuthenticated(false);
        return false;
      }
      setIsAuthenticated(true);
      return true;
    }
    return false;
  }, []);

  // On a fresh local desktop launch there is no stored auth. The launcher
  // provisions a bootstrap identity and writes a short-lived local-session
  // file (served only on localhost by the frontend static server) carrying
  // the API base URL and bootstrap credentials. Fetch it first so the user
  // lands directly in the Virtual Office without copying a credential.
  //
  // This is a single-user local-desktop convenience only. The login screen
  // remains the default for every other deployment mode, and the bootstrap
  // identity grants no permissions beyond what the local user already holds.
  useEffect(() => {
    let mounted = true;

    const establishSession = async (): Promise<void> => {
      let established = false;

      // 1. Local desktop session (preferred): launcher-provided credentials.
      try {
        const res = await fetch('/local-session.json', { cache: 'no-store' });
        if (res.ok) {
          const config = parseLocalSessionConfig(await res.text());
          if (config) {
            apiClient.setBaseURL(config.apiBase);
            apiClient.setAuth(config.actorId, config.credential);
            // Validate against a real authenticated endpoint before trusting it.
            await apiClient.listBusinesses();
            if (!mounted) return;
            const restored: AuthState = {
              actorId: config.actorId,
              credential: config.credential,
              businessId: config.businessId,
              controlKey: '',
            };
            setAuth(restored);
            localStorage.setItem('nexus-auth', JSON.stringify(restored));
            setIsAuthenticated(true);
            established = true;
          }
        }
      } catch {
        // local-session.json unavailable or invalid: fall through to stored auth or login.
        setIsAuthenticated(false);
      }

      // 2. Stored auth (refresh persistence, or non-desktop deployments).
      if (!established) {
        established = await restoreStoredSession();
      }

      if (!mounted) return;
      setIsLoading(false);
    };

    void establishSession();

    return () => {
      mounted = false;
    };
  }, [restoreStoredSession]);

  const login = useCallback(async (actorId: string, credential: string, businessId: string = 'default') => {
    setAuthError(null);

    // --- Input validation (before any network call) ---
    const cleanActorId = actorId.trim();
    const cleanCredential = credential.trim();
    const cleanBusinessId = businessId.trim() || 'default';

    if (!cleanActorId) {
      setAuthError('Actor ID is required');
      return false;
    }
    // Actor ID is sent as an HTTP header — allow only safe identifier characters
    if (!/^[\w:.-]+$/.test(cleanActorId)) {
      setAuthError('Actor ID contains invalid characters');
      return false;
    }
    if (cleanActorId.length > 128) {
      setAuthError('Actor ID is too long (max 128 characters)');
      return false;
    }
    if (!cleanCredential) {
      setAuthError('Credential is required');
      return false;
    }
    if (cleanCredential.length > 512) {
      setAuthError('Credential is too long (max 512 characters)');
      return false;
    }
    if (!/^[\w.-]*$/.test(cleanBusinessId)) {
      setAuthError('Business ID contains invalid characters');
      return false;
    }

    try {
      // Test the credentials against an authenticated endpoint.
      // GET /health is public and does not validate credentials, so we use
      // GET /api/v1/businesses, which requires a valid actor + credential
      // and returns 401 for wrong or missing credentials.
      apiClient.setAuth(cleanActorId, cleanCredential);
      await apiClient.listBusinesses();

      const newAuth = { actorId: cleanActorId, credential: cleanCredential, businessId: cleanBusinessId, controlKey: '' };
      setAuth(newAuth);
      localStorage.setItem('nexus-auth', JSON.stringify(newAuth));
      setIsAuthenticated(true);
      return true;
    } catch (err) {
      apiClient.clearAuth();
      const message = err instanceof Error ? err.message : 'Authentication failed';
      setAuthError(message);
      setIsAuthenticated(false);
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
    // Clear all protected React Query caches so no stale authorized data
    // survives logout or session invalidation.
    queryClient.clear();
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
    <AuthContext.Provider value={{ auth, login, setControlKey, logout, authError, isAuthenticated }}>
      {children}
    </AuthContext.Provider>
  );
}

const AuthContext = React.createContext<{
  auth: AuthState;
  login: (actorId: string, credential: string, businessId?: string) => Promise<boolean>;
  setControlKey: (key: string) => void;
  logout: () => void;
  authError: string | null;
  isAuthenticated: boolean;
} | null>(null);

function useAuth() {
  const context = React.useContext(AuthContext);
  if (!context) {
    throw new Error('useAuth must be used within AuthProvider');
  }
  return context;
}

export { useAuth };

function LoginScreen() {
  const { login, authError } = useAuth();
  const [actorId, setActorId] = useState('nx:human:bootstrap');
  const [credential, setCredential] = useState('');
  const [businessId, setBusinessId] = useState('default');
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
        <div className="login-header">
          <svg width="48" height="48" viewBox="0 0 48 48" fill="none" aria-hidden="true">
            <rect x="5" y="5" width="38" height="38" rx="8" stroke="currentColor" strokeWidth="2" />
            <path d="M16 24h16M24 16v16" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
          </svg>
          <h1>NEXUS</h1>
          <p>Command Center & Virtual HQ</p>
        </div>

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
            <input
              id="actor-id"
              type="text"
              value={actorId}
              onChange={(e) => setActorId(e.target.value)}
              required
              autoComplete="username"
            />
          </div>

          <div className="login-field">
            <label htmlFor="credential">Credential</label>
            <input
              id="credential"
              type="password"
              value={credential}
              onChange={(e) => setCredential(e.target.value)}
              required
              autoComplete="current-password"
            />
          </div>

          <div className="login-field">
            <label htmlFor="business-id">Business ID</label>
            <input
              id="business-id"
              type="text"
              value={businessId}
              onChange={(e) => setBusinessId(e.target.value)}
              required
            />
          </div>

          <button type="submit" className="login-submit" disabled={isSubmitting}>
            {isSubmitting ? 'Connecting…' : 'Connect to NEXUS'}
          </button>
        </form>

        <div className="login-footer">
          <p>Default bootstrap: <code>nx:human:bootstrap</code> with credential from <code>NEXUS_BOOTSTRAP_CREDENTIAL</code> env var</p>
        </div>
      </div>
    </div>
  );
}

function AppContent() {
  const { auth, logout, setControlKey, isAuthenticated } = useAuth();
  const [route, setRoute] = useState<AppRoute>('command');
  const storedBusinessId = (() => {
    try {
      const stored = localStorage.getItem('nexus-auth');
      if (!stored) return undefined;
      const parsed = JSON.parse(stored);
      if (parsed && parsed.businessId && parsed.businessId !== 'default') {
        return parsed.businessId as string;
      }
    } catch {
      // Ignore parse errors.
    }
    return undefined;
  })();
  const [businessId, setBusinessId] = useState(storedBusinessId ?? auth.businessId);
  const [onboardingComplete, setOnboardingComplete] = useState(!!storedBusinessId);

  if (!isAuthenticated) {
    return <LoginScreen />;
  }

  // First-run onboarding skipped - go directly to CommandCenter.
  // FirstRunPenpot remains available as a Penpot design route.

  // Penpot routes are complete full-page designs (1440×1024 with their own
  // topbar, panels, status bar). They render WITHOUT the OfficeLayout wrapper
  // so they match the Penpot design 1:1. Original NEXUS routes keep the
  // OfficeLayout shell.
  const PENPOT_ROUTES: Set<AppRoute> = new Set([
    'first-run', 'virtual-office', 'agent-inspector', 'agent-roster',
    'task-inspector', 'states', 'businesses', 'division-detail',
    'command-palette', 'attention-inbox', 'analytics', 'settings-system',
    'activity-events', 'governance-approvals', 'model-providers', 'web-search',
    'storage', 'configuration', 'knowledge',
  ]);

  const renderPenpotPage = () => (
    <PenpotPage
      route={route}
      businessId={businessId}
      actorId={auth.actorId}
    />
  );

  // Full-page Penpot designs: render without OfficeLayout chrome.
  if (PENPOT_ROUTES.has(route)) {
    return renderPenpotPage();
  }

  return (
    <OfficeLayout
      businessId={businessId}
      actorId={auth.actorId}
      controlKey={auth.controlKey}
      route={route}
      setRoute={setRoute}
      onLogout={logout}
      onSetControlKey={setControlKey}
    >
      {route === 'command' ? (
        <CommandCenter businessId={businessId} actorId={auth.actorId} />
      ) : route === 'research' ? (
        <Research businessId={businessId} actorId={auth.actorId} />
      ) : route === 'development' ? (
        <Development businessId={businessId} actorId={auth.actorId} />
      ) : route === 'media' ? (
        <Media businessId={businessId} actorId={auth.actorId} />
      ) : route === 'settings' ? (
        <Settings controlKey={auth.controlKey} />
      ) : (
        <Meeting businessId={businessId} actorId={auth.actorId} />
      )}
    </OfficeLayout>
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