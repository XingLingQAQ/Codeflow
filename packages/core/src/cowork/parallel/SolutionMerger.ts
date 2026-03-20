/**
 * SolutionMerger - 方案选择与合并
 * 支持用户选择最优方案并合并到主分支
 */

import { EventEmitter } from 'events';
import { execSync } from 'child_process';
import * as path from 'path';
import { Diff } from '../types.js';
import { AgentWorker } from './ParallelExecutor.js';
import { WorkerResult } from './ResultCollector.js';
import { SolutionEvaluation, ComparisonReport } from './SolutionEvaluator.js';

/**
 * 合并策略
 */
export type MergeStrategy = 'merge' | 'rebase' | 'squash' | 'cherry-pick';

/**
 * 冲突类型
 */
export type MergeConflictType = 'content' | 'rename' | 'delete' | 'mode';

/**
 * 冲突信息
 */
export interface MergeConflict {
  file: string;
  type: MergeConflictType;
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
 * 默认配置
 */
const DEFAULT_CONFIG: SelectorConfig = {
  defaultStrategy: 'merge',
  autoCleanup: true,
  createBackup: true,
  backupBranchPrefix: 'backup',
};

/**
 * SolutionSelector - 方案选择器
 */
export class SolutionSelector extends EventEmitter {
  private config: SelectorConfig;
  private repoRoot: string;
  private previews: Map<string, SolutionPreview> = new Map();
  private lastMergeResult?: MergeResult;
  private backupBranch?: string;

  constructor(repoRoot: string, config: Partial<SelectorConfig> = {}) {
    super();
    this.repoRoot = repoRoot;
    this.config = { ...DEFAULT_CONFIG, ...config };
  }

  /**
   * 生成方案预览
   */
  generatePreview(
    worker: AgentWorker,
    result: WorkerResult,
    evaluation?: SolutionEvaluation
  ): SolutionPreview {
    const diffs = result.diffs || [];
    const filesChanged = diffs.map(d => d.file);
    const additions = diffs.reduce((sum, d) => sum + d.additions, 0);
    const deletions = diffs.reduce((sum, d) => sum + d.deletions, 0);

    const preview: SolutionPreview = {
      workerId: worker.id,
      workerName: worker.name,
      modelId: worker.modelId,
      branch: worker.worktree?.branch || '',
      diffs,
      evaluation,
      filesChanged,
      additions,
      deletions,
    };

    this.previews.set(worker.id, preview);
    this.emit('preview:generated', preview);

    return preview;
  }

  /**
   * 获取所有预览
   */
  getAllPreviews(): SolutionPreview[] {
    return Array.from(this.previews.values());
  }

  /**
   * 获取预览
   */
  getPreview(workerId: string): SolutionPreview | undefined {
    return this.previews.get(workerId);
  }

  /**
   * 选择并合并方案
   */
  async selectAndMerge(
    workerId: string,
    targetBranch: string = 'main',
    strategy?: MergeStrategy
  ): Promise<MergeResult> {
    const preview = this.previews.get(workerId);
    if (!preview) {
      throw new Error(`Preview not found for worker: ${workerId}`);
    }

    const mergeStrategy = strategy || this.config.defaultStrategy;
    this.emit('merge:start', workerId, mergeStrategy);

    // 创建备份
    if (this.config.createBackup) {
      await this.createBackup(targetBranch);
    }

    try {
      const result = await this.executeMerge(preview, targetBranch, mergeStrategy);
      this.lastMergeResult = result;

      this.emit('merge:complete', result);

      // 自动清理
      if (this.config.autoCleanup && result.success) {
        await this.cleanup(workerId);
      }

      return result;
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : 'Unknown error';
      const result: MergeResult = {
        success: false,
        strategy: mergeStrategy,
        sourceBranch: preview.branch,
        targetBranch,
        conflicts: [],
        mergedFiles: [],
        error: errorMessage,
        rollbackAvailable: !!this.backupBranch,
      };

      this.lastMergeResult = result;
      this.emit('merge:complete', result);

      return result;
    }
  }

