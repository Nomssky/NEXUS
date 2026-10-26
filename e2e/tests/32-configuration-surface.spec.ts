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

/** Fingerprints emitted by the real launcher's startup log. */
function startupFingerprints(nexus: NexusHandle): string[] {
  const logs = `${nexus.stdout()}\n${nexus.stderr()}`;
  const found: string[] = [];
  for (const line of logs.split(/\r?\n/)) {
    if (!line.includes('config_fingerprint')) continue;
    try {
      const record = JSON.parse(line);
      const value = record?.context?.config_fingerprint;
      if (typeof value === 'string') found.push(value);
    } catch {
      // Non-JSON log lines cannot be the structured startup record.
    }
  }
  return found;
}

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
    // The versioned fingerprint is a deterministic identifier, not authority.
    expect(snapshot.fingerprint_version).toBe('sha256:v2');
    expect(snapshot.fingerprint).toMatch(/^sha256:v2:[0-9a-f]{64}$/);
    expect(snapshot.provenance).toMatchObject({
      kind: 'boot_snapshot',
      immutable: true,
      precedence: 'defaults < file < environment',
    });
    expect(snapshot.runtime_mutable).toBe(false);
    expect(snapshot.restart_required).toBe(true);
    // Every documented section is attributed, including rate limiting and
    // HTTP/CORS, which previously had no source metadata.
    for (const section of [
      'nexus', 'logging', 'health', 'lifecycle', 'security',
      'storage', 'models', 'rate_limit', 'http',
    ]) {
      expect(['default', 'file', 'environment', 'mixed'], `source for ${section}`)
        .toContain(snapshot.sources[section]);
    }
    expect(snapshot.sources.models).toBe('file');
    expect(snapshot.sources.storage).toBe('mixed');
    // The startup log and the control surface must name the same boot snapshot.
    const logged = startupFingerprints(nexus);
    expect(logged.length).toBeGreaterThan(0);
    expect(logged[logged.length - 1]).toBe(snapshot.fingerprint);
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

test('fingerprint is stable across restart and reflects changed boot settings', async ({ request }) => {
  const { root, dataDir, configPath } = makeWorkspace();
  let first: NexusHandle | undefined;
  let second: NexusHandle | undefined;
  try {
    first = await startNexus({
      args: ['--config', configPath],
      dataDir,
      extraEnv: {
        NEXUS_DATA_DIR: dataDir,
        NEXUS_E2E_CONFIG_KEY: 'e2e-fingerprint-secret',
        NEXUS_CAPABILITY_WEB: '',
        NEXUS_LOG_LEVEL: 'info',
      },
    });

    const readFingerprint = async (handle: NexusHandle): Promise<string> => {
      const response = await request.get(`${handle.baseURL}/api/v1/control/config`, {
        headers: controlHeaders(handle),
      });
      const text = await response.text();
      expect(response.status(), text).toBe(200);
      const body = JSON.parse(text);
      expect(body.fingerprint_version).toBe('sha256:v2');
      expect(body.fingerprint).toMatch(/^sha256:v2:[0-9a-f]{64}$/);
      const logged = startupFingerprints(handle);
      expect(logged.length).toBeGreaterThan(0);
      expect(logged[logged.length - 1]).toBe(body.fingerprint);
      return body.fingerprint;
    };

    const before = await readFingerprint(first);
    await first.restart();
    const afterRestart = await readFingerprint(first);
    expect(afterRestart).toBe(before);

    // Change a material boot setting (logging.level) and boot a fresh process
    // against the same file/data directory. The fingerprint must change.
    await first.stop();
    first = undefined;
    second = await startNexus({
      args: ['--config', configPath],
      dataDir,
      extraEnv: {
        NEXUS_DATA_DIR: dataDir,
        NEXUS_E2E_CONFIG_KEY: 'e2e-fingerprint-secret',
        NEXUS_CAPABILITY_WEB: '',
        NEXUS_LOG_LEVEL: 'debug',
      },
    });
    const changed = await readFingerprint(second);
    expect(changed).not.toBe(before);
  } finally {
    if (first) await first.dispose();
    if (second) await second.dispose();
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test('a scripted web-search selection is reported canonically and stays non-live', async ({ request }) => {
  const { root, dataDir, configPath } = makeWorkspace();
  const secret = 'e2e-scripted-secret-must-not-appear';
  let nexus: NexusHandle | undefined;
  try {
    nexus = await startNexus({
      args: ['--config', configPath],
      dataDir,
      extraEnv: {
        NEXUS_DATA_DIR: dataDir,
        NEXUS_E2E_CONFIG_KEY: secret,
        NEXUS_CAPABILITY_WEB: 'scripted',
      },
    });

    const response = await request.get(`${nexus.baseURL}/api/v1/control/config`, {
      headers: controlHeaders(nexus),
    });
    const text = await response.text();
    expect(response.status(), text).toBe(200);
    expect(response.headers()['cache-control'] ?? '').toContain('no-store');
    expect(text).not.toContain(secret);
    const body = JSON.parse(text);

    expect(body.web_search).toMatchObject({
      provider: 'scripted-research',
      kind: 'scripted',
      mode: 'scripted',
      registered: true,
      live: false,
      keyless: true,
      credential_status: 'not_required',
      selected_by: 'NEXUS_CAPABILITY_WEB',
      live_verified: false,
      scripted_fallback: true,
    });
    expect(body.web_search.endpoint ?? '').toBe('');
    expect(body.readiness.live_verified).toBe(false);
    expect(body.readiness.gate_zero_authorized).toBe(false);
    expect(body.readiness.live_preconditions_met).toBe(false);
    expect(JSON.stringify(body.readiness.blocking)).toContain('scripted provider');
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
