import React from 'react';

export interface InfoChipProps {
  label: React.ReactNode;
  tone?: 'slate' | 'violet' | 'blue';
  className?: string;
}

const toneClassMap: Record<NonNullable<InfoChipProps['tone']>, string> = {
  slate: 'border-slate-200 bg-white text-slate-500',
  violet: 'border-violet-200 bg-violet-50 text-violet-600',
  blue: 'border-blue-200 bg-blue-50 text-blue-600',
};

export const InfoChip: React.FC<InfoChipProps> = ({ label, tone = 'slate', className }) => (
  <span className={`px-2 py-0.5 rounded-full border text-[10px] font-medium ${toneClassMap[tone]} ${className ?? ''}`.trim()}>{label}</span>
);
