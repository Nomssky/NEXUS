import { test as base, expect, beforeAll, afterAll } from '@playwright/test';
import { bootstrapHeaders, controlHeaders, randomSuffix } from '../fixtures/api';
import { startNexus, type NexusHandle } from '../fixtures/nexus';
import { openSSE } from '../fixtures/sse';
import * as http from 'node:http';

/**
 * Operational Reliability & Tool Semantics v1, black-box over HTTP against the
 * real binary (contracts/OPERATIONAL_RELIABILITY_CONTRACTS.md).
 *
 * Boots its own gateway with the deterministic scripted provider and a loopback
 * HTTP fixture that can fail in the exact ways the contract distinguishes:
 * a dropped connection *before* the response (indeterminate), a connection that
 * never answers (deadline), and a clean accept with the idempotency header
 * recorded.
 *
 * Covered: read retry, idempotency-key mutation, a mutation that is never
 * retried because its outcome is unknown, unknown-outcome telemetry, duplicate
 * suppression, cancellation, deadline-bounded retries, capability disable
 * blocking new invocations, lifecycle during an in-flight run, budgets across
 * replanning, structured observations, secret-free telemetry, injected content
 * staying inert, governance still applying, and reliability discovery metadata.
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
      try {
        await use(handle);
      } finally {
        await handle.dispose();
      }
    },
    { scope: 'worker' },
  ],
});

interface SeenRequest {
  path: string;
  method: string;
  idempotencyKey: string;
}

let fixtureBase = '';
let fixtureServer: http.Server;
let seenRequests: SeenRequest[] = [];
let flakyReads = 0;
const slowDelayMs = 3000;
const lostResponsePath = '/lost-response';
const acceptPath = '/accept';
const flakyPath = '/flaky-read';
const hangPath = '/hang';
const slowPath = '/slow';

beforeAll(async () => {
  fixtureServer = http.createServer((req, res) => {
    const url = req.url ?? '/';
    const path = url.split('?')[0];
    seenRequests.push({
      path,
      method: req.method ?? 'GET',
      idempotencyKey: String(req.headers['idempotency-key'] ?? ''),
    });
    // Drain the body first so a mutation is fully dispatched before the
    // socket disappears: the remote side is indeterminate, not unsent.
    req.on('data', () => undefined);
    req.on('end', () => {
      switch (path) {
        case flakyPath:
          flakyReads++;
          if (flakyReads === 1) {
            // The request was dispatched and the response was lost: a
            // transport transient a read may retry exactly once.
            req.socket?.destroy();
            return;
          }
          res.setHeader('content-type', 'text/plain');
          res.end('nexus-fixture:flaky-read-ok');
          return;
        case hangPath:
          // Never answers: the shared call deadline has to end this.
          return;
        case slowPath:
          setTimeout(() => {
            res.setHeader('content-type', 'text/plain');
            res.end('nexus-fixture:slow');
          }, slowDelayMs);
          return;
        case lostResponsePath:
          // Mutation dispatched, response lost → unknown outcome.
          req.socket?.destroy();
          return;
        case acceptPath:
          res.writeHead(201, { 'content-type': 'application/json' });
          res.end('{"accepted":true}');
          return;
        default:
          res.setHeader('content-type', 'text/plain');
          res.end('nexus-fixture:' + path);
      }
    });
  });
  await new Promise<void>((r) => fixtureServer.listen(0, '127.0.0.1', r));
  const addr = fixtureServer.address() as any;
  fixtureBase = `http://127.0.0.1:${addr.port}`;
});

afterAll(async () => {
  if (fixtureServer) await new Promise<void>((r) => fixtureServer.close(() => r()));
});

function requestsTo(path: string, method?: string): SeenRequest[] {
  return seenRequests.filter((r) => r.path === path && (!method || r.method === method));
}

function resetFixtureCounters() {
  seenRequests = [];
  flakyReads = 0;
}

async function registerReliabilityAnalyst(request, nexus, overrides = {}) {
  const res = await request.post(`${nexus.baseURL}/api/v1/agents`, {
    headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
    data: {
      entity_id: `rel-analyst-${suffix}`,
      name: 'Reliability Analyst',
      business_id: nexus.businessID,
      capabilities: ['analysis'],
      allowed_tools: ['http.request', 'web.search', 'filesystem.write', 'calculator'],
      ...overrides,
    },
  });
  expect([200, 409]).toContain(res.status());
}

async function submitObjective(request, nexus, description: string) {
  return request.post(`${nexus.baseURL}/api/v1/intelligence/execute`, {
    headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
    data: { business_id: nexus.businessID, actor_id: nexus.actorID, objective: { description } },
  });
}

async function pollObjective(request, nexus, id: string, tries = 2400) {
  for (let i = 0; i < tries; i++) {
    const res = await request.get(
      `${nexus.baseURL}/api/v1/intelligence/${id}?business_id=${nexus.businessID}`,
      { headers: bootstrapHeaders(nexus) },
    );
    if (res.status() === 200) return (await res.json()) as any;
    if (res.status() !== 202) throw new Error(`poll: ${res.status()} ${await res.text()}`);
    await new Promise((r) => setTimeout(r, 10));
  }
  throw new Error('poll: never terminal');
}

async function runObjective(request, nexus, description: string) {
  const res = await submitObjective(request, nexus, description);
  expect(res.status(), `submit: ${await res.text()}`).toBe(202);
  const { execution_id } = await res.json();
  return { execution_id, result: await pollObjective(request, nexus, execution_id) };
}

async function setCapabilityState(request, nexus, toolID: string, state: string) {
  return request.post(`${nexus.baseURL}/api/v1/control/capabilities/${toolID}/state`, {
    headers: { ...controlHeaders(nexus), 'Content-Type': 'application/json' },
    data: { state, reason: 'e2e reliability probe' },
  });
}

function payloadOf(frame: any): Record<string, any> {
  const payload = frame.json?.payload;
  return typeof payload === 'string' ? JSON.parse(payload) : (payload ?? {});
}

function framesOf(conn: any, event: string): any[] {
  return conn.frames.filter((f: any) => f.event === event);
}

test.describe.configure({ mode: 'serial' });

test.describe('OPERATIONAL RELIABILITY & TOOL SEMANTICS', () => {
  test('discovery reports the reliability posture of each capability', async ({ request, nexus }) => {
    const res = await request.get(
      `${nexus.baseURL}/api/v1/tools?business_id=${nexus.businessID}`,
      { headers: bootstrapHeaders(nexus) },
    );
    expect(res.status()).toBe(200);
    const catalog = await res.json();
    const httpTool = catalog.tools.find((t: any) => t.tool_id === 'http.request');
    expect(httpTool.capability_state).toBe('enabled');
    expect(httpTool.supports_idempotency).toBe(true);
    expect(httpTool.supports_reconciliation).toBe(false);
    expect(httpTool.retry_policy).toBe('not_sent_retry');
    expect(httpTool.max_attempts).toBe(2);
    // A declared read capability advertises the bounded read retry policy.
    const readTool = catalog.tools.find((t: any) => t.tool_id === 'git');
    expect(readTool.side_effect_class).toBe('read');
    expect(readTool.retry_policy).toBe('bounded_read_retry');
    expect(readTool.max_attempts).toBe(2);
  });

  test('a lost response on a read is retried once, within one call deadline', async ({ request, nexus }) => {
    await registerReliabilityAnalyst(request, nexus);
    resetFixtureCounters();
    const conn = openSSE(nexus.baseURL, `/events?business_id=${nexus.businessID}`, {
      ...bootstrapHeaders(nexus),
    });
    try {
      const submit = await submitObjective(request, nexus, `fetch ${fixtureBase}${flakyPath} via http request`);
      expect(submit.status()).toBe(202);
      const { execution_id } = await submit.json();
      await (conn as any).waitForFrame(
        (f: any) => f.event === 'tool.invocation.completed' && f.json?.correlation_id === execution_id,
        30000,
        'tool.invocation.completed frame',
      );
      const result = await pollObjective(request, nexus, execution_id);
      expect(result.status).toBe('completed');
      expect(result.outcome.summary).toContain('tool_calls=1');
      // Exactly one retry: two physical attempts for one logical call.
      expect(requestsTo(flakyPath, 'GET')).toHaveLength(2);
      const started = framesOf(conn, 'tool.attempt.started').filter(
        (f: any) => f.json?.correlation_id === execution_id,
      );
      expect(started.length).toBeGreaterThanOrEqual(2);
      const retry = framesOf(conn, 'tool.retry.scheduled').filter(
        (f: any) => f.json?.correlation_id === execution_id,
      );
      expect(retry.length).toBe(1);
      expect(payloadOf(retry[0]).error_class).toBeTruthy();
      const completed = framesOf(conn, 'tool.invocation.completed').find(
        (f: any) => f.json?.correlation_id === execution_id,
      );
      const payload = payloadOf(completed);
      expect(payload.outcome).toBe('completed');
      expect(payload.attempts).toBe('2');
      expect(payload.call_id).toBeTruthy();
    } finally {
      await conn.close();
    }
  });

  test('a mutation carries the logical call id as its idempotency key', async ({ request, nexus }) => {
    await registerReliabilityAnalyst(request, nexus);
    resetFixtureCounters();
    const submit = await submitObjective(
      request, nexus, `post via http request to ${fixtureBase}${acceptPath}`,
    );
    expect(submit.status()).toBe(202);
    const { execution_id } = await submit.json();
    const result = await pollObjective(request, nexus, execution_id);
    expect(result.status).toBe('completed');
    const posts = requestsTo(acceptPath, 'POST');
    expect(posts).toHaveLength(1);
    expect(posts[0].idempotencyKey).toBeTruthy();
    // The key is the logical call identity, stable and non-secret.
    expect(posts[0].idempotencyKey).toBe(execution_id);
    // A read never claims mutation idempotency.
    await runObjective(request, nexus, `fetch ${fixtureBase}/plain via http request`);
    expect(requestsTo('/plain', 'GET')[0].idempotencyKey).toBe('');
  });

  test('an indeterminate mutation is never retried and stays a single mutation', async ({ request, nexus }) => {
    await registerReliabilityAnalyst(request, nexus);
    resetFixtureCounters();
    const conn = openSSE(nexus.baseURL, `/events?business_id=${nexus.businessID}`, {
      ...bootstrapHeaders(nexus),
    });
    try {
      const first = await runObjective(
        request, nexus, `post via http request to ${fixtureBase}${lostResponsePath}`,
      );
      expect(first.result.status).toBe('failed');
      expect(first.result.error.message).toContain('state=failed');
      expect(first.result.error.message).toMatch(/unknown outcome/i);
      expect(first.result.error.message).not.toMatch(/tool_calls=1 \| .*state=completed/);
      // Second identical mutation: still exactly one remote mutation each.
      const second = await runObjective(
        request, nexus, `post via http request to ${fixtureBase}${lostResponsePath}`,
      );
      expect(second.result.status).toBe('failed');
      // Duplicate suppression: two logical calls, two mutations, never more.
      expect(requestsTo(lostResponsePath, 'POST')).toHaveLength(2);
      for (const run of [first, second]) {
        const retries = framesOf(conn, 'tool.retry.scheduled').filter(
          (f: any) => f.json?.correlation_id === run.execution_id,
        );
        expect(retries).toHaveLength(0);
        // The terminal frame is awaited explicitly: the objective result and the
        // SSE frame are two paths, so scanning an array would race.
        const unknown = await (conn as any).waitForFrame(
          (f: any) =>
            f.event === 'tool.invocation.unknown' &&
            f.json?.correlation_id === run.execution_id,
          20000,
          `tool.invocation.unknown for ${run.execution_id}`,
        );
        const payload = payloadOf(unknown);
        expect(payload.outcome).toBe('unknown');
        expect(payload.error_kind).toContain('unknown');
        expect(payload.retry_recommended).toBe('false');
        expect(payload.attempts).toBe('1');
        expect(framesOf(conn, 'tool.invocation.failed').filter(
          (f: any) => f.json?.correlation_id === run.execution_id,
        )).toHaveLength(0);
      }
    } finally {
      await conn.close();
    }
  });

  test('cancellation stops a live invocation without reporting success', async ({ request, nexus }) => {
    await registerReliabilityAnalyst(request, nexus);
    const submit = await submitObjective(request, nexus, `fetch ${fixtureBase}${slowPath} via http request`);
    expect(submit.status()).toBe(202);
    const { execution_id } = await submit.json();
    await new Promise((r) => setTimeout(r, 400));
    const cancel = await request.post(
      `${nexus.baseURL}/api/v1/intelligence/${execution_id}/cancel?business_id=${nexus.businessID}`,
      { headers: bootstrapHeaders(nexus) },
    );
    expect([202, 409]).toContain(cancel.status());
    const result = await pollObjective(request, nexus, execution_id);
    expect(result.status).not.toBe('completed');
    expect(result.outcome.summary ?? '').not.toContain('state=completed');
  });

  test('retries share the call deadline instead of extending it', async ({ request, nexus }) => {
    await registerReliabilityAnalyst(request, nexus);
    const conn = openSSE(nexus.baseURL, `/events?business_id=${nexus.businessID}`, {
      ...bootstrapHeaders(nexus),
    });
    try {
      const started = Date.now();
      const submit = await submitObjective(
        request, nexus, `fetch ${fixtureBase}${hangPath} via http request`,
      );
      expect(submit.status()).toBe(202);
      const { execution_id } = await submit.json();
      // The endpoint never answers, so the terminal frame is the deadline.
      const timedOut = await (conn as any).waitForFrame(
        (f: any) => f.event === 'tool.invocation.timed_out' && f.json?.correlation_id === execution_id,
        40000,
        'tool.invocation.timed_out frame',
      );
      const elapsed = Date.now() - started;
      const result = await pollObjective(request, nexus, execution_id);
      expect(result.status).toBe('failed');
      expect(result.error.message).toMatch(/timed out|timeout/i);
      const payload = payloadOf(timedOut);
      expect(payload.outcome).toBe('timed_out');
      // http.request bounds a call at 10s and two attempts share that ONE
      // deadline: the call cannot take 2 x 10s.
      expect(payload.attempts).toBe('2');
      expect(elapsed).toBeGreaterThanOrEqual(9_000);
      expect(elapsed).toBeLessThan(18_000);
    } finally {
      await conn.close();
    }
  });

  test('a disabled capability blocks new invocations and can be re-enabled', async ({ request, nexus }) => {
    await registerReliabilityAnalyst(request, nexus);
    const disable = await setCapabilityState(request, nexus, 'http.request', 'disabled');
    expect(disable.status()).toBe(200);
    try {
      const blocked = await runObjective(request, nexus, `fetch ${fixtureBase}/plain via http request`);
      expect(blocked.result.status).toBe('failed');
      expect(blocked.result.error.message).toMatch(/capability_disabled|is disabled/i);
      // Deprecating from disabled is outside the transition graph.
      const invalid = await setCapabilityState(request, nexus, 'http.request', 'deprecated');
      expect(invalid.status()).toBe(409);
    } finally {
      const enable = await setCapabilityState(request, nexus, 'http.request', 'enabled');
      expect(enable.status()).toBe(200);
    }
    const recovered = await runObjective(request, nexus, `fetch ${fixtureBase}/plain via http request`);
    expect(recovered.result.status).toBe('completed');
  });

  test('a disable during an in-flight run never interrupts the authorized call', async ({ request, nexus }) => {
    await registerReliabilityAnalyst(request, nexus);
    const submit = await submitObjective(request, nexus, `fetch ${fixtureBase}${slowPath} via http request`);
    expect(submit.status()).toBe(202);
    const { execution_id } = await submit.json();
    // Disable while the adapter is mid-call: the authorized dispatch finishes,
    // only later invocations are refused.
    await new Promise((r) => setTimeout(r, 300));
    const disable = await setCapabilityState(request, nexus, 'http.request', 'disabled');
    expect(disable.status()).toBe(200);
    try {
      const result = await pollObjective(request, nexus, execution_id);
      expect(result.status).toBe('completed');
      const after = await runObjective(request, nexus, `fetch ${fixtureBase}/plain via http request`);
      expect(after.result.status).toBe('failed');
      expect(after.result.error.message).toMatch(/capability_disabled|is disabled/i);
    } finally {
      const enable = await setCapabilityState(request, nexus, 'http.request', 'enabled');
      expect(enable.status()).toBe(200);
    }
  });

  test('replanning does not reset the budget counters', async ({ request, nexus }) => {
    await registerReliabilityAnalyst(request, nexus);
    const { result } = await runObjective(
      request, nexus, 'replan the approach and then report the outcome',
    );
    expect(result.status).toBe('completed');
    const summary = result.outcome.summary as string;
    expect(summary).toContain('replans=1');
    // The loop kept counting iterations across the replan: a replan is a new
    // logical proposal, never a fresh budget.
    const iterations = Number(/iterations=(\d+)/.exec(summary)?.[1] ?? '0');
    expect(iterations).toBeGreaterThanOrEqual(2);
    expect(summary).toContain('tool_calls=0');
  });

  test('observations are structured and correlated per attempt', async ({ request, nexus }) => {
    await registerReliabilityAnalyst(request, nexus);
    resetFixtureCounters();
    const conn = openSSE(nexus.baseURL, `/events?business_id=${nexus.businessID}`, {
      ...bootstrapHeaders(nexus),
    });
    try {
      const submit = await submitObjective(
        request, nexus, `fetch ${fixtureBase}${flakyPath} via http request`,
      );
      expect(submit.status()).toBe(202);
      const { execution_id } = await submit.json();
      // Await the last frame of the sequence: the objective result and the SSE
      // frames travel on two independent paths.
      await (conn as any).waitForFrame(
        (f: any) => f.event === 'tool.invocation.completed' && f.json?.correlation_id === execution_id,
        30000,
        'tool.invocation.completed frame',
      );
      const result = await pollObjective(request, nexus, execution_id);
      expect(result.status).toBe('completed');
      const started = framesOf(conn, 'tool.attempt.started').filter(
        (f: any) => f.json?.correlation_id === execution_id,
      );
      expect(started.length).toBe(2);
      const seen = new Set<string>();
      for (const frame of started) {
        const payload = payloadOf(frame);
        expect(payload.tool_id).toBe('http.request');
        expect(payload.operation).toBe('get');
        expect(payload.call_id).toBeTruthy();
        expect(payload.max_attempts).toBe('2');
        seen.add(payload.attempt);
      }
      expect([...seen].sort()).toEqual(['1', '2']);
      const attemptDone = framesOf(conn, 'tool.attempt.completed').filter(
        (f: any) => f.json?.correlation_id === execution_id,
      );
      const outcomes = attemptDone.map((f: any) => payloadOf(f).outcome).sort();
      expect(outcomes).toEqual(['completed', 'failed']);
    } finally {
      await conn.close();
    }
  });

  test('telemetry carries no credential material', async ({ request, nexus }) => {
    await registerReliabilityAnalyst(request, nexus);
    resetFixtureCounters();
    const conn = openSSE(nexus.baseURL, `/events?business_id=${nexus.businessID}`, {
      ...bootstrapHeaders(nexus),
    });
    try {
      const { result } = await runObjective(
        request, nexus, `post via http request to ${fixtureBase}${acceptPath}`,
      );
      expect(result.status).toBe('completed');
      // The strongest form of the check: none of this run's actual secrets or
      // any credential-shaped material may appear anywhere in the stream.
      const raw = conn.raw;
      const forbidden = [
        nexus.credential,
        nexus.controlKey,
        'Bearer ',
        'client_secret',
        'private_key',
        '"secret"',
      ];
      for (const needle of forbidden) {
        expect(raw, `telemetry leaked ${needle}`).not.toContain(needle);
      }
      // The idempotency key is an identity, never a secret.
      const posts = requestsTo(acceptPath, 'POST');
      expect(posts[0].idempotencyKey).toBeTruthy();
      expect(posts[0].idempotencyKey.toLowerCase()).not.toContain('secret');
    } finally {
      await conn.close();
    }
  });

  test('injected instructions in tool results stay inert', async ({ request, nexus }) => {
    await registerReliabilityAnalyst(request, nexus);
    const conn = openSSE(nexus.baseURL, `/events?business_id=${nexus.businessID}`, {
      ...bootstrapHeaders(nexus),
    });
    try {
      const { result } = await runObjective(request, nexus, 'search the web for nexus runtime');
      expect(result.status).toBe('completed');
      const summary = result.outcome.summary as string;
      // Exactly one mediated tool call: injected text caused no new action.
      expect(summary).toContain('tool_calls=1');
      expect(summary).toContain('observations=1');
      const capabilityStarts = framesOf(conn, 'tool.invocation.started').filter(
        (f: any) => f.json?.correlation_id === result?.correlation_id,
      );
      expect(capabilityStarts.length).toBeLessThanOrEqual(1);
    } finally {
      await conn.close();
    }
  });

  test('governance still decides before any capability runs', async ({ request, nexus }) => {
    const policy = await request.put(`${nexus.baseURL}/api/v1/control/policies/deny-reliability`, {
      headers: { ...controlHeaders(nexus), 'Content-Type': 'application/json' },
      data: {
        policy_type: 'access_control',
        name: 'deny-reliability',
        description: 'deny every objective during the reliability probe',
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
    try {
      const { result } = await runObjective(request, nexus, `fetch ${fixtureBase}/plain via http request`);
      expect(result.status).toBe('failed');
      expect(result.error.category).toBe('POLICY_DENIED');
    } finally {
      await request.delete(`${nexus.baseURL}/api/v1/control/policies/deny-reliability`, {
        headers: controlHeaders(nexus),
      });
    }
  });

  test('lifecycle control requires the operator key', async ({ request, nexus }) => {
    const noKey = await request.post(`${nexus.baseURL}/api/v1/control/capabilities/http.request/state`, {
      headers: { 'Content-Type': 'application/json' },
      data: { state: 'disabled' },
    });
    expect(noKey.status()).toBe(401);
    const wrongKey = await request.post(`${nexus.baseURL}/api/v1/control/capabilities/http.request/state`, {
      headers: { 'Content-Type': 'application/json', 'X-API-Key': 'not-the-key' },
      data: { state: 'disabled' },
    });
    expect(wrongKey.status()).toBe(401);
    const unknown = await request.post(`${nexus.baseURL}/api/v1/control/capabilities/not.registered/state`, {
      headers: { ...controlHeaders(nexus), 'Content-Type': 'application/json' },
      data: { state: 'disabled' },
    });
    expect(unknown.status()).toBe(404);
    const bad = await setCapabilityState(request, nexus, 'http.request', 'sideways');
    expect(bad.status()).toBe(400);
  });
});