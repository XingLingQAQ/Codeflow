import { useNavigate, useParams } from 'react-router-dom';
import { motion } from 'motion/react';
import { PanelLeftClose, PanelLeftOpen } from 'lucide-react';
import { STAGES, STAGE_STATUS_LABEL, stageTone } from '../stages/stageMeta';
import type { Stage } from '../services-bridge/flows';
import { useLayoutStore } from '../stores/layout';
import { IconButton, StatusPill, Tooltip } from '../ui';
import { cn } from '../lib/cn';
import { EASE_FLOW } from '../lib/motion';

interface FlowRailProps {
  projectId: string;
  stages?: Stage[];
}

export function FlowRail({ projectId, stages = [] }: FlowRailProps) {
  const navigate = useNavigate();
  const { stage: currentSlug } = useParams();
  const collapsed = useLayoutStore((s) => s.flowRailCollapsed);
  const toggle = useLayoutStore((s) => s.toggleFlowRail);
  const byType = new Map(stages.map((s) => [s.type, s]));

  return (
    <aside className={cn('flex flex-col border-r border-line bg-panel transition-[width] duration-300', collapsed ? 'w-14' : 'w-52')}>
      <div className={cn('flex h-10 shrink-0 items-center border-b border-line px-2', collapsed ? 'justify-center' : 'justify-between pl-3.5')}>
        {!collapsed && <span className="text-[11px] font-semibold uppercase tracking-wide text-ink-mute">阶段</span>}
        <IconButton size="sm" onClick={toggle} aria-label={collapsed ? '展开' : '收起'}>
          {collapsed ? <PanelLeftOpen size={15} /> : <PanelLeftClose size={15} />}
        </IconButton>
      </div>

      <nav className="flex-1 space-y-0.5 overflow-y-auto p-1.5">
        {STAGES.map((meta) => {
          const st = byType.get(meta.type);
          const status = st?.status ?? 'pending';
          const active = meta.type === currentSlug;
          const tone = stageTone(status);
          const Icon = meta.icon;
          const label = `${meta.index}. ${meta.label}`;

          const btn = (
            <button
              key={meta.type}
              onClick={() => navigate(`/workbench/${projectId}/${meta.type}`)}
              className={cn(
                'relative flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left transition-colors',
                active ? 'text-ink' : 'text-ink-dim hover:text-ink hover:bg-tint',
              )}
            >
              {active && (
                <motion.span
                  layoutId="flow-rail-active"
                  transition={{ duration: 0.25, ease: EASE_FLOW }}
                  className="absolute inset-0 rounded-lg bg-tint-active"
                />
              )}
              <span className={cn(
                'relative z-10 grid size-6 shrink-0 place-items-center rounded-md text-[11px] font-bold',
                active ? '[background:var(--btn-primary-bg)] [color:var(--btn-primary-fg)]' : 'bg-tint text-ink-mute',
              )}>
                {meta.index}
              </span>
              {!collapsed && (
                <span className="relative z-10 flex min-w-0 flex-1 items-center justify-between gap-1.5">
                  <span className="truncate text-[13px] font-medium">{meta.label}</span>
                  <StatusPill tone={tone} pulse={status === 'active'} label={STAGE_STATUS_LABEL[status]} />
                </span>
              )}
            </button>
          );

          return collapsed ? (
            <Tooltip key={meta.type} content={label} side="right">
              {btn}
            </Tooltip>
          ) : (
            btn
          );
        })}
      </nav>
    </aside>
  );
}
