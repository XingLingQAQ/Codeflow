import { describe, it, expect, vi, beforeEach } from 'vitest';

import { HookManager } from '../HookManager.js';
import { ProfileInjectionHook } from '../ProfileInjectionHook.js';
import { UserProfileService, UserProfile } from '../../memory/UserProfileService.js';
import { AtomicMemoryService } from '../../memory/AtomicMemoryService.js';

vi.mock('../../memory/AtomicMemoryService.js', () => ({
  AtomicMemoryService: vi.fn().mockImplementation(() => ({
    getBySession: vi.fn().mockResolvedValue([]),
  })),
}));

const mockLLMAdapter = { send: vi.fn() };

describe('ProfileInjectionHook', () => {
  let hookManager: HookManager;
  let profileService: UserProfileService;
  let hook: ProfileInjectionHook;

  const testProfile: UserProfile = {
    userId: 'user-1',
    lastUpdated: 1000,
    sections: {
      preferences: '偏好 TypeScript',
      background: '全栈开发者',
      expertise: ['TypeScript', 'Go'],
      communicationStyle: '简洁直接',
      goals: ['提升代码质量'],
    },
    metadata: { totalSessions: 5, totalMessages: 100, lastActive: 1000 },
  };

  beforeEach(async () => {
    vi.clearAllMocks();
    hookManager = new HookManager();
    const memoryService = new AtomicMemoryService();
    profileService = new UserProfileService(mockLLMAdapter, memoryService);
    await profileService.saveProfile(testProfile);

    hook = new ProfileInjectionHook(hookManager, profileService, {
      userId: 'user-1',
    });
  });

  it('should inject profile as system message on before_send', async () => {
    hook.register();

    const payload = {
      messages: [{ role: 'user' as const, content: '你好' }],
    };

    const result = await hookManager.hook_before_send(payload);

    expect(result.messages.length).toBe(2);
    expect(result.messages[0].role).toBe('system');
    expect(result.messages[0].content).toContain('用户画像');
    expect(result.messages[0].content).toContain('TypeScript');
  });

  it('should not inject when disabled', async () => {
    hook.register();
    hook.setEnabled(false);

    const payload = {
      messages: [{ role: 'user' as const, content: '你好' }],
    };

    const result = await hookManager.hook_before_send(payload);
    expect(result.messages.length).toBe(1);
  });

  it('should not inject when profile not found', async () => {
    hook = new ProfileInjectionHook(hookManager, profileService, {
      userId: 'non-existent',
    });
    hook.register();

    const payload = {
      messages: [{ role: 'user' as const, content: '你好' }],
    };

    const result = await hookManager.hook_before_send(payload);
    expect(result.messages.length).toBe(1);
  });

  it('should support append position', async () => {
    hook = new ProfileInjectionHook(hookManager, profileService, {
      userId: 'user-1',
      position: 'append',
    });
    hook.register();

    const payload = {
      messages: [
        { role: 'system' as const, content: '你是助手' },
        { role: 'user' as const, content: '你好' },
      ],
    };

    const result = await hookManager.hook_before_send(payload);
    expect(result.messages.length).toBe(3);
    // Profile should be after the system message
    expect(result.messages[1].role).toBe('system');
    expect(result.messages[1].content).toContain('用户画像');
  });

  it('should format profile with all sections', async () => {
    hook.register();

    const payload = {
      messages: [{ role: 'user' as const, content: '你好' }],
    };

    const result = await hookManager.hook_before_send(payload);
    const profileMsg = result.messages[0].content;

    expect(profileMsg).toContain('偏好: 偏好 TypeScript');
    expect(profileMsg).toContain('背景: 全栈开发者');
    expect(profileMsg).toContain('专业领域: TypeScript, Go');
    expect(profileMsg).toContain('沟通风格: 简洁直接');
    expect(profileMsg).toContain('目标: 提升代码质量');
  });

  it('should support enable/disable toggle', () => {
    expect(hook.isEnabled()).toBe(true);
    hook.setEnabled(false);
    expect(hook.isEnabled()).toBe(false);
    hook.setEnabled(true);
    expect(hook.isEnabled()).toBe(true);
  });
});
