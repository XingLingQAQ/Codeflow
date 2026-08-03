import { create } from 'zustand';
import { persist } from 'zustand/middleware';

/** One open editor tab (path is relative to the workspace root). */
export interface EditorTab {
  path: string;
  name: string;
}

export interface WorkspaceProblem {
  id: string;
  severity: 'error' | 'warn';
  message: string;
  path?: string;
  source: string;
  time: string;
}

export interface WorkspaceAuditEntry {
  id: string;
  /** Operation label, e.g. 暂存写入 / 直接写入 / 应用 / 丢弃 */
  op: string;
  path: string;
  ok: boolean;
  detail?: string;
  time: string;
}

/** Buffer key: one buffer per project+path pair. */
export const bufKey = (projectId: string, path: string) => `${projectId}\u0000${path}`;

let seq = 0;
const nextId = () => `${Date.now().toString(36)}-${++seq}`;

const MAX_LOG = 200;

interface EditorState {
  /** Workspace root per project (absolute path; virtual in mock mode). */
  rootByProject: Record<string, string>;
  tabsByProject: Record<string, EditorTab[]>;
  activeByProject: Record<string, string | undefined>;
  /** Context-builder selection: path → byte size, per project. */
  contextByProject: Record<string, Record<string, number>>;
  /** Unsaved editor buffers (session only). */
  buffers: Record<string, string>;
  /** Last loaded/saved content per buffer (dirty = buffer !== baseline). */
  baselines: Record<string, string>;
  problems: WorkspaceProblem[];
  audit: WorkspaceAuditEntry[];

  setRoot: (projectId: string, root: string) => void;
  openFile: (projectId: string, path: string) => void;
  closeFile: (projectId: string, path: string) => void;
  setActive: (projectId: string, path: string) => void;
  /** Initialize buffer + baseline after a fresh read (no-op if buffer exists). */
  setLoaded: (key: string, content: string) => void;
  setBuffer: (key: string, content: string) => void;
  /** After a successful write: baseline catches up to the saved content. */
  markSaved: (key: string, content: string) => void;
  /** Drop buffer + baseline (e.g. discard staged → reload from disk). */
  dropBuffer: (key: string) => void;
  toggleContext: (projectId: string, path: string, size: number) => void;
  clearContext: (projectId: string) => void;
  addProblem: (p: Omit<WorkspaceProblem, 'id' | 'time'>) => void;
  clearProblems: () => void;
  addAudit: (e: Omit<WorkspaceAuditEntry, 'id' | 'time'>) => void;
}

export const useEditorStore = create<EditorState>()(
  persist(
    (set) => ({
      rootByProject: {},
      tabsByProject: {},
      activeByProject: {},
      contextByProject: {},
      buffers: {},
      baselines: {},
      problems: [],
      audit: [],

      setRoot: (projectId, root) =>
        set((s) => ({ rootByProject: { ...s.rootByProject, [projectId]: root } })),

      openFile: (projectId, path) =>
        set((s) => {
          const tabs = s.tabsByProject[projectId] ?? [];
          const exists = tabs.some((t) => t.path === path);
          return {
            tabsByProject: exists
              ? s.tabsByProject
              : {
                  ...s.tabsByProject,
                  [projectId]: [...tabs, { path, name: path.split('/').pop() ?? path }],
                },
            activeByProject: { ...s.activeByProject, [projectId]: path },
          };
        }),

      closeFile: (projectId, path) =>
        set((s) => {
          const tabs = s.tabsByProject[projectId] ?? [];
          const idx = tabs.findIndex((t) => t.path === path);
          if (idx === -1) return s;
          const next = tabs.filter((t) => t.path !== path);
          const key = bufKey(projectId, path);
          const { [key]: _b, ...buffers } = s.buffers;
          const { [key]: _bl, ...baselines } = s.baselines;
          let active = s.activeByProject[projectId];
          if (active === path) {
            active = next[Math.min(idx, next.length - 1)]?.path;
          }
          return {
            tabsByProject: { ...s.tabsByProject, [projectId]: next },
            activeByProject: { ...s.activeByProject, [projectId]: active },
            buffers,
            baselines,
          };
        }),

      setActive: (projectId, path) =>
        set((s) => ({ activeByProject: { ...s.activeByProject, [projectId]: path } })),

      setLoaded: (key, content) =>
        set((s) =>
          s.buffers[key] !== undefined
            ? s
            : {
                buffers: { ...s.buffers, [key]: content },
                baselines: { ...s.baselines, [key]: content },
              },
        ),

      setBuffer: (key, content) => set((s) => ({ buffers: { ...s.buffers, [key]: content } })),

      markSaved: (key, content) =>
        set((s) => ({ baselines: { ...s.baselines, [key]: content } })),

      dropBuffer: (key) =>
        set((s) => {
          const { [key]: _b, ...buffers } = s.buffers;
          const { [key]: _bl, ...baselines } = s.baselines;
          return { buffers, baselines };
        }),

      toggleContext: (projectId, path, size) =>
        set((s) => {
          const sel = { ...(s.contextByProject[projectId] ?? {}) };
          if (path in sel) delete sel[path];
          else sel[path] = size;
          return { contextByProject: { ...s.contextByProject, [projectId]: sel } };
        }),

      clearContext: (projectId) =>
        set((s) => ({ contextByProject: { ...s.contextByProject, [projectId]: {} } })),

      addProblem: (p) =>
        set((s) => ({
          problems: [{ ...p, id: nextId(), time: new Date().toISOString() }, ...s.problems].slice(
            0,
            MAX_LOG,
          ),
        })),

      clearProblems: () => set({ problems: [] }),

      addAudit: (e) =>
        set((s) => ({
          audit: [{ ...e, id: nextId(), time: new Date().toISOString() }, ...s.audit].slice(
            0,
            MAX_LOG,
          ),
        })),
    }),
    {
      name: 'codeflow.editor',
      version: 1,
      partialize: (s) => ({
        rootByProject: s.rootByProject,
        tabsByProject: s.tabsByProject,
        activeByProject: s.activeByProject,
        contextByProject: s.contextByProject,
      }),
    },
  ),
);
