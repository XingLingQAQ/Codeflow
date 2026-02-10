import { describe, it, expect, vi, beforeEach } from 'vitest';
import { FolderMemoryService } from '../FolderMemoryService.js';
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

describe('FolderMemoryService', () => {
  let mockMemoryService: ReturnType<typeof createMockMemoryService>;
  let folderService: FolderMemoryService;

  beforeEach(() => {
    vi.restoreAllMocks();
    mockMemoryService = createMockMemoryService();
    folderService = new FolderMemoryService(mockMemoryService as never);
  });

  describe('addToFolder', () => {
    it('should call memoryService.add with correct folderId', async () => {
      await folderService.addToFolder('folder-1', {
        content: '测试记忆内容',
        tags: ['test'],
        sessionId: 'session-1',
        source: 'user',
        importance: 0.8,
      });

      expect(mockMemoryService.add).toHaveBeenCalledTimes(1);
      const addedMemory = mockMemoryService.add.mock.calls[0][0] as AtomicMemory;
      expect(addedMemory.folderId).toBe('folder-1');
      expect(addedMemory.content).toBe('测试记忆内容');
      expect(addedMemory.sessionId).toBe('session-1');
      expect(addedMemory.source).toBe('user');
      expect(addedMemory.importance).toBe(0.8);
      expect(addedMemory.tags).toEqual(['test']);
      expect(addedMemory.id).toBeTruthy();
      expect(addedMemory.timestamp).toBeGreaterThan(0);
    });

    it('should throw when folderId is empty', async () => {
      await expect(
        folderService.addToFolder('', {
          content: '内容',
          sessionId: 's-1',
          source: 'user',
          importance: 0.5,
        })
      ).rejects.toThrow('folderId is required');
    });
  });

  describe('searchInFolder', () => {
    it('should pass folderId filter to memoryService.search', async () => {
      const mockResults: AtomicMemory[] = [
        {
          id: 'm-1',
          timestamp: 1000,
          content: '文件夹内记忆',
          tags: ['test'],
          sessionId: 's-1',
          folderId: 'folder-1',
          source: 'user',
          importance: 0.7,
        },
      ];
      mockMemoryService.search.mockResolvedValue(mockResults);

      const results = await folderService.searchInFolder('folder-1', '记忆');

      expect(mockMemoryService.search).toHaveBeenCalledWith('记忆', {
        folderId: 'folder-1',
        limit: undefined,
        offset: undefined,
        tags: undefined,
        startAt: undefined,
        endAt: undefined,
      });
      expect(results).toHaveLength(1);
      expect(results[0].folderId).toBe('folder-1');
    });

    it('should filter out results from other folders', async () => {
      const mockResults: AtomicMemory[] = [
        {
          id: 'm-1',
          timestamp: 1000,
          content: '文件夹1记忆',
          tags: [],
          sessionId: 's-1',
          folderId: 'folder-1',
          source: 'user',
          importance: 0.7,
        },
        {
          id: 'm-2',
          timestamp: 1001,
          content: '文件夹2记忆',
          tags: [],
          sessionId: 's-1',
          folderId: 'folder-2',
          source: 'user',
          importance: 0.6,
        },
      ];
      mockMemoryService.search.mockResolvedValue(mockResults);

      const results = await folderService.searchInFolder('folder-1', '记忆');
      expect(results).toHaveLength(1);
      expect(results[0].id).toBe('m-1');
    });
  });

  describe('searchAcrossFolders', () => {
    it('should search without folderId filter', async () => {
      const mockResults: AtomicMemory[] = [
        {
          id: 'm-1',
          timestamp: 1000,
          content: '记忆A',
          tags: [],
          sessionId: 's-1',
          folderId: 'folder-1',
          source: 'user',
          importance: 0.7,
        },
        {
          id: 'm-2',
          timestamp: 1001,
          content: '记忆B',
          tags: [],
          sessionId: 's-1',
          folderId: 'folder-2',
          source: 'user',
          importance: 0.6,
        },
      ];
      mockMemoryService.search.mockResolvedValue(mockResults);

      const results = await folderService.searchAcrossFolders('记忆');

      expect(mockMemoryService.search).toHaveBeenCalledWith('记忆', {
        limit: undefined,
        offset: undefined,
        tags: undefined,
        startAt: undefined,
        endAt: undefined,
      });
      expect(results).toHaveLength(2);
    });

    it('should filter by folderIds when provided', async () => {
      const mockResults: AtomicMemory[] = [
        {
          id: 'm-1',
          timestamp: 1000,
          content: '记忆A',
          tags: [],
          sessionId: 's-1',
          folderId: 'folder-1',
          source: 'user',
          importance: 0.7,
        },
        {
          id: 'm-2',
          timestamp: 1001,
          content: '记忆B',
          tags: [],
          sessionId: 's-1',
          folderId: 'folder-2',
          source: 'user',
          importance: 0.6,
        },
        {
          id: 'm-3',
          timestamp: 1002,
          content: '记忆C',
          tags: [],
          sessionId: 's-1',
          folderId: 'folder-3',
          source: 'user',
          importance: 0.5,
        },
      ];
      mockMemoryService.search.mockResolvedValue(mockResults);

      const results = await folderService.searchAcrossFolders('记忆', {
        folderIds: ['folder-1', 'folder-3'],
      });

      expect(results).toHaveLength(2);
      expect(results.map((r) => r.id)).toEqual(['m-1', 'm-3']);
    });
  });

  describe('folder isolation', () => {
    it('should isolate memories between folders', async () => {
      await folderService.addToFolder('folder-A', {
        content: '文件夹A的记忆',
        sessionId: 's-1',
        source: 'user',
        importance: 0.8,
      });

      await folderService.addToFolder('folder-B', {
        content: '文件夹B的记忆',
        sessionId: 's-1',
        source: 'user',
        importance: 0.7,
      });

      expect(mockMemoryService.add).toHaveBeenCalledTimes(2);

      const callA = mockMemoryService.add.mock.calls[0][0] as AtomicMemory;
      const callB = mockMemoryService.add.mock.calls[1][0] as AtomicMemory;

      expect(callA.folderId).toBe('folder-A');
      expect(callB.folderId).toBe('folder-B');
      expect(callA.folderId).not.toBe(callB.folderId);
    });
  });

  describe('listFolders', () => {
    it('should list folders from session memories', async () => {
      mockMemoryService.getBySession.mockResolvedValue([
        { id: 'm-1', timestamp: 100, folderId: 'folder-1', sessionId: 's-1', content: 'a', tags: [], source: 'user', importance: 0.5 },
        { id: 'm-2', timestamp: 200, folderId: 'folder-1', sessionId: 's-1', content: 'b', tags: [], source: 'user', importance: 0.5 },
        { id: 'm-3', timestamp: 300, folderId: 'folder-2', sessionId: 's-1', content: 'c', tags: [], source: 'user', importance: 0.5 },
      ]);

      const folders = await folderService.listFolders('s-1');

      expect(folders).toHaveLength(2);
      expect(folders[0].folderId).toBe('folder-2');
      expect(folders[0].memoryCount).toBe(1);
      expect(folders[1].folderId).toBe('folder-1');
      expect(folders[1].memoryCount).toBe(2);
    });
  });

  describe('deleteFolder', () => {
    it('should delete all memories in a folder', async () => {
      mockMemoryService.getBySession.mockResolvedValue([
        { id: 'm-1', timestamp: 100, folderId: 'folder-1', sessionId: 's-1', content: 'a', tags: [], source: 'user', importance: 0.5 },
        { id: 'm-2', timestamp: 200, folderId: 'folder-1', sessionId: 's-1', content: 'b', tags: [], source: 'user', importance: 0.5 },
      ]);

      const deleted = await folderService.deleteFolder('folder-1', 's-1');

      expect(deleted).toBe(2);
      expect(mockMemoryService.delete).toHaveBeenCalledTimes(2);
      expect(mockMemoryService.delete).toHaveBeenCalledWith('m-1');
      expect(mockMemoryService.delete).toHaveBeenCalledWith('m-2');
    });
  });
});
