import { motion } from 'motion/react';
import { FileText, GitFork, Swords } from 'lucide-react';
import { Card, CardBody, Button, Badge, Skeleton, SkeletonText } from '../ui';
import { staggerContainer, staggerItem } from '../lib/motion';

export default function DesignCanvas() {
  return (
    <motion.div variants={staggerContainer} initial="initial" animate="animate" className="flex h-full flex-col">
      <div className="flex min-h-0 flex-1 gap-5 p-5">
        <motion.div variants={staggerItem} className="flex flex-1 flex-col">
          <Card className="flex flex-1 flex-col overflow-hidden">
            <div className="flex items-center gap-2 border-b border-line px-4 py-2.5">
              <FileText size={14} className="text-ink-mute" />
              <span className="text-[12px] font-medium text-ink-dim">文档编辑器</span>
              <div className="ml-auto flex gap-1">
                <Skeleton className="h-5 w-8 rounded" />
                <Skeleton className="h-5 w-8 rounded" />
                <Skeleton className="h-5 w-8 rounded" />
              </div>
            </div>
            <CardBody className="flex-1 overflow-auto">
              <SkeletonText lines={10} />
            </CardBody>
          </Card>
        </motion.div>

        <motion.div variants={staggerItem} className="flex flex-1 flex-col">
          <Card className="flex flex-1 flex-col overflow-hidden">
            <div className="flex items-center gap-2 border-b border-line px-4 py-2.5">
              <GitFork size={14} className="text-ink-mute" />
              <span className="text-[12px] font-medium text-ink-dim">架构图</span>
            </div>
            <CardBody className="flex flex-1 items-center justify-center">
              <div className="text-center">
                <div className="mx-auto grid size-16 place-items-center rounded-xl bg-raised">
                  <GitFork size={28} className="text-ink-mute/50" />
                </div>
                <p className="mt-3 text-[13px] text-ink-dim">Mermaid 图将渲染于此</p>
                <p className="mt-1 text-[11px] text-ink-mute">设计画布完整功能将在 M4 交付</p>
              </div>
            </CardBody>
          </Card>
        </motion.div>
      </div>

      <div className="flex shrink-0 items-center gap-3 border-t border-line px-5 py-3">
        <Button variant="secondary"><Swords size={15} /> 发起辩论</Button>
        <Badge tone="neutral">ADR 模板</Badge>
      </div>
    </motion.div>
  );
}
