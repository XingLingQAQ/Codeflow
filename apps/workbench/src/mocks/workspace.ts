// Dev-only mock implementation of the workspace API. Loaded exclusively via
// dynamic import from services-bridge/workspace.ts inside an
// `if (import.meta.env.DEV)` branch, so none of this reaches production.
import { getMockStore, jitter } from './index';
import type {
  WorkspaceEntry,
  WorkspaceFileContent,
  WriteMode,
  ScriptInfo,
} from '../services-bridge/workspace';

type Store = Awaited<ReturnType<typeof getMockStore>>;

function guardCheck(store: Store, path: string, content: string): void {
  const norm = path.replace(/\\/g, '/');
  const denied =
    norm.includes('node_modules/') ||
    /(^|\/)\.env/.test(norm) ||
    norm.endsWith('.secret');
  if (denied) {
    throw new Error(`blocked by guard: 路径命中受保护规则 (${norm})`);
  }
  if (content.length > store.MOCK_GUARD_META.max_file_bytes) {
    throw new Error(
      `blocked by guard: 内容超出大小上限 ${store.MOCK_GUARD_META.max_file_bytes} 字节`,
    );
  }
}

function ensureEntry(store: Store, path: string, size: number): WorkspaceEntry {
  const name = path.split('/').pop() ?? path;
  const now = new Date().toISOString();
  const list = store.MOCK_WORKSPACE as WorkspaceEntry[];
  let entry = list.find((e) => e.path === path && !e.is_dir);
  if (entry) {
    entry.size = size;
    entry.mod_time = now;
  } else {
    // Create missing parent directory entries so the tree stays navigable.
    const segments = path.split('/');
    for (let i = 1; i < segments.length; i++) {
      const dirPath = segments.slice(0, i).join('/');
      if (!list.some((e) => e.path === dirPath && e.is_dir)) {
        list.push({ name: segments[i - 1], path: dirPath, is_dir: true, mod_time: now });
      }
    }
    entry = { name, path, is_dir: false, size, mod_time: now };
    list.push(entry);
  }
  return { ...entry };
}

function baselineContent(store: Store, path: string): string {
  const known = store.MOCK_FILE_CONTENTS[path];
  if (known !== undefined) return known;
  const exists = (store.MOCK_WORKSPACE as WorkspaceEntry[]).some(
    (e) => e.path === path && !e.is_dir,
  );
  if (!exists) throw new Error(`file not found: ${path}`);
  return `// ${path}\n// （开发模拟数据：该文件暂无独立内容 fixture）\n`;
}

export async function mockListWorkspace(path = '') {
  await jitter();
  const store = await getMockStore();
  const prefix = path ? path.replace(/\/+$/, '') + '/' : '';
  const items = (store.MOCK_WORKSPACE as WorkspaceEntry[])
    .filter((e) => {
      if (!e.path.startsWith(prefix)) return false;
      const rest = e.path.slice(prefix.length);
      return rest.length > 0 && !rest.includes('/');
    })
    .map((e) => ({ ...e }))
    .sort((a, b) => (a.is_dir === b.is_dir ? a.name.localeCompare(b.name) : a.is_dir ? -1 : 1));
  return { items, total: items.length };
}

export async function mockReadWorkspaceFile(
  path: string,
  staged = false,
): Promise<WorkspaceFileContent> {
  await jitter();
  const store = await getMockStore();
  if (staged) {
    const entry = store.MOCK_STAGED[path];
    if (!entry) throw new Error(`staged file not found: ${path}`);
    return { path, size: entry.content.length, mod_time: entry.mod_time, content_text: entry.content };
  }
  const content = baselineContent(store, path);
  const meta = (store.MOCK_WORKSPACE as WorkspaceEntry[]).find((e) => e.path === path);
  return {
    path,
    size: content.length,
    mod_time: meta?.mod_time ?? new Date().toISOString(),
    content_text: content,
  };
}

export async function mockWriteWorkspaceFile(
  path: string,
  contentText: string,
  mode: WriteMode,
): Promise<WorkspaceEntry> {
  await jitter();
  const store = await getMockStore();
  guardCheck(store, path, contentText);
  if (mode === 'stage') {
    store.MOCK_STAGED[path] = { content: contentText, mod_time: new Date().toISOString() };
    return {
      name: path.split('/').pop() ?? path,
      path,
      is_dir: false,
      size: contentText.length,
      mod_time: store.MOCK_STAGED[path].mod_time,
    };
  }
  store.MOCK_FILE_CONTENTS[path] = contentText;
  return ensureEntry(store, path, contentText.length);
}

export async function mockListStaged() {
  await jitter();
  const store = await getMockStore();
  const items: WorkspaceEntry[] = Object.entries(store.MOCK_STAGED)
    .map(([path, v]) => ({
      name: path.split('/').pop() ?? path,
      path,
      is_dir: false,
      size: v.content.length,
      mod_time: v.mod_time,
    }))
    .sort((a, b) => a.path.localeCompare(b.path));
  return { items, total: items.length };
}

export async function mockPromote(path: string): Promise<WorkspaceEntry> {
  await jitter();
  const store = await getMockStore();
  const staged = store.MOCK_STAGED[path];
  if (!staged) throw new Error(`staged file not found: ${path}`);
  guardCheck(store, path, staged.content);
  store.MOCK_FILE_CONTENTS[path] = staged.content;
  const entry = ensureEntry(store, path, staged.content.length);
  delete store.MOCK_STAGED[path];
  return entry;
}

export async function mockDiscard(path: string) {
  await jitter();
  const store = await getMockStore();
  if (!store.MOCK_STAGED[path]) throw new Error(`staged file not found: ${path}`);
  delete store.MOCK_STAGED[path];
  return { discarded: true, path };
}

export async function mockPromoteAll() {
  await jitter();
  const store = await getMockStore();
  const items: WorkspaceEntry[] = [];
  let firstError: string | undefined;
  for (const path of Object.keys(store.MOCK_STAGED)) {
    try {
      const staged = store.MOCK_STAGED[path];
      guardCheck(store, path, staged.content);
      store.MOCK_FILE_CONTENTS[path] = staged.content;
      items.push(ensureEntry(store, path, staged.content.length));
      delete store.MOCK_STAGED[path];
    } catch (err) {
      firstError = firstError ?? (err instanceof Error ? err.message : String(err));
    }
  }
  return { items, total: items.length, error: firstError, partial: !!firstError };
}

export async function mockDiscardAll() {
  await jitter();
  const store = await getMockStore();
  const keys = Object.keys(store.MOCK_STAGED);
  for (const path of keys) delete store.MOCK_STAGED[path];
  return { discarded: keys.length };
}

export async function mockDetectScripts() {
  await jitter();
  const store = await getMockStore();
  const items: ScriptInfo[] = store.MOCK_SCRIPTS.map((s) => ({ ...s }));
  return { items, total: items.length };
}
