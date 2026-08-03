import React from 'react';
import * as Menu from '@radix-ui/react-dropdown-menu';
import { cn } from '../lib/cn';

export const DropdownMenu = Menu.Root;
export const DropdownMenuTrigger = Menu.Trigger;

export function DropdownMenuContent({
  className,
  sideOffset = 6,
  align = 'start',
  ...props
}: React.ComponentPropsWithoutRef<typeof Menu.Content>) {
  return (
    <Menu.Portal>
      <Menu.Content
        align={align}
        sideOffset={sideOffset}
        className={cn(
          'glass z-50 min-w-44 rounded-xl p-1.5 shadow-xl',
          'data-[state=open]:animate-[cf-pop_150ms_var(--ease-flow)]',
          'data-[state=closed]:animate-[cf-pop-out_150ms_var(--ease-flow)_forwards]',
          className,
        )}
        {...props}
      />
    </Menu.Portal>
  );
}

export function DropdownMenuItem({
  className,
  inset,
  ...props
}: React.ComponentPropsWithoutRef<typeof Menu.Item> & { inset?: boolean }) {
  return (
    <Menu.Item
      className={cn(
        'flex cursor-pointer items-center gap-2 rounded-lg px-2.5 py-1.5 text-[13px] text-ink-dim outline-none',
        'data-[highlighted]:bg-tint-active data-[highlighted]:text-ink',
        'data-[disabled]:pointer-events-none data-[disabled]:opacity-40',
        inset && 'pl-8',
        className,
      )}
      {...props}
    />
  );
}

export function DropdownMenuSeparator({ className, ...props }: React.ComponentPropsWithoutRef<typeof Menu.Separator>) {
  return <Menu.Separator className={cn('my-1 h-px bg-line', className)} {...props} />;
}

export function DropdownMenuLabel({ className, ...props }: React.ComponentPropsWithoutRef<typeof Menu.Label>) {
  return (
    <Menu.Label
      className={cn('px-2.5 py-1 text-[11px] font-medium uppercase tracking-wide text-ink-mute', className)}
      {...props}
    />
  );
}
