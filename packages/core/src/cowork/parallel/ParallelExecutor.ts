/**
 * ParallelExecutor - 多 Agent 并行执行器
 * 在独立的 Git Worktree 中并行执行多个 Agent 任务
 */

import { EventEmitter } from 'events';
import { WorktreeManager, WorktreeInfo } from '../../git/WorktreeManager.js';
import {
  AgentRuntimeLike,
  CoworkTask,
  CoworkTaskStatus,
  ExecutionResult,
  ExecutorCapabilities,
  ExecutorRegistration,
  ICodeEditor,
} from '../types.js';
import { AgentRuntime } from '../runtime.js';
import { AiderCodeEditor } from '../editors/AiderCodeEditor.js';
import { ClaudeCodeEditor } from '../editors/ClaudeCodeEditor.js';
import { CodexCodeEditor } from '../editors/CodexCodeEditor.js';
import { GeminiCodeEditor } from '../editors/GeminiCodeEditor.js';
import { AiderAdapter } from '../adapters/AiderAdapter.js';
import { ClaudeAdapter } from '../../adapters/ClaudeAdapter.js';

/**
 * Agent Worker 状态
 */
export type AgentWorkerStatus = 'idle' | 'running' | 'completed' | 'failed' | 'cancelled';

/**
 * Agent Worker 信息
 */
export interface AgentWorker {
  id: string;
  name: string;
  modelId: string;
  worktree?: WorktreeInfo;
  status: AgentWorkerStatus;
  task?: CoworkTask;
  result?: ExecutionResult;
  startedAt?: number;
  completedAt?: number;
  error?: Error;
}

/**
 * 并行执行配置
 */
export interface ParallelExecutorConfig {
  maxWorkers: number;
  worktreePrefix: string;
  timeout: number;
  failFast: boolean;
  cleanupOnComplete: boolean;
}

/**
 * 并行执行结果
 */
export interface ParallelExecutionResult {
  success: boolean;
  workers: AgentWorker[];
  results: ExecutionResult[];
  duration: number;
  errors: Error[];
}

/**
 * 并行执行器事件
 */
export interface ParallelExecutorEvents {
  'worker:created': (worker: AgentWorker) => void;
  'worker:started': (worker: AgentWorker) => void;
  'worker:progress': (worker: AgentWorker, progress: number) => void;
  'worker:completed': (worker: AgentWorker) => void;
  'worker:failed': (worker: AgentWorker, error: Error) => void;
  'worker:cancelled': (worker: AgentWorker) => void;
  'execution:started': (workers: AgentWorker[]) => void;
  'execution:completed': (result: ParallelExecutionResult) => void;
  'execution:failed': (error: Error) => void;
}

/**
 * 默认配置
 */
const DEFAULT_CONFIG: ParallelExecutorConfig = {
  maxWorkers: 5,
  worktreePrefix: 'parallel-worker',
  timeout: 300000, // 5 minutes
  failFast: false,
  cleanupOnComplete: true,
};

/**
 * 执行器注册信息
 */
/**
 * ParallelExecutor - 多 Agent 并行执行器
 */
export class ParallelExecutor extends EventEmitter {
  private config: ParallelExecutorConfig;
  private worktreeManager: WorktreeManager;
  private workers: Map<string, AgentWorker> = new Map();
  private runtime: AgentRuntimeLike;
  private isRunning: boolean = false;
  private abortController?: AbortController;

  constructor(
    worktreeManager: WorktreeManager,
    config: Partial<ParallelExecutorConfig> = {},
    runtime?: AgentRuntimeLike
  ) {
    super();
    this.worktreeManager = worktreeManager;
    this.config = { ...DEFAULT_CONFIG, ...config };
    this.runtime = runtime || new AgentRuntime();
  }

  private cloneEditorForCwd(editor: ICodeEditor, cwd: string): ICodeEditor {
    if (editor instanceof AiderCodeEditor) {
      return new AiderCodeEditor((editor as any).adapter as AiderAdapter, {
        ...(editor as any).config,
        cwd,
      });
    }

    if (editor instanceof ClaudeCodeEditor) {
      return new ClaudeCodeEditor((editor as any).adapter as ClaudeAdapter, {
        ...(editor as any).config,
        cwd,
      });
    }

    if (editor instanceof CodexCodeEditor) {
      return new CodexCodeEditor(editor.getAdapter(), {
        ...editor.getConfig(),
        cwd,
      });
    }

    if (editor instanceof GeminiCodeEditor) {
      return new GeminiCodeEditor(editor.getAdapter(), {
        ...editor.getConfig(),
        cwd,
      });
    }

    return editor;
  }