  /**
   * 执行合并
   */
  private async executeMerge(
    preview: SolutionPreview,
    targetBranch: string,
    strategy: MergeStrategy
  ): Promise<MergeResult> {
    const conflicts: MergeConflict[] = [];
    const mergedFiles: string[] = [];

    try {
      // 切换到目标分支
      this.execGit(`checkout ${targetBranch}`);

      // 根据策略执行合并
      let commitHash: string | undefined;

      switch (strategy) {
        case 'merge':
          commitHash = await this.doMerge(preview.branch, conflicts);
          break;
        case 'rebase':
          commitHash = await this.doRebase(preview.branch, conflicts);
          break;
        case 'squash':
          commitHash = await this.doSquashMerge(preview.branch, conflicts);
          break;
        case 'cherry-pick':
          commitHash = await this.doCherryPick(preview.branch, conflicts);
          break;
      }

      // 获取合并的文件
      mergedFiles.push(...preview.filesChanged);

      return {
        success: conflicts.filter(c => !c.resolved).length === 0,
        strategy,
        sourceBranch: preview.branch,
        targetBranch,
        conflicts,
        mergedFiles,
        commitHash,
        rollbackAvailable: !!this.backupBranch,
      };
    } catch (error) {
      // 检测冲突
      const conflictFiles = this.detectConflicts();
      for (const file of conflictFiles) {
        const conflict: MergeConflict = {
          file,
          type: 'content',
          ours: '',
          theirs: '',
          resolved: false,
        };
        conflicts.push(conflict);
        this.emit('merge:conflict', conflict);
      }

      return {
        success: false,
        strategy,
        sourceBranch: preview.branch,
        targetBranch,
        conflicts,
        mergedFiles: [],
        error: error instanceof Error ? error.message : 'Merge failed',
        rollbackAvailable: !!this.backupBranch,
      };
    }
  }

  /**
   * 执行 merge
   */
  private async doMerge(sourceBranch: string, conflicts: MergeConflict[]): Promise<string> {
    try {
      this.execGit(`merge ${sourceBranch} --no-edit`);
      return this.execGit('rev-parse HEAD').trim();
    } catch {
      throw new Error('Merge failed with conflicts');
    }
  }

  /**
   * 执行 rebase
   */
  private async doRebase(sourceBranch: string, conflicts: MergeConflict[]): Promise<string> {
    try {
      this.execGit(`rebase ${sourceBranch}`);
      return this.execGit('rev-parse HEAD').trim();
    } catch {
      this.execGit('rebase --abort');
      throw new Error('Rebase failed with conflicts');
    }
  }

  /**
   * 执行 squash merge
   */
  private async doSquashMerge(sourceBranch: string, conflicts: MergeConflict[]): Promise<string> {
    try {
      this.execGit(`merge --squash ${sourceBranch}`);
      this.execGit('commit -m "Squash merge from parallel execution"');
      return this.execGit('rev-parse HEAD').trim();
    } catch {
      throw new Error('Squash merge failed');
    }
  }

  /**
   * 执行 cherry-pick
   */
  private async doCherryPick(sourceBranch: string, conflicts: MergeConflict[]): Promise<string> {
    try {
      // 获取源分支的最新提交
      const commits = this.execGit(`log ${sourceBranch} --oneline -1`).trim();
      const commitHash = commits.split(' ')[0];
      this.execGit(`cherry-pick ${commitHash}`);
      return this.execGit('rev-parse HEAD').trim();
    } catch {
      this.execGit('cherry-pick --abort');
      throw new Error('Cherry-pick failed');
    }
  }

  /**
   * 检测冲突文件
   */
  private detectConflicts(): string[] {
    try {
      const output = this.execGit('diff --name-only --diff-filter=U');
      return output.trim().split('\n').filter(f => f.length > 0);
    } catch {
      return [];
    }
  }

  /**
   * 解决冲突
   */
  async resolveConflict(
    file: string,
    resolution: 'ours' | 'theirs' | 'manual',
    manualContent?: string
  ): Promise<boolean> {
    try {
      if (resolution === 'ours') {
        this.execGit(`checkout --ours ${file}`);
      } else if (resolution === 'theirs') {
        this.execGit(`checkout --theirs ${file}`);
      } else if (resolution === 'manual' && manualContent) {
        // 写入手动解决的内容
        const fs = await import('fs');
        const filePath = path.join(this.repoRoot, file);
        await fs.promises.writeFile(filePath, manualContent, 'utf-8');
      }

      this.execGit(`add ${file}`);

      // 更新冲突状态
      if (this.lastMergeResult) {
        const conflict = this.lastMergeResult.conflicts.find(c => c.file === file);
        if (conflict) {
          conflict.resolved = true;
          conflict.resolution = resolution;
        }
      }

      return true;
    } catch {
      return false;
    }
  }

