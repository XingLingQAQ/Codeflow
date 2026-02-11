/**
 * ExecutionLoop - 执行循环
 * 实现 Aha-Loop 的 5 阶段执行循环：Research → Explore → Review → Implement → QA
 */
import { EventEmitter } from 'events';
import * as fs from 'fs';
import * as path from 'path';
const DEFAULT_CONFIG = {
    outputDir: '.codeflow/execution',
    autoSkipExplore: false,
    requireReviewApproval: true,
    maxQAIterations: 3,
    persistState: true,
    phaseModels: {},
};
/**
 * ResearchPhase - 技术研究阶段
 */
export class ResearchPhase extends EventEmitter {
    constructor(executor) {
        super();
        this.executor = executor;
    }
    async execute(context) {
        const startTime = Date.now();
        this.emit('phase:start', { phase: 'research' });
        try {
            let output;
            if (this.executor) {
                output = await this.executor(context);
            }
            else {
                output = this.generateStaticOutput(context);
            }
            const result = {
                phase: 'research',
                status: 'completed',
                startTime,
                endTime: Date.now(),
                output,
            };
            this.emit('phase:complete', result);
            return result;
        }
        catch (error) {
            const result = {
                phase: 'research',
                status: 'failed',
                startTime,
                endTime: Date.now(),
                error: error instanceof Error ? error.message : 'Unknown error',
            };
            this.emit('phase:error', result);
            return result;
        }
    }
    generateStaticOutput(context) {
        return {
            findings: [
                `Research for: ${context.prdItem.title}`,
                `Based on vision: ${context.vision.title}`,
            ],
            recommendations: context.prdItem.acceptanceCriteria.map(c => `Implement: ${c}`),
            risks: context.vision.risks,
            dependencies: context.constraints.constraints.map(c => c.description),
        };
    }
}
/**
 * ExplorePhase - 并行探索阶段
 */
export class ExplorePhase extends EventEmitter {
    constructor(executor) {
        super();
        this.executor = executor;
    }
    async execute(context) {
        const startTime = Date.now();
        this.emit('phase:start', { phase: 'explore' });
        try {
            let output;
            if (this.executor) {
                output = await this.executor(context);
            }
            else {
                output = this.generateStaticOutput(context);
            }
            const result = {
                phase: 'explore',
                status: 'completed',
                startTime,
                endTime: Date.now(),
                output,
            };
            this.emit('phase:complete', result);
            return result;
        }
        catch (error) {
            const result = {
                phase: 'explore',
                status: 'failed',
                startTime,
                endTime: Date.now(),
                error: error instanceof Error ? error.message : 'Unknown error',
            };
            this.emit('phase:error', result);
            return result;
        }
    }
    generateStaticOutput(context) {
        return {
            approaches: [
                {
                    id: 'approach-1',
                    name: 'Standard Implementation',
                    description: `Standard approach for ${context.prdItem.title}`,
                    pros: ['Well-tested patterns', 'Easy to maintain'],
                    cons: ['May not be optimal'],
                    complexity: 'medium',
                    recommended: true,
                },
            ],
            selectedApproach: 'approach-1',
            parallelExploration: false,
        };
    }
    /**
     * 判断是否需要并行探索
     */
    shouldExploreInParallel(context) {
        // 基于复杂度和风险判断
        const hasHighRisk = context.vision.risks.length > 2;
        const hasMultipleGoals = context.vision.goals.length > 3;
        const isLargeEffort = context.prdItem.estimatedEffort === 'large' || context.prdItem.estimatedEffort === 'xlarge';
        return hasHighRisk || (hasMultipleGoals && isLargeEffort);
    }
}
/**
 * ReviewPhase - 计划评审阶段
 */
export class ReviewPhase extends EventEmitter {
    constructor(executor) {
        super();
        this.executor = executor;
    }
    async execute(context) {
        const startTime = Date.now();
        this.emit('phase:start', { phase: 'review' });
        try {
            let output;
            if (this.executor) {
                output = await this.executor(context);
            }
            else {
                output = this.generateStaticOutput(context);
            }
            const result = {
                phase: 'review',
                status: 'completed',
                startTime,
                endTime: Date.now(),
                output,
            };
            this.emit('phase:complete', result);
            return result;
        }
        catch (error) {
            const result = {
                phase: 'review',
                status: 'failed',
                startTime,
                endTime: Date.now(),
                error: error instanceof Error ? error.message : 'Unknown error',
            };
            this.emit('phase:error', result);
            return result;
        }
    }
    generateStaticOutput(context) {
        return {
            approved: true,
            feedback: ['Plan looks good', 'Ready for implementation'],
            requiredChanges: [],
            reviewers: ['AI Reviewer'],
        };
    }
}
/**
 * ImplementPhase - 代码实现阶段
 */
