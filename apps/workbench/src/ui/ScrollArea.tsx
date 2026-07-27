import React from 'react';
import * as ScrollAreaPrimitive from '@radix-ui/react-scroll-area';
import { cn } from '../lib/cn';

export function ScrollArea({
  className,
  children,
  viewportClassName,
  ...props
}: React.ComponentPropsWithoutRef<typeof ScrollAreaPrimitive.Root> & { viewportClassName?: string }) {
  return (
    <ScrollAreaPrimitive.Root className={cn('overflow-hidden', className)} {...props}>
      <ScrollAreaPrimitive.Viewport className={cn('size-full', viewportClassName)}>
        {children}
      </ScrollAreaPrimitive.Viewport>
      <ScrollAreaPrimitive.Scrollbar
        orientation="vertical"
        className="flex w-2 touch-none select-none p-0.5"
      >
        <ScrollAreaPrimitive.Thumb className="relative flex-1 rounded-full bg-white/15 transition-colors hover:bg-white/25" />
      </ScrollAreaPrimitive.Scrollbar>
      <ScrollAreaPrimitive.Corner />
    </ScrollAreaPrimitive.Root>
  );
}
