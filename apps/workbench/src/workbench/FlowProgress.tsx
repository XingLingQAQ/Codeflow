import { useNavigate } from 'react-router-dom';
import { motion } from 'motion/react';
import { Check } from 'lucide-react';
import { STAGES, stageTone, type StageMeta } from '../stages/stageMeta';
import type { Stage, StageStatus } from '../services-bridge/flows';
import { Tooltip } from '../ui/Tooltip';
import { cn } from '../lib/cn';
import { EASE_FLOW } from '../lib/motion';

interface FlowProgressProps {
  projectId: string;
  stages?: Stage[];
  className?: string;
}

const nodeBase = 'relative z-10 grid place-items-center rounded-full font-display text-xs font-bold';

const nodeStatus: Record<StageStatus, string> = {
  pending: 'border border-line bg-raised text-ink-mute',
  active: 'border-2 border-ink bg-panel text-ink',
  waiting_gate: 'border-2 border-warn bg-panel text-warn',
  done: 'bg-[image:var(--grad-signature)] text-white',
  skipped: 'bg-tint text-ink-mute line-through',
};

function segClass(prev: StageStatus | undefined, cur: StageStatus): string {
  if (prev === 'done' || prev === 'skipped') {
    if (cur === 'done' || cur === 'skipped') return 'flow-sheen';
    return 'bg-[image:var(--grad-signature)] opacity-60';
  }
  return 'bg-line';
}

export function FlowProgress({ projectId, stages = [], className }: FlowProgressProps) {
  const navigate = useNavigate();
  const byType = new Map(stages.map((s) => [s.type, s]));

  return (
    <div className={cn('flex items-center gap-0', className)}>
      {STAGES.map((meta, i) => {
        const st = byType.get(meta.type);
        const status: StageStatus = st?.status ?? 'pending';
        const prevMeta = STAGES[i - 1];
        const prevStatus: StageStatus | undefined = prevMeta ? (byType.get(prevMeta.type)?.status ?? 'pending') : undefined;

        return (
          <div key={meta.type} className="flex items-center">
            {i > 0 && <div className={cn('h-0.5 w-8 transition-colors lg:w-12', segClass(prevStatus!, status))} />}
            <Tooltip content={`${meta.index}. ${meta.label} — ${meta.en}`} side="bottom">
              <motion.button
                whileHover={{ scale: 1.12 }}
                whileTap={{ scale: 0.94 }}
                transition={{ ease: EASE_FLOW, duration: 0.2 }}
                onClick={() => navigate(`/workbench/${projectId}/${meta.type}`)}
                data-testid="flow-progress-node"
                data-stage={meta.type}
                data-state={status}
                aria-label={`${meta.label} (${status})`}
                className={cn(nodeBase, 'size-8 lg:size-9', nodeStatus[status])}
              >
                {status === 'done' ? (
                  <motion.span initial={{ scale: 0 }} animate={{ scale: 1 }} transition={{ type: 'spring', stiffness: 500, damping: 25 }}>
                    <Check size={15} strokeWidth={3} />
                  </motion.span>
                ) : (
                  meta.index
                )}
              </motion.button>
            </Tooltip>
          </div>
        );
      })}
    </div>
  );
}
