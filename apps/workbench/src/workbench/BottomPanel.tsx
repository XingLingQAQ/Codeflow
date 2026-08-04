import { useMemo, useState } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import { toast } from 'sonner';
import {
  Terminal,
  ScrollText,
  History,
  AlertTriangle,
  Shield,
  CheckCircle2,
  Play,
  Workflow,
  FolderSync,
  MessageSquare,
  Check,
  X,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { Tabs, TabsList, TabsTrigger, TabsContent, Badge, Skeleton, IconButton, Tooltip } from '../ui';
import { useLayoutStore, type BottomTab } from '../stores/layout';
import { useEditorStore } from '../stores/editor';
import { useActivityStore, nextActivityId, type ActivityEntry, type ActivityKind } from '../stores/activity';
import {
  useProjectFlow,
  useGuardRules,
  useWorkspaceScripts,
  useExemptionRequests,
  useDecideExemption,
} from '../lib/queries';
import type { ExemptionRequestRecord } from '../services-bridge/guard';
import type { StageType } from '../services-bridge/flows';
import { relTime } from '../lib/format';
import { cn } from '../lib/cn';
import { EASE_FLOW } from '../lib/motion';

const TAB_META: { value: BottomTab; label: string; icon: LucideIcon }[] = [
  { value: 'terminal', label: '终端', icon: Terminal },
  { value: 'logs', label: '日志', icon: ScrollText },
  { value: 'audit', label: '审计', icon: History },
  { value: 'problems', label: '问题', icon: AlertTriangle },
  { value: 'guard', label: '守卫', icon: Shield },
];

/** Stage-linked default bottom tab (workbench-and-shell §2 阶段联动默认页). */
export const STAGE_DEFAULT_TAB: Partial<Record<StageType, BottomTab>> = {
  coding: 'terminal',
  review: 'problems',
  submit: 'audit',
};

function PanelHint({ icon: Icon, text }: { icon: LucideIcon; text: string }) {
  return (
    <div className="flex h-full items-center justify-center gap-2 text-[12px] leading-relaxed text-ink-mute">
      <Icon size={14} className="text-ink-mute/60" />
      {text}
    </div>
  );
}

function TerminalTab({ root }: { root: string }) {
  const scriptsQ = useWorkspaceScripts(root, !!root);
  if (!root) {
    return <PanelHint icon={Terminal} text="在编码画布加载工作区后，此处显示可用的 package.json 脚本" />;
  }
  if (scriptsQ.isLoading) {
    return (
      <div className="space-y-2 p-3">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} className="h-6 w-2/3" />
        ))}
      </div>
    );
  }
  if (scriptsQ.isError) {
    return <PanelHint icon={Terminal} text="脚本探测失败：工作区服务未就绪" />;
  }
  const scripts = scriptsQ.data?.items ?? [];
  if (scripts.length === 0) {
    return <PanelHint icon={Terminal} text="未在工作区根目录检测到 package.json 脚本" />;
  }
  return (
    <div className="flex h-full flex-col">
      <ul className="min-h-0 flex-1 space-y-0.5 overflow-y-auto p-2">
        {scripts.map((s) => (
          <li key={s.name} className="flex items-center gap-3 rounded-md px-2 py-1.5 hover:bg-tint">
            <button
              type="button"
              disabled
              title="Dev Server 进程管理将于 Live Preview（M6）启用"
              className="grid size-5 shrink-0 place-items-center rounded text-ink-mute/50"
            >
              <Play size={12} />
            </button>
            <Badge tone="neutral" className="shrink-0 font-mono">
              {s.name}
            </Badge>
            <code className="truncate font-mono text-[12px] text-ink-dim">{s.command}</code>
          </li>
        ))}
      </ul>
      <p className="shrink-0 border-t border-line px-3 py-1.5 text-xs leading-relaxed text-ink-mute">
        已探测 {scripts.length} 个脚本 · 启动/停止与日志接管将随 Live Preview（M6）交付
      </p>
    </div>
  );
}

