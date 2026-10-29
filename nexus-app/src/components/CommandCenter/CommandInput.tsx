/**
 * CommandInput - The primary command interface
 * "Ask NEXUS anything..."
 *
 * Requires an active agent to be selected before submitting an objective.
 * The selected agent_id is sent in `context.agent_id` so the runtime can
 * route the objective to a specific registered agent.
 */

import React, { useState, useCallback, useRef, useEffect } from 'react';
import { Textarea, Button, Badge } from '../primitives';
import { useSubmitIntelligence } from '../../api/hooks';
import type { SubmitIntelligenceResponse, Agent } from '../../types/api';
import styles from './CommandInput.module.css';

interface CommandInputProps {
  businessId: string;
  actorId: string;
  agents: Agent[];
  onSubmit?: (executionId: string) => void;
  disabled?: boolean;
}

export const CommandInput: React.FC<CommandInputProps> = ({
  businessId,
  actorId,
  agents,
  onSubmit,
  disabled = false,
}) => {
  const [value, setValue] = useState('');
  const [error, setError] = useState<string | undefined>(undefined);
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const submitCount = useRef(0);

  const activeAgents = agents.filter((a) => a.status === 'active');
  const agentKey = (a: Agent) => a.entity_id ?? a.id;
  const selectedAgent = activeAgents.find((a) => agentKey(a) === selectedAgentId);

  const submitMutation = useSubmitIntelligence({
    onSuccess: (response: SubmitIntelligenceResponse) => {
      setValue('');
      setError(undefined);
      submitCount.current = 0;
      onSubmit?.(response.execution_id);
    },
    onError: (err: Error) => {
      setError(err.message);
      submitCount.current = 0;
    },
  });

  // Clear agent selection when the available agents change (e.g. business/division switch)
  // so a stale selection is never submitted for a different scope.
  useEffect(() => {
    if (selectedAgentId && !activeAgents.some((a) => agentKey(a) === selectedAgentId)) {
      setSelectedAgentId(null);
    }
  }, [activeAgents, selectedAgentId]);

  const handleSelectChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      setSelectedAgentId(e.target.value || null);
      setError(undefined);
    },
    []
  );

  const handleSubmit = useCallback(
    (e: React.FormEvent) => {
      e.preventDefault();

      const trimmed = value.trim();
      if (!trimmed) return;
      if (submitMutation.isPending) return;
      if (submitCount.current > 0) return; // Prevent duplicate submissions

      // Check for minimum meaningful input
      if (trimmed.length < 3) {
        setError('Objective must be at least 3 characters');
        return;
      }

      // Require an agent when agents exist
      if (activeAgents.length === 0) {
        setError('No active agents available. Register an agent first.');
        return;
      }
      if (!selectedAgentId) {
        setError('Select an agent before submitting');
        return;
      }

      submitCount.current += 1;
      setError(undefined);

      submitMutation.mutate({
        business_id: businessId,
        actor_id: actorId,
        objective: { description: trimmed },
        context: { agent_id: selectedAgentId },
      });
    },
    [value, businessId, actorId, selectedAgentId, activeAgents, submitMutation]
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Enter' && !e.shiftKey && !e.ctrlKey && !e.metaKey) {
        e.preventDefault();
        handleSubmit(e);
      }
    },
    [handleSubmit]
  );

  // Auto-focus on mount
  useEffect(() => {
    textareaRef.current?.focus();
  }, []);

  const hasAgents = activeAgents.length > 0;
  const isDisabled = disabled || !hasAgents || submitMutation.isPending;

  return (
    <form onSubmit={handleSubmit} className={styles.form}>
      <div className={styles.inputWrapper}>
        <Textarea
          ref={textareaRef}
          value={value}
          onChange={(e) => {
            setValue(e.target.value);
            if (error) setError(undefined);
          }}
          onKeyDown={handleKeyDown}
          placeholder="Ask NEXUS anything…"
          disabled={isDisabled}
          aria-label="Command input"
          aria-busy={submitMutation.isPending}
          fullWidth
          minRows={2}
          maxRows={6}
          error={error}
          hint={
            !submitMutation.isPending
              ? 'Press Enter to submit, Shift+Enter for new line'
              : 'Submitting…'
          }
        />

        {hasAgents ? (
          <div className={styles.agentSelector}>
            <label htmlFor="agent-select" className={styles.agentLabel}>
              Agent
            </label>
            <select
              id="agent-select"
              value={selectedAgentId ?? ''}
              onChange={handleSelectChange}
              disabled={isDisabled || submitMutation.isPending}
              aria-label="Select agent"
            >
              <option value="">Select an agent…</option>
              {activeAgents.map((agent) => (
                <option key={agentKey(agent)} value={agentKey(agent)}>
                  {agent.name}
                </option>
              ))}
            </select>
          </div>
        ) : (
          <div className={styles.emptyState} role="alert">
            <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
              <circle cx="8" cy="8" r="7" stroke="currentColor" strokeWidth="2" />
              <path d="M8 4v6M8 12v.01" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
            </svg>
            <span>No active agents available. Register an agent first.</span>
          </div>
        )}
      </div>
      <div className={styles.actions}>
        <Button
          type="submit"
          variant="primary"
          size="md"
          isLoading={submitMutation.isPending}
          disabled={isDisabled || !value.trim() || submitMutation.isPending}
          aria-busy={submitMutation.isPending}
        >
          Submit
        </Button>
      </div>

      {error && (
        <div className={styles.errorBanner} role="alert">
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <circle cx="8" cy="8" r="7" stroke="currentColor" strokeWidth="2" />
            <path d="M8 4v6M8 12v.01" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
          </svg>
          <span>{error}</span>
        </div>
      )}

      {submitMutation.isPending && (
        <div className={styles.submittingBanner} aria-live="polite">
          <Badge variant="running" size="sm" dot>
            Submitting objective…
          </Badge>
        </div>
      )}
    </form>
  );
};
