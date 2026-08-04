import { useEffect, useMemo, useRef, useState } from 'react';
import { toast } from 'sonner';
import { AnimatePresence, motion } from 'motion/react';
import { X, Save, FilePen, ChevronRight, FileCode2, Layers, MessageSquareQuote } from 'lucide-react';
import {
  EmptyState,
  Badge,
  Button,
  Skeleton,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from '../ui';
import { useWorkspaceFile, useStagedList, useWriteWorkspaceFile } from '../lib/queries';
import { useEditorStore, bufKey } from '../stores/editor';
import { useChatStore } from '../stores/chat';
import { relTime } from '../lib/format';
import { isModEvent } from '../lib/platform';
import { cn } from '../lib/cn';
import { EASE_FLOW } from '../lib/motion';

export interface EditorPaneProps {
  projectId: string;
  root: string;
}

/**
 * Multi-tab plain-code editor (M3 interim for the Monaco slot in the coding
 * canvas spec): open/edit/save with dirty + staged markers. Ctrl/Cmd+S saves
 * to the shadow staging area; direct write is available as a secondary action.
 */
export function EditorPane({ projectId, root }: EditorPaneProps) {
  const tabs = useEditorStore((s) => s.tabsByProject[projectId]) ?? [];
  const activePath = useEditorStore((s) => s.activeByProject[projectId]);
  const buffers = useEditorStore((s) => s.buffers);
  const baselines = useEditorStore((s) => s.baselines);
  const setActive = useEditorStore((s) => s.setActive);
  const closeFile = useEditorStore((s) => s.closeFile);
  const setLoaded = useEditorStore((s) => s.setLoaded);
  const setBuffer = useEditorStore((s) => s.setBuffer);
  const markSaved = useEditorStore((s) => s.markSaved);
  const addProblem = useEditorStore((s) => s.addProblem);
  const addAudit = useEditorStore((s) => s.addAudit);

  const stagedQ = useStagedList(root);
  const stagedPaths = useMemo(
    () => new Set((stagedQ.data?.items ?? []).map((e) => e.path)),
    [stagedQ.data],
  );

  const key = activePath ? bufKey(projectId, activePath) : '';
  const buffer = key ? buffers[key] : undefined;
  const baseline = key ? baselines[key] : undefined;
  const dirty = buffer !== undefined && buffer !== baseline;

  // Load staged copy when one exists, otherwise the working-tree file. Wait
  // for the staged list before the first read so we pick the right source.
  const stagedKnown = stagedQ.isSuccess || stagedQ.isError;
  const isStaged = !!activePath && stagedPaths.has(activePath);
  const fileQ = useWorkspaceFile(root, activePath ?? '', isStaged, !!activePath && stagedKnown);

  useEffect(() => {
    if (fileQ.data && key) {
      // setLoaded is a no-op when a buffer already exists (unsaved edits win).
      setLoaded(key, fileQ.data.content_text);
    }
  }, [fileQ.data, key, setLoaded]);

  const writeMut = useWriteWorkspaceFile(root);

  const save = (mode: 'stage' | 'direct') => {
    if (!activePath || buffer === undefined || writeMut.isPending) return;
    const opLabel = mode === 'stage' ? '暂存写入' : '直接写入';
    writeMut.mutate(
      { path: activePath, contentText: buffer, mode },
      {
        onSuccess: () => {
          markSaved(key, buffer);
          addAudit({ op: opLabel, path: activePath, ok: true });
          toast.success(mode === 'stage' ? `已保存到暂存区：${activePath}` : `已写入工作树：${activePath}`);
        },
        onError: (err) => {
          const message = err instanceof Error ? err.message : String(err);
          addAudit({ op: opLabel, path: activePath, ok: false, detail: message });
          addProblem({
            severity: 'error',
            message: message.includes('blocked by guard') ? `守卫拦截：${message}` : `保存失败：${message}`,
            path: activePath,
            source: 'workspace',
          });
          toast.error(`保存失败：${message}`);
        },
      },
    );
  };

  // Closing a dirty tab asks for confirmation via dialog (not window.confirm).
  const [confirmClosePath, setConfirmClosePath] = useState<string | null>(null);

  const tryClose = (path: string) => {
    const k = bufKey(projectId, path);
    const st = useEditorStore.getState();
    const isDirty = st.buffers[k] !== undefined && st.buffers[k] !== st.baselines[k];
    if (isDirty) {
      setConfirmClosePath(path);
      return;
    }
    closeFile(projectId, path);
  };

  // Editor QoL shortcuts: mod+W closes the active tab, mod+Alt+←/→ cycles.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!isModEvent(e)) return;
      const st = useEditorStore.getState();
      const curTabs = st.tabsByProject[projectId] ?? [];
      const cur = st.activeByProject[projectId];
      if (curTabs.length === 0) return;
      if (e.key.toLowerCase() === 'w' && !e.altKey && !e.shiftKey) {
        if (!cur) return;
        e.preventDefault();
        tryClose(cur);
        return;
      }
      if (e.altKey && (e.key === 'ArrowLeft' || e.key === 'ArrowRight')) {
        e.preventDefault();
        const idx = Math.max(0, curTabs.findIndex((t) => t.path === cur));
        const next =
          curTabs[(idx + (e.key === 'ArrowRight' ? 1 : curTabs.length - 1)) % curTabs.length];
        if (next) setActive(projectId, next.path);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
    // tryClose reads fresh store state, so projectId is the only real dep.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId]);

  // ---- editor body scroll-sync gutter ----
  const taRef = useRef<HTMLTextAreaElement>(null);
  const [scrollTop, setScrollTop] = useState(0);
  const [hasSelection, setHasSelection] = useState(false);
  const lineCount = (buffer ?? '').split('\n').length;

  const syncSelection = () => {
    const ta = taRef.current;
    setHasSelection(!!ta && ta.selectionStart !== ta.selectionEnd);
  };

  // Quote the current selection into the next chat message (path:Lx-Ly + snippet).
  const quoteSelection = () => {
    const ta = taRef.current;
    if (!ta || !activePath || buffer === undefined) return;
    const { selectionStart, selectionEnd } = ta;
    if (selectionStart === selectionEnd) return;
    const startLine = buffer.slice(0, selectionStart).split('\n').length;
    const endLine = buffer.slice(0, selectionEnd).split('\n').length;
    useChatStore.getState().setQuote({
      path: activePath,
      startLine,
      endLine,
      snippet: buffer.slice(selectionStart, selectionEnd).slice(0, 2000),
    });
    toast.success(`已引用 ${activePath}:L${startLine}-L${endLine} 到对话`);
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
      e.preventDefault();
      save('stage');
      return;
    }
    if (e.key === 'Tab') {
      e.preventDefault();
      const ta = e.currentTarget;
      const { selectionStart, selectionEnd, value } = ta;
      const next = value.slice(0, selectionStart) + '  ' + value.slice(selectionEnd);
      setBuffer(key, next);
      requestAnimationFrame(() => {
        ta.selectionStart = ta.selectionEnd = selectionStart + 2;
      });
    }
  };

  if (tabs.length === 0) {
    return (
      <div className="card grid h-full place-items-center rounded-[var(--cf-radius-card)] border border-[var(--elev-rest-border)] bg-panel shadow-cf-rest">
        <EmptyState
          icon={<FileCode2 size={20} />}
          title="没有打开的文件"
          description="从左侧文件树选择文件开始编辑。Ctrl+S 保存到暂存区，经守卫检查后可应用到工作树。"
        />
      </div>
    );
  }

  return (
    <div className="card flex h-full flex-col overflow-hidden rounded-[var(--cf-radius-card)] border border-[var(--elev-rest-border)] bg-panel shadow-cf-rest">
      {/* Tab strip */}
      <div role="tablist" className="flex shrink-0 items-center gap-0.5 overflow-x-auto border-b border-line px-1.5 pt-1.5">
        <AnimatePresence initial={false} mode="popLayout">
          {tabs.map((t) => {
            const k = bufKey(projectId, t.path);
            const tabDirty = buffers[k] !== undefined && buffers[k] !== baselines[k];
            const tabStaged = stagedPaths.has(t.path);
            const isActive = t.path === activePath;
            return (
              <motion.div
                key={t.path}
                layout
                initial={{ opacity: 0, y: 4 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, scale: 0.96 }}
                transition={{ duration: 0.15, ease: EASE_FLOW }}
                role="tab"
                aria-selected={isActive}
                tabIndex={0}
                onClick={() => setActive(projectId, t.path)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') setActive(projectId, t.path);
                }}
                onAuxClick={(e) => {
                  // Middle-click closes the tab.
                  if (e.button === 1) {
                    e.preventDefault();
                    tryClose(t.path);
                  }
                }}
                title={t.path}
                className={cn(
                  'group relative flex shrink-0 cursor-pointer items-center gap-1.5 rounded-t-lg border border-b-0 px-2.5 py-1.5 text-[12px] transition-colors',
                  isActive
                    ? 'border-line bg-base text-ink'
                    : 'border-transparent text-ink-mute hover:bg-tint hover:text-ink-dim',
                )}
              >
                <span className="max-w-36 truncate">{t.name}</span>
                {/* Dirty dot flips into a close × on hover; staged shows amber. */}
                {tabDirty ? (
                  <span className="relative grid size-3.5 shrink-0 place-items-center">
                    <span
                      className="size-1.5 rounded-full bg-ink transition-opacity group-hover:opacity-0"
                      title="未保存修改"
                    />
                    <button
                      type="button"
                      aria-label={`关闭 ${t.name}`}
                      onClick={(e) => {
                        e.stopPropagation();
                        tryClose(t.path);
                      }}
                      className="absolute inset-0 grid place-items-center rounded text-ink-mute opacity-0 transition-opacity hover:bg-tint-active hover:text-ink group-hover:opacity-100 focus-visible:opacity-100"
                    >
                      <X size={12} />
                    </button>
                  </span>
                ) : (
                  <>
                    {tabStaged && <span className="size-1.5 shrink-0 rounded-full bg-warn" title="已暂存" />}
                    <button
                      type="button"
                      aria-label={`关闭 ${t.name}`}
                      onClick={(e) => {
                        e.stopPropagation();
                        tryClose(t.path);
                      }}
                      className="rounded p-0.5 text-ink-mute opacity-0 transition-opacity hover:bg-tint-active hover:text-ink group-hover:opacity-100 focus-visible:opacity-100"
                    >
                      <X size={12} />
                    </button>
                  </>
                )}
                {isActive && (
                  <motion.span
                    layoutId="editor-tab-active"
                    transition={{ duration: 0.25, ease: EASE_FLOW }}
                    className="absolute inset-x-1 -bottom-px h-0.5 rounded-full bg-ink"
                  />
                )}
              </motion.div>
            );
          })}
        </AnimatePresence>
      </div>

      {/* Breadcrumb + actions */}
      {activePath && (
        <div className="flex h-9 shrink-0 items-center justify-between gap-3 border-b border-line px-3">
          <div className="flex min-w-0 items-center gap-1 text-xs text-ink-mute">
            {activePath.split('/').map((seg, i, arr) => (
              <span key={i} className="flex items-center gap-1">
                <span className={cn('truncate', i === arr.length - 1 && 'text-ink-dim font-medium')}>{seg}</span>
                {i < arr.length - 1 && <ChevronRight size={11} className="shrink-0" />}
              </span>
            ))}
            {isStaged && <Badge tone="warn" className="ml-1.5">暂存副本</Badge>}
            {fileQ.data && (
              <span className="ml-1.5 hidden shrink-0 border-l border-line pl-2 lg:inline">
                {relTime(fileQ.data.mod_time)}
              </span>
            )}
          </div>
          <div className="flex shrink-0 items-center gap-1.5">
            {dirty && <span className="text-xs text-ink-mute">未保存</span>}
            <Button
              variant="ghost"
              size="sm"
              className="h-7 px-2 text-[12px]"
              disabled={!hasSelection}
              onClick={quoteSelection}
              title="把选中的代码作为引用附到下一条对话消息"
            >
              <MessageSquareQuote size={13} /> 引用选区
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="h-7 px-2 text-[12px]"
              disabled={!dirty || writeMut.isPending}
              onClick={() => save('direct')}
              title="绕过暂存区直接写入工作树（仍经守卫检查）"
            >
              <FilePen size={13} /> 直接写入
            </Button>
            <Button
              variant="primary"
              size="sm"
              className="h-7 px-2.5 text-[12px]"
              disabled={!dirty}
              loading={writeMut.isPending}
              onClick={() => save('stage')}
              title="保存到暂存区（Ctrl+S）"
            >
              <Save size={13} /> 暂存保存
            </Button>
          </div>
        </div>
      )}

      {/* Editor body */}
      <div className="flex min-h-0 flex-1 overflow-hidden bg-base">
        {activePath && buffer === undefined ? (
          fileQ.isError ? (
            <EmptyState
              className="mx-auto"
              icon={<Layers size={20} />}
              title="读取失败"
              description={fileQ.error instanceof Error ? fileQ.error.message : '文件读取失败'}
              action={
                <Button variant="secondary" size="sm" onClick={() => fileQ.refetch()}>
                  重试
                </Button>
              }
            />
          ) : (
            <div className="flex-1 space-y-2 p-4">
              {['w-4/5', 'w-3/5', 'w-11/12', 'w-2/3', 'w-3/4', 'w-1/2', 'w-5/6', 'w-2/5', 'w-3/4', 'w-1/3'].map(
                (w, i) => (
                  <Skeleton key={i} className={cn('h-4', w)} />
                ),
              )}
            </div>
          )
        ) : (
          <>
            <div aria-hidden className="relative w-11 shrink-0 select-none overflow-hidden border-r border-line bg-panel">
              <div
                className="nums py-2 pr-2 text-right font-mono text-[13px] leading-5 text-ink-mute/70"
                style={{ transform: `translateY(-${scrollTop}px)` }}
              >
                {Array.from({ length: lineCount }).map((_, i) => (
                  <div key={i}>{i + 1}</div>
                ))}
              </div>
            </div>
            <textarea
              ref={taRef}
              value={buffer ?? ''}
              onChange={(e) => setBuffer(key, e.target.value)}
              onScroll={(e) => setScrollTop(e.currentTarget.scrollTop)}
              onKeyDown={handleKeyDown}
              onSelect={syncSelection}
              onKeyUp={syncSelection}
              onMouseUp={syncSelection}
              spellCheck={false}
              autoCapitalize="off"
              autoCorrect="off"
              wrap="off"
              aria-label={`编辑 ${activePath ?? ''}`}
              className="flex-1 resize-none overflow-auto whitespace-pre bg-transparent px-3 py-2 font-mono text-[13px] leading-5 text-ink outline-none"
            />
          </>
        )}
      </div>

      {/* Dirty-close confirmation */}
      <Dialog open={confirmClosePath != null} onOpenChange={(open) => !open && setConfirmClosePath(null)}>
        <DialogContent className="w-[min(92vw,400px)]">
          <DialogTitle>关闭未保存的文件</DialogTitle>
          <DialogDescription>
            「{confirmClosePath?.split('/').pop()}」有未保存的修改，关闭后将丢失这些修改。
          </DialogDescription>
          <div className="mt-5 flex justify-end gap-2">
            <Button variant="ghost" size="sm" onClick={() => setConfirmClosePath(null)}>
              取消
            </Button>
            <Button
              variant="danger"
              size="sm"
              onClick={() => {
                if (confirmClosePath) closeFile(projectId, confirmClosePath);
                setConfirmClosePath(null);
              }}
            >
              放弃修改并关闭
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