// ---- Realtime activity timeline (P1: 事件流) ----

const KIND_META: Record<ActivityKind, { label: string; icon: LucideIcon }> = {
  flow: { label: '流程', icon: Workflow },
  workspace: { label: '工作区', icon: FolderSync },
  guard: { label: '守卫', icon: Shield },
  chat: { label: '对话', icon: MessageSquare },
};

function dotClass(title: string): string {
  if (title.startsWith('gate.')) return 'bg-warn';
  if (title.includes('abort')) return 'bg-danger';
  if (title === 'stage.done' || title === 'stage_done' || title === 'flow.completed') return 'bg-success';
  return 'bg-ink-mute/70';
}

function LogsTab({ projectId }: { projectId?: string }) {
  const { flow, isLoading } = useProjectFlow(projectId);
  const activity = useActivityStore((s) => s.entries);
  const [kindFilter, setKindFilter] = useState<'all' | ActivityKind>('all');
  const [stageFilter, setStageFilter] = useState<string>('all');

  const stageName = useMemo(() => {
    const map = new Map<string, string>();
    for (const s of flow?.stages ?? []) map.set(s.id, s.name);
    return map;
  }, [flow?.stages]);

  // Live entries merged with the flow's static event list (offline fallback),
  // deduped by event id — one flow:project subscription feeds everything.
  const merged = useMemo(() => {
    const fromFlow: ActivityEntry[] = (flow?.events ?? []).map((ev) => ({
      id: ev.id,
      kind: 'flow' as const,
      title: ev.type,
      detail: ev.message,
      stageId: ev.stage_id,
      at: ev.timestamp,
    }));
    const seen = new Set<string>();
    const all: ActivityEntry[] = [];
    for (const e of [...activity, ...fromFlow]) {
      if (seen.has(e.id)) continue;
      seen.add(e.id);
      all.push(e);
    }
    return all.sort((a, b) => (Date.parse(b.at) || 0) - (Date.parse(a.at) || 0));
  }, [activity, flow?.events]);

  const filtered = merged.filter(
    (e) =>
      (kindFilter === 'all' || e.kind === kindFilter) &&
      (stageFilter === 'all' || e.stageId === stageFilter),
  );

  if (isLoading && merged.length === 0) {
    return (
      <div className="space-y-2 p-3">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} className="h-5 w-3/4" />
        ))}
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex shrink-0 flex-wrap items-center gap-1 border-b border-line px-2 py-1">
        {(['all', 'flow', 'workspace', 'guard', 'chat'] as const).map((k) => (
          <button
            key={k}
            type="button"
            onClick={() => setKindFilter(k)}
            className={cn(
              'rounded-md px-2 py-0.5 text-[11px] transition-colors',
              kindFilter === k ? 'bg-tint-active text-ink' : 'text-ink-mute hover:text-ink',
            )}
          >
            {k === 'all' ? '全部' : KIND_META[k].label}
          </button>
        ))}
        {flow && flow.stages.length > 0 && (
          <select
            value={stageFilter}
            onChange={(e) => setStageFilter(e.target.value)}
            aria-label="按阶段筛选"
            className="ml-auto rounded-md border border-line bg-raised px-1.5 py-0.5 text-[11px] text-ink-dim outline-none"
          >
            <option value="all">全部阶段</option>
            {flow.stages.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
        )}
      </div>

      {filtered.length === 0 ? (
        <PanelHint icon={ScrollText} text="暂无匹配事件 — 阶段推进、Gate 决策与文件变更将实时记录在此" />
      ) : (
        <ul className="relative min-h-0 flex-1 overflow-y-auto py-1 pl-6 pr-2">
          {/* 2px timeline rail */}
          <span aria-hidden className="absolute bottom-0 left-[15px] top-0 w-0.5 bg-line" />
          <AnimatePresence initial={false}>
            {filtered.map((e) => {
              const KindIcon = KIND_META[e.kind].icon;
              return (
                <motion.li
                  key={e.id}
                  layout="position"
                  initial={{ opacity: 0, y: -4 }}
                  animate={{ opacity: 1, y: 0 }}
                  exit={{ opacity: 0 }}
                  transition={{ duration: 0.15, ease: EASE_FLOW }}
                  className="relative flex items-center gap-2.5 rounded-md py-1 pl-2 pr-1 text-[12px] hover:bg-tint"
                >
                  <span
                    aria-hidden
                    className={cn(
                      'absolute -left-[13.5px] top-1/2 size-[7px] -translate-y-1/2 rounded-full ring-2 ring-[var(--color-panel)]',
                      dotClass(e.title),
                    )}
                  />
                  <KindIcon size={12} className="shrink-0 text-ink-mute" />
                  <span className="nums w-16 shrink-0 font-mono text-[11px] text-ink-mute">{relTime(e.at)}</span>
                  <Badge tone={e.title.includes('abort') ? 'danger' : e.title.startsWith('gate.') ? 'warn' : 'neutral'} className="shrink-0 font-mono">
                    {e.title}
                  </Badge>
                  {e.stageId && stageName.get(e.stageId) && (
                    <span className="shrink-0 text-[11px] text-ink-mute">{stageName.get(e.stageId)}</span>
                  )}
                  <span className="truncate text-ink-dim">{e.detail}</span>
                </motion.li>
              );
            })}
          </AnimatePresence>
        </ul>
      )}
    </div>
  );
}

