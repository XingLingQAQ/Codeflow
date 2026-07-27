import { STAGES, stageTone } from './stageMeta';
import type { Stage } from '../services-bridge/flows';
import { cn } from '../lib/cn';

const toneDot: Record<'neutral' | 'accent' | 'success' | 'warn', string> = {
  neutral: 'bg-ink-mute/60',
  accent: 'bg-ink',
  success: 'bg-ink',
  warn: 'bg-ink-mute',
};

/** Compact seven-node flow indicator for cards and lists. */
export function MiniFlow({ stages, className }: { stages?: Stage[]; className?: string }) {
  const byType = new Map((stages ?? []).map((s) => [s.type, s]));
  return (
    <div className={cn('flex items-center', className)}>
      {STAGES.map((meta, i) => {
        const st = byType.get(meta.type);
        const tone = st ? stageTone(st.status) : 'neutral';
        const done = st?.status === 'done' || st?.status === 'skipped';
        return (
          <div key={meta.type} className="flex items-center">
            <span
              className={cn(
                'size-2 rounded-full transition-colors',
                toneDot[tone],
                st?.status === 'active' && 'ring-2 ring-ink/20',
              )}
            />
            {i < STAGES.length - 1 && <span className={cn('h-px w-3', done ? 'bg-success/50' : 'bg-line')} />}
          </div>
        );
      })}
    </div>
  );
}