  /**
   * 完成合并（解决所有冲突后）
   */
  async completeMerge(): Promise<boolean> {
    if (!this.lastMergeResult) {
      return false;
    }

    const unresolvedConflicts = this.lastMergeResult.conflicts.filter(c => !c.resolved);
    if (unresolvedConflicts.length > 0) {
      return false;
    }

    try {
      this.execGit('commit --no-edit');
      this.lastMergeResult.success = true;
      this.lastMergeResult.commitHash = this.execGit('rev-parse HEAD').trim();
      return true;
    } catch {
      return false;
    }
  }

  /**
   * 创建备份
   */
  private async createBackup(branch: string): Promise<void> {
    const timestamp = Date.now();
    this.backupBranch = `${this.config.backupBranchPrefix}-${branch}-${timestamp}`;

    try {
      this.execGit(`branch ${this.backupBranch}`);
    } catch {
      // 备份分支创建失败，继续执行
      this.backupBranch = undefined;
    }
  }

  /**
   * 回滚合并
   */
  async rollback(): Promise<boolean> {
    if (!this.backupBranch || !this.lastMergeResult) {
      this.emit('merge:rollback', false);
      return false;
    }

    try {
      // 重置到备份分支
      this.execGit(`reset --hard ${this.backupBranch}`);

      // 删除备份分支
      this.execGit(`branch -D ${this.backupBranch}`);
      this.backupBranch = undefined;

      this.emit('merge:rollback', true);
      return true;
    } catch {
      this.emit('merge:rollback', false);
      return false;
    }
  }

  /**
   * 清理 worktree
   */
  async cleanup(workerId: string): Promise<void> {
    const preview = this.previews.get(workerId);
    if (!preview) return;

    // 删除预览
    this.previews.delete(workerId);

    this.emit('cleanup:complete', workerId);
  }

  /**
   * 清理所有
   */
  async cleanupAll(): Promise<void> {
    const workerIds = Array.from(this.previews.keys());
    for (const workerId of workerIds) {
      await this.cleanup(workerId);
    }
  }

  /**
   * 获取最后的合并结果
   */
  getLastMergeResult(): MergeResult | undefined {
    return this.lastMergeResult;
  }

  /**
   * 是否可以回滚
   */
  canRollback(): boolean {
    return !!this.backupBranch;
  }

  /**
   * 执行 Git 命令
   */
  private execGit(command: string): string {
    return execSync(`git ${command}`, {
      cwd: this.repoRoot,
      encoding: 'utf-8',
      stdio: ['pipe', 'pipe', 'pipe'],
    });
  }

  /**
   * 更新配置
   */
  updateConfig(config: Partial<SelectorConfig>): void {
    this.config = { ...this.config, ...config };
  }

  /**
   * 获取配置
   */
  getConfig(): SelectorConfig {
    return { ...this.config };
  }
}

/**
 * ConflictResolver - 冲突解决器
 */
export class ConflictResolver {
  /**
   * 解析冲突标记
   */
  static parseConflictMarkers(content: string): {
    ours: string;
    theirs: string;
    base?: string;
  } | null {
    const conflictRegex = /<<<<<<< .*\n([\s\S]*?)(?:=======\n([\s\S]*?))?>>>>>>> .*/;
    const match = content.match(conflictRegex);

    if (!match) return null;

    return {
      ours: match[1] || '',
      theirs: match[2] || '',
    };
  }

  /**
   * 自动解决简单冲突
   */
  static autoResolve(
    ours: string,
    theirs: string,
    strategy: 'ours' | 'theirs' | 'union'
  ): string {
    switch (strategy) {
      case 'ours':
        return ours;
      case 'theirs':
        return theirs;
      case 'union':
        // 简单合并：两边都保留
        return `${ours}\n${theirs}`;
    }
  }

  /**
   * 检测冲突是否可以自动解决
   */
  static canAutoResolve(ours: string, theirs: string): boolean {
    // 如果一方为空，可以自动解决
    if (!ours.trim() || !theirs.trim()) return true;

    // 如果内容相同，可以自动解决
    if (ours.trim() === theirs.trim()) return true;

    // 如果是纯添加（没有重叠），可以自动解决
    const oursLines = new Set(ours.split('\n').map(l => l.trim()));
    const theirsLines = new Set(theirs.split('\n').map(l => l.trim()));
    const overlap = [...oursLines].filter(l => theirsLines.has(l) && l.length > 0);

    return overlap.length === 0;
  }
}
