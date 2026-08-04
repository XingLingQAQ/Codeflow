import { useParams } from 'react-router-dom';
import { motion } from 'motion/react';
import { GitFork, ListChecks } from 'lucide-react';
import { Card, Button, EmptyState, Tooltip } from '../ui';
import { ArtifactCard } from './ArtifactCard';
import { useProjectFlow } from '../lib/queries';
import { staggerContainer, staggerItem } from '../lib/motion';

const COLS = [
  { label: 'P0 — 关键', tone: 'text-danger' },
  { label: 'P1 — 重要', tone: 'text-warn' },
  { label: 'P2 — 可选', tone: 'text-ink-dim' },
] as const;

export default function PlanningCanvas() {
  const { projectId = '' } = useParams<{ projectId: string }>();
  const { flow } = useProjectFlow(projectId);
  const planningStage = flow?.stages.find((s) => s.type === 'planning');
  const artifacts = (flow?.artifacts ?? []).filter((a) => a.stage_id === planningStage?.id);

  return (
    <motion.div variants={staggerContainer} initial="initial" animate="animate" className="flex h-full flex-col p-5">
      <div className="mb-4 flex items-center justify-between">
        <h2 className="font-display-13 text-ink">任务看板</h2>
        <Tooltip content="任务依赖图将随看板一起在 M4 交付" side="left">
          <span>
            <Button variant="ghost" size="sm" disabled>
              <GitFork size={14} /> 依赖图
            </Button>
          </span>
        </Tooltip>
      </div>

      {artifacts.length > 0 && (
        <motion.div variants={staggerItem} className="mb-4 space-y-2">
          {artifacts.map((a) => (
            <ArtifactCard key={a.id} artifact={a} />
          ))}
        </motion.div>
      )}

      <motion.div variants={staggerItem} className="relative min-h-0 flex-1">
        <div className="grid h-full grid-cols-3 gap-4">
          {COLS.map((col) => (
            <div key={col.label} className="flex flex-col">
              <div className="mb-3 flex items-center gap-2">
                <span className={`font-display-13 ${col.tone}`}>{col.label}</span>
              </div>
              <Card className="flex-1 border-dashed bg-transparent" />
            </div>
          ))}
        </div>
        <div className="absolute inset-0 grid place-items-center">
          <EmptyState
            icon={<ListChecks size={20} />}
            title="看板任务即将到来"
            description="任务卡片来自 plan.md 的结构化分解（M4）。当前阶段的规划文档可在上方产物卡片查看。"
          />
        </div>
      </motion.div>
    </motion.div>
  );
}
