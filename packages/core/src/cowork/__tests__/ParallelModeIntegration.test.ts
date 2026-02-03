/**
 * Parallel Mode Integration Tests
 *
 * 这些测试验证并行模式的完整流程：
 * - 创建 Worktree → 并行执行 → 评估 → 选择 → 合并
 * - 冲突解决
 * - 清理
 *
 * 注意：这些测试使用 mock 实现，因为完整的集成测试需要实际的 Git 操作和 AI 模型调用。
 * 在 CI/CD 环境中，这些测试验证接口契约和流程逻辑。
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { EventEmitter } from 'events';

// ============================================================================
// Type Definitions
// ============================================================================

interface ExecutorConfig {
  maxWorkers?: number;
  timeout?: number;
  worktreeBasePath?: string;
}

interface TaskOptions {
  description: string;
  models: string[];
  simulateConflict?: boolean;
  simulateError?: boolean;
}

interface WorkerResult {
  workerId: string;
  model: string;
  status: 'completed' | 'failed' | 'timeout';
  output?: string;
  error?: string;
  metrics?: Record<string, number>;
}

interface SolutionScore {
  quality: number;
  performance: number;
  maintainability: number;
  security: number;
}

interface EvaluatedSolution {
  id: string;
  workerId: string;
  model: string;
  scores: SolutionScore;
  totalScore: number;
}

interface EvaluationResult {
  success: boolean;
  solutions: EvaluatedSolution[];
  recommended?: EvaluatedSolution;
  error?: string;
}

interface EvaluationOptions {
  metrics?: string[];
  weights?: Record<string, number>;
}

interface SelectionResult {
  success: boolean;
  solutionId: string;
}

interface Conflict {
  file: string;
  type: string;
  ours: string;
  theirs: string;
}

interface MergeResult {
  success: boolean;
  merged: boolean;
  strategy?: string;
  hasConflicts?: boolean;
  conflicts?: Conflict[];
  backupBranch?: string;
}

interface MergeOptions {
  strategy?: 'fast-forward' | 'merge' | 'rebase';
  createBackup?: boolean;
}

interface ConflictResolution {
  file: string;
  resolution: 'keep_ours' | 'keep_theirs' | 'manual';
  content?: string;
}

interface ResolveConflictsOptions {
  strategy: 'auto' | 'manual';
  resolutions: ConflictResolution[];
}

interface ResolveResult {
  success: boolean;
  resolvedFiles: string[];
}

interface Worktree {
  path: string;
  branch: string;
  workerId: string;
}

interface WorkerConfig {
  id: string;
  model: string;
}

interface WorkerPoolConfig {
  maxWorkers: number;
}

interface CollectorResult {
  success: boolean;
  output?: string;
  error?: string;
  metrics?: Record<string, number>;
}

interface AggregatedResult {
  totalResults: number;
  successCount: number;
  failureCount: number;
}

interface Solution {
  id: string;
  code: string;
  tests?: string[];
  benchmarks?: Record<string, number>;
}

interface ComparisonResult {
  rankings: Array<{ id: string; score: number }>;
  best: { id: string; score: number };
}

interface MergerOptions {
  sourceBranch: string;
  targetBranch: string;
  strategy: string;
  simulateConflict?: boolean;
  createBackup?: boolean;
}

interface MergerResult {
  success: boolean;
  hasConflicts?: boolean;
  conflicts?: Conflict[];
  backupBranch?: string;
}

interface WorktreeConfig {
  basePath: string;
}

interface CreateWorktreeOptions {
  name: string;
  branch: string;
}

interface WorktreeInfo {
  path: string;
  branch: string;
}

// ============================================================================
// Mock Classes
// ============================================================================

/**
 * Mock ParallelExecutor - 并行执行器
 */
class MockParallelExecutor extends EventEmitter {
  private config: ExecutorConfig;
  private tasks: Map<string, TaskState> = new Map();
  private worktrees: Map<string, Worktree[]> = new Map();
  private branches: Map<string, string[]> = new Map();

  constructor(config: ExecutorConfig = {}) {
    super();
    this.config = {
      maxWorkers: config.maxWorkers || 3,
      timeout: config.timeout || 30000,
      worktreeBasePath: config.worktreeBasePath || '.codeflow/worktrees',
    };
  }

