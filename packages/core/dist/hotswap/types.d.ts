/**
 * 模型热切换类型定义
 */
import { Message } from '../hooks/types.js';
/**
 * 模型信息
 */
export interface ModelInfo {
    id: string;
    name: string;
    provider: 'claude' | 'gemini' | 'codex' | 'openai' | 'custom';
    capabilities: ModelCapabilities;
    contextWindow: number;
    maxOutputTokens: number;
    costPer1kInput?: number;
    costPer1kOutput?: number;
    available: boolean;
    status?: 'online' | 'degraded' | 'offline';
}
/**
 * 模型能力
 */
export interface ModelCapabilities {
    streaming: boolean;
    vision: boolean;
    functionCalling: boolean;
    codeExecution: boolean;
    multimodal: boolean;
}
/**
 * 切换选项
 */
export interface SwitchOptions {
    preserveHistory: boolean;
    migrateContext: boolean;
    fallbackOnError: boolean;
    retryCount?: number;
}
/**
 * 切换结果
 */
export interface SwitchResult {
    success: boolean;
    previousModel: string;
    currentModel: string;
    contextMigrated: boolean;
    tokensMigrated: number;
    error?: string;
}
/**
 * 重试策略
 */
export interface RetryStrategy {
    maxRetries: number;
    baseDelay: number;
    maxDelay: number;
    backoffMultiplier: number;
    retryableErrors: string[];
}
/**
 * 接力配置
 */
export interface RelayConfig {
    enabled: boolean;
    fallbackChain: string[];
    autoSwitch: boolean;
    switchThreshold: number;
}
/**
 * 上下文迁移结果
 */
export interface ContextMigrationResult {
    success: boolean;
    originalTokens: number;
    migratedTokens: number;
    truncated: boolean;
    messages: Message[];
}
/**
 * 热切换管理器接口
 */
export interface IHotSwapManager {
    getAvailableModels(): ModelInfo[];
    getCurrentModel(): ModelInfo | null;
    getModelInfo(modelId: string): ModelInfo | null;
    switchModel(modelId: string, options?: Partial<SwitchOptions>): Promise<SwitchResult>;
    canSwitch(modelId: string): boolean;
    retry(options?: Partial<RetryStrategy>): Promise<SwitchResult>;
    relay(fallbackChain?: string[]): Promise<SwitchResult>;
    migrateContext(targetModel: string): Promise<ContextMigrationResult>;
    configure(config: Partial<HotSwapConfig>): void;
    setRelayConfig(config: Partial<RelayConfig>): void;
}
/**
 * 热切换配置
 */
export interface HotSwapConfig {
    defaultModel: string;
    autoRetry: boolean;
    retryStrategy: RetryStrategy;
    relayConfig: RelayConfig;
    contextMigrationEnabled: boolean;
    maxContextTokens: number;
}
/**
 * 默认配置
 */
export declare const DEFAULT_HOTSWAP_CONFIG: HotSwapConfig;
/**
 * 预定义模型列表
 */
export declare const PREDEFINED_MODELS: ModelInfo[];
//# sourceMappingURL=types.d.ts.map