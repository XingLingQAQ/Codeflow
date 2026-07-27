import { motion } from 'motion/react';
import { Bot, Crown, Code2, GitFork, Sparkles } from 'lucide-react';
import { PageShell, SectionTitle } from './PageShell';
import { Card, Badge, EmptyState, Button } from '../../ui';
import { staggerItem } from '../../lib/motion';

const roles = [
  { icon: Crown, name: '指挥官 · Main', desc: '统筹全局、拆解目标、调度子代理与工具集。', tone: 'accent' as const },
  { icon: Code2, name: '编码 · Coder', desc: '在编码画布内实现变更，受守卫与上下文预算约束。', tone: 'info' as const },
  { icon: GitFork, name: '子代理 · Sub', desc: '并行承接检索、评审、辩论等可隔离的子任务。', tone: 'neutral' as const },
];

const roleWrap: Record<'accent' | 'info' | 'neutral', string> = {
  accent: 'bg-tint text-ink-dim',
  info: 'bg-tint text-ink-dim',
  neutral: 'bg-tint text-ink-dim',
};

export default function Agents() {
  return (
    <PageShell
      title="Agent 广场"
      subtitle="Agent 资产的编排与市场（Registry 将于 M4 交付）"
      actions={
        <Button variant="secondary" disabled>
          <Sparkles size={15} /> 导入 Agent
        </Button>
      }
    >
      <motion.div variants={staggerItem}>
        <SectionTitle>内置角色</SectionTitle>
        <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
          {roles.map((r) => {
            const Icon = r.icon;
            return (
              <Card key={r.name} className="p-5">
                <div className="flex items-center gap-2.5">
                  <span className={`grid size-9 place-items-center rounded-lg ${roleWrap[r.tone]}`}>
                    <Icon size={17} />
                  </span>
                  <h3 className="font-medium text-ink">{r.name}</h3>
                </div>
                <p className="mt-2.5 text-[13px] leading-relaxed text-ink-dim">{r.desc}</p>
              </Card>
            );
          })}
        </div>
      </motion.div>

      <motion.div variants={staggerItem} className="mt-9">
        <SectionTitle>Agent 市场</SectionTitle>
        <Card className="py-4">
          <EmptyState
            icon={<Bot size={24} />}
            title="Agent Registry 即将到来"
            description="发布、发现与编排可复用 Agent 资产的市场将在 M4 里程碑上线。"
            action={
              <Badge tone="accent">M4</Badge>
            }
          />
        </Card>
      </motion.div>
    </PageShell>
  );
}
