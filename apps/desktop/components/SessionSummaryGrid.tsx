import React from 'react';
import { SessionSummaryField } from './SessionSummaryField';

export interface SessionSummaryGridProps {
  sessionText?: string;
  statusText?: string;
  statusTone?: string;
  taskCountText?: string;
  lastActiveText?: string;
}

export const SessionSummaryGrid: React.FC<SessionSummaryGridProps> = ({
  sessionText,
  statusText,
  statusTone,
  taskCountText,
  lastActiveText,
}) => (
  <div className="rounded-2xl border border-slate-200 bg-white p-4 grid grid-cols-1 md:grid-cols-4 gap-3">
    <SessionSummaryField label="Session">
      <div className="text-sm font-semibold text-slate-700">{sessionText || 'Unknown session'}</div>
    </SessionSummaryField>
    <SessionSummaryField label="Status">
      <span className={`inline-flex px-2 py-1 rounded-full border text-xs font-medium ${statusTone ?? 'text-slate-500 bg-slate-50 border-slate-100'}`}>
        {statusText || 'Unknown'}
      </span>
    </SessionSummaryField>
    <SessionSummaryField label="Workload">
      <div className="text-sm font-semibold text-slate-700">{taskCountText || '0 tasks'}</div>
    </SessionSummaryField>
    <SessionSummaryField label="Last active">
      <div className="text-sm font-semibold text-slate-700">{lastActiveText || 'Unknown'}</div>
    </SessionSummaryField>
  </div>
);
