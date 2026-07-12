import React from 'react';

export interface ProgressSummaryCardProps {
  completedCount: number;
  totalCount: number;
}

export const ProgressSummaryCard: React.FC<ProgressSummaryCardProps> = ({
  completedCount,
  totalCount,
}) => {
  const percent = totalCount > 0 ? (completedCount / totalCount) * 100 : 0;

  return (
    <div className="space-y-2">
      <div className="h-2 bg-slate-100 rounded-full overflow-hidden">
        <div
          className="h-full bg-gradient-to-r from-blue-500 to-indigo-500 rounded-full transition-all duration-500"
          style={{ width: `${percent}%` }}
        />
      </div>
      <p className="text-xs text-slate-400">{completedCount} of {totalCount} tasks completed</p>
    </div>
  );
};