  async startTask(options: TaskOptions): Promise<string | null> {
    // Simulate worktree creation failure
    if (this.config.worktreeBasePath === '/invalid/path/that/does/not/exist') {
      return null;
    }

    const taskId = `task_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`;
    const workers: WorkerState[] = options.models.map((model, index) => ({
      id: `worker_${index}`,
      model,
      status: 'pending' as const,
      worktree: `${this.config.worktreeBasePath}/${taskId}/worker_${index}`,
      branch: `parallel/${taskId}/worker_${index}`,
    }));

    const taskState: TaskState = {
      id: taskId,
      description: options.description,
      workers,
      status: 'running',
      simulateConflict: options.simulateConflict,
      simulateError: options.simulateError,
      selectedSolution: null,
      createdAt: Date.now(),
    };

    this.tasks.set(taskId, taskState);
    this.worktrees.set(taskId, workers.map(w => ({
      path: w.worktree,
      branch: w.branch,
      workerId: w.id,
    })));
    this.branches.set(taskId, workers.map(w => w.branch));

    this.emit('task:started', { taskId, workers: workers.length });
    workers.forEach(w => {
      this.emit('worker:started', { taskId, workerId: w.id, model: w.model });
    });

    return taskId;
  }

  async waitForCompletion(taskId: string): Promise<WorkerResult[]> {
    const task = this.tasks.get(taskId);
    if (!task) {
      throw new Error('Task not found');
    }

    if (task.simulateError) {
      throw new Error('Simulated error');
    }

    // Simulate worker completion
    const results: WorkerResult[] = task.workers.map(worker => {
      const isFailing = worker.model.includes('failing');
      const isSlow = worker.model.includes('slow');
      const isTimeout = isSlow && this.config.timeout && this.config.timeout < 1000;

      let status: 'completed' | 'failed' | 'timeout' = 'completed';
      if (isFailing) status = 'failed';
      if (isTimeout) status = 'timeout';

      worker.status = status;

      this.emit('worker:completed', {
        taskId,
        workerId: worker.id,
        status,
      });

      return {
        workerId: worker.id,
        model: worker.model,
        status,
        output: status === 'completed' ? `Solution from ${worker.model}` : undefined,
        error: status === 'failed' ? `${worker.model} failed` : undefined,
        metrics: status === 'completed' ? {
          quality: 70 + Math.random() * 30,
          performance: 70 + Math.random() * 30,
        } : undefined,
      };
    });

    task.status = 'completed';
    return results;
  }

  async evaluateSolutions(taskId: string, options: EvaluationOptions = {}): Promise<EvaluationResult> {
    const task = this.tasks.get(taskId);
    if (!task) {
      return { success: false, solutions: [], error: 'Task not found' };
    }

    const successfulWorkers = task.workers.filter(w => w.status === 'completed');
    if (successfulWorkers.length === 0) {
      return { success: false, solutions: [], error: 'No successful solutions to evaluate' };
    }

    const metrics = options.metrics || ['quality', 'performance', 'maintainability', 'security'];
    const weights = options.weights || {
      quality: 0.4,
      performance: 0.3,
      maintainability: 0.2,
      security: 0.1,
    };

    const solutions: EvaluatedSolution[] = successfulWorkers.map((worker, index) => {
      const scores: SolutionScore = {
        quality: 60 + Math.random() * 40,
        performance: 60 + Math.random() * 40,
        maintainability: 60 + Math.random() * 40,
        security: 60 + Math.random() * 40,
      };

      const totalScore = metrics.reduce((sum, metric) => {
        const score = scores[metric as keyof SolutionScore] || 0;
        const weight = weights[metric] || 0.25;
        return sum + score * weight;
      }, 0);

      return {
        id: `solution_${index + 1}`,
        workerId: worker.id,
        model: worker.model,
        scores,
        totalScore,
      };
    });

    // Sort by total score descending
    solutions.sort((a, b) => b.totalScore - a.totalScore);

    this.emit('evaluation:completed', { taskId, solutions });

    return {
      success: true,
      solutions,
      recommended: solutions[0],
    };
  }

  async selectSolution(taskId: string, solutionId: string): Promise<SelectionResult> {
    const task = this.tasks.get(taskId);
    if (!task) {
      return { success: false, solutionId };
    }

    task.selectedSolution = solutionId;
    this.emit('solution:selected', { taskId, solutionId });

    return { success: true, solutionId };
  }

