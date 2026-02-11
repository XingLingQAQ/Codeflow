/**
 * Git 冲突检测器
 * 用于检测并行任务执行后的 Git 冲突
 */
import { ConflictInfo, Diff } from './types.js';
/**
 * Git 冲突检测配置
 */
export interface GitConflictDetectorConfig {
    cwd?: string;
    gitPath?: string;
}
/**
 * Git 文件状态
 */
export interface GitFileStatus {
    file: string;
    status: 'modified' | 'added' | 'deleted' | 'untracked' | 'conflicted';
    staged: boolean;
}
/**
 * Git 冲突检测结果
 */
export interface GitConflictResult {
    hasConflicts: boolean;
    conflicts: ConflictInfo[];
    modifiedFiles: string[];
    stagedFiles: string[];
}
/**
 * Git 冲突检测器
 */
export declare class GitConflictDetector {
    private config;
    private gitAvailable;
    constructor(config?: GitConflictDetectorConfig);
    /**
     * 检测 Git 是否可用
     */
    checkGitAvailable(): Promise<boolean>;
    /**
     * 确保 Git 可用，否则抛出错误
     */
    ensureGitAvailable(): Promise<void>;
    /**
     * 检测工作目录中的冲突
     */
    detectConflicts(): Promise<GitConflictResult>;
    /**
     * 检测两个 diff 之间的冲突
     */
    detectDiffConflicts(diff1: Diff, diff2: Diff): ConflictInfo | null;
    /**
     * 批量检测多个 diff 之间的冲突
     */
    detectMultipleDiffConflicts(diffs: Diff[]): ConflictInfo[];
    /**
     * 获取 Git 状态
     */
    getStatus(): Promise<GitFileStatus[]>;
    /**
     * 检查是否在 Git 仓库中
     */
    isGitRepository(): Promise<boolean>;
    /**
     * 获取当前分支
     */
    getCurrentBranch(): Promise<string>;
    /**
     * 获取未提交的更改
     */
    getUncommittedChanges(): Promise<string[]>;
    /**
     * 获取暂存的更改
     */
    getStagedChanges(): Promise<string[]>;
    private runGitCommand;
}
//# sourceMappingURL=GitConflictDetector.d.ts.map