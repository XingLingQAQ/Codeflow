import { motion } from 'motion/react';
import { FileDiff, MessageSquareText, Swords } from 'lucide-react';
import { Card, CardBody, Skeleton, SkeletonText, EmptyState, Badge } from '../ui';
import { staggerContainer, staggerItem } from '../lib/motion';

const diffs = [
  { file: 'src/checkout/handler.ts', adds: 42, dels: 8 },
  { file: 'src/checkout/types.ts', adds: 15, dels: 3 },
  { file: 'src/tests/checkout.test.ts', adds: 68, dels: 0 },
];

export default function ReviewCanvas() {
  return (
    <motion.div variants={staggerContainer} initial="initial" animate="animate" className="flex h-full gap-4 p-5">
      <motion.div variants={staggerItem} className="w-64 shrink-0 space-y-2">
        <h3 className="flex items-center gap-2 text-[13px] font-semibold text-ink-dim">
          <FileDiff size={14} /> 变更集
        </h3>
        {diffs.map((d) => (
          <Card key={d.file} interactive className="px-3 py-2.5">
            <p className="truncate font-mono text-[12px] text-ink">{d.file}</p>
            <div className="mt-1.5 flex gap-3 text-[11px]">
              <span className="text-success">+{d.adds}</span>
              <span className="text-danger">-{d.dels}</span>
            </div>
          </Card>
        ))}
      </motion.div>

      <motion.div variants={staggerItem} className="flex min-w-0 flex-1 flex-col gap-3">
        <h3 className="flex items-center gap-2 text-[13px] font-semibold text-ink-dim">
          <MessageSquareText size={14} /> Critic 评审意见
        </h3>
        <div className="flex-1 space-y-3 overflow-auto">
          {[1, 2, 3].map((i) => (
            <Card key={i} className="p-4" style={{ animationDelay: `${i * 80}ms` }}>
              <div className="flex items-start gap-2.5">
                <div className="size-7 shrink-0 rounded-full border border-line bg-tint-active" />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <Skeleton className="h-3 w-20" />
                    <Badge tone="neutral">审查中</Badge>
                  </div>
                  <div className="mt-2">
                    <SkeletonText lines={2} />
                  </div>
                </div>
              </div>
            </Card>
          ))}
        </div>
      </motion.div>

      <motion.div variants={staggerItem} className="w-64 shrink-0">
        <Card className="h-full p-4">
          <div className="flex items-center gap-2 text-[12px] font-medium text-ink-dim">
            <Swords size={14} /> 辩论
          </div>
          <div className="mt-4">
            <EmptyState
              icon={<Swords size={20} />}
              title="开启辩论"
              description="对分歧点发起多方深度讨论"
            />
          </div>
        </Card>
      </motion.div>
    </motion.div>
  );
}
