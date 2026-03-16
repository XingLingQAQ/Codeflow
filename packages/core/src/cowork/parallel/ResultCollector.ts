/**
 * ResultCollector - 并行执行结果收集器
 * 收集、聚合和分析多个 Agent 的执行结果
 */

import { EventEmitter } from 'events';
import { ExecutionResult, Diff } from '../types.js';
import { AgentWorker, ParallelExecutionResult } from './ParallelExecutor.js';

/**
 * 结果摘要
 */
export interface ResultSummary {
  totalWorkers: number;
  successCount: number;
  failedCount: number;
  cancelledCount: number;
  totalDuration: number;
  averageDuration: number;
  totalDiffs: number;
  totalAdditions: number;
  totalDeletions: number;
  affectedFiles: string[];
}

/**
 * Worker 结果对比
 */
export interface WorkerComparison {
  workerId: string;
  workerName: string;
  modelId: string;
  success: boolean;
  duration: number;
  diffCount: number;
  additions: number;
  deletions: number;
  affectedFiles: string[];
  score?: number;
}

/**
 * 结果收集器事件
 */
export interface ResultCollectorEvents {
  'result:added': (workerId: string, result: ExecutionResult) => void;
  'result:updated': (workerId: string, result: ExecutionResult) => void;
  'summary:updated': (summary: ResultSummary) => void;
}

export type WorkerResult = ExecutionResult;

/**
 * ResultCollector - 结果收集器
 */
export class ResultCollector extends EventEmitter {
  private results: Map<string, ExecutionResult> = new Map();
  private workers: Map<string, AgentWorker> = new Map();

  private isSuccessful(result: ExecutionResult): boolean {
    if (typeof result.success === 'boolean') {
      return result.success;
    }
    return result.status === 'completed' && !result.output?.error;
  }

  private getDiffs(result: ExecutionResult): Diff[] {
    return result.diffs ?? result.output?.diffs ?? [];
  }

  /**
   * 添加结果
   */
  addResult(workerId: string, result: ExecutionResult, worker?: AgentWorker): void {
    const isUpdate = this.results.has(workerId);
    this.results.set(workerId, result);

    if (worker) {
      this.workers.set(workerId, worker);
    }

    if (isUpdate) {
      this.emit('result:updated', workerId, result);
    } else {
      this.emit('result:added', workerId, result);
    }

    this.emit('summary:updated', this.getSummary());
  }

  /**
   * 从并行执行结果收集
   */
  collectFromParallelResult(parallelResult: ParallelExecutionResult): void {
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
  getResult(workerId: string): ExecutionResult | undefined {
    return this.results.get(workerId);
  }

  /**
   * 获取所有结果
   */
  getAllResults(): ExecutionResult[] {
    return Array.from(this.results.values());
  }

  /**
   * 获取成功的结果
   */
  getSuccessfulResults(): ExecutionResult[] {
    return Array.from(this.results.values()).filter(r => this.isSuccessful(r));
  }

  /**
   * 获取失败的结果
   */
  getFailedResults(): ExecutionResult[] {
    return Array.from(this.results.values()).filter(r => !this.isSuccessful(r));
  }

  /**
   * 获取结果摘要
   */
  getSummary(): ResultSummary {
    const results = Array.from(this.results.values());
    const workers = Array.from(this.workers.values());

    const successCount = results.filter(r => this.isSuccessful(r)).length;
    const failedCount = results.filter(r => !this.isSuccessful(r)).length;
    const cancelledCount = workers.filter(w => w.status === 'cancelled').length;

    const durations = results.map(r => r.duration || 0);
    const totalDuration = durations.reduce((a, b) => a + b, 0);
    const averageDuration = durations.length > 0 ? totalDuration / durations.length : 0;

    const allDiffs = results.flatMap(r => this.getDiffs(r));
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
  getWorkerComparisons(): WorkerComparison[] {
    const comparisons: WorkerComparison[] = [];

    for (const [workerId, result] of this.results) {
      const worker = this.workers.get(workerId);
      const diffs = this.getDiffs(result);

      comparisons.push({
        workerId,
        workerName: worker?.name || 'unknown',
        modelId: worker?.modelId || 'unknown',
        success: this.isSuccessful(result),
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
  rankWorkers(
    metric: 'duration' | 'additions' | 'deletions' | 'diffCount' = 'duration',
    ascending: boolean = true
  ): WorkerComparison[] {
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
  getBestWorker(): WorkerComparison | undefined {
    const successful = this.getWorkerComparisons().filter(w => w.success);
    if (successful.length === 0) return undefined;

    return successful.reduce((best, current) =>
      current.duration < best.duration ? current : best
    );
  }

  /**
   * 获取文件级别的 Diff 对比
   */
  getFileDiffComparison(file: string): Map<string, Diff | undefined> {
    const comparison = new Map<string, Diff | undefined>();

    for (const [workerId, result] of this.results) {
      const diff = this.getDiffs(result).find(d => d.file === file);
      comparison.set(workerId, diff);
    }

    return comparison;
  }

  /**
   * 获取所有受影响的文件
   */
  getAffectedFiles(): string[] {
    const files = new Set<string>();

    for (const result of this.results.values()) {
      for (const diff of this.getDiffs(result)) {
        files.add(diff.file);
      }
    }

    return Array.from(files);
  }

  /**
   * 检查是否有冲突（多个 Worker 修改同一文件）
   */
  hasConflicts(): boolean {
    const fileWorkers = new Map<string, string[]>();

    for (const [workerId, result] of this.results) {
      for (const diff of this.getDiffs(result)) {
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
  getConflictingFiles(): string[] {
    const fileWorkers = new Map<string, string[]>();

    for (const [workerId, result] of this.results) {
      for (const diff of this.getDiffs(result)) {
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
  clear(): void {
    this.results.clear();
    this.workers.clear();
  }

  /**
   * 获取结果数量
   */
  get size(): number {
    return this.results.size;
  }
}