  private createSandboxedExecutor(
    executor: ExecutorRegistration,
    worktree: WorktreeInfo
  ): ExecutorRegistration {
    return {
      ...executor,
      editor: this.cloneEditorForCwd(executor.editor, worktree.path),
    };
  }

  /**
   * 注册执行器
   */
  registerExecutor(
    name: string,
    editor: ICodeEditor,
    capabilities: ExecutorCapabilities,
    modelId: string
  ): void {
    this.runtime.registerExecutor(name, editor, capabilities, modelId);
  }

  /**
   * 获取执行器
   */
  getExecutor(name: string): ExecutorRegistration | undefined {
    return this.runtime.getExecutor(name);
  }

  /**
   * 获取所有执行器
   */
  getAllExecutors(): ExecutorRegistration[] {
    return this.runtime.getAllExecutors();
  }

  /**
   * 创建 Worker
   */
  async createWorker(
    name: string,
    modelId: string,
    task: CoworkTask
  ): Promise<AgentWorker> {
    const workerId = `${this.config.worktreePrefix}-${name}-${Date.now()}`;

    // 创建 worktree
    const worktree = await this.worktreeManager.createWorktree(workerId, {
      createBranch: true,
    });

    const worker: AgentWorker = {
      id: workerId,
      name,
      modelId,
      worktree,
      status: 'idle',
      task,
    };

    this.workers.set(workerId, worker);
    this.emit('worker:created', worker);

    return worker;
  }

  /**
   * 启动 Worker
   */
  private async startWorker(worker: AgentWorker): Promise<ExecutionResult> {
    worker.status = 'running';
    worker.startedAt = Date.now();
    this.emit('worker:started', worker);

    try {
      const executor = this.runtime.getExecutor(worker.name);
      if (!executor) {
        throw new Error(`Executor not found: ${worker.name}`);
      }

      if (!worker.task) {
        throw new Error(`No task assigned to worker: ${worker.id}`);
      }

      // 在 worktree 中执行任务
      const result = await this.executeTaskInWorktree(
        executor,
        worker.task,
        worker.worktree!
      );

      worker.status = 'completed';
      worker.completedAt = Date.now();
      worker.result = result;
      this.emit('worker:completed', worker);

      return result;
    } catch (error) {
      const err = error instanceof Error ? error : new Error(String(error));
      worker.status = 'failed';
      worker.completedAt = Date.now();
      worker.error = err;
      this.emit('worker:failed', worker, err);

      return {
        taskId: worker.task?.id || worker.id,
        status: 'failed',
        output: { error: err.message },
        executor: worker.name,
        success: false,
        error: err.message,
        duration: Date.now() - (worker.startedAt || Date.now()),
      };
    }
  }

  /**
   * 在 Worktree 中执行任务
   */
  private async executeTaskInWorktree(
    executor: ExecutorRegistration,
    task: CoworkTask,
    worktree: WorktreeInfo
  ): Promise<ExecutionResult> {
    const sandboxedExecutor = this.createSandboxedExecutor(executor, worktree);
    return this.runtime.executeTask(task, {
      cwd: worktree.path,
      worktreePath: worktree.path,
      executorOverride: sandboxedExecutor,
    });
  }

