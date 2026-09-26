import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { Check, X, RefreshCw, WifiOff, FlaskConical, ChevronDown, ChevronUp } from 'lucide-react';
import { fetchReadiness, type Readiness } from '../services-bridge/readiness';
import { buildReadinessView, type ReadinessView } from './readinessView';
import { LogoMark } from '../shell/Logo';
import { Button, Spinner } from '../ui';
import { useShellStore } from '../stores/shell';
import { EASE_FLOW } from '../lib/motion';

type Phase = 'checking' | 'ready' | 'failed' | 'offline';

interface Step {
  key: string;
  label: string;
  check: (r: Readiness) => boolean;
}

const STEPS: Step[] = [
  { key: 'backend', label: '后端进程', check: (r) => r.reachable },
  { key: 'database', label: '数据库', check: (r) => !!r.components.project?.ready },
  { key: 'index', label: '索引', check: (r) => !!r.components.context?.ready },
  { key: 'ws', label: 'WebSocket', check: (r) => r.status === 'ready' },
];

const MAX_ATTEMPTS = 20;
/** Interval between attempts while the read-only surface is still unusable. */
const RETRY_MS = 1500;
/** Background re-check while the gate is open but a capability is missing. */
const CAPABILITY_REFRESH_MS = 15000;

/** The initial value: no check has run yet, so nothing is known. */
const UNKNOWN_VIEW = buildReadinessView(null);

interface ReadinessContextValue {
  view: ReadinessView;
  phase: Phase;
  /** Re-run the check now (used by the strip's 重新检查 and by run creation). */
  refresh: () => Promise<ReadinessView>;
}

const ReadinessContext = createContext<ReadinessContextValue>({
  view: UNKNOWN_VIEW,
  phase: 'checking',
  refresh: async () => UNKNOWN_VIEW,
});

/**
 * The latest capability view. T1.14 (Run creation UI) reads
 * `view.canCreateRun` / `canCreateRunForBackend(view, backend)` from here
 * instead of inventing its own readiness probe, so the button state and the
 * backend's pre-create check answer the same question.
 */
export function useReadinessView(): ReadinessView {
  return useContext(ReadinessContext).view;
}

/** Re-run the readiness check; resolves with the fresh view. */
export function useReadinessRefresh(): () => Promise<ReadinessView> {
  return useContext(ReadinessContext).refresh;
}

/** Coarse gate phase, for consumers that must not act while checking. */
export function useReadinessPhase(): Phase {
  return useContext(ReadinessContext).phase;
}

/**
 * True when the gate has released the shell and `view` is populated by a real
 * check (false while checking/failed/offline).
 */
function isCapabilityKnown(readiness: Readiness): boolean {
  return readiness.capabilities != null;
}

