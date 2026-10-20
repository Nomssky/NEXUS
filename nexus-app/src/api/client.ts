/**
 * NEXUS API Client
 * Thin wrapper around fetch with proper auth, error handling, and TypeScript types
 */

import type {
  AuthHeaders,
  ControlHeaders,
  Envelope,
  SubmitIntelligenceRequest,
  SubmitIntelligenceResponse,
  ExecutionStatus,
  ExecutionTrace,
  TraceEntry,
  Agent,
  CreateAgentRequest,
  Approval,
  ApproveRequest,
  Tool,
  MemoryRecord,
  MemoryQueryRequest,
  MemoryQueryResponse,
  CapabilityState,
  HealthResponse,
  StatusResponse,
  PaginationParams,
  APIError,
} from '../types/api';

const DEFAULT_BASE_URL = '/api/v1';

class APIErrorImpl extends Error implements APIError {
  status: number;
  envelope?: Envelope;

  constructor(message: string, status: number, envelope?: Envelope) {
    super(message);
    this.name = 'APIError';
    this.status = status;
    this.envelope = envelope;
  }
}

function createAuthHeaders(actorId: string, credential: string): AuthHeaders {
  return {
    'X-Actor-ID': actorId,
    'X-Actor-Credential': credential,
  };
}

function createControlHeaders(apiKey: string): ControlHeaders {
  return {
    'X-API-Key': apiKey,
  };
}

async function handleResponse<T>(response: Response): Promise<T> {
  const contentType = response.headers.get('content-type');

  if (!response.ok) {
    let envelope: Envelope | undefined;
    let message = `HTTP ${response.status}: ${response.statusText}`;

    if (contentType?.includes('application/json')) {
      try {
        const errorData = await response.json();
        envelope = errorData;
        message = errorData.message || message;
      } catch {
        // Ignore JSON parse errors
      }
    } else {
      const text = await response.text();
      message = text || message;
    }

    throw new APIErrorImpl(message, response.status, envelope);
  }

  if (response.status === 204) {
    return undefined as T;
  }

  if (contentType?.includes('application/json')) {
    return response.json();
  }

  return response.text() as T;
}

class NexusAPIClient {
  private baseURL: string;
  private defaultHeaders: Record<string, string> = {
    'Content-Type': 'application/json',
  };
  private authHeaders: AuthHeaders | null = null;
  private controlKey: string | null = null;

  constructor(baseURL: string = DEFAULT_BASE_URL) {
    this.baseURL = baseURL;
  }

  baseUrl(): string {
    return this.baseURL;
  }

  setAuth(actorId: string, credential: string): void {
    this.authHeaders = createAuthHeaders(actorId, credential);
  }

  setControlKey(key: string): void {
    this.controlKey = key;
  }

  clearAuth(): void {
    this.authHeaders = null;
    this.controlKey = null;
  }

  getHeaders(useControl: boolean = false): Record<string, string> {
    const headers: Record<string, string> = { ...this.defaultHeaders };

    if (useControl && this.controlKey) {
      Object.assign(headers, createControlHeaders(this.controlKey));
    } else if (this.authHeaders) {
      Object.assign(headers, this.authHeaders);
    }

    return headers;
  }

  private async request<T>(
    method: string,
    path: string,
    body?: unknown,
    options: { useControl?: boolean; baseUrl?: string } = {}
  ): Promise<T> {
    const base = options.baseUrl ?? this.baseURL;
    const url = `${base}${path}`;
    const headers = this.getHeaders(options.useControl);

    const response = await fetch(url, {
      method,
      headers,
      body: body ? JSON.stringify(body) : undefined,
      credentials: 'include',
    });

    return handleResponse<T>(response);
  }

  // Health — served by the health server on a different port than the API
  // gateway. In development these are proxied directly (not via /api/v1).
  async getHealth(): Promise<HealthResponse> {
    return this.request<HealthResponse>('GET', '/health', undefined, { baseUrl: '' });
  }

