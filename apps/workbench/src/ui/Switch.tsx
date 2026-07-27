import React from 'react';
import * as SwitchPrimitive from '@radix-ui/react-switch';
import { cn } from '../lib/cn';

export const Switch = React.forwardRef<
  HTMLButtonElement,
  React.ComponentPropsWithoutRef<typeof SwitchPrimitive.Root>
>(({ className, ...props }, ref) => (
  <SwitchPrimitive.Root
    ref={ref}
    className={cn(
      'peer inline-flex h-5 w-9 shrink-0 items-center rounded-full border border-line transition-colors',
      'data-[state=unchecked]:bg-tint-hover',
      'data-[state=checked]:border-transparent data-[state=checked]:[background:var(--btn-primary-bg)]',
      'disabled:opacity-50',
      className,
    )}
    {...props}
  >
    <SwitchPrimitive.Thumb className="pointer-events-none block size-3.5 translate-x-0.5 rounded-full bg-white shadow transition-transform data-[state=checked]:translate-x-[18px] data-[state=checked]:[background:var(--btn-primary-fg)]" />
  </SwitchPrimitive.Root>
));
Switch.displayName = 'Switch';
