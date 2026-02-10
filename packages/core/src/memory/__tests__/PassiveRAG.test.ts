import { describe, it, expect, vi, beforeEach } from 'vitest';
import { PassiveRAGService } from '../PassiveRAG.js';
import { MemoryInjectionHook } from '../../hooks/MemoryInjectionHook.js';
import { HookManager } from '../../hooks/HookManager.js';
import type { AtomicMemory } from '../types.js';

function createMockMemoryService() {
  return {
    add: vi.fn().mockResolvedValue(undefined),
    search: vi.fn().mockResolvedValue([]),
    searchByTimeRange: vi.fn().mockResolvedValue([]),
    searchByTags: vi.fn().mockResolvedValue([]),
    update: vi.fn().mockResolvedValue(undefined),
    delete: vi.fn().mockResolvedValue(undefined),
    getBySession: vi.fn().mockResolvedValue([]),
    create: vi.fn().mockResolvedValue(undefined),
  };
}

const now = Math.floor(Date.now() / 1000);

const sampleMemories: AtomicMemory[] = [
  {
    id: 'm-1',
    timestamp: now - 100,
    content: '用户偏好使用 TypeScript',
    tags: ['preference', 'language'],
    sessionId: 's-1',
    source: 'user',
    importance: 0.9,
  },
  {
    id: 'm-2',
    timestamp: now - 3600,
    content: '项目使用 monorepo 结构',
    tags: ['architecture'],
    sessionId: 's-1',
    source: 'assistant',
    importance: 0.7,
  },
  {
    id: 'm-3',
    timestamp: now - 86400,
    content: '数据库选择了 SQLite',
    tags: ['decision', 'database'],
    sessionId: 's-1',
    source: 'user',
    importance: 0.8,
  },
];

describe('PassiveRAGService', () => {
  let mockMemoryService: ReturnType<typeof createMockMemoryService>;
  let ragService: PassiveRAGService;

  beforeEach(() => {
    vi.restoreAllMocks();
    mockMemoryService = createMockMemoryService();
    ragService = new PassiveRAGService(mockMemoryService as never, {
      topN: 5,
      threshold: 0.1,
      timeLocalityWeight: 0.3,
      semanticWeight: 0.7,
    });
  });

  describe('retrieve', () => {
    it('should return ranked memories', async () => {
      mockMemoryService.search.mockResolvedValue(sampleMemories);

      const results = await ragService.retrieve('TypeScript 偏好', 's-1');

      expect(results.length).toBeGreaterThan(0);
      expect(results[0].memory).toBeDefined();
      expect(results[0].score).toBeGreaterThan(0);
      expect(results[0].timeScore).toBeGreaterThanOrEqual(0);
      expect(results[0].semanticScore).toBeGreaterThanOrEqual(0);

      for (let i = 1; i < results.length; i++) {
        expect(results[i - 1].score).toBeGreaterThanOrEqual(results[i].score);
      }
    });

    it('should return empty array for empty query', async () => {
      const results = await ragService.retrieve('');
      expect(results).toEqual([]);
      expect(mockMemoryService.search).not.toHaveBeenCalled();
    });

    it('should return empty array when no search results', async () => {
      mockMemoryService.search.mockResolvedValue([]);
      const results = await ragService.retrieve('不存在的内容');
      expect(results).toEqual([]);
    });

    it('should respect topN limit', async () => {
      const manyMemories = Array.from({ length: 20 }, (_, i) => ({
        id: `m-${i}`,
        timestamp: now - i * 100,
        content: `记忆内容 ${i}`,
        tags: [],
        sessionId: 's-1',
        source: 'user' as const,
        importance: 0.5,
      }));
      mockMemoryService.search.mockResolvedValue(manyMemories);

      const ragWithSmallN = new PassiveRAGService(mockMemoryService as never, {
        topN: 3,
        threshold: 0,
      });
      const results = await ragWithSmallN.retrieve('记忆');
      expect(results.length).toBeLessThanOrEqual(3);
    });
  });

  describe('formatForInjection', () => {
    it('should produce readable context string', async () => {
      mockMemoryService.search.mockResolvedValue(sampleMemories);
      const results = await ragService.retrieve('TypeScript');

      const formatted = ragService.formatForInjection(results);

      expect(formatted).toContain('[相关记忆上下文]');
      expect(formatted).toContain('1.');
      expect(formatted).toContain('相关度:');
    });

    it('should return empty string for empty memories', () => {
      const formatted = ragService.formatForInjection([]);
      expect(formatted).toBe('');
    });

    it('should include tags in formatted output', async () => {
      mockMemoryService.search.mockResolvedValue([sampleMemories[0]]);
      const results = await ragService.retrieve('TypeScript');

      const formatted = ragService.formatForInjection(results);
      expect(formatted).toContain('preference');
      expect(formatted).toContain('language');
    });
  });
});

describe('MemoryInjectionHook', () => {
  let mockMemoryService: ReturnType<typeof createMockMemoryService>;
  let ragService: PassiveRAGService;
  let hookManager: HookManager;
  let hook: MemoryInjectionHook;

  beforeEach(() => {
    vi.restoreAllMocks();
    mockMemoryService = createMockMemoryService();
    ragService = new PassiveRAGService(mockMemoryService as never, {
      topN: 3,
      threshold: 0,
    });
    hookManager = new HookManager();
    hook = new MemoryInjectionHook(hookManager, ragService);
  });

  it('should inject memories into payload on before_send', async () => {
    mockMemoryService.search.mockResolvedValue(sampleMemories);
    hook.register();

    const payload = {
      messages: [{ role: 'user' as const, content: 'TypeScript 项目结构' }],
    };

    const result = await hookManager.hook_before_send(payload);

    expect(result.messages.length).toBeGreaterThan(1);
    const injected = result.messages.find(
      (m) => m.role === 'system' && m.content.includes('[相关记忆上下文]')
    );
    expect(injected).toBeDefined();
  });

  it('should not inject when disabled', async () => {
    mockMemoryService.search.mockResolvedValue(sampleMemories);
    hook.register();
    hook.disable();

    const payload = {
      messages: [{ role: 'user' as const, content: 'TypeScript' }],
    };

    const result = await hookManager.hook_before_send(payload);
    expect(result.messages.length).toBe(1);
  });

  it('should support enable/disable toggle', () => {
    expect(hook.isEnabled()).toBe(true);
    hook.disable();
    expect(hook.isEnabled()).toBe(false);
    hook.enable();
    expect(hook.isEnabled()).toBe(true);
  });

  it('should not inject when no user message found', async () => {
    hook.register();

    const payload = {
      messages: [{ role: 'system' as const, content: '你是助手' }],
    };

    const result = await hookManager.hook_before_send(payload);
    expect(result.messages.length).toBe(1);
  });

  it('should not inject when no memories found', async () => {
    mockMemoryService.search.mockResolvedValue([]);
    hook.register();

    const payload = {
      messages: [{ role: 'user' as const, content: '你好' }],
    };

    const result = await hookManager.hook_before_send(payload);
    expect(result.messages.length).toBe(1);
  });

  it('should unregister correctly', async () => {
    mockMemoryService.search.mockResolvedValue(sampleMemories);
    hook.register();
    hook.unregister();

    const payload = {
      messages: [{ role: 'user' as const, content: 'TypeScript' }],
    };

    const result = await hookManager.hook_before_send(payload);
    expect(result.messages.length).toBe(1);
  });
});
