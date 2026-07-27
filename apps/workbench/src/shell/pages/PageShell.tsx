import React from 'react';
import { motion } from 'motion/react';
import { ScrollArea } from '../../ui/ScrollArea';
import { cn } from '../../lib/cn';
import { staggerContainer } from '../../lib/motion';

interface PageShellProps {
  title: React.ReactNode;
  subtitle?: React.ReactNode;
  actions?: React.ReactNode;
  children: React.ReactNode;
  maxWidth?: string;
}

export function PageShell({ title, subtitle, actions, children, maxWidth = 'max-w-6xl' }: PageShellProps) {
  return (
    <div className="flex h-full flex-col overflow-hidden">
      <header className="flex shrink-0 items-end justify-between gap-4 border-b border-line px-8 py-5">
        <div>
          <h1 className="font-display text-[26px] font-bold leading-tight text-ink">{title}</h1>
          {subtitle && <p className="mt-1 text-[13px] text-ink-dim">{subtitle}</p>}
        </div>
        {actions && <div className="flex items-center gap-2">{actions}</div>}
      </header>
      <ScrollArea className="flex-1" viewportClassName="px-8 py-7">
        <motion.div
          variants={staggerContainer}
          initial="initial"
          animate="animate"
          className={cn('mx-auto', maxWidth)}
        >
          {children}
        </motion.div>
      </ScrollArea>
    </div>
  );
}

export function SectionTitle({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <h2 className={cn('mb-3 text-[13px] font-semibold uppercase tracking-wide text-ink-mute', className)}>{children}</h2>
  );
}
