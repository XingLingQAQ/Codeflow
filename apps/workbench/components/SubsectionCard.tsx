import React from 'react';

export interface SubsectionCardProps {
  title: string;
  children: React.ReactNode;
  className?: string;
  titleClassName?: string;
  contentClassName?: string;
}

export const SubsectionCard: React.FC<SubsectionCardProps> = ({
  title,
  children,
  className,
  titleClassName,
  contentClassName,
}) => (
  <div className={`rounded-xl bg-slate-50 border border-slate-100 p-3 ${className ?? ''}`.trim()}>
    <div className={`text-[10px] uppercase tracking-widest text-slate-400 mb-2 ${titleClassName ?? ''}`.trim()}>{title}</div>
    <div className={contentClassName ?? ''}>{children}</div>
  </div>
);
