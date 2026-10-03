import { test, expect } from '../fixtures/test';
import type { APIRequestContext } from '@playwright/test';
import {
  bootstrapHeaders,
  actorHeaders,
  expectEnvelope,
  pollResult,
  putPolicy,
  randomSuffix,
  submit,
} from '../fixtures/api';
import { policyBody, sweepE2ePolicies } from '../fixtures/policy';
import type { NexusHandle } from '../fixtures/nexus';

/**
 * Approval and escalation lifecycle, end to end over HTTP.
 *
 * The endpoints themselves are contract-defined and already wired
 * (SCHEMA_WORK §6 approval record, CTR-ATT escalation surface): nothing here
 * adds a status or a category, it only drives what exists and pins the
 * observable behaviour.
 *
 * Discovered contract:
 *   - a REQUIRE_APPROVAL gate fails the request with category
 *     APPROVAL_REQUIRED and hands the caller `error.details.approval_id`;
 *     the approval is then listed as PENDING for the business scope;
 *   - the approver must be an active member of `business_id`; with
 *     `self_approval_prohibited` the requester is refused with 403
 *     AUTHORIZATION, so a second identity is needed to approve;
 *   - approve → 202 (accepted, not done) and the original request resumes to
 *     `completed`; the approval leaves the actionable list for good;
 *   - deny → 200 and NO resume: the stored result stays `failed` with
 *     APPROVAL_REQUIRED, and the record leaves the actionable list;
 *   - an ESCALATE gate fails with code ESCALATION_REQUIRED (category stays
 *     POLICY_DENIED — CORE §3 has no escalation category), hands back
 *     `error.details.escalation_ref`, and queues an alert that answers
 *     pending → acknowledged → resolved;
 *   - both decision surfaces require `reason` / `reasoning` in the body.
 *
 * Cleanup: policies are prefixed `e2e-` and swept after every test, so a
 * failed assertion cannot leave a gate installed for the next spec on the
 * shared worker-scoped gateway.
 */

const suffix = randomSuffix();

const approverID = `approver-${suffix}`;
const approverCredential = `cred-${suffix}-${Date.now().toString(36)}`;

let approverCreated = false;

/** Create a second active member of the home business, once per worker. */
async function ensureApprover(request: APIRequestContext, nexus: NexusHandle): Promise<void> {
  if (approverCreated) return;
  const res = await request.post(`${nexus.baseURL}/api/v1/identities`, {
    headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
    data: {
      entity_id: approverID,
      identity_type: 'human',
      display_name: 'E2E Approver',
      business_id: nexus.businessID,
      credential: approverCredential,
      credential_method: 'password',
    },
  });
  const text = await res.text();
  expect(res.status(), `create approver identity: ${text}`).toBe(200);
  approverCreated = true;
}

function approvalURL(nexus: NexusHandle, approvalID: string, decision: 'approve' | 'deny'): string {
  return `${nexus.baseURL}/api/v1/approvals/${encodeURIComponent(approvalID)}/${decision}?business_id=${encodeURIComponent(nexus.businessID)}`;
}

function escalationsURL(nexus: NexusHandle): string {
  return `${nexus.baseURL}/api/v1/escalations?business_id=${encodeURIComponent(nexus.businessID)}`;
}

