import React from 'react';

export interface HeaderStepIndicatorProps {
  icon: React.ReactNode;
  label: string;
  active?: boolean;
  completed?: boolean;
}

export const HeaderStepIndicator: React.FC<HeaderStepIndicatorProps> = ({
  icon,
  label,
  active = false,
  completed = false,
}) => {
  const containerClassName = completed ? 'opacity-100' : active ? 'opacity-100' : 'opacity-40';
  const iconClassName = active
    ? 'size-10 rounded-full bg-gradient-to-tr from-blue-600 to-indigo-600 text-white flex items-center justify-center mb-1 shadow-lg shadow-blue-500/30 scale-110'
    : completed
      ? 'size-8 rounded-full bg-blue-100 text-blue-600 flex items-center justify-center mb-1 shadow-sm'
      : 'size-8 rounded-full bg-white border border-slate-200 text-slate-400 flex items-center justify-center mb-1';
  const labelClassName = active
    ? 'text-[10px] font-bold text-indigo-600 uppercase'
    : completed
      ? 'text-[10px] font-bold text-blue-600 uppercase'
      : 'text-[10px] font-bold text-slate-400 uppercase';

  return (
    <div className={`flex flex-col items-center ${containerClassName}`.trim()}>
      <div className={iconClassName}>{icon}</div>
      <span className={labelClassName}>{label}</span>
    </div>
  );
};
