import { test, expect } from '../fixtures/test';
import {
  deletePolicy,
  expectEnvelope,
  getPolicy,
  listPolicies,
  pollResult,
  putPolicy,
  randomSuffix,
  submit,
} from '../fixtures/api';
import { policyBody, sweepE2ePolicies } from '../fixtures/policy';

/**
 * Governance end-to-end — the contract-defined control surface plus the five
 * evaluation outcomes, driven only through HTTP against the real binary.
 *
 * Discovered contract (contracts/SCHEMA_GOVERNANCE_ATTENTION.md §2/§9,
 * docs/http-gateway.md "Policy control"):
 *   - policies are reached with X-API-Key on /api/v1/control/policies; an
 *     absent or wrong key fails closed before the handler runs;
 *   - `effect` travels as the §2.1 enum name, never an ordinal;
 *   - a DENY lands as a terminal `failed` with category POLICY_DENIED whose
 *     message names the matched policy — that is how "which rule fired"
 *     becomes observable without a new field;
 *   - ALLOW_WITH_CONSTRAINTS is the only allowing outcome that leaves a mark:
 *     the terminal result carries `constraints` as `type:expression`, reported
 *     and not enforced;
 *   - scope is taken from the request: the submit body carries `business_id`
 *     only, so a policy pinned to another business must never match, and a
 *     disabled policy must never apply;
 *   - precedence breaks ties between equally restrictive policies, so the
 *     higher `precedence` id is the one named in the failure message;
 *   - the seeded `default-allow` is read-only.
 *
 * Cleanup: every policy this file writes is prefixed `e2e-` and swept in
 * afterEach, so a failed assertion cannot leak a DENY into the specs that run
 * after this one (the gateway is worker-scoped and shared).
 */

const suffix = randomSuffix();

/** Sweep every policy this spec created, even when a test failed midway. */
test.afterEach(async ({ request, nexus }) => {
  await sweepE2ePolicies(request, nexus);
});

