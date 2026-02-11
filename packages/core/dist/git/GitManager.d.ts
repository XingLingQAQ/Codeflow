import { IGitManager, GitSnapshot, GitCommitInfo, GitDiff, SnapshotMapping } from './types.js';
import { HookManager } from '../hooks/HookManager.js';
/**
 * Git 快照管理器实现
 * 支持 hook_after_exec 快照生成和回滚
 */
export declare class GitManager implements IGitManager {
    private workDir;
    private snapshots;
    private mappings;
    private hookManager?;
    constructor(workDir: string, hookManager?: HookManager);
    private registerHooks;
    init(): Promise<boolean>;
    isRepo(): Promise<boolean>;
    status(): Promise<GitDiff[]>;
    commit(message: string): Promise<GitCommitInfo>;
    getLog(limit?: number): Promise<GitCommitInfo[]>;
    getCurrentHash(): Promise<string | null>;
    createSnapshot(description?: string): Promise<GitSnapshot>;
    restoreSnapshot(snapshotId: string): Promise<boolean>;
    getSnapshot(snapshotId: string): GitSnapshot | null;
    listSnapshots(): GitSnapshot[];
    addMapping(mapping: SnapshotMapping): void;
    getMapping(snapshotId: string): SnapshotMapping | null;
    getMappingByGitHash(gitHash: string): SnapshotMapping | null;
    private execGit;
    private generateStateHash;
    /**
     * 获取两个快照之间的差异
     */
    getDiffBetweenSnapshots(fromSnapshotId: string, toSnapshotId: string): Promise<GitDiff[]>;
}
//# sourceMappingURL=GitManager.d.ts.map