export class ImplementPhase extends EventEmitter {
    constructor(executor) {
        super();
        this.executor = executor;
    }
    async execute(context) {
        const startTime = Date.now();
        this.emit('phase:start', { phase: 'implement' });
        try {
            let output;
            if (this.executor) {
                output = await this.executor(context);
            }
            else {
                output = this.generateStaticOutput(context);
            }
            const result = {
                phase: 'implement',
                status: 'completed',
                startTime,
                endTime: Date.now(),
                output,
            };
            this.emit('phase:complete', result);
            return result;
        }
        catch (error) {
            const result = {
                phase: 'implement',
                status: 'failed',
                startTime,
                endTime: Date.now(),
                error: error instanceof Error ? error.message : 'Unknown error',
            };
            this.emit('phase:error', result);
            return result;
        }
    }
    generateStaticOutput(context) {
        return {
            filesCreated: [],
            filesModified: [],
            testsAdded: [],
            documentation: [],
        };
    }
}
/**
 * QAPhase - 质量检查阶段
 */
export class QAPhase extends EventEmitter {
    constructor(executor) {
        super();
        this.executor = executor;
    }
    async execute(context) {
        const startTime = Date.now();
        this.emit('phase:start', { phase: 'qa' });
        try {
            let output;
            if (this.executor) {
                output = await this.executor(context);
            }
            else {
                output = this.generateStaticOutput(context);
            }
            const result = {
                phase: 'qa',
                status: 'completed',
                startTime,
                endTime: Date.now(),
                output,
            };
            this.emit('phase:complete', result);
            return result;
        }
        catch (error) {
            const result = {
                phase: 'qa',
                status: 'failed',
                startTime,
                endTime: Date.now(),
                error: error instanceof Error ? error.message : 'Unknown error',
            };
            this.emit('phase:error', result);
            return result;
        }
    }
    generateStaticOutput(context) {
        return {
            testsRun: 10,
            testsPassed: 10,
            testsFailed: 0,
            coverage: 80,
            issues: [],
            approved: true,
        };
    }
}
/**
 * ExecutionLoop - 执行循环编排器
 */
