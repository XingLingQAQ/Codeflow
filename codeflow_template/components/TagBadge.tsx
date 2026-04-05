import React from 'react';

export interface TagBadgeProps {
  label: React.ReactNode;
  tone?: 'slate' | 'amber';
  className?: string;
}

const toneClassMap: Record<NonNullable<TagBadgeProps['tone']>, string> = {
  slate: 'bg-slate-100 text-slate-500',
  amber: 'bg-amber-50 text-amber-600 border border-amber-200',
};

export const TagBadge: React.FC<TagBadgeProps> = ({
  label,
  tone = 'slate',
  className,
}) => (
  <span className={`px-2 py-1 rounded-md text-[10px] font-bold ${toneClassMap[tone]} ${className ?? ''}`.trim()}>
    {label}
  </span>
);
