import { useEffect } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter } from 'react-router-dom';
import { Toaster } from 'sonner';
import { TooltipProvider } from './ui/Tooltip';
import { StartupGate } from './startup/StartupGate';
import { AppShell } from './shell/AppShell';
import { useShellStore, applyThemeClass, resolveIsDark } from './stores/shell';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 0, refetchOnWindowFocus: false },
  },
});

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

export default function AppRoot() {
  useThemeSync();

  return (
    <QueryClientProvider client={queryClient}>
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
    </QueryClientProvider>
  );
}
