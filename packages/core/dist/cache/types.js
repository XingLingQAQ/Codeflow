/**
 * 前缀缓存类型定义
 * 用于优化长上下文场景的 TTFT (Time To First Token)
 */
/**
 * 默认前缀缓存配置
 */
export const DEFAULT_PREFIX_CACHE_CONFIG = {
    maxEntries: 1000,
    maxTokens: 500000,
    defaultTtl: 3600000, // 1 hour
    minPrefixLength: 100,
    evictionPolicy: 'lru',
    enableCompression: true,
    compressionThreshold: 10000,
};
/**
 * 默认上下文预算
 */
export const DEFAULT_CONTEXT_BUDGET = {
    totalTokens: 128000,
    usedTokens: 0,
    remainingTokens: 128000,
    allocations: [
        { category: 'system', tokens: 2000, priority: 1, compressible: false },
        { category: 'history', tokens: 50000, priority: 3, compressible: true },
        { category: 'context', tokens: 60000, priority: 2, compressible: true },
        { category: 'user', tokens: 10000, priority: 1, compressible: false },
        { category: 'reserved', tokens: 6000, priority: 0, compressible: false },
    ],
};
/**
 * 默认 BLA 配置
 */
export const DEFAULT_BLA_CONFIG = {
    decayRate: 0.5,
    baseActivation: 0.0,
    recencyWeight: 0.7,
    frequencyWeight: 0.3,
    minActivation: 0.01,
};
//# sourceMappingURL=types.js.map