import { test, expect } from '../fixtures/test';
import type { APIRequestContext } from '@playwright/test';
import {
  actorHeaders,
  bootstrapHeaders,
  expectEnvelope,
  pollResult,
  putPolicy,
  randomSuffix,
  submit,
} from '../fixtures/api';
import { policyBody, sweepE2ePolicies } from '../fixtures/policy';
import type { NexusHandle } from '../fixtures/nexus';

/**
 * Business / division topology, end to end over HTTP against the real binary.
 *
 * Contracts: CORE_INTERFACE_CONTRACTS §4.2 (CTR-AUTH-001 `division_id` on
 * submit), RUNTIME_EXECUTION_CONTRACTS §3.2/§7.3 (admission respects
 * business/division scope, the division survives every node),
 * SCHEMA_IDENTITIES_ORG §4.3 (a division-scoped identity cannot reach another
 * division's data) and §8 (`division_id` must be within `business_id`).
 *
 * Discovered contract:
 *   - `division_id` is optional on submit; omitting it submits at business
 *     scope and the stored result carries no `division_id`;
 *   - a division that does not exist is a 400 VALIDATION failure, not a
 *     silent drop;
 *   - a division-scoped membership may act inside its own division and may
 *     not act in a sibling division, while a business-wide membership covers
 *     every division of its business;
 *   - a recorded division is enforced on retrieval and on cancel, so a
 *     sibling division's result answers 403 rather than leaking;
 *   - a governance policy pinned to a division matches only requests that
 *     carry that division — the case that was unreachable before
 *     `division_id` existed on submit.
 *
 * Cleanup: policies are prefixed `e2e-` and swept after every test. The
 * identities, businesses and divisions this file creates are additive and
 * unique per run.
 */

const suffix = randomSuffix();

const divA = `topo-a-${suffix}`;
const divB = `topo-b-${suffix}`;
const narrowID = `topo-narrow-${suffix}`;
const narrowCredential = `cred-${suffix}-${Date.now().toString(36)}`;

let setupDone = false;

async function ensureSetup(request: APIRequestContext, nexus: NexusHandle): Promise<void> {
  if (setupDone) return;

  const createDivision = async (entityID: string, businessID: string) => {
    const res = await request.post(`${nexus.baseURL}/api/v1/divisions`, {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: {
        entity_id: entityID,
        business_id: businessID,
        name: `E2E ${entityID}`,
        owner_identity_id: nexus.actorID,
      },
    });
    // Idempotent: a half-finished earlier run must not wedge the whole file.
    expect([200, 409], `create division ${entityID}: ${await res.text()}`).toContain(res.status());
  };

  await createDivision(divA, nexus.businessID);
  await createDivision(divB, nexus.businessID);

  const created = await request.post(`${nexus.baseURL}/api/v1/identities`, {
    headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
    data: {
      entity_id: narrowID,
      identity_type: 'human',
      display_name: 'E2E Topology Narrow',
      business_id: nexus.businessID,
      division_id: divA,
      credential: narrowCredential,
      credential_method: 'password',
    },
  });
  expect([200, 409], `create narrow identity: ${await created.text()}`).toContain(created.status());

  setupDone = true;
}

const narrowHeaders = () => actorHeaders(narrowID, narrowCredential);

test.afterEach(async ({ request, nexus }) => {
  await sweepE2ePolicies(request, nexus);
});

