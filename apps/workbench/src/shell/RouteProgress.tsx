import { useEffect, useState } from 'react';
import { useLocation } from 'react-router-dom';
import { useIsFetching } from '@tanstack/react-query';
import { AnimatePresence, motion } from 'motion/react';
import { EASE_FLOW } from '../lib/motion';

type Phase = 'idle' | 'run' | 'done';

/**
 * Thin progress line at the very top. A route change starts it; it holds at
 * 90% while React Query has in-flight requests and completes when the count
 * returns to zero — no fixed fake timer.
 */
export function RouteProgress() {
  const location = useLocation();
  const fetching = useIsFetching();
  const [phase, setPhase] = useState<Phase>('idle');

  useEffect(() => {
    setPhase('run');
  }, [location.pathname]);

  useEffect(() => {
    if (phase !== 'run' || fetching > 0) return;
    // Small grace period so back-to-back query chains don't flicker-complete.
    const t = setTimeout(() => setPhase('done'), 120);
    return () => clearTimeout(t);
  }, [phase, fetching]);

  return (
    <AnimatePresence>
      {phase !== 'idle' && (
        <motion.div
          key="route-progress"
          className="pointer-events-none absolute left-0 top-0 z-[70] h-0.5 bg-ink"
          initial={{ width: '0%', opacity: 1 }}
          animate={{ width: phase === 'run' ? '90%' : '100%' }}
          exit={{ opacity: 0 }}
          transition={{ duration: phase === 'run' ? 0.4 : 0.15, ease: EASE_FLOW }}
          onAnimationComplete={() => {
            if (phase === 'done') setPhase('idle');
          }}
        />
      )}
    </AnimatePresence>
  );
}
