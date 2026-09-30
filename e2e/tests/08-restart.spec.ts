import * as fs from 'node:fs';
import * as path from 'node:path';
import { test, expect } from '../fixtures/test';
import {
  bootstrapHeaders,
  expectEnvelope,
  pollResult,
  randomSuffix,
  submit,
} from '../fixtures/api';

/**
 * §14 — restart against the same NEXUS_DATA_DIR.
 *
 * Discovered contract:
 *   - records are persisted by the file store at
 *       <NEXUS_DATA_DIR>/identity/<id>.json and <NEXUS_DATA_DIR>/business/<id>.json
 *   - the bootstrap identity and its membership are re-armed on every boot
 *     from NEXUS_BOOTSTRAP_CREDENTIAL / NEXUS_BOOTSTRAP_BUSINESS, so the same
 *     credential keeps working across a restart;
 *   - the gateway keeps serving authenticated requests afterwards.
 *
 * Known, documented limitation (not asserted here): credentials registered
 * through POST /api/v1/identities live only in the in-process authenticator
 * and are not re-armed at boot, so those identities cannot authenticate after
 * a restart even though their records persist. See e2e/README.md.
 */
test.describe('RESTART/PERSISTENCE', () => {
  test('data, bootstrap credential and real traffic survive a restart', async ({
    request,
    nexus,
  }) => {
    const secondActor = `persist-${randomSuffix()}`;
    const secondCredential = `persist-cred-${Date.now().toString(36)}`;

    // 1. Real authenticated traffic before the restart.
    const created = await request.post(`${nexus.baseURL}/api/v1/identities`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: {
        entity_id: secondActor,
        identity_type: 'human',
        display_name: 'E2E Persisted Actor',
        business_id: nexus.businessID,
        credential: secondCredential,
        credential_method: 'password',
      },
    });
    expect(created.status(), `create identity: ${await created.text()}`).toBe(200);

    const before = await submit(request, nexus, {
      intent: 'e2e before restart',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(before.status()).toBe(202);
    const { request_id: beforeId } = await before.json();
    const beforeResult = await pollResult(request, nexus, beforeId, nexus.businessID);
    expect(beforeResult.status).toBe('completed');

    // 2. The records are on disk under NEXUS_DATA_DIR before the restart.
    const identityRecord = path.join(nexus.dataDir, 'identity', `${nexus.actorID}.json`);
    const businessRecord = path.join(nexus.dataDir, 'business', `${nexus.businessID}.json`);
    const secondRecord = path.join(nexus.dataDir, 'identity', `${secondActor}.json`);
    expect(fs.existsSync(identityRecord), `${identityRecord} exists`).toBe(true);
    expect(fs.existsSync(businessRecord), `${businessRecord} exists`).toBe(true);
    expect(fs.existsSync(secondRecord), `${secondRecord} exists`).toBe(true);
    const parsed = JSON.parse(fs.readFileSync(identityRecord, 'utf8'));
    expect(JSON.stringify(parsed)).toContain(nexus.actorID);

    // 3. Restart the real binary on the same data dir and ports.
    await nexus.restart();

    const health = await request.get(`${nexus.baseURL}/health`);
    expect(health.status(), 'gateway healthy after restart').toBe(200);
    const ready = await request.get(`${nexus.baseURL}/ready`);
    expect(ready.status(), 'engine ready after restart').toBe(200);
    expect((await ready.json()).status).toBe('ready');

    // 4. The bootstrap identity still authenticates with the same credential
    //    and still reads its own record.
    const identity = await request.get(`${nexus.baseURL}/api/v1/identities/${nexus.actorID}`, {
      headers: bootstrapHeaders(nexus),
    });
    expect(identity.status(), 'bootstrap credential survives restart').toBe(200);
    expect((await identity.json()).entity_id).toBe(nexus.actorID);

    const members = await request.get(
      `${nexus.baseURL}/api/v1/identities?business_id=${nexus.businessID}`,
      { headers: bootstrapHeaders(nexus) },
    );
    expect(members.status(), 'bootstrap membership is re-armed').toBe(200);

    // 5. The identity record created before the restart was hydrated.
    const persisted = await request.get(
      `${nexus.baseURL}/api/v1/identities/${secondActor}`,
      { headers: bootstrapHeaders(nexus) },
    );
    expect(persisted.status(), 'created identity record survives restart').toBe(200);
    expect((await persisted.json()).entity_id).toBe(secondActor);
    expect((await persisted.json()).status).toBe('active');

    // 6. Real work still completes after the restart.
    const after = await submit(request, nexus, {
      intent: 'e2e after restart',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(after.status(), 'admission works after restart').toBe(202);
    const { request_id: afterId } = await after.json();
    const afterResult = await pollResult(request, nexus, afterId, nexus.businessID);
    expect(afterResult.status).toBe('completed');
    expect(afterResult.request_id).toBe(afterId);
    expect(afterResult.error).toBeUndefined();
    expect(afterResult.audit_trace!.length).toBeGreaterThan(0);

    // 7. The control plane is live again too.
    const status = await request.get(`${nexus.baseURL}/api/v1/control/status`, {
      headers: { 'X-API-Key': nexus.controlKey },
    });
    expect(status.status()).toBe(200);
    expect((await status.json()).status).toBe('RUNNING');

    // 8. Unauthenticated access is still rejected after the restart.
    const unauth = await request.get(`${nexus.baseURL}/api/v1/identities/${nexus.actorID}`);
    await expectEnvelope(unauth, 401, 'UNAUTHORIZED', 'AUTH', 'unauth after restart');

    // 9. The second identity's record was hydrated (asserted in step 5), but
    //    its credential is only ever registered in the in-process
    //    authenticator, which starts empty on every boot. That is a known,
    //    documented limitation (e2e/README.md, "Deferred"), so it is
    //    deliberately not asserted here as either passing or failing.
  });
});
