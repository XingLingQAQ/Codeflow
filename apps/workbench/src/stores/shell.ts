import { create } from 'zustand';

export type ThemeMode = 'light' | 'dark' | 'system';

interface ShellState {
  commandOpen: boolean;
  setCommandOpen: (v: boolean) => void;
  offline: boolean;
  setOffline: (v: boolean) => void;
  mock: boolean;
  setMock: (v: boolean) => void;
  themeMode: ThemeMode;
  setThemeMode: (m: ThemeMode) => void;
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
