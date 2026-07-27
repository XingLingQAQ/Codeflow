import { useState, useEffect, useCallback, type ReactNode } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { Check, X, RefreshCw, WifiOff, FlaskConical } from 'lucide-react';
import { fetchReadiness, type Readiness } from '../services-bridge/readiness';
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

export function StartupGate({ children }: { children: ReactNode }) {
  const [phase, setPhase] = useState<Phase>('checking');
  const [stepsDone, setStepsDone] = useState<Record<string, boolean | 'fail'>>({});
  const [attempt, setAttempt] = useState(0);
  const setOffline = useShellStore((s) => s.setOffline);

  const poll = useCallback(async () => {
    const r = await fetchReadiness();
    const next: Record<string, boolean | 'fail'> = {};
    let allDone = true;
    for (const step of STEPS) {
      const ok = step.check(r);
      next[step.key] = ok;
      if (!ok) allDone = false;
    }
    setStepsDone(next);

    if (allDone) {
      setPhase('ready');
      return true;
    }
    return false;
  }, []);

  useEffect(() => {
    if (phase !== 'checking') return;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    const run = async () => {
      if (cancelled) return;
      const done = await poll();
      if (done || cancelled) return;
      setAttempt((a) => {
        const next = a + 1;
        if (next >= MAX_ATTEMPTS) {
          setPhase('failed');
          return next;
        }
        timer = setTimeout(run, 1500);
        return next;
      });
    };
    run();
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [phase, poll]);

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

  return (
    <>
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
                <p className="text-[13px] text-danger">连接后端失败</p>
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
    </>
  );
}
