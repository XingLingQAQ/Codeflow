import { useParams } from 'react-router-dom';
import { motion } from 'motion/react';
import { FileText, GitFork, Swords } from 'lucide-react';
import { Card, Button, EmptyState, Tooltip } from '../ui';
import { ArtifactCard } from './ArtifactCard';
import { useProjectFlow } from '../lib/queries';
import { staggerContainer, staggerItem } from '../lib/motion';

export default function DesignCanvas() {
  const { projectId = '' } = useParams<{ projectId: string }>();
  const { flow } = useProjectFlow(projectId);
  const designStage = flow?.stages.find((s) => s.type === 'design');
  const artifacts = (flow?.artifacts ?? []).filter((a) => a.stage_id === designStage?.id);

  return (
    <motion.div variants={staggerContainer} initial="initial" animate="animate" className="flex h-full flex-col">
      <div className="flex min-h-0 flex-1 gap-5 p-5">
        <motion.div variants={staggerItem} className="flex min-w-0 flex-[3] flex-col gap-3">
          <h3 className="flex items-center gap-2 text-[13px] font-semibold text-ink-dim">
            <FileText size={14} /> 阶段产物
          </h3>
          {artifacts.length > 0 ? (
            <div className="space-y-2.5">
              {artifacts.map((a) => (
                <ArtifactCard key={a.id} artifact={a} />
              ))}
            </div>
          ) : (
            <p className="text-[12px] leading-relaxed text-ink-mute">
              设计阶段还没有产出 design.md — 产物由 Agent 起草后在此列出。
            </p>
          )}
          <Card className="flex flex-1 items-center justify-center">
            <EmptyState
              icon={<FileText size={20} />}
              title="文档编辑器即将到来"
              description="设计文档的富文本编辑与版本对比将在 M4 交付；当前可通过产物卡片查看已归档的设计文档。"
            />
          </Card>
        </motion.div>

        <motion.div variants={staggerItem} className="flex min-w-0 flex-[2] flex-col">
          <Card className="flex flex-1 flex-col overflow-hidden">
            <div className="flex items-center gap-2 border-b border-line px-4 py-2.5">
              <GitFork size={14} className="text-ink-mute" />
              <span className="text-[12px] font-medium text-ink-dim">架构图</span>
            </div>
            <div className="flex flex-1 items-center justify-center">
              <EmptyState
                icon={<GitFork size={20} />}
                title="Mermaid 渲染即将到来"
                description="架构图的 Mermaid 渲染与节点联动将在 M4 交付。"
              />
            </div>
          </Card>
        </motion.div>
      </div>

      <div className="flex shrink-0 items-center gap-3 border-t border-line px-5 py-3">
        <Tooltip content="多方辩论（/api/v1/debates 接入）将在 M4 交付" side="top">
          <span>
            <Button variant="secondary" disabled>
              <Swords size={15} /> 发起辩论
            </Button>
          </span>
        </Tooltip>
        <span className="text-[11px] leading-relaxed text-ink-mute">
          设计分歧可升级为多方辩论；Gate 驳回时按 on_fail 策略自动升级。
        </span>
      </div>
    </motion.div>
  );
}
