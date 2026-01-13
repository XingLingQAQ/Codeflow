/**
 * CLI Adapter 接口类型定义
 */
import { Message, AIResponse, StreamChunk } from '../hooks/types.js';
/**
 * 发送选项
 */
export interface SendOptions {
    model?: string;
    temperature?: number;
    maxTokens?: number;
    stream?: boolean;
    timeout?: number;
    [key: string]: unknown;
}
/**
 * Adapter 配置
 */
export interface AdapterConfig {
    apiKey?: string;
    baseURL?: string;
    model: string;
    temperature?: number;
    maxTokens?: number;
    timeout?: number;
    maxRetries?: number;
    retryDelay?: number;
}
/**
 * CLI Adapter 核心接口
 */
export interface ICliAdapter {
    send(prompt: string, options?: SendOptions): Promise<AIResponse>;
    receive(): AsyncGenerator<StreamChunk>;
    getHistory(): Message[];
    setHistory(messages: Message[]): void;
    rewind(steps: number): Promise<void>;
    compact(): Promise<void>;
    configure(config: Partial<AdapterConfig>): void;
    getConfig(): AdapterConfig;
}
/**
 * API 错误类型
 */
export declare class APIError extends Error {
    statusCode?: number | undefined;
    code?: string | undefined;
    retryable: boolean;
    constructor(message: string, statusCode?: number | undefined, code?: string | undefined, retryable?: boolean);
}
/**
 * 超时错误
 */
export declare class TimeoutError extends Error {
    constructor(message?: string);
}
//# sourceMappingURL=types.d.ts.map