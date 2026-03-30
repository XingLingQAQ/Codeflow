import React from 'react';
import type { QueryMemoryNode } from '../types';
import { buildKnowledgeNodeSummary, formatKnowledgeNodeProperties, formatKnowledgeNodeTypes } from '../adapters/knowledge';
import { InfoChip } from './InfoChip';

export interface KnowledgeNodeCardProps {
  title: string;
  node: QueryMemoryNode | null;
  onOpenSource: (nodeId: string) => void;
}

export const KnowledgeNodeCard: React.FC<KnowledgeNodeCardProps> = ({ title, node, onOpenSource }) => {
  const propertyEntries = formatKnowledgeNodeProperties(node?.properties);

  return (
    <div className="rounded-xl border border-slate-200 bg-slate-50 p-3 space-y-3">
      <div>
        <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-2">{title}</div>
        <div className="font-semibold text-slate-800 break-words">{node?.label || 'No node selected'}</div>
        {node && <div className="text-xs text-slate-400 mt-1 break-all">{node.id}</div>}
      </div>
      {node ? (
        <>
          <div className="text-xs text-slate-500">{buildKnowledgeNodeSummary(node)}</div>
          <div className="flex flex-wrap gap-2 text-[11px]">
            <InfoChip label={`hop ${node.hop}`} />
            <InfoChip label={`activation ${node.activation.toFixed(2)}`} />
            <InfoChip label={formatKnowledgeNodeTypes(node['@type'])} />
          </div>
          {node.aliases && node.aliases.length > 0 && (
            <div>
              <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-2">Aliases</div>
              <div className="flex flex-wrap gap-2">
                {node.aliases.map((alias) => (
                  <InfoChip key={alias} label={alias} tone="violet" />
                ))}
              </div>
            </div>
          )}
          {propertyEntries.length > 0 && (
            <div>
              <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-2">Properties</div>
              <div className="space-y-2">
                {propertyEntries.slice(0, 4).map(([key, value]) => (
                  <div key={key} className="rounded-lg border border-white bg-white px-3 py-2 text-xs text-slate-600 break-words">
                    <span className="text-slate-400">{key}</span> {typeof value === 'string' ? value : JSON.stringify(value)}
                  </div>
                ))}
              </div>
            </div>
          )}
          {node.pointers && node.pointers.length > 0 && (
            <div>
              <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-2">Pointers</div>
              <div className="space-y-2">
                {node.pointers.slice(0, 2).map((pointer, index) => (
                  <button
                    key={`${pointer.source_id}-${index}`}
                    type="button"
                    onClick={() => onOpenSource(node.id)}
                    className="w-full rounded-lg border border-white bg-white px-3 py-2 text-left text-xs text-slate-600 hover:border-blue-200 hover:bg-blue-50"
                  >
                    <div className="font-medium text-slate-700">{pointer.summary || pointer.source_id}</div>
                    <div className="text-slate-400 mt-1">{pointer.file_path || pointer.source_type || 'source'} {pointer.line_range ? `· ${pointer.line_range}` : ''}</div>
                  </button>
                ))}
              </div>
            </div>
          )}
        </>
      ) : null}
    </div>
  );
};
