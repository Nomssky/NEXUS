import { test, expect } from '../fixtures/test';
import { bootstrapHeaders, expectEnvelope, pollResult, randomSuffix, submit } from '../fixtures/api';

/**
 * G2 — lifecycle-aware admission (SCHEMA_IDENTITIES_ORG §12.2).
 *
 * Discovered contract over HTTP against the real binary:
 *   - business `suspended`/`archived` rejects new submits with
 *     409 CONFLICT "no new work is admitted", while existing results stay
 *     readable and cancellation stays open (status is not an authz status);
 *   - division `suspended`/`archived` rejects only that division's
 *     admissions — business-level submits still work;
 *   - `active` restores admission; nothing about existing work changes.
 */
const suffix = randomSuffix();

test.describe('LIFECYCLE-AWARE ADMISSION', () => {
  test('suspended business admits no new work but reads/cancels continue', async ({
    request,
    nexus,
  }) => {
    // A request admitted before the suspension keeps its terminal result.
    const before = await submit(request, nexus, {
      intent: 'g2 admitted before suspend',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(before.status()).toBe(202);
    const { request_id: beforeId } = await before.json();
    const done = await pollResult(request, nexus, beforeId, nexus.businessID);
    expect(done.status).toBe('completed');

    const suspend = await request.post(`${nexus.baseURL}/api/v1/businesses/${nexus.businessID}/suspend`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: {},
    });
    expect(suspend.status()).toBe(200);

    const blocked = await submit(request, nexus, {
      intent: 'g2 while suspended',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    const env = await expectEnvelope(blocked, 409, 'CONFLICT', 'CONFLICT', 'submit into suspended business');
    expect(env.message).toContain('no new work is admitted');

    // Existing work is readable after the suspension.
    const read = await request.get(`${nexus.baseURL}/api/v1/requests/${beforeId}?business_id=${nexus.businessID}`, {
      headers: bootstrapHeaders(nexus),
    });
    expect(read.status(), 'stored result stays readable').toBe(200);

    // Cancel of a known request is still a scope operation, not admission:
    // a repeat cancel answers 409 terminal state, proving the request
    // record itself is still reachable through the cancel path.
    const cancel = await request.post(`${nexus.baseURL}/api/v1/requests/${beforeId}/cancel?business_id=${nexus.businessID}`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: {},
    });
    expect(cancel.status(), 'cancel path open during suspension').toBe(409);

    const reactivate = await request.post(`${nexus.baseURL}/api/v1/businesses/${nexus.businessID}/activate`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: {},
    });
    expect(reactivate.status()).toBe(200);

    const after = await submit(request, nexus, {
      intent: 'g2 after reactivation',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(after.status(), 'admission restored').toBe(202);
  });

  test('suspended division gates only its own admissions', async ({ request, nexus }) => {
    const divisionID = `g2-div-${suffix}`;
    const created = await request.post(`${nexus.baseURL}/api/v1/divisions`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: { business_id: nexus.businessID, entity_id: divisionID, name: 'g2', owner_identity_id: nexus.actorID },
    });
    expect([200, 409]).toContain(created.status());

    const suspended = await request.post(`${nexus.baseURL}/api/v1/divisions/${divisionID}/suspend`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: {},
    });
    expect(suspended.status()).toBe(200);

    const blocked = await submit(request, nexus, {
      intent: 'g2 division suspended',
      business_id: nexus.businessID,
      division_id: divisionID,
      actor_id: nexus.actorID,
    });
    const env = await expectEnvelope(blocked, 409, 'CONFLICT', 'CONFLICT', 'submit into suspended division');
    expect(env.message).toContain('no new work is admitted');

    const businessLevel = await submit(request, nexus, {
      intent: 'g2 business level during suspended division',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(businessLevel.status(), 'business-level submit stays open').toBe(202);

    const archived = await request.post(`${nexus.baseURL}/api/v1/divisions/${divisionID}/archive`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: {},
    });
    expect(archived.status()).toBe(200);

    const blockedArchived = await submit(request, nexus, {
      intent: 'g2 division archived',
      business_id: nexus.businessID,
      division_id: divisionID,
      actor_id: nexus.actorID,
    });
    await expectEnvelope(blockedArchived, 409, 'CONFLICT', 'CONFLICT', 'submit into archived division');
  });
});
