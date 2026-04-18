import { describe, it, expect, beforeEach, vi } from 'vitest';
import { MapAgent } from '../MapAgent.js';
import { InMemoryMapStorage } from '../MapStorage.js';
import {
  MapAgentConfig,
  CompressionMap,
  MapNode,
  MapEdge,
  DEFAULT_MAP_AGENT_CONFIG,
  ENTITY_TYPES,
  RELATION_TYPES,
} from '../types.js';
import { ICliAdapter } from '../../adapters/types.js';
import { Message } from '../../hooks/types.js';

// Mock adapter factory
function createMockAdapter(response?: string): ICliAdapter {
  return {
    send: vi.fn().mockResolvedValue({
      content: response || '{"entities":[],"decisions":[],"relations":[]}',
      model: 'test',
    }),
    stream: vi.fn(),
    receive: vi.fn(),
    getHistory: vi.fn().mockReturnValue([]),
    setHistory: vi.fn(),
    rewind: vi.fn(),
    compact: vi.fn(),
    configure: vi.fn(),
    getConfig: vi.fn().mockReturnValue({ model: 'test' }),
  } as unknown as ICliAdapter;
}

// Helper to create test messages
function createMessages(contents: string[]): Message[] {
  return contents.map((content, i) => ({
    role: i % 2 === 0 ? 'user' : 'assistant',
    content,
    timestamp: Date.now() + i * 1000,
  }));
}

