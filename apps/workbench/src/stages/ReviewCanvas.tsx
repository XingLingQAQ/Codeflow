import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { motion } from 'motion/react';
import { FileDiff, MessageSquareText, Swords } from 'lucide-react';
import { Card, Skeleton, EmptyState, Badge, Spinner } from '../ui';
import { useStagedList, useStagedDiff } from '../lib/queries';
import { useEditorStore } from '../stores/editor';
import { DiffView, DiffStat } from '../workbench/DiffView';
import { staggerContainer, staggerItem } from '../lib/motion';
import { cn } from '../lib/cn';

function ChangeCard({
  root,
  path,
  active,
  onSelect,
}: {
  root: string;
  path: string;
  active: boolean;
  onSelect: () => void;
}) {
  const diff = useStagedDiff(root, path);
  return (
    <Card
      interactive
      onClick={onSelect}
      className={cn('px-3 py-2.5', active && 'border-line-strong bg-hover')}
    >
      <p className="truncate font-mono text-[12px] text-ink" title={path}>
        {path}
      </p>
      <div className="mt-1.5">
        {diff.ready ? <DiffStat adds={diff.stats.adds} dels={diff.stats.dels} /> : <Skeleton className="h-3 w-12" />}
      </div>
    </Card>
  );
}

function SelectedDiff({ root, path }: { root: string; path: string }) {
  const diff = useStagedDiff(root, path);
  return (
    <Card className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <div className="flex h-9 shrink-0 items-center justify-between border-b border-line px-3">
        <span className="truncate font-mono text-[12px] text-ink-dim">{path}</span>
        {diff.ready && <DiffStat adds={diff.stats.adds} dels={diff.stats.dels} />}
      </div>
      <div className="min-h-0 flex-1 overflow-auto bg-base">
        {diff.isLoading ? (
          <div className="grid h-full place-items-center">
            <Spinner size={18} />
          </div>
        ) : (
          <DiffView base={diff.base} current={diff.current} ops={diff.ops} />
        )}
      </div>
    </Card>
  );
}

export default function ReviewCanvas() {
  const { projectId = '' } = useParams<{ projectId: string }>();
  const root = useEditorStore((s) => s.rootByProject[projectId]) ?? '';
  const stagedQ = useStagedList(root, !!root);
  const items = stagedQ.data?.items ?? [];
  const [selected, setSelected] = useState<string | null>(null);

  // Default-select the first staged file; drop the selection if it disappears.
  useEffect(() => {
    if (items.length === 0) {
      if (selected !== null) setSelected(null);
      return;
    }
    if (!selected || !items.some((i) => i.path === selected)) {
      setSelected(items[0].path);
    }
  }, [items, selected]);

  return (
    <motion.div variants={staggerContainer} initial="initial" animate="animate" className="flex h-full gap-3 p-3">
      <motion.div variants={staggerItem} className="flex w-64 shrink-0 flex-col gap-2 overflow-y-auto">
        <h3 className="flex items-center gap-2 px-0.5 text-[13px] font-semibold text-ink-dim">
          <FileDiff size={14} /> 变更集
          {items.length > 0 && <Badge tone="warn">{items.length}</Badge>}
        </h3>
        {!root ? (
          <p className="px-0.5 text-[12px] leading-relaxed text-ink-mute">
            尚未加载工作区 — 在编码画布设置项目根目录后，这里展示暂存变更集。
          </p>
        ) : stagedQ.isLoading ? (
          Array.from({ length: 3 }).map((_, i) => <Skeleton key={i} className="h-14" />)
        ) : items.length === 0 ? (
          <p className="px-0.5 text-[12px] text-ink-mute">暂存区为空，暂无待评审变更。</p>
        ) : (
          items.map((it) => (
            <ChangeCard
              key={it.path}
              root={root}
              path={it.path}
              active={selected === it.path}
              onSelect={() => setSelected(it.path)}
            />
          ))
        )}
      </motion.div>

      <motion.div variants={staggerItem} className="flex min-w-0 flex-1 flex-col">
        {root && selected ? (
          <SelectedDiff root={root} path={selected} />
        ) : (
          <Card className="grid flex-1 place-items-center">
            <EmptyState
              icon={<FileDiff size={20} />}
              title="选择变更文件"
              description="左侧变更集中的文件会以逐行 Diff 展示在此，供 Critic 评审与人工复核。"
            />
          </Card>
        )}
      </motion.div>

      <motion.div variants={staggerItem} className="flex w-64 shrink-0 flex-col gap-3 overflow-y-auto">
        <div>
          <h3 className="mb-2 flex items-center gap-2 text-[13px] font-semibold text-ink-dim">
            <MessageSquareText size={14} /> Critic 评审意见
          </h3>
          <Card className="p-3.5">
            <p className="text-[12px] leading-relaxed text-ink-mute">
              Red Critic 的结构化评审意见（正确性 / 安全 / 性能 / 可维护性）将在后续里程碑接入；当前可在左侧逐行核对暂存 Diff。
            </p>
          </Card>
        </div>
        <Card className="p-3.5">
          <div className="flex items-center gap-2 font-display-13 text-ink">
            <Swords size={14} /> 辩论
          </div>
          <p className="mt-2 text-[12px] leading-relaxed text-ink-mute">
            对评审分歧点发起多方深度讨论（接入 /api/v1/debates）。评审不通过时，可在左侧阶段栏发起回环返工。
          </p>
        </Card>
      </motion.div>
    </motion.div>
  );
}
