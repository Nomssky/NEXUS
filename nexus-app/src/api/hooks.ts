/**
 * React Query hooks for NEXUS API
 * Provides typed, cached data fetching with proper error handling
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { useQuery, useMutation, useQueryClient, type UseQueryOptions, type UseMutationOptions } from '@tanstack/react-query';
import { apiClient, EventStream, type EventStreamState } from './client';
import type {
  ExecutionStatus,
  ExecutionTrace,
  ExecutionTraceEntry,
  Agent,
  Approval,
  Tool,
  MemoryRecord,
  MemoryQueryRequest,
  MemoryQueryResponse,
  CapabilityState,
  HealthResponse,
  StatusResponse,
  SubmitIntelligenceRequest,
  SubmitIntelligenceResponse,
  ApproveRequest,
  CreateAgentRequest,
  PaginationParams,
  DurableExecution,
  DurableExecutionList,
  ResearchComparison,
} from '../types/api';

// Type for query options that excludes queryKey (provided by the hook)
type QueryOptions<T> = Omit<UseQueryOptions<T>, 'queryKey' | 'queryFn'>;

// Query keys
export const queryKeys = {
  health: ['health'] as const,
  status: ['status'] as const,
  intelligence: (id: string, businessId: string) => ['intelligence', id, businessId] as const,
  intelligenceTrace: (id: string, businessId: string, params?: PaginationParams) =>
    ['intelligenceTrace', id, businessId, params] as const,
  approvals: (businessId: string) => ['approvals', businessId] as const,
  agents: (businessId: string) => ['agents', businessId] as const,
  agent: (id: string, businessId: string) => ['agent', id, businessId] as const,
  tools: ['tools'] as const,
  capabilities: ['capabilities'] as const,
  memory: (params: MemoryQueryRequest) => ['memory', params] as const,
  recoveryExecutions: (businessId: string) => ['recoveryExecutions', businessId] as const,
  recoveryExecution: (id: string, businessId: string) =>
    ['recoveryExecution', businessId, id] as const,
  recoveryComparison: (businessId: string, leftId: string, rightId: string) =>
    ['recoveryComparison', businessId, leftId, rightId] as const,
};

// Health & Status
export function useHealth(options?: QueryOptions<HealthResponse>) {
  return useQuery({
    queryKey: queryKeys.health,
    queryFn: () => apiClient.getHealth(),
    refetchInterval: 30000,
    ...options,
  });
}

export function useStatus(options?: QueryOptions<StatusResponse>) {
  return useQuery({
    queryKey: queryKeys.status,
    queryFn: () => apiClient.getStatus(),
    refetchInterval: 60000,
    ...options,
  });
}

// Intelligence / Executions
export function useIntelligence(id: string, businessId: string, options?: QueryOptions<ExecutionStatus>) {
  return useQuery({
    queryKey: queryKeys.intelligence(id, businessId),
    queryFn: () => apiClient.getIntelligence(id, businessId),
    enabled: !!id && !!businessId,
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === 'pending' || status === 'running' ? 2000 : false;
    },
    ...options,
  });
}

export function useIntelligenceTrace(
  id: string,
  businessId: string,
  params?: PaginationParams,
  options?: QueryOptions<ExecutionTrace>
) {
  return useQuery({
    queryKey: queryKeys.intelligenceTrace(id, businessId, params),
    queryFn: () => apiClient.getIntelligenceTrace(id, businessId, params),
    enabled: !!id && !!businessId,
    ...options,
  });
}

export function useSubmitIntelligence(
  options?: UseMutationOptions<SubmitIntelligenceResponse, Error, SubmitIntelligenceRequest>
) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (request) => apiClient.submitIntelligence(request),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['intelligence'] });
      queryClient.invalidateQueries({ queryKey: ['approvals'] });
    },
    ...options,
  });
}

export function useCancelIntelligence(
  options?: UseMutationOptions<void, Error, { id: string; businessId: string }>
) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ id, businessId }) => apiClient.cancelIntelligence(id, businessId),
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.intelligence(variables.id, variables.businessId) });
    },
    ...options,
  });
}

// Approvals
export function useApprovals(businessId: string, options?: QueryOptions<{ approvals: Approval[] }>) {
  return useQuery({
    queryKey: queryKeys.approvals(businessId),
    queryFn: () => apiClient.listApprovals(businessId),
    enabled: !!businessId,
    refetchInterval: 5000,
    ...options,
  });
}

export function useApproveApproval(
  options?: UseMutationOptions<{ approval_id: string; status: string }, Error, { id: string; businessId: string; request: ApproveRequest }>
) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ id, businessId, request }) => apiClient.approveApproval(id, businessId, request),
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.approvals(variables.businessId) });
      queryClient.invalidateQueries({ queryKey: ['intelligence'] });
    },
    ...options,
  });
}

export function useDenyApproval(
  options?: UseMutationOptions<{ approval_id: string; status: string }, Error, { id: string; businessId: string; request: ApproveRequest }>
) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ id, businessId, request }) => apiClient.denyApproval(id, businessId, request),
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.approvals(variables.businessId) });
      queryClient.invalidateQueries({ queryKey: ['intelligence'] });
    },
    ...options,
  });
}

// Agents
export function useAgents(businessId: string, options?: QueryOptions<{ agents: Agent[] }>) {
  return useQuery({
    queryKey: queryKeys.agents(businessId),
    queryFn: () => apiClient.listAgents(businessId),
    enabled: !!businessId,
    ...options,
  });
}

export function useAgent(id: string, businessId: string, options?: QueryOptions<Agent>) {
  return useQuery({
    queryKey: queryKeys.agent(id, businessId),
    queryFn: () => apiClient.getAgent(id, businessId),
    enabled: !!id && !!businessId,
    ...options,
  });
}

export function useCreateAgent(
  options?: UseMutationOptions<Agent, Error, CreateAgentRequest>
) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (request) => apiClient.createAgent(request),
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.agents(variables.business_id) });
    },
    ...options,
  });
}

// Tools
export function useTools(options?: QueryOptions<{ tools: Tool[] }>) {
  return useQuery({
    queryKey: queryKeys.tools,
    queryFn: () => apiClient.listTools(),
    ...options,
  });
}

export function useCapabilities(options?: QueryOptions<{ capabilities: CapabilityState[] }>) {
  return useQuery({
    queryKey: queryKeys.capabilities,
    queryFn: () => apiClient.listCapabilities(),
    ...options,
  });
}

// Memory
export function useMemory(params: MemoryQueryRequest, options?: QueryOptions<MemoryQueryResponse>) {
  return useQuery({
    queryKey: queryKeys.memory(params),
    queryFn: () => apiClient.queryMemory(params),
    enabled: !!params.business_id,
    ...options,
  });
}

// Durable recovery executions — the authoritative execution state. Polled on a
// moderate interval and reconciled after SSE reconnects (see useEventStream).
export function useRecoveryExecutions(
  businessId: string,
  options?: QueryOptions<DurableExecutionList>
) {
  return useQuery({
    queryKey: queryKeys.recoveryExecutions(businessId),
    queryFn: () => apiClient.listRecoveryExecutions(businessId, { limit: 50 }),
    enabled: !!businessId,
    refetchInterval: 5000,
    ...options,
  });
}

// Single-execution recovery detail — the only read exposing the durable
// research evidence snapshot and the real completed_at timestamp. Polled while
// the execution is not yet terminal so evidence appears as soon as the
// terminal journal write lands; a terminal state stops polling because the
// durable record cannot change again. Rejected queries (e.g. a 404 from an
// older backend that does not expose the detail read) surface as an error, and
// callers fall back to the process-local execution read.
export function useRecoveryExecution(
  id: string | null | undefined,
  businessId: string,
  options?: QueryOptions<DurableExecution>
) {
  return useQuery({
    queryKey: queryKeys.recoveryExecution(id || '', businessId),
    queryFn: () => apiClient.getRecoveryExecution(id as string, businessId),
    enabled: !!id && !!businessId,
    retry: false,
    refetchInterval: (query) => {
      const state = query.state.data?.state;
      const terminal =
        state === 'completed' ||
        state === 'failed' ||
        state === 'cancelled' ||
        state === 'timed_out' ||
        state === 'unknown';
      return terminal ? false : 5000;
    },
    ...options,
  });
}

// Deterministic comparison of two eligible durable research snapshots. It is
// fetched only for an explicitly requested pair and never polls: both inputs
// are settled records, and the comparison itself is a read-only projection.
export function useRecoveryComparison(
  businessId: string,
  leftExecutionId: string,
  rightExecutionId: string,
  options?: QueryOptions<ResearchComparison>
) {
  return useQuery({
    queryKey: queryKeys.recoveryComparison(businessId, leftExecutionId, rightExecutionId),
    queryFn: () =>
      apiClient.compareRecoveryExecutions(businessId, leftExecutionId, rightExecutionId),
    enabled:
      !!businessId &&
      !!leftExecutionId &&
      !!rightExecutionId &&
      leftExecutionId !== rightExecutionId,
    retry: false,
    refetchOnWindowFocus: false,
    staleTime: 60_000,
    ...options,
  });
}

// SSE hook for real-time execution events.
//
// Connects to /events?business_id=... with auth headers and dispatches events
// to callbacks. The underlying EventStream reconnects with bounded exponential
// backoff after transient failures and never retries permanent auth failures.
//
// Reconciliation strategy: SSE is a hint, never the source of truth. Polling
// (useIntelligence / useApprovals) remains authoritative. On reconnect the hook
// invalidates those queries so the UI reconciles from the backend, and callers
// receive onReconnected to trigger any additional authoritative reads.
//
// Handler changes do not tear down the connection: handlers are kept in a ref
// so the latest callbacks are always invoked. Only a businessId change (or
// unmount) closes and recreates the stream.
export function useEventStream(
  businessId: string,
  handlers: {
    onEvent?: (event: { type: string; data: unknown }) => void;
    onApprovalRequested?: (data: unknown) => void;
    onApprovalApproved?: (data: unknown) => void;
    onApprovalDenied?: (data: unknown) => void;
    onChainCompleted?: (data: unknown) => void;
    onChainFailed?: (data: unknown) => void;
    onStateChange?: (state: EventStreamState) => void;
    onReconnected?: () => void;
    onError?: (error: Event) => void;
    onOpen?: () => void;
  } = {}
) {
  const streamRef = useRef<EventStream | null>(null);
  const [connectionState, setConnectionState] = useState<EventStreamState>('closed');
  const [retryDelayMs, setRetryDelayMs] = useState(0);
  const handlersRef = useRef(handlers);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  // Keep the latest handlers available to the stream without tearing down the
  // connection on every callback identity change. Updated after render.
  useEffect(() => {
    handlersRef.current = handlers;
  });
  const queryClient = useQueryClient();

  // refreshRetryDelay must be defined at the top level of the hook (not inside
  // a useEffect) to comply with React's rules of hooks. It reads from a ref so
  // it is referentially stable and does not need to be re-created.
  const refreshRetryDelay = useCallback(() => {
    setRetryDelayMs(streamRef.current?.getRetryDelayMs() ?? 0);
  }, []);

  // Refresh the retry countdown while reconnecting. The displayed value is
  // always derived from the stream's own backoff scheduler; this interval
  // only triggers re-renders so the countdown ticks down smoothly.
  useEffect(() => {
    if (connectionState === 'reconnecting') {
      refreshRetryDelay();
      if (!intervalRef.current) {
        intervalRef.current = setInterval(refreshRetryDelay, 1000);
      }
    } else {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
      setRetryDelayMs(0);
    }
  }, [connectionState, refreshRetryDelay]);

  // Cleanup interval on unmount.
  useEffect(() => () => {
    if (intervalRef.current) clearInterval(intervalRef.current);
  }, []);

  // Create and connect the EventStream. SSE reconnects internally on transient
  // failures; permanent auth failures (401/403) are handled by the stream.
  useEffect(() => {
    if (!businessId) return;

    const dispatch = (msg: MessageEvent) => {
      const data = msg.data as { event_type?: string; type?: string; [key: string]: unknown };
      const type = data.event_type ?? data.type ?? 'unknown';
      handlersRef.current.onEvent?.({ type, data });
      switch (type) {
        case 'approval.requested':
          handlersRef.current.onApprovalRequested?.(data);
          break;
        case 'approval.approved':
          handlersRef.current.onApprovalApproved?.(data);
          break;
        case 'approval.denied':
          handlersRef.current.onApprovalDenied?.(data);
          break;
        case 'chain.completed':
          handlersRef.current.onChainCompleted?.(data);
          break;
        case 'chain.failed':
          handlersRef.current.onChainFailed?.(data);
          break;
      }
    };

    const stream = new EventStream(apiClient, businessId, {
      onMessage: dispatch,
      onOpen: () => handlersRef.current.onOpen?.(),
      onError: (err) => handlersRef.current.onError?.(err),
      onStateChange: (state) => {
        setConnectionState(state);
        setRetryDelayMs(streamRef.current?.getRetryDelayMs() ?? 0);
        handlersRef.current.onStateChange?.(state);
      },
      onReconnected: () => {
        // Reconcile authoritative state after a reconnect: invalidate the
        // queries so the UI reflects the backend, not just SSE hints.
        queryClient.invalidateQueries({ queryKey: ['intelligence'] });
        queryClient.invalidateQueries({ queryKey: ['approvals'] });
        queryClient.invalidateQueries({ queryKey: ['recoveryExecutions'] });
        handlersRef.current.onReconnected?.();
      },
    });
    streamRef.current = stream;
    stream.connect();

    // Cleanup: abort active fetch, clear pending retry timer.
    return () => {
      stream.close();
      streamRef.current = null;
    };
  }, [businessId, queryClient]);

  return {
    connectionState,
    retryDelayMs,
    close: () => streamRef.current?.close(),
  };
}
