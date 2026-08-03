import { useShellStore } from '../stores/shell';

/**
 * Synchronous mirror of `mocks/index.isMockActive` for render-time checks
 * (e.g. auto-filling a virtual workspace root). Every path is gated behind
 * `import.meta.env.DEV`, so production builds compile this down to
 * `return false` and drop the rest — no mock markers survive minification.
 */
export function isDevMockActive(): boolean {
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
