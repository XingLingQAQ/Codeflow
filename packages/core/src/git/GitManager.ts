import { exec } from 'child_process';
import { promisify } from 'util';
import { randomUUID } from 'crypto';
import { createHash } from 'crypto';
import { IGitManager, GitSnapshot, GitCommitInfo, GitDiff, SnapshotMapping } from './types.js';
import { HookManager } from '../hooks/HookManager.js';
import { HookEvent, ExecResult } from '../hooks/types.js';

const execAsync = promisify(exec);

/**
 * Git 快照管理器实现
 * 支持 hook_after_exec 快照生成和回滚
 */
export class GitManager implements IGitManager {
  private workDir: string;
  private snapshots: Map<string, GitSnapshot> = new Map();
  private mappings: Map<string, SnapshotMapping> = new Map();
  private hookManager?: HookManager;

  constructor(workDir: string, hookManager?: HookManager) {
    this.workDir = workDir;
    this.hookManager = hookManager;
    this.registerHooks();
  }

  private registerHooks(): void {
    if (this.hookManager) {
      // 注册 hook_after_exec 处理器
      this.hookManager.register(HookEvent.AFTER_EXEC, async (result: ExecResult) => {
        const snapshot = await this.createSnapshot(`Auto snapshot after: ${result.command}`);
        return snapshot.id;
      });
    }
  }

  // ==================== Git 操作 ====================

  async init(): Promise<boolean> {
    try {
      await this.execGit('init');
      return true;
    } catch {
      return false;
    }
  }

  async isRepo(): Promise<boolean> {
    try {
      await this.execGit('rev-parse --is-inside-work-tree');
      return true;
    } catch {
      return false;
    }
  }

  async status(): Promise<GitDiff[]> {
    try {
      const { stdout } = await this.execGit('status --porcelain');
      const lines = stdout.trim().split('\n').filter(Boolean);

      return lines.map((line) => {
        const status = line.substring(0, 2).trim();
        const file = line.substring(3);

        let statusType: GitDiff['status'];
        switch (status) {
          case 'A':
          case '??':
            statusType = 'added';
            break;
          case 'D':
            statusType = 'deleted';
            break;
          case 'R':
            statusType = 'renamed';
            break;
          default:
            statusType = 'modified';
        }

        return {
          file,
          status: statusType,
          additions: 0,
          deletions: 0,
        };
      });
    } catch {
      return [];
    }
  }

  async commit(message: string): Promise<GitCommitInfo> {
    // 先 add 所有变更
    await this.execGit('add -A');

    // 提交
    await this.execGit(`commit -m "${message.replace(/"/g, '\\"')}"`);

    // 获取提交信息
    const { stdout } = await this.execGit('log -1 --format="%H|%h|%s|%an|%at"');
    const [hash, shortHash, msg, author, timestamp] = stdout.trim().split('|');

    return {
      hash,
      shortHash,
      message: msg,
      author,
      timestamp: parseInt(timestamp, 10) * 1000,
    };
  }

  async getLog(limit: number = 10): Promise<GitCommitInfo[]> {
    try {
      const { stdout } = await this.execGit(`log -${limit} --format="%H|%h|%s|%an|%at"`);
      const lines = stdout.trim().split('\n').filter(Boolean);

      return lines.map((line) => {
        const [hash, shortHash, message, author, timestamp] = line.split('|');
        return {
          hash,
          shortHash,
          message,
          author,
          timestamp: parseInt(timestamp, 10) * 1000,
        };
      });
    } catch {
      return [];
    }
  }

  async getCurrentHash(): Promise<string | null> {
    try {
      const { stdout } = await this.execGit('rev-parse HEAD');
      return stdout.trim();
    } catch {
      return null;
    }
  }

  // ==================== 快照操作 ====================

  async createSnapshot(description?: string): Promise<GitSnapshot> {
    const status = await this.status();
    const hasChanges = status.length > 0;

    let gitHash: string;

    if (hasChanges) {
      // 有变更，创建新提交
      const commitInfo = await this.commit(description || 'Auto snapshot');
      gitHash = commitInfo.hash;
    } else {
      // 无变更，使用当前 HEAD
      gitHash = (await this.getCurrentHash()) || 'initial';
    }

    const snapshot: GitSnapshot = {
      id: randomUUID(),
      gitHash,
      dialogStateHash: this.generateStateHash(),
      timestamp: Date.now(),
      description,
      files: status.map((d) => d.file),
    };

    this.snapshots.set(snapshot.id, snapshot);

    return snapshot;
  }

  async restoreSnapshot(snapshotId: string): Promise<boolean> {
    const snapshot = this.snapshots.get(snapshotId);
    if (!snapshot) {
      return false;
    }

    try {
      // 硬重置到快照对应的 commit
      await this.execGit(`reset --hard ${snapshot.gitHash}`);
      return true;
    } catch {
      return false;
    }
  }

  getSnapshot(snapshotId: string): GitSnapshot | null {
    return this.snapshots.get(snapshotId) || null;
  }

  listSnapshots(): GitSnapshot[] {
    return Array.from(this.snapshots.values()).sort((a, b) => b.timestamp - a.timestamp);
  }

  // ==================== 映射管理 ====================

  addMapping(mapping: SnapshotMapping): void {
    this.mappings.set(mapping.snapshotId, mapping);
  }

  getMapping(snapshotId: string): SnapshotMapping | null {
    return this.mappings.get(snapshotId) || null;
  }

  getMappingByGitHash(gitHash: string): SnapshotMapping | null {
    for (const mapping of this.mappings.values()) {
      if (mapping.gitHash === gitHash) {
        return mapping;
      }
    }
    return null;
  }

  // ==================== 辅助方法 ====================

  private async execGit(command: string): Promise<{ stdout: string; stderr: string }> {
    return execAsync(`git ${command}`, { cwd: this.workDir });
  }

  private generateStateHash(): string {
    const state = {
      timestamp: Date.now(),
      random: Math.random(),
    };
    return createHash('sha256').update(JSON.stringify(state)).digest('hex').substring(0, 16);
  }

  /**
   * 获取两个快照之间的差异
   */
  async getDiffBetweenSnapshots(fromSnapshotId: string, toSnapshotId: string): Promise<GitDiff[]> {
    const fromSnapshot = this.snapshots.get(fromSnapshotId);
    const toSnapshot = this.snapshots.get(toSnapshotId);

    if (!fromSnapshot || !toSnapshot) {
      return [];
    }

    try {
      const { stdout } = await this.execGit(
        `diff --name-status ${fromSnapshot.gitHash} ${toSnapshot.gitHash}`
      );
      const lines = stdout.trim().split('\n').filter(Boolean);

      return lines.map((line) => {
        const [status, file] = line.split('\t');

        let statusType: GitDiff['status'];
        switch (status) {
          case 'A':
            statusType = 'added';
            break;
          case 'D':
            statusType = 'deleted';
            break;
          case 'R':
            statusType = 'renamed';
            break;
          default:
            statusType = 'modified';
        }

        return {
          file,
          status: statusType,
          additions: 0,
          deletions: 0,
        };
      });
    } catch {
      return [];
    }
  }
}
