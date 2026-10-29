import { test, expect } from '../fixtures/test';
import type { APIRequestContext } from '@playwright/test';
import {
  actorHeaders,
  bootstrapHeaders,
  expectEnvelope,
  randomSuffix,
} from '../fixtures/api';
import { startNexus, type NexusHandle } from '../fixtures/nexus';

/**
 * MILESTONE 1D.1 — TrendSentry research workflow: black-box validation.
 *
 * Validates that the merged NEXUS platform can execute one complete,
 * business-scoped research objective through its real HTTP gateway, using the
 * deterministic scripted provider, while preserving authorization, evidence
 * integrity, and restart persistence.
 *
 * This is a validation spike, not a claim that live TrendSentry operations are
 * available. Every scenario drives the real compiled `cmd/nexus` binary over
 * HTTP. No Go imports, in-process shortcuts, mocked production components, or
 * direct storage mutation are used.
 *
 * Deterministic scripted configuration:
 *   - NEXUS_SEEDED_PROVIDER_MODE=scripted  (consumed by the launcher;
 *     seeds the documented decision-table provider)
 *   - NEXUS_CAPABILITY_WEB=scripted        (matches existing E2E convention)
 *
 * Synthetic names are unique per run. No real TrendSentry organizational
 * taxonomy, live provider credentials, or production data are used.
 *
 * Each test starts its own NEXUS instance (like 28-research.spec.ts) so the
 * scripted provider/web capability are enabled. The shared `nexus` fixture
 * does NOT set these env vars, so web.search would not be registered.
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

async function registerAgent(
  request: APIRequestContext,
  nexus: NexusHandle,
  businessID: string,
  agentID: string,
  headers: Record<string, string>,
): Promise<APIResponse> {
  return request.post(`${nexus.baseURL}/api/v1/agents`, {
    headers: { ...headers, 'Content-Type': 'application/json' },
    data: {
      entity_id: agentID,
      name: `E2E ${agentID}`,
      business_id: businessID,
      capabilities: ['analysis'],
      allowed_tools: ['web.search'],
    },
  });
}

async function submitObjective(
  request: APIRequestContext,
  nexus: NexusHandle,
  businessID: string,
  actorID: string,
  objective: { description: string; context?: Record<string, string> },
  headers: Record<string, string>,
  divisionID?: string,
): Promise<APIResponse> {
  return request.post(`${nexus.baseURL}/api/v1/intelligence/execute`, {
    headers: { ...headers, 'Content-Type': 'application/json' },
    data: {
      business_id: businessID,
      actor_id: actorID,
      division_id: divisionID,
      objective,
    },
  });
}

async function pollObjective(
  request: APIRequestContext,
  nexus: NexusHandle,
  executionID: string,
  businessID: string,
  headers: Record<string, string>,
): Promise<any> {
  const deadline = Date.now() + 60_000;
  let lastStatus = -1;
  let lastBody = '';
  while (Date.now() < deadline) {
    const res = await request.get(
      `${nexus.baseURL}/api/v1/intelligence/${encodeURIComponent(executionID)}?business_id=${encodeURIComponent(businessID)}`,
      { headers },
    );
    lastStatus = res.status();
    lastBody = await res.text();
    if (res.status() === 200) {
      try {
        return JSON.parse(lastBody);
      } catch {
        /* fall through to the failure report below */
      }
    } else if (res.status() !== 202) {
      try {
        const parsed = JSON.parse(lastBody);
        if (parsed.error) return parsed;
      } catch {
        /* ignore */
      }
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(
    `execution ${executionID} never reached a terminal state\n` +
      `last status: ${lastStatus}\nlast body: ${lastBody.slice(0, 1000)}`,
  );
}

function bizName(prefix: string): string {
  return `${prefix}-${randomSuffix()}`;
}

async function startResearchNexus(): Promise<NexusHandle> {
  return startNexus({
    extraEnv: {
      NEXUS_SEEDED_PROVIDER_MODE: 'scripted',
      NEXUS_CAPABILITY_WEB: 'scripted',
    },
  });
}

