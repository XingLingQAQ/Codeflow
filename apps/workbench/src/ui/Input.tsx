import React from 'react';
import { cn } from '../lib/cn';

const field =
  'w-full rounded-lg border border-line bg-raised text-ink placeholder:text-ink-mute ' +
  'transition-[border-color,background] duration-150 hover:border-line-strong ' +
  'focus:border-ink/50 disabled:opacity-50';

export const Input = React.forwardRef<HTMLInputElement, React.InputHTMLAttributes<HTMLInputElement>>(
  ({ className, ...rest }, ref) => (
    <input ref={ref} className={cn(field, 'h-9 px-3 text-sm', className)} {...rest} />
  ),
);
Input.displayName = 'Input';

export const Textarea = React.forwardRef<
  HTMLTextAreaElement,
  React.TextareaHTMLAttributes<HTMLTextAreaElement>
>(({ className, ...rest }, ref) => (
  <textarea ref={ref} className={cn(field, 'px-3 py-2.5 text-sm leading-relaxed resize-none', className)} {...rest} />
));
Textarea.displayName = 'Textarea';

export function Label({ className, ...rest }: React.LabelHTMLAttributes<HTMLLabelElement>) {
  return <label className={cn('block text-xs font-medium text-ink-dim', className)} {...rest} />;
}
