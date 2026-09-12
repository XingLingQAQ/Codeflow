import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export type ChatRole = 'user' | 'agent';
export type ChatMessageStatus = 'streaming' | 'done' | 'error' | 'stopped';

export interface ChatMessage {
  id: string;
  role: ChatRole;
  /** Registry agent id for agent bubbles (avatar/name lookup). */
  agentId?: string;
  content: string;
  status: ChatMessageStatus;
  /** ISO timestamp. */
  at: string;
  /** Context injected with this turn. */
  ctx?: { stage: string; files: string[] };
}

/** Per-project history cap — the oldest messages fall off first. */
const MAX_MESSAGES = 200;

/** Editor selection quoted into the next chat message (session only). */
export interface ChatQuote {
  path: string;
  startLine: number;
  endLine: number;
  snippet: string;
}

let seq = 0;
export const nextMessageId = () =>
  `msg-${Date.now().toString(36)}-${(++seq).toString(36)}${Math.random().toString(36).slice(2, 6)}`;

interface ChatState {
  messagesByProject: Record<string, ChatMessage[]>;
  /** Explicit user choices keyed by projectId + stage. */
  agentByStage: Record<string, string | undefined>;
  /** Id of the agent message currently receiving deltas (session only). */
  streamingId: string | null;
  /** Pending quoted selection to attach to the next message (session only). */
  quote: ChatQuote | null;

  append: (projectId: string, msg: ChatMessage) => void;
  patchMessage: (projectId: string, id: string, patch: Partial<ChatMessage>) => void;
  appendDelta: (projectId: string, id: string, delta: string) => void;
  clearProject: (projectId: string) => void;
  setAgentForStage: (projectId: string, stage: string, agentId?: string) => void;
  setStreamingId: (id: string | null) => void;
  setQuote: (quote: ChatQuote | null) => void;
}

export const useChatStore = create<ChatState>()(
  persist(
    (set) => ({
      messagesByProject: {},
      agentByStage: {},
      streamingId: null,
      quote: null,

      append: (projectId, msg) =>
        set((s) => ({
          messagesByProject: {
            ...s.messagesByProject,
            [projectId]: [...(s.messagesByProject[projectId] ?? []), msg].slice(-MAX_MESSAGES),
          },
        })),

      patchMessage: (projectId, id, patch) =>
        set((s) => ({
          messagesByProject: {
            ...s.messagesByProject,
            [projectId]: (s.messagesByProject[projectId] ?? []).map((m) =>
              m.id === id ? { ...m, ...patch } : m,
            ),
          },
        })),

      appendDelta: (projectId, id, delta) =>
        set((s) => ({
          messagesByProject: {
            ...s.messagesByProject,
            [projectId]: (s.messagesByProject[projectId] ?? []).map((m) =>
              m.id === id ? { ...m, content: m.content + delta } : m,
            ),
          },
        })),

      clearProject: (projectId) =>
        set((s) => {
          const { [projectId]: _dropped, ...rest } = s.messagesByProject;
          return { messagesByProject: rest };
        }),

      setAgentForStage: (projectId, stage, agentId) =>
        set((s) => {
          const key = `${projectId}:${stage}`;
          if (agentId) return { agentByStage: { ...s.agentByStage, [key]: agentId } };
          const { [key]: _removed, ...rest } = s.agentByStage;
          return { agentByStage: rest };
        }),

      setStreamingId: (streamingId) => set({ streamingId }),

      setQuote: (quote) => set({ quote }),
    }),
    {
      name: 'codeflow.chat',
      version: 1,
      partialize: (s) => ({
        messagesByProject: s.messagesByProject,
        agentByStage: s.agentByStage,
      }),
      // A reload can interrupt a stream mid-flight; surface those messages as
      // stopped instead of leaving a phantom streaming state.
      merge: (persisted, current) => {
        const p = (persisted ?? {}) as Partial<ChatState>;
        const messagesByProject = Object.fromEntries(
          Object.entries(p.messagesByProject ?? {}).map(([pid, list]) => [
            pid,
            (list ?? []).map((m) =>
              m.status === 'streaming' ? { ...m, status: 'stopped' as const } : m,
            ),
          ]),
        );
        return { ...current, ...p, messagesByProject, streamingId: null, quote: null };
      },
    },
  ),
);
