/**
 * 语义冲突检测器实现
 * S-P-O 三元组冲突探测
 */

import {
  IConflictDetector,
  ConflictDetectionConfig,
  ConflictResult,
  Conflict,
  ConflictType,
  ConflictSeverity,
  ConflictResolution,
  PredicateConstraint,
  DEFAULT_CONFLICT_CONFIG,
  COMMON_PREDICATE_CONSTRAINTS,
} from './ConflictTypes.js';
import { Triple, TripleNode, LiteralValue, isLiteralValue } from './types.js';

export class ConflictDetector implements IConflictDetector {
  private config: ConflictDetectionConfig;
  private constraints: Map<string, PredicateConstraint> = new Map();
  private conflictIdCounter = 0;

  constructor(config: Partial<ConflictDetectionConfig> = {}) {
    this.config = { ...DEFAULT_CONFLICT_CONFIG, ...config };
    this.initializeConstraints();
  }

  private initializeConstraints(): void {
    for (const constraint of COMMON_PREDICATE_CONSTRAINTS) {
      this.constraints.set(constraint.predicate, constraint);
    }
    for (const constraint of this.config.predicateConstraints) {
      this.constraints.set(constraint.predicate, constraint);
    }
  }

  configure(config: Partial<ConflictDetectionConfig>): void {
    this.config = { ...this.config, ...config };
  }

  addConstraint(constraint: PredicateConstraint): void {
    this.constraints.set(constraint.predicate, constraint);
  }

  removeConstraint(predicate: string): void {
    this.constraints.delete(predicate);
  }

  detect(newTriple: Triple, existingTriples: Triple[]): ConflictResult {
    const startTime = Date.now();
    const conflicts: Conflict[] = [];

    for (const existing of existingTriples) {
      const detected = this.checkConflict(newTriple, existing);
      if (detected) {
        conflicts.push(detected);
      }
    }

    return {
      hasConflict: conflicts.length > 0,
      conflicts,
      checkedTriples: existingTriples.length,
      checkTime: Date.now() - startTime,
    };
  }

  detectBatch(newTriples: Triple[], existingTriples: Triple[]): ConflictResult {
    const startTime = Date.now();
    const conflicts: Conflict[] = [];

    for (const newTriple of newTriples) {
      for (const existing of existingTriples) {
        const detected = this.checkConflict(newTriple, existing);
        if (detected) {
          conflicts.push(detected);
        }
      }
    }

    return {
      hasConflict: conflicts.length > 0,
      conflicts,
      checkedTriples: existingTriples.length * newTriples.length,
      checkTime: Date.now() - startTime,
    };
  }

  resolve(conflict: Conflict, strategy: ConflictResolution['strategy']): Triple | null {
    switch (strategy) {
      case 'keep_existing':
        return conflict.existingTriple;

      case 'replace':
        return conflict.newTriple;

      case 'merge':
        return this.mergeTriples(conflict.existingTriple, conflict.newTriple);

      case 'manual':
      default:
        return null;
    }
  }

  // ==================== Private Methods ====================

  private checkConflict(newTriple: Triple, existing: Triple): Conflict | null {
    // 跳过低置信度三元组
    if (
      newTriple.confidence < this.config.confidenceThreshold ||
      existing.confidence < this.config.confidenceThreshold
    ) {
      return null;
    }

    // 检查各类冲突
    if (this.config.enableContradiction) {
      const contradiction = this.checkContradiction(newTriple, existing);
      if (contradiction) return contradiction;
    }

    if (this.config.enableCardinality) {
      const cardinality = this.checkCardinality(newTriple, existing);
      if (cardinality) return cardinality;
    }

    if (this.config.enableTypeMismatch) {
      const typeMismatch = this.checkTypeMismatch(newTriple, existing);
      if (typeMismatch) return typeMismatch;
    }

    if (this.config.enableTemporal) {
      const temporal = this.checkTemporal(newTriple, existing);
      if (temporal) return temporal;
    }

    if (this.config.enableSymmetry) {
      const symmetry = this.checkSymmetry(newTriple, existing);
      if (symmetry) return symmetry;
    }

    if (this.config.enableInverse) {
      const inverse = this.checkInverse(newTriple, existing);
      if (inverse) return inverse;
    }

    return null;
  }

  private checkContradiction(newTriple: Triple, existing: Triple): Conflict | null {
    // 同一主语和谓词，但宾语互斥
    if (
      this.isSameNode(newTriple.subject, existing.subject) &&
      newTriple.predicate === existing.predicate
    ) {
      const constraint = this.constraints.get(newTriple.predicate);

      // 检查互斥谓词
      if (constraint?.mutuallyExclusive) {
        for (const exclusive of constraint.mutuallyExclusive) {
          if (existing.predicate === exclusive) {
            return this.createConflict(
              'contradiction',
              'high',
              existing,
              newTriple,
              `Predicate "${newTriple.predicate}" is mutually exclusive with "${exclusive}"`
            );
          }
        }
      }

      // 检查布尔值矛盾
      if (
        isLiteralValue(newTriple.object) &&
        isLiteralValue(existing.object) &&
        typeof newTriple.object['@value'] === 'boolean' &&
        typeof existing.object['@value'] === 'boolean' &&
        newTriple.object['@value'] !== existing.object['@value']
      ) {
        return this.createConflict(
          'contradiction',
          'critical',
          existing,
          newTriple,
          `Boolean contradiction: ${existing.object['@value']} vs ${newTriple.object['@value']}`
        );
      }
    }

    return null;
  }

