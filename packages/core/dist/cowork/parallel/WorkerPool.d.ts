/**
 * WorkerPool - Worker 池管理
 * 管理 Agent Worker 的生命周期和资源分配
 */
import { EventEmitter } from 'events';
import { WorktreeManager } from '../../git/WorktreeManager.js';
import { AgentWorker } from './ParallelExecutor.js';
import { CoworkTask, ICodeEditor, ExecutorCapabilities } from '../types.js';
/**
 * Worker 池配置
 */
export interface WorkerPoolConfig {
    maxSize: number;
    minSize: number;
    idleTimeout: number;
    worktreePrefix: string;
}
/**
 * Worker 池统计
 */
export interface WorkerPoolStats {
    totalWorkers: number;
    idleWorkers: number;
    runningWorkers: number;
    completedWorkers: number;
    failedWorkers: number;
}
/**
 * Worker 池事件
 */
export interface WorkerPoolEvents {
    'pool:worker-added': (worker: AgentWorker) => void;
    'pool:worker-removed': (workerId: string) => void;
    'pool:worker-acquired': (worker: AgentWorker) => void;
    'pool:worker-released': (worker: AgentWorker) => void;
    'pool:exhausted': () => void;
    'pool:available': () => void;
}
/**
 * 执行器注册信息
 */
interface ExecutorInfo {
    name: string;
    editor: ICodeEditor;
    capabilities: ExecutorCapabilities;
    modelId: string;
}
/**
 * WorkerPool - Worker 池
 */
export declare class WorkerPool extends EventEmitter {
    private config;
    private worktreeManager;
    private workers;
    private executors;
    private idleTimers;
    private workerCounter;
    constructor(worktreeManager: WorktreeManager, config?: Partial<WorkerPoolConfig>);
    /**
     * 注册执行器
     */
    registerExecutor(name: string, editor: ICodeEditor, capabilities: ExecutorCapabilities, modelId: string): void;
    /**
     * 获取执行器
     */
    getExecutor(name: string): ExecutorInfo | undefined;
    /**
     * 创建新 Worker
     */
    private createWorker;
    /**
     * 获取可用 Worker（如果没有则创建）
     */
    acquire(executorName: string, task: CoworkTask): Promise<AgentWorker>;
    /**
     * 释放 Worker
     */
    release(workerId: string): void;
    /**
     * 标记 Worker 完成
     */
    markCompleted(workerId: string): void;
    /**
     * 标记 Worker 失败
     */
    markFailed(workerId: string, error: Error): void;
    /**
     * 移除 Worker
     */
    remove(workerId: string): Promise<void>;
    /**
     * 查找空闲 Worker
     */
    private findIdleWorker;
    /**
     * 设置空闲超时
     */
    private setIdleTimer;
    /**
     * 清除空闲超时
     */
    private clearIdleTimer;
    /**
     * 获取池统计
     */
    getStats(): WorkerPoolStats;
    /**
     * 获取所有 Workers
     */
    getWorkers(): AgentWorker[];
    /**
     * 获取 Worker
     */
    getWorker(workerId: string): AgentWorker | undefined;
    /**
     * 获取空闲 Workers
     */
    getIdleWorkers(): AgentWorker[];
    /**
     * 获取运行中的 Workers
     */
    getRunningWorkers(): AgentWorker[];
    /**
     * 清空池
     */
    drain(): Promise<void>;
    /**
     * 获取池大小
     */
    get size(): number;
    /**
     * 获取可用容量
     */
    get availableCapacity(): number;
    /**
     * 是否已满
     */
    get isFull(): boolean;
    /**
     * 是否为空
     */
    get isEmpty(): boolean;
    /**
     * 获取配置
     */
    getConfig(): WorkerPoolConfig;
    /**
     * 更新配置
     */
    updateConfig(config: Partial<WorkerPoolConfig>): void;
}
export {};
//# sourceMappingURL=WorkerPool.d.ts.map