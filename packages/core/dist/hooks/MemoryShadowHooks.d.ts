/**
 * MemoryShadowHooks - 记忆与影子系统 Hook 集成
 *
 * 将记忆提取、画像更新、影子目录初始化集成到 Hook Bus：
 * - hook_post_response: 提取记忆
 * - hook_on_message_complete: 更新用户画像
 * - project:init (手动触发): 初始化影子目录
 */
import { HookManager } from './HookManager.js';
import { MemoryExtractor } from '../memory/MemoryExtractor.js';
import { UserProfileService } from '../memory/UserProfileService.js';
import { ShadowScaffold } from '../shadow/ShadowScaffold.js';
export interface MemoryShadowHooksConfig {
    /** 用户 ID */
    userId: string;
    /** 当前会话 ID */
    sessionId: string;
    /** 项目根路径 */
    projectRoot: string;
    /** 是否启用记忆提取 */
    enableMemoryExtraction: boolean;
    /** 是否启用画像更新 */
    enableProfileUpdate: boolean;
    /** 画像更新的最小消息间隔（条数） */
    profileUpdateInterval: number;
}
export declare class MemoryShadowHooks {
    private readonly hookManager;
    private readonly memoryExtractor?;
    private readonly profileService?;
    private readonly shadowScaffold;
    private readonly config;
    private messageCount;
    private lastUserMessage;
    constructor(hookManager: HookManager, config?: Partial<MemoryShadowHooksConfig>, memoryExtractor?: MemoryExtractor, profileService?: UserProfileService, shadowScaffold?: ShadowScaffold);
    /**
     * 注册所有 hooks
     */
    register(): void;
    /**
     * 注销所有 hooks
     */
    unregister(): void;
    /**
     * 初始化影子目录（project:init 触发）
     */
    initializeShadowDirectory(): Promise<void>;
    /**
     * hook_post_response: 提取记忆
     *
     * 在 AI 响应后，异步提取对话中的记忆
     */
    private onPostResponse;
    /**
     * hook_on_message_complete: 更新用户画像
     *
     * 在消息完成后，按间隔触发画像更新
     */
    private onMessageComplete;
    /**
     * 获取当前消息计数
     */
    getMessageCount(): number;
    /**
     * 重置消息计数
     */
    resetMessageCount(): void;
}
//# sourceMappingURL=MemoryShadowHooks.d.ts.map