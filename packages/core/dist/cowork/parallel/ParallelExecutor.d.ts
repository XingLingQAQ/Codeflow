/**
 * ParallelExecutor - 多 Agent 并行执行器
 * 在独立的 Git Worktree 中并行执行多个 Agent 任务
 */
import { EventEmitter } from 'events';
import { WorktreeManager, WorktreeInfo } from '../../git/WorktreeManager.js';
import { CoworkTask, ExecutionResult, ICodeEditor, ExecutorCapabilities } from '../types.js';
/**
 * Agent Worker 状态
 */
export type WorkerStatus = 'idle' | 'running' | 'completed' | 'failed' | 'cancelled';
/**
 * Agent Worker 信息
 */
export interface AgentWorker {
    id: string;
    name: string;
    modelId: string;
    worktree?: WorktreeInfo;
    status: WorkerStatus;
    task?: CoworkTask;
    result?: ExecutionResult;
    startedAt?: number;
    completedAt?: number;
    error?: Error;
}
/**
 * 并行执行配置
 */
export interface ParallelExecutorConfig {
    maxWorkers: number;
    worktreePrefix: string;
    timeout: number;
    failFast: boolean;
    cleanupOnComplete: boolean;
}
/**
 * 并行执行结果
 */
export interface ParallelExecutionResult {
    success: boolean;
    workers: AgentWorker[];
    results: ExecutionResult[];
    duration: number;
    errors: Error[];
}
/**
 * 并行执行器事件
 */
export interface ParallelExecutorEvents {
    'worker:created': (worker: AgentWorker) => void;
    'worker:started': (worker: AgentWorker) => void;
    'worker:progress': (worker: AgentWorker, progress: number) => void;
    'worker:completed': (worker: AgentWorker) => void;
    'worker:failed': (worker: AgentWorker, error: Error) => void;
    'worker:cancelled': (worker: AgentWorker) => void;
    'execution:started': (workers: AgentWorker[]) => void;
    'execution:completed': (result: ParallelExecutionResult) => void;
    'execution:failed': (error: Error) => void;
}
/**
 * 执行器注册信息
 */
interface ExecutorRegistration {
    name: string;
    editor: ICodeEditor;
    capabilities: ExecutorCapabilities;
    modelId: string;
}
/**
 * ParallelExecutor - 多 Agent 并行执行器
 */
export declare class ParallelExecutor extends EventEmitter {
    private config;
    private worktreeManager;
    private workers;
    private executors;
    private isRunning;
    private abortController?;
    constructor(worktreeManager: WorktreeManager, config?: Partial<ParallelExecutorConfig>);
    /**
     * 注册执行器
     */
    registerExecutor(name: string, editor: ICodeEditor, capabilities: ExecutorCapabilities, modelId: string): void;
    /**
     * 获取执行器
     */
    getExecutor(name: string): ExecutorRegistration | undefined;
    /**
     * 获取所有执行器
     */
    getAllExecutors(): ExecutorRegistration[];
    /**
     * 创建 Worker
     */
    createWorker(name: string, modelId: string, task: CoworkTask): Promise<AgentWorker>;
    /**
     * 启动 Worker
     */
    private startWorker;
    /**
     * 在 Worktree 中执行任务
     */
    private executeTaskInWorktree;
    /**
     * 并行执行多个任务
     */
    executeParallel(tasks: Array<{
        executorName: string;
        task: CoworkTask;
    }>): Promise<ParallelExecutionResult>;
    /**
     * 带超时的 Worker 启动
     */
    private startWorkerWithTimeout;
    /**
     * 取消执行
     */
    cancel(): Promise<void>;
    /**
     * 清理所有 worktrees
     */
    cleanup(): Promise<void>;
    /**
     * 获取所有 Workers
     */
    getWorkers(): AgentWorker[];
    /**
     * 获取 Worker
     */
    getWorker(id: string): AgentWorker | undefined;
    /**
     * 获取运行中的 Workers
     */
    getRunningWorkers(): AgentWorker[];
    /**
     * 获取已完成的 Workers
     */
    getCompletedWorkers(): AgentWorker[];
    /**
     * 获取失败的 Workers
     */
    getFailedWorkers(): AgentWorker[];
    /**
     * 是否正在运行
     */
    isExecuting(): boolean;
    /**
     * 获取配置
     */
    getConfig(): ParallelExecutorConfig;
    /**
     * 更新配置
     */
    updateConfig(config: Partial<ParallelExecutorConfig>): void;
}
export {};
//# sourceMappingURL=ParallelExecutor.d.ts.map