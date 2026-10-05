import { test, expect } from '../fixtures/test';
import type { APIRequestContext } from '@playwright/test';
import { actorHeaders, bootstrapHeaders, expectEnvelope, randomSuffix } from '../fixtures/api';
import type { NexusHandle } from '../fixtures/nexus';

/**
 * Identity lifecycle over HTTP against the real binary
 * (contracts/SCHEMA_IDENTITIES_ORG §2, §9).
 *
 * Discovered contract:
 *   - status is enforced at authentication, so `suspend` and `revoke` are the
 *     revocation path: afterwards the same credential answers 401 UNAUTHORIZED
 *     with the CORE §3 envelope, not a bare status line;
 *   - `active` → `suspended` → `active` is a legal round trip and
 *     `active` → `revoked` is terminal (a further transition is 409);
 *   - creation accepts only `active` or `pending`, and a `pending` identity
 *     is created but cannot authenticate until it is activated;
 *   - a duplicate `entity_id` is 409 CONFLICT, malformed input is 400
 *     VALIDATION, and a rejected credential method leaves no identity behind
 *     (the record is rolled back);
 *   - the authority model for transitions is membership of the record's
 *     business: any member may transition it, and there is no role check.
 *     The contract defines no authority model for lifecycle transitions
 *     (audit §A1), so none is invented here.
 *
 * Not reachable over HTTP, covered by internal/gateway: a *foreign* record's
 * `404` on read/transition. Creating an identity inside a business requires
 * membership in it and nothing in the API creates the first membership for a
 * second business (docs/http-gateway.md "Creation is membership-bound"), so
 * no caller can be a non-member of a business that already has identities.
 *
 * Cleanup: every identity this file creates is unique per run; the bootstrap
 * identity is always restored to `active` in a finally block, because its
 * status survives a restart (docs/http-gateway.md "self-lockout").
 */

const suffix = randomSuffix();

const createIdentityURL = (nexus: NexusHandle) => `${nexus.baseURL}/api/v1/identities`;
const identityURL = (nexus: NexusHandle, id: string) => `${nexus.baseURL}/api/v1/identities/${id}`;

async function createIdentity(
  request: APIRequestContext,
  nexus: NexusHandle,
  id: string,
  credential: string,
  extra: Record<string, unknown> = {},
) {
  return request.post(createIdentityURL(nexus), {
    headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
    data: {
      entity_id: id,
      identity_type: 'human',
      display_name: `E2E ${id}`,
      business_id: nexus.businessID,
      credential,
      credential_method: 'password',
      ...extra,
    },
  });
}

function transition(
  request: APIRequestContext,
  nexus: NexusHandle,
  id: string,
  action: 'activate' | 'suspend' | 'revoke',
  headers: Record<string, string>,
) {
  return request.post(`${identityURL(nexus, id)}/${action}`, {
    headers: { 'Content-Type': 'application/json', ...headers },
    data: {},
  });
}

/** Assert the credential no longer authenticates anywhere on a scoped path. */
async function expectLockedOut(
  request: APIRequestContext,
  nexus: NexusHandle,
  id: string,
  credential: string,
  why: string,
) {
  const res = await request.get(`${nexus.baseURL}/api/v1/identities?business_id=${nexus.businessID}`, {
    headers: actorHeaders(id, credential),
  });
  await expectEnvelope(res, 401, 'UNAUTHORIZED', 'AUTH', why);
}

