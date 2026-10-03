import type { APIRequestContext, APIResponse } from '@playwright/test';
import { expect } from '@playwright/test';
import type { NexusHandle } from './nexus';

/** The error envelope mandated by contracts/CORE_INTERFACE_CONTRACTS.md §3. */
export interface Envelope {
  code: string;
  category: string;
  message: string;
  details?: Record<string, unknown>;
  retryable: boolean;
  correlation_id: string;
  timestamp: string;
}

/**
 * The categories CORE §3 actually defines (nerrors.allCategories — a closed
 * set of 14). There is no NOT_FOUND: a 404 carries code VALIDATION.
 */
export const CATEGORIES = [
  'VALIDATION',
  'AUTH',
  'AUTHORIZATION',
  'POLICY_DENIED',
  'APPROVAL_REQUIRED',
  'RESOURCE_UNAVAILABLE',
  'TIMEOUT',
  'DEPENDENCY_FAILURE',
  'RATE_LIMIT',
  'CONFLICT',
  'UNKNOWN_OUTCOME',
  'CANCELLATION',
  'SECURITY_REJECTION',
  'INTERNAL_FAILURE',
] as const;

export function actorHeaders(actorID: string, credential: string): Record<string, string> {
  return { 'X-Actor-ID': actorID, 'X-Actor-Credential': credential };
}

export function bootstrapHeaders(nexus: NexusHandle): Record<string, string> {
  return actorHeaders(nexus.actorID, nexus.credential);
}

export function controlHeaders(nexus: NexusHandle): Record<string, string> {
  return { 'X-API-Key': nexus.controlKey };
}

/**
 * Decode and structurally validate an error envelope. Fails the test if the
 * body is not the contract shape — a plain-text or ad-hoc JSON body must not
 * be accepted as "an error".
 */
export async function readEnvelope(res: APIResponse, context: string): Promise<Envelope> {
  const raw = await res.text();
  let parsed: any;
  try {
    parsed = JSON.parse(raw);
  } catch {
    throw new Error(`${context}: body is not JSON (${raw.slice(0, 300)})`);
  }
  if (!parsed || typeof parsed !== 'object' || !parsed.error) {
    throw new Error(`${context}: body has no top-level "error" envelope (${raw.slice(0, 300)})`);
  }
  const e = parsed.error;
  for (const field of ['code', 'category', 'message', 'timestamp', 'correlation_id'] as const) {
    if (typeof e[field] !== 'string' || e[field] === '') {
      throw new Error(`${context}: envelope.${field} missing or not a string (${raw.slice(0, 300)})`);
    }
  }
  if (typeof e.retryable !== 'boolean') {
    throw new Error(`${context}: envelope.retryable must be a boolean`);
  }
  if (!Number.isFinite(Date.parse(e.timestamp))) {
    throw new Error(`${context}: envelope.timestamp is not an ISO timestamp: ${e.timestamp}`);
  }
  if (!(CATEGORIES as readonly string[]).includes(e.category)) {
    throw new Error(`${context}: envelope.category ${e.category} is not a CORE §3 category`);
  }
  return e as Envelope;
}

export async function expectEnvelope(
  res: APIResponse,
  status: number,
  code: string,
  category: string,
  context: string,
): Promise<Envelope> {
  expect(res.status(), `${context}: status`).toBe(status);
  expect(res.headers()['content-type'] ?? '', `${context}: content-type`).toContain('application/json');
  const e = await readEnvelope(res, context);
  expect(e.code, `${context}: code`).toBe(code);
  expect(e.category, `${context}: category`).toBe(category);
  expect(res.headers()['x-correlation-id'] ?? '', `${context}: X-Correlation-ID header`).toBe(
    e.correlation_id,
  );
  return e;
}

export interface SubmitBody {
  intent: string;
  business_id: string;
  actor_id: string;
  priority?: number;
  constraints?: string[];
}

export async function submit(
  request: APIRequestContext,
  nexus: NexusHandle,
  body: SubmitBody,
  headers: Record<string, string> = bootstrapHeaders(nexus),
): Promise<APIResponse> {
  return request.post(`${nexus.baseURL}/api/v1/requests`, {
    headers: { 'Content-Type': 'application/json', ...headers },
    data: body,
  });
}

