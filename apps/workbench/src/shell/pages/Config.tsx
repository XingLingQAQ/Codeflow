import { useParams, useNavigate } from 'react-router-dom';
import { motion } from 'motion/react';
import { Cpu, Radio, Plug, Wrench } from 'lucide-react';
import { PageShell } from './PageShell';
import { Card, EmptyState, Badge } from '../../ui';
import { cn } from '../../lib/cn';
import { staggerItem } from '../../lib/motion';

const SECTIONS = [
  { key: 'models', label: '模型', icon: Cpu, desc: '配置可用模型、别名与热切换策略。' },
  { key: 'channels', label: '渠道', icon: Radio, desc: '管理 API 渠道、密钥与速率限制。' },
  { key: 'mcp', label: 'MCP', icon: Plug, desc: '接入 MCP 服务，暴露外部工具给 Agent。' },
  { key: 'skills', label: 'Skill', icon: Wrench, desc: '可视化编辑 Skill 匹配与注入规则。' },
] as const;

export default function Config() {
  const { section } = useParams();
  const navigate = useNavigate();
  const active = SECTIONS.find((s) => s.key === section) ?? SECTIONS[0];
  const ActiveIcon = active.icon;

  return (
    <PageShell title="配置中心" subtitle="模型 / 渠道 / MCP / Skill 的可视化编辑（M5）">
      <motion.div variants={staggerItem} className="flex gap-6">
        <aside className="w-48 shrink-0 space-y-1">
          {SECTIONS.map((s) => {
            const Icon = s.icon;
            const on = s.key === active.key;
            return (
              <button
                key={s.key}
                onClick={() => navigate(`/config/${s.key}`)}
                className={cn(
                  'flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-[13px] transition-colors',
                  on ? 'bg-tint-active text-ink' : 'text-ink-dim hover:bg-tint hover:text-ink',
                )}
              >
                <Icon size={15} /> {s.label}
              </button>
            );
          })}
        </aside>
        <div className="flex-1">
          <Card className="py-4">
            <EmptyState
              icon={<ActiveIcon size={24} />}
              title={`${active.label} 配置`}
              description={`${active.desc} 可视化编辑将在 M5 里程碑交付。`}
              action={<Badge tone="accent">M5</Badge>}
            />
          </Card>
        </div>
      </motion.div>
    </PageShell>
  );
}
