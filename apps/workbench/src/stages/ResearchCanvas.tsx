import { useParams } from 'react-router-dom';
import { motion } from 'motion/react';
import { toast } from 'sonner';
import { CircleSlash, Microscope } from 'lucide-react';
import { Button, EmptyState, Tooltip } from '../ui';
import { useProjectFlow, useSkipStage } from '../lib/queries';
import { staggerContainer, staggerItem } from '../lib/motion';

export default function ResearchCanvas() {
  const { projectId = '' } = useParams<{ projectId: string }>();
  const { flow } = useProjectFlow(projectId);
  const researchStage = flow?.stages.find((s) => s.type === 'research');
  const skipMut = useSkipStage(projectId);

  const canSkip =
    flow?.status === 'active' &&
    !!researchStage?.optional &&
    (researchStage.status === 'pending' || researchStage.status === 'active');

  const skipHint = !researchStage
    ? '当前工作流不包含调研阶段'
    : researchStage.status === 'skipped'
      ? '调研阶段已跳过'
      : researchStage.status === 'done'
        ? '调研阶段已完成'
        : !researchStage.optional
          ? '该工作流中调研不是可选阶段'
          : undefined;

  const skip = () => {
    if (!flow || !researchStage || !canSkip) return;
    skipMut.mutate(
      { flowId: flow.id, stageId: researchStage.id },
      {
        onSuccess: () => toast.success('已跳过调研阶段'),
        onError: (err) => toast.error(err instanceof Error ? err.message : String(err)),
      },
    );
  };

  return (
    <motion.div
      variants={staggerContainer}
      initial="initial"
      animate="animate"
      className="grid h-full place-items-center p-5"
    >
      <motion.div variants={staggerItem}>
        <EmptyState
          icon={<Microscope size={22} />}
          title="多源调研即将到来"
          description="检索计划树、结果流与证据篮将在后续里程碑交付：Deep Researcher 按「计划 → 检索 → 取证」三段式沉淀证据。调研是可选阶段，不需要时可以直接跳过。"
          action={
            <Tooltip content={skipHint ?? '跳过可选的调研阶段，直接进入下一阶段'} side="bottom">
              <span>
                <Button
                  variant="secondary"
                  disabled={!canSkip}
                  loading={skipMut.isPending}
                  onClick={skip}
                >
                  <CircleSlash size={14} /> 跳过调研阶段
                </Button>
              </span>
            </Tooltip>
          }
        />
      </motion.div>
    </motion.div>
  );
}
