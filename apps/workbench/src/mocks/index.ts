import { toast } from 'sonner';
import { useShellStore } from '../stores/shell';

/**
 * Resolve whether mock mode is active. ALL code paths are gated behind
 * import.meta.env.DEV so the mock system is completely inert in production
 * builds — Vite statically replaces the flag and tree-shakes dead branches.
 */
export function isMockActive(): boolean {
  if (!import.meta.env.DEV) return false;

  const params = new URLSearchParams(window.location.search);
  if (params.get('mock') === '0') return false;
  if (params.get('mock') === '1') return true;

  try {
    if (localStorage.getItem('codeflow.mock') === '1') return true;
  } catch {
    /* ignore */
  }

  return useShellStore.getState().mock;
}

/**
 * Activate mock mode (dev only). Called by StartupGate when backend is
 * unreachable and user accepts mock mode, or auto-activated in dev.
 */
export function activateMock(): void {
  if (!import.meta.env.DEV) return;
  useShellStore.getState().setMock(true);
  try {
    localStorage.setItem('codeflow.mock', '1');
  } catch {
    /* ignore */
  }
}

let noticeShown = false;
export function showMockNotice(): void {
  if (!import.meta.env.DEV || noticeShown) return;
  noticeShown = true;
  toast('开发模拟数据已启用（仅开发环境）', {
    duration: 5000,
    style: {
      background: 'rgba(251,191,36,0.12)',
      border: '1px solid rgba(251,191,36,0.3)',
      color: '#FBBF24',
    },
  });
}

/** Simulate network latency: 150-400ms jitter. */
export function jitter(): Promise<void> {
  const ms = 150 + Math.random() * 250;
  return new Promise((r) => setTimeout(r, ms));
}

/**
 * In-memory mutable copy of fixtures for mutations within a single page
 * session. Loaded lazily via dynamic import so the fixture module is never
 * in the prod bundle.
 */
let _store: Awaited<typeof import('./fixtures')> | null = null;

export async function getMockStore() {
  if (!import.meta.env.DEV) throw new Error('Mock store unavailable in production');
  if (!_store) {
    _store = await import('./fixtures');
  }
  return _store;
}
