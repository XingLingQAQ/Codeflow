import { useEffect, useState } from 'react';
import { useLocation } from 'react-router-dom';
import { AnimatePresence, motion } from 'motion/react';
import { EASE_FLOW } from '../lib/motion';

/** Thin gradient progress line at the very top during route/lazy transitions. */
export function RouteProgress() {
  const location = useLocation();
  const [active, setActive] = useState(false);

  useEffect(() => {
    setActive(true);
    const t = setTimeout(() => setActive(false), 520);
    return () => clearTimeout(t);
  }, [location.pathname]);

  return (
    <AnimatePresence>
      {active && (
        <motion.div
          className="pointer-events-none absolute left-0 top-0 z-[70] h-0.5 flow-sheen"
          initial={{ width: '0%', opacity: 1 }}
          animate={{ width: '100%' }}
          exit={{ opacity: 0 }}
          transition={{ duration: 0.52, ease: EASE_FLOW }}
        />
      )}
    </AnimatePresence>
  );
}