function AuditTab() {
  const audit = useEditorStore((s) => s.audit);
  if (audit.length === 0) {
    return <PanelHint icon={History} text="本次会话尚无写操作 — 暂存/写入/应用/丢弃将记录在此" />;
  }
  return (
    <ul className="h-full space-y-0.5 overflow-y-auto p-2">
      {audit.map((e) => (
        <li key={e.id} className="flex items-center gap-3 rounded-md px-2 py-1.5 text-[12px] hover:bg-tint">
          <span className="nums w-16 shrink-0 font-mono text-[11px] text-ink-mute">{relTime(e.time)}</span>
          <Badge tone={e.ok ? 'success' : 'danger'} className="shrink-0">
            {e.op}
          </Badge>
          <span className="truncate font-mono text-[12px] text-ink-dim">{e.path}</span>
          {e.detail && <span className="truncate text-[11px] text-ink-mute">{e.detail}</span>}
        </li>
      ))}
    </ul>
  );
}

function ProblemsTab() {
  const problems = useEditorStore((s) => s.problems);
  const clearProblems = useEditorStore((s) => s.clearProblems);
  if (problems.length === 0) {
    return (
      <div className="flex h-full items-center justify-center gap-2 text-[12px] text-ink-mute">
        <CheckCircle2 size={14} className="text-success/70" />
        没有检测到问题
      </div>
    );
  }
  return (
    <div className="flex h-full flex-col">
      <div className="flex shrink-0 items-center justify-between border-b border-line px-3 py-1">
        <span className="nums text-[11px] text-ink-mute">{problems.length} 个问题</span>
        <button
          type="button"
          onClick={clearProblems}
          className="text-[11px] text-ink-mute transition-colors hover:text-ink"
        >
          清空
        </button>
      </div>
      <ul className="min-h-0 flex-1 space-y-0.5 overflow-y-auto p-2">
        <AnimatePresence initial={false} mode="popLayout">
          {problems.map((p) => (
            <motion.li
              key={p.id}
              layout
              initial={{ opacity: 0, y: -4 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, scale: 0.96 }}
              transition={{ duration: 0.15, ease: EASE_FLOW }}
              className="flex items-center gap-3 rounded-md px-2 py-1.5 text-[12px] hover:bg-tint"
            >
              <Badge tone={p.severity === 'error' ? 'danger' : 'warn'} className="shrink-0">
                {p.severity}
              </Badge>
              <span className="truncate text-ink-dim" title={p.message}>
                {p.message}
              </span>
              {p.path && <span className="ml-auto shrink-0 font-mono text-[11px] text-ink-mute">{p.path}</span>}
            </motion.li>
          ))}
        </AnimatePresence>
      </ul>
    </div>
  );
}

