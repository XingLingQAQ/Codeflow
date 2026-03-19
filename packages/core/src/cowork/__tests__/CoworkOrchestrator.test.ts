/**
 * CoworkOrchestrator 单元测试
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  CoworkOrchestrator,
} from '../CoworkOrchestrator.js';
import { AgentRuntime } from '../runtime.js';
import { AuditManager } from '../../audit/AuditManager.js';
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

    it('should support interruption via AbortSignal', async () => {
      const controller = new AbortController();

      const tasks: CoworkTask[] = [
        {
          id: 'int-1',
          type: 'code-edit',
          executor: 'test',
          input: { files: ['a.ts'], instruction: 'Step 1' },
          status: 'pending',
          createdAt: Date.now(),
        },
        {
          id: 'int-2',
          type: 'code-edit',
          executor: 'test',
          input: { files: ['b.ts'], instruction: 'Step 2' },
          status: 'pending',
          createdAt: Date.now(),
        },
        {
          id: 'int-3',
          type: 'code-edit',
          executor: 'test',
          input: { files: ['c.ts'], instruction: 'Step 3' },
          status: 'pending',
          createdAt: Date.now(),
        },
      ];

      // 在第一个任务后中断
      const result = await orchestrator.executeSequence(tasks, {
        signal: controller.signal,
        onAfterTask: async (_task, _result, index) => {
          if (index === 0) {
            controller.abort();
          }
        },
      });

      expect(result.interrupted).toBe(true);
      expect(result.results.length).toBe(1);
    });

    it('should support onBeforeTask callback', async () => {
      const beforeCalls: number[] = [];

      const tasks: CoworkTask[] = [
        {
          id: 'cb-1',
          type: 'code-edit',
          executor: 'test',
          input: { files: ['a.ts'], instruction: 'Step 1' },
          status: 'pending',
          createdAt: Date.now(),
        },
        {
          id: 'cb-2',
          type: 'code-edit',
          executor: 'test',
          input: { files: ['b.ts'], instruction: 'Step 2' },
          status: 'pending',
          createdAt: Date.now(),
        },
      ];

      await orchestrator.executeSequence(tasks, {
        onBeforeTask: async (_task, index) => {
          beforeCalls.push(index);
          return true;
        },
      });

      expect(beforeCalls).toEqual([0, 1]);
    });

    it('should stop when onBeforeTask returns false', async () => {
      const tasks: CoworkTask[] = [
        {
          id: 'stop-1',
          type: 'code-edit',
          executor: 'test',
          input: { files: ['a.ts'], instruction: 'Step 1' },
          status: 'pending',
          createdAt: Date.now(),
        },
        {
          id: 'stop-2',
          type: 'code-edit',
          executor: 'test',
          input: { files: ['b.ts'], instruction: 'Step 2' },
          status: 'pending',
          createdAt: Date.now(),
        },
      ];

      const result = await orchestrator.executeSequence(tasks, {
        onBeforeTask: async (_task, index) => {
          return index === 0; // 只执行第一个
        },
      });

      expect(result.interrupted).toBe(true);
      expect(result.results.length).toBe(1);
    });
  });


  describe('runtime policy', () => {
    function createTask(id: string, policy?: CoworkTask['runtime']): CoworkTask {
      return {
        id,
        type: 'code-edit',
        executor: 'test',
        input: {
          files: ['src/runtime.ts'],
          instruction: 'Apply runtime policy aware edit',
        },
        runtime: policy,
        status: 'pending',
        createdAt: Date.now(),
      };
    }

    function createPolicyRuntime(boundaries: NonNullable<NonNullable<CoworkTask['runtime']>['policy']>['boundaries']) {
      const auditManager = new AuditManager();
      const runtime = new AgentRuntime({ auditManager });
      runtime.registerExecutor('test', mockEditor, mockCapabilities);

      const orchestratorWithRuntime = new CoworkOrchestrator(undefined, undefined, runtime);
      return { auditManager, orchestratorWithRuntime, runtimeTask: { policy: { boundaries } } };
    }

    it('should allow execution and write runtime outcome when policy matches request boundaries', async () => {
      const { auditManager, orchestratorWithRuntime, runtimeTask } = createPolicyRuntime([
        { type: 'command', value: 'test', risk: 'low' },
      ]);
      const task = createTask('task-runtime-allow', runtimeTask);

      const result = await orchestratorWithRuntime.execute(task);
      const auditEntries = await auditManager.query({ resourceType: 'tool-runtime', outcome: 'success' });

      expect(result.status).toBe('completed');
      expect(task.output?.runtime).toMatchObject({
        decision: 'allow',
        isolated: false,
        risk: 'low',
      });
      expect(mockEditor.editCalls.length).toBe(1);
      expect(auditEntries.entries[0]?.action).toBe('policy_execute');
    });

    it('should isolate execution for unmatched low-risk boundary and continue editing', async () => {
      const { orchestratorWithRuntime, runtimeTask } = createPolicyRuntime([
        { type: 'command', value: 'test', risk: 'low' },
      ]);
      const task = createTask('task-runtime-isolated', {
        ...runtimeTask,
        policy: {
          ...runtimeTask.policy,
          requestedBoundaries: [{ type: 'resource', value: 'memory:burst', risk: 'low' }],
        },
      });

      const result = await orchestratorWithRuntime.execute(task);

      expect(result.status).toBe('completed');
      expect(task.output?.runtime).toMatchObject({
        decision: 'allow_with_isolation',
        isolated: true,
        processId: 'isolated:task-runtime-isolated',
      });
      expect(mockEditor.editCalls.length).toBe(1);
    });

    it('should require approval for unmatched medium-risk boundary and skip editing', async () => {
      const { auditManager, orchestratorWithRuntime, runtimeTask } = createPolicyRuntime([
        { type: 'command', value: 'test', risk: 'low' },
      ]);
      const task = createTask('task-runtime-approval', {
        ...runtimeTask,
        policy: {
          ...runtimeTask.policy,
          requestedBoundaries: [{ type: 'network', value: 'api.internal', risk: 'medium' }],
        },
      });

      const result = await orchestratorWithRuntime.execute(task);
      const auditEntries = await auditManager.query({ resourceType: 'tool-runtime', outcome: 'failure' });

      expect(result.status).toBe('failed');
      expect(result.output?.error).toBe('Execution requires manual approval');
      expect(task.output?.runtime).toMatchObject({
        decision: 'require_approval',
        isolated: false,
        risk: 'medium',
      });
      expect(mockEditor.editCalls.length).toBe(0);
      expect(auditEntries.entries[0]?.details?.decision).toBe('require_approval');
    });

    it('should deny execution for unmatched high-risk boundary and skip editing', async () => {
      const { auditManager, orchestratorWithRuntime, runtimeTask } = createPolicyRuntime([
        { type: 'command', value: 'test', risk: 'low' },
      ]);
      const task = createTask('task-runtime-deny', {
        ...runtimeTask,
        policy: {
          ...runtimeTask.policy,
          requestedBoundaries: [{ type: 'command', value: 'powershell', risk: 'high' }],
        },
      });

      const result = await orchestratorWithRuntime.execute(task);
      const auditEntries = await auditManager.query({ resourceType: 'tool-runtime', outcome: 'failure' });

      expect(result.status).toBe('failed');
      expect(result.output?.error).toBe('Execution blocked by runtime policy');
      expect(task.output?.runtime).toMatchObject({
        decision: 'deny',
        isolated: false,
        risk: 'high',
      });
      expect(mockEditor.editCalls.length).toBe(0);
      expect(auditEntries.entries[0]?.details?.decision).toBe('deny');
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

    it('should support interruption via AbortSignal', async () => {
      const controller = new AbortController();

      const task: CoworkTask = {
        id: 'debate-int',
        type: 'code-edit',
        executor: 'generator',
        input: {
          files: ['src/test.ts'],
          instruction: 'Implement feature',
        },
        status: 'pending',
        createdAt: Date.now(),
      };

      // 立即中断
      controller.abort();

      const result = await orchestrator.executeDebate(task, {
        generator: 'generator',
        critic: 'critic',
        maxRounds: 5,
        signal: controller.signal,
      });

      expect(result.interrupted).toBe(true);
      expect(result.rounds.length).toBe(0);
    });

    it('should support custom convergence checker', async () => {
      const task: CoworkTask = {
        id: 'debate-conv',
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
        maxRounds: 5,
        checkConvergence: (round, allRounds) => {
          // 自定义：第一轮就收敛
          return round.round === 1;
        },
      });

      expect(result.converged).toBe(true);
      expect(result.rounds.length).toBe(1);
    });

    it('should call onRound callback', async () => {
      const roundCalls: number[] = [];

      const task: CoworkTask = {
        id: 'debate-cb',
        type: 'code-edit',
        executor: 'generator',
        input: {
          files: ['src/test.ts'],
          instruction: 'Implement feature',
        },
        status: 'pending',
        createdAt: Date.now(),
      };

      await orchestrator.executeDebate(task, {
        generator: 'generator',
        critic: 'critic',
        maxRounds: 2,
        onRound: async (round) => {
          roundCalls.push(round.round);
        },
      });

      expect(roundCalls.length).toBeGreaterThan(0);
    });

    it('should emit debate:round events', async () => {
      const events: OrchestratorEvent[] = [];
      orchestrator.on('event', (e) => events.push(e));

      const task: CoworkTask = {
        id: 'debate-evt',
        type: 'code-edit',
        executor: 'generator',
        input: {
          files: ['src/test.ts'],
          instruction: 'Implement feature',
        },
        status: 'pending',
        createdAt: Date.now(),
      };

      await orchestrator.executeDebate(task, {
        generator: 'generator',
        critic: 'critic',
        maxRounds: 2,
      });

      const debateEvents = events.filter((e) => e.type === 'debate:round');
      expect(debateEvents.length).toBeGreaterThan(0);
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

  describe('parseIssues (via executeDebate)', () => {
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

    it('should parse bug issues from critic output', async () => {
      // 模拟 critic 返回包含 bug 的输出
      criticEditor.edit = async () => ({
        success: true,
        file: 'test.ts',
        diff: { file: 'test.ts', hunks: [], additions: 0, deletions: 0 },
        result: 'Bug: Missing null check on line 42',
      });

      const task: CoworkTask = {
        id: 'parse-bug',
        type: 'code-edit',
        executor: 'generator',
        input: { files: ['test.ts'], instruction: 'Fix bugs' },
        status: 'pending',
        createdAt: Date.now(),
      };

      const result = await orchestrator.executeDebate(task, {
        generator: 'generator',
        critic: 'critic',
        maxRounds: 1,
      });

      expect(result.rounds.length).toBeGreaterThan(0);
    });

    it('should parse security issues from critic output', async () => {
      criticEditor.edit = async () => ({
        success: true,
        file: 'test.ts',
        diff: { file: 'test.ts', hunks: [], additions: 0, deletions: 0 },
        result: 'Security vulnerability: SQL injection risk',
      });

      const task: CoworkTask = {
        id: 'parse-security',
        type: 'code-edit',
        executor: 'generator',
        input: { files: ['test.ts'], instruction: 'Security review' },
        status: 'pending',
        createdAt: Date.now(),
      };

      const result = await orchestrator.executeDebate(task, {
        generator: 'generator',
        critic: 'critic',
        maxRounds: 1,
      });

      expect(result.rounds.length).toBeGreaterThan(0);
    });

    it('should parse performance issues from critic output', async () => {
      criticEditor.edit = async () => ({
        success: true,
        file: 'test.ts',
        diff: { file: 'test.ts', hunks: [], additions: 0, deletions: 0 },
        result: 'Performance: Slow database query in loop',
      });

      const task: CoworkTask = {
        id: 'parse-perf',
        type: 'code-edit',
        executor: 'generator',
        input: { files: ['test.ts'], instruction: 'Optimize' },
        status: 'pending',
        createdAt: Date.now(),
      };

      const result = await orchestrator.executeDebate(task, {
        generator: 'generator',
        critic: 'critic',
        maxRounds: 1,
      });

      expect(result.rounds.length).toBeGreaterThan(0);
    });

    it('should parse structured issue format', async () => {
      criticEditor.edit = async () => ({
        success: true,
        file: 'test.ts',
        diff: { file: 'test.ts', hunks: [], additions: 0, deletions: 0 },
        result: '- [critical] Memory leak in event handler\n- [low] Style: inconsistent naming',
      });

      const task: CoworkTask = {
        id: 'parse-structured',
        type: 'code-edit',
        executor: 'generator',
        input: { files: ['test.ts'], instruction: 'Review' },
        status: 'pending',
        createdAt: Date.now(),
      };

      const result = await orchestrator.executeDebate(task, {
        generator: 'generator',
        critic: 'critic',
        maxRounds: 1,
      });

      expect(result.rounds.length).toBeGreaterThan(0);
    });

    it('should parse issues with line numbers', async () => {
      criticEditor.edit = async () => ({
        success: true,
        file: 'test.ts',
        diff: { file: 'test.ts', hunks: [], additions: 0, deletions: 0 },
        result: 'Line 42: Error - undefined variable\nL15: Bug in condition',
      });

      const task: CoworkTask = {
        id: 'parse-lines',
        type: 'code-edit',
        executor: 'generator',
        input: { files: ['test.ts'], instruction: 'Fix' },
        status: 'pending',
        createdAt: Date.now(),
      };

      const result = await orchestrator.executeDebate(task, {
        generator: 'generator',
        critic: 'critic',
        maxRounds: 1,
      });

      expect(result.rounds.length).toBeGreaterThan(0);
    });

    it('should handle empty critic output', async () => {
      criticEditor.edit = async () => ({
        success: true,
        file: 'test.ts',
        diff: { file: 'test.ts', hunks: [], additions: 0, deletions: 0 },
        result: '',
      });

      const task: CoworkTask = {
        id: 'parse-empty',
        type: 'code-edit',
        executor: 'generator',
        input: { files: ['test.ts'], instruction: 'Check' },
        status: 'pending',
        createdAt: Date.now(),
      };

      const result = await orchestrator.executeDebate(task, {
        generator: 'generator',
        critic: 'critic',
        maxRounds: 1,
      });

      // 空输出应该导致收敛（无问题）
      expect(result.converged).toBe(true);
    });
  });
});