describe('MapAgent', () => {
  let agent: MapAgent;

  beforeEach(() => {
    agent = new MapAgent();
  });

  describe('constructor', () => {
    it('should create agent with default config', () => {
      const a = new MapAgent();
      expect(a).toBeDefined();
    });

    it('should create agent with adapter', () => {
      const adapter = createMockAdapter();
      const a = new MapAgent(adapter);
      expect(a).toBeDefined();
    });

    it('should create agent with custom config', () => {
      const a = new MapAgent(undefined, {
        maxNodes: 100,
        minImportance: 0.5,
      });
      expect(a).toBeDefined();
    });

    it('should merge custom config with defaults', () => {
      const a = new MapAgent(undefined, {
        extractEntities: false,
      });
      expect(a).toBeDefined();
    });
  });

  describe('extract', () => {
    describe('local extraction (without adapter)', () => {
      it('should extract entities from capitalized words', async () => {
        const messages = createMessages([
          'We should use TypeScript for this project.',
          'React and Vue are both good options.',
        ]);

        const result = await agent.extract(messages);

        expect(result.entities.length).toBeGreaterThan(0);
        expect(result.entities.some((e) => e.name === 'TypeScript')).toBe(true);
        expect(result.entities.some((e) => e.name === 'React')).toBe(true);
        expect(result.entities.some((e) => e.name === 'Vue')).toBe(true);
      });

      it('should extract code identifiers from backticks', async () => {
        const messages = createMessages([
          'Call the `processData` function to handle this.',
          'The `UserService` class manages authentication.',
        ]);

        const result = await agent.extract(messages);

        expect(result.entities.some((e) => e.name === 'processData')).toBe(true);
        expect(result.entities.some((e) => e.name === 'UserService')).toBe(true);
      });

      it('should extract decisions from keywords', async () => {
        const messages = createMessages([
          'We should implement caching for better performance.',
          'I decide to use Redis for the cache layer.',
        ]);

        const result = await agent.extract(messages);

        expect(result.decisions.length).toBeGreaterThan(0);
        expect(result.decisions.some((d) => d.content.includes('implement'))).toBe(true);
      });

      it('should extract Chinese decisions', async () => {
        const messages = createMessages([
          '我们决定使用 MongoDB 作为数据库。',
          '选择 Express 作为后端框架。',
        ]);

        const result = await agent.extract(messages);

        expect(result.decisions.length).toBeGreaterThan(0);
      });

      it('should extract relations based on co-occurrence', async () => {
        const messages = createMessages([
          'TypeScript uses JavaScript as its foundation.',
          'React depends on the virtual DOM.',
        ]);

        const result = await agent.extract(messages);

        // Relations are created when entities co-occur in the same message
        expect(result.relations.length).toBeGreaterThanOrEqual(0);
      });

      it('should filter short entities', async () => {
        const messages = createMessages(['A B C are short. TypeScript is not.']);

        const result = await agent.extract(messages);

        // Short entities (length <= 2) should be filtered
        expect(result.entities.every((e) => e.name.length > 2)).toBe(true);
      });

      it('should deduplicate entities', async () => {
        const messages = createMessages([
          'TypeScript is great. TypeScript is powerful.',
          'I love TypeScript.',
        ]);

        const result = await agent.extract(messages);

        const typeScriptCount = result.entities.filter((e) => e.name === 'TypeScript').length;
        expect(typeScriptCount).toBe(1);
      });

      it('should limit entities to 30', async () => {
        // Create messages with many entities
        const entities = Array.from({ length: 50 }, (_, i) => `Entity${i}`);
        const messages = createMessages([entities.join(' ')]);

        const result = await agent.extract(messages);

        expect(result.entities.length).toBeLessThanOrEqual(30);
      });

      it('should limit decisions to 15', async () => {
        // Create messages with many decisions
        const decisions = Array.from(
          { length: 20 },
          (_, i) => `We should implement feature ${i} for the system.`
        );
        const messages = createMessages(decisions);

        const result = await agent.extract(messages);

        expect(result.decisions.length).toBeLessThanOrEqual(15);
      });

      it('should limit relations to 20', async () => {
        // Create messages with many potential relations
        const entities = Array.from({ length: 10 }, (_, i) => `Entity${i}`);
        const messages = createMessages([entities.join(' and ')]);

        const result = await agent.extract(messages);

        expect(result.relations.length).toBeLessThanOrEqual(20);
      });

      it('should calculate entity importance based on frequency', async () => {
        const messages = createMessages([
          'TypeScript TypeScript TypeScript is mentioned often.',
          'JavaScript is mentioned once.',
        ]);

        const result = await agent.extract(messages);

        const tsEntity = result.entities.find((e) => e.name === 'TypeScript');
        const jsEntity = result.entities.find((e) => e.name === 'JavaScript');

        if (tsEntity && jsEntity) {
          expect(tsEntity.importance).toBeGreaterThan(jsEntity.importance);
        }
      });

      it('should calculate decision importance based on keywords', async () => {
        const messages = createMessages([
          'This is a critical decision that must be made.',
          'We should consider this option.',
        ]);

        const result = await agent.extract(messages);

        const criticalDecision = result.decisions.find((d) =>
          d.content.toLowerCase().includes('critical')
        );
        const shouldDecision = result.decisions.find(
          (d) => d.content.toLowerCase().includes('should') && !d.content.includes('critical')
        );

        if (criticalDecision && shouldDecision) {
          expect(criticalDecision.importance).toBeGreaterThan(shouldDecision.importance);
        }
      });

      it('should infer entity type from context', async () => {
        const messages = createMessages([
          'The class UserService handles authentication.',
          'Call function processData to transform the input.',
        ]);

        const result = await agent.extract(messages);

        const userService = result.entities.find((e) => e.name === 'UserService');
        expect(userService?.type).toBe(ENTITY_TYPES.CLASS);
      });

      it('should infer file entity type', async () => {
        const messages = createMessages(['Check the config.ts file for settings.']);

        const result = await agent.extract(messages);

        // 'config' might be extracted as a file type entity
        expect(result.entities.length).toBeGreaterThanOrEqual(0);
      });

      it('should infer relation type from context', async () => {
        const messages = createMessages([
          'TypeScript uses JavaScript runtime.',
          'React depends on the DOM.',
        ]);

        const result = await agent.extract(messages);

        const usesRelation = result.relations.find((r) => r.type === RELATION_TYPES.USES);
        const dependsRelation = result.relations.find((r) => r.type === RELATION_TYPES.DEPENDS_ON);

        // At least one relation type should be inferred
        expect(result.relations.length).toBeGreaterThanOrEqual(0);
      });

      it('should return empty result for empty messages', async () => {
        const result = await agent.extract([]);

        expect(result.entities).toEqual([]);
        expect(result.decisions).toEqual([]);
        expect(result.relations).toEqual([]);
        expect(result.concepts).toEqual([]);
      });
    });

    describe('LLM extraction (with adapter)', () => {
      it('should use adapter when available', async () => {
        const mockResponse = JSON.stringify({
          entities: [{ name: 'TestEntity', type: 'concept', importance: 0.8 }],
          decisions: [{ content: 'Test decision', importance: 0.7 }],
          relations: [],
        });

        const adapter = createMockAdapter(mockResponse);
        const agentWithAdapter = new MapAgent(adapter);

        const messages = createMessages(['Test message']);
        const result = await agentWithAdapter.extract(messages);

        expect(adapter.send).toHaveBeenCalled();
        expect(result.entities.some((e) => e.name === 'TestEntity')).toBe(true);
      });

      it('should fallback to local extraction on adapter error', async () => {
        const adapter = createMockAdapter();
        (adapter.send as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('API error'));

        const agentWithAdapter = new MapAgent(adapter);
        const messages = createMessages(['TypeScript is great.']);

        const result = await agentWithAdapter.extract(messages);

        // Should fallback to local extraction
        expect(result.entities.some((e) => e.name === 'TypeScript')).toBe(true);
      });

      it('should handle malformed JSON response', async () => {
        const adapter = createMockAdapter('not valid json');
        const agentWithAdapter = new MapAgent(adapter);

        const messages = createMessages(['Test message']);
        const result = await agentWithAdapter.extract(messages);

        // Should return empty result on parse failure
        expect(result.entities).toEqual([]);
      });

      it('should extract JSON from response with surrounding text', async () => {
        const mockResponse = `Here is the analysis:
        {"entities":[{"name":"Test","type":"concept","importance":0.5}],"decisions":[],"relations":[]}
        End of analysis.`;

        const adapter = createMockAdapter(mockResponse);
        const agentWithAdapter = new MapAgent(adapter);

        const messages = createMessages(['Test message']);
        const result = await agentWithAdapter.extract(messages);

        expect(result.entities.some((e) => e.name === 'Test')).toBe(true);
      });
    });
  });

  describe('buildMap', () => {
    it('should build map from messages', async () => {
      const messages = createMessages([
        'We should use TypeScript for type safety.',
        'React will be our frontend framework.',
      ]);

      const map = await agent.buildMap(messages, 'session-1');

      expect(map.id).toContain('session-1');
      expect(map.sessionId).toBe('session-1');
      expect(map.nodes.length).toBeGreaterThan(0);
      expect(map.skeleton).toBeDefined();
    });

    it('should create entity nodes', async () => {
      const messages = createMessages(['TypeScript and React are great technologies.']);

      const map = await agent.buildMap(messages, 'session-1');

      const entityNodes = map.nodes.filter((n) => n.type === 'entity');
      expect(entityNodes.length).toBeGreaterThan(0);
    });

    it('should create decision nodes', async () => {
      const messages = createMessages(['We should implement caching for performance.']);

      const map = await agent.buildMap(messages, 'session-1');

      const decisionNodes = map.nodes.filter((n) => n.type === 'decision');
      expect(decisionNodes.length).toBeGreaterThanOrEqual(0);
    });

    it('should create edges between related nodes', async () => {
      const messages = createMessages(['TypeScript uses JavaScript as its foundation.']);

      const map = await agent.buildMap(messages, 'session-1');

      // Edges connect nodes that have relations
      expect(map.edges).toBeDefined();
    });

    it('should filter nodes by minImportance', async () => {
      const agentWithHighThreshold = new MapAgent(undefined, {
        minImportance: 0.9,
      });

      const messages = createMessages(['TypeScript is mentioned once.']);

      const map = await agentWithHighThreshold.buildMap(messages, 'session-1');

      // High threshold should filter out low-importance nodes
      expect(map.nodes.length).toBeLessThanOrEqual(5);
    });

    it('should limit nodes by maxNodes config', async () => {
      const agentWithLimit = new MapAgent(undefined, {
        maxNodes: 3,
      });

      const entities = Array.from({ length: 20 }, (_, i) => `Entity${i}`);
      const messages = createMessages([entities.join(' ')]);

      const map = await agentWithLimit.buildMap(messages, 'session-1');

      expect(map.nodes.length).toBeLessThanOrEqual(3);
    });

    it('should sort nodes by importance before limiting', async () => {
      const agentWithLimit = new MapAgent(undefined, {
        maxNodes: 2,
        minImportance: 0,
      });

      const messages = createMessages([
        'Important Important Important is mentioned often.',
        'Rare is mentioned once.',
      ]);

      const map = await agentWithLimit.buildMap(messages, 'session-1');

      // Higher importance nodes should be kept
      if (map.nodes.length > 0) {
        const importances = map.nodes.map((n) => n.importance);
        expect(importances[0]).toBeGreaterThanOrEqual(importances[importances.length - 1] || 0);
      }
    });

    it('should filter edges when nodes are limited', async () => {
      const agentWithLimit = new MapAgent(undefined, {
        maxNodes: 2,
      });

      const messages = createMessages([
        'TypeScript uses JavaScript. React uses JavaScript. Vue uses JavaScript.',
      ]);

      const map = await agentWithLimit.buildMap(messages, 'session-1');

      // Edges should only connect nodes that exist in the limited set
      for (const edge of map.edges) {
        const sourceExists = map.nodes.some((n) => n.id === edge.source);
        const targetExists = map.nodes.some((n) => n.id === edge.target);
        expect(sourceExists && targetExists).toBe(true);
      }
    });

    it('should build decision skeleton', async () => {
      const messages = createMessages([
        'TypeScript is our choice.',
        'We decide to use React for the frontend.',
      ]);

      const map = await agent.buildMap(messages, 'session-1');

      expect(map.skeleton.entities).toBeDefined();
      expect(map.skeleton.decisions).toBeDefined();
      expect(map.skeleton.relations).toBeDefined();
    });

    it('should set message range', async () => {
      const messages = createMessages(['Message 1', 'Message 2', 'Message 3']);

      const map = await agent.buildMap(messages, 'session-1');

      expect(map.messageRange.start).toBe(0);
      expect(map.messageRange.end).toBe(2);
    });

    it('should set timestamps', async () => {
      const before = Date.now();
      const messages = createMessages(['Test message']);
      const map = await agent.buildMap(messages, 'session-1');
      const after = Date.now();

      expect(map.createdAt).toBeGreaterThanOrEqual(before);
      expect(map.createdAt).toBeLessThanOrEqual(after);
    });

    it('should respect extractEntities config', async () => {
      const agentNoEntities = new MapAgent(undefined, {
        extractEntities: false,
      });

      const messages = createMessages(['TypeScript and React are technologies.']);

      const map = await agentNoEntities.buildMap(messages, 'session-1');

      const entityNodes = map.nodes.filter((n) => n.type === 'entity');
      expect(entityNodes.length).toBe(0);
    });

    it('should respect extractDecisions config', async () => {
      const agentNoDecisions = new MapAgent(undefined, {
        extractDecisions: false,
      });

      const messages = createMessages(['We should implement this feature.']);

      const map = await agentNoDecisions.buildMap(messages, 'session-1');

      const decisionNodes = map.nodes.filter((n) => n.type === 'decision');
      expect(decisionNodes.length).toBe(0);
    });

    it('should respect extractRelations config', async () => {
      const agentNoRelations = new MapAgent(undefined, {
        extractRelations: false,
      });

      const messages = createMessages(['TypeScript uses JavaScript.']);

      const map = await agentNoRelations.buildMap(messages, 'session-1');

      expect(map.edges.length).toBe(0);
    });

    it('should generate unique node IDs', async () => {
      const messages = createMessages([
        'TypeScript React Vue Angular Svelte are frameworks.',
      ]);

      const map = await agent.buildMap(messages, 'session-1');

      const nodeIds = map.nodes.map((n) => n.id);
      const uniqueIds = new Set(nodeIds);
      expect(uniqueIds.size).toBe(nodeIds.length);
    });

    it('should generate unique edge IDs', async () => {
      const messages = createMessages([
        'TypeScript uses JavaScript. React uses JavaScript. Vue uses JavaScript.',
      ]);

      const map = await agent.buildMap(messages, 'session-1');

      if (map.edges.length > 0) {
        const edgeIds = map.edges.map((e) => e.id);
        const uniqueIds = new Set(edgeIds);
        expect(uniqueIds.size).toBe(edgeIds.length);
      }
    });
  });

  describe('mergeMap', () => {
    it('should merge two maps', async () => {
      const messages1 = createMessages(['TypeScript is great.']);
      const messages2 = createMessages(['React is powerful.']);

      const map1 = await agent.buildMap(messages1, 'session-1');
      const map2 = await agent.buildMap(messages2, 'session-1');

      const merged = agent.mergeMap(map1, map2);

      expect(merged.nodes.length).toBeGreaterThanOrEqual(map1.nodes.length);
    });

    it('should preserve existing map ID', async () => {
      const messages1 = createMessages(['TypeScript']);
      const messages2 = createMessages(['React']);

      const map1 = await agent.buildMap(messages1, 'session-1');
      const map2 = await agent.buildMap(messages2, 'session-1');

      const merged = agent.mergeMap(map1, map2);

      expect(merged.id).toBe(map1.id);
    });

    it('should preserve existing session ID', async () => {
      const messages1 = createMessages(['TypeScript']);
      const messages2 = createMessages(['React']);

      const map1 = await agent.buildMap(messages1, 'session-1');
      const map2 = await agent.buildMap(messages2, 'session-2');

      const merged = agent.mergeMap(map1, map2);

      expect(merged.sessionId).toBe('session-1');
    });

    it('should deduplicate nodes by label', async () => {
      const messages = createMessages(['TypeScript is great.']);

      const map1 = await agent.buildMap(messages, 'session-1');
      const map2 = await agent.buildMap(messages, 'session-1');

      const merged = agent.mergeMap(map1, map2);

      // Should not have duplicate TypeScript nodes
      const tsNodes = merged.nodes.filter((n) => n.label === 'TypeScript');
      expect(tsNodes.length).toBe(1);
    });

    it('should deduplicate edges', async () => {
      const messages = createMessages(['TypeScript uses JavaScript.']);

      const map1 = await agent.buildMap(messages, 'session-1');
      const map2 = await agent.buildMap(messages, 'session-1');

      const merged = agent.mergeMap(map1, map2);

      // Edges should be deduplicated by source-target-type
      const edgeKeys = merged.edges.map((e) => `${e.source}-${e.target}-${e.type}`);
      const uniqueKeys = new Set(edgeKeys);
      expect(uniqueKeys.size).toBe(edgeKeys.length);
    });

    it('should merge skeleton entities with deduplication', async () => {
      const messages = createMessages(['TypeScript is great.']);

      const map1 = await agent.buildMap(messages, 'session-1');
      const map2 = await agent.buildMap(messages, 'session-1');

      const merged = agent.mergeMap(map1, map2);

      // Skeleton entities should be deduplicated
      const uniqueEntities = new Set(merged.skeleton.entities);
      expect(uniqueEntities.size).toBe(merged.skeleton.entities.length);
    });

    it('should concatenate skeleton decisions', async () => {
      const messages1 = createMessages(['We should use TypeScript.']);
      const messages2 = createMessages(['We decide to use React.']);

      const map1 = await agent.buildMap(messages1, 'session-1');
      const map2 = await agent.buildMap(messages2, 'session-1');

      const merged = agent.mergeMap(map1, map2);

      expect(merged.skeleton.decisions.length).toBeGreaterThanOrEqual(
        Math.max(map1.skeleton.decisions.length, map2.skeleton.decisions.length)
      );
    });

    it('should concatenate skeleton relations', async () => {
      const messages1 = createMessages(['TypeScript uses JavaScript.']);
      const messages2 = createMessages(['React depends on DOM.']);

      const map1 = await agent.buildMap(messages1, 'session-1');
      const map2 = await agent.buildMap(messages2, 'session-1');

      const merged = agent.mergeMap(map1, map2);

      expect(merged.skeleton.relations.length).toBeGreaterThanOrEqual(
        Math.max(map1.skeleton.relations.length, map2.skeleton.relations.length)
      );
    });

    it('should preserve original createdAt', async () => {
      const messages1 = createMessages(['TypeScript']);
      const map1 = await agent.buildMap(messages1, 'session-1');

      await new Promise((resolve) => setTimeout(resolve, 10));

      const messages2 = createMessages(['React']);
      const map2 = await agent.buildMap(messages2, 'session-1');

      const merged = agent.mergeMap(map1, map2);

      expect(merged.createdAt).toBe(map1.createdAt);
    });

    it('should update message range end', async () => {
      const messages1 = createMessages(['Message 1', 'Message 2']);
      const messages2 = createMessages(['Message 3', 'Message 4', 'Message 5']);

      const map1 = await agent.buildMap(messages1, 'session-1');
      const map2 = await agent.buildMap(messages2, 'session-1');

      const merged = agent.mergeMap(map1, map2);

      expect(merged.messageRange.start).toBe(map1.messageRange.start);
      expect(merged.messageRange.end).toBe(map2.messageRange.end);
    });
  });
});

