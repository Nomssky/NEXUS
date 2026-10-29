import { test, expect } from '../fixtures/test';
import type { APIRequestContext } from '@playwright/test';
import {
  actorHeaders,
  bootstrapHeaders,
  randomSuffix,
} from '../fixtures/api';
import { startNexus, type NexusHandle } from '../fixtures/nexus';

/**
 * MILESTONE 1D.4 — Durable execution read API & pilot readiness verification.
 *
 * Black-box verification against the real compiled `cmd/nexus` binary over
 * HTTP, using deterministic scripted providers. Verifies which execution
 * information survives a process restart, how authorization is enforced on
 * durable reads, and what configuration is required to enable durability.
 *
 * No production code is modified. No real provider credentials or live
 * external search are used. Synthetic names are unique per run.
 *
 * Coverage:
 *   - Business + membership survive restart (durable, FileStore-hydrated)
 *   - GET /api/v1/recovery/executions/{id}: completed terminal record after
 *     restart (state, evidence, synthesis, approvals, source=durable_journal)
 *   - Missing execution ID → 404; unauthenticated → 401; foreign business →
 *     403 (requireActorMembership runs before record lookup); division-scoped
 *     identity outside permitted scope → 404
 *   - Live request-result API → 404 after restart (process-local), distinct
 *     from the durable read API which survives
 *   - Approval-history visibility; no prompts/credentials/secrets exposed
 *   - `recovery` metadata is only present when recovery reclassified a
 *     nonterminal record as `unknown`; terminal records are untouched
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
        /* fall through to failure report */
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

async function getRecoveryExecution(
  request: APIRequestContext,
  nexus: NexusHandle,
  executionID: string,
  businessID: string,
  headers: Record<string, string>,
): Promise<APIResponse> {
  return request.get(
    `${nexus.baseURL}/api/v1/recovery/executions/${encodeURIComponent(executionID)}?business_id=${encodeURIComponent(businessID)}`,
    { headers },
  );
}

async function getResult(
  request: APIRequestContext,
  nexus: NexusHandle,
  executionID: string,
  businessID: string,
  headers: Record<string, string>,
): Promise<APIResponse> {
  return request.get(
    `${nexus.baseURL}/api/v1/requests/${encodeURIComponent(executionID)}?business_id=${encodeURIComponent(businessID)}`,
    { headers },
  );
}

function bizName(prefix: string): string {
  return `${prefix}-${randomSuffix()}`;
}

async function startDurableNexus(): Promise<NexusHandle> {
  return startNexus({
    extraEnv: {
      NEXUS_SEEDED_PROVIDER_MODE: 'scripted',
      NEXUS_CAPABILITY_WEB: 'scripted',
    },
  });
}