/** Poll the escalation list until the alert is visible (queueing is async). */
async function waitForEscalation(
  request: APIRequestContext,
  nexus: NexusHandle,
  escalationID: string,
  timeoutMs = 10_000,
): Promise<Record<string, any>> {
  const deadline = Date.now() + timeoutMs;
  let last: unknown = null;
  while (Date.now() < deadline) {
    const res = await request.get(escalationsURL(nexus), { headers: bootstrapHeaders(nexus) });
    if (res.status() === 200) {
      const body = await res.json();
      last = body;
      const found = (body.escalations ?? []).find(
        (e: any) => e.escalation_id === escalationID,
      );
      if (found) return found;
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(
    `escalation ${escalationID} never appeared in ${timeoutMs}ms\nlast list: ${JSON.stringify(last)}`,
  );
}

test.afterEach(async ({ request, nexus }) => {
  await sweepE2ePolicies(request, nexus);
});

test.describe('APPROVAL / ESCALATION', () => {
  test('REQUIRE_APPROVAL holds the request, refuses self-approval, resumes on approval', async ({
    request,
    nexus,
  }) => {
    await ensureApprover(request, nexus);

    const id = `e2e-approve-${suffix}`;
    const created = await putPolicy(
      request,
      nexus,
      id,
      policyBody({
        business_id: nexus.businessID,
        effect: 'REQUIRE_APPROVAL',
        precedence: 3,
        approval_config: {
          approver_type: 'human',
          timeout_seconds: 3600,
          auto_deny_on_timeout: false,
          self_approval_prohibited: true,
          delegation_allowed: false,
        },
      }),
    );
    expect(created.status(), `create approval policy: ${await created.text()}`).toBe(200);

    const submitted = await submit(request, nexus, {
      intent: 'work that needs a human approval',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(submitted.status()).toBe(202);
    const { request_id } = await submitted.json();

    // The gate holds: failed with APPROVAL_REQUIRED, not a silent success.
    const held = await pollResult(request, nexus, request_id, nexus.businessID);
    expect(held.status).toBe('failed');
    expect(held.error?.category).toBe('APPROVAL_REQUIRED');
    expect(held.error?.code).toBe('APPROVAL_REQUIRED');
    expect(held.error?.retryable, 'waiting for approval is not a retry').toBe(false);
    const approvalID = held.error?.details?.approval_id;
    expect(approvalID, 'the caller is told where to decide').toBeTruthy();
    expect(held.outcome, 'nothing executed before the decision').toBeUndefined();

    // It is actionable for the scope.
    const listed = await request.get(`${nexus.baseURL}/api/v1/approvals?business_id=${nexus.businessID}`, {
      headers: bootstrapHeaders(nexus),
    });
    expect(listed.status()).toBe(200);
    const { approvals } = await listed.json();
    const pending = (approvals ?? []).find((a: any) => a.entity_id === approvalID);
    expect(pending, 'the approval is listed as actionable').toBeTruthy();
    expect(pending.status).toBe('PENDING');
    expect(pending.requester_id).toBe(nexus.actorID);
    expect(pending.policy_ref).toBe(id);

    // The requester may not approve their own request.
    const selfApproval = await request.post(approvalURL(nexus, approvalID!, 'approve'), {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: { reason: 'approving my own request' },
    });
    const refused = await expectEnvelope(
      selfApproval,
      403,
      'AUTHORIZATION',
      'AUTHORIZATION',
      'self approval',
    );
    // core maps the typed error at its boundary, so the caller sees the
    // contract category/code plus core's sentence, not the inner detail.
    expect(refused.message).toContain('self-approval prohibited');

    // A different member of the scope approves → the request resumes.
    const approved = await request.post(approvalURL(nexus, approvalID!, 'approve'), {
      headers: { ...actorHeaders(approverID, approverCredential), 'Content-Type': 'application/json' },
      data: { reason: 'reviewed and approved' },
    });
    expect(approved.status(), `approve: ${await approved.text()}`).toBe(202);
    const approvedBody = await approved.json().catch(() => ({}));
    expect(approvedBody.approval_id).toBe(approvalID);
    expect(approvedBody.status).toBe('approved');

    const resumed = await pollResult(request, nexus, request_id, nexus.businessID);
    expect(resumed.status, 'approval resumes the original request').toBe('completed');
    expect(resumed.error).toBeUndefined();

    // One decision per record: once the resume has stored its terminal
    // result the index entry is cleaned up (cleanupResumeApproval), so
    // there is nothing left to decide. (While the resume is still in flight
    // the entry exists and a second decision is 409 not-pending; that window
    // is deliberately not asserted — it races the run.)
    const again = await request.post(approvalURL(nexus, approvalID!, 'approve'), {
      headers: { ...actorHeaders(approverID, approverCredential), 'Content-Type': 'application/json' },
      data: { reason: 'deciding again' },
    });
    await expectEnvelope(again, 404, 'VALIDATION', 'VALIDATION', 'second approve');

    // Decided approvals are no longer actionable.
    const after = await request.get(`${nexus.baseURL}/api/v1/approvals?business_id=${nexus.businessID}`, {
      headers: bootstrapHeaders(nexus),
    });
    const afterBody = await after.json();
    expect(
      (afterBody.approvals ?? []).some((a: any) => a.entity_id === approvalID),
      'a decided approval leaves the actionable list',
    ).toBe(false);
  });

  test('denying an approval leaves the request failed with APPROVAL_REQUIRED', async ({
    request,
    nexus,
  }) => {
    const id = `e2e-approval-deny-${suffix}`;
    const created = await putPolicy(
      request,
      nexus,
      id,
      policyBody({
        business_id: nexus.businessID,
        effect: 'REQUIRE_APPROVAL',
        precedence: 3,
        approval_config: {
          approver_type: 'human',
          timeout_seconds: 3600,
          auto_deny_on_timeout: false,
          self_approval_prohibited: false,
          delegation_allowed: false,
        },
      }),
    );
    expect(created.status(), `create approval policy: ${await created.text()}`).toBe(200);

    // A decision without a rationale is not a decision.
    const submitted = await submit(request, nexus, {
      intent: 'work that will be denied',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(submitted.status()).toBe(202);
    const { request_id } = await submitted.json();

    const held = await pollResult(request, nexus, request_id, nexus.businessID);
    expect(held.status).toBe('failed');
    expect(held.error?.category).toBe('APPROVAL_REQUIRED');
    const approvalID = held.error?.details?.approval_id;
    expect(approvalID).toBeTruthy();

    const noReason = await request.post(approvalURL(nexus, approvalID!, 'deny'), {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: {},
    });
    await expectEnvelope(noReason, 400, 'VALIDATION', 'VALIDATION', 'deny without rationale');

    const denied = await request.post(approvalURL(nexus, approvalID!, 'deny'), {
      headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
      data: { reason: 'out of scope this quarter' },
    });
    expect(denied.status(), `deny: ${await denied.text()}`).toBe(200);
    const deniedBody = await denied.json();
    expect(deniedBody.approval_id).toBe(approvalID);
    expect(deniedBody.status).toBe('denied');

    // No resume: the stored result keeps saying why it never ran.
    const still = await request.get(
      `${nexus.baseURL}/api/v1/requests/${request_id}?business_id=${nexus.businessID}`,
      { headers: bootstrapHeaders(nexus) },
    );
    expect(still.status()).toBe(200);
    const stored = await still.json();
    expect(stored.status).toBe('failed');
    expect(stored.error?.category).toBe('APPROVAL_REQUIRED');
    expect(stored.outcome).toBeUndefined();

    // Denied approvals are not actionable either.
    const after = await request.get(`${nexus.baseURL}/api/v1/approvals?business_id=${nexus.businessID}`, {
      headers: bootstrapHeaders(nexus),
    });
    const afterBody = await after.json();
    expect((afterBody.approvals ?? []).some((a: any) => a.entity_id === approvalID)).toBe(false);
  });

  test('ESCALATE queues an alert that can be acknowledged and resolved', async ({
    request,
    nexus,
  }) => {
    const id = `e2e-escalate-${suffix}`;
    const created = await putPolicy(
      request,
      nexus,
      id,
      policyBody({
        business_id: nexus.businessID,
        effect: 'ESCALATE',
        precedence: 3,
      }),
    );
    expect(created.status(), `create escalation policy: ${await created.text()}`).toBe(200);

    const submitted = await submit(request, nexus, {
      intent: 'work that needs a higher authority',
      business_id: nexus.businessID,
      actor_id: nexus.actorID,
    });
    expect(submitted.status()).toBe(202);
    const { request_id } = await submitted.json();

    const result = await pollResult(request, nexus, request_id, nexus.businessID);
    expect(result.status).toBe('failed');
    expect(result.error?.code, 'escalation is distinguishable from a plain deny').toBe(
      'ESCALATION_REQUIRED',
    );
    expect(result.error?.category, 'CORE §3 has no escalation category').toBe('POLICY_DENIED');
    expect(result.error?.retryable).toBe(false);
    const escalationRef = result.error?.details?.escalation_ref;
    expect(escalationRef, 'the caller is handed the alert reference').toBeTruthy();

    const esc = await waitForEscalation(request, nexus, escalationRef!);
    expect(esc.status).toBe('pending');
    expect(esc.business_id).toBe(nexus.businessID);
    expect(esc.request_id).toBe(request_id);
    expect(esc.reason).toContain(`matched policy ${id}`);

    // A response without reasoning is not a response.
    const noReasoning = await request.post(
      `${nexus.baseURL}/api/v1/escalations/${encodeURIComponent(escalationRef!)}/ack?business_id=${nexus.businessID}`,
      { headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' }, data: {} },
    );
    await expectEnvelope(noReasoning, 400, 'VALIDATION', 'VALIDATION', 'ack without reasoning');

    const acked = await request.post(
      `${nexus.baseURL}/api/v1/escalations/${encodeURIComponent(escalationRef!)}/ack?business_id=${nexus.businessID}`,
      {
        headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
        data: { reasoning: 'seen, looking into it' },
      },
    );
    expect(acked.status(), `ack: ${await acked.text()}`).toBe(200);
    const ackedBody = await acked.json();
    expect(ackedBody.escalation_id).toBe(escalationRef);
    expect(ackedBody.status).toBe('acknowledged');
    expect(ackedBody.accepted).toBe(true);

    // Acknowledge is single-shot: pending → acknowledged only.
    const ackedAgain = await request.post(
      `${nexus.baseURL}/api/v1/escalations/${encodeURIComponent(escalationRef!)}/ack?business_id=${nexus.businessID}`,
      {
        headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
        data: { reasoning: 'acking again' },
      },
    );
    await expectEnvelope(ackedAgain, 409, 'CONFLICT', 'CONFLICT', 'second ack');

    const resolved = await request.post(
      `${nexus.baseURL}/api/v1/escalations/${encodeURIComponent(escalationRef!)}/resolve?business_id=${nexus.businessID}`,
      {
        headers: { ...bootstrapHeaders(nexus), 'Content-Type': 'application/json' },
        data: { reasoning: 'handled outside the platform' },
      },
    );
    expect(resolved.status(), `resolve: ${await resolved.text()}`).toBe(200);
    const resolvedBody = await resolved.json();
    expect(resolvedBody.status).toBe('resolved');
    expect(resolvedBody.accepted).toBe(true);

    const after = await request.get(escalationsURL(nexus), {
      headers: bootstrapHeaders(nexus),
    });
    const afterBody = await after.json();
    const final = (afterBody.escalations ?? []).find(
      (e: any) => e.escalation_id === escalationRef,
    );
    expect(final, 'the alert stays visible after it is resolved').toBeTruthy();
    expect(final.status).toBe('resolved');
    expect(final.resolved_by).toBe(nexus.actorID);
    expect(final.resolution).toBe('handled outside the platform');
  });
});
