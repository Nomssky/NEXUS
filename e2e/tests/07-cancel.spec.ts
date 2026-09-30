import { test, expect } from '../fixtures/test';
import {
  bootstrapHeaders,
  expectEnvelope,
  randomSuffix,
  submit,
} from '../fixtures/api';

interface Cancellation {
  request_id: string;
  correlation_id: string;
  status: string;
}

async function cancel(
  request: any,
  nexus: any,
  id: string,
  businessID: string,
  headers: Record<string, string> = bootstrapHeaders(nexus),
) {
  return request.post(
    `${nexus.baseURL}/api/v1/requests/${id}/cancel?business_id=${encodeURIComponent(businessID)}`,
    { headers },
  );
}

/**
 * §13 — cancellation over the public boundary.
 *
 * Discovered contract (internal/core/cancel.go + handleCancelRequest):
 *   POST /api/v1/requests/{id}/cancel?business_id=...
 *     -> 202 {request_id, correlation_id, status:"cancelling"}   accepted/queued
 *     -> 202 on an already-cancelled result (idempotent repeat cancel)
 *     -> 404 VALIDATION     unknown request id
 *     -> 403 AUTHORIZATION  business scope mismatch on a known id
 *     -> 409 CONFLICT       request already in a non-cancellable terminal state
 *     -> 400 VALIDATION     business_id missing
 * The engine's dispatch loop executes admitted requests serially, so a burst
 * keeps the last admitted request queued long enough to cancel it
 * deterministically.
 */
test.describe('CANCEL', () => {
  test('cancelling a queued request yields a cancelled terminal result', async ({
    request,
    nexus,
  }) => {
    const burstSize = 40;
    const burst: string[] = [];
    const submissions = await Promise.all(
      Array.from({ length: burstSize }, (_, i) =>
        submit(request, nexus, {
          intent: `e2e cancel burst ${i}`,
          business_id: nexus.businessID,
          actor_id: nexus.actorID,
        }),
      ),
    );
    for (const res of submissions) {
      expect(res.status()).toBe(202);
      burst.push((await res.json()).request_id);
    }

    // The target is admitted strictly after the burst, so it is the last
    // request in the queue when it is cancelled.
    const target = await submit(request, nexus, {
      intent: 'e2e cancel target',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(target.status()).toBe(202);
    const { request_id } = await target.json();

    const cancelRes = await cancel(request, nexus, request_id, nexus.businessID);
    expect(cancelRes.status(), 'cancel of a queued request').toBe(202);
    const body = (await cancelRes.json()) as Cancellation;
    expect(body.request_id).toBe(request_id);
    expect(body.correlation_id).toBe(request_id);
    expect(body.status).toBe('cancelling');
    expect(cancelRes.headers()['x-correlation-id']).toBe(request_id);

    // Poll to a terminal state: the target must be cancelled, never completed.
    const deadline = Date.now() + 25_000;
    let lastStatus = 'unknown';
    let lastBody: any;
    while (Date.now() < deadline) {
      const res = await request.get(
        `${nexus.baseURL}/api/v1/requests/${request_id}?business_id=${nexus.businessID}`,
        { headers: bootstrapHeaders(nexus) },
      );
      if (res.status() === 200) {
        lastBody = await res.json();
        lastStatus = lastBody.status;
        if (lastStatus === 'cancelled' || lastStatus === 'completed' || lastStatus === 'failed') {
          break;
        }
      } else if (res.status() === 404) {
        lastStatus = '404';
        lastBody = await res.json();
        break;
      } else {
        lastStatus = `HTTP ${res.status()}`;
        lastBody = await res.json().catch(() => ({}));
      }
      await new Promise((r) => setTimeout(r, 50));
    }
    expect(lastStatus, `terminal state of the cancelled request; last body: ${JSON.stringify(lastBody)}`).toBe(
      'cancelled',
    );
    expect(lastBody.request_id).toBe(request_id);
    expect(lastBody.business_id).toBe(nexus.businessID);
    expect(Array.isArray(lastBody.audit_trace)).toBe(true);

    // A cancelled result never masquerades as success.
    expect(lastBody.outcome?.summary ?? lastBody.status).not.toBe('completed');

    // Repeat cancel on a cancelled result is idempotent: 202, not 409/500.
    const repeat = await cancel(request, nexus, request_id, nexus.businessID);
    expect(repeat.status(), 'repeat cancel is idempotent').toBe(202);
    const repeatBody = (await repeat.json()) as Cancellation;
    expect(repeatBody.request_id).toBe(request_id);
    expect(repeatBody.status).toBe('cancelling');
  });

  test('a completed request cannot be cancelled -> 409 CONFLICT', async ({ request, nexus }) => {
    const res = await submit(request, nexus, {
      intent: 'e2e cancel after completion',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(res.status()).toBe(202);
    const { request_id } = await res.json();

    const deadline = Date.now() + 25_000;
    let status = '';
    while (Date.now() < deadline) {
      const got = await request.get(
        `${nexus.baseURL}/api/v1/requests/${request_id}?business_id=${nexus.businessID}`,
        { headers: bootstrapHeaders(nexus) },
      );
      if (got.status() === 200) {
        status = (await got.json()).status;
        if (status === 'completed' || status === 'failed' || status === 'cancelled') break;
      }
      await new Promise((r) => setTimeout(r, 50));
    }
    expect(status).toBe('completed');

    const cancelRes = await cancel(request, nexus, request_id, nexus.businessID);
    const e = await expectEnvelope(
      cancelRes,
      409,
      'CONFLICT',
      'CONFLICT',
      'cancel a completed request',
    );
    expect(e.message).toContain('request already in terminal state: completed');
    // CORE §3 derives retryable from the category; CONFLICT defaults true.
    expect(e.retryable).toBe(true);

    // The result is untouched by the rejected cancellation.
    const after = await request.get(
      `${nexus.baseURL}/api/v1/requests/${request_id}?business_id=${nexus.businessID}`,
      { headers: bootstrapHeaders(nexus) },
    );
    expect(after.status()).toBe(200);
    expect((await after.json()).status).toBe('completed');
  });

  test('cancel errors use the contract envelope', async ({ request, nexus }) => {
    const unknown = `req-${randomSuffix()}`;

    const notFound = await cancel(request, nexus, unknown, nexus.businessID);
    const e404 = await expectEnvelope(
      notFound,
      404,
      'VALIDATION',
      'VALIDATION',
      'cancel unknown request',
    );
    expect(e404.message).toBe('request not found');

    const noScope = await request.post(
      `${nexus.baseURL}/api/v1/requests/${unknown}/cancel`,
      { headers: bootstrapHeaders(nexus) },
    );
    await expectEnvelope(noScope, 400, 'VALIDATION', 'VALIDATION', 'cancel without business_id');

    // A known request id, cancelled under another business's scope.
    const res = await submit(request, nexus, {
      intent: 'e2e cancel scope check',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(res.status()).toBe(202);
    const { request_id } = await res.json();

    const wrongScope = await cancel(request, nexus, request_id, `foreign-${randomSuffix()}`);
    await expectEnvelope(
      wrongScope,
      403,
      'AUTHORIZATION',
      'AUTHORIZATION',
      'cancel under a foreign scope',
    );
  });

  test('unauthenticated cancel -> 401', async ({ request, nexus }) => {
    const res = await request.post(
      `${nexus.baseURL}/api/v1/requests/req-any/cancel?business_id=${nexus.businessID}`,
    );
    await expectEnvelope(res, 401, 'UNAUTHORIZED', 'AUTH', 'cancel without credentials');
  });
});
