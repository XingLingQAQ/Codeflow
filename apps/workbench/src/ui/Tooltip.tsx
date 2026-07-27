import React from 'react';
import * as TooltipPrimitive from '@radix-ui/react-tooltip';
import { cn } from '../lib/cn';

export function TooltipProvider({ children }: { children: React.ReactNode }) {
  return (
    <TooltipPrimitive.Provider delayDuration={220} skipDelayDuration={400}>
      {children}
    </TooltipPrimitive.Provider>
  );
}

export interface TooltipProps {
  content: React.ReactNode;
  children: React.ReactNode;
  side?: 'top' | 'right' | 'bottom' | 'left';
  align?: 'start' | 'center' | 'end';
  sideOffset?: number;
}

export function Tooltip({ content, children, side = 'right', align = 'center', sideOffset = 8 }: TooltipProps) {
  if (content == null || content === '') return <>{children}</>;
  return (
    <TooltipPrimitive.Root>
      <TooltipPrimitive.Trigger asChild>{children}</TooltipPrimitive.Trigger>
      <TooltipPrimitive.Portal>
        <TooltipPrimitive.Content
          side={side}
          align={align}
          sideOffset={sideOffset}
          className={cn(
            'glass z-[60] rounded-lg px-2.5 py-1.5 text-xs font-medium text-ink shadow-lg',
            'animate-[cf-pop_150ms_var(--ease-flow)]',
          )}
        >
          {content}
          <TooltipPrimitive.Arrow className="fill-white/[0.08]" />
        </TooltipPrimitive.Content>
      </TooltipPrimitive.Portal>
    </TooltipPrimitive.Root>
  );
}
