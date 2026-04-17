import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import * as path from 'path';
import * as fs from 'fs';
import * as os from 'os';
import { execSync } from 'child_process';
import { WorktreeManager } from '../../../git/WorktreeManager.js';
import {
  ParallelExecutor,
  AgentWorker,
  ParallelExecutionResult,
} from '../ParallelExecutor.js';
import { ResultCollector } from '../ResultCollector.js';
import { WorkerPool } from '../WorkerPool.js';
import { CodexCodeEditor } from '../../editors/CodexCodeEditor.js';
import { CodexCLIAdapter } from '../../adapters/CodexCLIAdapter.js';
import {
  CoworkTask,
  ICodeEditor,
  ExecutorCapabilities,
  EditResult,
  Diff,
} from '../../types.js';

// Mock ICodeEditor
const createMockEditor = (name: string): ICodeEditor => ({
  name,
  edit: vi.fn().mockResolvedValue({
    success: true,
    file: 'test.ts',
    diff: {
      file: 'test.ts',
      hunks: [],
      additions: 5,
      deletions: 2,
    },
  } as EditResult),
  editMultiple: vi.fn().mockResolvedValue([
    {
      success: true,
      file: 'test.ts',
      diff: {
        file: 'test.ts',
        hunks: [],
        additions: 5,
        deletions: 2,
      },
    },
  ] as EditResult[]),
  preview: vi.fn().mockResolvedValue({
    file: 'test.ts',
    hunks: [],
    additions: 5,
    deletions: 2,
  } as Diff),
  applyDiff: vi.fn().mockResolvedValue(undefined),
  undo: vi.fn().mockResolvedValue(undefined),
});

// Mock ExecutorCapabilities
const createMockCapabilities = (name: string): ExecutorCapabilities => ({
  name,
  supportedTypes: ['code-edit', 'refactor'],
  maxConcurrency: 5,
  estimatedSpeed: 'fast',
  features: {
    streaming: true,
    multiFile: true,
    contextAware: true,
    codeReview: true,
  },
});

// Create mock task
const createMockTask = (id: string): CoworkTask => ({
  id,
  type: 'code-edit',
  executor: 'claude',
  input: {
    files: ['test.ts'],
    instruction: 'Add a new function',
  },
  status: 'pending',
  createdAt: Date.now(),
});

