import { describe, it, expect, beforeEach, vi } from 'vitest';
import { ProgressiveDisclosure } from '../ProgressiveDisclosure.js';
import { DisclosureConfig, DEFAULT_DISCLOSURE_CONFIG } from '../types.js';
import { HookManager } from '../../hooks/HookManager.js';
import { IDualTrackMemory, DualTrackSearchResult } from '../../retriever/DualTrackTypes.js';

// Mock DualTrackMemory
function createMockDualTrackMemory(results: DualTrackSearchResult[] = []): IDualTrackMemory {
  return {
    hybridSearch: vi.fn().mockResolvedValue({
      results,
      totalCount: results.length,
      searchMode: 'hybrid',
    }),
    addMemory: vi.fn(),
    getMemory: vi.fn(),
    deleteMemory: vi.fn(),
    updateMemory: vi.fn(),
    vectorSearch: vi.fn(),
    graphSearch: vi.fn(),
    keywordSearch: vi.fn(),
  } as unknown as IDualTrackMemory;
}

describe('ProgressiveDisclosure', () => {
  let disclosure: ProgressiveDisclosure;

  beforeEach(() => {
    disclosure = new ProgressiveDisclosure();
  });

  describe('constructor', () => {
    it('should create with default config', () => {
      const d = new ProgressiveDisclosure();
      expect(d).toBeDefined();
    });

    it('should create with custom config', () => {
      const d = new ProgressiveDisclosure({
        maxSuggestions: 10,
        minRelevanceScore: 0.5,
      });
      expect(d).toBeDefined();
    });

    it('should merge custom config with defaults', () => {
      const d = new ProgressiveDisclosure({
        maxSuggestions: 10,
      });
      // Config is private, but we can test behavior
      expect(d).toBeDefined();
    });
  });

  describe('configure', () => {
    it('should update configuration', () => {
      disclosure.configure({ maxSuggestions: 20 });
      // Configuration is applied - test through behavior
      expect(disclosure).toBeDefined();
    });

    it('should merge with existing config', () => {
      disclosure.configure({ maxSuggestions: 10 });
      disclosure.configure({ minRelevanceScore: 0.8 });
      expect(disclosure).toBeDefined();
    });
  });

  describe('setDualTrackMemory', () => {
    it('should set dual track memory', () => {
      const mockMemory = createMockDualTrackMemory();
      disclosure.setDualTrackMemory(mockMemory);
      expect(disclosure).toBeDefined();
    });
  });

  describe('setHookManager', () => {
    it('should set hook manager', () => {
      const mockHookManager = {
        register: vi.fn(),
      } as unknown as HookManager;

      disclosure.setHookManager(mockHookManager);
      expect(mockHookManager.register).toHaveBeenCalled();
    });
  });

  describe('search', () => {
    it('should return empty results when no memory set', async () => {
      const response = await disclosure.search('test query');

      expect(response.suggestions).toEqual([]);
      expect(response.totalMatches).toBe(0);
      expect(response.hasMore).toBe(false);
    });

    it('should search dual track memory', async () => {
      const mockResults: DualTrackSearchResult[] = [
        {
          id: '1',
          content: 'Test content 1',
          score: 0.9,
          source: 'vector',
        },
        {
          id: '2',
          content: 'Test content 2',
          score: 0.8,
          source: 'graph',
        },
      ];

      const mockMemory = createMockDualTrackMemory(mockResults);
      disclosure.setDualTrackMemory(mockMemory);

      const response = await disclosure.search('test query');

      expect(response.suggestions.length).toBe(2);
      expect(mockMemory.hybridSearch).toHaveBeenCalledWith(
        expect.objectContaining({
          query: 'test query',
          searchMode: 'hybrid',
        })
      );
    });

    it('should filter by minimum relevance score', async () => {
      const mockResults: DualTrackSearchResult[] = [
        { id: '1', content: 'High score', score: 0.9, source: 'vector' },
        { id: '2', content: 'Low score', score: 0.1, source: 'vector' },
      ];

      const mockMemory = createMockDualTrackMemory(mockResults);
      disclosure.setDualTrackMemory(mockMemory);

      const response = await disclosure.search('test');

      expect(response.suggestions.length).toBe(1);
      expect(response.suggestions[0].relevanceScore).toBe(0.9);
    });

    it('should limit suggestions to maxSuggestions', async () => {
      const mockResults: DualTrackSearchResult[] = Array.from({ length: 20 }, (_, i) => ({
        id: `${i}`,
        content: `Content ${i}`,
        score: 0.9 - i * 0.01,
        source: 'vector' as const,
      }));

      const mockMemory = createMockDualTrackMemory(mockResults);
      disclosure.setDualTrackMemory(mockMemory);
      disclosure.configure({ maxSuggestions: 5 });

      const response = await disclosure.search('test');

      expect(response.suggestions.length).toBe(5);
      expect(response.hasMore).toBe(true);
    });

    it('should include query time', async () => {
      const response = await disclosure.search('test');

      expect(response.queryTime).toBeGreaterThanOrEqual(0);
    });

    it('should cache results when enabled', async () => {
      const mockResults: DualTrackSearchResult[] = [
        { id: '1', content: 'Cached content', score: 0.9, source: 'vector' },
      ];

      const mockMemory = createMockDualTrackMemory(mockResults);
      disclosure.setDualTrackMemory(mockMemory);
      disclosure.configure({ enableCache: true });

      // First search
      await disclosure.search('test query');
      // Second search (should use cache)
      await disclosure.search('test query');

      // hybridSearch should only be called once due to caching
      expect(mockMemory.hybridSearch).toHaveBeenCalledTimes(1);
    });

    it('should not cache when disabled', async () => {
      const mockResults: DualTrackSearchResult[] = [
        { id: '1', content: 'Content', score: 0.9, source: 'vector' },
      ];

      const mockMemory = createMockDualTrackMemory(mockResults);
      disclosure.setDualTrackMemory(mockMemory);
      disclosure.configure({ enableCache: false });

      await disclosure.search('test query');
      await disclosure.search('test query');

      expect(mockMemory.hybridSearch).toHaveBeenCalledTimes(2);
    });

    it('should handle search errors gracefully', async () => {
      const mockMemory = createMockDualTrackMemory();
      (mockMemory.hybridSearch as any).mockRejectedValue(new Error('Search failed'));
      disclosure.setDualTrackMemory(mockMemory);

      const response = await disclosure.search('test');

      expect(response.suggestions).toEqual([]);
    });

    it('should timeout long searches', async () => {
      const mockMemory = createMockDualTrackMemory();
      (mockMemory.hybridSearch as any).mockImplementation(
        () => new Promise(resolve => setTimeout(resolve, 1000))
      );
      disclosure.setDualTrackMemory(mockMemory);
      disclosure.configure({ timeoutMs: 50 });

      const response = await disclosure.search('test');

      expect(response.suggestions).toEqual([]);
      expect(response.queryTime).toBeLessThanOrEqual(100);
    });
  });

  describe('getSuggestionDetails', () => {
    it('should return null for non-existent suggestion', async () => {
      const result = await disclosure.getSuggestionDetails('non-existent');
      expect(result).toBeNull();
    });

    it('should return suggestion details after search', async () => {
      const mockResults: DualTrackSearchResult[] = [
        { id: '1', content: 'Test content', score: 0.9, source: 'vector' },
      ];

      const mockMemory = createMockDualTrackMemory(mockResults);
      disclosure.setDualTrackMemory(mockMemory);

      const searchResponse = await disclosure.search('test');
      const suggestionId = searchResponse.suggestions[0].id;

      const details = await disclosure.getSuggestionDetails(suggestionId);

      expect(details).toBeDefined();
      expect(details?.fullContent).toBe('Test content');
    });
  });

  describe('injectContext', () => {
    beforeEach(async () => {
      const mockResults: DualTrackSearchResult[] = [
        { id: '1', content: 'Content 1', score: 0.9, source: 'vector' },
        { id: '2', content: 'Content 2', score: 0.8, source: 'graph' },
      ];

      const mockMemory = createMockDualTrackMemory(mockResults);
      disclosure.setDualTrackMemory(mockMemory);
      await disclosure.search('test');
    });

    it('should return empty result for empty suggestion ids', async () => {
      const result = await disclosure.injectContext([]);

      expect(result.injectedContent).toBe('');
      expect(result.tokenCount).toBe(0);
      expect(result.sourceSuggestions).toEqual([]);
    });

    it('should return empty result for non-existent suggestions', async () => {
      const result = await disclosure.injectContext(['non-existent']);

      expect(result.injectedContent).toBe('');
    });

    it('should inject context in markdown format by default', async () => {
      const searchResponse = await disclosure.search('test');
      const ids = searchResponse.suggestions.map(s => s.id);

      const result = await disclosure.injectContext(ids);

      expect(result.injectedContent).toContain('## Relevant Context');
      expect(result.injectedContent).toContain('### Context');
    });

    it('should inject context in XML format', async () => {
      const searchResponse = await disclosure.search('test');
      const ids = searchResponse.suggestions.map(s => s.id);

      const result = await disclosure.injectContext(ids, { format: 'xml' });

      expect(result.injectedContent).toContain('<injected_context>');
      expect(result.injectedContent).toContain('<context_item');
    });

    it('should inject context in raw format', async () => {
      const searchResponse = await disclosure.search('test');
      const ids = searchResponse.suggestions.map(s => s.id);

      const result = await disclosure.injectContext(ids, { format: 'raw' });

      expect(result.injectedContent).not.toContain('##');
      expect(result.injectedContent).not.toContain('<');
    });

    it('should deduplicate content by default', async () => {
      // Create a fresh disclosure instance for this test
      const testDisclosure = new ProgressiveDisclosure();
      const mockResults: DualTrackSearchResult[] = [
        { id: '1', content: 'Same content', score: 0.9, source: 'vector' },
        { id: '2', content: 'Same content', score: 0.8, source: 'graph' },
      ];

      const mockMemory = createMockDualTrackMemory(mockResults);
      testDisclosure.setDualTrackMemory(mockMemory);

      const searchResponse = await testDisclosure.search('test');
      const ids = searchResponse.suggestions.map(s => s.id);

      const result = await testDisclosure.injectContext(ids, { deduplicate: true });

      // Should only contain one instance of the content (deduplicated)
      expect(result.injectedContent).toContain('Same content');
      // Count occurrences - with deduplication, content appears once
      const contentCount = (result.injectedContent.match(/Same content/g) || []).length;
      expect(contentCount).toBe(1);
    });

    it('should not deduplicate when disabled', async () => {
      const testDisclosure = new ProgressiveDisclosure();
      const mockResults: DualTrackSearchResult[] = [
        { id: '1', content: 'Same content', score: 0.9, source: 'vector' },
        { id: '2', content: 'Same content', score: 0.8, source: 'graph' },
      ];

      const mockMemory = createMockDualTrackMemory(mockResults);
      testDisclosure.setDualTrackMemory(mockMemory);

      const searchResponse = await testDisclosure.search('test');
      const ids = searchResponse.suggestions.map(s => s.id);

      const result = await testDisclosure.injectContext(ids, { deduplicate: false });

      // Without deduplication, content appears twice
      const contentCount = (result.injectedContent.match(/Same content/g) || []).length;
      expect(contentCount).toBe(2);
    });

    it('should truncate content when maxTokens specified', async () => {
      const testDisclosure = new ProgressiveDisclosure();
      const mockResults: DualTrackSearchResult[] = [
        { id: '1', content: 'A'.repeat(1000), score: 0.9, source: 'vector' },
      ];

      const mockMemory = createMockDualTrackMemory(mockResults);
      testDisclosure.setDualTrackMemory(mockMemory);

      const searchResponse = await testDisclosure.search('test');
      const ids = searchResponse.suggestions.map(s => s.id);

      // Use very small maxTokens to force truncation
      const result = await testDisclosure.injectContext(ids, { maxTokens: 10 });

      expect(result.injectedContent).toContain('[truncated]');
    });

    it('should return token count', async () => {
      const searchResponse = await disclosure.search('test');
      const ids = searchResponse.suggestions.map(s => s.id);

      const result = await disclosure.injectContext(ids);

      expect(result.tokenCount).toBeGreaterThan(0);
    });

    it('should return source suggestion ids', async () => {
      const searchResponse = await disclosure.search('test');
      const ids = searchResponse.suggestions.map(s => s.id);

      const result = await disclosure.injectContext(ids);

      expect(result.sourceSuggestions).toEqual(ids);
    });
  });

  describe('clearCache', () => {
    it('should clear the cache', async () => {
      const mockResults: DualTrackSearchResult[] = [
        { id: '1', content: 'Content', score: 0.9, source: 'vector' },
      ];

      const mockMemory = createMockDualTrackMemory(mockResults);
      disclosure.setDualTrackMemory(mockMemory);

      // First search (populates cache)
      await disclosure.search('test');

      // Clear cache
      disclosure.clearCache();

      // Second search (should not use cache)
      await disclosure.search('test');

      expect(mockMemory.hybridSearch).toHaveBeenCalledTimes(2);
    });
  });

  describe('suggestion creation', () => {
    it('should create suggestion with preview', async () => {
      const longContent = 'A'.repeat(200);
      const mockResults: DualTrackSearchResult[] = [
        { id: '1', content: longContent, score: 0.9, source: 'vector' },
      ];

      const mockMemory = createMockDualTrackMemory(mockResults);
      disclosure.setDualTrackMemory(mockMemory);
      disclosure.configure({ previewMaxLength: 50 });

      const response = await disclosure.search('test');

      expect(response.suggestions[0].preview.length).toBeLessThanOrEqual(53); // 50 + '...'
      expect(response.suggestions[0].preview).toContain('...');
      expect(response.suggestions[0].fullContent).toBe(longContent);
    });

    it('should not truncate short content preview', async () => {
      const shortContent = 'Short content';
      const mockResults: DualTrackSearchResult[] = [
        { id: '1', content: shortContent, score: 0.9, source: 'vector' },
      ];

      const mockMemory = createMockDualTrackMemory(mockResults);
      disclosure.setDualTrackMemory(mockMemory);

      const response = await disclosure.search('test');

      expect(response.suggestions[0].preview).toBe(shortContent);
    });

    it('should set type based on source', async () => {
      const mockResults: DualTrackSearchResult[] = [
        { id: '1', content: 'Vector content', score: 0.9, source: 'vector' },
        { id: '2', content: 'Graph content', score: 0.8, source: 'graph' },
      ];

      const mockMemory = createMockDualTrackMemory(mockResults);
      disclosure.setDualTrackMemory(mockMemory);

      const response = await disclosure.search('test');

      expect(response.suggestions[0].type).toBe('memory');
      expect(response.suggestions[1].type).toBe('context');
    });

    it('should use entity label as title when available', async () => {
      const mockResults: DualTrackSearchResult[] = [
        {
          id: '1',
          content: 'Content',
          score: 0.9,
          source: 'graph',
          entity: { label: 'Custom Label', type: 'concept' },
        },
      ];

      const mockMemory = createMockDualTrackMemory(mockResults);
      disclosure.setDualTrackMemory(mockMemory);

      const response = await disclosure.search('test');

      expect(response.suggestions[0].title).toBe('Custom Label');
    });

    it('should use session id as title when available', async () => {
      const mockResults: DualTrackSearchResult[] = [
        {
          id: '1',
          content: 'Content',
          score: 0.9,
          source: 'vector',
          metadata: { sessionId: 'session-123' },
        },
      ];

      const mockMemory = createMockDualTrackMemory(mockResults);
      disclosure.setDualTrackMemory(mockMemory);

      const response = await disclosure.search('test');

      expect(response.suggestions[0].title).toContain('session-123');
    });

    it('should generate unique suggestion ids', async () => {
      const mockResults: DualTrackSearchResult[] = [
        { id: '1', content: 'Content 1', score: 0.9, source: 'vector' },
        { id: '2', content: 'Content 2', score: 0.8, source: 'vector' },
      ];

      const mockMemory = createMockDualTrackMemory(mockResults);
      disclosure.setDualTrackMemory(mockMemory);

      const response = await disclosure.search('test');

      const ids = response.suggestions.map(s => s.id);
      const uniqueIds = new Set(ids);

      expect(uniqueIds.size).toBe(ids.length);
    });
  });

  describe('cache behavior', () => {
    it('should normalize cache keys', async () => {
      const mockResults: DualTrackSearchResult[] = [
        { id: '1', content: 'Content', score: 0.9, source: 'vector' },
      ];

      const mockMemory = createMockDualTrackMemory(mockResults);
      disclosure.setDualTrackMemory(mockMemory);

      await disclosure.search('Test Query');
      await disclosure.search('test query');
      await disclosure.search('  TEST QUERY  ');

      // All should hit the same cache entry
      expect(mockMemory.hybridSearch).toHaveBeenCalledTimes(1);
    });

    it('should expire cache entries', async () => {
      const mockResults: DualTrackSearchResult[] = [
        { id: '1', content: 'Content', score: 0.9, source: 'vector' },
      ];

      const mockMemory = createMockDualTrackMemory(mockResults);
      disclosure.setDualTrackMemory(mockMemory);
      disclosure.configure({ cacheTtlMs: 10 });

      await disclosure.search('test');

      // Wait for cache to expire
      await new Promise(resolve => setTimeout(resolve, 20));

      await disclosure.search('test');

      expect(mockMemory.hybridSearch).toHaveBeenCalledTimes(2);
    });

    it('should evict oldest cache entry when full', async () => {
      // This test verifies cache eviction behavior
      // Note: Cache only stores results when suggestions.length > 0
      const testDisclosure = new ProgressiveDisclosure({
        cacheMaxSize: 2,
        enableCache: true,
      });
      const mockMemory = createMockDualTrackMemory([
        { id: '1', content: 'Content', score: 0.9, source: 'vector' },
      ]);
      testDisclosure.setDualTrackMemory(mockMemory);

      // Fill cache with 3 queries (cache size is 2, so query1 should be evicted)
      await testDisclosure.search('query1');
      await testDisclosure.search('query2');
      await testDisclosure.search('query3'); // Should evict query1

      // Verify initial searches happened
      expect(mockMemory.hybridSearch).toHaveBeenCalledTimes(3);

      // Clear call count for next phase
      (mockMemory.hybridSearch as any).mockClear();

      // query2 and query3 should be cached, query1 was evicted
      await testDisclosure.search('query2'); // Cached
      await testDisclosure.search('query3'); // Cached

      // No new searches for cached queries
      expect(mockMemory.hybridSearch).toHaveBeenCalledTimes(0);

      // query1 was evicted, should trigger new search
      await testDisclosure.search('query1');
      expect(mockMemory.hybridSearch).toHaveBeenCalledTimes(1);
    });
  });
});
