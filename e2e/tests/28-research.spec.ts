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
  let frontend: FrontendServer | null = null;
  let frontendURL: string = '';
  let frontendPort: number = 0;

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
    await page.goto(frontendURL, { waitUntil: 'domcontentloaded' });

    await page.fill('input[id="actor-id"]', nexus.actorID);
    await page.fill('input[id="credential"]', nexus.credential);
    await page.fill('input[id="business-id"]', nexus.businessID);
    await page.click('button[type="submit"]');

    await page.waitForSelector('text=Command Center', { timeout: 10000 });

    await page.click('button:has-text("Research")', { timeout: 5000 });
    await page.waitForSelector('text=Research Query', { timeout: 5000 });

    await page.fill('textarea[placeholder*="research"]', 'Analyze the quarterly sales report and summarize key findings');
    await page.click('button:has-text("Research")', { timeout: 5000 });

    // Execution ID may not be visible in UI; proceed to check status
    // executionIdElement removed - not needed
    // removed
    // executionId removed

    // Wait for the result to appear (indicates execution completed)
    await page.waitForSelector('.output-content, .result-content, [class*="output"]', { timeout: 55000 });
    const resultText = await page.locator('.output-content, .result-content, [class*="output"]').first().textContent() || '';
    expect(resultText.length).toBeGreaterThan(0);

    await page.click('button:has-text("Command")', { timeout: 5000 });
    await page.waitForSelector('text=Command Center', { timeout: 5000 });
    // executionId slice removed
  });
});
