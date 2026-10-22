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
  ExecutionStatusValue,
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
  DurableExecution,
  DurableExecutionList,
  ResearchSynthesis,
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
    // Backend Response has request_id, outcome.summary, duration — map to
    // frontend ExecutionStatus shape (execution_id, output, duration_ms).
    const raw = await this.request<{
      request_id?: string;
      execution_id?: string;
      business_id?: string;
      division_id?: string;
      status: string;
      outcome?: {
        summary?: string;
        artifacts?: string[];
        metrics?: Record<string, unknown>;
        research_synthesis?: ResearchSynthesis;
      };
      error?: {
        code: string;
        category: string;
        message: string;
        details?: Record<string, unknown>;
      };
      audit_trace?: Array<{
        step: string;
        action: string;
        actor: string;
        timestamp: string;
        duration: number;
        outcome: string;
      }>;
      duration?: number;
    }>('GET', `/intelligence/${encodeURIComponent(id)}?business_id=${businessId}`);

    // The gateway returns outcome.metrics.duration_ms for completed
    // executions, and a Go time.Duration (nanoseconds) in `duration`.
    // Prefer the explicit metric; otherwise convert nanoseconds to
    // milliseconds. Never expose the raw nanoseconds value as duration_ms.
    const metrics = raw.outcome?.metrics;
    const metricDuration =
      typeof metrics?.duration_ms === 'number' && Number.isFinite(metrics.duration_ms)
        ? metrics.duration_ms
        : undefined;
    const durationMs =
      metricDuration ??
      (typeof raw.duration === 'number' && Number.isFinite(raw.duration)
        ? Math.round(raw.duration / 1_000_000)
        : undefined);

    return {
      execution_id: raw.request_id ?? raw.execution_id ?? id,
      status: raw.status as ExecutionStatusValue,
      error: raw.error,
      output: raw.outcome?.summary,
      research_synthesis: raw.outcome?.research_synthesis,
      duration_ms: durationMs,
    };
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

  // Durable recovery read (authoritative execution state)
  async listRecoveryExecutions(
    businessId: string,
    params?: { limit?: number; offset?: number; divisionId?: string }
  ): Promise<DurableExecutionList> {
    const searchParams = new URLSearchParams({ business_id: businessId });
    if (params?.limit) searchParams.set('limit', params.limit.toString());
    if (params?.offset) searchParams.set('offset', params.offset.toString());
    if (params?.divisionId) searchParams.set('division_id', params.divisionId);
    return this.request<DurableExecutionList>('GET', `/recovery/executions?${searchParams}`);
  }

  // Single-execution recovery detail. This is the only read that exposes the
  // durable research_evidence snapshot and the real completed_at timestamp.
  // A 404 on an older backend is a normal "not exposed" signal: callers fall
  // back to the process-local execution read instead of treating it as fatal.
  async getRecoveryExecution(id: string, businessId: string): Promise<DurableExecution> {
    return this.request<DurableExecution>(
      'GET',
      `/recovery/executions/${encodeURIComponent(id)}?business_id=${businessId}`
    );
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
// fetch-based SSE client that sends the auth headers and reconnects with
// bounded exponential backoff after transient failures.
//
// Lifecycle guarantees:
//   - Transient failures (network error, non-2xx except 401/403, stream end)
//     trigger a reconnect with exponential backoff + jitter, capped at maxDelayMs.
//   - A successful connection resets the backoff.
//   - Permanent authorization failures (401/403) do NOT retry.
//   - close()/dispose() clears pending retries and aborts the active fetch.
//   - Malformed frames are emitted raw and never crash the consumer.
//   - Duplicate events (same event_id) are dropped via a bounded set.
//   - onReconnected fires after a successful reconnect so callers can reconcile
//     authoritative state; SSE is a hint, never the source of truth.
//
export type EventStreamState = 'closed' | 'connecting' | 'open' | 'reconnecting' | 'failed';

export type EventStreamHandlers = {
  onMessage?: (event: MessageEvent) => void;
  onOpen?: () => void;
  onError?: (error: Event) => void;
  onStateChange?: (state: EventStreamState) => void;
  /** Fired once after a successful reconnect (not on the initial connection). */
  onReconnected?: () => void;
};

export interface EventStreamOptions {
  /** Base backoff delay in ms (default 500). */
  baseDelayMs?: number;
  /** Maximum backoff delay in ms (default 30000). */
  maxDelayMs?: number;
  /** Maximum jitter added to each delay in ms (default 250). */
  jitterMs?: number;
  /** Maximum number of event IDs retained for duplicate suppression (default 1000). */
  maxEvents?: number;
}

export class EventStream {
  private controller: AbortController | null = null;
  private handlers: EventStreamHandlers;
  private options: Required<EventStreamOptions>;
  private retryAttempt = 0;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private disposed = false;
  private state: EventStreamState = 'closed';
  private retryDelayMs: number | null = null;
  private seenEventIds = new Set<string>();

  constructor(
    private api: NexusAPIClient,
    private businessId: string,
    handlers: EventStreamHandlers = {},
    options: EventStreamOptions = {}
  ) {
    this.handlers = handlers;
    this.options = {
      baseDelayMs: options.baseDelayMs ?? 500,
      maxDelayMs: options.maxDelayMs ?? 30000,
      jitterMs: options.jitterMs ?? 250,
      maxEvents: options.maxEvents ?? 1000,
    };
  }

  getState(): EventStreamState {
    return this.state;
  }

  /** Delay in milliseconds for the next reconnect attempt, or null if none is scheduled.
   *  Derived from the stream's own backoff scheduler, not a separate timer. */
  getRetryDelayMs(): number | null {
    if (this.retryTimer === null) return null;
    return this.retryDelayMs ?? null;
  }

  /** Number of reconnect attempts made for the current connection attempt cycle. */
  getRetryAttempt(): number {
    return this.retryAttempt;
  }

  connect(): void {
    if (this.disposed) return;
    this.retryAttempt = 0;
    this.scheduleConnect(0);
  }

  close(): void {
    this.disposed = true;
    this.clearRetryTimer();
    this.abortFetch();
    this.setState('closed');
  }

  dispose(): void {
    this.close();
  }

  private scheduleConnect(delayMs: number): void {
    if (this.disposed) return;
    this.clearRetryTimer();
    if (delayMs <= 0) {
      this.retryDelayMs = null;
      this.attemptConnect();
      return;
    }
    this.retryDelayMs = delayMs;
    this.setState('reconnecting');
    this.retryTimer = setTimeout(() => {
      this.retryTimer = null;
      this.retryDelayMs = null;
      this.attemptConnect();
    }, delayMs);
  }

  private attemptConnect(): void {
    if (this.disposed) return;
    this.abortFetch();
    const controller = new AbortController();
    this.controller = controller;
    this.setState('connecting');

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
        if (this.disposed || controller.signal.aborted) return;
        // Permanent authorization failure: do not retry.
        if (response.status === 401 || response.status === 403) {
          this.setState('failed');
          this.handlers.onError?.(new Event('auth-error'));
          return;
        }
        if (!response.ok || !response.body) {
          this.scheduleReconnect();
          return;
        }
        const wasReconnect = this.retryAttempt > 0;
        this.retryAttempt = 0;
        this.setState('open');
        this.handlers.onOpen?.();
        if (wasReconnect) {
          this.handlers.onReconnected?.();
        }
        this.readStream(response.body);
      })
      .catch((err: unknown) => {
        if (this.disposed) return;
        if (err instanceof Error && err.name === 'AbortError') return;
        this.scheduleReconnect();
      });
  }

  private readStream(body: ReadableStream<Uint8Array>): void {
    const reader = body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';

    const pump = (): void => {
      reader
        .read()
        .then(({ done, value }) => {
          if (this.disposed) {
            reader.releaseLock();
            return;
          }
          if (done) {
            reader.releaseLock();
            this.scheduleReconnect();
            return;
          }
          buffer += decoder.decode(value, { stream: true });
          const lines = buffer.split('\n');
          buffer = lines.pop() ?? '';
          for (const line of lines) {
            this.handleLine(line);
          }
          pump();
        })
        .catch((err: unknown) => {
          if (this.disposed) return;
          if (err instanceof Error && err.name === 'AbortError') return;
          this.scheduleReconnect();
        });
    };
    pump();
  }

  private handleLine(line: string): void {
    if (!line.startsWith('data: ')) return;
    const data = line.slice(6);
    let parsed: { event_id?: string; [key: string]: unknown };
    try {
      parsed = JSON.parse(data);
    } catch {
      // Malformed frame — emit raw without crashing the consumer.
      this.handlers.onMessage?.(new MessageEvent('message', { data }));
      return;
    }
    // Duplicate suppression: bounded set of seen event IDs (insertion order).
    if (parsed.event_id) {
      if (this.seenEventIds.has(parsed.event_id)) return;
      this.seenEventIds.add(parsed.event_id);
      if (this.seenEventIds.size > this.options.maxEvents) {
        const excess = this.seenEventIds.size - this.options.maxEvents;
        let i = 0;
        for (const id of this.seenEventIds) {
          if (i >= excess) break;
          this.seenEventIds.delete(id);
          i++;
        }
      }
    }
    this.handlers.onMessage?.(new MessageEvent('message', { data: parsed }));
  }

  private scheduleReconnect(): void {
    if (this.disposed) return;
    this.setState('reconnecting');
    const expDelay = this.options.baseDelayMs * Math.pow(2, this.retryAttempt);
    const jitter = Math.random() * this.options.jitterMs;
    const delay = Math.min(this.options.maxDelayMs, expDelay + jitter);
    this.retryAttempt = Math.min(this.retryAttempt + 1, 32);
    this.scheduleConnect(delay);
  }

  private clearRetryTimer(): void {
    if (this.retryTimer !== null) {
      clearTimeout(this.retryTimer);
      this.retryTimer = null;
    }
  }

  private abortFetch(): void {
    if (this.controller) {
      this.controller.abort();
      this.controller = null;
    }
  }

  private setState(state: EventStreamState): void {
    if (this.state !== state) {
      this.state = state;
      this.handlers.onStateChange?.(state);
    }
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