test.describe('TOPOLOGY', () => {
  test('division_id on submit is validated, recorded and enforced end to end', async ({
    request,
    nexus,
  }) => {
    await ensureSetup(request, nexus);

    // §8: the division must exist. The second half of §8 — "division_id must
    // be within business_id" — has no HTTP reach on a fresh install: creating
    // an identity or a division inside a business requires membership in it,
    // and nothing in the API can create the first membership for a second
    // business (docs/http-gateway.md "Creation is membership-bound"), so no
    // caller can be a member of two businesses. That branch is covered by
    // TestDivisionScopeOnSubmitValidation in internal/gateway.
    const unknown = await submit(request, nexus, {
      intent: 'unknown division',
      business_id: nexus.businessID,
      division_id: `nope-${suffix}`,
      actor_id: nexus.actorID,
    });
    const unknownEnv = await expectEnvelope(
      unknown,
      400,
      'VALIDATION',
      'VALIDATION',
      'unknown division',
    );
    expect(unknownEnv.message).toBe('division not found');

    // A rejected scope never reached admission: no id was handed back and the
    // id we can name does not exist.
    const rejected = await submit(request, nexus, {
      intent: 'unknown division, second attempt',
      business_id: nexus.businessID,
      division_id: `nope-${suffix}`,
      actor_id: nexus.actorID,
    });
    expect(rejected.status()).toBe(400);
    const rejectedBody = await rejected.json();
    expect(rejectedBody.request_id, 'a rejected submit issues no id').toBeUndefined();

    // CTR-AUTH-001: a valid division is accepted and survives to the result.
    const ok = await submit(request, nexus, {
      intent: 'work inside a division',
      business_id: nexus.businessID,
      division_id: divA,
      actor_id: nexus.actorID,
    });
    expect(ok.status(), 'submit with a valid division').toBe(202);
    const { request_id } = await ok.json();
    const stored = await pollResult(request, nexus, request_id, nexus.businessID);
    expect(stored.business_id).toBe(nexus.businessID);
    expect(stored.division_id, 'the division reaches the stored result').toBe(divA);

    // Omitting it keeps the record at business scope.
    const plain = await submit(request, nexus, {
      intent: 'work at business scope',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(plain.status()).toBe(202);
    const plainID = (await plain.json()).request_id as string;
    const plainStored = await pollResult(request, nexus, plainID, nexus.businessID);
    expect(plainStored.business_id).toBe(nexus.businessID);
    expect(plainStored.division_id, 'business-scope work carries no division').toBeUndefined();
  });

  test('a division-scoped identity acts only inside its own division', async ({
    request,
    nexus,
  }) => {
    await ensureSetup(request, nexus);

    const own = await submit(
      request,
      nexus,
      {
        intent: 'narrow actor inside its division',
        business_id: nexus.businessID,
        division_id: divA,
        actor_id: narrowID,
      },
      narrowHeaders(),
    );
    expect(own.status(), 'narrow actor may act in its own division').toBe(202);

    const sibling = await submit(
      request,
      nexus,
      {
        intent: 'narrow actor into a sibling division',
        business_id: nexus.businessID,
        division_id: divB,
        actor_id: narrowID,
      },
      narrowHeaders(),
    );
    const env = await expectEnvelope(
      sibling,
      403,
      'AUTHORIZATION',
      'AUTHORIZATION',
      'narrow actor into a sibling division',
    );
    expect(env.message).toBe('access denied: actor is not a member of the requested division');

    // A business-wide request is not another division's data; the contract is
    // silent on whether a division-scoped member may raise one, so today it
    // is allowed and the narrowing applies wherever a division is recorded.
    const businessWide = await submit(
      request,
      nexus,
      {
        intent: 'narrow actor at business scope',
        business_id: nexus.businessID,
        actor_id: narrowID,
      },
      narrowHeaders(),
    );
    expect(businessWide.status(), 'divisionless submit stays business-scoped').toBe(202);
  });

  test('a recorded division is enforced on retrieval and on cancel', async ({
    request,
    nexus,
  }) => {
    await ensureSetup(request, nexus);

    const submitInto = async (divisionID?: string): Promise<string> => {
      const res = await submit(request, nexus, {
        intent: `topology retrieval ${divisionID ?? 'business'}`,
        business_id: nexus.businessID,
        ...(divisionID ? { division_id: divisionID } : {}),
        actor_id: nexus.actorID,
      });
      expect(res.status()).toBe(202);
      return (await res.json()).request_id as string;
    };

    const inA = await submitInto(divA);
    const inB = await submitInto(divB);
    const businessWide = await submitInto();

    await pollResult(request, nexus, inA, nexus.businessID);
    await pollResult(request, nexus, inB, nexus.businessID);
    await pollResult(request, nexus, businessWide, nexus.businessID);

    // Own division reads normally.
    const ownRead = await request.get(
      `${nexus.baseURL}/api/v1/requests/${inA}?business_id=${nexus.businessID}`,
      { headers: narrowHeaders() },
    );
    expect(ownRead.status(), 'narrow actor reads its own division').toBe(200);

    // A sibling division is a scope denial, not a 404 and not a leak.
    const siblingRead = await request.get(
      `${nexus.baseURL}/api/v1/requests/${inB}?business_id=${nexus.businessID}`,
      { headers: narrowHeaders() },
    );
    const readEnv = await expectEnvelope(
      siblingRead,
      403,
      'AUTHORIZATION',
      'AUTHORIZATION',
      'narrow actor reads a sibling division',
    );
    expect(readEnv.message).toBe('access denied: division scope mismatch');

    // Divisionless work is business scope and is not narrowed.
    const wideRead = await request.get(
      `${nexus.baseURL}/api/v1/requests/${businessWide}?business_id=${nexus.businessID}`,
      { headers: narrowHeaders() },
    );
    expect(wideRead.status(), 'divisionless record is not narrowed').toBe(200);

    // The same narrowing governs cancel: scope is decided before terminal
    // state, so the sibling division never learns whether the request is done.
    const siblingCancel = await request.post(
      `${nexus.baseURL}/api/v1/requests/${inB}/cancel?business_id=${nexus.businessID}`,
      { headers: { ...narrowHeaders(), 'Content-Type': 'application/json' }, data: { reason: 'nope' } },
    );
    const cancelEnv = await expectEnvelope(
      siblingCancel,
      403,
      'AUTHORIZATION',
      'AUTHORIZATION',
      'narrow actor cancels a sibling division',
    );
    expect(cancelEnv.message).toBe('access denied: division scope mismatch');
  });

  test('a policy pinned to a division matches only that division', async ({
    request,
    nexus,
  }) => {
    await ensureSetup(request, nexus);

    const id = `e2e-div-deny-${suffix}`;
    const created = await putPolicy(
      request,
      nexus,
      id,
      policyBody({
        business_id: nexus.businessID,
        division_id: divA,
        effect: 'DENY',
        precedence: 9,
      }),
    );
    expect(created.status(), `create division policy: ${await created.text()}`).toBe(200);

    const inA = await submit(request, nexus, {
      intent: 'denied inside divA',
      business_id: nexus.businessID,
      division_id: divA,
      actor_id: nexus.actorID,
    });
    expect(inA.status()).toBe(202);
    const denied = await pollResult(request, nexus, (await inA.json()).request_id, nexus.businessID);
    expect(denied.status).toBe('failed');
    expect(denied.error?.category).toBe('POLICY_DENIED');
    expect(denied.error?.message, 'the failure names the matched policy').toContain(id);

    // The same policy must not reach a sibling division…
    const inB = await submit(request, nexus, {
      intent: 'not denied in divB',
      business_id: nexus.businessID,
      division_id: divB,
      actor_id: nexus.actorID,
    });
    expect(inB.status()).toBe(202);
    const allowed = await pollResult(
      request,
      nexus,
      (await inB.json()).request_id,
      nexus.businessID,
    );
    expect(allowed.status, 'sibling division is unaffected').toBe('completed');
    expect(allowed.error?.code, 'sibling division is not POLICY_DENIED').not.toBe('POLICY_DENIED');

    // …nor at business scope, where no division is recorded.
    const businessWide = await submit(request, nexus, {
      intent: 'not denied at business scope',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(businessWide.status()).toBe(202);
    const plainAllowed = await pollResult(
      request,
      nexus,
      (await businessWide.json()).request_id,
      nexus.businessID,
    );
    expect(plainAllowed.status, 'divisionless submit is unaffected').toBe('completed');
    expect(plainAllowed.error?.code).not.toBe('POLICY_DENIED');
  });
});
