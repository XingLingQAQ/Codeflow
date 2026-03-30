import React from 'react';

export interface SimpleEmptyHintProps {
  message: string;
  className?: string;
}

export const SimpleEmptyHint: React.FC<SimpleEmptyHintProps> = ({ message, className }) => (
  <div className={`text-center py-8 text-sm text-slate-400 ${className ?? ''}`.trim()}>{message}</div>
);
