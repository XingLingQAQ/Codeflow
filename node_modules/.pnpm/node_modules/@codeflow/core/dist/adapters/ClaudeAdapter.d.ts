import { ICliAdapter, AdapterConfig, SendOptions } from './types.js';
import { Message, AIResponse, StreamChunk } from '../hooks/types.js';
import { HookManager } from '../hooks/HookManager.js';
/**
 * Claude API Adapter 实现
 * 封装 Anthropic SDK，提供统一的 ICliAdapter 接口
 */
export declare class ClaudeAdapter implements ICliAdapter {
    private client;
    private config;
    private history;
    private hookManager?;
    private currentStream?;
    constructor(config: AdapterConfig, hookManager?: HookManager);
    /**
     * 发送消息并获取响应
     */
    send(prompt: string, options?: SendOptions): Promise<AIResponse>;
    /**
     * 接收流式响应
     */
    receive(): AsyncGenerator<StreamChunk>;
    /**
     * 开始流式请求
     */
    stream(prompt: string, options?: SendOptions): AsyncGenerator<StreamChunk>;
    /**
     * 获取对话历史
     */
    getHistory(): Message[];
    /**
     * 设置对话历史
     */
    setHistory(messages: Message[]): void;
    /**
     * 回退指定步数
     */
    rewind(steps: number): Promise<void>;
    /**
     * 压缩对话历史
     */
    compact(): Promise<void>;
    /**
     * 配置 Adapter
     */
    configure(config: Partial<AdapterConfig>): void;
    /**
     * 获取配置
     */
    getConfig(): AdapterConfig;
    /**
     * 执行带重试的请求
     */
    private executeWithRetry;
    /**
     * 解析错误
     */
    private parseError;
    /**
     * 处理错误
     */
    private handleError;
    /**
     * 估算 Token 数量（简单实现）
     */
    private estimateTokens;
}
//# sourceMappingURL=ClaudeAdapter.d.ts.map