import React from 'react';

export interface ModelPillButtonProps {
  label: string;
  active?: boolean;
  onClick?: () => void;
}

const activeClassName = 'bg-white border-indigo-100 text-indigo-600 shadow-[0_0_20px_rgba(99,102,241,0.15)] ring-1 ring-indigo-500/20';
const idleClassName = 'bg-white/60 border-slate-200/60 text-slate-500 hover:bg-white hover:border-indigo-100 hover:text-indigo-600 hover:shadow-lg hover:shadow-indigo-500/5';

export const ModelPillButton: React.FC<ModelPillButtonProps> = ({
  label,
  active = false,
  onClick,
}) => (
  <button
    type="button"
    onClick={onClick}
    className={`group flex items-center gap-2.5 px-5 py-2.5 md:px-6 md:py-3 rounded-full border transition-all duration-300 transform hover:-translate-y-0.5 ${active ? activeClassName : idleClassName}`.trim()}
  >
    {active ? (
      <span className="relative flex h-2 w-2">
        <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-indigo-400 opacity-75"></span>
        <span className="relative inline-flex rounded-full h-2 w-2 bg-indigo-500"></span>
      </span>
    ) : null}
    <span className="text-sm font-bold tracking-wide">{label}</span>
  </button>
);
