/**
 * React Query hooks for NEXUS API
 * Provides typed, cached data fetching with proper error handling
 */

import { useQuery, useMutation, useQueryClient, type UseQueryOptions, type UseMutationOptions } from '@tanstack/react-query';
import { apiClient } from './client';
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

// SSE hook for real-time execution events.
// Connects to /events?business_id=... with auth headers and dispatches events
// to callbacks. Automatically reconnects on disconnect.
export function useEventStream(
  businessId: string,
  handlers: {
    onEvent?: (event: { type: string; data: unknown }) => void;
    onApprovalRequested?: (data: unknown) => void;
    onApprovalApproved?: (data: unknown) => void;
    onApprovalDenied?: (data: unknown) => void;
    onChainCompleted?: (data: unknown) => void;
    onChainFailed?: (data: unknown) => void;
    onError?: (error: Event) => void;
  } = {}
) {
  const streamRef = useRef<EventStream | null>(null);

  useEffect(() => {
    if (!businessId) return;

    const stream = createEventStream(
      businessId,
      (msg: MessageEvent) => {
        const data = msg.data as { type?: string; [key: string]: unknown };
        handlers.onEvent?.({ type: data.type ?? 'unknown', data });
        switch (data.type) {
          case 'approval.requested':
            handlers.onApprovalRequested?.(data);
            break;
          case 'approval.approved':
            handlers.onApprovalApproved?.(data);
            break;
          case 'approval.denied':
            handlers.onApprovalDenied?.(data);
            break;
          case 'chain.completed':
            handlers.onChainCompleted?.(data);
            break;
          case 'chain.failed':
            handlers.onChainFailed?.(data);
            break;
        }
      },
      (err) => {
        handlers.onError?.(err);
      },
    );
    streamRef.current = stream;

    return () => {
      stream.close();
      streamRef.current = null;
    };
  }, [businessId, handlers]);

  return {
    close: () => streamRef.current?.close(),
  };
}
