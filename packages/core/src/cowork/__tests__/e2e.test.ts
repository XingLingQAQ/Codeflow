/**
 * Cowork E2E 测试
 * 测试 Orchestrator 与 AiderCodeEditor 的集成
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { AuditManager } from '../../audit/index.js';
import { CoworkOrchestrator } from '../CoworkOrchestrator.js';
import { AgentRuntime } from '../runtime.js';
import { AiderAdapter } from '../adapters/AiderAdapter.js';
import { AiderCodeEditor } from '../editors/AiderCodeEditor.js';
import {
  registerAiderExecutor,
  createOrchestratorWithAider,
  createTask,
} from '../factory.js';
import { CoworkTask, ExecutionResult, OrchestratorEvent } from '../types.js';

describe('Cowork E2E Tests', () => {
  let orchestrator: CoworkOrchestrator;

  beforeEach(() => {
    orchestrator = new CoworkOrchestrator();
  });

  afterEach(async () => {
    await orchestrator.cleanup();
  });

  describe('Factory Functions', () => {
    it('should register Aider executor via factory', () => {
      const editor = registerAiderExecutor(orchestrator);

      expect(editor).toBeInstanceOf(AiderCodeEditor);
      expect(orchestrator.getExecutor('aider')).toBeDefined();
      expect(orchestrator.getExecutor('aider')?.editor).toBe(editor);
    });

    it('should create orchestrator with Aider pre-registered', () => {
      const { orchestrator: orch, aiderEditor } = createOrchestratorWithAider();

      expect(orch).toBeInstanceOf(CoworkOrchestrator);
      expect(aiderEditor).toBeInstanceOf(AiderCodeEditor);
      expect(orch.getExecutor('aider')).toBeDefined();

      // Cleanup
      orch.cleanup();
    });

    it('should create task with helper function', () => {
      const task = createTask('task-1', 'aider', ['file.ts'], 'Add logging');

      expect(task.id).toBe('task-1');
      expect(task.executor).toBe('aider');
      expect(task.input.files).toEqual(['file.ts']);
      expect(task.input.instruction).toBe('Add logging');
      expect(task.status).toBe('pending');
      expect(task.type).toBe('code-edit');
    });

    it('should create task with custom options', () => {
      const task = createTask('task-2', 'aider', ['a.ts', 'b.ts'], 'Refactor', {
        type: 'refactor',
        context: 'Previous changes...',
        timeout: 30000,
        priority: 1,
      });

      expect(task.type).toBe('refactor');
      expect(task.input.context).toBe('Previous changes...');
      expect(task.config?.timeout).toBe(30000);
      expect(task.config?.priority).toBe(1);
    });
  });

  describe('Orchestrator Integration', () => {
    it('should execute task through registered executor', async () => {
      // 创建 mock editor
      const mockEditor = {
        name: 'mock-editor',
        edit: vi.fn().mockResolvedValue({
          success: true,
          file: 'test.ts',
          diff: { file: 'test.ts', hunks: [], additions: 5, deletions: 2 },
        }),
        editMultiple: vi.fn(),
        preview: vi.fn(),
        applyDiff: vi.fn(),
        undo: vi.fn(),
      };

      orchestrator.registerExecutor('mock', mockEditor, {
        name: 'mock',
        supportedTypes: ['code-edit'],
        maxConcurrency: 1,
        estimatedSpeed: 'fast',
        features: {
          streaming: false,
          multiFile: false,
          contextAware: false,
          codeReview: false,
        },
      });

      const task = createTask('e2e-1', 'mock', ['test.ts'], 'Add function');
      const result = await orchestrator.execute(task);

      expect(result.status).toBe('completed');
      expect(result.executor).toBe('mock');
      expect(mockEditor.edit).toHaveBeenCalledWith('test.ts', 'Add function');
    });

    it('should emit events during execution', async () => {
      const events: OrchestratorEvent[] = [];
      orchestrator.on('event', (e) => events.push(e));

      const mockEditor = {
        name: 'mock-editor',
        edit: vi.fn().mockResolvedValue({
          success: true,
          file: 'test.ts',
          diff: { file: 'test.ts', hunks: [], additions: 0, deletions: 0 },
        }),
        editMultiple: vi.fn(),
        preview: vi.fn(),
        applyDiff: vi.fn(),
        undo: vi.fn(),
      };

      orchestrator.registerExecutor('mock', mockEditor, {
        name: 'mock',
        supportedTypes: ['code-edit'],
        maxConcurrency: 1,
        estimatedSpeed: 'fast',
        features: {
          streaming: false,
          multiFile: false,
          contextAware: false,
          codeReview: false,
        },
      });

      const task = createTask('e2e-2', 'mock', ['test.ts'], 'Test');
      await orchestrator.execute(task);

      const startEvent = events.find(
        (e) => e.type === 'task:start' && 'task' in e && e.task.id === 'e2e-2'
      );
      const completeEvent = events.find(
        (e) => e.type === 'task:complete' && 'taskId' in e && e.taskId === 'e2e-2'
      );

      expect(startEvent).toBeDefined();
      expect(completeEvent).toBeDefined();
    });

    it('should handle executor not found', async () => {
      const task = createTask('e2e-3', 'nonexistent', ['test.ts'], 'Test');
      const result = await orchestrator.execute(task);

      expect(result.status).toBe('failed');
      expect(result.output?.error).toContain('not found');
    });

    it('should handle execution errors gracefully', async () => {
      const mockEditor = {
        name: 'error-editor',
        edit: vi.fn().mockRejectedValue(new Error('Execution failed')),
        editMultiple: vi.fn(),
        preview: vi.fn(),
        applyDiff: vi.fn(),
        undo: vi.fn(),
      };

      orchestrator.registerExecutor('error', mockEditor, {
        name: 'error',
        supportedTypes: ['code-edit'],
        maxConcurrency: 1,
        estimatedSpeed: 'fast',
        features: {
          streaming: false,
          multiFile: false,
          contextAware: false,
          codeReview: false,
        },
      });

      const task = createTask('e2e-4', 'error', ['test.ts'], 'Test');
      const result = await orchestrator.execute(task);

      expect(result.status).toBe('failed');
      expect(result.output?.error).toBe('Execution failed');
    });
  });

  describe('Parallel Execution', () => {
    it('should execute multiple tasks in parallel', async () => {
      const executionOrder: string[] = [];

      const mockEditor = {
        name: 'parallel-editor',
        edit: vi.fn().mockImplementation(async (file: string) => {
          executionOrder.push(file);
          await new Promise((r) => setTimeout(r, 50));
          return {
            success: true,
            file,
            diff: { file, hunks: [], additions: 1, deletions: 0 },
          };
        }),
        editMultiple: vi.fn(),
        preview: vi.fn(),
        applyDiff: vi.fn(),
        undo: vi.fn(),
      };

      orchestrator.registerExecutor('parallel', mockEditor, {
        name: 'parallel',
        supportedTypes: ['code-edit'],
        maxConcurrency: 3,
        estimatedSpeed: 'fast',
        features: {
          streaming: false,
          multiFile: false,
          contextAware: false,
          codeReview: false,
        },
      });

      const tasks = [
        createTask('p1', 'parallel', ['a.ts'], 'Edit A'),
        createTask('p2', 'parallel', ['b.ts'], 'Edit B'),
        createTask('p3', 'parallel', ['c.ts'], 'Edit C'),
      ];

      const startTime = Date.now();
      const result = await orchestrator.executeParallel(tasks, { maxConcurrency: 3 });
      const duration = Date.now() - startTime;

      expect(result.mode).toBe('parallel');
      expect(result.successCount).toBe(3);
      expect(result.failureCount).toBe(0);
      // 并行执行应该比顺序执行快
      expect(duration).toBeLessThan(200); // 3 * 50ms = 150ms if sequential
    });

    it('should detect file conflicts', async () => {
      const mockEditor = {
        name: 'conflict-editor',
        edit: vi.fn().mockResolvedValue({
          success: true,
          file: 'shared.ts',
          diff: { file: 'shared.ts', hunks: [], additions: 1, deletions: 0 },
        }),
        editMultiple: vi.fn(),
        preview: vi.fn(),
        applyDiff: vi.fn(),
        undo: vi.fn(),
      };

      orchestrator.registerExecutor('conflict', mockEditor, {
        name: 'conflict',
        supportedTypes: ['code-edit'],
        maxConcurrency: 2,
        estimatedSpeed: 'fast',
        features: {
          streaming: false,
          multiFile: false,
          contextAware: false,
          codeReview: false,
        },
      });

      const tasks = [
        createTask('c1', 'conflict', ['shared.ts'], 'Edit 1'),
        createTask('c2', 'conflict', ['shared.ts'], 'Edit 2'),
      ];

      const result = await orchestrator.executeParallel(tasks, {
        conflictStrategy: 'fail',
      });

      expect(result.conflicts).toBeDefined();
      expect(result.conflicts?.length).toBeGreaterThan(0);
      expect(result.conflicts?.[0].file).toBe('shared.ts');
    });

    it('should respect maxConcurrency', async () => {
      let concurrentCount = 0;
      let maxConcurrent = 0;

      const mockEditor = {
        name: 'concurrency-editor',
        edit: vi.fn().mockImplementation(async (file: string) => {
          concurrentCount++;
          maxConcurrent = Math.max(maxConcurrent, concurrentCount);
          await new Promise((r) => setTimeout(r, 50));
          concurrentCount--;
          return {
            success: true,
            file,
            diff: { file, hunks: [], additions: 0, deletions: 0 },
          };
        }),
        editMultiple: vi.fn(),
        preview: vi.fn(),
        applyDiff: vi.fn(),
        undo: vi.fn(),
      };

      orchestrator.registerExecutor('concurrency', mockEditor, {
        name: 'concurrency',
        supportedTypes: ['code-edit'],
        maxConcurrency: 5,
        estimatedSpeed: 'fast',
        features: {
          streaming: false,
          multiFile: false,
          contextAware: false,
          codeReview: false,
        },
      });

      const tasks = Array.from({ length: 6 }, (_, i) =>
        createTask(`conc-${i}`, 'concurrency', [`file${i}.ts`], `Edit ${i}`)
      );

      await orchestrator.executeParallel(tasks, { maxConcurrency: 2 });

      expect(maxConcurrent).toBeLessThanOrEqual(2);
    });
  });

  describe('Sequential Execution', () => {
    it('should execute tasks in sequence', async () => {
      const executionOrder: string[] = [];

      const mockEditor = {
        name: 'seq-editor',
        edit: vi.fn().mockImplementation(async (file: string) => {
          executionOrder.push(file);
          return {
            success: true,
            file,
            diff: { file, hunks: [], additions: 1, deletions: 0 },
          };
        }),
        editMultiple: vi.fn(),
        preview: vi.fn(),
        applyDiff: vi.fn(),
        undo: vi.fn(),
      };

      orchestrator.registerExecutor('seq', mockEditor, {
        name: 'seq',
        supportedTypes: ['code-edit'],
        maxConcurrency: 1,
        estimatedSpeed: 'fast',
        features: {
          streaming: false,
          multiFile: false,
          contextAware: false,
          codeReview: false,
        },
      });

      const tasks = [
        createTask('s1', 'seq', ['first.ts'], 'First'),
        createTask('s2', 'seq', ['second.ts'], 'Second'),
        createTask('s3', 'seq', ['third.ts'], 'Third'),
      ];

      const result = await orchestrator.executeSequence(tasks);

      expect(result.mode).toBe('sequential');
      expect(result.successCount).toBe(3);
      expect(executionOrder).toEqual(['first.ts', 'second.ts', 'third.ts']);
    });

    it('should stop on error when configured', async () => {
      const executionOrder: string[] = [];

      const mockEditor = {
        name: 'stop-editor',
        edit: vi.fn().mockImplementation(async (file: string) => {
          executionOrder.push(file);
          if (file === 'fail.ts') {
            throw new Error('Intentional failure');
          }
          return {
            success: true,
            file,
            diff: { file, hunks: [], additions: 0, deletions: 0 },
          };
        }),
        editMultiple: vi.fn(),
        preview: vi.fn(),
        applyDiff: vi.fn(),
        undo: vi.fn(),
      };

      orchestrator.registerExecutor('stop', mockEditor, {
        name: 'stop',
        supportedTypes: ['code-edit'],
        maxConcurrency: 1,
        estimatedSpeed: 'fast',
        features: {
          streaming: false,
          multiFile: false,
          contextAware: false,
          codeReview: false,
        },
      });

      const tasks = [
        createTask('st1', 'stop', ['ok.ts'], 'OK'),
        createTask('st2', 'stop', ['fail.ts'], 'Fail'),
        createTask('st3', 'stop', ['never.ts'], 'Never'),
      ];

      const result = await orchestrator.executeSequence(tasks, { stopOnError: true });

      expect(result.successCount).toBe(1);
      expect(result.failureCount).toBe(1);
      expect(executionOrder).toEqual(['ok.ts', 'fail.ts']);
      expect(executionOrder).not.toContain('never.ts');
    });

    it('should pass context between tasks', async () => {
      const receivedContexts: (string | undefined)[] = [];

      const mockEditor = {
        name: 'context-editor',
        edit: vi.fn().mockImplementation(async (file: string, instruction: string) => {
          return {
            success: true,
            file,
            diff: {
              file,
              hunks: [],
              additions: 5,
              deletions: 2,
            },
            result: `Output from ${file}`,
          };
        }),
        editMultiple: vi.fn(),
        preview: vi.fn(),
        applyDiff: vi.fn(),
        undo: vi.fn(),
      };

      orchestrator.registerExecutor('context', mockEditor, {
        name: 'context',
        supportedTypes: ['code-edit'],
        maxConcurrency: 1,
        estimatedSpeed: 'fast',
        features: {
          streaming: false,
          multiFile: false,
          contextAware: true,
          codeReview: false,
        },
      });

      const tasks = [
        createTask('ctx1', 'context', ['first.ts'], 'First task'),
        createTask('ctx2', 'context', ['second.ts'], 'Second task'),
      ];

      await orchestrator.executeSequence(tasks, { passContext: true });

      // 验证 Blackboard 有数据
      const entries = orchestrator.getAllBlackboardEntries();
      expect(entries.length).toBeGreaterThan(0);
    });
  });

  describe('Blackboard', () => {
    it('should store and retrieve entries', () => {
      orchestrator.setBlackboardEntry('key1', { data: 'value1' }, 'test');
      orchestrator.setBlackboardEntry('key2', 'string value', 'test');

      const entry1 = orchestrator.getBlackboardEntry('key1');
      const entry2 = orchestrator.getBlackboardEntry('key2');

      expect(entry1?.value).toEqual({ data: 'value1' });
      expect(entry2?.value).toBe('string value');
    });

    it('should clear all entries', () => {
      orchestrator.setBlackboardEntry('k1', 'v1', 'test');
      orchestrator.setBlackboardEntry('k2', 'v2', 'test');

      orchestrator.clearBlackboard();

      expect(orchestrator.getAllBlackboardEntries()).toEqual([]);
    });
  });

  describe('Performance', () => {
    it('should complete single task within 5s', async () => {
      const mockEditor = {
        name: 'perf-editor',
        edit: vi.fn().mockImplementation(async () => {
          await new Promise((r) => setTimeout(r, 100));
          return {
            success: true,
            file: 'test.ts',
            diff: { file: 'test.ts', hunks: [], additions: 0, deletions: 0 },
          };
        }),
        editMultiple: vi.fn(),
        preview: vi.fn(),
        applyDiff: vi.fn(),
        undo: vi.fn(),
      };

      orchestrator.registerExecutor('perf', mockEditor, {
        name: 'perf',
        supportedTypes: ['code-edit'],
        maxConcurrency: 1,
        estimatedSpeed: 'fast',
        features: {
          streaming: false,
          multiFile: false,
          contextAware: false,
          codeReview: false,
        },
      });

      const task = createTask('perf-1', 'perf', ['test.ts'], 'Test');

      const startTime = Date.now();
      const result = await orchestrator.execute(task);
      const duration = Date.now() - startTime;

      expect(result.status).toBe('completed');
      expect(duration).toBeLessThan(5000);
    });

    it('should complete 3 parallel tasks within 10s', async () => {
      const mockEditor = {
        name: 'perf-parallel-editor',
        edit: vi.fn().mockImplementation(async (file: string) => {
          await new Promise((r) => setTimeout(r, 200));
          return {
            success: true,
            file,
            diff: { file, hunks: [], additions: 0, deletions: 0 },
          };
        }),
        editMultiple: vi.fn(),
        preview: vi.fn(),
        applyDiff: vi.fn(),
        undo: vi.fn(),
      };

      orchestrator.registerExecutor('perf-parallel', mockEditor, {
        name: 'perf-parallel',
        supportedTypes: ['code-edit'],
        maxConcurrency: 3,
        estimatedSpeed: 'fast',
        features: {
          streaming: false,
          multiFile: false,
          contextAware: false,
          codeReview: false,
        },
      });

      const tasks = [
        createTask('pp1', 'perf-parallel', ['a.ts'], 'A'),
        createTask('pp2', 'perf-parallel', ['b.ts'], 'B'),
        createTask('pp3', 'perf-parallel', ['c.ts'], 'C'),
      ];

      const startTime = Date.now();
      const result = await orchestrator.executeParallel(tasks, { maxConcurrency: 3 });
      const duration = Date.now() - startTime;

      expect(result.successCount).toBe(3);
      expect(duration).toBeLessThan(10000);
      // 并行应该比顺序快 2 倍以上
      expect(duration).toBeLessThan(600); // 3 * 200ms = 600ms if sequential
    });
  });

  describe('Runtime governance E2E', () => {
    it('should surface redacted metadata and fallback recovery through orchestrator execution', async () => {
      const auditManager = new AuditManager();
      const runtime = new AgentRuntime({ auditManager });
      const primaryEditor = {
        name: 'primary-editor',
        edit: vi.fn().mockResolvedValue({
          success: false,
          file: 'runtime.ts',
          diff: { file: 'runtime.ts', hunks: [], additions: 0, deletions: 0 },
          message: 'primary editor failed',
        }),
        editMultiple: vi.fn(),
        preview: vi.fn(),
        applyDiff: vi.fn(),
        undo: vi.fn(),
      };
      const fallbackEditor = {
        name: 'fallback-editor',
        edit: vi.fn().mockResolvedValue({
          success: true,
          file: 'runtime.ts',
          diff: { file: 'runtime.ts', hunks: [], additions: 1, deletions: 0 },
        }),
        editMultiple: vi.fn(),
        preview: vi.fn(),
        applyDiff: vi.fn(),
        undo: vi.fn(),
      };

      runtime.registerExecutor('primary', primaryEditor, {
        name: 'primary',
        supportedTypes: ['code-edit'],
        maxConcurrency: 1,
        estimatedSpeed: 'fast',
        features: { streaming: false, multiFile: false, contextAware: true, codeReview: false },
      });
      runtime.registerExecutor('backup', fallbackEditor, {
        name: 'backup',
        supportedTypes: ['code-edit'],
        maxConcurrency: 1,
        estimatedSpeed: 'fast',
        features: { streaming: false, multiFile: false, contextAware: true, codeReview: false },
      });

      const governedOrchestrator = new CoworkOrchestrator(undefined, undefined, runtime);
      const task: CoworkTask = {
        id: 'runtime-e2e-governed',
        type: 'code-edit',
        executor: 'primary',
        input: {
          files: ['runtime.ts'],
          instruction: 'Apply governed runtime change',
        },
        runtime: {
          actor: {
            id: 'agent-1',
            type: 'agent',
            sessionId: 'sess-runtime-e2e',
          },
          policy: {
            command: 'governed-edit',
            metadata: {
              apiToken: 'secret-runtime-token',
              wikiUrl: 'https://wiki.local/runtime',
            },
            boundaries: [{ type: 'command', value: 'governed-edit', risk: 'low' }],
          },
        },
        status: 'pending',
        createdAt: Date.now(),
      };

      const result = await governedOrchestrator.execute(task);
      const auditEntries = await auditManager.query({ resourceType: 'tool-runtime' });

      expect(result.status).toBe('completed');
      expect(result.executor).toBe('backup');
      expect(task.output?.runtime).toMatchObject({
        decision: 'allow',
        fallback: {
          attempted: true,
          fromExecutor: 'primary',
          toExecutor: 'backup',
          recovered: true,
        },
        snapshot: {
          command: 'governed-edit',
          metadataPreview: {
            apiToken: '[REDACTED]',
            wikiUrl: 'https://wiki.local/runtime',
          },
        },
      });
      expect(primaryEditor.edit).toHaveBeenCalledTimes(1);
      expect(fallbackEditor.edit).toHaveBeenCalledTimes(1);
      expect(auditEntries.entries[0]?.details?.metadataPreview).toMatchObject({
        apiToken: '[REDACTED]',
        wikiUrl: 'https://wiki.local/runtime',
      });

      await governedOrchestrator.cleanup();
    });

    it('should block high-risk runtime requests before editor invocation', async () => {
      const auditManager = new AuditManager();
      const runtime = new AgentRuntime({ auditManager });
      const editor = {
        name: 'deny-editor',
        edit: vi.fn(),
        editMultiple: vi.fn(),
        preview: vi.fn(),
        applyDiff: vi.fn(),
        undo: vi.fn(),
      };

      runtime.registerExecutor('guarded', editor, {
        name: 'guarded',
        supportedTypes: ['code-edit'],
        maxConcurrency: 1,
        estimatedSpeed: 'fast',
        features: { streaming: false, multiFile: false, contextAware: true, codeReview: false },
      });

      const governedOrchestrator = new CoworkOrchestrator(undefined, undefined, runtime);
      const task = createTask('runtime-e2e-deny', 'guarded', ['danger.ts'], 'Attempt risky sync') as CoworkTask;
      task.runtime = {
        actor: {
          id: 'user-1',
          type: 'user',
        },
        policy: {
          metadata: {
            accessToken: 'deny-secret',
          },
          boundaries: [{ type: 'command', value: 'guarded', risk: 'low' }],
          requestedBoundaries: [{ type: 'network', value: 'https://market.example', risk: 'high' }],
        },
      };

      const result = await governedOrchestrator.execute(task);
      const auditEntries = await auditManager.query({ resourceType: 'tool-runtime', outcome: 'failure' });

      expect(result.status).toBe('failed');
      expect(result.output?.error).toBe('Execution blocked by runtime policy');
      expect(task.output?.runtime).toMatchObject({
        decision: 'deny',
        risk: 'high',
        fallback: {
          attempted: false,
          recovered: false,
        },
        snapshot: {
          metadataPreview: {
            accessToken: '[REDACTED]',
          },
        },
      });
      expect(editor.edit).not.toHaveBeenCalled();
      expect(auditEntries.entries[0]?.details?.decision).toBe('deny');

      await governedOrchestrator.cleanup();
    });
  });
});
