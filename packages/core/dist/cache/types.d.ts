/**
 * 前缀缓存类型定义
 * 用于优化长上下文场景的 TTFT (Time To First Token)
 */
/**
 * 缓存条目
 */
export interface CacheEntry<T = unknown> {
    key: string;
    value: T;
    prefixHash: string;
    tokenCount: number;
    createdAt: number;
    lastAccessedAt: number;
    accessCount: number;
    ttl: number;
}
/**
 * 前缀缓存配置
 */
export interface PrefixCacheConfig {
    maxEntries: number;
    maxTokens: number;
    defaultTtl: number;
    minPrefixLength: number;
    evictionPolicy: 'lru' | 'lfu' | 'ttl';
    enableCompression: boolean;
    compressionThreshold: number;
}
/**
 * 缓存统计
 */
export interface CacheStats {
    hits: number;
    misses: number;
    hitRate: number;
    totalEntries: number;
    totalTokens: number;
    evictions: number;
    avgAccessTime: number;
}
/**
 * 前缀匹配结果
 */
export interface PrefixMatchResult<T = unknown> {
    found: boolean;
    entry?: CacheEntry<T>;
    matchedPrefixLength: number;
    matchedTokens: number;
    remainingTokens: number;
}
/**
 * 上下文预算
 */
export interface ContextBudget {
    totalTokens: number;
    usedTokens: number;
    remainingTokens: number;
    allocations: BudgetAllocation[];
}
/**
 * 预算分配
 */
export interface BudgetAllocation {
    category: 'system' | 'history' | 'context' | 'user' | 'reserved';
    tokens: number;
    priority: number;
    compressible: boolean;
}
/**
 * 观察屏蔽配置
 */
export interface ObservationMaskConfig {
    enabled: boolean;
    maskPatterns: string[];
    preserveStructure: boolean;
    replacementToken: string;
}
/**
 * 图谱衰减配置 (BLA - Base Level Activation)
 */
export interface BLAConfig {
    decayRate: number;
    baseActivation: number;
    recencyWeight: number;
    frequencyWeight: number;
    minActivation: number;
}
/**
 * 前缀缓存接口
 */
export interface IPrefixCache<T = unknown> {
    get(prefix: string): PrefixMatchResult<T>;
    set(prefix: string, value: T, tokenCount: number, ttl?: number): void;
    has(prefix: string): boolean;
    delete(prefix: string): boolean;
    clear(): void;
    getStats(): CacheStats;
    prune(): number;
    configure(config: Partial<PrefixCacheConfig>): void;
}
/**
 * 上下文预算管理器接口
 */
export interface IContextBudgetManager {
    allocate(category: BudgetAllocation['category'], tokens: number): boolean;
    release(category: BudgetAllocation['category'], tokens: number): void;
    getBudget(): ContextBudget;
    canAllocate(tokens: number): boolean;
    compress(targetTokens: number): number;
    reset(): void;
}
/**
 * 默认前缀缓存配置
 */
export declare const DEFAULT_PREFIX_CACHE_CONFIG: PrefixCacheConfig;
/**
 * 默认上下文预算
 */
export declare const DEFAULT_CONTEXT_BUDGET: ContextBudget;
/**
 * 默认 BLA 配置
 */
export declare const DEFAULT_BLA_CONFIG: BLAConfig;
//# sourceMappingURL=types.d.ts.map