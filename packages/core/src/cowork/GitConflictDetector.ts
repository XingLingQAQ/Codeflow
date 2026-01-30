/**
 * Git 冲突检测器
 * 用于检测并行任务执行后的 Git 冲突
 */

import { spawn } from 'child_process';
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
export class GitConflictDetector {
  private config: GitConflictDetectorConfig;
  private gitAvailable: boolean | null = null;

  constructor(config: GitConflictDetectorConfig = {}) {
    this.config = {
      gitPath: 'git',
      ...config,
    };
  }

  /**
   * 检测 Git 是否可用
   */
  async checkGitAvailable(): Promise<boolean> {
    if (this.gitAvailable !== null) {
      return this.gitAvailable;
    }

    try {
      await this.runGitCommand(['--version']);
      this.gitAvailable = true;
      return true;
    } catch {
      this.gitAvailable = false;
      return false;
    }
  }

  /**
   * 确保 Git 可用，否则抛出错误
   */
  async ensureGitAvailable(): Promise<void> {
    const available = await this.checkGitAvailable();
    if (!available) {
      throw new Error(
        'Git is not available. Please install Git and ensure it is in your PATH.'
      );
    }
  }

  /**
   * 检测工作目录中的冲突
   */
  async detectConflicts(): Promise<GitConflictResult> {
    const status = await this.getStatus();
    const conflicts: ConflictInfo[] = [];
    const modifiedFiles: string[] = [];
    const stagedFiles: string[] = [];

    for (const file of status) {
      if (file.status === 'conflicted') {
        conflicts.push({
          file: file.file,
          executors: [],
          type: 'content',
        });
      }

      if (file.status === 'modified' || file.status === 'added') {
        modifiedFiles.push(file.file);
      }

      if (file.staged) {
        stagedFiles.push(file.file);
      }
    }

    return {
      hasConflicts: conflicts.length > 0,
      conflicts,
      modifiedFiles,
      stagedFiles,
    };
  }

  /**
   * 检测两个 diff 之间的冲突
   */
  detectDiffConflicts(diff1: Diff, diff2: Diff): ConflictInfo | null {
    if (diff1.file !== diff2.file) {
      return null;
    }

    // 检查 hunks 是否有重叠
    for (const hunk1 of diff1.hunks) {
      for (const hunk2 of diff2.hunks) {
        const range1Start = hunk1.oldStart;
        const range1End = hunk1.oldStart + hunk1.oldLines;
        const range2Start = hunk2.oldStart;
        const range2End = hunk2.oldStart + hunk2.oldLines;

        // 检查范围是否重叠
        if (range1Start < range2End && range2Start < range1End) {
          return {
            file: diff1.file,
            executors: [],
            type: 'content',
          };
        }
      }
    }

    return null;
  }

  /**
   * 批量检测多个 diff 之间的冲突
   */
  detectMultipleDiffConflicts(diffs: Diff[]): ConflictInfo[] {
    const conflicts: ConflictInfo[] = [];
    const fileGroups = new Map<string, Diff[]>();

    // 按文件分组
    for (const diff of diffs) {
      const existing = fileGroups.get(diff.file) || [];
      existing.push(diff);
      fileGroups.set(diff.file, existing);
    }

    // 检测同一文件的多个 diff 之间的冲突
    for (const [file, fileDiffs] of fileGroups) {
      if (fileDiffs.length > 1) {
        for (let i = 0; i < fileDiffs.length; i++) {
          for (let j = i + 1; j < fileDiffs.length; j++) {
            const conflict = this.detectDiffConflicts(fileDiffs[i], fileDiffs[j]);
            if (conflict) {
              conflicts.push(conflict);
            }
          }
        }
      }
    }

    return conflicts;
  }

  /**
   * 获取 Git 状态
   */
  async getStatus(): Promise<GitFileStatus[]> {
    const output = await this.runGitCommand(['status', '--porcelain', '-u']);
    const lines = output.trim().split('\n').filter((l) => l.length > 0);
    const files: GitFileStatus[] = [];

    for (const line of lines) {
      const indexStatus = line[0];
      const workTreeStatus = line[1];
      const file = line.slice(3);

      let status: GitFileStatus['status'] = 'modified';
      let staged = false;

      // 解析状态
      if (indexStatus === 'U' || workTreeStatus === 'U') {
        status = 'conflicted';
      } else if (indexStatus === 'A' || workTreeStatus === 'A') {
        status = 'added';
        staged = indexStatus === 'A';
      } else if (indexStatus === 'D' || workTreeStatus === 'D') {
        status = 'deleted';
        staged = indexStatus === 'D';
      } else if (indexStatus === '?' && workTreeStatus === '?') {
        status = 'untracked';
      } else if (indexStatus === 'M' || workTreeStatus === 'M') {
        status = 'modified';
        staged = indexStatus === 'M';
      }

      files.push({ file, status, staged });
    }

    return files;
  }

  /**
   * 检查是否在 Git 仓库中
   */
  async isGitRepository(): Promise<boolean> {
    try {
      await this.runGitCommand(['rev-parse', '--git-dir']);
      return true;
    } catch {
      return false;
    }
  }

  /**
   * 获取当前分支
   */
  async getCurrentBranch(): Promise<string> {
    const output = await this.runGitCommand(['branch', '--show-current']);
    return output.trim();
  }

  /**
   * 获取未提交的更改
   */
  async getUncommittedChanges(): Promise<string[]> {
    const output = await this.runGitCommand(['diff', '--name-only']);
    return output.trim().split('\n').filter((l) => l.length > 0);
  }

  /**
   * 获取暂存的更改
   */
  async getStagedChanges(): Promise<string[]> {
    const output = await this.runGitCommand(['diff', '--cached', '--name-only']);
    return output.trim().split('\n').filter((l) => l.length > 0);
  }

  // ==================== 私有方法 ====================

  private runGitCommand(args: string[]): Promise<string> {
    return new Promise((resolve, reject) => {
      const git = spawn(this.config.gitPath || 'git', args, {
        cwd: this.config.cwd,
        shell: true,
      });

      let stdout = '';
      let stderr = '';

      git.stdout.on('data', (data) => {
        stdout += data.toString();
      });

      git.stderr.on('data', (data) => {
        stderr += data.toString();
      });

      git.on('close', (code) => {
        if (code === 0) {
          resolve(stdout);
        } else {
          reject(new Error(`Git command failed: ${stderr || stdout}`));
        }
      });

      git.on('error', (err) => {
        reject(err);
      });
    });
  }
}
