/**
 * 语义冲突检测器实现
 * S-P-O 三元组冲突探测
 */
import { DEFAULT_CONFLICT_CONFIG, COMMON_PREDICATE_CONSTRAINTS, } from './ConflictTypes.js';
import { isLiteralValue } from './types.js';
export class ConflictDetector {
    constructor(config = {}) {
        this.constraints = new Map();
        this.conflictIdCounter = 0;
        this.config = { ...DEFAULT_CONFLICT_CONFIG, ...config };
        this.initializeConstraints();
    }
    initializeConstraints() {
        for (const constraint of COMMON_PREDICATE_CONSTRAINTS) {
            this.constraints.set(constraint.predicate, constraint);
        }
        for (const constraint of this.config.predicateConstraints) {
            this.constraints.set(constraint.predicate, constraint);
        }
    }
    configure(config) {
        this.config = { ...this.config, ...config };
    }
    addConstraint(constraint) {
        this.constraints.set(constraint.predicate, constraint);
    }
    removeConstraint(predicate) {
        this.constraints.delete(predicate);
    }
    detect(newTriple, existingTriples) {
        const startTime = Date.now();
        const conflicts = [];
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
    detectBatch(newTriples, existingTriples) {
        const startTime = Date.now();
        const conflicts = [];
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
    resolve(conflict, strategy) {
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
    checkConflict(newTriple, existing) {
        // 跳过低置信度三元组
        if (newTriple.confidence < this.config.confidenceThreshold ||
            existing.confidence < this.config.confidenceThreshold) {
            return null;
        }
        // 检查各类冲突
        if (this.config.enableContradiction) {
            const contradiction = this.checkContradiction(newTriple, existing);
            if (contradiction)
                return contradiction;
        }
        if (this.config.enableCardinality) {
            const cardinality = this.checkCardinality(newTriple, existing);
            if (cardinality)
                return cardinality;
        }
        if (this.config.enableTypeMismatch) {
            const typeMismatch = this.checkTypeMismatch(newTriple, existing);
            if (typeMismatch)
                return typeMismatch;
        }
        if (this.config.enableTemporal) {
            const temporal = this.checkTemporal(newTriple, existing);
            if (temporal)
                return temporal;
        }
        if (this.config.enableSymmetry) {
            const symmetry = this.checkSymmetry(newTriple, existing);
            if (symmetry)
                return symmetry;
        }
        if (this.config.enableInverse) {
            const inverse = this.checkInverse(newTriple, existing);
            if (inverse)
                return inverse;
        }
        return null;
    }
    checkContradiction(newTriple, existing) {
        // 同一主语和谓词，但宾语互斥
        if (this.isSameNode(newTriple.subject, existing.subject) &&
            newTriple.predicate === existing.predicate) {
            const constraint = this.constraints.get(newTriple.predicate);
            // 检查互斥谓词
            if (constraint?.mutuallyExclusive) {
                for (const exclusive of constraint.mutuallyExclusive) {
                    if (existing.predicate === exclusive) {
                        return this.createConflict('contradiction', 'high', existing, newTriple, `Predicate "${newTriple.predicate}" is mutually exclusive with "${exclusive}"`);
                    }
                }
            }
            // 检查布尔值矛盾
            if (isLiteralValue(newTriple.object) &&
                isLiteralValue(existing.object) &&
                typeof newTriple.object['@value'] === 'boolean' &&
                typeof existing.object['@value'] === 'boolean' &&
                newTriple.object['@value'] !== existing.object['@value']) {
                return this.createConflict('contradiction', 'critical', existing, newTriple, `Boolean contradiction: ${existing.object['@value']} vs ${newTriple.object['@value']}`);
            }
        }
        return null;
    }
    checkCardinality(newTriple, existing) {
        const constraint = this.constraints.get(newTriple.predicate);
        if (constraint?.cardinality === 'single') {
            if (this.isSameNode(newTriple.subject, existing.subject) &&
                newTriple.predicate === existing.predicate &&
                !this.isSameObject(newTriple.object, existing.object)) {
                return this.createConflict('cardinality', 'medium', existing, newTriple, `Predicate "${newTriple.predicate}" has single cardinality but multiple values found`);
            }
        }
        return null;
    }
    checkTypeMismatch(newTriple, existing) {
        const constraint = this.constraints.get(newTriple.predicate);
        if (constraint?.range && !isLiteralValue(newTriple.object)) {
            const objectType = newTriple.object['@type'];
            const types = Array.isArray(objectType) ? objectType : [objectType];
            if (!types.some(t => constraint.range.includes(t || ''))) {
                return this.createConflict('type_mismatch', 'medium', existing, newTriple, `Object type "${objectType}" not in allowed range: ${constraint.range.join(', ')}`);
            }
        }
        return null;
    }
    checkTemporal(newTriple, existing) {
        // 检查时间相关谓词的时序一致性
        const temporalPredicates = ['startedAt', 'endedAt', 'occurredAt', 'before', 'after', 'during', 'createdAt', 'updatedAt', 'deletedAt'];
        if (temporalPredicates.includes(newTriple.predicate) &&
            temporalPredicates.includes(existing.predicate) &&
            this.isSameNode(newTriple.subject, existing.subject)) {
            // 检查 startedAt vs endedAt 冲突
            if (newTriple.predicate === 'startedAt' &&
                existing.predicate === 'endedAt' &&
                isLiteralValue(newTriple.object) &&
                isLiteralValue(existing.object)) {
                const startTime = this.parseTemporalValue(newTriple.object['@value']);
                const endTime = this.parseTemporalValue(existing.object['@value']);
                if (startTime !== null && endTime !== null && startTime > endTime) {
                    return this.createConflict('temporal', 'high', existing, newTriple, `Temporal inconsistency: start time (${startTime}) > end time (${endTime})`);
                }
            }
            // 检查 endedAt vs startedAt 冲突（反向）
            if (newTriple.predicate === 'endedAt' &&
                existing.predicate === 'startedAt' &&
                isLiteralValue(newTriple.object) &&
                isLiteralValue(existing.object)) {
                const endTime = this.parseTemporalValue(newTriple.object['@value']);
                const startTime = this.parseTemporalValue(existing.object['@value']);
                if (startTime !== null && endTime !== null && endTime < startTime) {
                    return this.createConflict('temporal', 'high', existing, newTriple, `Temporal inconsistency: end time (${endTime}) < start time (${startTime})`);
                }
            }
            // 检查 before/after 关系一致性
            if (newTriple.predicate === 'before' &&
                existing.predicate === 'after' &&
                !isLiteralValue(newTriple.object) &&
                !isLiteralValue(existing.object)) {
                // A before B 和 A after B 是矛盾的
                if (this.isSameNode(newTriple.object, existing.object)) {
                    return this.createConflict('temporal', 'critical', existing, newTriple, `Temporal contradiction: subject is both before and after the same entity`);
                }
            }
            // 检查 createdAt vs deletedAt 冲突
            if (newTriple.predicate === 'createdAt' &&
                existing.predicate === 'deletedAt' &&
                isLiteralValue(newTriple.object) &&
                isLiteralValue(existing.object)) {
                const createdTime = this.parseTemporalValue(newTriple.object['@value']);
                const deletedTime = this.parseTemporalValue(existing.object['@value']);
                if (createdTime !== null && deletedTime !== null && createdTime > deletedTime) {
                    return this.createConflict('temporal', 'critical', existing, newTriple, `Temporal inconsistency: created time (${createdTime}) > deleted time (${deletedTime})`);
                }
            }
            // 检查 updatedAt vs createdAt 冲突
            if (newTriple.predicate === 'updatedAt' &&
                existing.predicate === 'createdAt' &&
                isLiteralValue(newTriple.object) &&
                isLiteralValue(existing.object)) {
                const updatedTime = this.parseTemporalValue(newTriple.object['@value']);
                const createdTime = this.parseTemporalValue(existing.object['@value']);
                if (updatedTime !== null && createdTime !== null && updatedTime < createdTime) {
                    return this.createConflict('temporal', 'medium', existing, newTriple, `Temporal inconsistency: updated time (${updatedTime}) < created time (${createdTime})`);
                }
            }
        }
        return null;
    }
    /**
     * 解析时间值，支持多种格式
     */
    parseTemporalValue(value) {
        if (typeof value === 'number') {
            return value;
        }
        if (typeof value === 'string') {
            // 尝试解析 ISO 日期字符串
            const date = Date.parse(value);
            if (!isNaN(date)) {
                return date;
            }
            // 尝试解析纯数字字符串
            const num = Number(value);
            if (!isNaN(num)) {
                return num;
            }
        }
        return null;
    }
    checkSymmetry(newTriple, existing) {
        const constraint = this.constraints.get(newTriple.predicate);
        if (constraint?.symmetric) {
            // 对称关系：如果 A-P-B 存在，则 B-P-A 也应存在
            if (!isLiteralValue(newTriple.object) &&
                this.isSameNode(newTriple.subject, existing.object) &&
                this.isSameNode(newTriple.object, existing.subject) &&
                newTriple.predicate === existing.predicate) {
                // 对称关系已存在，无冲突
                return null;
            }
        }
        return null;
    }
    checkInverse(newTriple, existing) {
        const constraint = this.constraints.get(newTriple.predicate);
        if (constraint?.inverse) {
            // 检查逆关系一致性
            if (!isLiteralValue(newTriple.object) &&
                existing.predicate === constraint.inverse &&
                this.isSameNode(newTriple.subject, existing.object) &&
                this.isSameNode(newTriple.object, existing.subject)) {
                // 逆关系一致，无冲突
                return null;
            }
        }
        return null;
    }
    isSameNode(a, b) {
        return a['@id'] === b['@id'];
    }
    isSameObject(a, b) {
        if (isLiteralValue(a) && isLiteralValue(b)) {
            return a['@value'] === b['@value'];
        }
        if (!isLiteralValue(a) && !isLiteralValue(b)) {
            return a['@id'] === b['@id'];
        }
        return false;
    }
    createConflict(type, severity, existingTriple, newTriple, description) {
        return {
            id: `conflict_${++this.conflictIdCounter}`,
            type,
            severity,
            existingTriple,
            newTriple,
            description,
        };
    }
    mergeTriples(existing, newTriple) {
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
//# sourceMappingURL=ConflictDetector.js.map