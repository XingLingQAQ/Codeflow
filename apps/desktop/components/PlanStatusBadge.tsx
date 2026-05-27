import React from 'react';
import { HeaderStatusBadge } from './HeaderStatusBadge';

export interface PlanStatusBadgeProps {
  status: string;
}

const toneClassMap: Record<string, string> = {
  active: 'bg-green-50 text-green-700 border-green-200',
  completed: 'bg-blue-50 text-blue-700 border-blue-200',
};

export const PlanStatusBadge: React.FC<PlanStatusBadgeProps> = ({ status }) => (
  <HeaderStatusBadge
    label={(
      <>
        {status === 'active' && <span className="size-2 bg-green-500 rounded-full animate-pulse"></span>}
        {status}
      </>
    )}
    className={`flex items-center gap-2 text-xs px-3 py-1 rounded-full ${toneClassMap[status] ?? 'bg-slate-50 text-slate-600 border-slate-200'}`}
  />
);
