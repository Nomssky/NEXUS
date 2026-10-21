import { test, expect } from '@playwright/test';
import { startNexus, type NexusHandle } from '../fixtures/nexus';
import { bootstrapHeaders } from '../fixtures/api';
import * as http from 'node:http';
import * as fs from 'node:fs';
import * as path from 'node:path';
import * as net from 'node:net';

const FRONTEND_DIST = '/home/pc/NEXUS/nexus-app/dist';
const RESEARCH_AGENT_ID = 'research-e2e-analyst';
const RESEARCH_OBJECTIVE =
  'Search the web for NEXUS runtime capabilities and summarize the sources';

class FrontendServer {
  private server: http.Server | null = null;
  private port = 0;

  async start(gatewayURL: string): Promise<number> {
    return new Promise((resolve, reject) => {
      this.server = http.createServer((req, res) => {
        const url = req.url!;
        // Proxy API and control-plane paths to the gateway, forwarding the
        // full path (including /api/v1 prefix) since the gateway routes on
        // the full path.
        if (url.startsWith('/api/v1/') || url === '/health' || url === '/ready' || url === '/status' || url === '/events') {
          const parsed = new URL(gatewayURL);
          const proxyReq = http.request(parsed, {
            method: req.method,
            headers: req.headers,
            hostname: parsed.hostname,
            port: parseInt(parsed.port),
            path: url,
          });
          proxyReq.on('response', (proxyRes) => {
            res.writeHead(proxyRes.statusCode, proxyRes.headers);
            proxyRes.pipe(res);
          });
          proxyReq.on('error', (err) => {
            res.writeHead(502);
            res.end('Proxy error: ' + err.message);
          });
          if (req.method !== 'GET' && req.method !== 'HEAD') {
            req.pipe(proxyReq);
          } else {
            proxyReq.end();
          }
        } else {
          const filePath = path.join(FRONTEND_DIST, url === '/' ? 'index.html' : url);
          fs.readFile(filePath, (err, data) => {
            if (err) { res.writeHead(404); res.end('Not found'); return; }
            const ext = path.extname(filePath);
            const contentType = ext === '.html' ? 'text/html' : ext === '.css' ? 'text/css' : ext === '.js' ? 'application/javascript' : ext === '.json' ? 'application/json' : ext === '.png' ? 'image/png' : ext === '.svg' ? 'image/svg+xml' : 'application/octet-stream';
            res.writeHead(200, { 'Content-Type': contentType });
            res.end(data);
          });
        }
      });
      this.server.listen(0, '127.0.0.1', () => {
        const addr = this.server.address() as net.AddressInfo;
        resolve(addr.port);
      });
    });
  }

  async stop() {
    return new Promise<void>((resolve) => {
      if (this.server) { this.server.close(() => resolve()); this.server = null; } else { resolve(); }
    });
  }
}

