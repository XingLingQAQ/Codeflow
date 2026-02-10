/**
 * DynamicSync - MS-250 动态同步意图文档
 *
 * 监听文件变更，自动触发 BatchProjector 重新生成意图文档。
 * 支持防抖、增量更新和进度回调。
 */

import * as fs from 'fs';
import * as path from 'path';

import { BatchProjector } from './BatchProjector.js';

/**
 * DynamicSync 配置
 */
export interface DynamicSyncConfig {
  /** 防抖延迟（毫秒） */
  debounceMs: number;
  /** 监听的文件扩展名 */
  watchExtensions: string[];
  /** 项目根目录 */
  projectRoot: string;
  /** 监听的目录列表（相对于 projectRoot） */
  watchDirs?: string[];
}

const DEFAULT_CONFIG: DynamicSyncConfig = {
  debounceMs: 500,
  watchExtensions: ['.ts', '.js', '.go', '.py'],
  projectRoot: process.cwd(),
};

/**
 * 同步进度事件
 */
export interface SyncProgressEvent {
  type: 'start' | 'file_synced' | 'file_failed' | 'complete';
  filePath?: string;
  error?: string;
  total?: number;
  completed?: number;
}

export type SyncProgressCallback = (event: SyncProgressEvent) => void;

export class DynamicSync {
  private readonly batchProjector: BatchProjector;
  private readonly config: DynamicSyncConfig;
  private readonly onProgress: SyncProgressCallback;

  private watchers: fs.FSWatcher[] = [];
  private debounceTimers: Map<string, ReturnType<typeof setTimeout>> = new Map();
  private pendingFiles: Set<string> = new Set();
  private isRunning = false;
  private isSyncing = false;

  constructor(
    batchProjector: BatchProjector,
    config: Partial<DynamicSyncConfig> & { projectRoot: string },
    onProgress: SyncProgressCallback = () => {}
  ) {
    this.batchProjector = batchProjector;
    this.config = { ...DEFAULT_CONFIG, ...config };
    this.onProgress = onProgress;
  }

  /**
   * 启动文件监听
   */
  start(): void {
    if (this.isRunning) return;
    this.isRunning = true;

    const dirs = this.config.watchDirs
      ? this.config.watchDirs.map((d) => path.resolve(this.config.projectRoot, d))
      : [this.config.projectRoot];

    for (const dir of dirs) {
      try {
        const watcher = fs.watch(dir, { recursive: true }, (eventType, filename) => {
          if (!filename) return;
          const fullPath = path.resolve(dir, filename);
          this.handleFileEvent(fullPath);
        });

        watcher.on('error', () => {
          // 忽略 watcher 错误，避免崩溃
        });

        this.watchers.push(watcher);
      } catch {
        // 目录不存在或无权限，跳过
      }
    }
  }

  /**
   * 停止文件监听
   */
  stop(): void {
    this.isRunning = false;

    for (const watcher of this.watchers) {
      try {
        watcher.close();
      } catch {
        // 忽略关闭错误
      }
    }
    this.watchers = [];

    for (const timer of this.debounceTimers.values()) {
      clearTimeout(timer);
    }
    this.debounceTimers.clear();
    this.pendingFiles.clear();
  }

  /**
   * 是否正在运行
   */
  isActive(): boolean {
    return this.isRunning;
  }

  /**
   * 处理文件变更事件（带防抖）
   */
  onFileChange(filePath: string): void {
    this.handleFileEvent(filePath);
  }

  private handleFileEvent(filePath: string): void {
    if (!this.isRunning) return;

    const ext = path.extname(filePath).toLowerCase();
    if (!this.config.watchExtensions.includes(ext)) return;

    if (filePath.includes('.codeflow')) return;
    if (filePath.includes('node_modules')) return;

    const existing = this.debounceTimers.get(filePath);
    if (existing) {
      clearTimeout(existing);
    }

    this.pendingFiles.add(filePath);

    const timer = setTimeout(() => {
      this.debounceTimers.delete(filePath);
      this.syncFile(filePath);
    }, this.config.debounceMs);

    this.debounceTimers.set(filePath, timer);
  }

  /**
   * 同步单个文件
   */
  private async syncFile(filePath: string): Promise<void> {
    this.pendingFiles.delete(filePath);

    try {
      await fs.promises.access(filePath, fs.constants.R_OK);
    } catch {
      return;
    }

    this.onProgress({
      type: 'start',
      filePath,
      total: 1,
      completed: 0,
    });

    try {
      await this.batchProjector.projectFile(filePath, this.config.projectRoot);

      this.onProgress({
        type: 'file_synced',
        filePath,
        total: 1,
        completed: 1,
      });
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      this.onProgress({
        type: 'file_failed',
        filePath,
        error: message,
      });
    }

    this.onProgress({
      type: 'complete',
      total: 1,
      completed: 1,
    });
  }

  /**
   * 手动触发全量同步
   */
  async syncAll(): Promise<void> {
    if (this.isSyncing) return;
    this.isSyncing = true;

    try {
      const dirs = this.config.watchDirs
        ? this.config.watchDirs.map((d) => path.resolve(this.config.projectRoot, d))
        : [this.config.projectRoot];

      this.onProgress({ type: 'start' });

      for (const dir of dirs) {
        await this.batchProjector.projectDirectory(dir, this.config.projectRoot);
      }

      this.onProgress({ type: 'complete' });
    } finally {
      this.isSyncing = false;
    }
  }

  /**
   * 获取待同步文件数
   */
  getPendingCount(): number {
    return this.pendingFiles.size;
  }
}
