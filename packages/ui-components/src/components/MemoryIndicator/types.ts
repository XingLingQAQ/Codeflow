/**
 * 记忆预警指示灯类型定义
 */

/**
 * 匹配级别
 */
export type MatchLevel = 'none' | 'low' | 'medium' | 'high' | 'critical';

/**
 * 指示灯状态
 */
export interface IndicatorState {
  level: MatchLevel;
  matchCount: number;
  topMatchScore: number;
  isAnimating: boolean;
}

/**
 * 输入框包装器 Props
 */
export interface MemoryInputWrapperProps {
  children: React.ReactNode;
  matchLevel: MatchLevel;
  matchCount?: number;
  showBadge?: boolean;
  pulseOnHighMatch?: boolean;
  className?: string;
  style?: React.CSSProperties;
  onClick?: () => void;
}

/**
 * 指示灯 Props
 */
export interface MemoryIndicatorProps {
  level: MatchLevel;
  size?: 'small' | 'medium' | 'large';
  showLabel?: boolean;
  animate?: boolean;
}

/**
 * 匹配预览 Props
 */
export interface MatchPreviewProps {
  matches: MatchPreviewItem[];
  visible: boolean;
  position?: 'top' | 'bottom';
  onMatchClick?: (matchId: string) => void;
  onClose?: () => void;
}

/**
 * 匹配预览项
 */
export interface MatchPreviewItem {
  id: string;
  title: string;
  preview: string;
  score: number;
  source: 'vector' | 'graph' | 'rules';
}

/**
 * 级别颜色映射
 */
export const LEVEL_COLORS: Record<MatchLevel, string> = {
  none: 'transparent',
  low: '#90CAF9',
  medium: '#4CAF50',
  high: '#FF9800',
  critical: '#F44336',
};

/**
 * 级别边框颜色映射
 */
export const LEVEL_BORDER_COLORS: Record<MatchLevel, string> = {
  none: '#ddd',
  low: '#90CAF9',
  medium: '#4CAF50',
  high: '#FF9800',
  critical: '#F44336',
};

/**
 * 级别标签映射
 */
export const LEVEL_LABELS: Record<MatchLevel, string> = {
  none: 'No matches',
  low: 'Low relevance',
  medium: 'Relevant',
  high: 'Highly relevant',
  critical: 'Critical match',
};

/**
 * 根据分数计算匹配级别
 */
export function scoreToLevel(score: number): MatchLevel {
  if (score >= 0.9) return 'critical';
  if (score >= 0.7) return 'high';
  if (score >= 0.5) return 'medium';
  if (score >= 0.3) return 'low';
  return 'none';
}
