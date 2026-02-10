import React, { ReactNode } from 'react';

export interface EmptyStateProps {
  icon?: ReactNode;
  title: string;
  description?: string;
  action?: {
    label: string;
    onClick: () => void;
  };
}

export const EmptyState: React.FC<EmptyStateProps> = ({ icon, title, description, action }) => (
  <div className="flex flex-col items-center justify-center py-16 px-6 text-center">
    {icon && (
      <div className="mb-4 text-slate-300">
        {icon}
      </div>
    )}
    <h3 className="text-lg font-semibold text-slate-600 mb-2">{title}</h3>
    {description && (
      <p className="text-sm text-slate-400 max-w-sm mb-6">{description}</p>
    )}
    {action && (
      <button
        onClick={action.onClick}
        className="px-4 py-2 bg-blue-500 text-white text-sm font-medium rounded-lg hover:bg-blue-600 transition-colors"
      >
        {action.label}
      </button>
    )}
  </div>
);
