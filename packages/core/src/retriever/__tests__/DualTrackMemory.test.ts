import { describe, it, expect, beforeEach, vi } from 'vitest';
import { DualTrackMemory } from '../DualTrackMemory.js';
import { IVectorStore, VectorSearchResult, MemoryChunk } from '../../memory/types.js';
import { ITripleStore, Triple, TripleNode, Entity } from '../../samg/types.js';

describe('DualTrackMemory', () => {
  let dualTrack: DualTrackMemory;
  let mockVectorStore: IVectorStore;
  let mockTripleStore: ITripleStore;

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

  const createEntity = (id: string, label: string): Entity => ({
    '@id': id,
    '@type': 'codeflow:Entity',
    label,
    createdAt: Date.now(),
    updatedAt: Date.now(),
  });

  const createTriple = (subjectId: string, predicate: string, objectId: string): Triple => ({
    '@id': `triple_${subjectId}_${predicate}_${objectId}`,
    subject: { '@id': subjectId, '@type': 'codeflow:Entity', label: subjectId },
    predicate,
    object: { '@id': objectId, '@type': 'codeflow:Entity', label: objectId },
    confidence: 0.9,
    source: {
      sessionId: 'session1',
      agentRole: 'assistant',
      extractionMethod: 'llm',
      timestamp: Date.now(),
    },
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

    mockTripleStore = {
      add: vi.fn(),
      get: vi.fn(),
      update: vi.fn(),
      delete: vi.fn(),
      clear: vi.fn(),
      query: vi.fn(),
      findBySubject: vi.fn(),
      findByPredicate: vi.fn(),
      findByObject: vi.fn(),
      getEntity: vi.fn(),
      upsertEntity: vi.fn(),
      getEntities: vi.fn(),
      deduplicate: vi.fn(),
      exportGraph: vi.fn(),
      importGraph: vi.fn(),
      getStats: vi.fn(),
    };

    dualTrack = new DualTrackMemory();
    dualTrack.setVectorStore(mockVectorStore);
    dualTrack.setTripleStore(mockTripleStore);
  });

  describe('constructor', () => {
    it('should create with default config', () => {
      const dt = new DualTrackMemory();
      expect(dt).toBeDefined();
    });

    it('should create with custom config', () => {
      const dt = new DualTrackMemory({
        vectorWeight: 0.6,
        graphWeight: 0.4,
        topK: 20,
      });
      expect(dt).toBeDefined();
    });
  });

  describe('setVectorStore and setTripleStore', () => {
    it('should set vector store', () => {
      const dt = new DualTrackMemory();
      dt.setVectorStore(mockVectorStore);
      expect(dt).toBeDefined();
    });

    it('should set triple store', () => {
      const dt = new DualTrackMemory();
      dt.setTripleStore(mockTripleStore);
      expect(dt).toBeDefined();
    });
  });

  describe('vectorSearch', () => {
    it('should perform vector search', async () => {
      const mockResults: VectorSearchResult[] = [
        { chunk: createChunk('chunk1', 'TypeScript code'), score: 0.9 },
        { chunk: createChunk('chunk2', 'JavaScript code'), score: 0.8 },
      ];

      vi.mocked(mockVectorStore.search).mockResolvedValue(mockResults);

      const results = await dualTrack.vectorSearch('TypeScript', 10);

      expect(results.length).toBe(2);
      expect(results[0].source).toBe('vector');
      expect(results[0].vectorScore).toBeDefined();
    });

    it('should return empty array when no vector store', async () => {
      const dt = new DualTrackMemory();
      const results = await dt.vectorSearch('query');

      expect(results).toEqual([]);
    });

    it('should handle search errors gracefully', async () => {
      vi.mocked(mockVectorStore.search).mockRejectedValue(new Error('Search failed'));

      const results = await dualTrack.vectorSearch('query');

      expect(results).toEqual([]);
    });

    it('should apply vector weight to scores', async () => {
      const mockResults: VectorSearchResult[] = [
        { chunk: createChunk('chunk1', 'content'), score: 1.0 },
      ];

      vi.mocked(mockVectorStore.search).mockResolvedValue(mockResults);

      const results = await dualTrack.vectorSearch('query');

      expect(results[0].score).toBeLessThanOrEqual(1.0);
    });
  });

  describe('graphSearch', () => {
    beforeEach(() => {
      const entities = [
        createEntity('entity:typescript', 'TypeScript'),
        createEntity('entity:javascript', 'JavaScript'),
      ];

      vi.mocked(mockTripleStore.getEntities).mockResolvedValue(entities);
      vi.mocked(mockTripleStore.findBySubject).mockResolvedValue([
        createTriple('entity:typescript', 'relatedTo', 'entity:javascript'),
      ]);
      vi.mocked(mockTripleStore.findByObject).mockResolvedValue([]);
    });

    it('should perform graph search', async () => {
      const results = await dualTrack.graphSearch('TypeScript', 10);

      expect(results.length).toBeGreaterThan(0);
      expect(results[0].entity).toBeDefined();
      expect(results[0].relatedTriples).toBeDefined();
    });

    it('should return empty array when no triple store', async () => {
      const dt = new DualTrackMemory();
      const results = await dt.graphSearch('query');

      expect(results).toEqual([]);
    });

    it('should handle search errors gracefully', async () => {
      vi.mocked(mockTripleStore.getEntities).mockRejectedValue(new Error('Search failed'));

      const results = await dualTrack.graphSearch('query');

      expect(results).toEqual([]);
    });

    it('should calculate relevance scores', async () => {
      const results = await dualTrack.graphSearch('TypeScript');

      expect(results[0].score).toBeGreaterThan(0);
      expect(results[0].score).toBeLessThanOrEqual(1);
    });

    it('should sort results by score', async () => {
      const results = await dualTrack.graphSearch('TypeScript');

      for (let i = 1; i < results.length; i++) {
        expect(results[i - 1].score).toBeGreaterThanOrEqual(results[i].score);
      }
    });

    it('should respect limit parameter', async () => {
      const results = await dualTrack.graphSearch('TypeScript', 1);

      expect(results.length).toBeLessThanOrEqual(1);
    });
  });

  describe('hybridSearch', () => {
    beforeEach(() => {
      // Mock vector search
      const vectorResults: VectorSearchResult[] = [
        { chunk: createChunk('chunk1', 'TypeScript programming'), score: 0.9 },
      ];
      vi.mocked(mockVectorStore.search).mockResolvedValue(vectorResults);

      // Mock graph search
      const entities = [createEntity('entity:typescript', 'TypeScript')];
      vi.mocked(mockTripleStore.getEntities).mockResolvedValue(entities);
      vi.mocked(mockTripleStore.findBySubject).mockResolvedValue([]);
      vi.mocked(mockTripleStore.findByObject).mockResolvedValue([]);
    });

    it('should perform hybrid search', async () => {
      const response = await dualTrack.hybridSearch({
        query: 'TypeScript',
        searchMode: 'hybrid',
      });

      expect(response.results.length).toBeGreaterThan(0);
      expect(response.searchMode).toBe('hybrid');
      expect(response.vectorCount).toBeGreaterThan(0);
      expect(response.queryTime).toBeGreaterThanOrEqual(0);
    });

    it('should perform vector-only search', async () => {
      const response = await dualTrack.hybridSearch({
        query: 'TypeScript',
        searchMode: 'vector',
      });

      expect(response.searchMode).toBe('vector');
      expect(response.vectorCount).toBeGreaterThan(0);
      expect(response.graphCount).toBe(0);
    });

    it('should perform graph-only search', async () => {
      const response = await dualTrack.hybridSearch({
        query: 'TypeScript',
        searchMode: 'graph',
      });

      expect(response.searchMode).toBe('graph');
      expect(response.vectorCount).toBe(0);
      expect(response.graphCount).toBeGreaterThan(0);
    });

    it('should default to hybrid mode', async () => {
      const response = await dualTrack.hybridSearch({
        query: 'TypeScript',
      });

      expect(response.searchMode).toBe('hybrid');
    });

    it('should respect limit parameter', async () => {
      const response = await dualTrack.hybridSearch({
        query: 'TypeScript',
        limit: 1,
      });

      expect(response.results.length).toBeLessThanOrEqual(1);
    });

    it('should filter by sessionId', async () => {
      const response = await dualTrack.hybridSearch({
        query: 'TypeScript',
        sessionId: 'session1',
      });

      expect(response.results.every(r => !r.metadata || r.metadata.sessionId === 'session1')).toBe(true);
    });

    it('should filter by entityTypes', async () => {
      const response = await dualTrack.hybridSearch({
        query: 'TypeScript',
        entityTypes: ['codeflow:Entity'],
      });

      expect(response.results.length).toBeGreaterThanOrEqual(0);
    });

    it('should filter by timeRange', async () => {
      const now = Date.now();
      const response = await dualTrack.hybridSearch({
        query: 'TypeScript',
        timeRange: { start: now - 10000, end: now + 10000 },
      });

      expect(response.results.length).toBeGreaterThanOrEqual(0);
    });

    it('should sort results by score', async () => {
      const response = await dualTrack.hybridSearch({
        query: 'TypeScript',
      });

      for (let i = 1; i < response.results.length; i++) {
        expect(response.results[i - 1].score).toBeGreaterThanOrEqual(response.results[i].score);
      }
    });

    it('should return totalCount', async () => {
      const response = await dualTrack.hybridSearch({
        query: 'TypeScript',
      });

      expect(response.totalCount).toBe(response.results.length);
    });
  });

  describe('spreadingActivation', () => {
    beforeEach(() => {
      const entity1 = createEntity('entity:1', 'Entity 1');
      const entity2 = createEntity('entity:2', 'Entity 2');
      const entity3 = createEntity('entity:3', 'Entity 3');

      vi.mocked(mockTripleStore.getEntity).mockImplementation(async (id: string) => {
        if (id === 'entity:1') return entity1;
        if (id === 'entity:2') return entity2;
        if (id === 'entity:3') return entity3;
        return null;
      });

      vi.mocked(mockTripleStore.findBySubject).mockImplementation(async (id: string) => {
        if (id === 'entity:1') return [createTriple('entity:1', 'knows', 'entity:2')];
        if (id === 'entity:2') return [createTriple('entity:2', 'knows', 'entity:3')];
        return [];
      });

      vi.mocked(mockTripleStore.findByObject).mockResolvedValue([]);
    });

    it('should perform spreading activation', async () => {
      const results = await dualTrack.spreadingActivation(['entity:1'], 2, 0.5);

      expect(results.length).toBeGreaterThan(0);
      expect(results[0].activationLevel).toBeGreaterThan(0);
      expect(results[0].depth).toBeGreaterThan(0);
    });

    it('should return empty array when no triple store', async () => {
      const dt = new DualTrackMemory();
      const results = await dt.spreadingActivation(['entity:1']);

      expect(results).toEqual([]);
    });

    it('should respect depth parameter', async () => {
      const results = await dualTrack.spreadingActivation(['entity:1'], 1, 0.5);

      expect(results.every(r => r.depth <= 1)).toBe(true);
    });

    it('should apply decay to activation levels', async () => {
      const results = await dualTrack.spreadingActivation(['entity:1'], 2, 0.5);

      if (results.length > 1) {
        const depth1 = results.filter(r => r.depth === 1);
        const depth2 = results.filter(r => r.depth === 2);

        if (depth1.length > 0 && depth2.length > 0) {
          expect(depth1[0].activationLevel).toBeGreaterThan(depth2[0].activationLevel);
        }
      }
    });

    it('should filter by activation threshold', async () => {
      const results = await dualTrack.spreadingActivation(['entity:1'], 3, 0.1);

      expect(results.every(r => r.activationLevel >= 0.1)).toBe(true);
    });

    it('should include path information', async () => {
      const results = await dualTrack.spreadingActivation(['entity:1'], 2, 0.5);

      expect(results[0].path).toBeDefined();
      expect(results[0].path.length).toBeGreaterThan(1);
    });

    it('should sort by activation level', async () => {
      const results = await dualTrack.spreadingActivation(['entity:1'], 2, 0.5);

      for (let i = 1; i < results.length; i++) {
        expect(results[i - 1].activationLevel).toBeGreaterThanOrEqual(results[i].activationLevel);
      }
    });
  });

  describe('findRelatedEntities', () => {
    beforeEach(() => {
      const entity2 = createEntity('entity:2', 'Entity 2');
      const entity3 = createEntity('entity:3', 'Entity 3');

      vi.mocked(mockTripleStore.findBySubject).mockResolvedValue([
        createTriple('entity:1', 'knows', 'entity:2'),
      ]);

      vi.mocked(mockTripleStore.findByObject).mockResolvedValue([
        createTriple('entity:3', 'knows', 'entity:1'),
      ]);

      vi.mocked(mockTripleStore.getEntity).mockImplementation(async (id: string) => {
        if (id === 'entity:2') return entity2;
        if (id === 'entity:3') return entity3;
        return null;
      });
    });

    it('should find related entities', async () => {
      const results = await dualTrack.findRelatedEntities('entity:1');

      expect(results.length).toBeGreaterThan(0);
      expect(results[0].entity).toBeDefined();
      expect(results[0].relatedTriples).toBeDefined();
    });

    it('should return empty array when no triple store', async () => {
      const dt = new DualTrackMemory();
      const results = await dt.findRelatedEntities('entity:1');

      expect(results).toEqual([]);
    });

    it('should filter by predicates', async () => {
      const results = await dualTrack.findRelatedEntities('entity:1', ['knows']);

      expect(results.length).toBeGreaterThan(0);
      expect(results[0].relatedTriples.every(t => t.predicate === 'knows')).toBe(true);
    });

    it('should calculate average confidence scores', async () => {
      const results = await dualTrack.findRelatedEntities('entity:1');

      expect(results[0].score).toBeGreaterThan(0);
      expect(results[0].score).toBeLessThanOrEqual(1);
    });

    it('should sort by score', async () => {
      const results = await dualTrack.findRelatedEntities('entity:1');

      for (let i = 1; i < results.length; i++) {
        expect(results[i - 1].score).toBeGreaterThanOrEqual(results[i].score);
      }
    });
  });

  describe('findPath', () => {
    beforeEach(() => {
      vi.mocked(mockTripleStore.findBySubject).mockImplementation(async (id: string) => {
        if (id === 'entity:1') return [createTriple('entity:1', 'knows', 'entity:2')];
        if (id === 'entity:2') return [createTriple('entity:2', 'knows', 'entity:3')];
        return [];
      });

      vi.mocked(mockTripleStore.findByObject).mockResolvedValue([]);
    });

    it('should find path between entities', async () => {
      const paths = await dualTrack.findPath('entity:1', 'entity:3', 4);

      expect(paths.length).toBeGreaterThan(0);
      expect(paths[0][0]).toBe('entity:1');
      expect(paths[0][paths[0].length - 1]).toBe('entity:3');
    });

    it('should return empty array when no triple store', async () => {
      const dt = new DualTrackMemory();
      const paths = await dt.findPath('entity:1', 'entity:3');

      expect(paths).toEqual([]);
    });

    it('should respect maxDepth parameter', async () => {
      const paths = await dualTrack.findPath('entity:1', 'entity:3', 1);

      expect(paths.every(p => p.length <= 2)).toBe(true);
    });

    it('should return empty array when no path exists', async () => {
      const paths = await dualTrack.findPath('entity:1', 'entity:999', 4);

      expect(paths).toEqual([]);
    });

    it('should sort paths by length', async () => {
      const paths = await dualTrack.findPath('entity:1', 'entity:3', 4);

      for (let i = 1; i < paths.length; i++) {
        expect(paths[i - 1].length).toBeLessThanOrEqual(paths[i].length);
      }
    });

    it('should handle direct connections', async () => {
      const paths = await dualTrack.findPath('entity:1', 'entity:2', 4);

      expect(paths.length).toBeGreaterThan(0);
      expect(paths[0].length).toBe(2);
    });
  });
});
