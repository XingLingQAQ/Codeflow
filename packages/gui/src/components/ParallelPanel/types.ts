/**
 * ParallelPanel Types - 并行模式面板类型定义
 */

export interface ParallelTask {
  id: string;
  name: string;
  description: string;
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled';
  createdAt: number;
  startedAt?: number;
  completedAt?: number;
  workers: ParallelWorker[];
  selectedSolutionId?: string;
}

export interface ParallelWorker {
  id: string;
  name: string;
  model: string;
  modelProvider: string;
  status: 'idle' | 'running' | 'completed' | 'failed';
  progress: number;
  worktree: string;
  branch: string;
  solution?: WorkerSolution;
  logs: WorkerLog[];
  startedAt?: number;
  completedAt?: number;
}

export interface WorkerSolution {
  id: string;
  workerId: string;
  files: SolutionFile[];
  metrics: SolutionMetrics;
  summary: string;
}

export interface SolutionFile {
  path: string;
  action: 'create' | 'modify' | 'delete';
  additions: number;
  deletions: number;
  content?: string;
}

export interface SolutionMetrics {
  quality: number;
  performance: number;
  maintainability: number;
  security: number;
  overall: number;
}

export interface WorkerLog {
  id: string;
  timestamp: number;
  type: 'info' | 'warning' | 'error' | 'success';
  message: string;
}

export interface ParallelPanelProps {
  task?: ParallelTask;
  onWorkerSelect?: (workerId: string) => void;
  onSolutionSelect?: (solutionId: string) => void;
  onSolutionMerge?: (solutionId: string, strategy: MergeStrategy) => void;
  onTaskCancel?: () => void;
  onWorkerCancel?: (workerId: string) => void;
  className?: string;
  style?: React.CSSProperties;
}

export type MergeStrategy = 'fast-forward' | 'merge' | 'rebase';

// Worker 状态配置
export const WORKER_STATUS_CONFIG: Record<string, { color: string; label: string; icon: string }> = {
  idle: { color: '#94a3b8', label: 'Idle', icon: '⏸️' },
  running: { color: '#3b82f6', label: 'Running', icon: '🔄' },
  completed: { color: '#22c55e', label: 'Completed', icon: '✅' },
  failed: { color: '#ef4444', label: 'Failed', icon: '❌' },
};

// 模型提供商颜色
export const PROVIDER_COLORS: Record<string, string> = {
  anthropic: '#d97706',
  openai: '#10b981',
  google: '#3b82f6',
  local: '#8b5cf6',
};

// 指标配置
export const METRIC_CONFIG: Record<string, { label: string; icon: string; color: string }> = {
  quality: { label: 'Quality', icon: '⭐', color: '#f59e0b' },
  performance: { label: 'Performance', icon: '⚡', color: '#3b82f6' },
  maintainability: { label: 'Maintainability', icon: '🔧', color: '#8b5cf6' },
  security: { label: 'Security', icon: '🔒', color: '#22c55e' },
  overall: { label: 'Overall', icon: '📊', color: '#6366f1' },
};

// 格式化时间
export const formatDuration = (ms: number): string => {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
};

// 格式化时间戳
export const formatTimestamp = (ts: number): string => {
  const date = new Date(ts);
  return date.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
};
