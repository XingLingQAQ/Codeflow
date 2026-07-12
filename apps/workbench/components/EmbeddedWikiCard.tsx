import React from 'react';
import { ExternalLink } from 'lucide-react';
import { ActionButton } from './ActionButton';

export interface EmbeddedWikiEntryViewModel {
  id: string;
  label: string;
  content: string;
  action?: string;
  actionLabel?: string;
}

export interface EmbeddedWikiCardProps {
  entry: EmbeddedWikiEntryViewModel;
  onAction: (entry: EmbeddedWikiEntryViewModel) => void;
}

export const EmbeddedWikiCard: React.FC<EmbeddedWikiCardProps> = ({ entry, onAction }) => (
  <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4 text-sm text-slate-600">
    <div className="flex items-start justify-between gap-3">
      <div className="min-w-0">
        <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-2">{entry.label}</div>
        <div className="leading-6 whitespace-pre-wrap break-words">{entry.content}</div>
      </div>
      {entry.action && entry.actionLabel ? (
        <ActionButton
          onClick={() => onAction(entry)}
          tone="accent"
          size="inline"
          className="shrink-0"
        >
          {entry.actionLabel}
          <ExternalLink size={12} />
        </ActionButton>
      ) : null}
    </div>
  </div>
);
