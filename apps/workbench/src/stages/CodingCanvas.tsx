import { useEffect, useMemo, useState } from 'react';
import { useParams } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { motion } from 'motion/react';
import { FolderTree, Braces, Shield, FolderInput, RotateCcw } from 'lucide-react';
import { Card, Button, Input, Badge } from '../ui';
import { useGuardRules, useStagedList, qk } from '../lib/queries';
import { useEditorStore } from '../stores/editor';
import { useActivityStore } from '../stores/activity';
import { isDevMockActive } from '../lib/devMock';
import { createWorkspaceWatch, deleteWorkspaceWatch } from '../services-bridge/workspace';
import { subscribe as wsSubscribe } from '../services-bridge/ws';
import { FileTree } from '../workbench/FileTree';
import { EditorPane } from '../workbench/EditorPane';
import { StagedPanel } from '../workbench/StagedPanel';
import { staggerContainer, staggerItem } from '../lib/motion';

/** Rough token estimate: ~4 bytes per token. */
const TOKEN_BUDGET = 32_000;

/**
 * Last created workspace watch, module-scoped so it survives canvas remounts.
 * The backend watch API is idempotent per resolved root; we only DELETE when
 * the root actually changes (watch limit is 16 per process).
 */
let activeWatch: { root: string; id: string } | null = null;

