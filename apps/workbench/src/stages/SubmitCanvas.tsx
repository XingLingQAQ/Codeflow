import { useMemo } from 'react';
import { useParams } from 'react-router-dom';
import { motion } from 'motion/react';
import { toast } from 'sonner';
import { GitCommitHorizontal, Camera, Inbox, ArrowUpFromLine } from 'lucide-react';
import { Card, Button, Badge, Skeleton, EmptyState } from '../ui';
import { useStagedList, useStagedDiff, usePromoteStaged, useProjectFlow } from '../lib/queries';
import { useEditorStore } from '../stores/editor';
import { DiffStat } from '../workbench/DiffView';
import type { WorkspaceEntry } from '../services-bridge/workspace';
import { staggerContainer, staggerItem } from '../lib/motion';

function FileRow({ root, path }: { root: string; path: string }) {
  const diff = useStagedDiff(root, path);
  return (
    <li className="flex items-center justify-between gap-3 py-1">
      <span className="truncate font-mono text-[12px] text-ink-dim" title={path}>
        {path}
      </span>
      {diff.ready ? <DiffStat adds={diff.stats.adds} dels={diff.stats.dels} /> : <Skeleton className="h-3 w-10" />}
    </li>
  );
}

function GroupCard({
  root,
  dir,
  files,
  onPromoteGroup,
  promoting,
}: {
  root: string;
  dir: string;
  files: WorkspaceEntry[];
  onPromoteGroup: (paths: string[]) => void;
  promoting: boolean;
}) {
  return (
    <Card className="p-4">
      <div className="flex items-center gap-2">
        <GitCommitHorizontal size={15} className="shrink-0 text-ink-mute" />
        <span className="min-w-0 flex-1 truncate font-mono text-[13px] font-medium text-ink">{dir}</span>
        <Badge className="nums shrink-0">{files.length} 个文件</Badge>
      </div>
      <ul className="mt-2.5 divide-y divide-line pl-6">
        {files.map((f) => (
          <FileRow key={f.path} root={root} path={f.path} />
        ))}
      </ul>
      <div className="mt-3 flex justify-end gap-2 pl-6">
        <Button
          variant="primary"
          size="sm"
          loading={promoting}
          onClick={() => onPromoteGroup(files.map((f) => f.path))}
        >
          <ArrowUpFromLine size={13} /> 应用该组到工作树
        </Button>
      </div>
    </Card>
  );
}

export default function SubmitCanvas() {
  const { projectId = '' } = useParams<{ projectId: string }>();
  const root = useEditorStore((s) => s.rootByProject[projectId]) ?? '';
  const stagedQ = useStagedList(root, !!root);
  const items = stagedQ.data?.items ?? [];
  const promoteMut = usePromoteStaged(root);
  const { flow } = useProjectFlow(projectId);
  const submitStage = flow?.stages.find((s) => s.type === 'submit');

  // Commit-group candidates: staged files bucketed by top-level directory.
  const groups = useMemo(() => {
    const byDir = new Map<string, WorkspaceEntry[]>();
    for (const it of items) {
      const top = it.path.includes('/') ? it.path.split('/')[0] : '（根目录）';
      const list = byDir.get(top) ?? [];
      list.push(it);
      byDir.set(top, list);
    }
    return [...byDir.entries()].sort((a, b) => a[0].localeCompare(b[0]));
  }, [items]);

  const promoteGroup = async (paths: string[]) => {
    let ok = 0;
    for (const path of paths) {
      try {
        await promoteMut.mutateAsync(path);
        ok++;
      } catch (err) {
        toast.error(`应用失败 ${path}：${err instanceof Error ? err.message : String(err)}`);
      }
    }
    if (ok > 0) toast.success(`已应用 ${ok} 个文件到工作树`);
  };

  return (
    <motion.div variants={staggerContainer} initial="initial" animate="animate" className="flex h-full gap-5 overflow-y-auto p-5">
      <motion.div variants={staggerItem} className="flex min-w-0 flex-1 flex-col gap-4">
        <h2 className="font-display text-lg font-bold text-ink">提交分组</h2>
        {!root ? (
          <EmptyState
            icon={<Inbox size={20} />}
            title="尚未加载工作区"
            description="在编码画布设置项目根目录后，暂存区的修改会按顶层目录自动分组，作为提交候选展示在此。"
          />
        ) : stagedQ.isLoading ? (
          <div className="space-y-3">
            {[1, 2].map((i) => (
              <Card key={i} className="p-4">
                <Skeleton className="h-4 w-2/5" />
                <Skeleton className="mt-3 h-3 w-4/5" />
                <Skeleton className="mt-2 h-3 w-3/5" />
              </Card>
            ))}
          </div>
        ) : groups.length === 0 ? (
          <EmptyState
            icon={<Inbox size={20} />}
            title="暂存区为空"
            description="没有待提交的修改。编码画布中 Ctrl+S 保存的文件会进入暂存区，在此按目录分组提交。"
          />
        ) : (
          groups.map(([dir, files]) => (
            <GroupCard
              key={dir}
              root={root}
              dir={dir}
              files={files}
              promoting={promoteMut.isPending}
              onPromoteGroup={promoteGroup}
            />
          ))
        )}
      </motion.div>

      <motion.div variants={staggerItem} className="flex w-72 shrink-0 flex-col gap-4">
        <Card className="p-4">
          <div className="flex items-center gap-2 text-[12px] font-medium text-ink-dim">
            <Camera size={14} /> 快照绑定
          </div>
          <div className="mt-3 text-center text-[12px] leading-relaxed text-ink-mute">
            {submitStage?.snapshot_id ? (
              <>
                <Badge tone="success" className="nums font-mono">{submitStage.snapshot_id}</Badge>
                <p className="mt-2">提交阶段已绑定快照，回环时可原子恢复。</p>
              </>
            ) : (
              <>
                <Badge tone="neutral">尚未绑定</Badge>
                <p className="mt-2">阶段快照将在完成推进（advance）时自动创建。</p>
              </>
            )}
          </div>
        </Card>

        <Card className="p-4">
          <div className="flex items-center gap-2 text-[12px] font-medium text-ink-dim">
            <GitCommitHorizontal size={14} /> 提交编排
          </div>
          <p className="mt-2 text-[12px] leading-relaxed text-ink-mute">
            git 提交消息生成与归档报告将在后续里程碑交付；当前可按组把暂存修改应用到工作树。
          </p>
        </Card>
      </motion.div>
    </motion.div>
  );
}
