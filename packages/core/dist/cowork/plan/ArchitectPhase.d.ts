/**
 * ArchitectPhase - 架构设计阶段
 * 实现 Aha-Loop 的 Architect 阶段，支持技术选型和架构设计
 */
import { EventEmitter } from 'events';
import { VisionDocument, ConstraintSet, ArchitectureArtifact, RoadmapArtifact, TechResearchResult } from './types.js';
/**
 * AI 回调类型 - 技术研究
 */
export type TechResearchCallback = (topic: string, context: string) => Promise<TechResearchResult>;
/**
 * AI 回调类型 - 架构设计
 */
export type ArchitectureDesignCallback = (vision: VisionDocument, constraints: ConstraintSet, research: TechResearchResult[]) => Promise<ArchitectureArtifact['content']>;
/**
 * AI 回调类型 - Roadmap 生成
 */
export type RoadmapGenerateCallback = (vision: VisionDocument, architecture: ArchitectureArtifact) => Promise<RoadmapArtifact['content']>;
/**
 * TechResearcher 配置
 */
export interface TechResearcherConfig {
    maxTopics: number;
    cacheResults: boolean;
    modelId?: string;
}
/**
 * ArchitectureDesigner 配置
 */
export interface ArchitectureDesignerConfig {
    includeSecurityAnalysis: boolean;
    includeScalabilityPlan: boolean;
    modelId?: string;
}
/**
 * RoadmapGenerator 配置
 */
export interface RoadmapGeneratorConfig {
    defaultMilestoneCount: number;
    estimateTimeline: boolean;
    modelId?: string;
}
/**
 * ArchitectPhase 配置
 */
export interface ArchitectPhaseConfig {
    outputDir: string;
    researcher: TechResearcherConfig;
    designer: ArchitectureDesignerConfig;
    roadmap: RoadmapGeneratorConfig;
}
/**
 * TechResearcher - 技术研究器
 */
export declare class TechResearcher extends EventEmitter {
    private config;
    private cache;
    private aiCallback?;
    constructor(config?: Partial<TechResearcherConfig>, aiCallback?: TechResearchCallback);
    /**
     * 研究技术主题
     */
    research(topic: string, context: string): Promise<TechResearchResult>;
    /**
     * 批量研究
     */
    researchBatch(topics: Array<{
        topic: string;
        context: string;
    }>): Promise<TechResearchResult[]>;
    /**
     * 从愿景提取研究主题
     */
    extractTopicsFromVision(vision: VisionDocument): Array<{
        topic: string;
        context: string;
    }>;
    private containsTechKeyword;
    private extractTechTopic;
    private generateStaticResearch;
    /**
     * 清除缓存
     */
    clearCache(): void;
    /**
     * 获取缓存的研究结果
     */
    getCachedResults(): TechResearchResult[];
}
/**
 * ArchitectureDesigner - 架构设计器
 */
export declare class ArchitectureDesigner extends EventEmitter {
    private config;
    private aiCallback?;
    constructor(config?: Partial<ArchitectureDesignerConfig>, aiCallback?: ArchitectureDesignCallback);
    /**
     * 设计架构
     */
    design(vision: VisionDocument, constraints: ConstraintSet, research: TechResearchResult[]): Promise<ArchitectureArtifact>;
    private generateStaticArchitecture;
    /**
     * 生成架构 Markdown
     */
    generateMarkdown(architecture: ArchitectureArtifact): string;
}
/**
 * RoadmapGenerator - 里程碑规划器
 */
export declare class RoadmapGenerator extends EventEmitter {
    private config;
    private aiCallback?;
    constructor(config?: Partial<RoadmapGeneratorConfig>, aiCallback?: RoadmapGenerateCallback);
    /**
     * 生成 Roadmap
     */
    generate(vision: VisionDocument, architecture: ArchitectureArtifact): Promise<RoadmapArtifact>;
    private generateStaticRoadmap;
    /**
     * 生成 Roadmap JSON
     */
    generateJSON(roadmap: RoadmapArtifact): string;
    /**
     * 生成 Roadmap Markdown
     */
    generateMarkdown(roadmap: RoadmapArtifact): string;
}
/**
 * ArchitectPhase - 架构设计阶段编排器
 */
export declare class ArchitectPhase extends EventEmitter {
    private config;
    private researcher;
    private designer;
    private roadmapGenerator;
    private researchResults;
    private architecture?;
    private roadmap?;
    constructor(config?: Partial<ArchitectPhaseConfig>, callbacks?: {
        research?: TechResearchCallback;
        design?: ArchitectureDesignCallback;
        roadmap?: RoadmapGenerateCallback;
    });
    /**
     * 执行架构设计阶段
     */
    execute(vision: VisionDocument, constraints: ConstraintSet): Promise<{
        research: TechResearchResult[];
        architecture: ArchitectureArtifact;
        roadmap: RoadmapArtifact;
    }>;
    /**
     * 保存工件到文件系统
     */
    private saveArtifacts;
    /**
     * 获取研究结果
     */
    getResearchResults(): TechResearchResult[];
    /**
     * 获取架构
     */
    getArchitecture(): ArchitectureArtifact | undefined;
    /**
     * 获取 Roadmap
     */
    getRoadmap(): RoadmapArtifact | undefined;
    /**
     * 更新配置
     */
    updateConfig(config: Partial<ArchitectPhaseConfig>): void;
    /**
     * 获取配置
     */
    getConfig(): ArchitectPhaseConfig;
}
//# sourceMappingURL=ArchitectPhase.d.ts.map