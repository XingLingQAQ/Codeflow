import React from 'react';
import { ExternalLink } from 'lucide-react';
import type { MemoryAgentSource } from '../types';
import { getMemorySourceKindMeta, getMemorySourceTitle } from '../adapters/memory';
import { EmptyState } from './EmptyState';

export interface MemorySourceDetailProps {
  source: MemoryAgentSource | null;
  emptyState?: {
    title: string;
    description: string;
    iconSize?: number;
  };
  showTimestamp?: boolean;
  contentMinHeightClass?: string;
}

export const MemorySourceDetail: React.FC<MemorySourceDetailProps> = ({
  source,
  emptyState = {
    title: '选择来源卡片',
    description: '右侧会展示来源详情和可追溯内容。',
    iconSize: 32,
  },
  showTimestamp = false,
  contentMinHeightClass = 'min-h-24',
}) => {
  if (!source) {
    return (
      <EmptyState
        icon={<ExternalLink size={emptyState.iconSize ?? 32} />}
        title={emptyState.title}
        description={emptyState.description}
      />
    );
  }

  const meta = getMemorySourceKindMeta(source.kind);
  const SourceIcon = meta.icon;

  return (
    <div className="space-y-4">
      <div>
        <div className="flex flex-wrap items-center gap-2 mb-3">
          <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full border text-[10px] font-medium ${meta.badge}`}>
            <SourceIcon size={14} />
            {meta.label}
          </span>
        </div>
        <h3 className="text-lg font-semibold text-slate-900 break-words">{getMemorySourceTitle(source)}</h3>
        <div className="flex flex-wrap items-center gap-2 mt-3 text-[11px] text-slate-400">
          <span>{source.id}</span>
          {source.session_id && <span>session {source.session_id}</span>}
          {showTimestamp && source.timestamp && <span>{new Date(source.timestamp * 1000).toLocaleString()}</span>}
          {typeof source.relevance === 'number' && <span>relevance {source.relevance.toFixed(2)}</span>}
        </div>
      </div>

      {(source.file_path || source.line_range || source.source_type) && (
        <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4 space-y-2 text-sm text-slate-600">
          {source.source_type && <div><span className="text-slate-400">source_type</span> {source.source_type}</div>}
          {source.file_path && <div><span className="text-slate-400">file</span> {source.file_path}</div>}
          {source.line_range && <div><span className="text-slate-400">line</span> {source.line_range}</div>}
          {source.node_label && <div><span className="text-slate-400">node</span> {source.node_label}</div>}
        </div>
      )}

      {source.summary && (
        <div>
          <div className="text-xs font-medium uppercase tracking-wider text-slate-400 mb-2">Summary</div>
          <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4 text-sm leading-6 text-slate-600 whitespace-pre-wrap break-words">{source.summary}</div>
        </div>
      )}

      <div>
        <div className="text-xs font-medium uppercase tracking-wider text-slate-400 mb-2">Content</div>
        <div className={`rounded-2xl border border-slate-200 bg-slate-50 p-4 text-sm leading-6 text-slate-600 whitespace-pre-wrap break-words ${contentMinHeightClass}`}>
          {source.content || source.summary || '这个来源暂时没有展开内容。'}
        </div>
      </div>
    </div>
  );
};
