/**
 * UserProfileService - 用户画像动态更新服务
 *
 * 基于最近原子记忆，使用 LLM 分析并自动更新用户画像。
 * 支持本地存储、后端同步、定期更新触发。
 */
import { ICliAdapter } from '../adapters/types.js';
import { AtomicMemoryService } from './AtomicMemoryService.js';
/**
 * 用户画像分区
 */
export interface UserProfileSections {
    preferences: string;
    background: string;
    expertise: string[];
    communicationStyle: string;
    goals: string[];
}
/**
 * 用户画像元数据
 */
export interface UserProfileMetadata {
    totalSessions: number;
    totalMessages: number;
    lastActive: number;
}
/**
 * 用户画像
 */
export interface UserProfile {
    userId: string;
    lastUpdated: number;
    sections: UserProfileSections;
    metadata: UserProfileMetadata;
}
/**
 * 画像存储接口
 */
export interface IProfileStorage {
    load(userId: string): Promise<UserProfile | null>;
    save(profile: UserProfile): Promise<void>;
    delete(userId: string): Promise<void>;
}
/**
 * UserProfileService 配置
 */
export interface UserProfileServiceConfig {
    /** 更新间隔（秒），默认 3600（1 小时） */
    updateIntervalSec: number;
    /** 每次更新分析的最近记忆条数 */
    recentMemoryLimit: number;
    /** 后端 API 基础 URL */
    baseUrl: string;
    /** 请求超时（毫秒） */
    timeoutMs: number;
}
/**
 * 内存画像存储（本地缓存）
 */
export declare class InMemoryProfileStorage implements IProfileStorage {
    private profiles;
    load(userId: string): Promise<UserProfile | null>;
    save(profile: UserProfile): Promise<void>;
    delete(userId: string): Promise<void>;
}
/**
 * UserProfileService - 用户画像动态更新服务
 */
export declare class UserProfileService {
    private readonly llmAdapter;
    private readonly memoryService;
    private readonly storage;
    private readonly config;
    private updateTimer?;
    constructor(llmAdapter: Pick<ICliAdapter, 'send'>, memoryService: AtomicMemoryService, storage?: IProfileStorage, config?: Partial<UserProfileServiceConfig>);
    /**
     * 获取用户画像
     */
    getProfile(userId: string): Promise<UserProfile | null>;
    /**
     * 基于最近记忆更新用户画像
     * 使用 LLM 分析最近记忆并更新画像各分区
     */
    update(userId: string, sessionId: string): Promise<UserProfile>;
    /**
     * 启动定期更新触发器
     */
    startPeriodicUpdate(userId: string, sessionId: string): void;
    /**
     * 停止定期更新触发器
     */
    stopPeriodicUpdate(): void;
    /**
     * 保存画像（直接写入存储）
     */
    saveProfile(profile: UserProfile): Promise<void>;
    /**
     * 删除画像
     */
    deleteProfile(userId: string): Promise<void>;
    /**
     * 获取最近记忆
     */
    private fetchRecentMemories;
    /**
     * 使用 LLM 分析记忆并更新画像
     */
    private analyzeWithLLM;
    /**
     * 解析 LLM 响应为画像分区
     */
    private parseProfileResponse;
    /**
     * 从 LLM 输出中提取 JSON 对象
     */
    private extractJsonObject;
    /**
     * 空画像分区
     */
    private emptyProfileSections;
}
//# sourceMappingURL=UserProfileService.d.ts.map