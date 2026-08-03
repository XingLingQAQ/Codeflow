import { create } from 'zustand';

export type ActivityKind = 'flow' | 'workspace' | 'guard' | 'chat';

export interface ActivityEntry {
  /** Stable id (flow event_id where available) — used for dedupe. */
  id: string;
  kind: ActivityKind;
  /** Short machine-ish title, e.g. the event type. */
  title: string;
  detail?: string;
  stageId?: string;
  /** ISO timestamp. */
  at: string;
}

const MAX_ENTRIES = 300;

interface ActivityState {
  /** Newest first. */
  entries: ActivityEntry[];
  push: (entry: ActivityEntry) => void;
  clear: () => void;
}

/**
 * Session-local realtime activity ring buffer (300 entries), fed by the
 * WebSocket bridge (flow/workspace), guard decisions, and chat lifecycle.
 */
export const useActivityStore = create<ActivityState>((set) => ({
  entries: [],
  push: (entry) =>
    set((s) =>
      s.entries.some((e) => e.id === entry.id)
        ? s
        : { entries: [entry, ...s.entries].slice(0, MAX_ENTRIES) },
    ),
  clear: () => set({ entries: [] }),
}));

let activitySeq = 0;
/** Id helper for sources without a server-side event id. */
export const nextActivityId = (prefix: string) =>
  `${prefix}-${Date.now().toString(36)}-${(++activitySeq).toString(36)}`;