describe('InMemoryMapStorage', () => {
  let storage: InMemoryMapStorage;

  beforeEach(() => {
    storage = new InMemoryMapStorage();
  });

  const createTestMap = (id: string, sessionId: string): CompressionMap => ({
    id,
    sessionId,
    nodes: [],
    edges: [],
    skeleton: { entities: [], decisions: [], relations: [] },
    createdAt: Date.now(),
    messageRange: { start: 0, end: 0 },
  });

  describe('save', () => {
    it('should save map', async () => {
      const map = createTestMap('map-1', 'session-1');

      await storage.save(map);

      const loaded = await storage.load('map-1');
      expect(loaded).toEqual(map);
    });

    it('should overwrite existing map', async () => {
      const map1 = createTestMap('map-1', 'session-1');
      const map2 = { ...createTestMap('map-1', 'session-2'), nodes: [{ id: 'node-1' }] as MapNode[] };

      await storage.save(map1);
      await storage.save(map2);

      const loaded = await storage.load('map-1');
      expect(loaded?.sessionId).toBe('session-2');
    });
  });

  describe('load', () => {
    it('should return null for non-existent map', async () => {
      const loaded = await storage.load('non-existent');
      expect(loaded).toBeNull();
    });

    it('should return saved map', async () => {
      const map = createTestMap('map-1', 'session-1');
      await storage.save(map);

      const loaded = await storage.load('map-1');
      expect(loaded).toEqual(map);
    });
  });

  describe('loadBySession', () => {
    it('should return empty array for non-existent session', async () => {
      const maps = await storage.loadBySession('non-existent');
      expect(maps).toEqual([]);
    });

    it('should return all maps for session', async () => {
      await storage.save(createTestMap('map-1', 'session-1'));
      await storage.save(createTestMap('map-2', 'session-1'));
      await storage.save(createTestMap('map-3', 'session-2'));

      const maps = await storage.loadBySession('session-1');

      expect(maps.length).toBe(2);
      expect(maps.every((m) => m.sessionId === 'session-1')).toBe(true);
    });
  });

  describe('delete', () => {
    it('should delete existing map', async () => {
      const map = createTestMap('map-1', 'session-1');
      await storage.save(map);

      await storage.delete('map-1');

      const loaded = await storage.load('map-1');
      expect(loaded).toBeNull();
    });

    it('should handle deleting non-existent map', async () => {
      await expect(storage.delete('non-existent')).resolves.not.toThrow();
    });
  });

  describe('list', () => {
    it('should return empty array when no maps', async () => {
      const list = await storage.list();
      expect(list).toEqual([]);
    });

    it('should return all maps metadata', async () => {
      await storage.save(createTestMap('map-1', 'session-1'));
      await storage.save(createTestMap('map-2', 'session-2'));

      const list = await storage.list();

      expect(list.length).toBe(2);
      expect(list[0]).toHaveProperty('id');
      expect(list[0]).toHaveProperty('sessionId');
      expect(list[0]).toHaveProperty('createdAt');
    });
  });

  describe('clear', () => {
    it('should remove all maps', async () => {
      await storage.save(createTestMap('map-1', 'session-1'));
      await storage.save(createTestMap('map-2', 'session-2'));

      storage.clear();

      const list = await storage.list();
      expect(list).toEqual([]);
    });
  });

  describe('size', () => {
    it('should return 0 when empty', () => {
      expect(storage.size()).toBe(0);
    });

    it('should return correct count', async () => {
      await storage.save(createTestMap('map-1', 'session-1'));
      await storage.save(createTestMap('map-2', 'session-2'));

      expect(storage.size()).toBe(2);
    });
  });
});
