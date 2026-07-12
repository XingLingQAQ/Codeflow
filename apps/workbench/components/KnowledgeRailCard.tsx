import React from 'react';

export interface KnowledgeRailCardProps {
  label: string;
  value: React.ReactNode;
  valueClassName?: string;
}

export const KnowledgeRailCard: React.FC<KnowledgeRailCardProps> = ({
  label,
  value,
  valueClassName,
}) => (
  <div className="rounded-xl border border-blue-100 bg-white/80 p-3">
    <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-1">{label}</div>
    <div className={valueClassName ?? 'text-lg font-bold text-slate-800'}>{value}</div>
  </div>
);
