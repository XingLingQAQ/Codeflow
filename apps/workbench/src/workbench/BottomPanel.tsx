import { Terminal, ScrollText, History, AlertTriangle, Shield } from 'lucide-react';
import { Tabs, TabsList, TabsTrigger, TabsContent } from '../ui';
import { useLayoutStore, type BottomTab } from '../stores/layout';
import { cn } from '../lib/cn';
import type { LucideIcon } from 'lucide-react';

const TAB_META: { value: BottomTab; label: string; icon: LucideIcon }[] = [
  { value: 'terminal', label: '终端', icon: Terminal },
  { value: 'logs', label: '日志', icon: ScrollText },
  { value: 'audit', label: '审计', icon: History },
  { value: 'problems', label: '问题', icon: AlertTriangle },
  { value: 'guard', label: '守卫', icon: Shield },
];

const TAB_HINT: Record<BottomTab, string> = {
  terminal: '集成终端将在编码画布（M3）交付后启用',
  logs: '工作流运行日志将由事件总线实时推送',
  audit: '审计日志记录所有写操作与守卫决策',
  problems: 'TypeScript / 守卫规则诊断聚合',
  guard: '守卫拦截与豁免决策详情',
};

export function BottomPanel() {
  const tab = useLayoutStore((s) => s.bottomTab);
  const setTab = useLayoutStore((s) => s.setBottomTab);

  return (
    <div className="flex h-full flex-col overflow-hidden bg-panel">
      <Tabs value={tab} onValueChange={(v) => setTab(v as BottomTab)} className="flex h-full flex-col">
        <div className="shrink-0 border-b border-line px-2 pt-1">
          <TabsList className="border-0 bg-transparent p-0">
            {TAB_META.map((t) => {
              const Icon = t.icon;
              return (
                <TabsTrigger key={t.value} value={t.value} className="gap-1.5 text-[12px]">
                  <Icon size={13} /> {t.label}
                </TabsTrigger>
              );
            })}
          </TabsList>
        </div>
        {TAB_META.map((t) => {
          const Icon = t.icon;
          return (
            <TabsContent key={t.value} value={t.value} className="flex-1 overflow-auto">
              <div className="flex h-full items-center justify-center gap-2 text-[13px] text-ink-mute">
                <Icon size={15} className="text-ink-mute/60" />
                {TAB_HINT[t.value]}
              </div>
            </TabsContent>
          );
        })}
      </Tabs>
    </div>
  );
}
