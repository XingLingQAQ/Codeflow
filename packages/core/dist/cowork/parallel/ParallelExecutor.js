/**
 * ParallelExecutor - 多 Agent 并行执行器
 * 在独立的 Git Worktree 中并行执行多个 Agent 任务
 */
import { EventEmitter } from 'events';
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
 * ParallelExecutor - 多 Agent 并行执行器
 */
export class ParallelExecutor extends EventEmitter {
    constructor(worktreeManager, config = {}) {
        super();
        this.workers = new Map();
        this.executors = new Map();
        this.isRunning = false;
        this.worktreeManager = worktreeManager;
        this.config = { ...DEFAULT_CONFIG, ...config };
    }
    /**
     * 注册执行器
     */
    registerExecutor(name, editor, capabilities, modelId) {
        this.executors.set(name, { name, editor, capabilities, modelId });
    }
    /**
     * 获取执行器
     */
    getExecutor(name) {
        return this.executors.get(name);
    }
    /**
     * 获取所有执行器
     */
    getAllExecutors() {
        return Array.from(this.executors.values());
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
            const executor = this.executors.get(worker.name);
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
        const startTime = Date.now();
        try {
            // 使用编辑器执行任务
            const editResults = await executor.editor.editMultiple(task.input.files, task.input.instruction);
            const success = editResults.every(r => r.success);
            const diffs = editResults.map(r => r.diff);
            return {
                taskId: task.id,
                success,
                diffs,
                duration: Date.now() - startTime,
                output: success ? 'Task completed successfully' : 'Some edits failed',
            };
        }
        catch (error) {
            const err = error instanceof Error ? error : new Error(String(error));
            return {
                taskId: task.id,
                success: false,
                error: err.message,
                duration: Date.now() - startTime,
            };
        }
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
                const executor = this.executors.get(executorName);
                if (!executor) {
                    throw new Error(`Executor not found: ${executorName}`);
                }
                const worker = await this.createWorker(executorName, executor.modelId, task);
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