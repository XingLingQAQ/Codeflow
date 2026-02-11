/**
 * MemoryShadowHooks - 记忆与影子系统 Hook 集成
 *
 * 将记忆提取、画像更新、影子目录初始化集成到 Hook Bus：
 * - hook_post_response: 提取记忆
 * - hook_on_message_complete: 更新用户画像
 * - project:init (手动触发): 初始化影子目录
 */
import { HookEvent } from './types.js';
import { ShadowScaffold } from '../shadow/ShadowScaffold.js';
const DEFAULT_CONFIG = {
    userId: 'default',
    sessionId: '',
    projectRoot: '.',
    enableMemoryExtraction: true,
    enableProfileUpdate: true,
    profileUpdateInterval: 10,
};
export class MemoryShadowHooks {
    constructor(hookManager, config = {}, memoryExtractor, profileService, shadowScaffold) {
        this.messageCount = 0;
        this.lastUserMessage = '';
        this.hookManager = hookManager;
        this.config = { ...DEFAULT_CONFIG, ...config };
        this.memoryExtractor = memoryExtractor;
        this.profileService = profileService;
        this.shadowScaffold = shadowScaffold || new ShadowScaffold();
    }
    /**
     * 注册所有 hooks
     */
    register() {
        this.hookManager.register(HookEvent.POST_RESPONSE, this.onPostResponse.bind(this));
        this.hookManager.register(HookEvent.MESSAGE_COMPLETE, this.onMessageComplete.bind(this));
    }
    /**
     * 注销所有 hooks
     */
    unregister() {
        this.hookManager.unregister(HookEvent.POST_RESPONSE, this.onPostResponse.bind(this));
        this.hookManager.unregister(HookEvent.MESSAGE_COMPLETE, this.onMessageComplete.bind(this));
    }
    /**
     * 初始化影子目录（project:init 触发）
     */
    async initializeShadowDirectory() {
        await this.shadowScaffold.initialize(this.config.projectRoot);
    }
    /**
     * hook_post_response: 提取记忆
     *
     * 在 AI 响应后，异步提取对话中的记忆
     */
    async onPostResponse(response) {
        if (!this.config.enableMemoryExtraction || !this.memoryExtractor) {
            return;
        }
        if (!this.config.sessionId || !this.lastUserMessage) {
            return;
        }
        try {
            this.memoryExtractor.extractFromConversation(this.lastUserMessage, response.content || '', this.config.sessionId);
        }
        catch {
            // 记忆提取失败不阻塞主流程
        }
    }
    /**
     * hook_on_message_complete: 更新用户画像
     *
     * 在消息完成后，按间隔触发画像更新
     */
    async onMessageComplete(message) {
        // 记录最后的用户消息（用于记忆提取）
        if (message.role === 'user') {
            this.lastUserMessage = message.content;
        }
        this.messageCount++;
        if (!this.config.enableProfileUpdate || !this.profileService) {
            return;
        }
        if (!this.config.sessionId || !this.config.userId) {
            return;
        }
        // 按间隔触发画像更新
        if (this.messageCount % this.config.profileUpdateInterval !== 0) {
            return;
        }
        try {
            await this.profileService.update(this.config.userId, this.config.sessionId);
        }
        catch {
            // 画像更新失败不阻塞主流程
        }
    }
    /**
     * 获取当前消息计数
     */
    getMessageCount() {
        return this.messageCount;
    }
    /**
     * 重置消息计数
     */
    resetMessageCount() {
        this.messageCount = 0;
    }
}
//# sourceMappingURL=MemoryShadowHooks.js.map