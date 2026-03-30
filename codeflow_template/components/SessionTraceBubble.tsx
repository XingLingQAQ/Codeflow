import React from 'react';
import type { CallTrace } from '../types';

export interface SessionTraceBubbleProps {
  trace: CallTrace;
}

export const SessionTraceBubble: React.FC<SessionTraceBubbleProps> = ({ trace }) => {
  const isOrchestrator = trace.agent_role === 'orchestrator';

  return (
    <div className={`flex ${isOrchestrator ? 'justify-end' : 'justify-start'}`}>
      <div className={`rounded-2xl px-4 py-3 max-w-[90%] md:max-w-2xl shadow-sm ${isOrchestrator ? 'bg-blue-600 text-white rounded-br-sm' : 'bg-white border border-slate-200 rounded-tl-sm'}`}>
        <p className="text-xs font-bold mb-1 opacity-70">{trace.agent_role} • {trace.tool_name}</p>
        <p className="text-sm leading-relaxed">{trace.output || 'Processing...'}</p>
      </div>
    </div>
  );
};
