/**
 * MemoryInjectionHook - MS-220 记忆自动注入
 *
 * 注册到 HookEvent.BEFORE_SEND，在发送请求前自动检索相关记忆
 * 并注入为 system 消息。
 *
 * 支持两种模式：
 * - agent: 使用后端 MemoryAgent /agent/context API（默认）
 * - local: 使用前端 PassiveRAG 本地检索（兼容回退）
 */
import { HookEvent } from './types.js';
const DEFAULT_CONFIG = {
    enabled: true,
    position: 'prepend',
    maxInjectionLength: 2000,
    mode: 'agent',
};
export class MemoryInjectionHook {
    constructor(hookManager, ragService, config = {}, agentClient) {
        this.hookManager = hookManager;
        this.ragService = ragService;
        this.agentClient = agentClient;
        this.config = { ...DEFAULT_CONFIG, ...config };
        this.boundHandler = this.onBeforeSend.bind(this);
    }
    /**
     * 注册到 hook_before_send
     */
    register() {
        this.hookManager.register(HookEvent.BEFORE_SEND, this.boundHandler);
    }
    /**
     * 注销
     */
    unregister() {
        this.hookManager.unregister(HookEvent.BEFORE_SEND, this.boundHandler);
    }
    /**
     * 启用注入
     */
    enable() {
        this.config.enabled = true;
    }
    /**
     * 禁用注入
     */
    disable() {
        this.config.enabled = false;
    }
    /**
     * 是否启用
     */
    isEnabled() {
        return this.config.enabled;
    }
    /**
     * hook_before_send 处理器
     */
    async onBeforeSend(payload) {
        if (!this.config.enabled) {
            return payload;
        }
        try {
            const lastUserMessage = this.extractLastUserMessage(payload);
            if (!lastUserMessage) {
                return payload;
            }
            let contextText;
            if (this.config.mode === 'agent' && this.agentClient) {
                // 后端 MemoryAgent 模式
                const result = await this.agentClient.assembleContext({
                    session_id: this.config.sessionId || '',
                    query: lastUserMessage,
                    max_tokens: Math.floor(this.config.maxInjectionLength / 4),
                });
                contextText = result.context_block;
            }
            else {
                // 本地 PassiveRAG 模式（兼容回退）
                const memories = await this.ragService.retrieve(lastUserMessage, this.config.sessionId);
                if (memories.length === 0) {
                    return payload;
                }
                contextText = this.ragService.formatForInjection(memories);
            }
            if (!contextText) {
                return payload;
            }
            if (contextText.length > this.config.maxInjectionLength) {
                contextText = contextText.slice(0, this.config.maxInjectionLength) + '\n...（已截断）';
            }
            const memoryMessage = {
                role: 'system',
                content: contextText,
                timestamp: Date.now(),
            };
            const messages = [...payload.messages];
            if (this.config.position === 'prepend') {
                messages.unshift(memoryMessage);
            }
            else {
                const lastSystemIndex = this.findLastSystemIndex(messages);
                messages.splice(lastSystemIndex + 1, 0, memoryMessage);
            }
            return { ...payload, messages };
        }
        catch {
            return payload;
        }
    }
    /**
     * 提取最后一条用户消息
     */
    extractLastUserMessage(payload) {
        for (let i = payload.messages.length - 1; i >= 0; i--) {
            if (payload.messages[i].role === 'user') {
                return payload.messages[i].content;
            }
        }
        return null;
    }
    /**
     * 查找最后一个 system 消息的索引
     */
    findLastSystemIndex(messages) {
        for (let i = messages.length - 1; i >= 0; i--) {
            if (messages[i].role === 'system') {
                return i;
            }
        }
        return -1;
    }
}
//# sourceMappingURL=MemoryInjectionHook.js.map