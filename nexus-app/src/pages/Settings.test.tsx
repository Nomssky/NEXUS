/**
 * Settings rendering tests.
 *
 * Rendered with react-dom/server so the assertions cover the honest state
 * copy: permission, loading, failure, blocked readiness and the explicit
 * "no secrets here" posture. Hooks are mocked; no network call is made.
 */

import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { APIError } from '../api/client';
import { useConfiguration, useStorageVolumes, useValidateStoragePath } from '../api/hooks';
import type { ConfigurationSnapshot } from '../types/config';
import { Settings } from './Settings';

vi.mock('../api/hooks', () => ({
  useConfiguration: vi.fn(),
  useStorageVolumes: vi.fn(),
  useValidateStoragePath: vi.fn(),
}));

const configurationMock = vi.mocked(useConfiguration);
const volumesMock = vi.mocked(useStorageVolumes);
const validateMock = vi.mocked(useValidateStoragePath);

function snapshot(overrides: Partial<ConfigurationSnapshot> = {}): ConfigurationSnapshot {
  return {
    loaded: true,
    environment: 'test',
    nexus_id: 'nx:nexus:test',
    source: 'boot configuration (defaults < file < environment)',
    runtime_mutable: false,
    restart_required: true,
    credential_storage: 'environment_only',
    model_providers: [],
    model_routing: {
      mode: 'simulated',
      seeded: true,
      per_purpose_selection: false,
      note: 'The seeded simulator is active.',
    },
    web_search: {
      provider: 'disabled',
      mode: 'disabled',
      live: false,
      keyless: false,
      credential_status: 'not_required',
      selected_by: 'NEXUS_CAPABILITY_WEB',
      live_verified: false,
      verification_note: 'No web-search provider is selected.',
      scripted_fallback: false,
    },
    storage: {
      configured: false,
      mode: 'memory',
      persistence: 'none',
      exists: false,
      is_directory: false,
      writable: false,
      write_probe: 'access_check',
    },
    readiness: {
      model_provider_configured: false,
      model_credentials_present: false,
      web_search_configured: false,
      web_search_live: false,
      web_search_credential_ready: false,
      durable_storage_configured: false,
      durable_storage_writable: false,
      live_preconditions_met: false,
      live_verified: false,
      gate_zero_authorized: false,
      blocking: [
        'No real model provider is configured.',
        'web.search is disabled.',
        'Durable storage is not configured.',
        'No recorded Gate 0 authorization is available to this process.',
      ],
      notes: ['No provider is contacted.'],
    },
    generated_at: '2026-10-26T00:00:00Z',
    ...overrides,
  };
}

function stubHooks(options: {
  configuration?: Record<string, unknown>;
  volumes?: Record<string, unknown>;
}) {
  configurationMock.mockReturnValue(options.configuration as never);
  volumesMock.mockReturnValue(
    (options.volumes ?? {
      data: { volumes: [] },
      isLoading: false,
      isError: false,
      error: null,
    }) as never
  );
  validateMock.mockReturnValue({
    mutate: vi.fn(),
    reset: vi.fn(),
    isPending: false,
    isError: false,
    error: null,
    data: undefined,
  } as never);
}

beforeEach(() => {
  vi.clearAllMocks();
  stubHooks({
    configuration: {
      data: snapshot(),
      isLoading: false,
      isError: false,
      error: null,
      isFetching: false,
      refetch: vi.fn(),
    },
  });
});

