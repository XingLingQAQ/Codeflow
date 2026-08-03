import { create } from 'zustand';

export type ThemeMode = 'light' | 'dark' | 'system';
export type MotionPref = 'system' | 'reduce' | 'full';

interface ShellState {
  commandOpen: boolean;
  setCommandOpen: (v: boolean) => void;
  offline: boolean;
  setOffline: (v: boolean) => void;
  mock: boolean;
  setMock: (v: boolean) => void;
  themeMode: ThemeMode;
  setThemeMode: (m: ThemeMode) => void;
  motionPref: MotionPref;
  setMotionPref: (p: MotionPref) => void;
  /** Live WebSocket connection state (driven by services-bridge/ws). */
  wsConnected: boolean;
  setWsConnected: (v: boolean) => void;
}

function loadThemeMode(): ThemeMode {
  try {
    const stored = localStorage.getItem('codeflow.theme');
    if (stored === 'light' || stored === 'dark' || stored === 'system') return stored;
  } catch {
    /* ignore */
  }
  return 'light';
}

function loadMotionPref(): MotionPref {
  try {
    const stored = localStorage.getItem('codeflow.motion');
    if (stored === 'system' || stored === 'reduce' || stored === 'full') return stored;
  } catch {
    /* ignore */
  }
  return 'system';
}

export const useShellStore = create<ShellState>((set) => ({
  commandOpen: false,
  setCommandOpen: (commandOpen) => set({ commandOpen }),
  offline: false,
  setOffline: (offline) => set({ offline }),
  mock: false,
  setMock: (mock) => set({ mock }),
  themeMode: loadThemeMode(),
  setThemeMode: (themeMode) => {
    set({ themeMode });
    try {
      localStorage.setItem('codeflow.theme', themeMode);
    } catch {
      /* ignore */
    }
    applyThemeClass(themeMode);
  },
  motionPref: loadMotionPref(),
  setMotionPref: (motionPref) => {
    set({ motionPref });
    try {
      localStorage.setItem('codeflow.motion', motionPref);
    } catch {
      /* ignore */
    }
    applyMotionClass(motionPref);
  },
  wsConnected: false,
  setWsConnected: (wsConnected) => set({ wsConnected }),
}));

export function resolveIsDark(mode: ThemeMode): boolean {
  if (mode === 'dark') return true;
  if (mode === 'light') return false;
  return window.matchMedia('(prefers-color-scheme: dark)').matches;
}

export function applyThemeClass(mode: ThemeMode): void {
  const dark = resolveIsDark(mode);
  document.documentElement.classList.toggle('dark', dark);
}

export function resolveReducedMotion(pref: MotionPref): boolean {
  if (pref === 'reduce') return true;
  if (pref === 'full') return false;
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches;
}

/** Sync html.cf-reduced so the CSS side (theme.css) follows the preference too. */
export function applyMotionClass(pref: MotionPref): void {
  document.documentElement.classList.toggle('cf-reduced', resolveReducedMotion(pref));
}
