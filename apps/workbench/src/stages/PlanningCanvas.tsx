import { motion } from 'motion/react';
import { GripVertical, GitFork } from 'lucide-react';
import { Card, Button, Skeleton, Badge } from '../ui';
import { staggerContainer, staggerItem } from '../lib/motion';

const COLS = [
  { label: 'P0 — 关键', count: 3, tone: 'text-danger' },
  { label: 'P1 — 重要', count: 2, tone: 'text-warn' },
  { label: 'P2 — 可选', count: 2, tone: 'text-ink-dim' },
] as const;

function TaskCard() {
  return (
    <Card className="group flex items-start gap-2 p-3">
      <GripVertical size={14} className="mt-0.5 shrink-0 text-ink-mute/40 transition-colors group-hover:text-ink-mute" />
      <div className="min-w-0 flex-1">
        <Skeleton className="h-3.5 w-4/5" />
        <div className="mt-2 flex items-center gap-2">
          <span className="inline-block size-2 rounded-full bg-ink-mute/30" />
          <Skeleton className="h-2.5 w-16" />
        </div>
      </div>
    </Card>
  );
}

export default function PlanningCanvas() {
  return (
    <motion.div variants={staggerContainer} initial="initial" animate="animate" className="flex h-full flex-col p-5">
      <div className="mb-4 flex items-center justify-between">
        <h2 className="font-display text-lg font-bold text-ink">任务看板</h2>
        <Button variant="ghost" size="sm"><GitFork size={14} /> 依赖图</Button>
      </div>
      <div className="grid min-h-0 flex-1 grid-cols-3 gap-4">
        {COLS.map((col) => (
          <motion.div key={col.label} variants={staggerItem} className="flex flex-col">
            <div className="mb-3 flex items-center gap-2">
              <span className={`text-[13px] font-semibold ${col.tone}`}>{col.label}</span>
              <Badge>{col.count}</Badge>
            </div>
            <div className="flex-1 space-y-2.5 overflow-auto">
              {Array.from({ length: col.count }).map((_, i) => (
                <TaskCard key={i} />
              ))}
            </div>
          </motion.div>
        ))}
      </div>
    </motion.div>
  );
}
