import React from 'react';
import { cn } from '../lib/cn';

export function Kbd({ className, children }: { className?: string; children: React.ReactNode }) {
  return (
    <kbd
      className={cn(
        'inline-flex h-5 min-w-5 items-center justify-center rounded-md border border-line bg-tint',
        'px-1.5 font-mono text-[11px] text-ink-dim',
        className,
      )}
    >
      {children}
    </kbd>
  );
}
