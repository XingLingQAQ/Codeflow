import { motion } from 'motion/react';
import { cn } from '../lib/cn';
import { EASE_FLOW } from '../lib/motion';

interface LogoMarkProps {
  size?: number;
  /** Play the stroke-draw entrance (startup splash). */
  draw?: boolean;
  className?: string;
}

/** Signature flow glyph: an S-curve threading two nodes, gradient stroke. */
export function LogoMark({ size = 28, draw = false, className }: LogoMarkProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 32 32"
      fill="none"
      className={cn('block', className)}
      aria-hidden
    >
      <defs>
        <linearGradient id="cf-logo-grad" x1="4" y1="4" x2="28" y2="28" gradientUnits="userSpaceOnUse">
          <stop stopColor="#7C5CFF" />
          <stop offset="1" stopColor="#22D3EE" />
        </linearGradient>
      </defs>
      <motion.path
        d="M8.5 10.5C8.5 6.9 23.5 6.6 23.5 12.4C23.5 18.2 8.5 17.6 8.5 23.4C8.5 27.4 23.5 27.2 23.5 23.4"
        stroke="url(#cf-logo-grad)"
        strokeWidth="2.6"
        strokeLinecap="round"
        initial={draw ? { pathLength: 0, opacity: 0 } : false}
        animate={draw ? { pathLength: 1, opacity: 1 } : undefined}
        transition={{ duration: 1.1, ease: EASE_FLOW }}
      />
      <motion.circle
        cx="8.5"
        cy="10.5"
        r="2.7"
        fill="#7C5CFF"
        initial={draw ? { scale: 0 } : false}
        animate={draw ? { scale: 1 } : undefined}
        transition={{ delay: 0.15, duration: 0.4, ease: EASE_FLOW }}
        style={{ transformOrigin: '8.5px 10.5px' }}
      />
      <motion.circle
        cx="23.5"
        cy="23.4"
        r="2.7"
        fill="#22D3EE"
        initial={draw ? { scale: 0 } : false}
        animate={draw ? { scale: 1 } : undefined}
        transition={{ delay: 0.9, duration: 0.4, ease: EASE_FLOW }}
        style={{ transformOrigin: '23.5px 23.4px' }}
      />
    </svg>
  );
}

/** Nav-rail logo button: rounded tile, mark rotates on hover. */
export function LogoTile({ size = 40, onClick }: { size?: number; onClick?: () => void }) {
  return (
    <motion.button
      onClick={onClick}
      whileHover={{ scale: 1.04 }}
      whileTap={{ scale: 0.96 }}
      className="grid place-items-center rounded-xl border border-line bg-raised"
      style={{ width: size, height: size }}
      aria-label="CodeFlow"
    >
      <motion.span whileHover={{ rotate: 12 }} transition={{ ease: EASE_FLOW, duration: 0.4 }}>
        <LogoMark size={size * 0.62} />
      </motion.span>
    </motion.button>
  );
}

export function Wordmark({ className }: { className?: string }) {
  return (
    <span className={cn('font-display text-[15px] font-bold tracking-tight text-ink', className)}>
      Code<span className="text-gradient">Flow</span>
    </span>
  );
}
