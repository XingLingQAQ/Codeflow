/**
 * ResultCollector - 并行执行结果收集器
 * 收集、聚合和分析多个 Agent 的执行结果
 */
import { EventEmitter } from 'events';
/**
 * ResultCollector - 结果收集器
 */
export class ResultCollector extends EventEmitter {
    constructor() {
        super(...arguments);
        this.results = new Map();
        this.workers = new Map();
    }
    /**
     * 添加结果
     */
    addResult(workerId, result, worker) {
        const isUpdate = this.results.has(workerId);
        this.results.set(workerId, result);
        if (worker) {
            this.workers.set(workerId, worker);
        }
        if (isUpdate) {
            this.emit('result:updated', workerId, result);
        }
        else {
            this.emit('result:added', workerId, result);
        }
        this.emit('summary:updated', this.getSummary());
    }
    /**
     * 从并行执行结果收集
     */
    collectFromParallelResult(parallelResult) {
        for (let i = 0; i < parallelResult.workers.length; i++) {
            const worker = parallelResult.workers[i];
            const result = parallelResult.results[i];
            if (result) {
                this.addResult(worker.id, result, worker);
            }
        }
    }
    /**
     * 获取结果
     */
    getResult(workerId) {
        return this.results.get(workerId);
    }
    /**
     * 获取所有结果
     */
    getAllResults() {
        return Array.from(this.results.values());
    }
    /**
     * 获取成功的结果
     */
    getSuccessfulResults() {
        return Array.from(this.results.values()).filter(r => r.success);
    }
    /**
     * 获取失败的结果
     */
    getFailedResults() {
        return Array.from(this.results.values()).filter(r => !r.success);
    }
    /**
     * 获取结果摘要
     */
    getSummary() {
        const results = Array.from(this.results.values());
        const workers = Array.from(this.workers.values());
        const successCount = results.filter(r => r.success).length;
        const failedCount = results.filter(r => !r.success).length;
        const cancelledCount = workers.filter(w => w.status === 'cancelled').length;
        const durations = results.map(r => r.duration || 0);
        const totalDuration = durations.reduce((a, b) => a + b, 0);
        const averageDuration = durations.length > 0 ? totalDuration / durations.length : 0;
        const allDiffs = results.flatMap(r => r.diffs || []);
        const totalAdditions = allDiffs.reduce((a, d) => a + d.additions, 0);
        const totalDeletions = allDiffs.reduce((a, d) => a + d.deletions, 0);
        const affectedFiles = [...new Set(allDiffs.map(d => d.file))];
        return {
            totalWorkers: this.results.size,
            successCount,
            failedCount,
            cancelledCount,
            totalDuration,
            averageDuration,
            totalDiffs: allDiffs.length,
            totalAdditions,
            totalDeletions,
            affectedFiles,
        };
    }
    /**
     * 获取 Worker 对比
     */
    getWorkerComparisons() {
        const comparisons = [];
        for (const [workerId, result] of this.results) {
            const worker = this.workers.get(workerId);
            const diffs = result.diffs || [];
            comparisons.push({
                workerId,
                workerName: worker?.name || 'unknown',
                modelId: worker?.modelId || 'unknown',
                success: result.success,
                duration: result.duration || 0,
                diffCount: diffs.length,
                additions: diffs.reduce((a, d) => a + d.additions, 0),
                deletions: diffs.reduce((a, d) => a + d.deletions, 0),
                affectedFiles: diffs.map(d => d.file),
            });
        }
        return comparisons;
    }
    /**
     * 按指标排序 Worker
     */
    rankWorkers(metric = 'duration', ascending = true) {
        const comparisons = this.getWorkerComparisons();
        comparisons.sort((a, b) => {
            const aValue = a[metric];
            const bValue = b[metric];
            return ascending ? aValue - bValue : bValue - aValue;
        });
        return comparisons;
    }
    /**
     * 获取最佳 Worker（基于成功和最短时间）
     */
    getBestWorker() {
        const successful = this.getWorkerComparisons().filter(w => w.success);
        if (successful.length === 0)
            return undefined;
        return successful.reduce((best, current) => current.duration < best.duration ? current : best);
    }
    /**
     * 获取文件级别的 Diff 对比
     */
    getFileDiffComparison(file) {
        const comparison = new Map();
        for (const [workerId, result] of this.results) {
            const diff = result.diffs?.find(d => d.file === file);
            comparison.set(workerId, diff);
        }
        return comparison;
    }
    /**
     * 获取所有受影响的文件
     */
    getAffectedFiles() {
        const files = new Set();
        for (const result of this.results.values()) {
            for (const diff of result.diffs || []) {
                files.add(diff.file);
            }
        }
        return Array.from(files);
    }
    /**
     * 检查是否有冲突（多个 Worker 修改同一文件）
     */
    hasConflicts() {
        const fileWorkers = new Map();
        for (const [workerId, result] of this.results) {
            for (const diff of result.diffs || []) {
                const workers = fileWorkers.get(diff.file) || [];
                workers.push(workerId);
                fileWorkers.set(diff.file, workers);
            }
        }
        return Array.from(fileWorkers.values()).some(workers => workers.length > 1);
    }
    /**
     * 获取冲突文件
     */
    getConflictingFiles() {
        const fileWorkers = new Map();
        for (const [workerId, result] of this.results) {
            for (const diff of result.diffs || []) {
                const workers = fileWorkers.get(diff.file) || [];
                workers.push(workerId);
                fileWorkers.set(diff.file, workers);
            }
        }
        return Array.from(fileWorkers.entries())
            .filter(([_, workers]) => workers.length > 1)
            .map(([file]) => file);
    }
    /**
     * 清空结果
     */
    clear() {
        this.results.clear();
        this.workers.clear();
    }
    /**
     * 获取结果数量
     */
    get size() {
        return this.results.size;
    }
}
//# sourceMappingURL=ResultCollector.js.map