describe('Settings states', () => {
  it('asks for the control key before requesting configuration', () => {
    const html = renderToStaticMarkup(<Settings controlKey="" />);
    expect(html).toContain('Control key required');
    expect(html).toContain('never written to browser storage');
    expect(html).not.toContain('Live readiness');
  });

  it('shows a loading state without inventing values', () => {
    stubHooks({ configuration: { data: undefined, isLoading: true, isError: false } });
    const html = renderToStaticMarkup(<Settings controlKey="test-key" />);
    expect(html).toContain('aria-busy="true"');
    expect(html).toContain('Loading configuration');
    expect(html).not.toContain('Live readiness');
  });

  it('distinguishes a rejected control key from a general failure', () => {
    const denied = new APIError('invalid or missing API key', 403);
    stubHooks({
      configuration: { data: undefined, isLoading: false, isError: true, error: denied },
    });
    const html = renderToStaticMarkup(<Settings controlKey="wrong-key" />);
    expect(html).toContain('Control key rejected');
    expect(html).toContain('HTTP 403');
    expect(html).not.toContain('wrong-key');
  });

  it('reports server-disabled control access without blaming the key', () => {
    const disabled = new APIError(
      'control API key not configured — control endpoints disabled',
      403,
      {
        code: 'CONTROL_DISABLED',
        category: 'SECURITY_REJECTION',
        message: 'control API key not configured — control endpoints disabled',
        retryable: false,
        correlation_id: 'corr-control-disabled',
        timestamp: '2026-10-26T00:00:00Z',
      }
    );
    stubHooks({
      configuration: { data: undefined, isLoading: false, isError: true, error: disabled },
    });
    const html = renderToStaticMarkup(<Settings controlKey="any-key" />);
    expect(html).toContain('Control plane disabled');
    expect(html).toContain('NEXUS_CONTROL_API_KEY');
    expect(html).not.toContain('Control key rejected');
    expect(html).toContain('ref corr-control-disabled');
  });

  it('reports blocked readiness and Gate 0 honestly', () => {
    const html = renderToStaticMarkup(<Settings controlKey="test-key" />);
    expect(html).toContain('Live readiness');
    expect(html).toContain('Simulated provider only');
    expect(html).toContain('Gate 0 authorization');
    expect(html).toContain('Not recorded in this process');
    expect(html).toContain('Blocked');
    expect(html).toContain('environment-injected');
    expect(html).toContain('No provider calls are made from this console');
  });

  it('labels a selected live adapter as unverified', () => {
    stubHooks({
      configuration: {
        data: snapshot({
          web_search: {
            provider: 'brave',
            mode: 'live',
            live: true,
            keyless: false,
            credential_env: 'BRAVE_API_KEY',
            credential_status: 'present',
            endpoint: 'https://api.search.brave.com/res/v1/web/search',
            selected_by: 'NEXUS_CAPABILITY_WEB',
            live_verified: false,
            verification_note: 'No live search is performed by this console.',
            scripted_fallback: false,
          },
        }),
        isLoading: false,
        isError: false,
        error: null,
        isFetching: false,
        refetch: vi.fn(),
      },
    });

    const html = renderToStaticMarkup(<Settings controlKey="test-key" />);
    expect(html).toContain('Live adapter selected');
    expect(html).not.toContain('>Live provider<');
  });

  it('reflects a configured provider without exposing a credential value', () => {
    stubHooks({
      configuration: {
        data: snapshot({
          model_providers: [
            {
              id: 'primary',
              name: 'Primary',
              adapter: 'openai_compatible',
              endpoint: 'https://models.example.invalid/v1',
              model_id: 'test-model',
              api_key_env: 'MODEL_API_KEY',
              credential_status: 'present',
              timeout_ms: 30000,
              capabilities: ['reasoning'],
              configured: true,
              live_verified: false,
              runtime_mutable: false,
              verification_reason: 'No live calls.',
            },
          ],
          model_routing: {
            mode: 'configured',
            seeded: false,
            per_purpose_selection: false,
            note: 'Providers are registered at boot.',
          },
          readiness: {
            ...snapshot().readiness,
            model_provider_configured: true,
            model_credentials_present: true,
          },
        }),
        isLoading: false,
        isError: false,
        error: null,
        isFetching: false,
        refetch: vi.fn(),
      },
    });

    const html = renderToStaticMarkup(<Settings controlKey="test-key" />);
    expect(html).toContain('1 OpenAI-compatible provider');
    expect(html).toContain('real providers');
    expect(html).toContain('Configured');
    expect(html).toContain('not implemented');
    expect(html).not.toContain('sk-');
  });
});
