// Workspace filesystem client (experimental API /api/v1/workspace/*).
// Every function carries a dev-only mock branch behind import.meta.env.DEV +
// dynamic import so mock code and fixtures never reach the production bundle.
import { get, post, del, getApiBase } from '../../api';

export interface WorkspaceEntry {
  name: string;
  path: string;
  is_dir: boolean;
  size?: number;
  mod_time: string;
}

export interface WorkspaceFileContent {
  path: string;
  size: number;
  mod_time: string;
  content_text: string;
  content_base64?: string;
}

export interface ScriptInfo {
  name: string;
  command: string;
}

export type WriteMode = 'direct' | 'stage';

const base = () => `${getApiBase()}/api/v1/workspace`;

/** GET /api/v1/workspace/list — direct children of `path` under `root`. */
export async function listWorkspace(root: string, path = '', signal?: AbortSignal) {
  if (import.meta.env.DEV) {
    const { isMockActive } = await import('../mocks');
    if (isMockActive()) {
      const m = await import('../mocks/workspace');
      return m.mockListWorkspace(path);
    }
  }
  return get<{ items: WorkspaceEntry[]; total: number }>(
    `${base()}/list`,
    { root, path },
    signal,
  );
}

/** GET /api/v1/workspace/read — file content (staged=true reads the shadow copy). */
export async function readWorkspaceFile(
  root: string,
  path: string,
  staged = false,
  signal?: AbortSignal,
) {
  if (import.meta.env.DEV) {
    const { isMockActive } = await import('../mocks');
    if (isMockActive()) {
      const m = await import('../mocks/workspace');
      return m.mockReadWorkspaceFile(path, staged);
    }
  }
  return get<WorkspaceFileContent>(
    `${base()}/read`,
    { root, path, staged: staged ? 'true' : undefined },
    signal,
  );
}

/** POST /api/v1/workspace/write — guarded write (mode: stage | direct). */
export async function writeWorkspaceFile(
  input: { root: string; path: string; contentText: string; mode?: WriteMode; createParents?: boolean },
  signal?: AbortSignal,
) {
  const { root, path, contentText, mode = 'stage', createParents = true } = input;
  if (import.meta.env.DEV) {
    const { isMockActive } = await import('../mocks');
    if (isMockActive()) {
      const m = await import('../mocks/workspace');
      return m.mockWriteWorkspaceFile(path, contentText, mode);
    }
  }
  return post<WorkspaceEntry>(
    `${base()}/write`,
    { root, path, content_text: contentText, mode, create_parents: createParents },
    signal,
  );
}

/** GET /api/v1/workspace/staged — files sitting in .codeflow/staging. */
export async function listStagedWorkspace(root: string, signal?: AbortSignal) {
  if (import.meta.env.DEV) {
    const { isMockActive } = await import('../mocks');
    if (isMockActive()) {
      const m = await import('../mocks/workspace');
      return m.mockListStaged();
    }
  }
  return get<{ items: WorkspaceEntry[]; total: number }>(`${base()}/staged`, { root }, signal);
}

/** POST /api/v1/workspace/promote — apply one staged file to the working tree. */
export async function promoteWorkspaceFile(root: string, path: string, signal?: AbortSignal) {
  if (import.meta.env.DEV) {
    const { isMockActive } = await import('../mocks');
    if (isMockActive()) {
      const m = await import('../mocks/workspace');
      return m.mockPromote(path);
    }
  }
  return post<WorkspaceEntry>(`${base()}/promote`, { root, path }, signal);
}

/** POST /api/v1/workspace/discard — drop one staged file (working tree untouched). */
export async function discardStagedWorkspace(root: string, path: string, signal?: AbortSignal) {
  if (import.meta.env.DEV) {
    const { isMockActive } = await import('../mocks');
    if (isMockActive()) {
      const m = await import('../mocks/workspace');
      return m.mockDiscard(path);
    }
  }
  return post<{ discarded: boolean; path: string }>(`${base()}/discard`, { root, path }, signal);
}

/** POST /api/v1/workspace/promote-all — apply the whole staging area. */
export async function promoteAllWorkspace(root: string, signal?: AbortSignal) {
  if (import.meta.env.DEV) {
    const { isMockActive } = await import('../mocks');
    if (isMockActive()) {
      const m = await import('../mocks/workspace');
      return m.mockPromoteAll();
    }
  }
  return post<{ items: WorkspaceEntry[]; total: number; error?: string; partial?: boolean }>(
    `${base()}/promote-all`,
    { root },
    signal,
  );
}

/** POST /api/v1/workspace/discard-all — clear the staging area. */
export async function discardAllStagedWorkspace(root: string, signal?: AbortSignal) {
  if (import.meta.env.DEV) {
    const { isMockActive } = await import('../mocks');
    if (isMockActive()) {
      const m = await import('../mocks/workspace');
      return m.mockDiscardAll();
    }
  }
  return post<{ discarded: number }>(`${base()}/discard-all`, { root }, signal);
}

/** GET /api/v1/workspace/scripts — detected package.json scripts. */
export async function detectWorkspaceScripts(root: string, signal?: AbortSignal) {
  if (import.meta.env.DEV) {
    const { isMockActive } = await import('../mocks');
    if (isMockActive()) {
      const m = await import('../mocks/workspace');
      return m.mockDetectScripts();
    }
  }
  return get<{ items: ScriptInfo[]; total: number }>(`${base()}/scripts`, { root }, signal);
}

export interface WorkspaceWatch {
  watch_id: string;
  root: string;
  /** Per-root WS fan-out topic ("workspace:root:{hash8}") to subscribe with. */
  topic: string;
  interval_ms: number;
  created_at: string;
}

/**
 * POST /api/v1/workspace/watch — start (or reuse, idempotent per resolved
 * root) a polling watcher that broadcasts file changes on `topic`. The server
 * caps watches at 16 per process; delete the old watch when the root changes.
 * No mock branch: in dev-mock mode the workbench never opens a socket.
 */
export function createWorkspaceWatch(root: string, signal?: AbortSignal) {
  return post<WorkspaceWatch>(`${base()}/watch`, { root }, signal);
}

/** DELETE /api/v1/workspace/watch?id= — stop a watcher. */
export function deleteWorkspaceWatch(watchId: string, signal?: AbortSignal) {
  return del<{ watch_id: string; stopped: boolean }>(
    `${base()}/watch?id=${encodeURIComponent(watchId)}`,
    signal,
  );
}
