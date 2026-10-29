/**
 * CommandInput regression tests.
 *
 * Covers agent selection, request serialization, invalidation on scope
 * changes, and duplicate-submit prevention for the Ask NEXUS workflow.
 *
 * Uses container-scoped DOM queries for reliability across test runs.
 */

import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { CommandInput } from './CommandInput';
import type { Agent } from '../../types/api';

// Mock the hooks module so useSubmitIntelligence does not need a live API.
vi.mock('../../api/hooks', () => ({
  useSubmitIntelligence: vi.fn(),
}));

import { useSubmitIntelligence } from '../../api/hooks';

const mockMutate = vi.fn();
const mockIsPending = false;

beforeEach(() => {
  document.body.innerHTML = '';
  vi.resetAllMocks();
  (useSubmitIntelligence as ReturnType<typeof vi.fn>).mockReturnValue({
    mutate: mockMutate,
    isPending: mockIsPending,
  } as any);
});

const createAgent = (overrides: Partial<Agent> = {}): Agent => ({
  entity_id: overrides.entity_id || 'agent-test-1',
  name: overrides.name || 'Test Agent',
  business_id: 'default',
  capabilities: ['web.search'],
  allowed_tools: ['web.search'],
  status: 'active',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  ...overrides,
});

const renderInput = (props: React.ComponentProps<typeof CommandInput>) => {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <CommandInput {...props} />
    </QueryClientProvider>
  );
};

describe('CommandInput agent selection', () => {
  it('renders active agents as select options and excludes suspended agents', () => {
    const agents: Agent[] = [
      createAgent({ entity_id: 'agent-active', name: 'Research Agent', status: 'active' }),
      createAgent({ entity_id: 'agent-suspended', name: 'Suspended Agent', status: 'suspended' }),
      createAgent({ entity_id: 'agent-archived', name: 'Archived Agent', status: 'archived' }),
    ];

    const { container } = renderInput({
      businessId: 'default',
      actorId: 'nx:human:bootstrap',
      agents,
    });

    const select = container.querySelector('select');
    expect(select).not.toBeNull();
    expect(container.querySelector('option[value="agent-active"]')).not.toBeNull();
    expect(container.querySelector('option[value="agent-suspended"]')).toBeNull();
    expect(container.querySelector('option[value="agent-archived"]')).toBeNull();
  });

  it('renders an empty state when no active agents exist', () => {
    const { container } = renderInput({
      businessId: 'default',
      actorId: 'nx:human:bootstrap',
      agents: [],
    });

    expect(container.textContent).toContain('No active agents available');
    expect(container.querySelector('select')).toBeNull();
  });

  it('disables submit when no agent is selected', () => {
    const agents: Agent[] = [createAgent({ name: 'Research Agent' })];

    const { container } = renderInput({ businessId: 'default', actorId: 'nx:human:bootstrap', agents });

    const submitBtn = container.querySelector('button[type="submit"]') as HTMLButtonElement;
    expect(submitBtn).not.toBeNull();
    expect(submitBtn.disabled).toBe(true);
  });

  it('enables submit when an agent is selected', () => {
    const agents: Agent[] = [createAgent({ name: 'Research Agent' })];

    const { container } = renderInput({ businessId: 'default', actorId: 'nx:human:bootstrap', agents });

    // Type text first (button requires both agent and text)
    const textarea = container.querySelector('textarea')!;
    fireEvent.change(textarea, {
      target: { value: 'test objective' },
    });

    const select = container.querySelector('select')!;
    fireEvent.change(select, { target: { value: 'agent-test-1' } });

    const submitBtn = container.querySelector('button[type="submit"]') as HTMLButtonElement;
    expect(submitBtn.disabled).toBe(false);
  });

  it('clears the selected agent when it is no longer in the agent list (scope change)', async () => {
    const initialAgents: Agent[] = [
      createAgent({ entity_id: 'agent-keep', name: 'Keep Agent', status: 'active' }),
      createAgent({ entity_id: 'agent-remove', name: 'Remove Agent', status: 'active' }),
    ];

    const { container, rerender } = renderInput({
      businessId: 'default',
      actorId: 'nx:human:bootstrap',
      agents: initialAgents,
    });

    // Select the agent that will be removed
    const select = container.querySelector('select')!;
    fireEvent.change(select, { target: { value: 'agent-remove' } });
    expect(select.value).toBe('agent-remove');

    // Rerender with the removed agent gone
    rerender(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <CommandInput
          businessId="default"
          actorId="nx:human:bootstrap"
          agents={[initialAgents[0]]} // only agent-keep remains
        />
      </QueryClientProvider>
    );

    // Selection should have cleared
    await waitFor(() => {
      expect(container.querySelector('select')?.value).toBe('');
    });
  });
});

