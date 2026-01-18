/**
 * 动态上下文构建器组件
 * 左侧文件树 + 右侧AST树 + 底部Token预算显示
 */
import React from 'react';
import { ContextBuilderProps, FileTreeProps, ASTTreeProps, TokenBudgetDisplayProps } from './types';
/**
 * 文件树组件
 */
export declare const FileTree: React.FC<FileTreeProps>;
/**
 * AST树组件
 */
export declare const ASTTree: React.FC<ASTTreeProps>;
/**
 * Token预算显示组件
 */
export declare const TokenBudgetDisplay: React.FC<TokenBudgetDisplayProps>;
/**
 * 动态上下文构建器
 */
export declare const ContextBuilder: React.FC<ContextBuilderProps>;
export default ContextBuilder;
//# sourceMappingURL=ContextBuilder.d.ts.map