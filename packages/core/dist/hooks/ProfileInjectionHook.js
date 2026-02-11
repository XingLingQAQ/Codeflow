/**
 * ProfileInjectionHook - 用户画像自动注入
 *
 * 注册到 hook_before_send，在发送请求前自动将用户画像
 * 格式化为 system prompt 注入到消息列表中。
 */
import { HookEvent } from '../hooks/types.js';
const DEFAULT_CONFIG = {
    enabled: true,
    userId: 'default',
    position: 'prepend',
    maxProfileLength: 1000,
};
export class ProfileInjectionHook {
    constructor(hookManager, profileService, config = {}) {
        this.hookManager = hookManager;
        this.profileService = profileService;
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
     * 启用/禁用注入
     */
    setEnabled(enabled) {
        this.config.enabled = enabled;
    }
    /**
     * 是否启用
     */
    isEnabled() {
        return this.config.enabled;
    }
    /**
     * 更新配置
     */
    updateConfig(config) {
        this.config = { ...this.config, ...config };
    }
    /**
     * hook_before_send 处理器：注入用户画像
     */
    async onBeforeSend(payload) {
        if (!this.config.enabled) {
            return payload;
        }
        try {
            const profile = await this.profileService.getProfile(this.config.userId);
            if (!profile) {
                return payload;
            }
            const profileContent = this.formatProfile(profile);
            if (!profileContent) {
                return payload;
            }
            const profileMessage = {
                role: 'system',
                content: profileContent,
                timestamp: Date.now(),
            };
            const messages = [...payload.messages];
            if (this.config.position === 'prepend') {
                messages.unshift(profileMessage);
            }
            else {
                // 在最后一个 system 消息后插入
                const lastSystemIndex = this.findLastSystemIndex(messages);
                messages.splice(lastSystemIndex + 1, 0, profileMessage);
            }
            return { ...payload, messages };
        }
        catch {
            // 注入失败不阻塞发送
            return payload;
        }
    }
    /**
     * 将用户画像格式化为 system prompt
     */
    formatProfile(profile) {
        const parts = ['[用户画像]'];
        const s = profile.sections;
        if (s.preferences) {
            parts.push(`偏好: ${s.preferences}`);
        }
        if (s.background) {
            parts.push(`背景: ${s.background}`);
        }
        if (s.expertise.length > 0) {
            parts.push(`专业领域: ${s.expertise.join(', ')}`);
        }
        if (s.communicationStyle) {
            parts.push(`沟通风格: ${s.communicationStyle}`);
        }
        if (s.goals.length > 0) {
            parts.push(`目标: ${s.goals.join(', ')}`);
        }
        if (parts.length <= 1) {
            return '';
        }
        const content = parts.join('\n');
        if (content.length > this.config.maxProfileLength) {
            return content.slice(0, this.config.maxProfileLength) + '...';
        }
        return content;
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
//# sourceMappingURL=ProfileInjectionHook.js.map