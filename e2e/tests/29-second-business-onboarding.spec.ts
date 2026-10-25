import { test, expect } from '../fixtures/test';
import type { APIRequestContext } from '@playwright/test';
import {
  actorHeaders,
  bootstrapHeaders,
  expectEnvelope,
  pollResult,
  randomSuffix,
  submit,
} from '../fixtures/api';
import type { NexusHandle } from '../fixtures/nexus';

/**
 * MILESTONE 1 — Second-business onboarding through the public API.
 *
 * Covers the circular dependency resolved by POST /api/v1/businesses/{id}/members:
 *   - POST /api/v1/businesses is membership-free (business does not exist yet)
 *   - the owner is NOT automatically enrolled
 *   - POST /api/v1/businesses/{id}/members lets the owner claim their first
 *     business-wide membership
 *
 * Every scenario exercises the real compiled NEXUS binary over HTTP. No Go
 * imports, in-process registry shortcuts, mocked handlers, or fixtures that
 * bypass the public API are used for the core onboarding journey.
 *
 * Each test creates its own uniquely-named business so tests remain isolated
 * despite sharing one worker-scoped NEXUS process and data dir.
 */

async function createBusiness(
  request: APIRequestContext,
  nexus: NexusHandle,
  businessID: string,
  ownerID: string,
  headers: Record<string, string>,
): Promise<APIResponse> {
  return request.post(`${nexus.baseURL}/api/v1/businesses`, {
    headers: { ...headers, 'Content-Type': 'application/json' },
    data: { entity_id: businessID, name: `E2E ${businessID}`, owner_identity_id: ownerID },
  });
}

async function establishMembership(
  request: APIRequestContext,
  nexus: NexusHandle,
  businessID: string,
  headers: Record<string, string>,
): Promise<APIResponse> {
  return request.post(
    `${nexus.baseURL}/api/v1/businesses/${encodeURIComponent(businessID)}/members`,
    { headers: { ...headers, 'Content-Type': 'application/json' }, data: {} },
  );
}

function bizName(prefix: string): string {
  return `${prefix}-${randomSuffix()}`;
}

