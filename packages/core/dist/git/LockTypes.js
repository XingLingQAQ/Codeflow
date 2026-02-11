/**
 * 执行锁机制类型定义
 * 一致性保障与并发控制
 */
/**
 * 默认锁配置
 */
export const DEFAULT_LOCK_CONFIG = {
    defaultTimeout: 30000, // 30 seconds
    maxRetries: 3,
    retryDelay: 100,
    deadlockDetectionInterval: 5000, // 5 seconds
    enableDeadlockDetection: true,
    enableFairness: true,
};
/**
 * 资源类型常量
 */
export const RESOURCE_TYPES = {
    GIT: 'git',
    VECTOR_STORE: 'vector_store',
    TRIPLE_STORE: 'triple_store',
    CONVERSATION: 'conversation',
    SNAPSHOT: 'snapshot',
};
/**
 * 锁优先级
 */
export const LOCK_PRIORITIES = {
    LOW: 1,
    NORMAL: 5,
    HIGH: 10,
    CRITICAL: 100,
};
//# sourceMappingURL=LockTypes.js.map