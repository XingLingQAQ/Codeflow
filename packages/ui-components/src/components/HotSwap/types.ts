/**
 * 模型热切换 UI 类型定义
 */

/**
 * 模型状态
 */
export type ModelStatus = 'online' | 'degraded' | 'offline' | 'switching';

/**
 * 模型选项
 */
export interface ModelOption {
  id: string;
  name: string;
  provider: string;
  status: ModelStatus;
  contextWindow: number;
  capabilities: string[];
  isActive: boolean;
}

/**
 * 下拉菜单 Props
 */
export interface HotSwapDropdownProps {
  models: ModelOption[];
  currentModelId: string;
  disabled?: boolean;
  loading?: boolean;
  className?: string;
  style?: React.CSSProperties;
  onModelSelect: (modelId: string) => void;
  onRetry?: () => void;
}

/**
 * 状态指示器 Props
 */
export interface StatusIndicatorProps {
  status: ModelStatus;
  size?: 'small' | 'medium' | 'large';
}

/**
 * 状态颜色映射
 */
export const STATUS_COLORS: Record<ModelStatus, string> = {
  online: '#4CAF50',
  degraded: '#FF9800',
  offline: '#F44336',
  switching: '#2196F3',
};

/**
 * Provider 图标映射
 */
export const PROVIDER_ICONS: Record<string, string> = {
  claude: '🤖',
  gemini: '✨',
  codex: '💻',
  openai: '🧠',
  custom: '⚙️',
};
