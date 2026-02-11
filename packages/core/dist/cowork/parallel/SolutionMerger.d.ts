/**
 * SolutionMerger - 方案选择与合并
 * 支持用户选择最优方案并合并到主分支
 */
import { EventEmitter } from 'events';
import { Diff } from '../types.js';
import { AgentWorker } from './ParallelExecutor.js';
import { WorkerResult } from './ResultCollector.js';
import { SolutionEvaluation } from './SolutionEvaluator.js';
/**
 * 合并策略
 */
export type MergeStrategy = 'merge' | 'rebase' | 'squash' | 'cherry-pick';
/**
 * 冲突类型
 */
export type ConflictType = 'content' | 'rename' | 'delete' | 'mode';
/**
 * 冲突信息
 */
export interface MergeConflict {
    file: string;
    type: ConflictType;
    ours: string;
    theirs: string;
    base?: string;
    resolved: boolean;
    resolution?: 'ours' | 'theirs' | 'manual';
}
/**
 * 合并结果
 */
export interface MergeResult {
    success: boolean;
    strategy: MergeStrategy;
    sourceBranch: string;
    targetBranch: string;
    conflicts: MergeConflict[];
    mergedFiles: string[];
    commitHash?: string;
    error?: string;
    rollbackAvailable: boolean;
}
/**
 * 方案预览
 */
export interface SolutionPreview {
    workerId: string;
    workerName: string;
    modelId: string;
    branch: string;
    diffs: Diff[];
    evaluation?: SolutionEvaluation;
    filesChanged: string[];
    additions: number;
    deletions: number;
}
/**
 * 选择器配置
 */
export interface SelectorConfig {
    defaultStrategy: MergeStrategy;
    autoCleanup: boolean;
    createBackup: boolean;
    backupBranchPrefix: string;
}
/**
 * 合并器事件
 */
export interface MergerEvents {
    'preview:generated': (preview: SolutionPreview) => void;
    'merge:start': (workerId: string, strategy: MergeStrategy) => void;
    'merge:conflict': (conflict: MergeConflict) => void;
    'merge:complete': (result: MergeResult) => void;
    'merge:rollback': (success: boolean) => void;
    'cleanup:complete': (workerId: string) => void;
}
/**
 * SolutionSelector - 方案选择器
 */
export declare class SolutionSelector extends EventEmitter {
    private config;
    private repoRoot;
    private previews;
    private lastMergeResult?;
    private backupBranch?;
    constructor(repoRoot: string, config?: Partial<SelectorConfig>);
    /**
     * 生成方案预览
     */
    generatePreview(worker: AgentWorker, result: WorkerResult, evaluation?: SolutionEvaluation): SolutionPreview;
    /**
     * 获取所有预览
     */
    getAllPreviews(): SolutionPreview[];
    /**
     * 获取预览
     */
    getPreview(workerId: string): SolutionPreview | undefined;
    /**
     * 选择并合并方案
     */
    selectAndMerge(workerId: string, targetBranch?: string, strategy?: MergeStrategy): Promise<MergeResult>;
    /**
     * 执行合并
     */
    private executeMerge;
    /**
     * 执行 merge
     */
    private doMerge;
    /**
     * 执行 rebase
     */
    private doRebase;
    /**
     * 执行 squash merge
     */
    private doSquashMerge;
    /**
     * 执行 cherry-pick
     */
    private doCherryPick;
    /**
     * 检测冲突文件
     */
    private detectConflicts;
    /**
     * 解决冲突
     */
    resolveConflict(file: string, resolution: 'ours' | 'theirs' | 'manual', manualContent?: string): Promise<boolean>;
    /**
     * 完成合并（解决所有冲突后）
     */
    completeMerge(): Promise<boolean>;
    /**
     * 创建备份
     */
    private createBackup;
    /**
     * 回滚合并
     */
    rollback(): Promise<boolean>;
    /**
     * 清理 worktree
     */
    cleanup(workerId: string): Promise<void>;
    /**
     * 清理所有
     */
    cleanupAll(): Promise<void>;
    /**
     * 获取最后的合并结果
     */
    getLastMergeResult(): MergeResult | undefined;
    /**
     * 是否可以回滚
     */
    canRollback(): boolean;
    /**
     * 执行 Git 命令
     */
    private execGit;
    /**
     * 更新配置
     */
    updateConfig(config: Partial<SelectorConfig>): void;
    /**
     * 获取配置
     */
    getConfig(): SelectorConfig;
}
/**
 * ConflictResolver - 冲突解决器
 */
export declare class ConflictResolver {
    /**
     * 解析冲突标记
     */
    static parseConflictMarkers(content: string): {
        ours: string;
        theirs: string;
        base?: string;
    } | null;
    /**
     * 自动解决简单冲突
     */
    static autoResolve(ours: string, theirs: string, strategy: 'ours' | 'theirs' | 'union'): string;
    /**
     * 检测冲突是否可以自动解决
     */
    static canAutoResolve(ours: string, theirs: string): boolean;
}
//# sourceMappingURL=SolutionMerger.d.ts.map