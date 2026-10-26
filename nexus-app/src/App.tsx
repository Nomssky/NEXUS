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

function AuthProvider({ children }: { children: React.ReactNode }) {
  const [auth, setAuth] = useState<AuthState>(DEFAULT_AUTH);
  const [isAuthenticated, setIsAuthenticated] = useState(false);
  const [authError, setAuthError] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  // Check for stored auth on mount
  useEffect(() => {
    const stored = localStorage.getItem('nexus-auth');
    if (stored) {
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
        setIsAuthenticated(true);
      }
    }
    setIsLoading(false);
  }, []);

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
      // Test the credentials by calling health endpoint
      apiClient.setAuth(cleanActorId, cleanCredential);
      await apiClient.getHealth();

      const newAuth = { actorId: cleanActorId, credential: cleanCredential, businessId: cleanBusinessId, controlKey: '' };
      setAuth(newAuth);
      localStorage.setItem('nexus-auth', JSON.stringify(newAuth));
      setIsAuthenticated(true);
      return true;
    } catch (err) {
      apiClient.clearAuth();
      const message = err instanceof Error ? err.message : 'Authentication failed';
      setAuthError(message);
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
  const { isAuthenticated, auth, logout } = useAuth();
  const [route, setRoute] = useState<AppRoute>('command');

  if (!isAuthenticated) {
    return <LoginScreen />;
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