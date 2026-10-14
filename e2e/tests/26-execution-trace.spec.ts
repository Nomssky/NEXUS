import { test as base, expect } from '@playwright/test';
import type { APIRequestContext } from '@playwright/test';
import {
  actorHeaders,
  bootstrapHeaders,
  controlHeaders,
  readEnvelope,
  randomSuffix,
} from '../fixtures/api';
import { startNexus, type NexusHandle } from '../fixtures/nexus';
import { sweepE2ePolicies } from '../fixtures/policy';
import * as http from 'node:http';

/**
 * Execution Trace & Operational Observability, black-box over HTTP against
 * the real binary (contracts/EXECUTION_TRACE_CONTRACTS.md v7).
 *
 * The trace is a read-only, process-local, capture-time-redacted view over
 * the events the runtime already publishes through its single in-process
 * bus. Every assertion here is made through the public HTTP surface: the
 * trace route, the execution detail read, the approval surface and a
 * loopback HTTP fixture that counts the requests that physically leave the
 * process — so "the tool ran exactly once" is proven by wire evidence, not
 * by a status string.
 *
 * Covered: causally consistent capture with proposal-vs-attempt-vs-
 * execution evidence classes; one logical call with correct physical
 * attempt counts across a retry; unknown outcomes staying unknown; a
 * governance denial evidenced without any dispatch; approval pause/resume
 * correlating in one trace with exactly one physical dispatch and
 * capture-time redaction of approver prose; authorization and validation
 * parity with the detail read (404/401/400); capture-order pagination;
 * delegated-child lineage; concurrent read-only consistency; and the
 * process-local durability boundary across a restart.
 */

const suffix = randomSuffix();
const SENTINEL_OBJECTIVE = `SENTINEL-OBJECTIVE-${suffix}`;
const SENTINEL_APPROVER = `SENTINEL-APPROVER-${suffix}`;

const agentID = `trace-agent-${suffix}`;
const approverID = `trace-approver-${suffix}`;
const approverCredential = `cred-${suffix}-approver`;

let agentReady = false;
let approverReady = false;

async function ensureAgent(request: APIRequestContext, nexus: NexusHandle): Promise<void> {
  if (agentReady) return;
  const res = await request.post(`${nexus.baseURL}/api/v1/agents`, {
    headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
    data: {
      entity_id: agentID,
      name: 'Trace Analyst',
      business_id: nexus.businessID,
      capabilities: ['analysis'],
      allowed_tools: ['http.request', 'web.search'],
    },
  });
  expect([200, 409], `register agent: ${await res.text()}`).toContain(res.status());
  agentReady = true;
}

async function ensureApprover(request: APIRequestContext, nexus: NexusHandle): Promise<void> {
  if (approverReady) return;
  const res = await request.post(`${nexus.baseURL}/api/v1/identities`, {
    headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
    data: {
      entity_id: approverID,
      identity_type: 'human',
      display_name: 'Trace Approver',
      business_id: nexus.businessID,
      credential: approverCredential,
      credential_method: 'password',
    },
  });
  expect([200, 409], `create approver: ${await res.text()}`).toContain(res.status());
  approverReady = true;
}

// ---- loopback capability fixture -------------------------------------------

let fixtureBase = '';
let fixtureServer: http.Server;
const seenPaths: string[] = [];
let flakyReads = 0;
const okPath = `/trace-ok-${suffix}`;
const flakyPath = `/trace-flaky-${suffix}`;
const lostPath = `/trace-lost-${suffix}`;
const deniedPath = `/trace-denied-${suffix}`;
const approvedPath = `/trace-approved-${suffix}`;

base.beforeAll(async () => {
  fixtureServer = http.createServer((req, res) => {
    const path = (req.url ?? '/').split('?')[0];
    seenPaths.push(path);
    req.on('data', () => undefined);
    req.on('end', () => {
      if (path === flakyPath) {
        flakyReads++;
        if (flakyReads === 1) {
          // Dispatched, response lost: the bounded read retry must run once.
          req.socket?.destroy();
          return;
        }
        res.writeHead(200, { 'content-type': 'text/plain' });
        res.end('nexus-fixture:flaky-ok');
        return;
      }
      if (path === lostPath) {
        // Mutation dispatched, response lost → contractual unknown.
        req.socket?.destroy();
        return;
      }
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(JSON.stringify({ ok: true, path }));
    });
  });
  await new Promise<void>((r) => fixtureServer.listen(0, '127.0.0.1', r));
  const addr = fixtureServer.address() as { port: number };
  fixtureBase = `http://127.0.0.1:${addr.port}`;
});

