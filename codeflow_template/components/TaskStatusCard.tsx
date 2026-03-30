import React from 'react';

export interface TaskStatusCardProps {
  toneClassName: string;
  borderClassName: string;
  icon: React.ReactNode;
  title: string;
  badgeLabel: string;
  badgeClassName: string;
  description?: string;
  meta?: React.ReactNode;
  onClick: () => void;
}

export const TaskStatusCard: React.FC<TaskStatusCardProps> = ({
  toneClassName,
  borderClassName,
  icon,
  title,
  badgeLabel,
  badgeClassName,
  description,
  meta,
  onClick,
}) => (
  <div
    onClick={onClick}
    className={`p-4 rounded-xl border shadow-sm cursor-pointer hover:shadow-md transition-all ${toneClassName} ${borderClassName}`.trim()}
  >
    <div className="flex justify-between items-center mb-2">
      <div className="flex items-center gap-2">
        {icon}
        <span className="text-sm font-bold text-slate-800">{title}</span>
      </div>
      <span className={`text-[10px] font-bold px-2 py-0.5 rounded ${badgeClassName}`}>{badgeLabel}</span>
    </div>
    {description ? (
      <p className="text-xs text-slate-500 pl-6 border-l-2 border-slate-100 ml-1.5 line-clamp-2">{description}</p>
    ) : null}
    {meta ? <div className="flex flex-wrap gap-2 mt-2 pl-6 ml-1.5 text-[10px] text-slate-400">{meta}</div> : null}
  </div>
);
