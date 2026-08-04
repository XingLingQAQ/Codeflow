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
        // --cf-radius-card (16px light geometry) + crisp hairline + rest shadow:
        // pure-white cards must hold their own against the grey canvas.
        'card rounded-[var(--cf-radius-card)] border border-[var(--elev-rest-border)] shadow-cf-rest',
        raised ? 'bg-raised' : 'bg-surface',
        interactive &&
          // card-interactive carries hover:translateY(-1px)+shadow via plain
          // CSS (see theme.css) — Tailwind's hover:/translate variant escapes
          // don't survive the 4.3.2 build on compound classes.
          'card-interactive cursor-pointer transition duration-150 ease-[var(--ease-flow)] ' +
            'hover:border-[var(--elev-lift-border)] hover:bg-hover',
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