test.describe('TRENDSENTRY RESEARCH WORKFLOW', () => {
  test('owner onboards a business, establishes membership, and the member executes a research objective', async ({
    request,
  }) => {
    const nexus = await startResearchNexus();
    try {
      const businessID = bizName('trendsentry-research');
      const headers = bootstrapHeaders(nexus);
      const actorID = nexus.actorID;

      // 1. Authenticate: bootstrap credential authenticates on a scoped path.
      const health = await request.get(`${nexus.baseURL}/health`, { headers });
      expect(health.status(), 'bootstrap authenticates on health').toBe(200);

      // 2. Create a TrendSentry test business (membership-free; owner not enrolled yet).
      const created = await createBusiness(request, nexus, businessID, actorID, headers);
      const createdBody = JSON.parse(await created.text());
      expect(created.status(), 'create research business').toBe(200);
      expect(createdBody.business_id).toBe(businessID);
      expect(createdBody.owner_identity_id).toBe(actorID);

      // Owner still cannot use the business: no membership yet.
      const before = await submitObjective(
        request,
        nexus,
        businessID,
        actorID,
        { description: 'research thoroughly: probe the web for nexus capabilities' },
        headers,
      );
      await expectEnvelope(before, 403, 'AUTHORIZATION', 'AUTHORIZATION', 'no membership yet');

      // 3. Owner claims initial business-wide membership through the public path.
      const member = await establishMembership(request, nexus, businessID, headers);
      const memberBody = JSON.parse(await member.text());
      expect(member.status(), 'establish initial membership').toBe(200);
      expect(memberBody.business_id).toBe(businessID);
      expect(memberBody.identity_id).toBe(actorID);
      expect(memberBody.role).toBe('OWNER');
      expect(memberBody.status).toBe('active');

      // 4. Create a division under the business.
      const divID = bizName('div-research');
      const divCreate = await request.post(`${nexus.baseURL}/api/v1/divisions`, {
        headers: { ...headers, 'Content-Type': 'application/json' },
        data: {
          entity_id: divID,
          business_id: businessID,
          name: `E2E Research Division ${divID}`,
          owner_identity_id: actorID,
          parent_business_id: businessID,
        },
      });
      expect(divCreate.status(), 'create research division').toBe(200);
      const divBody = JSON.parse(await divCreate.text());
      expect(divBody.business_id).toBe(businessID);
      expect(divBody.status).toBe('active');

      // 5. Register an agent with web.search in the business, acting as the owner.
      const agentID = bizName('research-agent');
      const agentCreate = await registerAgent(request, nexus, businessID, agentID, headers);
      expect(agentCreate.status(), 'register research agent').toBe(200);

      // 6. Submit a research-intent objective scoped to the business and division.
      const objectiveDescription =
        'research thoroughly: search the web for nexus capabilities and summarize the findings';
      const sub = await submitObjective(
        request,
        nexus,
        businessID,
        actorID,
        {
          description: objectiveDescription,
          context: { agent_id: agentID },
        },
        headers,
      );
      expect(sub.status(), 'submit research objective').toBe(202);
      const subBody = JSON.parse(await sub.text());
      expect(subBody.status).toBe('accepted');
      const executionID = subBody.execution_id;

      // 7. Poll to a genuine terminal result.
      const result = await pollObjective(request, nexus, executionID, businessID, headers);
      expect(result.status, 'research objective reaches terminal state').toBe('completed');

      // 8. Evidence and synthesis: the synthesis is resolved against captured
      //    evidence and citations, not a fabricated non-empty citation string.
      const outcome = result.outcome;
      expect(outcome, 'outcome present').toBeTruthy();
      expect(outcome.summary, 'outcome summary present').toBeTruthy();
      expect(outcome.summary).toContain('state=completed');
      const synthesis = outcome.research_synthesis;
      expect(synthesis, 'research synthesis present').toBeTruthy();
      expect(synthesis.status, 'synthesis status complete').toBe('complete');
      const evidenceBacked = synthesis.evidence_backed;
      expect(Array.isArray(evidenceBacked), 'evidence_backed is an array').toBe(true);
      expect(evidenceBacked.length, 'at least one evidence-backed statement').toBeGreaterThan(0);
      expect(evidenceBacked[0].source_ids, 'source_ids present').toBeTruthy();
      expect(evidenceBacked[0].source_ids.length, 'at least one source id').toBeGreaterThan(0);
      // The deterministic quality evaluation is present.
      const evaluation = outcome.research_evaluation;
      expect(evaluation, 'research evaluation present').toBeTruthy();

      // 9. The member can list/read the business.
      const listed = await request.get(`${nexus.baseURL}/api/v1/businesses`, { headers });
      const listedBody = JSON.parse(await listed.text());
      expect(listed.status(), 'list businesses').toBe(200);
      expect(
        (listedBody.businesses as any[]).some((b: any) => b.business_id === businessID),
        'research business visible to its member',
      ).toBe(true);
    } finally {
      await nexus.dispose();
    }
  });

  test('cross-business isolation: a foreign business identity cannot read the research result', async ({
    request,
  }) => {
    const nexus = await startResearchNexus();
    try {
      const businessID = bizName('trendsentry-foreign');
      const headers = bootstrapHeaders(nexus);
      const actorID = nexus.actorID;

      await createBusiness(request, nexus, businessID, actorID, headers);
      await establishMembership(request, nexus, businessID, headers);

      const divID = bizName('div-foreign');
      await request.post(`${nexus.baseURL}/api/v1/divisions`, {
        headers: { ...headers, 'Content-Type': 'application/json' },
        data: {
          entity_id: divID,
          business_id: businessID,
          name: `E2E Foreign Division ${divID}`,
          owner_identity_id: actorID,
          parent_business_id: businessID,
        },
      });

      const agentID = bizName('foreign-agent');
      await registerAgent(request, nexus, businessID, agentID, headers);

      const sub = await submitObjective(
        request,
        nexus,
        businessID,
        actorID,
        {
          description: 'research thoroughly: search the web for nexus capabilities',
          context: { agent_id: agentID },
        },
        headers,
      );
      expect(sub.status(), 'foreign submit accepted').toBe(202);
      const subBody = JSON.parse(await sub.text());
      const executionID = subBody.execution_id;
      const result = await pollObjective(request, nexus, executionID, businessID, headers);
      expect(result.status).toBe('completed');

      // Foreign identity in a different business.
      const foreignID = bizName('foreign-user');
      const foreignCredential = `cred-foreign-${Date.now().toString(36)}`;
      await request.post(`${nexus.baseURL}/api/v1/identities`, {
        headers: { ...headers, 'Content-Type': 'application/json' },
        data: {
          entity_id: foreignID,
          identity_type: 'human',
          display_name: 'E2E Foreign User',
          business_id: nexus.businessID,
          credential: foreignCredential,
          credential_method: 'password',
        },
      });
      const foreignHeaders = actorHeaders(foreignID, foreignCredential);

      // Foreign actor lists businesses: does not see the research business.
      const listed = await request.get(`${nexus.baseURL}/api/v1/businesses`, {
        headers: foreignHeaders,
      });
      const listedBody = JSON.parse(await listed.text());
      expect(listed.status(), 'foreign lists businesses').toBe(200);
      expect(
        (listedBody.businesses as any[]).some((b: any) => b.business_id === businessID),
        'foreign business not visible',
      ).toBe(false);

      // Foreign actor reads the research result: denied (not a member of the business).
      const got = await request.get(
        `${nexus.baseURL}/api/v1/intelligence/${encodeURIComponent(executionID)}?business_id=${encodeURIComponent(businessID)}`,
        { headers: foreignHeaders },
      );
      await expectEnvelope(
        got,
        403,
        'AUTHORIZATION',
        'AUTHORIZATION',
        'foreign actor reads research result',
      );

      // Foreign actor submits into the research business: denied.
      const foreignSub = await submitObjective(
        request,
        nexus,
        businessID,
        foreignID,
        { description: 'research thoroughly: search the web for nexus', context: { agent_id: agentID } },
        foreignHeaders,
      );
      await expectEnvelope(
        foreignSub,
        403,
        'AUTHORIZATION',
        'AUTHORIZATION',
        'foreign actor submits research',
      );
    } finally {
      await nexus.dispose();
    }
  });

  test('authentication and membership boundaries', async ({ request }) => {
    const nexus = await startResearchNexus();
    try {
      const businessID = bizName('trendsentry-auth');
      const headers = bootstrapHeaders(nexus);
      const actorID = nexus.actorID;

      await createBusiness(request, nexus, businessID, actorID, headers);

      // Unauthenticated requests fail closed.
      const noAuth = await createBusiness(request, nexus, bizName('noauth'), actorID, {});
      await expectEnvelope(
        noAuth,
        401,
        'UNAUTHORIZED',
        'AUTH',
        'unauthenticated business creation',
      );
      const noAuthSubmit = await submitObjective(
        request,
        nexus,
        businessID,
        actorID,
        { description: 'unauthenticated research' },
        {},
      );
      await expectEnvelope(
        noAuthSubmit,
        401,
        'UNAUTHORIZED',
        'AUTH',
        'unauthenticated research submit',
      );

      // A non-owner authenticated identity cannot claim membership in this business.
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
      const usurp = await establishMembership(request, nexus, businessID, nonOwnerHeaders);
      await expectEnvelope(
        usurp,
        403,
        'AUTHORIZATION',
        'AUTHORIZATION',
        'non-owner cannot claim membership',
      );

      // Division-scoped membership cannot submit business-level work (G3).
      // Establish owner membership first, then create the division.
      await establishMembership(request, nexus, businessID, headers);
      const divID = bizName('div-auth');
      await request.post(`${nexus.baseURL}/api/v1/divisions`, {
        headers: { ...headers, 'Content-Type': 'application/json' },
        data: {
          entity_id: divID,
          business_id: businessID,
          name: `E2E Auth Division ${divID}`,
          owner_identity_id: actorID,
          parent_business_id: businessID,
        },
      });

      const narrowID = bizName('narrow-auth');
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

      // Division-scoped submit into the division is allowed.
      const narrowAgentID = bizName('div-agent');
      await registerAgent(request, nexus, businessID, narrowAgentID, headers);
      const divSub = await submitObjective(
        request,
        nexus,
        businessID,
        narrowID,
        {
          description: 'search the web for nexus',
          context: { agent_id: narrowAgentID },
        },
        narrowHeaders,
        divID,
      );
      const divSubBody = JSON.parse(await divSub.text());
      expect(divSub.status(), 'division-scoped submit accepted').toBe(202);
      expect(divSubBody.status).toBe('accepted');

      // Division-scoped submit at business level is denied (G3).
      const narrowAgentID2 = bizName('div-agent2');
      await registerAgent(request, nexus, businessID, narrowAgentID2, headers);
      const bizSub = await submitObjective(
        request,
        nexus,
        businessID,
        narrowID,
        { description: 'search the web for nexus', context: { agent_id: narrowAgentID2 } },
        narrowHeaders,
      );
      await expectEnvelope(
        bizSub,
        403,
        'AUTHORIZATION',
        'AUTHORIZATION',
        'division-scoped membership cannot submit business-level',
      );
    } finally {
      await nexus.dispose();
    }
  });

  test('restart persistence: business and membership survive; execution result is process-local', async ({
    request,
  }) => {
    const nexus = await startResearchNexus();
    try {
      const businessID = bizName('trendsentry-restart');
      const headers = bootstrapHeaders(nexus);
      const actorID = nexus.actorID;

      await createBusiness(request, nexus, businessID, actorID, headers);
      await establishMembership(request, nexus, businessID, headers);

      const divID = bizName('div-restart');
      await request.post(`${nexus.baseURL}/api/v1/divisions`, {
        headers: { ...headers, 'Content-Type': 'application/json' },
        data: {
          entity_id: divID,
          business_id: businessID,
          name: `E2E Restart Division ${divID}`,
          owner_identity_id: actorID,
          parent_business_id: businessID,
        },
      });

      const agentID = bizName('restart-agent');
      await registerAgent(request, nexus, businessID, agentID, headers);

      const sub = await submitObjective(
        request,
        nexus,
        businessID,
        actorID,
        {
          description: 'research thoroughly: search the web for nexus capabilities',
          context: { agent_id: agentID },
        },
        headers,
      );
      expect(sub.status(), 'pre-restart submit').toBe(202);
      const subBody = JSON.parse(await sub.text());
      const executionID = subBody.execution_id;
      const preResult = await pollObjective(request, nexus, executionID, businessID, headers);
      expect(preResult.status, 'pre-restart research completes').toBe('completed');

      // Restart NEXUS (same process, same data dir).
      await nexus.restart();

      // Business and membership survive restart (durable, FileStore-hydrated).
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
        'research business still listed after restart',
      ).toBe(true);

      // Execution results are process-local (G4 Level 1): after restart the
      // completed execution is NOT retrievable through the live read path.
      // This is the documented contract, not a defect — report it honestly.
      const after = await request.get(
        `${nexus.baseURL}/api/v1/intelligence/${encodeURIComponent(executionID)}?business_id=${encodeURIComponent(businessID)}`,
        { headers },
      );
      expect(
        after.status(),
        'execution result is process-local (404 after restart per G4 Level 1)',
      ).toBe(404);

      // The member can still submit new work after restart.
      const sub2 = await submitObjective(
        request,
        nexus,
        businessID,
        actorID,
        {
          description: 'search the web for nexus',
          context: { agent_id: agentID },
        },
        headers,
      );
      expect(sub2.status(), 'post-restart submit accepted').toBe(202);
      const sub2Body = JSON.parse(await sub2.text());
      await pollObjective(request, nexus, sub2Body.execution_id, businessID, headers);
    } finally {
      await nexus.dispose();
    }
  });
});
