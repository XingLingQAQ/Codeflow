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
  heat: number; // 0-1 热度值
  surprise: number; // 0-1 惊喜度
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
  value: number; // 0-1
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
export const SURPRISE_COLORS = {
  low: '#4CAF50',      // 绿色 0-0.3
  medium: '#FF9800',   // 橙色 0.3-0.7
  high: '#F44336',     // 红色 0.7-1.0
};

/**
 * 根据惊喜度获取颜色
 */
export function getSurpriseColor(value: number): string {
  if (value >= 0.7) return SURPRISE_COLORS.high;
  if (value >= 0.3) return SURPRISE_COLORS.medium;
  return SURPRISE_COLORS.low;
}

/**
 * 热度颜色映射
 */
export const HEAT_COLORS = {
  cold: '#90CAF9',     // 冷 0-0.3
  warm: '#FFB74D',     // 温 0.3-0.7
  hot: '#EF5350',      // 热 0.7-1.0
};

/**
 * 根据热度获取颜色
 */
export function getHeatColor(value: number): string {
  if (value >= 0.7) return HEAT_COLORS.hot;
  if (value >= 0.3) return HEAT_COLORS.warm;
  return HEAT_COLORS.cold;
}

/**
 * 排序选项
 */
export const SORT_OPTIONS: Array<{ field: SortField; label: string }> = [
  { field: 'timestamp', label: 'Time' },
  { field: 'heat', label: 'Heat' },
  { field: 'surprise', label: 'Surprise' },
];
