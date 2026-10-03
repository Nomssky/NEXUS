import * as fs from 'node:fs';
import * as path from 'node:path';
import { test, expect } from '../fixtures/test';
import {
  actorHeaders,
  bootstrapHeaders,
  expectEnvelope,
  pollResult,
  randomSuffix,
  submit,
} from '../fixtures/api';

/**
 * §14 — restart against the same NEXUS_DATA_DIR.
 *
 * Discovered contract (SCHEMA_IDENTITIES_ORG §10 addendum):
 *   - records are persisted by the file store at
 *       <NEXUS_DATA_DIR>/identity/<id>.json and <NEXUS_DATA_DIR>/business/<id>.json,
 *       plus the namespaced credential/membership records
 *       <NEXUS_DATA_DIR>/credential/credential:<identity id>.json and
 *       <NEXUS_DATA_DIR>/membership/membership:<identity id>.json;
 *   - the bootstrap identity, its credential and its membership are re-armed
 *     on every boot from NEXUS_BOOTSTRAP_CREDENTIAL / NEXUS_BOOTSTRAP_BUSINESS,
 *     so the same credential keeps working across a restart;
 *   - a credential registered through POST /api/v1/identities and the
 *     membership that call creates are hydrated on the next boot too, so a
 *     non-bootstrap identity authenticates and reaches scoped endpoints after
 *     a restart without being re-created;
 *   - the credential record carries a verification hash, never the raw value;
 *   - the gateway keeps serving authenticated requests afterwards.
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

    // The credential and membership written by that same call are on disk,
    // and the credential file holds the hash rather than the secret.
    // Record ids are unique store-wide, so both records are namespaced by
    // their type: <type>:<identity id>.
    const credentialRecord = path.join(nexus.dataDir, 'credential', `credential:${secondActor}.json`);
    const membershipRecord = path.join(nexus.dataDir, 'membership', `membership:${secondActor}.json`);
    expect(fs.existsSync(credentialRecord), `${credentialRecord} exists`).toBe(true);
    expect(fs.existsSync(membershipRecord), `${membershipRecord} exists`).toBe(true);
    const credentialRaw = fs.readFileSync(credentialRecord, 'utf8');
    expect(credentialRaw, 'no raw credential on disk').not.toContain(secondCredential);
    const credentialOnDisk = JSON.parse(credentialRaw);
    expect(credentialOnDisk.type).toBe('credential');
    const credentialPayload = JSON.parse(Buffer.from(credentialOnDisk.data, 'base64').toString('utf8'));
    expect(credentialPayload.entity_type).toBe('credential');
    expect(credentialPayload.identity_id).toBe(secondActor);
    expect(credentialPayload.method).toBe('password');
    expect(typeof credentialPayload.hash).toBe('string');

    const membershipOnDisk = JSON.parse(fs.readFileSync(membershipRecord, 'utf8'));
    expect(membershipOnDisk.type).toBe('membership');
    // One record per identity, so the envelope carries no single business id.
    expect(membershipOnDisk.business_id ?? '').toBe('');
    const membershipPayload = JSON.parse(Buffer.from(membershipOnDisk.data, 'base64').toString('utf8'));
    expect(membershipPayload.entity_type).toBe('membership');
    expect(membershipPayload.identity_id).toBe(secondActor);
    expect(
      membershipPayload.memberships.some((m: any) => m.business_id === nexus.businessID),
      'the membership this identity was created with is stored',
    ).toBe(true);

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

    // 9. The identity created before the restart authenticates with its own
    //    credential — the hash was hydrated, not re-armed from anywhere —
    //    and reaches a scoped endpoint, which also proves its membership
    //    hydrated (a credential alone is not enough to pass the scope check).
    const secondHeaders = actorHeaders(secondActor, secondCredential);
    const selfRead = await request.get(
      `${nexus.baseURL}/api/v1/identities/${secondActor}`,
      { headers: secondHeaders },
    );
    expect(selfRead.status(), 'API-registered credential survives a restart').toBe(200);
    expect((await selfRead.json()).entity_id).toBe(secondActor);

    const scoped = await request.get(
      `${nexus.baseURL}/api/v1/identities?business_id=${nexus.businessID}`,
      { headers: secondHeaders },
    );
    expect(scoped.status(), 'membership survives a restart').toBe(200);

    // A wrong credential is still rejected after hydration (the stored hash
    // does not widen what authenticates).
    const wrong = await request.get(
      `${nexus.baseURL}/api/v1/identities/${secondActor}`,
      { headers: actorHeaders(secondActor, `${secondCredential}-wrong`) },
    );
    await expectEnvelope(wrong, 401, 'UNAUTHORIZED', 'AUTH', 'wrong credential after restart');
  });
});
