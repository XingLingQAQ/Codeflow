import React from 'react';
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { X } from 'lucide-react';
import { cn } from '../lib/cn';

export const Dialog = DialogPrimitive.Root;
export const DialogTrigger = DialogPrimitive.Trigger;
export const DialogClose = DialogPrimitive.Close;

export function DialogContent({
  className,
  children,
  showClose = true,
  ...props
}: React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content> & { showClose?: boolean }) {
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay
        className={cn(
          'fixed inset-0 z-50 bg-[var(--dialog-overlay)] backdrop-blur-[2px]',
          'data-[state=open]:animate-[cf-fade_250ms_var(--ease-flow)]',
          'data-[state=closed]:animate-[cf-fade-out_150ms_var(--ease-flow)_forwards]',
        )}
      />
      <DialogPrimitive.Content
        className={cn(
          'glass fixed left-1/2 top-1/2 z-50 w-[min(92vw,520px)] -translate-x-1/2 -translate-y-1/2 rounded-2xl p-5 shadow-2xl',
          'data-[state=open]:animate-[cf-pop_250ms_var(--ease-flow)]',
          'data-[state=closed]:animate-[cf-pop-out_150ms_var(--ease-flow)_forwards]',
          'focus:outline-none',
          className,
        )}
        {...props}
      >
        {children}
        {showClose && (
          <DialogPrimitive.Close
            aria-label="关闭"
            className="absolute right-3.5 top-3.5 grid size-7 place-items-center rounded-lg text-ink-mute transition hover:bg-tint-active hover:text-ink"
          >
            <X size={16} />
          </DialogPrimitive.Close>
        )}
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  );
}

export function DialogTitle({ className, ...props }: React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>) {
  return <DialogPrimitive.Title className={cn('font-display text-lg font-semibold text-ink', className)} {...props} />;
}

export function DialogDescription({
  className,
  ...props
}: React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description>) {
  return <DialogPrimitive.Description className={cn('mt-1 text-[13px] leading-relaxed text-ink-dim', className)} {...props} />;
}
