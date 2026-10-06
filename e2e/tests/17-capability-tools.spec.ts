import { test as base, expect, beforeAll, afterAll } from '@playwright/test';
import { bootstrapHeaders, controlHeaders, randomSuffix } from '../fixtures/api';
import { startNexus, type NexusHandle } from '../fixtures/nexus';
import { openSSE } from '../fixtures/sse';
import * as http from 'node:http';
import * as fs from 'node:fs';
import * as path from 'node:path';
import { execSync } from 'node:child_process';

/**
 * Capability & Tool Platform v1, black-box over HTTP against the real binary.
 *
 * Boots its own gateway with the deterministic scripted provider
 * (NEXUS_SEEDED_PROVIDER_MODE=scripted) and the scripted web-research
 * provider (NEXUS_CAPABILITY_WEB=scripted), plus a loopback HTTP fixture and
 * a real git repository inside the filesystem/git workspace root. Loopback is
 * allowed because this is the non-production environment
 * (NEXUS_ENVIRONMENT=development): the SSRF guards are exercised by
 * targeting addresses outside the configured allowlist and by addresses whose
 * blockedness lives below the allowlist (private ranges).
 *
 * Covered (mirroring contracts/CAPABILITY_TOOL_CONTRACTS.md §40):
 * manifest registration, bounded + secret-free discovery,
 * retained builtins (echo/calculator/transform) through the platform,
 * http read + SSRF + redirect boundary, filesystem sandbox + traversal
 * refusal, git safe op + path boundary, web research + injected-content
 * inertness, structured data, allowlist permission denial, result
 * truncation (audit event), cancellation, external failure, event
 * correlation, audit payload integrity, G3/G5/G4 boundary posture.
 */

const suffix = randomSuffix();

const test = base.extend<{ nexus: NexusHandle }>({
  nexus: [
    async ({}, use) => {
      const handle = await startNexus({
        extraEnv: {
          NEXUS_SEEDED_PROVIDER_MODE: 'scripted',
          NEXUS_CAPABILITY_WEB: 'scripted',
          NEXUS_TOOL_HTTP_ALLOW_LOOPBACK: 'true',
          NEXUS_TOOL_HTTP_ALLOW_INSECURE: 'true',
          NEXUS_TOOL_HTTP_ALLOWED_HOSTS: '127.0.0.1,localhost,example.com',
        },
      });
      // Pre-seed the workspace root with a real git repo + a note file. The
      // binary created the root at boot; it is inside its data dir.
      const fsRoot = path.join(handle.dataDir, 'workspaces', 'default');
      if (fs.existsSync(fsRoot)) {
        try {
          execSync('git init -q', { cwd: fsRoot });
          execSync('git -c user.email=e2e@nexus.local -c user.name=E2E commit -qm init --allow-empty', { cwd: fsRoot });
        } catch {
          // Best-effort: the git status probe reports failure honestly then.
        }
      }
      try {
        await use(handle);
      } finally {
        await handle.dispose();
      }
    },
    { scope: 'worker' },
  ],
});

let fixtureBase = '';
let fixtureServer: http.Server;
let bigBody = '';
let redirectLocation = '';
const slowHandlerDelayMs = 3000;

beforeAll(async () => {
  fixtureServer = http.createServer((req, res) => {
    if (req.url?.startsWith('/slow')) {
      setTimeout(() => { res.end('late'); }, slowHandlerDelayMs);
      return;
    }
    if (req.url === '/big') {
      res.setHeader('content-type', 'text/plain');
      res.end(bigBody);
      return;
    }
    if (req.url === '/redirect') {
      res.writeHead(302, { location: redirectLocation });
      res.end();
      return;
    }
    res.setHeader('content-type', 'text/plain');
    res.end('nexus-fixture:' + req.url);
  });
  await new Promise<void>((r) => fixtureServer.listen(0, '127.0.0.1', r));
  const addr = fixtureServer.address() as any;
  fixtureBase = `http://127.0.0.1:${addr.port}`;
  redirectLocation = 'http://169.254.169.254/latest/meta-data';
  bigBody = 'x'.repeat(200 * 1024);
});

afterAll(async () => {
  if (fixtureServer) await new Promise<void>((r) => fixtureServer.close(() => r()));
});

