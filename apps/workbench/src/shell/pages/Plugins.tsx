import { motion } from 'motion/react';
import { Puzzle, Blocks, ShieldCheck } from 'lucide-react';
import { PageShell, SectionTitle } from './PageShell';
import { Card, Badge, EmptyState, Tabs, TabsList, TabsTrigger, TabsContent } from '../../ui';
import { staggerItem } from '../../lib/motion';

export default function Plugins() {
  return (
    <PageShell title="插件管理" subtitle="核心能力亦为贡献点，可被插件扩展或替换（M6）">
      <motion.div variants={staggerItem}>
        <Tabs defaultValue="installed">
          <TabsList>
            <TabsTrigger value="installed">已安装</TabsTrigger>
            <TabsTrigger value="market">市场</TabsTrigger>
          </TabsList>

          <TabsContent value="installed" className="mt-5">
            <Card className="py-4">
              <EmptyState
                icon={<Puzzle size={24} />}
                title="还没有安装插件"
                description="贡献点注册表与沙箱运行时将在 M6 交付，届时可安装并管理插件。"
                action={<Badge tone="accent">M6</Badge>}
              />
            </Card>
          </TabsContent>

          <TabsContent value="market" className="mt-5">
            <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
              {[
                { icon: Blocks, title: '贡献点', desc: '面板、命令、阶段画布均可由插件注册。' },
                { icon: ShieldCheck, title: '沙箱运行时', desc: '受限权限模型下运行第三方代码。' },
                { icon: Puzzle, title: '深度扩展', desc: '核心功能本身也是可替换的贡献点。' },
              ].map((c) => {
                const Icon = c.icon;
                return (
                  <Card key={c.title} className="p-5">
                    <span className="grid size-9 place-items-center rounded-lg bg-tint-active text-ink-dim">
                      <Icon size={17} />
                    </span>
                    <h3 className="mt-3 font-medium text-ink">{c.title}</h3>
                    <p className="mt-1.5 text-[13px] leading-relaxed text-ink-dim">{c.desc}</p>
                  </Card>
                );
              })}
            </div>
          </TabsContent>
        </Tabs>
      </motion.div>
    </PageShell>
  );
}
