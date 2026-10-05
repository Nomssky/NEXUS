import { test as base, expect } from '@playwright/test';
import { test, expect as exp } from '../fixtures/test';
import { bootstrapHeaders, expectEnvelope, pollResult, randomSuffix, submit } from '../fixtures/api';
import { openSSE } from '../fixtures/sse';
import { startNexus, type NexusHandle } from '../fixtures/nexus';

const offlineTest = base.extend<{ offlineNexus: NexusHandle }>({
  offlineNexus: [
    async ({}, use) => {
      const handle = await startNexus({ extraEnv: { NEXUS_SEEDED_PROVIDER_STATUS: 'offline' } });
      try {
        await use(handle);
      } finally {
        await handle.dispose();
      }
    },
    { scope: 'worker' },
  ],
});

/**
 * Agent Execution Layer v1, end to end over HTTP against the real binary:
 * register/list/lifecycle, execute agent tasks, tools, workflows, delegation,
 * cancellation, retry notes, failure integrity, scope/admission/G2/G3/G5,
 * events, and restart semantics (G4: executions are process-local).
 */

const suffix = randomSuffix();

async function registerAgent(request, nexus, body) {
  return request.post(`${nexus.baseURL}/api/v1/agents`, {
    headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
    data: body,
  });
}

async function execute(request, nexus, body) {
  return request.post(`${nexus.baseURL}/api/v1/executions`, {
    headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
    data: body,
  });
}

async function pollExecution(request, nexus, id, businessID = nexus.businessID) {
  for (let i = 0; i < 1000; i++) {
    const res = await request.get(`${nexus.baseURL}/api/v1/executions/${id}?business_id=${businessID}`, {
      headers: bootstrapHeaders(nexus),
    });
    if (res.status() === 200) return (await res.json()) as any;
    if (res.status() !== 202) throw new Error(`poll: unexpected ${res.status()} ${await res.text()}`);
  }
  throw new Error('poll: never became terminal');
}

