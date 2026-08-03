import { Suspense, lazy, useEffect } from 'react';
import { Routes, Route, Navigate, useLocation, useParams } from 'react-router-dom';
import { AnimatePresence, motion } from 'motion/react';
import { NavRail } from './NavRail';
import { RouteProgress } from './RouteProgress';
import { CommandPalette } from './CommandPalette';
import { Spinner } from '../ui/Spinner';
import { useShellStore } from '../stores/shell';
import { useLayoutStore } from '../stores/layout';
import { fadeSlide } from '../lib/motion';
import { DEFAULT_STAGE } from '../stages/stageMeta';

const Dashboard = lazy(() => import('./pages/Dashboard'));
const Projects = lazy(() => import('./pages/Projects'));
const Flows = lazy(() => import('./pages/Flows'));
const Agents = lazy(() => import('./pages/Agents'));
const Plugins = lazy(() => import('./pages/Plugins'));
const Config = lazy(() => import('./pages/Config'));
const SettingsPage = lazy(() => import('./pages/Settings'));
const Workbench = lazy(() => import('../workbench/Workbench'));

/** Keep the workbench mounted across stage changes; animate only real page swaps. */
function routeGroup(pathname: string): string {
  if (pathname.startsWith('/workbench/')) return `/workbench/${pathname.split('/')[2] ?? ''}`;
  return pathname;
}

function WorkbenchRedirect() {
  const { projectId } = useParams();
  return <Navigate to={`/workbench/${projectId}/${DEFAULT_STAGE}`} replace />;
}

/** Bare /workbench: jump back to the last visited workbench, else pick a project. */
function WorkbenchIndexRedirect() {
  const lastProjectId = useLayoutStore((s) => s.lastProjectId);
  const lastStage = useLayoutStore((s) => s.lastStage);
  return (
    <Navigate
      to={lastProjectId ? `/workbench/${lastProjectId}/${lastStage}` : '/projects'}
      replace
    />
  );
}

function PageFallback() {
  return (
    <div className="grid h-full place-items-center">
      <Spinner size={22} />
    </div>
  );
}

export function AppShell() {
  const location = useLocation();

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        const { commandOpen, setCommandOpen } = useShellStore.getState();
        setCommandOpen(!commandOpen);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  return (
    <div className="flex h-screen w-screen overflow-hidden">
      <NavRail />
      <div className="relative flex-1 overflow-hidden">
        <RouteProgress />
        <AnimatePresence mode="wait" initial={false}>
          <motion.main
            key={routeGroup(location.pathname)}
            className="h-full overflow-hidden"
            initial={fadeSlide.initial}
            animate={fadeSlide.animate}
            exit={fadeSlide.exit}
            transition={fadeSlide.transition}
          >
            <Suspense fallback={<PageFallback />}>
              <Routes location={location}>
                <Route path="/" element={<Dashboard />} />
                <Route path="/projects" element={<Projects />} />
                <Route path="/workbench/:projectId/:stage" element={<Workbench />} />
                <Route path="/workbench/:projectId" element={<WorkbenchRedirect />} />
                <Route path="/workbench" element={<WorkbenchIndexRedirect />} />
                <Route path="/flows" element={<Flows />} />
                <Route path="/agents" element={<Agents />} />
                <Route path="/plugins" element={<Plugins />} />
                <Route path="/config" element={<Config />} />
                <Route path="/config/:section" element={<Config />} />
                <Route path="/settings" element={<SettingsPage />} />
                <Route path="*" element={<Navigate to="/" replace />} />
              </Routes>
            </Suspense>
          </motion.main>
        </AnimatePresence>
      </div>
      <CommandPalette />
    </div>
  );
}
