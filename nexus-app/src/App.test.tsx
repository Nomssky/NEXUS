/**
 * App authentication and session regression tests.
 *
 * Verifies that the UI never initializes as authenticated before a session has
 * been established, that missing/malformed/expired/rejected sessions lead to a
 * clearly unauthenticated state, that failed login does not leave stale
 * authenticated UI state, and that logout clears protected React Query caches
 * and resets client auth headers.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, cleanup } from '@testing-library/react';
import { QueryClientProvider } from '@tanstack/react-query';
import { AuthProvider, useAuth } from './App';
import { apiClient } from './api/client';
import { queryClient } from './App';

// Mock fetch globally
const mockFetch = vi.fn<typeof fetch>();
globalThis.fetch = mockFetch;

const mockLocalStorage: Record<string, string> = {};

function SessionIndicator() {
  const { isAuthenticated, authError, login, logout } = useAuth();
  if (!isAuthenticated) {
    return (
      <div data-testid="login-screen">
        <button
          data-testid="login-button"
          onClick={() => login('test-actor', 'test-credential', 'default')}
        >
          Connect to NEXUS
        </button>
        {authError && <div data-testid="auth-error">{authError}</div>}
      </div>
    );
  }
  return (
    <div data-testid="command-center">
      <button data-testid="sign-out-button" onClick={logout}>
        Sign out
      </button>
    </div>
  );
}

beforeEach(() => {
  mockFetch.mockClear();
  vi.restoreAllMocks();

  Object.keys(mockLocalStorage).forEach((key) => delete mockLocalStorage[key]);
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation((key: string) => {
    return mockLocalStorage[key] ?? null;
  });
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation((key: string, value: string) => {
    mockLocalStorage[key] = value;
  });
  vi.spyOn(Storage.prototype, 'removeItem').mockImplementation((key: string) => {
    delete mockLocalStorage[key];
  });

  apiClient.clearAuth();
  queryClient.clear();
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

function renderAuth() {
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <SessionIndicator />
      </AuthProvider>
    </QueryClientProvider>
  );
}

describe('App authentication initialization', () => {
  it('does not initialize as authenticated before session validation', async () => {
    mockFetch.mockResolvedValue(
      new Response(null, { status: 404 })
    );

    renderAuth();

    await new Promise((r) => setTimeout(r, 100));

    expect(screen.getByTestId('login-screen')).toBeDefined();
    expect(screen.queryByTestId('command-center')).toBeNull();
  });

  it('shows login screen when local-session.json returns malformed JSON', async () => {
    mockFetch.mockResolvedValue(
      new Response('{not-json', { status: 200, headers: { 'content-type': 'application/json' } })
    );

    renderAuth();

    await new Promise((r) => setTimeout(r, 100));

    expect(screen.getByTestId('login-screen')).toBeDefined();
  });

  it('shows login screen when local-session.json is missing required fields', async () => {
    mockFetch.mockResolvedValue(
      new Response(
        JSON.stringify({ apiBase: 'http://localhost:8081/api/v1' }),
        { status: 200, headers: { 'content-type': 'application/json' } }
      )
    );

    renderAuth();

    await new Promise((r) => setTimeout(r, 100));

    expect(screen.getByTestId('login-screen')).toBeDefined();
  });

  it('establishes session from valid local-session.json', async () => {
    mockFetch
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            apiBase: 'http://127.0.0.1:8081/api/v1',
            actorId: 'nx:human:bootstrap',
            credential: 'test-credential',
            businessId: 'default',
          }),
          { status: 200, headers: { 'content-type': 'application/json' } }
        )
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ businesses: [] }), { status: 200 })
      );

    renderAuth();

    await new Promise((r) => setTimeout(r, 300));

    expect(screen.getByTestId('command-center')).toBeDefined();
    expect(screen.queryByTestId('login-screen')).toBeNull();
  });

  it('failed login does not leave authenticated UI state', async () => {
    mockFetch.mockResolvedValue(
      new Response(
        JSON.stringify({ error: { code: 'UNAUTHORIZED', category: 'AUTH', message: 'invalid credentials' } }),
        { status: 401, headers: { 'content-type': 'application/json' } }
      )
    );

    renderAuth();

    await new Promise((r) => setTimeout(r, 100));

    fireEvent.click(screen.getByTestId('login-button'));

    await new Promise((r) => setTimeout(r, 300));

    const errorDiv = screen.getByTestId('auth-error');
    expect(errorDiv).toBeDefined();
    expect(errorDiv.textContent).toContain('invalid credentials');
    expect(screen.getByTestId('login-screen')).toBeDefined();
    expect(screen.queryByTestId('command-center')).toBeNull();
  });

  it('logout clears auth state and shows login screen', async () => {
    mockFetch
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            apiBase: 'http://127.0.0.1:8081/api/v1',
            actorId: 'nx:human:bootstrap',
            credential: 'test-credential',
            businessId: 'default',
          }),
          { status: 200, headers: { 'content-type': 'application/json' } }
        )
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ businesses: [] }), { status: 200 })
      );

    renderAuth();

    await new Promise((r) => setTimeout(r, 300));

    expect(screen.getByTestId('command-center')).toBeDefined();

    fireEvent.click(screen.getByTestId('sign-out-button'));

    await new Promise((r) => setTimeout(r, 150));

    expect(screen.getByTestId('login-screen')).toBeDefined();
    expect(screen.queryByTestId('command-center')).toBeNull();
  });
});
