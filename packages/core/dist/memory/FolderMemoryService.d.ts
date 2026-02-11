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
export declare class FolderMemoryService {
    private readonly memoryService;
    constructor(memoryService: AtomicMemoryService);
    /**
     * 向指定文件夹添加记忆
     */
    addToFolder(folderId: string, input: AddToFolderInput): Promise<void>;
    /**
     * 在指定文件夹内搜索记忆
     */
    searchInFolder(folderId: string, query: string, options?: FolderSearchOptions): Promise<FolderMemory[]>;
    /**
     * 跨文件夹搜索记忆（不限定 folderId）
     */
    searchAcrossFolders(query: string, options?: CrossFolderSearchOptions): Promise<AtomicMemory[]>;
    /**
     * 获取指定文件夹内的所有记忆
     */
    getFolder(folderId: string, sessionId: string): Promise<FolderMemory[]>;
    /**
     * 列出所有已知文件夹（基于 session 内的记忆）
     */
    listFolders(sessionId: string): Promise<FolderInfo[]>;
    /**
     * 删除指定文件夹内的所有记忆
     */
    deleteFolder(folderId: string, sessionId: string): Promise<number>;
    private generateId;
}
//# sourceMappingURL=FolderMemoryService.d.ts.map