  /**
   * 并行执行多个任务
   */
  async executeParallel(
    tasks: Array<{ executorName: string; task: CoworkTask }>
  ): Promise<ParallelExecutionResult> {
    if (this.isRunning) {
      throw new Error('Parallel execution already in progress');
    }

    if (tasks.length > this.config.maxWorkers) {
      throw new Error(
        `Too many tasks (${tasks.length}), max workers: ${this.config.maxWorkers}`
      );
    }

    this.isRunning = true;
    this.abortController = new AbortController();
    const startTime = Date.now();
    const errors: Error[] = [];

    try {
      // 创建所有 workers
      const workers: AgentWorker[] = [];
      for (const { executorName, task } of tasks) {
        const executor = this.runtime.getExecutor(executorName);
        if (!executor) {
          throw new Error(`Executor not found: ${executorName}`);
        }
        const worker = await this.createWorker(
          executorName,
          executor.modelId || executorName,
          task
        );
        workers.push(worker);
      }

      this.emit('execution:started', workers);

      // 并行执行所有 workers
      const resultPromises = workers.map(worker =>
        this.startWorkerWithTimeout(worker)
      );

      // 等待所有结果
      const results = await Promise.all(resultPromises);

      // 收集错误
      for (const worker of workers) {
        if (worker.error) {
          errors.push(worker.error);
        }
      }

      const result: ParallelExecutionResult = {
        success: errors.length === 0,
        workers,
        results,
        duration: Date.now() - startTime,
        errors,
      };

      this.emit('execution:completed', result);

      // 清理 worktrees
      if (this.config.cleanupOnComplete) {
        await this.cleanup();
      }

      return result;
    } catch (error) {
      const err = error instanceof Error ? error : new Error(String(error));
      this.emit('execution:failed', err);
      throw err;
    } finally {
      this.isRunning = false;
      this.abortController = undefined;
    }
  }

  /**
   * 带超时的 Worker 启动
   */
  private async startWorkerWithTimeout(
    worker: AgentWorker
  ): Promise<ExecutionResult> {
    return new Promise<ExecutionResult>((resolve, reject) => {
      const timeoutId = setTimeout(() => {
        worker.status = 'failed';
        worker.error = new Error('Worker timeout');
        this.emit('worker:failed', worker, worker.error);
        resolve({
          taskId: worker.task?.id || worker.id,
          status: 'failed',
          output: { error: 'Worker timeout' },
          executor: worker.name,
          success: false,
          error: 'Worker timeout',
          duration: this.config.timeout,
        });
      }, this.config.timeout);

      this.startWorker(worker)
        .then(result => {
          clearTimeout(timeoutId);
          resolve(result);
        })
        .catch(error => {
          clearTimeout(timeoutId);
          if (this.config.failFast) {
            reject(error);
          } else {
            resolve({
              taskId: worker.task?.id || worker.id,
              status: 'failed',
              output: { error: error.message },
              executor: worker.name,
              success: false,
              error: error.message,
              duration: Date.now() - (worker.startedAt || Date.now()),
            });
          }
        });
    });
  }

  /**
   * 取消执行
   */
  async cancel(): Promise<void> {
    if (!this.isRunning) return;

    this.abortController?.abort();

    for (const worker of this.workers.values()) {
      if (worker.status === 'running') {
        worker.status = 'cancelled';
        worker.completedAt = Date.now();
        this.emit('worker:cancelled', worker);
      }
    }

    await this.cleanup();
  }

  /**
   * 清理所有 worktrees
   */
  async cleanup(): Promise<void> {
    for (const worker of this.workers.values()) {
      if (worker.worktree) {
        try {
          await this.worktreeManager.removeWorktree(worker.worktree.path, true);
        } catch {
          // Ignore cleanup errors
        }
      }
    }
    this.workers.clear();
  }

  /**
   * 获取所有 Workers
   */
  getWorkers(): AgentWorker[] {
    return Array.from(this.workers.values());
  }

  /**
   * 获取 Worker
   */
  getWorker(id: string): AgentWorker | undefined {
    return this.workers.get(id);
  }

  /**
   * 获取运行中的 Workers
   */
  getRunningWorkers(): AgentWorker[] {
    return Array.from(this.workers.values()).filter(
      w => w.status === 'running'
    );
  }

  /**
   * 获取已完成的 Workers
   */
  getCompletedWorkers(): AgentWorker[] {
    return Array.from(this.workers.values()).filter(
      w => w.status === 'completed'
    );
  }

  /**
   * 获取失败的 Workers
   */
  getFailedWorkers(): AgentWorker[] {
    return Array.from(this.workers.values()).filter(w => w.status === 'failed');
  }

  /**
   * 是否正在运行
   */
  isExecuting(): boolean {
    return this.isRunning;
  }

  /**
   * 获取配置
   */
  getConfig(): ParallelExecutorConfig {
    return { ...this.config };
  }

  /**
   * 更新配置
   */
  updateConfig(config: Partial<ParallelExecutorConfig>): void {
    if (this.isRunning) {
      throw new Error('Cannot update config while executing');
    }
    this.config = { ...this.config, ...config };
  }
}
