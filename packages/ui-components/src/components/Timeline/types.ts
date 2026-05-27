/**
 * Git 时间轴类型定义
 */

import { AtomicSnapshot, SnapshotTrigger } from '@codeflow/core';

/**
 * 时间轴条目
 */
export interface TimelineEntry {
  id: string;
  timestamp: number;
  gitHash: string;
  shortHash: string;
  message: string;
  trigger: SnapshotTrigger;
  filesChanged: number;
  messageCount: number;
  tags?: string[];
  isActive?: boolean;
}

/**
 * 时间轴条目点击事件
 */
export interface TimelineEntryClickEvent {
  entry: TimelineEntry;
  snapshot?: AtomicSnapshot;
  originalEvent: React.MouseEvent;
}

/**
 * 回滚确认事件
 */
export interface RollbackConfirmEvent {
  targetEntry: TimelineEntry;
  options: {
    rollbackGit: boolean;
    rollbackConversation: boolean;
    rollbackVector: boolean;
    rollbackGraph: boolean;
  };
}

/**
 * 时间轴视图配置
 */
export interface TimelineViewConfig {
  maxEntries: number;
  showDiffPreview: boolean;
  showFileList: boolean;
  compactMode: boolean;
  groupByDate: boolean;
}

/**
 * 时间轴组件 Props
 */
export interface TimelineViewProps {
  entries: TimelineEntry[];
  activeEntryId?: string;
  config?: Partial<TimelineViewConfig>;
  width?: number | string;
  height?: number | string;
  className?: string;
  style?: React.CSSProperties;
  onEntryClick?: (event: TimelineEntryClickEvent) => void;
  onEntryDoubleClick?: (event: TimelineEntryClickEvent) => void;
  onRollbackRequest?: (event: RollbackConfirmEvent) => void;
  onLoadMore?: () => void;
  loading?: boolean;
  hasMore?: boolean;
  emptyMessage?: string;
}

/**
 * Diff 预览 Props
 */
export interface DiffPreviewProps {
  gitHash: string;
  files: string[];
  onClose: () => void;
}

/**
 * 默认配置
 */
export const DEFAULT_TIMELINE_CONFIG: TimelineViewConfig = {
  maxEntries: 50,
  showDiffPreview: true,
  showFileList: true,
  compactMode: false,
  groupByDate: true,
};

/**
 * 触发类型颜色映射
 */
export const TRIGGER_COLORS: Record<SnapshotTrigger, string> = {
  hook_after_exec: '#4CAF50',
  manual: '#2196F3',
  auto_checkpoint: '#FF9800',
  before_rollback: '#F44336',
  session_end: '#9C27B0',
};

/**
 * 触发类型标签映射
 */
export const TRIGGER_LABELS: Record<SnapshotTrigger, string> = {
  hook_after_exec: 'Exec',
  manual: 'Manual',
  auto_checkpoint: 'Auto',
  before_rollback: 'Backup',
  session_end: 'Session',
};

/**
 * 从 AtomicSnapshot 转换为 TimelineEntry
 */
export function snapshotToTimelineEntry(snapshot: AtomicSnapshot): TimelineEntry {
  return {
    id: snapshot.id,
    timestamp: snapshot.timestamp,
    gitHash: snapshot.git.hash,
    shortHash: snapshot.git.shortHash,
    message: snapshot.git.message || snapshot.description || 'No message',
    trigger: snapshot.metadata.trigger,
    filesChanged: snapshot.git.files.length,
    messageCount: snapshot.conversation.messageCount,
    tags: snapshot.metadata.tags,
  };
}

/**
 * 按日期分组条目
 */
export function groupEntriesByDate(entries: TimelineEntry[]): Map<string, TimelineEntry[]> {
  const groups = new Map<string, TimelineEntry[]>();

  for (const entry of entries) {
    const date = new Date(entry.timestamp).toLocaleDateString();
    const existing = groups.get(date) || [];
    existing.push(entry);
    groups.set(date, existing);
  }

  return groups;
}

/**
 * 格式化相对时间
 */
export function formatRelativeTime(timestamp: number): string {
  const now = Date.now();
  const diff = now - timestamp;

  const seconds = Math.floor(diff / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);

  if (days > 0) return `${days}d ago`;
  if (hours > 0) return `${hours}h ago`;
  if (minutes > 0) return `${minutes}m ago`;
  return 'just now';
}