  private checkCardinality(newTriple: Triple, existing: Triple): Conflict | null {
    const constraint = this.constraints.get(newTriple.predicate);

    if (constraint?.cardinality === 'single') {
      if (
        this.isSameNode(newTriple.subject, existing.subject) &&
        newTriple.predicate === existing.predicate &&
        !this.isSameObject(newTriple.object, existing.object)
      ) {
        return this.createConflict(
          'cardinality',
          'medium',
          existing,
          newTriple,
          `Predicate "${newTriple.predicate}" has single cardinality but multiple values found`
        );
      }
    }

    return null;
  }

  private checkTypeMismatch(newTriple: Triple, existing: Triple): Conflict | null {
    const constraint = this.constraints.get(newTriple.predicate);

    if (constraint?.range && !isLiteralValue(newTriple.object)) {
      const objectType = newTriple.object['@type'];
      const types = Array.isArray(objectType) ? objectType : [objectType];

      if (!types.some(t => constraint.range!.includes(t || ''))) {
        return this.createConflict(
          'type_mismatch',
          'medium',
          existing,
          newTriple,
          `Object type "${objectType}" not in allowed range: ${constraint.range.join(', ')}`
        );
      }
    }

    return null;
  }

  private checkTemporal(newTriple: Triple, existing: Triple): Conflict | null {
    // 检查时间相关谓词的时序一致性
    const temporalPredicates = ['startedAt', 'endedAt', 'occurredAt', 'before', 'after'];

    if (
      temporalPredicates.includes(newTriple.predicate) &&
      temporalPredicates.includes(existing.predicate) &&
      this.isSameNode(newTriple.subject, existing.subject)
    ) {
      // 简化检查：如果 startedAt > endedAt，则冲突
      if (
        newTriple.predicate === 'startedAt' &&
        existing.predicate === 'endedAt' &&
        isLiteralValue(newTriple.object) &&
        isLiteralValue(existing.object)
      ) {
        const startTime = Number(newTriple.object['@value']);
        const endTime = Number(existing.object['@value']);

        if (startTime > endTime) {
          return this.createConflict(
            'temporal',
            'high',
            existing,
            newTriple,
            `Temporal inconsistency: start time (${startTime}) > end time (${endTime})`
          );
        }
      }
    }

    return null;
  }

  private checkSymmetry(newTriple: Triple, existing: Triple): Conflict | null {
    const constraint = this.constraints.get(newTriple.predicate);

    if (constraint?.symmetric) {
      // 对称关系：如果 A-P-B 存在，则 B-P-A 也应存在
      if (
        !isLiteralValue(newTriple.object) &&
        this.isSameNode(newTriple.subject, existing.object as TripleNode) &&
        this.isSameNode(newTriple.object, existing.subject) &&
        newTriple.predicate === existing.predicate
      ) {
        // 对称关系已存在，无冲突
        return null;
      }
    }

    return null;
  }

  private checkInverse(newTriple: Triple, existing: Triple): Conflict | null {
    const constraint = this.constraints.get(newTriple.predicate);

    if (constraint?.inverse) {
      // 检查逆关系一致性
      if (
        !isLiteralValue(newTriple.object) &&
        existing.predicate === constraint.inverse &&
        this.isSameNode(newTriple.subject, existing.object as TripleNode) &&
        this.isSameNode(newTriple.object, existing.subject)
      ) {
        // 逆关系一致，无冲突
        return null;
      }
    }

    return null;
  }

  private isSameNode(a: TripleNode, b: TripleNode): boolean {
    return a['@id'] === b['@id'];
  }

  private isSameObject(
    a: TripleNode | LiteralValue,
    b: TripleNode | LiteralValue
  ): boolean {
    if (isLiteralValue(a) && isLiteralValue(b)) {
      return a['@value'] === b['@value'];
    }
    if (!isLiteralValue(a) && !isLiteralValue(b)) {
      return a['@id'] === b['@id'];
    }
    return false;
  }

  private createConflict(
    type: ConflictType,
    severity: ConflictSeverity,
    existingTriple: Triple,
    newTriple: Triple,
    description: string
  ): Conflict {
    return {
      id: `conflict_${++this.conflictIdCounter}`,
      type,
      severity,
      existingTriple,
      newTriple,
      description,
    };
  }

  private mergeTriples(existing: Triple, newTriple: Triple): Triple {
    // 合并策略：取较高置信度，保留较新时间戳
    return {
      ...existing,
      confidence: Math.max(existing.confidence, newTriple.confidence),
      timestamp: Math.max(existing.timestamp, newTriple.timestamp),
      metadata: {
        ...existing.metadata,
        ...newTriple.metadata,
        merged: true,
        mergedAt: Date.now(),
      },
    };
  }
}
