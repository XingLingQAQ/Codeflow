/**
 * 语义冲突检测类型定义
 * S-P-O 三元组冲突探测
 */
import { Triple } from './types.js';
/**
 * 冲突类型
 */
export type ConflictType = 'contradiction' | 'temporal' | 'cardinality' | 'type_mismatch' | 'transitivity' | 'symmetry' | 'inverse';
/**
 * 冲突严重程度
 */
export type ConflictSeverity = 'low' | 'medium' | 'high' | 'critical';
/**
 * 冲突检测结果
 */
export interface ConflictResult {
    hasConflict: boolean;
    conflicts: Conflict[];
    checkedTriples: number;
    checkTime: number;
}
/**
 * 单个冲突
 */
export interface Conflict {
    id: string;
    type: ConflictType;
    severity: ConflictSeverity;
    existingTriple: Triple;
    newTriple: Triple;
    description: string;
    resolution?: ConflictResolution;
}
/**
 * 冲突解决方案
 */
export interface ConflictResolution {
    strategy: 'keep_existing' | 'replace' | 'merge' | 'manual';
    resolvedTriple?: Triple;
    reason: string;
}
/**
 * 谓词约束
 */
export interface PredicateConstraint {
    predicate: string;
    cardinality: 'single' | 'multiple';
    symmetric: boolean;
    transitive: boolean;
    inverse?: string;
    domain?: string[];
    range?: string[];
    mutuallyExclusive?: string[];
}
/**
 * 冲突检测配置
 */
export interface ConflictDetectionConfig {
    enableContradiction: boolean;
    enableTemporal: boolean;
    enableCardinality: boolean;
    enableTypeMismatch: boolean;
    enableTransitivity: boolean;
    enableSymmetry: boolean;
    enableInverse: boolean;
    confidenceThreshold: number;
    predicateConstraints: PredicateConstraint[];
}
/**
 * 冲突检测器接口
 */
export interface IConflictDetector {
    detect(newTriple: Triple, existingTriples: Triple[]): ConflictResult;
    detectBatch(newTriples: Triple[], existingTriples: Triple[]): ConflictResult;
    addConstraint(constraint: PredicateConstraint): void;
    removeConstraint(predicate: string): void;
    configure(config: Partial<ConflictDetectionConfig>): void;
    resolve(conflict: Conflict, strategy: ConflictResolution['strategy']): Triple | null;
}
/**
 * 默认冲突检测配置
 */
export declare const DEFAULT_CONFLICT_CONFIG: ConflictDetectionConfig;
/**
 * 常见谓词约束
 */
export declare const COMMON_PREDICATE_CONSTRAINTS: PredicateConstraint[];
//# sourceMappingURL=ConflictTypes.d.ts.map