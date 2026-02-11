/**
 * ParallelCommands - 并行模式 CLI 命令
 * 实现 /codeflow:parallel-* 系列命令
 */
import { EventEmitter } from 'events';
/**
 * 命令参数基础接口
 */
export interface CommandParams {
    [key: string]: unknown;
}
/**
 * 并行命令参数
 */
export interface ParallelStartParams extends CommandParams {
    task: string;
    workers?: number;
    models?: string[];
    timeout?: number;
}
export interface ParallelStatusParams extends CommandParams {
    taskId?: string;
    verbose?: boolean;
}
export interface ParallelCompareParams extends CommandParams {
    taskId: string;
    metrics?: string[];
    format?: 'table' | 'json' | 'markdown';
}
export interface ParallelSelectParams extends CommandParams {
    taskId: string;
    solutionId: string;
    reason?: string;
}
export interface ParallelMergeParams extends CommandParams {
    taskId: string;
    solutionId: string;
    strategy?: 'fast-forward' | 'merge' | 'rebase';
    backup?: boolean;
}
/**
 * 命令执行结果
 */
export interface CommandResult {
    success: boolean;
    command: string;
    output: string;
    data?: unknown;
    error?: string;
    duration: number;
}
/**
 * 命令定义
 */
export interface CommandDefinition {
    name: string;
    description: string;
    usage: string;
    examples: string[];
    parameters: ParameterDefinition[];
}
/**
 * 参数定义
 */
export interface ParameterDefinition {
    name: string;
    type: 'string' | 'boolean' | 'number' | 'array';
    description: string;
    required: boolean;
    default?: unknown;
    choices?: string[];
}
/**
 * Worker 状态
 */
export interface WorkerStatus {
    id: string;
    model: string;
    status: 'pending' | 'running' | 'completed' | 'failed';
    progress: number;
    startTime?: number;
    endTime?: number;
    worktree?: string;
    branch?: string;
}
/**
 * 并行任务状态
 */
export interface ParallelTaskStatus {
    taskId: string;
    task: string;
    status: 'pending' | 'running' | 'completed' | 'failed';
    workers: WorkerStatus[];
    createdAt: number;
    completedAt?: number;
}
/**
 * 并行命令处理器
 */
export declare class ParallelCommands extends EventEmitter {
    private commands;
    private activeTasks;
    constructor();
    /**
     * 注册所有并行命令
     */
    private registerCommands;
    /**
     * 执行命令
     */
    execute(commandName: string, params: CommandParams): Promise<CommandResult>;
    /**
     * 验证参数
     */
    private validateParams;
    /**
     * 执行 parallel-start 命令
     */
    private executeParallelStart;
    /**
     * 执行 parallel-status 命令
     */
    private executeParallelStatus;
    /**
     * 执行 parallel-compare 命令
     */
    private executeParallelCompare;
    /**
     * 执行 parallel-select 命令
     */
    private executeParallelSelect;
    /**
     * 执行 parallel-merge 命令
     */
    private executeParallelMerge;
    /**
     * 获取命令定义
     */
    getCommand(name: string): CommandDefinition | undefined;
    /**
     * 获取所有命令
     */
    getAllCommands(): CommandDefinition[];
    /**
     * 获取帮助信息
     */
    getHelp(commandName?: string): string;
    /**
     * 获取活动任务
     */
    getActiveTasks(): ParallelTaskStatus[];
    /**
     * 获取任务
     */
    getTask(taskId: string): ParallelTaskStatus | undefined;
    /**
     * 解析命令行参数
     */
    parseArgs(args: string): CommandParams;
}
//# sourceMappingURL=ParallelCommands.d.ts.map