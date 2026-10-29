/**
 * DevelopmentInput - Focused engineering task input
 * Accepts a development objective and submits to the executions API
 */

import React, { useState, useCallback, useRef, useEffect } from 'react';
import { Textarea, Button, Badge } from '../primitives';
import { useSubmitExecution, useCancelExecution } from '../../api/hooks';
import type { Agent } from '../../types/api';
import styles from './DevelopmentInput.module.css';

interface DevelopmentInputProps {
  businessId: string;
  actorId: string;
  onSubmit?: (executionId: string) => void;
  onCancel?: (() => Promise<void>) | undefined;
  canCancel?: boolean;
  disabled?: boolean;
  agents?: Agent[];
}

export const DevelopmentInput: React.FC<DevelopmentInputProps> = ({
  businessId,
  actorId,
  onSubmit,
  onCancel,
  canCancel = false,
  disabled = false,
  agents,
}) => {
  const [value, setValue] = useState('');
  const [error, setError] = useState<string | undefined>(undefined);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const submitCount = useRef(0);

  const submitMutation = useSubmitExecution({
    onSuccess: (response) => {
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

  const cancelMutation = useCancelExecution();

  const handleCancel = useCallback(async () => {
    if (!onCancel) return;
    try {
      await onCancel();
    } catch {
      // Error already surfaced by the caller
    }
  }, [onCancel]);

  const handleSubmit = useCallback(
    (e: React.FormEvent) => {
      e.preventDefault();

      const trimmed = value.trim();
      if (!trimmed) return;
      if (submitMutation.isPending) return;
      if (submitCount.current > 0) return; // Prevent duplicate submissions

      // Check for minimum meaningful input
      if (trimmed.length < 3) {
        setError('Development task must be at least 3 characters');
        return;
      }

      submitCount.current += 1;
      setError(undefined);

      submitMutation.mutate({
        business_id: businessId,
        actor_id: actorId,
        objective: { description: trimmed },
      });
    },
    [value, businessId, submitMutation]
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
          placeholder="Describe a development task (e.g., 'Show git status for the workspace', 'Show diff for modified files', 'Show recent commit history')"
          disabled={submitMutation.isPending || disabled}
          aria-label="Development task"
          aria-busy={submitMutation.isPending}
          fullWidth
          minRows={3}
          maxRows={8}
          error={error}
          hint={
            !submitMutation.isPending
              ? 'Press Enter to submit, Shift+Enter for new line. Use git, filesystem, or web search tools.'
              : 'Submitting development task…'
          }
        />
        <div className={styles.actions}>
          {canCancel && onCancel && (
            <Button
              variant="ghost"
              size="md"
              onClick={handleCancel}
              disabled={cancelMutation.isPending}
              aria-busy={cancelMutation.isPending}
            >
              Cancel Execution
            </Button>
          )}
          <Button
            type="submit"
            variant="primary"
            size="md"
            isLoading={submitMutation.isPending}
            disabled={!value.trim() || submitMutation.isPending || disabled}
            aria-busy={submitMutation.isPending}
          >
            Run Task
          </Button>
        </div>
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
            Submitting development task…
          </Badge>
        </div>
      )}
    </form>
  );
};