import React from 'react';
import { cn } from '../lib/cn';

export interface IconButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  size?: 'sm' | 'md' | 'lg';
  active?: boolean;
}

const sizes = {
  sm: 'size-7',
  md: 'size-9',
  lg: 'size-10',
} as const;

export const IconButton = React.forwardRef<HTMLButtonElement, IconButtonProps>(
  ({ size = 'md', active, className, children, ...rest }, ref) => (
    <button
      ref={ref}
      className={cn(
        'inline-flex items-center justify-center rounded-lg text-ink-dim',
        'transition-[transform,background,color] duration-150 ease-[cubic-bezier(0.32,0.72,0,1)]',
        'hover:text-ink hover:bg-tint-hover active:scale-[0.94] disabled:opacity-40 disabled:pointer-events-none',
        active && 'text-ink bg-tint-active',
        sizes[size],
        className,
      )}
      {...rest}
    >
      {children}
    </button>
  ),
);
IconButton.displayName = 'IconButton';
