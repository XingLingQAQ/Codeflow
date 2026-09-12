/**
 * CLI Adapter 接口类型定义
 */
import { Message, AIResponse, StreamChunk, RequestPayload, DecisionSkeleton } from '../hooks/types.js';
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
export interface ProviderRequestConfig {
    apiKey?: string;
    baseURL?: string;
    timeout?: number;
    maxRetries?: number;
}
export interface AdapterRuntimeConfig extends ProviderRequestConfig {
    model: string;
    temperature?: number;
    maxTokens?: number;
}
/**
 * 运行时 resolved config 的最小只读视图。
 * provider 认证/重试属于 provider request config；
 * runtime metadata 继续由 ConfigManager/runtime 真相源持有，不下沉到 adapter config。
 */
export interface ResolvedAdapterConfig extends AdapterRuntimeConfig {
    systemPrompt?: string;
    answerStyle?: string;
    capabilities?: string[];
    allowedSkills?: string[];
    allowedHooks?: string[];
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
export interface AdapterPayloadContext {
    messages: Message[];
    model: string;
    temperature?: number;
    maxTokens?: number;
}
export declare function cloneMessages(messages: Message[]): Message[];
export declare function messageToText(message: Message): string;
export declare function messagesToPrompt(messages: Message[], separator?: string): string;
export declare function serializeMessage<T extends Message>(message: T): T & {
    content: string;
};
export declare function deserializeMessage<T extends Message>(message: T): T;
export declare function toHookPayload(context: AdapterPayloadContext): RequestPayload;
export declare function applyHookPayload(context: AdapterPayloadContext, payload: RequestPayload): AdapterPayloadContext;
export interface HistoryGovernanceParts {
    systemMessages: Message[];
    dialogueMessages: Message[];
}
export declare function splitHistoryForGovernance(messages: Message[]): HistoryGovernanceParts;
export declare function countCompletedTurns(messages: Message[]): number;
export declare function rewindHistoryByTurns(messages: Message[], steps: number): Message[];
export declare function compactHistoryWithSummary(messages: Message[], options?: {
    buildSkeleton?: (messages: Message[], tokenCount: number) => Promise<DecisionSkeleton>;
    estimateTokens?: (messages: Message[]) => number;
    keepRatio?: number;
    minimumRecentMessages?: number;
    summaryTimestamp?: number;
}): Promise<Message[]>;
export declare function hasToolTurns(messages: Message[]): boolean;
/**
 * CLI Adapter 核心接口
 */
export interface ICliAdapter {
    send(prompt: string, options?: SendOptions): Promise<AIResponse>;
    stream(prompt: string, options?: SendOptions): AsyncGenerator<StreamChunk>;
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