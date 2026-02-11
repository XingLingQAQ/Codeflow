/**
 * Cowork Orchestrator
 * 多 CLI 协作编排器 - 支持并行、顺序、辩论三种协作模式
 */
import { EventEmitter } from 'events';
import { CoworkTask, ICodeEditor, ExecutorCapabilities, ParallelOptions, SequentialOptions, DebateOptions, ExecutionResult, BatchExecutionResult, DebateResult } from './types.js';
import { CLIProcessManager } from './process/CLIProcessManager.js';
import { GitConflictDetector } from './GitConflictDetector.js';
/**
 * 执行器注册信息
 */
interface ExecutorRegistration {
    name: string;
    editor: ICodeEditor;
    capabilities: ExecutorCapabilities;
}
/**
 * Blackboard 条目
 */
interface BlackboardEntry {
    key: string;
    value: unknown;
    source: string;
    timestamp: number;
}
/**
 * Cowork Orchestrator
 */
export declare class CoworkOrchestrator extends EventEmitter {
    private executors;
    private processManager;
    private blackboard;
    private runningTasks;
    private gitConflictDetector;
    constructor(processManager?: CLIProcessManager, cwd?: string);
    /**
     * 注册执行器
     */
    registerExecutor(name: string, editor: ICodeEditor, capabilities: ExecutorCapabilities): void;
    /**
     * 获取执行器
     */
    getExecutor(name: string): ExecutorRegistration | undefined;
    /**
     * 获取所有执行器
     */
    getAllExecutors(): ExecutorRegistration[];
    /**
     * 执行单个任务
     */
    execute(task: CoworkTask): Promise<ExecutionResult>;
    /**
     * 并行执行多个任务
     */
    executeParallel(tasks: CoworkTask[], options?: ParallelOptions): Promise<BatchExecutionResult>;
    /**
     * 顺序执行任务链
     */
    executeSequence(tasks: CoworkTask[], options?: SequentialOptions): Promise<BatchExecutionResult>;
    /**
     * 辩论模式执行
     */
    executeDebate(task: CoworkTask, options: DebateOptions): Promise<DebateResult>;
    /**
     * Blackboard 操作
     */
    setBlackboardEntry(key: string, value: unknown, source: string): void;
    getBlackboardEntry(key: string): BlackboardEntry | undefined;
    getAllBlackboardEntries(): BlackboardEntry[];
    clearBlackboard(): void;
    /**
     * 获取运行中的任务
     */
    getRunningTasks(): CoworkTask[];
    /**
     * 取消任务
     */
    cancelTask(taskId: string): Promise<boolean>;
    /**
     * 获取进程管理器
     */
    getProcessManager(): CLIProcessManager;
    /**
     * 获取 Git 冲突检测器
     */
    getGitConflictDetector(): GitConflictDetector;
    /**
     * 清理资源
     */
    cleanup(): Promise<void>;
    private emitEvent;
    private buildContextFromResult;
    private parseIssues;
    /**
     * 根据描述推断问题类型
     */
    private inferIssueType;
    /**
     * 将关键词映射到问题类型
     */
    private mapToIssueType;
    /**
     * 根据类型和描述推断严重性
     */
    private inferSeverity;
    private refineInstruction;
}
export {};
//# sourceMappingURL=CoworkOrchestrator.d.ts.map