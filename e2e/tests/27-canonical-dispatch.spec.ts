import { test as base, expect } from '@playwright/test';
import type { APIRequestContext } from '@playwright/test';
import {
  bootstrapHeaders,
  controlHeaders,
  randomSuffix,
} from '../fixtures/api';
import { startNexus, type NexusHandle } from '../fixtures/nexus';
import { sweepE2ePolicies } from '../fixtures/policy';

/**
 * Canonical Tool Dispatch (v8): the degraded (no capability platform) boot
 * path over a real process. A malformed HTTP policy makes `capabilityPlatform`
 * fail, so the launcher continues without a platform and only the built-in
 * tools remain executable through the pre-platform path. That path now
 * emits honest dispatch evidence without fabricating physical attempts.
 */

const suffix = randomSuffix();

const test = base.extend<{ nexus: NexusHandle }>({
  nexus: [
    async ({}, use) => {
      const handle = await startNexus({
        extraEnv: {
          NEXUS_SEEDED_PROVIDER_MODE: 'scripted',
          // Non-numeric port → LoadHTTPPolicyFromEnv fails → capabilityPlatform
          // returns an error → launcher logs and continues without a platform.
          NEXUS_TOOL_HTTP_ALLOWED_PORTS: 'not-a-port',
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

test.afterEach(async ({ request, nexus }) => {
  await sweepE2ePolicies(request, nexus);
});

test.describe.configure({ mode: 'serial' });

interface TraceEntry {
  seq: number;
  type: string;
  source: string;
  data?: Record<string, string>;
}

interface TraceView {
  execution_id: string;
  status: string;
  durability: string;
  correlation_id: string;
  capture: { available: boolean; entries: number; terminal_seq?: number };
  entries: TraceEntry[];
}

const agentID = `canon-agent-${suffix}`;

async function ensureAgent(request: APIRequestContext, nexus: NexusHandle): Promise<void> {
  const res = await request.post(`${nexus.baseURL}/api/v1/agents`, {
    headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
    data: {
      entity_id: agentID,
      name: 'Canonical Dispatch Analyst',
      business_id: nexus.businessID,
      capabilities: ['analysis'],
      allowed_tools: ['calculator'],
    },
  });
  expect([200, 409], `register agent: ${await res.text()}`).toContain(res.status());
}

async function submitObjective(
  request: APIRequestContext,
  nexus: NexusHandle,
  description: string,
): Promise<string> {
  const res = await request.post(`${nexus.baseURL}/api/v1/intelligence/execute`, {
    headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
    data: { business_id: nexus.businessID, actor_id: nexus.actorID, objective: { description } },
  });
  expect(res.status(), `submit: ${await res.text()}`).toBe(202);
  const body = await res.json();
  return body.execution_id as string;
}

async function pollObjective(
  request: APIRequestContext,
  nexus: NexusHandle,
  id: string,
): Promise<any> {
  for (let i = 0; i < 2400; i++) {
    const res = await request.get(
      `${nexus.baseURL}/api/v1/intelligence/${encodeURIComponent(id)}?business_id=${nexus.businessID}`,
      { headers: bootstrapHeaders(nexus) },
    );
    if (res.status() === 200) return await res.json();
    if (res.status() !== 202) throw new Error(`poll: ${res.status()} ${await res.text()}`);
    await new Promise((r) => setTimeout(r, 10));
  }
  throw new Error(`poll: execution ${id} never terminal`);
}

test.describe('CANONICAL TOOL DISPATCH', () => {
  test('the pre-platform path emits honest dispatch evidence over a real process', async ({ request, nexus }) => {
    await ensureAgent(request, nexus);

    const executionID = await submitObjective(request, nexus, 'multiply six by seven with the calculator');
    const result = await pollObjective(request, nexus, executionID);
    expect(result.status, `objective: ${JSON.stringify(result.error)}`).toBe('completed');

    const res = await request.get(
      `${nexus.baseURL}/api/v1/intelligence/${encodeURIComponent(executionID)}/trace?business_id=${nexus.businessID}`,
      { headers: bootstrapHeaders(nexus) },
    );
    expect(res.status(), `trace: ${await res.text()}`).toBe(200);
    const view = (await res.json()) as TraceView;
    expect(view.execution_id).toBe(executionID);
    expect(view.status).toBe('completed');
    expect(view.durability).toBe('process_local');
    expect(view.capture.available).toBe(true);

    const indexOf = (type: string): number =>
      view.entries.findIndex((e) => e.type === type);
    const types = view.entries.map((e) => e.type);

    const proposal = indexOf('action.proposed');
    const decision = indexOf('governance.decided');
    const started = view.entries.findIndex((e) => e.type === 'tool.invocation.started');
    const completed = view.entries.findIndex((e) => e.type === 'tool.invocation.completed');
    const terminal = view.entries.findIndex((e) => e.type === 'objective.completed' && e.data?.state === 'completed');

    expect(proposal, `proposal missing: ${types}`).toBeGreaterThanOrEqual(0);
    expect(decision, `decision missing: ${types}`).toBeGreaterThan(proposal);
    expect(started, `started missing: ${types}`).toBeGreaterThan(decision);
    expect(completed, `completed missing: ${types}`).toBeGreaterThan(started);
    expect(terminal, `terminal missing: ${types}`).toBeGreaterThan(completed);

    const startedEntry = view.entries[started];
    expect(startedEntry.source).toBe('agentexec');
    expect(startedEntry.data?.side_effect_class).toBe('read');

    // The pre-platform path does not fabricate physical attempts or retries:
    // the pure local invocation has no external dispatch.
    const attemptEntries = view.entries.filter((e) => e.type.startsWith('tool.attempt.'));
    expect(attemptEntries, 'no physical attempt events on pre-platform path').toHaveLength(0);
    const capabilityEntries = view.entries.filter(
      (e) => e.type === 'tool.invocation.started' && e.source === 'capability',
    );
    expect(capabilityEntries, 'no capability-sourced invocation evidence on degraded boot').toHaveLength(0);
  });
});
