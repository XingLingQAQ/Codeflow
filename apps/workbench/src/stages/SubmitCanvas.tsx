import { motion } from 'motion/react';
import { GitCommitHorizontal, Camera, FileArchive } from 'lucide-react';
import { Card, CardBody, Button, Badge, Skeleton, SkeletonText } from '../ui';
import { staggerContainer, staggerItem } from '../lib/motion';

export default function SubmitCanvas() {
  return (
    <motion.div variants={staggerContainer} initial="initial" animate="animate" className="flex h-full gap-5 p-5">
      <motion.div variants={staggerItem} className="flex min-w-0 flex-1 flex-col gap-4">
        <h2 className="font-display text-lg font-bold text-ink">提交分组</h2>
        {[1, 2].map((g) => (
          <Card key={g} className="p-4">
            <div className="flex items-center gap-2">
              <GitCommitHorizontal size={15} className="text-accent" />
              <Skeleton className="h-4 w-3/5" />
              <Badge>{g === 1 ? '3 files' : '2 files'}</Badge>
            </div>
            <div className="mt-3 space-y-1.5 pl-6">
              {Array.from({ length: g === 1 ? 3 : 2 }).map((_, i) => (
                <Skeleton key={i} className="h-3 w-4/5" />
              ))}
            </div>
            <div className="mt-4 flex gap-2 pl-6">
              <Button variant="secondary" size="sm">编辑消息</Button>
              <Button variant="primary" size="sm">提交</Button>
            </div>
          </Card>
        ))}
      </motion.div>

      <motion.div variants={staggerItem} className="flex w-72 shrink-0 flex-col gap-4">
        <Card className="p-4">
          <div className="flex items-center gap-2 text-[12px] font-medium text-ink-dim">
            <Camera size={14} /> 快照绑定
          </div>
          <CardBody className="mt-2 text-center text-[12px] text-ink-mute">
            <Badge tone="neutral">尚未绑定</Badge>
            <p className="mt-2">阶段快照将在 advance 时自动创建</p>
          </CardBody>
        </Card>

        <Card className="flex-1 p-4">
          <div className="flex items-center gap-2 text-[12px] font-medium text-ink-dim">
            <FileArchive size={14} /> 归档报告预览
          </div>
          <CardBody className="mt-2">
            <SkeletonText lines={6} />
          </CardBody>
        </Card>
      </motion.div>
    </motion.div>
  );
}
