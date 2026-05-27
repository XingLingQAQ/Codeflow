import React from 'react';

export interface ErrorStateProps {
  title?: string;
  message: string;
  onRetry?: () => void;
}

export const ErrorState: React.FC<ErrorStateProps> = ({ title = 'Something went wrong', message, onRetry }) => (
  <div className="flex flex-col items-center justify-center py-16 px-6 text-center">
    <div className="mb-4 size-12 rounded-full bg-red-50 flex items-center justify-center">
      <svg className="size-6 text-red-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
        <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.964-.833-2.732 0L4.082 16.5c-.77.833.192 2.5 1.732 2.5z" />
      </svg>
    </div>
    <h3 className="text-lg font-semibold text-slate-700 mb-1">{title}</h3>
    <p className="text-sm text-slate-400 max-w-sm mb-6">{message}</p>
    {onRetry && (
      <button
        onClick={onRetry}
        className="px-4 py-2 bg-slate-100 text-slate-600 text-sm font-medium rounded-lg hover:bg-slate-200 transition-colors"
      >
        Try again
      </button>
    )}
  </div>
);
