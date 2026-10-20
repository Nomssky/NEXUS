/**
 * CommandInput - The primary command interface
 * "Ask NEXUS anything..."
 */

import React, { useState, useCallback, useRef, useEffect } from 'react';
import { Textarea, Button, Badge } from '../primitives';
import { useSubmitIntelligence } from '../../api/hooks';
import type { SubmitIntelligenceResponse } from '../../types/api';
import styles from './CommandInput.module.css';

interface CommandInputProps {
  businessId: string;
  actorId: string;
  onSubmit?: (executionId: string) => void;
  disabled?: boolean;
}

export const CommandInput: React.FC<CommandInputProps> = ({
  businessId,
  actorId,
  onSubmit,
  disabled = false,
}) => {
  const [value, setValue] = useState('');
  const [error, setError] = useState<string | undefined>(undefined);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const submitCount = useRef(0);

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

      submitCount.current += 1;
      setError(undefined);

      submitMutation.mutate({
        business_id: businessId,
        actor_id: actorId,
        objective: { description: trimmed },
      });
    },
    [value, businessId, actorId, submitMutation]
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
          placeholder="Ask NEXUS anything…"
          disabled={submitMutation.isPending || disabled}
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
        <div className={styles.actions}>
          <Button
            type="submit"
            variant="primary"
            size="md"
            isLoading={submitMutation.isPending}
            disabled={!value.trim() || submitMutation.isPending || disabled}
            aria-busy={submitMutation.isPending}
          >
            Submit
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
            Submitting objective…
          </Badge>
        </div>
      )}
    </form>
  );
};