import { motion } from 'motion/react';
import { Lightbulb, MessageCircleQuestion, ThumbsUp, ThumbsDown } from 'lucide-react';
import { Button, Textarea, Card, Skeleton } from '../ui';
import { staggerContainer, staggerItem } from '../lib/motion';

export default function IdeaCanvas() {
  return (
    <motion.div variants={staggerContainer} initial="initial" animate="animate" className="flex h-full gap-5 p-5">
      <motion.div variants={staggerItem} className="flex flex-[3] flex-col gap-4">
        <h2 className="font-display text-lg font-bold text-ink">意图澄清</h2>
        <Textarea rows={8} placeholder="描述你的想法、目标与约束…" className="min-h-48 flex-1 text-[15px] leading-relaxed" />
        <div className="flex justify-end">
          <Button variant="primary">完成意图澄清</Button>
        </div>
      </motion.div>

      <motion.div variants={staggerItem} className="flex w-80 flex-col gap-3">
        <h3 className="text-[13px] font-semibold text-ink-dim">Agent 追问</h3>
        {[
          '目标用户群体是谁？有无规模约束？',
          '技术栈有特定要求吗（语言/框架/部署）？',
          '是否需要与现有系统集成？哪些端点？',
        ].map((q, i) => (
          <Card key={i} className="animate-rise p-3.5" style={{ animationDelay: `${120 + i * 80}ms` }}>
            <div className="flex items-start gap-2.5">
              <MessageCircleQuestion size={16} className="mt-0.5 shrink-0 text-ink-mute" />
              <p className="text-[13px] leading-relaxed text-ink-dim">{q}</p>
            </div>
            <div className="mt-2.5 flex gap-1.5">
              <Button variant="ghost" size="sm"><ThumbsUp size={13} /> 采纳</Button>
              <Button variant="ghost" size="sm"><ThumbsDown size={13} /> 忽略</Button>
            </div>
          </Card>
        ))}
      </motion.div>
    </motion.div>
  );
}
