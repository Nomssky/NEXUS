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
        if (url.startsWith('/api/v1/') || url === '/health' || url === '/ready' || url === '/status' || url === '/events') {
          const parsed = new URL(gatewayURL);
          let proxyPath = url.startsWith('/api/v1/') ? url.replace(/^\/api\/v1\//, '/') : url;
          const proxyReq = http.request(parsed, {
            method: req.method,
            headers: req.headers,
            hostname: parsed.hostname,
            port: parseInt(parsed.port),
            path: proxyPath,
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
          const filePath = path.join(FRONTEND_DIST, req.url === '/' ? 'index.html' : req.url);
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
    // Set auth credentials via addInitScript so they are available before any page scripts run
    await page.context().addInitScript(({ actorID, credential, businessID }) => {
      localStorage.setItem('nexus-auth', JSON.stringify({
        actorID,
        credential,
        businessID,
        controlKey: '',
      }));
    }, { actorID: nexus.actorID, credential: nexus.credential, businessID: nexus.businessID });

    // Navigate to the frontend
    await page.goto(frontendURL, { waitUntil: 'domcontentloaded' });

    // Debug: check page state after load
    console.log('DEBUG: page URL:', page.url());
    console.log('DEBUG: page title:', await page.title());
    const bodyText = await page.locator('body').textContent();
    console.log('DEBUG: body text:', bodyText.substring(0, 500));
    const btnCount = await page.locator('button').count();
    console.log('DEBUG: button count:', btnCount);

    // Wait for Command Center page to load (authentication successful via localStorage)
    await page.waitForSelector('text=Command Center', { timeout: 10000 });

    // Navigate to Research workspace
    await page.click('button:has-text("Research")', { timeout: 5000 });
    await page.waitForSelector('text=Research Query', { timeout: 5000 });

    // Submit a research objective
    await page.fill('textarea[placeholder*="research"]', 'Analyze the quarterly sales report and summarize key findings');
    await page.click('button:has-text("Research")', { timeout: 5000 });

    // Wait for execution to complete by polling status
    let executionStatus = '';
    let retries = 0;
    const maxRetries = 60;

    while (retries < maxRetries) {
      const statusText = await page.locator('[class*="status"], .status-badge').first().textContent() || '';
      if (statusText === 'completed' || statusText === 'failed' || statusText === 'cancelled') {
        executionStatus = statusText;
        break;
      }
      await new Promise((r) => setTimeout(r, 1000));
      retries++;
    }

    expect(executionStatus).toBe('completed');

    // Verify the result is displayed
    await page.waitForSelector('[class*="output"], .output-content, .result-content', { timeout: 5000 });
    const resultText = await page.locator('[class*="output"], .output-content, .result-content').first().textContent() || '';
    expect(resultText.length).toBeGreaterThan(0);

    // Return to Command Center
    await page.click('button:has-text("Command")', { timeout: 5000 });
    await page.waitForSelector('text=Command Center', { timeout: 5000 });
  });
});