export async function getResult(
  request: APIRequestContext,
  nexus: NexusHandle,
  requestID: string,
  businessID: string,
  headers: Record<string, string> = bootstrapHeaders(nexus),
): Promise<APIResponse> {
  return request.get(
    `${nexus.baseURL}/api/v1/requests/${encodeURIComponent(requestID)}?business_id=${encodeURIComponent(businessID)}`,
    { headers },
  );
}

export interface StoredResult {
  request_id: string;
  business_id: string;
  status: string;
  constraints?: string[];
  outcome?: { summary?: string; artifacts?: string[]; metrics?: Record<string, unknown> };
  error?: {
    code?: string;
    category?: string;
    message?: string;
    retryable?: boolean;
    chain_step?: string;
    details?: Record<string, string>;
  };
  audit_trace?: unknown[];
  duration?: number;
}

/**
 * Poll GET /api/v1/requests/{id} until it is terminal (200). Bounded: on
 * timeout the last response, the request id and the process logs are all
 * reported so a hang is diagnosable from the test output alone.
 */
export async function pollResult(
  request: APIRequestContext,
  nexus: NexusHandle,
  requestID: string,
  businessID: string,
  timeoutMs = 30_000,
): Promise<StoredResult> {
  const deadline = Date.now() + timeoutMs;
  let lastStatus = -1;
  let lastBody = '';
  let lastEnvelope: Envelope | null = null;
  while (Date.now() < deadline) {
    const res = await getResult(request, nexus, requestID, businessID);
    lastStatus = res.status();
    lastBody = await res.text();
    if (res.status() === 200) {
      try {
        return JSON.parse(lastBody) as StoredResult;
      } catch {
        /* fall through to the failure report below */
      }
    } else if (res.status() !== 202) {
      try {
        lastEnvelope = JSON.parse(lastBody).error ?? null;
      } catch {
        lastEnvelope = null;
      }
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(
    `request ${requestID} never reached a terminal state in ${timeoutMs}ms\n` +
      `last status: ${lastStatus}\nlast body: ${lastBody.slice(0, 1000)}\n` +
      `last envelope: ${JSON.stringify(lastEnvelope)}\n` +
      `--- nexus stdout ---\n${nexus.stdout().slice(-4000)}\n` +
      `--- nexus stderr ---\n${nexus.stderr().slice(-4000)}`,
  );
}

/**
 * Policy control surface (contracts/SCHEMA_GOVERNANCE_ATTENTION.md §9).
 * These are control-plane calls: X-API-Key, never an identity.
 */
export async function listPolicies(
  request: APIRequestContext,
  nexus: NexusHandle,
): Promise<APIResponse> {
  return request.get(`${nexus.baseURL}/api/v1/control/policies`, {
    headers: controlHeaders(nexus),
  });
}

export async function getPolicy(
  request: APIRequestContext,
  nexus: NexusHandle,
  policyID: string,
): Promise<APIResponse> {
  return request.get(
    `${nexus.baseURL}/api/v1/control/policies/${encodeURIComponent(policyID)}`,
    { headers: controlHeaders(nexus) },
  );
}

export async function putPolicy(
  request: APIRequestContext,
  nexus: NexusHandle,
  policyID: string,
  body: Record<string, unknown>,
): Promise<APIResponse> {
  return request.put(
    `${nexus.baseURL}/api/v1/control/policies/${encodeURIComponent(policyID)}`,
    { headers: { ...controlHeaders(nexus), 'Content-Type': 'application/json' }, data: body },
  );
}

export async function deletePolicy(
  request: APIRequestContext,
  nexus: NexusHandle,
  policyID: string,
): Promise<APIResponse> {
  return request.delete(
    `${nexus.baseURL}/api/v1/control/policies/${encodeURIComponent(policyID)}`,
    { headers: controlHeaders(nexus) },
  );
}

export function randomSuffix(): string {
  return Math.random().toString(36).slice(2, 10);
}
