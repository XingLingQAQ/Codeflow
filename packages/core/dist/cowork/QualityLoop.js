/**
 * QualityLoop - 质量控制循环
 * 实现 Trellis 的 ralph-loop 质量控制循环：Check → Fix → Check
 */
import { EventEmitter } from 'events';
const DEFAULT_CONFIG = {
    maxIterations: 3,
    checkTypes: ['lint', 'type', 'test'],
    autoFixEnabled: true,
    stopOnError: false,
    timeout: 60000,
};
/**
 * CheckAgent - 检查 Agent
 */
export class CheckAgent extends EventEmitter {
    constructor(callback, modelId) {
        super();
        this.callback = callback;
        this.modelId = modelId;
    }
    /**
     * 执行检查
     */
    async check(files, checkTypes) {
        const startTime = Date.now();
        this.emit('check:start', { files, checkTypes });
        try {
            let result;
            if (this.callback) {
                result = await this.callback(files, checkTypes);
            }
            else {
                result = this.generateStaticCheck(files, checkTypes);
            }
            result.duration = Date.now() - startTime;
            this.emit('check:complete', result);
            return result;
        }
        catch (error) {
            const errorResult = {
                passed: false,
                issues: [{
                        id: 'check-error',
                        type: 'custom',
                        severity: 'error',
                        message: error instanceof Error ? error.message : 'Check failed',
                        autoFixable: false,
                    }],
                summary: { errors: 1, warnings: 0, infos: 0, autoFixable: 0 },
                duration: Date.now() - startTime,
                checkType: 'custom',
            };
            this.emit('check:error', errorResult);
            return errorResult;
        }
    }
    generateStaticCheck(files, checkTypes) {
        // 模拟检查结果
        const issues = [];
        // 随机生成一些问题用于测试
        if (Math.random() > 0.7) {
            issues.push({
                id: `issue-${Date.now()}`,
                type: checkTypes[0] || 'lint',
                severity: 'warning',
                message: 'Potential issue detected',
                autoFixable: true,
            });
        }
        return {
            passed: issues.filter(i => i.severity === 'error').length === 0,
            issues,
            summary: {
                errors: issues.filter(i => i.severity === 'error').length,
                warnings: issues.filter(i => i.severity === 'warning').length,
                infos: issues.filter(i => i.severity === 'info').length,
                autoFixable: issues.filter(i => i.autoFixable).length,
            },
            duration: 0,
            checkType: checkTypes[0] || 'custom',
        };
    }
    /**
     * 获取模型 ID
     */
    getModelId() {
        return this.modelId;
    }
}
/**
 * FixAgent - 修复 Agent
 */
export class FixAgent extends EventEmitter {
    constructor(callback, modelId) {
        super();
        this.callback = callback;
        this.modelId = modelId;
    }
    /**
     * 执行修复
     */
    async fix(issues) {
        const startTime = Date.now();
        this.emit('fix:start', { issues });
        // 只修复可自动修复的问题
        const fixableIssues = issues.filter(i => i.autoFixable);
        if (fixableIssues.length === 0) {
            const noFixResult = {
                success: true,
                fixedIssues: [],
                failedIssues: [],
                filesModified: [],
                duration: Date.now() - startTime,
            };
            this.emit('fix:complete', noFixResult);
            return noFixResult;
        }
        try {
            let result;
            if (this.callback) {
                result = await this.callback(fixableIssues);
            }
            else {
                result = this.generateStaticFix(fixableIssues);
            }
            result.duration = Date.now() - startTime;
            this.emit('fix:complete', result);
            return result;
        }
        catch (error) {
            const errorResult = {
                success: false,
                fixedIssues: [],
                failedIssues: fixableIssues.map(i => i.id),
                filesModified: [],
                duration: Date.now() - startTime,
            };
            this.emit('fix:error', errorResult);
            return errorResult;
        }
    }
    generateStaticFix(issues) {
        // 模拟修复结果
        const fixedIssues = issues.map(i => i.id);
        const filesModified = [...new Set(issues.filter(i => i.file).map(i => i.file))];
        return {
            success: true,
            fixedIssues,
            failedIssues: [],
            filesModified,
            duration: 0,
        };
    }
    /**
     * 获取模型 ID
     */
    getModelId() {
        return this.modelId;
    }
}
/**
 * IterationManager - 迭代管理器
 */
export class IterationManager extends EventEmitter {
    constructor(maxIterations = 3) {
        super();
        this.iterations = [];
        this.maxIterations = maxIterations;
    }
    /**
     * 记录迭代
     */
    recordIteration(result) {
        this.iterations.push(result);
        this.emit('iteration:recorded', result);
    }
    /**
     * 是否可以继续迭代
     */
    canContinue() {
        return this.iterations.length < this.maxIterations;
    }
    /**
     * 获取当前迭代次数
     */
    getCurrentIteration() {
        return this.iterations.length;
    }
    /**
     * 获取所有迭代
     */
    getIterations() {
        return [...this.iterations];
    }
    /**
     * 获取最后一次迭代
     */
    getLastIteration() {
        return this.iterations[this.iterations.length - 1];
    }
    /**
     * 重置
     */
    reset() {
        this.iterations = [];
        this.emit('iterations:reset');
    }
    /**
     * 更新最大迭代次数
     */
    setMaxIterations(max) {
        this.maxIterations = max;
    }
}
/**
 * QualityLoop - 质量控制循环
 */
