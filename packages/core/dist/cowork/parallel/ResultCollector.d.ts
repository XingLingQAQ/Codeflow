/**
 * ResultCollector - 并行执行结果收集器
 * 收集、聚合和分析多个 Agent 的执行结果
 */
import { EventEmitter } from 'events';
import { ExecutionResult, Diff } from '../types.js';
import { AgentWorker, ParallelExecutionResult } from './ParallelExecutor.js';
/**
 * 结果摘要
 */
export interface ResultSummary {
    totalWorkers: number;
    successCount: number;
    failedCount: number;
    cancelledCount: number;
    totalDuration: number;
    averageDuration: number;
    totalDiffs: number;
    totalAdditions: number;
    totalDeletions: number;
    affectedFiles: string[];
}
/**
 * Worker 结果对比
 */
export interface WorkerComparison {
    workerId: string;
    workerName: string;
    modelId: string;
    success: boolean;
    duration: number;
    diffCount: number;
    additions: number;
    deletions: number;
    affectedFiles: string[];
    score?: number;
}
/**
 * 结果收集器事件
 */
export interface ResultCollectorEvents {
    'result:added': (workerId: string, result: ExecutionResult) => void;
    'result:updated': (workerId: string, result: ExecutionResult) => void;
    'summary:updated': (summary: ResultSummary) => void;
}
/**
 * ResultCollector - 结果收集器
 */
export declare class ResultCollector extends EventEmitter {
    private results;
    private workers;
    /**
     * 添加结果
     */
    addResult(workerId: string, result: ExecutionResult, worker?: AgentWorker): void;
    /**
     * 从并行执行结果收集
     */
    collectFromParallelResult(parallelResult: ParallelExecutionResult): void;
    /**
     * 获取结果
     */
    getResult(workerId: string): ExecutionResult | undefined;
    /**
     * 获取所有结果
     */
    getAllResults(): ExecutionResult[];
    /**
     * 获取成功的结果
     */
    getSuccessfulResults(): ExecutionResult[];
    /**
     * 获取失败的结果
     */
    getFailedResults(): ExecutionResult[];
    /**
     * 获取结果摘要
     */
    getSummary(): ResultSummary;
    /**
     * 获取 Worker 对比
     */
    getWorkerComparisons(): WorkerComparison[];
    /**
     * 按指标排序 Worker
     */
    rankWorkers(metric?: 'duration' | 'additions' | 'deletions' | 'diffCount', ascending?: boolean): WorkerComparison[];
    /**
     * 获取最佳 Worker（基于成功和最短时间）
     */
    getBestWorker(): WorkerComparison | undefined;
    /**
     * 获取文件级别的 Diff 对比
     */
    getFileDiffComparison(file: string): Map<string, Diff | undefined>;
    /**
     * 获取所有受影响的文件
     */
    getAffectedFiles(): string[];
    /**
     * 检查是否有冲突（多个 Worker 修改同一文件）
     */
    hasConflicts(): boolean;
    /**
     * 获取冲突文件
     */
    getConflictingFiles(): string[];
    /**
     * 清空结果
     */
    clear(): void;
    /**
     * 获取结果数量
     */
    get size(): number;
}
//# sourceMappingURL=ResultCollector.d.ts.map