/**
 * Gemini Adapter 实现
 * 支持多模态输入（文本、图片）
 */
import { ICliAdapter, AdapterConfig, SendOptions } from './types.js';
import { Message, AIResponse, StreamChunk } from '../hooks/types.js';
import { HookManager } from '../hooks/HookManager.js';
export interface MultimodalInput {
    text?: string;
    images?: Array<{
        data: string;
        mimeType: string;
    }>;
}
export declare class GeminiAdapter implements ICliAdapter {
    private client;
    private model;
    private config;
    private history;
    private hookManager?;
    private currentStream?;
    constructor(config: AdapterConfig, hookManager?: HookManager);
    setHookManager(hookManager?: HookManager): void;
    getHookManager(): HookManager | undefined;
    private createClient;
    private createModel;
    private mergeConfig;
    private mergeRuntimeOptions;
    private resolveAttemptCount;
    private buildPayloadContext;
    private applyBeforeSendHooks;
    send(prompt: string | MultimodalInput, options?: SendOptions): Promise<AIResponse>;
    stream(prompt: string | MultimodalInput, options?: SendOptions): AsyncGenerator<StreamChunk>;
    receive(): AsyncGenerator<StreamChunk>;
    getHistory(): Message[];
    setHistory(messages: Message[]): void;
    rewind(steps: number): Promise<void>;
    compact(): Promise<void>;
    configure(config: Partial<AdapterConfig>): void;
    getConfig(): AdapterConfig;
    private resolvePromptFromPayload;
    private convertToGeminiFormat;
    private getModel;
    private buildGeminiRequest;
    private sendWithTimeout;
    private createStreamGenerator;
    private isRetryableError;
    private wrapError;
    private delay;
}
//# sourceMappingURL=GeminiAdapter.d.ts.map