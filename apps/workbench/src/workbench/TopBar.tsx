import { useNavigate } from 'react-router-dom';
import { FolderKanban, PanelLeft, PanelRight, PanelBottom, Wifi, WifiOff } from 'lucide-react';
import { FlowProgress } from './FlowProgress';
import type { Flow } from '../services-bridge/flows';
import { useLayoutStore } from '../stores/layout';
import { IconButton, Tooltip, Kbd } from '../ui';
import { modLabel } from '../lib/platform';
import { cn } from '../lib/cn';

interface TopBarProps {
  projectId: string;
  projectTitle?: string;
  flow?: Flow;
  connected?: boolean;
}

function PanelToggle({
  label,
  keys,
  collapsed,
  onToggle,
  icon,
}: {
  label: string;
  keys: string;
  collapsed: boolean;
  onToggle: () => void;
  icon: React.ReactNode;
}) {
  return (
    <Tooltip
      side="bottom"
      content={
        <span className="flex items-center gap-1.5">
          {collapsed ? `展开${label}` : `收起${label}`}
          <Kbd>{modLabel()}</Kbd>
          <Kbd>{keys}</Kbd>
        </span>
      }
    >
      <IconButton size="sm" active={!collapsed} aria-label={collapsed ? `展开${label}` : `收起${label}`} onClick={onToggle}>
        {icon}
      </IconButton>
    </Tooltip>
  );
}

export function TopBar({ projectId, projectTitle, flow, connected = false }: TopBarProps) {
  const navigate = useNavigate();
  const flowRailCollapsed = useLayoutStore((s) => s.flowRailCollapsed);
  const companionCollapsed = useLayoutStore((s) => s.companionCollapsed);
  const bottomCollapsed = useLayoutStore((s) => s.bottomCollapsed);
  const toggleFlowRail = useLayoutStore((s) => s.toggleFlowRail);
  const toggleCompanion = useLayoutStore((s) => s.toggleCompanion);
  const toggleBottom = useLayoutStore((s) => s.toggleBottom);

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
        <FlowProgress projectId={projectId} flow={flow} />
      </div>

      <div className="flex items-center gap-2">
        <span
          className={cn(
            'flex items-center gap-1.5 rounded-full px-2 py-1 text-xs',
            connected ? 'text-success' : 'text-ink-mute',
          )}
        >
          {connected ? <Wifi size={13} /> : <WifiOff size={13} />}
          <span className="hidden sm:inline">{connected ? '已连接' : '离线'}</span>
        </span>
        <span className="flex items-center gap-0.5 border-l border-line pl-2">
          <PanelToggle
            label="阶段栏"
            keys="B"
            collapsed={flowRailCollapsed}
            onToggle={toggleFlowRail}
            icon={<PanelLeft size={15} />}
          />
          <PanelToggle
            label="底部面板"
            keys="J"
            collapsed={bottomCollapsed}
            onToggle={toggleBottom}
            icon={<PanelBottom size={15} />}
          />
          <PanelToggle
            label="Agent 伴侣"
            keys="\"
            collapsed={companionCollapsed}
            onToggle={toggleCompanion}
            icon={<PanelRight size={15} />}
          />
        </span>
      </div>
    </header>
  );
}
