import { test as base, expect, beforeAll } from '@playwright/test';
import { bootstrapHeaders } from '../fixtures/api';
import { startNexus, type NexusHandle } from '../fixtures/nexus';
import * as http from 'node:http';
import * as path from 'node:path';
import * as fs from 'node:fs';
import * as os from 'node:os';

/**
 * Production inference provider boundary, black-box over HTTP.
 *
 * The gateway is booted with an explicit models.providers configuration that
 * points at a LOCAL deterministic OpenAI-compatible fixture server — never a
 * real external LLM. This proves the production provider path is wired
 * end-to-end (PROVIDER_CONTRACTS §13) while the default suite keeps using the
 * deterministic scripted provider and needs no external API key.
 */
const configDir = fs.mkdtempSync(path.join(os.tmpdir(), 'nexus-provider-config-'));

let modelFixture: http.Server;
let modelFixtureBase = '';
let lastAuthHeader = '';
let lastModel = '';
let lastBody: Record<string, unknown> | null = null;

beforeAll(async () => {
  modelFixture = http.createServer((req, res) => {
    const path = (req.url ?? '/').split('?')[0];
    lastAuthHeader = String(req.headers.authorization ?? '');
    let body = '';
    req.on('data', (c) => (body += c));
    req.on('end', () => {
      lastBody = (() => { try { return JSON.parse(body); } catch { return null; } })();
      lastModel = String((lastBody as any)?.model ?? '');
      if (path !== '/v1/chat/completions') {
        res.writeHead(404).end(JSON.stringify({ error: 'unknown path' }));
        return;
      }
      // Match the runtime envelope: planner prompt -> one-step plan;
      // decision prompt -> immediate complete (StepHasWork is true for a
      // one-step plan), so the loop reaches a deterministic terminal state.
      // Protocol v2 marks the request with the structured schema name, which
      // is how the fixture tells a plan apart from a decision.
      const schemaName = (lastBody as any)?.response_format?.json_schema?.name ?? '';
      let content: string;
      if (schemaName === 'agent_plan_v1' || /planner of a nexus agent execution/i.test(JSON.stringify(lastBody))) {
        content = '{"steps":[{"step_id":"s1","intent":"gather"}]}';
      } else {
        content = '{"type":"complete","result":"finished through the production provider path"}';
      }
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(JSON.stringify({
        choices: [{ message: { role: 'assistant', content }, finish_reason: 'stop' }],
        usage: { prompt_tokens: 32, completion_tokens: 7, total_tokens: 39 },
      }));
    });
  });
  await new Promise<void>((resolve) => modelFixture.listen(0, '127.0.0.1', resolve));
  const addr = modelFixture.address() as { port: number };
  modelFixtureBase = `http://127.0.0.1:${addr.port}`;
});

const test = base.extend<{ nexus: NexusHandle }>({
  nexus: [
    async ({}, use) => {
      const configPath = path.join(configDir, 'config.json');
      fs.writeFileSync(configPath, JSON.stringify({
        models: {
          providers: [{
            id: 'fixture-openai',
            endpoint: `${modelFixtureBase}/v1`,
            model_id: 'fixture-model',
            api_key_env: 'NEXUS_FIXTURE_MODEL_KEY',
            allow_insecure_http: true,
          }],
        },
      }));
      const handle = await startNexus({
        args: ['--config', configPath],
        extraEnv: {
          NEXUS_FIXTURE_MODEL_KEY: 'fixture-test-key',
          NEXUS_DATA_DIR: path.join(configDir, `run-${Date.now()}`),
        },
      });
      try {
        await use(handle);
      } finally {
        await handle.dispose();
      }
    },
    { scope: 'worker' },
  ],
});

test('the production provider adapter carries an objective end-to-end', async ({ request, nexus }) => {
  const res = await request.post(`${nexus.baseURL}/api/v1/intelligence/execute`, {
    headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
    data: {
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
      objective: { description: 'prove the production provider is live' },
    },
  });
  expect(res.status(), await res.text()).toBe(202);
  const { execution_id: executionID } = await res.json();
  const deadline = Date.now() + 20_000;
  let last: Record<string, any> | null = null;
  while (Date.now() < deadline) {
    const r = await request.get(`${nexus.baseURL}/api/v1/intelligence/${executionID}?business_id=${nexus.businessID}`, {
      headers: bootstrapHeaders(nexus),
    });
    if (r.status() === 200) { last = await r.json(); break; }
    expect(r.status()).toBe(202);
    await new Promise((r) => setTimeout(r, 15));
  }
  expect(last, JSON.stringify(last)).not.toBeNull();
  expect(last.status, JSON.stringify(last)).toBe('completed');
  // The fixture was the provider: auth header came from the env credential,
  // and the model ID travelled from the config, never from model output.
  expect(lastAuthHeader).toBe('Bearer fixture-test-key');
  expect(lastModel).toBe('fixture-model');
  expect(lastBody?.response_format?.type, JSON.stringify(lastBody?.response_format)).toBe('json_schema');
  expect(lastBody?.response_format?.json_schema?.name).toBe('agent_decision_v1');
  expect(lastBody?.response_format?.json_schema?.strict).toBe(true);
});
