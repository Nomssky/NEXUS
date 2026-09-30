import { defineConfig } from '@playwright/test';

/**
 * Black-box HTTP E2E. There is no browser: every test drives the real NEXUS
 * gateway over HTTP (Playwright's API request client, plus node:http for the
 * SSE stream). The gateway under test is a real compiled `cmd/nexus` process
 * started by fixtures/nexus.ts — no internal Go packages are imported.
 *
 * Serial by design: one NEXUS instance is shared per worker so pause/resume
 * and restart flows cannot interleave with unrelated tests.
 */
export default defineConfig({
  testDir: './tests',
  timeout: 60_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: !!process.env.CI,
  reporter: [['list'], ['html', { open: 'never', outputFolder: 'playwright-report' }]],
  outputDir: 'test-results',
});
