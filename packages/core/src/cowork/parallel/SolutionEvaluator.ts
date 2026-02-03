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
export type EvaluationDimension =
  | 'quality'        // 代码质量
  | 'performance'    // 性能
  | 'maintainability' // 可维护性
  | 'security'       // 安全性
  | 'completeness';  // 完整性

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
    scoreRange: { min: number; max: number };
  };
  dimensionComparison: {
    dimension: EvaluationDimension;
    scores: { workerId: string; score: number }[];
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
export type AIEvaluateCallback = (
  diffs: Diff[],
  dimension: EvaluationDimension
) => Promise<{ score: number; details: string[]; issues: EvaluationIssue[] }>;

/**
 * 默认配置
 */
const DEFAULT_CONFIG: EvaluatorConfig = {
  dimensions: ['quality', 'performance', 'maintainability', 'security', 'completeness'],
  weights: {
    quality: 0.25,
    performance: 0.2,
    maintainability: 0.25,
    security: 0.2,
    completeness: 0.1,
  },
  thresholds: {
    recommended: 80,
    acceptable: 60,
    needsImprovement: 40,
  },
};

/**
 * SolutionEvaluator - 方案评估器
 */
export class SolutionEvaluator extends EventEmitter {
  private config: EvaluatorConfig;
  private aiEvaluate?: AIEvaluateCallback;

  constructor(config: Partial<EvaluatorConfig> = {}, aiEvaluate?: AIEvaluateCallback) {
    super();
    this.config = { ...DEFAULT_CONFIG, ...config };
    this.aiEvaluate = aiEvaluate;
  }

  /**
   * 评估单个方案
   */
  async evaluate(
    workerId: string,
    workerName: string,
    modelId: string,
    result: WorkerResult
  ): Promise<SolutionEvaluation> {
    const diffs = result.diffs || [];
    const scores: DimensionScore[] = [];
    const allIssues: EvaluationIssue[] = [];

    // 评估每个维度
    for (const dimension of this.config.dimensions) {
      const weight = this.config.weights[dimension] || 0.2;
      let score: DimensionScore;

      if (this.aiEvaluate) {
        const aiResult = await this.aiEvaluate(diffs, dimension);
        score = {
          dimension,
          score: aiResult.score,
          weight,
          details: aiResult.details,
          issues: aiResult.issues,
        };
      } else {
        score = this.evaluateDimensionStatic(dimension, diffs, weight);
      }

      scores.push(score);
      allIssues.push(...score.issues);
    }

    // 计算总分
    const totalScore = this.calculateTotalScore(scores);

    // 计算指标
    const codeMetrics = this.calculateCodeMetrics(diffs);
    const performanceMetrics = this.calculatePerformanceMetrics(diffs);
    const maintainabilityMetrics = this.calculateMaintainabilityMetrics(diffs);

    // 确定推荐级别
    const recommendation = this.determineRecommendation(totalScore);

    return {
      workerId,
      workerName,
      modelId,
      scores,
      totalScore,
      rank: 0, // 将在比较时设置
      codeMetrics,
      performanceMetrics,
      maintainabilityMetrics,
      issues: allIssues,
      recommendation,
      evaluatedAt: Date.now(),
    };
  }

  /**
   * 评估并比较多个方案
   */
  async evaluateAndCompare(
    results: { workerId: string; workerName: string; modelId: string; result: WorkerResult }[]
  ): Promise<ComparisonReport> {
    const evaluations: SolutionEvaluation[] = [];

    // 评估所有方案
    for (const { workerId, workerName, modelId, result } of results) {
      const evaluation = await this.evaluate(workerId, workerName, modelId, result);
      evaluations.push(evaluation);
    }

    // 按总分排序并设置排名
    evaluations.sort((a, b) => b.totalScore - a.totalScore);
    evaluations.forEach((e, i) => (e.rank = i + 1));

    // 生成对比报告
    return this.generateComparisonReport(evaluations);
  }

  /**
   * 静态评估维度
   */
  private evaluateDimensionStatic(
    dimension: EvaluationDimension,
    diffs: Diff[],
    weight: number
  ): DimensionScore {
    const details: string[] = [];
    const issues: EvaluationIssue[] = [];
    let score = 70; // 基础分

    switch (dimension) {
      case 'quality':
        score = this.evaluateQuality(diffs, details, issues);
        break;
      case 'performance':
        score = this.evaluatePerformance(diffs, details, issues);
        break;
      case 'maintainability':
        score = this.evaluateMaintainability(diffs, details, issues);
        break;
      case 'security':
        score = this.evaluateSecurity(diffs, details, issues);
        break;
      case 'completeness':
        score = this.evaluateCompleteness(diffs, details, issues);
        break;
    }

    return { dimension, score, weight, details, issues };
  }

  /**
   * 评估代码质量
   */
  private evaluateQuality(diffs: Diff[], details: string[], issues: EvaluationIssue[]): number {
    let score = 75;

    const totalAdditions = diffs.reduce((sum, d) => sum + d.additions, 0);
    const totalDeletions = diffs.reduce((sum, d) => sum + d.deletions, 0);

    // 检查变更规模
    if (totalAdditions > 500) {
      score -= 10;
      issues.push({
        severity: 'medium',
        dimension: 'quality',
        description: 'Large number of additions may indicate over-engineering',
      });
    }

    // 检查删除比例
    const ratio = totalDeletions / (totalAdditions || 1);
    if (ratio > 0.5) {
      score += 5;
      details.push('Good refactoring: significant code cleanup');
    }

    // 检查文件数量
    if (diffs.length > 10) {
      score -= 5;
      issues.push({
        severity: 'low',
        dimension: 'quality',
        description: 'Changes span many files, may be hard to review',
      });
    }

    details.push(`Total changes: +${totalAdditions}/-${totalDeletions} across ${diffs.length} files`);

    return Math.max(0, Math.min(100, score));
  }

  /**
   * 评估性能
   */
  private evaluatePerformance(diffs: Diff[], details: string[], issues: EvaluationIssue[]): number {
    let score = 75;

    for (const diff of diffs) {
      const content = diff.hunks.map(h => h.content).join('\n').toLowerCase();

      // 检查潜在性能问题
      if (content.includes('for') && content.includes('for')) {
        score -= 5;
        issues.push({
          severity: 'medium',
          dimension: 'performance',
          description: 'Nested loops detected, may cause O(n²) complexity',
          file: diff.file,
        });
      }

      if (content.includes('settimeout') || content.includes('setinterval')) {
        details.push('Uses timers - ensure proper cleanup');
      }

      if (content.includes('async') || content.includes('await')) {
        score += 3;
        details.push('Uses async/await for non-blocking operations');
      }
    }

    return Math.max(0, Math.min(100, score));
  }

  /**
   * 评估可维护性
   */
  private evaluateMaintainability(diffs: Diff[], details: string[], issues: EvaluationIssue[]): number {
    let score = 75;

    for (const diff of diffs) {
      const content = diff.hunks.map(h => h.content).join('\n');

      // 检查注释
      const commentLines = (content.match(/\/\/|\/\*|\*\//g) || []).length;
      if (commentLines > 0) {
        score += 3;
        details.push('Contains documentation comments');
      }

      // 检查函数长度（简单启发式）
      const functionMatches = content.match(/function|=>/g) || [];
      if (functionMatches.length > 10) {
        score -= 5;
        issues.push({
          severity: 'low',
          dimension: 'maintainability',
          description: 'Many functions in single file, consider splitting',
          file: diff.file,
        });
      }

      // 检查类型注解（TypeScript）
      if (content.includes(': ') && (content.includes('string') || content.includes('number'))) {
        score += 5;
        details.push('Uses type annotations');
      }
    }

    return Math.max(0, Math.min(100, score));
  }

  /**
   * 评估安全性
   */
  private evaluateSecurity(diffs: Diff[], details: string[], issues: EvaluationIssue[]): number {
    let score = 80;

    for (const diff of diffs) {
      const content = diff.hunks.map(h => h.content).join('\n').toLowerCase();

      // 检查潜在安全问题
      if (content.includes('eval(') || content.includes('new function(')) {
        score -= 20;
        issues.push({
          severity: 'critical',
          dimension: 'security',
          description: 'Use of eval() or Function constructor is dangerous',
          file: diff.file,
          suggestion: 'Avoid dynamic code execution',
        });
      }

      if (content.includes('innerhtml')) {
        score -= 10;
        issues.push({
          severity: 'high',
          dimension: 'security',
          description: 'innerHTML usage may lead to XSS vulnerabilities',
          file: diff.file,
          suggestion: 'Use textContent or sanitize input',
        });
      }

      if (content.includes('password') && !content.includes('hash')) {
        score -= 5;
        issues.push({
          severity: 'medium',
          dimension: 'security',
          description: 'Password handling detected - ensure proper hashing',
          file: diff.file,
        });
      }

      // 正面检查
      if (content.includes('sanitize') || content.includes('escape')) {
        score += 5;
        details.push('Uses input sanitization');
      }
    }

    return Math.max(0, Math.min(100, score));
  }

  /**
   * 评估完整性
   */
  private evaluateCompleteness(diffs: Diff[], details: string[], issues: EvaluationIssue[]): number {
    let score = 70;

    // 检查是否有测试文件
    const hasTests = diffs.some(d => d.file.includes('test') || d.file.includes('spec'));
    if (hasTests) {
      score += 15;
      details.push('Includes test files');
    } else {
      issues.push({
        severity: 'medium',
        dimension: 'completeness',
        description: 'No test files included in changes',
        suggestion: 'Consider adding tests for new functionality',
      });
    }

    // 检查是否有类型定义
    const hasTypes = diffs.some(d => d.file.endsWith('.d.ts') || d.file.includes('types'));
    if (hasTypes) {
      score += 5;
      details.push('Includes type definitions');
    }

    // 检查变更数量
    if (diffs.length === 0) {
      score = 0;
      issues.push({
        severity: 'critical',
        dimension: 'completeness',
        description: 'No changes produced',
      });
    }

    return Math.max(0, Math.min(100, score));
  }

  /**
   * 计算总分
   */
  private calculateTotalScore(scores: DimensionScore[]): number {
    const totalWeight = scores.reduce((sum, s) => sum + s.weight, 0);
    const weightedSum = scores.reduce((sum, s) => sum + s.score * s.weight, 0);
    return Math.round(weightedSum / totalWeight);
  }

  /**
   * 计算代码指标
   */
  private calculateCodeMetrics(diffs: Diff[]): CodeQualityMetrics {
    const totalAdditions = diffs.reduce((sum, d) => sum + d.additions, 0);
    const totalDeletions = diffs.reduce((sum, d) => sum + d.deletions, 0);

    return {
      linesOfCode: totalAdditions,
      cyclomaticComplexity: Math.ceil(totalAdditions / 50), // 简单估算
      duplicateLines: 0, // 需要更复杂的分析
      codeSmells: 0, // 需要更复杂的分析
    };
  }

  /**
   * 计算性能指标
   */
  private calculatePerformanceMetrics(diffs: Diff[]): PerformanceMetrics {
    let asyncOps = 0;
    const bottlenecks: string[] = [];

    for (const diff of diffs) {
      const content = diff.hunks.map(h => h.content).join('\n');
      asyncOps += (content.match(/async|await|Promise/g) || []).length;

      if (content.includes('for') && content.includes('for')) {
        bottlenecks.push(`Nested loops in ${diff.file}`);
      }
    }

    return {
      estimatedTimeComplexity: bottlenecks.length > 0 ? 'O(n²)' : 'O(n)',
      estimatedSpaceComplexity: 'O(n)',
      asyncOperations: asyncOps,
      potentialBottlenecks: bottlenecks,
    };
  }

  /**
   * 计算可维护性指标
   */
  private calculateMaintainabilityMetrics(diffs: Diff[]): MaintainabilityMetrics {
    let readability = 70;
    let modularity = 70;

    // 简单启发式评估
    const avgLinesPerFile = diffs.reduce((sum, d) => sum + d.additions, 0) / (diffs.length || 1);
    if (avgLinesPerFile < 100) {
      modularity += 10;
    }

    return {
      readabilityScore: readability,
      modularity,
      coupling: 50, // 需要更复杂的分析
      cohesion: 60, // 需要更复杂的分析
      namingConsistency: 70, // 需要更复杂的分析
    };
  }

  /**
   * 确定推荐级别
   */
  private determineRecommendation(
    score: number
  ): SolutionEvaluation['recommendation'] {
    const { thresholds } = this.config;
    if (score >= thresholds.recommended) return 'recommended';
    if (score >= thresholds.acceptable) return 'acceptable';
    if (score >= thresholds.needsImprovement) return 'needs-improvement';
    return 'not-recommended';
  }

  /**
   * 生成对比报告
   */
  private generateComparisonReport(evaluations: SolutionEvaluation[]): ComparisonReport {
    const scores = evaluations.map(e => e.totalScore);
    const recommendedCount = evaluations.filter(e => e.recommendation === 'recommended').length;

    // 按维度比较
    const dimensionComparison = this.config.dimensions.map(dimension => {
      const dimScores = evaluations.map(e => ({
        workerId: e.workerId,
        score: e.scores.find(s => s.dimension === dimension)?.score || 0,
      }));
      dimScores.sort((a, b) => b.score - a.score);

      return {
        dimension,
        scores: dimScores,
        best: dimScores[0]?.workerId || '',
      };
    });

    return {
      id: `comparison-${Date.now()}`,
      evaluations,
      bestSolution: evaluations[0]?.workerId || '',
      summary: {
        totalSolutions: evaluations.length,
        recommendedCount,
        averageScore: Math.round(scores.reduce((a, b) => a + b, 0) / (scores.length || 1)),
        scoreRange: {
          min: Math.min(...scores),
          max: Math.max(...scores),
        },
      },
      dimensionComparison,
      createdAt: Date.now(),
    };
  }

  /**
   * 获取最佳方案
   */
  getBestSolution(report: ComparisonReport): SolutionEvaluation | undefined {
    return report.evaluations.find(e => e.workerId === report.bestSolution);
  }

  /**
   * 获取推荐方案
   */
  getRecommendedSolutions(report: ComparisonReport): SolutionEvaluation[] {
    return report.evaluations.filter(e => e.recommendation === 'recommended');
  }

  /**
   * 更新配置
   */
  updateConfig(config: Partial<EvaluatorConfig>): void {
    this.config = { ...this.config, ...config };
  }

  /**
   * 获取配置
   */
  getConfig(): EvaluatorConfig {
    return { ...this.config };
  }

  /**
   * 设置 AI 评估回调
   */
  setAICallback(callback: AIEvaluateCallback): void {
    this.aiEvaluate = callback;
  }
}