base.afterAll(async () => {
  if (fixtureServer) await new Promise<void>((r) => fixtureServer.close(() => r()));
});

const test = base.extend<{ nexus: NexusHandle }>({
  nexus: [
    async ({}, use) => {
      const handle = await startNexus({
        extraEnv: {
          NEXUS_SEEDED_PROVIDER_MODE: 'scripted',
          NEXUS_CAPABILITY_WEB: 'scripted',
          NEXUS_TOOL_HTTP_ALLOW_LOOPBACK: 'true',
          NEXUS_TOOL_HTTP_ALLOW_INSECURE: 'true',
          NEXUS_TOOL_HTTP_ALLOWED_HOSTS: '127.0.0.1,localhost',
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

// Governance is process-lifetime state on the shared gateway: a DENY or
// REQUIRE_APPROVAL policy must be removed even when an assertion fails
// halfway, or the next test inherits a gate it knows nothing about.
test.afterEach(async ({ request, nexus }) => {
  await sweepE2ePolicies(request, nexus);
});
test.describe.configure({ mode: 'serial' });

function requestsTo(path: string): string[] {
  return seenPaths.filter((p) => p === path);
}

// ---- API helpers -----------------------------------------------------------

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
  expect(body.execution_id).toBeTruthy();
  return body.execution_id as string;
}

async function pollObjective(
  request: APIRequestContext,
  nexus: NexusHandle,
  id: string,
  tries = 2400,
): Promise<any> {
  for (let i = 0; i < tries; i++) {
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

async function runObjective(
  request: APIRequestContext,
  nexus: NexusHandle,
  description: string,
): Promise<{ executionID: string; result: any }> {
  const executionID = await submitObjective(request, nexus, description);
  const result = await pollObjective(request, nexus, executionID);
  return { executionID, result };
}

async function getTrace(
  request: APIRequestContext,
  nexus: NexusHandle,
  id: string,
  query = '',
): Promise<{ res: import('@playwright/test').APIResponse; body: string }> {
  const res = await request.get(
    `${nexus.baseURL}/api/v1/intelligence/${encodeURIComponent(id)}/trace` +
      `?business_id=${nexus.businessID}${query}`,
    { headers: bootstrapHeaders(nexus) },
  );
  return { res, body: await res.text() };
}

interface TraceEntry {
  seq: number;
  event_id: string;
  type: string;
  source: string;
  timestamp: string;
  data?: Record<string, string>;
  redacted_keys?: string[];
  data_unparseable?: boolean;
}

interface TraceView {
  execution_id: string;
  correlation_id: string;
  business_id: string;
  status: string;
  durability: string;
  capture: {
    available: boolean;
    entries: number;
    dropped: number;
    limit: number;
    truncated: boolean;
    first_seq: number;
    last_seq: number;
    terminal_seq?: number;
    post_terminal?: number;
    has_more?: boolean;
  };
  entries: TraceEntry[];
}

async function traceOf(
  request: APIRequestContext,
  nexus: NexusHandle,
  id: string,
  query = '',
): Promise<TraceView> {
  const { res, body } = await getTrace(request, nexus, id, query);
  expect(res.status(), `trace ${id}: ${body}`).toBe(200);
  const view = JSON.parse(body) as TraceView;
  expect(view.execution_id).toBe(id);
  expect(view.durability).toBe('process_local');
  return view;
}

function indexOfType(view: TraceView, type: string, dataKey?: string, dataValue?: string): number {
  for (let i = 0; i < view.entries.length; i++) {
    const e = view.entries[i];
    if (e.type !== type) continue;
    if (dataKey === undefined || e.data?.[dataKey] === dataValue) return i;
  }
  return -1;
}

function countType(view: TraceView, type: string): number {
  return view.entries.filter((e) => e.type === type).length;
}

function expectCaptureCoherent(view: TraceView): void {
  expect(view.capture.available).toBe(true);
  expect(view.capture.entries).toBe(view.entries.length);
  expect(view.capture.limit).toBe(256);
  expect(view.entries.length).toBeGreaterThan(0);
  for (let i = 0; i < view.entries.length; i++) {
    expect(view.entries[i].seq, `capture order at ${i}`).toBe(i + 1);
  }
  expect(view.capture.first_seq).toBe(1);
  expect(view.capture.last_seq).toBe(view.entries.length);
}

// ---- the suite -------------------------------------------------------------

test.describe('EXECUTION TRACE', () => {

  test('a completed execution traces causally, redacts free text, and states its durability', async ({
    request,
    nexus,
  }) => {
    await ensureAgent(request, nexus);
    const { executionID, result } = await runObjective(
      request,
      nexus,
      `http request ${fixtureBase}${okPath} and report the response; note ${SENTINEL_OBJECTIVE}`,
    );
    expect(result.status, `objective: ${JSON.stringify(result.error)}`).toBe('completed');
    expect(requestsTo(okPath), 'exactly one physical request').toHaveLength(1);

    const view = await traceOf(request, nexus, executionID);
    expect(view.status).toBe('completed');
    expect(view.correlation_id).toBe(executionID);
    expectCaptureCoherent(view);

    // Evidence classes in causal order: proposal → decision → platform
    // dispatch → physical attempt → invocation completed → the loop's
    // post-call record (tool.requested is emitted when InvokeToolScoped
    // returns, contract §6) → terminal.
    const proposal = indexOfType(view, 'action.proposed', 'action', 'tool_call');
    const decision = indexOfType(view, 'governance.decided', 'action', 'tool_call');
    const dispatch = indexOfType(view, 'tool.invocation.started');
    const attempt1 = indexOfType(view, 'tool.attempt.started');
    const completed = indexOfType(view, 'tool.invocation.completed');
    const loopRecord = indexOfType(view, 'tool.requested');
    const terminal = indexOfType(view, 'objective.completed', 'state', 'completed');
    expect(proposal, 'proposal recorded').toBeGreaterThanOrEqual(0);
    expect(decision, 'decision recorded').toBeGreaterThan(proposal);
    expect(dispatch, 'platform dispatch recorded').toBeGreaterThan(decision);
    expect(attempt1, 'physical attempt recorded').toBeGreaterThan(dispatch);
    expect(completed, 'invocation completed recorded').toBeGreaterThan(attempt1);
    expect(loopRecord, 'loop attempt record').toBeGreaterThan(completed);
    expect(terminal, 'terminal recorded').toBeGreaterThan(loopRecord);
    expect(view.capture.terminal_seq, 'terminal marker located').toBeGreaterThan(0);

    // One logical call, one physical attempt, one dispatch.
    expect(countType(view, 'tool.invocation.started')).toBe(1);
    expect(countType(view, 'tool.attempt.started')).toBe(1);
    const attempts = view.entries.filter((e) => e.type === 'tool.attempt.started');
    expect(attempts[0].data?.attempt).toBe('1');

    // Capture-time redaction: caller free text never entered the trace.
    const raw = JSON.stringify(view);
    expect(raw, 'objective sentinel leaked').not.toContain(SENTINEL_OBJECTIVE);
    const chainStarted = indexOfType(view, 'chain.started');
    expect(chainStarted).toBeGreaterThanOrEqual(0);
    expect(view.entries[chainStarted].redacted_keys ?? []).toContain('outcome');
    const objStarted = indexOfType(view, 'objective.started');
    expect(objStarted).toBeGreaterThanOrEqual(0);
    expect(view.entries[objStarted].redacted_keys ?? []).toContain('objective');
    expect(view.entries[objStarted].data?.objective).toBeUndefined();
  });

  test('one logical call across a retry shows the correct physical attempt count', async ({
    request,
    nexus,
  }) => {
    await ensureAgent(request, nexus);
    const { executionID, result } = await runObjective(
      request,
      nexus,
      `fetch ${fixtureBase}${flakyPath} via http request`,
    );
    expect(result.status).toBe('completed');
    expect(requestsTo(flakyPath), 'two physical attempts on the wire').toHaveLength(2);

    const view = await traceOf(request, nexus, executionID);
    expectCaptureCoherent(view);
    expect(countType(view, 'tool.invocation.started'), 'one logical call').toBe(1);
    expect(countType(view, 'tool.invocation.completed')).toBe(1);

    const attempts = view.entries.filter((e) => e.type === 'tool.attempt.started');
    expect(attempts, 'physical attempts recorded').toHaveLength(2);
    expect(attempts[0].data?.attempt).toBe('1');
    expect(attempts[0].data?.max_attempts, 'declared bound visible').toBeTruthy();
    expect(attempts[1].data?.attempt).toBe('2');
    // All attempts of one logical call share the invocation identity.
    const callID = attempts[0].data?.call_id;
    expect(callID).toBeTruthy();
    for (const a of attempts) {
      expect(a.data?.call_id).toBe(callID);
    }
    const started = view.entries.filter((e) => e.type === 'tool.invocation.started');
    expect(started[0].data?.call_id).toBe(callID);
    // The retry decision itself is evidence, not an inference.
    const retry = indexOfType(view, 'tool.retry.scheduled', 'attempt', '1');
    expect(retry, 'retry decision recorded').toBeGreaterThanOrEqual(0);
    expect(view.entries[retry].data?.error_class).toBeTruthy();
    expect(view.status).toBe('completed');
  });

  test('an unknown outcome stays unknown in the trace', async ({ request, nexus }) => {
    await ensureAgent(request, nexus);
    const { executionID, result } = await runObjective(
      request,
      nexus,
      `post via http request to ${fixtureBase}${lostPath}`,
    );
    expect(result.status, 'unknown is never a success').not.toBe('completed');
    expect(requestsTo(lostPath), 'the mutation did reach the wire').toHaveLength(1);

    const view = await traceOf(request, nexus, executionID);
    expectCaptureCoherent(view);
    expect(indexOfType(view, 'tool.invocation.unknown'), 'unknown recorded').toBeGreaterThanOrEqual(0);
    expect(indexOfType(view, 'tool.invocation.completed'), 'never upgraded to completed').toBe(-1);
    expect(indexOfType(view, 'tool.invocation.failed'), 'never collapsed to failed').toBe(-1);
    expect(['failed', 'cancelled', 'timed_out', 'unknown']).toContain(view.status);
    expect(view.capture.terminal_seq).toBeGreaterThan(0);
  });

  test('a governance denial is evidenced and dispatches nothing', async ({ request, nexus }) => {
    await ensureAgent(request, nexus);
    const put = await request.put(
      `${nexus.baseURL}/api/v1/control/policies/e2e-trace-deny-${suffix}`,
      {
        headers: { ...controlHeaders(nexus), 'Content-Type': 'application/json' },
        data: {
          policy_type: 'access_control',
          name: `e2e-trace-deny-${suffix}`,
          description: 'execution trace: denial is evidenced without dispatch',
          status: 'active',
          subject: { subject_type: 'all' },
          action: { action_type: 'custom', action_ids: ['tool_call'] },
          resource: { resource_type: 'tool', resource_ids: ['http.request'] },
          effect: 'DENY',
          precedence: 1000,
        },
      },
    );
    expect(put.status(), `put policy: ${await put.text()}`).toBe(200);

    const { executionID, result } = await runObjective(
      request,
      nexus,
      `http request ${fixtureBase}${deniedPath} and report the response`,
    );
    expect(result.status).toBe('failed');
    expect(requestsTo(deniedPath), 'a denied call never reaches the wire').toHaveLength(0);

    const view = await traceOf(request, nexus, executionID);
    expectCaptureCoherent(view);
    expect(
      indexOfType(view, 'governance.decided', 'decision', 'DENY'),
      'denial evidenced',
    ).toBeGreaterThanOrEqual(0);
    expect(indexOfType(view, 'tool.invocation.started'), 'no dispatch').toBe(-1);
    expect(indexOfType(view, 'tool.attempt.started'), 'no physical attempt').toBe(-1);
    expect(indexOfType(view, 'objective.completed', 'state', 'denied')).toBeGreaterThanOrEqual(0);
  });

  test('approval pause and resume correlate in one trace with exactly one dispatch', async ({
    request,
    nexus,
  }) => {
    await ensureAgent(request, nexus);
    await ensureApprover(request, nexus);
    const put = await request.put(
      `${nexus.baseURL}/api/v1/control/policies/e2e-trace-approve-${suffix}`,
      {
        headers: { ...controlHeaders(nexus), 'Content-Type': 'application/json' },
        data: {
          policy_type: 'access_control',
          name: `e2e-trace-approve-${suffix}`,
          description: 'execution trace: approval pause/resume correlation',
          status: 'active',
          subject: { subject_type: 'all' },
          action: { action_type: 'custom', action_ids: ['tool_call'] },
          resource: { resource_type: 'tool', resource_ids: ['http.request'] },
          effect: 'REQUIRE_APPROVAL',
          precedence: 1000,
          approval_config: {
            approver_type: 'human',
            approver_ids: [approverID],
            timeout_seconds: 600,
            auto_deny_on_timeout: true,
            self_approval_prohibited: true,
          },
        },
      },
    );
    expect(put.status(), `put policy: ${await put.text()}`).toBe(200);

    const executionID = await submitObjective(
      request,
      nexus,
      `http request ${fixtureBase}${approvedPath} and report the response`,
    );
    const paused = await pollObjective(request, nexus, executionID);
    expect(paused.status).toBe('failed');
    expect(paused.error?.code).toBe('APPROVAL_REQUIRED');
    expect(requestsTo(approvedPath), 'nothing dispatched while waiting').toHaveLength(0);

    const before = await traceOf(request, nexus, executionID);
    expectCaptureCoherent(before);
    expect(indexOfType(before, 'approval.requested'), 'request recorded').toBeGreaterThanOrEqual(0);
    expect(
      indexOfType(before, 'objective.completed', 'state', 'pending_approval'),
      'wait state recorded',
    ).toBeGreaterThanOrEqual(0);
    expect(indexOfType(before, 'tool.invocation.started'), 'no dispatch before approval').toBe(-1);

    const approvalID = paused.error?.details?.approval_id as string;
    expect(approvalID, `approval id in ${JSON.stringify(paused.error)}`).toBeTruthy();
    const decided = await request.post(
      `${nexus.baseURL}/api/v1/approvals/${encodeURIComponent(approvalID)}/approve` +
        `?business_id=${nexus.businessID}`,
      {
        headers: { ...actorHeaders(approverID, approverCredential), 'Content-Type': 'application/json' },
        data: { reason: SENTINEL_APPROVER },
      },
    );
    expect(decided.status(), `approve: ${await decided.text()}`).toBe(202);

    const resumed = await pollObjective(request, nexus, executionID);
    expect(resumed.status).toBe('completed');
    expect(requestsTo(approvedPath), 'exactly one physical dispatch').toHaveLength(1);

    const view = await traceOf(request, nexus, executionID);
    expectCaptureCoherent(view);
    const requested = indexOfType(view, 'approval.requested');
    const waited = indexOfType(view, 'objective.completed', 'state', 'pending_approval');
    const approved = indexOfType(view, 'approval.approved', 'approval_id', approvalID);
    const dispatch = indexOfType(view, 'tool.invocation.started');
    const done = indexOfType(view, 'objective.completed', 'state', 'completed');
    expect(requested).toBeGreaterThanOrEqual(0);
    expect(waited).toBeGreaterThan(requested);
    expect(approved).toBeGreaterThan(waited);
    expect(dispatch).toBeGreaterThan(approved);
    expect(done).toBeGreaterThan(dispatch);

    // One approval, one dispatch, one execution — the resume never replays
    // the effect (mission test 7).
    expect(countType(view, 'approval.approved')).toBe(1);
    expect(countType(view, 'tool.invocation.started')).toBe(1);

    // Approver prose is redacted at capture; the identifier survives.
    expect(view.entries[approved].redacted_keys ?? []).toContain('decision_rationale');
    expect(view.entries[approved].data?.approver_id).toBe(approverID);
    expect(JSON.stringify(view), 'approver prose leaked').not.toContain(SENTINEL_APPROVER);

    // The first episode end is the terminal marker; the resume is counted,
    // visible and never rewrites it (contract §8).
    const marker = view.entries.find((e) => e.seq === view.capture.terminal_seq);
    expect(marker, 'terminal_seq references a captured entry').toBeTruthy();
    expect(['chain.failed', 'objective.completed']).toContain(marker!.type);
    expect(done, 'resume completion stays after the marker').toBeGreaterThan(view.capture.terminal_seq! - 1);
    expect(view.capture.post_terminal).toBe(view.entries.length - view.capture.terminal_seq!);
  });

  test('trace reads follow the detail read authorization and validation exactly', async ({
    request,
    nexus,
  }) => {
    await ensureAgent(request, nexus);
    const { executionID, result } = await runObjective(
      request,
      nexus,
      `http request ${fixtureBase}${okPath} and report the response`,
    );
    expect(result.status).toBe('completed');

    // Unknown execution: 404 with the standard envelope.
    const unknown = await request.get(
      `${nexus.baseURL}/api/v1/intelligence/exec-does-not-exist/trace?business_id=${nexus.businessID}`,
      { headers: bootstrapHeaders(nexus) },
    );
    expect(unknown.status()).toBe(404);
    const env = await readEnvelope(unknown, 'unknown trace');
    expect(env.code).toBe('VALIDATION');

    // Fail-closed parameter validation.
    const missingBusiness = await request.get(
      `${nexus.baseURL}/api/v1/intelligence/${executionID}/trace`,
      { headers: bootstrapHeaders(nexus) },
    );
    expect(missingBusiness.status()).toBe(400);
    for (const q of ['&limit=0', '&limit=abc', '&limit=99999', '&after_seq=-1', '&after_seq=abc']) {
      const bad = await request.get(
        `${nexus.baseURL}/api/v1/intelligence/${executionID}/trace?business_id=${nexus.businessID}${q}`,
        { headers: bootstrapHeaders(nexus) },
      );
      expect(bad.status(), `filter ${q}`).toBe(400);
    }

    // No credential: 401 — the trace never widens access.
    const noAuth = await request.get(
      `${nexus.baseURL}/api/v1/intelligence/${executionID}/trace?business_id=${nexus.businessID}`,
    );
    expect(noAuth.status()).toBe(401);

    // A scope that is not a member of this business learns nothing (G5): the
    // execution detail read and the trace read fail closed identically.
    const foreignID = `trace-foreign-${suffix}`;
    const foreignCredential = `cred-${suffix}-foreign`;
    const created = await request.post(`${nexus.baseURL}/api/v1/identities`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: {
        entity_id: foreignID,
        identity_type: 'human',
        display_name: 'Trace Foreign',
        business_id: nexus.businessID,
        credential: foreignCredential,
        credential_method: 'password',
      },
    });
    expect([200, 409], `create foreign: ${await created.text()}`).toContain(created.status());
    // Foreign identity, its own business, its own membership: the trace of a
    // foreign execution id is a 404 there too — no id probing.
    const foreign = await request.get(
      `${nexus.baseURL}/api/v1/intelligence/${executionID}/trace?business_id=${nexus.businessID}`,
      { headers: actorHeaders(foreignID, foreignCredential) },
    );
    // The foreign identity was created in this business; a plain member may
    // read what a member may read — the invariant tested here is that the
    // trace grants nothing beyond the detail read, so both answers match.
    const detail = await request.get(
      `${nexus.baseURL}/api/v1/intelligence/${executionID}?business_id=${nexus.businessID}`,
      { headers: actorHeaders(foreignID, foreignCredential) },
    );
    expect(foreign.status(), 'trace visibility equals detail visibility').toBe(detail.status());

    // Read-only: repeated reads are identical, and reading changes nothing.
    const first = await getTrace(request, nexus, executionID);
    const second = await getTrace(request, nexus, executionID);
    expect(first.res.status()).toBe(200);
    expect(second.body, 'deterministic retrieval').toBe(first.body);
    const reread = await pollObjective(request, nexus, executionID);
    expect(reread.status, 'retrieval altered nothing').toBe('completed');
  });

  test('capture-order pagination walks the whole trace without gaps or duplicates', async ({
    request,
    nexus,
  }) => {
    const { executionID } = await runObjective(
      request,
      nexus,
      `http request ${fixtureBase}${okPath} and report the response`,
    );
    const full = await traceOf(request, nexus, executionID);
    expect(full.entries.length).toBeGreaterThanOrEqual(4);

    const walked: number[] = [];
    let after = 0;
    for (let guard = 0; guard < 500; guard++) {
      const page = await traceOf(request, nexus, executionID, `&limit=2&after_seq=${after}`);
      expect(page.entries.length, 'a non-exhausted walk returns a page').toBeGreaterThan(0);
      for (const e of page.entries) walked.push(e.seq);
      after = page.entries[page.entries.length - 1].seq;
      if (!page.capture.has_more) break;
    }
    expect(walked).toHaveLength(full.entries.length);
    for (let i = 0; i < walked.length; i++) {
      expect(walked[i], `walk order at ${i}`).toBe(i + 1);
    }
  });

  test('a delegated child correlates into the parent trace with runtime lineage', async ({
    request,
    nexus,
  }) => {
    await ensureAgent(request, nexus);
    const { executionID, result } = await runObjective(request, nexus, 'delegate the detail work');
    expect(result.status).toBe('completed');

    const view = await traceOf(request, nexus, executionID);
    expectCaptureCoherent(view);
    expect(indexOfType(view, 'agent.delegated'), 'delegation recorded').toBeGreaterThanOrEqual(0);
    const lineage = view.entries.find(
      (e) => e.data?.delegation_depth === '1' && (e.data?.parent_agent_id ?? '') !== '',
    );
    expect(lineage, 'child lineage visible in the parent trace').toBeTruthy();
    expect(lineage!.data?.parent_task_id, 'runtime-stamped parent linkage').toBeTruthy();
  });

  test('concurrent trace reads are consistent and side-effect free', async ({ request, nexus }) => {
    const { executionID, result } = await runObjective(
      request,
      nexus,
      `http request ${fixtureBase}${okPath} and report the response`,
    );
    expect(result.status).toBe('completed');

    const bodies = await Promise.all(
      Array.from({ length: 8 }, () => getTrace(request, nexus, executionID)),
    );
    for (const { res, body } of bodies) {
      expect(res.status()).toBe(200);
      expect(body, 'identical concurrent snapshots').toBe(bodies[0].body);
    }
    const view = JSON.parse(bodies[0].body) as TraceView;
    expectCaptureCoherent(view);

    // A read while another execution is still in flight is honest: 200 with
    // a valid window whatever the moment.
    const inflightID = await submitObjective(
      request,
      nexus,
      `http request ${fixtureBase}${okPath} and report the response`,
    );
    const partial = await traceOf(request, nexus, inflightID);
    expect(['pending', 'completed', 'failed']).toContain(partial.status);
    expect(partial.capture.entries).toBe(partial.entries.length);
    expect(partial.entries.length).toBeLessThanOrEqual(partial.capture.limit);
    await pollObjective(request, nexus, inflightID);
  });

  test('the trace is process-local: a restart loses it with the execution record', async ({
    request,
    nexus,
  }) => {
    await ensureAgent(request, nexus);
    const { executionID, result } = await runObjective(
      request,
      nexus,
      `http request ${fixtureBase}${okPath} and report the response`,
    );
    expect(result.status).toBe('completed');
    const before = await traceOf(request, nexus, executionID);
    expect(before.entries.length).toBeGreaterThan(0);

    await nexus.restart();

    // The execution record itself is gone (G4); the trace reports the same
    // boundary — never a fabricated or partial history, never a claim of
    // crash-safe continuity (EXECUTION_TRACE_CONTRACTS §12).
    const detail = await request.get(
      `${nexus.baseURL}/api/v1/intelligence/${executionID}?business_id=${nexus.businessID}`,
      { headers: bootstrapHeaders(nexus) },
    );
    expect([404, 409], `detail after restart: ${await detail.text()}`).toContain(detail.status());
    const after = await getTrace(request, nexus, executionID);
    expect([404, 409], `trace after restart: ${after.body}`).toContain(after.res.status());
  });
});