test.describe('GOVERNANCE', () => {
  test('the policy control surface is key-gated and carries the enum effect', async ({
    request,
    nexus,
  }) => {
    const noKey = await request.get(`${nexus.baseURL}/api/v1/control/policies`, {
      headers: { Accept: 'application/json' },
    });
    await expectEnvelope(noKey, 401, 'UNAUTHORIZED', 'AUTH', 'policy list without key');

    const wrongKey = await request.get(`${nexus.baseURL}/api/v1/control/policies`, {
      headers: { 'X-API-Key': 'not-the-key' },
    });
    await expectEnvelope(wrongKey, 401, 'UNAUTHORIZED', 'AUTH', 'policy list wrong key');

    const listed = await listPolicies(request, nexus);
    expect(listed.status(), 'policy list with the real key').toBe(200);
    const { policies } = await listed.json();
    expect(Array.isArray(policies), 'policies is a list').toBe(true);

    const builtin = policies.find((p: any) => p.policy_id === 'default-allow');
    expect(builtin, 'the seeded built-in is listed').toBeTruthy();
    expect(builtin.effect, 'effect is the §2.1 enum name, not an ordinal').toBe('ALLOW');
    expect(builtin.entity_type).toBe('policy');
    expect(typeof builtin.provenance?.origin).toBe('string');

    // The built-in cannot be overwritten or removed: that is the rule that
    // keeps an unconfigured installation on ALLOW instead of default-DENY.
    const overwrite = await putPolicy(
      request,
      nexus,
      'default-allow',
      policyBody({ effect: 'DENY' }),
    );
    await expectEnvelope(overwrite, 409, 'CONFLICT', 'CONFLICT', 'overwrite built-in');

    const removed = await deletePolicy(request, nexus, 'default-allow');
    await expectEnvelope(removed, 409, 'CONFLICT', 'CONFLICT', 'delete built-in');

    const unknown = await getPolicy(request, nexus, `e2e-missing-${suffix}`);
    await expectEnvelope(unknown, 404, 'VALIDATION', 'VALIDATION', 'unknown policy');

    // A rejected write must leave decisions alone.
    const invalid = await putPolicy(request, nexus, `e2e-bad-${suffix}`, {
      policy_type: 'access_control',
      name: 'missing everything else',
      description: 'incomplete',
      status: 'active',
      subject: { subject_type: 'all' },
      action: { action_type: 'custom' },
      resource: { resource_type: 'all' },
      effect: 'NOT_AN_OUTCOME',
      precedence: 0,
    });
    await expectEnvelope(invalid, 400, 'VALIDATION', 'VALIDATION', 'invalid policy body');
    const stillMissing = await getPolicy(request, nexus, `e2e-bad-${suffix}`);
    await expectEnvelope(stillMissing, 404, 'VALIDATION', 'VALIDATION', 'rejected write not stored');
  });

  test('a DENY policy fails the request and names the matched policy', async ({
    request,
    nexus,
  }) => {
    const id = `e2e-deny-${suffix}`;
    const created = await putPolicy(
      request,
      nexus,
      id,
      policyBody({
        business_id: nexus.businessID,
        effect: 'DENY',
        precedence: 5,
      }),
    );
    expect(created.status(), `create deny policy: ${await created.text()}`).toBe(200);
    const stored = await getPolicy(request, nexus, id);
    expect(stored.status()).toBe(200);
    const storedRecord = await stored.json();
    expect(storedRecord.effect).toBe('DENY');
    expect(storedRecord.business_id).toBe(nexus.businessID);
    expect(storedRecord.precedence).toBe(5);

    const submitted = await submit(request, nexus, {
      intent: 'should be denied by governance',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(submitted.status()).toBe(202);
    const { request_id } = await submitted.json();

    const result = await pollResult(request, nexus, request_id, nexus.businessID);
    expect(result.status, 'a denied request is terminal failed').toBe('failed');
    expect(result.error?.category).toBe('POLICY_DENIED');
    expect(result.error?.code).toBe('POLICY_DENIED');
    expect(result.error?.retryable, 'retrying a denial cannot succeed').toBe(false);
    expect(result.error?.chain_step).toBe('governance');
    expect(result.error?.message).toContain(`matched policy ${id}`);
    expect(result.error?.message).toContain('precedence 5');
    expect(result.error?.details?.approval_id, 'DENY opens no approval loop').toBeUndefined();
    // The gate ran before execution: no outcome means no work happened.
    expect(result.outcome, 'a denied request never executed').toBeUndefined();
  });

  test('scope isolation: a DENY pinned to another business changes nothing here', async ({
    request,
    nexus,
  }) => {
    const foreignBusiness = `biz-${suffix}`;
    const id = `e2e-deny-foreign-${suffix}`;
    const created = await putPolicy(
      request,
      nexus,
      id,
      policyBody({
        business_id: foreignBusiness,
        effect: 'DENY',
        precedence: 50,
      }),
    );
    expect(created.status(), `create foreign deny: ${await created.text()}`).toBe(200);

    const readBack = await getPolicy(request, nexus, id);
    expect(readBack.status()).toBe(200);
    const record = await readBack.json();
    expect(record.business_id).toBe(foreignBusiness);
    expect(record.effect).toBe('DENY');

    // The foreign policy must not match a request in this business, even
    // though its precedence is far higher than anything here.
    const submitted = await submit(request, nexus, {
      intent: 'unaffected by a foreign-scoped deny',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(submitted.status()).toBe(202);
    const { request_id } = await submitted.json();

    const result = await pollResult(request, nexus, request_id, nexus.businessID);
    expect(result.status, 'foreign policy must not deny a local request').toBe('completed');
    expect(result.error, 'no error on an unaffected request').toBeUndefined();
  });

  test('ALLOW_WITH_CONSTRAINTS reports its constraints on the terminal result', async ({
    request,
    nexus,
  }) => {
    const id = `e2e-constraints-${suffix}`;
    const created = await putPolicy(
      request,
      nexus,
      id,
      policyBody({
        business_id: nexus.businessID,
        effect: 'ALLOW_WITH_CONSTRAINTS',
        precedence: 1,
        constraints: [
          {
            constraint_id: 'e2e-budget',
            constraint_type: 'budget',
            expression: '500',
            severity: 'advisory',
          },
        ],
      }),
    );
    expect(created.status(), `create constraints policy: ${await created.text()}`).toBe(200);

    const submitted = await submit(request, nexus, {
      intent: 'allowed with constraints',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(submitted.status()).toBe(202);
    const { request_id } = await submitted.json();

    const result = await pollResult(request, nexus, request_id, nexus.businessID);
    expect(result.status, 'constraints do not block the request').toBe('completed');
    expect(result.constraints, 'the decision output carries the constraints').toEqual([
      'budget:500',
    ]);
    expect(result.error).toBeUndefined();
  });

  test('precedence picks which of two equal DENY policies is matched', async ({
    request,
    nexus,
  }) => {
    const low = `e2e-prec-low-${suffix}`;
    const high = `e2e-prec-high-${suffix}`;
    for (const [id, precedence] of [
      [low, 1],
      [high, 20],
    ] as const) {
      const res = await putPolicy(
        request,
        nexus,
        id,
        policyBody({ business_id: nexus.businessID, effect: 'DENY', precedence }),
      );
      expect(res.status(), `create ${id}: ${await res.text()}`).toBe(200);
    }

    const submitted = await submit(request, nexus, {
      intent: 'precedence decides the matched policy',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(submitted.status()).toBe(202);
    const { request_id } = await submitted.json();

    const result = await pollResult(request, nexus, request_id, nexus.businessID);
    expect(result.status).toBe('failed');
    expect(result.error?.category).toBe('POLICY_DENIED');
    expect(result.error?.message, 'the higher precedence policy wins').toContain(
      `matched policy ${high}`,
    );
    expect(result.error?.message).toContain('precedence 20');
    expect(result.error?.message).not.toContain(`matched policy ${low}`);
  });

  test('a disabled policy does not apply', async ({ request, nexus }) => {
    const id = `e2e-disabled-${suffix}`;
    const created = await putPolicy(
      request,
      nexus,
      id,
      policyBody({
        business_id: nexus.businessID,
        effect: 'DENY',
        status: 'disabled',
        precedence: 99,
      }),
    );
    expect(created.status(), `create disabled deny: ${await created.text()}`).toBe(200);

    const submitted = await submit(request, nexus, {
      intent: 'a disabled policy must not deny',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(submitted.status()).toBe(202);
    const { request_id } = await submitted.json();

    const result = await pollResult(request, nexus, request_id, nexus.businessID);
    expect(result.status, 'disabled policies are not active').toBe('completed');
    expect(result.error).toBeUndefined();
  });

  test('a policy is replaced by a PUT and removed by a DELETE, and both change decisions', async ({
    request,
    nexus,
  }) => {
    const id = `e2e-policy-lifecycle-${suffix}`;

    const submitOnce = async (intent: string) => {
      const res = await submit(request, nexus, {
        intent,
        business_id: nexus.businessID,
        actor_id: nexus.actorID,
      });
      expect(res.status()).toBe(202);
      const { request_id } = await res.json();
      return pollResult(request, nexus, request_id, nexus.businessID);
    };

    // §9.5 is an upsert on policy_id: create.
    const created = await putPolicy(
      request,
      nexus,
      id,
      policyBody({ business_id: nexus.businessID, effect: 'DENY', precedence: 5 }),
    );
    expect(created.status(), `create deny: ${await created.text()}`).toBe(200);
    const denied = await submitOnce('denied by the created policy');
    expect(denied.status).toBe('failed');
    expect(denied.error?.category).toBe('POLICY_DENIED');
    expect(denied.error?.message).toContain(id);

    // …replace: the same id now carries a different effect.
    const replaced = await putPolicy(
      request,
      nexus,
      id,
      policyBody({ business_id: nexus.businessID, effect: 'ALLOW', precedence: 5 }),
    );
    expect(replaced.status(), `replace policy: ${await replaced.text()}`).toBe(200);
    const readBack = await getPolicy(request, nexus, id);
    expect((await readBack.json()).effect, 'the replacement is readable back').toBe('ALLOW');
    const allowed = await submitOnce('allowed by the replaced policy');
    expect(allowed.status, 'the replaced effect governs the next decision').toBe('completed');
    expect(allowed.error).toBeUndefined();

    // …delete: the record is gone and the seeded default-allow governs again.
    const removed = await deletePolicy(request, nexus, id);
    expect(removed.status(), `delete: ${await removed.text()}`).toBe(200);
    expect((await removed.json()).deleted).toBe(true);
    const gone = await getPolicy(request, nexus, id);
    await expectEnvelope(gone, 404, 'VALIDATION', 'VALIDATION', 'deleted policy');

    const afterRemoval = await submitOnce('allowed after the policy is removed');
    expect(afterRemoval.status).toBe('completed');
    expect(afterRemoval.error).toBeUndefined();

    // Removing what is not there is a 404, not a silent success.
    const again = await deletePolicy(request, nexus, id);
    await expectEnvelope(again, 404, 'VALIDATION', 'VALIDATION', 'second delete');
  });
});