describe('CommandInput request serialization', () => {
  it('sends context.agent_id with the submit request', async () => {
    const agents: Agent[] = [createAgent({ name: 'Research Agent' })];

    const { container, unmount } = renderInput({
      businessId: 'default',
      actorId: 'nx:human:bootstrap',
      agents,
    });
    try {
      const textarea = container.querySelector('textarea')!;
      fireEvent.change(textarea, {
        target: { value: 'research thoroughly: search the web for nexus capabilities' },
      });

      // Select an agent
      const select = container.querySelector('select')!;
      fireEvent.change(select, {
        target: { value: 'agent-test-1' },
      });

      // Submit
      const submitBtn = container.querySelector('button[type="submit"]')!;
      fireEvent.click(submitBtn);

      await waitFor(() => {
        expect(mockMutate).toHaveBeenCalledTimes(1);
      });

      const callArg = mockMutate.mock.calls[0][0];
      expect(callArg.business_id).toBe('default');
      expect(callArg.actor_id).toBe('nx:human:bootstrap');
      expect(callArg.objective).toEqual({ description: 'research thoroughly: search the web for nexus capabilities' });
      expect(callArg.context).toEqual({ agent_id: 'agent-test-1' });
    } finally {
      unmount();
    }
  });

  it('does not submit when no agent is selected', () => {
    const agents: Agent[] = [createAgent({ name: 'Research Agent' })];

    const { container, unmount } = renderInput({
      businessId: 'default',
      actorId: 'nx:human:bootstrap',
      agents,
    });
    try {
      const textarea = container.querySelector('textarea')!;
      fireEvent.change(textarea, {
        target: { value: 'test objective' },
      });

      const submitBtn = container.querySelector('button[type="submit"]')!;
      fireEvent.click(submitBtn);

      expect(mockMutate).not.toHaveBeenCalled();

      // Check for error message text in container
      const errorEl = container.querySelector('[role="alert"]')!;
      expect(errorEl).not.toBeNull();
      expect(errorEl.textContent).toContain('Select an agent before submitting');
    } finally {
      unmount();
    }
  });
});

describe('CommandInput duplicate submission prevention', () => {
  it('prevents duplicate submissions while a request is pending', () => {
    const agents: Agent[] = [createAgent({ name: 'Research Agent' })];

    const { container, unmount } = renderInput({ businessId: 'default', actorId: 'nx:human:bootstrap', agents });
    try {
      const textarea = container.querySelector('textarea')!;
      fireEvent.change(textarea, {
        target: { value: 'test objective' },
      });
      const select = container.querySelector('select')!;
      fireEvent.change(select, {
        target: { value: 'agent-test-1' },
      });

      // First click - mutate is called
      const submitBtn = container.querySelector('button[type="submit"]')!;
      fireEvent.click(submitBtn);
      expect(mockMutate).toHaveBeenCalledTimes(1);

      // Second click - should be blocked by submitCount guard
      fireEvent.click(submitBtn);
      expect(mockMutate).toHaveBeenCalledTimes(1);
    } finally {
      unmount();
    }
  });
});