describe('ParallelExecutor', () => {
  let testDir: string;
  let worktreeManager: WorktreeManager;
  let executor: ParallelExecutor;

  beforeEach(async () => {
    // 创建临时测试目录
    testDir = path.join(os.tmpdir(), `parallel-test-${Date.now()}`);
    fs.mkdirSync(testDir, { recursive: true });

    // 初始化 Git 仓库
    execSync('git init', { cwd: testDir });
    execSync('git config user.email "test@test.com"', { cwd: testDir });
    execSync('git config user.name "Test User"', { cwd: testDir });
    fs.writeFileSync(path.join(testDir, 'README.md'), '# Test');
    execSync('git add .', { cwd: testDir });
    execSync('git commit -m "Initial commit"', { cwd: testDir });

    // 创建 WorktreeManager
    worktreeManager = new WorktreeManager(testDir);
    await worktreeManager.initialize();

    // 创建 ParallelExecutor
    executor = new ParallelExecutor(worktreeManager, {
      maxWorkers: 3,
      timeout: 10000,
      cleanupOnComplete: true,
    });

    // 注册执行器
    executor.registerExecutor(
      'claude',
      createMockEditor('claude'),
      createMockCapabilities('claude'),
      'claude-opus-4'
    );
    executor.registerExecutor(
      'gemini',
      createMockEditor('gemini'),
      createMockCapabilities('gemini'),
      'gemini-2.5-pro'
    );
  });

  afterEach(async () => {
    // 清理
    try {
      await executor.cleanup();
      await worktreeManager.removeAllWorktrees(true);
    } catch {
      // Ignore cleanup errors
    }

    try {
      fs.rmSync(testDir, { recursive: true, force: true });
    } catch {
      // Ignore cleanup errors
    }
  });

  describe('registerExecutor', () => {
    it('should register executor', () => {
      const exec = executor.getExecutor('claude');
      expect(exec).toBeDefined();
      expect(exec?.name).toBe('claude');
      expect(exec?.modelId).toBe('claude-opus-4');
    });

    it('should return all executors', () => {
      const executors = executor.getAllExecutors();
      expect(executors.length).toBe(2);
    });
  });

  describe('createWorker', () => {
    it('should create worker with worktree', async () => {
      const task = createMockTask('task-1');
      const worker = await executor.createWorker('claude', 'claude-opus-4', task);

      expect(worker).toBeDefined();
      expect(worker.id).toContain('parallel-worker');
      expect(worker.name).toBe('claude');
      expect(worker.modelId).toBe('claude-opus-4');
      expect(worker.worktree).toBeDefined();
      expect(worker.status).toBe('idle');
    });

    it('should emit worker:created event', async () => {
      const listener = vi.fn();
      executor.on('worker:created', listener);

      const task = createMockTask('task-1');
      await executor.createWorker('claude', 'claude-opus-4', task);

      expect(listener).toHaveBeenCalled();
    });
  });

  describe('executeParallel', () => {
    it('should execute tasks in parallel', async () => {
      const tasks = [
        { executorName: 'claude', task: createMockTask('task-1') },
        { executorName: 'gemini', task: createMockTask('task-2') },
      ];

      const result = await executor.executeParallel(tasks);

      expect(result.success).toBe(true);
      expect(result.workers.length).toBe(2);
      expect(result.results.length).toBe(2);
      expect(result.errors.length).toBe(0);
    });

    it('should emit execution events', async () => {
      const startedListener = vi.fn();
      const completedListener = vi.fn();
      executor.on('execution:started', startedListener);
      executor.on('execution:completed', completedListener);

      const tasks = [
        { executorName: 'claude', task: createMockTask('task-1') },
      ];

      await executor.executeParallel(tasks);

      expect(startedListener).toHaveBeenCalled();
      expect(completedListener).toHaveBeenCalled();
    });

    it('should throw error if too many tasks', async () => {
      const tasks = [
        { executorName: 'claude', task: createMockTask('task-1') },
        { executorName: 'claude', task: createMockTask('task-2') },
        { executorName: 'claude', task: createMockTask('task-3') },
        { executorName: 'claude', task: createMockTask('task-4') },
      ];

      await expect(executor.executeParallel(tasks)).rejects.toThrow('Too many tasks');
    });

    it('should throw error if executor not found', async () => {
      const tasks = [
        { executorName: 'unknown', task: createMockTask('task-1') },
      ];

      await expect(executor.executeParallel(tasks)).rejects.toThrow('Executor not found');
    });
  });

  describe('cancel', () => {
    it('should cancel running execution', async () => {
      const cancelledListener = vi.fn();
      executor.on('worker:cancelled', cancelledListener);

      // Start execution but don't await
      const tasks = [
        { executorName: 'claude', task: createMockTask('task-1') },
      ];
      const promise = executor.executeParallel(tasks);

      // Cancel immediately
      await executor.cancel();

      // Wait for execution to complete
      try {
        await promise;
      } catch {
        // May throw due to cancellation
      }
    });
  });

  describe('getWorkers', () => {
    it('should return all workers', async () => {
      const task = createMockTask('task-1');
      await executor.createWorker('claude', 'claude-opus-4', task);

      const workers = executor.getWorkers();
      expect(workers.length).toBe(1);
    });
  });

  describe('isExecuting', () => {
    it('should return false when not executing', () => {
      expect(executor.isExecuting()).toBe(false);
    });
  });

  describe('Codex editor cloning', () => {
    it('preserves Codex CLI adapter when cloning editor for worktree cwd', () => {
      const codexCliAdapter = new CodexCLIAdapter({ codexPath: 'codex' });
      const editor = new CodexCodeEditor(codexCliAdapter, {
        cwd: '/original-cwd',
        model: 'gpt-5-codex',
      });

      const clonedEditor = (executor as any).cloneEditorForCwd(editor, '/sandboxed-cwd') as CodexCodeEditor;

      expect(clonedEditor).toBeInstanceOf(CodexCodeEditor);
      expect(clonedEditor).not.toBe(editor);
      expect(clonedEditor.getAdapter()).toBe(codexCliAdapter);
      expect(clonedEditor.getConfig().cwd).toBe('/sandboxed-cwd');
      expect(clonedEditor.getConfig().model).toBe('gpt-5-codex');
    });
  });

  describe('config', () => {
    it('should return current config', () => {
      const config = executor.getConfig();

      expect(config.maxWorkers).toBe(3);
      expect(config.timeout).toBe(10000);
      expect(config.cleanupOnComplete).toBe(true);
      expect(config.worktreePrefix).toBe('parallel-worker');
      expect(config.failFast).toBe(false);
    });

    it('should update config when not executing', () => {
      executor.updateConfig({
        maxWorkers: 4,
        timeout: 5000,
        failFast: true,
      });

      const config = executor.getConfig();

      expect(config.maxWorkers).toBe(4);
      expect(config.timeout).toBe(5000);
      expect(config.failFast).toBe(true);
      expect(config.cleanupOnComplete).toBe(true);
    });
  });
});

