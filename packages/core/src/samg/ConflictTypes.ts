/**
 * 语义冲突检测类型定义
 * S-P-O 三元组冲突探测
 */

import { Triple, TripleNode, LiteralValue } from './types.js';

/**
 * 冲突类型
 */
export type ConflictType =
  | 'contradiction'      // 直接矛盾（同一主谓，不同宾语且互斥）
  | 'temporal'           // 时序冲突（时间线不一致）
  | 'cardinality'        // 基数冲突（单值属性多值）
  | 'type_mismatch'      // 类型不匹配
  | 'transitivity'       // 传递性冲突
  | 'symmetry'           // 对称性冲突
  | 'inverse';           // 逆关系冲突

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
export const DEFAULT_CONFLICT_CONFIG: ConflictDetectionConfig = {
  enableContradiction: true,
  enableTemporal: true,
  enableCardinality: true,
  enableTypeMismatch: true,
  enableTransitivity: false,
  enableSymmetry: false,
  enableInverse: false,
  confidenceThreshold: 0.5,
  predicateConstraints: [],
};

/**
 * 常见谓词约束
 */
export const COMMON_PREDICATE_CONSTRAINTS: PredicateConstraint[] = [
  {
    predicate: 'hasType',
    cardinality: 'multiple',
    symmetric: false,
    transitive: false,
  },
  {
    predicate: 'isA',
    cardinality: 'multiple',
    symmetric: false,
    transitive: true,
  },
  {
    predicate: 'equals',
    cardinality: 'single',
    symmetric: true,
    transitive: true,
  },
  {
    predicate: 'contains',
    cardinality: 'multiple',
    symmetric: false,
    transitive: true,
    inverse: 'containedIn',
  },
  {
    predicate: 'dependsOn',
    cardinality: 'multiple',
    symmetric: false,
    transitive: true,
  },
  {
    predicate: 'conflictsWith',
    cardinality: 'multiple',
    symmetric: true,
    transitive: false,
    mutuallyExclusive: ['compatibleWith'],
  },
  {
    predicate: 'compatibleWith',
    cardinality: 'multiple',
    symmetric: true,
    transitive: false,
    mutuallyExclusive: ['conflictsWith'],
  },
];