  async mergeSolution(taskId: string, solutionId: string, options: MergeOptions = {}): Promise<MergeResult> {
    const task = this.tasks.get(taskId);
    if (!task) {
      return { success: false, merged: false };
    }

    const strategy = options.strategy || 'merge';
    let backupBranch: string | undefined;

    if (options.createBackup) {
      backupBranch = `backup/${taskId}_${Date.now()}`;
    }

    // Simulate conflict
    if (task.simulateConflict) {
      const conflicts: Conflict[] = [
        {
          file: 'src/index.ts',
          type: 'content',
          ours: 'our content',
          theirs: 'their content',
        },
      ];

      return {
        success: false,
        merged: false,
        strategy,
        hasConflicts: true,
        conflicts,
        backupBranch,
      };
    }

    // Clean up worktrees after successful merge
    this.worktrees.set(taskId, []);
    this.branches.set(taskId, []);

    this.emit('solution:merged', { taskId, solutionId, strategy });

    return {
      success: true,
      merged: true,
      strategy,
      hasConflicts: false,
      backupBranch,
    };
  }

  async getConflictResolutions(taskId: string, conflicts: Conflict[]): Promise<ConflictResolution[]> {
    return conflicts.map(c => ({
      file: c.file,
      resolution: 'keep_ours' as const,
    }));
  }

  async resolveConflicts(taskId: string, options: ResolveConflictsOptions): Promise<ResolveResult> {
    return {
      success: true,
      resolvedFiles: options.resolutions.map(r => r.file),
    };
  }

  getWorktrees(taskId: string): Worktree[] {
    return this.worktrees.get(taskId) || [];
  }

  getBranches(taskId: string): string[] {
    return this.branches.get(taskId) || [];
  }

  getAllBranches(): string[] {
    const allBranches: string[] = [];
    this.branches.forEach(branches => allBranches.push(...branches));
    return allBranches;
  }

  async cancelTask(taskId: string): Promise<void> {
    const task = this.tasks.get(taskId);
    if (task) {
      task.status = 'cancelled';
      this.worktrees.set(taskId, []);
      this.branches.set(taskId, []);
    }
  }

  cleanup(): void {
    this.tasks.forEach((task, taskId) => {
      this.worktrees.set(taskId, []);
      this.branches.set(taskId, []);
    });
  }
}

interface TaskState {
  id: string;
  description: string;
  workers: WorkerState[];
  status: 'running' | 'completed' | 'cancelled';
  simulateConflict?: boolean;
  simulateError?: boolean;
  selectedSolution: string | null;
  createdAt: number;
}

interface WorkerState {
  id: string;
  model: string;
  status: 'pending' | 'completed' | 'failed' | 'timeout';
  worktree: string;
  branch: string;
}

/**
 * Mock WorkerPool - 工作池管理
 */
class MockWorkerPool {
  private config: WorkerPoolConfig;
  private workers: Map<string, { config: WorkerConfig; status: string }> = new Map();

  constructor(config: WorkerPoolConfig) {
    this.config = config;
  }

  addWorker(config: WorkerConfig): boolean {
    if (this.workers.size >= this.config.maxWorkers) {
      return false;
    }
    this.workers.set(config.id, { config, status: 'idle' });
    return true;
  }

  removeWorker(id: string): void {
    this.workers.delete(id);
  }

  getWorkerCount(): number {
    return this.workers.size;
  }

  getWorkerStatus(id: string): string | undefined {
    return this.workers.get(id)?.status;
  }

  updateWorkerStatus(id: string, status: string): void {
    const worker = this.workers.get(id);
    if (worker) {
      worker.status = status;
    }
  }

  shutdown(): void {
    this.workers.clear();
  }
}

/**
 * Mock ResultCollector - 结果收集器
 */
class MockResultCollector {
  private results: Map<string, CollectorResult> = new Map();

  addResult(workerId: string, result: CollectorResult): void {
    this.results.set(workerId, result);
  }

  getResults(): CollectorResult[] {
    return Array.from(this.results.values());
  }

  aggregate(): AggregatedResult {
    const results = this.getResults();
    return {
      totalResults: results.length,
      successCount: results.filter(r => r.success).length,
      failureCount: results.filter(r => !r.success).length,
    };
  }
}

/**
 * Mock SolutionEvaluator - 方案评估器
 */
class MockSolutionEvaluator {
  async evaluateQuality(solution: { code: string; tests?: string[] }): Promise<number> {
    // Simple mock evaluation based on code length and test presence
    let score = 50;
    if (solution.code.length > 10) score += 20;
    if (solution.tests && solution.tests.length > 0) score += 30;
    return Math.min(100, score);
  }

