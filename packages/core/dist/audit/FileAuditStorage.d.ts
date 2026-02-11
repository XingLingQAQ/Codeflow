/**
 * 文件审计存储实现
 * JSONL 格式持久化 + 哈希链完整性验证 + 日志轮转
 */
import { IAuditStorage, AuditLogEntry, AuditQuery } from './types.js';
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
export declare const DEFAULT_FILE_STORAGE_CONFIG: FileStorageConfig;
/**
 * 文件审计存储实现
 */
export declare class FileAuditStorage implements IAuditStorage {
    private config;
    private currentFile;
    private writeBuffer;
    private flushTimer;
    private initialized;
    private entryIndex;
    private lastEntry;
    constructor(config?: Partial<FileStorageConfig>);
    /**
     * 初始化存储
     */
    initialize(): Promise<void>;
    /**
     * 追加日志条目
     */
    append(entry: AuditLogEntry): Promise<void>;
    /**
     * 获取单条日志
     */
    get(id: string): Promise<AuditLogEntry | null>;
    /**
     * 查询日志
     */
    query(query: AuditQuery): Promise<AuditLogEntry[]>;
    /**
     * 计数
     */
    count(query?: AuditQuery): Promise<number>;
    /**
     * 获取最后一条日志
     */
    getLastEntry(): Promise<AuditLogEntry | null>;
    /**
     * 删除日志（标记删除，不物理删除以保持哈希链）
     */
    delete(ids: string[]): Promise<number>;
    /**
     * 清空存储
     */
    clear(): Promise<void>;
    /**
     * 关闭存储
     */
    close(): Promise<void>;
    /**
     * 验证哈希链完整性
     */
    verifyHashChain(): Promise<boolean>;
    /**
     * 获取存储统计
     */
    getStorageStats(): Promise<{
        totalFiles: number;
        totalSize: number;
        totalEntries: number;
        currentFileSize: number;
    }>;
    private ensureInitialized;
    private getCurrentLogFile;
    private createNewLogFile;
    private getLogFiles;
    private rotateIfNeeded;
    private buildIndex;
    private readEntryFromFile;
    private readEntriesFromFile;
    private matchesQuery;
    private flush;
    private countLines;
    private startFlushTimer;
    private stopFlushTimer;
    private calculateHash;
}
/**
 * 创建文件审计存储实例并初始化
 */
export declare function createFileAuditStorage(config?: Partial<FileStorageConfig>): Promise<FileAuditStorage>;
//# sourceMappingURL=FileAuditStorage.d.ts.map