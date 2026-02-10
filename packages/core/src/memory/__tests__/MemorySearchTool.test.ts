import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MemorySearchTool } from '../MemorySearchTool.js';
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

const sampleMemories: AtomicMemory[] = [
  {
    id: 'm-1',
    timestamp: 1000,
    content: '用户偏好 TypeScript',
    tags: ['preference', 'language'],
    sessionId: 's-1',
    source: 'user',
    importance: 0.9,
  },
  {
    id: 'm-2',
    timestamp: 2000,
    content: '选择了 SQLite 作为数据库',
    tags: ['decision', 'database'],
    sessionId: 's-1',
    source: 'assistant',
    importance: 0.8,
  },
  {
    id: 'm-3',
    timestamp: 3000,
    content: '项目使用 monorepo 结构',
    tags: ['architecture'],
    sessionId: 's-2',
    source: 'user',
    importance: 0.7,
  },
];

describe('MemorySearchTool', () => {
  let mockMemoryService: ReturnType<typeof createMockMemoryService>;
  let tool: MemorySearchTool;

  beforeEach(() => {
    vi.restoreAllMocks();
    mockMemoryService = createMockMemoryService();
    tool = new MemorySearchTool(mockMemoryService as never);
  });

  describe('getToolDefinition', () => {
    it('should return valid MCP-compatible schema', () => {
      const definition = tool.getToolDefinition();

      expect(definition.name).toBe('search_memory');
      expect(definition.description).toBeTruthy();
      expect(definition.inputSchema.type).toBe('object');
      expect(definition.inputSchema.required).toContain('query');
      expect(definition.inputSchema.properties.query).toBeDefined();
      expect(definition.inputSchema.properties.timeRange).toBeDefined();
      expect(definition.inputSchema.properties.tags).toBeDefined();
      expect(definition.inputSchema.properties.limit).toBeDefined();
    });

    it('should include sessionId and folderId in properties', () => {
      const definition = tool.getToolDefinition();
      expect(definition.inputSchema.properties.sessionId).toBeDefined();
      expect(definition.inputSchema.properties.folderId).toBeDefined();
    });
  });

  describe('execute with query', () => {
    it('should call memoryService.search with query', async () => {
      mockMemoryService.search.mockResolvedValue([sampleMemories[0]]);

      const result = await tool.execute({ query: 'TypeScript 偏好' });

      expect(mockMemoryService.search).toHaveBeenCalledWith('TypeScript 偏好', expect.objectContaining({
        limit: expect.any(Number),
        offset: 0,
      }));
      expect(result.memories).toHaveLength(1);
      expect(result.memories[0].id).toBe('m-1');
      expect(result.query).toBe('TypeScript 偏好');
    });

    it('should respect limit parameter', async () => {
      mockMemoryService.search.mockResolvedValue(sampleMemories);

      const result = await tool.execute({ query: '记忆', limit: 2 });

      expect(result.memories.length).toBeLessThanOrEqual(2);
    });
  });

  describe('execute with tags filter', () => {
    it('should call searchByTags when only tags provided', async () => {
      mockMemoryService.searchByTags.mockResolvedValue([sampleMemories[1]]);

      const result = await tool.execute({
        query: '',
        tags: ['decision'],
      });

      expect(mockMemoryService.searchByTags).toHaveBeenCalledWith(['decision']);
      expect(result.memories).toHaveLength(1);
      expect(result.memories[0].tags).toContain('decision');
    });

    it('should pass tags to search when query is also provided', async () => {
      mockMemoryService.search.mockResolvedValue([sampleMemories[0]]);

      await tool.execute({
        query: 'TypeScript',
        tags: ['preference'],
      });

      expect(mockMemoryService.search).toHaveBeenCalledWith('TypeScript', expect.objectContaining({
        tags: ['preference'],
      }));
    });
  });

  describe('execute with timeRange filter', () => {
    it('should call searchByTimeRange when timeRange provided', async () => {
      mockMemoryService.searchByTimeRange.mockResolvedValue([sampleMemories[1]]);

      const result = await tool.execute({
        query: '',
        timeRange: { start: 1500, end: 2500 },
      });

      expect(mockMemoryService.searchByTimeRange).toHaveBeenCalledWith(1500, 2500);
      expect(result.memories).toHaveLength(1);
    });

    it('should combine timeRange with semantic search when query provided', async () => {
      mockMemoryService.searchByTimeRange.mockResolvedValue([sampleMemories[1]]);
      mockMemoryService.search.mockResolvedValue([sampleMemories[0]]);

      const result = await tool.execute({
        query: 'TypeScript',
        timeRange: { start: 500, end: 3500 },
      });

      expect(mockMemoryService.searchByTimeRange).toHaveBeenCalled();
      expect(mockMemoryService.search).toHaveBeenCalled();
      expect(result.memories.length).toBeGreaterThanOrEqual(1);
    });

    it('should reject invalid timeRange', async () => {
      await expect(
        tool.execute({
          query: 'test',
          timeRange: { start: 3000, end: 1000 },
        })
      ).rejects.toThrow('timeRange.start must be <= timeRange.end');
    });
  });

  describe('validation', () => {
    it('should throw on invalid params', async () => {
      await expect(tool.execute(null as never)).rejects.toThrow('params is required');
    });

    it('should throw on non-string query', async () => {
      await expect(tool.execute({ query: 123 as never })).rejects.toThrow('query must be a string');
    });
  });
});