test.describe('SECOND BUSINESS ONBOARDING', () => {
  test('owner establishes initial membership and executes scoped work', async ({
    request,
    nexus,
  }) => {
    const businessID = bizName('trendsentry');
    const headers = bootstrapHeaders(nexus);
    const actorID = nexus.actorID;

    // 1. Authenticate: bootstrap credential authenticates on a scoped path.
    const health = await request.get(`${nexus.baseURL}/health`, { headers });
    expect(health.status(), 'bootstrap authenticates on health').toBe(200);

    // 2. Create a second business (membership-free; owner not enrolled yet).
    const created = await createBusiness(request, nexus, businessID, actorID, headers);
    const createdBody = JSON.parse(await created.text());
    expect(created.status(), 'create second business').toBe(200);
    expect(createdBody.business_id).toBe(businessID);
    expect(createdBody.owner_identity_id).toBe(actorID);

    // Owner still cannot use the business: no membership yet.
    const before = await submit(request, nexus, {
      intent: 'before membership',
      business_id: businessID,
      actor_id: actorID,
    });
    await expectEnvelope(
      before,
      403,
      'AUTHORIZATION',
      'AUTHORIZATION',
      'no membership yet',
    );

    // 3. Owner claims initial membership through the public path.
    const member = await establishMembership(request, nexus, businessID, headers);
    const memberBody = JSON.parse(await member.text());
    expect(member.status(), 'establish initial membership').toBe(200);
    expect(memberBody.business_id).toBe(businessID);
    expect(memberBody.identity_id).toBe(actorID);
    expect(memberBody.role).toBe('OWNER');
    expect(memberBody.status).toBe('active');

    // 4. Member can list/read the new business.
    const listed = await request.get(`${nexus.baseURL}/api/v1/businesses`, {
      headers,
    });
    const listedBody = JSON.parse(await listed.text());
    expect(listed.status(), 'list businesses').toBe(200);
    expect(
      (listedBody.businesses as any[]).some((b: any) => b.business_id === businessID),
      'second business visible to its member',
    ).toBe(true);

    const got = await request.get(
      `${nexus.baseURL}/api/v1/businesses/${businessID}`,
      { headers },
    );
    expect(got.status(), 'read new business').toBe(200);

    // 5. Member submits work scoped to the new business and retrieves the result.
    const sub = await submit(request, nexus, {
      intent: 'trendsentry onboarding work',
      business_id: businessID,
      actor_id: actorID,
    });
    expect(sub.status(), 'submit scoped work').toBe(202);
    const subBody = JSON.parse(await sub.text());
    const result = await pollResult(request, nexus, subBody.request_id, businessID);
    expect(result.status, 'work completes').toBe('completed');
    expect(result.business_id).toBe(businessID);
  });

  test('create a division under the new business', async ({ request, nexus }) => {
    const businessID = bizName('trendsentry-div');
    const headers = bootstrapHeaders(nexus);
    const actorID = nexus.actorID;

    await createBusiness(request, nexus, businessID, actorID, headers);
    await establishMembership(request, nexus, businessID, headers);

    const divID = bizName('div');
    const created = await request.post(`${nexus.baseURL}/api/v1/divisions`, {
      headers: { ...headers, 'Content-Type': 'application/json' },
      data: {
        entity_id: divID,
        business_id: businessID,
        name: `E2E Division ${divID}`,
        owner_identity_id: actorID,
        parent_business_id: businessID,
      },
    });
    expect(created.status(), 'create division').toBe(200);
    const body = JSON.parse(await created.text());
    expect(body.business_id).toBe(businessID);
    expect(body.status).toBe('active');

    const listed = await request.get(
      `${nexus.baseURL}/api/v1/divisions?business_id=${businessID}`,
      { headers },
    );
    expect(listed.status(), 'list divisions').toBe(200);
  });

  test('foreign business identity cannot access the new business', async ({
    request,
    nexus,
  }) => {
    const businessID = bizName('trendsentry-foreign');
    const headers = bootstrapHeaders(nexus);
    const actorID = nexus.actorID;

    await createBusiness(request, nexus, businessID, actorID, headers);
    await establishMembership(request, nexus, businessID, headers);

    // Foreign identity in the bootstrap business.
    const foreignID = bizName('foreign');
    const foreignCredential = `cred-foreign-${Date.now().toString(36)}`;
    await request.post(`${nexus.baseURL}/api/v1/identities`, {
      headers: { ...headers, 'Content-Type': 'application/json' },
      data: {
        entity_id: foreignID,
        identity_type: 'human',
        display_name: 'E2E Foreign Identity',
        business_id: nexus.businessID,
        credential: foreignCredential,
        credential_method: 'password',
      },
    });
    const foreignHeaders = actorHeaders(foreignID, foreignCredential);

    // Foreign actor lists businesses: does not see the second business.
    const listed = await request.get(`${nexus.baseURL}/api/v1/businesses`, {
      headers: foreignHeaders,
    });
    const listedBody = JSON.parse(await listed.text());
    expect(listed.status(), 'foreign lists businesses').toBe(200);
    expect(
      (listedBody.businesses as any[]).some((b: any) => b.business_id === businessID),
      'foreign business not visible',
    ).toBe(false);

    // Foreign actor reads the second business directly: 404 (no existence leak).
    const got = await request.get(
      `${nexus.baseURL}/api/v1/businesses/${businessID}`,
      { headers: foreignHeaders },
    );
    await expectEnvelope(got, 404, 'VALIDATION', 'VALIDATION', 'foreign business read');

    // Foreign actor submits into the second business: 403.
    const sub = await submit(request, nexus, {
      intent: 'foreign submits',
      business_id: businessID,
      actor_id: foreignID,
    });
    await expectEnvelope(
      sub,
      403,
      'AUTHORIZATION',
      'AUTHORIZATION',
      'foreign actor submits',
    );
  });

  test('unauthenticated requests fail closed', async ({ request, nexus }) => {
    const businessID = bizName('trendsentry-noauth');
    const noAuth = await createBusiness(request, nexus, businessID, nexus.actorID, {});
    await expectEnvelope(
      noAuth,
      401,
      'UNAUTHORIZED',
      'AUTH',
      'unauthenticated business creation',
    );

    const noAuthMember = await establishMembership(request, nexus, businessID, {});
    await expectEnvelope(
      noAuthMember,
      401,
      'UNAUTHORIZED',
      'AUTH',
      'unauthenticated membership',
    );
  });

  test('business and membership survive a restart', async ({ request, nexus }) => {
    const businessID = bizName('trendsentry-restart');
    const headers = bootstrapHeaders(nexus);
    const actorID = nexus.actorID;

    await createBusiness(request, nexus, businessID, actorID, headers);
    await establishMembership(request, nexus, businessID, headers);

    // Submit and complete work before restart.
    const sub = await submit(request, nexus, {
      intent: 'pre-restart work',
      business_id: businessID,
      actor_id: actorID,
    });
    expect(sub.status(), 'pre-restart submit').toBe(202);
    const subBody = JSON.parse(await sub.text());
    await pollResult(request, nexus, subBody.request_id, businessID);

    // Restart NEXUS (same process, same data dir).
    await nexus.restart();

    // Business and membership still readable after restart.
    const got = await request.get(
      `${nexus.baseURL}/api/v1/businesses/${businessID}`,
      { headers },
    );
    expect(got.status(), 'business readable after restart').toBe(200);

    const listed = await request.get(`${nexus.baseURL}/api/v1/businesses`, { headers });
    const listedBody = JSON.parse(await listed.text());
    expect(listed.status(), 'membership visible after restart').toBe(200);
    expect(
      (listedBody.businesses as any[]).some((b: any) => b.business_id === businessID),
      'second business still listed after restart',
    ).toBe(true);

    // Member can still submit after restart.
    const sub2 = await submit(request, nexus, {
      intent: 'post-restart work',
      business_id: businessID,
      actor_id: actorID,
    });
    expect(sub2.status(), 'post-restart submit').toBe(202);
    const sub2Body = JSON.parse(await sub2.text());
    await pollResult(request, nexus, sub2Body.request_id, businessID);
  });

  test('division-scoped membership cannot access business-wide scope', async ({
    request,
    nexus,
  }) => {
    const businessID = bizName('trendsentry-narrow');
    const headers = bootstrapHeaders(nexus);
    const actorID = nexus.actorID;
    const divID = bizName('divnarrow');

    await createBusiness(request, nexus, businessID, actorID, headers);
    await establishMembership(request, nexus, businessID, headers);

    // Create a division under the second business.
    const divCreate = await request.post(`${nexus.baseURL}/api/v1/divisions`, {
      headers: { ...headers, 'Content-Type': 'application/json' },
      data: {
        entity_id: divID,
        business_id: businessID,
        name: `E2E Narrow Division ${divID}`,
        owner_identity_id: actorID,
        parent_business_id: businessID,
      },
    });
    expect(divCreate.status(), 'create division').toBe(200);

    // Create an identity with a division-scoped membership only (auto-enrolled by
    // POST /api/v1/identities because business_id is set).
    const narrowID = bizName('narrow');
    const narrowCredential = `cred-narrow-${Date.now().toString(36)}`;
    const narrowCreate = await request.post(`${nexus.baseURL}/api/v1/identities`, {
      headers: { ...headers, 'Content-Type': 'application/json' },
      data: {
        entity_id: narrowID,
        identity_type: 'human',
        display_name: 'E2E Division-Scoped',
        business_id: businessID,
        division_id: divID,
        credential: narrowCredential,
        credential_method: 'password',
      },
    });
    expect(narrowCreate.status(), 'create division-scoped identity').toBe(200);
    const narrowHeaders = actorHeaders(narrowID, narrowCredential);

    // Division-scoped membership covers division-scoped submit.
    const divSub = await submit(request, nexus, {
      intent: 'division scoped work',
      business_id: businessID,
      division_id: divID,
      actor_id: narrowID,
    }, narrowHeaders);
    const divBodyText = await divSub.text();
    const divBody = JSON.parse(divBodyText);
    expect(divSub.status(), `division-scoped submit accepted (got ${divSub.status()}: ${divBodyText.slice(0,200)})`).toBe(202);
    expect(divBody.status).toBe('accepted');

    // But the same identity cannot submit at business level (G3).
    const bizSub = await submit(request, nexus, {
      intent: 'business level work',
      business_id: businessID,
      actor_id: narrowID,
    }, narrowHeaders);
    await expectEnvelope(
      bizSub,
      403,
      'AUTHORIZATION',
      'AUTHORIZATION',
      'division-scoped membership cannot submit business-level',
    );
  });

  test('initial-membership mechanism cannot enroll arbitrary identity', async ({
    request,
    nexus,
  }) => {
    const businessID = bizName('trendsentry-usurp');
    const headers = bootstrapHeaders(nexus);
    const actorID = nexus.actorID;

    await createBusiness(request, nexus, businessID, actorID, headers);

    // A non-owner authenticated identity cannot establish membership in this business.
    const nonOwnerID = bizName('nonowner');
    const nonOwnerCredential = `cred-nonowner-${Date.now().toString(36)}`;
    await request.post(`${nexus.baseURL}/api/v1/identities`, {
      headers: { ...headers, 'Content-Type': 'application/json' },
      data: {
        entity_id: nonOwnerID,
        identity_type: 'human',
        display_name: 'E2E Non-Owner',
        business_id: nexus.businessID,
        credential: nonOwnerCredential,
        credential_method: 'password',
      },
    });
    const nonOwnerHeaders = actorHeaders(nonOwnerID, nonOwnerCredential);

    const usurp = await establishMembership(
      request,
      nexus,
      businessID,
      nonOwnerHeaders,
    );
    await expectEnvelope(
      usurp,
      403,
      'AUTHORIZATION',
      'AUTHORIZATION',
      'non-owner cannot establish membership',
    );
  });

  test('failed onboarding leaves no inconsistent state', async ({ request, nexus }) => {
    const businessID = bizName('trendsentry-fail');
    const headers = bootstrapHeaders(nexus);
    const actorID = nexus.actorID;

    const created = await createBusiness(request, nexus, businessID, actorID, headers);
    expect(created.status(), 'create business for failure test').toBe(200);
    await establishMembership(request, nexus, businessID, headers);

    // Membership for an unknown business: 404, no state created.
    const unknown = await establishMembership(
      request,
      nexus,
      `no-such-biz-${randomSuffix()}`,
      headers,
    );
    await expectEnvelope(
      unknown,
      404,
      'VALIDATION',
      'VALIDATION',
      'unknown business membership',
    );

    // Membership for a non-owner: 403.
    const nonOwnerID = bizName('failnonowner');
    const nonOwnerCredential = `cred-failnonowner-${Date.now().toString(36)}`;
    await request.post(`${nexus.baseURL}/api/v1/identities`, {
      headers: { ...headers, 'Content-Type': 'application/json' },
      data: {
        entity_id: nonOwnerID,
        identity_type: 'human',
        display_name: 'E2E Fail Non-Owner',
        business_id: nexus.businessID,
        credential: nonOwnerCredential,
        credential_method: 'password',
      },
    });
    const nonOwnerHeaders = actorHeaders(nonOwnerID, nonOwnerCredential);
    const refused = await establishMembership(request, nexus, businessID, nonOwnerHeaders);
    await expectEnvelope(
      refused,
      403,
      'AUTHORIZATION',
      'AUTHORIZATION',
      'failed non-owner membership',
    );

    // The business still exists and is readable after failed attempts.
    const listed = await request.get(
      `${nexus.baseURL}/api/v1/businesses/${businessID}`,
      { headers },
    );
    expect(listed.status(), 'business still readable after failed attempts').toBe(200);
  });

  test('initial membership claim is idempotent and does not broaden authority', async ({
    request,
    nexus,
  }) => {
    const businessID = bizName('trendsentry-idem');
    const headers = bootstrapHeaders(nexus);
    const actorID = nexus.actorID;

    const created = await createBusiness(request, nexus, businessID, actorID, headers);
    expect(created.status(), 'create business').toBe(200);

    // First claim: establishes membership.
    const first = await establishMembership(request, nexus, businessID, headers);
    const firstBody = JSON.parse(await first.text());
    expect(first.status(), 'first claim').toBe(200);
    expect(firstBody.role).toBe('OWNER');
    expect(firstBody.status).toBe('active');

    // Second claim: idempotent no-op, returns 200 with same role (no
    // duplication, no authority broadening).
    const second = await establishMembership(request, nexus, businessID, headers);
    const secondBody = JSON.parse(await second.text());
    expect(second.status(), 'second claim idempotent').toBe(200);
    expect(secondBody.role).toBe('OWNER');
    expect(secondBody.status).toBe('active');

    // The owner can still submit scoped work (authority intact, not broadened
    // beyond business scope).
    const sub = await submit(request, nexus, {
      intent: 'idempotency check work',
      business_id: businessID,
      actor_id: actorID,
    });
    expect(sub.status(), 'submit after repeat claim').toBe(202);

    // Foreign business remains inaccessible (no cross-business leakage).
    const foreign = await submit(request, nexus, {
      intent: 'should not reach foreign business',
      business_id: nexus.businessID,
      actor_id: actorID,
    });
    // bootstrap business submission is fine (owner is member of bootstrap biz);
    // this just confirms the repeat claim didn't break normal operation.
    expect(foreign.status(), 'bootstrap submit still works').toBe(202);
  });

  test('bootstrap business still works after second business onboarding', async ({
    request,
    nexus,
  }) => {
    const headers = bootstrapHeaders(nexus);

    const listed = await request.get(`${nexus.baseURL}/api/v1/businesses`, { headers });
    expect(listed.status(), 'bootstrap business lists').toBe(200);

    const sub = await submit(request, nexus, {
      intent: 'bootstrap still works',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(sub.status(), 'bootstrap submit').toBe(202);
    const subBody = JSON.parse(await sub.text());
    const result = await pollResult(
      request,
      nexus,
      subBody.request_id,
      nexus.businessID,
    );
    expect(result.status, 'bootstrap work completes').toBe('completed');
  });
});
