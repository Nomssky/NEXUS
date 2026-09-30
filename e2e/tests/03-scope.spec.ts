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
 * §8 — business / actor scope isolation, exercised only through HTTP.
 *
 * Discovered contract:
 *   - every scoped endpoint requires an authenticated actor that is an active
 *     member of the requested business_id (403 AUTHORIZATION otherwise);
 *   - POST /api/v1/requests additionally requires body actor_id to equal the
 *     authenticated identity (403 otherwise);
 *   - a business can be created by any authenticated caller; ownership does
 *     NOT grant membership, so the creating identity stays outside it.
 */

const suffix = randomSuffix();
const otherBusiness = `biz-${suffix}`;
const secondActor = `user-${suffix}`;
const secondCredential = `cred-${suffix}-${Date.now().toString(36)}`;

interface ScopeState {
  businessCreated: boolean;
  identityCreated: boolean;
}
const state: ScopeState = { businessCreated: false, identityCreated: false };

/** Create the foreign business and the second identity once, on first use. */
async function ensureSetup(request: APIRequestContext, nexus: NexusHandle): Promise<void> {
  if (!state.businessCreated) {
    const created = await request.post(`${nexus.baseURL}/api/v1/businesses`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: {
        entity_id: otherBusiness,
        name: 'E2E Foreign Business',
        owner_identity_id: nexus.actorID,
      },
    });
    const bodyText = await created.text();
    expect(created.status(), `create foreign business: ${bodyText}`).toBe(200);
    const body = JSON.parse(bodyText);
    expect(body.business_id).toBe(otherBusiness);
    expect(body.owner_identity_id).toBe(nexus.actorID);
    expect(body.status).toBe('active');
    state.businessCreated = true;
  }

  if (!state.identityCreated) {
    const created = await request.post(`${nexus.baseURL}/api/v1/identities`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: {
        entity_id: secondActor,
        identity_type: 'human',
        display_name: 'E2E Second Actor',
        business_id: nexus.businessID,
        credential: secondCredential,
        credential_method: 'password',
      },
    });
    const bodyText = await created.text();
    expect(created.status(), `create second identity: ${bodyText}`).toBe(200);
    const record = JSON.parse(bodyText);
    expect(record.entity_id).toBe(secondActor);
    expect(record.status).toBe('active');
    state.identityCreated = true;
  }
}

test.describe('SCOPE ISOLATION', () => {
  test('foreign business -> 403 on result, submit and SSE', async ({ request, nexus }) => {
    await ensureSetup(request, nexus);

    const res = await request.get(
      `${nexus.baseURL}/api/v1/requests/req-any?business_id=${otherBusiness}`,
      { headers: bootstrapHeaders(nexus) },
    );
    const e = await expectEnvelope(
      res,
      403,
      'AUTHORIZATION',
      'AUTHORIZATION',
      'foreign business result',
    );
    expect(e.message).toContain('not a member');

    const submitted = await submit(request, nexus, {
      intent: 'cross business submit',
      business_id: otherBusiness,
      actor_id: nexus.actorID,
    });
    await expectEnvelope(
      submitted,
      403,
      'AUTHORIZATION',
      'AUTHORIZATION',
      'foreign business submit',
    );

    const sse = await request.get(`${nexus.baseURL}/events?business_id=${otherBusiness}`, {
      headers: bootstrapHeaders(nexus),
    });
    await expectEnvelope(sse, 403, 'AUTHORIZATION', 'AUTHORIZATION', 'foreign business SSE');
  });

  test('a second identity in the home business authenticates and completes work', async ({
    request,
    nexus,
  }) => {
    await ensureSetup(request, nexus);

    const list = await request.get(
      `${nexus.baseURL}/api/v1/identities?business_id=${nexus.businessID}`,
      { headers: actorHeaders(secondActor, secondCredential) },
    );
    expect(list.status(), 'second identity can authenticate').toBe(200);

    const res = await submit(
      request,
      nexus,
      {
        intent: 'work by the second identity',
        business_id: nexus.businessID,
        actor_id: secondActor,
      },
      actorHeaders(secondActor, secondCredential),
    );
    expect(res.status()).toBe(202);
    const { request_id } = await res.json();
    const result = await pollResult(request, nexus, request_id, nexus.businessID);
    expect(result.status).toBe('completed');
    expect(result.request_id).toBe(request_id);
  });

  test('foreign actor -> 403: body actor_id must match the authenticated identity', async ({
    request,
    nexus,
  }) => {
    await ensureSetup(request, nexus);

    const spoofBootstrap = await submit(
      request,
      nexus,
      {
        intent: 'spoofed actor',
        business_id: nexus.businessID,
        actor_id: nexus.actorID,
      },
      actorHeaders(secondActor, secondCredential),
    );
    const e = await expectEnvelope(
      spoofBootstrap,
      403,
      'AUTHORIZATION',
      'AUTHORIZATION',
      'spoofed bootstrap actor',
    );
    expect(e.message).toContain('does not match authenticated identity');

    const spoofSecond = await submit(request, nexus, {
      intent: 'spoofed actor mirror',
      business_id: nexus.businessID,
      actor_id: secondActor,
    });
    await expectEnvelope(
      spoofSecond,
      403,
      'AUTHORIZATION',
      'AUTHORIZATION',
      'spoofed second actor',
    );

    const foreignRead = await request.get(
      `${nexus.baseURL}/api/v1/requests/req-any?business_id=${otherBusiness}`,
      { headers: actorHeaders(secondActor, secondCredential) },
    );
    await expectEnvelope(
      foreignRead,
      403,
      'AUTHORIZATION',
      'AUTHORIZATION',
      'second identity foreign business read',
    );
  });
});
