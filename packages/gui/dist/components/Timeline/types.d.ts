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
export declare const DEFAULT_TIMELINE_CONFIG: TimelineViewConfig;
/**
 * 触发类型颜色映射
 */
export declare const TRIGGER_COLORS: Record<SnapshotTrigger, string>;
/**
 * 触发类型标签映射
 */
export declare const TRIGGER_LABELS: Record<SnapshotTrigger, string>;
/**
 * 从 AtomicSnapshot 转换为 TimelineEntry
 */
export declare function snapshotToTimelineEntry(snapshot: AtomicSnapshot): TimelineEntry;
/**
 * 按日期分组条目
 */
export declare function groupEntriesByDate(entries: TimelineEntry[]): Map<string, TimelineEntry[]>;
/**
 * 格式化相对时间
 */
export declare function formatRelativeTime(timestamp: number): string;
//# sourceMappingURL=types.d.ts.map