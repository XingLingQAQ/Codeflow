/**
 * SolutionEvaluator - 并行方案评估器
 * 支持多维度评估和比较并行执行的方案
 */
import { EventEmitter } from 'events';
import { Diff } from '../types.js';
import { WorkerResult } from './ResultCollector.js';
/**
 * 评估维度
 */
export type EvaluationDimension = 'quality' | 'performance' | 'maintainability' | 'security' | 'completeness';
/**
 * 评估分数（0-100）
 */
export interface DimensionScore {
    dimension: EvaluationDimension;
    score: number;
    weight: number;
    details: string[];
    issues: EvaluationIssue[];
}
/**
 * 评估问题
 */
export interface EvaluationIssue {
    severity: 'low' | 'medium' | 'high' | 'critical';
    dimension: EvaluationDimension;
    description: string;
    file?: string;
    line?: number;
    suggestion?: string;
}
/**
 * 代码质量指标
 */
export interface CodeQualityMetrics {
    linesOfCode: number;
    cyclomaticComplexity: number;
    duplicateLines: number;
    codeSmells: number;
    testCoverage?: number;
    documentationCoverage?: number;
}
/**
 * 性能指标
 */
export interface PerformanceMetrics {
    estimatedTimeComplexity: string;
    estimatedSpaceComplexity: string;
    asyncOperations: number;
    potentialBottlenecks: string[];
}
/**
 * 可维护性指标
 */
export interface MaintainabilityMetrics {
    readabilityScore: number;
    modularity: number;
    coupling: number;
    cohesion: number;
    namingConsistency: number;
}
/**
 * 方案评估结果
 */
export interface SolutionEvaluation {
    workerId: string;
    workerName: string;
    modelId: string;
    scores: DimensionScore[];
    totalScore: number;
    rank: number;
    codeMetrics: CodeQualityMetrics;
    performanceMetrics: PerformanceMetrics;
    maintainabilityMetrics: MaintainabilityMetrics;
    issues: EvaluationIssue[];
    recommendation: 'recommended' | 'acceptable' | 'needs-improvement' | 'not-recommended';
    evaluatedAt: number;
}
/**
 * 对比报告
 */
export interface ComparisonReport {
    id: string;
    evaluations: SolutionEvaluation[];
    bestSolution: string;
    summary: {
        totalSolutions: number;
        recommendedCount: number;
        averageScore: number;
        scoreRange: {
            min: number;
            max: number;
        };
    };
    dimensionComparison: {
        dimension: EvaluationDimension;
        scores: {
            workerId: string;
            score: number;
        }[];
        best: string;
    }[];
    createdAt: number;
}
/**
 * 评估器配置
 */
export interface EvaluatorConfig {
    dimensions: EvaluationDimension[];
    weights: Partial<Record<EvaluationDimension, number>>;
    thresholds: {
        recommended: number;
        acceptable: number;
        needsImprovement: number;
    };
}
/**
 * AI 评估回调
 */
export type AIEvaluateCallback = (diffs: Diff[], dimension: EvaluationDimension) => Promise<{
    score: number;
    details: string[];
    issues: EvaluationIssue[];
}>;
/**
 * SolutionEvaluator - 方案评估器
 */
export declare class SolutionEvaluator extends EventEmitter {
    private config;
    private aiEvaluate?;
    constructor(config?: Partial<EvaluatorConfig>, aiEvaluate?: AIEvaluateCallback);
    /**
     * 评估单个方案
     */
    evaluate(workerId: string, workerName: string, modelId: string, result: WorkerResult): Promise<SolutionEvaluation>;
    /**
     * 评估并比较多个方案
     */
    evaluateAndCompare(results: {
        workerId: string;
        workerName: string;
        modelId: string;
        result: WorkerResult;
    }[]): Promise<ComparisonReport>;
    /**
     * 静态评估维度
     */
    private evaluateDimensionStatic;
    /**
     * 评估代码质量
     */
    private evaluateQuality;
    /**
     * 评估性能
     */
    private evaluatePerformance;
    /**
     * 评估可维护性
     */
    private evaluateMaintainability;
    /**
     * 评估安全性
     */
    private evaluateSecurity;
    /**
     * 评估完整性
     */
    private evaluateCompleteness;
    /**
     * 计算总分
     */
    private calculateTotalScore;
    /**
     * 计算代码指标
     */
    private calculateCodeMetrics;
    /**
     * 计算性能指标
     */
    private calculatePerformanceMetrics;
    /**
     * 计算可维护性指标
     */
    private calculateMaintainabilityMetrics;
    /**
     * 确定推荐级别
     */
    private determineRecommendation;
    /**
     * 生成对比报告
     */
    private generateComparisonReport;
    /**
     * 获取最佳方案
     */
    getBestSolution(report: ComparisonReport): SolutionEvaluation | undefined;
    /**
     * 获取推荐方案
     */
    getRecommendedSolutions(report: ComparisonReport): SolutionEvaluation[];
    /**
     * 更新配置
     */
    updateConfig(config: Partial<EvaluatorConfig>): void;
    /**
     * 获取配置
     */
    getConfig(): EvaluatorConfig;
    /**
     * 设置 AI 评估回调
     */
    setAICallback(callback: AIEvaluateCallback): void;
}
//# sourceMappingURL=SolutionEvaluator.d.ts.map