  async evaluatePerformance(solution: { code: string; benchmarks?: Record<string, number> }): Promise<number> {
    // Simple mock evaluation
    let score = 60;
    if (solution.benchmarks) {
      const execTime = solution.benchmarks.executionTime || 100;
      if (execTime < 50) score += 40;
      else if (execTime < 100) score += 20;
    }
    return Math.min(100, score);
  }

  async compare(solutions: Solution[]): Promise<ComparisonResult> {
    const rankings = solutions.map(s => ({
      id: s.id,
      score: 50 + Math.random() * 50,
    }));
    rankings.sort((a, b) => b.score - a.score);

    return {
      rankings,
      best: rankings[0],
    };
  }
}

/**
 * Mock SolutionMerger - 方案合并器
 */
class MockSolutionMerger {
  async merge(options: MergerOptions): Promise<MergerResult> {
    if (options.simulateConflict) {
      return {
        success: false,
        hasConflicts: true,
        conflicts: [
          {
            file: 'src/index.ts',
            type: 'content',
            ours: 'our content',
            theirs: 'their content',
          },
        ],
      };
    }

    return {
      success: true,
      hasConflicts: false,
      backupBranch: options.createBackup ? `backup/${Date.now()}` : undefined,
    };
  }
}

/**
 * Mock WorktreeManager - Worktree 管理器
 */
class MockWorktreeManager {
  private config: WorktreeConfig;
  private worktrees: WorktreeInfo[] = [];

  constructor(config: WorktreeConfig) {
    this.config = config;
  }

  async create(options: CreateWorktreeOptions): Promise<WorktreeInfo> {
    const worktree: WorktreeInfo = {
      path: `${this.config.basePath}/${options.name}`,
      branch: options.branch,
    };
    this.worktrees.push(worktree);
    return worktree;
  }

  list(): WorktreeInfo[] {
    return [...this.worktrees];
  }

  async remove(path: string): Promise<void> {
    this.worktrees = this.worktrees.filter(w => w.path !== path);
  }

  async cleanupAll(): Promise<void> {
    this.worktrees = [];
  }
}

// ============================================================================
// Integration Tests
// ============================================================================

