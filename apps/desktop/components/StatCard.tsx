import React from 'react';

export interface StatCardProps {
  label: string;
  value: React.ReactNode;
  tone?: 'slate' | 'emerald' | 'amber' | 'blue';
  size?: 'sm' | 'lg';
  valueClassName?: string;
}

const toneClassMap: Record<NonNullable<StatCardProps['tone']>, string> = {
  slate: 'border-slate-200 bg-slate-50',
  emerald: 'border-emerald-200 bg-emerald-50',
  amber: 'border-amber-200 bg-amber-50',
  blue: 'border-blue-200 bg-blue-50',
};

const labelToneClassMap: Record<NonNullable<StatCardProps['tone']>, string> = {
  slate: 'text-slate-400',
  emerald: 'text-emerald-600',
  amber: 'text-amber-600',
  blue: 'text-blue-600',
};

const valueToneClassMap: Record<NonNullable<StatCardProps['tone']>, string> = {
  slate: 'text-slate-900',
  emerald: 'text-emerald-900',
  amber: 'text-amber-900',
  blue: 'text-blue-900',
};

export const StatCard: React.FC<StatCardProps> = ({
  label,
  value,
  tone = 'slate',
  size = 'sm',
  valueClassName,
}) => {
  const defaultValueClassName = size === 'lg' ? 'text-2xl font-bold' : 'text-lg font-bold';

  return (
    <div className={`rounded-xl border p-3 ${toneClassMap[tone]}`}>
      <div className={`text-[10px] uppercase tracking-widest mb-1 ${labelToneClassMap[tone]}`}>
        {label}
      </div>
      <div className={valueClassName ?? `${defaultValueClassName} ${valueToneClassMap[tone]}`}>
        {value}
      </div>
    </div>
  );
};
