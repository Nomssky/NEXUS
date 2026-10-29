import { test, expect } from '../fixtures/test';
import type { APIRequestContext } from '@playwright/test';
import {
  actorHeaders,
  bootstrapHeaders,
  randomSuffix,
} from '../fixtures/api';
import { startNexus, type NexusHandle, fixtures } from '../fixtures/nexus';
import * as fs from 'node:fs';
import * as path from 'node:path';
import { spawn, ChildProcess } from 'node:child_process';
import * as net from 'node:net';
import * as os from 'node:os';

const DIST_DIR = path.resolve(fixtures.REPO_ROOT, 'nexus-app', 'dist');

async function reserveSinglePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.unref();
    srv.once('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const addr = srv.address() as net.AddressInfo;
      srv.close(() => resolve(addr.port));
    });
  });
}

async function serveFrontendWithSession(
  port: number,
  sessionConfig: { apiBase: string; actorId: string; credential: string; businessId: string },
): Promise<{ dir: string; server: ChildProcess }> {
  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'nexus-frontend-test-'));
  fs.cpSync(DIST_DIR, tmpDir, { recursive: true });
  const sessionPath = path.join(tmpDir, 'local-session.json');
  fs.writeFileSync(sessionPath, JSON.stringify(sessionConfig), { encoding: 'utf-8' });

  const server = spawn('python3', ['-m', 'http.server', String(port), '--bind', '127.0.0.1'], {
    cwd: tmpDir,
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  server.stdout?.on('data', () => { /* ignore */ });
  server.stderr?.on('data', () => { /* ignore */ });

  for (let i = 0; i < 100; i++) {
    try {
      const res = await fetch(`http://127.0.0.1:${port}/`);
      if (res.ok) {
        res.body?.cancel();
        return { dir: tmpDir, server };
      }
    } catch {
      /* not ready yet */
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`frontend server did not become ready on port ${port}`);
}

async function startResearchNexus(frontendPort: number): Promise<NexusHandle> {
  return startNexus({
    extraEnv: {
      NEXUS_SEEDED_PROVIDER_MODE: 'scripted',
      NEXUS_CAPABILITY_WEB: 'scripted',
      NEXUS_HTTP_CORS_ALLOWED_ORIGINS: `http://127.0.0.1:${frontendPort}`,
    },
  });
}

async function pollExecutionCompleted(
  request: APIRequestContext,
  baseURL: string,
  businessId: string,
  headers: Record<string, string>,
  timeoutMs = 60_000,
) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const res = await request.get(
      `${baseURL}/api/v1/recovery/executions?business_id=${encodeURIComponent(businessId)}`,
      { headers },
    );
    expect(res.status()).toBe(200);
    const body = await res.json();
    const exec = body.executions?.[0];
    if (exec && exec.state === 'completed') return exec;
    await new Promise((r) => setTimeout(r, 1000));
  }
  throw new Error('execution never reached completed state');
}

