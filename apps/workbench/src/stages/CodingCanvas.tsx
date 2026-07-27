import { useState } from 'react';
import { motion } from 'motion/react';
import { FolderTree, FileCode2, Braces, Shield } from 'lucide-react';
import { Card, CardBody, Button, Input, Badge, EmptyState, Skeleton, SkeletonText } from '../ui';
import { useGuardRules, useWorkspaceList } from '../lib/queries';
import { staggerContainer, staggerItem } from '../lib/motion';
import { cn } from '../lib/cn';

export default function CodingCanvas() {
  const [root, setRoot] = useState('');
  const [loadRoot, setLoadRoot] = useState('');
  const wsQ = useWorkspaceList(loadRoot, '', !!loadRoot);
  const guardQ = useGuardRules(true);
  const entries = wsQ.data?.items ?? [];
  const rules = guardQ.data?.items ?? [];

  return (
    <motion.div variants={staggerContainer} initial="initial" animate="animate" className="flex h-full gap-4 p-4">
      <motion.div variants={staggerItem} className="w-56 shrink-0">
        <Card className="flex h-full flex-col overflow-hidden">
          <div className="flex items-center gap-2 border-b border-line px-3 py-2.5">
            <FolderTree size={14} className="text-ink-mute" />
            <span className="text-[12px] font-medium text-ink-dim">文件树</span>
          </div>
          <CardBody className="flex-1 overflow-auto">
            {!loadRoot ? (
              <div className="space-y-2 py-2">
                <p className="text-[12px] text-ink-dim">输入项目根路径以加载文件树</p>
                <Input value={root} onChange={(e) => setRoot(e.target.value)} placeholder="D:\Project\..." className="text-[12px]" />
                <Button variant="secondary" size="sm" onClick={() => setLoadRoot(root)} disabled={!root.trim()}>加载</Button>
              </div>
            ) : wsQ.isLoading ? (
              <div className="space-y-2 py-2">{Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-4 w-full" />)}</div>
            ) : wsQ.isError ? (
              <EmptyState icon={<FolderTree size={18} />} title="加载失败" description="工作区服务未就绪（实验特性）" action={<Button variant="ghost" size="sm" onClick={() => wsQ.refetch()}>重试</Button>} />
            ) : (
              <div className="space-y-0.5 py-1">
                {entries.map((e) => (
                  <div key={e.path} className="flex items-center gap-1.5 rounded px-1.5 py-1 text-[12px] text-ink-dim hover:bg-tint">
                    {e.is_dir ? <FolderTree size={13} className="text-ink-mute" /> : <FileCode2 size={13} className="text-ink-mute" />}
                    <span className="truncate">{e.name}</span>
                  </div>
                ))}
              </div>
            )}
          </CardBody>
        </Card>
      </motion.div>

      <motion.div variants={staggerItem} className="flex min-w-0 flex-1 flex-col">
        <Card className="flex flex-1 flex-col overflow-hidden">
          <div className="flex items-center gap-1 border-b border-line px-2 py-1.5">
            {['index.ts', 'utils.ts', 'types.ts'].map((f, i) => (
              <button key={f} className={cn('rounded-md px-3 py-1 text-[12px]', i === 0 ? 'bg-tint-hover text-ink' : 'text-ink-mute hover:text-ink-dim')}>{f}</button>
            ))}
          </div>
          <CardBody className="flex-1 overflow-auto font-mono text-[13px] leading-relaxed">
            <SkeletonText lines={14} />
          </CardBody>
        </Card>
      </motion.div>

      <motion.div variants={staggerItem} className="flex w-64 shrink-0 flex-col gap-4">
        <Card className="p-4">
          <div className="flex items-center gap-2 text-[12px] font-medium text-ink-dim">
            <Braces size={14} /> 上下文构建器
          </div>
          <div className="mt-4 flex items-center justify-center">
            <div className="grid size-24 place-items-center rounded-full border-4 border-accent/30">
              <span className="text-[13px] font-display font-bold text-ink-mute">— tokens</span>
            </div>
          </div>
          <p className="mt-3 text-center text-[11px] text-ink-mute">Token 预算饼图（M3）</p>
        </Card>

        <Card className="flex-1 overflow-hidden p-4">
          <div className="flex items-center gap-2 text-[12px] font-medium text-ink-dim">
            <Shield size={14} /> 守卫规则
          </div>
          {guardQ.isError ? (
            <p className="mt-3 text-[12px] text-ink-mute">守卫服务未就绪</p>
          ) : rules.length === 0 ? (
            <p className="mt-3 text-[12px] text-ink-mute">暂无规则</p>
          ) : (
            <div className="mt-3 space-y-1.5">
              {rules.slice(0, 6).map((r) => (
                <div key={r.id} className="flex items-center justify-between text-[12px]">
                  <span className="truncate text-ink-dim">{r.id}</span>
                  <Badge tone={r.severity === 'error' ? 'danger' : r.severity === 'warn' ? 'warn' : 'neutral'}>{r.severity}</Badge>
                </div>
              ))}
            </div>
          )}
        </Card>
      </motion.div>
    </motion.div>
  );
}
