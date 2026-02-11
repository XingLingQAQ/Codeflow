/**
 * PlanModeOrchestrator - Plan 模式编排器
 * 整合 OpenSpec 约束驱动和 Aha-Loop 愿景构建能力
 */
import { EventEmitter } from 'events';
import { VisionBuilder } from './VisionBuilder.js';
import { ConstraintExtractor } from './ConstraintExtractor.js';
import { PlanArtifactManager } from './PlanArtifactManager.js';
/**
 * 默认配置
 */
const DEFAULT_CONFIG = {
    outputDir: '.codeflow/changes',
    autoAdvance: false,
    requireApproval: true,
    language: 'en',
};
/**
 * PlanModeOrchestrator - Plan 模式编排器
 */
export class PlanModeOrchestrator extends EventEmitter {
    constructor(baseDir, config = {}) {
        super();
        this.sessions = new Map();
        this.currentSession = null;
        this.baseDir = baseDir;
        const mergedConfig = { ...DEFAULT_CONFIG, ...config };
        this.visionBuilder = new VisionBuilder({ language: mergedConfig.language });
        this.constraintExtractor = new ConstraintExtractor();
        this.artifactManager = new PlanArtifactManager(baseDir, {
            outputDir: mergedConfig.outputDir,
        });
        // 转发子组件事件
        this.visionBuilder.on('event', (event) => this.emitEvent(event));
        this.constraintExtractor.on('event', (event) => this.emitEvent(event));
        this.artifactManager.on('event', (event) => this.emitEvent(event));
    }
    /**
     * 启动 Plan 模式
     */
    async startPlanMode(name, config) {
        const sessionConfig = { ...DEFAULT_CONFIG, ...config };
        // 初始化工件目录
        await this.artifactManager.initialize();
        const session = {
            id: `plan-${Date.now()}`,
            name,
            status: 'running',
            currentPhase: 'vision',
            artifacts: [],
            config: sessionConfig,
            createdAt: Date.now(),
            updatedAt: Date.now(),
        };
        this.sessions.set(session.id, session);
        this.currentSession = session;
        this.emitEvent({ type: 'phase:start', phase: 'vision', sessionId: session.id });
        // 启动愿景构建
        this.visionBuilder.updateConfig({ language: sessionConfig.language });
        this.visionBuilder.start();
        return session;
    }
    /**
     * 获取当前愿景问题
     */
    getCurrentVisionQuestion() {
        return this.visionBuilder.getCurrentQuestion();
    }
    /**
     * 提交愿景回答
     */
    submitVisionAnswer(answer) {
        return this.visionBuilder.submitAnswer(answer);
    }
    /**
     * 跳过当前愿景问题
     */
    skipVisionQuestion() {
        return this.visionBuilder.skipQuestion();
    }
    /**
     * 完成愿景构建阶段
     */
    async completeVision(title) {
        if (!this.currentSession) {
            throw new Error('No active session');
        }
        const vision = this.visionBuilder.complete(title);
        this.currentSession.vision = vision;
        this.currentSession.updatedAt = Date.now();
        this.emitEvent({ type: 'phase:complete', phase: 'vision', sessionId: this.currentSession.id });
        // 自动进入约束提取阶段
        if (this.currentSession.config.autoAdvance) {
            await this.advanceToConstraints();
        }
        else {
            this.currentSession.currentPhase = 'constraints';
            this.emitEvent({ type: 'phase:start', phase: 'constraints', sessionId: this.currentSession.id });
        }
        return vision;
    }
    /**
     * 进入约束提取阶段
     */
    async advanceToConstraints() {
        if (!this.currentSession || !this.currentSession.vision) {
            throw new Error('Vision not completed');
        }
        this.currentSession.currentPhase = 'constraints';
        this.emitEvent({ type: 'phase:start', phase: 'constraints', sessionId: this.currentSession.id });
        const constraints = this.constraintExtractor.extract(this.currentSession.vision);
        this.currentSession.constraints = constraints;
        this.currentSession.updatedAt = Date.now();
        this.emitEvent({ type: 'phase:complete', phase: 'constraints', sessionId: this.currentSession.id });
        // 自动进入提案阶段
        if (this.currentSession.config.autoAdvance) {
            await this.advanceToProposal();
        }
        else {
            this.currentSession.currentPhase = 'proposal';
            this.emitEvent({ type: 'phase:start', phase: 'proposal', sessionId: this.currentSession.id });
        }
        return constraints;
    }
    /**
     * 进入提案生成阶段
     */
    async advanceToProposal() {
        if (!this.currentSession || !this.currentSession.vision || !this.currentSession.constraints) {
            throw new Error('Constraints not extracted');
        }
        this.currentSession.currentPhase = 'proposal';
        this.emitEvent({ type: 'phase:start', phase: 'proposal', sessionId: this.currentSession.id });
        const proposal = await this.artifactManager.createProposal(this.currentSession.vision, this.currentSession.constraints);
        this.currentSession.artifacts.push(proposal);
        this.currentSession.updatedAt = Date.now();
        this.emitEvent({ type: 'phase:complete', phase: 'proposal', sessionId: this.currentSession.id });
        return proposal;
    }
    /**
     * 创建 Spec 工件
     */
    async createSpec(name, requirement, scenarios) {
        if (!this.currentSession) {
            throw new Error('No active session');
        }
        const spec = await this.artifactManager.createSpec(name, requirement, scenarios);
        this.currentSession.artifacts.push(spec);
        this.currentSession.updatedAt = Date.now();
        return spec;
    }
    /**
     * 创建 Design 工件
     */
    async createDesign(title, overview, components, interfaces) {
        if (!this.currentSession) {
            throw new Error('No active session');
        }
        if (this.currentSession.currentPhase !== 'design') {
            this.currentSession.currentPhase = 'design';
            this.emitEvent({ type: 'phase:start', phase: 'design', sessionId: this.currentSession.id });
        }
        const design = await this.artifactManager.createDesign(title, overview, components, interfaces);
        this.currentSession.artifacts.push(design);
        this.currentSession.updatedAt = Date.now();
        this.emitEvent({ type: 'phase:complete', phase: 'design', sessionId: this.currentSession.id });
        return design;
    }
    /**
     * 创建 Tasks 工件
     */
    async createTasks(title, tasks, dependencies) {
        if (!this.currentSession) {
            throw new Error('No active session');
        }
        if (this.currentSession.currentPhase !== 'tasks') {
            this.currentSession.currentPhase = 'tasks';
            this.emitEvent({ type: 'phase:start', phase: 'tasks', sessionId: this.currentSession.id });
        }
        const tasksArtifact = await this.artifactManager.createTasks(title, tasks, dependencies);
        this.currentSession.artifacts.push(tasksArtifact);
        this.currentSession.updatedAt = Date.now();
        this.emitEvent({ type: 'phase:complete', phase: 'tasks', sessionId: this.currentSession.id });
        return tasksArtifact;
    }
    /**
     * Fast-forward: 一次性生成所有工件
     */
    async fastForward(title, specs, design, tasks) {
        if (!this.currentSession) {
            throw new Error('No active session');
        }
        const artifacts = [];
        // 1. 完成愿景（如果还没完成）
        if (!this.currentSession.vision) {
            // 使用默认回答快速完成愿景
            while (this.visionBuilder.getCurrentQuestion()) {
                const question = this.visionBuilder.getCurrentQuestion();
                if (question?.required) {
                    this.visionBuilder.submitAnswer(title);
                }
                else {
                    try {
                        this.visionBuilder.skipQuestion();
                    }
                    catch {
                        this.visionBuilder.submitAnswer('N/A');
                    }
                }
            }
            await this.completeVision(title);
        }
        // 2. 提取约束（如果还没提取）
        if (!this.currentSession.constraints) {
            await this.advanceToConstraints();
        }
        // 3. 生成 Proposal
        const proposal = await this.advanceToProposal();
        artifacts.push(proposal);
        // 4. 生成 Specs
        if (specs) {
            this.currentSession.currentPhase = 'specs';
            this.emitEvent({ type: 'phase:start', phase: 'specs', sessionId: this.currentSession.id });
            for (const spec of specs) {
                const specArtifact = await this.createSpec(spec.name, spec.requirement, spec.scenarios);
                artifacts.push(specArtifact);
            }
            this.emitEvent({ type: 'phase:complete', phase: 'specs', sessionId: this.currentSession.id });
        }
        // 5. 生成 Design
        if (design) {
            const designArtifact = await this.createDesign(`${title} - Design`, design.overview, design.components, design.interfaces);
            artifacts.push(designArtifact);
        }
        // 6. 生成 Tasks
        if (tasks) {
            const tasksArtifact = await this.createTasks(`${title} - Tasks`, tasks);
            artifacts.push(tasksArtifact);
        }
        return artifacts;
    }
    /**
     * 完成 Plan 模式
     */
    async completePlanMode() {
        if (!this.currentSession) {
            throw new Error('No active session');
        }
        this.currentSession.status = 'completed';
        this.currentSession.currentPhase = 'completed';
        this.currentSession.completedAt = Date.now();
        this.currentSession.updatedAt = Date.now();
        this.emitEvent({ type: 'session:complete', session: this.currentSession });
        const session = this.currentSession;
        this.currentSession = null;
        return session;
    }
    /**
     * 暂停 Plan 模式
     */
    pausePlanMode() {
        if (!this.currentSession) {
            throw new Error('No active session');
        }
        this.currentSession.status = 'paused';
        this.currentSession.updatedAt = Date.now();
    }
    /**
     * 恢复 Plan 模式
     */
    resumePlanMode() {
        if (!this.currentSession) {
            throw new Error('No active session');
        }
        this.currentSession.status = 'running';
        this.currentSession.updatedAt = Date.now();
    }
    /**
     * 取消 Plan 模式
     */
    cancelPlanMode() {
        if (!this.currentSession) {
            throw new Error('No active session');
        }
        this.currentSession.status = 'failed';
        this.currentSession.updatedAt = Date.now();
        this.emitEvent({
            type: 'session:error',
            sessionId: this.currentSession.id,
            error: 'Session cancelled by user',
        });
        this.currentSession = null;
    }
    /**
     * 获取当前会话
     */
    getCurrentSession() {
        return this.currentSession;
    }
    /**
     * 获取会话
     */
    getSession(id) {
        return this.sessions.get(id);
    }
    /**
     * 获取所有会话
     */
    getAllSessions() {
        return Array.from(this.sessions.values());
    }
    /**
     * 获取愿景构建器
     */
    getVisionBuilder() {
        return this.visionBuilder;
    }
    /**
     * 获取约束提取器
     */
    getConstraintExtractor() {
        return this.constraintExtractor;
    }
    /**
     * 获取工件管理器
     */
    getArtifactManager() {
        return this.artifactManager;
    }
    /**
     * 添加事件监听器
     */
    addListener(listener) {
        this.on('event', listener);
    }
    /**
     * 移除事件监听器
     */
    removeListener(listener) {
        this.off('event', listener);
    }
    /**
     * 清理资源
     */
    async cleanup() {
        this.visionBuilder.reset();
        await this.artifactManager.cleanup();
        this.sessions.clear();
        this.currentSession = null;
    }
    /**
     * 发送事件
     */
    emitEvent(event) {
        this.emit('event', event);
    }
}
//# sourceMappingURL=PlanModeOrchestrator.js.map