test.describe('IDENTITY LIFECYCLE', () => {
  test('suspend and revoke both remove authentication', async ({ request, nexus }) => {
    const suspendedID = `lifecycle-suspended-${suffix}`;
    const revokedID = `lifecycle-revoked-${suffix}`;
    const cred = `cred-${suffix}-${Date.now().toString(36)}`;

    expect(
      (await createIdentity(request, nexus, suspendedID, cred)).status(),
      'create suspended target',
    ).toBe(200);
    expect((await createIdentity(request, nexus, revokedID, cred)).status(), 'create revoked target').toBe(
      200,
    );

    // Both authenticate while active.
    const before = await request.get(`${nexus.baseURL}/api/v1/identities?business_id=${nexus.businessID}`, {
      headers: actorHeaders(suspendedID, cred),
    });
    expect(before.status(), 'an active identity authenticates').toBe(200);

    const suspended = await transition(request, nexus, suspendedID, 'suspend', bootstrapHeaders(nexus));
    expect(suspended.status(), 'suspend succeeds for a member').toBe(200);
    expect((await suspended.json()).status).toBe('suspended');
    await expectLockedOut(request, nexus, suspendedID, cred, 'suspended identity');

    const revoked = await transition(request, nexus, revokedID, 'revoke', bootstrapHeaders(nexus));
    expect(revoked.status(), 'revoke succeeds for a member').toBe(200);
    expect((await revoked.json()).status).toBe('revoked');
    await expectLockedOut(request, nexus, revokedID, cred, 'revoked identity');

    // Suspension is reversible, revocation is not.
    const reactivated = await transition(
      request,
      nexus,
      suspendedID,
      'activate',
      bootstrapHeaders(nexus),
    );
    expect(reactivated.status(), 'activate restores a suspended identity').toBe(200);
    const after = await request.get(`${nexus.baseURL}/api/v1/identities?business_id=${nexus.businessID}`, {
      headers: actorHeaders(suspendedID, cred),
    });
    expect(after.status(), 'authentication returns after activate').toBe(200);

    const lateActivate = await transition(
      request,
      nexus,
      revokedID,
      'activate',
      bootstrapHeaders(nexus),
    );
    await expectEnvelope(
      lateActivate,
      409,
      'CONFLICT',
      'CONFLICT',
      'revoked identity is terminal',
    );
  });

  test('a pending identity is created but cannot authenticate', async ({ request, nexus }) => {
    const id = `lifecycle-pending-${suffix}`;
    const cred = `cred-${suffix}-${Date.now().toString(36)}`;

    const created = await createIdentity(request, nexus, id, cred, { status: 'pending' });
    expect(created.status(), 'pending is accepted at creation').toBe(200);
    expect((await created.json()).status).toBe('pending');

    await expectLockedOut(request, nexus, id, cred, 'pending identity');

    const activated = await transition(request, nexus, id, 'activate', bootstrapHeaders(nexus));
    expect(activated.status()).toBe(200);
    const after = await request.get(`${nexus.baseURL}/api/v1/identities?business_id=${nexus.businessID}`, {
      headers: actorHeaders(id, cred),
    });
    expect(after.status(), 'activated identity authenticates').toBe(200);
  });

  test('malformed identity and credential input is rejected without a record', async ({
    request,
    nexus,
  }) => {
    const badType = await request.post(createIdentityURL(nexus), {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: {
        entity_id: `lifecycle-badtype-${suffix}`,
        identity_type: 'not-a-type',
        display_name: 'E2E Bad Type',
        business_id: nexus.businessID,
      },
    });
    await expectEnvelope(badType, 400, 'VALIDATION', 'VALIDATION', 'bad identity_type');

    const badStatus = await request.post(createIdentityURL(nexus), {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: {
        entity_id: `lifecycle-badstatus-${suffix}`,
        identity_type: 'human',
        display_name: 'E2E Bad Status',
        business_id: nexus.businessID,
        status: 'revoked',
      },
    });
    const statusEnv = await expectEnvelope(
      badStatus,
      400,
      'VALIDATION',
      'VALIDATION',
      'bad status at creation',
    );
    expect(statusEnv.message).toContain('status must be active or pending');

    // The credential method is validated after the record exists and the
    // record is rolled back, so a rejected write leaves nothing behind.
    const badMethodID = `lifecycle-badmethod-${suffix}`;
    const badMethod = await request.post(createIdentityURL(nexus), {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: {
        entity_id: badMethodID,
        identity_type: 'human',
        display_name: 'E2E Bad Method',
        business_id: nexus.businessID,
        credential: 'a-credential',
        credential_method: 'telepathy',
      },
    });
    const methodEnv = await expectEnvelope(
      badMethod,
      400,
      'VALIDATION',
      'VALIDATION',
      'bad credential_method',
    );
    expect(methodEnv.message).toContain('credential_method');

    const leftover = await request.get(identityURL(nexus, badMethodID), {
      headers: bootstrapHeaders(nexus),
    });
    await expectEnvelope(leftover, 404, 'VALIDATION', 'VALIDATION', 'rejected write not stored');

    // A duplicate entity_id is a conflict, not a silent overwrite.
    const duplicateID = `lifecycle-dup-${suffix}`;
    expect(
      (await createIdentity(request, nexus, duplicateID, `cred-${suffix}-one`)).status(),
      'first create',
    ).toBe(200);
    const duplicate = await createIdentity(request, nexus, duplicateID, `cred-${suffix}-two`);
    await expectEnvelope(duplicate, 409, 'CONFLICT', 'CONFLICT', 'duplicate identity');
  });

  test('bootstrap self-lockout is recoverable through a second identity', async ({
    request,
    nexus,
  }) => {
    const secondID = `lifecycle-second-${suffix}`;
    const secondCredential = `cred-${suffix}-${Date.now().toString(36)}`;
    expect(
      (await createIdentity(request, nexus, secondID, secondCredential)).status(),
      'create the recovery identity first',
    ).toBe(200);

    // docs/http-gateway.md "self-lockout": suspending bootstrap is not undone
    // by a restart, so the recovery path has to be another identity.
    const locked = await transition(
      request,
      nexus,
      nexus.actorID,
      'suspend',
      bootstrapHeaders(nexus),
    );
    expect(locked.status(), 'a member may suspend the bootstrap identity').toBe(200);
    try {
      await expectLockedOut(request, nexus, nexus.actorID, nexus.credential, 'suspended bootstrap');
    } finally {
      const recovered = await transition(
        request,
        nexus,
        nexus.actorID,
        'activate',
        actorHeaders(secondID, secondCredential),
      );
      expect(recovered.status(), 'the second identity activates bootstrap back').toBe(200);
    }

    const restored = await request.get(
      `${nexus.baseURL}/api/v1/identities?business_id=${nexus.businessID}`,
      { headers: bootstrapHeaders(nexus) },
    );
    expect(restored.status(), 'bootstrap authenticates again').toBe(200);
  });
});
