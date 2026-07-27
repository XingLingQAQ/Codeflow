import { useNavigate } from 'react-router-dom';
import { FolderKanban, Wifi, WifiOff } from 'lucide-react';
import { FlowProgress } from './FlowProgress';
import type { Stage } from '../services-bridge/flows';
import { cn } from '../lib/cn';

interface TopBarProps {
  projectId: string;
  projectTitle?: string;
  stages?: Stage[];
  connected?: boolean;
}

export function TopBar({ projectId, projectTitle, stages, connected = false }: TopBarProps) {
  const navigate = useNavigate();

  return (
    <header className="flex h-12 shrink-0 items-center justify-between gap-4 border-b border-line bg-panel px-4">
      <button
        onClick={() => navigate('/projects')}
        className="flex items-center gap-2 text-sm text-ink-dim transition-colors hover:text-ink"
      >
        <FolderKanban size={15} />
        <span className="max-w-36 truncate font-medium">{projectTitle ?? projectId.slice(0, 8)}</span>
      </button>

      <div className="flex flex-1 justify-center">
        <FlowProgress projectId={projectId} stages={stages} />
      </div>

      <div className="flex items-center gap-2">
        <span
          className={cn(
            'flex items-center gap-1.5 rounded-full px-2.5 py-1 text-[11px]',
            connected ? 'text-success' : 'text-ink-mute',
          )}
        >
          {connected ? <Wifi size={13} /> : <WifiOff size={13} />}
          <span className="hidden sm:inline">{connected ? '已连接' : '离线'}</span>
        </span>
      </div>
    </header>
  );
}
