import { test, expect } from '../fixtures/test';
import { bootstrapHeaders, expectEnvelope, randomSuffix } from '../fixtures/api';

/**
 * §9/§10 — unknown resources, method and route errors, and validation.
 *
 * Discovered contract (internal/gateway/server.go envelopeRoutes + handlers):
 *   - unrouted path      -> 404 code VALIDATION, message
 *                           "no such endpoint: <METHOD> <path>"
 *   - wrong method       -> 405 code METHOD_NOT_ALLOWED (category VALIDATION),
 *                           keeping the mux's Allow header
 *   - unknown request id -> 404 code VALIDATION "request not found"
 *   - missing business_id-> 400 code VALIDATION
 * There is no NOT_FOUND code and no bare non-JSON error response.
 */
test.describe('UNKNOWN RESOURCE', () => {
  test('unknown request id -> 404 envelope with code VALIDATION', async ({ request, nexus }) => {
    const unknown = `req-${randomSuffix()}`;
    const res = await request.get(
      `${nexus.baseURL}/api/v1/requests/${unknown}?business_id=${nexus.businessID}`,
      { headers: bootstrapHeaders(nexus) },
    );
    const e = await expectEnvelope(res, 404, 'VALIDATION', 'VALIDATION', 'unknown request id');
    expect(e.message).toBe('request not found');
    expect(e.retryable).toBe(false);
  });

  test('unknown request id never leaks another scope', async ({ request, nexus }) => {
    // A result id that does not exist in this business must not be
    // distinguishable from a typo: 404 VALIDATION either way.
    const res = await request.get(
      `${nexus.baseURL}/api/v1/requests/does-not-exist?business_id=${nexus.businessID}`,
      { headers: bootstrapHeaders(nexus) },
    );
    expect(res.status()).toBe(404);
    const body = await res.json();
    expect(body.error.code).toBe('VALIDATION');
  });
});

test.describe('METHOD/ROUTE ERROR', () => {
  test('unrouted path on the real gateway -> 404 VALIDATION', async ({ request, nexus }) => {
    const res = await request.get(`${nexus.baseURL}/nope`);
    const e = await expectEnvelope(res, 404, 'VALIDATION', 'VALIDATION', 'unrouted path');
    expect(e.message).toContain('no such endpoint: GET /nope');
  });

  test('routed path with the wrong method -> 405 with an Allow header', async ({ request, nexus }) => {
    const res = await request.delete(`${nexus.baseURL}/api/v1/requests`, {
      headers: bootstrapHeaders(nexus),
    });
    const e = await expectEnvelope(
      res,
      405,
      'METHOD_NOT_ALLOWED',
      'VALIDATION',
      'DELETE on a POST-only route',
    );
    // The 405 message is the mux's own text (passed through the envelope);
    // the 404 branch is the one that says "no such endpoint: ...".
    expect(e.message).toContain('DELETE');
    expect(e.message).toContain('/api/v1/requests');
    const allow = res.headers()['allow'];
    expect(allow, 'Allow header is preserved from the mux').toBeTruthy();
    expect(allow).toContain('POST');
  });

  test('wrong method on the SSE route -> 405, not a hung stream', async ({ request, nexus }) => {
    const res = await request.post(`${nexus.baseURL}/events`, {
      headers: bootstrapHeaders(nexus),
      data: {},
    });
    await expectEnvelope(res, 405, 'METHOD_NOT_ALLOWED', 'VALIDATION', 'POST /events');
  });
});

test.describe('VALIDATION', () => {
  test('result without business_id -> 400 VALIDATION', async ({ request, nexus }) => {
    const res = await request.get(`${nexus.baseURL}/api/v1/requests/req-any`, {
      headers: bootstrapHeaders(nexus),
    });
    const e = await expectEnvelope(res, 400, 'VALIDATION', 'VALIDATION', 'missing business_id');
    expect(e.message).toBe('business_id required');
  });

  test('submit with a missing intent -> 400 VALIDATION', async ({ request, nexus }) => {
    const res = await request.post(`${nexus.baseURL}/api/v1/requests`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: { business_id: nexus.businessID, actor_id: nexus.actorID },
    });
    await expectEnvelope(res, 400, 'VALIDATION', 'VALIDATION', 'missing intent');
  });

  test('submit with a missing actor_id -> 400 VALIDATION', async ({ request, nexus }) => {
    const res = await request.post(`${nexus.baseURL}/api/v1/requests`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: { intent: 'no actor', business_id: nexus.businessID },
    });
    await expectEnvelope(res, 400, 'VALIDATION', 'VALIDATION', 'missing actor_id');
  });

  test('submit with a missing business_id -> 400 VALIDATION', async ({ request, nexus }) => {
    const res = await request.post(`${nexus.baseURL}/api/v1/requests`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: { intent: 'no business', actor_id: nexus.actorID },
    });
    await expectEnvelope(res, 400, 'VALIDATION', 'VALIDATION', 'missing business_id');
  });

  test('every error response is JSON with a correlation id in the body and header', async ({
    request,
    nexus,
  }) => {
    const res = await request.get(`${nexus.baseURL}/api/v1/requests/nope?business_id=${nexus.businessID}`, {
      headers: bootstrapHeaders(nexus),
    });
    expect(res.status()).toBe(404);
    expect(res.headers()['content-type']).toContain('application/json');
    expect(res.headers()['x-correlation-id']).toBeTruthy();
    const body = await res.json();
    expect(typeof body.error.correlation_id).toBe('string');
    expect(body.error.correlation_id.length).toBeGreaterThan(0);
    expect(body.error.timestamp).toBeTruthy();
    expect(typeof body.error.retryable).toBe('boolean');
    expect(typeof body.error.category).toBe('string');
    expect(typeof body.error.code).toBe('string');
    expect(typeof body.error.message).toBe('string');
    expect(res.headers()['x-correlation-id']).toBe(body.error.correlation_id);
  });
});
