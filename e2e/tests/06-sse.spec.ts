import { test, expect } from '../fixtures/test';
import { bootstrapHeaders, expectEnvelope, randomSuffix, submit } from '../fixtures/api';
import { openSSE, type SSEFrame } from '../fixtures/sse';

/** Wait until the SSE response headers have arrived (status 0 until then). */
async function waitForStatus(
  conn: ReturnType<typeof openSSE>,
  expected: number,
  timeoutMs = 10_000,
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (conn.statusCode !== 0) break;
    await new Promise((r) => setTimeout(r, 20));
  }
  expect(conn.statusCode, `SSE response status (raw: ${conn.raw.slice(0, 500)})`).toBe(expected);
}

function chainFramesFor(conn: ReturnType<typeof openSSE>, requestId: string): SSEFrame[] {
  return conn.frames.filter((f) => f.json?.correlation_id === requestId);
}

/**
 * §12 — the server-sent event stream.
 *
 * Discovered contract (internal/gateway/server.go handleSSE):
 *   GET /events?business_id=...   headers auth + membership required
 *     -> 200 text/event-stream, frames "event: <type>\ndata: <json>\n\n"
 *     -> 400 VALIDATION when business_id is missing, 403 when not a member
 *   Events are the §2.2 Event Record projection: entity_type "event",
 *   event_id, event_type, nexus_id, business_id, occurred_at, emitted_at,
 *   producer{producer_id,producer_type,module_name}, correlation_id, payload.
 *   chain.* events carry correlation_id == the request id and event_id
 *   "<request id>-<event type>", which is how a client correlates a run.
 */
test.describe('SSE', () => {
  test('stream delivers contract events correlated to a submitted request', async ({
    request,
    nexus,
  }) => {
    const conn = openSSE(nexus.baseURL, `/events?business_id=${nexus.businessID}`, {
      ...bootstrapHeaders(nexus),
    });
    try {
      await waitForStatus(conn, 200);
      expect(String(conn.headers['content-type'])).toContain('text/event-stream');

      const res = await submit(request, nexus, {
        intent: 'e2e sse observation',
        business_id: nexus.businessID,
        actor_id: nexus.actorID,
      });
      expect(res.status()).toBe(202);
      const { request_id } = await res.json();

      const started = await conn.waitForFrame(
        (f) => f.json?.correlation_id === request_id && f.event === 'chain.started',
        20_000,
        'chain.started frame for the submitted request',
      );
      expect(started.json!.event_type).toBe('chain.started');
      expect(started.json!.business_id).toBe(nexus.businessID);
      expect(started.json!.event_id).toBe(`${request_id}-chain.started`);
      expect(started.json!.entity_type).toBe('event');
      expect(started.json!.nexus_id, 'installation id is stamped').toBeTruthy();
      expect(started.json!.schema_version, 'schema version is stamped').toBeTruthy();
      expect(started.json!.occurred_at).toBeTruthy();
      expect(started.json!.emitted_at).toBeTruthy();
      expect(started.json!.payload, 'chain payload is a JSON object').toBeTruthy();

      const producer = started.json!.producer as Record<string, unknown>;
      expect(producer.producer_id).toBe('chain');
      expect(producer.producer_type).toBe('module');
      expect(producer.module_name).toBe('chain');

      // The run reaches its terminal event on the same stream.
      const completed = await conn.waitForFrame(
        (f) => f.json?.correlation_id === request_id && f.event === 'chain.completed',
        20_000,
        'chain.completed frame for the submitted request',
      );
      expect(completed.event).toBe(completed.json!.event_type as string);

      // Every correlated frame belongs to this business — no cross-scope leak.
      for (const frame of chainFramesFor(conn, request_id)) {
        expect(frame.json!.business_id).toBe(nexus.businessID);
        expect(frame.json!.correlation_id).toBe(request_id);
      }
      expect(chainFramesFor(conn, request_id).length).toBeGreaterThan(1);
    } finally {
      conn.close();
      await conn.closed;
    }
  });

  test('the stream stays open well past the 30s write timeout', async ({ request, nexus }) => {
    // server.WriteTimeout is 30s on the gateway; handleSSE clears it so a
    // stream is bounded per frame write, not by total connection age. An
    // idle stream therefore must still deliver events after 30s.
    const conn = openSSE(nexus.baseURL, `/events?business_id=${nexus.businessID}`, {
      ...bootstrapHeaders(nexus),
    });
    try {
      await waitForStatus(conn, 200);
      await new Promise((r) => setTimeout(r, 32_000));

      const res = await submit(request, nexus, {
        intent: 'e2e sse long stream',
        business_id: nexus.businessID,
        actor_id: nexus.actorID,
      });
      expect(res.status()).toBe(202);
      const { request_id } = await res.json();

      const frame = await conn.waitForFrame(
        (f) => f.json?.correlation_id === request_id,
        20_000,
        'event after the 30s write timeout',
      );
      expect(String(frame.event)).toContain('chain.');
      expect(conn.ended, 'the stream must still be open').toBe(false);
    } finally {
      conn.close();
      await conn.closed;
    }
  });

  test('SSE without business_id -> 400 VALIDATION', async ({ request, nexus }) => {
    const res = await request.get(`${nexus.baseURL}/events`, { headers: bootstrapHeaders(nexus) });
    await expectEnvelope(res, 400, 'VALIDATION', 'VALIDATION', 'SSE without business_id');
  });

  test('SSE for a business the caller is not a member of -> 403', async ({ request, nexus }) => {
    const foreign = `biz-${randomSuffix()}`;
    const created = await request.post(`${nexus.baseURL}/api/v1/businesses`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: { entity_id: foreign, name: 'SSE Foreign', owner_identity_id: nexus.actorID },
    });
    expect(created.status()).toBe(200);

    const res = await request.get(`${nexus.baseURL}/events?business_id=${foreign}`, {
      headers: bootstrapHeaders(nexus),
    });
    await expectEnvelope(res, 403, 'AUTHORIZATION', 'AUTHORIZATION', 'SSE foreign business');
  });
});
