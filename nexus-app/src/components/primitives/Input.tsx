/**
 * Input primitive component
 * Accessible, with proper label association and error states
 */

import React, { forwardRef } from 'react';
import styles from './Input.module.css';

export interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  label?: string;
  error?: string;
  hint?: string;
  leftIcon?: React.ReactNode;
  rightIcon?: React.ReactNode;
  fullWidth?: boolean;
}

export const Input = forwardRef<HTMLInputElement, InputProps>(
  (
    {
      label,
      error,
      hint,
      leftIcon,
      rightIcon,
      fullWidth = false,
      className = '',
      id,
      'aria-describedby': ariaDescribedBy,
      ...props
    },
    ref
  ) => {
    const inputId = id || React.useId();
    const errorId = error ? `${inputId}-error` : undefined;
    const hintId = hint ? `${inputId}-hint` : undefined;
    const describedBy = [errorId, hintId, ariaDescribedBy].filter(Boolean).join(' ') || undefined;

    const wrapperClassNames = [
      styles.wrapper,
      fullWidth ? styles.fullWidth : '',
      className,
    ].filter(Boolean).join(' ');

    const inputClassNames = [
      styles.input,
      error ? styles.error : '',
      leftIcon ? styles.hasLeftIcon : '',
      rightIcon ? styles.hasRightIcon : '',
    ].filter(Boolean).join(' ');

    return (
      <div className={wrapperClassNames}>
        {label && (
          <label htmlFor={inputId} className={styles.label}>
            {label}
          </label>
        )}
        <div className={styles.inputWrapper}>
          {leftIcon && <span className={styles.iconLeft} aria-hidden="true">{leftIcon}</span>}
          <input
            ref={ref}
            id={inputId}
            className={inputClassNames}
            aria-invalid={error ? 'true' : 'false'}
            aria-describedby={describedBy}
            aria-errormessage={errorId}
            {...props}
          />
          {rightIcon && <span className={styles.iconRight} aria-hidden="true">{rightIcon}</span>}
        </div>
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

Input.displayName = 'Input';