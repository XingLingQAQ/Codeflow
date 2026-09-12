/**
 * ParallelExecutor - 多 Agent 并行执行器
 * 在独立的 Git Worktree 中并行执行多个 Agent 任务
 */
import { EventEmitter } from 'events';
import { AgentRuntime } from '../runtime.js';
import { AiderCodeEditor } from '../editors/AiderCodeEditor.js';
import { ClaudeCodeEditor } from '../editors/ClaudeCodeEditor.js';
import { CodexCodeEditor } from '../editors/CodexCodeEditor.js';
import { GeminiCodeEditor } from '../editors/GeminiCodeEditor.js';
/**
 * 默认配置
 */
const DEFAULT_CONFIG = {
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
    constructor(worktreeManager, config = {}, runtime) {
        super();
        this.workers = new Map();
        this.isRunning = false;
        this.worktreeManager = worktreeManager;
        this.config = { ...DEFAULT_CONFIG, ...config };
        this.runtime = runtime || new AgentRuntime();
    }
    cloneEditorForCwd(editor, cwd) {
        if (editor instanceof AiderCodeEditor) {
            return new AiderCodeEditor(editor.getAdapter(), {
                ...editor.getConfig(),
                cwd,
            });
        }
        if (editor instanceof ClaudeCodeEditor) {
            return new ClaudeCodeEditor(editor.getAdapter(), {
                ...editor.getConfig(),
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
    createSandboxedExecutor(executor, worktree) {
        return {
            ...executor,
            editor: this.cloneEditorForCwd(executor.editor, worktree.path),
        };
    }
    /**
     * 注册执行器
     */
    registerExecutor(name, editor, capabilities, modelId) {
        this.runtime.registerExecutor(name, editor, capabilities, modelId);
    }
    /**
     * 获取执行器
     */
    getExecutor(name) {
        return this.runtime.getExecutor(name);
    }
    /**
     * 获取所有执行器
     */
    getAllExecutors() {
        return this.runtime.getAllExecutors();
    }
    /**
     * 创建 Worker
     */
    async createWorker(name, modelId, task) {
        const workerId = `${this.config.worktreePrefix}-${name}-${Date.now()}`;
        // 创建 worktree
        const worktree = await this.worktreeManager.createWorktree(workerId, {
            createBranch: true,
        });
        const worker = {
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
    async startWorker(worker) {
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
            const result = await this.executeTaskInWorktree(executor, worker.task, worker.worktree);
            worker.status = 'completed';
            worker.completedAt = Date.now();
            worker.result = result;
            this.emit('worker:completed', worker);
            return result;
        }
        catch (error) {
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
    async executeTaskInWorktree(executor, task, worktree) {
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
    async executeParallel(tasks) {
        if (this.isRunning) {
            throw new Error('Parallel execution already in progress');
        }
        if (tasks.length > this.config.maxWorkers) {
            throw new Error(`Too many tasks (${tasks.length}), max workers: ${this.config.maxWorkers}`);
        }
        this.isRunning = true;
        this.abortController = new AbortController();
        const startTime = Date.now();
        const errors = [];
        try {
            // 创建所有 workers
            const workers = [];
            for (const { executorName, task } of tasks) {
                const executor = this.runtime.getExecutor(executorName);
                if (!executor) {
                    throw new Error(`Executor not found: ${executorName}`);
                }
                const worker = await this.createWorker(executorName, executor.modelId || executorName, task);
                workers.push(worker);
            }
            this.emit('execution:started', workers);
            // 并行执行所有 workers
            const resultPromises = workers.map(worker => this.startWorkerWithTimeout(worker));
            // 等待所有结果
            const results = await Promise.all(resultPromises);
            // 收集错误
            for (const worker of workers) {
                if (worker.error) {
                    errors.push(worker.error);
                }
            }
            const result = {
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
        }
        catch (error) {
            const err = error instanceof Error ? error : new Error(String(error));
            this.emit('execution:failed', err);
            throw err;
        }
        finally {
            this.isRunning = false;
            this.abortController = undefined;
        }
    }
    /**
     * 带超时的 Worker 启动
     */
    async startWorkerWithTimeout(worker) {
        return new Promise((resolve, reject) => {
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
                }
                else {
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
    async cancel() {
        if (!this.isRunning)
            return;
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
    async cleanup() {
        for (const worker of this.workers.values()) {
            if (worker.worktree) {
                try {
                    await this.worktreeManager.removeWorktree(worker.worktree.path, true);
                }
                catch {
                    // Ignore cleanup errors
                }
            }
        }
        this.workers.clear();
    }
    /**
     * 获取所有 Workers
     */
    getWorkers() {
        return Array.from(this.workers.values());
    }
    /**
     * 获取 Worker
     */
    getWorker(id) {
        return this.workers.get(id);
    }
    /**
     * 获取运行中的 Workers
     */
    getRunningWorkers() {
        return Array.from(this.workers.values()).filter(w => w.status === 'running');
    }
    /**
     * 获取已完成的 Workers
     */
    getCompletedWorkers() {
        return Array.from(this.workers.values()).filter(w => w.status === 'completed');
    }
    /**
     * 获取失败的 Workers
     */
    getFailedWorkers() {
        return Array.from(this.workers.values()).filter(w => w.status === 'failed');
    }
    /**
     * 是否正在运行
     */
    isExecuting() {
        return this.isRunning;
    }
    /**
     * 获取配置
     */
    getConfig() {
        return { ...this.config };
    }
    /**
     * 更新配置
     */
    updateConfig(config) {
        if (this.isRunning) {
            throw new Error('Cannot update config while executing');
        }
        this.config = { ...this.config, ...config };
    }
}
//# sourceMappingURL=ParallelExecutor.js.map