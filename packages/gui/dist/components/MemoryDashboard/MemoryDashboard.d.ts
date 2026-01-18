/**
 * 记忆管理仪表盘组件
 * 双栏布局：左侧 STM（短期记忆池），右侧 LTM（长期规则库）
 * 支持拖拽归档、惊喜度评分显示、排序
 */
import React from 'react';
import { MemoryDashboardProps, MemoryPoolProps, MemoryItemCardProps, SurpriseBarProps } from './types';
/**
 * 惊喜度颜色条
 */
export declare const SurpriseBar: React.FC<SurpriseBarProps>;
/**
 * 记忆条目卡片
 */
export declare const MemoryItemCard: React.FC<MemoryItemCardProps>;
/**
 * 记忆池面板
 */
export declare const MemoryPool: React.FC<MemoryPoolProps>;
/**
 * 记忆管理仪表盘
 */
export declare const MemoryDashboard: React.FC<MemoryDashboardProps>;
export default MemoryDashboard;
//# sourceMappingURL=MemoryDashboard.d.ts.map