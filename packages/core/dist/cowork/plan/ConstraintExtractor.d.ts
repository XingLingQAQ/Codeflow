/**
 * ConstraintExtractor - 约束集提取器
 * 来自 OpenSpec 的约束驱动能力
 */
import { EventEmitter } from 'events';
import { VisionDocument, Constraint, ConstraintSet, ConstraintType, ConstraintPriority, ConstraintExtractorConfig } from './types.js';
/**
 * ConstraintExtractor - 约束提取器
 */
export declare class ConstraintExtractor extends EventEmitter {
    private config;
    constructor(config?: Partial<ConstraintExtractorConfig>);
    /**
     * 从愿景文档提取约束集
     */
    extract(vision: VisionDocument): ConstraintSet;
    /**
     * 从文本提取约束
     */
    private extractFromText;
    /**
     * 分割成句子
     */
    private splitIntoSentences;
    /**
     * 推断约束类型
     */
    private inferConstraintType;
    /**
     * 推断优先级
     */
    private inferPriority;
    /**
     * 判断是否可验证
     */
    private isVerifiable;
    /**
     * 生成验证标准
     */
    private generateVerificationCriteria;
    /**
     * 去重约束
     */
    private deduplicateConstraints;
    /**
     * 生成默认约束
     */
    private generateDefaultConstraints;
    /**
     * 手动添加约束
     */
    addConstraint(constraintSet: ConstraintSet, constraint: Omit<Constraint, 'id'>): Constraint;
    /**
     * 移除约束
     */
    removeConstraint(constraintSet: ConstraintSet, constraintId: string): boolean;
    /**
     * 更新约束
     */
    updateConstraint(constraintSet: ConstraintSet, constraintId: string, updates: Partial<Omit<Constraint, 'id'>>): Constraint | null;
    /**
     * 按类型过滤约束
     */
    filterByType(constraintSet: ConstraintSet, type: ConstraintType): Constraint[];
    /**
     * 按优先级过滤约束
     */
    filterByPriority(constraintSet: ConstraintSet, priority: ConstraintPriority): Constraint[];
    /**
     * 获取必须满足的约束
     */
    getMustConstraints(constraintSet: ConstraintSet): Constraint[];
    /**
     * 获取可验证的约束
     */
    getVerifiableConstraints(constraintSet: ConstraintSet): Constraint[];
    /**
     * 验证约束集完整性
     */
    validate(constraintSet: ConstraintSet): {
        valid: boolean;
        issues: string[];
    };
    /**
     * 更新配置
     */
    updateConfig(config: Partial<ConstraintExtractorConfig>): void;
    /**
     * 获取配置
     */
    getConfig(): ConstraintExtractorConfig;
    /**
     * 发送事件
     */
    private emitEvent;
}
//# sourceMappingURL=ConstraintExtractor.d.ts.map