test.describe('Research Workspace E2E', () => {
  let nexus: NexusHandle;
  let frontend: FrontendServer;
  let frontendURL: string;
  let frontendPort: number;

  test.beforeAll(async () => {
    // Use the shipped deterministic provider and web capability so the E2E
    // exercises a real completed agent-intelligence execution through the
    // compiled Go binary. No API responses are mocked.
    nexus = await startNexus({
      extraEnv: {
        NEXUS_SEEDED_PROVIDER_MODE: 'scripted',
        NEXUS_CAPABILITY_WEB: 'scripted',
      },
    });

    const registered = await fetch(`${nexus.baseURL}/api/v1/agents`, {
      method: 'POST',
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      body: JSON.stringify({
        entity_id: RESEARCH_AGENT_ID,
        name: 'Research E2E Analyst',
        business_id: nexus.businessID,
        capabilities: ['analysis'],
        allowed_tools: ['web.search'],
      }),
    });
    if (!registered.ok && registered.status !== 409) {
      throw new Error(
        `failed to register research agent: HTTP ${registered.status} ${await registered.text()}`
      );
    }

    frontend = new FrontendServer();
    frontendPort = await frontend.start(nexus.baseURL);
    frontendURL = `http://127.0.0.1:${frontendPort}`;
  });

  test.afterAll(async () => {
    await frontend?.stop();
    await nexus.dispose();
  });

  test('research workspace: submit objective, observe execution, inspect result', async ({ page }) => {
    // Set auth credentials via localStorage BEFORE page loads so React's
    // AuthProvider reads them on mount and skips the login screen.
    await page.context().addInitScript(({ actorID, credential, businessID }) => {
      localStorage.setItem('nexus-auth', JSON.stringify({
        actorID,
        actorId: actorID,
        credential,
        businessID,
        businessId: businessID,
        controlKey: '',
      }));
    }, { actorID: nexus.actorID, credential: nexus.credential, businessID: nexus.businessID });

    // Navigate to the frontend (localStorage already set → authenticated on mount)
    await page.goto(frontendURL, { waitUntil: 'domcontentloaded' });

    // Wait for Command Center page to load (authentication successful)
    await page.waitForSelector('text=Command Input', { timeout: 15000 });

    // Navigate to Research workspace using the app's main navigation. The
    // VirtualHQ zone rail also exposes a "Research" tab, so scope the click to
    // the top-level tablist.
    const mainNav = page.getByRole('tablist', { name: 'Main navigation' });
    await mainNav.getByRole('tab', { name: 'Research', exact: true }).click();
    await page.waitForSelector('text=Research Query', { timeout: 5000 });

    // Submit a research objective. The scripted provider deterministically
    // calls web.search once, then completes.
    await page.fill('textarea[placeholder*="research"]', RESEARCH_OBJECTIVE);
    await page.click('button[type="submit"]', { timeout: 5000 });

    // Wait for submission to be accepted (textarea cleared on success)
    await page.waitForFunction(() => {
      const textarea = document.querySelector('textarea[placeholder*="research"]') as HTMLTextAreaElement | null;
      return textarea?.value === '';
    }, { timeout: 10000 });

    const output = page.locator('section[aria-labelledby="research-output-heading"]');
    const evidence = page.locator('section[aria-labelledby="research-evidence-heading"]');

    // The real execution must complete and render the parsed result — not the
    // raw summary string and not a fabricated evidence list.
    await expect(
      output.getByRole('heading', { name: 'Research result', exact: true })
    ).toBeVisible({ timeout: 20000 });
    await expect(output.getByText('objective pursued deterministically', { exact: true })).toBeVisible();
    await expect(output.getByText(RESEARCH_OBJECTIVE, { exact: true })).toBeVisible();
    await expect(
      output.locator('dt', { hasText: 'Tool calls' }).locator('..').locator('dd')
    ).toHaveText('1');

    // The execution ran web.search but NEXUS does not expose the tool's
    // source payload after execution. The UI must say so instead of inventing
    // citations or claiming the search returned nothing.
    await expect(evidence.getByText('No source records available')).toBeVisible();
    await expect(evidence.getByText('web.search', { exact: true })).toBeVisible();
    await expect(evidence.getByText('not citations')).toBeVisible();
    await expect(evidence.locator('a')).toHaveCount(0);

    // The trace overlay must not open automatically. Navigating away and back
    // through the real app navigation would be blocked by the modal if it did.
    await expect(page.getByRole('heading', { name: 'Execution Trace' })).toHaveCount(0);
    await mainNav.getByRole('tab', { name: 'Command', exact: true }).click();
    await page.waitForSelector('text=Command Input', { timeout: 10000 });
    await mainNav.getByRole('tab', { name: 'Research', exact: true }).click();
    await page.waitForSelector('text=Research Query', { timeout: 5000 });
    await expect(output.getByText('objective pursued deterministically', { exact: true })).toBeVisible({
      timeout: 15000,
    });

    // The trace is available only on explicit request and can be closed.
    await evidence.getByRole('button', { name: 'View Trace' }).click();
    await expect(page.getByRole('heading', { name: /Execution Trace/ })).toBeVisible({
      timeout: 10000,
    });
    await page.getByRole('button', { name: 'Close trace' }).click();
    await expect(page.getByRole('heading', { name: /Execution Trace/ })).toHaveCount(0);

    // Reload: the completed result must be recovered from the authoritative
    // durable recovery read and remain readable.
    await page.reload({ waitUntil: 'domcontentloaded' });
    await page.waitForSelector('text=Command Input', { timeout: 15000 });
    await page.getByRole('tablist', { name: 'Main navigation' })
      .getByRole('tab', { name: 'Research', exact: true })
      .click();
    await page.waitForSelector('text=Research Query', { timeout: 5000 });
    await expect(output.getByText('objective pursued deterministically', { exact: true })).toBeVisible({
      timeout: 20000,
    });
    await expect(evidence.getByText('No source records available')).toBeVisible();
    await expect(output.getByText('Research failed')).toHaveCount(0);
  });
});
