import { get, getApiBase } from '../../api';

export interface WorkspaceEntry {
  name: string;
  path: string;
  is_dir: boolean;
  size?: number;
  mod_time: string;
}

/** GET /api/v1/workspace/list — requires an absolute project root (experimental). */
export async function listWorkspace(root: string, path = '', signal?: AbortSignal) {
  if (import.meta.env.DEV) {
    const { isMockActive, jitter, getMockStore } = await import('../mocks');
    if (isMockActive()) {
      await jitter();
      const store = await getMockStore();
      let items = [...store.MOCK_WORKSPACE];
      if (path) {
        items = items.filter((e) => e.path.startsWith(path + '/') || e.path === path);
      }
      return { items, total: items.length };
    }
  }
  return get<{ items: WorkspaceEntry[]; total: number }>(
    `${getApiBase()}/api/v1/workspace/list`,
    { root, path },
    signal,
  );
}
