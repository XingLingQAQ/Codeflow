import { describe, it, expect, beforeEach } from 'vitest';
import { InMemoryTripleStore } from '../InMemoryTripleStore.js';
import { Triple, TripleNode, LiteralValue } from '../types.js';

describe('InMemoryTripleStore', () => {
  let store: InMemoryTripleStore;

  const createTriple = (
    id: string,
    subjectId: string,
    predicate: string,
    objectId: string,
    confidence = 0.9
  ): Triple => ({
    '@id': id,
    subject: { '@id': subjectId, '@type': 'codeflow:Entity' },
    predicate,
    object: { '@id': objectId, '@type': 'codeflow:Entity' },
    confidence,
    source: {
      sessionId: 'session1',
      agentRole: 'assistant',
      extractionMethod: 'llm',
      timestamp: Date.now(),
    },
  });

  const createLiteralTriple = (
    id: string,
    subjectId: string,
    predicate: string,
    value: string,
    confidence = 0.9
  ): Triple => ({
    '@id': id,
    subject: { '@id': subjectId, '@type': 'codeflow:Entity' },
    predicate,
    object: { '@value': value, '@type': 'xsd:string' } as LiteralValue,
    confidence,
    source: {
      sessionId: 'session1',
      agentRole: 'assistant',
      extractionMethod: 'llm',
      timestamp: Date.now(),
    },
  });

  beforeEach(() => {
    store = new InMemoryTripleStore();
  });

  describe('constructor', () => {
    it('should create store with default config', () => {
      const defaultStore = new InMemoryTripleStore();
      expect(defaultStore).toBeDefined();
    });

    it('should create store with custom config', () => {
      const customStore = new InMemoryTripleStore({
        maxTriples: 500,
        enableDeduplication: false,
      });
      expect(customStore).toBeDefined();
    });
  });

  describe('add and get', () => {
    it('should add and retrieve triple', async () => {
      const triple = createTriple('t1', 'user:1', 'knows', 'user:2');
      await store.add([triple]);

      const result = await store.get('t1');
      expect(result).not.toBeNull();
      expect(result?.['@id']).toBe('t1');
    });

    it('should return null for non-existent triple', async () => {
      const result = await store.get('nonexistent');
      expect(result).toBeNull();
    });

    it('should handle multiple triples', async () => {
      const triples = [
        createTriple('t1', 'user:1', 'knows', 'user:2'),
        createTriple('t2', 'user:2', 'knows', 'user:3'),
      ];
      await store.add(triples);

      const stats = await store.getStats();
      expect(stats.tripleCount).toBe(2);
    });
  });

  describe('update', () => {
    it('should update existing triple', async () => {
      const triple = createTriple('t1', 'user:1', 'knows', 'user:2', 0.5);
      await store.add([triple]);

      await store.update('t1', { confidence: 0.9 });

      const result = await store.get('t1');
      expect(result?.confidence).toBe(0.9);
    });

    it('should throw error for non-existent triple', async () => {
      await expect(store.update('nonexistent', { confidence: 0.9 }))
        .rejects.toThrow('Triple not found');
    });
  });

  describe('delete', () => {
    it('should delete triples by ids', async () => {
      const triples = [
        createTriple('t1', 'user:1', 'knows', 'user:2'),
        createTriple('t2', 'user:2', 'knows', 'user:3'),
      ];
      await store.add(triples);

      await store.delete(['t1']);

      const result = await store.get('t1');
      expect(result).toBeNull();

      const stats = await store.getStats();
      expect(stats.tripleCount).toBe(1);
    });

    it('should handle deleting non-existent ids', async () => {
      await store.add([createTriple('t1', 'user:1', 'knows', 'user:2')]);
      await store.delete(['nonexistent']);

      const stats = await store.getStats();
      expect(stats.tripleCount).toBe(1);
    });
  });

  describe('clear', () => {
    it('should clear all triples and entities', async () => {
      await store.add([
        createTriple('t1', 'user:1', 'knows', 'user:2'),
        createTriple('t2', 'user:2', 'knows', 'user:3'),
      ]);

      await store.clear();

      const stats = await store.getStats();
      expect(stats.tripleCount).toBe(0);
      expect(stats.entityCount).toBe(0);
    });
  });

  describe('query', () => {
    beforeEach(async () => {
      await store.add([
        createTriple('t1', 'user:1', 'knows', 'user:2', 0.9),
        createTriple('t2', 'user:1', 'likes', 'user:3', 0.8),
        createTriple('t3', 'user:2', 'knows', 'user:3', 0.7),
      ]);
    });

    it('should query by subject', async () => {
      const results = await store.query({ subject: 'user:1' });
      expect(results.length).toBe(2);
    });

    it('should query by predicate', async () => {
      const results = await store.query({ predicate: 'knows' });
      expect(results.length).toBe(2);
    });

    it('should query by object', async () => {
      const results = await store.query({ object: 'user:3' });
      expect(results.length).toBe(2);
    });

    it('should query with multiple conditions', async () => {
      const results = await store.query({
        subject: 'user:1',
        predicate: 'knows',
      });
      expect(results.length).toBe(1);
      expect(results[0]['@id']).toBe('t1');
    });

    it('should filter by minConfidence', async () => {
      const results = await store.query({ minConfidence: 0.85 });
      expect(results.length).toBe(1);
      expect(results[0].confidence).toBeGreaterThanOrEqual(0.85);
    });

    it('should filter by source', async () => {
      const results = await store.query({
        source: { sessionId: 'session1' },
      });
      expect(results.length).toBe(3);
    });

    it('should respect limit and offset', async () => {
      const results = await store.query({ limit: 2, offset: 1 });
      expect(results.length).toBe(2);
    });

    it('should sort by confidence descending', async () => {
      const results = await store.query({});
      for (let i = 1; i < results.length; i++) {
        expect(results[i - 1].confidence).toBeGreaterThanOrEqual(results[i].confidence);
      }
    });
  });

  describe('findBySubject/Predicate/Object', () => {
    beforeEach(async () => {
      await store.add([
        createTriple('t1', 'user:1', 'knows', 'user:2'),
        createTriple('t2', 'user:1', 'likes', 'user:3'),
      ]);
    });

    it('should find by subject', async () => {
      const results = await store.findBySubject('user:1');
      expect(results.length).toBe(2);
    });

    it('should find by predicate', async () => {
      const results = await store.findByPredicate('knows');
      expect(results.length).toBe(1);
    });

    it('should find by object', async () => {
      const results = await store.findByObject('user:2');
      expect(results.length).toBe(1);
    });
  });

  describe('entities', () => {
    it('should auto-create entities from triples', async () => {
      await store.add([createTriple('t1', 'user:1', 'knows', 'user:2')]);

      const entity1 = await store.getEntity('user:1');
      const entity2 = await store.getEntity('user:2');

      expect(entity1).not.toBeNull();
      expect(entity2).not.toBeNull();
    });

    it('should upsert entity', async () => {
      await store.upsertEntity({
        '@id': 'user:1',
        '@type': 'codeflow:User',
        label: 'User One',
        createdAt: Date.now(),
        updatedAt: Date.now(),
      });

      const entity = await store.getEntity('user:1');
      expect(entity?.label).toBe('User One');
    });

    it('should update existing entity on upsert', async () => {
      const now = Date.now();
      await store.upsertEntity({
        '@id': 'user:1',
        '@type': 'codeflow:User',
        label: 'Original',
        createdAt: now,
        updatedAt: now,
      });

      await store.upsertEntity({
        '@id': 'user:1',
        '@type': 'codeflow:User',
        label: 'Updated',
        createdAt: now,
        updatedAt: now + 1000,
      });

      const entity = await store.getEntity('user:1');
      expect(entity?.label).toBe('Updated');
    });

    it('should get all entities', async () => {
      await store.add([
        createTriple('t1', 'user:1', 'knows', 'user:2'),
        createTriple('t2', 'user:2', 'knows', 'user:3'),
      ]);

      const entities = await store.getEntities();
      expect(entities.length).toBe(3);
    });
  });

  describe('deduplication', () => {
    it('should deduplicate triples with same S-P-O', async () => {
      const store = new InMemoryTripleStore({ enableDeduplication: true });

      await store.add([
        createTriple('t1', 'user:1', 'knows', 'user:2', 0.5),
        createTriple('t2', 'user:1', 'knows', 'user:2', 0.9),
      ]);

      const stats = await store.getStats();
      expect(stats.tripleCount).toBe(1);

      // Should keep higher confidence
      const results = await store.query({});
      expect(results[0].confidence).toBe(0.9);
    });

    it('should deduplicate existing triples', async () => {
      const store = new InMemoryTripleStore({ enableDeduplication: false });

      await store.add([
        createTriple('t1', 'user:1', 'knows', 'user:2', 0.5),
        createTriple('t2', 'user:1', 'knows', 'user:2', 0.9),
      ]);

      const removed = await store.deduplicate();
      expect(removed).toBe(1);

      const stats = await store.getStats();
      expect(stats.tripleCount).toBe(1);
    });
  });

  describe('literal values', () => {
    it('should handle literal object values', async () => {
      const triple = createLiteralTriple('t1', 'user:1', 'name', 'John Doe');
      await store.add([triple]);

      const result = await store.get('t1');
      expect(result).not.toBeNull();
      expect((result?.object as LiteralValue)['@value']).toBe('John Doe');
    });
  });

  describe('exportGraph and importGraph', () => {
    it('should export graph as JSON-LD', async () => {
      await store.add([
        createTriple('t1', 'user:1', 'knows', 'user:2'),
      ]);

      const graph = await store.exportGraph();

      expect(graph['@context']).toBeDefined();
      expect(graph['@graph'].length).toBe(1);
      expect(graph.metadata.tripleCount).toBe(1);
    });

    it('should import graph from JSON-LD', async () => {
      const graph = await store.exportGraph();
      graph['@graph'] = [createTriple('t1', 'user:1', 'knows', 'user:2')];

      const newStore = new InMemoryTripleStore();
      await newStore.importGraph(graph);

      const stats = await newStore.getStats();
      expect(stats.tripleCount).toBe(1);
    });
  });

  describe('getStats', () => {
    it('should return accurate statistics', async () => {
      await store.add([
        createTriple('t1', 'user:1', 'knows', 'user:2'),
        createTriple('t2', 'user:1', 'likes', 'user:3'),
      ]);

      const stats = await store.getStats();

      expect(stats.tripleCount).toBe(2);
      expect(stats.entityCount).toBe(3);
      expect(stats.predicateCount).toBe(2);
      expect(stats.version).toBe('1.0.0');
    });
  });

  describe('maxTriples limit', () => {
    it('should throw error when max triples exceeded', async () => {
      const limitedStore = new InMemoryTripleStore({ maxTriples: 2 });

      await limitedStore.add([
        createTriple('t1', 'user:1', 'knows', 'user:2'),
        createTriple('t2', 'user:2', 'knows', 'user:3'),
      ]);

      await expect(
        limitedStore.add([createTriple('t3', 'user:3', 'knows', 'user:4')])
      ).rejects.toThrow('Max triples limit reached');
    });
  });
});
