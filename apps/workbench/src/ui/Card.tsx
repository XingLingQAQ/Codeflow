import React from 'react';
import { cn } from '../lib/cn';

export interface CardProps extends React.HTMLAttributes<HTMLDivElement> {
  raised?: boolean;
  interactive?: boolean;
}

export const Card = React.forwardRef<HTMLDivElement, CardProps>(
  ({ raised, interactive, className, ...rest }, ref) => (
    <div
      ref={ref}
      className={cn(
        'rounded-xl border border-line',
        raised ? 'bg-raised' : 'bg-panel',
        interactive &&
          'transition-[background,border-color,transform] duration-250 ease-[cubic-bezier(0.32,0.72,0,1)] ' +
            'hover:border-line-strong hover:bg-hover cursor-pointer',
        className,
      )}
      {...rest}
    />
  ),
);
Card.displayName = 'Card';

export function CardHeader({ className, ...rest }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('flex items-center justify-between gap-3 px-4 pt-4', className)} {...rest} />;
}

export function CardTitle({ className, ...rest }: React.HTMLAttributes<HTMLHeadingElement>) {
  return <h3 className={cn('text-sm font-semibold text-ink', className)} {...rest} />;
}

export function CardBody({ className, ...rest }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('p-4', className)} {...rest} />;
}
