/**
 * 前缀缓存实现
 * 优化长上下文场景的 TTFT
 */

import {
  IPrefixCache,
  PrefixCacheConfig,
  CacheEntry,
  CacheStats,
  PrefixMatchResult,
  DEFAULT_PREFIX_CACHE_CONFIG,
} from './types.js';

export class PrefixCache<T = unknown> implements IPrefixCache<T> {
  private config: PrefixCacheConfig;
  private cache: Map<string, CacheEntry<T>> = new Map();
  private prefixIndex: Map<string, Set<string>> = new Map();
  private stats: CacheStats = {
    hits: 0,
    misses: 0,
    hitRate: 0,
    totalEntries: 0,
    totalTokens: 0,
    evictions: 0,
    avgAccessTime: 0,
  };
  private accessTimes: number[] = [];

  constructor(config: Partial<PrefixCacheConfig> = {}) {
    this.config = { ...DEFAULT_PREFIX_CACHE_CONFIG, ...config };
  }

  configure(config: Partial<PrefixCacheConfig>): void {
    this.config = { ...this.config, ...config };
  }

  get(prefix: string): PrefixMatchResult<T> {
    const startTime = Date.now();

    // 计算前缀哈希
    const prefixHash = this.hashPrefix(prefix);

    // 精确匹配
    const exactEntry = this.cache.get(prefixHash);
    if (exactEntry && !this.isExpired(exactEntry)) {
      this.recordHit(exactEntry, startTime);
      return {
        found: true,
        entry: exactEntry,
        matchedPrefixLength: prefix.length,
        matchedTokens: exactEntry.tokenCount,
        remainingTokens: 0,
      };
    }

    // 前缀匹配（查找最长匹配）
    const longestMatch = this.findLongestPrefixMatch(prefix);
    if (longestMatch && longestMatch.entry) {
      this.recordHit(longestMatch.entry, startTime);
      return longestMatch;
    }

    // 未命中
    this.recordMiss(startTime);
    return {
      found: false,
      matchedPrefixLength: 0,
      matchedTokens: 0,
      remainingTokens: this.estimateTokens(prefix),
    };
  }

  set(prefix: string, value: T, tokenCount: number, ttl?: number): void {
    if (prefix.length < this.config.minPrefixLength) {
      return;
    }

    // 检查容量
    this.ensureCapacity(tokenCount);

    const prefixHash = this.hashPrefix(prefix);
    const now = Date.now();

    const entry: CacheEntry<T> = {
      key: prefixHash,
      value,
      prefixHash,
      tokenCount,
      createdAt: now,
      lastAccessedAt: now,
      accessCount: 1,
      ttl: ttl || this.config.defaultTtl,
    };

    this.cache.set(prefixHash, entry);
    this.indexPrefix(prefix, prefixHash);
    this.updateStats();
  }

  has(prefix: string): boolean {
    const prefixHash = this.hashPrefix(prefix);
    const entry = this.cache.get(prefixHash);
    return entry !== undefined && !this.isExpired(entry);
  }

  delete(prefix: string): boolean {
    const prefixHash = this.hashPrefix(prefix);
    const deleted = this.cache.delete(prefixHash);
    if (deleted) {
      this.removeFromIndex(prefix, prefixHash);
      this.updateStats();
    }
    return deleted;
  }

  clear(): void {
    this.cache.clear();
    this.prefixIndex.clear();
    this.stats = {
      hits: 0,
      misses: 0,
      hitRate: 0,
      totalEntries: 0,
      totalTokens: 0,
      evictions: 0,
      avgAccessTime: 0,
    };
    this.accessTimes = [];
  }

  getStats(): CacheStats {
    return { ...this.stats };
  }

  prune(): number {
    const now = Date.now();
    let pruned = 0;

    for (const [key, entry] of this.cache) {
      if (this.isExpired(entry)) {
        this.cache.delete(key);
        pruned++;
      }
    }

    if (pruned > 0) {
      this.rebuildIndex();
      this.updateStats();
    }

    return pruned;
  }

  // ==================== Private Methods ====================

  private hashPrefix(prefix: string): string {
    // 简单哈希实现（生产环境应使用更强的哈希）
    let hash = 0;
    for (let i = 0; i < prefix.length; i++) {
      const char = prefix.charCodeAt(i);
      hash = ((hash << 5) - hash) + char;
      hash = hash & hash;
    }
    return `prefix_${hash.toString(36)}_${prefix.length}`;
  }

  private indexPrefix(prefix: string, hash: string): void {
    // 索引不同长度的前缀片段
    const segments = [
      prefix.slice(0, 100),
      prefix.slice(0, 500),
      prefix.slice(0, 1000),
    ].filter(s => s.length > 0);

    for (const segment of segments) {
      const segmentHash = this.hashPrefix(segment);
      if (!this.prefixIndex.has(segmentHash)) {
        this.prefixIndex.set(segmentHash, new Set());
      }
      this.prefixIndex.get(segmentHash)!.add(hash);
    }
  }

