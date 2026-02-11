/**
 * 语义冲突检测器实现
 * S-P-O 三元组冲突探测
 */
import { IConflictDetector, ConflictDetectionConfig, ConflictResult, Conflict, ConflictResolution, PredicateConstraint } from './ConflictTypes.js';
import { Triple } from './types.js';
export declare class ConflictDetector implements IConflictDetector {
    private config;
    private constraints;
    private conflictIdCounter;
    constructor(config?: Partial<ConflictDetectionConfig>);
    private initializeConstraints;
    configure(config: Partial<ConflictDetectionConfig>): void;
    addConstraint(constraint: PredicateConstraint): void;
    removeConstraint(predicate: string): void;
    detect(newTriple: Triple, existingTriples: Triple[]): ConflictResult;
    detectBatch(newTriples: Triple[], existingTriples: Triple[]): ConflictResult;
    resolve(conflict: Conflict, strategy: ConflictResolution['strategy']): Triple | null;
    private checkConflict;
    private checkContradiction;
    private checkCardinality;
    private checkTypeMismatch;
    private checkTemporal;
    /**
     * 解析时间值，支持多种格式
     */
    private parseTemporalValue;
    private checkSymmetry;
    private checkInverse;
    private isSameNode;
    private isSameObject;
    private createConflict;
    private mergeTriples;
}
//# sourceMappingURL=ConflictDetector.d.ts.map