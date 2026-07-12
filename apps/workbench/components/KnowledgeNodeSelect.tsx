import React from 'react';
import type { QueryMemoryNode } from '../types';

export interface KnowledgeNodeSelectProps {
  label: string;
  value: string;
  nodes: QueryMemoryNode[];
  onChange: (value: string | null) => void;
}

export const KnowledgeNodeSelect: React.FC<KnowledgeNodeSelectProps> = ({
  label,
  value,
  nodes,
  onChange,
}) => (
  <label className="text-xs text-slate-500 space-y-2">
    <span className="block uppercase tracking-widest">{label}</span>
    <select
      value={value}
      onChange={(e) => onChange(e.target.value || null)}
      className="w-full rounded-xl border border-slate-200 bg-white px-3 py-2 text-sm text-slate-700"
    >
      {nodes.map((node) => (
        <option key={node.id} value={node.id}>{node.label} · hop {node.hop}</option>
      ))}
    </select>
  </label>
);