  async getStatus(): Promise<StatusResponse> {
    return this.request<StatusResponse>('GET', '/status', undefined, { baseUrl: '' });
  }

  // Intelligence / Executions
  async submitIntelligence(request: SubmitIntelligenceRequest): Promise<SubmitIntelligenceResponse> {
    return this.request<SubmitIntelligenceResponse>('POST', '/intelligence/execute', request);
  }

  async getIntelligence(id: string, businessId: string): Promise<ExecutionStatus> {
    return this.request<ExecutionStatus>('GET', `/intelligence/${encodeURIComponent(id)}?business_id=${businessId}`);
  }

  async getIntelligenceTrace(
    id: string,
    businessId: string,
    params?: PaginationParams
  ): Promise<ExecutionTrace> {
    const searchParams = new URLSearchParams({ business_id: businessId });
    if (params?.limit) searchParams.set('limit', params.limit.toString());
    if (params?.after_seq !== undefined) searchParams.set('after_seq', params.after_seq.toString());

    return this.request<ExecutionTrace>('GET', `/intelligence/${encodeURIComponent(id)}/trace?${searchParams}`);
  }

  async cancelIntelligence(id: string, businessId: string): Promise<void> {
    return this.request('POST', `/intelligence/${encodeURIComponent(id)}/cancel?business_id=${businessId}`);
  }

  // Approvals
  async listApprovals(businessId: string): Promise<{ approvals: Approval[] }> {
    return this.request('GET', `/approvals?business_id=${businessId}`);
  }

  async approveApproval(id: string, businessId: string, request: ApproveRequest): Promise<{ approval_id: string; status: string }> {
    return this.request('POST', `/approvals/${encodeURIComponent(id)}/approve?business_id=${businessId}`, request);
  }

  async denyApproval(id: string, businessId: string, request: ApproveRequest): Promise<{ approval_id: string; status: string }> {
    return this.request('POST', `/approvals/${encodeURIComponent(id)}/deny?business_id=${businessId}`, request);
  }

  // Agents
  async listAgents(businessId: string): Promise<{ agents: Agent[] }> {
    return this.request('GET', `/agents?business_id=${businessId}`);
  }

  async getAgent(id: string, businessId: string): Promise<Agent> {
    return this.request('GET', `/agents/${encodeURIComponent(id)}?business_id=${businessId}`);
  }

  async createAgent(request: CreateAgentRequest): Promise<Agent> {
    return this.request('POST', '/agents', request);
  }

  // Tools
  async listTools(): Promise<{ tools: Tool[] }> {
    return this.request('GET', '/tools');
  }

  // Capabilities
  async listCapabilities(): Promise<{ capabilities: CapabilityState[] }> {
    return this.request('GET', '/control/capabilities');
  }

  async setCapabilityState(toolId: string, state: 'enabled' | 'disabled'): Promise<void> {
    return this.request('POST', `/control/capabilities/${encodeURIComponent(toolId)}/state`, { state }, { useControl: true });
  }

  // Memory
  async queryMemory(request: MemoryQueryRequest): Promise<MemoryQueryResponse> {
    return this.request('POST', '/memory/query', request);
  }

  async createMemory(data: Partial<MemoryRecord>): Promise<MemoryRecord> {
    return this.request('POST', '/memory', data);
  }

  async getMemory(id: string): Promise<MemoryRecord> {
    return this.request('GET', `/memory/${encodeURIComponent(id)}`);
  }

  async updateMemory(id: string, data: Partial<MemoryRecord>): Promise<MemoryRecord> {
    return this.request('PATCH', `/memory/${encodeURIComponent(id)}`, data);
  }

  async deleteMemory(id: string): Promise<void> {
    return this.request('DELETE', `/memory/${encodeURIComponent(id)}`);
  }

  // Identities (control plane)
  async listIdentities(businessId: string, useControl: boolean = true): Promise<{ identities: Array<{ entity_id: string; identity_type: string; display_name: string; business_id: string; status: string }> }> {
    return this.request('GET', `/identities?business_id=${businessId}`, undefined, { useControl });
  }

