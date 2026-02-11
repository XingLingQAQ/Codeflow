/**
 * MemoryInjectionHook - MS-220 记忆自动注入
 *
 * 注册到 HookEvent.BEFORE_SEND，在发送请求前自动检索相关记忆
 * 并注入为 system 消息。
 */
import { HookManager } from './HookManager.js';
import { PassiveRAGService } from '../memory/PassiveRAG.js';
/**
 * MemoryInjectionHook 配置
 */
export interface MemoryInjectionConfig {
    /** 是否启用注入 */
    enabled: boolean;
    /** 默认 sessionId */
    sessionId?: string;
    /** 注入位置 */
    position: 'prepend' | 'append';
    /** 最大注入字符数 */
    maxInjectionLength: number;
}
export declare class MemoryInjectionHook {
    private readonly hookManager;
    private readonly ragService;
    private config;
    private boundHandler;
    constructor(hookManager: HookManager, ragService: PassiveRAGService, config?: Partial<MemoryInjectionConfig>);
    /**
     * 注册到 hook_before_send
     */
    register(): void;
    /**
     * 注销
     */
    unregister(): void;
    /**
     * 启用注入
     */
    enable(): void;
    /**
     * 禁用注入
     */
    disable(): void;
    /**
     * 是否启用
     */
    isEnabled(): boolean;
    /**
     * hook_before_send 处理器
     */
    private onBeforeSend;
    /**
     * 提取最后一条用户消息
     */
    private extractLastUserMessage;
    /**
     * 查找最后一个 system 消息的索引
     */
    private findLastSystemIndex;
}
//# sourceMappingURL=MemoryInjectionHook.d.ts.map