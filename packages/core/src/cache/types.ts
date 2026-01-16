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
export const DEFAULT_PREFIX_CACHE_CONFIG: PrefixCacheConfig = {
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
export const DEFAULT_CONTEXT_BUDGET: ContextBudget = {
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
export const DEFAULT_BLA_CONFIG: BLAConfig = {
  decayRate: 0.5,
  baseActivation: 0.0,
  recencyWeight: 0.7,
  frequencyWeight: 0.3,
  minActivation: 0.01,
};
