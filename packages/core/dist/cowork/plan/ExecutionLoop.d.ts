/**
 * ExecutionLoop - 执行循环
 * 实现 Aha-Loop 的 5 阶段执行循环：Research → Explore → Review → Implement → QA
 */
import { EventEmitter } from 'events';
import { VisionDocument, ConstraintSet, ArchitectureArtifact, RoadmapArtifact, PRDItem } from './types.js';
/**
 * 执行阶段
 */
export type ExecutionPhase = 'research' | 'explore' | 'review' | 'implement' | 'qa';
/**
 * 阶段状态
 */
export type PhaseStatus = 'pending' | 'running' | 'completed' | 'skipped' | 'failed';
/**
 * 阶段结果
 */
export interface PhaseResult {
    phase: ExecutionPhase;
    status: PhaseStatus;
    startTime: number;
    endTime?: number;
    output?: unknown;
    error?: string;
    skippedReason?: string;
}
/**
 * 执行上下文
 */
export interface ExecutionContext {
    sessionId: string;
    prdItem: PRDItem;
    vision: VisionDocument;
    constraints: ConstraintSet;
    architecture?: ArchitectureArtifact;
    roadmap?: RoadmapArtifact;
    previousResults: Map<ExecutionPhase, PhaseResult>;
}
/**
 * Research 阶段输出
 */
export interface ResearchOutput {
    findings: string[];
    recommendations: string[];
    risks: string[];
    dependencies: string[];
}
/**
 * Explore 阶段输出
 */
export interface ExploreOutput {
    approaches: ExploreApproach[];
    selectedApproach?: string;
    parallelExploration: boolean;
}
/**
 * 探索方案
 */
export interface ExploreApproach {
    id: string;
    name: string;
    description: string;
    pros: string[];
    cons: string[];
    complexity: 'low' | 'medium' | 'high';
    recommended: boolean;
}
/**
 * Review 阶段输出
 */
export interface ReviewOutput {
    approved: boolean;
    feedback: string[];
    requiredChanges: string[];
    reviewers: string[];
}
/**
 * Implement 阶段输出
 */
export interface ImplementOutput {
    filesCreated: string[];
    filesModified: string[];
    testsAdded: string[];
    documentation: string[];
}
/**
 * QA 阶段输出
 */
export interface QAOutput {
    testsRun: number;
    testsPassed: number;
    testsFailed: number;
    coverage?: number;
    issues: QAIssue[];
    approved: boolean;
}
/**
 * QA 问题
 */
export interface QAIssue {
    severity: 'critical' | 'major' | 'minor';
    description: string;
    file?: string;
    line?: number;
    suggestion?: string;
}
/**
 * 阶段执行器回调
 */
export type PhaseExecutor<T> = (context: ExecutionContext) => Promise<T>;
/**
 * 执行循环配置
 */
export interface ExecutionLoopConfig {
    outputDir: string;
    autoSkipExplore: boolean;
    requireReviewApproval: boolean;
    maxQAIterations: number;
    persistState: boolean;
    phaseModels: {
        research?: string;
        explore?: string;
        review?: string;
        implement?: string;
        qa?: string;
    };
}
/**
 * 执行循环状态
 */
export interface ExecutionLoopState {
    sessionId: string;
    currentPhase: ExecutionPhase;
    phaseResults: PhaseResult[];
    startTime: number;
    endTime?: number;
    status: 'running' | 'completed' | 'failed' | 'paused';
    qaIterations: number;
}
/**
 * ResearchPhase - 技术研究阶段
 */
export declare class ResearchPhase extends EventEmitter {
    private executor?;
    constructor(executor?: PhaseExecutor<ResearchOutput>);
    execute(context: ExecutionContext): Promise<PhaseResult>;
    private generateStaticOutput;
}
/**
 * ExplorePhase - 并行探索阶段
 */
export declare class ExplorePhase extends EventEmitter {
    private executor?;
    constructor(executor?: PhaseExecutor<ExploreOutput>);
    execute(context: ExecutionContext): Promise<PhaseResult>;
    private generateStaticOutput;
    /**
     * 判断是否需要并行探索
     */
    shouldExploreInParallel(context: ExecutionContext): boolean;
}
/**
 * ReviewPhase - 计划评审阶段
 */
export declare class ReviewPhase extends EventEmitter {
    private executor?;
    constructor(executor?: PhaseExecutor<ReviewOutput>);
    execute(context: ExecutionContext): Promise<PhaseResult>;
    private generateStaticOutput;
}
/**
 * ImplementPhase - 代码实现阶段
 */
export declare class ImplementPhase extends EventEmitter {
    private executor?;
    constructor(executor?: PhaseExecutor<ImplementOutput>);
    execute(context: ExecutionContext): Promise<PhaseResult>;
    private generateStaticOutput;
}
/**
 * QAPhase - 质量检查阶段
 */
export declare class QAPhase extends EventEmitter {
    private executor?;
    constructor(executor?: PhaseExecutor<QAOutput>);
    execute(context: ExecutionContext): Promise<PhaseResult>;
    private generateStaticOutput;
}
/**
 * ExecutionLoop - 执行循环编排器
 */
export declare class ExecutionLoop extends EventEmitter {
    private config;
    private state?;
    private phases;
    constructor(config?: Partial<ExecutionLoopConfig>, executors?: {
        research?: PhaseExecutor<ResearchOutput>;
        explore?: PhaseExecutor<ExploreOutput>;
        review?: PhaseExecutor<ReviewOutput>;
        implement?: PhaseExecutor<ImplementOutput>;
        qa?: PhaseExecutor<QAOutput>;
    });
    /**
     * 执行完整循环
     */
    execute(context: ExecutionContext): Promise<ExecutionLoopState>;
    /**
     * 执行单个阶段
     */
    executePhase(phase: ExecutionPhase, context: ExecutionContext): Promise<PhaseResult>;
    /**
     * 跳过阶段
     */
    skipPhase(phase: ExecutionPhase, reason: string): Promise<void>;
    /**
     * 判断是否跳过 explore 阶段
     */
    private shouldSkipExplore;
    /**
     * 恢复执行
     */
    resume(context: ExecutionContext): Promise<ExecutionLoopState>;
    /**
     * 持久化状态
     */
    private persistState;
    /**
     * 加载状态
     */
    loadState(sessionId: string): Promise<ExecutionLoopState | null>;
    /**
     * 获取当前状态
     */
    getState(): ExecutionLoopState | undefined;
    /**
     * 获取阶段结果
     */
    getPhaseResult(phase: ExecutionPhase): PhaseResult | undefined;
    /**
     * 更新配置
     */
    updateConfig(config: Partial<ExecutionLoopConfig>): void;
    /**
     * 获取配置
     */
    getConfig(): ExecutionLoopConfig;
}
//# sourceMappingURL=ExecutionLoop.d.ts.map