/**
 * FolderMemoryService - MS-210 文件夹级记忆隔离
 *
 * 在 AtomicMemoryService 之上提供文件夹维度的记忆管理，
 * 确保所有操作都绑定到指定的 folderId。
 */

import { AtomicMemoryService } from './AtomicMemoryService.js';
import { AtomicMemory, AtomicMemorySource } from './types.js';

/**
 * 文件夹记忆（folderId 为必填）
 */
export interface FolderMemory extends AtomicMemory {
  folderId: string;
}

/**
 * 添加到文件夹的输入参数
 */
export interface AddToFolderInput {
  content: string;
  tags?: string[];
  sessionId: string;
  source: AtomicMemorySource;
  importance: number;
  embedding?: number[];
}

/**
 * 文件夹内搜索选项
 */
export interface FolderSearchOptions {
  limit?: number;
  offset?: number;
  tags?: string[];
  startAt?: number;
  endAt?: number;
}

/**
 * 跨文件夹搜索选项
 */
export interface CrossFolderSearchOptions extends FolderSearchOptions {
  folderIds?: string[];
}

/**
 * 文件夹信息
 */
export interface FolderInfo {
  folderId: string;
  memoryCount: number;
  latestTimestamp: number;
}

export class FolderMemoryService {
  private readonly memoryService: AtomicMemoryService;

  constructor(memoryService: AtomicMemoryService) {
    this.memoryService = memoryService;
  }

  /**
   * 向指定文件夹添加记忆
   */
  async addToFolder(folderId: string, input: AddToFolderInput): Promise<void> {
    const trimmedFolderId = folderId.trim();
    if (!trimmedFolderId) {
      throw new Error('folderId is required');
    }

    const memory: AtomicMemory = {
      id: this.generateId(),
      timestamp: Math.floor(Date.now() / 1000),
      content: input.content,
      tags: input.tags || [],
      sessionId: input.sessionId,
      folderId: trimmedFolderId,
      source: input.source,
      importance: input.importance,
      embedding: input.embedding,
    };

    await this.memoryService.add(memory);
  }

  /**
   * 在指定文件夹内搜索记忆
   */
  async searchInFolder(
    folderId: string,
    query: string,
    options?: FolderSearchOptions
  ): Promise<FolderMemory[]> {
    const trimmedFolderId = folderId.trim();
    if (!trimmedFolderId) {
      throw new Error('folderId is required');
    }

    const results = await this.memoryService.search(query, {
      folderId: trimmedFolderId,
      limit: options?.limit,
      offset: options?.offset,
      tags: options?.tags,
      startAt: options?.startAt,
      endAt: options?.endAt,
    });

    return results.filter(
      (m): m is FolderMemory => m.folderId === trimmedFolderId
    );
  }

  /**
   * 跨文件夹搜索记忆（不限定 folderId）
   */
  async searchAcrossFolders(
    query: string,
    options?: CrossFolderSearchOptions
  ): Promise<AtomicMemory[]> {
    const results = await this.memoryService.search(query, {
      limit: options?.limit,
      offset: options?.offset,
      tags: options?.tags,
      startAt: options?.startAt,
      endAt: options?.endAt,
    });

    if (options?.folderIds && options.folderIds.length > 0) {
      const folderSet = new Set(options.folderIds.map((id) => id.trim()));
      return results.filter((m) => m.folderId && folderSet.has(m.folderId));
    }

    return results;
  }

  /**
   * 获取指定文件夹内的所有记忆
   */
  async getFolder(folderId: string, sessionId: string): Promise<FolderMemory[]> {
    const trimmedFolderId = folderId.trim();
    if (!trimmedFolderId) {
      throw new Error('folderId is required');
    }

    const sessionMemories = await this.memoryService.getBySession(sessionId);
    return sessionMemories.filter(
      (m): m is FolderMemory => m.folderId === trimmedFolderId
    );
  }

  /**
   * 列出所有已知文件夹（基于 session 内的记忆）
   */
  async listFolders(sessionId: string): Promise<FolderInfo[]> {
    const memories = await this.memoryService.getBySession(sessionId);
    const folderMap = new Map<string, { count: number; latest: number }>();

    for (const memory of memories) {
      if (!memory.folderId) continue;

      const existing = folderMap.get(memory.folderId);
      if (existing) {
        existing.count++;
        if (memory.timestamp > existing.latest) {
          existing.latest = memory.timestamp;
        }
      } else {
        folderMap.set(memory.folderId, {
          count: 1,
          latest: memory.timestamp,
        });
      }
    }

    const folders: FolderInfo[] = [];
    for (const [folderId, info] of folderMap) {
      folders.push({
        folderId,
        memoryCount: info.count,
        latestTimestamp: info.latest,
      });
    }

    return folders.sort((a, b) => b.latestTimestamp - a.latestTimestamp);
  }

  /**
   * 删除指定文件夹内的所有记忆
   */
  async deleteFolder(folderId: string, sessionId: string): Promise<number> {
    const trimmedFolderId = folderId.trim();
    if (!trimmedFolderId) {
      throw new Error('folderId is required');
    }

    const memories = await this.getFolder(trimmedFolderId, sessionId);
    let deleted = 0;

    for (const memory of memories) {
      await this.memoryService.delete(memory.id);
      deleted++;
    }

    return deleted;
  }

  private generateId(): string {
    const timestamp = Date.now().toString(36);
    const random = Math.random().toString(36).slice(2, 9);
    return `fm-${timestamp}-${random}`;
  }
}