export class ExecutionLoop extends EventEmitter {
    constructor(config = {}, executors) {
        super();
        this.config = { ...DEFAULT_CONFIG, ...config };
        this.phases = {
            research: new ResearchPhase(executors?.research),
            explore: new ExplorePhase(executors?.explore),
            review: new ReviewPhase(executors?.review),
            implement: new ImplementPhase(executors?.implement),
            qa: new QAPhase(executors?.qa),
        };
        // 转发阶段事件
        for (const [name, phase] of Object.entries(this.phases)) {
            phase.on('phase:start', (data) => this.emit('phase:start', { ...data, phaseName: name }));
            phase.on('phase:complete', (data) => this.emit('phase:complete', { ...data, phaseName: name }));
            phase.on('phase:error', (data) => this.emit('phase:error', { ...data, phaseName: name }));
        }
    }
    /**
     * 执行完整循环
     */
    async execute(context) {
        this.state = {
            sessionId: context.sessionId,
            currentPhase: 'research',
            phaseResults: [],
            startTime: Date.now(),
            status: 'running',
            qaIterations: 0,
        };
        this.emit('loop:start', { sessionId: context.sessionId });
        const phaseOrder = ['research', 'explore', 'review', 'implement', 'qa'];
        for (const phase of phaseOrder) {
            this.state.currentPhase = phase;
            // 检查是否跳过 explore 阶段
            if (phase === 'explore' && this.shouldSkipExplore(context)) {
                const skipResult = {
                    phase: 'explore',
                    status: 'skipped',
                    startTime: Date.now(),
                    endTime: Date.now(),
                    skippedReason: 'Auto-skipped: simple task or no parallel exploration needed',
                };
                this.state.phaseResults.push(skipResult);
                context.previousResults.set('explore', skipResult);
                this.emit('phase:skipped', skipResult);
                continue;
            }
            // 执行阶段
            const result = await this.executePhase(phase, context);
            this.state.phaseResults.push(result);
            context.previousResults.set(phase, result);
            // 检查失败
            if (result.status === 'failed') {
                this.state.status = 'failed';
                this.state.endTime = Date.now();
                this.emit('loop:failed', { sessionId: context.sessionId, phase, error: result.error });
                await this.persistState();
                return this.state;
            }
            // Review 阶段检查
            if (phase === 'review' && this.config.requireReviewApproval) {
                const reviewOutput = result.output;
                if (!reviewOutput.approved) {
                    this.state.status = 'paused';
                    this.emit('loop:paused', { sessionId: context.sessionId, reason: 'Review not approved' });
                    await this.persistState();
                    return this.state;
                }
            }
            // QA 阶段检查
            if (phase === 'qa') {
                const qaOutput = result.output;
                if (!qaOutput.approved && this.state.qaIterations < this.config.maxQAIterations) {
                    this.state.qaIterations++;
                    // 回到 implement 阶段
                    const implResult = await this.executePhase('implement', context);
                    this.state.phaseResults.push(implResult);
                    context.previousResults.set('implement', implResult);
                    // 重新执行 QA
                    const qaRetryResult = await this.executePhase('qa', context);
                    this.state.phaseResults.push(qaRetryResult);
                    context.previousResults.set('qa', qaRetryResult);
                }
            }
        }
        this.state.status = 'completed';
        this.state.endTime = Date.now();
        this.emit('loop:complete', { sessionId: context.sessionId, state: this.state });
        await this.persistState();
        return this.state;
    }
    /**
     * 执行单个阶段
     */
    async executePhase(phase, context) {
        switch (phase) {
            case 'research':
                return this.phases.research.execute(context);
            case 'explore':
                return this.phases.explore.execute(context);
            case 'review':
                return this.phases.review.execute(context);
            case 'implement':
                return this.phases.implement.execute(context);
            case 'qa':
                return this.phases.qa.execute(context);
        }
    }
    /**
     * 跳过阶段
     */
    async skipPhase(phase, reason) {
        if (!this.state)
            return;
        const skipResult = {
            phase,
            status: 'skipped',
            startTime: Date.now(),
            endTime: Date.now(),
            skippedReason: reason,
        };
        this.state.phaseResults.push(skipResult);
        this.emit('phase:skipped', skipResult);
    }
    /**
     * 判断是否跳过 explore 阶段
     */
    shouldSkipExplore(context) {
        if (this.config.autoSkipExplore)
            return true;
        return !this.phases.explore.shouldExploreInParallel(context);
    }
    /**
     * 恢复执行
     */
    async resume(context) {
        if (!this.state || this.state.status !== 'paused') {
            throw new Error('No paused execution to resume');
        }
        this.state.status = 'running';
        this.emit('loop:resumed', { sessionId: context.sessionId });
        // 从当前阶段继续
        const phaseOrder = ['research', 'explore', 'review', 'implement', 'qa'];
        const currentIndex = phaseOrder.indexOf(this.state.currentPhase);
        for (let i = currentIndex + 1; i < phaseOrder.length; i++) {
            const phase = phaseOrder[i];
            this.state.currentPhase = phase;
            const result = await this.executePhase(phase, context);
            this.state.phaseResults.push(result);
            context.previousResults.set(phase, result);
            if (result.status === 'failed') {
                this.state.status = 'failed';
                this.state.endTime = Date.now();
                await this.persistState();
                return this.state;
            }
        }
        this.state.status = 'completed';
        this.state.endTime = Date.now();
        await this.persistState();
        return this.state;
    }
    /**
     * 持久化状态
     */
    async persistState() {
        if (!this.config.persistState || !this.state)
            return;
        const outputDir = this.config.outputDir;
        if (!fs.existsSync(outputDir)) {
            fs.mkdirSync(outputDir, { recursive: true });
        }
        const statePath = path.join(outputDir, `${this.state.sessionId}-state.json`);
        fs.writeFileSync(statePath, JSON.stringify(this.state, null, 2), 'utf-8');
    }
    /**
     * 加载状态
     */
    async loadState(sessionId) {
        const statePath = path.join(this.config.outputDir, `${sessionId}-state.json`);
        if (!fs.existsSync(statePath)) {
            return null;
        }
        const content = fs.readFileSync(statePath, 'utf-8');
        this.state = JSON.parse(content);
        return this.state;
    }
    /**
     * 获取当前状态
     */
    getState() {
        return this.state;
    }
    /**
     * 获取阶段结果
     */
    getPhaseResult(phase) {
        return this.state?.phaseResults.find(r => r.phase === phase);
    }
    /**
     * 更新配置
     */
    updateConfig(config) {
        this.config = { ...this.config, ...config };
    }
    /**
     * 获取配置
     */
    getConfig() {
        return { ...this.config };
    }
}
//# sourceMappingURL=ExecutionLoop.js.map