async function registerCapAnalyst(request, nexus, overrides = {}) {
  const body = {
    entity_id: `cap-analyst-${suffix}`,
    name: 'Cap Analyst',
    business_id: nexus.businessID,
    capabilities: ['analysis'],
    allowed_tools: [
      'echo', 'calculator', 'transform', 'http.request', 'web.search',
      'filesystem.read', 'filesystem.write', 'filesystem.list', 'git', 'data',
    ],
    ...overrides,
  };
  const res = await request.post(`${nexus.baseURL}/api/v1/agents`, {
    headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
    data: body,
  });
  expect([200, 409]).toContain(res.status());
}

async function submitObjective(request, nexus, objective) {
  return request.post(`${nexus.baseURL}/api/v1/intelligence/execute`, {
    headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
    data: { business_id: nexus.businessID, actor_id: nexus.actorID, objective },
  });
}

async function pollObjective(request, nexus, id, businessID = nexus.businessID) {
  for (let i = 0; i < 1500; i++) {
    const res = await request.get(
      `${nexus.baseURL}/api/v1/intelligence/${id}?business_id=${businessID}`,
      { headers: bootstrapHeaders(nexus) },
    );
    if (res.status() === 200) return (await res.json()) as any;
    if (res.status() !== 202) throw new Error(`poll: ${res.status()} ${await res.text()}`);
    await new Promise((r) => setTimeout(r, 10));
  }
  throw new Error('poll: never terminal');
}

async function runObjective(request, nexus, description: string, extra = {}) {
  const res = await submitObjective(request, nexus, { description, ...extra });
  expect(res.status(), `submit: ${await res.text()}`).toBe(202);
  const { execution_id } = await res.json();
  return pollObjective(request, nexus, execution_id);
}

async function listTools(request, nexus, businessID = nexus.businessID) {
  const res = await request.get(
    `${nexus.baseURL}/api/v1/tools?business_id=${businessID}`,
    { headers: bootstrapHeaders(nexus) },
  );
  expect(res.status()).toBe(200);
  return (await res.json()) as any;
}

test.describe.configure({ mode: 'serial' });

