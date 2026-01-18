/**
 * 动态上下文构建器类型定义
 * 左侧文件树 + 右侧AST树 + 底部Token预算
 */

/**
 * 文件节点类型
 */
export type FileNodeType = 'file' | 'directory';

/**
 * AST节点类型
 */
export type ASTNodeType = 'function' | 'class' | 'variable' | 'interface' | 'type' | 'import' | 'export' | 'method' | 'property';

/**
 * 文件树节点
 */
export interface FileTreeNode {
  id: string;
  name: string;
  path: string;
  type: FileNodeType;
  children?: FileTreeNode[];
  isExpanded?: boolean;
  isSelected?: boolean;
  tokenCount?: number;
}

/**
 * AST树节点
 */
export interface ASTTreeNode {
  id: string;
  name: string;
  type: ASTNodeType;
  startLine: number;
  endLine: number;
  tokenCount: number;
  children?: ASTTreeNode[];
  isExpanded?: boolean;
  isChecked?: boolean;
  isIndeterminate?: boolean;
  content?: string;
}

/**
 * Token预算配置
 */
export interface TokenBudget {
  total: number;
  used: number;
  systemPrompt: number;
  recentDialog: number;
  toolSchema: number;
  outputSpace: number;
  contextSelection: number;
}

/**
 * 上下文预设
 */
export interface ContextPreset {
  id: string;
  name: string;
  description?: string;
  selectedFiles: string[];
  selectedNodes: string[];
  createdAt: number;
  updatedAt: number;
}

/**
 * 文件树 Props
 */
export interface FileTreeProps {
  nodes: FileTreeNode[];
  selectedPath?: string;
  onNodeClick?: (node: FileTreeNode) => void;
  onNodeExpand?: (node: FileTreeNode) => void;
  onNodeSelect?: (node: FileTreeNode) => void;
  className?: string;
  style?: React.CSSProperties;
}

/**
 * AST树 Props
 */
export interface ASTTreeProps {
  nodes: ASTTreeNode[];
  onNodeCheck?: (node: ASTTreeNode, checked: boolean) => void;
  onNodeExpand?: (node: ASTTreeNode) => void;
  onSelectAll?: () => void;
  onDeselectAll?: () => void;
  className?: string;
  style?: React.CSSProperties;
}

/**
 * Token预算显示 Props
 */
export interface TokenBudgetDisplayProps {
  budget: TokenBudget;
  warningThreshold?: number; // 0-1, default 0.9
  className?: string;
  style?: React.CSSProperties;
}

/**
 * 上下文构建器 Props
 */
export interface ContextBuilderProps {
  fileTree: FileTreeNode[];
  astTree: ASTTreeNode[];
  budget: TokenBudget;
  presets?: ContextPreset[];
  onFileSelect?: (node: FileTreeNode) => void;
  onASTNodeCheck?: (node: ASTTreeNode, checked: boolean) => void;
  onSavePreset?: (name: string) => void;
  onLoadPreset?: (preset: ContextPreset) => void;
  onDeletePreset?: (preset: ContextPreset) => void;
  onBuildContext?: () => void;
  isLoading?: boolean;
  className?: string;
  style?: React.CSSProperties;
}

/**
 * AST节点类型图标映射
 */
export const AST_TYPE_ICONS: Record<ASTNodeType, string> = {
  function: 'ƒ',
  class: 'C',
  variable: 'V',
  interface: 'I',
  type: 'T',
  import: '→',
  export: '←',
  method: 'M',
  property: 'P',
};

/**
 * AST节点类型颜色映射
 */
export const AST_TYPE_COLORS: Record<ASTNodeType, string> = {
  function: '#2196F3',
  class: '#9C27B0',
  variable: '#4CAF50',
  interface: '#FF9800',
  type: '#00BCD4',
  import: '#607D8B',
  export: '#795548',
  method: '#3F51B5',
  property: '#8BC34A',
};

/**
 * Token预算分类颜色
 */
export const BUDGET_COLORS = {
  systemPrompt: '#2196F3',
  recentDialog: '#4CAF50',
  toolSchema: '#FF9800',
  outputSpace: '#9C27B0',
  contextSelection: '#00BCD4',
  available: '#E0E0E0',
};

/**
 * 计算Token使用百分比
 */
export function calculateUsagePercent(budget: TokenBudget): number {
  return budget.total > 0 ? (budget.used / budget.total) * 100 : 0;
}

/**
 * 判断是否超出预算
 */
export function isOverBudget(budget: TokenBudget): boolean {
  return budget.used > budget.total;
}

/**
 * 判断是否接近预算上限
 */
export function isNearBudget(budget: TokenBudget, threshold = 0.9): boolean {
  return budget.used >= budget.total * threshold;
}
