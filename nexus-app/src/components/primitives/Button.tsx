/**
 * Button primitive component
 * Restrained, accessible, with consistent interaction states
 */

import React, { forwardRef } from 'react';
import styles from './Button.module.css';

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger' | 'success';
export type ButtonSize = 'sm' | 'md' | 'lg';

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  isLoading?: boolean;
  leftIcon?: React.ReactNode;
  rightIcon?: React.ReactNode;
  fullWidth?: boolean;
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  (
    {
      children,
      variant = 'primary',
      size = 'md',
      isLoading = false,
      leftIcon,
      rightIcon,
      fullWidth = false,
      className = '',
      disabled,
      'aria-busy': ariaBusy,
      type = 'button',
      onClick,
      ...props
    },
    ref
  ) => {
    const classNames = [
      styles.button,
      styles[variant],
      styles[size],
      fullWidth ? styles.fullWidth : '',
      isLoading ? styles.loading : '',
      className,
    ]
      .filter(Boolean)
      .join(' ');

    const isDisabled = disabled || isLoading;

    return (
      <button
        ref={ref}
        type={type}
        className={classNames}
        disabled={isDisabled}
        aria-busy={isLoading ? 'true' : ariaBusy}
        aria-disabled={isDisabled}
        onClick={onClick}
        {...props}
      >
        {isLoading && (
          <span className={styles.spinner} aria-hidden="true">
            <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
              <circle
                cx="12"
                cy="12"
                r="10"
                stroke="currentColor"
                strokeWidth="3"
                strokeLinecap="round"
                strokeDasharray="31.4 31.4"
                className={styles.spinnerCircle}
              />
            </svg>
          </span>
        )}
        {!isLoading && leftIcon && <span className={styles.iconLeft} aria-hidden="true">{leftIcon}</span>}
        <span className={isLoading ? styles.contentHidden : styles.content}>{children}</span>
        {!isLoading && rightIcon && <span className={styles.iconRight} aria-hidden="true">{rightIcon}</span>}
      </button>
    );
  }
);

Button.displayName = 'Button';