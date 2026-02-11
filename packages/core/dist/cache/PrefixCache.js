/**
 * 前缀缓存实现
 * 优化长上下文场景的 TTFT
 */
import { DEFAULT_PREFIX_CACHE_CONFIG, } from './types.js';
export class PrefixCache {
    constructor(config = {}) {
        this.cache = new Map();
        this.prefixIndex = new Map();
        this.originalPrefixes = new Map(); // hash -> original prefix
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
        this.config = { ...DEFAULT_PREFIX_CACHE_CONFIG, ...config };
    }
    configure(config) {
        this.config = { ...this.config, ...config };
    }
    get(prefix) {
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
    set(prefix, value, tokenCount, ttl) {
        if (prefix.length < this.config.minPrefixLength) {
            return;
        }
        // 检查容量
        this.ensureCapacity(tokenCount);
        const prefixHash = this.hashPrefix(prefix);
        const now = Date.now();
        const entry = {
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
    has(prefix) {
        const prefixHash = this.hashPrefix(prefix);
        const entry = this.cache.get(prefixHash);
        return entry !== undefined && !this.isExpired(entry);
    }
    delete(prefix) {
        const prefixHash = this.hashPrefix(prefix);
        const deleted = this.cache.delete(prefixHash);
        if (deleted) {
            this.removeFromIndex(prefix, prefixHash);
            this.updateStats();
        }
        return deleted;
    }
    clear() {
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
    getStats() {
        return { ...this.stats };
    }
    prune() {
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
    hashPrefix(prefix) {
        // 使用 FNV-1a 哈希算法（快速且分布均匀）
        let hash = 2166136261; // FNV offset basis
        for (let i = 0; i < prefix.length; i++) {
            hash ^= prefix.charCodeAt(i);
            hash = Math.imul(hash, 16777619); // FNV prime
        }
        // 转换为正数并添加长度信息以减少碰撞
        const positiveHash = hash >>> 0;
        return `prefix_${positiveHash.toString(36)}_${prefix.length}`;
    }
    indexPrefix(prefix, hash) {
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
            this.prefixIndex.get(segmentHash).add(hash);
        }
        // 存储原始前缀用于索引重建
        this.originalPrefixes.set(hash, prefix);
    }
    removeFromIndex(prefix, hash) {
        for (const [, hashes] of this.prefixIndex) {
            hashes.delete(hash);
        }
        this.originalPrefixes.delete(hash);
    }
    rebuildIndex() {
        this.prefixIndex.clear();
        // 使用存储的原始前缀重建索引
        for (const [hash, prefix] of this.originalPrefixes) {
            if (this.cache.has(hash)) {
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
                    this.prefixIndex.get(segmentHash).add(hash);
                }
            }
            else {
                // 清理不存在的缓存条目
                this.originalPrefixes.delete(hash);
            }
        }
    }
    findLongestPrefixMatch(prefix) {
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
    isExpired(entry) {
        return Date.now() - entry.createdAt > entry.ttl;
    }
    ensureCapacity(newTokens) {
        // 检查条目数量
        while (this.cache.size >= this.config.maxEntries) {
            this.evictOne();
        }
        // 检查 token 总量
        while (this.stats.totalTokens + newTokens > this.config.maxTokens) {
            this.evictOne();
        }
    }
    evictOne() {
        let victimKey = null;
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
    findLRUVictim() {
        let oldest = null;
        let oldestKey = null;
        for (const [key, entry] of this.cache) {
            if (!oldest || entry.lastAccessedAt < oldest.lastAccessedAt) {
                oldest = entry;
                oldestKey = key;
            }
        }
        return oldestKey;
    }
    findLFUVictim() {
        let leastUsed = null;
        let leastUsedKey = null;
        for (const [key, entry] of this.cache) {
            if (!leastUsed || entry.accessCount < leastUsed.accessCount) {
                leastUsed = entry;
                leastUsedKey = key;
            }
        }
        return leastUsedKey;
    }
    findTTLVictim() {
        let soonestExpiry = null;
        let soonestKey = null;
        for (const [key, entry] of this.cache) {
            const expiryTime = entry.createdAt + entry.ttl;
            if (!soonestExpiry || expiryTime < soonestExpiry.createdAt + soonestExpiry.ttl) {
                soonestExpiry = entry;
                soonestKey = key;
            }
        }
        return soonestKey;
    }
    recordHit(entry, startTime) {
        entry.lastAccessedAt = Date.now();
        entry.accessCount++;
        this.stats.hits++;
        this.recordAccessTime(startTime);
        this.updateHitRate();
    }
    recordMiss(startTime) {
        this.stats.misses++;
        this.recordAccessTime(startTime);
        this.updateHitRate();
    }
    recordAccessTime(startTime) {
        const accessTime = Date.now() - startTime;
        this.accessTimes.push(accessTime);
        if (this.accessTimes.length > 1000) {
            this.accessTimes.shift();
        }
        this.stats.avgAccessTime =
            this.accessTimes.reduce((a, b) => a + b, 0) / this.accessTimes.length;
    }
    updateHitRate() {
        const total = this.stats.hits + this.stats.misses;
        this.stats.hitRate = total > 0 ? this.stats.hits / total : 0;
    }
    updateStats() {
        this.stats.totalEntries = this.cache.size;
        this.stats.totalTokens = Array.from(this.cache.values()).reduce((sum, entry) => sum + entry.tokenCount, 0);
    }
    estimateTokens(text) {
        return Math.ceil(text.length / 4);
    }
}
//# sourceMappingURL=PrefixCache.js.map