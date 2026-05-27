import React from 'react';
import { Archive } from 'lucide-react';
import type { MemoryAgentSource } from '../types';
import { buildMemorySourceKey, getMemorySourceKindMeta, getMemorySourcePreview, getMemorySourceTitle } from '../adapters/memory';
import { EmptyState } from './EmptyState';

export interface MemorySourceListProps {
  sources: MemoryAgentSource[];
  selectedSourceKey: string | null;
  onSelect: (sourceKey: string) => void;
  emptyState?: {
    title: string;
    description: string;
    iconSize?: number;
  };
}

export const MemorySourceList: React.FC<MemorySourceListProps> = ({
  sources,
  selectedSourceKey,
  onSelect,
  emptyState = {
    title: '没有来源卡片',
    description: '当前输入还没有返回可追溯来源。',
    iconSize: 32,
  },
}) => {
  if (sources.length === 0) {
    return (
      <EmptyState
        icon={<Archive size={emptyState.iconSize ?? 32} />}
        title={emptyState.title}
        description={emptyState.description}
      />
    );
  }

  return (
    <div className="space-y-2">
      {sources.map((source) => {
        const meta = getMemorySourceKindMeta(source.kind);
        const SourceIcon = meta.icon;
        const sourceKey = buildMemorySourceKey(source);
        const active = sourceKey === selectedSourceKey;

        return (
          <button
            key={sourceKey}
            type="button"
            onClick={() => onSelect(sourceKey)}
            className={`w-full text-left rounded-2xl border p-3 transition-colors ${
              active ? 'border-blue-200 bg-blue-50' : 'border-slate-200 bg-white hover:bg-slate-50'
            }`}
          >
            <div className="flex items-center gap-2 mb-2">
              <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full border text-[10px] font-medium ${meta.badge}`}>
                <SourceIcon size={14} />
                {meta.label}
              </span>
            </div>
            <div className="text-sm font-medium text-slate-800 truncate">{getMemorySourceTitle(source)}</div>
            <div className="text-xs text-slate-400 mt-1 line-clamp-2">{getMemorySourcePreview(source)}</div>
          </button>
        );
      })}
    </div>
  );
};
