/**
 * ArtifactGenerators - OpenSpec 风格的工件生成器
 * 支持 AI 驱动的内容生成
 */
import { EventEmitter } from 'events';
import { VisionDocument, ConstraintSet, ProposalArtifact, SpecArtifact, DesignArtifact, TasksArtifact } from './types.js';
/**
 * 生成器配置
 */
export interface GeneratorConfig {
    language: 'en' | 'zh';
    modelId?: string;
    maxTokens?: number;
    temperature?: number;
}
/**
 * AI 生成回调
 */
export type AIGenerateCallback = (prompt: string, config?: GeneratorConfig) => Promise<string>;
/**
 * ProposalGenerator - 提案生成器
 */
export declare class ProposalGenerator extends EventEmitter {
    private config;
    private aiGenerate?;
    constructor(config?: Partial<GeneratorConfig>, aiGenerate?: AIGenerateCallback);
    /**
     * 生成提案内容
     */
    generate(vision: VisionDocument, constraints: ConstraintSet): Promise<ProposalArtifact['content']>;
    /**
     * 使用 AI 生成
     */
    private generateWithAI;
    /**
     * 静态生成（无 AI）
     */
    private generateStatic;
    /**
     * 构建提案 Prompt
     */
    private buildProposalPrompt;
    /**
     * 解析 AI 响应
     */
    private parseProposalResponse;
    private generateHowSection;
    private generateImpactSection;
    updateConfig(config: Partial<GeneratorConfig>): void;
}
/**
 * SpecGenerator - 规格生成器
 */
export declare class SpecGenerator extends EventEmitter {
    private config;
    private aiGenerate?;
    constructor(config?: Partial<GeneratorConfig>, aiGenerate?: AIGenerateCallback);
    /**
     * 从需求生成规格
     */
    generate(requirement: string, context?: {
        vision?: VisionDocument;
        constraints?: ConstraintSet;
    }): Promise<SpecArtifact['content']>;
    /**
     * 批量生成规格
     */
    generateBatch(requirements: string[], context?: {
        vision?: VisionDocument;
        constraints?: ConstraintSet;
    }): Promise<SpecArtifact['content'][]>;
    /**
     * 使用 AI 生成
     */
    private generateWithAI;
    /**
     * 静态生成
     */
    private generateStatic;
    /**
     * 从需求提取场景
     */
    private extractScenarios;
    /**
     * 构建规格 Prompt
     */
    private buildSpecPrompt;
    /**
     * 解析 AI 响应
     */
    private parseSpecResponse;
    updateConfig(config: Partial<GeneratorConfig>): void;
}
/**
 * DesignGenerator - 设计生成器
 */
export declare class DesignGenerator extends EventEmitter {
    private config;
    private aiGenerate?;
    constructor(config?: Partial<GeneratorConfig>, aiGenerate?: AIGenerateCallback);
    /**
     * 生成设计文档
     */
    generate(specs: SpecArtifact['content'][], constraints: ConstraintSet): Promise<DesignArtifact['content']>;
    /**
     * 使用 AI 生成
     */
    private generateWithAI;
    /**
     * 静态生成
     */
    private generateStatic;
    /**
     * 从规格提取组件
     */
    private extractComponents;
    /**
     * 从规格提取接口
     */
    private extractInterfaces;
    /**
     * 生成概述
     */
    private generateOverview;
    /**
     * 生成数据流
     */
    private generateDataFlow;
    /**
     * 构建设计 Prompt
     */
    private buildDesignPrompt;
    /**
     * 解析 AI 响应
     */
    private parseDesignResponse;
    updateConfig(config: Partial<GeneratorConfig>): void;
}
/**
 * TaskGenerator - 任务生成器
 */
export declare class TaskGenerator extends EventEmitter {
    private config;
    private aiGenerate?;
    constructor(config?: Partial<GeneratorConfig>, aiGenerate?: AIGenerateCallback);
    /**
     * 生成任务列表
     */
    generate(design: DesignArtifact['content'], specs: SpecArtifact['content'][]): Promise<TasksArtifact['content']>;
    /**
     * 使用 AI 生成
     */
    private generateWithAI;
    /**
     * 静态生成
     */
    private generateStatic;
    /**
     * 构建任务 Prompt
     */
    private buildTaskPrompt;
    /**
     * 解析 AI 响应
     */
    private parseTaskResponse;
    updateConfig(config: Partial<GeneratorConfig>): void;
}
/**
 * ArtifactGeneratorFactory - 生成器工厂
 */
export declare class ArtifactGeneratorFactory {
    private config;
    private aiGenerate?;
    constructor(config?: Partial<GeneratorConfig>, aiGenerate?: AIGenerateCallback);
    createProposalGenerator(): ProposalGenerator;
    createSpecGenerator(): SpecGenerator;
    createDesignGenerator(): DesignGenerator;
    createTaskGenerator(): TaskGenerator;
    /**
     * 一次性生成所有工件内容
     */
    generateAll(vision: VisionDocument, constraints: ConstraintSet, requirements?: string[]): Promise<{
        proposal: ProposalArtifact['content'];
        specs: SpecArtifact['content'][];
        design: DesignArtifact['content'];
        tasks: TasksArtifact['content'];
    }>;
    updateConfig(config: Partial<GeneratorConfig>): void;
    setAICallback(callback: AIGenerateCallback): void;
}
//# sourceMappingURL=ArtifactGenerators.d.ts.map