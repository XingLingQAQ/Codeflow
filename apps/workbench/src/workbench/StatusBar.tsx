import { cn } from '../lib/cn';
import { useShellStore } from '../stores/shell';

interface StatusBarProps {
  stage?: string;
  connected?: boolean;
}

export function StatusBar({ stage, connected }: StatusBarProps) {
  const mock = useShellStore((s) => s.mock);

  return (
    <footer className="flex h-6 shrink-0 items-center justify-between border-t border-line bg-panel px-3 text-[11px] text-ink-mute">
      <div className="flex items-center gap-4">
        <span>{stage ?? '—'}</span>
        <span className="border-l border-line pl-4">模型 —</span>
        <span className="border-l border-line pl-4">Token —</span>
      </div>
      <div className="flex items-center gap-4">
        {import.meta.env.DEV && mock && (
          <span className="rounded bg-warn/15 px-1.5 py-px font-mono text-[10px] font-bold text-warn">MOCK</span>
        )}
        <span>快照 —</span>
        <span className="flex items-center gap-1.5">
          守卫 <span className="inline-block size-1.5 rounded-full bg-success" />
        </span>
        <span className="flex items-center gap-1.5">
          <span className={cn('inline-block size-1.5 rounded-full', connected ? 'bg-success' : 'bg-danger')} />
          {connected ? '已连接' : '离线'}
        </span>
      </div>
    </footer>
  );
}
