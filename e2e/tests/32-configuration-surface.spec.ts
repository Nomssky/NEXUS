import { test, expect } from '../fixtures/test';
import { expectEnvelope, bootstrapHeaders, controlHeaders } from '../fixtures/api';
import { fixtures, startNexus, type NexusHandle } from '../fixtures/nexus';
import * as fs from 'node:fs';
import * as path from 'node:path';

/**
 * Black-box validation of the read-only configuration/storage surface.
 *
 * Starts the real compiled `cmd/nexus` binary with an explicit provider and
 * data directory, then talks to it only over HTTP. The provider endpoint is an
 * unroutable `.invalid` host and is never called: these routes must not probe
 * a provider. Credential values are synthetic and only checked for absence.
 */

function makeWorkspace(): { root: string; dataDir: string; configPath: string } {
  const tmpRoot = path.join(fixtures.E2E_DIR, '.tmp');
  fs.mkdirSync(tmpRoot, { recursive: true });
  const root = fs.mkdtempSync(path.join(tmpRoot, 'config-surface-'));
  const dataDir = path.join(root, 'data');
  fs.mkdirSync(dataDir, { recursive: true });
  const configPath = path.join(root, 'config.json');
  fs.writeFileSync(
    configPath,
    JSON.stringify({
      nexus: { id: 'nx:nexus:config-e2e', environment: 'development' },
      storage: { data_dir: dataDir },
      models: {
        providers: [
          {
            id: 'e2e-openai-compatible',
            name: 'E2E OpenAI-compatible provider',
            endpoint: 'https://models.example.invalid/v1',
            model_id: 'e2e-model',
            api_key_env: 'NEXUS_E2E_CONFIG_KEY',
          },
        ],
      },
    })
  );
  return { root, dataDir, configPath };
}

