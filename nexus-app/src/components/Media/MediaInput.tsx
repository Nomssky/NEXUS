/**
 * MediaInput - media task composer.
 *
 * This workspace uses the existing intelligence execution API (the same
 * governed path as the command center). It does NOT submit standalone media
 * generation jobs: the backend currently provides no image/video/audio
 * generation capability. The supported workflow is creative preparation —
 * briefs, reference research, and plan synthesis — through the real
 * execution and research path.
 */

import React, { useState, useCallback, useRef, useEffect } from 'react';
import { Textarea, Button, Badge } from '../primitives';
import { useSubmitIntelligence } from '../../api/hooks';
import styles from './MediaInput.module.css';

interface MediaInputProps {
  businessId: string;
  actorId: string;
  onSubmit?: (executionId: string) => void;
  disabled?: boolean;
}

export const MediaInput: React.FC<MediaInputProps> = ({
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
      if (submitCount.current > 0) return;
      if (trimmed.length < 3) {
        setError('A creative objective must be at least 3 characters');
        return;
      }
      submitCount.current += 1;
      setError(undefined);

      // The objective text carries the media intent for the model; nothing
      // about this request is privileged beyond the governed execution path.
      const description = trimmed.startsWith('search the web')
        ? trimmed
        : `Prepare a creative brief: ${trimmed}`;

      submitMutation.mutate({
        business_id: businessId,
        actor_id: actorId,
        objective: { description },
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
          placeholder="Describe a media objective (e.g., 'Search the web for reference typical of a minimalist poster', or 'Brief for a short product video')"
          disabled={submitMutation.isPending || disabled}
          aria-label="Media objective"
          aria-busy={submitMutation.isPending}
          fullWidth
          minRows={3}
          maxRows={8}
          error={error}
          hint={
            !submitMutation.isPending
              ? 'Press Enter to submit, Shift+Enter for new line. Supported: creative preparation and web research. Generation is not available.'
              : 'Submitting media objective…'
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
            Run
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
            Submitting media objective…
          </Badge>
        </div>
      )}
    </form>
  );
};