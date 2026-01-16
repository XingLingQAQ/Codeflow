/**
 * 文件审计存储实现
 * JSONL 格式持久化 + 哈希链完整性验证 + 日志轮转
 */

import * as fs from 'fs';
import * as path from 'path';
import * as crypto from 'crypto';
import * as readline from 'readline';
import {
  IAuditStorage,
  AuditLogEntry,
  AuditQuery,
  GENESIS_HASH,
} from './types.js';

/**
 * 文件存储配置
 */
export interface FileStorageConfig {
  logDir: string;
  filePrefix: string;
  maxFileSize: number;
  maxFiles: number;
  verifyOnStartup: boolean;
  flushInterval: number;
}

/**
 * 默认文件存储配置
 */
export const DEFAULT_FILE_STORAGE_CONFIG: FileStorageConfig = {
  logDir: './audit-logs',
  filePrefix: 'audit',
  maxFileSize: 10 * 1024 * 1024, // 10MB
  maxFiles: 10,
  verifyOnStartup: true,
  flushInterval: 1000,
};

/**
 * 文件审计存储实现
 */
export class FileAuditStorage implements IAuditStorage {
  private config: FileStorageConfig;
  private currentFile: string = '';
  private writeBuffer: AuditLogEntry[] = [];
  private flushTimer: NodeJS.Timeout | null = null;
  private initialized: boolean = false;
  private entryIndex: Map<string, { file: string; line: number }> = new Map();
  private lastEntry: AuditLogEntry | null = null;

  constructor(config?: Partial<FileStorageConfig>) {
    this.config = { ...DEFAULT_FILE_STORAGE_CONFIG, ...config };
  }

  /**
   * 初始化存储
   */
  async initialize(): Promise<void> {
    if (this.initialized) return;

    // 确保目录存在
    await fs.promises.mkdir(this.config.logDir, { recursive: true });

    // 获取或创建当前日志文件
    this.currentFile = await this.getCurrentLogFile();

    // 构建索引并获取最后一条记录
    await this.buildIndex();

    // 验证哈希链完整性
    if (this.config.verifyOnStartup) {
      const isValid = await this.verifyHashChain();
      if (!isValid) {
        console.warn('[FileAuditStorage] Hash chain integrity verification failed');
      }
    }

    // 启动定时刷新
    this.startFlushTimer();

    this.initialized = true;
  }

  /**
   * 追加日志条目
   */
  async append(entry: AuditLogEntry): Promise<void> {
    await this.ensureInitialized();

    this.writeBuffer.push(entry);
    this.lastEntry = entry;

    // 如果缓冲区较大，立即刷新
    if (this.writeBuffer.length >= 100) {
      await this.flush();
    }
  }

  /**
   * 获取单条日志
   */
  async get(id: string): Promise<AuditLogEntry | null> {
    await this.ensureInitialized();

    // 先检查写缓冲区
    const buffered = this.writeBuffer.find(e => e.id === id);
    if (buffered) return buffered;

    // 从索引查找
    const location = this.entryIndex.get(id);
    if (!location) return null;

    return this.readEntryFromFile(location.file, location.line);
  }

  /**
   * 查询日志
   */
  async query(query: AuditQuery): Promise<AuditLogEntry[]> {
    await this.ensureInitialized();

    // 刷新缓冲区确保数据完整
    await this.flush();

    const results: AuditLogEntry[] = [];
    const files = await this.getLogFiles();

    // 从最新文件开始读取
    for (const file of files.reverse()) {
      const entries = await this.readEntriesFromFile(file);

      for (const entry of entries) {
        if (this.matchesQuery(entry, query)) {
          results.push(entry);
        }

        // 检查是否达到限制
        if (query.limit && results.length >= query.limit + (query.offset || 0)) {
          break;
        }
      }

      if (query.limit && results.length >= query.limit + (query.offset || 0)) {
        break;
      }
    }

    // 按时间排序（最新在前）
    results.sort((a, b) => b.timestamp - a.timestamp);

    // 应用分页
    const offset = query.offset || 0;
    const limit = query.limit || results.length;

    return results.slice(offset, offset + limit);
  }

  /**
   * 计数
   */
  async count(query?: AuditQuery): Promise<number> {
    await this.ensureInitialized();
    await this.flush();

    if (!query) {
      return this.entryIndex.size + this.writeBuffer.length;
    }

    const entries = await this.query({ ...query, limit: undefined, offset: undefined });
    return entries.length;
  }

