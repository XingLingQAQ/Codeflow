import {
  Home,
  FolderKanban,
  PanelsTopLeft,
  Workflow,
  Bot,
  Puzzle,
  SlidersHorizontal,
  Settings,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';

export interface NavItem {
  key: string;
  label: string;
  path: string;
  icon: LucideIcon;
  match: (pathname: string) => boolean;
}

export const NAV_ITEMS: NavItem[] = [
  { key: 'dashboard', label: '总览', path: '/', icon: Home, match: (p) => p === '/' },
  { key: 'projects', label: '项目', path: '/projects', icon: FolderKanban, match: (p) => p.startsWith('/projects') },
  { key: 'workbench', label: '工作台', path: '/workbench', icon: PanelsTopLeft, match: (p) => p.startsWith('/workbench') },
  { key: 'flows', label: '工作流', path: '/flows', icon: Workflow, match: (p) => p.startsWith('/flows') },
  { key: 'agents', label: 'Agent', path: '/agents', icon: Bot, match: (p) => p.startsWith('/agents') },
  { key: 'plugins', label: '插件', path: '/plugins', icon: Puzzle, match: (p) => p.startsWith('/plugins') },
  { key: 'config', label: '配置', path: '/config', icon: SlidersHorizontal, match: (p) => p.startsWith('/config') },
  { key: 'settings', label: '设置', path: '/settings', icon: Settings, match: (p) => p.startsWith('/settings') },
];

/**
 * Concrete navigation target for a nav item. The workbench entry is special:
 * it needs a project + stage, so it resolves to the last visited workbench
 * location (or the project list when there is no history). Used by NavRail,
 * the command palette, and the /workbench index redirect so all three agree.
 */
export function resolveNavPath(
  item: Pick<NavItem, 'key' | 'path'>,
  last: { projectId?: string; stage: string },
): string {
  if (item.key === 'workbench') {
    return last.projectId ? `/workbench/${last.projectId}/${last.stage}` : '/projects';
  }
  return item.path;
}
