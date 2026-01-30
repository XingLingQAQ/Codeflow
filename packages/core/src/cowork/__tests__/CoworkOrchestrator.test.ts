/**
 * CoworkOrchestrator 单元测试
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  CoworkOrchestrator,
} from '../CoworkOrchestrator.js';
import {
  ICodeEditor,
  ExecutorCapabilities,
  CoworkTask,
  EditResult,
  Diff,
  OrchestratorEvent,
} from '../types.js';

// Mock CodeEditor
class MockCodeEditor implements ICodeEditor {
  name = 'mock-editor';
  editCalls: Array<{ file: string; instruction: string }> = [];
  shouldFail = false;

  async edit(file: string, instruction: string): Promise<EditResult> {
    this.editCalls.push({ file, instruction });

    if (this.shouldFail) {
      throw new Error('Mock edit failed');
    }

    return {
      success: true,
      file,
      diff: {
        file,
        hunks: [],
        additions: 10,
        deletions: 5,
      },
    };
  }

  async editMultiple(files: string[], instruction: string): Promise<EditResult[]> {
    return Promise.all(files.map((f) => this.edit(f, instruction)));
  }

  async preview(file: string, instruction: string): Promise<Diff> {
    return {
      file,
      hunks: [],
      additions: 0,
      deletions: 0,
    };
  }

  async applyDiff(file: string, diff: Diff): Promise<void> {}

  async undo(): Promise<void> {}
}

const mockCapabilities: ExecutorCapabilities = {
  name: 'mock',
  supportedTypes: ['code-edit', 'refactor'],
  maxConcurrency: 3,
  estimatedSpeed: 'fast',
  features: {
    streaming: true,
    multiFile: true,
    contextAware: true,
    codeReview: false,
  },
};

describe('CoworkOrchestrator', () => {
  let orchestrator: CoworkOrchestrator;
  let mockEditor: MockCodeEditor;

  beforeEach(() => {
    orchestrator = new CoworkOrchestrator();
    mockEditor = new MockCodeEditor();
  });

  afterEach(async () => {
    await orchestrator.cleanup();
  });

  describe('registerExecutor', () => {
    it('should register an executor', () => {
      orchestrator.registerExecutor('test', mockEditor, mockCapabilities);

      const executor = orchestrator.getExecutor('test');
      expect(executor).toBeDefined();
      expect(executor?.name).toBe('test');
    });

    it('should return all registered executors', () => {
      orchestrator.registerExecutor('exec1', mockEditor, mockCapabilities);
      orchestrator.registerExecutor('exec2', mockEditor, mockCapabilities);

      const all = orchestrator.getAllExecutors();
      expect(all.length).toBe(2);
    });
  });

  describe('execute', () => {
    beforeEach(() => {
      orchestrator.registerExecutor('test', mockEditor, mockCapabilities);
    });

    it('should execute a single task', async () => {
      const task: CoworkTask = {
        id: 'task-1',
        type: 'code-edit',
        executor: 'test',
        input: {
          files: ['src/test.ts'],
          instruction: 'Add a function',
        },
        status: 'pending',
        createdAt: Date.now(),
      };

      const result = await orchestrator.execute(task);

      expect(result.status).toBe('completed');
      expect(result.taskId).toBe('task-1');
      expect(mockEditor.editCalls.length).toBe(1);
    });

    it('should handle multiple files', async () => {
      const task: CoworkTask = {
        id: 'task-2',
        type: 'code-edit',
        executor: 'test',
        input: {
          files: ['src/a.ts', 'src/b.ts', 'src/c.ts'],
          instruction: 'Refactor',
        },
        status: 'pending',
        createdAt: Date.now(),
      };

      const result = await orchestrator.execute(task);

      expect(result.status).toBe('completed');
      expect(mockEditor.editCalls.length).toBe(3);
    });

    it('should return failed result for unknown executor', async () => {
      const task: CoworkTask = {
        id: 'task-3',
        type: 'code-edit',
        executor: 'unknown',
        input: {
          files: ['src/test.ts'],
          instruction: 'Test',
        },
        status: 'pending',
        createdAt: Date.now(),
      };

      const result = await orchestrator.execute(task);

      expect(result.status).toBe('failed');
      expect(result.output?.error).toContain('not found');
    });

    it('should handle editor errors', async () => {
      mockEditor.shouldFail = true;

      const task: CoworkTask = {
        id: 'task-4',
        type: 'code-edit',
        executor: 'test',
        input: {
          files: ['src/test.ts'],
          instruction: 'Test',
        },
        status: 'pending',
        createdAt: Date.now(),
      };

      const result = await orchestrator.execute(task);

      expect(result.status).toBe('failed');
      expect(result.output?.error).toContain('Mock edit failed');
    });

    it('should emit events', async () => {
      const events: OrchestratorEvent[] = [];
      orchestrator.on('event', (e) => events.push(e));

      const task: CoworkTask = {
        id: 'task-5',
        type: 'code-edit',
        executor: 'test',
        input: {
          files: ['src/test.ts'],
          instruction: 'Test',
        },
        status: 'pending',
        createdAt: Date.now(),
      };

      await orchestrator.execute(task);

      expect(events.some((e) => e.type === 'task:start')).toBe(true);
      expect(events.some((e) => e.type === 'task:complete')).toBe(true);
    });
  });

  describe('executeParallel', () => {
    beforeEach(() => {
      orchestrator.registerExecutor('test', mockEditor, mockCapabilities);
    });

    it('should execute tasks in parallel', async () => {
      const tasks: CoworkTask[] = [
        {
          id: 'p-1',
          type: 'code-edit',
          executor: 'test',
          input: { files: ['a.ts'], instruction: 'Edit A' },
          status: 'pending',
          createdAt: Date.now(),
        },
        {
          id: 'p-2',
          type: 'code-edit',
          executor: 'test',
          input: { files: ['b.ts'], instruction: 'Edit B' },
          status: 'pending',
          createdAt: Date.now(),
        },
      ];

      const result = await orchestrator.executeParallel(tasks);

      expect(result.mode).toBe('parallel');
      expect(result.results.length).toBe(2);
      expect(result.successCount).toBe(2);
    });

    it('should detect file conflicts', async () => {
      const tasks: CoworkTask[] = [
        {
          id: 'c-1',
          type: 'code-edit',
          executor: 'test',
          input: { files: ['same.ts'], instruction: 'Edit 1' },
          status: 'pending',
          createdAt: Date.now(),
        },
        {
          id: 'c-2',
          type: 'code-edit',
          executor: 'test',
          input: { files: ['same.ts'], instruction: 'Edit 2' },
          status: 'pending',
          createdAt: Date.now(),
        },
      ];

      const result = await orchestrator.executeParallel(tasks, {
        conflictStrategy: 'fail',
      });

      expect(result.conflicts?.length).toBeGreaterThan(0);
      expect(result.failureCount).toBe(2);
    });

    it('should respect maxConcurrency', async () => {
      const tasks: CoworkTask[] = Array.from({ length: 10 }, (_, i) => ({
        id: `mc-${i}`,
        type: 'code-edit' as const,
        executor: 'test',
        input: { files: [`file${i}.ts`], instruction: 'Edit' },
        status: 'pending' as const,
        createdAt: Date.now(),
      }));

      const result = await orchestrator.executeParallel(tasks, {
        maxConcurrency: 3,
      });

      expect(result.results.length).toBe(10);
    });

    it('should stop on error when failFast is true', async () => {
      mockEditor.shouldFail = true;

      const tasks: CoworkTask[] = [
        {
          id: 'ff-1',
          type: 'code-edit',
          executor: 'test',
          input: { files: ['a.ts'], instruction: 'Edit' },
          status: 'pending',
          createdAt: Date.now(),
        },
        {
          id: 'ff-2',
          type: 'code-edit',
          executor: 'test',
          input: { files: ['b.ts'], instruction: 'Edit' },
          status: 'pending',
          createdAt: Date.now(),
        },
      ];

      const result = await orchestrator.executeParallel(tasks, {
        failFast: true,
        maxConcurrency: 1,
      });

      expect(result.failureCount).toBeGreaterThan(0);
    });
  });

  describe('executeSequence', () => {
    beforeEach(() => {
      orchestrator.registerExecutor('test', mockEditor, mockCapabilities);
    });

    it('should execute tasks sequentially', async () => {
      const tasks: CoworkTask[] = [
        {
          id: 's-1',
          type: 'code-edit',
          executor: 'test',
          input: { files: ['a.ts'], instruction: 'Step 1' },
          status: 'pending',
          createdAt: Date.now(),
        },
        {
          id: 's-2',
          type: 'code-edit',
          executor: 'test',
          input: { files: ['b.ts'], instruction: 'Step 2' },
          status: 'pending',
          createdAt: Date.now(),
        },
      ];

      const result = await orchestrator.executeSequence(tasks);

      expect(result.mode).toBe('sequential');
      expect(result.results.length).toBe(2);
      expect(result.successCount).toBe(2);
    });

    it('should stop on error when stopOnError is true', async () => {
      mockEditor.shouldFail = true;

      const tasks: CoworkTask[] = [
        {
          id: 'se-1',
          type: 'code-edit',
          executor: 'test',
          input: { files: ['a.ts'], instruction: 'Step 1' },
          status: 'pending',
          createdAt: Date.now(),
        },
        {
          id: 'se-2',
          type: 'code-edit',
          executor: 'test',
          input: { files: ['b.ts'], instruction: 'Step 2' },
          status: 'pending',
          createdAt: Date.now(),
        },
      ];

      const result = await orchestrator.executeSequence(tasks, {
        stopOnError: true,
      });

      expect(result.results.length).toBe(1);
      expect(result.failureCount).toBe(1);
    });

    it('should pass context between tasks', async () => {
      const tasks: CoworkTask[] = [
        {
          id: 'ctx-1',
          type: 'code-edit',
          executor: 'test',
          input: { files: ['a.ts'], instruction: 'Step 1' },
          status: 'pending',
          createdAt: Date.now(),
        },
        {
          id: 'ctx-2',
          type: 'code-edit',
          executor: 'test',
          input: { files: ['b.ts'], instruction: 'Step 2' },
          status: 'pending',
          createdAt: Date.now(),
        },
      ];

      await orchestrator.executeSequence(tasks, { passContext: true });

      // 检查 Blackboard 是否有条目
      const entries = orchestrator.getAllBlackboardEntries();
      expect(entries.length).toBeGreaterThan(0);
    });
  });

  describe('executeDebate', () => {
    let criticEditor: MockCodeEditor;

    beforeEach(() => {
      criticEditor = new MockCodeEditor();
      criticEditor.name = 'critic-editor';

      orchestrator.registerExecutor('generator', mockEditor, mockCapabilities);
      orchestrator.registerExecutor('critic', criticEditor, {
        ...mockCapabilities,
        name: 'critic',
      });
    });

    it('should execute debate rounds', async () => {
      const task: CoworkTask = {
        id: 'debate-1',
        type: 'code-edit',
        executor: 'generator',
        input: {
          files: ['src/test.ts'],
          instruction: 'Implement feature',
        },
        status: 'pending',
        createdAt: Date.now(),
      };

      const result = await orchestrator.executeDebate(task, {
        generator: 'generator',
        critic: 'critic',
        maxRounds: 2,
      });

      expect(result.mode).toBe('debate');
      expect(result.rounds.length).toBeGreaterThan(0);
    });

    it('should return failure for missing executors', async () => {
      const task: CoworkTask = {
        id: 'debate-2',
        type: 'code-edit',
        executor: 'unknown',
        input: {
          files: ['src/test.ts'],
          instruction: 'Test',
        },
        status: 'pending',
        createdAt: Date.now(),
      };

      const result = await orchestrator.executeDebate(task, {
        generator: 'unknown',
        critic: 'also-unknown',
      });

      expect(result.failureCount).toBe(1);
    });
  });

  describe('Blackboard', () => {
    it('should set and get entries', () => {
      orchestrator.setBlackboardEntry('key1', { data: 'value' }, 'test');

      const entry = orchestrator.getBlackboardEntry('key1');
      expect(entry?.value).toEqual({ data: 'value' });
      expect(entry?.source).toBe('test');
    });

    it('should return all entries', () => {
      orchestrator.setBlackboardEntry('k1', 'v1', 's1');
      orchestrator.setBlackboardEntry('k2', 'v2', 's2');

      const all = orchestrator.getAllBlackboardEntries();
      expect(all.length).toBe(2);
    });

    it('should clear all entries', () => {
      orchestrator.setBlackboardEntry('k1', 'v1', 's1');
      orchestrator.clearBlackboard();

      expect(orchestrator.getAllBlackboardEntries().length).toBe(0);
    });
  });

  describe('Task management', () => {
    beforeEach(() => {
      orchestrator.registerExecutor('test', mockEditor, mockCapabilities);
    });

    it('should track running tasks', async () => {
      // 创建一个慢任务
      const slowEditor = new MockCodeEditor();
      slowEditor.edit = async (file, instruction) => {
        await new Promise((resolve) => setTimeout(resolve, 100));
        return {
          success: true,
          file,
          diff: { file, hunks: [], additions: 0, deletions: 0 },
        };
      };

      orchestrator.registerExecutor('slow', slowEditor, mockCapabilities);

      const task: CoworkTask = {
        id: 'running-1',
        type: 'code-edit',
        executor: 'slow',
        input: { files: ['test.ts'], instruction: 'Test' },
        status: 'pending',
        createdAt: Date.now(),
      };

      const promise = orchestrator.execute(task);

      // 任务应该在运行中
      const running = orchestrator.getRunningTasks();
      expect(running.length).toBe(1);

      await promise;
    });

    it('should cancel running tasks', async () => {
      const slowEditor = new MockCodeEditor();
      slowEditor.edit = async (file, instruction) => {
        await new Promise((resolve) => setTimeout(resolve, 1000));
        return {
          success: true,
          file,
          diff: { file, hunks: [], additions: 0, deletions: 0 },
        };
      };

      orchestrator.registerExecutor('slow', slowEditor, mockCapabilities);

      const task: CoworkTask = {
        id: 'cancel-1',
        type: 'code-edit',
        executor: 'slow',
        input: { files: ['test.ts'], instruction: 'Test' },
        status: 'pending',
        createdAt: Date.now(),
      };

      orchestrator.execute(task);

      const cancelled = await orchestrator.cancelTask('cancel-1');
      expect(cancelled).toBe(true);
    });
  });

  describe('cleanup', () => {
    it('should cleanup all resources', async () => {
      orchestrator.registerExecutor('test', mockEditor, mockCapabilities);
      orchestrator.setBlackboardEntry('key', 'value', 'test');

      await orchestrator.cleanup();

      expect(orchestrator.getAllBlackboardEntries().length).toBe(0);
      expect(orchestrator.getRunningTasks().length).toBe(0);
    });
  });
});
