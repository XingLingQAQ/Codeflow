import { motion } from 'motion/react';
import { ListTree, Newspaper, Archive } from 'lucide-react';
import { Card, CardBody, Skeleton, SkeletonText } from '../ui';
import { staggerContainer, staggerItem } from '../lib/motion';

export default function ResearchCanvas() {
  return (
    <motion.div variants={staggerContainer} initial="initial" animate="animate" className="flex h-full gap-4 p-5">
      <motion.div variants={staggerItem} className="w-56 shrink-0">
        <Card className="h-full overflow-hidden">
          <div className="flex items-center gap-2 border-b border-line px-3 py-2.5">
            <ListTree size={14} className="text-ink-mute" />
            <span className="text-[12px] font-medium text-ink-dim">检索计划树</span>
          </div>
          <CardBody className="space-y-2">
            {[1, 2, 3, 4].map((i) => (
              <div key={i} className="flex items-center gap-2" style={{ paddingLeft: `${(i % 3) * 12}px` }}>
                <span className="inline-block size-1.5 rounded-full bg-ink-mute/40" />
                <Skeleton className="h-3 flex-1" />
              </div>
            ))}
          </CardBody>
        </Card>
      </motion.div>

      <motion.div variants={staggerItem} className="flex min-w-0 flex-1 flex-col gap-3">
        <h3 className="flex items-center gap-2 text-[13px] font-semibold text-ink-dim">
          <Newspaper size={14} /> 结果流
        </h3>
        <div className="flex-1 space-y-3 overflow-auto">
          {[1, 2, 3].map((i) => (
            <Card key={i} className="p-4">
              <Skeleton className="mb-2 h-4 w-2/3" />
              <SkeletonText lines={3} />
            </Card>
          ))}
        </div>
      </motion.div>

      <motion.div variants={staggerItem} className="w-56 shrink-0">
        <Card className="flex h-full flex-col items-center justify-center overflow-hidden border-dashed p-4 text-center">
          <Archive size={28} className="mb-3 text-ink-mute/40" />
          <p className="text-[13px] font-medium text-ink-dim">证据篮</p>
          <p className="mt-1 text-[11px] text-ink-mute">将检索结果拖入此处沉淀证据</p>
        </Card>
      </motion.div>
    </motion.div>
  );
}
