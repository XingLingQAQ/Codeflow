/**
 * 语义冲突检测类型定义
 * S-P-O 三元组冲突探测
 */
/**
 * 默认冲突检测配置
 */
export const DEFAULT_CONFLICT_CONFIG = {
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
export const COMMON_PREDICATE_CONSTRAINTS = [
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
//# sourceMappingURL=ConflictTypes.js.map