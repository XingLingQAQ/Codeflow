import React from 'react';
import type { WorkflowReplayItem } from '../types';

export interface WorkflowTimelineItemProps {
  item: WorkflowReplayItem;
}

export const WorkflowTimelineItem: React.FC<WorkflowTimelineItemProps> = ({ item }) => (
  <div className="flex gap-3 text-sm">
    <div className="w-20 shrink-0 text-[10px] uppercase tracking-widest text-slate-400">{item.lane}</div>
    <div className="flex-1 rounded-xl border border-slate-100 bg-slate-50 px-3 py-2">
      <div className="flex items-center justify-between gap-2">
        <span className="font-semibold text-slate-700">{item.title}</span>
        {item.status ? <span className="text-[10px] text-slate-400 uppercase">{item.status}</span> : null}
      </div>
      <p className="text-xs text-slate-500 mt-1">{item.message}</p>
    </div>
  </div>
);
