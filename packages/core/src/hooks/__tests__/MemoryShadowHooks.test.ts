import { describe, it, expect, vi, beforeEach } from 'vitest';

import { HookManager } from '../HookManager.js';
import { MemoryShadowHooks } from '../MemoryShadowHooks.js';
import { MemoryExtractor } from '../../memory/MemoryExtractor.js';
import { UserProfileService } from '../../memory/UserProfileService.js';
import { ShadowScaffold } from '../../shadow/ShadowScaffold.js';
import { AtomicMemoryService } from '../../memory/AtomicMemoryService.js';

vi.mock('../../memory/AtomicMemoryService.js', () => ({
  AtomicMemoryService: vi.fn().mockImplementation(() => ({
    add: vi.fn().mockResolvedValue(undefined),
    search: vi.fn().mockResolvedValue([]),
    getBySession: vi.fn().mockResolvedValue([]),
  })),
}));

vi.mock('../../shadow/ShadowScaffold.js', () => ({
  ShadowScaffold: vi.fn().mockImplementation(() => ({
    initialize: vi.fn().mockResolvedValue(undefined),
  })),
}));

const mockLLMAdapter = { send: vi.fn().mockResolvedValue({ content: '{}', model: 'test' }) };

describe('MemoryShadowHooks', () => {
  let hookManager: HookManager;
  let memoryExtractor: MemoryExtractor;
  let profileService: UserProfileService;
  let shadowScaffold: ShadowScaffold;
  let hooks: MemoryShadowHooks;

  beforeEach(() => {
    vi.clearAllMocks();
    hookManager = new HookManager();
    const memoryService = new AtomicMemoryService();
    memoryExtractor = new MemoryExtractor(mockLLMAdapter, memoryService);
    profileService = new UserProfileService(mockLLMAdapter, memoryService);
    shadowScaffold = new ShadowScaffold();

    hooks = new MemoryShadowHooks(
      hookManager,
      {
        userId: 'user-1',
        sessionId: 'session-1',
        projectRoot: '/tmp/test',
        profileUpdateInterval: 3,
      },
      memoryExtractor,
      profileService,
      shadowScaffold
    );
  });

  describe('register', () => {
    it('should register hooks without error', () => {
      hooks.register();
    });
  });

  describe('hook_post_response (memory extraction)', () => {
    it('should trigger memory extraction on post_response', async () => {
      const extractSpy = vi.spyOn(memoryExtractor, 'extractFromConversation');
      hooks.register();

      // Simulate user message first
      await hookManager.hook_on_message_complete({
        role: 'user',
        content: '我喜欢 TypeScript',
      });

      // Then AI response
      await hookManager.hook_post_response({
        content: '好的，我记住了',
        model: 'test',
      });

      expect(extractSpy).toHaveBeenCalledWith(
        '我喜欢 TypeScript',
        '好的，我记住了',
        'session-1'
      );
    });

    it('should not extract when disabled', async () => {
      hooks = new MemoryShadowHooks(
        hookManager,
        { enableMemoryExtraction: false, sessionId: 'session-1' },
        memoryExtractor,
        profileService
      );
      hooks.register();

      const extractSpy = vi.spyOn(memoryExtractor, 'extractFromConversation');

      await hookManager.hook_on_message_complete({
        role: 'user',
        content: '测试',
      });
      await hookManager.hook_post_response({ content: '回复', model: 'test' });

      expect(extractSpy).not.toHaveBeenCalled();
    });
  });

  describe('hook_on_message_complete (profile update)', () => {
    it('should update profile at configured interval', async () => {
      const updateSpy = vi.spyOn(profileService, 'update').mockResolvedValue({
        userId: 'user-1',
        lastUpdated: 1000,
        sections: { preferences: '', background: '', expertise: [], communicationStyle: '', goals: [] },
        metadata: { totalSessions: 1, totalMessages: 3, lastActive: 1000 },
      });

      hooks.register();

      // Send 3 messages (interval = 3)
      for (let i = 0; i < 3; i++) {
        await hookManager.hook_on_message_complete({
          role: 'user',
          content: `消息 ${i}`,
        });
      }

      expect(updateSpy).toHaveBeenCalledTimes(1);
      expect(updateSpy).toHaveBeenCalledWith('user-1', 'session-1');
    });

    it('should not update before interval', async () => {
      const updateSpy = vi.spyOn(profileService, 'update');
      hooks.register();

      // Send only 2 messages (interval = 3)
      for (let i = 0; i < 2; i++) {
        await hookManager.hook_on_message_complete({
          role: 'user',
          content: `消息 ${i}`,
        });
      }

      expect(updateSpy).not.toHaveBeenCalled();
    });
  });

  describe('initializeShadowDirectory', () => {
    it('should call scaffold initialize', async () => {
      await hooks.initializeShadowDirectory();
      expect(shadowScaffold.initialize).toHaveBeenCalledWith('/tmp/test');
    });
  });

  describe('message count', () => {
    it('should track and reset message count', async () => {
      hooks.register();

      await hookManager.hook_on_message_complete({ role: 'user', content: 'a' });
      await hookManager.hook_on_message_complete({ role: 'user', content: 'b' });

      expect(hooks.getMessageCount()).toBe(2);

      hooks.resetMessageCount();
      expect(hooks.getMessageCount()).toBe(0);
    });
  });
});
