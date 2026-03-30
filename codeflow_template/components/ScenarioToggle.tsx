import React from 'react';
import { InfoChip } from './InfoChip';

export interface ScenarioToggleProps {
  label: React.ReactNode;
  active: boolean;
  onClick: () => void;
}

export const ScenarioToggle: React.FC<ScenarioToggleProps> = ({ label, active, onClick }) => (
  <button type="button" onClick={onClick}>
    <InfoChip
      label={label}
      tone={active ? 'blue' : 'slate'}
      className={active ? 'bg-blue-600 text-white border-blue-200 px-3 py-2 rounded-xl text-xs font-semibold' : 'hover:bg-slate-50 px-3 py-2 rounded-xl text-xs font-semibold text-slate-600'}
    />
  </button>
);