  private removeFromIndex(prefix: string, hash: string): void {
    for (const [, hashes] of this.prefixIndex) {
      hashes.delete(hash);
    }
  }

  private rebuildIndex(): void {
    this.prefixIndex.clear();
    // 重建索引需要原始前缀，这里简化处理
  }

  private findLongestPrefixMatch(prefix: string): PrefixMatchResult<T> | null {
    // 从长到短尝试匹配
    const lengths = [1000, 500, 100].filter(l => l <= prefix.length);

    for (const length of lengths) {
      const segment = prefix.slice(0, length);
      const segmentHash = this.hashPrefix(segment);
      const candidates = this.prefixIndex.get(segmentHash);

      if (candidates) {
        for (const candidateHash of candidates) {
          const entry = this.cache.get(candidateHash);
          if (entry && !this.isExpired(entry)) {
            return {
              found: true,
              entry,
              matchedPrefixLength: length,
              matchedTokens: Math.floor(entry.tokenCount * (length / prefix.length)),
              remainingTokens: this.estimateTokens(prefix.slice(length)),
            };
          }
        }
      }
    }

    return null;
  }

  private isExpired(entry: CacheEntry<T>): boolean {
    return Date.now() - entry.createdAt > entry.ttl;
  }

  private ensureCapacity(newTokens: number): void {
    // 检查条目数量
    while (this.cache.size >= this.config.maxEntries) {
      this.evictOne();
    }

    // 检查 token 总量
    while (this.stats.totalTokens + newTokens > this.config.maxTokens) {
      this.evictOne();
    }
  }

  private evictOne(): void {
    let victimKey: string | null = null;

    switch (this.config.evictionPolicy) {
      case 'lru':
        victimKey = this.findLRUVictim();
        break;
      case 'lfu':
        victimKey = this.findLFUVictim();
        break;
      case 'ttl':
        victimKey = this.findTTLVictim();
        break;
    }

    if (victimKey) {
      this.cache.delete(victimKey);
      this.stats.evictions++;
    }
  }

  private findLRUVictim(): string | null {
    let oldest: CacheEntry<T> | null = null;
    let oldestKey: string | null = null;

    for (const [key, entry] of this.cache) {
      if (!oldest || entry.lastAccessedAt < oldest.lastAccessedAt) {
        oldest = entry;
        oldestKey = key;
      }
    }

    return oldestKey;
  }

  private findLFUVictim(): string | null {
    let leastUsed: CacheEntry<T> | null = null;
    let leastUsedKey: string | null = null;

    for (const [key, entry] of this.cache) {
      if (!leastUsed || entry.accessCount < leastUsed.accessCount) {
        leastUsed = entry;
        leastUsedKey = key;
      }
    }

    return leastUsedKey;
  }

  private findTTLVictim(): string | null {
    let soonestExpiry: CacheEntry<T> | null = null;
    let soonestKey: string | null = null;

    for (const [key, entry] of this.cache) {
      const expiryTime = entry.createdAt + entry.ttl;
      if (!soonestExpiry || expiryTime < soonestExpiry.createdAt + soonestExpiry.ttl) {
        soonestExpiry = entry;
        soonestKey = key;
      }
    }

    return soonestKey;
  }

  private recordHit(entry: CacheEntry<T>, startTime: number): void {
    entry.lastAccessedAt = Date.now();
    entry.accessCount++;
    this.stats.hits++;
    this.recordAccessTime(startTime);
    this.updateHitRate();
  }

  private recordMiss(startTime: number): void {
    this.stats.misses++;
    this.recordAccessTime(startTime);
    this.updateHitRate();
  }

  private recordAccessTime(startTime: number): void {
    const accessTime = Date.now() - startTime;
    this.accessTimes.push(accessTime);
    if (this.accessTimes.length > 1000) {
      this.accessTimes.shift();
    }
    this.stats.avgAccessTime =
      this.accessTimes.reduce((a, b) => a + b, 0) / this.accessTimes.length;
  }

  private updateHitRate(): void {
    const total = this.stats.hits + this.stats.misses;
    this.stats.hitRate = total > 0 ? this.stats.hits / total : 0;
  }

  private updateStats(): void {
    this.stats.totalEntries = this.cache.size;
    this.stats.totalTokens = Array.from(this.cache.values()).reduce(
      (sum, entry) => sum + entry.tokenCount,
      0
    );
  }

  private estimateTokens(text: string): number {
    return Math.ceil(text.length / 4);
  }
}