// ---- Guard: rules + policy + exemption approvals (P1-6) ----

function ExemptionRow({
  req,
  onDecide,
  pending,
}: {
  req: ExemptionRequestRecord;
  onDecide: (decision: 'approve' | 'reject') => void;
  pending: boolean;
}) {
  return (
    <motion.li
      layout
      initial={{ opacity: 0, y: -4 }}
      animate={{ opacity: 1, y: 0 }}
      exit={{ opacity: 0, x: 12 }}
      transition={{ duration: 0.15, ease: EASE_FLOW }}
      className="rounded-lg border border-line bg-raised px-2.5 py-2"
    >
      <div className="flex items-center gap-2">
        <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-ink" title={req.path}>
          {req.path}
        </span>
        {req.rule_id && (
          <Badge tone="warn" className="shrink-0 px-1.5 py-px font-mono text-[11px]">
            {req.rule_id}
          </Badge>
        )}
      </div>
      <p className="mt-1 line-clamp-2 text-xs leading-relaxed text-ink-dim" title={req.reason}>
        {req.reason}
      </p>
      <div className="mt-1.5 flex items-center justify-between gap-2">
        <span className="truncate text-2xs text-ink-mute">
          {req.requester} · {relTime(req.created_at)}
        </span>
        <span className="flex shrink-0 gap-1">
          <Tooltip content="批准豁免" side="top">
            <IconButton size="sm" aria-label={`批准 ${req.path} 的豁免`} disabled={pending} onClick={() => onDecide('approve')}>
              <Check size={13} className="text-success" />
            </IconButton>
          </Tooltip>
          <Tooltip content="驳回豁免" side="top">
            <IconButton size="sm" aria-label={`驳回 ${req.path} 的豁免`} disabled={pending} onClick={() => onDecide('reject')}>
              <X size={13} className="text-danger" />
            </IconButton>
          </Tooltip>
        </span>
      </div>
    </motion.li>
  );
}