test.describe('DURABLE EXECUTION READ API', () => {
  test('completed research survives restart via durable read API', async ({
    request,
  }) => {
    const nexus = await startDurableNexus();
    try {
      const businessID = bizName('durable-research');
      const headers = bootstrapHeaders(nexus);
      const actorID = nexus.actorID;

      // Authenticate on a scoped path.
      const health = await request.get(`${nexus.baseURL}/health`, { headers });
      expect(health.status(), 'bootstrap authenticates').toBe(200);

      // Create business + establish owner membership.
      const created = await createBusiness(request, nexus, businessID, actorID, headers);
      expect(created.status(), 'create business').toBe(200);
      const createdBody = JSON.parse(await created.text());
      expect(createdBody.business_id).toBe(businessID);

      const member = await establishMembership(request, nexus, businessID, headers);
      expect(member.status(), 'establish membership').toBe(200);

      // Create division + register agent (business-wide member registers agent).
      const divID = bizName('div-durable');
      await request.post(`${nexus.baseURL}/api/v1/divisions`, {
        headers: { ...headers, 'Content-Type': 'application/json' },
        data: {
          entity_id: divID,
          business_id: businessID,
          name: `E2E ${divID}`,
          owner_identity_id: actorID,
          parent_business_id: businessID,
        },
      });
      const agentID = bizName('durable-agent');
      await registerAgent(request, nexus, businessID, agentID, headers);

      // Submit research objective → get execution ID.
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
      expect(sub.status(), 'submit research objective').toBe(202);
      const subBody = JSON.parse(await sub.text());
      expect(subBody.status).toBe('accepted');
      const executionID = subBody.execution_id;

      // Poll to terminal.
      const result = await pollObjective(request, nexus, executionID, businessID, headers);
      expect(result.status, 'research completes').toBe('completed');
      expect(result.business_id, 'business id echoed').toBe(businessID);

      // Durable read BEFORE restart.
      const before = await getRecoveryExecution(request, nexus, executionID, businessID, headers);
      expect(before.status(), 'durable read before restart').toBe(200);
      const beforeBody = JSON.parse(await before.text());
      expect(beforeBody.source, 'source is durable_journal').toBe('durable_journal');
      expect(beforeBody.execution_id, 'execution_id echoed').toBe(executionID);
      expect(beforeBody.business_id, 'business_id echoed').toBe(businessID);
      expect(beforeBody.state, 'state is completed').toBe('completed');
      // Evidence + synthesis present (validated, atomically written with terminal).
      expect(beforeBody.research_evidence, 'research evidence present').toBeTruthy();
      expect(beforeBody.research_synthesis, 'research synthesis present').toBeTruthy();
      expect(beforeBody.research_synthesis.status, 'synthesis status complete').toBe('complete');
      const evidenceBacked = beforeBody.research_synthesis.evidence_backed;
      expect(Array.isArray(evidenceBacked), 'evidence_backed is array').toBe(true);
      expect(evidenceBacked.length, 'at least one evidence-backed statement').toBeGreaterThan(0);
      expect(evidenceBacked[0].source_ids, 'source_ids present').toBeTruthy();
      // No actor_id exposed (identity references excluded per v11 §3).
      expect(beforeBody.actor_id, 'actor_id not exposed').toBeUndefined();
      // No raw prompt/model output/credentials.
      expect(JSON.stringify(beforeBody).toLowerCase()).not.toContain('sk-');
      expect(JSON.stringify(beforeBody).toLowerCase()).not.toContain('prompt');

      // Restart NEXUS (same data dir → durable journal hydrated).
      await nexus.restart();

      // Live request-result API → 404 after restart (process-local).
      const afterLive = await getResult(request, nexus, executionID, businessID, headers);
      expect(afterLive.status(), 'live result 404 after restart').toBe(404);

      // Durable read API → still 200, state completed, evidence + synthesis intact.
      // Terminal records are left untouched by recovery (never rewritten), so
      // `recovery` metadata is undefined here — recovery only marks nonterminal
      // records as `unknown`.
      const after = await getRecoveryExecution(request, nexus, executionID, businessID, headers);
      expect(after.status(), 'durable read after restart').toBe(200);
      const afterBody = JSON.parse(await after.text());
      expect(afterBody.source).toBe('durable_journal');
      expect(afterBody.execution_id).toBe(executionID);
      expect(afterBody.state).toBe('completed');
      expect(afterBody.research_evidence, 'evidence survives restart').toBeTruthy();
      expect(afterBody.research_synthesis, 'synthesis survives restart').toBeTruthy();
      expect(afterBody.research_synthesis.status).toBe('complete');
      expect(afterBody.research_synthesis.evidence_backed.length, 'evidence-backed statements survive').toBeGreaterThan(0);
      // Terminal records are never rewritten by recovery, so no recovery metadata.
      expect(afterBody.recovery, 'terminal record not reclassified by recovery').toBeUndefined();

      // Business + membership survive restart.
      const listed = await request.get(`${nexus.baseURL}/api/v1/businesses`, { headers });
      const listedBody = JSON.parse(await listed.text());
      expect(listed.status()).toBe(200);
      expect(
        (listedBody.businesses as any[]).some((b: any) => b.business_id === businessID),
        'business survives restart',
      ).toBe(true);
    } finally {
      await nexus.dispose();
    }
  });

  test('missing execution ID → 404; unauthenticated → 401; foreign business → 403', async ({
    request,
  }) => {
    const nexus = await startDurableNexus();
    try {
      const businessID = bizName('durable-missing');
      const headers = bootstrapHeaders(nexus);
      const actorID = nexus.actorID;

      await createBusiness(request, nexus, businessID, actorID, headers);
      await establishMembership(request, nexus, businessID, headers);

      // Missing execution ID → 404.
      const missing = await getRecoveryExecution(request, nexus, 'does-not-exist', businessID, headers);
      expect(missing.status(), 'missing ID → 404').toBe(404);

      // Unauthenticated → 401.
      const noAuth = await getRecoveryExecution(request, nexus, 'does-not-exist', businessID, {});
      expect(noAuth.status(), 'unauthenticated → 401').toBe(401);

      // Foreign business identity (authenticated but not a member of businessID)
      // → 403, because requireActorMembership runs before the record lookup.
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
      const foreignRead = await getRecoveryExecution(request, nexus, 'does-not-exist', businessID, foreignHeaders);
      expect(foreignRead.status(), 'foreign authenticated non-member → 403').toBe(403);
    } finally {
      await nexus.dispose();
    }
  });

  test('division-scoped identity cannot read outside its permitted scope', async ({
    request,
  }) => {
    const nexus = await startDurableNexus();
    try {
      const businessID = bizName('durable-div');
      const headers = bootstrapHeaders(nexus);
      const actorID = nexus.actorID;

      await createBusiness(request, nexus, businessID, actorID, headers);
      await establishMembership(request, nexus, businessID, headers);

      const divID = bizName('div-durable');
      await request.post(`${nexus.baseURL}/api/v1/divisions`, {
        headers: { ...headers, 'Content-Type': 'application/json' },
        data: {
          entity_id: divID,
          business_id: businessID,
          name: `E2E ${divID}`,
          owner_identity_id: actorID,
          parent_business_id: businessID,
        },
      });
      const otherDivID = bizName('div-other');
      await request.post(`${nexus.baseURL}/api/v1/divisions`, {
        headers: { ...headers, 'Content-Type': 'application/json' },
        data: {
          entity_id: otherDivID,
          business_id: businessID,
          name: `E2E ${otherDivID}`,
          owner_identity_id: actorID,
          parent_business_id: businessID,
        },
      });

      // Business-wide member (actorID) registers an agent usable in divID.
      const agentID = bizName('durable-agent');
      await registerAgent(request, nexus, businessID, agentID, headers);

      // Division-scoped identity + its own agent registration is NOT possible
      // (agent registration requires business-wide membership), so the narrow
      // identity submits using the business-wide agent registered above.
      const narrowID = bizName('narrow-div');
      const narrowCredential = `cred-narrow-${Date.now().toString(36)}`;
      await request.post(`${nexus.baseURL}/api/v1/identities`, {
        headers: { ...headers, 'Content-Type': 'application/json' },
        data: {
          entity_id: narrowID,
          identity_type: 'human',
          display_name: 'E2E Narrow',
          business_id: businessID,
          division_id: divID,
          credential: narrowCredential,
          credential_method: 'password',
        },
      });
      const narrowHeaders = actorHeaders(narrowID, narrowCredential);

      // Division-scoped member submits division-scoped work (allowed: narrow is
      // member of divID, agent is business-wide → matches divID).
      const sub = await submitObjective(
        request,
        nexus,
        businessID,
        narrowID,
        { description: 'search the web for nexus', context: { agent_id: agentID } },
        narrowHeaders,
        divID,
      );
      expect(sub.status(), 'division-scoped submit accepted').toBe(202);
      const subBody = JSON.parse(await sub.text());
      const executionID = subBody.execution_id;
      const result = await pollObjective(request, nexus, executionID, businessID, narrowHeaders);
      expect(result.status).toBe('completed');

      // Division-scoped member CAN read its own division's durable record.
      const ownRead = await getRecoveryExecution(request, nexus, executionID, businessID, narrowHeaders);
      expect(ownRead.status(), 'own division read → 200').toBe(200);

      // Business-wide member submits into the OTHER division to create a record there.
      const otherSub = await submitObjective(
        request,
        nexus,
        businessID,
        actorID,
        { description: 'search the web for nexus', context: { agent_id: agentID } },
        headers,
        otherDivID,
      );
      expect(otherSub.status(), 'other-division submit accepted').toBe(202);
      const otherBody = JSON.parse(await otherSub.text());
      const otherExecID = otherBody.execution_id;
      await pollObjective(request, nexus, otherExecID, businessID, headers);

      // Division-scoped member reads the OTHER division's record → 404 (G5:
      // indistinguishable from missing).
      const crossRead = await getRecoveryExecution(request, nexus, otherExecID, businessID, narrowHeaders);
      expect(crossRead.status(), 'cross-division read → 404').toBe(404);
    } finally {
      await nexus.dispose();
    }
  });

  test('approval history visible on durable read; no secrets exposed', async ({
    request,
  }) => {
    const nexus = await startDurableNexus();
    try {
      const businessID = bizName('durable-approval');
      const headers = bootstrapHeaders(nexus);
      const actorID = nexus.actorID;

      await createBusiness(request, nexus, businessID, actorID, headers);
      await establishMembership(request, nexus, businessID, headers);

      const agentID = bizName('durable-approval-agent');
      await registerAgent(request, nexus, businessID, agentID, headers);

      // Submit an objective. If governance requires approval, an approval
      // record is durably journaled; otherwise approvals is omitted (both
      // valid per contract v11 §3).
      const sub = await submitObjective(
        request,
        nexus,
        businessID,
        actorID,
        { description: 'search the web for nexus', context: { agent_id: agentID } },
        headers,
      );
      expect(sub.status()).toBe(202);
      const subBody = JSON.parse(await sub.text());
      const executionID = subBody.execution_id;
      const result = await pollObjective(request, nexus, executionID, businessID, headers);
      expect(result.status).toBe('completed');

      const rec = await getRecoveryExecution(request, nexus, executionID, businessID, headers);
      expect(rec.status()).toBe(200);
      const body = JSON.parse(await rec.text());
      // approvals is omitted entirely when no approval gate was exercised,
      // or present as an array when it was (contract v11 §3).
      if (body.approvals !== undefined) {
        expect(Array.isArray(body.approvals), 'approvals is array when present').toBe(true);
        for (const a of body.approvals) {
          expect(a.approval_id, 'approval_id present').toBeTruthy();
          expect(a.outcome, 'outcome present').toBeTruthy();
          expect(a.decided_at, 'decided_at present').toBeTruthy();
          expect(a.approver_id, 'approver_id NOT exposed').toBeUndefined();
        }
      }
      // No secrets/credentials in the durable read.
      const text = JSON.stringify(body);
      expect(text.toLowerCase()).not.toContain('sk-');
      expect(text.toLowerCase()).not.toContain('credential');
      expect(text.toLowerCase()).not.toContain('secret');
    } finally {
      await nexus.dispose();
    }
  });
});