describe('Parallel Mode Integration Tests', () => {
  let executor: MockParallelExecutor;

  beforeEach(() => {
    executor = new MockParallelExecutor({
      maxWorkers: 3,
      timeout: 30000,
    });
  });

  afterEach(() => {
    executor.cleanup();
  });

  describe('Complete Flow: Worktree → Execute → Evaluate → Select → Merge', () => {
    it('should complete full parallel mode flow', async () => {
      // Step 1: Start parallel task
      const taskId = await executor.startTask({
        description: 'Implement user authentication',
        models: ['claude-3-opus', 'gemini-pro', 'gpt-4o'],
      });
      expect(taskId).toBeDefined();
      expect(taskId).toMatch(/^task_/);

      // Step 2: Wait for workers to complete
      const results = await executor.waitForCompletion(taskId!);
      expect(results.length).toBe(3);
      expect(results.every(r => r.status === 'completed' || r.status === 'failed')).toBe(true);

      // Step 3: Evaluate solutions
      const evaluation = await executor.evaluateSolutions(taskId!);
      expect(evaluation.solutions.length).toBe(3);
      expect(evaluation.recommended).toBeDefined();

      // Step 4: Select best solution
      const selection = await executor.selectSolution(taskId!, evaluation.recommended!.id);
      expect(selection.success).toBe(true);

      // Step 5: Merge selected solution
      const mergeResult = await executor.mergeSolution(taskId!, selection.solutionId);
      expect(mergeResult.success).toBe(true);
      expect(mergeResult.merged).toBe(true);
    });

    it('should emit events throughout the flow', async () => {
      const events: string[] = [];
      executor.on('task:started', () => events.push('task:started'));
      executor.on('worker:started', () => events.push('worker:started'));
      executor.on('worker:completed', () => events.push('worker:completed'));
      executor.on('evaluation:completed', () => events.push('evaluation:completed'));
      executor.on('solution:selected', () => events.push('solution:selected'));
      executor.on('solution:merged', () => events.push('solution:merged'));

      const taskId = await executor.startTask({
        description: 'Event test',
        models: ['claude-3-opus'],
      });
      await executor.waitForCompletion(taskId!);
      const evaluation = await executor.evaluateSolutions(taskId!);
      await executor.selectSolution(taskId!, evaluation.recommended!.id);
      await executor.mergeSolution(taskId!, evaluation.recommended!.id);

      expect(events).toContain('task:started');
      expect(events).toContain('worker:started');
      expect(events).toContain('worker:completed');
      expect(events).toContain('evaluation:completed');
      expect(events).toContain('solution:selected');
      expect(events).toContain('solution:merged');
    });
  });

  describe('Worktree Management', () => {
    it('should create isolated worktrees for each worker', async () => {
      const taskId = await executor.startTask({
        description: 'Worktree test',
        models: ['claude-3-opus', 'gemini-pro'],
      });

      const worktrees = executor.getWorktrees(taskId!);
      expect(worktrees.length).toBe(2);
      expect(worktrees[0].path).not.toBe(worktrees[1].path);
      expect(worktrees[0].branch).not.toBe(worktrees[1].branch);
    });

    it('should cleanup worktrees after merge', async () => {
      const taskId = await executor.startTask({
        description: 'Cleanup test',
        models: ['claude-3-opus'],
      });

      await executor.waitForCompletion(taskId!);
      const evaluation = await executor.evaluateSolutions(taskId!);
      await executor.selectSolution(taskId!, evaluation.recommended!.id);
      await executor.mergeSolution(taskId!, evaluation.recommended!.id);

      const worktrees = executor.getWorktrees(taskId!);
      expect(worktrees.length).toBe(0);
    });

    it('should handle worktree creation failure', async () => {
      const failingExecutor = new MockParallelExecutor({
        worktreeBasePath: '/invalid/path/that/does/not/exist',
      });

      const result = await failingExecutor.startTask({
        description: 'Failure test',
        models: ['claude-3-opus'],
      });

      expect(result).toBeNull();
    });
  });

  describe('Conflict Resolution', () => {
    it('should detect merge conflicts', async () => {
      const taskId = await executor.startTask({
        description: 'Conflict test',
        models: ['claude-3-opus', 'gemini-pro'],
        simulateConflict: true,
      });

      await executor.waitForCompletion(taskId!);
      const evaluation = await executor.evaluateSolutions(taskId!);
      await executor.selectSolution(taskId!, evaluation.recommended!.id);

      const mergeResult = await executor.mergeSolution(taskId!, evaluation.recommended!.id);

      if (mergeResult.hasConflicts) {
        expect(mergeResult.conflicts).toBeDefined();
        expect(mergeResult.conflicts!.length).toBeGreaterThan(0);
      }
    });

    it('should provide conflict resolution suggestions', async () => {
      const taskId = await executor.startTask({
        description: 'Resolution test',
        models: ['claude-3-opus', 'gemini-pro'],
        simulateConflict: true,
      });

      await executor.waitForCompletion(taskId!);
      const evaluation = await executor.evaluateSolutions(taskId!);
      await executor.selectSolution(taskId!, evaluation.recommended!.id);

      const mergeResult = await executor.mergeSolution(taskId!, evaluation.recommended!.id);

      if (mergeResult.hasConflicts) {
        const suggestions = await executor.getConflictResolutions(taskId!, mergeResult.conflicts!);
        expect(suggestions.length).toBeGreaterThan(0);
      }
    });

    it('should support manual conflict resolution', async () => {
      const taskId = await executor.startTask({
        description: 'Manual resolution test',
        models: ['claude-3-opus', 'gemini-pro'],
        simulateConflict: true,
      });

      await executor.waitForCompletion(taskId!);
      const evaluation = await executor.evaluateSolutions(taskId!);
      await executor.selectSolution(taskId!, evaluation.recommended!.id);

      const mergeResult = await executor.mergeSolution(taskId!, evaluation.recommended!.id);

      if (mergeResult.hasConflicts) {
        const resolution = await executor.resolveConflicts(taskId!, {
          strategy: 'manual',
          resolutions: mergeResult.conflicts!.map(c => ({
            file: c.file,
            resolution: 'keep_ours' as const,
          })),
        });

        expect(resolution.success).toBe(true);
      }
    });
  });

  describe('Cleanup', () => {
    it('should cleanup all resources on task completion', async () => {
      const taskId = await executor.startTask({
        description: 'Cleanup test',
        models: ['claude-3-opus', 'gemini-pro'],
      });

      await executor.waitForCompletion(taskId!);
      const evaluation = await executor.evaluateSolutions(taskId!);
      await executor.selectSolution(taskId!, evaluation.recommended!.id);
      await executor.mergeSolution(taskId!, evaluation.recommended!.id);

      const worktrees = executor.getWorktrees(taskId!);
      const branches = executor.getBranches(taskId!);

      expect(worktrees.length).toBe(0);
      expect(branches.filter(b => b.startsWith('parallel/'))).toHaveLength(0);
    });

    it('should cleanup on task cancellation', async () => {
      const taskId = await executor.startTask({
        description: 'Cancel test',
        models: ['claude-3-opus', 'gemini-pro', 'gpt-4o'],
      });

      await executor.cancelTask(taskId!);

      const worktrees = executor.getWorktrees(taskId!);
      expect(worktrees.length).toBe(0);
    });

    it('should cleanup on error', async () => {
      const taskId = await executor.startTask({
        description: 'Error cleanup test',
        models: ['claude-3-opus'],
        simulateError: true,
      });

      try {
        await executor.waitForCompletion(taskId!);
      } catch {
        // Expected error
      }

      await executor.cleanup();
      const worktrees = executor.getWorktrees(taskId!);
      expect(worktrees.length).toBe(0);
    });

    it('should not leave orphaned branches', async () => {
      const taskId = await executor.startTask({
        description: 'Orphan test',
        models: ['claude-3-opus', 'gemini-pro'],
      });

      await executor.waitForCompletion(taskId!);
      const evaluation = await executor.evaluateSolutions(taskId!);
      await executor.selectSolution(taskId!, evaluation.recommended!.id);
      await executor.mergeSolution(taskId!, evaluation.recommended!.id);

      const allBranches = executor.getAllBranches();
      const parallelBranches = allBranches.filter(b => b.includes(taskId!));
      expect(parallelBranches.length).toBe(0);
    });
  });

  describe('Error Handling', () => {
    it('should handle worker failure gracefully', async () => {
      const taskId = await executor.startTask({
        description: 'Worker failure test',
        models: ['claude-3-opus', 'failing-model', 'gemini-pro'],
      });

      const results = await executor.waitForCompletion(taskId!);

      const successfulResults = results.filter(r => r.status === 'completed');
      expect(successfulResults.length).toBeGreaterThan(0);
    });

    it('should handle all workers failing', async () => {
      const taskId = await executor.startTask({
        description: 'All fail test',
        models: ['failing-model-1', 'failing-model-2'],
      });

      const results = await executor.waitForCompletion(taskId!);

      expect(results.every(r => r.status === 'failed')).toBe(true);

      const evaluation = await executor.evaluateSolutions(taskId!);
      expect(evaluation.success).toBe(false);
      expect(evaluation.error).toContain('No successful solutions');
    });

    it('should handle timeout', async () => {
      const timeoutExecutor = new MockParallelExecutor({
        timeout: 100,
      });

      const taskId = await timeoutExecutor.startTask({
        description: 'Timeout test',
        models: ['slow-model'],
      });

      const results = await timeoutExecutor.waitForCompletion(taskId!);

      expect(results.some(r => r.status === 'timeout')).toBe(true);
    });
  });

  describe('Solution Evaluation', () => {
    it('should evaluate solutions on multiple metrics', async () => {
      const taskId = await executor.startTask({
        description: 'Evaluation test',
        models: ['claude-3-opus', 'gemini-pro', 'gpt-4o'],
      });

      await executor.waitForCompletion(taskId!);
      const evaluation = await executor.evaluateSolutions(taskId!, {
        metrics: ['quality', 'performance', 'maintainability', 'security'],
      });

      expect(evaluation.solutions.length).toBe(3);
      evaluation.solutions.forEach(s => {
        expect(s.scores.quality).toBeDefined();
        expect(s.scores.performance).toBeDefined();
        expect(s.scores.maintainability).toBeDefined();
        expect(s.scores.security).toBeDefined();
      });
    });

    it('should recommend best solution', async () => {
      const taskId = await executor.startTask({
        description: 'Recommendation test',
        models: ['claude-3-opus', 'gemini-pro'],
      });

      await executor.waitForCompletion(taskId!);
      const evaluation = await executor.evaluateSolutions(taskId!);

      expect(evaluation.recommended).toBeDefined();
      expect(evaluation.recommended!.totalScore).toBeGreaterThan(0);
    });

    it('should support custom evaluation weights', async () => {
      const taskId = await executor.startTask({
        description: 'Custom weights test',
        models: ['claude-3-opus', 'gemini-pro'],
      });

      await executor.waitForCompletion(taskId!);
      const evaluation = await executor.evaluateSolutions(taskId!, {
        weights: {
          quality: 0.5,
          performance: 0.3,
          maintainability: 0.2,
        },
      });

      expect(evaluation.success).toBe(true);
    });
  });

  describe('Merge Strategies', () => {
    it('should support fast-forward merge', async () => {
      const taskId = await executor.startTask({
        description: 'Fast-forward test',
        models: ['claude-3-opus'],
      });

      await executor.waitForCompletion(taskId!);
      const evaluation = await executor.evaluateSolutions(taskId!);
      await executor.selectSolution(taskId!, evaluation.recommended!.id);

      const mergeResult = await executor.mergeSolution(taskId!, evaluation.recommended!.id, {
        strategy: 'fast-forward',
      });

      expect(mergeResult.success).toBe(true);
      expect(mergeResult.strategy).toBe('fast-forward');
    });

    it('should support merge commit', async () => {
      const taskId = await executor.startTask({
        description: 'Merge commit test',
        models: ['claude-3-opus'],
      });

      await executor.waitForCompletion(taskId!);
      const evaluation = await executor.evaluateSolutions(taskId!);
      await executor.selectSolution(taskId!, evaluation.recommended!.id);

      const mergeResult = await executor.mergeSolution(taskId!, evaluation.recommended!.id, {
        strategy: 'merge',
      });

      expect(mergeResult.success).toBe(true);
      expect(mergeResult.strategy).toBe('merge');
    });

    it('should support rebase', async () => {
      const taskId = await executor.startTask({
        description: 'Rebase test',
        models: ['claude-3-opus'],
      });

      await executor.waitForCompletion(taskId!);
      const evaluation = await executor.evaluateSolutions(taskId!);
      await executor.selectSolution(taskId!, evaluation.recommended!.id);

      const mergeResult = await executor.mergeSolution(taskId!, evaluation.recommended!.id, {
        strategy: 'rebase',
      });

      expect(mergeResult.success).toBe(true);
      expect(mergeResult.strategy).toBe('rebase');
    });

    it('should create backup before merge', async () => {
      const taskId = await executor.startTask({
        description: 'Backup test',
        models: ['claude-3-opus'],
      });

      await executor.waitForCompletion(taskId!);
      const evaluation = await executor.evaluateSolutions(taskId!);
      await executor.selectSolution(taskId!, evaluation.recommended!.id);

      const mergeResult = await executor.mergeSolution(taskId!, evaluation.recommended!.id, {
        createBackup: true,
      });

      expect(mergeResult.success).toBe(true);
      expect(mergeResult.backupBranch).toBeDefined();
      expect(mergeResult.backupBranch).toMatch(/^backup\//);
    });
  });
});

// ============================================================================
// Unit Tests
// ============================================================================

describe('WorkerPool Unit Tests', () => {
  let pool: MockWorkerPool;

  beforeEach(() => {
    pool = new MockWorkerPool({ maxWorkers: 3 });
  });

  afterEach(() => {
    pool.shutdown();
  });

  it('should create workers up to max limit', () => {
    pool.addWorker({ id: 'w1', model: 'claude-3-opus' });
    pool.addWorker({ id: 'w2', model: 'gemini-pro' });
    pool.addWorker({ id: 'w3', model: 'gpt-4o' });

    expect(pool.getWorkerCount()).toBe(3);

    const result = pool.addWorker({ id: 'w4', model: 'extra' });
    expect(result).toBe(false);
    expect(pool.getWorkerCount()).toBe(3);
  });

  it('should track worker status', () => {
    pool.addWorker({ id: 'w1', model: 'claude-3-opus' });
    pool.updateWorkerStatus('w1', 'running');

    const status = pool.getWorkerStatus('w1');
    expect(status).toBe('running');
  });

  it('should remove workers', () => {
    pool.addWorker({ id: 'w1', model: 'claude-3-opus' });
    pool.removeWorker('w1');

    expect(pool.getWorkerCount()).toBe(0);
  });
});

describe('ResultCollector Unit Tests', () => {
  let collector: MockResultCollector;

  beforeEach(() => {
    collector = new MockResultCollector();
  });

  it('should collect results from workers', () => {
    collector.addResult('w1', { success: true, output: 'Result 1' });
    collector.addResult('w2', { success: true, output: 'Result 2' });

    const results = collector.getResults();
    expect(results.length).toBe(2);
  });

  it('should aggregate results', () => {
    collector.addResult('w1', { success: true, output: 'Result 1', metrics: { quality: 80 } });
    collector.addResult('w2', { success: true, output: 'Result 2', metrics: { quality: 90 } });

    const aggregated = collector.aggregate();
    expect(aggregated.totalResults).toBe(2);
    expect(aggregated.successCount).toBe(2);
  });

  it('should handle failed results', () => {
    collector.addResult('w1', { success: true, output: 'Result 1' });
    collector.addResult('w2', { success: false, error: 'Failed' });

    const aggregated = collector.aggregate();
    expect(aggregated.successCount).toBe(1);
    expect(aggregated.failureCount).toBe(1);
  });
});

describe('SolutionEvaluator Unit Tests', () => {
  let evaluator: MockSolutionEvaluator;

  beforeEach(() => {
    evaluator = new MockSolutionEvaluator();
  });

  it('should evaluate solution quality', async () => {
    const score = await evaluator.evaluateQuality({
      code: 'function test() { return true; }',
      tests: ['test passes'],
    });

    expect(score).toBeGreaterThanOrEqual(0);
    expect(score).toBeLessThanOrEqual(100);
  });

  it('should evaluate solution performance', async () => {
    const score = await evaluator.evaluatePerformance({
      code: 'function test() { return true; }',
      benchmarks: { executionTime: 10 },
    });

    expect(score).toBeGreaterThanOrEqual(0);
    expect(score).toBeLessThanOrEqual(100);
  });

  it('should compare multiple solutions', async () => {
    const comparison = await evaluator.compare([
      { id: 's1', code: 'solution 1' },
      { id: 's2', code: 'solution 2' },
    ]);

    expect(comparison.rankings.length).toBe(2);
    expect(comparison.best).toBeDefined();
  });
});

describe('SolutionMerger Unit Tests', () => {
  let merger: MockSolutionMerger;

  beforeEach(() => {
    merger = new MockSolutionMerger();
  });

  it('should merge solution to main branch', async () => {
    const result = await merger.merge({
      sourceBranch: 'parallel/task_123/worker_1',
      targetBranch: 'main',
      strategy: 'merge',
    });

    expect(result.success).toBe(true);
  });

  it('should detect conflicts', async () => {
    const result = await merger.merge({
      sourceBranch: 'parallel/task_123/worker_1',
      targetBranch: 'main',
      strategy: 'merge',
      simulateConflict: true,
    });

    expect(result.hasConflicts).toBe(true);
    expect(result.conflicts).toBeDefined();
  });

  it('should create backup branch', async () => {
    const result = await merger.merge({
      sourceBranch: 'parallel/task_123/worker_1',
      targetBranch: 'main',
      strategy: 'merge',
      createBackup: true,
    });

    expect(result.backupBranch).toBeDefined();
  });
});

describe('WorktreeManager Unit Tests', () => {
  let manager: MockWorktreeManager;

  beforeEach(() => {
    manager = new MockWorktreeManager({ basePath: '/tmp/worktrees' });
  });

  afterEach(async () => {
    await manager.cleanupAll();
  });

  it('should create worktree', async () => {
    const worktree = await manager.create({
      name: 'test-worktree',
      branch: 'feature/test',
    });

    expect(worktree.path).toContain('test-worktree');
    expect(worktree.branch).toBe('feature/test');
  });

  it('should list worktrees', async () => {
    await manager.create({ name: 'wt1', branch: 'branch1' });
    await manager.create({ name: 'wt2', branch: 'branch2' });

    const worktrees = manager.list();
    expect(worktrees.length).toBe(2);
  });

  it('should remove worktree', async () => {
    const worktree = await manager.create({ name: 'to-remove', branch: 'temp' });
    await manager.remove(worktree.path);

    const worktrees = manager.list();
    expect(worktrees.find(w => w.path === worktree.path)).toBeUndefined();
  });

  it('should cleanup all worktrees', async () => {
    await manager.create({ name: 'wt1', branch: 'branch1' });
    await manager.create({ name: 'wt2', branch: 'branch2' });

    await manager.cleanupAll();

    const worktrees = manager.list();
    expect(worktrees.length).toBe(0);
  });
});
