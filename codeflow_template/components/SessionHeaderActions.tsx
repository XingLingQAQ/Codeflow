import React from 'react';
import { ActionButton } from './ActionButton';

export interface SessionHeaderActionsProps {
  onReplay: () => void;
  onStop: () => void;
  onRetry: () => void;
}

export const SessionHeaderActions: React.FC<SessionHeaderActionsProps> = ({
  onReplay,
  onStop,
  onRetry,
}) => (
  <div className="flex items-center gap-2">
    <ActionButton onClick={onReplay} tone="secondary" className="text-indigo-600 hover:bg-indigo-50 border-transparent">Replay</ActionButton>
    <ActionButton onClick={onStop} tone="secondary" className="text-red-500 hover:bg-red-50 border-transparent">Stop</ActionButton>
    <ActionButton onClick={onRetry} tone="secondary" className="text-blue-500 hover:bg-blue-50 border-transparent">Retry</ActionButton>
  </div>
);
