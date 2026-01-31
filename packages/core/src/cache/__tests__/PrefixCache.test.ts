import { describe, it, expect, beforeEach } from 'vitest';
import { PrefixCache } from '../PrefixCache.js';

describe('PrefixCache', () => {
  let cache: PrefixCache<string>;

  // Helper to create a prefix of specified length
  const createPrefix = (length: number, char = 'A') => char.repeat(length);

  beforeEach(() => {
    cache = new PrefixCache({
      maxEntries: 100,
      maxTokens: 10000,
      minPrefixLength: 50,
      defaultTtl: 60000,
      evictionPolicy: 'lru',
    });
  });

  describe('constructor', () => {
    it('should create cache with default config', () => {
      const defaultCache = new PrefixCache();
      expect(defaultCache.getStats().totalEntries).toBe(0);
    });

    it('should create cache with custom config', () => {
      const customCache = new PrefixCache({ maxEntries: 50 });
      expect(customCache.getStats().totalEntries).toBe(0);
    });
  });

  describe('set and get', () => {
    it('should store and retrieve value', () => {
      const prefix = createPrefix(100);
      cache.set(prefix, 'cached_value', 100);

      const result = cache.get(prefix);
      expect(result.found).toBe(true);
      expect(result.entry?.value).toBe('cached_value');
    });

    it('should return miss for non-existent prefix', () => {
      const result = cache.get(createPrefix(100, 'B'));
      expect(result.found).toBe(false);
      expect(result.matchedPrefixLength).toBe(0);
    });

    it('should ignore prefix shorter than minPrefixLength', () => {
      cache.set('short', 'value', 10);
      const result = cache.get('short');
      expect(result.found).toBe(false);
    });

    it('should update stats on hit', () => {
      const prefix = createPrefix(100);
      cache.set(prefix, 'value', 100);
      cache.get(prefix);

      const stats = cache.getStats();
      expect(stats.hits).toBe(1);
      expect(stats.hitRate).toBeGreaterThan(0);
    });

    it('should update stats on miss', () => {
      cache.get(createPrefix(100, 'X'));

      const stats = cache.getStats();
      expect(stats.misses).toBe(1);
    });
  });

  describe('has', () => {
    it('should return true for existing prefix', () => {
      const prefix = createPrefix(100);
      cache.set(prefix, 'value', 100);
      expect(cache.has(prefix)).toBe(true);
    });

    it('should return false for non-existent prefix', () => {
      expect(cache.has(createPrefix(100, 'Z'))).toBe(false);
    });
  });

  describe('delete', () => {
    it('should delete existing entry', () => {
      const prefix = createPrefix(100);
      cache.set(prefix, 'value', 100);

      const deleted = cache.delete(prefix);
      expect(deleted).toBe(true);
      expect(cache.has(prefix)).toBe(false);
    });

    it('should return false for non-existent entry', () => {
      const deleted = cache.delete(createPrefix(100, 'Y'));
      expect(deleted).toBe(false);
    });
  });

  describe('clear', () => {
    it('should clear all entries', () => {
      cache.set(createPrefix(100, 'A'), 'value1', 100);
      cache.set(createPrefix(100, 'B'), 'value2', 100);

      cache.clear();

      const stats = cache.getStats();
      expect(stats.totalEntries).toBe(0);
      expect(stats.hits).toBe(0);
      expect(stats.misses).toBe(0);
    });
  });

  describe('prune', () => {
    it('should return 0 when no entries expired', () => {
      cache.set(createPrefix(100), 'value', 100);
      const pruned = cache.prune();
      expect(pruned).toBe(0);
    });
  });

  describe('eviction', () => {
    it('should evict entries when maxEntries exceeded', () => {
      const smallCache = new PrefixCache<string>({
        maxEntries: 2,
        minPrefixLength: 50,
      });

      smallCache.set(createPrefix(100, 'A'), 'value1', 100);
      smallCache.set(createPrefix(100, 'B'), 'value2', 100);
      smallCache.set(createPrefix(100, 'C'), 'value3', 100);

      const stats = smallCache.getStats();
      expect(stats.totalEntries).toBeLessThanOrEqual(2);
      expect(stats.evictions).toBeGreaterThan(0);
    });

    it('should evict entries when maxTokens exceeded', () => {
      const smallTokenCache = new PrefixCache<string>({
        maxTokens: 150,
        minPrefixLength: 50,
      });

      smallTokenCache.set(createPrefix(100, 'A'), 'value1', 100);
      smallTokenCache.set(createPrefix(100, 'B'), 'value2', 100);

      const stats = smallTokenCache.getStats();
      expect(stats.totalTokens).toBeLessThanOrEqual(150);
    });
  });

  describe('configure', () => {
    it('should update configuration', () => {
      cache.configure({ maxEntries: 200 });
      expect(() => cache.configure({ maxTokens: 20000 })).not.toThrow();
    });
  });

  describe('getStats', () => {
    it('should return accurate statistics', () => {
      cache.set(createPrefix(100, 'A'), 'value1', 100);
      cache.set(createPrefix(100, 'B'), 'value2', 200);
      cache.get(createPrefix(100, 'A'));
      cache.get(createPrefix(100, 'X'));

      const stats = cache.getStats();
      expect(stats.totalEntries).toBe(2);
      expect(stats.totalTokens).toBe(300);
      expect(stats.hits).toBe(1);
      expect(stats.misses).toBe(1);
      expect(stats.hitRate).toBe(0.5);
    });
  });
});
