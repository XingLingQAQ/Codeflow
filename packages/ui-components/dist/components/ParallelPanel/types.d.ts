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
export declare const WORKER_STATUS_CONFIG: Record<string, {
    color: string;
    label: string;
    icon: string;
}>;
export declare const PROVIDER_COLORS: Record<string, string>;
export declare const METRIC_CONFIG: Record<string, {
    label: string;
    icon: string;
    color: string;
}>;
export declare const formatDuration: (ms: number) => string;
export declare const formatTimestamp: (ts: number) => string;
//# sourceMappingURL=types.d.ts.map