  /**
   * 获取最后一条日志
   */
  async getLastEntry(): Promise<AuditLogEntry | null> {
    await this.ensureInitialized();

    // 优先返回缓冲区中的最后一条
    if (this.writeBuffer.length > 0) {
      return this.writeBuffer[this.writeBuffer.length - 1];
    }

    return this.lastEntry;
  }

  /**
   * 删除日志（标记删除，不物理删除以保持哈希链）
   */
  async delete(ids: string[]): Promise<number> {
    // 文件存储不支持物理删除（会破坏哈希链）
    // 可以实现软删除或归档
    console.warn('[FileAuditStorage] Delete operation not supported for file storage (would break hash chain)');
    return 0;
  }

  /**
   * 清空存储
   */
  async clear(): Promise<void> {
    await this.ensureInitialized();

    // 停止刷新定时器
    this.stopFlushTimer();

    // 清空缓冲区
    this.writeBuffer = [];
    this.entryIndex.clear();
    this.lastEntry = null;

    // 删除所有日志文件
    const files = await this.getLogFiles();
    for (const file of files) {
      await fs.promises.unlink(file).catch(() => {});
    }

    // 重新创建当前文件
    this.currentFile = await this.getCurrentLogFile();

    // 重启刷新定时器
    this.startFlushTimer();
  }

  /**
   * 关闭存储
   */
  async close(): Promise<void> {
    this.stopFlushTimer();
    await this.flush();
    this.initialized = false;
  }

  /**
   * 验证哈希链完整性
   */
  async verifyHashChain(): Promise<boolean> {
    const files = await this.getLogFiles();
    let expectedPreviousHash = GENESIS_HASH;
    let isValid = true;

    for (const file of files) {
      const entries = await this.readEntriesFromFile(file);

      for (const entry of entries) {
        // 验证链接
        if (entry.previousHash !== expectedPreviousHash) {
          console.error(`[FileAuditStorage] Chain broken at entry ${entry.id}: expected ${expectedPreviousHash}, got ${entry.previousHash}`);
          isValid = false;
        }

        // 验证哈希
        const calculatedHash = this.calculateHash(entry);
        if (calculatedHash !== entry.hash) {
          console.error(`[FileAuditStorage] Invalid hash for entry ${entry.id}`);
          isValid = false;
        }

        expectedPreviousHash = entry.hash;
      }
    }

    return isValid;
  }

  /**
   * 获取存储统计
   */
  async getStorageStats(): Promise<{
    totalFiles: number;
    totalSize: number;
    totalEntries: number;
    currentFileSize: number;
  }> {
    const files = await this.getLogFiles();
    let totalSize = 0;

    for (const file of files) {
      const stat = await fs.promises.stat(file);
      totalSize += stat.size;
    }

    const currentStat = await fs.promises.stat(this.currentFile).catch(() => ({ size: 0 }));

    return {
      totalFiles: files.length,
      totalSize,
      totalEntries: this.entryIndex.size + this.writeBuffer.length,
      currentFileSize: currentStat.size,
    };
  }

  // ==================== 私有方法 ====================

  private async ensureInitialized(): Promise<void> {
    if (!this.initialized) {
      await this.initialize();
    }
  }

  private async getCurrentLogFile(): Promise<string> {
    const files = await this.getLogFiles();

    if (files.length === 0) {
      return this.createNewLogFile();
    }

    const latestFile = files[files.length - 1];
    const stat = await fs.promises.stat(latestFile);

    if (stat.size >= this.config.maxFileSize) {
      return this.createNewLogFile();
    }

    return latestFile;
  }

  private async createNewLogFile(): Promise<string> {
    const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
    const filename = `${this.config.filePrefix}_${timestamp}.jsonl`;
    const filepath = path.join(this.config.logDir, filename);

    await fs.promises.writeFile(filepath, '', 'utf8');

    // 检查是否需要轮转
    await this.rotateIfNeeded();

    return filepath;
  }

  private async getLogFiles(): Promise<string[]> {
    try {
      const files = await fs.promises.readdir(this.config.logDir);
      return files
        .filter(f => f.startsWith(this.config.filePrefix) && f.endsWith('.jsonl'))
        .map(f => path.join(this.config.logDir, f))
        .sort();
    } catch {
      return [];
    }
  }

  private async rotateIfNeeded(): Promise<void> {
    const files = await this.getLogFiles();

    if (files.length > this.config.maxFiles) {
      const toDelete = files.slice(0, files.length - this.config.maxFiles);

      for (const file of toDelete) {
        await fs.promises.unlink(file).catch(() => {});

        // 从索引中移除
        for (const [id, location] of this.entryIndex) {
          if (location.file === file) {
            this.entryIndex.delete(id);
          }
        }
      }
    }
  }

