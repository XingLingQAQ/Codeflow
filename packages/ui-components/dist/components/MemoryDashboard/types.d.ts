/**
 * 记忆管理仪表盘类型定义
 * 双栏布局：STM（短期记忆池）+ LTM（长期规则库）
 */
/**
 * 记忆类型
 */
export type MemoryType = 'stm' | 'ltm';
/**
 * 记忆状态
 */
export type MemoryStatus = 'active' | 'archived' | 'pending_archive' | 'pending_delete';
/**
 * 排序字段
 */
export type SortField = 'timestamp' | 'heat' | 'surprise';
/**
 * 排序方向
 */
export type SortOrder = 'asc' | 'desc';
/**
 * 记忆条目
 */
export interface MemoryItem {
    id: string;
    content: string;
    type: MemoryType;
    status: MemoryStatus;
    sessionId: string;
    messageIndex: number;
    timestamp: number;
    heat: number;
    surprise: number;
    tags?: string[];
    source: 'user' | 'assistant' | 'system';
    isPermanent?: boolean;
}
/**
 * 记忆池 Props
 */
export interface MemoryPoolProps {
    title: string;
    type: MemoryType;
    items: MemoryItem[];
    sortField: SortField;
    sortOrder: SortOrder;
    onSortChange: (field: SortField, order: SortOrder) => void;
    onItemClick?: (item: MemoryItem) => void;
    onItemDoubleClick?: (item: MemoryItem) => void;
    onItemDragStart?: (item: MemoryItem) => void;
    onItemDrop?: (item: MemoryItem, targetType: MemoryType) => void;
    onTogglePermanent?: (item: MemoryItem) => void;
    onDeleteItem?: (item: MemoryItem) => void;
    isDropTarget?: boolean;
    isDragging?: boolean;
    className?: string;
    style?: React.CSSProperties;
}
/**
 * 记忆条目 Props
 */
export interface MemoryItemCardProps {
    item: MemoryItem;
    isDragging?: boolean;
    onClick?: () => void;
    onDoubleClick?: () => void;
    onDragStart?: () => void;
    onTogglePermanent?: () => void;
    onDelete?: () => void;
}
/**
 * 惊喜度颜色条 Props
 */
export interface SurpriseBarProps {
    value: number;
    width?: number;
    height?: number;
    showLabel?: boolean;
}
/**
 * 仪表盘 Props
 */
export interface MemoryDashboardProps {
    stmItems: MemoryItem[];
    ltmItems: MemoryItem[];
    onArchive?: (item: MemoryItem) => void;
    onRestore?: (item: MemoryItem) => void;
    onDelete?: (item: MemoryItem) => void;
    onTogglePermanent?: (item: MemoryItem) => void;
    onNavigateToMessage?: (sessionId: string, messageIndex: number) => void;
    onRefresh?: () => void;
    isLoading?: boolean;
    className?: string;
    style?: React.CSSProperties;
}
/**
 * 惊喜度颜色映射
 */
export declare const SURPRISE_COLORS: {
    low: string;
    medium: string;
    high: string;
};
/**
 * 根据惊喜度获取颜色
 */
export declare function getSurpriseColor(value: number): string;
/**
 * 热度颜色映射
 */
export declare const HEAT_COLORS: {
    cold: string;
    warm: string;
    hot: string;
};
/**
 * 根据热度获取颜色
 */
export declare function getHeatColor(value: number): string;
/**
 * 排序选项
 */
export declare const SORT_OPTIONS: Array<{
    field: SortField;
    label: string;
}>;
//# sourceMappingURL=types.d.ts.map