import { cn } from '../lib/cn';
import { useShellStore } from '../stores/shell';
import { useEditorStore } from '../stores/editor';
import { useGlobalConfig, useGuardRules, useExemptionRequests } from '../lib/queries';
import type { Stage, StageType } from '../services-bridge/flows';

interface StatusBarProps {
  projectId?: string;
  stage?: string;
  stageSlug?: StageType;
  /** The stage currently shown in the canvas (for its snapshot binding). */
  currentStage?: Stage;
  connected?: boolean;
}

function fmtTokens(n: number): string {
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`;
  return String(n);
}

export function StatusBar({ projectId, stage, stageSlug, currentStage, connected }: StatusBarProps) {
  const mock = useShellStore((s) => s.mock);
  const configQ = useGlobalConfig();
  const guardQ = useGuardRules(true);
  const exemptQ = useExemptionRequests('pending');
  const ctxSelection = useEditorStore((s) => (projectId ? s.contextByProject[projectId] : undefined));

  const model = configQ.data?.default_model;
  const ctxTokens = Math.round(
    Object.values(ctxSelection ?? {}).reduce((sum, size) => sum + size, 0) / 4,
  );
  const snapshotId = currentStage?.snapshot_id;
  const pendingExemptions = exemptQ.data?.items.length ?? 0;

  const guardState: 'off' | 'pending' | 'ok' = guardQ.isError
    ? 'off'
    : pendingExemptions > 0
      ? 'pending'
      : 'ok';

  return (
    <footer className="flex h-6 shrink-0 items-center justify-between border-t border-line bg-panel px-3 text-xs text-ink-mute">
      <div className="flex items-center gap-4">
        <span>{stage ?? '—'}</span>
        {model && <span className="border-l border-line pl-4">模型 {model}</span>}
        {stageSlug === 'coding' && (
          <span className="nums border-l border-line pl-4" title="上下文构建器已选文件的估算 Token">
            Token {fmtTokens(ctxTokens)}
          </span>
        )}
      </div>
      <div className="flex items-center gap-4">
        {import.meta.env.DEV && mock && (
          <span className="rounded bg-warn/15 px-1.5 py-px font-mono text-[10px] font-bold text-warn">MOCK</span>
        )}
        <span className="nums font-mono text-2xs" title={snapshotId ? `阶段快照 ${snapshotId}` : '当前阶段尚未绑定快照'}>
          快照 {snapshotId ? snapshotId.slice(0, 10) : '—'}
        </span>
        <span
          className={cn(
            'flex items-center gap-1.5 rounded-full px-2 py-px',
            guardState === 'pending' && 'bg-warn-soft text-warn ring-1 ring-inset ring-warn/40',
          )}
          title={
            guardState === 'off'
              ? '守卫服务未启用（实验特性）'
              : guardState === 'pending'
                ? `守卫在线 · ${pendingExemptions} 条豁免请求待审批`
                : '守卫在线'
          }
        >
          守卫
          <span
            className={cn(
              'inline-block rounded-full',
              guardState === 'off' && 'size-1.5 bg-ink-mute/50',
              guardState === 'pending' && 'size-2 bg-warn',
              guardState === 'ok' && 'size-1.5 bg-success',
            )}
          />
          {guardState === 'off' && <span>未启用</span>}
          {guardState === 'pending' && <span className="nums font-semibold">{pendingExemptions}</span>}
        </span>
        <span className="flex items-center gap-1.5 px-2">
          <span className={cn('inline-block size-2 rounded-full', connected ? 'bg-success' : 'bg-danger')} />
          {connected ? '已连接' : '离线'}
        </span>
      </div>
    </footer>
  );
}
