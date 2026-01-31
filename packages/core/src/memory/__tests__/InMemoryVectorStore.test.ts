import { describe, it, expect, beforeEach } from 'vitest';
import { InMemoryVectorStore } from '../InMemoryVectorStore.js';
import { DocumentChunk, ChunkMetadata } from '../types.js';

describe('InMemoryVectorStore', () => {
  let store: InMemoryVectorStore;

  const createChunk = (id: string, content: string, sessionId = 'session1'): DocumentChunk => ({
    id,
    content,
    metadata: {
      sessionId,
      agentRole: 'assistant',
      messageIndex: 0,
      chunkIndex: 0,
      timestamp: Date.now(),
      source: 'assistant',
    },
  });

  beforeEach(() => {
    store = new InMemoryVectorStore();
  });

  describe('constructor', () => {
    it('should create store with default config', () => {
      const defaultStore = new InMemoryVectorStore();
      expect(defaultStore).toBeDefined();
    });

    it('should create store with custom config', () => {
      const customStore = new InMemoryVectorStore({ collectionName: 'custom' });
      expect(customStore).toBeDefined();
    });
  });

  describe('add and count', () => {
    it('should add chunks and update count', async () => {
      const chunks = [
        createChunk('chunk1', 'Hello world'),
        createChunk('chunk2', 'Goodbye world'),
      ];

      await store.add(chunks);
      const count = await store.count();
      expect(count).toBe(2);
    });

    it('should handle empty array', async () => {
      await store.add([]);
      const count = await store.count();
      expect(count).toBe(0);
    });
  });

  describe('search', () => {
    it('should search and return results sorted by score', async () => {
      const chunks = [
        createChunk('chunk1', 'TypeScript programming language'),
        createChunk('chunk2', 'JavaScript programming language'),
        createChunk('chunk3', 'Python programming language'),
      ];

      await store.add(chunks);
      const results = await store.search('TypeScript');

      expect(results.length).toBeGreaterThan(0);
      expect(results[0].score).toBeGreaterThanOrEqual(results[results.length - 1].score);
    });

    it('should respect topK option', async () => {
      const chunks = [
        createChunk('chunk1', 'First document'),
        createChunk('chunk2', 'Second document'),
        createChunk('chunk3', 'Third document'),
      ];

      await store.add(chunks);
      const results = await store.search('document', { topK: 2 });

      expect(results.length).toBeLessThanOrEqual(2);
    });

    it('should filter by minScore', async () => {
      const chunks = [
        createChunk('chunk1', 'Exact match query'),
        createChunk('chunk2', 'Completely different content'),
      ];

      await store.add(chunks);
      const results = await store.search('Exact match query', { minScore: 0.5 });

      for (const result of results) {
        expect(result.score).toBeGreaterThanOrEqual(0.5);
      }
    });

    it('should apply metadata filter', async () => {
      const chunks = [
        createChunk('chunk1', 'Session one content', 'session1'),
        createChunk('chunk2', 'Session two content', 'session2'),
      ];

      await store.add(chunks);
      const results = await store.search('content', {
        filter: { sessionId: 'session1' },
      });

      for (const result of results) {
        expect(result.chunk.metadata.sessionId).toBe('session1');
      }
    });

    it('should include embeddings when requested', async () => {
      const chunks = [createChunk('chunk1', 'Test content')];

      await store.add(chunks);
      const results = await store.search('Test', { includeEmbeddings: true });

      if (results.length > 0) {
        expect(results[0].chunk.embedding).toBeDefined();
        expect(Array.isArray(results[0].chunk.embedding)).toBe(true);
      }
    });
  });

  describe('delete', () => {
    it('should delete chunks by ids', async () => {
      const chunks = [
        createChunk('chunk1', 'First'),
        createChunk('chunk2', 'Second'),
      ];

      await store.add(chunks);
      await store.delete(['chunk1']);

      const count = await store.count();
      expect(count).toBe(1);
    });

    it('should handle deleting non-existent ids', async () => {
      await store.add([createChunk('chunk1', 'Content')]);
      await store.delete(['nonexistent']);

      const count = await store.count();
      expect(count).toBe(1);
    });
  });

  describe('clear', () => {
    it('should clear all chunks', async () => {
      const chunks = [
        createChunk('chunk1', 'First'),
        createChunk('chunk2', 'Second'),
      ];

      await store.add(chunks);
      await store.clear();

      const count = await store.count();
      expect(count).toBe(0);
    });
  });

  describe('getBySessionId', () => {
    it('should return chunks for specific session', async () => {
      const chunks = [
        createChunk('chunk1', 'Session 1 content', 'session1'),
        createChunk('chunk2', 'Session 2 content', 'session2'),
        createChunk('chunk3', 'Another session 1', 'session1'),
      ];

      await store.add(chunks);
      const results = await store.getBySessionId('session1');

      expect(results.length).toBe(2);
      for (const chunk of results) {
        expect(chunk.metadata.sessionId).toBe('session1');
      }
    });

    it('should return empty array for non-existent session', async () => {
      await store.add([createChunk('chunk1', 'Content', 'session1')]);
      const results = await store.getBySessionId('nonexistent');

      expect(results).toEqual([]);
    });
  });

  describe('getByGitCommit', () => {
    it('should return chunks for specific commit', async () => {
      const chunk: DocumentChunk = {
        id: 'chunk1',
        content: 'Commit content',
        metadata: {
          sessionId: 'session1',
          agentRole: 'assistant',
          gitCommitHash: 'abc123',
          messageIndex: 0,
          chunkIndex: 0,
          timestamp: Date.now(),
          source: 'assistant',
        },
      };

      await store.add([chunk]);
      const results = await store.getByGitCommit('abc123');

      expect(results.length).toBe(1);
      expect(results[0].metadata.gitCommitHash).toBe('abc123');
    });
  });

  describe('getCollectionInfo', () => {
    it('should return collection info', async () => {
      await store.add([createChunk('chunk1', 'Content')]);
      const info = await store.getCollectionInfo();

      expect(info.name).toBe('codeflow_memory');
      expect(info.count).toBe(1);
      expect(info.dimension).toBeGreaterThan(0);
    });
  });
});
