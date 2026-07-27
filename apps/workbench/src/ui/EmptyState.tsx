import React from 'react';
import { cn } from '../lib/cn';

export interface EmptyStateProps {
  icon?: React.ReactNode;
  title: string;
  description?: string;
  action?: React.ReactNode;
  className?: string;
}

/** Illustrated empty state: soft gradient blob + single primary action (frontend-experience §3.2). */
export function EmptyState({ icon, title, description, action, className }: EmptyStateProps) {
  return (
    <div className={cn('flex flex-col items-center justify-center px-6 py-12 text-center', className)}>
      <div className="relative mb-5 grid size-20 place-items-center">
        <svg viewBox="0 0 120 120" className="absolute inset-0 size-full opacity-70" aria-hidden>
          <path
            fill="currentColor"
            className="text-ink"
            opacity="0.06"
            d="M61 12c14 0 27 6 35 18s11 27 5 40-19 22-34 26-31 3-42-7S9 88 12 72s10-30 23-41c8-7 17-19 26-19Z"
          />
        </svg>
        <div className="relative text-ink-mute">{icon}</div>
      </div>
      <h3 className="text-[15px] font-semibold text-ink">{title}</h3>
      {description && <p className="mt-1.5 max-w-xs text-[13px] leading-relaxed text-ink-dim">{description}</p>}
      {action && <div className="mt-5">{action}</div>}
    </div>
  );
}