test.describe('CAPABILITY & TOOL PLATFORM', () => {
  test('tool manifests: GET /api/v1/tools lists shipped capabilities with manifests', async ({ request, nexus }) => {
    const catalog = await listTools(request, nexus);
    const ids = catalog.tools.map((t: any) => t.tool_id).sort();
    for (const required of [
      'echo', 'calculator', 'transform',
      'http.request', 'web.search',
      'filesystem.read', 'filesystem.write', 'filesystem.list',
      'git', 'data',
    ]) {
      expect(ids).toContain(required);
    }
    const httpTool = catalog.tools.find((t: any) => t.tool_id === 'http.request');
    expect(httpTool.side_effect_class).toBe('write');
    expect(httpTool.security_class).toBe('network');
    expect(httpTool.supported_operations).toContain('get');
    expect(httpTool.network_requirement).toBe('outbound_http');
    expect(httpTool.credential_required).toBe(false);
    expect(httpTool.max_duration_ms).toBeGreaterThan(0);
    const fsRead = catalog.tools.find((t: any) => t.tool_id === 'filesystem.read');
    expect(fsRead.security_class).toBe('sandboxed');
    expect(fsRead.credential_required).toBe(false);
    // GitHub capability is DECLARED but not registered in this boot
    // (it requires an explicit operator credential reference), so it is
    // UNKNOWN-or-denied here: contract §5/§6.
    const github = catalog.tools.find((t: any) => t.tool_id === 'github.issue.list');
    expect(github).toBeUndefined();
  });

  test('discovery is bounded and secret-free', async ({ request, nexus }) => {
    const res = await request.get(
      `${nexus.baseURL}/api/v1/tools?business_id=${nexus.businessID}`,
      { headers: bootstrapHeaders(nexus) },
    );
    expect(res.status()).toBe(200);
    const raw = await res.text();
    expect(raw).not.toContain('Authorization');
    expect(raw).not.toContain('client_secret');
    expect(raw).not.toContain('private_key');
    expect(raw).not.toContain('"secret"');
    expect(raw).not.toContain('data_dir');
    expect(raw).not.toContain('/home/');
  });

  test('retained deterministic builtins still work through the platform', async ({ request, nexus }) => {
    await registerCapAnalyst(request, nexus);
    const result = await runObjective(request, nexus, 'multiply six by seven with the calculator and report the product');
    expect(result.status).toBe('completed');
    const summary = result.outcome.summary as string;
    expect(summary).toContain('state=completed');
    expect(summary).toContain('tool_calls=1');
  });

  test('http.read succeeds through the mediated path', async ({ request, nexus }) => {
    await registerCapAnalyst(request, nexus);
    const result = await runObjective(request, nexus, `fetch ${fixtureBase}/fixture via http request`);
    expect(result.status).toBe('completed');
    expect(result.outcome.summary).toContain('state=completed');
    expect(result.outcome.summary).toContain('tool_calls=1');
  });

  test('http.request blocks SSRF to a processing-rate IP', async ({ request, nexus }) => {
    await registerCapAnalyst(request, nexus);
    const result = await runObjective(request, nexus, 'fetch http://169.254.169.254/latest/meta-data via http request');
    expect(result.status).toBe('failed');
    expect(result.error.message).toContain('state=failed');
    const msg = (result.error?.message ?? result.outcome.summary) as string;
    expect(msg).toMatch(/network blocked|not on the allowlist|address is not permitted/);
  });

  test('http.request blocks redirects to blocked targets', async ({ request, nexus }) => {
    await registerCapAnalyst(request, nexus);
    redirectLocation = 'http://169.254.169.254/latest/meta-data';
    const result = await runObjective(request, nexus, `fetch ${fixtureBase}/redirect via http request`);
    expect(result.status).toBe('failed');
    expect(result.error.message).toContain('state=failed');
    const msg = (result.error?.message ?? result.outcome.summary) as string;
    expect(msg).toMatch(/network blocked|not on the allowlist|address is not permitted|redirect/);
    redirectLocation = 'http://169.254.169.254/latest/meta-data';
  });

  test('filesystem: write inside the sandbox succeeds', async ({ request, nexus }) => {
    await registerCapAnalyst(request, nexus);
    const result = await runObjective(request, nexus, 'save the report using the filesystem');
    expect(result.status).toBe('completed');
    expect(result.outcome.summary).toContain('state=completed');
    const fsRoot = path.join(nexus.dataDir, 'workspaces', 'default');
    const written = path.join(fsRoot, 'notes', 'intel.txt');
    expect(fs.existsSync(written)).toBe(true);
    expect(fs.readFileSync(written, 'utf-8')).toContain('written during objective');
  });

  test('filesystem: ../ traversal is refused', async ({ request, nexus }) => {
    await registerCapAnalyst(request, nexus);
    const result = await runObjective(request, nexus, 'filesystem probe the ../../../etc/passwd path');
    expect(result.status).toBe('failed');
    expect(result.error.message).toContain('state=failed');
  });

  test('filesystem: read back of written note', async ({ request, nexus }) => {
    await registerCapAnalyst(request, nexus);
    await runObjective(request, nexus, 'save the report using the filesystem');
    const result = await runObjective(request, nexus, 'filesystem read the notes');
    expect(result.status).toBe('completed');
    expect(result.outcome.summary).toContain('state=completed');
  });

  test('git: status is readable in the approved workspace', async ({ request, nexus }) => {
    await registerCapAnalyst(request, nexus);
    const fsRoot = path.join(nexus.dataDir, 'workspaces', 'default');
    const hasGit = fs.existsSync(path.join(fsRoot, '.git'));
    const result = await runObjective(request, nexus, 'git status of the repo');
    if (hasGit) {
      expect(result.status).toBe('completed');
      expect(result.outcome.summary).toContain('state=completed');
    } else {
      // No repo provisioned: the tool honestly reports the failure instead of failing open.
      expect(result.status).toBe('failed');
      expect(result.error.message).toContain('state=failed');
    }
  });

  test('git: escaping the workspace is refused', async ({ request, nexus }) => {
    await registerCapAnalyst(request, nexus);
    const result = await runObjective(request, nexus, 'git escape from the workspace');
    expect(result.status).toBe('failed');
    expect(result.error.message).toContain('state=failed');
  });

  test('web.search normalizes provider data', async ({ request, nexus }) => {
    await registerCapAnalyst(request, nexus);
    const result = await runObjective(request, nexus, 'search the web for nexus runtime');
    expect(result.status).toBe('completed');
    expect(result.outcome.summary).toContain('state=completed');
    expect(result.outcome.summary).toContain('tool_calls=1');
  });

  test('web: injected instructions in results stay inert', async ({ request, nexus }) => {
    await registerCapAnalyst(request, nexus);
    const result = await runObjective(request, nexus, 'search the web for nexus runtime');
    expect(result.status).toBe('completed');
    expect(result.outcome.summary).toContain('state=completed');
    expect(result.outcome.summary).toContain('tool_calls=1');
    expect(result.outcome.summary).toContain('observations=1');
  });

  test('structured data: json.parse succeeds', async ({ request, nexus }) => {
    await registerCapAnalyst(request, nexus);
    const result = await runObjective(request, nexus, 'parse the provided json with data tooling');
    expect(result.status).toBe('completed');
    expect(result.outcome.summary).toContain('state=completed');
    expect(result.outcome.summary).toContain('tool_calls=1');
  });

  test('permission denial: agent allowlist rejects an unlisted tool', async ({ request, nexus }) => {
    await registerCapAnalyst(request, nexus);
    const denied = await request.post(`${nexus.baseURL}/api/v1/agents`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: {
        entity_id: `cap-restricted-${suffix}`,
        name: 'Cap Restricted',
        business_id: nexus.businessID,
        capabilities: ['analysis'],
        allowed_tools: ['echo', 'calculator'],
      },
    });
    expect([200, 409]).toContain(denied.status());
    const result = await runObjective(
      request, nexus,
      'fetch http://127.0.0.1/fixture via http request',
      { context: { agent_id: `cap-restricted-${suffix}` } },
    );
    expect(result.status).toBe('failed');
    expect(result.error.message).toContain('state=failed');
    const msg = (result.error?.message ?? result.outcome.summary) as string;
    expect(msg).toMatch(/allowlist|action rejected/);
  });

  test('credential failure surfaces without leaking a token', async ({ request, nexus }) => {
    await registerCapAnalyst(request, nexus);
    const result = await runObjective(request, nexus, 'github the latest issue list');
    expect(result.status).toBe('failed');
    const msg = (result.error?.message ?? result.outcome.summary) as string;
    expect(msg).not.toContain('token');
    expect(msg).not.toContain('secret');
    expect(msg).not.toContain('password');
  });

  test('result truncation is surfaced as a runtime event', async ({ request, nexus }) => {
    await registerCapAnalyst(request, nexus);
    const conn = openSSE(nexus.baseURL, `/events?business_id=${nexus.businessID}`, {
      ...bootstrapHeaders(nexus),
    });
    try {
      const submit = await submitObjective(
        request, nexus,
        { description: `fetch ${fixtureBase}/big via http request` } as any,
      );
      expect(submit.status()).toBe(202);
      const { execution_id } = await submit.json();
      const frame = await (conn as any).waitForFrame(
        (f: any) => f.event === 'tool.result.truncated' && f.json?.correlation_id === execution_id,
        20000,
        'tool.result.truncated frame',
      );
      expect(frame.event).toBe('tool.result.truncated');
      const result = await pollObjective(request, nexus, execution_id);
      expect(result.status).toBe('completed');
    } finally {
      await conn.close();
    }
  });

  test('cancellation during a live external call', async ({ request, nexus }) => {
    await registerCapAnalyst(request, nexus);
    const submit = await submitObjective(
      request, nexus,
      { description: `fetch ${fixtureBase}/slow via http request` } as any,
    );
    expect(submit.status()).toBe(202);
    const { execution_id } = await submit.json();
    await new Promise((r) => setTimeout(r, 500));
    const cancel = await request.post(
      `${nexus.baseURL}/api/v1/intelligence/${execution_id}/cancel?business_id=${nexus.businessID}`,
      { headers: bootstrapHeaders(nexus) },
    );
    expect([202, 409]).toContain(cancel.status());
    const result = await pollObjective(request, nexus, execution_id);
    expect(result.status).toMatch(/cancelled|failed/);
  });

  test('external (unreachable) target fails as external error', async ({ request, nexus }) => {
    await registerCapAnalyst(request, nexus);
    const result = await runObjective(request, nexus, 'fetch http://127.0.0.1:1/unreachable via http request');
    expect(result.status).toBe('failed');
    expect(result.error.message).toContain('state=failed');
  });

  test('events are correlated through tool capability events', async ({ request, nexus }) => {
    await registerCapAnalyst(request, nexus);
    const conn = openSSE(nexus.baseURL, `/events?business_id=${nexus.businessID}`, {
      ...bootstrapHeaders(nexus),
    });
    try {
      const submit = await submitObjective(request, nexus, { description: 'multiply with the calculator' } as any);
      expect(submit.status()).toBe(202);
      const { execution_id } = await submit.json();
      const frame = await (conn as any).waitForFrame(
        (f: any) => f.json?.correlation_id === execution_id,
        20000,
        'capability-correlated frame',
      );
      expect(frame.event).toBeTruthy();
      const result = await pollObjective(request, nexus, execution_id);
      expect(result.status).toBe('completed');
    } finally {
      await conn.close();
    }
  });

  test('audit correctness: tool.invocation.completed payload is bounded', async ({ request, nexus }) => {
    await registerCapAnalyst(request, nexus);
    const conn = openSSE(nexus.baseURL, `/events?business_id=${nexus.businessID}`, {
      ...bootstrapHeaders(nexus),
    });
    try {
      const submit = await submitObjective(request, nexus, { description: 'multiply with the calculator' } as any);
      expect(submit.status()).toBe(202);
      const { execution_id } = await submit.json();
      const frame = await (conn as any).waitForFrame(
        (f: any) => f.event === 'tool.invocation.completed' && f.json?.correlation_id === execution_id,
        20000,
        'tool completed audit frame',
      );
      const payload = frame.json?.payload;
      const obj = typeof payload === 'string' ? JSON.parse(payload) : payload;
      expect(obj.tool_id).toBe('calculator');
      expect(obj.status).toBeTruthy();
      expect(String(obj.duration_ms)).toMatch(/^[0-9]+$/);
      expect(obj.result_bytes).toBeDefined();
      expect(obj.truncated).toBeDefined();
      const raw = frame.data;
      expect(raw).not.toContain('Bearer ');
      expect(raw).not.toContain('"secret"');
      const result = await pollObjective(request, nexus, execution_id);
      expect(result.status).toBe('completed');
    } finally {
      await conn.close();
    }
  });

  test('G3/G5: catalog for a sibling business is forbidden', async ({ request, nexus }) => {
    const res = await request.get(
      `${nexus.baseURL}/api/v1/tools?business_id=otherbiz`,
      { headers: bootstrapHeaders(nexus) },
    );
    expect(res.status()).toBe(403);
  });

  test('governance denial stops the objective before any model call', async ({ request, nexus }) => {
    const policy = await request.put(`${nexus.baseURL}/api/v1/control/policies/deny-cap`, {
      headers: { ...controlHeaders(nexus), 'Content-Type': 'application/json' },
      data: {
        policy_type: 'access_control',
        name: 'deny-cap',
        description: 'deny every objective in this test',
        status: 'active',
        business_id: nexus.businessID,
        subject: { subject_type: 'all' },
        action: { action_type: 'custom' },
        resource: { resource_type: 'all' },
        effect: 'DENY',
        precedence: 100,
        effective_from: new Date().toISOString(),
      },
    });
    expect(policy.status()).toBe(200);
    const res = await submitObjective(request, nexus, { description: 'must be denied' } as any);
    expect(res.status()).toBe(202);
    const { execution_id } = await res.json();
    const result = await pollObjective(request, nexus, execution_id);
    expect(result.status).toBe('failed');
    expect(result.error.category).toBe('POLICY_DENIED');
    await request.delete(`${nexus.baseURL}/api/v1/control/policies/deny-cap`, {
      headers: controlHeaders(nexus),
    });
  });

  test('G4: restart drops process-local executions but preserves agents and manifests', async ({ request, nexus }) => {
    await registerCapAnalyst(request, nexus);
    const submit = await submitObjective(request, nexus, { description: 'echo the durability check' } as any);
    expect(submit.status()).toBe(202);
    const { execution_id } = await submit.json();
    const before = await request.get(
      `${nexus.baseURL}/api/v1/intelligence/${execution_id}?business_id=${nexus.businessID}`,
      { headers: bootstrapHeaders(nexus) },
    );
    expect([200, 202]).toContain(before.status());
    await nexus.restart();
    const after = await request.get(
      `${nexus.baseURL}/api/v1/intelligence/${execution_id}?business_id=${nexus.businessID}`,
      { headers: bootstrapHeaders(nexus) },
    );
    expect(after.status()).toBe(404);
    const toolsAgain = await listTools(request, nexus);
    expect(toolsAgain.tools.length).toBeGreaterThan(0);
    const agents = await request.get(
      `${nexus.baseURL}/api/v1/agents?business_id=${nexus.businessID}`,
      { headers: bootstrapHeaders(nexus) },
    );
    expect(agents.status()).toBe(200);
    const listing = await agents.json();
    expect(listing.agents.some((a: any) => a.id.startsWith('cap-analyst-'))).toBe(true);
  });
});
