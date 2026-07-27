import React from 'react';
import { cn } from '../lib/cn';

export type Tone = 'neutral' | 'accent' | 'success' | 'warn' | 'danger' | 'info';

const toneClasses: Record<Tone, string> = {
  neutral: 'text-ink-dim bg-tint border-line',
  accent: 'text-ink bg-tint-active border-line-strong',
  success: 'text-success bg-success/10 border-success/25',
  warn: 'text-warn bg-warn/10 border-warn/25',
  danger: 'text-danger bg-danger/10 border-danger/25',
  info: 'text-ink-dim bg-tint border-line',
};

const dotColor: Record<Tone, string> = {
  neutral: 'bg-ink-mute',
  accent: 'bg-ink',
  success: 'bg-success',
  warn: 'bg-warn',
  danger: 'bg-danger',
  info: 'bg-ink-mute',
};

export interface BadgeProps extends React.HTMLAttributes<HTMLSpanElement> {
  tone?: Tone;
}

export function Badge({ tone = 'neutral', className, ...rest }: BadgeProps) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[11px] font-medium',
        toneClasses[tone],
        className,
      )}
      {...rest}
    />
  );
}

export interface StatusPillProps extends BadgeProps {
  /** Pulse the dot to indicate a live / active state. */
  pulse?: boolean;
  label: React.ReactNode;
}

export function StatusPill({ tone = 'neutral', pulse, label, className, ...rest }: StatusPillProps) {
  return (
    <Badge tone={tone} className={className} {...rest}>
      <span className="relative flex size-1.5">
        <span className={cn('relative inline-flex size-1.5 rounded-full', dotColor[tone])} />
      </span>
      {label}
    </Badge>
  );
}