  async createIdentity(data: { entity_id: string; identity_type: 'human' | 'agent'; display_name: string; business_id: string; credential?: string; credential_method?: 'password' }): Promise<void> {
    return this.request('POST', '/identities', data, { useControl: true });
  }

  // Policies (control plane)
  async listPolicies(useControl: boolean = true): Promise<{ policies: unknown[] }> {
    return this.request('GET', '/control/policies', undefined, { useControl });
  }

  async putPolicy(id: string, policy: unknown): Promise<void> {
    return this.request('PUT', `/control/policies/${encodeURIComponent(id)}`, policy, { useControl: true });
  }
}

// SSE Event Stream
//
// Browser EventSource cannot send custom headers (X-Actor-ID / X-Actor-Credential),
// but the backend requires authentication on /events. We therefore implement a
// fetch-based SSE client that sends the auth headers, matching the EventSource
// callback interface (onmessage / onerror / close).
//
export type EventStreamHandlers = {
  onMessage?: (event: MessageEvent) => void;
  onError?: (error: Event) => void;
  onOpen?: () => void;
};

export class EventStream {
  private controller: AbortController | null = null;
  private handlers: EventStreamHandlers;

  constructor(
    private api: NexusAPIClient,
    private businessId: string,
    handlers: EventStreamHandlers = {}
  ) {
    this.handlers = handlers;
  }

  connect(): void {
    this.close();
    const controller = new AbortController();
    this.controller = controller;

    const base = this.api.baseUrl();
    const url = `${base.replace('/api/v1', '')}/events?business_id=${encodeURIComponent(this.businessId)}`;

    fetch(url, {
      method: 'GET',
      headers: {
        Accept: 'text/event-stream',
        Cache: 'no-cache',
        ...this.api.getHeaders(true), // auth headers
      },
      credentials: 'include',
      signal: controller.signal,
    })
      .then((response) => {
        if (!response.ok || !response.body) {
          throw new Error(`SSE connection failed: HTTP ${response.status}`);
        }
        this.handlers.onOpen?.();
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';
        const pump = () => {
          reader.read().then(({ done, value }) => {
            if (done) {
              this.handlers.onError?.(
                new Event('error', { bubbles: false, cancelable: false })
              );
              return;
            }
            buffer += decoder.decode(value, { stream: true });
            const lines = buffer.split('\n');
            buffer = lines.pop() ?? ''; // keep incomplete last line
            for (const line of lines) {
              if (line.startsWith('data: ')) {
                const data = line.slice(6);
                try {
                  const parsed = JSON.parse(data);
                  const msg = new MessageEvent('message', { data: parsed });
                  this.handlers.onMessage?.(msg);
                } catch {
                  // non-JSON data frame — emit raw
                  const msg = new MessageEvent('message', { data });
                  this.handlers.onMessage?.(msg);
                }
              }
            }
            pump();
          });
        };
        pump();
      })
      .catch((err) => {
        if (err.name === 'AbortError') return;
        this.handlers.onError?.(
          new Event('error', { bubbles: false, cancelable: false })
        );
      });
  }

  close(): void {
    this.controller?.abort();
    this.controller = null;
  }
}

// Backwards-compatible factory: create an EventStream-like object with the
// EventSource callback interface. Uses the shared apiClient so auth headers
// and base URL are consistent with the rest of the API.
export function createEventStream(
  businessId: string,
  onMessage: (event: MessageEvent) => void,
  onError?: (error: Event) => void
): EventStream {
  const stream = new EventStream(apiClient, businessId, { onMessage, onError });
  stream.connect();
  return stream;
}

// Singleton instance
export const apiClient = new NexusAPIClient();

// Helper to create a new client instance (useful for testing or different base URLs)
export function createAPIClient(baseURL?: string): NexusAPIClient {
  return new NexusAPIClient(baseURL);
}

export { APIErrorImpl as APIError };