test.describe('TRENDSENTRY BROWSER RESEARCH WORKFLOW (isolated instance)', () => {
  test('user completes a research objective and sees synthesis + evidence in the UI', async ({
    page,
    request,
  }) => {
    const frontendPort = await reserveSinglePort();
    const frontendURL = `http://127.0.0.1:${frontendPort}/`;
    const nexus = await startResearchNexus(frontendPort);
    let frontend: { dir: string; server: ChildProcess } | null = null;
    try {
      const actorID = nexus.actorID;
      const businessID = nexus.businessID;
      const headers = bootstrapHeaders(nexus);
      const baseURL = nexus.baseURL;

      // Install route BEFORE page.goto so browser API calls go to isolated gateway,
      // with auth headers added so that the first API call (e.g. listBusinesses
      // during session establishment) carries valid credentials.
      // Uses a RegExp matcher so that ALL localhost:8081 API calls — including
      // /events (EventStream) and /control/** — are redirected, not just
      // /api/v1/** paths.
      await page.route(/.*localhost:8081.*/, (route) => {
        const url = route.request().url();
        return route.continue({
          headers: {
            ...route.request().headers(),
            'X-Actor-ID': actorID,
            'X-Actor-Credential': nexus.credential,
          },
          url: url.replace('http://localhost:8081', nexus.baseURL),
        });
      });

      // Also set auth headers at the browser level as a belt-and-suspenders
      // measure so that any request not caught by the route still carries auth.
      await page.setExtraHTTPHeaders({
        'X-Actor-ID': actorID,
        'X-Actor-Credential': nexus.credential,
      });

      // Create division and agent via page.request (direct to isolated gateway)
      const divisionID = `div-${randomSuffix()}`;
      await page.request.post(`${baseURL}/api/v1/divisions`, {
        headers: { ...headers, 'Content-Type': 'application/json' },
        data: {
          entity_id: divisionID,
          business_id: businessID,
          name: `E2E Research Division ${divisionID}`,
          owner_identity_id: actorID,
          parent_business_id: businessID,
        },
      });

      const agentID = `agent-${randomSuffix()}`;
      await page.request.post(`${baseURL}/api/v1/agents`, {
        headers: { ...headers, 'Content-Type': 'application/json' },
        data: {
          entity_id: agentID,
          name: `E2E Research Agent ${agentID}`,
          business_id: businessID,
          division_id: divisionID,
          capabilities: ['analysis'],
          allowed_tools: ['web.search'],
        },
      });

      // Submit research objective via API (guaranteed completion, like test 30)
      const sub = await page.request.post(`${baseURL}/api/v1/intelligence/execute`, {
        headers: { ...headers, 'Content-Type': 'application/json' },
        data: {
          business_id: businessID,
          actor_id: actorID,
          division_id: divisionID,
          objective: {
            description: 'research thoroughly: search the web for nexus capabilities and summarize the findings',
            context: { agent_id: agentID },
          },
        },
      });
      expect(sub.status(), 'submit research objective').toBe(202);
      const subBody = JSON.parse(await sub.text());
      const executionID = subBody.execution_id;

      // Poll for completion
      const ourExec = await pollExecutionCompleted(request, baseURL, businessID, headers);
      expect(ourExec.execution_id).toBe(executionID);
      expect(ourExec.business_id).toBe(businessID);

      // Fetch execution detail for synthesis + evidence
      const detailRes = await request.get(
        `${baseURL}/api/v1/recovery/executions/${encodeURIComponent(executionID)}?business_id=${encodeURIComponent(businessID)}`,
        { headers },
      );
      expect(detailRes.status()).toBe(200);
      const detailBody = await detailRes.json();
      expect(detailBody.research_evidence).toBeTruthy();

      const synthesis = detailBody.research_synthesis;
      expect(synthesis).toBeTruthy();
      expect(synthesis.status).toBe('complete');
      const expectedSummary = synthesis.summary;
      expect(expectedSummary).toBeTruthy();

      const evidence = detailBody.research_evidence;
      const sources = evidence?.sources || [];
      expect(sources.length).toBeGreaterThan(0);

      // Serve frontend with session on the same port nexus CORS allows
      frontend = await serveFrontendWithSession(frontendPort, {
        apiBase: `${baseURL}/api/v1`,
        actorId: actorID,
        credential: nexus.credential,
        businessId: businessID,
      });

      await page.goto(frontendURL, { waitUntil: 'domcontentloaded' });

      await expect(page.locator('h1')).toContainText('NEXUS', { timeout: 10000 });
      await expect(page.locator('text=Command Input')).toBeVisible({ timeout: 10000 });

      // Wait for auth (local-session.json) to be processed and useAgents to succeed
      await page.waitForTimeout(8000);

      // --- Verify agent selection ---
      const agentSelect = page.locator('#agent-select');
      await expect(agentSelect).toBeVisible();
      const options = agentSelect.locator('option');
      await expect(options).toHaveCount(2);
      await expect(options.nth(1)).toHaveText('E2E Research Agent ' + agentID);

      // Select the registered agent
      await agentSelect.selectOption(agentID);

      // --- Navigate to the Research page to read the result ---
      const analyticsTab = page.getByRole('tab', { name: 'Analytics' });
      await expect(analyticsTab).toBeVisible();
      await analyticsTab.click();

      await expect(page.locator('text=Research Output')).toBeVisible({ timeout: 15000 });

      // --- Assert actual synthesis is visible in the browser UI ---
      await expect(page.getByText(expectedSummary)).toBeVisible({ timeout: 10000 });

      // --- Assert actual evidence/source references are visible in the UI ---
      await expect(page.getByText('Research Sources')).toBeVisible({ timeout: 10000 });

      for (const source of sources) {
        await expect(page.getByRole('heading', { name: source.title })).toBeVisible();
        await expect(page.locator(`article[data-source-id="${source.id}"]`)).toBeVisible();
      }

      await nexus.dispose();
      try { frontend.server.kill(); } catch { /* best effort */ }
      try { fs.rmSync(frontend.dir, { recursive: true, force: true }); } catch { /* leave intact */ }
    } catch (e) {
      await nexus.dispose();
      throw e;
    }
  });

  test('shows login screen when no valid local-session.json exists', async ({
    page,
  }) => {
    const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'nexus-frontend-nosession-'));
    fs.cpSync(DIST_DIR, tmpDir, { recursive: true });

    const port = await reserveSinglePort();
    const server = spawn('python3', ['-m', 'http.server', String(port), '--bind', '127.0.0.1'], {
      cwd: tmpDir,
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    server.stdout?.on('data', () => { /* ignore */ });
    server.stderr?.on('data', () => { /* ignore */ });

    try {
      for (let i = 0; i < 100; i++) {
        try {
          const res = await fetch(`http://127.0.0.1:${port}/`);
          if (res.ok) {
            res.body?.cancel();
            break;
          }
        } catch {
          /* not ready yet */
        }
        await new Promise((r) => setTimeout(r, 100));
      }

      const frontendURL = `http://127.0.0.1:${port}/`;

      // Block all API calls so the frontend cannot connect to any backend.
      // Without local-session.json, the frontend falls back to
      // http://localhost:8081/api/v1 (DEFAULT_BASE_URL); blocking those
      // requests prevents any authenticated access.
      await page.route('**/api/v1/**', (route) => route.abort());

      await page.goto(frontendURL, { waitUntil: 'domcontentloaded' });

      await expect(page.locator('h1')).toContainText('NEXUS', { timeout: 10000 });

      // No valid session -> Login screen is shown (not Command Center),
      // no agents available, Command Input not usable.
      await expect(page.getByRole('button', { name: /Connect to NEXUS/i })).toBeVisible({
        timeout: 10000,
      });
      await expect(page.locator('#agent-select')).not.toBeVisible();
    } finally {
      try {
        server.kill();
      } catch {
        /* best effort */
      }
      try {
        fs.rmSync(tmpDir, { recursive: true, force: true });
      } catch {
        /* leave intact if removal cannot be proven safe */
      }
    }
  });
});