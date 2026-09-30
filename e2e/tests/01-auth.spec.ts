import { test, expect } from '../fixtures/test';
import {
  actorHeaders,
  bootstrapHeaders,
  controlHeaders,
  expectEnvelope,
  randomSuffix,
} from '../fixtures/api';

/**
 * §5 — authentication against the real gateway.
 *
 * Discovered contract: scoped paths (requests, org records, /events) are
 * wrapped by identityMiddleware when security.require_authentication or
 * security.enforce_business_scope is on (both default true). Credentials are
 * X-Actor-ID + X-Actor-Credential, or Authorization: Basic base64(id:cred).
 * Failures are always the CORE §3 envelope with code UNAUTHORIZED, category
 * AUTH (never a bare status line).
 */
test.describe('AUTH', () => {
  test('no credentials on a scoped endpoint -> 401 with the contract envelope', async ({
    request,
    nexus,
  }) => {
    const res = await request.get(`${nexus.baseURL}/api/v1/identities/${nexus.actorID}`);
    const e = await expectEnvelope(res, 401, 'UNAUTHORIZED', 'AUTH', 'no credentials');
    expect(e.message).toBe('authentication required');
    expect(e.retryable).toBe(false);
    expect(e.correlation_id).not.toBe('');
  });

  test('wrong credential -> 401 and does not reveal whether the id exists', async ({
    request,
    nexus,
  }) => {
    const unknownIdRes = await request.get(
      `${nexus.baseURL}/api/v1/identities/${nexus.actorID}`,
      { headers: actorHeaders('nx:human:does-not-exist', 'wrong-credential') },
    );
    const wrongCredRes = await request.get(
      `${nexus.baseURL}/api/v1/identities/${nexus.actorID}`,
      { headers: actorHeaders(nexus.actorID, 'wrong-credential') },
    );

    const unknownIdEnv = await expectEnvelope(
      unknownIdRes,
      401,
      'UNAUTHORIZED',
      'AUTH',
      'unknown identity',
    );
    const wrongCredEnv = await expectEnvelope(
      wrongCredRes,
      401,
      'UNAUTHORIZED',
      'AUTH',
      'wrong credential',
    );

    // Existence oracle: both failures must be indistinguishable.
    expect(unknownIdEnv.message).toBe(wrongCredEnv.message);
    expect(unknownIdEnv.code).toBe(wrongCredEnv.code);
    expect(unknownIdEnv.category).toBe(wrongCredEnv.category);
    expect(wrongCredEnv.message).toBe('invalid credentials');
  });

  test('correct credentials authenticate and return the bootstrap identity record', async ({
    request,
    nexus,
  }) => {
    const res = await request.get(`${nexus.baseURL}/api/v1/identities/${nexus.actorID}`, {
      headers: bootstrapHeaders(nexus),
    });
    expect(res.status(), 'authenticated identity GET').toBe(200);
    const body = await res.json();
    expect(body.entity_id).toBe(nexus.actorID);
    expect(body.identity_type).toBe('human');
    expect(body.status).toBe('active');
    expect(body.entity_type).toBe('identity');
    expect(body.provenance).toBeTruthy();
  });

  test('membership-scoped listing returns the identities of that business', async ({
    request,
    nexus,
  }) => {
    const res = await request.get(
      `${nexus.baseURL}/api/v1/identities?business_id=${nexus.businessID}`,
      { headers: bootstrapHeaders(nexus) },
    );
    expect(res.status()).toBe(200);
    const body = await res.json();
    expect(Array.isArray(body.identities), 'identities is an array').toBe(true);

    // List an identity actually scoped to this business. The bootstrap record
    // itself is global-scope (created before any business exists), so it is
    // deliberately not part of a business-scoped listing — see README.
    const memberID = `listable-${randomSuffix()}`;
    const created = await request.post(`${nexus.baseURL}/api/v1/identities`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: {
        entity_id: memberID,
        identity_type: 'human',
        display_name: 'E2E Listable',
        business_id: nexus.businessID,
        credential: `cred-${memberID}`,
        credential_method: 'password',
      },
    });
    expect(created.status(), `create listable identity: ${await created.text()}`).toBe(200);

    const listed = await request.get(
      `${nexus.baseURL}/api/v1/identities?business_id=${nexus.businessID}`,
      { headers: bootstrapHeaders(nexus) },
    );
    expect(listed.status()).toBe(200);
    const listedBody = await listed.json();
    const found = listedBody.identities.find((i: any) => i.entity_id === memberID);
    expect(found, 'the created identity is listed for its business').toBeTruthy();
    expect(found.business_id).toBe(nexus.businessID);
    expect(found.status).toBe('active');
    // Credentials never appear in a listing.
    expect(JSON.stringify(listedBody)).not.toContain(`cred-${memberID}`);
  });

  test('listing without business_id -> 400 VALIDATION', async ({ request, nexus }) => {
    const res = await request.get(`${nexus.baseURL}/api/v1/identities`, {
      headers: bootstrapHeaders(nexus),
    });
    await expectEnvelope(res, 400, 'VALIDATION', 'VALIDATION', 'listing without business_id');
  });

  test('HTTP Basic credentials are equivalent to the header form', async ({ request, nexus }) => {
    const basic = Buffer.from(`${nexus.actorID}:${nexus.credential}`).toString('base64');
    const res = await request.get(`${nexus.baseURL}/api/v1/identities/${nexus.actorID}`, {
      headers: { Authorization: `Basic ${basic}` },
    });
    expect(res.status(), 'basic auth').toBe(200);
  });

  test('control endpoints: bad API key -> 401, correct key -> status', async ({
    request,
    nexus,
  }) => {
    const bad = await request.get(`${nexus.baseURL}/api/v1/control/status`, {
      headers: { 'X-API-Key': 'not-the-key' },
    });
    await expectEnvelope(bad, 401, 'UNAUTHORIZED', 'AUTH', 'control bad key');

    const good = await request.get(`${nexus.baseURL}/api/v1/control/status`, {
      headers: controlHeaders(nexus),
    });
    expect(good.status()).toBe(200);
    const body = await good.json();
    expect(body.status).toBe('RUNNING');
    expect(body.components.engine).toBe('RUNNING');
    expect(typeof body.request_count).toBe('number');
  });
});
