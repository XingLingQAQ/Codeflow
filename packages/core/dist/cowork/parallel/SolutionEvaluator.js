/**
 * SolutionEvaluator - 并行方案评估器
 * 支持多维度评估和比较并行执行的方案
 */
import { EventEmitter } from 'events';
/**
 * 默认配置
 */
const DEFAULT_CONFIG = {
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
    constructor(config = {}, aiEvaluate) {
        super();
        this.config = { ...DEFAULT_CONFIG, ...config };
        this.aiEvaluate = aiEvaluate;
    }
    /**
     * 评估单个方案
     */
    async evaluate(workerId, workerName, modelId, result) {
        const diffs = result.diffs || [];
        const scores = [];
        const allIssues = [];
        // 评估每个维度
        for (const dimension of this.config.dimensions) {
            const weight = this.config.weights[dimension] || 0.2;
            let score;
            if (this.aiEvaluate) {
                const aiResult = await this.aiEvaluate(diffs, dimension);
                score = {
                    dimension,
                    score: aiResult.score,
                    weight,
                    details: aiResult.details,
                    issues: aiResult.issues,
                };
            }
            else {
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
    async evaluateAndCompare(results) {
        const evaluations = [];
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
    evaluateDimensionStatic(dimension, diffs, weight) {
        const details = [];
        const issues = [];
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
    evaluateQuality(diffs, details, issues) {
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
    evaluatePerformance(diffs, details, issues) {
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
    evaluateMaintainability(diffs, details, issues) {
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
    evaluateSecurity(diffs, details, issues) {
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
    evaluateCompleteness(diffs, details, issues) {
        let score = 70;
        // 检查是否有测试文件
        const hasTests = diffs.some(d => d.file.includes('test') || d.file.includes('spec'));
        if (hasTests) {
            score += 15;
            details.push('Includes test files');
        }
        else {
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
    calculateTotalScore(scores) {
        const totalWeight = scores.reduce((sum, s) => sum + s.weight, 0);
        const weightedSum = scores.reduce((sum, s) => sum + s.score * s.weight, 0);
        return Math.round(weightedSum / totalWeight);
    }
    /**
     * 计算代码指标
     */
    calculateCodeMetrics(diffs) {
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
    calculatePerformanceMetrics(diffs) {
        let asyncOps = 0;
        const bottlenecks = [];
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
    calculateMaintainabilityMetrics(diffs) {
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
    determineRecommendation(score) {
        const { thresholds } = this.config;
        if (score >= thresholds.recommended)
            return 'recommended';
        if (score >= thresholds.acceptable)
            return 'acceptable';
        if (score >= thresholds.needsImprovement)
            return 'needs-improvement';
        return 'not-recommended';
    }
    /**
     * 生成对比报告
     */
    generateComparisonReport(evaluations) {
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
    getBestSolution(report) {
        return report.evaluations.find(e => e.workerId === report.bestSolution);
    }
    /**
     * 获取推荐方案
     */
    getRecommendedSolutions(report) {
        return report.evaluations.filter(e => e.recommendation === 'recommended');
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
    /**
     * 设置 AI 评估回调
     */
    setAICallback(callback) {
        this.aiEvaluate = callback;
    }
}
//# sourceMappingURL=SolutionEvaluator.js.map