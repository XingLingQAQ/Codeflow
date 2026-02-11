/**
 * 前缀缓存实现
 * 优化长上下文场景的 TTFT
 */
import { IPrefixCache, PrefixCacheConfig, CacheStats, PrefixMatchResult } from './types.js';
export declare class PrefixCache<T = unknown> implements IPrefixCache<T> {
    private config;
    private cache;
    private prefixIndex;
    private originalPrefixes;
    private stats;
    private accessTimes;
    constructor(config?: Partial<PrefixCacheConfig>);
    configure(config: Partial<PrefixCacheConfig>): void;
    get(prefix: string): PrefixMatchResult<T>;
    set(prefix: string, value: T, tokenCount: number, ttl?: number): void;
    has(prefix: string): boolean;
    delete(prefix: string): boolean;
    clear(): void;
    getStats(): CacheStats;
    prune(): number;
    private hashPrefix;
    private indexPrefix;
    private removeFromIndex;
    private rebuildIndex;
    private findLongestPrefixMatch;
    private isExpired;
    private ensureCapacity;
    private evictOne;
    private findLRUVictim;
    private findLFUVictim;
    private findTTLVictim;
    private recordHit;
    private recordMiss;
    private recordAccessTime;
    private updateHitRate;
    private updateStats;
    private estimateTokens;
}
//# sourceMappingURL=PrefixCache.d.ts.map