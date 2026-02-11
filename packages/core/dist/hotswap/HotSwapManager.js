/**
 * 模型热切换管理器实现
 */
import { DEFAULT_HOTSWAP_CONFIG, PREDEFINED_MODELS, } from './types.js';
export class HotSwapManager {
    constructor(config = {}) {
        this.models = new Map();
        this.currentModelId = null;
        this.adapters = new Map();
        this.currentAdapter = null;
        this.failureCount = new Map();
        this.config = { ...DEFAULT_HOTSWAP_CONFIG, ...config };
        this.initializeModels();
    }
    initializeModels() {
        for (const model of PREDEFINED_MODELS) {
            this.models.set(model.id, model);
        }
    }
    registerAdapter(modelId, adapter) {
        this.adapters.set(modelId, adapter);
        if (!this.currentAdapter) {
            this.currentAdapter = adapter;
            this.currentModelId = modelId;
        }
    }
    registerModel(model) {
        this.models.set(model.id, model);
    }
    getAvailableModels() {
        return Array.from(this.models.values()).filter(m => m.available);
    }
    getCurrentModel() {
        if (!this.currentModelId)
            return null;
        return this.models.get(this.currentModelId) || null;
    }
    getModelInfo(modelId) {
        return this.models.get(modelId) || null;
    }
    canSwitch(modelId) {
        const model = this.models.get(modelId);
        if (!model)
            return false;
        if (!model.available)
            return false;
        if (model.status === 'offline')
            return false;
        return this.adapters.has(modelId);
    }
    async switchModel(modelId, options = {}) {
        const opts = {
            preserveHistory: true,
            migrateContext: true,
            fallbackOnError: true,
            retryCount: 0,
            ...options,
        };
        const previousModel = this.currentModelId || 'none';
        const targetModel = this.models.get(modelId);
        if (!targetModel) {
            return {
                success: false,
                previousModel,
                currentModel: previousModel,
                contextMigrated: false,
                tokensMigrated: 0,
                error: `Model ${modelId} not found`,
            };
        }
        if (!this.canSwitch(modelId)) {
            if (opts.fallbackOnError) {
                return this.relay();
            }
            return {
                success: false,
                previousModel,
                currentModel: previousModel,
                contextMigrated: false,
                tokensMigrated: 0,
                error: `Cannot switch to model ${modelId}`,
            };
        }
        try {
            // 获取当前历史
            let history = [];
            let tokensMigrated = 0;
            if (opts.preserveHistory && this.currentAdapter) {
                history = this.currentAdapter.getHistory();
            }
            // 切换到新 adapter
            const newAdapter = this.adapters.get(modelId);
            if (!newAdapter) {
                throw new Error(`Adapter for ${modelId} not registered`);
            }
            // 迁移上下文
            if (opts.migrateContext && history.length > 0) {
                const migration = await this.migrateContext(modelId);
                if (migration.success) {
                    newAdapter.setHistory(migration.messages);
                    tokensMigrated = migration.migratedTokens;
                }
            }
            else if (opts.preserveHistory && history.length > 0) {
                newAdapter.setHistory(history);
                tokensMigrated = this.estimateTokens(history);
            }
            this.currentAdapter = newAdapter;
            this.currentModelId = modelId;
            this.failureCount.set(modelId, 0);
            return {
                success: true,
                previousModel,
                currentModel: modelId,
                contextMigrated: opts.migrateContext,
                tokensMigrated,
            };
        }
        catch (error) {
            const errorMessage = error instanceof Error ? error.message : String(error);
            if (opts.fallbackOnError) {
                return this.relay();
            }
            return {
                success: false,
                previousModel,
                currentModel: previousModel,
                contextMigrated: false,
                tokensMigrated: 0,
                error: errorMessage,
            };
        }
    }
    async retry(options = {}) {
        const strategy = {
            ...this.config.retryStrategy,
            ...options,
        };
        if (!this.currentModelId) {
            return {
                success: false,
                previousModel: 'none',
                currentModel: 'none',
                contextMigrated: false,
                tokensMigrated: 0,
                error: 'No current model to retry',
            };
        }
        const failures = this.failureCount.get(this.currentModelId) || 0;
        if (failures >= strategy.maxRetries) {
            // 超过重试次数，尝试接力
            return this.relay();
        }
        // 计算延迟
        const delay = Math.min(strategy.baseDelay * Math.pow(strategy.backoffMultiplier, failures), strategy.maxDelay);
        await this.sleep(delay);
        // 重新尝试当前模型
        this.failureCount.set(this.currentModelId, failures + 1);
        return {
            success: true,
            previousModel: this.currentModelId,
            currentModel: this.currentModelId,
            contextMigrated: false,
            tokensMigrated: 0,
        };
    }
    async relay(fallbackChain) {
        const chain = fallbackChain || this.config.relayConfig.fallbackChain;
        const previousModel = this.currentModelId || 'none';
        for (const modelId of chain) {
            if (modelId === this.currentModelId)
                continue;
            if (!this.canSwitch(modelId))
                continue;
            const result = await this.switchModel(modelId, {
                preserveHistory: true,
                migrateContext: true,
                fallbackOnError: false,
            });
            if (result.success) {
                return result;
            }
        }
        return {
            success: false,
            previousModel,
            currentModel: previousModel,
            contextMigrated: false,
            tokensMigrated: 0,
            error: 'All fallback models failed',
        };
    }
    async migrateContext(targetModel) {
        if (!this.currentAdapter) {
            return {
                success: false,
                originalTokens: 0,
                migratedTokens: 0,
                truncated: false,
                messages: [],
            };
        }
        const history = this.currentAdapter.getHistory();
        const originalTokens = this.estimateTokens(history);
        const targetModelInfo = this.models.get(targetModel);
        if (!targetModelInfo) {
            return {
                success: false,
                originalTokens,
                migratedTokens: 0,
                truncated: false,
                messages: [],
            };
        }
        // 检查是否需要截断
        const maxTokens = Math.min(targetModelInfo.contextWindow, this.config.maxContextTokens);
        let migratedMessages = [...history];
        let truncated = false;
        if (originalTokens > maxTokens) {
            // 从最早的消息开始截断，保留最近的
            migratedMessages = this.truncateMessages(history, maxTokens);
            truncated = true;
        }
        const migratedTokens = this.estimateTokens(migratedMessages);
        return {
            success: true,
            originalTokens,
            migratedTokens,
            truncated,
            messages: migratedMessages,
        };
    }
    configure(config) {
        this.config = { ...this.config, ...config };
    }
    setRelayConfig(config) {
        this.config.relayConfig = { ...this.config.relayConfig, ...config };
    }
    // ==================== Private Methods ====================
    estimateTokens(messages) {
        let total = 0;
        for (const msg of messages) {
            // 粗略估计：4 字符 ≈ 1 token
            total += Math.ceil(msg.content.length / 4);
        }
        return total;
    }
    truncateMessages(messages, maxTokens) {
        const result = [];
        let currentTokens = 0;
        // 从最新的消息开始，向前添加
        for (let i = messages.length - 1; i >= 0; i--) {
            const msg = messages[i];
            const msgTokens = Math.ceil(msg.content.length / 4);
            if (currentTokens + msgTokens > maxTokens) {
                break;
            }
            result.unshift(msg);
            currentTokens += msgTokens;
        }
        return result;
    }
    sleep(ms) {
        return new Promise(resolve => setTimeout(resolve, ms));
    }
    recordFailure(modelId) {
        const current = this.failureCount.get(modelId) || 0;
        this.failureCount.set(modelId, current + 1);
        // 检查是否需要自动切换
        if (this.config.relayConfig.autoSwitch &&
            current + 1 >= this.config.relayConfig.switchThreshold) {
            this.relay();
        }
    }
    resetFailures(modelId) {
        this.failureCount.set(modelId, 0);
    }
}
//# sourceMappingURL=HotSwapManager.js.map