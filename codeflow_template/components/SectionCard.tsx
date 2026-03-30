import React from 'react';

export interface SectionCardProps {
  title: string;
  subtitle?: string;
  action?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
  contentClassName?: string;
  headerClassName?: string;
}

export const SectionCard: React.FC<SectionCardProps> = ({
  title,
  subtitle,
  action,
  children,
  className,
  contentClassName,
  headerClassName,
}) => (
  <section className={`rounded-[28px] border border-slate-200 bg-white shadow-sm ${className ?? ''}`.trim()}>
    <div className={`flex items-center justify-between gap-4 px-5 py-4 ${subtitle || action ? 'border-b border-slate-100' : ''} ${headerClassName ?? ''}`.trim()}>
      <div>
        <h2 className="text-base font-semibold text-slate-900">{title}</h2>
        {subtitle ? <p className="mt-1 text-xs text-slate-400">{subtitle}</p> : null}
      </div>
      {action}
    </div>
    <div className={contentClassName ?? 'p-5'}>{children}</div>
  </section>
);
