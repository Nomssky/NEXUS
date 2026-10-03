import type { APIRequestContext } from '@playwright/test';
import { deletePolicy, listPolicies } from './api';
import type { NexusHandle } from './nexus';

/**
 * Shared helpers for specs that write policies through
 * `/api/v1/control/policies` (contracts/SCHEMA_GOVERNANCE_ATTENTION.md §9).
 *
 * Policies are process-lifetime state on a gateway that is shared by every
 * spec in the worker, so a spec that installs a DENY or a REQUIRE_APPROVAL
 * gate must remove it again even when an assertion fails halfway through —
 * otherwise the next spec inherits a policy it knows nothing about.
 */

/** The §2.2 minimum the gateway does not derive, with per-test overrides. */
export function policyBody(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    policy_type: 'access_control',
    name: 'e2e policy',
    description: 'written by the NEXUS e2e suite',
    status: 'active',
    subject: { subject_type: 'all' },
    action: { action_type: 'custom' },
    resource: { resource_type: 'all' },
    effect: 'ALLOW',
    precedence: 0,
    ...overrides,
  };
}

/** Remove every policy this suite installed (id prefix `e2e-`). */
export async function sweepE2ePolicies(
  request: APIRequestContext,
  nexus: NexusHandle,
): Promise<void> {
  const res = await listPolicies(request, nexus);
  if (res.status() !== 200) return;
  const { policies } = await res.json();
  for (const p of policies ?? []) {
    if (typeof p?.policy_id === 'string' && p.policy_id.startsWith('e2e-')) {
      await deletePolicy(request, nexus, p.policy_id);
    }
  }
}
