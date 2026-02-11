/**
 * 模型热切换管理器实现
 */
import { IHotSwapManager, ModelInfo, SwitchOptions, SwitchResult, RetryStrategy, RelayConfig, ContextMigrationResult, HotSwapConfig } from './types.js';
import { ICliAdapter } from '../adapters/types.js';
export declare class HotSwapManager implements IHotSwapManager {
    private config;
    private models;
    private currentModelId;
    private adapters;
    private currentAdapter;
    private failureCount;
    constructor(config?: Partial<HotSwapConfig>);
    private initializeModels;
    registerAdapter(modelId: string, adapter: ICliAdapter): void;
    registerModel(model: ModelInfo): void;
    getAvailableModels(): ModelInfo[];
    getCurrentModel(): ModelInfo | null;
    getModelInfo(modelId: string): ModelInfo | null;
    canSwitch(modelId: string): boolean;
    switchModel(modelId: string, options?: Partial<SwitchOptions>): Promise<SwitchResult>;
    retry(options?: Partial<RetryStrategy>): Promise<SwitchResult>;
    relay(fallbackChain?: string[]): Promise<SwitchResult>;
    migrateContext(targetModel: string): Promise<ContextMigrationResult>;
    configure(config: Partial<HotSwapConfig>): void;
    setRelayConfig(config: Partial<RelayConfig>): void;
    private estimateTokens;
    private truncateMessages;
    private sleep;
    recordFailure(modelId: string): void;
    resetFailures(modelId: string): void;
}
//# sourceMappingURL=HotSwapManager.d.ts.map