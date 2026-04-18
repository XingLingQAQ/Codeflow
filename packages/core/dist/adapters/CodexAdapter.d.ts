/**
 * Codex Adapter 实现
 * 基于 OpenAI API（兼容 Codex 端点）
 */
import { ICliAdapter, AdapterConfig, SendOptions } from './types.js';
import { Message, AIResponse, StreamChunk } from '../hooks/types.js';
import { HookManager } from '../hooks/HookManager.js';
export declare class CodexAdapter implements ICliAdapter {
    private client;
    private config;
    private history;
    private hookManager?;
    private currentStream?;
    constructor(config: AdapterConfig, hookManager?: HookManager);
    setHookManager(hookManager?: HookManager): void;
    getHookManager(): HookManager | undefined;
    send(prompt: string, options?: SendOptions): Promise<AIResponse>;
    receive(): AsyncGenerator<StreamChunk>;
    stream(prompt: string, options?: SendOptions): AsyncGenerator<StreamChunk>;
    getHistory(): Message[];
    setHistory(messages: Message[]): void;
    rewind(steps: number): Promise<void>;
    compact(): Promise<void>;
    configure(config: Partial<AdapterConfig>): void;
    getConfig(): AdapterConfig;
    private createStreamGenerator;
    private wrapError;
}
//# sourceMappingURL=CodexAdapter.d.ts.map