import { useEffect, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { motion } from 'motion/react';
import { Check } from 'lucide-react';
import { STAGES } from '../stages/stageMeta';
import type { Flow, Stage, StageStatus } from '../services-bridge/flows';
import { Tooltip } from '../ui/Tooltip';
import { GateApprovalDialog } from './StageActions';
import { cn } from '../lib/cn';
import { EASE_FLOW } from '../lib/motion';

interface FlowProgressProps {
  projectId: string;
  flow?: Flow;
  stages?: Stage[];
  className?: string;
}

const nodeBase = 'relative z-10 grid place-items-center rounded-full font-display text-13 font-bold';

const nodeStatus: Record<StageStatus, string> = {
  pending: 'border border-line bg-raised text-ink-mute',
  active: 'ring-active border-2 border-ink bg-surface text-ink',
  waiting_gate: 'ring-2 ring-warn/35 border-2 border-warn bg-warn-soft text-warn',
  done: 'bg-[image:var(--grad-signature)] text-white shadow-[0_1px_4px_oklch(0.80_0.11_205/0.40)]',
  skipped: 'bg-tint text-ink-mute line-through',
};

function segClass(prev: StageStatus | undefined, cur: StageStatus): string {
  if (prev === 'done' || prev === 'skipped') {
    if (cur === 'done' || cur === 'skipped') return 'flow-sheen';
    return 'bg-[image:var(--grad-signature)] opacity-60';
  }
  return 'bg-line';
}

export function FlowProgress({ projectId, flow, stages, className }: FlowProgressProps) {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const resolvedStages = flow?.stages ?? stages ?? [];
  const byType = new Map(resolvedStages.map((s) => [s.type, s]));
  const [gateStage, setGateStage] = useState<Stage | null>(null);

  // Deep link (?gate=1, e.g. from the dashboard's waiting-gates card): open
  // the approval dialog for the flow's waiting stage once the flow is loaded.
  const waitingStageId = resolvedStages.find((s) => s.status === 'waiting_gate')?.id;
  useEffect(() => {
    if (searchParams.get('gate') !== '1' || !waitingStageId) return;
    const waiting = resolvedStages.find((s) => s.id === waitingStageId);
    if (!waiting) return;
    setGateStage(waiting);
    const next = new URLSearchParams(searchParams);
    next.delete('gate');
    setSearchParams(next, { replace: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchParams, waitingStageId]);

  return (
    <div className={cn('flex items-center gap-0', className)}>
      {STAGES.map((meta, i) => {
        const st = byType.get(meta.type);
        const status: StageStatus = st?.status ?? 'pending';
        const prevMeta = STAGES[i - 1];
        const prevStatus: StageStatus | undefined = prevMeta ? (byType.get(prevMeta.type)?.status ?? 'pending') : undefined;
        const artifacts = st ? (flow?.artifacts ?? []).filter((a) => a.stage_id === st.id) : [];

        return (
          <div key={meta.type} className="flex items-center">
            {i > 0 && (
              <div className={cn('h-[3px] w-8 rounded-full transition-colors lg:w-12', segClass(prevStatus!, status))} />
            )}
            <Tooltip
              side="bottom"
              content={
                <span className="block max-w-56">
                  <span className="block">
                    {meta.index}. {meta.label} — {meta.en}
                  </span>
                  {status === 'waiting_gate' && (
                    <span className="mt-0.5 block text-warn">等待 Gate 审批 — 点击处理</span>
                  )}
                  {artifacts.length > 0 && (
                    <span className="mt-1 block border-t border-line pt-1 text-ink-mute">
                      {artifacts.map((a) => (
                        <span key={a.id} className="block truncate font-mono text-2xs">
                          {a.type} · v{a.version} · {a.status}
                        </span>
                      ))}
                    </span>
                  )}
                </span>
              }
            >
              <motion.button
                whileHover={{ scale: 1.12 }}
                whileTap={{ scale: 0.94 }}
                transition={{ ease: EASE_FLOW, duration: 0.2 }}
                onClick={() => {
                  navigate(`/workbench/${projectId}/${meta.type}`);
                  if (status === 'waiting_gate' && st) setGateStage(st);
                }}
                data-testid="flow-progress-node"
                data-stage={meta.type}
                data-state={status}
                aria-label={`${meta.label} (${status})`}
                className={cn(
                  nodeBase,
                  status === 'active' || status === 'waiting_gate' ? 'size-9 lg:size-10' : 'size-8 lg:size-9',
                  nodeStatus[status],
                )}
              >
                {status === 'done' ? (
                  <motion.span initial={{ scale: 0 }} animate={{ scale: 1 }} transition={{ type: 'spring', stiffness: 500, damping: 25 }}>
                    <Check size={17} strokeWidth={3.5} />
                  </motion.span>
                ) : (
                  meta.index
                )}
              </motion.button>
            </Tooltip>
          </div>
        );
      })}

      <GateApprovalDialog
        projectId={projectId}
        flow={flow}
        stage={gateStage ?? undefined}
        open={gateStage != null}
        onOpenChange={(open) => {
          if (!open) setGateStage(null);
        }}
      />
    </div>
  );
}
