/**
 * SessionJournal - 会话日志持久化和上下文恢复
 * 实现会话摘要生成、存储和自动加载
 */
import { EventEmitter } from 'events';
/**
 * 会话条目
 */
export interface SessionEntry {
    role: 'user' | 'assistant' | 'system';
    content: string;
    timestamp: number;
    metadata?: Record<string, unknown>;
}
/**
 * 会话摘要
 */
export interface SessionSummary {
    id: string;
    sessionId: string;
    userId: string;
    title: string;
    summary: string;
    keyPoints: string[];
    decisions: string[];
    codeChanges: CodeChange[];
    tags: string[];
    startTime: number;
    endTime: number;
    entryCount: number;
    createdAt: number;
}
/**
 * 代码变更
 */
export interface CodeChange {
    file: string;
    type: 'created' | 'modified' | 'deleted';
    description: string;
}
/**
 * 日志索引条目
 */
export interface JournalIndexEntry {
    id: string;
    sessionId: string;
    title: string;
    summary: string;
    tags: string[];
    date: string;
    path: string;
}
/**
 * 日志索引
 */
export interface JournalIndex {
    version: string;
    userId: string;
    lastUpdated: number;
    entries: JournalIndexEntry[];
}
/**
 * 上下文恢复结果
 */
export interface ContextRestoreResult {
    summaries: SessionSummary[];
    relevantContext: string;
    suggestions: string[];
}
/**
 * Session Journal 配置
 */
export interface SessionJournalConfig {
    workspaceDir: string;
    maxJournals: number;
    maxContextLength: number;
    autoSave: boolean;
    summaryModel?: string;
}
/**
 * 摘要生成回调
 */
export type SummaryGeneratorCallback = (entries: SessionEntry[]) => Promise<SessionSummary>;
/**
 * JournalWriter - 日志写入器
 */
export declare class JournalWriter extends EventEmitter {
    private config;
    constructor(config?: Partial<SessionJournalConfig>);
    /**
     * 写入会话摘要
     */
    write(summary: SessionSummary): Promise<string>;
    /**
     * 生成文件名
     */
    private generateFilename;
    /**
     * 格式化摘要为 Markdown
     */
    private formatSummary;
    /**
     * 格式化持续时间
     */
    private formatDuration;
    /**
     * 确保目录存在
     */
    private ensureDir;
}
/**
 * JournalIndex - 日志索引管理
 */
export declare class JournalIndexManager extends EventEmitter {
    private config;
    private indices;
    constructor(config?: Partial<SessionJournalConfig>);
    /**
     * 加载用户索引
     */
    loadIndex(userId: string): Promise<JournalIndex>;
    /**
     * 添加条目到索引
     */
    addEntry(userId: string, entry: JournalIndexEntry): Promise<void>;
    /**
     * 保存索引
     */
    saveIndex(userId: string, index: JournalIndex): Promise<void>;
    /**
     * 搜索索引
     */
    search(userId: string, query: string): Promise<JournalIndexEntry[]>;
    /**
     * 获取最近条目
     */
    getRecent(userId: string, count?: number): Promise<JournalIndexEntry[]>;
    /**
     * 获取索引路径
     */
    private getIndexPath;
    /**
     * 解析索引文件
     */
    private parseIndex;
    /**
     * 格式化索引为 Markdown
     */
    private formatIndex;
}
/**
 * ContextRestorer - 上下文恢复器
 */
export declare class ContextRestorer extends EventEmitter {
    private config;
    private indexManager;
    constructor(config?: Partial<SessionJournalConfig>, indexManager?: JournalIndexManager);
    /**
     * 恢复上下文
     */
    restore(userId: string, query?: string): Promise<ContextRestoreResult>;
    /**
     * 加载摘要
     */
    private loadSummary;
    /**
     * 解析摘要文件
     */
    private parseSummary;
    /**
     * 构建上下文
     */
    private buildContext;
    /**
     * 生成建议
     */
    private generateSuggestions;
}
/**
 * SessionJournal - 会话日志管理器
 */
export declare class SessionJournal extends EventEmitter {
    private config;
    private writer;
    private indexManager;
    private restorer;
    private currentSession;
    private currentSessionId;
    private currentUserId;
    private sessionStartTime;
    private summaryGenerator?;
    constructor(config?: Partial<SessionJournalConfig>, summaryGenerator?: SummaryGeneratorCallback);
    /**
     * 开始新会话
     */
    startSession(sessionId: string, userId: string): void;
    /**
     * 添加会话条目
     */
    addEntry(entry: Omit<SessionEntry, 'timestamp'>): void;
    /**
     * 结束会话并生成摘要
     */
    endSession(): Promise<SessionSummary | null>;
    /**
     * 生成摘要
     */
    private generateSummary;
    /**
     * 默认摘要生成
     */
    private generateDefaultSummary;
    /**
     * 提取标签
     */
    private extractTags;
    /**
     * 恢复上下文
     */
    restoreContext(userId: string, query?: string): Promise<ContextRestoreResult>;
    /**
     * 获取最近日志
     */
    getRecentJournals(userId: string, count?: number): Promise<JournalIndexEntry[]>;
    /**
     * 搜索日志
     */
    searchJournals(userId: string, query: string): Promise<JournalIndexEntry[]>;
    /**
     * 获取当前会话条目
     */
    getCurrentSession(): SessionEntry[];
    /**
     * 获取当前会话 ID
     */
    getCurrentSessionId(): string;
    /**
     * 更新配置
     */
    updateConfig(config: Partial<SessionJournalConfig>): void;
    /**
     * 获取配置
     */
    getConfig(): SessionJournalConfig;
}
//# sourceMappingURL=SessionJournal.d.ts.map