function fmtTokens(n: number): string {
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`;
  return String(n);
}

function RootPicker({ onLoad }: { onLoad: (root: string) => void }) {
  const [value, setValue] = useState('');
  return (
    <div className="space-y-2 py-2">
      <p className="text-[12px] leading-relaxed text-ink-dim">
        输入项目根目录的绝对路径以加载文件树（路径白名单限制在该目录内）。
      </p>
      <Input
        value={value}
        onChange={(e) => setValue(e.target.value)}
        placeholder="D:\Project\..."
        className="h-8 text-[12px]"
        onKeyDown={(e) => {
          if (e.key === 'Enter' && value.trim()) onLoad(value.trim());
        }}
      />
      <Button variant="secondary" size="sm" onClick={() => onLoad(value.trim())} disabled={!value.trim()}>
        <FolderInput size={13} /> 加载
      </Button>
    </div>
  );
}

export default function CodingCanvas() {
  const { projectId = '' } = useParams<{ projectId: string }>();
  const qc = useQueryClient();

  const root = useEditorStore((s) => s.rootByProject[projectId]) ?? '';
  const setRoot = useEditorStore((s) => s.setRoot);
  const activePath = useEditorStore((s) => s.activeByProject[projectId]);
  const openFile = useEditorStore((s) => s.openFile);
  const contextSelection = useEditorStore((s) => s.contextByProject[projectId]) ?? {};
  const toggleContext = useEditorStore((s) => s.toggleContext);
  const clearContext = useEditorStore((s) => s.clearContext);

  // Dev-mock: auto-attach a virtual workspace root so the canvas is usable
  // without a backend. Compiles to a no-op in production builds.
  useEffect(() => {
    if (projectId && !root && isDevMockActive()) {
      setRoot(projectId, `/mock/${projectId}`);
    }
  }, [projectId, root, setRoot]);

  // Realtime workspace watch: POST /workspace/watch, subscribe to the per-root
  // topic from the response, and refresh the tree + staged list on external
  // edits. The previous watch is deleted only when the root changes.
  useEffect(() => {
    if (!root || isDevMockActive()) return;
    let cancelled = false;
    let unsubscribe: (() => void) | undefined;
    (async () => {
      try {
        if (activeWatch && activeWatch.root !== root) {
          await deleteWorkspaceWatch(activeWatch.id).catch(() => {});
          activeWatch = null;
        }
        const watch = await createWorkspaceWatch(root);
        if (cancelled) return;
        activeWatch = { root, id: watch.watch_id };
        unsubscribe = wsSubscribe(watch.topic, (frame) => {
          qc.invalidateQueries({ queryKey: ['workspace', root] });
          qc.invalidateQueries({ queryKey: ['workspace-file', root] });
          qc.invalidateQueries({ queryKey: qk.staged(root) });
          const d = frame.data ?? {};
          useActivityStore.getState().push({
            id: `ws-${String(d.path ?? '')}-${String(d.timestamp ?? Date.now())}`,
            kind: 'workspace',
            title: String(d.change ?? frame.content ?? 'change'),
            detail: typeof d.path === 'string' ? d.path : undefined,
            at: typeof d.timestamp === 'string' ? d.timestamp : new Date().toISOString(),
          });
        });
      } catch {
        // Watching is best-effort; manual refresh still works without it.
      }
    })();
    return () => {
      cancelled = true;
      unsubscribe?.();
    };
  }, [root, qc]);

  const guardQ = useGuardRules(true);
  const rules = guardQ.data?.items ?? [];
  const deniedGlobs = guardQ.data?.denied_path_globs ?? [];

  const stagedQ = useStagedList(root, !!root);
  const stagedPaths = useMemo(
    () => new Set((stagedQ.data?.items ?? []).map((e) => e.path)),
    [stagedQ.data],
  );

  const ctxEntries = Object.entries(contextSelection);
  const ctxTokens = Math.round(ctxEntries.reduce((sum, [, size]) => sum + size, 0) / 4);
  const ctxPct = Math.min(100, (ctxTokens / TOKEN_BUDGET) * 100);
  const overBudget = ctxTokens > TOKEN_BUDGET;

  return (
    <motion.div
      variants={staggerContainer}
      initial="initial"
      animate="animate"
      className="flex h-full gap-3 p-3"
    >
      {/* Left: file tree */}
      <motion.aside variants={staggerItem} className="w-60 shrink-0">
        <Card className="flex h-full flex-col overflow-hidden">
          <div className="flex h-9 shrink-0 items-center gap-2 border-b border-line px-3">
            <FolderTree size={13} className="shrink-0 text-ink-mute" />
            <span className="text-[12px] font-medium text-ink-dim">文件树</span>
            {root && (
              <>
                <span className="min-w-0 flex-1 truncate text-right font-mono text-[10px] text-ink-mute" title={root}>
                  {root}
                </span>
                <button
                  type="button"
                  aria-label="更换工作区根目录"
                  title="更换根目录"
                  onClick={() => setRoot(projectId, '')}
                  className="shrink-0 rounded p-1 text-ink-mute transition-colors hover:bg-tint hover:text-ink"
                >
                  <RotateCcw size={12} />
                </button>
              </>
            )}
          </div>
          <div className="min-h-0 flex-1 overflow-auto p-1.5">
            {!root ? (
              <div className="px-1.5">
                <RootPicker onLoad={(r) => setRoot(projectId, r)} />
              </div>
            ) : (
              <FileTree
                root={root}
                activePath={activePath}
                stagedPaths={stagedPaths}
                contextSelection={contextSelection}
                onOpen={(path) => openFile(projectId, path)}
                onToggleContext={(path, size) => toggleContext(projectId, path, size)}
              />
            )}
          </div>
        </Card>
      </motion.aside>

      {/* Center: multi-tab editor */}
      <motion.main variants={staggerItem} className="min-w-0 flex-1">
        <EditorPane projectId={projectId} root={root} />
      </motion.main>

      {/* Right: context builder + staging area + guard */}
      <motion.aside variants={staggerItem} className="flex w-72 shrink-0 flex-col gap-3 overflow-hidden">
        <Card className="shrink-0 p-3.5">
          <div className="flex items-center justify-between">
            <span className="flex items-center gap-2 text-[12px] font-medium text-ink-dim">
              <Braces size={13} /> 上下文构建器
            </span>
            {ctxEntries.length > 0 && (
              <button
                type="button"
                onClick={() => clearContext(projectId)}
                className="text-[11px] text-ink-mute transition-colors hover:text-ink"
              >
                清空
              </button>
            )}
          </div>
          <div className="mt-3 flex items-center gap-4">
            <div
              className="grid size-[72px] shrink-0 place-items-center rounded-full"
              role="img"
              aria-label={`Token 预算已用 ${Math.round(ctxPct)}%`}
              style={{
                background: `conic-gradient(${overBudget ? 'var(--color-danger)' : 'var(--color-accent)'} ${ctxPct}%, var(--color-tint) 0)`,
              }}
            >
              <div className="grid size-[58px] place-items-center rounded-full bg-panel">
                <span className={`nums font-mono text-[12px] font-semibold ${overBudget ? 'text-danger' : 'text-ink'}`}>
                  {fmtTokens(ctxTokens)}
                </span>
              </div>
            </div>
            <div className="min-w-0 flex-1 space-y-1 text-[11px] leading-relaxed text-ink-mute">
              <p>
                已选 <span className="nums font-medium text-ink-dim">{ctxEntries.length}</span> 个文件
              </p>
              <p>
                预算 <span className="nums font-mono">{fmtTokens(TOKEN_BUDGET)}</span> tokens
              </p>
              <p className="leading-snug">在文件树勾选文件加入 Agent 上下文</p>
            </div>
          </div>
          {ctxEntries.length > 0 && (
            <ul className="mt-2.5 max-h-20 space-y-0.5 overflow-y-auto border-t border-line pt-2">
              {ctxEntries.map(([path]) => (
                <li key={path} className="truncate font-mono text-[11px] text-ink-mute" title={path}>
                  {path}
                </li>
              ))}
            </ul>
          )}
        </Card>

        <Card className="flex min-h-0 flex-1 flex-col overflow-hidden p-3.5">
          <div className="mb-1.5 flex shrink-0 items-center justify-between">
            <span className="text-[12px] font-medium text-ink-dim">暂存区</span>
            {stagedPaths.size > 0 && <Badge tone="warn">{stagedPaths.size}</Badge>}
          </div>
          {root ? (
            <StagedPanel projectId={projectId} root={root} className="min-h-0 flex-1" />
          ) : (
            <p className="text-[12px] text-ink-mute">加载工作区后可查看暂存修改</p>
          )}
        </Card>

        <Card className="shrink-0 p-3.5">
          <div className="flex items-center justify-between">
            <span className="flex items-center gap-2 text-[12px] font-medium text-ink-dim">
              <Shield size={13} /> 守卫
            </span>
            {rules.length > 0 && <span className="text-[11px] text-ink-mute">{rules.length} 条规则</span>}
          </div>
          {guardQ.isError ? (
            <p className="mt-2 text-[12px] text-ink-mute">守卫服务未就绪</p>
          ) : rules.length === 0 ? (
            <p className="mt-2 text-[12px] text-ink-mute">暂无规则</p>
          ) : (
            <div className="mt-2 space-y-1">
              {rules.slice(0, 4).map((r) => (
                <div key={r.id} className="flex items-center justify-between gap-2 text-[12px]">
                  <span className="truncate font-mono text-[11px] text-ink-dim">{r.id}</span>
                  <Badge tone={r.severity === 'error' ? 'danger' : r.severity === 'warn' ? 'warn' : 'neutral'}>
                    {r.severity}
                  </Badge>
                </div>
              ))}
              {deniedGlobs.length > 0 && (
                <p className="border-t border-line pt-1.5 text-[11px] leading-relaxed text-ink-mute">
                  受保护路径：{deniedGlobs.join('、')}
                </p>
              )}
            </div>
          )}
        </Card>
      </motion.aside>
    </motion.div>
  );
}
