import { EventEmitter } from 'events';
/**
 * Worktree 信息
 */
export interface WorktreeInfo {
    path: string;
    branch: string;
    commit: string;
    isMain: boolean;
    isLocked: boolean;
    prunable: boolean;
}
/**
 * Worktree 创建选项
 */
export interface CreateWorktreeOptions {
    branch?: string;
    baseBranch?: string;
    createBranch?: boolean;
    detach?: boolean;
    force?: boolean;
    lock?: boolean;
}
/**
 * Worktree 管理器配置
 */
export interface WorktreeManagerConfig {
    baseDir: string;
    worktreePrefix: string;
    maxWorktrees: number;
    autoCleanup: boolean;
    lockTimeout: number;
}
/**
 * Worktree 管理器事件
 */
export interface WorktreeManagerEvents {
    'worktree:created': (info: WorktreeInfo) => void;
    'worktree:removed': (path: string) => void;
    'worktree:locked': (path: string) => void;
    'worktree:unlocked': (path: string) => void;
    'worktree:error': (error: Error, operation: string) => void;
}
/**
 * Worktree 管理器
 * 管理 Git Worktree 的生命周期，支持并行开发
 */
export declare class WorktreeManager extends EventEmitter {
    private config;
    private repoRoot;
    private worktrees;
    constructor(repoRoot: string, config?: Partial<WorktreeManagerConfig>);
    /**
     * 初始化管理器
     */
    initialize(): Promise<void>;
    /**
     * 检查是否是 Git 仓库
     */
    isGitRepo(): Promise<boolean>;
    /**
     * 创建新的 Worktree
     */
    createWorktree(name: string, options?: CreateWorktreeOptions): Promise<WorktreeInfo>;
    /**
     * 移除 Worktree
     */
    removeWorktree(pathOrName: string, force?: boolean): Promise<boolean>;
    /**
     * 列出所有 Worktrees
     */
    listWorktrees(): Promise<WorktreeInfo[]>;
    /**
     * 获取 Worktree 信息
     */
    getWorktree(pathOrName: string): WorktreeInfo | undefined;
    /**
     * 锁定 Worktree
     */
    lockWorktree(pathOrName: string, reason?: string): Promise<boolean>;
    /**
     * 解锁 Worktree
     */
    unlockWorktree(pathOrName: string): Promise<boolean>;
    /**
     * 清理可修剪的 Worktrees
     */
    cleanupPrunable(): Promise<number>;
    /**
     * 移除所有非主 Worktrees
     */
    removeAllWorktrees(force?: boolean): Promise<number>;
    /**
     * 获取 Worktree 数量
     */
    getWorktreeCount(): number;
    /**
     * 检查 Worktree 是否存在
     */
    hasWorktree(pathOrName: string): boolean;
    /**
     * 在 Worktree 中执行 Git 命令
     */
    execInWorktree(pathOrName: string, command: string): Promise<string>;
    /**
     * 获取 Worktree 的当前分支
     */
    getWorktreeBranch(pathOrName: string): Promise<string | null>;
    /**
     * 获取 Worktree 的当前 commit
     */
    getWorktreeCommit(pathOrName: string): Promise<string | null>;
    /**
     * 刷新 Worktree 列表
     */
    refresh(): Promise<void>;
    /**
     * 解析 Worktree 条目
     */
    private parseWorktreeEntry;
    /**
     * 规范化路径（统一使用正斜杠）
     */
    private normalizePath;
    /**
     * 规范化路径用于比较（处理 Windows 短路径名）
     */
    private normalizePathForCompare;
    /**
     * 检查两个路径是否等价（处理 Windows 短路径名如 ADMINI~1）
     */
    private pathsEqual;
    /**
     * 在 worktrees Map 中查找路径（处理 Windows 短路径名）
     */
    private findWorktreeByPath;
    /**
     * 解析 Worktree 路径
     */
    private resolveWorktreePath;
    /**
     * 执行 Git 命令
     */
    private execGit;
}
//# sourceMappingURL=WorktreeManager.d.ts.map