function GuardTab() {
  const guardQ = useGuardRules(true);
  const exemptQ = useExemptionRequests('pending');
  const decideMut = useDecideExemption();
  const addAudit = useEditorStore((s) => s.addAudit);

  const decide = (req: ExemptionRequestRecord, decision: 'approve' | 'reject') => {
    decideMut.mutate(
      { id: req.id, decision },
      {
        onSuccess: () => {
          const op = decision === 'approve' ? '豁免批准' : '豁免驳回';
          addAudit({ op, path: req.path, ok: true, detail: req.rule_id });
          useActivityStore.getState().push({
            id: nextActivityId('guard'),
            kind: 'guard',
            title: decision === 'approve' ? 'guard.exemption_approved' : 'guard.exemption_rejected',
            detail: req.path,
            at: new Date().toISOString(),
          });
          toast.success(`${op}：${req.path}`);
        },
        onError: (err) =>
          toast.error(`豁免决策失败：${err instanceof Error ? err.message : String(err)}`),
      },
    );
  };

  if (guardQ.isLoading) {
    return (
      <div className="space-y-2 p-3">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} className="h-5 w-1/2" />
        ))}
      </div>
    );
  }
  if (guardQ.isError) {
    return <PanelHint icon={Shield} text="守卫服务未就绪（实验特性）" />;
  }
  const rules = guardQ.data?.items ?? [];
  const globs = guardQ.data?.denied_path_globs ?? [];
  const maxBytes = guardQ.data?.max_file_bytes;
  const pendingReqs = exemptQ.data?.items ?? [];

  return (
    <div className="grid h-full grid-cols-1 gap-x-6 gap-y-3 overflow-y-auto p-3 md:grid-cols-3">
      <section>
        <h4 className="mb-1.5 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wide text-ink-mute">
          豁免审批
          {pendingReqs.length > 0 && <Badge tone="warn" className="nums">{pendingReqs.length}</Badge>}
        </h4>
        {exemptQ.isError ? (
          <p className="text-[12px] text-ink-mute">豁免审批服务未就绪</p>
        ) : pendingReqs.length === 0 ? (
          <p className="text-[12px] leading-relaxed text-ink-mute">暂无待审批的豁免请求。</p>
        ) : (
          <ul className="space-y-1.5">
            <AnimatePresence initial={false} mode="popLayout">
              {pendingReqs.map((req) => (
                <ExemptionRow
                  key={req.id}
                  req={req}
                  pending={decideMut.isPending}
                  onDecide={(d) => decide(req, d)}
                />
              ))}
            </AnimatePresence>
          </ul>
        )}
      </section>
      <section>
        <h4 className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-ink-mute">规则</h4>
        <ul className="space-y-1">
          {rules.map((r) => (
            <li key={r.id} className="flex items-center justify-between gap-3 text-[12px]">
              <span className="truncate font-mono text-ink-dim">{r.id}</span>
              <Badge tone={r.severity === 'error' ? 'danger' : r.severity === 'warn' ? 'warn' : 'neutral'}>
                {r.severity}
              </Badge>
            </li>
          ))}
          {rules.length === 0 && <li className="text-[12px] text-ink-mute">暂无规则</li>}
        </ul>
      </section>
      <section>
        <h4 className="mb-1.5 text-[11px] font-semibold uppercase tracking-wide text-ink-mute">策略</h4>
        {globs.length > 0 && (
          <div className="flex flex-wrap gap-1.5">
            {globs.map((g) => (
              <code key={g} className="rounded bg-tint px-1.5 py-0.5 font-mono text-[11px] text-ink-dim">
                {g}
              </code>
            ))}
          </div>
        )}
        {maxBytes != null && (
          <p className="mt-2 text-[12px] leading-relaxed text-ink-mute">
            单文件写入上限 <span className="nums font-mono text-ink-dim">{Math.round(maxBytes / 1024)} KB</span>
          </p>
        )}
        <p className="mt-2 text-xs leading-relaxed text-ink-mute">
          所有写操作（含暂存应用）均经守卫检查；被拦截的操作会出现在「问题」页，豁免决策会写入「审计」页。
        </p>
      </section>
    </div>
  );
}

export interface BottomPanelProps {
  projectId?: string;
}

export function BottomPanel({ projectId }: BottomPanelProps) {
  const tab = useLayoutStore((s) => s.bottomTab);
  const setTab = useLayoutStore((s) => s.setBottomTab);
  const problemCount = useEditorStore((s) => s.problems.length);
  const root = useEditorStore((s) => (projectId ? s.rootByProject[projectId] : undefined)) ?? '';

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
                  {t.value === 'problems' && problemCount > 0 && (
                    <span
                      className={cn(
                        'nums ml-0.5 grid min-w-4 place-items-center rounded-full bg-danger/15 px-1',
                        'font-mono text-[10px] font-bold text-danger',
                      )}
                    >
                      {problemCount}
                    </span>
                  )}
                </TabsTrigger>
              );
            })}
          </TabsList>
        </div>
        <TabsContent value="terminal" className="min-h-0 flex-1 overflow-hidden">
          <TerminalTab root={root} />
        </TabsContent>
        <TabsContent value="logs" className="min-h-0 flex-1 overflow-hidden">
          <LogsTab projectId={projectId} />
        </TabsContent>
        <TabsContent value="audit" className="min-h-0 flex-1 overflow-hidden">
          <AuditTab />
        </TabsContent>
        <TabsContent value="problems" className="min-h-0 flex-1 overflow-hidden">
          <ProblemsTab />
        </TabsContent>
        <TabsContent value="guard" className="min-h-0 flex-1 overflow-hidden">
          <GuardTab />
        </TabsContent>
      </Tabs>
    </div>
  );
}
