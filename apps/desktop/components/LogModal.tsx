import React, { useMemo, useState } from 'react';
import { X, Copy, Maximize2, Minus, Code } from 'lucide-react';
import type { WorkflowLane, WorkflowReplayData, WorkflowReplayItem } from '../types';

interface LogModalProps {
  isOpen: boolean;
  onClose: () => void;
  replay?: WorkflowReplayData | null;
}

const laneMeta: Record<WorkflowLane, { label: string; badge: string }> = {
  project: {
    label: 'PROJECT',
    badge: 'text-sky-700 bg-sky-50 border-sky-200',
  },
  plan: {
    label: 'PLAN',
    badge: 'text-indigo-700 bg-indigo-50 border-indigo-200',
  },
  task: {
    label: 'TASK',
    badge: 'text-violet-700 bg-violet-50 border-violet-200',
  },
  trace: {
    label: 'TRACE',
    badge: 'text-emerald-700 bg-emerald-50 border-emerald-200',
  },
  archive: {
    label: 'ARCHIVE',
    badge: 'text-amber-700 bg-amber-50 border-amber-200',
  },
  audit: {
    label: 'AUDIT',
    badge: 'text-rose-700 bg-rose-50 border-rose-200',
  },
};

const formatTimestamp = (timestamp?: number) => {
  if (!timestamp) return '—';
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return '—';
  return date.toLocaleTimeString('en-GB', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });
};

const buildReplayCopyText = (replay: WorkflowReplayData | null | undefined) => {
  if (!replay) {
    return 'Workflow replay is not available.';
  }

  const sections = [replay.title];
  if (replay.subtitle) sections.push(replay.subtitle);
  if (replay.summary) sections.push(replay.summary);

  const items = replay.items.map((item) => {
    const meta = [
      item.status ? `status=${item.status}` : null,
      item.actor ? `actor=${item.actor}` : null,
      item.evidence ? `evidence=${item.evidence}` : null,
      item.sourceId ? `source=${item.sourceId}` : null,
    ].filter(Boolean);

    return [
      `${formatTimestamp(item.timestamp)} [${laneMeta[item.lane]?.label ?? item.lane}] ${item.title}`,
      item.message,
      meta.length > 0 ? `  ${meta.join(' | ')}` : null,
    ].filter(Boolean).join('\n');
  });

  return [...sections, ...items].join('\n\n');
};

const ReplayItemCard = ({ item }: { item: WorkflowReplayItem }) => {
  const meta = laneMeta[item.lane] ?? laneMeta.trace;

  return (
    <div className="flex flex-col gap-2 rounded-2xl border border-slate-200 bg-white p-4 shadow-sm">
      <div className="flex flex-wrap items-center gap-2 md:gap-3 text-slate-800">
        <span className="text-[10px] font-bold text-slate-400 bg-slate-50 px-1.5 py-0.5 rounded border border-slate-200">
          {formatTimestamp(item.timestamp)}
        </span>
        <span className={`text-[10px] font-bold tracking-wide px-2 py-0.5 rounded-full border ${meta.badge}`}>
          {meta.label}
        </span>
        <span className="font-semibold break-words">{item.title}</span>
        {item.status ? (
          <span className="text-[10px] font-semibold uppercase tracking-wide text-slate-500 bg-slate-100 border border-slate-200 rounded-full px-2 py-0.5">
            {item.status}
          </span>
        ) : null}
      </div>

      <div className="pl-4 md:pl-6 border-l-2 border-slate-100 space-y-3">
        <p className="text-sm md:text-[13px] leading-6 text-slate-600 whitespace-pre-wrap break-words">
          {item.message}
        </p>

        {(item.actor || item.evidence || item.sourceId) ? (
          <div className="flex flex-wrap gap-2 text-[11px] text-slate-500">
            {item.actor ? (
              <span className="px-2 py-1 rounded-full border border-slate-200 bg-slate-50">
                actor {item.actor}
              </span>
            ) : null}
            {item.evidence ? (
              <span className="px-2 py-1 rounded-full border border-slate-200 bg-slate-50">
                evidence {item.evidence}
              </span>
            ) : null}
            {item.sourceId ? (
              <span className="px-2 py-1 rounded-full border border-slate-200 bg-slate-50 font-mono break-all">
                source {item.sourceId}
              </span>
            ) : null}
          </div>
        ) : null}
      </div>
    </div>
  );
};

