/**
 * ResearchInput - Focused research command input
 * Accepts a research question/objective and submits to the intelligence API
 */

import React, { useState, useCallback, useRef, useEffect } from 'react';
import { Textarea, Button, Badge } from '../primitives';
import { useSubmitIntelligence } from '../../api/hooks';
import styles from './ResearchInput.module.css';

interface ResearchInputProps {
  businessId: string;
  actorId: string;
  onSubmit?: (executionId: string) => void;
  disabled?: boolean;
}

export const ResearchInput: React.FC<ResearchInputProps> = ({
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

  const handleSubmit = useCallback(
    (e: React.FormEvent) => {
      e.preventDefault();

      const trimmed = value.trim();
      if (!trimmed) return;
      if (submitMutation.isPending) return;
      if (submitCount.current > 0) return; // Prevent duplicate submissions

      // Check for minimum meaningful input
      if (trimmed.length < 3) {
        setError('Research question must be at least 3 characters');
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
          placeholder="What would you like to research? (e.g., 'Search the web for NEXUS runtime capabilities')"
          disabled={submitMutation.isPending || disabled}
          aria-label="Research query"
          aria-busy={submitMutation.isPending}
          fullWidth
          minRows={3}
          maxRows={8}
          error={error}
          hint={
            !submitMutation.isPending
              ? 'Press Enter to submit, Shift+Enter for new line'
              : 'Submitting research objective…'
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
            Research
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
            Submitting research objective…
          </Badge>
        </div>
      )}
    </form>
  );
};