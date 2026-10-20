/**
 * Textarea primitive component
 * For multi-line command input
 */

import React, { forwardRef } from 'react';
import styles from './Textarea.module.css';

export interface TextareaProps extends React.TextareaHTMLAttributes<HTMLTextAreaElement> {
  label?: string;
  error?: string;
  hint?: string;
  fullWidth?: boolean;
  autoResize?: boolean;
  minRows?: number;
  maxRows?: number;
}

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(
  (
    {
      label,
      error,
      hint,
      fullWidth = false,
      autoResize = true,
      minRows = 3,
      maxRows = 10,
      className = '',
      id,
      'aria-describedby': ariaDescribedBy,
      ...props
    },
    ref
  ) => {
    const textareaId = id || React.useId();
    const errorId = error ? `${textareaId}-error` : undefined;
    const hintId = hint ? `${textareaId}-hint` : undefined;
    const describedBy = [errorId, hintId, ariaDescribedBy].filter(Boolean).join(' ') || undefined;

    const wrapperClassNames = [
      styles.wrapper,
      fullWidth ? styles.fullWidth : '',
      className,
    ].filter(Boolean).join(' ');

    const textareaClassNames = [
      styles.textarea,
      error ? styles.error : '',
    ].filter(Boolean).join(' ');

    const textareaRef = React.useRef<HTMLTextAreaElement>(null);
    const combinedRef = React.useCallback(
      (node: HTMLTextAreaElement | null) => {
        textareaRef.current = node;
        if (ref) {
          if (typeof ref === 'function') ref(node);
          else ref.current = node;
        }
      },
      [ref]
    );

    // Auto-resize effect
    React.useEffect(() => {
      if (!autoResize || !textareaRef.current) return;

      const textarea = textareaRef.current;
      textarea.style.height = 'auto';
      const newHeight = Math.min(
        Math.max(textarea.scrollHeight, minRows * 24),
        maxRows * 24
      );
      textarea.style.height = `${newHeight}px`;
    }, [props.value, autoResize, minRows, maxRows]);

    return (
      <div className={wrapperClassNames}>
        {label && (
          <label htmlFor={textareaId} className={styles.label}>
            {label}
          </label>
        )}
        <textarea
          ref={textareaRef}
          id={textareaId}
          className={textareaClassNames}
          aria-invalid={error ? 'true' : 'false'}
          aria-describedby={describedBy}
          aria-errormessage={errorId}
          rows={minRows}
          {...props}
        />
        {error && (
          <p id={errorId} className={styles.errorMessage} role="alert">
            {error}
          </p>
        )}
        {hint && !error && (
          <p id={hintId} className={styles.hintMessage}>
            {hint}
          </p>
        )}
      </div>
    );
  }
);

Textarea.displayName = 'Textarea';