import React from 'react';
import { cn } from '../lib/cn';

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger';
export type ButtonSize = 'sm' | 'md' | 'lg';

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  loading?: boolean;
}

const base =
  'relative inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-lg font-medium ' +
  'transition-[transform,background,box-shadow,border-color,color] duration-150 ' +
  'ease-[var(--ease-flow)] active:scale-[0.97] select-none ' +
  'disabled:opacity-45 disabled:pointer-events-none';

const variants: Record<ButtonVariant, string> = {
  primary:
    // Front the neutral primary ramp directly (the --btn-primary-* vars are
    // aliases of these tokens, kept for the FlowRail/Settings index numerals).
    // shadow-sm: the ink fill carries a touch of depth so the button reads as
    // the page's most sure control on the white canvas.
    'border shadow-sm transition-[filter] [background:var(--color-primary)] [color:var(--color-on-primary)] ' +
      '[border-color:var(--color-primary-active)] hover:brightness-110',
  secondary:
    'bg-raised text-ink border border-line hover:bg-hover hover:border-line-strong',
  ghost: 'text-ink-dim hover:text-ink hover:bg-tint-hover',
  danger: 'text-danger bg-danger/10 border border-danger/30 hover:bg-danger/20',
};

const sizes: Record<ButtonSize, string> = {
  sm: 'h-8 px-3 text-[13px]',
  md: 'h-9 px-4 text-sm',
  lg: 'h-11 px-5 text-[15px]',
};

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ variant = 'secondary', size = 'md', loading, className, children, disabled, ...rest }, ref) => (
    <button
      ref={ref}
      disabled={disabled || loading}
      className={cn(base, variants[variant], sizes[size], className)}
      {...rest}
    >
      {loading && (
        <span className="absolute inset-0 flex items-center justify-center">
          <span className="size-4 rounded-full border-2 border-current border-t-transparent animate-spin opacity-80" />
        </span>
      )}
      <span className={cn('inline-flex items-center gap-2', loading && 'opacity-0')}>{children}</span>
    </button>
  ),
);
Button.displayName = 'Button';
