import React from 'react';

export interface MobileTabButtonProps {
  label: string;
  active: boolean;
  onClick: () => void;
}

export const MobileTabButton: React.FC<MobileTabButtonProps> = ({
  label,
  active,
  onClick,
}) => (
  <button
    type="button"
    className={`flex-1 py-3 text-sm font-bold ${active ? 'text-blue-600 border-b-2 border-blue-500' : 'text-slate-500'}`}
    onClick={onClick}
  >
    {label}
  </button>
);
