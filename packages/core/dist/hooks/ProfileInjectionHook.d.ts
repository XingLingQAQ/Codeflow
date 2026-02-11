/**
 * ProfileInjectionHook - 用户画像自动注入
 *
 * 注册到 hook_before_send，在发送请求前自动将用户画像
 * 格式化为 system prompt 注入到消息列表中。
 */
import { HookManager } from '../hooks/HookManager.js';
import { UserProfileService } from '../memory/UserProfileService.js';
export interface ProfileInjectionConfig {
    /** 是否启用注入，默认 true */
    enabled: boolean;
    /** 用户 ID */
    userId: string;
    /** 注入位置：prepend（消息列表最前）或 append（system 消息后） */
    position: 'prepend' | 'append';
    /** 画像内容最大字符数 */
    maxProfileLength: number;
}
export declare class ProfileInjectionHook {
    private readonly hookManager;
    private readonly profileService;
    private config;
    private boundHandler;
    constructor(hookManager: HookManager, profileService: UserProfileService, config?: Partial<ProfileInjectionConfig>);
    /**
     * 注册到 hook_before_send
     */
    register(): void;
    /**
     * 注销
     */
    unregister(): void;
    /**
     * 启用/禁用注入
     */
    setEnabled(enabled: boolean): void;
    /**
     * 是否启用
     */
    isEnabled(): boolean;
    /**
     * 更新配置
     */
    updateConfig(config: Partial<ProfileInjectionConfig>): void;
    /**
     * hook_before_send 处理器：注入用户画像
     */
    private onBeforeSend;
    /**
     * 将用户画像格式化为 system prompt
     */
    private formatProfile;
    /**
     * 查找最后一个 system 消息的索引
     */
    private findLastSystemIndex;
}
//# sourceMappingURL=ProfileInjectionHook.d.ts.map