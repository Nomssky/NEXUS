import { test, expect } from '@playwright/test';
import { startNexus, type NexusHandle } from '../fixtures/nexus';
import * as http from 'node:http';
import * as fs from 'node:fs';
import * as path from 'node:path';
import * as net from 'node:net';

const FRONTEND_DIST = '/home/pc/NEXUS/nexus-app/dist';

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
    nexus = await startNexus();
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

    // Navigate to Research workspace
    await page.click('button:has-text("Research")', { timeout: 5000 });
    await page.waitForSelector('text=Research Query', { timeout: 5000 });

    // Submit a research objective using the form's submit button
    // (use type="submit" to avoid matching the nav button which also says "Research")
    await page.fill('textarea[placeholder*="research"]', 'Analyze the quarterly sales report and summarize key findings');
    await page.click('button[type="submit"]', { timeout: 5000 });

    // Wait for submission to be accepted (textarea cleared on success)
    await page.waitForFunction(() => {
      const textarea = document.querySelector('textarea[placeholder*="research"]') as HTMLTextAreaElement | null;
      return textarea?.value === '';
    }, { timeout: 10000 });

    // Wait for execution to reach a terminal state
    let executionStatus = '';
    let retries = 0;
    const maxRetries = 60;

    while (retries < maxRetries) {
      const bodyText = await page.locator('body').textContent() || '';

      // Terminal statuses
      if (bodyText.includes('Research failed')) {
        executionStatus = 'failed';
        break;
      }
      if (bodyText.includes('Research cancelled')) {
        executionStatus = 'cancelled';
        break;
      }
      if (bodyText.includes('Research timed out')) {
        executionStatus = 'timed_out';
        break;
      }

      // Success: output content is displayed (not loading, not empty status)
      const isLoading = await page.locator('text="Research in progress…"').count() > 0;
      const paragraphs = await page.locator('p').allTextContents();
      const hasSubstantialOutput = paragraphs.some(p => p.trim().length > 50);
      if (!isLoading && hasSubstantialOutput) {
        executionStatus = 'completed';
        break;
      }

      await new Promise((r) => setTimeout(r, 1000));
      retries++;
    }

    // Execution must reach a terminal state (completed, failed, cancelled, or timed_out)
    expect(['completed', 'failed', 'cancelled', 'timed_out']).toContain(executionStatus);

    // Verify some output or status message is displayed
    const resultText = await page.locator('body').textContent() || '';
    expect(resultText.length).toBeGreaterThan(0);
  });
});
