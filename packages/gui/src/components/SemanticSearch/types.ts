/**
 * 语义检索引擎UI类型定义
 * 三位一体搜索：向量搜索 + 全文搜索 + 图谱关联
 */

/**
 * 搜索类型
 */
export type SearchType = 'vector' | 'fulltext' | 'graph';

/**
 * 排序字段
 */
export type SearchSortField = 'score' | 'timestamp' | 'heat';

/**
 * 排序方向
 */
export type SearchSortOrder = 'asc' | 'desc';

/**
 * 搜索结果来源
 */
export type SearchSource = 'memory' | 'document' | 'code' | 'graph';

/**
 * 搜索结果项
 */
export interface SearchResultItem {
  id: string;
  title: string;
  content: string;
  preview: string;
  score: number;
  timestamp: number;
  heat: number;
  source: SearchSource;
  type: SearchType;
  highlights?: SearchHighlight[];
  metadata?: Record<string, unknown>;
}

/**
 * 搜索高亮
 */
export interface SearchHighlight {
  field: string;
  fragments: string[];
}

/**
 * 搜索历史项
 */
export interface SearchHistoryItem {
  id: string;
  query: string;
  type: SearchType;
  timestamp: number;
  resultCount: number;
}

/**
 * 搜索状态
 */
export interface SearchState {
  query: string;
  type: SearchType;
  isLoading: boolean;
  results: SearchResultItem[];
  totalCount: number;
  page: number;
  pageSize: number;
  sortField: SearchSortField;
  sortOrder: SearchSortOrder;
  error?: string;
}

/**
 * 搜索输入 Props
 */
export interface SearchInputProps {
  value: string;
  placeholder?: string;
  isLoading?: boolean;
  onChange: (value: string) => void;
  onSearch: () => void;
  onClear?: () => void;
  className?: string;
  style?: React.CSSProperties;
}

/**
 * 搜索标签页 Props
 */
export interface SearchTabsProps {
  activeTab: SearchType;
  onTabChange: (tab: SearchType) => void;
  counts?: Record<SearchType, number>;
  className?: string;
  style?: React.CSSProperties;
}

/**
 * 搜索结果列表 Props
 */
export interface SearchResultListProps {
  results: SearchResultItem[];
  query: string;
  isLoading?: boolean;
  sortField: SearchSortField;
  sortOrder: SearchSortOrder;
  onSortChange: (field: SearchSortField, order: SearchSortOrder) => void;
  onResultClick?: (result: SearchResultItem) => void;
  onAddToContext?: (result: SearchResultItem) => void;
  className?: string;
  style?: React.CSSProperties;
}

/**
 * 搜索结果卡片 Props
 */
export interface SearchResultCardProps {
  result: SearchResultItem;
  query: string;
  onClick?: () => void;
  onAddToContext?: () => void;
}

/**
 * 搜索历史 Props
 */
export interface SearchHistoryProps {
  history: SearchHistoryItem[];
  onHistoryClick: (item: SearchHistoryItem) => void;
  onClearHistory?: () => void;
  className?: string;
  style?: React.CSSProperties;
}

/**
 * 语义搜索中心 Props
 */
export interface SemanticSearchCenterProps {
  onSearch: (query: string, type: SearchType) => Promise<SearchResultItem[]>;
  onAddToContext?: (result: SearchResultItem) => void;
  onExportMarkdown?: (results: SearchResultItem[]) => void;
  history?: SearchHistoryItem[];
  onHistoryClick?: (item: SearchHistoryItem) => void;
  onClearHistory?: () => void;
  className?: string;
  style?: React.CSSProperties;
}

/**
 * 搜索类型配置
 */
export const SEARCH_TYPE_CONFIG: Record<SearchType, { label: string; icon: string; color: string }> = {
  vector: { label: 'Vector Search', icon: '🔮', color: '#9C27B0' },
  fulltext: { label: 'Full-text Search', icon: '📝', color: '#2196F3' },
  graph: { label: 'Graph Relations', icon: '🕸️', color: '#4CAF50' },
};

/**
 * 来源颜色映射
 */
export const SOURCE_COLORS: Record<SearchSource, string> = {
  memory: '#9C27B0',
  document: '#2196F3',
  code: '#4CAF50',
  graph: '#FF9800',
};

/**
 * 排序选项
 */
export const SEARCH_SORT_OPTIONS: Array<{ field: SearchSortField; label: string }> = [
  { field: 'score', label: 'Relevance' },
  { field: 'timestamp', label: 'Time' },
  { field: 'heat', label: 'Heat' },
];

/**
 * 高亮文本中的关键词
 */
export function highlightText(text: string, query: string): string {
  if (!query.trim()) return text;
  const words = query.trim().split(/\s+/).filter(Boolean);
  let result = text;
  words.forEach((word) => {
    const regex = new RegExp(`(${word})`, 'gi');
    result = result.replace(regex, '<mark>$1</mark>');
  });
  return result;
}

/**
 * 格式化时间戳
 */
export function formatTimestamp(ts: number): string {
  const date = new Date(ts);
  const now = new Date();
  const diff = now.getTime() - date.getTime();

  if (diff < 60000) return 'Just now';
  if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`;
  if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`;
  if (diff < 604800000) return `${Math.floor(diff / 86400000)}d ago`;

  return date.toLocaleDateString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
  });
}