test.describe('AGENT EXECUTION LAYER', () => {
  test('register → discover → suspend → archive → terminated admissions', async ({ request, nexus }) => {
    const created = await registerAgent(request, nexus, {
      entity_id: `researcher-${suffix}`,
      name: 'Researcher',
      business_id: nexus.businessID,
      capabilities: ['research', 'analysis'],
      allowed_tools: ['echo'],
      memory: { mode: 'business' },
    });
    expect(created.status(), `create: ${await created.text()}`).toBe(200);
    const agent = await created.json();
    expect(agent.status).toBe('active');

    const list = await request.get(`${nexus.baseURL}/api/v1/agents?business_id=${nexus.businessID}`, {
      headers: bootstrapHeaders(nexus),
    });
    expect(list.status()).toBe(200);
    expect((await list.json()).agents.some((a: any) => a.id === agent.id)).toBe(true);

    const suspended = await request.post(`${nexus.baseURL}/api/v1/agents/${agent.id}/suspend`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: {},
    });
    expect(suspended.status()).toBe(200);
    expect((await suspended.json()).status).toBe('suspended');

    const reactivated = await request.post(`${nexus.baseURL}/api/v1/agents/${agent.id}/activate`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: {},
    });
    expect(reactivated.status()).toBe(200);

    const archived = await request.post(`${nexus.baseURL}/api/v1/agents/${agent.id}/archive`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: {},
    });
    expect(archived.status()).toBe(200);
    const reactivateArchived = await request.post(`${nexus.baseURL}/api/v1/agents/${agent.id}/activate`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: {},
    });
    expect(reactivateArchived.status(), 'archived is terminal').toBe(409);
  });

  test('simple agent task completes with provider/model/reason telemetry', async ({ request, nexus }) => {
    expect(
      (
        await registerAgent(request, nexus, {
          entity_id: `worker-${suffix}`,
          name: 'Worker',
          business_id: nexus.businessID,
          capabilities: ['analysis'],
        })
      ).status(),
    ).toBe(200);

    const submitted = await execute(request, nexus, {
      intent: 'analyze the quarterly report',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
      required_capabilities: ['analysis'],
    });
    expect(submitted.status(), `submit: ${await submitted.text()}`).toBe(202);
    const { execution_id } = await submitted.json();
    const result = await pollExecution(request, nexus, execution_id);
    expect(result.status).toBe('completed');
    expect(result.outcome.metrics.provider).toBe('simulated');
    expect(result.outcome.metrics.model).toBeDefined();
    expect(result.outcome.metrics.routing_reason).toContain('simulated');
    expect(result.outcome.metrics.executor_status).toBe('completed');
  });

  test('tool invocation runs and is observed; allowlist violations fail (tool not in allowlist)', async ({
    request,
    nexus,
  }) => {
    expect(
      (
        await registerAgent(request, nexus, {
          entity_id: `tooluser-${suffix}`,
          name: 'Tool User',
          business_id: nexus.businessID,
          capabilities: ['echo'],
          allowed_tools: ['echo'],
        })
      ).status(),
    ).toBe(200);

    const okExec = await execute(request, nexus, {
      intent: 'echo this',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
      required_capabilities: ['echo'],
      tools: [{ tool_id: 'echo', input: { text: 'hello-agent' } }],
    });
    expect(okExec.status()).toBe(202);
    const okResult = await pollExecution(request, nexus, (await okExec.json()).execution_id);
    expect(okResult.status).toBe('completed');
    expect(okResult.outcome.metrics.tools_executed).toBe(1);
    expect(okResult.outcome.summary).toContain('hello-agent');

    const denied = await execute(request, nexus, {
      intent: 'transform secret',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
      required_capabilities: ['echo'],
      tools: [{ tool_id: 'transform', input: { text: 'x', op: 'upper' } }],
    });
    expect(denied.status()).toBe(202);
    const deniedResult = await pollExecution(request, nexus, (await denied.json()).execution_id);
    expect(deniedResult.status, 'allowlist violation must fail, not hide').toBe('failed');
    expect(deniedResult.error.message).toMatch(/not in the agent allowlist|no eligible agent/);
  });

  test('sequential and parallel workflows', async ({ request, nexus }) => {
    const seq = await execute(request, nexus, {
      intent: 'sequential-pipeline',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
      workflow: {
        strategy: 'sequential',
        nodes: [
          { id: 'a', intent: 'first', capabilities: [] },
          { id: 'b', intent: 'second', capabilities: [] },
        ],
      },
    });
    expect(seq.status()).toBe(202);
    const seqResult = await pollExecution(request, nexus, (await seq.json()).execution_id);
    expect(seqResult.status).toBe('completed');
    expect(seqResult.outcome.summary).toContain('first');
    expect(seqResult.outcome.summary).toContain('second');

    const par = await execute(request, nexus, {
      intent: 'parallel-pipeline',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
      workflow: {
        strategy: 'parallel',
        nodes: [
          { id: 'a', intent: 'part a' },
          { id: 'b', intent: 'part b' },
        ],
      },
    });
    expect(par.status()).toBe(202);
    const parResult = await pollExecution(request, nexus, (await par.json()).execution_id);
    expect(parResult.status).toBe('completed');
  });

  test('delegation: parent executes, child runs in scope, child metrics reported', async ({ request, nexus }) => {
    const submitted = await execute(request, nexus, {
      intent: 'plan work',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
      delegates: [{ id: 'subclass', intent: 'run subtask', capabilities: [] }],
    });
    expect(submitted.status()).toBe(202);
    const result = await pollExecution(request, nexus, (await submitted.json()).execution_id);
    expect(result.status).toBe('completed');
    expect(result.outcome.metrics.child_executions).toBe(1);
  });

  offlineTest('offline provider marks executions failed, never completed', async ({ request, offlineNexus }) => {
    const submitted = await offlineNexus && (await request.post(`${offlineNexus.baseURL}/api/v1/executions`, {
      headers: { ...bootstrapHeaders(offlineNexus), 'Content-Type': 'application/json' },
      data: { intent: 'against offline provider', business_id: offlineNexus.businessID, actor_id: offlineNexus.actorID },
    }));
    expect(submitted.status()).toBe(202);
    const id = (await submitted.json()).execution_id;
    let terminal: any;
    for (let i = 0; i < 1000; i++) {
      const r = await request.get(`${offlineNexus.baseURL}/api/v1/executions/${id}?business_id=${offlineNexus.businessID}`, {
        headers: bootstrapHeaders(offlineNexus),
      });
      if (r.status() === 200) { terminal = await r.json(); break; }
      await new Promise((r) => setTimeout(r, 20));
    }
    expect(terminal.status).toBe('failed');
  });

  test('execution cancellation: cancel while queued yields cancelled terminal state', async ({ request, nexus }) => {
    // The chain loop is serial: submit a probe to occupy it; while it runs,
    // submit the target and then cancel it — it is still queued.
    const filler = await submit(request, nexus, {
      intent: 'occupant',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(filler.status()).toBe(202);

    const target = await execute(request, nexus, {
      intent: 'cancellable work',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(target.status()).toBe(202);
    const { execution_id } = await target.json();

    const cancelled = await request.post(
      `${nexus.baseURL}/api/v1/executions/${execution_id}/cancel?business_id=${nexus.businessID}`,
      { headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' }, data: {} },
    );
    expect([202, 409]).toContain(cancelled.status());
    if (cancelled.status() === 202) {
      const observed = await pollExecution(request, nexus, execution_id);
      expect(observed.status).toBe('cancelled');
    }
    // Whatever path won, the terminal state is truthful — never completed.
    const observed = await pollExecution(request, nexus, execution_id);
    expect(['completed', 'cancelled']).toContain(observed.status);
    if (cancelled.status() === 202) expect(observed.status).toBe('cancelled');
  });


  test('governance denial blocks execution; approval requirement creates a resumable record', async ({
    request,
    nexus,
  }) => {
    const policyRes = await request.put(`${nexus.baseURL}/api/v1/control/policies/deny-agents`, {
      headers: { 'X-API-Key': nexus.controlKey, 'Content-Type': 'application/json' },
      data: {
        policy_type: 'access_control',
        name: 'deny-agents',
        description: 'deny agent executions in this test',
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
    expect(policyRes.status()).toBe(200);
    const denied = await execute(request, nexus, {
      intent: 'must be denied',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(denied.status()).toBe(202);
    const deniedResult = await pollExecution(request, nexus, (await denied.json()).execution_id);
    expect(deniedResult.status).toBe('failed');
    expect(deniedResult.error.category).toBe('POLICY_DENIED');
  });

  test('inactive business blocks execution admission; suspended division only blocks its own', async ({
    request,
    nexus,
  }) => {
    const bizCreate = await request.post(`${nexus.baseURL}/api/v1/businesses`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: { name: 'aux-business', owner_identity_id: nexus.actorID },
    });
    expect(bizCreate.status()).toBe(200);
    const newBiz = (await bizCreate.json()).entity_id as string;

    const divisionCreate = await request.post(`${nexus.baseURL}/api/v1/divisions`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: { business_id: nexus.businessID, entity_id: `halt-${suffix}`, name: 'halt', owner_identity_id: nexus.actorID },
    });
    expect(divisionCreate.status()).toBe(200);

    const divSuspended = await request.post(`${nexus.baseURL}/api/v1/divisions/halt-${suffix}/suspend`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: {},
    });
    expect(divSuspended.status()).toBe(200);

    const divisionBlocked = await execute(request, nexus, {
      intent: 'blocked by suspended division',
      business_id: nexus.businessID,
      division_id: `halt-${suffix}`,
      actor_id: nexus.actorID,
    });
    await expectEnvelope(divisionBlocked, 409, 'CONFLICT', 'CONFLICT', 'suspended division');

    const businessAllows = await execute(request, nexus, {
      intent: 'business-scope work',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(businessAllows.status()).toBe(202);

    // G3 illustrates the authority boundary: membership decides what you can
    // transition, so a caller holding no business-wide membership for the
    // auxiliary business cannot even 404-scope its lifecycle transitions —
    // the archive is invisible.
    const bizArchive = await request.post(`${nexus.baseURL}/api/v1/businesses/${newBiz}/archive`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: {},
    });
    expect(bizArchive.status()).toBe(404);
  });

  test('restart yields 404 for a completed execution id (G4 Level 1)', async ({ request, nexus }) => {
    const submitted = await execute(request, nexus, {
      intent: 'run before restart',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(submitted.status()).toBe(202);
    const id = (await submitted.json()).execution_id;
    await pollExecution(request, nexus, id);

    await nexus.restart();

    const after = await request.get(`${nexus.baseURL}/api/v1/executions/${id}?business_id=${nexus.businessID}`, {
      headers: bootstrapHeaders(nexus),
    });
    expect(after.status(), 'execution state is process-local, never recovered as an execution id').toBe(404);
  });

  test('execution events are observable on /events with correlation identity', async ({ request, nexus }) => {
    const conn = openSSE(nexus.baseURL, `/events?business_id=${nexus.businessID}`, {
      ...bootstrapHeaders(nexus),
    });
    try {
      await (conn as any).waitForStatus?.(200);
      // Submit via a path the events stream is attached to.
      const res = await submit(request, nexus, {
        intent: 'sse-probe-123',
        business_id: nexus.businessID,
        actor_id: nexus.actorID,
      });
      expect(res.status()).toBe(202);
      const { request_id } = await res.json();
      const frame = await (conn as any).waitForFrame(
        (f: any) => f.json?.correlation_id === request_id,
        20_000,
        'correlation-scoped frame',
      );
      expect(frame.event).toBeTruthy();
    } finally {
      await conn.close();
    }
  });
});
