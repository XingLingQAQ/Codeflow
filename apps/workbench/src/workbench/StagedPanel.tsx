import { useState } from 'react';
import { toast } from 'sonner';
import { AnimatePresence, motion } from 'motion/react';
import { GitCompareArrows, ArrowUpFromLine, Undo2, Inbox } from 'lucide-react';
import {
  Button,
  Skeleton,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  Spinner,
} from '../ui';
import {
  useStagedList,
  usePromoteStaged,
  useDiscardStaged,
  usePromoteAllStaged,
  useDiscardAllStaged,
  useStagedDiff,
} from '../lib/queries';
import { useEditorStore, bufKey } from '../stores/editor';
import { DiffView, DiffStat } from './DiffView';
import { cn } from '../lib/cn';
import { EASE_FLOW } from '../lib/motion';

function StagedRowStats({ root, path }: { root: string; path: string }) {
  const diff = useStagedDiff(root, path);
  if (!diff.ready) return <Skeleton className="h-3 w-10" />;
  return <DiffStat adds={diff.stats.adds} dels={diff.stats.dels} />;
}

function StagedDiffDialog({
  root,
  path,
  onClose,
  onPromote,
  onDiscard,
}: {
  root: string;
  path: string | null;
  onClose: () => void;
  onPromote: (path: string) => void;
  onDiscard: (path: string) => void;
}) {
  const diff = useStagedDiff(root, path ?? '', !!path);
  return (
    <Dialog open={!!path} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="w-[min(94vw,880px)]">
        <DialogTitle className="pr-8 font-mono text-[14px]">{path}</DialogTitle>
        <DialogDescription className="flex items-center gap-3">
          工作树 → 暂存副本
          {diff.ready && <DiffStat adds={diff.stats.adds} dels={diff.stats.dels} />}
        </DialogDescription>
        <div className="mt-3 max-h-[62vh] overflow-auto rounded-lg border border-line bg-base">
          {diff.isLoading ? (
            <div className="grid h-40 place-items-center">
              <Spinner size={18} />
            </div>
          ) : (
            <DiffView base={diff.base} current={diff.current} ops={diff.ops} />
          )}
        </div>
        <div className="mt-4 flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={onClose}>
            关闭
          </Button>
          <Button
            variant="danger"
            size="sm"
            onClick={() => {
              // Destructive: hand off to the confirm dialog before discarding.
              if (path) onDiscard(path);
              onClose();
            }}
          >
            <Undo2 size={13} /> 丢弃
          </Button>
          <Button
            variant="primary"
            size="sm"
            onClick={() => {
              if (path) onPromote(path);
              onClose();
            }}
          >
            <ArrowUpFromLine size={13} /> 应用到工作树
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

export interface StagedPanelProps {
  projectId: string;
  root: string;
  className?: string;
}

/**
 * Staging-area viewer: per-file diff/promote/discard plus promote-all /
 * discard-all. Backed by /api/v1/workspace/staged|promote|discard(-all).
 */
export function StagedPanel({ projectId, root, className }: StagedPanelProps) {
  const stagedQ = useStagedList(root);
  const items = stagedQ.data?.items ?? [];
  const [diffPath, setDiffPath] = useState<string | null>(null);
  const [confirmDiscardAll, setConfirmDiscardAll] = useState(false);
  // Destructive actions confirm via the project Dialog (not window.confirm).
  const [confirmDiscardPath, setConfirmDiscardPath] = useState<string | null>(null);

  const addAudit = useEditorStore((s) => s.addAudit);
  const addProblem = useEditorStore((s) => s.addProblem);
  const dropBuffer = useEditorStore((s) => s.dropBuffer);
  const openFile = useEditorStore((s) => s.openFile);

  const promoteMut = usePromoteStaged(root);
  const discardMut = useDiscardStaged(root);
  const promoteAllMut = usePromoteAllStaged(root);
  const discardAllMut = useDiscardAllStaged(root);

  const fail = (op: string, path: string, err: unknown) => {
    const message = err instanceof Error ? err.message : String(err);
    addAudit({ op, path, ok: false, detail: message });
    addProblem({
      severity: 'error',
      message: message.includes('blocked by guard') ? `守卫拦截：${message}` : `${op}失败：${message}`,
      path,
      source: 'staging',
    });
    toast.error(`${op}失败：${message}`);
  };

  const promote = (path: string) =>
    promoteMut.mutate(path, {
      onSuccess: () => {
        addAudit({ op: '应用', path, ok: true });
        toast.success(`已应用到工作树：${path}`);
      },
      onError: (err) => fail('应用', path, err),
    });

  /** Request a destructive discard; the dialog confirm runs `discard`. */
  const requestDiscard = (path: string) => setConfirmDiscardPath(path);

  const discard = (path: string) =>
    discardMut.mutate(path, {
      onSuccess: () => {
        // Drop the local buffer so a reopened editor reloads working-tree content.
        dropBuffer(bufKey(projectId, path));
        addAudit({ op: '丢弃', path, ok: true });
        toast.success(`已丢弃暂存：${path}`);
      },
      onError: (err) => fail('丢弃', path, err),
    });

  const promoteAll = () =>
    promoteAllMut.mutate(undefined, {
      onSuccess: (res) => {
        addAudit({ op: '全部应用', path: `${res.total} 个文件`, ok: !res.error, detail: res.error });
        if (res.error) toast.warning(`部分应用完成（${res.total} 个），存在拦截：${res.error}`);
        else toast.success(`已应用全部暂存（${res.total} 个文件）`);
      },
      onError: (err) => fail('全部应用', '*', err),
    });

  const discardAll = () => {
    setConfirmDiscardAll(false);
    discardAllMut.mutate(undefined, {
      onSuccess: (res) => {
        for (const it of items) dropBuffer(bufKey(projectId, it.path));
        addAudit({ op: '全部丢弃', path: `${res.discarded} 个文件`, ok: true });
        toast.success(`已丢弃 ${res.discarded} 个暂存文件`);
      },
      onError: (err) => fail('全部丢弃', '*', err),
    });
  };

  return (
    <div className={cn('flex h-full flex-col overflow-hidden', className)}>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {stagedQ.isLoading ? (
          <div className="space-y-2 p-1">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-8" />
            ))}
          </div>
        ) : stagedQ.isError ? (
          <p className="px-1 py-2 text-xs text-ink-dim">暂存区服务未就绪</p>
        ) : items.length === 0 ? (
          <div className="flex flex-col items-center gap-1.5 px-2 py-5 text-center">
            <Inbox size={16} className="text-ink-mute/60" />
            <p className="text-xs text-ink-dim">暂存区为空 — Ctrl+S 保存的修改会先进入这里</p>
          </div>
        ) : (
          <ul className="space-y-1 py-0.5">
            <AnimatePresence initial={false} mode="popLayout">
              {items.map((it) => (
                <motion.li
                  key={it.path}
                  layout
                  initial={{ opacity: 0, y: -4 }}
                  animate={{ opacity: 1, y: 0 }}
                  exit={{ opacity: 0, scale: 0.96 }}
                  transition={{ duration: 0.15, ease: EASE_FLOW }}
                  className="group flex items-center gap-2 rounded-md px-1.5 py-1.5 transition-colors hover:bg-tint"
                >
                <button
                  type="button"
                  onClick={() => openFile(projectId, it.path)}
                  title={`打开 ${it.path}`}
                  className="min-w-0 flex-1 text-left"
                >
                  <span className="block truncate font-mono text-[12px] text-ink-dim group-hover:text-ink">
                    {it.path}
                  </span>
                </button>
                <StagedRowStats root={root} path={it.path} />
                <span className="flex shrink-0 items-center gap-0.5">
                  <button
                    type="button"
                    aria-label={`查看 ${it.path} 差异`}
                    title="查看差异"
                    onClick={() => setDiffPath(it.path)}
                    className="rounded p-1 text-ink-mute transition-colors hover:bg-tint-active hover:text-ink"
                  >
                    <GitCompareArrows size={13} />
                  </button>
                  <button
                    type="button"
                    aria-label={`应用 ${it.path}`}
                    title="应用到工作树"
                    onClick={() => promote(it.path)}
                    className="rounded p-1 text-ink-mute transition-colors hover:bg-tint-active hover:text-success"
                  >
                    <ArrowUpFromLine size={13} />
                  </button>
                  <button
                    type="button"
                    aria-label={`丢弃 ${it.path}`}
                    title="丢弃暂存"
                    onClick={() => requestDiscard(it.path)}
                    className="rounded p-1 text-ink-mute transition-colors hover:bg-tint-active hover:text-danger"
                  >
                    <Undo2 size={13} />
                  </button>
                </span>
              </motion.li>
            ))}
            </AnimatePresence>
          </ul>
        )}
      </div>

      {items.length > 0 && (
        <div className="flex shrink-0 items-center justify-between gap-2 border-t border-line pt-2">
          <span className="nums text-xs text-ink-mute">{items.length} 个文件待应用</span>
          <span className="flex gap-1.5">
            <Button
              variant="ghost"
              size="sm"
              className="h-6 px-2 text-[11px]"
              loading={discardAllMut.isPending}
              onClick={() => setConfirmDiscardAll(true)}
            >
              全部丢弃
            </Button>
            <Button
              variant="primary"
              size="sm"
              className="h-6 px-2 text-[11px]"
              loading={promoteAllMut.isPending}
              onClick={promoteAll}
            >
              全部应用
            </Button>
          </span>
        </div>
      )}

      {/* Per-file discard confirmation */}
      <Dialog open={!!confirmDiscardPath} onOpenChange={(open) => !open && setConfirmDiscardPath(null)}>
        <DialogContent className="w-[min(92vw,400px)]">
          <DialogTitle>丢弃暂存</DialogTitle>
          <DialogDescription>
            将丢弃 <span className="font-mono">{confirmDiscardPath}</span> 的暂存副本，工作树不受影响，此操作不可撤销。
          </DialogDescription>
          <div className="mt-5 flex justify-end gap-2">
            <Button variant="ghost" size="sm" onClick={() => setConfirmDiscardPath(null)}>
              取消
            </Button>
            <Button
              variant="danger"
              size="sm"
              onClick={() => {
                if (confirmDiscardPath) discard(confirmDiscardPath);
                setConfirmDiscardPath(null);
              }}
            >
              <Undo2 size={13} /> 丢弃暂存
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      {/* Discard-all confirmation */}
      <Dialog open={confirmDiscardAll} onOpenChange={setConfirmDiscardAll}>
        <DialogContent className="w-[min(92vw,400px)]">
          <DialogTitle>丢弃全部暂存</DialogTitle>
          <DialogDescription>
            将丢弃全部 {items.length} 个暂存文件，工作树不受影响，此操作不可撤销。
          </DialogDescription>
          <div className="mt-5 flex justify-end gap-2">
            <Button variant="ghost" size="sm" onClick={() => setConfirmDiscardAll(false)}>
              取消
            </Button>
            <Button variant="danger" size="sm" onClick={discardAll}>
              <Undo2 size={13} /> 全部丢弃
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      <StagedDiffDialog
        root={root}
        path={diffPath}
        onClose={() => setDiffPath(null)}
        onPromote={promote}
        onDiscard={requestDiscard}
      />
    </div>
  );
}
