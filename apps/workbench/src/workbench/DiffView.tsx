import { useMemo } from 'react';
import { diffLines, type DiffOp } from '../lib/diff';
import { cn } from '../lib/cn';

export interface DiffViewProps {
  /** Old (working tree) content. */
  base: string;
  /** New (staged / current) content. */
  current: string;
  /** Precomputed ops override (skip recompute when caller already has them). */
  ops?: DiffOp[];
  className?: string;
}

/** Unified line diff renderer used by the staging dialog + review canvas. */
export function DiffView({ base, current, ops: opsProp, className }: DiffViewProps) {
  const ops = useMemo(() => opsProp ?? diffLines(base, current), [opsProp, base, current]);

  if (ops.length === 0) {
    return <p className={cn('px-3 py-4 text-[12px] text-ink-mute', className)}>无差异</p>;
  }

  return (
    <div className={cn('min-w-max font-mono text-[12px] leading-5', className)}>
      {ops.map((op, i) => (
        <div
          key={i}
          className={cn(
            'flex',
            op.type === 'add' && 'bg-success/10',
            op.type === 'del' && 'bg-danger/10',
          )}
        >
          <span className="w-10 shrink-0 select-none pr-1.5 text-right text-ink-mute/70">
            {op.aLine ?? ''}
          </span>
          <span className="w-10 shrink-0 select-none border-r border-line pr-1.5 text-right text-ink-mute/70">
            {op.bLine ?? ''}
          </span>
          <span
            className={cn(
              'w-5 shrink-0 select-none text-center',
              op.type === 'add' && 'text-success',
              op.type === 'del' && 'text-danger',
            )}
          >
            {op.type === 'add' ? '+' : op.type === 'del' ? '-' : ''}
          </span>
          <span className="whitespace-pre pr-4 text-ink-dim">{op.text || ' '}</span>
        </div>
      ))}
    </div>
  );
}

/** Compact +adds / −dels statistic pair. */
export function DiffStat({ adds, dels, className }: { adds: number; dels: number; className?: string }) {
  return (
    <span className={cn('nums inline-flex items-center gap-2 font-mono text-[11px]', className)}>
      <span className="text-success">+{adds}</span>
      <span className="text-danger">-{dels}</span>
    </span>
  );
}
