import { create } from 'zustand';
import { persist } from 'zustand/middleware';

interface DraftsState {
  /** Idea-stage draft text per project. */
  byProject: Record<string, string>;
  setDraft: (projectId: string, text: string) => void;
}

export const useDraftsStore = create<DraftsState>()(
  persist(
    (set) => ({
      byProject: {},
      setDraft: (projectId, text) =>
        set((s) => ({ byProject: { ...s.byProject, [projectId]: text } })),
    }),
    { name: 'codeflow.drafts', version: 1 },
  ),
);