describe('ResultCollector', () => {
  let collector: ResultCollector;

  beforeEach(() => {
    collector = new ResultCollector();
  });

  describe('addResult', () => {
    it('should add result', () => {
      collector.addResult('worker-1', {
        taskId: 'task-1',
        success: true,
        duration: 1000,
        diffs: [
          { file: 'test.ts', hunks: [], additions: 5, deletions: 2 },
        ],
      });

      expect(collector.size).toBe(1);
      expect(collector.getResult('worker-1')).toBeDefined();
    });

    it('should emit result:added event', () => {
      const listener = vi.fn();
      collector.on('result:added', listener);

      collector.addResult('worker-1', {
        taskId: 'task-1',
        success: true,
        duration: 1000,
      });

      expect(listener).toHaveBeenCalled();
    });
  });

  describe('getSummary', () => {
    it('should return correct summary', () => {
      collector.addResult('worker-1', {
        taskId: 'task-1',
        success: true,
        duration: 1000,
        diffs: [
          { file: 'test.ts', hunks: [], additions: 5, deletions: 2 },
        ],
      });
      collector.addResult('worker-2', {
        taskId: 'task-2',
        success: false,
        duration: 500,
        error: 'Failed',
      });

      const summary = collector.getSummary();

      expect(summary.totalWorkers).toBe(2);
      expect(summary.successCount).toBe(1);
      expect(summary.failedCount).toBe(1);
      expect(summary.totalDuration).toBe(1500);
      expect(summary.averageDuration).toBe(750);
    });
  });

  describe('getWorkerComparisons', () => {
    it('should return worker comparisons', () => {
      collector.addResult('worker-1', {
        taskId: 'task-1',
        success: true,
        duration: 1000,
        diffs: [
          { file: 'test.ts', hunks: [], additions: 5, deletions: 2 },
        ],
      });

      const comparisons = collector.getWorkerComparisons();

      expect(comparisons.length).toBe(1);
      expect(comparisons[0].workerId).toBe('worker-1');
      expect(comparisons[0].additions).toBe(5);
      expect(comparisons[0].deletions).toBe(2);
    });
  });

  describe('getBestWorker', () => {
    it('should return best worker', () => {
      collector.addResult('worker-1', {
        taskId: 'task-1',
        success: true,
        duration: 1000,
      });
      collector.addResult('worker-2', {
        taskId: 'task-2',
        success: true,
        duration: 500,
      });

      const best = collector.getBestWorker();

      expect(best).toBeDefined();
      expect(best?.workerId).toBe('worker-2');
    });

    it('should return undefined if no successful workers', () => {
      collector.addResult('worker-1', {
        taskId: 'task-1',
        success: false,
        duration: 1000,
      });

      const best = collector.getBestWorker();
      expect(best).toBeUndefined();
    });
  });

  describe('hasConflicts', () => {
    it('should detect conflicts', () => {
      collector.addResult('worker-1', {
        taskId: 'task-1',
        success: true,
        duration: 1000,
        diffs: [{ file: 'test.ts', hunks: [], additions: 5, deletions: 2 }],
      });
      collector.addResult('worker-2', {
        taskId: 'task-2',
        success: true,
        duration: 500,
        diffs: [{ file: 'test.ts', hunks: [], additions: 3, deletions: 1 }],
      });

      expect(collector.hasConflicts()).toBe(true);
    });

    it('should return false if no conflicts', () => {
      collector.addResult('worker-1', {
        taskId: 'task-1',
        success: true,
        duration: 1000,
        diffs: [{ file: 'test.ts', hunks: [], additions: 5, deletions: 2 }],
      });
      collector.addResult('worker-2', {
        taskId: 'task-2',
        success: true,
        duration: 500,
        diffs: [{ file: 'other.ts', hunks: [], additions: 3, deletions: 1 }],
      });

      expect(collector.hasConflicts()).toBe(false);
    });
  });

  describe('getConflictingFiles', () => {
    it('should return conflicting files', () => {
      collector.addResult('worker-1', {
        taskId: 'task-1',
        success: true,
        duration: 1000,
        diffs: [{ file: 'test.ts', hunks: [], additions: 5, deletions: 2 }],
      });
      collector.addResult('worker-2', {
        taskId: 'task-2',
        success: true,
        duration: 500,
        diffs: [{ file: 'test.ts', hunks: [], additions: 3, deletions: 1 }],
      });

      const conflicts = collector.getConflictingFiles();

      expect(conflicts).toContain('test.ts');
    });
  });
});