export class QualityLoop extends EventEmitter {
    constructor(config = {}, callbacks) {
        super();
        this.running = false;
        this.config = { ...DEFAULT_CONFIG, ...config };
        this.checkAgent = new CheckAgent(callbacks?.check, this.config.checkModelId);
        this.fixAgent = new FixAgent(callbacks?.fix, this.config.fixModelId);
        this.iterationManager = new IterationManager(this.config.maxIterations);
        // 转发事件
        this.checkAgent.on('check:start', (data) => this.emit('check:start', data));
        this.checkAgent.on('check:complete', (data) => this.emit('check:complete', data));
        this.fixAgent.on('fix:start', (data) => this.emit('fix:start', data));
        this.fixAgent.on('fix:complete', (data) => this.emit('fix:complete', data));
        this.iterationManager.on('iteration:recorded', (data) => this.emit('iteration:recorded', data));
    }
    /**
     * 执行质量循环
     */
    async run(files) {
        if (this.running) {
            throw new Error('Quality loop is already running');
        }
        this.running = true;
        this.iterationManager.reset();
        const startTime = Date.now();
        this.emit('loop:start', { files, config: this.config });
        try {
            while (this.iterationManager.canContinue()) {
                const iteration = this.iterationManager.getCurrentIteration() + 1;
                this.emit('iteration:start', { iteration });
                // 执行检查
                const checkResult = await this.checkAgent.check(files, this.config.checkTypes);
                // 如果检查通过，结束循环
                if (checkResult.passed) {
                    const iterationResult = {
                        iteration,
                        checkResult,
                        status: 'passed',
                    };
                    this.iterationManager.recordIteration(iterationResult);
                    break;
                }
                // 如果有错误且配置为停止，结束循环
                if (this.config.stopOnError && checkResult.summary.errors > 0) {
                    const iterationResult = {
                        iteration,
                        checkResult,
                        status: 'failed',
                    };
                    this.iterationManager.recordIteration(iterationResult);
                    break;
                }
                // 尝试自动修复
                if (this.config.autoFixEnabled && checkResult.summary.autoFixable > 0) {
                    const fixResult = await this.fixAgent.fix(checkResult.issues);
                    const iterationResult = {
                        iteration,
                        checkResult,
                        fixResult,
                        status: fixResult.success ? 'fixed' : 'failed',
                    };
                    this.iterationManager.recordIteration(iterationResult);
                    // 如果修复失败，结束循环
                    if (!fixResult.success) {
                        break;
                    }
                }
                else {
                    // 没有可自动修复的问题，需要人工介入
                    const iterationResult = {
                        iteration,
                        checkResult,
                        status: 'manual_required',
                    };
                    this.iterationManager.recordIteration(iterationResult);
                    break;
                }
            }
            const result = this.buildLoopResult(startTime);
            this.emit('loop:complete', result);
            return result;
        }
        finally {
            this.running = false;
        }
    }
    /**
     * 构建循环结果
     */
    buildLoopResult(startTime) {
        const iterations = this.iterationManager.getIterations();
        const lastIteration = this.iterationManager.getLastIteration();
        const passed = lastIteration?.status === 'passed';
        const requiresManualIntervention = lastIteration?.status === 'manual_required' ||
            (lastIteration?.status === 'failed' && !this.iterationManager.canContinue());
        const finalIssues = lastIteration?.checkResult.issues.filter(i => i.severity === 'error') || [];
        let summary;
        if (passed) {
            summary = `Quality check passed after ${iterations.length} iteration(s)`;
        }
        else if (requiresManualIntervention) {
            summary = `Quality check requires manual intervention. ${finalIssues.length} issue(s) remaining.`;
        }
        else {
            summary = `Quality check failed after ${iterations.length} iteration(s). Max iterations reached.`;
        }
        return {
            passed,
            iterations,
            totalDuration: Date.now() - startTime,
            finalIssues,
            requiresManualIntervention,
            summary,
        };
    }
    /**
     * 停止循环
     */
    stop() {
        this.running = false;
        this.emit('loop:stopped');
    }
    /**
     * 是否正在运行
     */
    isRunning() {
        return this.running;
    }
    /**
     * 获取当前迭代次数
     */
    getCurrentIteration() {
        return this.iterationManager.getCurrentIteration();
    }
    /**
     * 获取迭代历史
     */
    getIterationHistory() {
        return this.iterationManager.getIterations();
    }
    /**
     * 更新配置
     */
    updateConfig(config) {
        this.config = { ...this.config, ...config };
        this.iterationManager.setMaxIterations(this.config.maxIterations);
    }
    /**
     * 获取配置
     */
    getConfig() {
        return { ...this.config };
    }
    /**
     * 获取 Check Agent
     */
    getCheckAgent() {
        return this.checkAgent;
    }
    /**
     * 获取 Fix Agent
     */
    getFixAgent() {
        return this.fixAgent;
    }
}
//# sourceMappingURL=QualityLoop.js.map