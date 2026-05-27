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
    warningThreshold?: number;
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
export declare const AST_TYPE_ICONS: Record<ASTNodeType, string>;
/**
 * AST节点类型颜色映射
 */
export declare const AST_TYPE_COLORS: Record<ASTNodeType, string>;
/**
 * Token预算分类颜色
 */
export declare const BUDGET_COLORS: {
    systemPrompt: string;
    recentDialog: string;
    toolSchema: string;
    outputSpace: string;
    contextSelection: string;
    available: string;
};
/**
 * 计算Token使用百分比
 */
export declare function calculateUsagePercent(budget: TokenBudget): number;
/**
 * 判断是否超出预算
 */
export declare function isOverBudget(budget: TokenBudget): boolean;
/**
 * 判断是否接近预算上限
 */
export declare function isNearBudget(budget: TokenBudget, threshold?: number): boolean;
//# sourceMappingURL=types.d.ts.map