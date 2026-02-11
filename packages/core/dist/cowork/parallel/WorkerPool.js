/**
 * WorkerPool - Worker 池管理
 * 管理 Agent Worker 的生命周期和资源分配
 */
import { EventEmitter } from 'events';
/**
 * 默认配置
 */
const DEFAULT_CONFIG = {
    maxSize: 10,
    minSize: 0,
    idleTimeout: 60000, // 1 minute
    worktreePrefix: 'pool-worker',
};
/**
 * WorkerPool - Worker 池
 */
export class WorkerPool extends EventEmitter {
    constructor(worktreeManager, config = {}) {
        super();
        this.workers = new Map();
        this.executors = new Map();
        this.idleTimers = new Map();
        this.workerCounter = 0;
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
     * 创建新 Worker
     */
    async createWorker(executorName, task) {
        const executor = this.executors.get(executorName);
        if (!executor) {
            throw new Error(`Executor not found: ${executorName}`);
        }
        const workerId = `${this.config.worktreePrefix}-${++this.workerCounter}-${Date.now()}`;
        // 创建 worktree
        const worktree = await this.worktreeManager.createWorktree(workerId, {
            createBranch: true,
        });
        const worker = {
            id: workerId,
            name: executorName,
            modelId: executor.modelId,
            worktree,
            status: 'idle',
            task,
        };
        this.workers.set(workerId, worker);
        this.emit('pool:worker-added', worker);
        return worker;
    }
    /**
     * 获取可用 Worker（如果没有则创建）
     */
    async acquire(executorName, task) {
        // 查找空闲的同类型 Worker
        const idleWorker = this.findIdleWorker(executorName);
        if (idleWorker) {
            this.clearIdleTimer(idleWorker.id);
            idleWorker.status = 'running';
            idleWorker.task = task;
            this.emit('pool:worker-acquired', idleWorker);
            return idleWorker;
        }
        // 检查是否达到最大容量
        if (this.workers.size >= this.config.maxSize) {
            this.emit('pool:exhausted');
            throw new Error('Worker pool exhausted');
        }
        // 创建新 Worker
        const newWorker = await this.createWorker(executorName, task);
        newWorker.status = 'running';
        this.emit('pool:worker-acquired', newWorker);
        return newWorker;
    }
    /**
     * 释放 Worker
     */
    release(workerId) {
        const worker = this.workers.get(workerId);
        if (!worker)
            return;
        worker.status = 'idle';
        worker.task = undefined;
        this.emit('pool:worker-released', worker);
        // 设置空闲超时
        this.setIdleTimer(workerId);
        // 检查是否有等待的请求
        this.emit('pool:available');
    }
    /**
     * 标记 Worker 完成
     */
    markCompleted(workerId) {
        const worker = this.workers.get(workerId);
        if (worker) {
            worker.status = 'completed';
            worker.completedAt = Date.now();
        }
    }
    /**
     * 标记 Worker 失败
     */
    markFailed(workerId, error) {
        const worker = this.workers.get(workerId);
        if (worker) {
            worker.status = 'failed';
            worker.completedAt = Date.now();
            worker.error = error;
        }
    }
    /**
     * 移除 Worker
     */
    async remove(workerId) {
        const worker = this.workers.get(workerId);
        if (!worker)
            return;
        this.clearIdleTimer(workerId);
        // 移除 worktree
        if (worker.worktree) {
            try {
                await this.worktreeManager.removeWorktree(worker.worktree.path, true);
            }
            catch {
                // Ignore cleanup errors
            }
        }
        this.workers.delete(workerId);
        this.emit('pool:worker-removed', workerId);
    }
    /**
     * 查找空闲 Worker
     */
    findIdleWorker(executorName) {
        for (const worker of this.workers.values()) {
            if (worker.name === executorName && worker.status === 'idle') {
                return worker;
            }
        }
        return undefined;
    }
    /**
     * 设置空闲超时
     */
    setIdleTimer(workerId) {
        this.clearIdleTimer(workerId);
        const timer = setTimeout(async () => {
            // 检查是否低于最小容量
            if (this.workers.size > this.config.minSize) {
                await this.remove(workerId);
            }
        }, this.config.idleTimeout);
        this.idleTimers.set(workerId, timer);
    }
    /**
     * 清除空闲超时
     */
    clearIdleTimer(workerId) {
        const timer = this.idleTimers.get(workerId);
        if (timer) {
            clearTimeout(timer);
            this.idleTimers.delete(workerId);
        }
    }
    /**
     * 获取池统计
     */
    getStats() {
        const workers = Array.from(this.workers.values());
        return {
            totalWorkers: workers.length,
            idleWorkers: workers.filter(w => w.status === 'idle').length,
            runningWorkers: workers.filter(w => w.status === 'running').length,
            completedWorkers: workers.filter(w => w.status === 'completed').length,
            failedWorkers: workers.filter(w => w.status === 'failed').length,
        };
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
    getWorker(workerId) {
        return this.workers.get(workerId);
    }
    /**
     * 获取空闲 Workers
     */
    getIdleWorkers() {
        return Array.from(this.workers.values()).filter(w => w.status === 'idle');
    }
    /**
     * 获取运行中的 Workers
     */
    getRunningWorkers() {
        return Array.from(this.workers.values()).filter(w => w.status === 'running');
    }
    /**
     * 清空池
     */
    async drain() {
        // 清除所有空闲超时
        for (const timer of this.idleTimers.values()) {
            clearTimeout(timer);
        }
        this.idleTimers.clear();
        // 移除所有 Workers
        const workerIds = Array.from(this.workers.keys());
        for (const workerId of workerIds) {
            await this.remove(workerId);
        }
    }
    /**
     * 获取池大小
     */
    get size() {
        return this.workers.size;
    }
    /**
     * 获取可用容量
     */
    get availableCapacity() {
        return this.config.maxSize - this.workers.size;
    }
    /**
     * 是否已满
     */
    get isFull() {
        return this.workers.size >= this.config.maxSize;
    }
    /**
     * 是否为空
     */
    get isEmpty() {
        return this.workers.size === 0;
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
        this.config = { ...this.config, ...config };
    }
}
//# sourceMappingURL=WorkerPool.js.map