  private async buildIndex(): Promise<void> {
    const files = await this.getLogFiles();

    for (const file of files) {
      let lineNumber = 0;
      const fileStream = fs.createReadStream(file);
      const rl = readline.createInterface({
        input: fileStream,
        crlfDelay: Infinity,
      });

      for await (const line of rl) {
        if (line.trim()) {
          try {
            const entry = JSON.parse(line) as AuditLogEntry;
            this.entryIndex.set(entry.id, { file, line: lineNumber });
            this.lastEntry = entry;
          } catch {
            // 跳过无效行
          }
        }
        lineNumber++;
      }
    }
  }

  private async readEntryFromFile(file: string, lineNumber: number): Promise<AuditLogEntry | null> {
    const fileStream = fs.createReadStream(file);
    const rl = readline.createInterface({
      input: fileStream,
      crlfDelay: Infinity,
    });

    let currentLine = 0;
    for await (const line of rl) {
      if (currentLine === lineNumber && line.trim()) {
        try {
          return JSON.parse(line) as AuditLogEntry;
        } catch {
          return null;
        }
      }
      currentLine++;
    }

    return null;
  }

  private async readEntriesFromFile(file: string): Promise<AuditLogEntry[]> {
    const entries: AuditLogEntry[] = [];
    const fileStream = fs.createReadStream(file);
    const rl = readline.createInterface({
      input: fileStream,
      crlfDelay: Infinity,
    });

    for await (const line of rl) {
      if (line.trim()) {
        try {
          entries.push(JSON.parse(line) as AuditLogEntry);
        } catch {
          // 跳过无效行
        }
      }
    }

    return entries;
  }

  private matchesQuery(entry: AuditLogEntry, query: AuditQuery): boolean {
    if (query.startTime && entry.timestamp < query.startTime) return false;
    if (query.endTime && entry.timestamp > query.endTime) return false;
    if (query.eventTypes?.length && !query.eventTypes.includes(entry.eventType)) return false;
    if (query.severities?.length && !query.severities.includes(entry.severity)) return false;
    if (query.actorId && entry.actor.id !== query.actorId) return false;
    if (query.resourceId && entry.resource.id !== query.resourceId) return false;
    if (query.resourceType && entry.resource.type !== query.resourceType) return false;
    if (query.outcome && entry.outcome !== query.outcome) return false;

    return true;
  }

  private async flush(): Promise<void> {
    if (this.writeBuffer.length === 0) return;

    // 检查是否需要轮转
    const stat = await fs.promises.stat(this.currentFile).catch(() => ({ size: 0 }));
    if (stat.size >= this.config.maxFileSize) {
      this.currentFile = await this.createNewLogFile();
    }

    // 写入缓冲区内容
    const lines = this.writeBuffer.map(e => JSON.stringify(e)).join('\n') + '\n';
    await fs.promises.appendFile(this.currentFile, lines, 'utf8');

    // 更新索引
    const currentLineCount = await this.countLines(this.currentFile);
    let lineOffset = currentLineCount - this.writeBuffer.length;

    for (const entry of this.writeBuffer) {
      this.entryIndex.set(entry.id, { file: this.currentFile, line: lineOffset });
      lineOffset++;
    }

    // 清空缓冲区
    this.writeBuffer = [];
  }

  private async countLines(file: string): Promise<number> {
    const content = await fs.promises.readFile(file, 'utf8');
    return content.split('\n').filter(line => line.trim()).length;
  }

  private startFlushTimer(): void {
    if (this.flushTimer) return;

    this.flushTimer = setInterval(() => {
      this.flush().catch(err => {
        console.error('[FileAuditStorage] Flush error:', err);
      });
    }, this.config.flushInterval);
  }

  private stopFlushTimer(): void {
    if (this.flushTimer) {
      clearInterval(this.flushTimer);
      this.flushTimer = null;
    }
  }

  private calculateHash(entry: AuditLogEntry): string {
    const data = {
      id: entry.id,
      timestamp: entry.timestamp,
      eventType: entry.eventType,
      severity: entry.severity,
      actor: entry.actor,
      resource: entry.resource,
      action: entry.action,
      outcome: entry.outcome,
      details: entry.details,
      previousHash: entry.previousHash,
    };

    return crypto
      .createHash('sha256')
      .update(JSON.stringify(data))
      .digest('hex');
  }
}

/**
 * 创建文件审计存储实例并初始化
 */
export async function createFileAuditStorage(
  config?: Partial<FileStorageConfig>
): Promise<FileAuditStorage> {
  const storage = new FileAuditStorage(config);
  await storage.initialize();
  return storage;
}
