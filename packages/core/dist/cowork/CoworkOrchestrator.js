/**
 * Cowork Orchestrator
 * 多 CLI 协作编排器 - 支持并行、顺序、辩论三种协作模式
 */
import { EventEmitter } from 'events';
import { CLIProcessManager } from './process/CLIProcessManager.js';
/**
 * Cowork Orchestrator
 */
export class CoworkOrchestrator extends EventEmitter {
    constructor(processManager) {
        super();
        this.executors = new Map();
        this.blackboard = new Map();
        this.runningTasks = new Map();
        this.processManager = processManager || new CLIProcessManager();
    }
    /**
     * 注册执行器
     */
    registerExecutor(name, editor, capabilities) {
        this.executors.set(name, { name, editor, capabilities });
        this.emitEvent({ type: 'task:start', task: { id: `register_${name}` } });
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
     * 执行单个任务
     */
    async execute(task) {
        const startTime = Date.now();
        // 获取执行器
        const executor = this.executors.get(task.executor);
        if (!executor) {
            return {
                taskId: task.id,
                status: 'failed',
                output: { error: `Executor '${task.executor}' not found` },
                executor: task.executor,
                duration: Date.now() - startTime,
            };
        }
        // 更新任务状态
        task.status = 'running';
        task.startedAt = startTime;
        this.runningTasks.set(task.id, task);
        this.emitEvent({ type: 'task:start', task });
        try {
            // 执行编辑
            const diffs = [];
            if (task.input.files.length === 1) {
                const result = await executor.editor.edit(task.input.files[0], task.input.instruction);
                if (result.success) {
                    diffs.push(result.diff);
                }
            }
            else if (task.input.files.length > 1) {
                const results = await executor.editor.editMultiple(task.input.files, task.input.instruction);
                for (const result of results) {
                    if (result.success) {
                        diffs.push(result.diff);
                    }
                }
            }
            // 更新任务状态
            task.status = 'completed';
            task.completedAt = Date.now();
            task.output = {
                diffs,
                metrics: {
                    duration: Date.now() - startTime,
                },
            };
            const result = {
                taskId: task.id,
                status: 'completed',
                output: task.output,
                executor: task.executor,
                duration: Date.now() - startTime,
            };
            this.emitEvent({ type: 'task:complete', taskId: task.id, result });
            this.runningTasks.delete(task.id);
            return result;
        }
        catch (error) {
            const errorMessage = error instanceof Error ? error.message : 'Unknown error';
            task.status = 'failed';
            task.completedAt = Date.now();
            task.output = { error: errorMessage };
            const result = {
                taskId: task.id,
                status: 'failed',
                output: task.output,
                executor: task.executor,
                duration: Date.now() - startTime,
            };
            this.emitEvent({ type: 'task:error', taskId: task.id, error: errorMessage });
            this.runningTasks.delete(task.id);
            return result;
        }
    }
    /**
     * 并行执行多个任务
     */
    async executeParallel(tasks, options = {}) {
        const startTime = Date.now();
        const { maxConcurrency = 5, failFast = false, conflictStrategy = 'fail' } = options;
        const results = [];
        const conflicts = [];
        // 检测文件冲突
        const fileToTasks = new Map();
        for (const task of tasks) {
            for (const file of task.input.files) {
                const existing = fileToTasks.get(file) || [];
                existing.push(task);
                fileToTasks.set(file, existing);
            }
        }
        // 报告冲突
        for (const [file, fileTasks] of fileToTasks) {
            if (fileTasks.length > 1) {
                const conflict = {
                    file,
                    executors: fileTasks.map((t) => t.executor),
                    type: 'content',
                };
                conflicts.push(conflict);
                this.emitEvent({ type: 'conflict:detected', conflict });
                if (conflictStrategy === 'fail') {
                    return {
                        mode: 'parallel',
                        results: [],
                        totalDuration: Date.now() - startTime,
                        successCount: 0,
                        failureCount: tasks.length,
                        conflicts,
                    };
                }
            }
        }
        // 分批执行
        const batches = [];
        for (let i = 0; i < tasks.length; i += maxConcurrency) {
            batches.push(tasks.slice(i, i + maxConcurrency));
        }
        for (const batch of batches) {
            const batchPromises = batch.map((task) => this.execute(task));
            if (failFast) {
                // 任一失败立即停止
                const batchResults = await Promise.all(batchPromises.map((p) => p.catch((e) => ({
                    taskId: 'unknown',
                    status: 'failed',
                    output: { error: e.message },
                    executor: 'unknown',
                    duration: 0,
                }))));
                results.push(...batchResults);
                if (batchResults.some((r) => r.status === 'failed')) {
                    break;
                }
            }
            else {
                const batchResults = await Promise.allSettled(batchPromises);
                for (const result of batchResults) {
                    if (result.status === 'fulfilled') {
                        results.push(result.value);
                    }
                    else {
                        results.push({
                            taskId: 'unknown',
                            status: 'failed',
                            output: { error: result.reason?.message || 'Unknown error' },
                            executor: 'unknown',
                            duration: 0,
                        });
                    }
                }
            }
        }
        return {
            mode: 'parallel',
            results,
            totalDuration: Date.now() - startTime,
            successCount: results.filter((r) => r.status === 'completed').length,
            failureCount: results.filter((r) => r.status === 'failed').length,
            conflicts,
        };
    }
    /**
     * 顺序执行任务链
     */
    async executeSequence(tasks, options = {}) {
        const startTime = Date.now();
        const { stopOnError = true, passContext = true } = options;
        const results = [];
        for (let i = 0; i < tasks.length; i++) {
            const task = tasks[i];
            // 传递上下文
            if (passContext && i > 0) {
                const prevResult = results[i - 1];
                if (prevResult.status === 'completed' && prevResult.output) {
                    // 将前一个任务的输出添加到当前任务的上下文
                    task.input.context = this.buildContextFromResult(prevResult);
                    // 写入 Blackboard
                    this.setBlackboardEntry(`task_${tasks[i - 1].id}_output`, prevResult.output, tasks[i - 1].executor);
                }
            }
            // 报告进度
            this.emitEvent({
                type: 'task:progress',
                taskId: task.id,
                progress: (i / tasks.length) * 100,
                message: `Executing task ${i + 1}/${tasks.length}`,
            });
            const result = await this.execute(task);
            results.push(result);
            if (stopOnError && result.status === 'failed') {
                break;
            }
        }
        return {
            mode: 'sequential',
            results,
            totalDuration: Date.now() - startTime,
            successCount: results.filter((r) => r.status === 'completed').length,
            failureCount: results.filter((r) => r.status === 'failed').length,
        };
    }
    /**
     * 辩论模式执行
     */
    async executeDebate(task, options) {
        const startTime = Date.now();
        const { maxRounds = 3, convergenceThreshold = 0.8, generator, critic } = options;
        const rounds = [];
        let converged = false;
        let finalOutput;
        let finalDiffs;
        // 获取执行器
        const generatorExecutor = this.executors.get(generator);
        const criticExecutor = this.executors.get(critic);
        if (!generatorExecutor || !criticExecutor) {
            return {
                mode: 'debate',
                results: [],
                totalDuration: Date.now() - startTime,
                successCount: 0,
                failureCount: 1,
                rounds: [],
                converged: false,
            };
        }
        let currentInstruction = task.input.instruction;
        for (let round = 1; round <= maxRounds && !converged; round++) {
            // Generator 生成
            const generatorTask = {
                ...task,
                id: `${task.id}_gen_${round}`,
                executor: generator,
                input: {
                    ...task.input,
                    instruction: currentInstruction,
                },
            };
            const genResult = await this.execute(generatorTask);
            const genOutput = genResult.output?.result || '';
            const genDiffs = genResult.output?.diffs || [];
            // Critic 评审
            const criticTask = {
                ...task,
                id: `${task.id}_critic_${round}`,
                executor: critic,
                type: 'review',
                input: {
                    ...task.input,
                    instruction: `Review the following code changes and identify issues:\n\n${genOutput}`,
                },
            };
            const criticResult = await this.execute(criticTask);
            const criticOutput = criticResult.output?.result || '';
            const issues = this.parseIssues(criticOutput);
            const debateRound = {
                round,
                generator: {
                    executor: generator,
                    output: genOutput,
                    diffs: genDiffs,
                },
                critic: {
                    executor: critic,
                    feedback: criticOutput,
                    issues,
                },
            };
            // 检查收敛
            const criticalIssues = issues.filter((i) => i.severity === 'critical' || i.severity === 'high');
            if (criticalIssues.length === 0) {
                converged = true;
                finalOutput = genOutput;
                finalDiffs = genDiffs;
                debateRound.refined = { output: genOutput, diffs: genDiffs };
            }
            else {
                // 根据反馈调整指令
                currentInstruction = this.refineInstruction(task.input.instruction, issues);
            }
            rounds.push(debateRound);
            this.emitEvent({ type: 'debate:round', round: debateRound });
        }
        return {
            mode: 'debate',
            results: [],
            totalDuration: Date.now() - startTime,
            successCount: converged ? 1 : 0,
            failureCount: converged ? 0 : 1,
            rounds,
            converged,
            finalOutput,
            finalDiffs,
        };
    }
    /**
     * Blackboard 操作
     */
    setBlackboardEntry(key, value, source) {
        this.blackboard.set(key, {
            key,
            value,
            source,
            timestamp: Date.now(),
        });
    }
    getBlackboardEntry(key) {
        return this.blackboard.get(key);
    }
    getAllBlackboardEntries() {
        return Array.from(this.blackboard.values());
    }
    clearBlackboard() {
        this.blackboard.clear();
    }
    /**
     * 获取运行中的任务
     */
    getRunningTasks() {
        return Array.from(this.runningTasks.values());
    }
    /**
     * 取消任务
     */
    async cancelTask(taskId) {
        const task = this.runningTasks.get(taskId);
        if (!task) {
            return false;
        }
        task.status = 'cancelled';
        task.completedAt = Date.now();
        this.runningTasks.delete(taskId);
        return true;
    }
    /**
     * 获取进程管理器
     */
    getProcessManager() {
        return this.processManager;
    }
    /**
     * 清理资源
     */
    async cleanup() {
        // 取消所有运行中的任务
        for (const taskId of this.runningTasks.keys()) {
            await this.cancelTask(taskId);
        }
        // 清理进程
        await this.processManager.cleanup();
        // 清理 Blackboard
        this.clearBlackboard();
    }
    // ==================== 私有方法 ====================
    emitEvent(event) {
        this.emit('event', event);
    }
    buildContextFromResult(result) {
        if (!result.output)
            return '';
        const parts = [];
        if (result.output.result) {
            parts.push(`Previous output:\n${result.output.result}`);
        }
        if (result.output.diffs && result.output.diffs.length > 0) {
            parts.push(`Previous changes:\n${result.output.diffs.map((d) => `- ${d.file}: +${d.additions}/-${d.deletions}`).join('\n')}`);
        }
        return parts.join('\n\n');
    }
    parseIssues(criticOutput) {
        // 简单解析 - 实际实现可以更复杂
        const issues = [];
        const lines = criticOutput.split('\n');
        for (const line of lines) {
            const lowerLine = line.toLowerCase();
            if (lowerLine.includes('bug') || lowerLine.includes('error')) {
                issues.push({
                    type: 'bug',
                    severity: lowerLine.includes('critical') ? 'critical' : 'medium',
                    description: line.trim(),
                });
            }
            else if (lowerLine.includes('security') || lowerLine.includes('vulnerability')) {
                issues.push({
                    type: 'security',
                    severity: 'high',
                    description: line.trim(),
                });
            }
            else if (lowerLine.includes('performance') || lowerLine.includes('slow')) {
                issues.push({
                    type: 'performance',
                    severity: 'medium',
                    description: line.trim(),
                });
            }
            else if (lowerLine.includes('style') || lowerLine.includes('format')) {
                issues.push({
                    type: 'style',
                    severity: 'low',
                    description: line.trim(),
                });
            }
        }
        return issues;
    }
    refineInstruction(original, issues) {
        const issueDescriptions = issues
            .filter((i) => i.severity === 'critical' || i.severity === 'high')
            .map((i) => `- ${i.description}`)
            .join('\n');
        return `${original}\n\nPlease address the following issues:\n${issueDescriptions}`;
    }
}
//# sourceMappingURL=CoworkOrchestrator.js.map