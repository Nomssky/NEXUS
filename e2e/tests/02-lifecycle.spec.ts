import { test, expect } from '../fixtures/test';
import {
  bootstrapHeaders,
  pollResult,
  randomSuffix,
  submit,
} from '../fixtures/api';

/**
 * §6/§7 — the happy-path lifecycle through the public boundary.
 *
 * Discovered contract:
 *   POST /api/v1/requests {intent, business_id, actor_id, priority?, constraints?}
 *     -> 202 {request_id, correlation_id, status:"accepted", actor_id}
 *   GET  /api/v1/requests/{id}?business_id=...
 *     -> 202 {request_id, correlation_id, status:"pending"}  (admitted, running)
 *     -> 200 {request_id, business_id, status, outcome?, error?, audit_trace, duration}
 *     -> 404 envelope (unknown id)
 *
 * The core processes its queue serially, so a burst keeps later requests in
 * the pending state long enough to observe the 202 deterministically.
 */
test.describe('HAPPY PATH', () => {
  test('submit is accepted with a request id and matching correlation id', async ({
    request,
    nexus,
  }) => {
    const res = await submit(request, nexus, {
      intent: 'e2e happy path',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(res.status(), 'submit status').toBe(202);
    expect(res.headers()['content-type']).toContain('application/json');
    const body = await res.json();
    expect(body.request_id, 'request_id').toBeTruthy();
    expect(body.correlation_id, 'correlation_id').toBe(body.request_id);
    expect(body.status).toBe('accepted');
    expect(body.actor_id).toBe(nexus.actorID);
    expect(res.headers()['x-correlation-id']).toBe(body.request_id);
  });

  test('a client-supplied correlation id is honoured', async ({ request, nexus }) => {
    const corr = `e2e-corr-${randomSuffix()}`;
    const res = await request.post(`${nexus.baseURL}/api/v1/requests`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json', 'X-Correlation-ID': corr },
      data: {
        intent: 'e2e correlation echo',
        business_id: nexus.businessID,
        actor_id: nexus.actorID,
      },
    });
    expect(res.status()).toBe(202);
    const body = await res.json();
    expect(body.request_id).toBe(corr);
    expect(res.headers()['x-correlation-id']).toBe(corr);

    const result = await pollResult(request, nexus, corr, nexus.businessID);
    expect(result.status).toBe('completed');
    expect(result.request_id).toBe(corr);
  });

  test('a burst observes 202 pending and every request reaches completed', async ({
    request,
    nexus,
  }) => {
    const ids: string[] = [];
    const submissions = await Promise.all(
      Array.from({ length: 12 }, (_, i) =>
        submit(request, nexus, {
          intent: `e2e burst ${i}`,
          business_id: nexus.businessID,
          actor_id: nexus.actorID,
        }),
      ),
    );
    for (const res of submissions) {
      expect(res.status()).toBe(202);
      ids.push((await res.json()).request_id);
    }

    // Poll every admitted id immediately: at least one must still be running.
    let pendingSeen = 0;
    for (const id of ids) {
      const res = await request.get(
        `${nexus.baseURL}/api/v1/requests/${id}?business_id=${nexus.businessID}`,
        { headers: bootstrapHeaders(nexus) },
      );
      if (res.status() === 202) {
        pendingSeen++;
        const body = await res.json();
        expect(body.status, 'pending body status').toBe('pending');
        expect(body.request_id).toBe(id);
        expect(body.correlation_id, 'pending correlation id').toBeTruthy();
      } else {
        expect(res.status(), 'in-flight GET is 202 or 200, never 404').toBe(200);
      }
    }
    expect(pendingSeen, 'the documented 202 pending state was observed').toBeGreaterThan(0);

    for (const id of ids) {
      const result = await pollResult(request, nexus, id, nexus.businessID);
      expect(result.status, `terminal status of ${id}`).toBe('completed');
      expect(result.error, `completed result must not carry an error for ${id}`).toBeUndefined();
    }
  });

  test('a completed result carries the contract result shape and no false success', async ({
    request,
    nexus,
  }) => {
    const submitted = await submit(request, nexus, {
      intent: 'e2e result shape',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
      priority: 5,
      constraints: ['budget:1000'],
    });
    expect(submitted.status()).toBe(202);
    const { request_id } = await submitted.json();

    const result = await pollResult(request, nexus, request_id, nexus.businessID);

    expect(result.request_id).toBe(request_id);
    expect(result.business_id).toBe(nexus.businessID);
    expect(result.status).toBe('completed');
    expect(result.error).toBeUndefined();
    expect(result.outcome, 'a completed result has a structured outcome').toBeTruthy();
    expect(typeof result.outcome!.summary).toBe('string');
    expect(result.outcome!.summary!.length).toBeGreaterThan(0);
    expect(Array.isArray(result.audit_trace), 'audit_trace is an array').toBe(true);
    expect(result.audit_trace!.length, 'audit_trace is populated').toBeGreaterThan(0);
    expect(typeof result.duration).toBe('number');
    // A completed response must not masquerade as an error envelope.
    expect((result as any).code).toBeUndefined();
    expect((result as any).message).toBeUndefined();
  });
});
