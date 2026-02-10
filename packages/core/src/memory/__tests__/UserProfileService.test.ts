import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

import { AtomicMemoryService } from '../AtomicMemoryService.js';
import {
  UserProfileService,
  InMemoryProfileStorage,
  UserProfile,
  UserProfileSections,
} from '../UserProfileService.js';

vi.mock('../AtomicMemoryService.js', () => {
  return {
    AtomicMemoryService: vi.fn().mockImplementation(() => ({
      add: vi.fn().mockResolvedValue(undefined),
      search: vi.fn().mockResolvedValue([]),
      getBySession: vi.fn().mockResolvedValue([]),
    })),
  };
});

const mockLLMAdapter = {
  send: vi.fn().mockResolvedValue({
    content: JSON.stringify({
      preferences: '偏好 TypeScript',
      background: '全栈开发者',
      expertise: ['TypeScript', 'Go'],
      communicationStyle: '简洁直接',
      goals: ['提升代码质量'],
    }),
    model: 'test',
  }),
};

describe('UserProfileService', () => {
  let memoryService: AtomicMemoryService;
  let storage: InMemoryProfileStorage;
  let service: UserProfileService;

  beforeEach(() => {
    vi.clearAllMocks();
    memoryService = new AtomicMemoryService();
    storage = new InMemoryProfileStorage();
    service = new UserProfileService(mockLLMAdapter, memoryService, storage);
  });

  afterEach(() => {
    service.stopPeriodicUpdate();
  });

  describe('getProfile', () => {
    it('should return null for non-existent user', async () => {
      const profile = await service.getProfile('user-1');
      expect(profile).toBeNull();
    });

    it('should return saved profile', async () => {
      const profile: UserProfile = {
        userId: 'user-1',
        lastUpdated: 1000,
        sections: {
          preferences: 'test',
          background: '',
          expertise: [],
          communicationStyle: '',
          goals: [],
        },
        metadata: { totalSessions: 1, totalMessages: 0, lastActive: 1000 },
      };
      await storage.save(profile);

      const result = await service.getProfile('user-1');
      expect(result).toBeDefined();
      expect(result!.userId).toBe('user-1');
    });
  });

  describe('update', () => {
    it('should create new profile from memories', async () => {
      (memoryService.getBySession as any).mockResolvedValue([
        { id: 'm1', content: '我喜欢 TypeScript', tags: ['preference'], importance: 0.8, timestamp: 1000, sessionId: 's1', source: 'user' },
        { id: 'm2', content: '我是全栈开发者', tags: ['background'], importance: 0.7, timestamp: 1001, sessionId: 's1', source: 'user' },
      ]);

      const profile = await service.update('user-1', 'session-1');

      expect(profile.userId).toBe('user-1');
      expect(profile.sections.preferences).toBe('偏好 TypeScript');
      expect(profile.sections.expertise).toContain('TypeScript');
      expect(profile.metadata.totalSessions).toBe(1);
      expect(mockLLMAdapter.send).toHaveBeenCalled();
    });

    it('should return existing profile when no new memories', async () => {
      const existing: UserProfile = {
        userId: 'user-1',
        lastUpdated: 1000,
        sections: {
          preferences: '已有偏好',
          background: '',
          expertise: [],
          communicationStyle: '',
          goals: [],
        },
        metadata: { totalSessions: 1, totalMessages: 5, lastActive: 1000 },
      };
      await storage.save(existing);

      (memoryService.getBySession as any).mockResolvedValue([]);

      const profile = await service.update('user-1', 'session-1');
      expect(profile.sections.preferences).toBe('已有偏好');
      expect(mockLLMAdapter.send).not.toHaveBeenCalled();
    });

    it('should handle LLM parse failure gracefully', async () => {
      (memoryService.getBySession as any).mockResolvedValue([
        { id: 'm1', content: '记忆', tags: [], importance: 0.5, timestamp: 1000, sessionId: 's1', source: 'user' },
      ]);

      mockLLMAdapter.send.mockResolvedValueOnce({
        content: 'invalid json response',
        model: 'test',
      });

      const profile = await service.update('user-1', 'session-1');
      // Should fallback to empty sections
      expect(profile.userId).toBe('user-1');
      expect(profile.sections.preferences).toBe('');
    });
  });

  describe('InMemoryProfileStorage', () => {
    it('should support CRUD operations', async () => {
      const profile: UserProfile = {
        userId: 'user-1',
        lastUpdated: 1000,
        sections: {
          preferences: 'test',
          background: '',
          expertise: [],
          communicationStyle: '',
          goals: [],
        },
        metadata: { totalSessions: 0, totalMessages: 0, lastActive: 0 },
      };

      await storage.save(profile);
      const loaded = await storage.load('user-1');
      expect(loaded).toBeDefined();
      expect(loaded!.userId).toBe('user-1');

      await storage.delete('user-1');
      const deleted = await storage.load('user-1');
      expect(deleted).toBeNull();
    });
  });

  describe('periodic update', () => {
    it('should start and stop without error', () => {
      service.startPeriodicUpdate('user-1', 'session-1');
      service.stopPeriodicUpdate();
    });
  });
});
