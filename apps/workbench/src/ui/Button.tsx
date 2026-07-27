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
  'ease-[cubic-bezier(0.32,0.72,0,1)] active:scale-[0.97] select-none ' +
  'disabled:opacity-45 disabled:pointer-events-none';

const variants: Record<ButtonVariant, string> = {
  primary:
    'border border-transparent hover:brightness-[0.92] [background:var(--btn-primary-bg)] [color:var(--btn-primary-fg)]',
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
