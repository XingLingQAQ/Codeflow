/**
 * Plan模式任务看板类型定义
 * 任务列表+详情面板支持拖拽排序与批量模型切换
 */

/**
 * 任务状态
 */
export type TaskStatus = 'pending' | 'in_progress' | 'completed' | 'blocked' | 'cancelled';

/**
 * 任务优先级
 */
export type TaskPriority = 'P0' | 'P1' | 'P2' | 'P3';

/**
 * 模型类型
 */
export type ModelType = 'claude-opus' | 'claude-sonnet' | 'gpt-4' | 'gemini-pro' | 'codex';

/**
 * 视图模式
 */
export type ViewMode = 'list' | 'gantt';

/**
 * 任务依赖
 */
export interface TaskDependency {
  taskId: string;
  type: 'blocks' | 'blocked_by';
}

/**
 * 任务项
 */
export interface PlanTask {
  id: string;
  title: string;
  description: string;
  status: TaskStatus;
  priority: TaskPriority;
  model: ModelType;
  dependencies: TaskDependency[];
  estimatedTime?: number;
  actualTime?: number;
  startTime?: number;
  endTime?: number;
  assignee?: string;
  tags: string[];
  order: number;
  isSelected?: boolean;
}

/**
 * 模型预设
 */
export interface ModelPreset {
  id: string;
  name: string;
  model: ModelType;
  papiVariables: Record<string, string>;
  description: string;
}

/**
 * 任务卡片Props
 */
export interface TaskCardProps {
  task: PlanTask;
  isSelected?: boolean;
  isDragging?: boolean;
  onSelect?: (taskId: string) => void;
  onDragStart?: (taskId: string) => void;
  onDragEnd?: () => void;
}

/**
 * 任务详情面板Props
 */
export interface TaskDetailPanelProps {
  task: PlanTask;
  allTasks: PlanTask[];
  modelPresets: ModelPreset[];
  onModelChange?: (taskId: string, model: ModelType) => void;
  onStatusChange?: (taskId: string, status: TaskStatus) => void;
  onClose?: () => void;
}

/**
 * 任务列表Props
 */
export interface TaskListProps {
  tasks: PlanTask[];
  selectedTaskIds: string[];
  onTaskSelect?: (taskId: string, multiSelect?: boolean) => void;
  onTaskReorder?: (taskId: string, newOrder: number) => void;
  onTaskClick?: (taskId: string) => void;
}

/**
 * Gantt图Props
 */
export interface GanttChartProps {
  tasks: PlanTask[];
  startDate: Date;
  endDate: Date;
  onTaskClick?: (taskId: string) => void;
}

/**
 * 批量操作栏Props
 */
export interface BatchActionsProps {
  selectedCount: number;
  onBatchModelChange?: (model: ModelType) => void;
  onBatchStatusChange?: (status: TaskStatus) => void;
  onClearSelection?: () => void;
}

/**
 * 任务看板Props
 */
export interface PlanBoardProps {
  tasks: PlanTask[];
  modelPresets: ModelPreset[];
  viewMode?: ViewMode;
  onTaskSelect?: (taskId: string, multiSelect?: boolean) => void;
  onTaskReorder?: (taskId: string, newOrder: number) => void;
  onTaskModelChange?: (taskId: string, model: ModelType) => void;
  onTaskStatusChange?: (taskId: string, status: TaskStatus) => void;
  onBatchModelChange?: (taskIds: string[], model: ModelType) => void;
  onViewModeChange?: (mode: ViewMode) => void;
  className?: string;
  style?: React.CSSProperties;
}

/**
 * 任务状态配置
 */
export const TASK_STATUS_CONFIG: Record<TaskStatus, { label: string; color: string; icon: string }> = {
  pending: { label: 'Pending', color: '#9E9E9E', icon: '⏳' },
  in_progress: { label: 'In Progress', color: '#2196F3', icon: '🔄' },
  completed: { label: 'Completed', color: '#4CAF50', icon: '✅' },
  blocked: { label: 'Blocked', color: '#F44336', icon: '🚫' },
  cancelled: { label: 'Cancelled', color: '#757575', icon: '❌' },
};

/**
 * 优先级配置
 */
export const TASK_PRIORITY_CONFIG: Record<TaskPriority, { label: string; color: string }> = {
  P0: { label: 'Critical', color: '#F44336' },
  P1: { label: 'High', color: '#FF9800' },
  P2: { label: 'Medium', color: '#2196F3' },
  P3: { label: 'Low', color: '#9E9E9E' },
};

/**
 * 模型配置
 */
export const MODEL_CONFIG: Record<ModelType, { label: string; icon: string; color: string }> = {
  'claude-opus': { label: 'Claude Opus', icon: '🎭', color: '#9C27B0' },
  'claude-sonnet': { label: 'Claude Sonnet', icon: '🎵', color: '#673AB7' },
  'gpt-4': { label: 'GPT-4', icon: '🤖', color: '#10a37f' },
  'gemini-pro': { label: 'Gemini Pro', icon: '💎', color: '#4285f4' },
  codex: { label: 'Codex', icon: '💻', color: '#FF6B6B' },
};

/**
 * 格式化时间
 */
export function formatTaskTime(minutes?: number): string {
  if (!minutes) return '-';
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  const mins = minutes % 60;
  return mins > 0 ? `${hours}h ${mins}m` : `${hours}h`;
}

/**
 * 计算任务进度
 */
export function calculateTaskProgress(tasks: PlanTask[]): {
  total: number;
  completed: number;
  inProgress: number;
  blocked: number;
  percentage: number;
} {
  const total = tasks.length;
  const completed = tasks.filter(t => t.status === 'completed').length;
  const inProgress = tasks.filter(t => t.status === 'in_progress').length;
  const blocked = tasks.filter(t => t.status === 'blocked').length;
  const percentage = total > 0 ? (completed / total) * 100 : 0;

  return { total, completed, inProgress, blocked, percentage };
}

/**
 * 按优先级排序
 */
export function sortByPriority(tasks: PlanTask[]): PlanTask[] {
  const priorityOrder: Record<TaskPriority, number> = { P0: 0, P1: 1, P2: 2, P3: 3 };
  return [...tasks].sort((a, b) => priorityOrder[a.priority] - priorityOrder[b.priority]);
}