test('configuration surface is gated, redacted, bounded and non-mutating', async ({ request }) => {
  const { root, dataDir, configPath } = makeWorkspace();
  const secret = 'e2e-config-secret-must-never-appear';
  let nexus: NexusHandle | undefined;
  try {
    nexus = await startNexus({
      args: ['--config', configPath],
      dataDir,
      extraEnv: {
        NEXUS_DATA_DIR: dataDir,
        NEXUS_E2E_CONFIG_KEY: secret,
        // Make the search posture deterministic regardless of the host env.
        NEXUS_CAPABILITY_WEB: '',
      },
    });

    const configURL = `${nexus.baseURL}/api/v1/control/config`;
    const storageURL = `${nexus.baseURL}/api/v1/control/config/storage`;
    const validateURL = `${nexus.baseURL}/api/v1/control/config/storage/validate`;

    // No key: rejected with the CORE envelope and no cache permission.
    const missing = await request.get(configURL);
    const missingEnvelope = await expectEnvelope(
      missing,
      401,
      'UNAUTHORIZED',
      'AUTH',
      'configuration without a control key',
    );
    expect(JSON.stringify(missingEnvelope)).not.toContain(secret);
    expect(missing.headers()['cache-control'] ?? '').toContain('no-store');

    // Ordinary actor credentials are not control-plane credentials.
    const actorOnly = await request.get(configURL, { headers: bootstrapHeaders(nexus) });
    await expectEnvelope(
      actorOnly,
      401,
      'UNAUTHORIZED',
      'AUTH',
      'configuration with actor credentials only',
    );
    expect(actorOnly.headers()['cache-control'] ?? '').toContain('no-store');

    // Authorized snapshot: presence statuses, never values.
    const snapshotResponse = await request.get(configURL, { headers: controlHeaders(nexus) });
    const snapshotText = await snapshotResponse.text();
    expect(snapshotResponse.status(), snapshotText).toBe(200);
    expect(snapshotResponse.headers()['cache-control'] ?? '').toContain('no-store');
    expect(snapshotText).not.toContain(secret);
    const snapshot = JSON.parse(snapshotText);
    expect(snapshot.loaded).toBe(true);
    expect(snapshot.credential_storage).toBe('environment_only');
    expect(snapshot.model_providers).toHaveLength(1);
    expect(snapshot.model_providers[0]).toMatchObject({
      id: 'e2e-openai-compatible',
      endpoint: 'https://models.example.invalid/v1',
      model_id: 'e2e-model',
      api_key_env: 'NEXUS_E2E_CONFIG_KEY',
      credential_status: 'present',
      live_verified: false,
      runtime_mutable: false,
    });
    expect(snapshot.storage.configured).toBe(true);
    expect(snapshot.storage.data_dir).toBe(dataDir);
    expect(snapshot.storage.writable).toBe(true);
    expect(snapshot.readiness.live_verified).toBe(false);
    expect(snapshot.readiness.gate_zero_authorized).toBe(false);
    expect(snapshot.readiness.live_preconditions_met).toBe(false);
    expect(JSON.stringify(snapshot.readiness.blocking)).toContain('Gate 0');

    // Bounded discovery: the configured data dir is present, sensitive system
    // roots are not enumerated as candidates.
    const volumesResponse = await request.get(storageURL, { headers: controlHeaders(nexus) });
    const volumesText = await volumesResponse.text();
    expect(volumesResponse.status(), volumesText).toBe(200);
    expect(volumesResponse.headers()['cache-control'] ?? '').toContain('no-store');
    const volumes = JSON.parse(volumesText);
    expect(volumes.host_kind).toBe('local_host');
    expect(volumes.discovery).toBe('bounded');
    expect(volumes.data_dir).toBe(dataDir);
    expect(volumes.volumes.some((v: { is_data_dir: boolean; path: string }) =>
      v.is_data_dir && v.path === dataDir
    )).toBe(true);
    expect(volumes.volumes.some((v: { path: string }) =>
      v.path === '/etc' || v.path.startsWith('/etc/')
    )).toBe(false);

    // Validation is read-only: the target does not exist before or after, and
    // the response explicitly disclaims apply/migration support.
    const target = path.join(dataDir, 'new-data');
    expect(fs.existsSync(target)).toBe(false);
    const validationResponse = await request.post(validateURL, {
      headers: { ...controlHeaders(nexus), 'Content-Type': 'application/json' },
      data: { path: target },
    });
    const validationText = await validationResponse.text();
    expect(validationResponse.status(), validationText).toBe(200);
    const validation = JSON.parse(validationText);
    expect(validation).toMatchObject({
      path: target,
      valid: true,
      exists: false,
      parent_exists: true,
      is_directory: false,
      writable: true,
      applying_supported: false,
      restart_required: true,
      migration_supported: false,
    });
    expect(fs.existsSync(target)).toBe(false);

    // Hostile paths are rejected without touching the filesystem.
    for (const hostile of ['', 'relative/path', '/tmp/../etc', '/etc', '/etc/nexus']) {
      const rejected = await request.post(validateURL, {
        headers: { ...controlHeaders(nexus), 'Content-Type': 'application/json' },
        data: { path: hostile },
      });
      const envelope = await expectEnvelope(
        rejected,
        400,
        'VALIDATION',
        'VALIDATION',
        `hostile path ${JSON.stringify(hostile)}`,
      );
      expect(JSON.stringify(envelope)).not.toContain(hostile === '' ? 'impossible-marker' : hostile);
      expect(rejected.headers()['cache-control'] ?? '').toContain('no-store');
    }

    // The provider configuration is never probed by these routes.
    expect(nexus.stdout() + nexus.stderr()).not.toContain(secret);
  } finally {
    if (nexus) await nexus.dispose();
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test('a gateway without a control key fails closed', async ({ request }) => {
  const { root, dataDir, configPath } = makeWorkspace();
  let nexus: NexusHandle | undefined;
  try {
    nexus = await startNexus({
      args: ['--config', configPath],
      dataDir,
      extraEnv: {
        NEXUS_DATA_DIR: dataDir,
        NEXUS_E2E_CONFIG_KEY: 'e2e-control-disabled-secret',
        NEXUS_CONTROL_API_KEY: '',
        NEXUS_CAPABILITY_WEB: '',
      },
    });

    const response = await request.get(`${nexus.baseURL}/api/v1/control/config`, {
      headers: { 'X-API-Key': nexus.controlKey },
    });
    const envelope = await expectEnvelope(
      response,
      403,
      'CONTROL_DISABLED',
      'SECURITY_REJECTION',
      'configuration with no server control key',
    );
    expect(JSON.stringify(envelope)).not.toContain(nexus.controlKey);
    expect(response.headers()['cache-control'] ?? '').toContain('no-store');
  } finally {
    if (nexus) await nexus.dispose();
    fs.rmSync(root, { recursive: true, force: true });
  }
});
