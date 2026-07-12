import React from 'react';

export interface SimpleInfoPanelProps {
  title: string;
  description?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
  contentClassName?: string;
  padding?: 'md' | 'lg';
}

const paddingClassMap: Record<NonNullable<SimpleInfoPanelProps['padding']>, string> = {
  md: 'p-3',
  lg: 'p-4',
};

export const SimpleInfoPanel: React.FC<SimpleInfoPanelProps> = ({
  title,
  description,
  children,
  className,
  contentClassName,
  padding = 'md',
}) => (
  <div className={`rounded-xl border border-slate-200 bg-slate-50 ${paddingClassMap[padding]} ${className ?? ''}`.trim()}>
    <div className="text-[10px] uppercase tracking-widest text-slate-400">{title}</div>
    {description ? <div className="mt-1 text-[10px] text-slate-400">{description}</div> : null}
    <div className={contentClassName ?? 'mt-2'}>{children}</div>
  </div>
);
