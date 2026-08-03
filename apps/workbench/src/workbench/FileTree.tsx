import { useState } from 'react';
import { AnimatePresence, motion } from 'motion/react';
import {
  ChevronRight,
  Folder,
  FolderOpen,
  FileCode2,
  FileText,
  FileJson2,
  Image,
  SquareCheck,
  Square,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { Skeleton, Button } from '../ui';
import { useWorkspaceList } from '../lib/queries';
import { cn } from '../lib/cn';
import { EASE_FLOW } from '../lib/motion';

function fileIcon(name: string): LucideIcon {
  if (/\.(ts|tsx|js|jsx|go|rs|py|css)$/.test(name)) return FileCode2;
  if (/\.(json|ya?ml|toml)$/.test(name)) return FileJson2;
  if (/\.(png|jpe?g|svg|ico|gif|webp)$/.test(name)) return Image;
  return FileText;
}

export interface FileTreeProps {
  root: string;
  activePath?: string;
  /** Paths that have a staged (shadow) copy — marked with an amber dot. */
  stagedPaths: ReadonlySet<string>;
  /** Context-builder selection (path → size). */
  contextSelection: Record<string, number>;
  onOpen: (path: string) => void;
  onToggleContext: (path: string, size: number) => void;
}

interface LevelProps extends FileTreeProps {
  path: string;
  depth: number;
  expanded: Set<string>;
  onToggleDir: (path: string) => void;
}

function TreeLevel(props: LevelProps) {
  const { root, path, depth, expanded, onToggleDir } = props;
  const q = useWorkspaceList(root, path, true);
  const entries = q.data?.items ?? [];

  if (q.isLoading) {
    return (
      <div className="space-y-1 py-1" style={{ paddingLeft: depth * 12 + 8 }}>
        {Array.from({ length: depth === 0 ? 5 : 2 }).map((_, i) => (
          <Skeleton key={i} className="h-4 w-4/5" />
        ))}
      </div>
    );
  }
  if (q.isError) {
    return (
      <div className="px-2 py-1.5" style={{ paddingLeft: depth * 12 + 8 }}>
        <p className="text-[11px] text-danger/80">目录加载失败</p>
        <Button variant="ghost" size="sm" className="mt-1 h-6 px-2 text-[11px]" onClick={() => q.refetch()}>
          重试
        </Button>
      </div>
    );
  }
  if (entries.length === 0 && depth === 0) {
    return <p className="px-2 py-2 text-[12px] text-ink-mute">目录为空</p>;
  }

  return (
    <div role={depth === 0 ? 'tree' : 'group'}>
      {entries.map((e) => {
        const isOpen = expanded.has(e.path);
        const Icon = e.is_dir ? (isOpen ? FolderOpen : Folder) : fileIcon(e.name);
        const inContext = e.path in props.contextSelection;
        const isActive = !e.is_dir && props.activePath === e.path;
        return (
          <div key={e.path}>
            <div
              role="treeitem"
              aria-expanded={e.is_dir ? isOpen : undefined}
              aria-selected={isActive}
              tabIndex={0}
              onClick={() => (e.is_dir ? onToggleDir(e.path) : props.onOpen(e.path))}
              onKeyDown={(ev) => {
                if (ev.key === 'Enter' || ev.key === ' ') {
                  ev.preventDefault();
                  if (e.is_dir) onToggleDir(e.path);
                  else props.onOpen(e.path);
                }
              }}
              className={cn(
                'group flex cursor-pointer items-center gap-1.5 rounded-md py-1 pr-1.5 text-[12px] transition-colors',
                isActive ? 'bg-tint-active text-ink' : 'text-ink-dim hover:bg-tint',
              )}
              style={{ paddingLeft: depth * 12 + 6 }}
            >
              <ChevronRight
                size={12}
                className={cn(
                  'shrink-0 text-ink-mute transition-transform duration-150',
                  e.is_dir ? (isOpen ? 'rotate-90' : '') : 'invisible',
                )}
              />
              <Icon size={13} className="shrink-0 text-ink-mute" />
              <span className="min-w-0 flex-1 truncate">{e.name}</span>
              {!e.is_dir && props.stagedPaths.has(e.path) && (
                <span className="size-1.5 shrink-0 rounded-full bg-warn" title="有暂存修改" />
              )}
              {!e.is_dir && (
                <button
                  type="button"
                  aria-label={inContext ? `移出上下文：${e.name}` : `加入上下文：${e.name}`}
                  title={inContext ? '移出上下文' : '加入上下文'}
                  onClick={(ev) => {
                    ev.stopPropagation();
                    props.onToggleContext(e.path, e.size ?? 0);
                  }}
                  className={cn(
                    'shrink-0 rounded p-0.5 text-ink-mute transition-opacity hover:text-ink',
                    inContext ? 'opacity-100 text-ink' : 'opacity-0 group-hover:opacity-100 focus-visible:opacity-100',
                  )}
                >
                  {inContext ? <SquareCheck size={13} /> : <Square size={13} />}
                </button>
              )}
            </div>
            {e.is_dir && (
              <AnimatePresence initial={false}>
                {isOpen && (
                  <motion.div
                    key="children"
                    initial={{ height: 0, opacity: 0 }}
                    animate={{ height: 'auto', opacity: 1 }}
                    exit={{ height: 0, opacity: 0 }}
                    transition={{ duration: 0.2, ease: EASE_FLOW }}
                    className="overflow-hidden"
                  >
                    <TreeLevel {...props} path={e.path} depth={depth + 1} />
                  </motion.div>
                )}
              </AnimatePresence>
            )}
          </div>
        );
      })}
    </div>
  );
}

/** Lazy-loading workspace file tree (design: 编码画布辅助面板·文件树). */
export function FileTree(props: FileTreeProps) {
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set(['src']));

  const toggleDir = (path: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });

  return (
    <TreeLevel
      {...props}
      path=""
      depth={0}
      expanded={expanded}
      onToggleDir={toggleDir}
    />
  );
}
