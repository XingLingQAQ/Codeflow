import { describe, it, expect, beforeEach, vi } from 'vitest';
import { SemanticRetriever } from '../SemanticRetriever.js';
import { IVectorStore, VectorSearchResult, MemoryChunk } from '../../memory/types.js';

describe('SemanticRetriever', () => {
  let retriever: SemanticRetriever;
  let mockVectorStore: IVectorStore;

  const createChunk = (id: string, content: string): MemoryChunk => ({
    id,
    content,
    embedding: new Array(384).fill(0),
    metadata: {
      sessionId: 'session1',
      agentRole: 'assistant',
      messageIndex: 0,
      chunkIndex: 0,
      timestamp: Date.now(),
      source: 'assistant',
    },
  });

  const createVectorResult = (chunk: MemoryChunk, score: number): VectorSearchResult => ({
    chunk,
    score,
  });

  beforeEach(() => {
    mockVectorStore = {
      search: vi.fn(),
      add: vi.fn(),
      delete: vi.fn(),
      clear: vi.fn(),
      count: vi.fn(),
      getBySessionId: vi.fn(),
      getByGitCommit: vi.fn(),
      getCollectionInfo: vi.fn(),
    };

    retriever = new SemanticRetriever(mockVectorStore);
  });

  describe('constructor', () => {
    it('should create retriever with default config', () => {
      expect(retriever).toBeDefined();
    });

    it('should create retriever with custom config', () => {
      const customRetriever = new SemanticRetriever(mockVectorStore, {
        vectorWeight: 0.8,
        keywordWeight: 0.2,
        topK: 20,
      });
      expect(customRetriever).toBeDefined();
    });
  });

  describe('vectorSearch', () => {
    it('should perform vector search with default options', async () => {
      const mockResults = [
        createVectorResult(createChunk('chunk1', 'TypeScript code'), 0.9),
        createVectorResult(createChunk('chunk2', 'JavaScript code'), 0.8),
      ];

      vi.mocked(mockVectorStore.search).mockResolvedValue(mockResults);

      const results = await retriever.vectorSearch('TypeScript');

      expect(mockVectorStore.search).toHaveBeenCalledWith('TypeScript', {
        topK: 10,
        minScore: 0.3,
      });
      expect(results).toEqual(mockResults);
    });

    it('should perform vector search with custom options', async () => {
      const mockResults = [createVectorResult(createChunk('chunk1', 'content'), 0.95)];

      vi.mocked(mockVectorStore.search).mockResolvedValue(mockResults);

      await retriever.vectorSearch('query', { topK: 5, minScore: 0.5 });

      expect(mockVectorStore.search).toHaveBeenCalledWith('query', {
        topK: 5,
        minScore: 0.5,
      });
    });
  });

  describe('keywordSearch', () => {
    beforeEach(() => {
      // Index some content
      retriever.indexContent('chunk1', 'TypeScript programming language', {
        sessionId: 'session1',
        agentRole: 'assistant',
        messageIndex: 0,
        chunkIndex: 0,
        timestamp: Date.now(),
        source: 'assistant',
      });

      retriever.indexContent('chunk2', 'JavaScript programming language', {
        sessionId: 'session1',
        agentRole: 'assistant',
        messageIndex: 1,
        chunkIndex: 0,
        timestamp: Date.now(),
        source: 'assistant',
      });
    });

    it('should perform keyword search', async () => {
      const results = await retriever.keywordSearch('TypeScript');

      expect(results.length).toBeGreaterThan(0);
      expect(results[0].content).toContain('TypeScript');
    });

    it('should return results sorted by score', async () => {
      const results = await retriever.keywordSearch('programming');

      expect(results.length).toBe(2);
      for (let i = 1; i < results.length; i++) {
        expect(results[i - 1].score).toBeGreaterThanOrEqual(results[i].score);
      }
    });

    it('should include highlights', async () => {
      const results = await retriever.keywordSearch('TypeScript');

      expect(results[0].highlights).toBeDefined();
      expect(results[0].highlights.length).toBeGreaterThan(0);
    });

    it('should respect minScore threshold', async () => {
      const results = await retriever.keywordSearch('nonexistent', { minScore: 0.9 });

      expect(results.length).toBe(0);
    });

    it('should respect topK limit', async () => {
      const results = await retriever.keywordSearch('programming', { topK: 1 });

      expect(results.length).toBeLessThanOrEqual(1);
    });

    it('should handle empty index', async () => {
      retriever.clearIndex();
      const results = await retriever.keywordSearch('TypeScript');

      expect(results.length).toBe(0);
    });
  });

  describe('hybridSearch', () => {
    beforeEach(() => {
      // Index content for keyword search
      retriever.indexContent('session1_0_0', 'TypeScript programming', {
        sessionId: 'session1',
        agentRole: 'assistant',
        messageIndex: 0,
        chunkIndex: 0,
        timestamp: Date.now(),
        source: 'assistant',
      });

      // Mock vector search
      const mockResults = [
        createVectorResult(createChunk('session1_0_0', 'TypeScript programming'), 0.9),
      ];
      vi.mocked(mockVectorStore.search).mockResolvedValue(mockResults);
    });

    it('should perform hybrid search', async () => {
      const results = await retriever.hybridSearch('TypeScript');

      expect(results.length).toBeGreaterThan(0);
      expect(results[0].source).toBe('hybrid');
    });

    it('should combine vector and keyword scores', async () => {
      const results = await retriever.hybridSearch('TypeScript');

      expect(results[0].vectorScore).toBeDefined();
      expect(results[0].keywordScore).toBeDefined();
      expect(results[0].score).toBeGreaterThan(0);
    });

    it('should apply custom weights', async () => {
      const results = await retriever.hybridSearch('TypeScript', {
        vectorWeight: 0.8,
        keywordWeight: 0.2,
      });

      expect(results.length).toBeGreaterThan(0);
    });

    it('should respect topK limit', async () => {
      const results = await retriever.hybridSearch('TypeScript', { topK: 1 });

      expect(results.length).toBeLessThanOrEqual(1);
    });

    it('should apply reranking when enabled', async () => {
      const results = await retriever.hybridSearch('TypeScript', { reranking: true });

      expect(results.length).toBeGreaterThan(0);
    });

    it('should skip reranking when disabled', async () => {
      const results = await retriever.hybridSearch('TypeScript', { reranking: false });

      expect(results.length).toBeGreaterThan(0);
    });
  });

  describe('searchHistoricalContext', () => {
    beforeEach(() => {
      retriever.indexContent('session1_0_0', 'TypeScript code example', {
        sessionId: 'session1',
        agentRole: 'assistant',
        messageIndex: 0,
        chunkIndex: 0,
        timestamp: Date.now(),
        source: 'assistant',
      });

      const mockResults = [
        createVectorResult(createChunk('session1_0_0', 'TypeScript code example'), 0.9),
      ];
      vi.mocked(mockVectorStore.search).mockResolvedValue(mockResults);
    });

    it('should search with vector mode', async () => {
      const result = await retriever.searchHistoricalContext({
        query: 'TypeScript',
        searchType: 'vector',
      });

      expect(result.searchType).toBe('vector');
      expect(result.matches.length).toBeGreaterThan(0);
      expect(result.queryTime).toBeGreaterThanOrEqual(0);
    });

    it('should search with keyword mode', async () => {
      const result = await retriever.searchHistoricalContext({
        query: 'TypeScript',
        searchType: 'keyword',
      });

      expect(result.searchType).toBe('keyword');
      expect(result.matches.length).toBeGreaterThan(0);
    });

    it('should search with hybrid mode', async () => {
      const result = await retriever.searchHistoricalContext({
        query: 'TypeScript',
        searchType: 'hybrid',
      });

      expect(result.searchType).toBe('hybrid');
      expect(result.matches.length).toBeGreaterThan(0);
    });

    it('should default to hybrid mode', async () => {
      const result = await retriever.searchHistoricalContext({
        query: 'TypeScript',
      });

      expect(result.searchType).toBe('hybrid');
    });

    it('should filter by sessionId', async () => {
      const result = await retriever.searchHistoricalContext({
        query: 'TypeScript',
        sessionId: 'session1',
      });

      expect(result.matches.every(m => m.metadata.sessionId === 'session1')).toBe(true);
    });

    it('should filter by agentRole', async () => {
      const result = await retriever.searchHistoricalContext({
        query: 'TypeScript',
        agentRole: 'assistant',
      });

      expect(result.matches.every(m => m.metadata.agentRole === 'assistant')).toBe(true);
    });

    it('should filter by timeRange', async () => {
      const now = Date.now();
      const result = await retriever.searchHistoricalContext({
        query: 'TypeScript',
        timeRange: { start: now - 10000, end: now + 10000 },
      });

      expect(result.matches.length).toBeGreaterThan(0);
    });

    it('should respect limit parameter', async () => {
      const result = await retriever.searchHistoricalContext({
        query: 'TypeScript',
        limit: 1,
      });

      expect(result.matches.length).toBeLessThanOrEqual(1);
    });

    it('should return totalCount', async () => {
      const result = await retriever.searchHistoricalContext({
        query: 'TypeScript',
      });

      expect(result.totalCount).toBe(result.matches.length);
    });
  });

  describe('indexContent', () => {
    it('should index content for keyword search', () => {
      retriever.indexContent('chunk1', 'test content', {
        sessionId: 'session1',
        agentRole: 'assistant',
        messageIndex: 0,
        chunkIndex: 0,
        timestamp: Date.now(),
        source: 'assistant',
      });

      // Verify by searching
      retriever.keywordSearch('test').then(results => {
        expect(results.length).toBeGreaterThan(0);
      });
    });

    it('should handle multiple content pieces', () => {
      retriever.indexContent('chunk1', 'first content', {
        sessionId: 'session1',
        agentRole: 'assistant',
        messageIndex: 0,
        chunkIndex: 0,
        timestamp: Date.now(),
        source: 'assistant',
      });

      retriever.indexContent('chunk2', 'second content', {
        sessionId: 'session1',
        agentRole: 'assistant',
        messageIndex: 1,
        chunkIndex: 0,
        timestamp: Date.now(),
        source: 'assistant',
      });

      retriever.keywordSearch('content').then(results => {
        expect(results.length).toBe(2);
      });
    });
  });

  describe('clearIndex', () => {
    it('should clear all indexed content', async () => {
      retriever.indexContent('chunk1', 'test content', {
        sessionId: 'session1',
        agentRole: 'assistant',
        messageIndex: 0,
        chunkIndex: 0,
        timestamp: Date.now(),
        source: 'assistant',
      });

      retriever.clearIndex();

      const results = await retriever.keywordSearch('test');
      expect(results.length).toBe(0);
    });
  });
});
