/**
 * PlanModeOrchestrator - Plan 模式编排器
 * 整合 OpenSpec 约束驱动和 Aha-Loop 愿景构建能力
 */
import { EventEmitter } from 'events';
import { PlanSession, PlanConfig, PlanEventListener, VisionDocument, ConstraintSet, ArtifactMetadata } from './types.js';
import { VisionBuilder } from './VisionBuilder.js';
import { ConstraintExtractor } from './ConstraintExtractor.js';
import { PlanArtifactManager } from './PlanArtifactManager.js';
/**
 * PlanModeOrchestrator - Plan 模式编排器
 */
export declare class PlanModeOrchestrator extends EventEmitter {
    private visionBuilder;
    private constraintExtractor;
    private artifactManager;
    private sessions;
    private currentSession;
    private baseDir;
    constructor(baseDir: string, config?: Partial<PlanConfig>);
    /**
     * 启动 Plan 模式
     */
    startPlanMode(name: string, config?: Partial<PlanConfig>): Promise<PlanSession>;
    /**
     * 获取当前愿景问题
     */
    getCurrentVisionQuestion(): import("./types.js").VisionQuestion | null;
    /**
     * 提交愿景回答
     */
    submitVisionAnswer(answer: string): import("./types.js").VisionQuestion | null;
    /**
     * 跳过当前愿景问题
     */
    skipVisionQuestion(): import("./types.js").VisionQuestion | null;
    /**
     * 完成愿景构建阶段
     */
    completeVision(title: string): Promise<VisionDocument>;
    /**
     * 进入约束提取阶段
     */
    advanceToConstraints(): Promise<ConstraintSet>;
    /**
     * 进入提案生成阶段
     */
    advanceToProposal(): Promise<ArtifactMetadata>;
    /**
     * 创建 Spec 工件
     */
    createSpec(name: string, requirement: string, scenarios: {
        name: string;
        given: string;
        when: string;
        then: string;
    }[]): Promise<ArtifactMetadata>;
    /**
     * 创建 Design 工件
     */
    createDesign(title: string, overview: string, components: {
        name: string;
        responsibility: string;
        dependencies: string[];
    }[], interfaces: {
        name: string;
        methods: string[];
        description: string;
    }[]): Promise<ArtifactMetadata>;
    /**
     * 创建 Tasks 工件
     */
    createTasks(title: string, tasks: {
        title: string;
        description: string;
        priority: 'high' | 'medium' | 'low';
        files?: string[];
    }[], dependencies?: {
        taskIndex: number;
        dependsOn: number[];
    }[]): Promise<ArtifactMetadata>;
    /**
     * Fast-forward: 一次性生成所有工件
     */
    fastForward(title: string, specs?: {
        name: string;
        requirement: string;
        scenarios: {
            name: string;
            given: string;
            when: string;
            then: string;
        }[];
    }[], design?: {
        overview: string;
        components: {
            name: string;
            responsibility: string;
            dependencies: string[];
        }[];
        interfaces: {
            name: string;
            methods: string[];
            description: string;
        }[];
    }, tasks?: {
        title: string;
        description: string;
        priority: 'high' | 'medium' | 'low';
        files?: string[];
    }[]): Promise<ArtifactMetadata[]>;
    /**
     * 完成 Plan 模式
     */
    completePlanMode(): Promise<PlanSession>;
    /**
     * 暂停 Plan 模式
     */
    pausePlanMode(): void;
    /**
     * 恢复 Plan 模式
     */
    resumePlanMode(): void;
    /**
     * 取消 Plan 模式
     */
    cancelPlanMode(): void;
    /**
     * 获取当前会话
     */
    getCurrentSession(): PlanSession | null;
    /**
     * 获取会话
     */
    getSession(id: string): PlanSession | undefined;
    /**
     * 获取所有会话
     */
    getAllSessions(): PlanSession[];
    /**
     * 获取愿景构建器
     */
    getVisionBuilder(): VisionBuilder;
    /**
     * 获取约束提取器
     */
    getConstraintExtractor(): ConstraintExtractor;
    /**
     * 获取工件管理器
     */
    getArtifactManager(): PlanArtifactManager;
    /**
     * 添加事件监听器
     */
    addListener(listener: PlanEventListener): void;
    /**
     * 移除事件监听器
     */
    removeListener(listener: PlanEventListener): void;
    /**
     * 清理资源
     */
    cleanup(): Promise<void>;
    /**
     * 发送事件
     */
    private emitEvent;
}
//# sourceMappingURL=PlanModeOrchestrator.d.ts.map