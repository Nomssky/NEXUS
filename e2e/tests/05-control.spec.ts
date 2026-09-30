import { test, expect } from '../fixtures/test';
import {
  bootstrapHeaders,
  controlHeaders,
  expectEnvelope,
  pollResult,
  submit,
} from '../fixtures/api';

/**
 * §11 — the control surface, in one test so the engine is always restored.
 *
 * Discovered contract (internal/gateway/server.go):
 *   GET  /api/v1/control/status   X-API-Key -> 200 ControlStatusResponse
 *   POST /api/v1/control/pause    running -> 200 {status:"paused"}; paused -> 409 ALREADY_PAUSED
 *   POST /api/v1/control/resume   paused  -> 200 {status:"resumed"}; running -> 409 ALREADY_RUNNING
 *   while paused: GET /ready -> 503, POST /api/v1/requests -> 503 RESOURCE_UNAVAILABLE
 *   control auth is X-API-Key: wrong/missing -> 401 UNAUTHORIZED
 */
test.describe('PAUSE/RESUME', () => {
  test('pause blocks admission and resume restores it', async ({ request, nexus }) => {
    // Bad control credentials first, while the engine is still running.
    const noKey = await request.get(`${nexus.baseURL}/api/v1/control/status`);
    await expectEnvelope(noKey, 401, 'UNAUTHORIZED', 'AUTH', 'control without key');

    const badKey = await request.get(`${nexus.baseURL}/api/v1/control/status`, {
      headers: { 'X-API-Key': 'definitely-not-the-key' },
    });
    await expectEnvelope(badKey, 401, 'UNAUTHORIZED', 'AUTH', 'control with wrong key');

    const statusBefore = await request.get(`${nexus.baseURL}/api/v1/control/status`, {
      headers: controlHeaders(nexus),
    });
    expect(statusBefore.status()).toBe(200);
    const before = await statusBefore.json();
    expect(before.status).toBe('RUNNING');
    expect(before.components.engine).toBe('RUNNING');
    expect(typeof before.request_count).toBe('number');

    let paused = false;
    try {
      const pause = await request.post(`${nexus.baseURL}/api/v1/control/pause`, {
        headers: controlHeaders(nexus),
      });
      expect(pause.status()).toBe(200);
      const pauseBody = await pause.json();
      expect(pauseBody.status).toBe('paused');
      expect(typeof pauseBody.message).toBe('string');
      paused = true;

      // Idempotent pause is a conflict, not a second 200 or a 500.
      const pauseAgain = await request.post(`${nexus.baseURL}/api/v1/control/pause`, {
        headers: controlHeaders(nexus),
      });
      const conflict = await expectEnvelope(
        pauseAgain,
        409,
        'ALREADY_PAUSED',
        'CONFLICT',
        'pause twice',
      );
      // CORE §3 derives retryable from the category; CONFLICT defaults true.
      expect(conflict.retryable).toBe(true);

      // The gateway is still up: health stays 200 while readiness drops.
      const health = await request.get(`${nexus.baseURL}/health`);
      expect(health.status(), 'health endpoint stays healthy while paused').toBe(200);

      const ready = await request.get(`${nexus.baseURL}/ready`);
      expect(ready.status(), 'readiness drops while paused').toBe(503);
      expect((await ready.json()).status).toBe('not ready');

      // Admission must fail closed with a retryable envelope, never 202.
      const blocked = await submit(request, nexus, {
        intent: 'submitted while paused',
        business_id: nexus.businessID,
        actor_id: nexus.actorID,
      });
      const blockedEnv = await expectEnvelope(
        blocked,
        503,
        'RESOURCE_UNAVAILABLE',
        'RESOURCE_UNAVAILABLE',
        'submit while paused',
      );
      expect(blockedEnv.retryable).toBe(true);

      // Control status reflects the stopped engine.
      const statusPaused = await request.get(`${nexus.baseURL}/api/v1/control/status`, {
        headers: controlHeaders(nexus),
      });
      expect(statusPaused.status()).toBe(200);
      const pausedBody = await statusPaused.json();
      expect(pausedBody.status).toBe('STOPPED');
      expect(pausedBody.components.engine).toBe('STOPPED');

      const resume = await request.post(`${nexus.baseURL}/api/v1/control/resume`, {
        headers: controlHeaders(nexus),
      });
      expect(resume.status()).toBe(200);
      const resumeBody = await resume.json();
      expect(resumeBody.status).toBe('resumed');
      paused = false;

      const resumeAgain = await request.post(`${nexus.baseURL}/api/v1/control/resume`, {
        headers: controlHeaders(nexus),
      });
      await expectEnvelope(
        resumeAgain,
        409,
        'ALREADY_RUNNING',
        'CONFLICT',
        'resume twice',
      );

      const readyAgain = await request.get(`${nexus.baseURL}/ready`);
      expect(readyAgain.status()).toBe(200);
      expect((await readyAgain.json()).status).toBe('ready');

      // End to end after the resume: admission and execution both work.
      const accepted = await submit(request, nexus, {
        intent: 'submitted after resume',
        business_id: nexus.businessID,
        actor_id: nexus.actorID,
      });
      expect(accepted.status(), 'admission works again after resume').toBe(202);
      const { request_id } = await accepted.json();
      const result = await pollResult(request, nexus, request_id, nexus.businessID);
      expect(result.status).toBe('completed');
    } finally {
      if (paused) {
        const restore = await request.post(`${nexus.baseURL}/api/v1/control/resume`, {
          headers: controlHeaders(nexus),
        });
        expect(restore.status(), 'engine restored in finally').toBe(200);
      }
    }
  });

  test('the gateway never advertises readiness without a running engine', async ({
    request,
    nexus,
  }) => {
    // A regression guard for the paused-engine behaviour: /ready and /health
    // must not be conflated. /health is process liveness, /ready is engine
    // state, and only the latter is allowed to answer 503.
    const [health, ready] = await Promise.all([
      request.get(`${nexus.baseURL}/health`),
      request.get(`${nexus.baseURL}/ready`),
    ]);
    expect(health.status()).toBe(200);
    expect(ready.status()).toBe(200);
    expect((await ready.json()).status).toBe('ready');
    expect(bootstrapHeaders(nexus)['X-Actor-ID']).toBe(nexus.actorID);
  });
});
