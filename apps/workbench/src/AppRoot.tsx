import { useEffect } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter } from 'react-router-dom';
import { MotionConfig } from 'motion/react';
import { Toaster } from 'sonner';
import { TooltipProvider } from './ui/Tooltip';
import { StartupGate } from './startup/StartupGate';
import { AppShell } from './shell/AppShell';
import {
  useShellStore,
  applyThemeClass,
  applyMotionClass,
} from './stores/shell';
import {
  registerIdentityScopedCache,
  resetQueryClientForNewPairing,
} from './services-bridge/identityCaches';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 0, refetchOnWindowFocus: false },
  },
});

// React Query holds server-derived data (projects, flows, audit, ...) for the
// current pairing only; a sidecar re-pair must not leak it across identities.
// The reset goes through resetQueryClientForNewPairing because a bare
// queryClient.clear() empties the store without telling mounted observers —
// they would keep rendering the previous backend's data. User-local state
// (drafts/layout/editor/chat in zustand persist, theme) is untouched — it
// never lives in the query cache.
registerIdentityScopedCache('react-query', () => resetQueryClientForNewPairing(queryClient));

function useThemeSync() {
  const mode = useShellStore((s) => s.themeMode);

  useEffect(() => {
    applyThemeClass(mode);
  }, [mode]);

  useEffect(() => {
    if (mode !== 'system') return;
    const mql = window.matchMedia('(prefers-color-scheme: dark)');
    const onChange = () => applyThemeClass('system');
    mql.addEventListener('change', onChange);
    return () => mql.removeEventListener('change', onChange);
  }, [mode]);
}

function useMotionSync() {
  const pref = useShellStore((s) => s.motionPref);

  useEffect(() => {
    applyMotionClass(pref);
  }, [pref]);

  useEffect(() => {
    if (pref !== 'system') return;
    const mql = window.matchMedia('(prefers-reduced-motion: reduce)');
    const onChange = () => applyMotionClass('system');
    mql.addEventListener('change', onChange);
    return () => mql.removeEventListener('change', onChange);
  }, [pref]);
}

export default function AppRoot() {
  useThemeSync();
  useMotionSync();
  const motionPref = useShellStore((s) => s.motionPref);

  return (
    <QueryClientProvider client={queryClient}>
      <MotionConfig
        reducedMotion={motionPref === 'reduce' ? 'always' : motionPref === 'full' ? 'never' : 'user'}
      >
        <BrowserRouter>
          <TooltipProvider>
            <StartupGate>
              <AppShell />
            </StartupGate>
            <Toaster
              position="bottom-right"
              toastOptions={{
                style: {
                  background: 'var(--glass-bg)',
                  border: '1px solid var(--glass-border)',
                  backdropFilter: 'blur(12px)',
                  color: 'var(--color-ink)',
                },
              }}
            />
          </TooltipProvider>
        </BrowserRouter>
      </MotionConfig>
    </QueryClientProvider>
  );
}
