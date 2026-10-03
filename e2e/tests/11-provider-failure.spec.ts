import { test as base, expect } from '@playwright/test';
import { startNexus, NexusHandle } from '../fixtures/nexus';
import { expectEnvelope, pollResult, submit } from '../fixtures/api';

/**
 * Provider failure end-to-end — a terminal failed outcome that is honestly
 * reported, driven only through HTTP against the real binary.
 *
 * Discovered contract (contracts/PROVIDER_CONTRACTS.md §12 addendum,
 * docs/m6-model-router.md "Configuration"):
 *   - `NEXUS_SEEDED_PROVIDER_STATUS=offline` starts the launcher-seeded
 *     `simulated` provider offline; it is the only configuration surface that
 *     makes a provider failure reachable without registering a real provider,
 *     and it selects an existing health state rather than adding one;
 *   - the gateway still boots and serves — the failure is a result, not a
 *     crash, so /ready and the control plane stay up;
 *   - the request ends terminal `failed` with EXECUTION_FAILED /
 *     INTERNAL_FAILURE / chain_step=agent (CORE §3) and never `completed`,
 *     which is the no-false-success rule (E-008);
 *   - `outcome.metrics.executor_status` is `failed` and the audit trace
 *     records the executor step, so the failure is attributable without a new
 *     field;
 *   - the same request submitted again fails the same way: the state is
 *     deterministic, not a one-off race.
 *
 * The harness still owns every credential, so this file starts its own
 * worker-scoped gateway instead of changing the shared one's environment.
 */

const test = base.extend<{ offlineNexus: NexusHandle }>({
  offlineNexus: [
    async ({}, use) => {
      const handle = await startNexus({
        extraEnv: { NEXUS_SEEDED_PROVIDER_STATUS: 'offline' },
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

test.describe('PROVIDER FAILURE', () => {
  test('a request against an offline seeded provider ends failed, never completed', async ({
    request,
    offlineNexus,
  }) => {
    const submitted = await submit(request, offlineNexus, {
      intent: 'e2e provider offline',
      business_id: offlineNexus.businessID,
      actor_id: offlineNexus.actorID,
    });
    expect(submitted.status(), 'admission is independent of provider health').toBe(202);
    const { request_id: requestID } = await submitted.json();

    const result = await pollResult(
      request,
      offlineNexus,
      requestID,
      offlineNexus.businessID,
    );

    // No false success: the terminal state is failed, and no summary was
    // fabricated to look like work.
    expect(result.status).toBe('failed');
    expect(result.outcome?.summary ?? '', 'no fabricated output').toBe('');
    expect(result.request_id).toBe(requestID);
    expect(result.business_id).toBe(offlineNexus.businessID);

    // The contract envelope for a provider failure (PROVIDER_CONTRACTS §12.2).
    expect(result.error?.code).toBe('EXECUTION_FAILED');
    expect(result.error?.category).toBe('INTERNAL_FAILURE');
    expect(result.error?.chain_step).toBe('agent');
    expect(result.error?.retryable).toBe(false);
    expect(result.error?.message).toContain('provider invocation failed');
    expect(result.error?.message).toContain('provider simulated is offline');

    // The executor's own verdict is on the record.
    expect(result.outcome?.metrics?.executor_status).toBe('failed');
    const agentEntry = (result.audit_trace ?? []).find(
      (entry: any) =>
        entry?.step === 'agent' && String(entry?.outcome ?? '').includes('status=failed'),
    );
    expect(agentEntry, 'audit trail records the failed executor step').toBeTruthy();
    const verifyEntry = (result.audit_trace ?? []).find(
      (entry: any) => entry?.step === 'verify' && String(entry?.outcome ?? '') === 'status=failed',
    );
    expect(verifyEntry, 'audit trail verifies the failed outcome').toBeTruthy();
  });

  test('the provider failure is deterministic and the gateway keeps serving', async ({
    request,
    offlineNexus,
  }) => {
    // The gateway is not the thing that failed: it is still ready.
    const ready = await request.get(`${offlineNexus.baseURL}/ready`);
    expect(ready.status(), 'gateway stays ready').toBe(200);
    expect((await ready.json()).status).toBe('ready');
    const status = await request.get(`${offlineNexus.baseURL}/api/v1/control/status`, {
      headers: { 'X-API-Key': offlineNexus.controlKey },
    });
    expect(status.status()).toBe(200);
    expect((await status.json()).status).toBe('RUNNING');

    // Two more submissions fail the same way — deterministic, not a race.
    for (const attempt of ['first', 'second']) {
      const submitted = await submit(request, offlineNexus, {
        intent: `e2e provider offline ${attempt}`,
        business_id: offlineNexus.businessID,
        actor_id: offlineNexus.actorID,
      });
      expect(submitted.status()).toBe(202);
      const { request_id: requestID } = await submitted.json();
      const result = await pollResult(
        request,
        offlineNexus,
        requestID,
        offlineNexus.businessID,
      );
      expect(result.status, `${attempt} attempt must fail, not complete`).toBe('failed');
      expect(result.error?.code).toBe('EXECUTION_FAILED');
      expect(result.error?.category).toBe('INTERNAL_FAILURE');
      expect(result.outcome?.metrics?.executor_status).toBe('failed');
    }

    // Failures stay in their own category: authentication and authorization
    // still answer normally, so a provider outage cannot be mistaken for a
    // rejection (and vice versa).
    const unauth = await request.get(
      `${offlineNexus.baseURL}/api/v1/identities/${offlineNexus.actorID}`,
    );
    await expectEnvelope(unauth, 401, 'UNAUTHORIZED', 'AUTH', 'unauth on offline gateway');
  });
});