describe('WorkerPool', () => {
  let testDir: string;
  let worktreeManager: WorktreeManager;
  let pool: WorkerPool;

  beforeEach(async () => {
    // 创建临时测试目录
    testDir = path.join(os.tmpdir(), `pool-test-${Date.now()}`);
    fs.mkdirSync(testDir, { recursive: true });

    // 初始化 Git 仓库
    execSync('git init', { cwd: testDir });
    execSync('git config user.email "test@test.com"', { cwd: testDir });
    execSync('git config user.name "Test User"', { cwd: testDir });
    fs.writeFileSync(path.join(testDir, 'README.md'), '# Test');
    execSync('git add .', { cwd: testDir });
    execSync('git commit -m "Initial commit"', { cwd: testDir });

    // 创建 WorktreeManager
    worktreeManager = new WorktreeManager(testDir);
    await worktreeManager.initialize();

    // 创建 WorkerPool
    pool = new WorkerPool(worktreeManager, {
      maxSize: 3,
      minSize: 0,
      idleTimeout: 1000,
    });

    // 注册执行器
    pool.registerExecutor(
      'claude',
      createMockEditor('claude'),
      createMockCapabilities('claude'),
      'claude-opus-4'
    );
  });

  afterEach(async () => {
    // 清理
    try {
      await pool.drain();
      await worktreeManager.removeAllWorktrees(true);
    } catch {
      // Ignore cleanup errors
    }

    try {
      fs.rmSync(testDir, { recursive: true, force: true });
    } catch {
      // Ignore cleanup errors
    }
  });

  describe('acquire', () => {
    it('should acquire worker', async () => {
      const task = createMockTask('task-1');
      const worker = await pool.acquire('claude', task);

      expect(worker).toBeDefined();
      expect(worker.status).toBe('running');
      expect(pool.size).toBe(1);
    });

    it('should throw error if pool exhausted', async () => {
      const task1 = createMockTask('task-1');
      const task2 = createMockTask('task-2');
      const task3 = createMockTask('task-3');
      const task4 = createMockTask('task-4');

      await pool.acquire('claude', task1);
      await pool.acquire('claude', task2);
      await pool.acquire('claude', task3);

      await expect(pool.acquire('claude', task4)).rejects.toThrow('Worker pool exhausted');
    });
  });

  describe('release', () => {
    it('should release worker', async () => {
      const task = createMockTask('task-1');
      const worker = await pool.acquire('claude', task);

      pool.release(worker.id);

      expect(worker.status).toBe('idle');
    });
  });

  describe('getStats', () => {
    it('should return correct stats', async () => {
      const task = createMockTask('task-1');
      await pool.acquire('claude', task);

      const stats = pool.getStats();

      expect(stats.totalWorkers).toBe(1);
      expect(stats.runningWorkers).toBe(1);
      expect(stats.idleWorkers).toBe(0);
    });
  });

  describe('drain', () => {
    it('should drain pool', async () => {
      const task = createMockTask('task-1');
      await pool.acquire('claude', task);

      await pool.drain();

      expect(pool.size).toBe(0);
      expect(pool.isEmpty).toBe(true);
    });
  });

  describe('properties', () => {
    it('should return correct properties', async () => {
      expect(pool.isEmpty).toBe(true);
      expect(pool.isFull).toBe(false);
      expect(pool.availableCapacity).toBe(3);

      const task = createMockTask('task-1');
      await pool.acquire('claude', task);

      expect(pool.isEmpty).toBe(false);
      expect(pool.availableCapacity).toBe(2);
    });
  });
});
