import React from 'react';
import type { CallTrace } from '../types';
import { SessionTraceBubble } from './SessionTraceBubble';
import { SimpleEmptyHint } from './SimpleEmptyHint';

export interface SessionTraceListProps {
  traces?: CallTrace[];
}

export const SessionTraceList: React.FC<SessionTraceListProps> = ({ traces }) => (
  <div className="space-y-4">
    {traces?.map((child, i) => (
      <SessionTraceBubble key={child.id || i} trace={child} />
    )) ?? <SimpleEmptyHint message="Empty trace" className="py-4" />}
  </div>
);
