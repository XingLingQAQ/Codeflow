import { describe, it, expect, vi, beforeEach } from 'vitest';

import { HookManager } from '../../../hooks/HookManager.js';
import { HookEvent, TaskExecutionContext, TaskExecutionResult, TaskFailureContext } from '../../../hooks/types.js';
import { AtomicMemoryService } from '../../../memory/AtomicMemoryService.js';
import { MemoryShadowIntegration } from '../MemoryShadowIntegration.js';

// Mock AtomicMemoryService
vi.mock('../../../memory/AtomicMemoryService.js', () => {
  return {
    AtomicMemoryService: vi.fn().mockImplementation(() => ({
      add: vi.fn().mockResolvedValue(undefined),
      search: vi.fn().mockResolvedValue([]),
      searchByTimeRange: vi.fn().mockResolvedValue([]),
      searchByTags: vi.fn().mockResolvedValue([]),
      update: vi.fn().mockResolvedValue(undefined),
      delete: vi.fn().mockResolvedValue(undefined),
      getBySession: vi.fn().mockResolvedValue([]),
    })),
  };
});

describe('MemoryShadowIntegration', () => {
  let hookManager: HookManager;
  let memoryService: AtomicMemoryService;
  let integration: MemoryShadowIntegration;

  beforeEach(() => {
    hookManager = new HookManager();
    memoryService = new AtomicMemoryService();
    integration = new MemoryShadowIntegration(hookManager, memoryService, {
      projectRoot: '/tmp/test-project',
      shadowRoot: '.codeflow',
    });
  });

  describe('register/unregister', () => {
    it('should register all 4 task-level hooks', () => {
      integration.register();

      // Verify handlers are registered by checking handler count
      const events = [
        HookEvent.BEFORE_TASK_EXECUTE,
        HookEvent.AFTER_TASK_EXECUTE,
        HookEvent.ON_TASK_FAILURE,
        HookEvent.ON_TASK_COMPLETE,
      ];

      for (const event of events) {
        // Access private handlers map via any cast for testing
        const handlers = (hookManager as any).handlers.get(event);
        expect(handlers).toBeDefined();
        expect(handlers.size).toBe(1);
      }
    });
  });

  describe('hook_before_task_execute', () => {
    it('should search relevant memories and inject into metadata', async () => {
      const mockMemories = [
        {
          id: 'mem1',
          content: '相关记忆内容',
          tags: ['test'],
          importance: 0.8,
          timestamp: 1000,
          sessionId: 'session1',
          source: 'user' as const,
        },
      ];
      (memoryService.search as any).mockResolvedValue(mockMemories);

      integration.register();

      const context: TaskExecutionContext = {
        taskId: 'task-1',
        planId: 'plan-1',
        title: '测试任务',
        description: '测试描述',
        sessionId: 'session1',
        metadata: {},
      };

      await hookManager.hook_before_task_execute(context);

      expect(memoryService.search).toHaveBeenCalled();
      expect(context.metadata?._relevantMemories).toBeDefined();
      expect((context.metadata?._relevantMemories as any[]).length).toBe(1);
    });

    it('should not fail if memory search throws', async () => {
      (memoryService.search as any).mockRejectedValue(new Error('search failed'));

      integration.register();

      const context: TaskExecutionContext = {
        taskId: 'task-1',
        planId: 'plan-1',
        title: '测试任务',
        description: '测试描述',
        sessionId: 'session1',
      };

      // Should not throw
      await hookManager.hook_before_task_execute(context);
    });
  });

  describe('hook_after_task_execute', () => {
    it('should store task execution memory', async () => {
      integration.register();

      const result: TaskExecutionResult = {
        taskId: 'task-1',
        planId: 'plan-1',
        title: '完成的任务',
        status: 'completed',
        filesModified: ['src/index.ts'],
        sessionId: 'session1',
        durationMs: 5000,
      };

      await hookManager.hook_after_task_execute(result);

      expect(memoryService.add).toHaveBeenCalledWith(
        expect.objectContaining({
          content: expect.stringContaining('完成的任务'),
          tags: expect.arrayContaining(['task_execution', 'plan-1']),
          source: 'system',
          sessionId: 'session1',
        })
      );
    });

    it('should not fail if memory add throws', async () => {
      (memoryService.add as any).mockRejectedValue(new Error('add failed'));

      integration.register();

      const result: TaskExecutionResult = {
        taskId: 'task-1',
        planId: 'plan-1',
        title: '任务',
        status: 'completed',
        sessionId: 'session1',
      };

      await hookManager.hook_after_task_execute(result);
    });
  });

  describe('hook_on_task_failure', () => {
    it('should store failure memory and search historical fixes', async () => {
      const historicalFixes = [
        {
          id: 'fix1',
          content: '历史修复方案',
          tags: ['task_failure'],
          importance: 0.8,
          timestamp: 900,
          sessionId: 'old-session',
          source: 'system' as const,
        },
      ];
      (memoryService.search as any).mockResolvedValue(historicalFixes);

      integration.register();

      const failContext: TaskFailureContext = {
        taskId: 'task-1',
        planId: 'plan-1',
        title: '失败的任务',
        error: 'TypeError: cannot read property',
        phase: 'implement',
        sessionId: 'session1',
        metadata: {},
      };

      await hookManager.hook_on_task_failure(failContext);

      // Should store failure memory
      expect(memoryService.add).toHaveBeenCalledWith(
        expect.objectContaining({
          content: expect.stringContaining('任务失败'),
          tags: expect.arrayContaining(['task_failure']),
          importance: 0.8,
        })
      );

      // Should search historical fixes
      expect(memoryService.search).toHaveBeenCalled();

      // Should inject historical fixes into metadata
      expect(failContext.metadata?._historicalFixes).toBeDefined();
    });
  });

  describe('hook_on_task_complete', () => {
    it('should store completion memory', async () => {
      integration.register();

      const result: TaskExecutionResult = {
        taskId: 'task-1',
        planId: 'plan-1',
        title: '完成的任务',
        status: 'completed',
        filesModified: ['src/index.ts', 'src/utils.ts'],
        output: '所有测试通过',
        sessionId: 'session1',
      };

      await hookManager.hook_on_task_complete(result);

      expect(memoryService.add).toHaveBeenCalledWith(
        expect.objectContaining({
          content: expect.stringContaining('任务完成'),
          tags: expect.arrayContaining(['task_complete', 'plan-1']),
          source: 'system',
          importance: 0.6,
        })
      );
    });
  });

  describe('HookEvent enum', () => {
    it('should include all 4 task-level events', () => {
      expect(HookEvent.BEFORE_TASK_EXECUTE).toBe('before_task_execute');
      expect(HookEvent.AFTER_TASK_EXECUTE).toBe('after_task_execute');
      expect(HookEvent.ON_TASK_FAILURE).toBe('on_task_failure');
      expect(HookEvent.ON_TASK_COMPLETE).toBe('on_task_complete');
    });
  });
});
