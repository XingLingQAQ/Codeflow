import React from 'react';

export interface SessionSummaryFieldProps {
  label: string;
  children: React.ReactNode;
}

export const SessionSummaryField: React.FC<SessionSummaryFieldProps> = ({ label, children }) => (
  <div>
    <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-1">{label}</div>
    {children}
  </div>
);