export function StartupGate({ children }: { children: ReactNode }) {
  const [phase, setPhase] = useState<Phase>('checking');
  const [stepsDone, setStepsDone] = useState<Record<string, boolean | 'fail'>>({});
  const [attempt, setAttempt] = useState(0);
  const [readiness, setReadiness] = useState<Readiness | null>(null);
  const setOffline = useShellStore((s) => s.setOffline);

  const view = useMemo(() => buildReadinessView(readiness), [readiness]);

  const poll = useCallback(async (): Promise<{ usable: boolean; view: ReadinessView }> => {
    const r = await fetchReadiness();
    setReadiness(r);
    const next: Record<string, boolean | 'fail'> = {};
    for (const step of STEPS) next[step.key] = step.check(r);
    setStepsDone(next);
    const nextView = buildReadinessView(r);
    // The gate opens when the read-only surface is usable. execution/merge may
    // be unavailable — that is a "browse only" state, not a failure to start.
    // A backend that does not publish capabilities at all (older build) falls
    // back to its HTTP verdict for browsing only; nothing claims that a Run can
    // be created, because canCreateRun stays false in that case.
    const capabilityUnavailable = nextView.canBrowse;
    const legacyFallback = !isCapabilityKnown(r) && r.reachable && r.status === 'ready';
    return { usable: capabilityUnavailable || legacyFallback, view: nextView };
  }, []);

  const refresh = useCallback(async (): Promise<ReadinessView> => {
    const { view: nextView } = await poll();
    return nextView;
  }, [poll]);

  useEffect(() => {
    if (phase !== 'checking') return;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    const run = async () => {
      if (cancelled) return;
      const { usable } = await poll();
      if (cancelled) return;
      if (usable) {
        setPhase('ready');
        return;
      }
      setAttempt((a) => {
        const next = a + 1;
        if (next >= MAX_ATTEMPTS) {
          setPhase('failed');
          return next;
        }
        timer = setTimeout(run, RETRY_MS);
        return next;
      });
    };
    run();
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [phase, poll]);

  // While the shell is open on a degraded backend, keep re-checking so a
  // repaired dependency updates the strip without a frontend restart (plan
  // section 15 T0.12 step 5). Nothing is cached across checks.
  useEffect(() => {
    if (phase !== 'ready' || view.display === 'ready') return;
    const timer = setInterval(() => {
      void refresh();
    }, CAPABILITY_REFRESH_MS);
    return () => clearInterval(timer);
  }, [phase, view.display, refresh]);

  const retry = () => {
    setAttempt(0);
    setStepsDone({});
    setPhase('checking');
  };

  const goOffline = () => {
    setOffline(true);
    setPhase('offline');
  };

  const goMock = async () => {
    if (import.meta.env.DEV) {
      const { activateMock, showMockNotice } = await import('../mocks');
      activateMock();
      showMockNotice();
    }
    setPhase('ready');
  };

  const show = phase === 'checking' || phase === 'failed';
  // Reachable but read_only is not usable — a dependency failure, not an
  // offline backend. The two must never be conflated (plan section 15 T0.12
  // step 4).
  const backendReachable = readiness?.reachable === true;

  const contextValue = useMemo<ReadinessContextValue>(
    () => ({ view, phase, refresh }),
    [view, phase, refresh],
  );

  return (
    <ReadinessContext.Provider value={contextValue}>
      <AnimatePresence>
        {show && (
          <motion.div
            key="splash"
            className="fixed inset-0 z-[100] flex flex-col items-center justify-center bg-base"
            initial={{ opacity: 1 }}
            exit={{ opacity: 0, scale: 0.97 }}
            transition={{ duration: 0.5, ease: EASE_FLOW }}
          >
            <motion.div initial={{ scale: 0.8, opacity: 0 }} animate={{ scale: 1, opacity: 1 }} transition={{ duration: 0.6, ease: EASE_FLOW }}>
              <LogoMark size={56} draw />
            </motion.div>

            <motion.h1 initial={{ y: 8, opacity: 0 }} animate={{ y: 0, opacity: 1 }} transition={{ delay: 0.3, duration: 0.4, ease: EASE_FLOW }} className="mt-5 font-display text-xl font-bold text-ink">
              Code<span className="text-gradient">Flow</span>
            </motion.h1>

            <motion.div initial={{ y: 12, opacity: 0 }} animate={{ y: 0, opacity: 1 }} transition={{ delay: 0.5, duration: 0.4, ease: EASE_FLOW }} className="mt-8 w-64 space-y-2.5">
              {STEPS.map((step) => {
                const state = stepsDone[step.key];
                return (
                  <div key={step.key} className="flex items-center gap-3">
                    <span className="grid size-5 place-items-center">
                      {state === true ? (
                        <motion.span initial={{ scale: 0 }} animate={{ scale: 1 }} transition={{ type: 'spring', stiffness: 500, damping: 25 }}>
                          <Check size={14} className="text-success" />
                        </motion.span>
                      ) : state === 'fail' ? (
                        <X size={14} className="text-danger" />
                      ) : (
                        <Spinner size={14} />
                      )}
                    </span>
                    <span className="text-[13px] text-ink-dim">{step.label}</span>
                  </div>
                );
              })}
            </motion.div>

            {phase === 'failed' && (
              <motion.div initial={{ y: 10, opacity: 0 }} animate={{ y: 0, opacity: 1 }} transition={{ duration: 0.3, ease: EASE_FLOW }} className="mt-8 flex flex-col items-center gap-3">
                {/* Only an unreachable backend is a connection failure; a
                    reachable backend whose read-only dependencies are not
                    ready keeps its own message and reasons. */}
                <p className="text-[13px] text-danger">
                  {backendReachable ? '只读依赖未就绪，暂不能进入' : '连接后端失败'}
                </p>
                {backendReachable && view.blockers.length > 0 && (
                  <ul className="max-w-md space-y-1 text-center text-[12px] text-ink-dim">
                    {view.blockers.slice(0, 4).map((blocker) => (
                      <li key={`${blocker.component}-${blocker.errorCode}-${blocker.remediation}`}>
                        {blocker.message}（{blocker.component}）
                      </li>
                    ))}
                  </ul>
                )}
                <div className="flex gap-2">
                  <Button variant="secondary" onClick={retry}>
                    <RefreshCw size={14} /> 重试
                  </Button>
                  {import.meta.env.DEV && (
                    <Button variant="primary" onClick={goMock}>
                      <FlaskConical size={14} /> 使用模拟数据
                    </Button>
                  )}
                  <Button variant="ghost" onClick={goOffline}>
                    <WifiOff size={14} /> 以离线模式继续
                  </Button>
                </div>
              </motion.div>
            )}
          </motion.div>
        )}
      </AnimatePresence>

      {(phase === 'ready' || phase === 'offline') && children}
      {phase === 'ready' && <CapabilityStrip view={view} onRefresh={refresh} />}
    </ReadinessContext.Provider>
  );
}

interface CapabilityItem {
  key: 'browse' | 'run' | 'merge';
  label: string;
  ok: boolean;
}

function itemsFor(view: ReadinessView): CapabilityItem[] {
  return [
    { key: 'browse', label: '可浏览', ok: view.canBrowse },
    { key: 'run', label: '可创建 Run', ok: view.canCreateRun },
    { key: 'merge', label: '可合入', ok: view.canMerge },
  ];
}

/**
 * Non-intrusive capability strip (plan section 15 T0.12 step 4): shows the
 * three capability states after the shell is open, without covering the
 * workspace. Collapsed when everything is usable; expands to the blocking
 * dependencies and their remediation when anything is not.
 */
function CapabilityStrip({
  view,
  onRefresh,
}: {
  view: ReadinessView;
  onRefresh: () => Promise<ReadinessView>;
}) {
  const [open, setOpen] = useState(false);
  const [checking, setChecking] = useState(false);
  const items = itemsFor(view);
  const degraded = items.some((item) => !item.ok) || view.display === 'unknown';

  const recheck = async () => {
    setChecking(true);
    try {
      await onRefresh();
    } finally {
      setChecking(false);
    }
  };

  return (
    <div
      className="fixed bottom-4 left-1/2 z-40 -translate-x-1/2 rounded-xl border border-line bg-raised/95 px-3 py-2 shadow-lg backdrop-blur"
      data-capability-strip={view.display}
    >
      <div className="flex items-center gap-3 text-[12px]">
        <span className="font-medium text-ink-dim">{view.label}</span>
        {items.map((item) => (
          <span
            key={item.key}
            data-capability={item.key}
            data-state={item.ok ? 'ready' : 'unavailable'}
            className="flex items-center gap-1 text-ink-dim"
          >
            {item.ok ? (
              <Check size={12} className="text-success" />
            ) : (
              <X size={12} className="text-danger" />
            )}
            {item.label}
            <span className="text-ink-mute">{item.ok ? '可用' : '不可用'}</span>
          </span>
        ))}
        {degraded && (
          <button
            type="button"
            onClick={() => setOpen((v) => !v)}
            className="flex items-center gap-1 rounded-md px-1.5 py-0.5 text-ink-dim transition-colors hover:bg-tint-hover hover:text-ink"
            aria-expanded={open}
          >
            {open ? <ChevronDown size={12} /> : <ChevronUp size={12} />}
            原因
          </button>
        )}
        <button
          type="button"
          onClick={recheck}
          disabled={checking}
          className="flex items-center gap-1 rounded-md px-1.5 py-0.5 text-ink-dim transition-colors hover:bg-tint-hover hover:text-ink disabled:opacity-50"
        >
          <RefreshCw size={12} className={checking ? 'animate-spin' : undefined} />
          重新检查
        </button>
      </div>

      {open && degraded && (
        <div className="mt-2 max-w-xl space-y-1.5 border-t border-line pt-2">
          {view.unknownReason && (
            <p className="text-[12px] text-ink-dim" data-capability-reason="unknown">
              {view.unknownReason}
            </p>
          )}
          {view.blockers.length === 0 && !view.unknownReason && (
            <p className="text-[12px] text-ink-dim">能力集合未说明阻塞原因</p>
          )}
          {view.blockers.map((blocker) => (
            <div
              key={`${blocker.component}-${blocker.errorCode}-${blocker.remediation}`}
              className="text-[12px] text-ink-dim"
            >
              <p className="text-ink">{blocker.message}</p>
              <p className="text-ink-mute">
                {blocker.component} · 建议：{blocker.action}
              </p>
            </div>
          ))}
          {Object.entries(view.backends).map(([name, backend]) =>
            backend.state === 'ready' ? null : (
              <div
                key={`backend-${name}`}
                data-capability-backend={name}
                data-state="unavailable"
                className="text-[12px] text-ink-dim"
              >
                {backend.blockers.length > 0 ? (
                  backend.blockers.map((blocker) => (
                    <div key={`${name}-${blocker.component}-${blocker.errorCode}`}>
                      <p className="text-ink">{blocker.message}</p>
                      <p className="text-ink-mute">
                        后端 {name} · {blocker.component} · 建议：{blocker.action}
                      </p>
                    </div>
                  ))
                ) : (
                  <p className="text-ink-mute">后端 {name} 不可用（未说明原因）</p>
                )}
              </div>
            ),
          )}
        </div>
      )}
    </div>
  );
}
