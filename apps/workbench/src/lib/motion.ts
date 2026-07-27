/**
 * Shared motion language: one easing, three durations (frontend-experience §2).
 * Consumers pass these straight into motion/react `transition`/`variants` props.
 */

export const EASE_FLOW = [0.32, 0.72, 0, 1] as const;

export const DUR = {
  fast: 0.15,
  base: 0.25,
  slow: 0.4,
} as const;

export const springSoft = {
  type: 'spring' as const,
  stiffness: 420,
  damping: 34,
  mass: 0.9,
};

/** Route / canvas fade+slide entrance. */
export const fadeSlide = {
  initial: { opacity: 0, y: 8 },
  animate: { opacity: 1, y: 0 },
  exit: { opacity: 0, y: -8 },
  transition: { duration: DUR.base, ease: EASE_FLOW },
};

/** Stage-canvas morph: outgoing shrinks/fades, incoming rises. */
export const canvasMorph = {
  initial: { opacity: 0, y: 12, scale: 0.99 },
  animate: { opacity: 1, y: 0, scale: 1 },
  exit: { opacity: 0, y: -6, scale: 0.98 },
  transition: { duration: DUR.slow, ease: EASE_FLOW },
};

/** Staggered list container + item pair (20ms/item per spec). */
export const staggerContainer = {
  animate: { transition: { staggerChildren: 0.02 } },
};

export const staggerItem = {
  initial: { opacity: 0, y: 6 },
  animate: { opacity: 1, y: 0, transition: { duration: DUR.base, ease: EASE_FLOW } },
};
