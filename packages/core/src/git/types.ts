/**
 * Git 快照绑定类型定义
 */

export interface GitSnapshot {
  id: string;
  gitHash: string;
  dialogStateHash: string;
  vectorStateHash?: string;
  timestamp: number;
  description?: string;
  files: string[];
}

export interface GitCommitInfo {
  hash: string;
  shortHash: string;
  message: string;
  author: string;
  timestamp: number;
}

export interface GitDiff {
  file: string;
  status: 'added' | 'modified' | 'deleted' | 'renamed';
  additions: number;
  deletions: number;
}

export interface SnapshotMapping {
  snapshotId: string;
  gitHash: string;
  sessionId: string;
  messageId?: string;
}

export interface IGitManager {
  // Git 操作
  init(): Promise<boolean>;
  isRepo(): Promise<boolean>;
  status(): Promise<GitDiff[]>;
  commit(message: string): Promise<GitCommitInfo>;
  getLog(limit?: number): Promise<GitCommitInfo[]>;
  getCurrentHash(): Promise<string | null>;

  // 快照操作
  createSnapshot(description?: string): Promise<GitSnapshot>;
  restoreSnapshot(snapshotId: string): Promise<boolean>;
  getSnapshot(snapshotId: string): GitSnapshot | null;
  listSnapshots(): GitSnapshot[];

  // 映射管理
  addMapping(mapping: SnapshotMapping): void;
  getMapping(snapshotId: string): SnapshotMapping | null;
  getMappingByGitHash(gitHash: string): SnapshotMapping | null;
}
