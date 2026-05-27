/**
 * Plan模式任务看板组件
 * 任务列表+详情面板支持拖拽排序与批量模型切换
 */
import React from 'react';
import { PlanBoardProps, TaskCardProps, TaskDetailPanelProps, TaskListProps, BatchActionsProps, GanttChartProps } from './types';
/**
 * 任务卡片
 */
export declare const TaskCard: React.FC<TaskCardProps>;
/**
 * 任务列表
 */
export declare const TaskList: React.FC<TaskListProps>;
/**
 * 任务详情面板
 */
export declare const TaskDetailPanel: React.FC<TaskDetailPanelProps>;
/**
 * 批量操作栏
 */
export declare const BatchActions: React.FC<BatchActionsProps>;
/**
 * 简化Gantt图
 */
export declare const GanttChart: React.FC<GanttChartProps>;
/**
 * Plan模式任务看板
 */
export declare const PlanBoard: React.FC<PlanBoardProps>;
export default PlanBoard;
//# sourceMappingURL=PlanBoard.d.ts.map