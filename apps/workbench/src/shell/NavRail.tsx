import { useNavigate, useLocation } from 'react-router-dom';
import { motion } from 'motion/react';
import {
  Search, Command as CommandIcon, Sun, Moon, Monitor,
  PanelLeftClose, PanelLeftOpen,
} from 'lucide-react';
import { NAV_ITEMS, resolveNavPath } from './nav';
import { LogoTile, Wordmark } from './Logo';
import { Tooltip } from '../ui/Tooltip';
import { Kbd } from '../ui/Kbd';
import { cn } from '../lib/cn';
import { modLabel } from '../lib/platform';
import { useShellStore, type ThemeMode, resolveIsDark } from '../stores/shell';
import { useLayoutStore } from '../stores/layout';
import { EASE_FLOW } from '../lib/motion';

const THEME_CYCLE: ThemeMode[] = ['light', 'dark', 'system'];
const THEME_LABELS: Record<ThemeMode, string> = { light: '浅色', dark: '深色', system: '跟随系统' };

export function NavRail() {
  const navigate = useNavigate();
  const location = useLocation();
  const setCommandOpen = useShellStore((s) => s.setCommandOpen);
  const themeMode = useShellStore((s) => s.themeMode);
  const setThemeMode = useShellStore((s) => s.setThemeMode);
  const lastProjectId = useLayoutStore((s) => s.lastProjectId);
  const lastStage = useLayoutStore((s) => s.lastStage);
  const expanded = useLayoutStore((s) => s.navRailExpanded);
  const toggleNavRail = useLayoutStore((s) => s.toggleNavRail);

  const goto = (item: (typeof NAV_ITEMS)[number]) => {
    navigate(resolveNavPath(item, { projectId: lastProjectId, stage: lastStage }));
  };

  return (
    <nav
      className={cn(
        'relative z-20 flex shrink-0 flex-col border-r border-line bg-panel py-3 transition-[width] duration-300 ease-[cubic-bezier(0.32,0.72,0,1)]',
        expanded ? 'w-[200px] items-stretch px-2' : 'w-16 items-center px-0',
      )}
    >
      {/* Logo: mark-only when collapsed, mark + wordmark when expanded */}
      <div className={cn('mb-2 flex items-center', expanded ? 'gap-2.5 pl-1.5' : 'justify-center')}>
        <LogoTile onClick={() => navigate('/')} size={expanded ? 36 : 40} />
        {expanded && <Wordmark />}
      </div>

      {/* Nav items */}
      <div className={cn('flex flex-1 flex-col gap-1', expanded ? 'items-stretch' : 'items-center')}>
        {NAV_ITEMS.map((item) => {
          const active = item.match(location.pathname);
          const Icon = item.icon;
          const btn = (
            <button
              onClick={() => goto(item)}
              aria-label={item.label}
              aria-current={active ? 'page' : undefined}
              className={cn(
                'relative flex items-center rounded-xl transition-colors duration-200',
                expanded ? 'h-9 gap-2.5 px-2.5' : 'size-11 justify-center',
                active ? 'font-semibold text-ink' : 'text-ink-mute hover:text-ink hover:bg-tint',
              )}
            >
              {active && (
                <motion.span
                  layoutId="nav-active"
                  transition={{ duration: 0.3, ease: EASE_FLOW }}
                  className="absolute inset-0 rounded-xl border border-ink/25 bg-tint-active"
                />
              )}
              {active && (
                <motion.span
                  layoutId="nav-active-bar"
                  transition={{ duration: 0.3, ease: EASE_FLOW }}
                  className={cn(
                    'absolute top-1/2 h-6 w-[3px] -translate-y-1/2 rounded-full bg-ink shadow-[0_0_0_1px_var(--color-base)]',
                    expanded ? '-left-2' : '-left-2',
                  )}
                />
              )}
              <Icon size={19} className="relative shrink-0" strokeWidth={active ? 2.5 : 2} />
              {expanded && <span className="relative truncate text-13 font-medium">{item.label}</span>}
            </button>
          );
          return expanded ? (
            <div key={item.key}>{btn}</div>
          ) : (
            <Tooltip key={item.key} content={item.label} side="right">
              {btn}
            </Tooltip>
          );
        })}
      </div>

      {/* Bottom cluster: theme toggle + Cmd+K + collapse toggle */}
      <div className={cn('mt-2 flex flex-col gap-1', expanded ? 'items-stretch' : 'items-center')}>
        <RailButton
          expanded={expanded}
          onClick={() => {
            const next = THEME_CYCLE[(THEME_CYCLE.indexOf(themeMode) + 1) % THEME_CYCLE.length];
            setThemeMode(next);
          }}
          ariaLabel="切换主题"
          icon={
            themeMode === 'system' ? <Monitor size={18} /> : resolveIsDark(themeMode) ? <Moon size={18} /> : <Sun size={18} />
          }
          label={`主题：${THEME_LABELS[themeMode]}`}
          tooltip={`主题：${THEME_LABELS[themeMode]}`}
        />
        <RailButton
          expanded={expanded}
          onClick={() => setCommandOpen(true)}
          ariaLabel="命令面板"
          icon={
            <span className="relative">
              <Search size={18} />
              <CommandIcon size={9} className="absolute -bottom-1 -right-1 text-ink-mute" />
            </span>
          }
          label="命令面板"
          tooltip={
            <span className="flex items-center gap-1.5">
              命令面板 <Kbd>{modLabel()}</Kbd>
              <Kbd>K</Kbd>
            </span>
          }
        />
        <RailButton
          expanded={expanded}
          onClick={toggleNavRail}
          ariaLabel={expanded ? '收起侧栏' : '展开侧栏'}
          icon={expanded ? <PanelLeftClose size={18} /> : <PanelLeftOpen size={18} />}
          label="收起"
          tooltip={expanded ? '收起' : '展开'}
        />
      </div>
    </nav>
  );
}

interface RailButtonProps {
  expanded: boolean;
  onClick: () => void;
  ariaLabel: string;
  icon: React.ReactNode;
  label: string;
  tooltip: React.ReactNode;
}

function RailButton({ expanded, onClick, ariaLabel, icon, label, tooltip }: RailButtonProps) {
  const btn = (
    <button
      onClick={onClick}
      aria-label={ariaLabel}
      className={cn(
        'flex items-center rounded-xl text-ink-mute transition-colors hover:bg-tint hover:text-ink',
        expanded ? 'h-9 gap-2.5 px-2.5' : 'size-11 justify-center',
      )}
    >
      <span className="shrink-0">{icon}</span>
      {expanded && <span className="truncate text-13 font-medium">{label}</span>}
    </button>
  );
  return expanded ? btn : <Tooltip content={tooltip} side="right">{btn}</Tooltip>;
}
