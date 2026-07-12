import React from 'react';

export interface InlineAlertProps {
  message: React.ReactNode;
  tone?: 'error' | 'muted';
}

const toneClassMap: Record<NonNullable<InlineAlertProps['tone']>, string> = {
  error: 'text-red-600 border-red-200 bg-red-50',
  muted: 'text-slate-400 border-slate-200 bg-slate-50',
};

export const InlineAlert: React.FC<InlineAlertProps> = ({ message, tone = 'error' }) => (
  <div className={`text-xs rounded-xl border p-3 ${toneClassMap[tone]}`}>
    {message}
  </div>
);
