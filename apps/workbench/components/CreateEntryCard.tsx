import React from 'react';
import { ArrowRight } from 'lucide-react';

export interface CreateEntryCardProps {
  title: string;
  description: string;
  icon: React.ReactNode;
  onClick?: () => void;
}

export const CreateEntryCard: React.FC<CreateEntryCardProps> = ({
  title,
  description,
  icon,
  onClick,
}) => (
  <button
    type="button"
    onClick={onClick}
    className="border-2 border-dashed border-slate-200 rounded-2xl p-6 flex flex-col items-start justify-between text-left hover:border-blue-300 hover:bg-gradient-to-br hover:from-blue-50 hover:to-white transition-all group min-h-[260px]"
  >
    <div className="space-y-4">
      <div className="size-14 rounded-2xl bg-slate-50 border border-slate-200 flex items-center justify-center text-slate-400 group-hover:scale-110 group-hover:border-blue-200 group-hover:text-blue-500 transition-all">
        {icon}
      </div>
      <div className="space-y-2">
        <div className="inline-flex rounded-full border border-blue-100 bg-blue-50 px-2.5 py-1 text-[10px] font-bold uppercase tracking-[0.22em] text-blue-600">
          Quick start
        </div>
        <div>
          <h3 className="font-bold text-slate-700 group-hover:text-blue-600 transition-colors">{title}</h3>
          <p className="text-sm leading-6 text-slate-400 mt-1">{description}</p>
        </div>
      </div>
    </div>
    <div className="flex items-center gap-2 text-sm font-semibold text-blue-600 transition-transform group-hover:translate-x-1">
      <span>Create project</span>
      <ArrowRight size={16} />
    </div>
  </button>
);