export const LogModal: React.FC<LogModalProps> = ({ isOpen, onClose, replay }) => {
  const [copied, setCopied] = useState(false);

  const replayTitle = replay?.title?.trim() || 'Workflow replay';
  const replaySubtitle = replay?.subtitle?.trim() || 'Template → Plan → Task → Trace → Archive → Audit';
  const replaySummary = replay?.summary?.trim() || 'Replay data is not available for the current workflow.';
  const replayStatus = replay?.status?.trim() || (replay?.items?.length ? 'Captured' : 'Pending');
  const replayItems = replay?.items ?? [];

  const copyText = useMemo(() => buildReplayCopyText(replay), [replay]);

  if (!isOpen) return null;

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(copyText);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      setCopied(false);
    }
  };

  return (
    <div className="fixed inset-0 z-[100] flex items-center justify-center p-4">
      <div
        className="absolute inset-0 bg-white/20 backdrop-blur-sm transition-opacity"
        onClick={onClose}
      />

      <div
        role="dialog"
        aria-modal="true"
        aria-label={replayTitle}
        className="w-full max-w-4xl bg-white/80 backdrop-blur-xl rounded-3xl shadow-2xl border border-white/50 flex flex-col max-h-[85vh] animate-in fade-in zoom-in-95 duration-200 overflow-hidden relative z-10 m-2"
      >
        <div className="px-4 md:px-6 py-4 border-b border-slate-200/60 bg-white/40 flex items-center justify-between shrink-0">
          <div className="flex items-center gap-3 md:gap-4 min-w-0">
            <div className="relative group shrink-0">
              <div className="size-10 md:size-12 rounded-2xl bg-gradient-to-br from-blue-50 to-indigo-50 border border-white/60 shadow-sm flex items-center justify-center text-blue-600">
                <Code className="size-5 md:size-6" />
              </div>
              <div className="absolute -top-1 -right-1 flex h-3 w-3">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 opacity-75"></span>
                <span className="relative inline-flex rounded-full h-3 w-3 bg-green-500 border-2 border-white"></span>
              </div>
            </div>
            <div className="flex flex-col min-w-0">
              <h3 className="font-bold text-slate-800 text-base md:text-lg tracking-tight flex items-center gap-2 min-w-0">
                <span className="truncate">{replayTitle}</span>
                <span className="px-2.5 py-0.5 bg-blue-100/50 backdrop-blur-sm text-blue-600 text-[10px] rounded-full font-bold uppercase tracking-wider border border-blue-200/50 shrink-0">
                  {replayStatus}
                </span>
              </h3>
              <span className="text-[10px] md:text-xs text-slate-500 font-mono bg-white/50 px-1.5 rounded break-words md:truncate max-w-[220px] md:max-w-none">
                {replaySubtitle}
              </span>
            </div>
          </div>
          <div className="flex items-center gap-1 md:gap-2 shrink-0">
            <button type="button" className="size-8 hidden md:flex items-center justify-center rounded-full hover:bg-slate-100 text-slate-400 hover:text-slate-600 transition-all">
              <Minus className="size-4" />
            </button>
            <button type="button" className="size-8 hidden md:flex items-center justify-center rounded-full hover:bg-slate-100 text-slate-400 hover:text-slate-600 transition-all">
              <Maximize2 className="size-4" />
            </button>
            <button type="button" onClick={onClose} className="size-8 flex items-center justify-center rounded-full hover:bg-slate-100 text-slate-400 hover:text-slate-600 transition-all" aria-label="Close modal">
              <X className="size-4" />
            </button>
          </div>
        </div>

        <div className="flex-1 overflow-y-auto p-4 md:p-8 bg-slate-50/30 font-mono text-xs md:text-[13px] leading-relaxed space-y-4 md:space-y-6">
          <div className="rounded-2xl border border-slate-200 bg-white/80 p-4 md:p-5 shadow-sm space-y-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <div className="text-[10px] uppercase tracking-widest text-slate-400 mb-1">Workflow replay</div>
                <div className="text-sm md:text-base font-semibold text-slate-800">{replayTitle}</div>
              </div>
              <div className="text-[11px] text-slate-500 rounded-full border border-slate-200 bg-slate-50 px-3 py-1">
                {replayItems.length} events
              </div>
            </div>
            <div className="text-sm text-slate-600 leading-6 whitespace-pre-wrap break-words">{replaySummary}</div>
          </div>

          {replayItems.length === 0 ? (
            <div className="rounded-2xl border border-dashed border-slate-300 bg-white/70 p-6 md:p-8 text-center text-slate-500">
              <div className="text-sm font-semibold text-slate-700 mb-2">No replay events captured yet</div>
              <div className="text-xs md:text-sm leading-6">The workflow shell is available, but the current timeline does not contain replay entries.</div>
            </div>
          ) : (
            <div className="space-y-4">
              {replayItems.map((item) => (
                <ReplayItemCard key={item.id} item={item} />
              ))}
            </div>
          )}
        </div>

        <div className="px-4 md:px-8 py-4 md:py-5 border-t border-slate-200 bg-white/50 backdrop-blur-md flex justify-between items-center gap-3 shrink-0">
          <div className="flex items-center gap-2.5 text-xs font-medium text-slate-500 bg-white px-3 py-1.5 rounded-full border border-slate-200 shadow-sm">
            <span className="size-2 rounded-full bg-green-500 animate-pulse shadow-[0_0_8px_rgba(34,197,94,0.6)]"></span>
            <span>Replay stream</span>
          </div>
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={handleCopy}
              className="hidden md:flex px-5 py-2.5 rounded-full bg-white border border-slate-200 text-slate-600 text-sm font-medium hover:bg-slate-50 hover:text-blue-600 hover:border-blue-200 transition-all items-center gap-2"
            >
              <Copy className="size-4" />
              {copied ? 'Copied' : 'Copy Log'}
            </button>
            <button type="button" onClick={onClose} className="px-6 md:px-8 py-2 md:py-2.5 rounded-full bg-gradient-to-r from-blue-600 to-indigo-600 text-white text-sm font-medium hover:shadow-lg hover:shadow-blue-500/20 hover:-translate-y-0.5 transition-all">
              Close
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};
